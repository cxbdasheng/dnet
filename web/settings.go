package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"strconv"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

// cliOverride 读取 CLI 锁定 env 变量，返回 (locked, cliValue)
func cliOverride(envName string) (bool, int) {
	raw := os.Getenv(envName)
	if raw == "" {
		return false, 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return false, 0
	}
	return true, v
}

//go:embed settings.html
var settingsEmbedFile embed.FS

type SettingsRequest struct {
	Username          string `json:"username"`
	Password          string `json:"password"`
	NotAllowWanAccess bool   `json:"not_allow_wan_access"`
	Every             int    `json:"every"`
	DCDNCacheTimes    int    `json:"dcdn_cache_times"`
	DDNSCacheTimes    int    `json:"ddns_cache_times"`
}

func (s *Server) Settings(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		s.handleSettingsGet(writer, request)
	case http.MethodPost:
		s.handleSettingsPost(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}

func (s *Server) handleSettingsGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(settingsEmbedFile, "settings.html")
	if err != nil {
		helper.Error(helper.LogTypeConfig, "解析 settings.html 模板失败: %v", err)
		return
	}
	conf, err := s.configRepo.Load()
	if err != nil {
		helper.Error(helper.LogTypeConfig, "获取配置失败: %v", err)
		return
	}

	// 未配置时填充默认值，前端直接展示"生效值"
	settings := conf.Settings
	if settings.Every == 0 {
		settings.Every = config.DefaultEvery
	}
	dcdnCacheTimes := conf.DCDNConfig.CacheTimes
	if dcdnCacheTimes == 0 {
		dcdnCacheTimes = config.DefaultCacheTimes
	}
	ddnsCacheTimes := conf.DDNSConfig.CacheTimes
	if ddnsCacheTimes == 0 {
		ddnsCacheTimes = config.DefaultCacheTimes
	}

	// 如果 CLI 显式锁定了对应参数，展示 CLI 值（Web UI 修改无效）
	everyLocked, cliEvery := cliOverride(config.CLIEveryENV)
	if everyLocked {
		settings.Every = cliEvery
	}
	dcdnLocked, cliDCDN := cliOverride(config.CLIDCDNCacheTimesENV)
	if dcdnLocked {
		dcdnCacheTimes = cliDCDN
	}
	ddnsLocked, cliDDNS := cliOverride(config.CLIDDNSCacheTimesENV)
	if ddnsLocked {
		ddnsCacheTimes = cliDDNS
	}

	err = tmpl.Execute(writer, struct {
		config.User
		config.Settings
		DCDNCacheTimes       int
		DDNSCacheTimes       int
		EveryLocked          bool
		DCDNCacheTimesLocked bool
		DDNSCacheTimesLocked bool
	}{
		conf.User,
		settings,
		dcdnCacheTimes,
		ddnsCacheTimes,
		everyLocked,
		dcdnLocked,
		ddnsLocked,
	})
	if err != nil {
		helper.Error(helper.LogTypeConfig, "执行 settings 模板失败: %v", err)
	}
}

func (s *Server) handleSettingsPost(writer http.ResponseWriter, request *http.Request) {
	var settingsReq SettingsRequest
	if err := json.NewDecoder(request.Body).Decode(&settingsReq); err != nil {
		helper.Error(helper.LogTypeConfig, "请求解析失败: %v", err)
		helper.ReturnError(writer, "请求格式错误")
		return
	}
	conf, err := s.configRepo.Load()
	if err != nil {
		helper.Error(helper.LogTypeConfig, "获取配置失败: %v", err)
		helper.ReturnError(writer, "获取配置失败")
		return
	}
	if settingsReq.Every != 0 && (settingsReq.Every < 10 || settingsReq.Every > 86400) {
		helper.ReturnError(writer, "同步间隔需在 10 – 86400 秒之间")
		return
	}
	if settingsReq.DCDNCacheTimes != 0 && (settingsReq.DCDNCacheTimes < 1 || settingsReq.DCDNCacheTimes > 1000) {
		helper.ReturnError(writer, "DCDN 强制同步次数需在 1 – 1000 之间")
		return
	}
	if settingsReq.DDNSCacheTimes != 0 && (settingsReq.DDNSCacheTimes < 1 || settingsReq.DDNSCacheTimes > 1000) {
		helper.ReturnError(writer, "DDNS 强制同步次数需在 1 – 1000 之间")
		return
	}
	conf.NotAllowWanAccess = settingsReq.NotAllowWanAccess
	conf.Username = settingsReq.Username
	// CLI 锁定的字段不接受 Web UI 更新，保留用户已有的 config 值
	// （用户后续移除 CLI 参数重启后，仍能拿回之前的配置）
	if everyLocked, _ := cliOverride(config.CLIEveryENV); !everyLocked {
		conf.Every = settingsReq.Every
	}
	if dcdnLocked, _ := cliOverride(config.CLIDCDNCacheTimesENV); !dcdnLocked {
		conf.DCDNConfig.CacheTimes = settingsReq.DCDNCacheTimes
	}
	if ddnsLocked, _ := cliOverride(config.CLIDDNSCacheTimesENV); !ddnsLocked {
		conf.DDNSConfig.CacheTimes = settingsReq.DDNSCacheTimes
	}
	if settingsReq.Password != "" {
		hashedPwd, err := conf.GeneratePassword(settingsReq.Password)
		if err != nil {
			helper.ReturnError(writer, "密码加密失败")
			return
		}
		conf.Password = hashedPwd
	}
	// 保存配置
	if err := s.configRepo.Save(&conf); err != nil {
		helper.Error(helper.LogTypeConfig, "保存配置失败: %v", err)
		helper.ReturnError(writer, "保存配置失败")
		return
	}

	helper.ReturnSuccess(writer, "配置保存成功", nil)
}
