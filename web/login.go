package web

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
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
	MaxFailedAttempts  = 5                // 最大失败尝试次数
	SetupTimeLimit     = 30 * time.Minute // 初始设置时间限制
	LoginLockDuration  = 30 * time.Minute // 登录失败锁定时间
	LoginCheckInterval = 5 * time.Minute  // 登录检测间隔
)

// currentCookie 当前系统Cookie实例（单例模式）
var currentCookie = &http.Cookie{}

// serverStartTime 服务启动时间
var serverStartTime = time.Now()

// LoginDetector 登录检测器
type LoginDetector struct {
	FailedAttempts uint32       // 失败尝试次数
	ResetTicker    *time.Ticker // 重置定时器
}

// globalLoginDetector 全局登录检测器实例
var globalLoginDetector = &LoginDetector{
	ResetTicker: time.NewTicker(LoginCheckInterval),
}

// LoginRequest 登录请求结构体
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func Login(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case "GET":
		handleLoginGet(writer, request)
		return
	case "POST":
		handleLoginPost(writer, request)
		return
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}

// handleLoginGet 处理登录页面GET请求
func handleLoginGet(writer http.ResponseWriter, _ *http.Request) {
	tmpl, err := template.ParseFS(loginEmbedFile, "login.html")
	if err != nil {
		log.Printf("模板解析失败: %v", err)
		helper.ReturnError(writer, "页面加载失败")
		return
	}
	// 初始化时无配置文件
	conf, _ := config.GetConfigCached()

	data := struct {
		EmptyUser bool `json:"empty_user"`
	}{
		EmptyUser: conf.Username == "" || conf.Password == "",
	}

	if err = tmpl.Execute(writer, data); err != nil {
		log.Printf("模板执行失败: %v", err)
		helper.ReturnError(writer, "页面渲染失败")
	}
}

// handleLoginPost 处理登录POST请求
func handleLoginPost(writer http.ResponseWriter, request *http.Request) {
	// 检查登录失败次数限制
	if globalLoginDetector.FailedAttempts >= MaxFailedAttempts {
		resetLoginAttempts()
		helper.ReturnError(writer, "登录失败次数过多，请稍后再试")
		return
	}

	// 解析请求体
	var loginReq LoginRequest
	if err := json.NewDecoder(request.Body).Decode(&loginReq); err != nil {
		log.Printf("请求解析失败: %v", err)
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
	conf, _ := config.GetConfigCached()

	// 处理初始用户设置
	if conf.Username == "" || conf.Password == "" {
		// 获取客户端IP
		clientIP := helper.GetClientIP(request)
		log.Printf("登录尝试 - 用户: %s, IP: %s", loginReq.Username, clientIP)
		if err := handleInitialSetup(&conf, loginReq, clientIP); err != nil {
			log.Printf("初始设置失败: %v", err)
			helper.ReturnError(writer, err.Error())
			return
		}
	}

	// 验证登录信息
	if loginReq.Username == conf.Username && conf.VerifyPassword(loginReq.Password) {
		// 登录成功处理
		if err := handleLoginSuccess(writer, &conf); err != nil {
			log.Printf("登录成功处理失败: %v", err)
			helper.ReturnError(writer, "登录处理失败")
			return
		}
		return
	}

	// 登录失败处理
	globalLoginDetector.FailedAttempts++
	log.Printf("登录失败 - 用户: %s, 失败次数: %d", loginReq.Username, globalLoginDetector.FailedAttempts)
	helper.ReturnError(writer, "用户名或密码错误")
}

// handleInitialSetup 处理初始用户设置
func handleInitialSetup(conf *config.Config, loginReq LoginRequest, clientIP string) error {
	if time.Since(serverStartTime) > SetupTimeLimit {
		deadline := serverStartTime.Add(SetupTimeLimit)
		return fmt.Errorf("需在 %s 之前完成用户名密码设置,请重启ddns-go",
			deadline.Format("2006-01-02 15:04:05"))
	}

	// 根据IP类型设置访问权限
	conf.NotAllowWanAccess = helper.IsPrivateIP(clientIP)

	conf.Username = loginReq.Username
	hashedPwd, err := conf.GeneratePassword(loginReq.Password)
	if err != nil {
		return fmt.Errorf("密码加密失败: %v", err)
	}

	conf.Password = hashedPwd
	if err = conf.SaveConfig(); err != nil {
		return fmt.Errorf("保存配置失败: %v", err)
	}

	log.Printf("初始设置完成 - 用户: %s, 内网模式: %v", conf.Username, conf.NotAllowWanAccess)
	return nil
}

// handleLoginSuccess 处理登录成功
func handleLoginSuccess(writer http.ResponseWriter, conf *config.Config) error {
	// 重置登录检测器
	globalLoginDetector.ResetTicker.Stop()
	globalLoginDetector.FailedAttempts = 0

	// 计算Cookie过期时间
	var expires time.Time
	if conf.NotAllowWanAccess {
		expires = time.Now().Add(CookieMaxAgePrivate)
	} else {
		expires = time.Now().Add(CookieMaxAgePublic)
	}

	// 生成并设置Cookie
	currentCookie = &http.Cookie{
		Name:     CookieName,
		Value:    generateToken(),
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   false, // 根据需要调整
		SameSite: http.SameSiteStrictMode,
	}

	http.SetCookie(writer, currentCookie)
	log.Printf("用户登录成功: %s, Cookie过期时间: %v", conf.Username, expires)

	helper.ReturnSuccess(writer, "用户登录成功", currentCookie.Value)
	return nil
}

// resetLoginAttempts 重置登录尝试次数（带锁定机制）
func resetLoginAttempts() {
	globalLoginDetector.FailedAttempts++
	globalLoginDetector.ResetTicker.Reset(LoginLockDuration)

	log.Printf("登录尝试已锁定 %v，失败次数: %d", LoginLockDuration, globalLoginDetector.FailedAttempts)

	// 启动后台协程处理解锁
	go func(ticker *time.Ticker) {
		defer ticker.Stop() // 确保资源释放

		for range ticker.C {
			// 解锁：重置为最大尝试次数-1，允许再次尝试
			globalLoginDetector.FailedAttempts = MaxFailedAttempts - 1
			log.Printf("登录锁定已解除，可重新尝试登录")
			return
		}
	}(globalLoginDetector.ResetTicker)
}

// GetCurrentCookie 获取当前Cookie（用于其他模块验证）
func GetCurrentCookie() *http.Cookie {
	return currentCookie
}

// IsValidToken 验证令牌是否有效
func IsValidToken(token string) bool {
	if currentCookie == nil || currentCookie.Value == "" {
		return false
	}

	// 检查Cookie是否过期
	if time.Now().After(currentCookie.Expires) {
		return false
	}

	return currentCookie.Value == token
}

// generateToken 生成安全的登录令牌
func generateToken() string {
	randomBytes := make([]byte, TokenLength)
	if _, err := rand.Read(randomBytes); err != nil {
		// 容错处理：使用时间戳作为后备方案
		log.Printf("生成随机令牌失败，使用时间戳后备: %v", err)
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000")))
	}

	// 转换为十六进制字符串并返回
	return hex.EncodeToString(randomBytes)
}
