package certificates

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// additionalDNSRequest creates an independent TXT record or deletes it by the returned ID.
func additionalDNSRequest(ctx context.Context, c Certificate, rr, value, id string, client *http.Client) (string, error) {
	var endpoint, contentType string
	var body io.Reader
	method := http.MethodPost
	switch c.Challenge {
	case "namesilo":
		method = http.MethodGet
		q := url.Values{"key": {c.APIToken}, "version": {"1"}, "type": {"xml"}, "domain": {c.ZoneID}}
		action := "dnsAddRecord"
		if id == "" {
			q.Set("rrhost", rr)
			q.Set("rrtype", "TXT")
			q.Set("rrvalue", value)
			q.Set("rrttl", "3600")
		} else {
			action = "dnsDeleteRecord"
			q.Set("rrid", id)
		}
		endpoint = "https://www.namesilo.com/api/" + action + "?" + q.Encode()
	case "dnspod":
		q := url.Values{"login_token": {c.AccessKey + "," + c.APIToken}, "format": {"json"}, "domain": {c.ZoneID}}
		endpoint = "https://dnsapi.cn/Record.Create"
		if id == "" {
			q.Set("sub_domain", rr)
			q.Set("record_type", "TXT")
			q.Set("record_line", "默认")
			q.Set("value", value)
			q.Set("ttl", "600")
		} else {
			endpoint = "https://dnsapi.cn/Record.Remove"
			q.Set("record_id", id)
		}
		contentType = "application/x-www-form-urlencoded"
		body = strings.NewReader(q.Encode())
	case "porkbun":
		payload := map[string]string{"apikey": c.AccessKey, "secretapikey": c.APIToken}
		base := "https://api.porkbun.com/api/json/v3/dns/"
		if id == "" {
			endpoint = base + "create/" + url.PathEscape(c.ZoneID)
			payload["type"] = "TXT"
			payload["name"] = rr
			payload["content"] = value
			payload["ttl"] = "600"
		} else {
			endpoint = base + "delete/" + url.PathEscape(c.ZoneID) + "/" + url.PathEscape(id)
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		body = bytes.NewReader(data)
		contentType = "application/json"
	default:
		return "", fmt.Errorf("不支持的 DNS 服务商")
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return "", fmt.Errorf("DNS 请求无效")
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s DNS 请求失败", c.Challenge)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("%s DNS HTTP %d", c.Challenge, resp.StatusCode)
	}
	r := io.LimitReader(resp.Body, 1<<20)
	switch c.Challenge {
	case "namesilo":
		var result struct {
			Code int    `xml:"reply>code"`
			ID   string `xml:"reply>record_id"`
		}
		if xml.NewDecoder(r).Decode(&result) == nil && result.Code == 300 {
			return result.ID, nil
		}
	case "dnspod":
		var result struct {
			Status struct{ Code string }
			Record struct{ ID json.Number }
		}
		if json.NewDecoder(r).Decode(&result) == nil && result.Status.Code == "1" {
			return result.Record.ID.String(), nil
		}
	case "porkbun":
		var result struct {
			Status string
			ID     json.Number
		}
		if json.NewDecoder(r).Decode(&result) == nil && result.Status == "SUCCESS" {
			return result.ID.String(), nil
		}
	}
	return "", fmt.Errorf("%s DNS 操作失败，请检查凭据和域名权限", c.Challenge)
}
