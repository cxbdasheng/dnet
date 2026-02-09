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
}
