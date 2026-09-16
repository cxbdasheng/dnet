package certificates

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestZeroSSLCredentials(t *testing.T) {
	c := Certificate{Source: "acme", CA: ZeroSSLCA, Email: "user@example.com", Domains: []string{"example.com"}, Challenge: "http", Listen: ":18081", RenewDays: 30, AcceptTerms: true, EABKeyID: "kid"}
	if ValidateACME(c) == nil {
		t.Fatal("missing EAB secret accepted")
	}
	c.EABHMACKey = "invalid+base64"
	if ValidateACME(c) == nil {
		t.Fatal("invalid EAB encoding accepted")
	}
	c.EABHMACKey = "c2VjcmV0"
	if err := ValidateACME(c); err != nil {
		t.Fatal(err)
	}
	summary := Describe(c, time.Now())
	if summary.EABHMACKey != "" {
		t.Fatal("secret retained in summary")
	}
	data, _ := json.Marshal(c)
	if strings.Contains(string(data), c.EABHMACKey) {
		t.Fatal("secret exposed in JSON")
	}
}
