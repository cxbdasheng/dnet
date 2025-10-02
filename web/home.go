package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

const VersionEnv = "DNET_VERSION"

//go:embed home.html
var homeEmbedFile embed.FS

func Home(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case "GET":
		handleHomeGet(writer, request)
	case "POST":
		handleHomePost(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}
func handleHomePost(writer http.ResponseWriter, request *http.Request) {
	var configData config.DCDNConfig
	if err := json.NewDecoder(request.Body).Decode(&configData); err != nil {
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

	// 更新 DCDN 配置
	conf.DCDNConfig = configData

	// 保存配置
	if err := conf.SaveConfig(); err != nil {
		log.Printf("保存配置失败: %v", err)
		helper.ReturnError(writer, "保存配置失败")
		return
	}

	helper.ReturnSuccess(writer, "配置保存成功", nil)
}
func handleHomeGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(homeEmbedFile, "home.html")
	if err != nil {
		log.Printf("解析模板失败: %v", err)
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	conf, err := config.GetConfigCached()
	if err != nil {
		log.Printf("获取配置失败: %v", err)
		// 使用默认配置
		conf = config.Config{
			Settings: config.Settings{
				NotAllowWanAccess: true,
			},
		}
	}

	ipv4, ipv6, _ := config.GetNetInterface()

	err = tmpl.Execute(writer, struct {
		DCDNConf template.JS
		Version  string
		IPv4     []config.NetInterface
		IPv6     []config.NetInterface
	}{
		DCDNConf: template.JS(config.GetDCDNConfigJSON(conf.DCDNConfig)),
		Version:  os.Getenv(VersionEnv),
		IPv4:     ipv4,
		IPv6:     ipv6,
	})
	if err != nil {
		log.Printf("渲染模板失败: %v", err)
	}
}
