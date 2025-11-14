package dcdn

import (
	"os"
	"strconv"
	"sync"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

const CacheTimesENV = "DCDN_CACHE_TIMES"

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

type Cache struct {
	Times         int               // 剩余次数
	TimesFailedIP int               // 获取ip失败的次数
	DynamicIPs    map[string]string // 动态 IP 缓存: key=source唯一标识(type:value), value=获取到的IP
	mu            sync.RWMutex      // 保护 DynamicIPs 的读写锁
	HasRun        bool              //
}

type CDN interface {
	Init(cdnConfig *config.CDN, cache *Cache)
	UpdateOrCreateSources() bool
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

// IsDynamicType 判断 source 类型是否为动态类型
func IsDynamicType(sourceType string) bool {
	return dynamicTypes[sourceType]
}

// CheckIPChanged 检查动态 IP 是否发生变化
func (c *Cache) CheckIPChanged(sourceKey string, currentIP string) (bool, string) {
	c.mu.RLock()
	cachedIP, exists := c.DynamicIPs[sourceKey]
	c.mu.RUnlock()

	// 如果缓存中不存在或IP发生变化
	if !exists || cachedIP != currentIP {
		return true, cachedIP
	}

	return false, cachedIP
}

// UpdateDynamicIP 更新缓存中的动态 IP
func (c *Cache) UpdateDynamicIP(sourceKey string, newIP string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.DynamicIPs == nil {
		c.DynamicIPs = make(map[string]string)
	}
	c.DynamicIPs[sourceKey] = newIP
}

// ResetTimes 重置计数器
func (c *Cache) ResetTimes() {
	times, err := strconv.Atoi(os.Getenv(CacheTimesENV))
	if err != nil {
		times = 5
	}
	c.Times = times
}

// GetDynamicIPs 获取动态 IP 缓存的副本（线程安全）
func (c *Cache) GetDynamicIPs() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 返回 map 的副本
	result := make(map[string]string, len(c.DynamicIPs))
	for k, v := range c.DynamicIPs {
		result[k] = v
	}
	return result
}
