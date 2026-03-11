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
	"github.com/cxbdasheng/dnet/signer"
)

// 华为云 DNS API Endpoint (根据区域可能不同,这里使用华北-北京四)
// 完整的区域列表: https://developer.huaweicloud.com/endpoint?DNS
const huaweiDNSEndpoint = "https://dns.myhuaweicloud.com"

type Huawei struct {
	Group  *config.DNSGroup
	Caches []*Cache
	zoneID string // Zone ID 缓存（同组所有记录共享）
}

// HuaweiZone 华为云 Zone 结构
type HuaweiZone struct {
	ID       string `json:"id"`
	Name     string `json:"name"`      // 域名
	ZoneType string `json:"zone_type"` // public/private
	Email    string `json:"email"`
	TTL      int    `json:"ttl"`
	Serial   int    `json:"serial"`
}

// HuaweiZonesResponse 查询 Zone 列表响应
type HuaweiZonesResponse struct {
	Zones    []HuaweiZone      `json:"zones"`
	Links    map[string]string `json:"links"`
	Metadata HuaweiMetadata    `json:"metadata"`
}

// HuaweiMetadata 分页元数据
type HuaweiMetadata struct {
	TotalCount int `json:"total_count"`
}

// HuaweiRecordSet 华为云 DNS 记录集
type HuaweiRecordSet struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`        // 完整域名 (如 www.example.com.)
	Description string   `json:"description"` // 描述
	ZoneID      string   `json:"zone_id"`     // Zone ID
	ZoneName    string   `json:"zone_name"`   // Zone 名称
	Type        string   `json:"type"`        // 记录类型 A/AAAA/CNAME/TXT
	TTL         int      `json:"ttl"`         // TTL 值
	Records     []string `json:"records"`     // 记录值列表
	Status      string   `json:"status"`      // 状态
	Line        string   `json:"line"`        // 解析线路
	Weight      *int     `json:"weight"`      // 权重
	CreateAt    string   `json:"create_at"`   // 创建时间
	UpdateAt    string   `json:"update_at"`   // 更新时间
}

// HuaweiRecordSetsResponse 查询记录集列表响应
type HuaweiRecordSetsResponse struct {
	Recordsets []HuaweiRecordSet `json:"recordsets"`
	Links      map[string]string `json:"links"`
	Metadata   HuaweiMetadata    `json:"metadata"`
}

// huaweiRecordsByType 按类型分组的华为云记录映射
type huaweiRecordsByType struct {
	cnameRecords []HuaweiRecordSet
	otherRecords map[string]*HuaweiRecordSet // Type -> RecordSet
}

func (h *Huawei) GetServiceName() string {
	if h.Group == nil {
		return ""
	}
	if h.Group.Name != "" {
		return h.Group.Name
	}
	return h.Group.Domain
}

// Init 初始化华为云 DDNS（批量处理模式）
func (h *Huawei) Init(group *config.DNSGroup, caches []*Cache) {
	h.Group = group
	h.Caches = caches

	if h.Group.Domain == "" || h.Group.AccessKey == "" || h.Group.AccessSecret == "" {
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", h.GetServiceName())
		return
	}

	// 获取 Zone ID（同组所有记录共用，只需查询一次）
	zoneID, err := h.getZoneID()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 获取 Zone ID 失败: %v", h.GetServiceName(), err)
		return
	}
	h.zoneID = zoneID

	if len(h.Caches) > 0 && !h.Caches[0].HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功，共 %d 条记录 [ZoneID=%s]", h.GetServiceName(), len(h.Caches), zoneID)
	}
}

// UpdateOrCreateRecords 批量更新或创建 DNS 记录（一次查询，处理所有记录）
func (h *Huawei) UpdateOrCreateRecords() []RecordResult {
	// 1. 预先过滤有效记录
	validRecords := make([]validRecord, 0, len(h.Group.Records))
	cacheIdx := 0
	for i := range h.Group.Records {
		if h.Group.Records[i].Value != "" {
			validRecords = append(validRecords, validRecord{
				record: &h.Group.Records[i],
				cache:  h.Caches[cacheIdx],
			})
			cacheIdx++
		}
	}

	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	// 2. 验证配置与 Zone ID
	if h.Group.Domain == "" || h.Group.AccessKey == "" || h.Group.AccessSecret == "" {
		return h.createErrorResults(validRecords, InitFailed, "配置不完整")
	}
	if h.zoneID == "" {
		return h.createErrorResults(validRecords, InitFailed, "Zone ID 未初始化")
	}

	// 3. 一次性查询该子域下的所有 DNS 记录
	allRecords, err := h.describeAllRecordSets()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", h.GetServiceName(), err)
		return h.createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	// 4. 将现有记录按类型分组
	existing := h.parseExistingRecords(allRecords)

	// 5. 逐条处理
	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		result := h.processRecord(vr.record, vr.cache, existing)
		results = append(results, result)
	}

	return results
}

// parseExistingRecords 将现有记录按类型分组（只遍历一次）
func (h *Huawei) parseExistingRecords(allRecords []HuaweiRecordSet) *huaweiRecordsByType {
	existing := &huaweiRecordsByType{
		cnameRecords: make([]HuaweiRecordSet, 0),
		otherRecords: make(map[string]*HuaweiRecordSet),
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
func (h *Huawei) createErrorResults(validRecords []validRecord, status statusType, errMsg string) []RecordResult {
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
func (h *Huawei) processRecord(record *config.DNSRecord, cache *Cache, existing *huaweiRecordsByType) RecordResult {
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
				helper.Error(helper.LogTypeDDNS, "[%s] [%s] 获取 IP 失败", h.GetServiceName(), record.Type)
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
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型: %s", h.GetServiceName(), record.Type)
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
			helper.Debug(helper.LogTypeDDNS, "[%s] [%s] 值未变化，跳过更新 [当前=%s, 剩余次数=%d]", h.GetServiceName(), record.Type, currentValue, cache.Times)
			return result
		}

		if forceUpdate && !valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 达到强制更新阈值，执行更新 [值=%s]", h.GetServiceName(), record.Type, currentValue)
		} else if valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 检测到值变化 [旧值=%s, 新值=%s]", h.GetServiceName(), record.Type, oldValue, currentValue)
		}
	}

	// 3. 处理记录类型变更（特殊处理 CNAME 类型冲突）
	var updateErr error

	if record.Type == RecordTypeCNAME {
		totalRecords := len(existing.cnameRecords) + len(existing.otherRecords)
		if totalRecords > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有现有记录 [数量=%d]", h.GetServiceName(), totalRecords)

			if deleteErr := h.deleteRecordSets(existing.cnameRecords, "CNAME"); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}

			for _, rec := range existing.otherRecords {
				if deleteErr := h.deleteRecordSet(rec.ID); deleteErr != nil {
					result.Status = UpdatedFailed
					result.ErrorMessage = deleteErr.Error()
					result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
					helper.Error(helper.LogTypeDDNS, "[%s] [CNAME] 删除 DNS 记录失败 [RecordID=%s, 类型=%s, 错误=%v]", h.GetServiceName(), rec.ID, rec.Type, deleteErr)
					return result
				}
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 已删除 DNS 记录 [RecordID=%s, 类型=%s, 值=%v]", h.GetServiceName(), rec.ID, rec.Type, rec.Records)
			}
		}

		helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", h.GetServiceName())
		updateErr = h.createRecordSet(record.Type, currentValue)
	} else {
		if len(existing.cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", h.GetServiceName(), record.Type, record.Type, len(existing.cnameRecords))
			if deleteErr := h.deleteRecordSets(existing.cnameRecords, record.Type); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}
		}

		targetRecord := existing.otherRecords[record.Type]
		if targetRecord != nil {
			if len(targetRecord.Records) == 1 && targetRecord.Records[0] == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录值未变化，无需更新 [RecordID=%s, 值=%s]", h.GetServiceName(), record.Type, targetRecord.ID, currentValue)
				updateErr = nil
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录已存在 [RecordID=%s, 旧值=%v]", h.GetServiceName(), record.Type, targetRecord.ID, targetRecord.Records)
				updateErr = h.updateRecordSet(targetRecord.ID, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录不存在，创建新记录", h.GetServiceName(), record.Type)
			updateErr = h.createRecordSet(record.Type, currentValue)
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

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] DNS 记录更新成功 [值=%s]", h.GetServiceName(), record.Type, currentValue)
	return result
}

// getZoneID 获取根域名对应的 Zone ID
func (h *Huawei) getZoneID() (string, error) {
	rootDomain := h.getRootDomain()

	// 华为云 Zone 名称需要带尾部 "."
	zoneName := rootDomain
	if !strings.HasSuffix(zoneName, ".") {
		zoneName += "."
	}

	helper.Debug(helper.LogTypeDDNS, "[%s] 正在获取 Zone ID [域名=%s, 根域名=%s]", h.GetServiceName(), h.Group.Domain, rootDomain)

	var zonesResp HuaweiZonesResponse
	err := h.request(http.MethodGet, fmt.Sprintf("/v2/zones?name=%s&type=public", zoneName), nil, &zonesResp)
	if err != nil {
		return "", err
	}

	if len(zonesResp.Zones) == 0 {
		return "", fmt.Errorf("未找到域名 %s 对应的 Zone", rootDomain)
	}

	return zonesResp.Zones[0].ID, nil
}

// describeAllRecordSets 查询指定域名的所有 DNS 记录（所有类型）
func (h *Huawei) describeAllRecordSets() ([]HuaweiRecordSet, error) {
	// 华为云 DNS 的完整域名需要带尾部 "."
	fullDomain := h.Group.Domain
	if !strings.HasSuffix(fullDomain, ".") {
		fullDomain += "."
	}

	helper.Debug(helper.LogTypeDDNS, "[%s] 正在查询所有 DNS 记录 [域名=%s]", h.GetServiceName(), fullDomain)

	var recordsResp HuaweiRecordSetsResponse
	err := h.request(http.MethodGet, fmt.Sprintf("/v2/zones/%s/recordsets?name=%s", h.zoneID, fullDomain), nil, &recordsResp)
	if err != nil {
		return nil, err
	}

	return recordsResp.Recordsets, nil
}

// createRecordSet 创建 DNS 记录
func (h *Huawei) createRecordSet(recordType, value string) error {
	fullDomain := h.Group.Domain
	if !strings.HasSuffix(fullDomain, ".") {
		fullDomain += "."
	}

	data := map[string]interface{}{
		"name":    fullDomain,
		"type":    recordType,
		"ttl":     h.parseTTL(),
		"records": []string{value},
	}

	var recordResp HuaweiRecordSet
	err := h.request(http.MethodPost, fmt.Sprintf("/v2/zones/%s/recordsets", h.zoneID), data, &recordResp)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录失败 [域名=%s, 错误=%v]", h.GetServiceName(), recordType, h.Group.Domain, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录成功 [RecordID=%s, 值=%s]", h.GetServiceName(), recordType, recordResp.ID, value)
	return nil
}

// updateRecordSet 更新 DNS 记录
func (h *Huawei) updateRecordSet(recordID, recordType, value string) error {
	data := map[string]interface{}{
		"records": []string{value},
		"ttl":     h.parseTTL(),
	}

	var recordResp HuaweiRecordSet
	err := h.request(http.MethodPut, fmt.Sprintf("/v2/zones/%s/recordsets/%s", h.zoneID, recordID), data, &recordResp)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录失败 [RecordID=%s, 错误=%v]", h.GetServiceName(), recordType, recordID, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录成功 [RecordID=%s, 新值=%s]", h.GetServiceName(), recordType, recordID, value)
	return nil
}

// deleteRecordSets 批量删除 DNS 记录
func (h *Huawei) deleteRecordSets(records []HuaweiRecordSet, contextType string) error {
	for _, rec := range records {
		if deleteErr := h.deleteRecordSet(rec.ID); deleteErr != nil {
			helper.Error(helper.LogTypeDDNS, "[%s] [%s] 删除 DNS 记录失败 [RecordID=%s, 类型=%s, 错误=%v]", h.GetServiceName(), contextType, rec.ID, rec.Type, deleteErr)
			return deleteErr
		}
		helper.Info(helper.LogTypeDDNS, "[%s] [%s] 已删除 DNS 记录 [RecordID=%s, 类型=%s, 值=%v]", h.GetServiceName(), contextType, rec.ID, rec.Type, rec.Records)
	}
	return nil
}

// deleteRecordSet 删除 DNS 记录
func (h *Huawei) deleteRecordSet(recordID string) error {
	var recordResp HuaweiRecordSet
	err := h.request(http.MethodDelete, fmt.Sprintf("/v2/zones/%s/recordsets/%s", h.zoneID, recordID), nil, &recordResp)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordID=%s, 错误=%v]", h.GetServiceName(), recordID, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 删除 DNS 记录成功 [RecordID=%s]", h.GetServiceName(), recordID)
	return nil
}

// request 统一请求方法
func (h *Huawei) request(method, path string, body interface{}, result interface{}) error {
	reqURL := huaweiDNSEndpoint + path

	var reqBody []byte
	var err error
	bodyStr := ""

	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求数据失败: %v", err)
		}
		bodyStr = string(reqBody)
	}

	var req *http.Request
	if reqBody != nil {
		req, err = http.NewRequest(method, reqURL, bytes.NewBuffer(reqBody))
	} else {
		req, err = http.NewRequest(method, reqURL, nil)
	}
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	uri := req.URL.Path
	query := req.URL.RawQuery

	signHeaders := map[string]string{
		"host":         req.Host,
		"content-type": "application/json",
	}

	timestamp := signer.GetFormattedTime()
	signHeaders[signer.HeaderXDate] = timestamp
	req.Header.Set(signer.HeaderXDate, timestamp)

	authHeaders := signer.HuaweiSigner(
		h.Group.AccessKey,
		h.Group.AccessSecret,
		method,
		uri,
		query,
		signHeaders,
		bodyStr,
	)

	for k, v := range authHeaders {
		req.Header.Set(k, v)
	}

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

	helper.Debug(helper.LogTypeDDNS, "[%s] API 响应 [状态码=%d, 长度=%d]", h.GetServiceName(), resp.StatusCode, len(responseBody))

	// 华为云 API 返回 2xx 表示成功
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		helper.Warn(helper.LogTypeDDNS, "[%s] API 响应状态码异常 [状态码=%d, 响应=%s]", h.GetServiceName(), resp.StatusCode, string(responseBody))
		return fmt.Errorf("请求失败 [状态码=%d]: %s", resp.StatusCode, string(responseBody))
	}

	if result != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, result); err != nil {
			helper.Error(helper.LogTypeDDNS, "[%s] 解析响应失败: %v", h.GetServiceName(), err)
			helper.Debug(helper.LogTypeDDNS, "[%s] 响应内容: %s", h.GetServiceName(), string(responseBody))
			return fmt.Errorf("解析响应失败: %v", err)
		}
	}

	return nil
}

// getRootDomain 获取根域名
// 例如: www.sub.example.com -> example.com
func (h *Huawei) getRootDomain() string {
	domain := h.Group.Domain
	if strings.HasPrefix(domain, "*.") {
		domain = domain[2:]
	}
	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return domain
	}
	return parts[len(parts)-2] + "." + parts[len(parts)-1]
}

// parseTTL 解析 TTL 值（华为云最小值 300 秒）
func (h *Huawei) parseTTL() int {
	if h.Group.TTL == "" || h.Group.TTL == "AUTO" {
		return 300
	}

	clamp := func(v int) int {
		if v < 300 {
			return 300
		}
		return v
	}

	if ttl, err := strconv.Atoi(h.Group.TTL); err == nil {
		return clamp(ttl)
	}

	ttlStr := strings.ToLower(h.Group.TTL)
	if strings.HasSuffix(ttlStr, "s") {
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			return clamp(ttl)
		}
	} else if strings.HasSuffix(ttlStr, "m") {
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			return clamp(ttl * 60)
		}
	} else if strings.HasSuffix(ttlStr, "h") {
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			return ttl * 3600
		}
	}

	return 300
}
