package ddns

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

const CacheTimesENV = "DDNS_CACHE_TIMES"

// DNS 记录类型常量
const (
	RecordTypeA     = "A"     // IPv4 地址记录
	RecordTypeAAAA  = "AAAA"  // IPv6 地址记录
	RecordTypeCNAME = "CNAME" // 别名记录
	RecordTypeTXT   = "TXT"   // 文本记录
)

// DNS 服务提供商常量
const (
	ProviderAliDNS     = "alidns"     // 阿里云 DNS
	ProviderTencent    = "tencent"    // 腾讯云 DNS
	ProviderBaiduCloud = "baiducloud" // 百度云 DNS
	ProviderCloudflare = "cloudflare" // Cloudflare DNS
	ProviderHuawei     = "huawei"     // 华为云 DNS
)

var dynamicTypes = map[string]bool{
	helper.DynamicIPv4URL:       true,
	helper.DynamicIPv4Interface: true,
	helper.DynamicIPv4Command:   true,
	helper.DynamicIPv6URL:       true,
	helper.DynamicIPv6Interface: true,
	helper.DynamicIPv6Command:   true,
}

var ForceCompareGlobal = true

type statusType string

const (
	InitFailed      statusType = "初始化失败"
	InitSuccess                = "初始化成功"
	InitGetIPFailed            = "IP 获取失败"
	// UpdatedNothing 未改变
	UpdatedNothing = "未改变"
	// UpdatedFailed 更新失败
	UpdatedFailed = "失败"
	// UpdatedSuccess 更新成功
	UpdatedSuccess = "成功"
)

// Cache DDNS 缓存结构
type Cache struct {
	Times       int               // 剩余次数
	TimesFailed int               // 获取 IP 失败的次数
	DynamicIPs  map[string]string // 动态 IP 缓存: key=source 唯一标识(type:value), value=获取到的 IP
	mu          sync.RWMutex      // 保护 DynamicIPs 的读写锁
	HasRun      bool              // 是否已经运行过
}

// DNS DNS 提供商接口
type DNS interface {
	Init(group *config.DNSGroup, caches []*Cache)
	UpdateOrCreateRecords() []RecordResult
	GetServiceName() string
}

// RecordResult 单条记录的处理结果
type RecordResult struct {
	RecordType    string     // 记录类型 (A, AAAA, CNAME, TXT)
	Status        statusType // 处理状态
	ShouldWebhook bool       // 是否需要发送 Webhook
	ErrorMessage  string     // 错误信息（如果有）
}

// NewCache 创建新的缓存实例
func NewCache() Cache {
	times, err := strconv.Atoi(os.Getenv(CacheTimesENV))
	if err != nil {
		times = 5
	}
	return Cache{
		Times:      times,
		DynamicIPs: make(map[string]string),
	}
}

// IsDynamicType 判断类型是否为动态类型
func IsDynamicType(ipType string) bool {
	return dynamicTypes[ipType]
}

// CheckIPChanged 检查动态 IP 是否发生变化
func (c *Cache) CheckIPChanged(cacheKey string, currentIP string) (bool, string) {
	c.mu.RLock()
	cachedIP, exists := c.DynamicIPs[cacheKey]
	c.mu.RUnlock()

	// 如果缓存中不存在或 IP 发生变化
	if !exists || cachedIP != currentIP {
		return true, cachedIP
	}

	return false, cachedIP
}

// UpdateDynamicIP 更新动态 IP 缓存
func (c *Cache) UpdateDynamicIP(cacheKey string, ip string) {
	c.mu.Lock()
	c.DynamicIPs[cacheKey] = ip
	c.mu.Unlock()
}

// GetDynamicIP 获取缓存的动态 IP
func (c *Cache) GetDynamicIP(cacheKey string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ip, exists := c.DynamicIPs[cacheKey]
	return ip, exists
}

// ResetTimes 重置计数器
func (c *Cache) ResetTimes() {
	times, err := strconv.Atoi(os.Getenv(CacheTimesENV))
	if err != nil {
		times = 5
	}
	c.Times = times
}

// BaseDNSProvider 公共基础结构体，消除各提供商重复的字段和方法
type BaseDNSProvider struct {
	Group  *config.DNSGroup
	Caches []*Cache
}

// GetServiceName 获取服务名称
func (b *BaseDNSProvider) GetServiceName() string {
	if b.Group == nil {
		return ""
	}
	if b.Group.Name != "" {
		return b.Group.Name
	}
	return b.Group.Domain
}

// initConfig 验证并设置基础配置，返回 false 表示配置不完整
func (b *BaseDNSProvider) initConfig(group *config.DNSGroup, caches []*Cache) bool {
	b.Group = group
	b.Caches = caches
	if b.Group.Domain == "" || b.Group.AccessKey == "" || b.Group.AccessSecret == "" {
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", b.GetServiceName())
		return false
	}
	return true
}

// Init 标准初始化（适用于 aliyun、tencent、baidu 等无额外步骤的提供商）
func (b *BaseDNSProvider) Init(group *config.DNSGroup, caches []*Cache) {
	if !b.initConfig(group, caches) {
		return
	}
	if len(b.Caches) > 0 && !b.Caches[0].HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功，共 %d 条记录", b.GetServiceName(), len(b.Caches))
	}
}

// getRootDomain 从完整域名提取根域名
// 例如: www.sub.example.com -> example.com
func getRootDomain(domain string) string {
	if strings.HasPrefix(domain, "*.") {
		domain = domain[2:]
	}
	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return domain
	}
	return parts[len(parts)-2] + "." + parts[len(parts)-1]
}

