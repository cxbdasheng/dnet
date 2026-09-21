package ddns

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

func newAddressProvider(t *testing.T, service, endpoint string, group *config.DNSGroup, caches []*Cache) DNS {
	t.Helper()
	var provider DNS
	if service == ProviderDNSLA {
		old := dnslaAPIEndpoint
		dnslaAPIEndpoint = endpoint
		t.Cleanup(func() { dnslaAPIEndpoint = old })
		provider = &DNSLA{}
	} else {
		old := porkbunAPIEndpoint
		porkbunAPIEndpoint = endpoint
		t.Cleanup(func() { porkbunAPIEndpoint = old })
		provider = &Porkbun{}
	}
	provider.Init(group, caches)
	return provider
}

func serveDNSLADomain(t *testing.T, w http.ResponseWriter, r *http.Request, zone string) bool {
	t.Helper()
	if r.URL.Path != "/domain" {
		return false
	}
	key, secret, ok := r.BasicAuth()
	if r.Method != http.MethodGet || r.URL.Query().Get("domain") != zone || !ok || key != "key" || secret != "secret" {
		t.Errorf("invalid DNSLA domain lookup: %s", r.URL)
	}
	json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]string{"id": "zone-id", "domain": zone + "."}})
	return true
}

func TestAddressProvidersCreateUpdateAndUnchanged(t *testing.T) {
	for _, service := range []string{ProviderDNSLA, ProviderPorkbun} {
		for _, recordType := range []string{RecordTypeA, RecordTypeAAAA, RecordTypeCNAME, RecordTypeTXT} {
			for _, domain := range []string{"example.com", "www.sub.example.co.uk", "*.example.com", "_acme-challenge.example.com"} {
				for _, mode := range []string{"create", "update", "unchanged"} {
					t.Run(service+"/"+recordType+"/"+domain+"/"+mode, func(t *testing.T) {
						value, oldValue := "192.0.2.1", "192.0.2.2"
						if recordType == RecordTypeAAAA {
							value, oldValue = "2001:db8::1", "2001:db8::2"
						}
						if recordType == RecordTypeCNAME {
							value, oldValue = "target.example.net", "old.example.net"
						}
						if recordType == RecordTypeTXT {
							value, oldValue = "token=AbC \"quoted\" \\ 保留空格 ", "token=abc"
						}
						if mode == "unchanged" {
							oldValue = value
							if recordType == RecordTypeCNAME {
								oldValue = strings.ToUpper(value) + "."
							}
						}
						zone, host := "example.com", ""
						if domain == "www.sub.example.co.uk" {
							zone, host = "example.co.uk", "www.sub"
						}
						if domain == "*.example.com" {
							host = "*"
						}
						if domain == "_acme-challenge.example.com" {
							host = "_acme-challenge"
						}
						writes, reads := 0, 0
						srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							if service == ProviderDNSLA && serveDNSLADomain(t, w, r, zone) {
								return
							}
							var body map[string]any
							if r.Method != http.MethodGet {
								if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
									t.Error(err)
								}
							}
							if service == ProviderDNSLA {
								key, secret, ok := r.BasicAuth()
								if !ok || key != "key" || secret != "secret" {
									t.Error("missing Basic authentication")
								}
								dnsHost, typeID := host, 1
								if dnsHost == "" {
									dnsHost = "@"
								}
								if recordType == RecordTypeAAAA {
									typeID = 28
								}
								if recordType == RecordTypeCNAME {
									typeID = 5
								}
								if recordType == RecordTypeTXT {
									typeID = 16
								}
								if r.Method == http.MethodGet {
									reads++
									q := r.URL.Query()
									if r.URL.Path != "/recordList" || q.Get("domainId") != "zone-id" || q.Get("host") != dnsHost || q.Get("type") != fmt.Sprint(typeID) {
										t.Errorf("incorrect query: %s", r.URL)
									}
									if mode == "create" {
										io.WriteString(w, `{"code":200,"data":{"total":0,"results":[]}}`)
									} else {
										json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"total": 1, "results": []dnslaRecord{{ID: "123", Host: dnsHost, Type: typeID, Data: oldValue}}}})
									}
									return
								}
								if r.URL.Path != "/record" || body["host"] != dnsHost || body["type"] != float64(typeID) || body["data"] != value || body["ttl"] != float64(600) {
									t.Errorf("incorrect write: %s %+v", r.URL, body)
								}
								if mode == "create" && (r.Method != http.MethodPost || body["domainId"] != "zone-id") {
									t.Errorf("incorrect create: %+v", body)
								}
								if mode == "update" && (r.Method != http.MethodPut || body["id"] != "123") {
									t.Errorf("incorrect update: %+v", body)
								}
								io.WriteString(w, `{"code":200,"data":{"id":"123"}}`)
							} else {
								if r.Method != http.MethodPost || body["apikey"] != "key" || body["secretapikey"] != "secret" {
									t.Error("missing JSON authentication")
								}
								if strings.HasPrefix(r.URL.Path, "/retrieveByNameType/") {
									reads++
									want := "/retrieveByNameType/" + zone + "/" + recordType
									if host != "" {
										want += "/" + host
									}
									if r.URL.Path != want {
										t.Errorf("query path = %s, want %s", r.URL.Path, want)
									}
									if mode == "create" {
										io.WriteString(w, `{"status":"SUCCESS","records":[]}`)
									} else {
										json.NewEncoder(w).Encode(map[string]any{"status": "SUCCESS", "records": []map[string]string{{"id": "123", "name": domain, "type": recordType, "content": oldValue}}})
									}
									return
								}
								want := "/create/" + zone
								if mode == "update" {
									want = "/edit/" + zone + "/123"
								}
								if r.URL.Path != want || body["name"] != host || body["type"] != recordType || body["content"] != value || body["ttl"] != "600" {
									t.Errorf("incorrect write: %s %+v", r.URL, body)
								}
								io.WriteString(w, `{"status":"SUCCESS"}`)
							}
							writes++
						}))
						defer srv.Close()
						group := &config.DNSGroup{Domain: domain, AccessKey: "key", AccessSecret: "secret", TTL: "10m", Records: []config.DNSRecord{{Type: recordType, Value: value}}}
						cache := NewCache()
						cache.TimesFailed = 2
						provider := newAddressProvider(t, service, srv.URL, group, []*Cache{&cache})
						results := provider.UpdateOrCreateRecords()
						wantStatus, wantWrites, wantWebhook := statusType(UpdatedSuccess), 1, true
						if mode == "unchanged" {
							wantStatus, wantWrites, wantWebhook = UpdatedNothing, 0, false
						}
						if len(results) != 1 || results[0].Status != wantStatus || results[0].ShouldWebhook != wantWebhook {
							t.Fatalf("results = %+v", results)
						}
						if reads != 1 || writes != wantWrites || !cache.HasRun || cache.TimesFailed != 0 {
							t.Errorf("reads=%d writes=%d cache=%+v", reads, writes, &cache)
						}
					})
				}
			}
		}
	}
}

