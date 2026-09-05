package web

import (
	"net/http"
	"time"
)

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	// 仅注销当前请求携带的会话令牌，不影响其他设备的在线会话
	if c, err := r.Cookie(CookieName); err == nil {
		globalSessions.remove(c.Value)
	}

	expiredCookie := &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
	}
	// 设置过期的 Cookie
	http.SetCookie(w, expiredCookie)

	// 重定向用户到登录页面
	http.Redirect(w, r, "./login", http.StatusFound)
}
