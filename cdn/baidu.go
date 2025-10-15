package cdn

import "github.com/cxbdasheng/dnet/config"

type Baidu struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	AccessKey    string `json:"access_key"`
	AccessSecret string `json:"access_secret"`
	CDNType      string `json:"cdn_type"`

	IPv4Addr   string
	IPv4Weight int
	StaticIPv4 []config.StaticIP

	IPv6Addr   string
	IPv6Weight int
	StaticIPv6 []config.StaticIP
}
