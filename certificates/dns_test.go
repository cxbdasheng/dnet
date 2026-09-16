package certificates

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type dnsTransport func(*http.Request) (*http.Response, error)

func (f dnsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestCloudDNSCreateAndCleanup(t *testing.T) {
	for _, kind := range []string{"aliyun", "tencent"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: dnsTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				result := `{"RecordId":"123"}`
				if kind == "aliyun" {
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if r.Form.Get("Signature") == "" {
						t.Fatal("unsigned request")
					}
					if calls == 1 {
						if r.Form.Get("Action") != "AddDomainRecord" || r.Form.Get("RR") != "_acme-challenge.sub" || r.Form.Get("Value") != "proof" {
							t.Fatal("wrong create")
						}
					} else if r.Form.Get("Action") != "DeleteDomainRecord" || r.Form.Get("RecordId") != "123" {
						t.Fatal("wrong cleanup")
					}
				} else {
					if r.Header.Get("Authorization") == "" || r.Header.Get("X-TC-Version") != "2021-03-23" {
						t.Fatal("missing auth/version")
					}
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if calls == 1 {
						if body["SubDomain"] != "_acme-challenge.sub" || body["Value"] != "proof" || r.Header.Get("X-TC-Action") != "CreateRecord" {
							t.Fatal("wrong create")
						}
					} else if body["RecordId"] != float64(123) || r.Header.Get("X-TC-Action") != "DeleteRecord" {
						t.Fatal("wrong cleanup")
					}
					result = `{"Response":{"RecordId":123}}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(result)), Header: make(http.Header)}, nil
			})}
			c := Certificate{Challenge: kind, ZoneID: "example.com", AccessKey: "id", APIToken: "secret"}
			cleanup, err := presentCloudDNS(context.Background(), c, "_acme-challenge.sub.example.com", "proof", client)
			if err != nil {
				t.Fatal(err)
			}
			cleanup()
			if calls != 2 {
				t.Fatal(calls)
			}
			if _, err = presentCloudDNS(context.Background(), c, "_acme-challenge.notexample.com", "proof", client); err == nil {
				t.Fatal("foreign domain accepted")
			}
		})
	}
}
func TestDNSProviderValidation(t *testing.T) {
	for _, kind := range []string{"aliyun", "tencent"} {
		c := Certificate{Challenge: kind, ZoneID: "example.com", AccessKey: "id", APIToken: "secret", CA: StagingCA, Email: "user@example.com", AcceptTerms: true, RenewDays: 30, Domains: []string{"*.example.com"}}
		if err := ValidateACME(c); err != nil {
			t.Fatal(err)
		}
		c.Domains = []string{"evil-example.com"}
		if ValidateACME(c) == nil {
			t.Fatal("foreign domain accepted")
		}
		c.Domains = []string{"example.com"}
		c.APIToken = ""
		if ValidateACME(c) == nil {
			t.Fatal("missing secret accepted")
		}
	}
}

func TestDNSAutomaticZone(t *testing.T) {
	for _, domain := range []string{"example.com", "example.com.cn"} {
		c := Certificate{Challenge: "aliyun", AccessKey: "id", APIToken: "secret", CA: StagingCA, Email: "user@example.com", AcceptTerms: true, RenewDays: 30, Domains: []string{"*." + domain}}
		if err := ValidateACME(c); err != nil {
			t.Fatal(err)
		}
		client := &http.Client{Transport: dnsTransport(func(r *http.Request) (*http.Response, error) {
			r.ParseForm()
			if r.Form.Get("DomainName") != domain || r.Form.Get("RR") != "_acme-challenge.sub" {
				t.Fatalf("wrong zone/record: %s / %s", r.Form.Get("DomainName"), r.Form.Get("RR"))
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"RecordId":"123"}`)), Header: make(http.Header)}, nil
		})}
		if _, err := presentCloudDNS(context.Background(), c, "_acme-challenge.sub."+domain, "proof", client); err != nil {
			t.Fatal(err)
		}
	}
}
