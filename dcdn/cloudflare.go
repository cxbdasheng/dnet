package dcdn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

const (
	cloudflareAPIEndpoint = "https://api.cloudflare.com/client/v4"
)

type Cloudflare struct {
	BaseProvider
	zoneID   string
	recordID string
}

type cloudflareAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CloudflareZone Cloudflare Zone 信息
type CloudflareZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// CloudflareZonesResponse Zone 列表响应
type CloudflareZonesResponse struct {
	Success bool                 `json:"success"`
	Errors  []cloudflareAPIError `json:"errors"`
	Result  []CloudflareZone     `json:"result"`
}

// CloudflareDNSRecord DNS 记录
type CloudflareDNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
	TTL     int    `json:"ttl"`
}

// CloudflareDNSRecordsResponse DNS 记录列表响应
type CloudflareDNSRecordsResponse struct {
	Success bool                  `json:"success"`
	Errors  []cloudflareAPIError  `json:"errors"`
	Result  []CloudflareDNSRecord `json:"result"`
}

// CloudflareDNSRecordResponse 单个 DNS 记录响应
type CloudflareDNSRecordResponse struct {
	Success bool                 `json:"success"`
	Errors  []cloudflareAPIError `json:"errors"`
	Result  CloudflareDNSRecord  `json:"result"`
}

func cfError(errors []cloudflareAPIError, fallback string) error {
	if len(errors) > 0 {
		return fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", errors[0].Code, errors[0].Message)
	}
	return fmt.Errorf("%s", fallback)
}

func (cf *Cloudflare) Init(cdnConfig *config.CDN, cache *Cache) {
	cf.CDN = cdnConfig
	cf.Cache = cache
	cf.Status = InitFailed

	if cf.validateConfig() {
		cf.Status = InitSuccess
		cdnType := strings.ToUpper(cdnConfig.CDNType)
		helper.Info(helper.LogTypeDCDN, "Cloudflare %s 初始化成功 [域名=%s, 源站数量=%d]", cdnType, cdnConfig.Domain, len(cdnConfig.Sources))
	} else {
		helper.Error(helper.LogTypeDCDN, "Cloudflare 初始化失败：配置校验不通过 [域名=%s]", cdnConfig.Domain)
	}
}

// validateConfig 校验 CDN 配置是否有效
// 不使用 validateBaseConfig，因为 Cloudflare 只需要 AccessKey（Token），无 AccessSecret
func (cf *Cloudflare) validateConfig() bool {
	if cf.CDN == nil {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：配置对象为空")
		return false
	}
	if cf.CDN.AccessKey == "" {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：API Token 为空 [域名=%s]", cf.CDN.Domain)
		return false
	}
	if cf.CDN.Domain == "" {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：域名为空")
		return false
	}
	if cf.CDN.CDNType == "" {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：类型为空 [域名=%s]", cf.CDN.Domain)
		return false
	}
	cdnType := strings.ToLower(cf.CDN.CDNType)
	if cdnType != "cdn" && cdnType != "dns" {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：不支持的类型 [域名=%s, 类型=%s]，仅支持 CDN 或 DNS", cf.CDN.Domain, cf.CDN.CDNType)
		return false
	}
	if len(cf.CDN.Sources) == 0 {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：源站配置为空 [域名=%s]", cf.CDN.Domain)
		return false
	}
	return true
}

func (cf *Cloudflare) UpdateOrCreateSources() bool {
	return cf.runUpdateOrCreate("Cloudflare CDN", cf.updateOrCreateDNSRecord)
}

// request 统一请求方法
func (cf *Cloudflare) request(method, path string, body interface{}, result interface{}) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	req, err := http.NewRequest(method, cloudflareAPIEndpoint+path, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cf.CDN.AccessKey))
	req.Header.Set("Content-Type", "application/json")

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	return helper.GetHTTPResponse(resp, err, result)
}

// updateOrCreateDNSRecord 更新或创建 DNS 记录
func (cf *Cloudflare) updateOrCreateDNSRecord() {
	// 获取 Zone ID
	if cf.zoneID == "" {
		zoneID, err := cf.getZoneID()
		if err != nil {
			cf.Status = UpdatedFailed
			helper.Error(helper.LogTypeDCDN, "获取 Zone ID 失败 [域名=%s, 错误=%v]", cf.CDN.Domain, err)
			return
		}
		cf.zoneID = zoneID
	}

	// 获取当前 DNS 记录
	record, err := cf.getDNSRecord()
	if err != nil {
		helper.Warn(helper.LogTypeDCDN, "获取 DNS 记录失败，尝试创建新记录 [域名=%s, 错误=%v]", cf.CDN.Domain, err)
		// 创建新记录
		if err := cf.createDNSRecord(); err != nil {
			cf.Status = UpdatedFailed
			helper.Error(helper.LogTypeDCDN, "创建 DNS 记录失败 [域名=%s, 错误=%v]", cf.CDN.Domain, err)
			return
		}
		cf.Status = UpdatedSuccess
		helper.Info(helper.LogTypeDCDN, "创建 DNS 记录成功 [域名=%s]", cf.CDN.Domain)
		return
	}

	// 更新现有记录
	if err := cf.updateDNSRecord(record.ID); err != nil {
		cf.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "更新 DNS 记录失败 [域名=%s, 错误=%v]", cf.CDN.Domain, err)
		return
	}

	cf.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "更新 DNS 记录成功 [域名=%s]", cf.CDN.Domain)
}

