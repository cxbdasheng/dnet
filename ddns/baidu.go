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

// 百度智能云 DNS API Endpoint
const baiduDNSEndpoint = "https://dns.baidubce.com"

type Baidu struct {
	Group  *config.DNSGroup
	Caches []*Cache
}

// BaiduRecord 百度云 DNS 记录
type BaiduRecord struct {
	RecordID    string `json:"recordId"`    // 记录ID
	Domain      string `json:"domain"`      // 域名
	View        string `json:"view"`        // 视图(默认为空)
	Rdtype      string `json:"rdtype"`      // 记录类型 A/AAAA/CNAME/TXT
	TTL         int    `json:"ttl"`         // TTL值
	Rdata       string `json:"rdata"`       // 记录值
	ZoneName    string `json:"zoneName"`    // Zone名称
	Status      string `json:"status"`      // 状态
	Description string `json:"description"` // 描述
}

// BaiduListRecordsResponse 查询记录列表响应
type BaiduListRecordsResponse struct {
	Marker      string        `json:"marker"`
	IsTruncated bool          `json:"isTruncated"`
	NextMarker  string        `json:"nextMarker"`
	MaxKeys     int           `json:"maxKeys"`
	Records     []BaiduRecord `json:"records"`
}

// BaiduRecordResponse 单条记录响应
type BaiduRecordResponse struct {
	RecordID string `json:"recordId"`
}

// baiduRecordsByType 按类型分组的百度云记录映射
type baiduRecordsByType struct {
	cnameRecords []BaiduRecord
	otherRecords map[string]*BaiduRecord // Rdtype -> Record
}

func (b *Baidu) GetServiceName() string {
	if b.Group == nil {
		return ""
	}
	if b.Group.Name != "" {
		return b.Group.Name
	}
	return b.Group.Domain
}

// Init 初始化百度云 DDNS（批量处理模式）
func (b *Baidu) Init(group *config.DNSGroup, caches []*Cache) {
	b.Group = group
	b.Caches = caches

	if b.Group.Domain == "" || b.Group.AccessKey == "" || b.Group.AccessSecret == "" {
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", b.GetServiceName())
		return
	}

	if len(b.Caches) > 0 && !b.Caches[0].HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功，共 %d 条记录", b.GetServiceName(), len(b.Caches))
	}
}

// UpdateOrCreateRecords 批量更新或创建 DNS 记录（一次查询，处理所有记录）
func (b *Baidu) UpdateOrCreateRecords() []RecordResult {
	// 1. 预先过滤有效记录
	validRecords := make([]validRecord, 0, len(b.Group.Records))
	cacheIdx := 0
	for i := range b.Group.Records {
		if b.Group.Records[i].Value != "" {
			validRecords = append(validRecords, validRecord{
				record: &b.Group.Records[i],
				cache:  b.Caches[cacheIdx],
			})
			cacheIdx++
		}
	}

	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	// 2. 验证配置
	if b.Group.Domain == "" || b.Group.AccessKey == "" || b.Group.AccessSecret == "" {
		return b.createErrorResults(validRecords, InitFailed, "配置不完整")
	}

	// 3. 一次性查询该子域下的所有 DNS 记录
	allRecords, err := b.listAllRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", b.GetServiceName(), err)
		return b.createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	// 4. 将现有记录按类型分组
	existing := b.parseExistingRecords(allRecords)

	// 5. 逐条处理
	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		result := b.processRecord(vr.record, vr.cache, existing)
		results = append(results, result)
	}

	return results
}

// parseExistingRecords 将现有记录按类型分组（只遍历一次）
func (b *Baidu) parseExistingRecords(allRecords []BaiduRecord) *baiduRecordsByType {
	existing := &baiduRecordsByType{
		cnameRecords: make([]BaiduRecord, 0),
		otherRecords: make(map[string]*BaiduRecord),
	}

	for i := range allRecords {
		if allRecords[i].Rdtype == RecordTypeCNAME {
			existing.cnameRecords = append(existing.cnameRecords, allRecords[i])
		} else {
			if _, exists := existing.otherRecords[allRecords[i].Rdtype]; !exists {
				rec := allRecords[i]
				existing.otherRecords[allRecords[i].Rdtype] = &rec
			}
		}
	}

	return existing
}

