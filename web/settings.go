package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

//go:embed settings.html
var settingsEmbedFile embed.FS

type SettingsRequest struct {
	Username          string `json:"username"`
	Password          string `json:"password"`
	NotAllowWanAccess bool   `json:"not_allow_wan_access"`
}

func Settings(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		handleSettingsGet(writer, request)
	case http.MethodPost:
		handleSettingsPost(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}

func handleSettingsGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(settingsEmbedFile, "settings.html")
	if err != nil {
		helper.Error(helper.LogTypeConfig, "解析 settings.html 模板失败: %v", err)
		return
	}
	conf, err := config.GetConfigCached()
	if err != nil {
		helper.Error(helper.LogTypeConfig, "获取配置失败: %v", err)
		return
	}

	err = tmpl.Execute(writer, struct {
		config.User
		config.Settings
	}{
		conf.User,
		conf.Settings,
	})
	if err != nil {
		helper.Error(helper.LogTypeConfig, "执行 settings 模板失败: %v", err)
	}
}

func handleSettingsPost(writer http.ResponseWriter, request *http.Request) {
	var settingsReq SettingsRequest
	if err := json.NewDecoder(request.Body).Decode(&settingsReq); err != nil {
		helper.Error(helper.LogTypeConfig, "请求解析失败: %v", err)
		helper.ReturnError(writer, "请求格式错误")
		return
	}
	conf, err := config.GetConfigCached()
	if err != nil {
		helper.Error(helper.LogTypeConfig, "获取配置失败: %v", err)
		helper.ReturnError(writer, "获取配置失败")
		return
	}
	conf.NotAllowWanAccess = settingsReq.NotAllowWanAccess
	conf.Username = settingsReq.Username
	if settingsReq.Password != "" {
		hashedPwd, err := conf.GeneratePassword(settingsReq.Password)
		if err != nil {
			helper.ReturnError(writer, "密码加密失败")
		}
		conf.Password = hashedPwd
	}
	// 保存配置
	if err := conf.SaveConfig(); err != nil {
		helper.Error(helper.LogTypeConfig, "保存配置失败: %v", err)
		helper.ReturnError(writer, "保存配置失败")
		return
	}

	helper.ReturnSuccess(writer, "配置保存成功", nil)
}
