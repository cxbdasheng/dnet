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

//go:embed dcdn.html
var DCDNEmbedFile embed.FS

func (s *Server) DCDN(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		s.handleDCDNGet(writer, request)
	case http.MethodPost:
		s.handleDCDNPost(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}
func (s *Server) handleDCDNGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(DCDNEmbedFile, "dcdn.html")
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "解析模板失败: %v", err)
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	conf, err := s.configRepo.Load()
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
		IPv4     []helper.NetInterface
		IPv6     []helper.NetInterface
	}{
		DCDNConf: template.JS(config.GetDCDNConfigJSON(conf.DCDNConfig)),
		IPv4:     ipv4,
		IPv6:     ipv6,
	})
	if err != nil {
		// 检查是否是客户端主动关闭连接（broken pipe 或 connection reset）
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "broken pipe") || strings.Contains(errStr, "connection reset") {
			// 客户端主动关闭连接（如快速刷新页面），这是正常情况，只记录调试信息
			helper.Debug(helper.LogTypeDCDN, "客户端关闭连接: %v", err)
		} else {
			// 其他错误需要记录
			helper.Error(helper.LogTypeDCDN, "渲染模板失败: %v", err)
		}
	}
}

func (s *Server) handleDCDNPost(writer http.ResponseWriter, request *http.Request) {
	var configData config.DCDNConfig
	if err := json.NewDecoder(request.Body).Decode(&configData); err != nil {
		helper.Error(helper.LogTypeDCDN, "请求解析失败: %v", err)
		helper.ReturnError(writer, "请求格式错误")
		return
	}

	conf, err := s.configRepo.Load()
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "获取配置失败: %v", err)
		helper.ReturnError(writer, "获取配置失败")
		return
	}

	// 恢复脱敏字段的原始值（如果前端发送的是脱敏数据）
	configData = config.RestoreSensitiveFields(configData, conf.DCDNConfig)

	// 更新 DCDN 配置
	conf.DCDNConfig = configData

	// 保存用户提交的配置
	if err := s.configRepo.Save(&conf); err != nil {
		helper.Error(helper.LogTypeDCDN, "保存配置失败: %v", err)
		helper.ReturnError(writer, "保存配置失败")
		return
	}

	s.syncer.TriggerDCDNSyncAsync()

	helper.ReturnSuccess(writer, "配置保存成功", nil)
}

// DCDNConfigAPI 返回 DCDN 配置的 JSON 数据
func (s *Server) DCDNConfigAPI(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}

	conf, err := s.configRepo.Load()
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "获取配置失败: %v", err)
		helper.ReturnError(writer, "获取配置失败")
		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.Write([]byte(config.GetDCDNConfigJSON(conf.DCDNConfig)))
}