func TestAddressProvidersRejectFailures(t *testing.T) {
	for _, service := range []string{ProviderDNSLA, ProviderPorkbun} {
		for _, mode := range []string{"http", "malformed", "empty", "business", "write_business", "multiple", "mismatch", "unsupported", "credentials"} {
			t.Run(service+"/"+mode, func(t *testing.T) {
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if service == ProviderDNSLA && serveDNSLADomain(t, w, r, "example.com") {
						return
					}
					calls++
					if mode == "http" {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}
					if mode == "malformed" {
						io.WriteString(w, "not-json")
						return
					}
					if mode == "empty" {
						io.WriteString(w, "{}")
						return
					}
					if mode == "write_business" && calls == 1 {
						if service == ProviderDNSLA {
							io.WriteString(w, `{"code":200,"data":{"total":0,"results":[]}}`)
						} else {
							io.WriteString(w, `{"status":"SUCCESS","records":[]}`)
						}
						return
					}
					if service == ProviderDNSLA {
						switch mode {
						case "multiple":
							io.WriteString(w, `{"code":200,"data":{"total":2,"results":[{"id":"1","host":"www","type":1,"data":"192.0.2.2"}]}}`)
						case "mismatch":
							io.WriteString(w, `{"code":200,"data":{"total":1,"results":[{"id":"1","host":"other","type":1,"data":"192.0.2.2"}]}}`)
						default:
							io.WriteString(w, `{"code":403,"msg":"denied"}`)
						}
					} else {
						switch mode {
						case "multiple":
							io.WriteString(w, `{"status":"SUCCESS","records":[{},{}]}`)
						case "mismatch":
							io.WriteString(w, `{"status":"SUCCESS","records":[{"id":"1","name":"other.example.com","type":"A","content":"192.0.2.2"}]}`)
						default:
							io.WriteString(w, `{"status":"ERROR","message":"denied"}`)
						}
					}
				}))
				defer srv.Close()
				group := &config.DNSGroup{Domain: "www.example.com", AccessKey: "key", AccessSecret: "secret", Records: []config.DNSRecord{{Type: RecordTypeA, Value: "192.0.2.1"}}}
				if mode == "unsupported" {
					group.Records[0].Type = "MX"
				}
				if mode == "credentials" {
					group.AccessSecret = ""
				}
				cache := NewCache()
				cache.TimesFailed = 2
				provider := newAddressProvider(t, service, srv.URL, group, []*Cache{&cache})
				results := provider.UpdateOrCreateRecords()
				wantStatus := statusType(UpdatedFailed)
				if mode == "credentials" {
					wantStatus = InitFailed
				}
				if len(results) != 1 || results[0].Status != wantStatus || results[0].ErrorMessage == "" || cache.HasRun {
					t.Fatalf("results=%+v cache=%+v", results, &cache)
				}
				if mode != "credentials" && !results[0].ShouldWebhook {
					t.Error("third failure must trigger webhook")
				}
				wantCalls := 1
				if mode == "write_business" {
					wantCalls = 2
				}
				if mode == "unsupported" || mode == "credentials" {
					wantCalls = 0
				}
				if calls != wantCalls {
					t.Errorf("calls=%d, want %d", calls, wantCalls)
				}
			})
		}
	}
}

