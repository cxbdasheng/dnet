package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
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
	case "GET":
		handleSettingsGet(writer, request)
	case "POST":
		handleSettingsPost(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}
func handleSettingsGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(settingsEmbedFile, "settings.html")
	if err != nil {
		fmt.Println("Error happened..")
		fmt.Println(err)
		return
	}
	conf, err := config.GetConfigCached()
	if err != nil {
		fmt.Println("Error happened..")
		fmt.Println(err)
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
		fmt.Println("Error happened..")
		fmt.Println(err)
	}
}
func handleSettingsPost(writer http.ResponseWriter, request *http.Request) {
	var settingsReq SettingsRequest
	if err := json.NewDecoder(request.Body).Decode(&settingsReq); err != nil {
		log.Printf("请求解析失败: %v", err)
		helper.ReturnError(writer, "请求格式错误")
		return
	}
	conf, err := config.GetConfigCached()
	if err != nil {
		log.Printf("获取配置失败: %v", err)
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
		log.Printf("保存配置失败: %v", err)
		helper.ReturnError(writer, "保存配置失败")
		return
	}

	helper.ReturnSuccess(writer, "配置保存成功", nil)
}