// getZoneID 获取 Zone ID
func (cf *Cloudflare) getZoneID() (string, error) {
	// 从域名中提取根域名
	rootDomain := cf.CDN.GetRootDomain()
	helper.Debug(helper.LogTypeDCDN, "正在获取 Zone ID [域名=%s, 根域名=%s]", cf.CDN.Domain, rootDomain)

	var resp CloudflareZonesResponse
	if err := cf.request(http.MethodGet, "/zones?name="+rootDomain, nil, &resp); err != nil {
		return "", err
	}
	if !resp.Success {
		return "", cfError(resp.Errors, "获取 Zone 失败")
	}
	if len(resp.Result) == 0 {
		return "", fmt.Errorf("未找到域名 %s 对应的 Zone，请确认域名已添加到 Cloudflare", rootDomain)
	}
	helper.Debug(helper.LogTypeDCDN, "成功获取 Zone ID [Zone ID=%s, 域名=%s]", resp.Result[0].ID, rootDomain)
	return resp.Result[0].ID, nil
}

// getDNSRecord 获取 DNS 记录
func (cf *Cloudflare) getDNSRecord() (*CloudflareDNSRecord, error) {
	var resp CloudflareDNSRecordsResponse
	path := fmt.Sprintf("/zones/%s/dns_records?name=%s", cf.zoneID, cf.CDN.Domain)
	if err := cf.request(http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, cfError(resp.Errors, "获取 DNS 记录失败")
	}
	if len(resp.Result) == 0 {
		return nil, fmt.Errorf("未找到 DNS 记录")
	}
	return &resp.Result[0], nil
}

// createDNSRecord 创建 DNS 记录
func (cf *Cloudflare) createDNSRecord() error {
	content, recordType, err := cf.getSourceContent()
	if err != nil {
		return err
	}
	var resp CloudflareDNSRecordResponse
	path := fmt.Sprintf("/zones/%s/dns_records", cf.zoneID)
	err = cf.request(http.MethodPost, path, map[string]interface{}{
		"type":    recordType,
		"name":    cf.CDN.Domain,
		"content": content,
		"ttl":     1,
		"proxied": strings.ToLower(cf.CDN.CDNType) == "cdn",
	}, &resp)
	if err != nil {
		return err
	}
	if !resp.Success {
		return cfError(resp.Errors, "创建 DNS 记录失败")
	}
	cf.recordID = resp.Result.ID
	return nil
}

// updateDNSRecord 更新 DNS 记录
func (cf *Cloudflare) updateDNSRecord(recordID string) error {
	content, recordType, err := cf.getSourceContent()
	if err != nil {
		return err
	}
	proxied := strings.ToLower(cf.CDN.CDNType) == "cdn"
	helper.Debug(helper.LogTypeDCDN, "正在更新 DNS 记录 [域名=%s, 记录ID=%s, 类型=%s, 内容=%s, 代理=%v]",
		cf.CDN.Domain, recordID, recordType, content, proxied)
	var resp CloudflareDNSRecordResponse
	path := fmt.Sprintf("/zones/%s/dns_records/%s", cf.zoneID, recordID)
	err = cf.request(http.MethodPut, path, map[string]interface{}{
		"type":    recordType,
		"name":    cf.CDN.Domain,
		"content": content,
		"ttl":     1,
		"proxied": proxied,
	}, &resp)
	if err != nil {
		return err
	}
	if !resp.Success {
		return cfError(resp.Errors, "更新 DNS 记录失败")
	}
	return nil
}

// getSourceContent 获取源站内容和记录类型
func (cf *Cloudflare) getSourceContent() (content string, recordType string, err error) {
	if len(cf.CDN.Sources) == 0 {
		return "", "", fmt.Errorf("没有可用的源站配置")
	}

	source := cf.CDN.Sources[0]

	// 获取源站值
	if IsDynamicType(source.Type) {
		// 动态 IP，从缓存中获取
		cacheKey := helper.GetIPCacheKey(source.Type, source.Value)
		dynamicIPs := cf.Cache.GetDynamicIPs()
		content = dynamicIPs[cacheKey]
		if content == "" {
			// 如果缓存中没有，尝试获取
			addr, ok := helper.GetOrSetDynamicIPWithCache(source.Type, source.Value)
			if !ok {
				return "", "", fmt.Errorf("获取动态 IP 失败")
			}
			content = addr
		}
	} else if IsDomainType(source.Type) {
		// 域名类型
		return source.Value, "CNAME", nil
	} else {
		// 静态 IP
		content = source.Value
	}

	// 判断是 IPv4 还是 IPv6
	if strings.Contains(content, ":") {
		return content, "AAAA", nil
	}
	return content, "A", nil
}
