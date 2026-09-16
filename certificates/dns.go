package certificates

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cxbdasheng/dnet/signer"
	"golang.org/x/net/publicsuffix"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func dnsProvider(kind string) bool {
	return kind == "cloudflare" || kind == "aliyun" || kind == "tencent"
}

func presentDNS(ctx context.Context, c Certificate, name, value string) (func(), error) {
	if c.Challenge == "cloudflare" {
		return presentCloudflare(ctx, c, name, value)
	}
	return presentCloudDNS(ctx, c, name, value, &http.Client{Timeout: 30 * time.Second})
}

func presentCloudDNS(ctx context.Context, c Certificate, name, value string, client *http.Client) (func(), error) {
	if c.Challenge != "aliyun" && c.Challenge != "tencent" {
		return nil, fmt.Errorf("不支持的 DNS 服务商")
	}
	if c.ZoneID == "" {
		var err error
		c.ZoneID, err = publicsuffix.EffectiveTLDPlusOne(strings.TrimPrefix(name, "_acme-challenge."))
		if err != nil {
			return nil, fmt.Errorf("无法从申请域名确定 DNS 主域名，请检查申请域名")
		}
	}
	if !strings.HasSuffix(name, "."+c.ZoneID) {
		return nil, fmt.Errorf("验证域名不属于指定 DNS 区域")
	}
	rr := strings.TrimSuffix(name, "."+c.ZoneID)
	id, err := cloudDNSRequest(ctx, c, rr, value, "", client)
	if err != nil {
		return nil, err
	}
	if id == "" || id == "0" {
		return nil, fmt.Errorf("DNS 服务商未返回记录 ID")
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cloudDNSRequest(cleanup, c, "", "", id, client)
	}, nil
}

func cloudDNSRequest(ctx context.Context, c Certificate, rr, value, id string, client *http.Client) (string, error) {
	var req *http.Request
	var err error
	if c.Challenge == "aliyun" {
		params := url.Values{"Version": {"2015-01-09"}}
		if id == "" {
			params.Set("Action", "AddDomainRecord")
			params.Set("DomainName", c.ZoneID)
			params.Set("RR", rr)
			params.Set("Type", "TXT")
			params.Set("Value", value)
			params.Set("TTL", "600")
		} else {
			params.Set("Action", "DeleteDomainRecord")
			params.Set("RecordId", id)
		}
		signer.AliyunSigner(c.AccessKey, c.APIToken, &params, http.MethodPost)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, "https://alidns.aliyuncs.com/", strings.NewReader(params.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	} else {
		action := "CreateRecord"
		body := map[string]any{"Domain": c.ZoneID, "SubDomain": rr, "RecordType": "TXT", "RecordLine": "默认", "Value": value, "TTL": 600}
		if id != "" {
			action = "DeleteRecord"
			body = map[string]any{"Domain": c.ZoneID, "RecordId": json.Number(id)}
		}
		data, e := json.Marshal(body)
		if e != nil {
			return "", fmt.Errorf("DNS 参数无效")
		}
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, "https://dnspod.tencentcloudapi.com/", strings.NewReader(string(data)))
		if err == nil {
			req.Header.Set("X-TC-Action", action)
			signer.TencentSigner(c.AccessKey, c.APIToken, "dnspod", "dnspod.tencentcloudapi.com", string(data), req)
			req.Header.Set("X-TC-Version", "2021-03-23")
		}
	}
	if err != nil {
		return "", fmt.Errorf("DNS 请求创建失败")
	}
	response, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("DNS 服务商连接失败")
	}
	defer response.Body.Close()
	var result struct {
		RecordID string `json:"RecordId"`
		Code     string
		Response struct {
			RecordID json.Number `json:"RecordId"`
			Error    *struct{ Code string }
		}
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil || response.StatusCode >= 300 || result.Code != "" || result.Response.Error != nil {
		return "", fmt.Errorf("DNS 操作失败（HTTP %d），请检查凭据、区域和记录增删权限", response.StatusCode)
	}
	if c.Challenge == "aliyun" {
		return result.RecordID, nil
	}
	return string(result.Response.RecordID), nil
}
