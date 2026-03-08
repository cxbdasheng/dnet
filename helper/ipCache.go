package helper

import "sync"

const (
	DynamicIPv4URL       = "dynamic_ipv4_url"
	DynamicIPv4Interface = "dynamic_ipv4_interface"
	DynamicIPv4Command   = "dynamic_ipv4_command"
	DynamicIPv6URL       = "dynamic_ipv6_url"
	DynamicIPv6Interface = "dynamic_ipv6_interface"
	DynamicIPv6Command   = "dynamic_ipv6_command"
)

// globalIPCache 全局 IP 缓存结构
type globalIPCache struct {
	mu    sync.RWMutex
	cache map[string]string
}

// GlobalIPCache 全局 IP 缓存实例，用于在一次运行周期内避免重复获取相同的动态 IP
var GlobalIPCache = &globalIPCache{
	cache: make(map[string]string),
}

// Get 从缓存中获取 IP（线程安全）
func (g *globalIPCache) Get(key string) (string, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	ip, exists := g.cache[key]
	return ip, exists
}

// Set 设置缓存中的 IP（线程安全）
func (g *globalIPCache) Set(key, ip string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cache[key] = ip
}

// Clear 清空缓存（线程安全）
func (g *globalIPCache) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cache = make(map[string]string)
}

// GetIPCacheKey 的唯一标识
func GetIPCacheKey(sourceType, sourceValue string) string {
	if sourceType == DynamicIPv4Interface || sourceType == DynamicIPv6Interface {
		return sourceType + ":" + sourceValue
	}
	return sourceValue
}

// GetIPCacheKeyWithRegex 的唯一标识（包含正则表达式）
func GetIPCacheKeyWithRegex(sourceType, sourceValue, regex string) string {
	if sourceType == DynamicIPv4Interface || sourceType == DynamicIPv6Interface {
		if regex != "" {
			return sourceType + ":" + sourceValue + ":" + regex
		}
		return sourceType + ":" + sourceValue
	}
	return sourceValue
}

// ClearGlobalIPCache 清空全局 IP 缓存
func ClearGlobalIPCache() {
	GlobalIPCache.Clear()
}

// GetDynamicIPWithCache 获取动态 IP，使用全局缓存避免重复获取
func GetDynamicIPWithCache(sourceType, sourceValue string) (string, bool) {
	sourceKey := GetIPCacheKey(sourceType, sourceValue)
	return GlobalIPCache.Get(sourceKey)
}

// SetGlobalIPCache 设置全局 IP 缓存
func SetGlobalIPCache(sourceType, sourceValue, ip string) {
	sourceKey := GetIPCacheKey(sourceType, sourceValue)
	GlobalIPCache.Set(sourceKey, ip)
}

// GetOrSetDynamicIPWithCache 获取或设置动态 IP，使用全局缓存避免重复获取
func GetOrSetDynamicIPWithCache(sourceType, sourceValue string) (string, bool) {
	sourceKey := GetIPCacheKey(sourceType, sourceValue)
	addr, ok := GlobalIPCache.Get(sourceKey)
	if ok {
		return addr, ok
	}
	switch sourceType {
	case DynamicIPv4URL:
		addr = GetAddrFromUrl(sourceValue, IPv4)
	case DynamicIPv6URL:
		addr = GetAddrFromUrl(sourceValue, IPv6)
	case DynamicIPv4Interface:
		addr = GetAddrFromInterface(sourceValue, IPv4)
	case DynamicIPv6Interface:
		addr = GetAddrFromInterface(sourceValue, IPv6)
	case DynamicIPv4Command:
		addr = GetAddrFromCmd(sourceValue, IPv4)
	case DynamicIPv6Command:
		addr = GetAddrFromCmd(sourceValue, IPv6)
	default:
		return "", false
	}
	if addr != "" {
		SetGlobalIPCache(sourceType, sourceValue, addr)
		return addr, true
	}
	return "", false
}

// GetOrSetDynamicIPWithCacheAndRegex 获取或设置动态 IP，使用全局缓存避免重复获取（支持正则表达式）
func GetOrSetDynamicIPWithCacheAndRegex(sourceType, sourceValue, regex string) (string, bool) {
	sourceKey := GetIPCacheKeyWithRegex(sourceType, sourceValue, regex)
	addr, ok := GlobalIPCache.Get(sourceKey)
	if ok {
		return addr, ok
	}
	switch sourceType {
	case DynamicIPv4URL:
		addr = GetAddrFromUrl(sourceValue, IPv4)
	case DynamicIPv6URL:
		addr = GetAddrFromUrl(sourceValue, IPv6)
	case DynamicIPv4Interface:
		addr = GetAddrFromInterfaceWithRegex(sourceValue, IPv4, regex)
	case DynamicIPv6Interface:
		addr = GetAddrFromInterfaceWithRegex(sourceValue, IPv6, regex)
	case DynamicIPv4Command:
		addr = GetAddrFromCmd(sourceValue, IPv4)
	case DynamicIPv6Command:
		addr = GetAddrFromCmd(sourceValue, IPv6)
	default:
		return "", false
	}
	if addr != "" {
		GlobalIPCache.Set(sourceKey, addr)
		return addr, true
	}
	return "", false
}
