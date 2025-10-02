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

//go:embed webhook.html
var webhookEmbedFile embed.FS

func Mock(writer http.ResponseWriter, request *http.Request) {
	if request.Method == "POST" {
		helper.ReturnSuccess(writer, "测试成功", nil)
		return
	}
	helper.ReturnError(writer, "不支持的请求方法")
	return
}

func Webhook(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case "GET":
		handleWebhookGet(writer, request)
	case "POST":
		handleWebhookPost(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}
func handleWebhookPost(writer http.ResponseWriter, request *http.Request) {
	var webhook config.Webhook
	if err := json.NewDecoder(request.Body).Decode(&webhook); err != nil {
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
	conf.Webhook = webhook
	// 保存配置
	if err := conf.SaveConfig(); err != nil {
		log.Printf("保存配置失败: %v", err)
		helper.ReturnError(writer, "保存配置失败")
		return
	}

	helper.ReturnSuccess(writer, "配置保存成功", nil)
}
func handleWebhookGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(webhookEmbedFile, "webhook.html")
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
		config.Webhook
	}{conf.Webhook})
	if err != nil {
		fmt.Println("Error happened..")
		fmt.Println(err)
	}
}
