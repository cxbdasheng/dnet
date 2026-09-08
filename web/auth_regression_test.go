package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

func TestWANAccessRejectsSpoofedHeaders(t *testing.T) {
	t.Setenv(helper.TrustedProxiesENV, "127.0.0.1")
	conf := config.Config{}
	conf.NotAllowWanAccess = true
	server := NewServer(&stubRepository{conf: conf}, nil)
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		req := httptest.NewRequest("GET", "/login", nil)
		req.RemoteAddr = "203.0.113.10:1234"
		req.Header.Set(header, "127.0.0.1")
		if server.checkWANAccess(req).Allowed {
			t.Fatalf("WAN restriction bypassed using %s", header)
		}
	}
}

func TestSettingsPasswordWhitespaceLogin(t *testing.T) {
	resetAuthStateForTest()
	t.Cleanup(resetAuthStateForTest)
	repo := &stubRepository{conf: config.Config{User: config.User{Username: "admin"}}}
	server := NewServer(repo, nil)
	response := httptest.NewRecorder()
	server.handleSettingsPost(response, httptest.NewRequest("POST", "/settings", strings.NewReader(`{"username":"admin","password":" secret "}`)))
	if !decodeResult(t, response).Status {
		t.Fatalf("settings failed: %s", response.Body.String())
	}
	for _, tc := range []struct {
		password string
		success  bool
	}{{"secret", false}, {" secret ", true}} {
		response = httptest.NewRecorder()
		server.handleLoginPost(response, httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"admin","password":"`+tc.password+`"}`)))
		if got := len(response.Result().Cookies()) > 0; got != tc.success {
			t.Fatalf("login success = %v, want %v: %s", got, tc.success, response.Body.String())
		}
	}
}
