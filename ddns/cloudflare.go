package ddns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

const cloudflareAPIEndpoint = "https://api.cloudflare.com/client/v4"

type Cloudflare struct {
	BaseDNSProvider
	zoneID string // Zone ID 缓存（同组所有记录共享）
}

// CloudflareAPIError Cloudflare API 错误
type CloudflareAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CloudflareAPIMessage Cloudflare API 消息
type CloudflareAPIMessage struct {
	Message          string `json:"message"`
	DocumentationURL string `json:"documentation_url,omitempty"`
}

// CloudflareZonesResponse Zone 列表响应
type CloudflareZonesResponse struct {
	Success  bool                   `json:"success"`
	Errors   []CloudflareAPIError   `json:"errors"`
	Messages []CloudflareAPIMessage `json:"messages"`
	Result   []CloudflareZoneResult `json:"result"`
}

// CloudflareZoneResult Zone 信息
type CloudflareZoneResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CloudflareDNSRecord DNS 记录
type CloudflareDNSRecord struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Content    string                 `json:"content"`
	TTL        int                    `json:"ttl"`
	Proxied    bool                   `json:"proxied"`
	Proxiable  bool                   `json:"proxiable,omitempty"`
	Settings   map[string]interface{} `json:"settings,omitempty"`
	Meta       map[string]interface{} `json:"meta,omitempty"`
	Comment    *string                `json:"comment,omitempty"`
	Tags       []string               `json:"tags,omitempty"`
	CreatedOn  string                 `json:"created_on,omitempty"`
	ModifiedOn string                 `json:"modified_on,omitempty"`
}

// CloudflareDNSRecordResponse 单条 DNS 记录响应
type CloudflareDNSRecordResponse struct {
	Success    bool                   `json:"success"`
	Errors     []CloudflareAPIError   `json:"errors"`
	Messages   []CloudflareAPIMessage `json:"messages"`
	Result     *CloudflareDNSRecord   `json:"result,omitempty"`
	ResultInfo *CloudflareResultInfo  `json:"result_info,omitempty"`
}

// CloudflareDNSRecordsResponse DNS 记录列表响应
type CloudflareDNSRecordsResponse struct {
	Success    bool                   `json:"success"`
	Errors     []CloudflareAPIError   `json:"errors"`
	Messages   []CloudflareAPIMessage `json:"messages"`
	Result     []CloudflareDNSRecord  `json:"result"`
	ResultInfo *CloudflareResultInfo  `json:"result_info,omitempty"`
}

// CloudflareResultInfo 分页信息
type CloudflareResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
}

// cloudflareRecordsByType 按类型分组的 Cloudflare 记录映射
type cloudflareRecordsByType struct {
	cnameRecords []CloudflareDNSRecord
	otherRecords map[string]*CloudflareDNSRecord
}

// Init 初始化 Cloudflare DDNS（需额外获取 Zone ID，且仅用 AccessKey）
func (cf *Cloudflare) Init(group *config.DNSGroup, caches []*Cache) {
	cf.Group = group
	cf.Caches = caches

	if cf.Group.Domain == "" || strings.TrimSpace(cf.Group.AccessKey) == "" {
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", cf.GetServiceName())
		return
	}

	zoneID, err := cf.getZoneID()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 获取 Zone ID 失败: %v", cf.GetServiceName(), err)
		return
	}
	cf.zoneID = zoneID

	if len(cf.Caches) > 0 && !cf.Caches[0].HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功，共 %d 条记录 [ZoneID=%s]", cf.GetServiceName(), len(cf.Caches), zoneID)
	}
}

