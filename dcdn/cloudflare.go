package dcdn

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// CloudflareZone Cloudflare Zone 信息
type CloudflareZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// CloudflareZonesResponse Zone 列表响应
type CloudflareZonesResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result []CloudflareZone `json:"result"`
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
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result []CloudflareDNSRecord `json:"result"`
}

// CloudflareDNSRecordResponse 单个 DNS 记录响应
type CloudflareDNSRecordResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result CloudflareDNSRecord `json:"result"`
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
func (cf *Cloudflare) validateConfig() bool {
	if cf.CDN == nil {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：配置对象为空")
		return false
	}
	// Cloudflare 使用 API Token，存储在 AccessKey 中
	if cf.CDN.AccessKey == "" {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：API Token 为空 [域名=%s]", cf.CDN.Domain)
		return false
	}
	// 检查域名
	if cf.CDN.Domain == "" {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：域名为空")
		return false
	}
	// 检查 CDN 类型
	if cf.CDN.CDNType == "" {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：类型为空 [域名=%s]", cf.CDN.Domain)
		return false
	}
	// 验证 CDN 类型是否合法（不区分大小写）
	cdnType := strings.ToLower(cf.CDN.CDNType)
	if cdnType != "cdn" && cdnType != "dns" {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：不支持的类型 [域名=%s, 类型=%s]，仅支持 CDN 或 DNS", cf.CDN.Domain, cf.CDN.CDNType)
		return false
	}
	// 检查源站配置
	if len(cf.CDN.Sources) == 0 {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 配置校验失败：源站配置为空 [域名=%s]", cf.CDN.Domain)
		return false
	}
	return true
}

func (cf *Cloudflare) UpdateOrCreateSources() bool {
	if cf.Status == InitFailed {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare 更新跳过：初始化失败 [域名=%s]", cf.CDN.Domain)
		return false
	}
	helper.Debug(helper.LogTypeDCDN, "开始检查 Cloudflare 源站 IP 变化 [域名=%s]", cf.CDN.Domain)

	changedIPCount, ok := cf.checkDynamicIPChanges()
	if !ok {
		return false
	}
	if changedIPCount > 0 {
		helper.Info(helper.LogTypeDCDN, "共检测到 %d 个源站 IP 发生变化 [域名=%s]", changedIPCount, cf.CDN.Domain)
	} else {
		helper.Debug(helper.LogTypeDCDN, "未检测到源站 IP 变化 [域名=%s]", cf.CDN.Domain)
	}

	if cf.shouldUpdateDNS(changedIPCount) {
		helper.Info(helper.LogTypeDCDN, "开始更新 Cloudflare DNS 记录 [域名=%s, IP变化数=%d, 计数器=%d]",
			cf.CDN.Domain, changedIPCount, cf.Cache.Times)
		cf.updateOrCreateDNSRecord()
		cf.Cache.ResetTimes()
		return true
	}
	cf.Status = UpdatedNothing
	helper.Debug(helper.LogTypeDCDN, "无需更新 DNS 记录 [域名=%s, 计数器剩余=%d]", cf.CDN.Domain, cf.Cache.Times)
	return false
}

// shouldUpdateDNS 判断是否需要更新 DNS 记录
func (cf *Cloudflare) shouldUpdateDNS(changedIPCount int) bool {
	// 第一次运行，需要初始化
	if !cf.Cache.HasRun {
		cf.Cache.HasRun = true
		helper.Info(helper.LogTypeDCDN, "首次运行，需要初始化 Cloudflare DNS 记录 [域名=%s]", cf.CDN.Domain)
		return true
	}

	// 有 IP 发生变化，需要更新
	if changedIPCount > 0 {
		helper.Info(helper.LogTypeDCDN, "源站 IP 变化，需要更新 DNS 记录 [域名=%s, 变化数=%d]",
			cf.CDN.Domain, changedIPCount)
		return true
	}

	// 递减计数器
	cf.Cache.Times--

	// 计数器归零，需要强制更新
	if cf.Cache.Times == 0 {
		helper.Info(helper.LogTypeDCDN, "定时计数器归零，强制更新 DNS 记录 [域名=%s]", cf.CDN.Domain)
		return true
	}

	// 强制更新标志
	if ForceCompareGlobal {
		helper.Info(helper.LogTypeDCDN, "强制更新标志已设置，需要更新 DNS 记录 [域名=%s]", cf.CDN.Domain)
		return true
	}

	return false
}

func (cf *Cloudflare) ShouldSendWebhook() bool {
	return cf.Status == UpdatedSuccess || cf.Status == UpdatedFailed || cf.Status == InitGetIPFailed
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

	// Trim API Token，移除可能的空格
	apiToken := strings.TrimSpace(cf.CDN.AccessKey)
	if apiToken == "" {
		return "", fmt.Errorf("API Token 为空")
	}

	url := fmt.Sprintf("%s/zones?name=%s", cloudflareAPIEndpoint, rootDomain)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	helper.Debug(helper.LogTypeDCDN, "正在获取 Zone ID [域名=%s, 根域名=%s]", cf.CDN.Domain, rootDomain)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	// 记录响应状态码
	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare API 响应状态码异常 [状态码=%d, 响应=%s]", resp.StatusCode, string(body))
	}

	var zonesResp CloudflareZonesResponse
	if err := json.Unmarshal(body, &zonesResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %v, 响应内容: %s", err, string(body))
	}

	if !zonesResp.Success {
		if len(zonesResp.Errors) > 0 {
			return "", fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", zonesResp.Errors[0].Code, zonesResp.Errors[0].Message)
		}
		return "", fmt.Errorf("API 请求失败，响应: %s", string(body))
	}

	if len(zonesResp.Result) == 0 {
		return "", fmt.Errorf("未找到域名 %s 对应的 Zone，请确认域名已添加到 Cloudflare", rootDomain)
	}

	helper.Debug(helper.LogTypeDCDN, "成功获取 Zone ID [Zone ID=%s, 域名=%s]", zonesResp.Result[0].ID, rootDomain)
	return zonesResp.Result[0].ID, nil
}