func TestAddressProviderDynamicCache(t *testing.T) {
	oldForce := ForceCompareGlobal
	ForceCompareGlobal = false
	t.Cleanup(func() { ForceCompareGlobal = oldForce })
	for _, service := range []string{ProviderDNSLA, ProviderPorkbun} {
		t.Run(service, func(t *testing.T) {
			ipServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "192.0.2.1") }))
			defer ipServer.Close()
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if service == ProviderDNSLA && serveDNSLADomain(t, w, r, "example.com") {
					return
				}
				calls++
				if service == ProviderDNSLA {
					io.WriteString(w, `{"code":200,"data":{"total":1,"results":[{"id":"1","host":"www","type":1,"data":"192.0.2.1"}]}}`)
				} else {
					io.WriteString(w, `{"status":"SUCCESS","records":[{"id":"1","name":"www.example.com","type":"A","content":"192.0.2.1"}]}`)
				}
			}))
			defer srv.Close()
			group := &config.DNSGroup{Domain: "www.example.com", AccessKey: "key", AccessSecret: "secret", Records: []config.DNSRecord{{Type: RecordTypeA, IPType: helper.DynamicIPv4URL, Value: ipServer.URL}}}
			cache := NewCache()
			provider := newAddressProvider(t, service, srv.URL, group, []*Cache{&cache})
			for i := 0; i < 2; i++ {
				result := provider.UpdateOrCreateRecords()[0]
				if result.Status != UpdatedNothing || result.ShouldWebhook {
					t.Fatalf("unexpected result: %+v", result)
				}
			}
			if calls != 1 {
				t.Errorf("expected second run to skip DNS API, calls=%d", calls)
			}
			if value, ok := cache.GetDynamicIP(getCacheKey(helper.DynamicIPv4URL, ipServer.URL, "")); !ok || value != "192.0.2.1" {
				t.Error("dynamic cache not refreshed")
			}
			cache.Times = 0
			provider.UpdateOrCreateRecords()
			if calls != 2 {
				t.Error("cache expiry must compare with provider")
			}
		})
	}
}

