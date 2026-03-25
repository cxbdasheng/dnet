package dcdn

import (
	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

// BaseProvider 所有 CDN 服务商的公共字段和方法
type BaseProvider struct {
	CDN           *config.CDN
	Cache         *Cache
	Status        statusType
	configChanged bool
}

func (b *BaseProvider) GetServiceStatus() string {
	return string(b.Status)
}

func (b *BaseProvider) GetServiceName() string {
	if b.CDN == nil {
		return ""
	}
	if b.CDN.Name != "" {
		return b.CDN.Name
	}
	return b.CDN.Domain
}

func (b *BaseProvider) ConfigChanged() bool {
	return b.configChanged
}

// validateBaseConfig 校验公共配置（认证信息、域名、源站）
func (b *BaseProvider) validateBaseConfig(providerName string) bool {
	if b.CDN == nil {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：配置对象为空", providerName)
		return false
	}
	if b.CDN.AccessKey == "" || b.CDN.AccessSecret == "" {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：AccessKey 或 AccessSecret 为空 [域名=%s]", providerName, b.CDN.Domain)
		return false
	}
	if b.CDN.Domain == "" {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：域名为空", providerName)
		return false
	}
	if len(b.CDN.Sources) == 0 {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：源站配置为空 [域名=%s]", providerName, b.CDN.Domain)
		return false
	}
	return true
}

// checkDynamicIPChanges 检查所有动态源站的 IP 变化，返回变化数量
// 若 IP 获取失败，设置 Status=InitGetIPFailed 并返回 (0, false)
func (b *BaseProvider) checkDynamicIPChanges() (int, bool) {
	changedIPCount := 0
	for _, source := range b.CDN.Sources {
		if !IsDynamicType(source.Type) {
			continue
		}
		addr, ok := helper.GetOrSetDynamicIPWithCache(source.Type, source.Value)
		if !ok {
			b.Status = InitGetIPFailed
			helper.Error(helper.LogTypeDCDN, "获取动态 IP 失败 [域名=%s, 源类型=%s, 配置值=%s]",
				b.CDN.Domain, source.Type, source.Value)
			return 0, false
		}
		cacheKey := helper.GetIPCacheKey(source.Type, source.Value)
		ipChanged, oldIP := b.Cache.CheckIPChanged(cacheKey, addr)
		if ipChanged {
			b.Cache.UpdateDynamicIP(cacheKey, addr)
			changedIPCount++
			helper.Info(helper.LogTypeDCDN, "检测到源站 IP 变化 [域名=%s, 源类型=%s, 旧IP=%s, 新IP=%s]",
				b.CDN.Domain, source.Type, oldIP, addr)
		}
	}
	return changedIPCount, true
}

// hasDynamicSources 判断是否包含动态类型的源站
func (b *BaseProvider) hasDynamicSources() bool {
	for _, source := range b.CDN.Sources {
		if IsDynamicType(source.Type) {
			return true
		}
	}
	return false
}

// shouldUpdate 判断是否需要更新配置（首次运行、IP 变化、计数器归零）
// 计数器归零的强制更新仅对含动态源站的配置生效
func (b *BaseProvider) shouldUpdate(providerName string, changedIPCount int) bool {
	if !b.Cache.HasRun {
		b.Cache.HasRun = true
		helper.Info(helper.LogTypeDCDN, "首次运行，需要初始化 %s 配置 [域名=%s]", providerName, b.CDN.Domain)
		return true
	}
	if changedIPCount > 0 {
		helper.Info(helper.LogTypeDCDN, "源站 IP 变化，需要更新 %s [域名=%s, 变化数=%d]", providerName, b.CDN.Domain, changedIPCount)
		return true
	}
	if !b.hasDynamicSources() {
		return false
	}
	b.Cache.Times--
	if b.Cache.Times == 0 {
		helper.Info(helper.LogTypeDCDN, "计数器归零，强制更新 %s [域名=%s]", providerName, b.CDN.Domain)
		return true
	}
	return false
}

// runUpdateOrCreate 执行通用的检查→判断→更新流程
func (b *BaseProvider) runUpdateOrCreate(providerName string, doUpdate func()) bool {
	if b.Status == InitFailed {
		helper.Warn(helper.LogTypeDCDN, "%s 更新跳过：初始化失败 [域名=%s]", providerName, b.CDN.Domain)
		return false
	}
	helper.Debug(helper.LogTypeDCDN, "开始检查 %s 源站 IP 变化 [域名=%s]", providerName, b.CDN.Domain)

	changedIPCount, ok := b.checkDynamicIPChanges()
	if !ok {
		return false
	}
	if changedIPCount > 0 {
		helper.Info(helper.LogTypeDCDN, "共检测到 %d 个源站 IP 发生变化 [域名=%s]", changedIPCount, b.CDN.Domain)
	} else {
		helper.Debug(helper.LogTypeDCDN, "未检测到源站 IP 变化 [域名=%s]", b.CDN.Domain)
	}

	if b.shouldUpdate(providerName, changedIPCount) {
		helper.Info(helper.LogTypeDCDN, "开始更新 %s 配置 [域名=%s, IP变化数=%d, 计数器=%d]",
			providerName, b.CDN.Domain, changedIPCount, b.Cache.Times)
		doUpdate()
		b.Cache.ResetTimes()
		return true
	}
	b.Status = UpdatedNothing
	helper.Debug(helper.LogTypeDCDN, "无需更新 %s 配置 [域名=%s, 计数器剩余=%d]",
		providerName, b.CDN.Domain, b.Cache.Times)
	return false
}

// ShouldSendWebhook 默认 webhook 策略：成功立即通知，失败连续 3 次后通知
func (b *BaseProvider) ShouldSendWebhook() bool {
	if b.Status == UpdatedSuccess {
		b.Cache.TimesFailed = 0
		return true
	}
	if b.Status == UpdatedFailed {
		b.Cache.TimesFailed++
		if b.Cache.TimesFailed >= 3 {
			helper.Warn(helper.LogTypeDCDN, "连续更新失败 %d 次，触发 Webhook 通知 [域名=%s]", b.Cache.TimesFailed, b.CDN.Domain)
			b.Cache.TimesFailed = 0
			return true
		}
		helper.Warn(helper.LogTypeDCDN, "更新失败，将不会触发 Webhook，仅在连续失败 3 次时触发，当前失败次数：%d [域名=%s]", b.Cache.TimesFailed, b.CDN.Domain)
	}
	return false
}

// updateCnameIfChanged 仅当 CNAME 发生变化时更新并标记配置已改变
func (b *BaseProvider) updateCnameIfChanged(newCname string) {
	if newCname != "" && newCname != b.CDN.CName {
		helper.Info(helper.LogTypeDCDN, "CNAME 发生变化 [域名=%s, 旧 CNAME=%s, 新 CNAME=%s]",
			b.CDN.Domain, b.CDN.CName, newCname)
		b.CDN.CName = newCname
		b.configChanged = true
	}
}

// getSourceAddr 获取源站的实际地址（处理动态 IP，返回原始 IP 或域名）
func (b *BaseProvider) getSourceAddr(source *config.Source) string {
	if IsDynamicType(source.Type) {
		cacheKey := helper.GetIPCacheKey(source.Type, source.Value)
		if ip, ok := b.Cache.DynamicIPs[cacheKey]; ok {
			return ip
		}
		if addr, ok := helper.GetOrSetDynamicIPWithCache(source.Type, source.Value); ok {
			return addr
		}
		helper.Warn(helper.LogTypeDCDN, "无法获取动态源站IP [类型=%s, 值=%s]，使用配置值", source.Type, source.Value)
	}
	return source.Value
}
