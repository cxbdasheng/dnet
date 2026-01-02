package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

type ViewFunc func(http.ResponseWriter, *http.Request)

// AccessCheckResult 访问检查结果
type AccessCheckResult struct {
	Allowed bool
	Reason  string
}

// checkWANAccess 检查WAN访问权限（提取公共逻辑）
func checkWANAccess(r *http.Request) AccessCheckResult {
	clientIP := helper.GetClientIP(r)
	isPrivateIP := helper.IsLocalAddress(clientIP)

	conf, err := config.GetConfigCached()

	// 配置文件为空且启动时间超过3小时，禁止从公网访问
	if err != nil && time.Since(serverStartTime) > 3*time.Hour && !isPrivateIP {
		return AccessCheckResult{
			Allowed: false,
			Reason:  fmt.Sprintf("客户端 %s 被拒绝访问：配置文件为空，超过3小时禁止从公网访问", clientIP),
		}
	}

	// 当配置禁止公网访问时，检查客户端IP
	if conf.NotAllowWanAccess && !isPrivateIP {
		return AccessCheckResult{
			Allowed: false,
			Reason:  fmt.Sprintf("客户端 %s 被拒绝访问：禁止从公网访问", clientIP),
		}
	}

	return AccessCheckResult{Allowed: true}
}

// AuthAssert 保护静态等文件不被公网访问
func AuthAssert(f ViewFunc) ViewFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accessResult := checkWANAccess(r)
		if !accessResult.Allowed {
			w.WriteHeader(http.StatusForbidden)
			helper.Warn(helper.LogTypeAuth, "%s", accessResult.Reason)
			return
		}

		f(w, r) // 执行被装饰的函数
	}
}

// Auth 验证Token是否已经通过
func Auth(f ViewFunc) ViewFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 检查WAN访问权限
		accessResult := checkWANAccess(r)
		if !accessResult.Allowed {
			w.WriteHeader(http.StatusForbidden)
			helper.Warn(helper.LogTypeAuth, "%s", accessResult.Reason)
			return
		}

		// 检查登录Cookie
		cookieInWeb, err := r.Cookie(CookieName)
		if err != nil {
			http.Redirect(w, r, "./login", http.StatusTemporaryRedirect)
			return
		}

		// 验证token
		if currentCookie.Value != "" &&
			currentCookie.Value == cookieInWeb.Value &&
			currentCookie.Expires.After(time.Now()) {
			f(w, r) // 执行被装饰的函数
			return
		}

		http.Redirect(w, r, "./login", http.StatusTemporaryRedirect)
	}
}