func TestAddressDomainPartsAndTTL(t *testing.T) {
	for _, tc := range []struct{ domain, zone, host string }{
		{"EXAMPLE.COM.", "example.com", ""},
		{"www.example.co.uk", "example.co.uk", "www"},
		{"*.sub.example.com", "example.com", "*.sub"},
		{"例子.com", "xn--fsqu00a.com", ""},
		{"_acme-challenge.example.com", "example.com", "_acme-challenge"},
		{"selector._domainkey.example.com", "example.com", "selector._domainkey"},
	} {
		zone, host, err := addressDomainParts(tc.domain)
		if err != nil || zone != tc.zone || host != tc.host {
			t.Errorf("%s: %s %s %v", tc.domain, zone, host, err)
		}
	}
	if _, _, err := addressDomainParts("localhost"); err == nil {
		t.Error("expected invalid domain error")
	}
	for _, tc := range []struct {
		raw       string
		min, want int
	}{
		{"AUTO", 600, 600}, {"", 1, 600}, {"10m", 600, 600}, {"1h", 600, 3600}, {"60", 600, 600}, {"60s", 1, 60},
	} {
		if got := addressTTL(tc.raw, tc.min); got != tc.want {
			t.Errorf("TTL %s=%d want %d", tc.raw, got, tc.want)
		}
	}
}

func TestProviderCNAMEConflictingConfiguration(t *testing.T) {
	for _, other := range []string{RecordTypeA, RecordTypeAAAA, RecordTypeTXT, RecordTypeCNAME} {
		for _, cnameFirst := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/first=%v", other, cnameFirst), func(t *testing.T) {
				records := []config.DNSRecord{{Type: RecordTypeCNAME, Value: "target.example.net"}, {Type: other, Value: "value"}}
				if !cnameFirst {
					records[0], records[1] = records[1], records[0]
				}
				first, second := NewCache(), NewCache()
				b := BaseDNSProvider{Group: &config.DNSGroup{Domain: "www.example.com", AccessKey: "key", AccessSecret: "secret", Records: records}, Caches: []*Cache{&first, &second}}
				results := b.updateProviderRecords(func(string, string) (bool, error) {
					t.Fatal("conflicting configuration must not send any API requests")
					return false, nil
				})
				if len(results) != 2 {
					t.Fatalf("results=%+v", results)
				}
				for _, result := range results {
					if result.Status != UpdatedFailed || !strings.Contains(result.ErrorMessage, "CNAME") {
						t.Errorf("result=%+v", result)
					}
				}
			})
		}
	}
}

func TestProviderStaticRecordsIgnoreStaleIPType(t *testing.T) {
	oldForce := ForceCompareGlobal
	ForceCompareGlobal = false
	t.Cleanup(func() { ForceCompareGlobal = oldForce })
	for _, recordType := range []string{RecordTypeCNAME, RecordTypeTXT} {
		t.Run(recordType, func(t *testing.T) {
			record := config.DNSRecord{Type: recordType, IPType: helper.DynamicIPv4URL, Value: "example.net"}
			cache := NewCache()
			cache.HasRun = true
			cache.UpdateDynamicIP(getCacheKey(record.IPType, record.Value, ""), record.Value)
			b := BaseDNSProvider{Group: &config.DNSGroup{Domain: "www.example.com", AccessKey: "key", AccessSecret: "secret", Records: []config.DNSRecord{record}}, Caches: []*Cache{&cache}}
			calls := 0
			result := b.updateProviderRecords(func(typ, value string) (bool, error) {
				calls++
				if typ != recordType || value != record.Value {
					t.Errorf("type=%s value=%s", typ, value)
				}
				return true, nil
			})[0]
			if calls != 1 || result.Status != UpdatedSuccess || !result.ShouldWebhook || result.NewValue != "" {
				t.Errorf("calls=%d result=%+v", calls, result)
			}
			if b.Group.Records[0].IPType != record.IPType {
				t.Error("configuration was mutated")
			}
		})
	}
}

