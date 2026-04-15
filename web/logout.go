package web

import (
	"net/http"
	"time"
)

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	expiredCookie := &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
	}
	setCurrentCookie(expiredCookie)
	// 设置过期的 Cookie
	http.SetCookie(w, expiredCookie)

	// 重定向用户到登录页面
	http.Redirect(w, r, "./login", http.StatusFound)
}