// createErrorResults 创建批量错误结果
func (b *Baidu) createErrorResults(validRecords []validRecord, status statusType, errMsg string) []RecordResult {
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
func (b *Baidu) processRecord(record *config.DNSRecord, cache *Cache, existing *baiduRecordsByType) RecordResult {
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
				helper.Error(helper.LogTypeDDNS, "[%s] [%s] 获取 IP 失败", b.GetServiceName(), record.Type)
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
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型: %s", b.GetServiceName(), record.Type)
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
			helper.Debug(helper.LogTypeDDNS, "[%s] [%s] 值未变化，跳过更新 [当前=%s, 剩余次数=%d]", b.GetServiceName(), record.Type, currentValue, cache.Times)
			return result
		}

		if forceUpdate && !valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 达到强制更新阈值，执行更新 [值=%s]", b.GetServiceName(), record.Type, currentValue)
		} else if valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 检测到值变化 [旧值=%s, 新值=%s]", b.GetServiceName(), record.Type, oldValue, currentValue)
		}
	}

	// 3. 处理记录类型变更（特殊处理 CNAME 类型冲突）
	var updateErr error

	if record.Type == RecordTypeCNAME {
		totalRecords := len(existing.cnameRecords) + len(existing.otherRecords)
		if totalRecords > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有现有记录 [数量=%d]", b.GetServiceName(), totalRecords)

			if deleteErr := b.deleteRecords(existing.cnameRecords, "CNAME"); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}

			for _, rec := range existing.otherRecords {
				if deleteErr := b.deleteRecord(rec.RecordID); deleteErr != nil {
					result.Status = UpdatedFailed
					result.ErrorMessage = deleteErr.Error()
					result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
					helper.Error(helper.LogTypeDDNS, "[%s] [CNAME] 删除 DNS 记录失败 [RecordID=%s, 类型=%s, 错误=%v]", b.GetServiceName(), rec.RecordID, rec.Rdtype, deleteErr)
					return result
				}
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 已删除 DNS 记录 [RecordID=%s, 类型=%s, 值=%s]", b.GetServiceName(), rec.RecordID, rec.Rdtype, rec.Rdata)
			}
		}

		helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", b.GetServiceName())
		updateErr = b.addRecord(record.Type, currentValue)
	} else {
		if len(existing.cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", b.GetServiceName(), record.Type, record.Type, len(existing.cnameRecords))
			if deleteErr := b.deleteRecords(existing.cnameRecords, record.Type); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}
		}

		targetRecord := existing.otherRecords[record.Type]
		if targetRecord != nil {
			if targetRecord.Rdata == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录值未变化，无需更新 [RecordID=%s, 值=%s]", b.GetServiceName(), record.Type, targetRecord.RecordID, currentValue)
				updateErr = nil
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录已存在 [RecordID=%s, 旧值=%s]", b.GetServiceName(), record.Type, targetRecord.RecordID, targetRecord.Rdata)
				updateErr = b.updateRecord(targetRecord.RecordID, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录不存在，创建新记录", b.GetServiceName(), record.Type)
			updateErr = b.addRecord(record.Type, currentValue)
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

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] DNS 记录更新成功 [值=%s]", b.GetServiceName(), record.Type, currentValue)
	return result
}

// listAllRecords 查询指定域名的所有 DNS 记录（一次查询，所有类型）
func (b *Baidu) listAllRecords() ([]BaiduRecord, error) {
	zoneName := b.getRootDomain()
	rr := b.getHostRecord()

	helper.Debug(helper.LogTypeDDNS, "[%s] 正在查询所有 DNS 记录 [Zone=%s, RR=%s]", b.GetServiceName(), zoneName, rr)

	path := fmt.Sprintf("/v1/dns/zone/%s/record", zoneName)
	if rr != "" && rr != "@" {
		path += "?rr=" + rr
	}

	var response BaiduListRecordsResponse
	err := b.request(http.MethodGet, path, nil, &response)
	if err != nil {
		return nil, err
	}

	return response.Records, nil
}

// addRecord 创建 DNS 记录
func (b *Baidu) addRecord(recordType, value string) error {
	zoneName := b.getRootDomain()

	data := map[string]interface{}{
		"rr":    b.getHostRecord(),
		"type":  recordType,
		"value": value,
		"ttl":   b.parseTTL(),
		"line":  "default",
	}

	path := fmt.Sprintf("/v1/dns/zone/%s/record", zoneName)

	var response BaiduRecordResponse
	err := b.request(http.MethodPost, path, data, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录失败 [Zone=%s, 错误=%v]", b.GetServiceName(), recordType, zoneName, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录成功 [RecordID=%s, 值=%s]", b.GetServiceName(), recordType, response.RecordID, value)
	return nil
}

