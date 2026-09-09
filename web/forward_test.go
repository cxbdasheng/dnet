package web

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/cxbdasheng/dnet/forward"
)

func TestForwardAPIValidationAndPersistence(t *testing.T) {
	repo := &stubRepository{}
	repo.conf.Username = "keep-user"
	m := forward.NewManager()
	defer m.Close()
	s := NewServer(repo, nil)
	s.Forwarder = m
	r := forward.Rule{ID: "one", Name: "NAS", Network: "tcp4", ListenAddress: "127.0.0.1", ListenPort: 18443, TargetHost: "127.0.0.1", TargetPort: 443, DialTimeoutSec: 10, MaxConnections: 100}
	body, _ := json.Marshal(map[string]any{"rules": []forward.Rule{r}})
	w := httptest.NewRecorder()
	s.ForwardAPI(w, httptest.NewRequest("POST", "/api/forward", strings.NewReader(string(body))))
	if !decodeResult(t, w).Status || len(repo.conf.ForwardRules) != 1 || repo.conf.Username != "keep-user" {
		t.Fatal("save failed", w.Body.String())
	}
	w = httptest.NewRecorder()
	s.ForwardAPI(w, httptest.NewRequest("POST", "/api/forward", strings.NewReader(`{"rules":[{"id":"invalid"}]}`)))
	if decodeResult(t, w).Status || repo.conf.ForwardRules[0].ID != "one" {
		t.Fatal("invalid config replaced saved rules")
	}
	w = httptest.NewRecorder()
	s.ForwardAPI(w, httptest.NewRequest("GET", "/api/forward", nil))
	if !decodeResult(t, w).Status {
		t.Fatal(w.Body.String())
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/forward", strings.NewReader(string(body))))
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("unauthenticated API status = %d", w.Code)
	}
}

func TestForwardGlobalSwitchPreservesRules(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	r := forward.Rule{ID: "one", Name: "NAS", Enabled: true, Network: "tcp4", ListenAddress: "127.0.0.1", ListenPort: port, TargetHost: "127.0.0.1", TargetPort: 443, DialTimeoutSec: 1, MaxConnections: 10}
	repo := &stubRepository{}
	m := forward.NewManager()
	defer m.Close()
	s := NewServer(repo, nil)
	s.Forwarder = m
	for _, enabled := range []bool{true, false, true} {
		body, _ := json.Marshal(map[string]any{"enabled": enabled, "rules": []forward.Rule{r}})
		w := httptest.NewRecorder()
		s.ForwardAPI(w, httptest.NewRequest("POST", "/api/forward", strings.NewReader(string(body))))
		if !decodeResult(t, w).Status {
			t.Fatal(w.Body.String())
		}
		if repo.conf.ForwardEnabled != enabled || len(repo.conf.ForwardRules) != 1 || !repo.conf.ForwardRules[0].Enabled {
			t.Fatal("global switch lost rule configuration")
		}
		if got := len(m.Status()) > 0; got != enabled {
			t.Fatalf("runtime enabled=%v want %v", got, enabled)
		}
		if got := len(repo.conf.ActiveForwardRules()) > 0; got != enabled {
			t.Fatal("startup rules ignore global switch")
		}
		if !enabled {
			check, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
			if err != nil {
				t.Fatal("disabled listener remains bound", err)
			}
			check.Close()
		}
	}
}
