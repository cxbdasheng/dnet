package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

type stubRepository struct {
	conf config.Config
}

func (r *stubRepository) Load() (config.Config, error) {
	return r.conf, nil
}

func (r *stubRepository) Save(conf *config.Config) error {
	r.conf = *conf
	return nil
}

func (r *stubRepository) ResetPassword(string) error {
	return nil
}

func resetAuthStateForTest() {
	globalLoginDetector.Reset()
	setCurrentCookie(&http.Cookie{})
}

func decodeResult(t *testing.T, recorder *httptest.ResponseRecorder) helper.Result {
	t.Helper()

	var result helper.Result
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode result: %v", err)
	}
	return result
}

func TestHandleLoginPost_LocksImmediatelyAtMaxFailedAttempts(t *testing.T) {
	resetAuthStateForTest()
	t.Cleanup(resetAuthStateForTest)

	conf := config.Config{}
	hashedPassword, err := conf.GeneratePassword("correct-password")
	if err != nil {
		t.Fatalf("GeneratePassword() error = %v", err)
	}

	repo := &stubRepository{
		conf: config.Config{
			User: config.User{
				Username: "admin",
				Password: hashedPassword,
			},
		},
	}
	server := &Server{configRepo: repo}

	for i := 0; i < MaxFailedAttempts-1; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"wrong-password"}`))
		server.handleLoginPost(recorder, request)

		result := decodeResult(t, recorder)
		if result.Msg != "用户名或密码错误" {
			t.Fatalf("attempt %d: msg = %q, want 用户名或密码错误", i+1, result.Msg)
		}
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"wrong-password"}`))
	server.handleLoginPost(recorder, request)

	result := decodeResult(t, recorder)
	if result.Msg != "登录失败次数过多，请稍后再试" {
		t.Fatalf("lock attempt msg = %q, want 登录失败次数过多，请稍后再试", result.Msg)
	}
	if !globalLoginDetector.IsLocked(time.Now()) {
		t.Fatal("expected login detector to be locked after hitting max failed attempts")
	}
}

func TestLogoutClearsCurrentCookie(t *testing.T) {
	resetAuthStateForTest()
	t.Cleanup(resetAuthStateForTest)

	token := "token-to-clear"
	setCurrentCookie(&http.Cookie{
		Name:    CookieName,
		Value:   token,
		Path:    "/",
		Expires: time.Now().Add(time.Hour),
	})

	if !IsValidToken(token) {
		t.Fatal("expected token to be valid before logout")
	}

	server := &Server{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/logout", nil)
	server.Logout(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want %d", recorder.Code, http.StatusFound)
	}
	if IsValidToken(token) {
		t.Fatal("expected token to be invalid after logout")
	}

	current := GetCurrentCookie()
	if current == nil {
		t.Fatal("expected current cookie snapshot after logout")
	}
	if current.Value != "" {
		t.Fatalf("current cookie value = %q, want empty", current.Value)
	}
	if current.MaxAge != -1 {
		t.Fatalf("current cookie MaxAge = %d, want -1", current.MaxAge)
	}
}

func TestGetCurrentCookieReturnsSnapshot(t *testing.T) {
	resetAuthStateForTest()
	t.Cleanup(resetAuthStateForTest)

	setCurrentCookie(&http.Cookie{
		Name:    CookieName,
		Value:   "snapshot-token",
		Path:    "/",
		Expires: time.Now().Add(time.Hour),
	})

	snapshot := GetCurrentCookie()
	snapshot.Value = "mutated"

	current := GetCurrentCookie()
	if current.Value != "snapshot-token" {
		t.Fatalf("current cookie value = %q, want snapshot-token", current.Value)
	}
}
