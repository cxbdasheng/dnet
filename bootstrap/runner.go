package bootstrap

import (
	"fmt"
	"os"
	"strconv"
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
	ddnsCaches map[string]*ddns.Cache
}

func NewRunner(repo config.Repository) *Runner {
	return &Runner{
		repo:       repo,
		ddnsCaches: make(map[string]*ddns.Cache),
	}
}

func (r *Runner) RunTimer(nextInterval func() time.Duration) {
	for {
		r.RunOnce()
		time.Sleep(nextInterval())
	}
}

func (r *Runner) RunOnce() {
	conf, err := r.repo.Load()
	if err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	applyCacheTimesFromConfig(&conf)

	helper.ClearGlobalIPCache()
	r.processDCDNServices(&conf)
	r.processDDNSServices(&conf)
}

// applyCacheTimesFromConfig 将配置中的 CacheTimes 同步到环境变量，
// 供 dcdn.NewCache/ResetTimes、ddns.NewCache/ResetTimes 使用。
// 仅在 >0 时覆盖，等于 0 表示未配置，保留启动时 CLI 传入的值。
func applyCacheTimesFromConfig(conf *config.Config) {
	if conf.DCDNConfig.CacheTimes > 0 {
		os.Setenv(dcdn.CacheTimesENV, strconv.Itoa(conf.DCDNConfig.CacheTimes))
	}
	if conf.DDNSConfig.CacheTimes > 0 {
		os.Setenv(ddns.CacheTimesENV, strconv.Itoa(conf.DDNSConfig.CacheTimes))
	}
}

func (r *Runner) SyncDCDNOnce() {
	conf, err := r.repo.Load()
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "加载配置失败: %v", err)
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	applyCacheTimesFromConfig(&conf)
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
	applyCacheTimesFromConfig(&conf)
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
			config.ExecWebhook(&conf.Webhook, string(helper.LogTypeDCDN), cdnSelected.GetServiceName(), cdnSelected.GetServiceStatus(), formatDCDNChanges(cdnSelected.GetUpdateDetails()))
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

	r.rebuildDDNSCaches(conf)

	for groupIdx := range conf.DDNSConfig.DDNS {
		group := &conf.DDNSConfig.DDNS[groupIdx]
		if group.Domain == "" {
			continue
		}

		groupCaches := r.groupDDNSCaches(group)
		if len(groupCaches) == 0 {
			continue
		}

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
			webhookResults := make([]ddns.RecordResult, 0)

			for _, result := range results {
				if result.ShouldWebhook {
					needWebhook = true
					recordTypes = append(recordTypes, result.RecordType)
					webhookResults = append(webhookResults, result)
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
				config.ExecWebhook(&conf.Webhook, string(helper.LogTypeDDNS), serviceName, status, formatDDNSChanges(webhookResults))
			}
		}
	}

	ddns.ForceCompareGlobal = false
}

func (r *Runner) rebuildDDNSCaches(conf *config.Config) {
	next := make(map[string]*ddns.Cache)

	for groupIdx := range conf.DDNSConfig.DDNS {
		group := &conf.DDNSConfig.DDNS[groupIdx]
		if group.Domain == "" {
			continue
		}

		for recordIdx := range group.Records {
			record := &group.Records[recordIdx]
			if record.Value == "" {
				continue
			}

			key := buildDDNSCacheKey(group, record)
			if !ddns.ForceCompareGlobal {
				if cache, ok := r.ddnsCaches[key]; ok {
					next[key] = cache
					continue
				}
			}

			cache := ddns.NewCache()
			next[key] = &cache
		}
	}

	r.ddnsCaches = next
}

func (r *Runner) groupDDNSCaches(group *config.DNSGroup) []*ddns.Cache {
	groupCaches := make([]*ddns.Cache, 0, len(group.Records))

	for recordIdx := range group.Records {
		record := &group.Records[recordIdx]
		if record.Value == "" {
			continue
		}

		key := buildDDNSCacheKey(group, record)
		cache, ok := r.ddnsCaches[key]
		if !ok {
			newCache := ddns.NewCache()
			cache = &newCache
			r.ddnsCaches[key] = cache
		}
		groupCaches = append(groupCaches, cache)
	}

	return groupCaches
}

func buildDDNSCacheKey(group *config.DNSGroup, record *config.DNSRecord) string {
	return strings.Join([]string{
		group.ID,
		group.Service,
		group.Domain,
		group.TTL,
		record.Type,
		record.IPType,
		record.Value,
		record.Regex,
	}, "\x1f")
}

// formatDCDNChanges 将 DCDN 变更明细格式化为单行字符串供 webhook 模板替换
// 形如: "ipv4url(https://x): 1.1.1.1 -> 2.2.2.2; ipv4interface(eth0): 3.3.3.3 -> 4.4.4.4"
func formatDCDNChanges(details []dcdn.UpdateDetail) string {
	if len(details) == 0 {
		return ""
	}
	parts := make([]string, 0, len(details))
	for _, d := range details {
		parts = append(parts, fmt.Sprintf("%s(%s): %s -> %s", d.SourceType, d.SourceValue, d.OldIP, d.NewIP))
	}
	return strings.Join(parts, "; ")
}

// formatDDNSChanges 将 DDNS 变更明细格式化为单行字符串供 webhook 模板替换
// 仅包含填充了 NewValue 的动态记录；形如: "A: 1.1.1.1 -> 2.2.2.2; AAAA: ::1 -> ::2"
func formatDDNSChanges(results []ddns.RecordResult) string {
	if len(results) == 0 {
		return ""
	}
	parts := make([]string, 0, len(results))
	for _, r := range results {
		if r.NewValue == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s -> %s", r.RecordType, r.OldValue, r.NewValue))
	}
	return strings.Join(parts, "; ")
}
