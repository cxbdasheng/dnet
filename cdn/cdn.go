package cdn

import (
	"fmt"
	"time"

	"github.com/cxbdasheng/dnet/config"
)

type CDN interface {
}

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
	// 未开启 DCND 功能
	if !conf.DCDNConfig.DCDNEnabled {
		return
	}

	for _, cdn := range conf.DCDNConfig.DCDN {
		// 如果 IPv4 和 IPv6 都未启用，则属于无效的配置
		if !cdn.IPv4Enable && !cdn.IPv6Enable {
			continue
		}
		var cdnSelected CDN
		switch cdn.Service {
		case "aliyun":
			cdnSelected = &Aliyun{}
		case "baidu":
			cdnSelected = &Baidu{}
		default:
			cdnSelected = &Aliyun{}
		}
		fmt.Println(cdnSelected)
	}
}
