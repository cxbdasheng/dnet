package webservice

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"github.com/cxbdasheng/dnet/certificates"
	"math/big"
	"testing"
	"time"
)

func managedFixture(t *testing.T, serial int64, domains ...string) certificates.Certificate {
	t.Helper()
	if len(domains) == 0 {
		domains = []string{"one.example"}
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(serial), DNSNames: domains, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := x509.MarshalPKCS8PrivateKey(key)
	return certificates.Certificate{ID: "shared", Name: "shared", Source: "upload", CertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), KeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}))}
}
func TestManagedCertificateReload(t *testing.T) {
	c := managedFixture(t, 1)
	r := testRule(t, "http://localhost:3000")
	r.TLS = true
	r.CertificateID = c.ID
	m := NewManager()
	defer m.Close()
	if err := m.ApplyCertificates([]Rule{r}, []certificates.Certificate{c}, nil); err != nil {
		t.Fatal(err)
	}
	serial := func() int64 {
		conn, err := tls.Dial("tcp", r.address(), &tls.Config{InsecureSkipVerify: true})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		return conn.ConnectionState().PeerCertificates[0].SerialNumber.Int64()
	}
	if serial() != 1 {
		t.Fatal("initial certificate")
	}
	c = managedFixture(t, 2)
	if err := m.ApplyCertificates([]Rule{r}, []certificates.Certificate{c}, nil); err != nil {
		t.Fatal(err)
	}
	if serial() != 2 {
		t.Fatal("renewed certificate not loaded")
	}
	c.KeyPEM = "bad"
	if err := m.ApplyCertificates([]Rule{r}, []certificates.Certificate{c}, nil); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	if serial() != 2 {
		t.Fatal("invalid replacement broke live cert")
	}
	r.Domain = "other.example"
	if _, err := ResolveCertificates([]Rule{r}, []certificates.Certificate{managedFixture(t, 3)}); err == nil {
		t.Fatal("hostname mismatch accepted")
	}
}
