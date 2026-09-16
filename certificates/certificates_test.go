package certificates

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sample(t *testing.T) Certificate {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	now := time.Now()
	leaf := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"example.com"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(90 * 24 * time.Hour)}
	der, e := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	priv, _ := x509.MarshalPKCS8PrivateKey(key)
	return Certificate{ID: "one", Name: "example", Source: "upload", CertPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), KeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv}))}
}
func TestCertificateValidationAndSecrets(t *testing.T) {
	c := sample(t)
	if e := Validate(c); e != nil {
		t.Fatal(e)
	}
	summary := Describe(c, time.Now())
	if summary.State != "valid" || summary.Domains[0] != "example.com" {
		t.Fatal(summary)
	}
	c.APIToken = "token-secret"
	c.AccountKey = "account-secret"
	b, _ := json.Marshal(Describe(c, time.Now()))
	for _, secret := range []string{"PRIVATE KEY", "token-secret", "account-secret"} {
		if strings.Contains(string(b), secret) {
			t.Fatal("secret exposed")
		}
	}
	c.KeyPEM = sample(t).KeyPEM
	if Validate(c) == nil {
		t.Fatal("mismatched private key accepted")
	}
	c = sample(t)
	dir := t.TempDir()
	c.CertPath = filepath.Join(dir, "cert")
	c.KeyPath = filepath.Join(dir, "key")
	os.WriteFile(c.CertPath, []byte(c.CertPEM), 0600)
	os.WriteFile(c.KeyPath, []byte(c.KeyPEM), 0600)
	c.Source = "path"
	if e := Validate(c); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(c.KeyPath, []byte("invalid"), 0600)
	if Describe(c, time.Now()).State != "invalid" {
		t.Fatal("invalid file hidden")
	}
}
func TestACMEDueAndSettings(t *testing.T) {
	c := sample(t)
	c.Source = "acme"
	c.CA = StagingCA
	c.Email = "user@example.com"
	c.Challenge = "http"
	c.Listen = "127.0.0.1:18081"
	c.Domains = []string{"example.com"}
	c.AcceptTerms = true
	c.RenewDays = 30
	c.AutoRenew = true
	if e := ValidateACME(c); e != nil {
		t.Fatal(e)
	}
	if Due(c, time.Now()) {
		t.Fatal("premature renewal")
	}
	if !Due(c, time.Now().Add(65*24*time.Hour)) {
		t.Fatal("renewal not due")
	}
	c.LastAttempt = time.Now()
	c.CertPEM = ""
	if Due(c, time.Now()) {
		t.Fatal("backoff ignored")
	}
	c.LastAttempt = time.Time{}
	if !Due(c, time.Now()) {
		t.Fatal("first issuance not due")
	}
	c.Domains = []string{"*.example.com"}
	if ValidateACME(c) == nil {
		t.Fatal("HTTP wildcard accepted")
	}
	c.Challenge = "cloudflare"
	c.ZoneID = "123abc"
	c.APIToken = "token"
	if e := ValidateACME(c); e != nil {
		t.Fatal(e)
	}
	c.AcceptTerms = false
	if ValidateACME(c) == nil {
		t.Fatal("terms ignored")
	}
}
