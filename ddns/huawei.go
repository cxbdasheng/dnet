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
	BaseDNSProvider
	zoneID string // Zone ID 缓存（同组所有记录共享）
}

// HuaweiZone 华为云 Zone 结构
type HuaweiZone struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ZoneType string `json:"zone_type"`
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
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ZoneID      string   `json:"zone_id"`
	ZoneName    string   `json:"zone_name"`
	Type        string   `json:"type"`
	TTL         int      `json:"ttl"`
	Records     []string `json:"records"`
	Status      string   `json:"status"`
	Line        string   `json:"line"`
	Weight      *int     `json:"weight"`
	CreateAt    string   `json:"create_at"`
	UpdateAt    string   `json:"update_at"`
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
	otherRecords map[string]*HuaweiRecordSet
}

// Init 初始化华为云 DDNS（需额外获取 Zone ID）
func (h *Huawei) Init(group *config.DNSGroup, caches []*Cache) {
	if !h.initConfig(group, caches) {
		return
	}

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

// UpdateOrCreateRecords 批量更新或创建 DNS 记录
func (h *Huawei) UpdateOrCreateRecords() []RecordResult {
	validRecords := filterValidRecords(h.Group, h.Caches)
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	if h.Group.Domain == "" || h.Group.AccessKey == "" || h.Group.AccessSecret == "" {
		return createErrorResults(validRecords, InitFailed, "配置不完整")
	}
	if h.zoneID == "" {
		return createErrorResults(validRecords, InitFailed, "Zone ID 未初始化")
	}

	allRecords, err := h.describeAllRecordSets()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", h.GetServiceName(), err)
		return createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	existing := h.parseExistingRecords(allRecords)

	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, h.processRecord(vr.record, vr.cache, existing))
	}
	return results
}

// parseExistingRecords 将现有记录按类型分组
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

// processRecord 处理单条 DNS 记录
func (h *Huawei) processRecord(record *config.DNSRecord, cache *Cache, existing *huaweiRecordsByType) RecordResult {
	// 1. 获取当前值
	currentValue, result, ok := getCurrentValue(h.GetServiceName(), record, cache)
	if !ok {
		return result
	}

	// 2. 检查缓存
	if skip, r := checkDynamicCache(h.GetServiceName(), record, cache, currentValue); skip {
		return r
	}

	// 3. 处理记录类型变更
	var updateErr error

	if record.Type == RecordTypeCNAME {
		if len(existing.otherRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有非 CNAME 记录 [数量=%d]", h.GetServiceName(), len(existing.otherRecords))
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

		if len(existing.cnameRecords) > 0 {
			existingCNAME := existing.cnameRecords[0]
			if len(existingCNAME.Records) == 1 && existingCNAME.Records[0] == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 记录值未变化，无需更新 [RecordID=%s, 值=%s]", h.GetServiceName(), existingCNAME.ID, currentValue)
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 更新已有 CNAME 记录 [RecordID=%s, 旧值=%v]", h.GetServiceName(), existingCNAME.ID, existingCNAME.Records)
				updateErr = h.updateRecordSet(existingCNAME.ID, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", h.GetServiceName())
			updateErr = h.createRecordSet(record.Type, currentValue)
		}
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
	finalizeSuccess(h.GetServiceName(), record, cache, currentValue, &result)
	return result
}

// getZoneID 获取根域名对应的 Zone ID
func (h *Huawei) getZoneID() (string, error) {
	rootDomain := getRootDomain(h.Group.Domain)

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

// describeAllRecordSets 查询指定域名的所有 DNS 记录
func (h *Huawei) describeAllRecordSets() ([]HuaweiRecordSet, error) {
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
		"host":         req.URL.Host,
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
