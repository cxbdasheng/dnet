package web

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

//go:embed login.html
var loginEmbedFile embed.FS

// CookieName 登录令牌Cookie名称
const CookieName = "token"

// TokenLength 长度
// 32字节 = 256位熵
const TokenLength = 32 // 32字节 = 256位熵

// CookieMaxAge Cookie最大存活时间配置
const (
	CookieMaxAgePublic  = 24 * time.Hour      // 外网访问：1天
	CookieMaxAgePrivate = 30 * 24 * time.Hour // 内网访问：30天
)

// LoginLimits 登录限制配置
const (
	MaxFailedAttempts = 5                // 最大失败尝试次数
	SetupTimeLimit    = 30 * time.Minute // 初始设置时间限制
	LoginLockDuration = 30 * time.Minute // 登录失败锁定时间
)

// currentCookie 当前系统Cookie实例（单例模式）
var currentCookie = &http.Cookie{}
var currentCookieMu sync.RWMutex

// serverStartTime 服务启动时间
var serverStartTime = time.Now()

// LoginDetector 登录检测器
type LoginDetector struct {
	mu             sync.Mutex
	FailedAttempts uint32
	LockedUntil    time.Time
}

// globalLoginDetector 全局登录检测器实例
var globalLoginDetector = &LoginDetector{}

// LoginRequest 登录请求结构体
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) Login(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		s.handleLoginGet(writer, request)
		return
	case http.MethodPost:
		s.handleLoginPost(writer, request)
		return
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}

// handleLoginGet 处理登录页面GET请求
func (s *Server) handleLoginGet(writer http.ResponseWriter, _ *http.Request) {
	tmpl, err := template.ParseFS(loginEmbedFile, "login.html")
	if err != nil {
		helper.Info(helper.LogTypeSystem, "模板解析失败: %v", err)
		helper.ReturnError(writer, "页面加载失败")
		return
	}
	// 初始化时无配置文件
	conf, _ := s.configRepo.Load()

	data := struct {
		EmptyUser bool `json:"empty_user"`
		Version   string
	}{
		EmptyUser: conf.Username == "" || conf.Password == "",
		Version:   os.Getenv(VersionEnv),
	}

	if err = tmpl.Execute(writer, data); err != nil {
		helper.Info(helper.LogTypeSystem, "模板执行失败: %v", err)
		helper.ReturnError(writer, "页面渲染失败")
	}
}

// handleLoginPost 处理登录POST请求
func (s *Server) handleLoginPost(writer http.ResponseWriter, request *http.Request) {
	// 检查登录失败次数限制
	if globalLoginDetector.IsLocked(time.Now()) {
		helper.ReturnError(writer, "登录失败次数过多，请稍后再试")
		return
	}

	// 解析请求体
	var loginReq LoginRequest
	if err := json.NewDecoder(request.Body).Decode(&loginReq); err != nil {
		helper.Info(helper.LogTypeSystem, "请求解析失败: %v", err)
		helper.ReturnError(writer, "请求格式错误")
		return
	}

	// 验证输入
	loginReq.Username = strings.TrimSpace(loginReq.Username)
	loginReq.Password = strings.TrimSpace(loginReq.Password)

	if loginReq.Username == "" || loginReq.Password == "" {
		helper.ReturnError(writer, "用户名和密码不能为空")
		return
	}
	// 获取配置
	conf, _ := s.configRepo.Load()

	// 处理初始用户设置
	if conf.Username == "" || conf.Password == "" {
		// 获取客户端IP
		clientIP := helper.GetClientIP(request)
		helper.Info(helper.LogTypeSystem, "登录尝试 - 用户: %s, IP: %s", loginReq.Username, clientIP)
		if err := s.handleInitialSetup(&conf, loginReq, clientIP); err != nil {
			helper.Info(helper.LogTypeSystem, "初始设置失败: %v", err)
			helper.ReturnError(writer, err.Error())
			return
		}
	}

	// 验证登录信息
	if loginReq.Username == conf.Username && conf.VerifyPassword(loginReq.Password) {
		// 登录成功处理
		if err := s.handleLoginSuccess(writer, &conf); err != nil {
			helper.Info(helper.LogTypeSystem, "登录成功处理失败: %v", err)
			helper.ReturnError(writer, "登录处理失败")
			return
		}
		return
	}

	// 登录失败处理
	attempts, locked := globalLoginDetector.RecordFailure(time.Now())
	if locked {
		helper.Info(helper.LogTypeSystem, "登录尝试已锁定 %v，失败次数: %d", LoginLockDuration, attempts)
		helper.ReturnError(writer, "登录失败次数过多，请稍后再试")
		return
	}
	helper.Info(helper.LogTypeSystem, "登录失败 - 用户: %s, 失败次数: %d", loginReq.Username, attempts)
	helper.ReturnError(writer, "用户名或密码错误")
}

