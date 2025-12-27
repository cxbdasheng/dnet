package config

import (
	"encoding/json"
	"strings"

	"github.com/cxbdasheng/dnet/helper"
)

type DCDNConfig struct {
	DCDNEnabled bool  `json:"dcdn_enable"`
	DCDN        []CDN `json:"dcdn"`
}

type CDN struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Domain       string   `json:"domain"`
	Service      string   `json:"service"`
	AccessKey    string   `json:"access_key"`
	AccessSecret string   `json:"access_secret"`
	CDNType      string   `json:"cdn_type"`
	Sources      []Source `json:"sources"`
	CName        string   `json:"cname"`
}

// GetRootDomain 获取域名的根域名
// 例如：test.example.com -> example.com
//
//	example.com -> example.com
func (c *CDN) GetRootDomain() string {
	if c.Domain == "" {
		return ""
	}

	parts := strings.Split(c.Domain, ".")
	if len(parts) <= 2 {
		// 已经是根域名或无效域名
		return c.Domain
	}

	// 返回最后两段作为根域名
	return parts[len(parts)-2] + "." + parts[len(parts)-1]
}

type Source struct {
	Type     string `json:"type"`
	Value    string `json:"value"`
	Priority string `json:"priority"`
	Weight   string `json:"weight"`
	Port     string `json:"port"`
	Protocol string `json:"protocol"` // http 或 https，默认 http
}

// maskSensitiveString 对敏感字符串进行脱敏处理
// 规则：保留前4位和后4位，中间用 * 代替；长度 <= 8 时全部用 * 代替
func maskSensitiveString(s string) string {
	if s == "" {
		return ""
	}

	length := len(s)
	if length <= 8 {
		// 如果长度 <= 8，全部用 * 代替
		return strings.Repeat("*", length)
	}

	// 保留前4位和后4位，中间用 * 代替
	maskCount := length - 8
	return s[:4] + strings.Repeat("*", maskCount) + s[length-4:]
}

// RestoreSensitiveFields 恢复脱敏字段的原始值
// 通过比对新值与旧值的脱敏结果，判断用户是否真正修改了敏感字段
// 只有当新值与旧值脱敏后完全一致时，才使用旧值的原始值
func RestoreSensitiveFields(newConf, oldConf DCDNConfig) DCDNConfig {
	// 创建映射以快速查找旧配置
	oldCDNMap := make(map[string]CDN)
	for _, cdn := range oldConf.DCDN {
		oldCDNMap[cdn.ID] = cdn
	}

	// 恢复每个 CDN 配置的敏感字段
	for i := range newConf.DCDN {
		if oldCDN, exists := oldCDNMap[newConf.DCDN[i].ID]; exists {
			// 检查 AccessKey：如果新值与旧值脱敏后完全一致，说明未修改，恢复原始值
			oldAccessKeyMasked := maskSensitiveString(oldCDN.AccessKey)
			if newConf.DCDN[i].AccessKey == oldAccessKeyMasked {
				newConf.DCDN[i].AccessKey = oldCDN.AccessKey
			}
			// 检查 AccessSecret：如果新值与旧值脱敏后完全一致，说明未修改，恢复原始值
			oldAccessSecretMasked := maskSensitiveString(oldCDN.AccessSecret)
			if newConf.DCDN[i].AccessSecret == oldAccessSecretMasked {
				newConf.DCDN[i].AccessSecret = oldCDN.AccessSecret
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
