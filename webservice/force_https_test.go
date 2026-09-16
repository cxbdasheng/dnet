package webservice

import (
	"context"
	"crypto/tls"
	"github.com/cxbdasheng/dnet/certificates"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestDomainForceHTTPS(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "backend") }))
	defer backend.Close()
	c := managedFixture(t, 1, "one.example", "two.example")
	r := testRule(t, backend.URL)
	r.TLS = true
	r.ForceHTTPS = true
	r.CertificateID = c.ID
	other := r
	other.ID = "other"
	other.Name = "other"
	other.Domain = "two.example"
	other.ForceHTTPS = false
	m := NewManager()
	defer m.Close()
	apply := func() {
		t.Helper()
		if err := m.ApplyCertificates([]Rule{r, other}, []certificates.Certificate{c}, nil); err != nil {
			t.Fatal(err)
		}
	}
	apply()
	idle, err := net.Dial("tcp", r.address())
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", r.address())
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	port := strconv.Itoa(r.ListenPort)
	check := func(scheme, host string, status int, location string) {
		t.Helper()
		res, err := client.Get(scheme + "://" + host + "/a%2Fb?x=1&x=2")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != status || res.Header.Get("Location") != location {
			t.Fatalf("%s %s: %d %q", scheme, host, res.StatusCode, res.Header.Get("Location"))
		}
	}
	check("http", "one.example:"+port, 307, "https://one.example:"+port+"/a%2Fb?x=1&x=2")
	check("http", "two.example:"+port, 400, "")
	check("http", "unknown.example:"+port, 404, "")
	check("https", "one.example:"+port, 200, "")
	check("https", "two.example:"+port, 200, "")
	check("http", "one.example", 307, "https://one.example/a%2Fb?x=1&x=2")
	r.ForceHTTPS = false
	other.ForceHTTPS = true
	apply()
	check("http", "one.example:"+port, 400, "")
	check("http", "two.example:"+port, 307, "https://two.example:"+port+"/a%2Fb?x=1&x=2")
	r.TLS = false
	r.ForceHTTPS = true
	if Validate([]Rule{r}) == nil {
		t.Fatal("force HTTPS without TLS accepted")
	}
}
