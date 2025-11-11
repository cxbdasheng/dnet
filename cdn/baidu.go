package cdn

type Baidu struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	AccessKey    string `json:"access_key"`
	AccessSecret string `json:"access_secret"`
	CDNType      string `json:"cdn_type"`
}
