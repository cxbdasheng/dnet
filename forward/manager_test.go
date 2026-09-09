package forward

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func endpoint(t *testing.T, l net.Listener) (string, int) {
	t.Helper()
	host, port, _ := net.SplitHostPort(l.Addr().String())
	p, _ := strconv.Atoi(port)
	return host, p
}
func backend(t *testing.T, fn func(net.Conn)) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); c.SetDeadline(time.Now().Add(5 * time.Second)); fn(c) }()
		}
	}()
	return l
}
func rule(t *testing.T, target net.Listener) Rule {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host, port := endpoint(t, l)
	l.Close()
	th, tp := endpoint(t, target)
	return Rule{ID: "test", Name: "test", Enabled: true, Network: "tcp4", ListenAddress: host, ListenPort: port, TargetHost: th, TargetPort: tp, DialTimeoutSec: 1, MaxConnections: 10}
}
func connect(t *testing.T, r Rule) *net.TCPConn {
	t.Helper()
	c, err := net.DialTimeout(r.Network, r.address(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(5 * time.Second))
	return c.(*net.TCPConn)
}
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}
func assertClosed(t *testing.T, c net.Conn) {
	t.Helper()
	_, err := c.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("connection was not closed")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("connection only ended at the test deadline", err)
	}
}

func TestRelayHalfCloseAndStats(t *testing.T) {
	target := backend(t, func(c net.Conn) { b, _ := io.ReadAll(c); c.Write(b) })
	r := rule(t, target)
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	c := connect(t, r)
	defer c.Close()
	payload := bytes.Repeat([]byte("hello"), 20000)
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	c.CloseWrite()
	got, err := io.ReadAll(c)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("response truncated: %d", len(got))
	}
	eventually(t, func() bool {
		s := m.Status()[0]
		return s.Connections == 0 && s.Upload == int64(len(payload)) && s.Download == int64(len(payload))
	})
}
func TestApplyFailurePreservesOldListener(t *testing.T) {
	target := backend(t, func(c net.Conn) { io.Copy(c, c) })
	r := rule(t, target)
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	changed := r
	_, changed.ListenPort = endpoint(t, target)
	if err := m.Apply([]Rule{changed}, nil); err == nil {
		t.Fatal("expected bind failure")
	}
	changed = rule(t, target)
	if err := m.Apply([]Rule{changed}, func() error { return fmt.Errorf("disk full") }); err == nil {
		t.Fatal("expected save failure")
	}
	l, err := net.Listen(changed.Network, changed.address())
	if err != nil {
		t.Fatal("staged socket leaked", err)
	}
	l.Close()
	c := connect(t, r)
	defer c.Close()
	c.Write([]byte("ok"))
	b := make([]byte, 2)
	if _, err := io.ReadFull(c, b); err != nil {
		t.Fatal(err)
	}
}
func TestTargetChangesOnlyAffectNewConnections(t *testing.T) {
	first := backend(t, func(c net.Conn) { io.Copy(c, c) })
	second := backend(t, func(c net.Conn) { b := make([]byte, 1); c.Read(b); c.Write([]byte("B")) })
	r := rule(t, first)
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	old := connect(t, r)
	defer old.Close()
	old.Write([]byte("A"))
	b := make([]byte, 1)
	io.ReadFull(old, b)
	r.TargetHost, r.TargetPort = endpoint(t, second)
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	fresh := connect(t, r)
	defer fresh.Close()
	fresh.Write([]byte("X"))
	if _, err := io.ReadFull(fresh, b); err != nil || string(b) != "B" {
		t.Fatal("new target not applied", err)
	}
	old.Write([]byte("C"))
	if _, err := io.ReadFull(old, b); err != nil || string(b) != "C" {
		t.Fatal("old connection interrupted", err)
	}
}
func TestAdmissionAndDisable(t *testing.T) {
	target := backend(t, func(c net.Conn) { io.Copy(c, c) })
	r := rule(t, target)
	r.MaxConnections = 1
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	first := connect(t, r)
	defer first.Close()
	first.Write([]byte("1"))
	b := make([]byte, 1)
	io.ReadFull(first, b)
	second := connect(t, r)
	defer second.Close()
	assertClosed(t, second)
	r.Enabled = false
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	assertClosed(t, first)
	r.Enabled = true
	r.AllowCIDRs = []string{"192.0.2.0/24"}
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	denied := connect(t, r)
	defer denied.Close()
	assertClosed(t, denied)
}
func TestIdleTimeoutAndShutdown(t *testing.T) {
	target := backend(t, func(c net.Conn) { io.Copy(c, c) })
	r := rule(t, target)
	r.IdleTimeoutSec = 1
	m := NewManager()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	c := connect(t, r)
	defer c.Close()
	assertClosed(t, c)
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown hung")
	}
	if err := m.Apply([]Rule{r}, nil); err == nil {
		t.Fatal("closed manager restarted")
	}
}
func TestIPv6ListenerToIPv4Target(t *testing.T) {
	l, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skip("IPv6 unavailable:", err)
	}
	host, p := endpoint(t, l)
	l.Close()
	target := backend(t, func(c net.Conn) { io.Copy(c, c) })
	r := rule(t, target)
	r.Network = "tcp6"
	r.ListenAddress = host
	r.ListenPort = p
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	c := connect(t, r)
	defer c.Close()
	c.Write([]byte("v6"))
	b := make([]byte, 2)
	if _, err := io.ReadFull(c, b); err != nil || string(b) != "v6" {
		t.Fatal("IPv6 relay failed", err)
	}
}
func TestValidation(t *testing.T) {
	target := backend(t, func(c net.Conn) {})
	r := rule(t, target)
	for _, mutate := range []func(*Rule){func(r *Rule) { r.ListenPort = 0 }, func(r *Rule) { r.Network = "udp" }, func(r *Rule) { r.AllowCIDRs = []string{"bad"} }, func(r *Rule) { r.MaxConnections = 0 }, func(r *Rule) { r.ListenAddress = "::" }, func(r *Rule) { r.TargetPort = r.ListenPort; r.TargetHost = r.ListenAddress }} {
		bad := r
		mutate(&bad)
		if Validate([]Rule{bad}) == nil {
			t.Fatal("invalid rule accepted")
		}
	}
	if Validate([]Rule{r, r}) == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestTargetHostValidation(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1", "2001:db8::1", "localhost", "nas.local", "example.com.", "xn--fiqs8s.example"} {
		if !validTargetHost(host) {
			t.Errorf("valid host rejected: %q", host)
		}
	}
	for _, host := range []string{"", "https://example.com", "example.com:443", "[::1]", "999.1.1.1", "01.2.3.4", "a..com", "-a.com", "a_.com", "example.com/path", " example.com", "a.com?x=1"} {
		if validTargetHost(host) {
			t.Errorf("invalid host accepted: %q", host)
		}
	}
}
