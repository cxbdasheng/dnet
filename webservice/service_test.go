package webservice

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testRule(t *testing.T, target string) Rule {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return Rule{ID: "one", Name: "one", Domain: "one.example", Network: "tcp4", ListenAddress: "127.0.0.1", ListenPort: port, Target: target, TimeoutSec: 1}
}
func request(t *testing.T, r Rule, host, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://"+r.address()+path, nil)
	req.Host = host
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}
func body(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func TestDomainRoutingAndRewrite(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s|%s|%s|%s|%s|%s", r.Method, r.URL.RequestURI(), r.Host, r.Header.Get("X-Forwarded-For"), r.Header.Get("X-Forwarded-Host"), r.Header.Get("X-Real-IP"))
	}))
	defer upstream.Close()
	first := testRule(t, upstream.URL+"/base")
	second := first
	second.ID = "two"
	second.Domain = "two.example"
	second.PreserveHost = true
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{first, second}, nil); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", "http://"+first.address()+"/hello%20world?q=1", strings.NewReader("data"))
	req.Host = "ONE.EXAMPLE:8080"
	req.Header.Set("X-Forwarded-For", "forged")
	req.Header.Set("X-Real-IP", "forged")
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	value := body(t, res)
	if !strings.Contains(value, "POST|/base/hello%20world?q=1|") || strings.Contains(value, "forged") || !strings.Contains(value, "|127.0.0.1|ONE.EXAMPLE:8080|127.0.0.1") {
		t.Fatal(value)
	}
	if got := body(t, request(t, second, "two.example", "/")); !strings.Contains(got, "|two.example|") {
		t.Fatal(got)
	}
	if got := request(t, first, "unknown.example", "/").StatusCode; got != 404 {
		t.Fatal(got)
	}
}
func TestApplyRollbackAndUpdate(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, r.URL.Path) }))
	defer up.Close()
	r := testRule(t, up.URL+"/old")
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	next := r
	next.Target = up.URL + "/new"
	if err := m.Apply([]Rule{next}, func() error { return errors.New("save failed") }); err == nil {
		t.Fatal("save failure ignored")
	}
	if got := body(t, request(t, r, r.Domain, "/")); got != "/old/" {
		t.Fatal(got)
	}
	if err := m.Apply([]Rule{next}, nil); err != nil {
		t.Fatal(err)
	}
	if got := body(t, request(t, r, r.Domain, "/")); got != "/new/" {
		t.Fatal(got)
	}
	extra := testRule(t, up.URL)
	extra.ID = "extra"
	if err := m.Apply([]Rule{next, extra}, func() error { return errors.New("save failed") }); err == nil {
		t.Fatal("save failure ignored")
	}
	l, err := net.Listen(extra.Network, extra.address())
	if err != nil {
		t.Fatal("staged socket leaked", err)
	}
	l.Close()
	if err := m.Apply(nil, nil); err != nil {
		t.Fatal(err)
	}
	l, err = net.Listen(r.Network, r.address())
	if err != nil {
		t.Fatal("disabled socket not released", err)
	}
	l.Close()
}
func TestAuthenticationAndSecrets(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("proxy credential leaked upstream")
		}
		fmt.Fprint(w, "ok")
	}))
	defer up.Close()
	r := testRule(t, up.URL)
	r.AuthEnabled = true
	r.Username = "reader"
	r.Password = "test-secret"
	rules, err := Prepare([]Rule{r}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rules[0].Password != "" || rules[0].PasswordHash == "test-secret" {
		t.Fatal("password was not hashed")
	}
	visible := PublicRules(rules)
	if visible[0].PasswordHash != "" || visible[0].Password != "" || !visible[0].HasPassword {
		t.Fatal("secret exposed")
	}
	retained, err := Prepare(visible, rules)
	if err != nil || retained[0].PasswordHash != rules[0].PasswordHash {
		t.Fatal("password not retained", err)
	}
	visible[0].ID = "new"
	if _, err := Prepare(visible, rules); err == nil {
		t.Fatal("new rule inherited secret")
	}
	m := NewManager()
	defer m.Close()
	if err := m.Apply(rules, nil); err != nil {
		t.Fatal(err)
	}
	if got := request(t, r, r.Domain, "/").StatusCode; got != 401 {
		t.Fatal(got)
	}
	req, _ := http.NewRequest("GET", "http://"+r.address(), nil)
	req.Host = r.Domain
	req.SetBasicAuth("reader", "test-secret")
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := body(t, res); got != "ok" {
		t.Fatal(got)
	}
}
func TestHTTPSAndUpstreamVerification(t *testing.T) {
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "ok") }))
	defer up.Close()
	r := testRule(t, up.URL)
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	if got := request(t, r, r.Domain, "/").StatusCode; got != 502 {
		t.Fatal("untrusted upstream certificate accepted", got)
	}
	if err := m.Apply(nil, nil); err != nil {
		t.Fatal(err)
	}
	// Reuse the test server certificate to exercise frontend HTTPS independently.
	cert := up.TLS.Certificates[0]
	dir := t.TempDir()
	r.TLS = true
	r.Certificate = filepath.Join(dir, "cert.pem")
	r.Key = filepath.Join(dir, "key.pem")
	os.WriteFile(r.Certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600)
	key, err := marshalTestKey(cert)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(r.Key, key, 0600)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "tls-ok") }))
	defer plain.Close()
	r.Target = plain.URL
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	client := up.Client()
	client.Timeout = 3 * time.Second
	req, _ := http.NewRequest("GET", "https://"+r.address(), nil)
	req.Host = r.Domain
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := body(t, res); got != "tls-ok" {
		t.Fatal(got)
	}
}
func TestWebSocketUpgradeAndShutdown(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer c.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		rw.Flush()
		io.Copy(c, c)
	}))
	defer up.Close()
	r := testRule(t, up.URL)
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", r.address())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", r.Domain)
	reader := bufio.NewReader(c)
	res, err := http.ReadResponse(reader, nil)
	if err != nil || res.StatusCode != 101 {
		t.Fatal("upgrade failed", err)
	}
	c.Write([]byte("ping"))
	b := make([]byte, 4)
	if _, err := io.ReadFull(reader, b); err != nil || string(b) != "ping" {
		t.Fatal("duplex stream failed", err)
	}
	m.Close()
	if _, err := reader.ReadByte(); err == nil {
		t.Fatal("upgraded connection survived shutdown")
	}
}
func TestRejectLoopAndInvalidRules(t *testing.T) {
	r := testRule(t, "http://127.0.0.1:3000")
	r.Target = "http://" + r.address()
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	if got := request(t, r, r.Domain, "/").StatusCode; got != 502 {
		t.Fatal(got)
	}
	for _, change := range []func(*Rule){func(r *Rule) { r.Domain = "https://bad" }, func(r *Rule) { r.Target = "ftp://host" }, func(r *Rule) { r.Target = "http://u:p@host" }, func(r *Rule) { r.ListenAddress = "::1" }, func(r *Rule) { r.TimeoutSec = 0 }} {
		bad := r
		change(&bad)
		if Validate([]Rule{bad}) == nil {
			t.Fatal("invalid rule accepted")
		}
	}
	second := r
	second.ID = "second"
	second.Domain = "ONE.EXAMPLE"
	if Validate([]Rule{r, second}) == nil {
		t.Fatal("duplicate domain accepted")
	}
}

