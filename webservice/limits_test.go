package webservice

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func authRequest(password string) *http.Request {
	r := httptest.NewRequest("GET", "http://one.example/", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.SetBasicAuth("reader", password)
	return r
}
func TestAuthCacheConcurrentExpiryAndFailures(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	rule := Rule{Username: "reader", PasswordHash: string(hash)}
	a := newAccessAuth()
	var checks atomic.Int32
	a.verify = func(h, p []byte) error { checks.Add(1); return bcrypt.CompareHashAndPassword(h, p) }
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if code := a.check(authRequest("secret"), rule, now); code != 0 {
				t.Errorf("valid auth: %d", code)
			}
		}()
	}
	wg.Wait()
	if checks.Load() != 1 {
		t.Fatal("parallel requests repeated bcrypt", checks.Load())
	}
	if code := a.check(authRequest("wrong"), rule, now); code != 401 {
		t.Fatal("cache accepted different password", code)
	}
	if code := a.check(authRequest("secret"), rule, now.Add(authCacheTTL)); code != 0 {
		t.Fatal(code)
	}
	if checks.Load() != 3 {
		t.Fatal("expired cache not revalidated", checks.Load())
	}
	a = newAccessAuth()
	for i := 0; i < 5; i++ {
		if code := a.check(authRequest("wrong"), rule, now); code != 401 {
			t.Fatal(code)
		}
	}
	if code := a.check(authRequest("wrong"), rule, now); code != 429 {
		t.Fatal("failure throttle missing", code)
	}
	if code := a.check(authRequest("secret"), rule, now.Add(time.Minute)); code != 0 {
		t.Fatal("throttle did not expire", code)
	}
	for i := 0; i < 300; i++ {
		r := authRequest("wrong")
		r.RemoteAddr = fmt.Sprintf("192.0.%d.%d:1234", i/256, i%256)
		a.check(r, rule, now)
	}
	if len(a.attempts) > 256 {
		t.Fatal("unbounded failure map")
	}
}
func TestSpeedSessionBounds(t *testing.T) {
	l := &speedAdmission{sessions: map[string]*speedSession{}}
	now := time.Now()
	first, ok := l.start("ip1", "a", now)
	if !ok {
		t.Fatal("start")
	}
	if _, ok := l.start("ip1", "b", now); ok {
		t.Fatal("per IP bypass")
	}
	second, ok := l.start("ip2", "b", now)
	if !ok {
		t.Fatal("second")
	}
	if _, ok := l.start("ip3", "c", now); ok {
		t.Fatal("global limit bypass")
	}
	if _, code := l.enter(first, "ip2", "a", now); code != 403 {
		t.Fatal("IP binding bypass")
	}
	if _, code := l.enter(first, "ip1", "b", now); code != 403 {
		t.Fatal("host binding bypass")
	}
	var releases []func()
	for i := 0; i < 8; i++ {
		leave, code := l.enter(first, "ip1", "a", now)
		if code != 0 {
			t.Fatal(code)
		}
		releases = append(releases, leave)
	}
	if _, code := l.enter(first, "ip1", "a", now); code != 429 {
		t.Fatal("stream limit bypass")
	}
	l.release(first, "ip1", "a")
	if _, ok := l.start("ip1", "a", now); ok {
		t.Fatal("released active transfers bypassed limit")
	}
	for _, leave := range releases {
		leave()
		leave()
	}
	if _, ok := l.start("ip1", "a", now); !ok {
		t.Fatal("slot not released")
	}
	if _, code := l.enter(second, "ip2", "b", now.Add(speedSessionTTL)); code != 403 {
		t.Fatal("expired token accepted")
	}
	if _, ok := l.start("ip3", "c", now.Add(speedSessionTTL)); !ok {
		t.Fatal("abandoned slots not reclaimed")
	}
}
func TestSpeedAdmissionHTTP(t *testing.T) {
	l := &speedAdmission{sessions: map[string]*speedSession{}}
	for _, action := range []string{"empty", "garbage", "download", "upload", "ping"} {
		w := httptest.NewRecorder()
		l.serve(w, httptest.NewRequest("GET", "http://one.example/?dws_test="+action, nil))
		if w.Code != 403 {
			t.Fatal("unguarded transfer", action, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "http://one.example/?dws_test=start", nil)
	r.Header.Set("Origin", "http://other.example")
	l.serve(w, r)
	if w.Code != 403 {
		t.Fatal("foreign origin accepted")
	}
	w = httptest.NewRecorder()
	l.serve(w, httptest.NewRequest("GET", "http://one.example/", nil))
	if w.Code != 200 {
		t.Fatal("page blocked")
	}
}
func TestCredentialUpdateInvalidatesCache(t *testing.T) {
	rule := testRule(t, "http://127.0.0.1:3000")
	rule.Type = "redirect"
	rule.AuthEnabled = true
	rule.Username = "reader"
	rule.Password = "secret"
	rules, err := Prepare([]Rule{rule}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	defer m.Close()
	if err = m.Apply(rules, nil); err != nil {
		t.Fatal(err)
	}
	call := func(password string) int {
		req := authRequest(password)
		req.Host = rule.Domain
		w := httptest.NewRecorder()
		m.groups[rule.groupKey()].ServeHTTP(w, req)
		return w.Code
	}
	if call("secret") != 302 {
		t.Fatal("initial auth")
	}
	rule.Password = "new-secret"
	next, err := Prepare([]Rule{rule}, rules)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Apply(next, nil); err != nil {
		t.Fatal(err)
	}
	if call("secret") != 401 || call("new-secret") != 302 {
		t.Fatal("credential update reused stale cache")
	}
}

func TestSpeedAdmissionConcurrent(t *testing.T) {
	l := &speedAdmission{sessions: map[string]*speedSession{}}
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, ok := l.start(fmt.Sprint(i), "host", time.Now()); ok {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != maxSpeedSessions {
		t.Fatal("concurrent admission exceeded bound", accepted.Load())
	}
	token, _ := func() (string, bool) {
		for k := range l.sessions {
			return k, true
		}
		return "", false
	}()
	s := l.sessions[token]
	accepted.Store(0)
	release := make(chan struct{})
	ready := make(chan struct{}, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			leave, code := l.enter(token, s.ip, s.host, time.Now())
			if code == 0 {
				accepted.Add(1)
			}
			ready <- struct{}{}
			if code == 0 {
				<-release
				leave()
			}
		}()
	}
	// Wait until every attempt either owns a stream or has been rejected.
	// Separate readiness avoids timing assumptions while keeping acquired slots occupied.
	for i := 0; i < 32; i++ {
		<-ready
	}
	count := accepted.Load()
	close(release)
	wg.Wait()
	if count != maxSpeedStreams {
		t.Fatal("concurrent streams exceeded bound", count)
	}
	if accepted.Load() < 1 {
		t.Fatal("no stream admitted")
	}
	if l.sessions[token].active != 0 {
		t.Fatal("stream slot leaked")
	}
}
