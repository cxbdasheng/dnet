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

// GetDCDNConfigJSON 将 DCDN 配置转换为 JSON 字符串
func GetDCDNConfigJSON(DCDNConf DCDNConfig) string {
	// 如果 DCDN 数组为空，初始化为空数组而不是 null
	if DCDNConf.DCDN == nil {
		DCDNConf.DCDN = []CDN{}
	}

	data, err := json.Marshal(DCDNConf)
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "序列化DCDN配置失败: %v", err)
		// 返回包含空数组的默认配置
		return `{"dcdn_enable":false,"dcdn":[]}`
	}
	return string(data)
}