func marshalTestKey(cert tls.Certificate) ([]byte, error) {
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), nil
}

func TestDualStackRoutingAndRollback(t *testing.T) {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 unavailable: %v", err)
	}
	probe.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "dual-ok") }))
	defer upstream.Close()
	rule := testRule(t, upstream.URL)
	rule.Network, rule.ListenAddress = "tcp", "::"
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{rule}, nil); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"127.0.0.1", "::1"} {
		clientRule := rule
		clientRule.ListenAddress = address
		if got := body(t, request(t, clientRule, rule.Domain, "/")); got != "dual-ok" {
			t.Fatal(got)
		}
	}
	if status := m.Status(); len(status) != 1 || !status[0].Listening {
		t.Fatalf("dual stack status: %+v", status)
	}
	duplicate := rule
	duplicate.ID = "two"
	duplicate.Network = "tcp4"
	duplicate.ListenAddress = "0.0.0.0"
	if Validate([]Rule{rule, duplicate}) == nil {
		t.Fatal("overlapping domain accepted")
	}
	invalid := rule
	invalid.ListenAddress = "127.0.0.1"
	if Validate([]Rule{invalid}) == nil {
		t.Fatal("dual stack accepted single IPv4 address")
	}
	// Occupy IPv6 only. The staged IPv4 socket must be released on failure.
	occupied, err := net.Listen("tcp6", "[::]:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	extra := rule
	extra.ID = "extra"
	extra.ListenPort = occupied.Addr().(*net.TCPAddr).Port
	saved := false
	if err := m.Apply([]Rule{rule, extra}, func() error { saved = true; return nil }); err == nil || saved {
		t.Fatal("partial dual-stack configuration committed")
	}
	free, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", extra.ListenPort))
	if err != nil {
		t.Fatal("staged IPv4 listener leaked", err)
	}
	free.Close()
	clientRule := rule
	clientRule.ListenAddress = "::1"
	if got := body(t, request(t, clientRule, rule.Domain, "/")); got != "dual-ok" {
		t.Fatal("old service lost", got)
	}
	if err := m.Apply(nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, family := range []struct{ network, address string }{{"tcp4", "0.0.0.0"}, {"tcp6", "::"}} {
		l, err := net.Listen(family.network, net.JoinHostPort(family.address, fmt.Sprint(rule.ListenPort)))
		if err != nil {
			t.Fatal("disabled dual-stack socket leaked", err)
		}
		l.Close()
	}
}
