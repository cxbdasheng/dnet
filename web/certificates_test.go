package web

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/cxbdasheng/dnet/certificates"
	"github.com/cxbdasheng/dnet/webservice"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCertificateAPIProtection(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	raw, _ := x509.MarshalPKCS8PrivateKey(key)
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}))
	repo := &stubRepository{}
	s := NewServer(repo, nil)
	post := func(v any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(v)
		r := httptest.NewRequest("POST", "http://localhost/api/certificates", strings.NewReader(string(b)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.CertificatesAPI(w, r)
		return w
	}
	w := post(map[string]any{"action": "save", "certificate": map[string]any{"name": "test", "source": "upload"}, "cert_pem": certPEM, "key_pem": keyPEM})
	if !decodeResult(t, w).Status {
		t.Fatal(w.Body.String())
	}
	if len(repo.conf.Certificates) != 1 {
		t.Fatal("not saved")
	}
	c := repo.conf.Certificates[0]
	w = httptest.NewRecorder()
	s.CertificatesAPI(w, httptest.NewRequest("GET", "/api/certificates", nil))
	if strings.Contains(w.Body.String(), "PRIVATE KEY") || strings.Contains(w.Body.String(), "BEGIN CERTIFICATE") {
		t.Fatal("secret content exposed")
	}
	c.Name = "renamed"
	w = post(map[string]any{"action": "save", "certificate": c})
	if !decodeResult(t, w).Status || repo.conf.Certificates[0].KeyPEM != keyPEM {
		t.Fatal("blank key did not preserve secret")
	}
	repo.conf.WebServiceRules = []webservice.Rule{{Name: "uses-cert", CertificateID: c.ID}}
	w = post(map[string]any{"action": "delete", "certificate": map[string]any{"id": c.ID}})
	if decodeResult(t, w).Status {
		t.Fatal("deleted referenced certificate")
	}
	repo.conf.WebServiceRules = nil
	w = post(map[string]any{"action": "delete", "certificate": map[string]any{"id": c.ID}})
	if !decodeResult(t, w).Status || len(repo.conf.Certificates) != 0 {
		t.Fatal("delete failed")
	}
	for _, tc := range []struct {
		content, origin string
		code            int
	}{{"text/plain", "", 415}, {"application/json", "http://other.example", 403}} {
		r := httptest.NewRequest("POST", "http://localhost/api/certificates", strings.NewReader("{}"))
		r.Header.Set("Content-Type", tc.content)
		r.Header.Set("Origin", tc.origin)
		w = httptest.NewRecorder()
		s.CertificatesAPI(w, r)
		if w.Code != tc.code {
			t.Fatal(w.Code)
		}
	}
	c = certificates.Certificate{ID: "a", Name: "ACME", Source: "acme", CA: certificates.StagingCA, Domains: []string{"example.com"}, Email: "user@example.com", Challenge: "cloudflare", ZoneID: "abc", APIToken: "token-secret", AccountKey: "account-secret", AcceptTerms: true, RenewDays: 30}
	repo.conf.Certificates = []certificates.Certificate{c}
	w = httptest.NewRecorder()
	s.CertificatesAPI(w, httptest.NewRequest("GET", "/api/certificates", nil))
	if strings.Contains(w.Body.String(), "token-secret") || strings.Contains(w.Body.String(), "account-secret") || !strings.Contains(w.Body.String(), "example.com") {
		t.Fatal("ACME secret or domain serialization failure")
	}
}

func TestCertificateJobFailurePreservesCertificate(t *testing.T) {
	repo := &stubRepository{}
	repo.conf.Certificates = []certificates.Certificate{{ID: "job", Name: "job", Source: "acme", CA: certificates.StagingCA, Domains: []string{"example.com"}, Email: "user@example.com", Challenge: "cloudflare", ZoneID: "abc", APIToken: "private-token", AcceptTerms: true, RenewDays: 30, CertPEM: "old-cert", KeyPEM: "old-key"}}
	s := NewServer(repo, nil)
	release := make(chan struct{})
	s.certificateIssuer = func(ctx context.Context, c certificates.Certificate) (certificates.Certificate, error) {
		<-release
		return c, fmt.Errorf("failed private-token")
	}
	if err := s.startCertificateJob("job"); err != nil {
		t.Fatal(err)
	}
	if err := s.startCertificateJob("job"); err == nil {
		t.Fatal("concurrent job accepted")
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.webServiceMu.Lock()
		running := s.certificateJobs["job"]
		c := repo.conf.Certificates[0]
		s.webServiceMu.Unlock()
		if !running {
			if c.CertPEM != "old-cert" || c.KeyPEM != "old-key" || strings.Contains(c.LastError, "private-token") || c.LastError == "" || c.AccountKey == "" || c.LastAttempt.IsZero() {
				t.Fatal("failed issuance lost state or leaked token")
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("job did not finish")
}
