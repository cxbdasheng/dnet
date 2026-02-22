package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/cxbdasheng/dnet/config"
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

	conf, err := config.GetConfigCached()
	if err != nil {
		helper.Warn(helper.LogTypeDDNS, "获取配置失败: %v", err)
		// 使用默认配置
		conf = config.Config{
			Settings: config.Settings{
				NotAllowWanAccess: true,
			},
		}
	}

	// 设置响应头
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	ipv4, ipv6, _ := helper.GetNetInterface()
	err = tmpl.Execute(writer, struct {
		DDNSConf template.JS
		IPv4     []helper.NetInterface
		IPv6     []helper.NetInterface
	}{
		DDNSConf: template.JS(config.GetDDNSConfigJSON(conf.DDNSConfig)),
		IPv4:     ipv4,
		IPv6:     ipv6,
	})
	if err != nil {
		// 检查是否是客户端主动关闭连接（broken pipe 或 connection reset）
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "broken pipe") || strings.Contains(errStr, "connection reset") {
			// 客户端主动关闭连接（如快速刷新页面），这是正常情况，只记录调试信息
			helper.Debug(helper.LogTypeDDNS, "客户端关闭连接: %v", err)
		} else {
			// 其他错误需要记录
			helper.Error(helper.LogTypeDDNS, "渲染模板失败: %v", err)
		}
	}
}
func handleDDNSPost(writer http.ResponseWriter, request *http.Request) {
	var configData config.DDNSConfig
	if err := json.NewDecoder(request.Body).Decode(&configData); err != nil {
		helper.Error(helper.LogTypeDDNS, "请求解析失败: %v", err)
		helper.ReturnError(writer, "请求格式错误")
		return
	}

	conf, err := config.GetConfigCached()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "获取配置失败: %v", err)
		helper.ReturnError(writer, "获取配置失败")
		return
	}

	// 恢复脱敏字段的原始值（如果前端发送的是脱敏数据）
	configData = config.RestoreSensitiveFieldsForDDNS(configData, conf.DDNSConfig)

	// 更新 DDNS 配置
	conf.DDNSConfig = configData

	// 保存用户提交的配置
	if err := conf.SaveConfig(); err != nil {
		helper.Error(helper.LogTypeDDNS, "保存配置失败: %v", err)
		helper.ReturnError(writer, "保存配置失败")
		return
	}

	helper.ReturnSuccess(writer, "配置保存成功", nil)
}
