package webservice

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const authCacheTTL = 5 * time.Minute

var passwordChecks = make(chan struct{}, 4)

type authAttempt struct {
	count int
	until time.Time
}
type accessAuth struct {
	mu       sync.Mutex
	digest   [32]byte
	expires  time.Time
	attempts map[string]authAttempt
	verify   func([]byte, []byte) error
}

func newAccessAuth() *accessAuth {
	return &accessAuth{attempts: map[string]authAttempt{}, verify: bcrypt.CompareHashAndPassword}
}
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

// Each rule generation owns its cache: applying credentials never reuses old successes.
// Cache only a digest of credentials, never the password or Authorization header.
func (a *accessAuth) check(r *http.Request, rule Rule, now time.Time) int {
	user, password, ok := r.BasicAuth()
	if !ok {
		return http.StatusUnauthorized
	}
	key := sha256.Sum256([]byte(user + "\x00" + password))
	a.mu.Lock()
	defer a.mu.Unlock()
	if now.Before(a.expires) && subtle.ConstantTimeCompare(key[:], a.digest[:]) == 1 {
		return 0
	}
	ip := clientAddress(r)
	for k, v := range a.attempts {
		if !now.Before(v.until) {
			delete(a.attempts, k)
		}
	}
	attempt, exists := a.attempts[ip]
	if (!exists && len(a.attempts) >= 256) || attempt.count >= 5 {
		return http.StatusTooManyRequests
	}
	if !exists {
		attempt.until = now.Add(time.Minute)
	}
	attempt.count++
	a.attempts[ip] = attempt
	if user != rule.Username || len(password) > 72 {
		return http.StatusUnauthorized
	}
	select {
	case passwordChecks <- struct{}{}:
	default:
		return http.StatusTooManyRequests
	}
	err := a.verify([]byte(rule.PasswordHash), []byte(password))
	<-passwordChecks
	if err != nil {
		return http.StatusUnauthorized
	}
	a.digest = key
	a.expires = now.Add(authCacheTTL)
	delete(a.attempts, ip)
	return 0
}
