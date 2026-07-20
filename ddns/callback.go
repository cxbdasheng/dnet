package ddns

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

// Callback 自定义回调 DNS 提供商。
// 不对接具体云厂商，而是在记录值发生变化（或触发强制更新）时，
// 向用户配置的 URL 发起一次 HTTP 请求，用于对接自建 DNS、路由器或内部接口。
//
// 字段映射：
//   - AccessKey    -> 回调 URL（必填，支持变量）
//   - AccessSecret -> 请求内容 RequestBody（可选，支持变量）；为空发 GET，否则发 POST
//
// 支持的变量：#{ip} #{domain} #{recordType} #{ttl}
type Callback struct {
	BaseDNSProvider
}

// Init 初始化（只需要 Domain 和回调 URL）
func (c *Callback) Init(group *config.DNSGroup, caches []*Cache) {
	c.Group = group
	c.Caches = caches

	if group == nil || group.Domain == "" || strings.TrimSpace(group.AccessKey) == "" {
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整（需要域名和回调 URL）", c.GetServiceName())
		return
	}

	if len(caches) > 0 && !caches[0].HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功，共 %d 条记录", c.GetServiceName(), len(caches))
	}
}

// UpdateOrCreateRecords 遍历记录，值变化时发起回调
func (c *Callback) UpdateOrCreateRecords() []RecordResult {
	validRecords := filterValidRecords(c.Group, c.Caches)
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	if c.Group.Domain == "" || strings.TrimSpace(c.Group.AccessKey) == "" {
		return createErrorResults(validRecords, InitFailed, "配置不完整（需要域名和回调 URL）")
	}

	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, c.processRecord(vr.record, vr.cache))
	}
	return results
}

// processRecord 处理单条记录
func (c *Callback) processRecord(record *config.DNSRecord, cache *Cache) RecordResult {
	// 1. 获取当前值
	currentValue, result, ok := getCurrentValue(c.GetServiceName(), record, cache)
	if !ok {
		return result
	}

	// 2. 检查缓存
	if skip, r := checkDynamicCache(c.GetServiceName(), record, cache, currentValue, &result); skip {
		return r
	}

	// 3. 发起回调
	if err := c.doCallback(record, currentValue); err != nil {
		result.Status = UpdatedFailed
		result.ErrorMessage = err.Error()
		result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 回调失败 [值=%s, 错误=%v]", c.GetServiceName(), record.Type, currentValue, err)
		return result
	}

	// 4. 更新缓存
	finalizeSuccess(c.GetServiceName(), record, cache, currentValue, &result)
	return result
}

// doCallback 构造并发送回调请求（body 为空发 GET，否则发 POST）
func (c *Callback) doCallback(record *config.DNSRecord, value string) error {
	reqURL := c.replaceVars(strings.TrimSpace(c.Group.AccessKey), record, value)
	rawBody := strings.TrimSpace(c.Group.AccessSecret)

	method := http.MethodGet
	var bodyReader io.Reader
	contentType := "application/x-www-form-urlencoded"
	if rawBody != "" {
		method = http.MethodPost
		body := c.replaceVars(rawBody, record, value)
		bodyReader = strings.NewReader(body)
		if json.Valid([]byte(body)) {
			contentType = "application/json"
		}
	}

	req, err := http.NewRequest(method, reqURL, bodyReader)
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
	helper.Debug(helper.LogTypeDDNS, "[%s] [%s] 回调响应 [状态码=%d, 响应=%s]", c.GetServiceName(), record.Type, resp.StatusCode, string(respBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(respBody) > 0 {
			return fmt.Errorf("回调返回非 2xx 状态码 [状态码=%d, 响应=%s]", resp.StatusCode, string(respBody))
		}
		return fmt.Errorf("回调返回非 2xx 状态码 [状态码=%d]", resp.StatusCode)
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 回调成功 [值=%s]", c.GetServiceName(), record.Type, value)
	return nil
}

// replaceVars 替换回调模板中的变量
func (c *Callback) replaceVars(tmpl string, record *config.DNSRecord, value string) string {
	return strings.NewReplacer(
		"#{ip}", value,
		"#{value}", value,
		"#{domain}", c.Group.Domain,
		"#{recordType}", record.Type,
		"#{ttl}", c.Group.TTL,
	).Replace(tmpl)
}
