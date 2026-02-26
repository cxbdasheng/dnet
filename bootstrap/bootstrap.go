package bootstrap

import (
	"time"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/dcdn"
	"github.com/cxbdasheng/dnet/ddns"
	"github.com/cxbdasheng/dnet/helper"
)

var DCDNCaches []dcdn.Cache
var DDNSCaches []ddns.Cache

func RunTimer(delay time.Duration) {
	for {
		RunOnce()
		time.Sleep(delay)
	}
}
func RunOnce() {
	conf, err := config.GetConfigCached()
	if err != nil {
		return
	}
	helper.ClearGlobalIPCache()
	// 处理 DCDN 服务
	ProcessDCDNServices(&conf)
	// 处理 DDNS 服务
	ProcessDDNSServices(&conf)
}

func ProcessDCDNServices(conf *config.Config) {
	// 未开启 DCND 功能，直接返回
	if !conf.DCDNConfig.DCDNEnabled {
		return
	}
	if dcdn.ForceCompareGlobal || len(conf.DCDNConfig.DCDN) != len(DCDNCaches) {
		DCDNCaches = []dcdn.Cache{}
		for range conf.DCDNConfig.DCDN {
			DCDNCaches = append(DCDNCaches, dcdn.NewCache())
		}
	}
	configChanged := false
	for i := range conf.DCDNConfig.DCDN {
		var cdnSelected dcdn.CDN
		switch conf.DCDNConfig.DCDN[i].Service {
		case "aliyun":
			cdnSelected = &dcdn.Aliyun{}
		case "baiducloud":
			cdnSelected = &dcdn.Baidu{}
		case "tencent":
			cdnSelected = &dcdn.Tencent{}
		default:
			cdnSelected = &dcdn.Aliyun{}
		}
		cdnSelected.Init(&conf.DCDNConfig.DCDN[i], &DCDNCaches[i])
		cdnSelected.UpdateOrCreateSources()
		if conf.WebhookEnabled && cdnSelected.ShouldSendWebhook() {
			config.ExecWebhook(&conf.Webhook, string(helper.LogTypeDCDN), cdnSelected.GetServiceName(), cdnSelected.GetServiceStatus())
		}
		// 检查配置是否发生变化
		if cdnSelected.ConfigChanged() {
			configChanged = true
		}
	}
	// 如果有配置变化，保存到文件
	if configChanged {
		if err := conf.SaveConfig(); err != nil {
			helper.Error(helper.LogTypeDCDN, "保存配置文件失败 [错误=%v]", err)
		} else {
			helper.Info(helper.LogTypeDCDN, "配置文件已保存（CNAME 已更新）")
		}
	}
	dcdn.ForceCompareGlobal = false
}
func ProcessDDNSServices(conf *config.Config) {
	// 未开启 DDNS 功能，直接返回
	if !conf.DDNSConfig.DDNSEnabled {
		return
	}

	// 初始化或重置缓存
	if ddns.ForceCompareGlobal || len(conf.DDNSConfig.DDNS) != len(DDNSCaches) {
		DDNSCaches = []ddns.Cache{}
		for range conf.DDNSConfig.DDNS {
			DDNSCaches = append(DDNSCaches, ddns.NewCache())
		}
	}

	for i := range conf.DDNSConfig.DDNS {
		var dnsSelected ddns.DNS

		// 根据服务提供商选择对应的实现
		switch conf.DDNSConfig.DDNS[i].Service {
		case ddns.ProviderAliDNS:
			dnsSelected = &ddns.Aliyun{}
		// 未来可以添加其他 DNS 提供商
		// case ddns.ProviderTencent:
		//     dnsSelected = &ddns.Tencent{}
		// case ddns.ProviderBaiduCloud:
		//     dnsSelected = &ddns.Baidu{}
		default:
			helper.Warn(helper.LogTypeDDNS, "不支持的 DNS 提供商: %s，跳过", conf.DDNSConfig.DDNS[i].Service)
			continue
		}

		// 初始化 DNS 服务
		dnsSelected.Init(&conf.DDNSConfig.DDNS[i], &DDNSCaches[i])

		// 更新或创建 DNS 记录
		dnsSelected.UpdateOrCreateRecord()

		// 发送 Webhook 通知
		if conf.WebhookEnabled && dnsSelected.ShouldSendWebhook() {
			config.ExecWebhook(&conf.Webhook, string(helper.LogTypeDDNS), dnsSelected.GetServiceName(), dnsSelected.GetServiceStatus())
		}
	}

	ddns.ForceCompareGlobal = false
}
