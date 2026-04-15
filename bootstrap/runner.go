package bootstrap

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/dcdn"
	"github.com/cxbdasheng/dnet/ddns"
	"github.com/cxbdasheng/dnet/helper"
)

// Runner serializes sync work and keeps cache state local to the instance.
type Runner struct {
	repo       config.Repository
	mu         sync.Mutex
	dcdnCaches []dcdn.Cache
	ddnsCaches []ddns.Cache
}

func NewRunner(repo config.Repository) *Runner {
	return &Runner{repo: repo}
}

func (r *Runner) RunTimer(delay time.Duration) {
	for {
		r.RunOnce()
		time.Sleep(delay)
	}
}

func (r *Runner) RunOnce() {
	conf, err := r.repo.Load()
	if err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	helper.ClearGlobalIPCache()
	r.processDCDNServices(&conf)
	r.processDDNSServices(&conf)
}

func (r *Runner) SyncDCDNOnce() {
	conf, err := r.repo.Load()
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "加载配置失败: %v", err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.processDCDNServices(&conf)
}

func (r *Runner) SyncDDNSOnce() {
	conf, err := r.repo.Load()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "加载配置失败: %v", err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.processDDNSServices(&conf)
}

func (r *Runner) TriggerDCDNSyncAsync() {
	go r.SyncDCDNOnce()
}

func (r *Runner) TriggerDDNSSyncAsync() {
	go r.SyncDDNSOnce()
}

func (r *Runner) processDCDNServices(conf *config.Config) {
	if !conf.DCDNConfig.DCDNEnabled {
		return
	}
	if dcdn.ForceCompareGlobal || len(conf.DCDNConfig.DCDN) != len(r.dcdnCaches) {
		r.dcdnCaches = []dcdn.Cache{}
		for range conf.DCDNConfig.DCDN {
			r.dcdnCaches = append(r.dcdnCaches, dcdn.NewCache())
		}
	}
	configChanged := false
	for i := range conf.DCDNConfig.DCDN {
		if conf.DCDNConfig.DCDN[i].Domain == "" {
			continue
		}
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
		case "upyun":
			cdnSelected = &dcdn.Upyun{}
		default:
			cdnSelected = &dcdn.Aliyun{}
		}
		cdnSelected.Init(&conf.DCDNConfig.DCDN[i], &r.dcdnCaches[i])
		cdnSelected.UpdateOrCreateSources()
		if conf.WebhookEnabled && cdnSelected.ShouldSendWebhook() {
			config.ExecWebhook(&conf.Webhook, string(helper.LogTypeDCDN), cdnSelected.GetServiceName(), cdnSelected.GetServiceStatus())
		}
		if cdnSelected.ConfigChanged() {
			configChanged = true
		}
	}
	if configChanged {
		if err := r.repo.Save(conf); err != nil {
			helper.Error(helper.LogTypeDCDN, "保存配置文件失败 [错误=%v]", err)
		} else {
			helper.Info(helper.LogTypeDCDN, "配置文件已保存（CNAME 已更新）")
		}
	}
	dcdn.ForceCompareGlobal = false
}

func (r *Runner) processDDNSServices(conf *config.Config) {
	if !conf.DDNSConfig.DDNSEnabled {
		return
	}

	totalRecords := 0
	for _, group := range conf.DDNSConfig.DDNS {
		totalRecords += len(group.Records)
	}

	if ddns.ForceCompareGlobal || totalRecords != len(r.ddnsCaches) {
		r.ddnsCaches = make([]ddns.Cache, totalRecords)
		for i := range r.ddnsCaches {
			r.ddnsCaches[i] = ddns.NewCache()
		}
	}

	cacheIndex := 0
	for groupIdx := range conf.DDNSConfig.DDNS {
		group := &conf.DDNSConfig.DDNS[groupIdx]
		if group.Domain == "" {
			continue
		}

		validRecordsCount := 0
		for _, record := range group.Records {
			if record.Value != "" {
				validRecordsCount++
			}
		}
		if validRecordsCount == 0 {
			continue
		}

		groupCaches := make([]*ddns.Cache, validRecordsCount)
		for i := 0; i < validRecordsCount; i++ {
			groupCaches[i] = &r.ddnsCaches[cacheIndex+i]
		}
		cacheIndex += validRecordsCount

		var dnsSelected ddns.DNS
		switch group.Service {
		case ddns.ProviderAliDNS:
			dnsSelected = &ddns.Aliyun{}
		case ddns.ProviderTencent:
			dnsSelected = &ddns.TencentCloud{}
		case ddns.ProviderCloudflare:
			dnsSelected = &ddns.Cloudflare{}
		case ddns.ProviderHuawei:
			dnsSelected = &ddns.Huawei{}
		case ddns.ProviderBaiduCloud:
			dnsSelected = &ddns.Baidu{}
		case ddns.ProviderDnspod:
			dnsSelected = &ddns.Dnspod{}
		case ddns.ProviderNameSilo:
			dnsSelected = &ddns.NameSilo{}
		default:
			helper.Warn(helper.LogTypeDDNS, "不支持的 DNS 提供商: %s，跳过", group.Service)
			continue
		}

		dnsSelected.Init(group, groupCaches)
		results := dnsSelected.UpdateOrCreateRecords()

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
