package ddns

import (
	"os"
	"strconv"
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
	Init(dnsConfig *config.DNS, cache *Cache)
	UpdateOrCreateRecord() bool
	ShouldSendWebhook() bool
	GetServiceStatus() string
	GetServiceName() string
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