// handleInitialSetup 处理初始用户设置
func (s *Server) handleInitialSetup(conf *config.Config, loginReq LoginRequest, clientIP string) error {
	if time.Since(serverStartTime) > SetupTimeLimit {
		deadline := serverStartTime.Add(SetupTimeLimit)
		return fmt.Errorf("需在 %s 之前完成用户名密码设置，请重启 D-NET",
			deadline.Format("2006-01-02 15:04:05"))
	}

	// 根据IP类型设置访问权限
	conf.NotAllowWanAccess = helper.IsLocalAddress(clientIP)

	conf.Username = loginReq.Username
	hashedPwd, err := conf.GeneratePassword(loginReq.Password)
	if err != nil {
		return fmt.Errorf("密码加密失败: %v", err)
	}

	conf.Password = hashedPwd
	if err = s.configRepo.Save(conf); err != nil {
		return fmt.Errorf("保存配置失败: %v", err)
	}
	helper.Info(helper.LogTypeSystem, "初始设置完成 - 用户: %s, 内网模式: %v", conf.Username, conf.NotAllowWanAccess)
	return nil
}

// handleLoginSuccess 处理登录成功
func (s *Server) handleLoginSuccess(writer http.ResponseWriter, conf *config.Config) error {
	// 重置登录检测器
	globalLoginDetector.Reset()

	// 计算Cookie过期时间
	var expires time.Time
	if conf.NotAllowWanAccess {
		expires = time.Now().Add(CookieMaxAgePrivate)
	} else {
		expires = time.Now().Add(CookieMaxAgePublic)
	}

	// 生成并设置Cookie
	newCookie := &http.Cookie{
		Name:     CookieName,
		Value:    generateToken(),
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   false, // 根据需要调整
		SameSite: http.SameSiteStrictMode,
	}

	setCurrentCookie(newCookie)

	http.SetCookie(writer, newCookie)
	helper.Info(helper.LogTypeSystem, "用户登录成功: %s, Cookie 过期时间: %v", conf.Username, expires)

	helper.ReturnSuccess(writer, "用户登录成功", newCookie.Value)
	return nil
}

// IsLocked 返回当前是否仍处于锁定状态，过期锁定会被自动清理。
func (d *LoginDetector) IsLocked(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.clearExpiredLock(now)
	return !d.LockedUntil.IsZero()
}

// RecordFailure 记录一次失败尝试，并在到达阈值时立即锁定。
func (d *LoginDetector) RecordFailure(now time.Time) (uint32, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.clearExpiredLock(now)
	if !d.LockedUntil.IsZero() {
		return d.FailedAttempts, true
	}

	d.FailedAttempts++
	if d.FailedAttempts >= MaxFailedAttempts {
		d.LockedUntil = now.Add(LoginLockDuration)
		return d.FailedAttempts, true
	}

	return d.FailedAttempts, false
}

// Reset 在登录成功后清空失败状态和锁定窗口。
func (d *LoginDetector) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.FailedAttempts = 0
	d.LockedUntil = time.Time{}
}

func (d *LoginDetector) clearExpiredLock(now time.Time) {
	if d.LockedUntil.IsZero() {
		return
	}
	if now.Before(d.LockedUntil) {
		return
	}

	d.FailedAttempts = 0
	d.LockedUntil = time.Time{}
	helper.Info(helper.LogTypeSystem, "登录锁定已解除，可重新尝试登录")
}

func setCurrentCookie(cookie *http.Cookie) {
	currentCookieMu.Lock()
	defer currentCookieMu.Unlock()

	if cookie == nil {
		currentCookie = &http.Cookie{}
		return
	}

	cookieCopy := *cookie
	currentCookie = &cookieCopy
}

func getCurrentCookieSnapshot() *http.Cookie {
	currentCookieMu.RLock()
	defer currentCookieMu.RUnlock()

	if currentCookie == nil {
		return nil
	}

	cookieCopy := *currentCookie
	return &cookieCopy
}

// GetCurrentCookie 获取当前Cookie（用于其他模块验证）
func GetCurrentCookie() *http.Cookie {
	return getCurrentCookieSnapshot()
}

// IsValidToken 验证令牌是否有效
func IsValidToken(token string) bool {
	c := getCurrentCookieSnapshot()

	if c == nil || c.Value == "" {
		return false
	}
	if time.Now().After(c.Expires) {
		return false
	}
	return c.Value == token
}

// generateToken 生成安全的登录令牌
func generateToken() string {
	randomBytes := make([]byte, TokenLength)
	if _, err := rand.Read(randomBytes); err != nil {
		// 容错处理：使用时间戳作为后备方案
		helper.Info(helper.LogTypeSystem, "生成随机令牌失败，使用时间戳后备: %v", err)
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000")))
	}

	// 转换为十六进制字符串并返回
	return hex.EncodeToString(randomBytes)
}
