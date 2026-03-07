package bootstrap

import (
	"time"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/dcdn"
	"github.com/cxbdasheng/dnet/helper"
)

var DCDNCaches []dcdn.Cache

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
		// 过滤空配置：跳过域名为空或没有有效源站的配置
		if conf.DCDNConfig.DCDN[i].Domain == "" {
			continue
		}
		// 检查是否有至少一个有效的源站
		hasValidSource := false
		for _, source := range conf.DCDNConfig.DCDN[i].Sources {
			if source.Value != "" {
				hasValidSource = true
				break
			}
		}
		if !hasValidSource {
			continue
		}

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
