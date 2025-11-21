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
		for i, cdn := range conf.DCDNConfig.DCDN {
			var cdnSelected dcdn.CDN
			switch cdn.Service {
			case "aliyun":
				cdnSelected = &dcdn.Aliyun{}
			case "baiducloud":
				cdnSelected = &dcdn.Baidu{}
			default:
				cdnSelected = &dcdn.Aliyun{}
			}
			cdnSelected.Init(&cdn, &DCDNCaches[i])
			cdnSelected.UpdateOrCreateSources()
			if conf.WebhookEnabled && cdnSelected.ShouldSendWebhook() {
				config.ExecWebhook(&conf.Webhook, string(helper.LogTypeDCDN), cdnSelected.GetServiceName(), cdnSelected.GetServiceStatus())
			}
		}
		dcdn.ForceCompareGlobal = false
	}
	// 开启了 DDNS 功能
}
