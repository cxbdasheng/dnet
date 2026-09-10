package web

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxbdasheng/dnet/forward"
	"github.com/cxbdasheng/dnet/helper"
)

func TestForwardProbe(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	rule := forward.Rule{ID: t.Name(), Name: "probe", Network: "tcp4", ListenAddress: "127.0.0.1", ListenPort: 8443, TargetHost: "127.0.0.1", TargetPort: l.Addr().(*net.TCPAddr).Port, DialTimeoutSec: 1, MaxConnections: 10}
	if rule.TargetPort == rule.ListenPort {
		rule.ListenPort++
	}
	repo := &stubRepository{}
	s := NewServer(repo, nil)
	call := func() *httptest.ResponseRecorder {
		b, _ := json.Marshal(rule)
		w := httptest.NewRecorder()
		s.ForwardProbe(w, httptest.NewRequest("POST", "/api/forward/probe", strings.NewReader(string(b))))
		return w
	}
	if w := call(); !decodeResult(t, w).Status {
		t.Fatal(w.Body.String())
	}
	if len(repo.conf.ForwardRules) != 0 || repo.conf.ForwardEnabled {
		t.Fatal("probe changed configuration")
	}
	l.Close()
	if w := call(); decodeResult(t, w).Status {
		t.Fatal("closed target passed")
	}
	rule.Network = "udp4"
	w := call()
	if !decodeResult(t, w).Status || !strings.Contains(w.Body.String(), `"supported":false`) {
		t.Fatal("UDP was reported reachable", w.Body.String())
	}
	rule.TargetHost = "http://invalid"
	if w := call(); decodeResult(t, w).Status {
		t.Fatal("invalid target accepted")
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	for _, path := range []string{"/api/forward/probe", "/api/forward/logs?id=x"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusTemporaryRedirect {
			t.Fatal("diagnostics missing authentication", path, w.Code)
		}
	}
}
func TestForwardRuleLogsUseID(t *testing.T) {
	first, second := t.Name()+"1", t.Name()+"2"
	helper.RuleLog(helper.LogLevelINFO, first, "same-name-before-rename")
	helper.RuleLog(helper.LogLevelINFO, second, "other-rule-secret")
	helper.RuleLog(helper.LogLevelINFO, first, "renamed-rule")
	s := NewServer(&stubRepository{}, nil)
	w := httptest.NewRecorder()
	s.ForwardLogs(w, httptest.NewRequest("GET", "/api/forward/logs?id="+first, nil))
	body := w.Body.String()
	if !strings.Contains(body, "same-name-before-rename") || !strings.Contains(body, "renamed-rule") || strings.Contains(body, "other-rule-secret") {
		t.Fatal(body)
	}
}