// getDNSRecord 获取 DNS 记录
func (cf *Cloudflare) getDNSRecord() (*CloudflareDNSRecord, error) {
	apiToken := strings.TrimSpace(cf.CDN.AccessKey)
	url := fmt.Sprintf("%s/zones/%s/dns_records?name=%s", cloudflareAPIEndpoint, cf.zoneID, cf.CDN.Domain)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDCDN, "获取 DNS 记录响应异常 [状态码=%d, 响应=%s]", resp.StatusCode, string(body))
	}

	var recordsResp CloudflareDNSRecordsResponse
	if err := json.Unmarshal(body, &recordsResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if !recordsResp.Success {
		if len(recordsResp.Errors) > 0 {
			return nil, fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordsResp.Errors[0].Code, recordsResp.Errors[0].Message)
		}
		return nil, fmt.Errorf("获取 DNS 记录失败")
	}

	if len(recordsResp.Result) == 0 {
		return nil, fmt.Errorf("未找到 DNS 记录")
	}

	return &recordsResp.Result[0], nil
}

// createDNSRecord 创建 DNS 记录
func (cf *Cloudflare) createDNSRecord() error {
	// 获取第一个源站的 IP
	content, recordType, err := cf.getSourceContent()
	if err != nil {
		return err
	}

	// 根据 CDNType 决定是否开启代理
	// CDN 类型：开启代理（使用 Cloudflare CDN）
	// DNS 类型：仅 DNS，不开启代理
	proxied := strings.ToLower(cf.CDN.CDNType) == "cdn"

	data := map[string]interface{}{
		"type":    recordType,
		"name":    cf.CDN.Domain,
		"content": content,
		"ttl":     1, // Auto TTL
		"proxied": proxied,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	apiToken := strings.TrimSpace(cf.CDN.AccessKey)
	url := fmt.Sprintf("%s/zones/%s/dns_records", cloudflareAPIEndpoint, cf.zoneID)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDCDN, "创建 DNS 记录响应异常 [状态码=%d, 响应=%s]", resp.StatusCode, string(body))
	}

	var recordResp CloudflareDNSRecordResponse
	if err := json.Unmarshal(body, &recordResp); err != nil {
		return fmt.Errorf("解析响应失败: %v", err)
	}

	if !recordResp.Success {
		if len(recordResp.Errors) > 0 {
			return fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordResp.Errors[0].Code, recordResp.Errors[0].Message)
		}
		return fmt.Errorf("创建 DNS 记录失败")
	}

	cf.recordID = recordResp.Result.ID
	return nil
}

// updateDNSRecord 更新 DNS 记录
func (cf *Cloudflare) updateDNSRecord(recordID string) error {
	// 获取源站内容
	content, recordType, err := cf.getSourceContent()
	if err != nil {
		return err
	}

	// 根据 CDNType 决定是否开启代理
	// CDN 类型：开启代理（使用 Cloudflare CDN）
	// DNS 类型：仅 DNS，不开启代理
	proxied := strings.ToLower(cf.CDN.CDNType) == "cdn"

	// Trim API Token
	apiToken := strings.TrimSpace(cf.CDN.AccessKey)
	if apiToken == "" {
		return fmt.Errorf("API Token 为空")
	}

	data := map[string]interface{}{
		"type":    recordType,
		"name":    cf.CDN.Domain,
		"content": content,
		"ttl":     1, // Auto TTL
		"proxied": proxied,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", cloudflareAPIEndpoint, cf.zoneID, recordID)
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	helper.Debug(helper.LogTypeDCDN, "正在更新 DNS 记录 [域名=%s, 记录ID=%s, 类型=%s, 内容=%s, 代理=%v]", cf.CDN.Domain, recordID, recordType, content, proxied)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("更新 DNS 记录请求失败 [错误=%v]", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败 [错误=%v]", err)
	}

	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDCDN, "Cloudflare API 响应状态码异常 [状态码=%d, 响应=%s]", resp.StatusCode, string(body))
	}

	var recordResp CloudflareDNSRecordResponse
	if err := json.Unmarshal(body, &recordResp); err != nil {
		return fmt.Errorf("解析响应失败 [错误=%v, 响应=%s]", err, string(body))
	}

	if !recordResp.Success {
		if len(recordResp.Errors) > 0 {
			return fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordResp.Errors[0].Code, recordResp.Errors[0].Message)
		}
		return fmt.Errorf("更新 DNS 记录失败")
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
