package dcdn

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

// Callback 自定义回调 CDN 提供商。
// 不对接具体云厂商，而是在源站 IP 发生变化（或触发强制更新）时，
// 向用户配置的 URL 发起一次 HTTP 请求，用于对接自建 CDN、负载均衡或内部接口。
//
// 字段映射：
//   - AccessKey    -> 回调 URL（必填，支持变量）
//   - AccessSecret -> 请求内容 RequestBody（可选，支持变量）；为空发 GET，否则发 POST
//
// 支持的变量：
//   - #{domain}  域名
//   - #{ips}     逗号分隔的源站地址
//   - #{sources} 源站 JSON 数组（含地址/优先级/权重/端口/协议）
type Callback struct {
	BaseProvider
}

// callbackSource 回调 payload 中的单个源站
type callbackSource struct {
	Addr      string `json:"addr"`
	Priority  string `json:"priority"`
	Weight    string `json:"weight"`
	Port      string `json:"port"`
	HttpsPort string `json:"https_port"`
	Protocol  string `json:"protocol"`
}

// Init 初始化（AccessKey=回调 URL，AccessSecret=请求内容，可选）
func (c *Callback) Init(cdnConfig *config.CDN, cache *Cache) {
	c.CDN = cdnConfig
	c.Cache = cache
	c.Status = InitFailed

	if c.validateConfig() {
		c.Status = InitSuccess
		helper.Info(helper.LogTypeDCDN, "自定义回调初始化成功 [域名=%s, 源站数量=%d]", cdnConfig.Domain, len(cdnConfig.Sources))
	} else {
		helper.Error(helper.LogTypeDCDN, "自定义回调初始化失败：配置校验不通过")
	}
}

// validateConfig 校验配置（只需 AccessKey=URL、域名和源站；AccessSecret 可选）
func (c *Callback) validateConfig() bool {
	if c.CDN == nil {
		helper.Warn(helper.LogTypeDCDN, "自定义回调配置校验失败：配置对象为空")
		return false
	}
	if strings.TrimSpace(c.CDN.AccessKey) == "" {
		helper.Warn(helper.LogTypeDCDN, "自定义回调配置校验失败：回调 URL 为空 [域名=%s]", c.CDN.Domain)
		return false
	}
	if c.CDN.Domain == "" {
		helper.Warn(helper.LogTypeDCDN, "自定义回调配置校验失败：域名为空")
		return false
	}
	if len(c.CDN.Sources) == 0 {
		helper.Warn(helper.LogTypeDCDN, "自定义回调配置校验失败：源站配置为空 [域名=%s]", c.CDN.Domain)
		return false
	}
	return true
}

func (c *Callback) UpdateOrCreateSources() bool {
	return c.runUpdateOrCreate("自定义回调", c.doCallback)
}

// doCallback 构造并发送回调请求（body 为空发 GET，否则发 POST）
func (c *Callback) doCallback() {
	reqURL := c.replaceVars(strings.TrimSpace(c.CDN.AccessKey))
	rawBody := strings.TrimSpace(c.CDN.AccessSecret)

	method := http.MethodGet
	var bodyReader io.Reader
	contentType := "application/x-www-form-urlencoded"
	if rawBody != "" {
		method = http.MethodPost
		body := c.replaceVars(rawBody)
		bodyReader = strings.NewReader(body)
		if json.Valid([]byte(body)) {
			contentType = "application/json"
		}
	}

	if err := c.send(method, reqURL, bodyReader, contentType); err != nil {
		c.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "[自定义回调] 回调失败 [域名=%s, 错误=%v]", c.CDN.Domain, err)
		return
	}

	c.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "[自定义回调] 回调成功 [域名=%s]", c.CDN.Domain)
}

// send 发送 HTTP 请求并校验 2xx 状态码
func (c *Callback) send(method, reqURL string, body io.Reader, contentType string) error {
	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", contentType)
	}

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	helper.Debug(helper.LogTypeDCDN, "[自定义回调] 回调响应 [域名=%s, 状态码=%d, 响应=%s]", c.CDN.Domain, resp.StatusCode, string(respBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(respBody) > 0 {
			return fmt.Errorf("回调返回非 2xx 状态码 [状态码=%d, 响应=%s]", resp.StatusCode, string(respBody))
		}
		return fmt.Errorf("回调返回非 2xx 状态码 [状态码=%d]", resp.StatusCode)
	}
	return nil
}

// replaceVars 替换回调模板中的变量
func (c *Callback) replaceVars(tmpl string) string {
	return strings.NewReplacer(
		"#{domain}", c.CDN.Domain,
		"#{ips}", c.buildIPs(),
		"#{sources}", c.buildSourcesJSON(),
	).Replace(tmpl)
}

// buildIPs 构造逗号分隔的源站地址列表
func (c *Callback) buildIPs() string {
	addrs := make([]string, 0, len(c.CDN.Sources))
	for i := range c.CDN.Sources {
		addrs = append(addrs, c.getSourceAddr(&c.CDN.Sources[i]))
	}
	return strings.Join(addrs, ",")
}

// buildSourcesJSON 构造源站 JSON 数组
func (c *Callback) buildSourcesJSON() string {
	sources := make([]callbackSource, 0, len(c.CDN.Sources))
	for i := range c.CDN.Sources {
		source := &c.CDN.Sources[i]
		sources = append(sources, callbackSource{
			Addr:      c.getSourceAddr(source),
			Priority:  source.Priority,
			Weight:    source.Weight,
			Port:      source.Port,
			HttpsPort: source.HttpsPort,
			Protocol:  source.Protocol,
		})
	}
	data, err := json.Marshal(sources)
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "[自定义回调] 序列化源站失败 [域名=%s, 错误=%v]", c.CDN.Domain, err)
		return "[]"
	}
	return string(data)
}
