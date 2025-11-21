package config

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/cxbdasheng/dnet/helper"
)

// Webhook Webhook
type Webhook struct {
	WebhookEnabled     bool   `json:"webhook_enabled"`
	WebhookURL         string `json:"webhook_url"`
	WebhookHeaders     string `json:"webhook_headers"`
	WebhookRequestBody string `json:"webhook_request_body"`
}

func ExecWebhook(conf *Webhook, serviceType, serviceName, serviceStatus string) bool {
	if conf.WebhookURL == "" {
		return false
	}
	// 成功和失败都要触发webhook
	method := http.MethodGet
	contentType := "application/x-www-form-urlencoded"
	body := ""

	if conf.WebhookRequestBody != "" {
		method = http.MethodPost
		body = replacePara(conf.WebhookRequestBody, serviceType, serviceName, serviceStatus)
		if json.Valid([]byte(body)) {
			contentType = "application/json"
		} else if hasJSONPrefix(body) {
			// 如果 RequestBody 的 JSON 无效但前缀为 JSON，提示无效
			helper.Info(helper.LogTypeSystem, "Webhook 中的 RequestBody JSON 无效")
		}
	}
	u, err := url.Parse(replacePara(conf.WebhookURL, serviceType, serviceName, serviceStatus))
	if err != nil {
		helper.Error(helper.LogTypeSystem, "Webhook 配置中的 URL 不正确: %s", err)
		return false
	}
	q, _ := url.ParseQuery(u.RawQuery)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(method, u.String(), strings.NewReader(body))
	if err != nil {
		helper.Error(helper.LogTypeSystem, "Webhook 调用失败! 异常信息：%s", err)
		return false
	}

	headers := extractHeaders(conf.WebhookHeaders)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", contentType)

	clt := helper.CreateHTTPClient()
	resp, err := clt.Do(req)
	respBody, err := helper.GetHTTPResponseOrg(resp, err)
	if err == nil {
		helper.Info(helper.LogTypeSystem, "Webhook 调用成功! 返回数据：%s", string(respBody))
		return true
	}

	helper.Error(helper.LogTypeSystem, "Webhook 调用失败! 异常信息：%s", err)
	return false
}

// hasJSONPrefix returns true if the string starts with a JSON open brace.
func hasJSONPrefix(s string) bool {
	return strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")
}

// extractHeaders converts s into a map of headers.
//
// See also: https://github.com/appleboy/gorush/blob/v1.17.0/notify/feedback.go#L15
func extractHeaders(s string) map[string]string {
	lines := helper.SplitLines(s)
	headers := make(map[string]string, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			helper.Info(helper.LogTypeSystem, "Webhook Header不正确: %s", line)
			continue
		}

		k, v := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		headers[k] = v
	}

	return headers
}

// replacePara 替换参数
func replacePara(orgPara, serviceType, serviceName, serviceStatus string) string {
	return strings.NewReplacer(
		"#{serviceType}", serviceType,
		"#{serviceName}", serviceName,
		"#{serviceStatus}", serviceStatus,
	).Replace(orgPara)
}
