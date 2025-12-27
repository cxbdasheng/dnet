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
	// 开启了 DCND 功能
	if conf.DCDNConfig.DCDNEnabled {
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
	// 开启了 DDNS 功能
}
