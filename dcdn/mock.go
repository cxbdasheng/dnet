package dcdn

import (
	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

// Mock 模拟测试 CDN 提供商——不发起任何真实 API 请求，
// 仅走完整状态机并打印"若在真实环境将做什么"。
// 用途：验证配置、观察 IP 探测流程、测试 Webhook 通道。
type Mock struct {
	BaseProvider
}

// Init 无需鉴权，只校验域名与源站
func (m *Mock) Init(cdnConfig *config.CDN, cache *Cache) {
	m.CDN = cdnConfig
	m.Cache = cache
	m.Status = InitFailed

	if cdnConfig == nil || cdnConfig.Domain == "" {
		helper.Warn(helper.LogTypeDCDN, "[MOCK] 初始化失败: 域名为空")
		return
	}
	if len(cdnConfig.Sources) == 0 {
		helper.Warn(helper.LogTypeDCDN, "[MOCK] 初始化失败: 源站配置为空 [域名=%s]", cdnConfig.Domain)
		return
	}

	m.Status = InitSuccess
	helper.Info(helper.LogTypeDCDN, "[MOCK] 初始化成功 [域名=%s, 源站数量=%d]", cdnConfig.Domain, len(cdnConfig.Sources))
}

func (m *Mock) UpdateOrCreateSources() bool {
	return m.runUpdateOrCreate("Mock", m.mockUpdate)
}

// mockUpdate 输出模拟推送日志，不调用任何云商 API
func (m *Mock) mockUpdate() {
	for i := range m.CDN.Sources {
		source := &m.CDN.Sources[i]
		addr := m.getSourceAddr(source)
		helper.Info(helper.LogTypeDCDN,
			"[MOCK] 若在真实环境将推送源站 [域名=%s, 类型=%s, 配置值=%s, 实际地址=%s, 优先级=%s, 权重=%s]",
			m.CDN.Domain, source.Type, source.Value, addr, source.Priority, source.Weight)
	}
	m.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "[MOCK] 模拟同步完成 [域名=%s]", m.CDN.Domain)
}
