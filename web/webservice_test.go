package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/webservice"
)

type failingWebRepository struct{ stubRepository }

func (r *failingWebRepository) Save(*config.Config) error { return errors.New("save failed") }

func TestWebServiceAPISecretsAndValidation(t *testing.T) {
	repo := &stubRepository{}
	repo.conf.Username = "unchanged"
	m := webservice.NewManager()
	defer m.Close()
	s := NewServer(repo, nil)
	s.WebServices = m
	rule := webservice.Rule{ID: "one", Name: "1", Domain: "nas.example.com", Network: "tcp4", ListenAddress: "127.0.0.1", ListenPort: 18080, Target: "http://127.0.0.1:3000", TimeoutSec: 30, AuthEnabled: true, Username: "visitor", Password: "secret-password"}
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/webservice", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.WebServiceAPI(w, r)
		return w
	}
	body, _ := json.Marshal(map[string]any{"enabled": false, "rules": []webservice.Rule{rule}})
	w := post(string(body))
	if !decodeResult(t, w).Status || repo.conf.Username != "unchanged" || len(repo.conf.WebServiceRules) != 1 {
		t.Fatal(w.Body.String())
	}
	hash := repo.conf.WebServiceRules[0].PasswordHash
	if hash == "" || repo.conf.WebServiceRules[0].Password != "" || strings.Contains(w.Body.String(), hash) || strings.Contains(w.Body.String(), "secret-password") {
		t.Fatal("password exposed or not hashed")
	}
	rule.Password = ""
	body, _ = json.Marshal(map[string]any{"enabled": false, "rules": []webservice.Rule{rule}})
	if !decodeResult(t, post(string(body))).Status || repo.conf.WebServiceRules[0].PasswordHash != hash {
		t.Fatal("password not preserved")
	}
	for _, body := range []string{`{"unexpected":true}`, `{} {}`, `{"rules":[{"id":"invalid"}]}`} {
		if decodeResult(t, post(body)).Status || repo.conf.WebServiceRules[0].ID != "one" {
			t.Fatal("invalid config accepted", body)
		}
	}
	for _, tc := range []struct {
		content, origin string
		status          int
	}{{"text/plain", "", 415}, {"application/json", "https://other.example", 403}} {
		r := httptest.NewRequest("POST", "/api/webservice", strings.NewReader(`{}`))
		r.Header.Set("Content-Type", tc.content)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.WebServiceAPI(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code)
		}
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	for _, path := range []string{"/webservice", "/api/webservice"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusTemporaryRedirect {
			t.Fatal("unauthenticated access", path, w.Code)
		}
	}
}

func TestWebServiceAPISaveFailure(t *testing.T) {
	repo := &failingWebRepository{}
	repo.conf.WebServiceEnabled = true
	m := webservice.NewManager()
	defer m.Close()
	s := NewServer(repo, nil)
	s.WebServices = m
	r := httptest.NewRequest("POST", "/api/webservice", strings.NewReader(`{"enabled":false,"rules":[]}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.WebServiceAPI(w, r)
	if decodeResult(t, w).Status || !repo.conf.WebServiceEnabled {
		t.Fatal("failed save changed config")
	}
}
