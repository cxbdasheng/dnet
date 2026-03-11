package bootstrap

import (
	"fmt"
	"strings"
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

// ProcessDCDNServices 处理 DCDN 服务
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
		case "cloudflare":
			cdnSelected = &dcdn.Cloudflare{}
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

// ProcessDDNSServices 处理 DDNS 服务
func ProcessDDNSServices(conf *config.Config) {
	// 未开启 DDNS 功能，直接返回
	if !conf.DDNSConfig.DDNSEnabled {
		return
	}

	// 计算总记录数（所有配置组的所有记录）
	totalRecords := 0
	for _, group := range conf.DDNSConfig.DDNS {
		totalRecords += len(group.Records)
	}

	// 初始化或重置缓存
	if ddns.ForceCompareGlobal || totalRecords != len(DDNSCaches) {
		DDNSCaches = make([]ddns.Cache, totalRecords)
		for i := range DDNSCaches {
			DDNSCaches[i] = ddns.NewCache()
		}
	}

	cacheIndex := 0
	for groupIdx := range conf.DDNSConfig.DDNS {
		group := &conf.DDNSConfig.DDNS[groupIdx]

		// 过滤空配置：跳过域名为空的配置组
		if group.Domain == "" {
			continue
		}

		// 过滤空记录（跳过 Value 为空的记录）
		validRecordsCount := 0
		for _, record := range group.Records {
			if record.Value != "" {
				validRecordsCount++
			}
		}

		// 如果没有有效记录，跳过整个配置组
		if validRecordsCount == 0 {
			continue
		}

		// 为该配置组分配缓存（每条有效记录一个缓存）
		groupCaches := make([]*ddns.Cache, validRecordsCount)
		for i := 0; i < validRecordsCount; i++ {
			groupCaches[i] = &DDNSCaches[cacheIndex+i]
		}
		cacheIndex += validRecordsCount

		var dnsSelected ddns.DNS

		// 根据服务提供商选择对应的实现
		switch group.Service {
		case ddns.ProviderAliDNS:
			dnsSelected = &ddns.Aliyun{}
		case ddns.ProviderTencent:
			dnsSelected = &ddns.TencentCloud{}
		case ddns.ProviderCloudflare:
			dnsSelected = &ddns.Cloudflare{}
		case ddns.ProviderHuawei:
			dnsSelected = &ddns.Huawei{}
		//case ddns.ProviderBaiduCloud:
		//	dnsSelected = &ddns.Baidu{}
		default:
			helper.Warn(helper.LogTypeDDNS, "不支持的 DNS 提供商: %s，跳过", group.Service)
			continue
		}

		// 初始化 DNS 服务（传入整个配置组和对应的缓存数组）
		dnsSelected.Init(group, groupCaches)

		// 批量更新或创建 DNS 记录（一次查询，处理所有记录）
		results := dnsSelected.UpdateOrCreateRecords()

		// 统计需要发送 Webhook 的记录，合并为一条通知
		if conf.WebhookEnabled {
			needWebhook := false
			successCount := 0
			failedCount := 0
			recordTypes := make([]string, 0)

			for _, result := range results {
				if result.ShouldWebhook {
					needWebhook = true
					recordTypes = append(recordTypes, result.RecordType)
					if result.Status == ddns.UpdatedSuccess {
						successCount++
					} else {
						failedCount++
					}
				}
			}

			// 如果有需要通知的记录，发送一条合并的 Webhook
			if needWebhook {
				var status string
				if failedCount > 0 {
					if successCount == 0 {
						status = "失败"
					} else {
						status = fmt.Sprintf("部分失败 (成功: %d, 失败: %d)", successCount, failedCount)
					}
				} else {
					status = "成功"
				}

				serviceName := fmt.Sprintf("%s [%s]", dnsSelected.GetServiceName(), strings.Join(recordTypes, ", "))
				config.ExecWebhook(&conf.Webhook, string(helper.LogTypeDDNS), serviceName, status)
			}
		}
	}

	ddns.ForceCompareGlobal = false
}
