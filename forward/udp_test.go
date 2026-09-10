package forward

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

func udpBackend(t *testing.T, prefix string) net.PacketConn {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	go func() {
		b := make([]byte, 65535)
		for {
			n, a, err := c.ReadFrom(b)
			if err != nil {
				return
			}
			c.WriteTo(append([]byte(prefix), b[:n]...), a)
		}
	}()
	return c
}
func udpRule(t *testing.T, target net.PacketConn) Rule {
	t.Helper()
	l, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.LocalAddr().(*net.UDPAddr).Port
	l.Close()
	a := target.LocalAddr().(*net.UDPAddr)
	return Rule{ID: "udp", Name: "UDP", Enabled: true, Network: "udp4", ListenAddress: "127.0.0.1", ListenPort: port, TargetHost: a.IP.String(), TargetPort: a.Port, DialTimeoutSec: 1, IdleTimeoutSec: 1, MaxConnections: 10}
}
func udpClient(t *testing.T, r Rule) net.Conn {
	t.Helper()
	c, err := net.Dial(r.Network, r.address())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func udpExchange(t *testing.T, c net.Conn, payload, want []byte) {
	t.Helper()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 65535)
	n, err := c.Read(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b[:n], want) {
		t.Fatalf("reply differs: got %d bytes want %d", n, len(want))
	}
}
func TestUDPDatagramsAndClientIsolation(t *testing.T) {
	r := udpRule(t, udpBackend(t, ""))
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	a, b := udpClient(t, r), udpClient(t, r)
	size := 60000
	if runtime.GOOS == "darwin" {
		size = 8000
	} // macOS defaults to a smaller UDP send buffer.
	for _, data := range [][]byte{[]byte("first"), {}, bytes.Repeat([]byte("x"), size)} {
		udpExchange(t, a, data, data)
		udpExchange(t, b, []byte("second"), []byte("second"))
	}
	eventually(t, func() bool {
		s := m.Status()[0]
		return s.Connections == 2 && s.Upload == int64(size+23) && s.Download == int64(size+23)
	})
	eventually(t, func() bool { return m.Status()[0].Connections == 0 })
	udpExchange(t, a, []byte("new"), []byte("new"))
}
func TestUDPUpdatesAndRollback(t *testing.T) {
	r := udpRule(t, udpBackend(t, "a"))
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	c := udpClient(t, r)
	udpExchange(t, c, []byte("x"), []byte("ax"))
	next := r
	next.TargetPort = udpBackend(t, "b").LocalAddr().(*net.UDPAddr).Port
	if err := m.Apply([]Rule{next}, func() error { return errors.New("save failed") }); err == nil {
		t.Fatal("save failure ignored")
	}
	udpExchange(t, c, []byte("x"), []byte("ax"))
	if err := m.Apply([]Rule{next}, nil); err != nil {
		t.Fatal(err)
	}
	udpExchange(t, c, []byte("x"), []byte("bx"))
	// A failed new bind must leave the existing mapping active.
	occupied := udpBackend(t, "")
	bad := next
	bad.ID = "other"
	bad.ListenPort = occupied.LocalAddr().(*net.UDPAddr).Port
	if err := m.Apply([]Rule{next, bad}, nil); err == nil {
		t.Fatal("bind failure ignored")
	}
	udpExchange(t, c, []byte("x"), []byte("bx"))
	if err := m.Apply(nil, nil); err != nil {
		t.Fatal(err)
	}
	l, err := net.ListenPacket(r.Network, r.address())
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
}
func TestUDPLimitsWhitelistAndShutdown(t *testing.T) {
	r := udpRule(t, udpBackend(t, ""))
	r.MaxConnections = 1
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	a, b := udpClient(t, r), udpClient(t, r)
	udpExchange(t, a, []byte("ok"), []byte("ok"))
	b.SetDeadline(time.Now().Add(150 * time.Millisecond))
	b.Write([]byte("blocked"))
	if _, err := b.Read(make([]byte, 64)); err == nil {
		t.Fatal("session limit ignored")
	}
	r.AllowCIDRs = []string{"192.0.2.0/24"}
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	a.SetDeadline(time.Now().Add(150 * time.Millisecond))
	a.Write([]byte("blocked"))
	if _, err := a.Read(make([]byte, 64)); err == nil {
		t.Fatal("whitelist ignored")
	}
	if m.Status()[0].Connections != 0 {
		t.Fatal("old sessions retained")
	}
	m.Close()
}
func TestUDPIPv6ToIPv4AndTCPCoexistence(t *testing.T) {
	r := udpRule(t, udpBackend(t, ""))
	l, err := net.ListenPacket("udp6", "[::1]:0")
	if err != nil {
		t.Skip(err)
	}
	r.Network = "udp6"
	r.ListenAddress = "::1"
	r.ListenPort = l.LocalAddr().(*net.UDPAddr).Port
	l.Close()
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	udpExchange(t, udpClient(t, r), []byte("v6"), []byte("v6"))
	tcp, err := net.Listen("tcp6", net.JoinHostPort("::1", strconv.Itoa(r.ListenPort)))
	if err != nil {
		t.Fatal(err)
	}
	tcp.Close()
}

func TestUDPStagedSocketRollback(t *testing.T) {
	r := udpRule(t, udpBackend(t, ""))
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, func() error { return errors.New("cannot save") }); err == nil {
		t.Fatal("expected save failure")
	}
	socket, err := net.ListenPacket(r.Network, r.address())
	if err != nil {
		t.Fatal("staged UDP socket leaked", err)
	}
	socket.Close()
	if len(m.Status()) != 0 {
		t.Fatal("failed rule became active")
	}
	// TCP and UDP can use the same numeric port.
	tcp := r
	tcp.ID = "tcp"
	tcp.Network = "tcp4"
	if err := m.Apply([]Rule{r, tcp}, nil); err != nil {
		t.Fatal(err)
	}
	udpExchange(t, udpClient(t, r), []byte("both"), []byte("both"))
}

func TestUDPConcurrentTrafficAndRepeatedUpdates(t *testing.T) {
	r := udpRule(t, udpBackend(t, ""))
	r.MaxConnections = 32
	r.IdleTimeoutSec = 10
	m := NewManager()
	defer m.Close()
	if err := m.Apply([]Rule{r}, nil); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := net.Dial(r.Network, r.address())
			if err != nil {
				t.Error(err)
				return
			}
			defer c.Close()
			for n := 0; n < 20; n++ {
				data := []byte(fmt.Sprintf("client=%d packet=%d", i, n))
				c.SetDeadline(time.Now().Add(3 * time.Second))
				if _, err := c.Write(data); err != nil {
					t.Error(err)
					return
				}
				buf := make([]byte, 128)
				count, err := c.Read(buf)
				if err != nil || !bytes.Equal(buf[:count], data) {
					t.Errorf("mixed or missing reply: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < 20; i++ {
		r.IdleTimeoutSec = 11 + i
		if err := m.Apply([]Rule{r}, nil); err != nil {
			t.Fatal(err)
		}
		if got := m.Status()[0].Connections; got != 0 {
			t.Fatal("old UDP mappings retained", got)
		}
		c := udpClient(t, r)
		udpExchange(t, c, []byte("after-update"), []byte("after-update"))
		c.Close()
	}
	m.Close()
	socket, err := net.ListenPacket(r.Network, r.address())
	if err != nil {
		t.Fatal("shutdown retained listener", err)
	}
	socket.Close()
}
