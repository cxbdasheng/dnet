package webservice

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const speedSessionTTL = 75 * time.Second
const maxSpeedSessions = 2
const maxSpeedStreams = 8

type speedSession struct {
	ip, host string
	expires  time.Time
	active   int
	closed   bool
}
type speedAdmission struct {
	mu       sync.Mutex
	sessions map[string]*speedSession
}

var speedSlots = &speedAdmission{sessions: map[string]*speedSession{}}

func (l *speedAdmission) prune(now time.Time) {
	for k, s := range l.sessions {
		if (s.closed || !now.Before(s.expires)) && s.active == 0 {
			delete(l.sessions, k)
		}
	}
}
func (l *speedAdmission) start(ip, host string, now time.Time) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	if len(l.sessions) >= maxSpeedSessions {
		return "", false
	}
	for _, s := range l.sessions {
		if s.ip == ip {
			return "", false
		}
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", false
	}
	token := hex.EncodeToString(b)
	l.sessions[token] = &speedSession{ip: ip, host: host, expires: now.Add(speedSessionTTL)}
	return token, true
}
func (l *speedAdmission) release(token, ip, host string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if s := l.sessions[token]; s != nil && s.ip == ip && s.host == host {
		s.closed = true
	}
	l.prune(time.Now())
}
func (l *speedAdmission) enter(token, ip, host string, now time.Time) (func(), int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune(now)
	s := l.sessions[token]
	if s == nil || s.closed || !now.Before(s.expires) || s.ip != ip || s.host != host {
		return nil, http.StatusForbidden
	}
	if s.active >= maxSpeedStreams {
		return nil, http.StatusTooManyRequests
	}
	s.active++
	var once sync.Once
	return func() { once.Do(func() { l.mu.Lock(); defer l.mu.Unlock(); s.active--; l.prune(time.Now()) }) }, 0
}
func speedTestManaged(w http.ResponseWriter, r *http.Request) { speedSlots.serve(w, r) }
func (l *speedAdmission) serve(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Query().Get("dws_test")
	ip, host := clientAddress(r), r.Host
	if action == "start" || action == "stop" {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "请求方法不支持", 405)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != host {
				http.Error(w, "请求来源不匹配", 403)
				return
			}
		}
		if action == "stop" {
			l.release(r.URL.Query().Get("session"), ip, host)
			w.WriteHeader(204)
			return
		}
		token, ok := l.start(ip, host, time.Now())
		if !ok {
			w.Header().Set("Retry-After", "5")
			http.Error(w, "测速名额已满，或当前 IP 已有测速任务，请稍后重试。", 429)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"session": token})
		return
	}
	switch action {
	case "empty", "garbage", "ping", "download", "upload":
		leave, code := l.enter(r.URL.Query().Get("session"), ip, host, time.Now())
		if code != 0 {
			w.Header().Set("Cache-Control", "no-store")
			if code == 429 {
				w.Header().Set("Retry-After", "5")
			}
			http.Error(w, "测速会话无效、已过期或并发请求过多，请重新开始测速。", code)
			return
		}
		defer leave()
	}
	speedTest(w, r)
}
