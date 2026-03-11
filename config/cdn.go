package config

import (
	"strings"
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

// Source 源站配置
type Source struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Priority  string `json:"priority"`
	Weight    string `json:"weight"`
	Port      string `json:"port"`       // HTTP 端口
	HttpsPort string `json:"https_port"` // HTTPS 端口
	Protocol  string `json:"protocol"`   // HTTP、HTTPS、AUTO（协议跟随），默认 http
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
			// 检查 Domain：如果新值与旧值不相等，说明已修改，将 CName 设置为空
			if oldCDN.Domain != newConf.DCDN[i].Domain {
				newConf.DCDN[i].CName = ""
			}
		}
	}

	return newConf
}