// UpdateOrCreateRecords 批量更新或创建 DNS 记录
func (cf *Cloudflare) UpdateOrCreateRecords() []RecordResult {
	validRecords := filterValidRecords(cf.Group, cf.Caches)
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	if cf.Group.Domain == "" || strings.TrimSpace(cf.Group.AccessKey) == "" {
		return createErrorResults(validRecords, InitFailed, "配置不完整")
	}
	if cf.zoneID == "" {
		return createErrorResults(validRecords, InitFailed, "Zone ID 未初始化")
	}

	allRecords, err := cf.getAllDNSRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", cf.GetServiceName(), err)
		return createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	existing := cf.parseExistingRecords(allRecords)

	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, cf.processRecord(vr.record, vr.cache, existing))
	}
	return results
}

// parseExistingRecords 将现有记录按类型分组
func (cf *Cloudflare) parseExistingRecords(allRecords []CloudflareDNSRecord) *cloudflareRecordsByType {
	existing := &cloudflareRecordsByType{
		cnameRecords: make([]CloudflareDNSRecord, 0),
		otherRecords: make(map[string]*CloudflareDNSRecord),
	}
	for i := range allRecords {
		if allRecords[i].Type == RecordTypeCNAME {
			existing.cnameRecords = append(existing.cnameRecords, allRecords[i])
		} else {
			if _, exists := existing.otherRecords[allRecords[i].Type]; !exists {
				rec := allRecords[i]
				existing.otherRecords[allRecords[i].Type] = &rec
			}
		}
	}
	return existing
}

// processRecord 处理单条 DNS 记录
func (cf *Cloudflare) processRecord(record *config.DNSRecord, cache *Cache, existing *cloudflareRecordsByType) RecordResult {
	// 1. 获取当前值
	currentValue, result, ok := getCurrentValue(cf.GetServiceName(), record, cache)
	if !ok {
		return result
	}

	// 2. 检查缓存
	if skip, r := checkDynamicCache(cf.GetServiceName(), record, cache, currentValue); skip {
		return r
	}

	// 3. 处理记录类型变更
	var updateErr error

	if record.Type == RecordTypeCNAME {
		if len(existing.otherRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有非 CNAME 记录 [数量=%d]", cf.GetServiceName(), len(existing.otherRecords))
			for _, rec := range existing.otherRecords {
				if deleteErr := cf.deleteDNSRecord(rec.ID); deleteErr != nil {
					result.Status = UpdatedFailed
					result.ErrorMessage = deleteErr.Error()
					result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
					helper.Error(helper.LogTypeDDNS, "[%s] [CNAME] 删除 DNS 记录失败 [RecordID=%s, 类型=%s, 错误=%v]", cf.GetServiceName(), rec.ID, rec.Type, deleteErr)
					return result
				}
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 已删除 DNS 记录 [RecordID=%s, 类型=%s, 值=%s]", cf.GetServiceName(), rec.ID, rec.Type, rec.Content)
			}
		}

		if len(existing.cnameRecords) > 0 {
			existingCNAME := existing.cnameRecords[0]
			if existingCNAME.Content == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 记录值未变化，无需更新 [RecordID=%s, 值=%s]", cf.GetServiceName(), existingCNAME.ID, currentValue)
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 更新已有 CNAME 记录 [RecordID=%s, 旧值=%s]", cf.GetServiceName(), existingCNAME.ID, existingCNAME.Content)
				updateErr = cf.updateDNSRecord(existingCNAME.ID, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", cf.GetServiceName())
			updateErr = cf.createDNSRecord(record.Type, currentValue)
		}
	} else {
		if len(existing.cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", cf.GetServiceName(), record.Type, record.Type, len(existing.cnameRecords))
			if deleteErr := cf.deleteDNSRecords(existing.cnameRecords, record.Type); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}
		}

		targetRecord := existing.otherRecords[record.Type]
		if targetRecord != nil {
			if targetRecord.Content == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录值未变化，无需更新 [RecordID=%s, 值=%s]", cf.GetServiceName(), record.Type, targetRecord.ID, currentValue)
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录已存在 [RecordID=%s, 旧值=%s]", cf.GetServiceName(), record.Type, targetRecord.ID, targetRecord.Content)
				updateErr = cf.updateDNSRecord(targetRecord.ID, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录不存在，创建新记录", cf.GetServiceName(), record.Type)
			updateErr = cf.createDNSRecord(record.Type, currentValue)
		}
	}

	if updateErr != nil {
		result.Status = UpdatedFailed
		result.ErrorMessage = updateErr.Error()
		result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
		return result
	}

	// 4. 更新缓存
	finalizeSuccess(cf.GetServiceName(), record, cache, currentValue, &result)
	return result
}

// getZoneID 获取根域名对应的 Zone ID
func (cf *Cloudflare) getZoneID() (string, error) {
	rootDomain := getRootDomain(cf.Group.Domain)
	helper.Debug(helper.LogTypeDDNS, "[%s] 正在获取 Zone ID [域名=%s, 根域名=%s]", cf.GetServiceName(), cf.Group.Domain, rootDomain)

	var zonesResp CloudflareZonesResponse
	if err := cf.request("GET", fmt.Sprintf("/zones?name=%s", rootDomain), nil, &zonesResp); err != nil {
		return "", err
	}

	if !zonesResp.Success {
		if len(zonesResp.Errors) > 0 {
			return "", fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", zonesResp.Errors[0].Code, zonesResp.Errors[0].Message)
		}
		return "", fmt.Errorf("获取 Zone ID 失败")
	}

	if len(zonesResp.Result) == 0 {
		return "", fmt.Errorf("未找到域名 %s 对应的 Zone", rootDomain)
	}
	return zonesResp.Result[0].ID, nil
}

// getAllDNSRecords 查询指定域名的所有 DNS 记录
func (cf *Cloudflare) getAllDNSRecords() ([]CloudflareDNSRecord, error) {
	helper.Debug(helper.LogTypeDDNS, "[%s] 正在查询所有 DNS 记录 [域名=%s]", cf.GetServiceName(), cf.Group.Domain)

	var recordsResp CloudflareDNSRecordsResponse
	if err := cf.request("GET", fmt.Sprintf("/zones/%s/dns_records?name=%s", cf.zoneID, cf.Group.Domain), nil, &recordsResp); err != nil {
		return nil, err
	}

	if !recordsResp.Success {
		if len(recordsResp.Errors) > 0 {
			return nil, fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordsResp.Errors[0].Code, recordsResp.Errors[0].Message)
		}
		return nil, fmt.Errorf("查询 DNS 记录失败")
	}
	return recordsResp.Result, nil
}