// getHostRecord 从完整域名提取主机记录
// 例如: www.sub.example.com -> www.sub
// 例如: example.com -> @
// 例如: *.example.com -> *
func getHostRecord(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return "@"
	}
	return strings.Join(parts[:len(parts)-2], ".")
}

// filterValidRecords 过滤 Value 不为空的有效记录
func filterValidRecords(group *config.DNSGroup, caches []*Cache) []validRecord {
	valid := make([]validRecord, 0, len(group.Records))
	cacheIdx := 0
	for i := range group.Records {
		if group.Records[i].Value != "" {
			valid = append(valid, validRecord{
				record: &group.Records[i],
				cache:  caches[cacheIdx],
			})
			cacheIdx++
		}
	}
	return valid
}

// createErrorResults 为所有有效记录批量生成错误结果
func createErrorResults(validRecords []validRecord, status statusType, errMsg string) []RecordResult {
	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, RecordResult{
			RecordType:    vr.record.Type,
			Status:        status,
			ShouldWebhook: false,
			ErrorMessage:  errMsg,
		})
	}
	return results
}

// shouldSendWebhook 判断单条记录是否需要发送 Webhook
func shouldSendWebhook(cache *Cache, status statusType) bool {
	if status == UpdatedSuccess {
		cache.TimesFailed = 0
		return true
	}
	if status == UpdatedFailed || status == InitGetIPFailed {
		cache.TimesFailed++
		return cache.TimesFailed >= 3
	}
	return false
}

// getCacheKey 获取缓存键（支持正则表达式）
func getCacheKey(ipType, value, regex string) string {
	if ipType == helper.DynamicIPv6Interface && regex != "" {
		return helper.GetIPCacheKeyWithRegex(ipType, value, regex)
	}
	return helper.GetIPCacheKey(ipType, value)
}

// getCurrentValue 步骤1：获取当前记录值，返回 (值, 初始化后的result, 是否成功)
func getCurrentValue(serviceName string, record *config.DNSRecord, cache *Cache) (string, RecordResult, bool) {
	result := RecordResult{
		RecordType:    record.Type,
		Status:        UpdatedNothing,
		ShouldWebhook: false,
	}
	switch record.Type {
	case RecordTypeA, RecordTypeAAAA:
		if IsDynamicType(record.IPType) {
			var currentValue string
			var ok bool
			if record.IPType == helper.DynamicIPv6Interface && record.Regex != "" {
				currentValue, ok = helper.GetOrSetDynamicIPWithCacheAndRegex(record.IPType, record.Value, record.Regex)
			} else {
				currentValue, ok = helper.GetOrSetDynamicIPWithCache(record.IPType, record.Value)
			}
			if !ok {
				result.Status = InitGetIPFailed
				result.ShouldWebhook = shouldSendWebhook(cache, InitGetIPFailed)
				result.ErrorMessage = "获取 IP 失败"
				helper.Error(helper.LogTypeDDNS, "[%s] [%s] 获取 IP 失败", serviceName, record.Type)
				return "", result, false
			}
			return currentValue, result, true
		}
		return record.Value, result, true
	case RecordTypeCNAME, RecordTypeTXT:
		return record.Value, result, true
	default:
		result.Status = UpdatedFailed
		result.ErrorMessage = "不支持的记录类型"
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型: %s", serviceName, record.Type)
		return "", result, false
	}
}

// checkDynamicCache 步骤2：检查动态 IP 缓存，返回 (是否跳过, 跳过时的结果)
func checkDynamicCache(serviceName string, record *config.DNSRecord, cache *Cache, currentValue string) (bool, RecordResult) {
	if !IsDynamicType(record.IPType) {
		return false, RecordResult{}
	}
	cacheKey := getCacheKey(record.IPType, record.Value, record.Regex)
	valueChanged, oldValue := cache.CheckIPChanged(cacheKey, currentValue)
	forceUpdate := cache.Times <= 0

	if !valueChanged && cache.HasRun && !ForceCompareGlobal && !forceUpdate {
		cache.Times--
		helper.Debug(helper.LogTypeDDNS, "[%s] [%s] 值未变化，跳过更新 [当前=%s, 剩余次数=%d]", serviceName, record.Type, currentValue, cache.Times)
		return true, RecordResult{RecordType: record.Type, Status: UpdatedNothing}
	}

	if forceUpdate && !valueChanged {
		helper.Info(helper.LogTypeDDNS, "[%s] [%s] 达到强制更新阈值，执行更新 [值=%s]", serviceName, record.Type, currentValue)
	} else if valueChanged {
		helper.Info(helper.LogTypeDDNS, "[%s] [%s] 检测到值变化 [旧值=%s, 新值=%s]", serviceName, record.Type, oldValue, currentValue)
	}
	return false, RecordResult{}
}

// finalizeSuccess 步骤4：更新缓存并设置成功状态
func finalizeSuccess(serviceName string, record *config.DNSRecord, cache *Cache, currentValue string, result *RecordResult) {
	if IsDynamicType(record.IPType) {
		cacheKey := getCacheKey(record.IPType, record.Value, record.Regex)
		cache.UpdateDynamicIP(cacheKey, currentValue)
	}
	cache.HasRun = true
	cache.TimesFailed = 0
	cache.ResetTimes()
	result.Status = UpdatedSuccess
	result.ShouldWebhook = shouldSendWebhook(cache, UpdatedSuccess)
	helper.Info(helper.LogTypeDDNS, "[%s] [%s] DNS 记录更新成功 [值=%s]", serviceName, record.Type, currentValue)
}
