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
	globalSessions.reset()
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
	globalSessions.add(token, time.Now().Add(time.Hour))

	if !IsValidToken(token) {
		t.Fatal("expected token to be valid before logout")
	}

	server := &Server{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/logout", nil)
	request.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	server.Logout(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("logout status = %d, want %d", recorder.Code, http.StatusFound)
	}
	if IsValidToken(token) {
		t.Fatal("expected token to be invalid after logout")
	}
}

func TestLogoutOnlyClearsCurrentSession(t *testing.T) {
	resetAuthStateForTest()
	t.Cleanup(resetAuthStateForTest)

	// 两个设备各自的会话令牌
	tokenA := "device-a-token"
	tokenB := "device-b-token"
	globalSessions.add(tokenA, time.Now().Add(time.Hour))
	globalSessions.add(tokenB, time.Now().Add(time.Hour))

	// 设备 A 登出
	server := &Server{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/logout", nil)
	request.AddCookie(&http.Cookie{Name: CookieName, Value: tokenA})
	server.Logout(recorder, request)

	if IsValidToken(tokenA) {
		t.Fatal("expected device A token to be invalid after its logout")
	}
	// 设备 B 的会话不应受影响
	if !IsValidToken(tokenB) {
		t.Fatal("expected device B token to remain valid after device A logout")
	}
}

func TestSessionStoreRejectsExpiredToken(t *testing.T) {
	resetAuthStateForTest()
	t.Cleanup(resetAuthStateForTest)

	expired := "expired-token"
	globalSessions.add(expired, time.Now().Add(-time.Minute))

	if IsValidToken(expired) {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestMultipleSessionsCoexist(t *testing.T) {
	resetAuthStateForTest()
	t.Cleanup(resetAuthStateForTest)

	// 模拟同一账号在多个设备并发登录：后登录不应挤掉先登录
	tokens := []string{"t1", "t2", "t3"}
	for _, tk := range tokens {
		globalSessions.add(tk, time.Now().Add(time.Hour))
	}
	for _, tk := range tokens {
		if !IsValidToken(tk) {
			t.Fatalf("expected token %q to remain valid", tk)
		}
	}
}
