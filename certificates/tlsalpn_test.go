package certificates

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"golang.org/x/crypto/acme"
	"net"
	"testing"
	"time"
)

func TestTLSALPNChallenge(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	client := &acme.Client{Key: key}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	closeChallenge, err := presentTLSALPN(client, addr, "example.com", "test-token")
	if err != nil {
		t.Fatal(err)
	}
	defer closeChallenge()
	for _, tc := range []struct {
		host, proto string
		valid       bool
	}{{"example.com", acme.ALPNProto, true}, {"wrong.example.com", acme.ALPNProto, false}, {"example.com", "h2", false}} {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true, ServerName: tc.host, NextProtos: []string{tc.proto}}) // Test-only self-signed challenge certificate.
		if (err == nil) != tc.valid {
			t.Fatalf("host=%s proto=%s: %v", tc.host, tc.proto, err)
		}
		if conn != nil {
			state := conn.ConnectionState()
			conn.Close()
			if state.NegotiatedProtocol != acme.ALPNProto {
				t.Fatal("wrong ALPN")
			}
			cert := state.PeerCertificates[0]
			if len(cert.DNSNames) != 1 || cert.DNSNames[0] != "example.com" {
				t.Fatal("wrong SAN")
			}
			found := false
			for _, ext := range cert.Extensions {
				if ext.Id.String() == "1.3.6.1.5.5.7.1.31" && ext.Critical {
					found = true
				}
			}
			if !found {
				t.Fatal("missing critical ACME validation extension")
			}
		}
	}
	closeChallenge()
	l, err = net.Listen("tcp", addr)
	if err != nil {
		t.Fatal("listener not released", err)
	}
	l.Close()
	c := Certificate{Source: "acme", CA: StagingCA, Email: "user@example.com", Domains: []string{"example.com"}, Challenge: "tls-alpn", Listen: addr, AcceptTerms: true, RenewDays: 30}
	if err = ValidateACME(c); err != nil {
		t.Fatal(err)
	}
	c.Domains = []string{"*.example.com"}
	if ValidateACME(c) == nil {
		t.Fatal("wildcard accepted")
	}
}