// updateRecord 更新 DNS 记录
func (b *Baidu) updateRecord(recordID, recordType, value string) error {
	zoneName := b.getRootDomain()

	data := map[string]interface{}{
		"rr":    b.getHostRecord(),
		"type":  recordType,
		"value": value,
		"ttl":   b.parseTTL(),
	}

	path := fmt.Sprintf("/v1/dns/zone/%s/record/%s", zoneName, recordID)

	err := b.request(http.MethodPut, path, data, nil)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录失败 [RecordID=%s, 错误=%v]", b.GetServiceName(), recordType, recordID, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录成功 [RecordID=%s, 新值=%s]", b.GetServiceName(), recordType, recordID, value)
	return nil
}

// deleteRecords 批量删除 DNS 记录
func (b *Baidu) deleteRecords(records []BaiduRecord, contextType string) error {
	for _, rec := range records {
		if deleteErr := b.deleteRecord(rec.RecordID); deleteErr != nil {
			helper.Error(helper.LogTypeDDNS, "[%s] [%s] 删除 DNS 记录失败 [RecordID=%s, 类型=%s, 错误=%v]", b.GetServiceName(), contextType, rec.RecordID, rec.Rdtype, deleteErr)
			return deleteErr
		}
		helper.Info(helper.LogTypeDDNS, "[%s] [%s] 已删除 DNS 记录 [RecordID=%s, 类型=%s, 值=%s]", b.GetServiceName(), contextType, rec.RecordID, rec.Rdtype, rec.Rdata)
	}
	return nil
}

// deleteRecord 删除 DNS 记录
func (b *Baidu) deleteRecord(recordID string) error {
	zoneName := b.getRootDomain()
	path := fmt.Sprintf("/v1/dns/zone/%s/record/%s", zoneName, recordID)

	err := b.request(http.MethodDelete, path, nil, nil)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordID=%s, 错误=%v]", b.GetServiceName(), recordID, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 删除 DNS 记录成功 [RecordID=%s]", b.GetServiceName(), recordID)
	return nil
}

// request 统一请求方法
func (b *Baidu) request(method, path string, body interface{}, result interface{}) error {
	reqURL := baiduDNSEndpoint + path

	var reqBody []byte
	var err error

	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求数据失败: %v", err)
		}
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
	signer.BaiduSigner(b.Group.AccessKey, b.Group.AccessSecret, req)

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

	helper.Debug(helper.LogTypeDDNS, "[%s] API 响应 [状态码=%d, 长度=%d]", b.GetServiceName(), resp.StatusCode, len(responseBody))

	// 百度云 API 返回 2xx 表示成功
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		helper.Warn(helper.LogTypeDDNS, "[%s] API 响应状态码异常 [状态码=%d, 响应=%s]", b.GetServiceName(), resp.StatusCode, string(responseBody))
		return fmt.Errorf("请求失败 [状态码=%d]: %s", resp.StatusCode, string(responseBody))
	}

	if result != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, result); err != nil {
			helper.Error(helper.LogTypeDDNS, "[%s] 解析响应失败: %v", b.GetServiceName(), err)
			helper.Debug(helper.LogTypeDDNS, "[%s] 响应内容: %s", b.GetServiceName(), string(responseBody))
			return fmt.Errorf("解析响应失败: %v", err)
		}
	}

	return nil
}

// getRootDomain 获取根域名
// 例如: www.sub.example.com -> example.com
func (b *Baidu) getRootDomain() string {
	domain := b.Group.Domain
	if strings.HasPrefix(domain, "*.") {
		domain = domain[2:]
	}
	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return domain
	}
	return parts[len(parts)-2] + "." + parts[len(parts)-1]
}

// getHostRecord 获取主机记录
// 例如: www.sub.example.com -> www.sub
// 例如: example.com -> @
// 例如: *.example.com -> *
func (b *Baidu) getHostRecord() string {
	domain := b.Group.Domain
	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return "@"
	}
	return strings.Join(parts[:len(parts)-2], ".")
}

// parseTTL 解析 TTL 值（百度云范围 60-86400 秒）
func (b *Baidu) parseTTL() int {
	if b.Group.TTL == "" || b.Group.TTL == "AUTO" {
		return 600
	}

	clamp := func(v int) int {
		if v < 60 {
			return 60
		}
		if v > 86400 {
			return 86400
		}
		return v
	}

	if ttl, err := strconv.Atoi(b.Group.TTL); err == nil {
		return clamp(ttl)
	}

	ttlStr := strings.ToLower(b.Group.TTL)
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
			return clamp(ttl * 3600)
		}
	}

	return 600
}
