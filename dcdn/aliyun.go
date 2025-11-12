package dcdn

import (
	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

type Aliyun struct {
	CDN    *config.CDN
	Cache  *Cache
	Status statusType
}

func (aliyun *Aliyun) Init(cdnConfig *config.CDN, cache *Cache) {
	aliyun.CDN = cdnConfig
	aliyun.Cache = cache
	aliyun.Status = InitFailed
	if aliyun.validateConfig() {
		aliyun.Status = InitSuccess
	}
}

// validateConfig 校验 CDN 配置是否有效
func (aliyun *Aliyun) validateConfig() bool {
	if aliyun.CDN == nil {
		return false
	}
	// 检查必填的认证信息
	if aliyun.CDN.AccessKey == "" || aliyun.CDN.AccessSecret == "" {
		return false
	}
	// 检查域名
	if aliyun.CDN.Domain == "" {
		return false
	}
	// 检查 CDN 类型
	if aliyun.CDN.CDNType == "" {
		return false
	}
	// 检查源站配置
	if len(aliyun.CDN.Sources) == 0 {
		return false
	}
	return true
}

func (aliyun *Aliyun) UpdateOrCreateSources() bool {
	// 初始化失败则不继续执行
	if aliyun.Status == InitFailed {
		return false
	}

	// 检查动态源的 IP 变化情况
	changedIPCount := 0
	for _, source := range aliyun.CDN.Sources {
		// 跳过静态源
		if !IsDynamicType(source.Type) {
			continue
		}

		// 获取动态 IP
		addr, ok := helper.GetOrSetDynamicIPWithCache(source.Type, source.Value)
		if !ok {
			// IP 获取失败，标记状态并终止
			aliyun.Status = InitGetIPFailed
			return false
		}

		cacheKey := helper.GetIPCacheKey(source.Type, source.Value)

		// 检查 IP 是否发生变化
		ipChanged, _ := aliyun.Cache.CheckIPChanged(cacheKey, addr)
		if ipChanged {
			// IP 发生变化，更新缓存
			aliyun.Cache.UpdateDynamicIP(cacheKey, addr)
			changedIPCount++
		}
	}

	// 判断是否需要更新 CDN
	shouldUpdate := aliyun.shouldUpdateCDN(changedIPCount)
	if shouldUpdate {
		aliyun.updateOrCreateCDN()
		aliyun.Cache.ResetTimes()
	}

	return shouldUpdate
}

// shouldUpdateCDN 判断是否需要更新 CDN 配置
func (aliyun *Aliyun) shouldUpdateCDN(changedIPCount int) bool {
	// 第一次运行，需要初始化
	if !aliyun.Cache.HasRun {
		aliyun.Cache.HasRun = true
		return true
	}

	// 有 IP 发生变化，需要更新
	if changedIPCount > 0 {
		return true
	}

	// 递减计数器
	aliyun.Cache.Times--

	// 计数器归零，需要强制更新
	if aliyun.Cache.Times == 0 {
		return true
	}

	// 无需更新
	return false
}

func (aliyun *Aliyun) updateOrCreateCDN() {

}
