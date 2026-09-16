package certificates

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestIssueHTTP01(t *testing.T)                                    { testIssueChallenge(t, "http") }
func TestIssueTLSALPN01(t *testing.T)                                 { testIssueChallenge(t, "tls-alpn") }
func TestIssueZeroSSL(t *testing.T)                                   { testIssueChallenge(t, "zerossl") }
func TestIssueZeroSSLWithoutEAB(t *testing.T)                         { testIssueChallenge(t, "zerossl-reuse") }
func testIssueChallenge(t *testing.T, kind string) {
	challengeType := "http-01"
	if kind == "tls-alpn" {
		challengeType = "tls-alpn-01"
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	c := Certificate{ID: "test", Name: "test", Source: "acme", CA: StagingCA, Email: "user@example.com", Domains: []string{"example.com"}, Challenge: kind, Listen: addr, AcceptTerms: true, RenewDays: 30}
	if strings.HasPrefix(kind, "zerossl") {
		c.CA = ZeroSSLCA
		c.Challenge = "http"
		c.EABKeyID = "test-kid"
		c.EABHMACKey = base64.RawURLEncoding.EncodeToString([]byte("test-eab-secret"))
	}
	if kind == "zerossl-reuse" {
		c.EABDisabled = true
		c.EABKeyID = ""
		c.EABHMACKey = ""
	}
	accepted := false
	var certificate []byte
	const base = "https://acme.invalid"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Replay-Nonce", fmt.Sprint(time.Now().UnixNano()))
		w.Header().Set("Content-Type", "application/json")
		var envelope struct {
			Payload string `json:"payload"`
		}
		json.NewDecoder(r.Body).Decode(&envelope)
		payload, _ := base64.RawURLEncoding.DecodeString(envelope.Payload)
		switch r.URL.Path {
		case "/directory":
			fmt.Fprintf(w, `{"newNonce":%q,"newAccount":%q,"newOrder":%q}`, base+"/nonce", base+"/account", base+"/order")
		case "/nonce":
			w.WriteHeader(200)
		case "/account":
			if kind == "zerossl-reuse" {
				var body map[string]any
				json.Unmarshal(payload, &body)
				if body["onlyReturnExisting"] != true || body["externalAccountBinding"] != nil {
					t.Error("must only retrieve existing account without EAB")
				}
			}

			if kind == "zerossl" {
				var registration struct {
					Binding struct{ Protected, Payload, Signature string } `json:"externalAccountBinding"`
				}
				if err := json.Unmarshal(payload, &registration); err != nil {
					t.Error(err)
				}
				binding := registration.Binding
				mac := hmac.New(sha256.New, []byte("test-eab-secret"))
				mac.Write([]byte(binding.Protected + "." + binding.Payload))
				signature, _ := base64.RawURLEncoding.DecodeString(binding.Signature)
				if !hmac.Equal(signature, mac.Sum(nil)) {
					t.Error("invalid EAB signature")
				}
				protected, _ := base64.RawURLEncoding.DecodeString(binding.Protected)
				var header struct{ Kid, Alg string }
				json.Unmarshal(protected, &header)
				if header.Kid != "test-kid" || header.Alg != "HS256" {
					t.Error("invalid EAB header")
				}
			}

			w.Header().Set("Location", base+"/account/1")
			if kind == "zerossl-reuse" {
				w.WriteHeader(200)
			} else {
				w.WriteHeader(201)
			}
			fmt.Fprint(w, `{"status":"valid"}`)
		case "/order":
			w.Header().Set("Location", base+"/order/1")
			w.WriteHeader(201)
			fmt.Fprintf(w, `{"status":"pending","authorizations":[%q],"finalize":%q}`, base+"/auth", base+"/finalize")
		case "/auth":
			status := "pending"
			if accepted {
				status = "valid"
			}
			fmt.Fprintf(w, `{"status":%q,"identifier":{"type":"dns","value":"example.com"},"challenges":[{"type":%q,"url":%q,"token":"test-token","status":"pending"}]}`, status, challengeType, base+"/challenge")
		case "/challenge":
			if kind == "tls-alpn" {
				conn, e := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", addr, &tls.Config{InsecureSkipVerify: true, ServerName: "example.com", NextProtos: []string{"acme-tls/1"}})
				if e != nil {
					t.Error(e)
					w.WriteHeader(500)
					return
				}
				conn.Close()
				accepted = true
				fmt.Fprint(w, `{"status":"valid","type":"tls-alpn-01","token":"test-token"}`)
				return
			}
			req, _ := http.NewRequest("GET", "http://"+addr+"/.well-known/acme-challenge/test-token", nil)
			req.Host = "example.com"
			res, e := http.DefaultClient.Do(req)
			if e != nil {
				t.Error(e)
				w.WriteHeader(500)
				return
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if res.StatusCode != 200 || !strings.HasPrefix(string(b), "test-token.") {
				t.Error("challenge failed", string(b))
			}
			accepted = true
			fmt.Fprint(w, `{"status":"valid","type":"http-01","token":"test-token"}`)
		case "/order/1":
			status := "ready"
			if certificate != nil {
				status = "valid"
			}
			fmt.Fprintf(w, `{"status":%q,"finalize":%q,"certificate":%q}`, status, base+"/finalize", base+"/cert")
		case "/finalize":
			var body struct {
				CSR string `json:"csr"`
			}
			json.Unmarshal(payload, &body)
			raw, _ := base64.RawURLEncoding.DecodeString(body.CSR)
			csr, e := x509.ParseCertificateRequest(raw)
			if e != nil {
				t.Error(e)
				w.WriteHeader(500)
				return
			}
			if e = csr.CheckSignature(); e != nil {
				t.Error(e)
			}
			caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: csr.DNSNames, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
			certificate, _ = x509.CreateCertificate(rand.Reader, leaf, leaf, csr.PublicKey, caKey)
			fmt.Fprintf(w, `{"status":"valid","certificate":%q}`, base+"/cert")
		case "/cert":
			w.Header().Set("Content-Type", "application/pem-certificate-chain")
			pem.Encode(w, &pem.Block{Type: "CERTIFICATE", Bytes: certificate})
		default:
			t.Error("unexpected path", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		u := *r.URL
		u.Scheme = target.Scheme
		u.Host = target.Host
		copy.URL = &u
		if u.Path == "/v2/DV90" || u.Path == "/directory" || strings.HasSuffix(u.Path, "/directory") {
			copy.URL.Path = "/directory"
		}
		return http.DefaultTransport.RoundTrip(copy)
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := issue(ctx, c, client)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := Load(result)
	if err != nil {
		t.Fatal(err)
	}
	if pair.Leaf.DNSNames[0] != "example.com" || result.AccountKey == "" || !accepted {
		t.Fatal("issuance incomplete")
	}
}