// createDNSRecord 创建 DNS 记录
func (cf *Cloudflare) createDNSRecord(recordType, content string) error {
	data := map[string]interface{}{
		"type":    recordType,
		"name":    cf.Group.Domain,
		"content": content,
		"ttl":     cf.parseTTL(),
		"proxied": false,
	}

	var recordResp CloudflareDNSRecordResponse
	if err := cf.request("POST", fmt.Sprintf("/zones/%s/dns_records", cf.zoneID), data, &recordResp); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录失败 [错误=%v]", cf.GetServiceName(), recordType, err)
		return err
	}

	if !recordResp.Success {
		if len(recordResp.Errors) > 0 {
			return fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordResp.Errors[0].Code, recordResp.Errors[0].Message)
		}
		return fmt.Errorf("创建 DNS 记录失败")
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录成功 [值=%s]", cf.GetServiceName(), recordType, content)
	return nil
}

// updateDNSRecord 更新 DNS 记录
func (cf *Cloudflare) updateDNSRecord(recordID, recordType, content string) error {
	data := map[string]interface{}{
		"type":    recordType,
		"name":    cf.Group.Domain,
		"content": content,
		"ttl":     cf.parseTTL(),
		"proxied": false,
	}

	var recordResp CloudflareDNSRecordResponse
	if err := cf.request("PUT", fmt.Sprintf("/zones/%s/dns_records/%s", cf.zoneID, recordID), data, &recordResp); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录失败 [RecordID=%s, 错误=%v]", cf.GetServiceName(), recordType, recordID, err)
		return err
	}

	if !recordResp.Success {
		if len(recordResp.Errors) > 0 {
			return fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordResp.Errors[0].Code, recordResp.Errors[0].Message)
		}
		return fmt.Errorf("更新 DNS 记录失败")
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录成功 [RecordID=%s, 新值=%s]", cf.GetServiceName(), recordType, recordID, content)
	return nil
}

