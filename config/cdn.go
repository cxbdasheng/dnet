package config

import (
	"encoding/json"
	"log"
)

type DCDNConfig struct {
	DCDNEnabled bool  `json:"dcdn_enable"`
	DCDN        []CDN `json:"dcdn"`
}

type CDN struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	Service      string `json:"service"`
	AccessKey    string `json:"access_key"`
	AccessSecret string `json:"access_secret"`
	CDNType      string `json:"cdn_type"`

	IPv4Enable          bool       `json:"ipv4_enable"`
	IPv4Type            string     `json:"ipv4_type"`
	IPv4URLValue        string     `json:"ipv4_url_value"`
	IPv4InterfaceSelect string     `json:"ipv4_interface_select"`
	IPv4CommandValue    string     `json:"ipv4_command_value"`
	IPv4Port            uint       `json:"ipv4_port"`
	IPv4Priority        uint       `json:"ipv4_priority"`
	IPv4Weight          string     `json:"ipv4_weight"`
	StaticIPv4          []StaticIP `json:"static_ipv4"`

	IPv6Enable          bool       `json:"ipv6_enable"`
	IPv6Type            string     `json:"ipv6_type"`
	IPv6URLValue        string     `json:"ipv6_url_value"`
	IPv6InterfaceSelect string     `json:"ipv6_interface_select"`
	IPv6CommandValue    string     `json:"ipv6_command_value"`
	IPv6Port            uint       `json:"ipv6_port"`
	IPv6Priority        uint       `json:"ipv6_priority"`
	IPv6Weight          string     `json:"ipv6_weight"`
	StaticIPv6          []StaticIP `json:"static_ipv6"`
}

type StaticIP struct {
	IP       string `json:"ip"`
	Port     uint   `json:"port"`
	Priority uint   `json:"priority"`
	Weight   string `json:"weight"`
}

// GetDCDNConfigJSON 将 DCDN 配置转换为 JSON 字符串
func GetDCDNConfigJSON(DCDNConf DCDNConfig) string {
	// 如果 DCDN 数组为空，初始化为空数组而不是 null
	if DCDNConf.DCDN == nil {
		DCDNConf.DCDN = []CDN{}
	}

	data, err := json.Marshal(DCDNConf)
	if err != nil {
		log.Printf("序列化DCDN配置失败: %v", err)
		// 返回包含空数组的默认配置
		return `{"dcdn_enable":false,"dcdn":[]}`
	}
	return string(data)
}
