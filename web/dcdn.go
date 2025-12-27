package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"os"

	"github.com/cxbdasheng/dnet/bootstrap"
	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/dcdn"
	"github.com/cxbdasheng/dnet/helper"
)

//go:embed dcdn.html
var DCDNEmbedFile embed.FS

func DCDN(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		handleDCDNGet(writer, request)
	case http.MethodPost:
		handleDCDNPost(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}
func handleDCDNGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(DCDNEmbedFile, "dcdn.html")
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "解析模板失败: %v", err)
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	conf, err := config.GetConfigCached()
	if err != nil {
		helper.Warn(helper.LogTypeDCDN, "获取配置失败: %v", err)
		// 使用默认配置
		conf = config.Config{
			Settings: config.Settings{
				NotAllowWanAccess: true,
			},
		}
	}

	ipv4, ipv6, _ := helper.GetNetInterface()

	err = tmpl.Execute(writer, struct {
		DCDNConf template.JS
		Version  string
		IPv4     []helper.NetInterface
		IPv6     []helper.NetInterface
	}{
		DCDNConf: template.JS(config.GetDCDNConfigJSON(conf.DCDNConfig)),
		Version:  os.Getenv(VersionEnv),
		IPv4:     ipv4,
		IPv6:     ipv6,
	})
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "渲染模板失败: %v", err)
	}
}

func handleDCDNPost(writer http.ResponseWriter, request *http.Request) {
	var configData config.DCDNConfig
	if err := json.NewDecoder(request.Body).Decode(&configData); err != nil {
		helper.Error(helper.LogTypeDCDN, "请求解析失败: %v", err)
		helper.ReturnError(writer, "请求格式错误")
		return
	}

	conf, err := config.GetConfigCached()
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "获取配置失败: %v", err)
		helper.ReturnError(writer, "获取配置失败")
		return
	}

	// 恢复脱敏字段的原始值（如果前端发送的是脱敏数据）
	configData = config.RestoreSensitiveFields(configData, conf.DCDNConfig)

	// 更新 DCDN 配置
	conf.DCDNConfig = configData
	dcdn.ForceCompareGlobal = true
	go bootstrap.RunOnce()
	// 保存配置
	if err := conf.SaveConfig(); err != nil {
		helper.Error(helper.LogTypeDCDN, "保存配置失败: %v", err)
		helper.ReturnError(writer, "保存配置失败")
		return
	}

	helper.ReturnSuccess(writer, "配置保存成功", nil)
}
