package ddns

import (
	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

// Mock 模拟测试 DNS 提供商——不发起任何真实 API 请求，
// 仅走完整状态机并打印"若在真实环境将做什么"。
// 用途：验证配置、观察 IP 探测流程、测试 Webhook 通道。
type Mock struct {
	BaseDNSProvider
}

// Init 无需鉴权，只校验域名
func (m *Mock) Init(group *config.DNSGroup, caches []*Cache) {
	m.Group = group
	m.Caches = caches

	if group == nil || group.Domain == "" {
		helper.Error(helper.LogTypeDDNS, "[%s] [MOCK] 初始化失败: 域名为空", m.GetServiceName())
		return
	}

	if len(caches) > 0 && !caches[0].HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] [MOCK] 初始化成功，共 %d 条记录", m.GetServiceName(), len(caches))
	}
}

// UpdateOrCreateRecords 模拟批量更新或创建记录
func (m *Mock) UpdateOrCreateRecords() []RecordResult {
	validRecords := filterValidRecords(m.Group, m.Caches)
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	if m.Group.Domain == "" {
		return createErrorResults(validRecords, InitFailed, "配置不完整")
	}

	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, m.processRecord(vr.record, vr.cache))
	}
	return results
}

// processRecord 走一遍完整状态机，但不发起真实请求
func (m *Mock) processRecord(record *config.DNSRecord, cache *Cache) RecordResult {
	// 1. 获取当前值
	currentValue, result, ok := getCurrentValue(m.GetServiceName(), record, cache)
	if !ok {
		return result
	}
	// 2. 检查缓存（未变化直接跳过）
	if skip, r := checkDynamicCache(m.GetServiceName(), record, cache, currentValue, &result); skip {
		return r
	}
	// 3. 模拟推送
	helper.Info(helper.LogTypeDDNS,
		"[%s] [MOCK] [%s] 若在真实环境将推送记录 [域名=%s, TTL=%s, 值=%s]",
		m.GetServiceName(), record.Type, m.Group.Domain, m.Group.TTL, currentValue)
	// 4. 标记成功、更新缓存
	finalizeSuccess(m.GetServiceName(), record, cache, currentValue, &result)
	return result
}
