package config

import (
	"encoding/json"

	"github.com/cxbdasheng/dnet/helper"
)

type DDNSConfig struct {
	DDNSEnabled bool       `json:"ddns_enable"`
	DDNS        []DNSGroup `json:"ddns"`
}

// RestoreSensitiveFieldsForDDNS 恢复 DDNS 脱敏字段的原始值
// 通过比对新值与旧值的脱敏结果，判断用户是否真正修改了敏感字段
// 只有当新值与旧值脱敏后完全一致时，才使用旧值的原始值
func RestoreSensitiveFieldsForDDNS(newConf, oldConf DDNSConfig) DDNSConfig {
	// 创建映射以快速查找旧配置
	oldGroupMap := make(map[string]DNSGroup)
	for _, group := range oldConf.DDNS {
		oldGroupMap[group.ID] = group
	}

	// 恢复每个 DNS 配置组的敏感字段
	for i := range newConf.DDNS {
		if oldGroup, exists := oldGroupMap[newConf.DDNS[i].ID]; exists {
			// 检查 AccessKey：如果新值与旧值脱敏后完全一致，说明未修改，恢复原始值
			oldAccessKeyMasked := maskSensitiveString(oldGroup.AccessKey)
			if newConf.DDNS[i].AccessKey == oldAccessKeyMasked {
				newConf.DDNS[i].AccessKey = oldGroup.AccessKey
			}
			// 检查 AccessSecret：如果新值与旧值脱敏后完全一致，说明未修改，恢复原始值
			oldAccessSecretMasked := maskSensitiveString(oldGroup.AccessSecret)
			if newConf.DDNS[i].AccessSecret == oldAccessSecretMasked {
				newConf.DDNS[i].AccessSecret = oldGroup.AccessSecret
			}
		}
	}

	return newConf
}

// GetDCDNConfigJSON 将 DCDN 配置转换为 JSON 字符串（敏感信息脱敏）
func GetDCDNConfigJSON(DCDNConf DCDNConfig) string {
	// 如果 DCDN 数组为空，初始化为空数组而不是 null
	if DCDNConf.DCDN == nil {
		DCDNConf.DCDN = []CDN{}
	}

	// 创建副本以避免修改原始配置
	maskedConf := DCDNConfig{
		DCDNEnabled: DCDNConf.DCDNEnabled,
		DCDN:        make([]CDN, len(DCDNConf.DCDN)),
	}

	// 复制并脱敏每个 CDN 配置
	for i, cdn := range DCDNConf.DCDN {
		maskedConf.DCDN[i] = CDN{
			ID:           cdn.ID,
			Name:         cdn.Name,
			Domain:       cdn.Domain,
			CName:        cdn.CName,
			Service:      cdn.Service,
			AccessKey:    maskSensitiveString(cdn.AccessKey),
			AccessSecret: maskSensitiveString(cdn.AccessSecret),
			CDNType:      cdn.CDNType,
			Sources:      cdn.Sources,
		}
	}

	data, err := json.Marshal(maskedConf)
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "序列化DCDN配置失败: %v", err)
		// 返回包含空数组的默认配置
		return `{"dcdn_enable":false,"dcdn":[]}`
	}
	return string(data)
}

// DNSGroup 表示一个域名的配置组，包含多条记录
type DNSGroup struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Domain       string      `json:"domain"`
	Service      string      `json:"service"`
	AccessKey    string      `json:"access_key"`
	AccessSecret string      `json:"access_secret"`
	TTL          string      `json:"ttl"`
	Records      []DNSRecord `json:"records"` // 该域名的多条 DNS 记录
}

// DNSRecord 表示单条 DNS 记录
type DNSRecord struct {
	Type   string `json:"type"`    // 记录类型：A, AAAA, CNAME, TXT
	IPType string `json:"ip_type"` // IP 获取方式：static_ipv4, dynamic_ipv4_url, dynamic_ipv4_interface, dynamic_ipv4_command, static_ipv6, dynamic_ipv6_url, dynamic_ipv6_interface, dynamic_ipv6_command
	Value  string `json:"value"`   // 值：IP地址、URL、网卡名称、命令、CNAME值、TXT 值
	Regex  string `json:"regex"`   // IPv6 正则表达式匹配（仅用于 dynamic_ipv6_interface）
}

// DNSConfig DNS 完整配置（用于传递给 DDNS 处理器）
// 将 DNSGroup 和单个 DNSRecord 合并为一个扁平结构
type DNSConfig struct {
	ID           string
	Name         string
	Domain       string
	Service      string
	AccessKey    string
	AccessSecret string
	TTL          string
	Type         string // 当前记录类型
	IPType       string
	Value        string
	Regex        string
}

// BuildDNSConfig 从 DNSGroup 和 DNSRecord 构建 DNSConfig
func (g *DNSGroup) BuildDNSConfig(record *DNSRecord) DNSConfig {
	return DNSConfig{
		ID:           g.ID,
		Name:         g.Name,
		Domain:       g.Domain,
		Service:      g.Service,
		AccessKey:    g.AccessKey,
		AccessSecret: g.AccessSecret,
		TTL:          g.TTL,
		Type:         record.Type,
		IPType:       record.IPType,
		Value:        record.Value,
		Regex:        record.Regex,
	}
}

// GetDDNSConfigJSON 将 DDNS 配置转换为 JSON 字符串（敏感信息脱敏）
func GetDDNSConfigJSON(DDNSConf DDNSConfig) string {
	// 如果 DDNS 数组为空，初始化为空数组而不是 null
	if DDNSConf.DDNS == nil {
		DDNSConf.DDNS = []DNSGroup{}
	}

	// 创建副本以避免修改原始配置
	maskedConf := DDNSConfig{
		DDNSEnabled: DDNSConf.DDNSEnabled,
		DDNS:        make([]DNSGroup, len(DDNSConf.DDNS)),
	}

	// 复制并脱敏每个 DNS 配置组
	for i, group := range DDNSConf.DDNS {
		maskedConf.DDNS[i] = DNSGroup{
			ID:           group.ID,
			Name:         group.Name,
			Domain:       group.Domain,
			Service:      group.Service,
			AccessKey:    maskSensitiveString(group.AccessKey),
			AccessSecret: maskSensitiveString(group.AccessSecret),
			TTL:          group.TTL,
			Records:      make([]DNSRecord, len(group.Records)),
		}
		// 复制记录数组
		copy(maskedConf.DDNS[i].Records, group.Records)
	}

	data, err := json.Marshal(maskedConf)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "序列化DDNS配置失败: %v", err)
		// 返回包含空数组的默认配置
		return `{"ddns_enable":false,"ddns":[]}`
	}
	return string(data)
}