// deleteDNSRecords 批量删除 DNS 记录
func (cf *Cloudflare) deleteDNSRecords(records []CloudflareDNSRecord, contextType string) error {
	for _, rec := range records {
		if deleteErr := cf.deleteDNSRecord(rec.ID); deleteErr != nil {
			helper.Error(helper.LogTypeDDNS, "[%s] [%s] 删除 DNS 记录失败 [RecordID=%s, 类型=%s, 错误=%v]", cf.GetServiceName(), contextType, rec.ID, rec.Type, deleteErr)
			return deleteErr
		}
		helper.Info(helper.LogTypeDDNS, "[%s] [%s] 已删除 DNS 记录 [RecordID=%s, 类型=%s, 值=%s]", cf.GetServiceName(), contextType, rec.ID, rec.Type, rec.Content)
	}
	return nil
}

// deleteDNSRecord 删除 DNS 记录
func (cf *Cloudflare) deleteDNSRecord(recordID string) error {
	var recordResp CloudflareDNSRecordResponse
	if err := cf.request("DELETE", fmt.Sprintf("/zones/%s/dns_records/%s", cf.zoneID, recordID), nil, &recordResp); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordID=%s, 错误=%v]", cf.GetServiceName(), recordID, err)
		return err
	}

	if !recordResp.Success {
		if len(recordResp.Errors) > 0 {
			return fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordResp.Errors[0].Code, recordResp.Errors[0].Message)
		}
		return fmt.Errorf("删除 DNS 记录失败")
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 删除 DNS 记录成功 [RecordID=%s]", cf.GetServiceName(), recordID)
	return nil
}

// request 统一请求方法
func (cf *Cloudflare) request(method, urlPath string, body interface{}, result interface{}) error {
	reqURL := cloudflareAPIEndpoint + urlPath

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求数据失败: %v", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, reqURL, reqBody)
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cf.Group.AccessKey))
	req.Header.Set("Content-Type", "application/json")

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	helper.Debug(helper.LogTypeDDNS, "[%s] API 响应 [状态码=%d, 长度=%d]", cf.GetServiceName(), resp.StatusCode, len(responseBody))

	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDDNS, "[%s] API 响应状态码异常 [状态码=%d, 响应=%s]", cf.GetServiceName(), resp.StatusCode, string(responseBody))
		return fmt.Errorf("请求失败 [状态码=%d]", resp.StatusCode)
	}

	if err := json.Unmarshal(responseBody, result); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 解析响应失败: %v", cf.GetServiceName(), err)
		helper.Debug(helper.LogTypeDDNS, "[%s] 响应内容: %s", cf.GetServiceName(), string(responseBody))
		return fmt.Errorf("解析响应失败: %v", err)
	}
	return nil
}

// parseTTL 解析 TTL 值（Cloudflare TTL=1 表示自动，自定义最小值 60）
func (cf *Cloudflare) parseTTL() int {
	if cf.Group.TTL == "" || cf.Group.TTL == "AUTO" {
		return 1
	}

	if ttl, err := strconv.Atoi(cf.Group.TTL); err == nil {
		if ttl > 1 && ttl < 60 {
			return 60
		}
		return ttl
	}

	ttlStr := strings.ToLower(cf.Group.TTL)
	if strings.HasSuffix(ttlStr, "m") {
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			seconds := ttl * 60
			if seconds < 60 {
				return 60
			}
			return seconds
		}
	} else if strings.HasSuffix(ttlStr, "h") {
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			return ttl * 3600
		}
	}
	return 1
}
