package config

type DDNSConfig struct {
	DDNSEnabled bool  `json:"ddns_enable"`
	DDNS        []DNS `json:"ddns"`
}
type DNS struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	Service      string `json:"service"`
	AccessKey    string `json:"access_key"`
	AccessSecret string `json:"access_secret"`
	TTL          string `json:"ttl"`
	Type         string `json:"type"`    // 记录类型：A, AAAA, CNAME, TXT
	IPType       string `json:"ip_type"` // IP 获取方式：static_ipv4, dynamic_ipv4_url, dynamic_ipv4_interface, dynamic_ipv4_command, static_ipv6, dynamic_ipv6_url, dynamic_ipv6_interface, dynamic_ipv6_command
	Value        string `json:"value"`   // 值：IP地址、URL、网卡名称、命令、CNAME值、TXT 值
	Regex        string `json:"regex"`   // IPv6 正则表达式匹配（仅用于 dynamic_ipv6_interface）
}
