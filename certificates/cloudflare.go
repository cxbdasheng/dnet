package certificates

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func cloudflare(ctx context.Context, c Certificate, method, path string, body any) (string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.cloudflare.com/client/v4/zones/"+c.ZoneID+path, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIToken)
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", fmt.Errorf("Cloudflare 连接失败")
	}
	defer response.Body.Close()
	var result struct {
		Success bool `json:"success"`
		Result  struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil || response.StatusCode >= 300 || !result.Success {
		return "", fmt.Errorf("Cloudflare 操作失败（HTTP %d），请检查 Token 权限和 Zone ID", response.StatusCode)
	}
	return result.Result.ID, nil
}
func presentCloudflare(ctx context.Context, c Certificate, name, value string) (func(), error) {
	zone, err := discoverCloudflareZone(ctx, c.APIToken, strings.TrimPrefix(name, "_acme-challenge."), &http.Client{Timeout: 30 * time.Second})
	if err != nil {
		return nil, err
	}
	c.ZoneID = zone
	id, err := cloudflare(ctx, c, "POST", "/dns_records", map[string]any{"type": "TXT", "name": name, "content": value, "ttl": 120})
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, fmt.Errorf("Cloudflare 未返回记录 ID")
	}
	return func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cloudflare(cleanup, c, "DELETE", "/dns_records/"+id, nil)
	}, nil
}

// Query exact suffixes from most specific to least specific, including delegated zones.
func discoverCloudflareZone(ctx context.Context, token, domain string, client *http.Client) (string, error) {
	for candidate := strings.TrimSuffix(strings.ToLower(domain), "."); strings.Contains(candidate, "."); candidate = strings.SplitN(candidate, ".", 2)[1] {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.cloudflare.com/client/v4/zones?"+url.Values{"name": {candidate}, "per_page": {"50"}}.Encode(), nil)
		if err != nil {
			return "", fmt.Errorf("Cloudflare 区域查询请求无效")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("Cloudflare 区域查询连接失败")
		}
		var result struct {
			Success bool
			Result  []struct {
				ID   string
				Name string
			}
			ResultInfo struct {
				TotalCount int `json:"total_count"`
			} `json:"result_info"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || !result.Success {
			return "", fmt.Errorf("Cloudflare 区域查询失败，请检查 Token 的 Zone Read 权限及授权域名范围")
		}
		found := ""
		for _, zone := range result.Result {
			if strings.EqualFold(zone.Name, candidate) && zone.ID != "" {
				if found != "" {
					return "", fmt.Errorf("Cloudflare 存在同名区域，请将 Token 限定到目标区域")
				}
				found = zone.ID
			}
		}
		if found != "" {
			if result.ResultInfo.TotalCount > len(result.Result) {
				return "", fmt.Errorf("Cloudflare 存在多个区域，请缩小 Token 授权范围")
			}
			return found, nil
		}
	}
	return "", fmt.Errorf("Cloudflare 未找到域名所属区域，请检查域名及 Token 的 Zone Read 权限和授权范围")
}