func TestProviderRecordValueComparison(t *testing.T) {
	for _, tc := range []struct {
		typ, existing, desired string
		equal                  bool
	}{
		{RecordTypeCNAME, "TARGET.Example.net.", "target.example.net", true},
		{RecordTypeCNAME, "old.example.net", "target.example.net", false},
		{RecordTypeTXT, "Token=AbC", "token=abc", false},
		{RecordTypeTXT, "value.", "value", false},
		{RecordTypeTXT, " value ", "value", false},
		{RecordTypeTXT, `"value"`, "value", false},
		{RecordTypeTXT, " value ", " value ", true},
	} {
		if got := providerRecordValueEqual(tc.typ, tc.existing, tc.desired); got != tc.equal {
			t.Errorf("%s %q vs %q = %v", tc.typ, tc.existing, tc.desired, got)
		}
	}
}

func TestDNSLADomainLookupFailure(t *testing.T) {
	for _, body := range []string{`{"code":403}`, `{"code":200,"data":null}`, `{"code":200,"data":{"domain":"example.com."}}`, `{"code":200,"data":{"id":"zone-id","domain":"other.com."}}`} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/domain" {
					t.Errorf("unexpected write or record lookup: %s", r.URL)
				}
				io.WriteString(w, body)
			}))
			defer srv.Close()
			group := &config.DNSGroup{Domain: "www.example.com", AccessKey: "key", AccessSecret: "secret", Records: []config.DNSRecord{{Type: RecordTypeTXT, Value: "token"}}}
			cache := NewCache()
			result := newAddressProvider(t, ProviderDNSLA, srv.URL, group, []*Cache{&cache}).UpdateOrCreateRecords()[0]
			if calls != 1 || result.Status != UpdatedFailed || cache.HasRun {
				t.Fatalf("calls=%d result=%+v", calls, result)
			}
		})
	}
}

func TestProvidersPreserveRecordsOnTXTMultiplicityOrRemoteConflict(t *testing.T) {
	for _, service := range []string{ProviderDNSLA, ProviderPorkbun} {
		for _, mode := range []string{"multiple_txt", "cname_conflict", "txt_conflict"} {
			t.Run(service+"/"+mode, func(t *testing.T) {
				writes := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if service == ProviderDNSLA && serveDNSLADomain(t, w, r, "example.com") {
						return
					}
					isRead := r.URL.Path == "/recordList" || strings.HasPrefix(r.URL.Path, "/retrieveByNameType/")
					if isRead {
						if service == ProviderDNSLA {
							if mode == "multiple_txt" {
								io.WriteString(w, `{"code":200,"data":{"total":2,"results":[{"id":"1","host":"www","type":16,"data":"verification=abc"},{"id":"2","host":"www","type":16,"data":"v=spf1 -all"}]}}`)
							} else {
								io.WriteString(w, `{"code":200,"data":{"total":0,"results":[]}}`)
							}
						} else {
							if mode == "multiple_txt" {
								io.WriteString(w, `{"status":"SUCCESS","records":[{"id":"1","name":"www.example.com","type":"TXT","content":"verification=abc"},{"id":"2","name":"www.example.com","type":"TXT","content":"v=spf1 -all"}]}`)
							} else {
								io.WriteString(w, `{"status":"SUCCESS","records":[]}`)
							}
						}
						return
					}
					writes++
					if r.Method != http.MethodPost || (r.URL.Path != "/record" && r.URL.Path != "/create/example.com") {
						t.Errorf("unexpected deletion/update: %s %s", r.Method, r.URL.Path)
					}
					if service == ProviderDNSLA {
						io.WriteString(w, `{"code":409,"msg":"CNAME conflict"}`)
					} else {
						io.WriteString(w, `{"status":"ERROR","message":"CNAME conflict"}`)
					}
				}))
				defer srv.Close()
				typ := RecordTypeTXT
				if mode == "cname_conflict" {
					typ = RecordTypeCNAME
				}
				group := &config.DNSGroup{Domain: "www.example.com", AccessKey: "key", AccessSecret: "secret", Records: []config.DNSRecord{{Type: typ, Value: "target.example.net"}}}
				cache := NewCache()
				result := newAddressProvider(t, service, srv.URL, group, []*Cache{&cache}).UpdateOrCreateRecords()[0]
				wantWrites := 1
				if mode == "multiple_txt" {
					wantWrites = 0
				}
				if result.Status != UpdatedFailed || cache.HasRun || writes != wantWrites {
					t.Fatalf("writes=%d result=%+v", writes, result)
				}
			})
		}
	}
}
