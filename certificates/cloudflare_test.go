package certificates

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCloudflareZoneDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{{"delegated", `{"success":true,"result":[{"id":"sub-zone","name":"dev.example.com"}]}`, "sub-zone", 200}, {"denied", `{"success":false}`, "", 403}, {"missing", `{"success":true,"result":[]}`, "", 200}, {"ambiguous", `{"success":true,"result":[{"id":"a","name":"dev.example.com"},{"id":"b","name":"dev.example.com"}]}`, "", 200}} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: dnsTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Fatal("missing token")
				}
				body := tc.body
				if calls == 1 && tc.status == 200 {
					body = `{"success":true,"result":[]}`
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			got, err := discoverCloudflareZone(context.Background(), "secret", "nas.dev.example.com", client)
			if got != tc.want || (err == nil) != (tc.want != "") {
				t.Fatalf("got %q, %v", got, err)
			}
			if tc.want != "" && calls != 2 {
				t.Fatal("did not select longest matching zone")
			}
		})
	}
}
