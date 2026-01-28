package web

import (
	"embed"
	"html/template"
	"net/http"

	"github.com/cxbdasheng/dnet/helper"
)

//go:embed ddns.html
var DDNSEmbedFile embed.FS

func DDNS(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		handleDDNSGet(writer, request)
	case http.MethodPost:
		handleDDNSPost(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}
func handleDDNSGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(DDNSEmbedFile, "ddns.html")
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "解析模板失败: %v", err)
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	//
	//conf, err := config.GetConfigCached()
	//if err != nil {
	//	helper.Warn(helper.LogTypeDCDN, "获取配置失败: %v", err)
	//	// 使用默认配置
	//	conf = config.Config{
	//		Settings: config.Settings{
	//			NotAllowWanAccess: true,
	//		},
	//	}
	//}
	err = tmpl.Execute(writer, struct {
	}{})
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "渲染模板失败: %v", err)
	}
}
func handleDDNSPost(writer http.ResponseWriter, request *http.Request) {
	return
}
