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
	Group  *config.DNSGroup
	Caches []*Cache
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
	otherRecords map[string]*CloudflareDNSRecord // Type -> Record
}

func (cf *Cloudflare) GetServiceName() string {
	if cf.Group == nil {
		return ""
	}
	if cf.Group.Name != "" {
		return cf.Group.Name
	}
	return cf.Group.Domain
}

// Init 初始化 Cloudflare DDNS（批量处理模式）
func (cf *Cloudflare) Init(group *config.DNSGroup, caches []*Cache) {
	cf.Group = group
	cf.Caches = caches

	if cf.Group.Domain == "" || strings.TrimSpace(cf.Group.AccessKey) == "" {
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", cf.GetServiceName())
		return
	}

	// 获取 Zone ID（同组所有记录共用，只需查询一次）
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

// UpdateOrCreateRecords 批量更新或创建 DNS 记录（一次查询，处理所有记录）
func (cf *Cloudflare) UpdateOrCreateRecords() []RecordResult {
	// 1. 预先过滤有效记录
	validRecords := make([]validRecord, 0, len(cf.Group.Records))
	cacheIdx := 0
	for i := range cf.Group.Records {
		if cf.Group.Records[i].Value != "" {
			validRecords = append(validRecords, validRecord{
				record: &cf.Group.Records[i],
				cache:  cf.Caches[cacheIdx],
			})
			cacheIdx++
		}
	}

	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	// 2. 验证配置与 Zone ID
	if cf.Group.Domain == "" || strings.TrimSpace(cf.Group.AccessKey) == "" {
		return cf.createErrorResults(validRecords, InitFailed, "配置不完整")
	}
	if cf.zoneID == "" {
		return cf.createErrorResults(validRecords, InitFailed, "Zone ID 未初始化")
	}

	// 3. 一次性查询该域名下的所有 DNS 记录
	allRecords, err := cf.getAllDNSRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", cf.GetServiceName(), err)
		return cf.createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	// 4. 将现有记录按类型分组
	existing := cf.parseExistingRecords(allRecords)

	// 5. 逐条处理
	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		result := cf.processRecord(vr.record, vr.cache, existing)
		results = append(results, result)
	}

	return results
}

// parseExistingRecords 将现有记录按类型分组（只遍历一次）
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

// createErrorResults 创建批量错误结果
func (cf *Cloudflare) createErrorResults(validRecords []validRecord, status statusType, errMsg string) []RecordResult {
	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, RecordResult{
			RecordType:    vr.record.Type,
			Status:        status,
			ShouldWebhook: false,
			ErrorMessage:  errMsg,
		})
	}
	return results
}

// processRecord 处理单条 DNS 记录
func (cf *Cloudflare) processRecord(record *config.DNSRecord, cache *Cache, existing *cloudflareRecordsByType) RecordResult {
	result := RecordResult{
		RecordType:    record.Type,
		Status:        UpdatedNothing,
		ShouldWebhook: false,
	}

	// 1. 获取当前值
	var currentValue string
	var ok bool

	switch record.Type {
	case RecordTypeA, RecordTypeAAAA:
		if IsDynamicType(record.IPType) {
			if record.IPType == helper.DynamicIPv6Interface && record.Regex != "" {
				currentValue, ok = helper.GetOrSetDynamicIPWithCacheAndRegex(record.IPType, record.Value, record.Regex)
			} else {
				currentValue, ok = helper.GetOrSetDynamicIPWithCache(record.IPType, record.Value)
			}
			if !ok {
				result.Status = InitGetIPFailed
				result.ShouldWebhook = shouldSendWebhook(cache, InitGetIPFailed)
				result.ErrorMessage = "获取 IP 失败"
				helper.Error(helper.LogTypeDDNS, "[%s] [%s] 获取 IP 失败", cf.GetServiceName(), record.Type)
				return result
			}
		} else {
			currentValue = record.Value
		}
	case RecordTypeCNAME, RecordTypeTXT:
		currentValue = record.Value
	default:
		result.Status = UpdatedFailed
		result.ErrorMessage = "不支持的记录类型"
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型: %s", cf.GetServiceName(), record.Type)
		return result
	}

	// 2. 检查值是否变化（仅对动态类型检查）
	if IsDynamicType(record.IPType) {
		cacheKey := getCacheKey(record.IPType, record.Value, record.Regex)
		valueChanged, oldValue := cache.CheckIPChanged(cacheKey, currentValue)
		forceUpdate := cache.Times <= 0

		if !valueChanged && cache.HasRun && !ForceCompareGlobal && !forceUpdate {
			result.Status = UpdatedNothing
			result.ShouldWebhook = false
			cache.Times--
			helper.Debug(helper.LogTypeDDNS, "[%s] [%s] 值未变化，跳过更新 [当前=%s, 剩余次数=%d]", cf.GetServiceName(), record.Type, currentValue, cache.Times)
			return result
		}

		if forceUpdate && !valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 达到强制更新阈值，执行更新 [值=%s]", cf.GetServiceName(), record.Type, currentValue)
		} else if valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 检测到值变化 [旧值=%s, 新值=%s]", cf.GetServiceName(), record.Type, oldValue, currentValue)
		}
	}

	// 3. 处理记录类型变更（特殊处理 CNAME 类型冲突）
	var updateErr error

	if record.Type == RecordTypeCNAME {
		totalRecords := len(existing.cnameRecords) + len(existing.otherRecords)
		if totalRecords > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有现有记录 [数量=%d]", cf.GetServiceName(), totalRecords)

			if deleteErr := cf.deleteDNSRecords(existing.cnameRecords, "CNAME"); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}

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

		helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", cf.GetServiceName())
		updateErr = cf.createDNSRecord(record.Type, currentValue)
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
				updateErr = nil
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
	if IsDynamicType(record.IPType) {
		cacheKey := getCacheKey(record.IPType, record.Value, record.Regex)
		cache.UpdateDynamicIP(cacheKey, currentValue)
	}

	cache.HasRun = true
	cache.TimesFailed = 0
	cache.ResetTimes()
	result.Status = UpdatedSuccess
	result.ShouldWebhook = shouldSendWebhook(cache, UpdatedSuccess)

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] DNS 记录更新成功 [值=%s]", cf.GetServiceName(), record.Type, currentValue)
	return result
}

// getZoneID 获取根域名对应的 Zone ID
func (cf *Cloudflare) getZoneID() (string, error) {
	rootDomain := cf.getRootDomain()

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

// getAllDNSRecords 查询指定域名的所有 DNS 记录（所有类型）
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
		"proxied": false, // DDNS 不开启代理
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

// getRootDomain 获取根域名
// 例如: www.sub.example.com -> example.com
func (cf *Cloudflare) getRootDomain() string {
	domain := cf.Group.Domain
	if strings.HasPrefix(domain, "*.") {
		domain = domain[2:]
	}
	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return domain
	}
	return parts[len(parts)-2] + "." + parts[len(parts)-1]
}

// parseTTL 解析 TTL 值（Cloudflare TTL=1 表示自动，自定义最小值 60）
func (cf *Cloudflare) parseTTL() int {
	if cf.Group.TTL == "" || cf.Group.TTL == "AUTO" {
		return 1 // Cloudflare 的 1 表示自动 TTL
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
