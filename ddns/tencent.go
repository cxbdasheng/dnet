package ddns

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
	"github.com/cxbdasheng/dnet/signer"
)

const (
	tencentCloudDNSEndpoint = "https://dnspod.tencentcloudapi.com/"
	tencentCloudDNSHost     = "dnspod.tencentcloudapi.com"
	tencentCloudDNSService  = "dnspod"
	tencentCloudDNSVersion  = "2021-03-23"
)

type TencentCloud struct {
	Group  *config.DNSGroup
	Caches []*Cache
}

// TencentCloudRecord 腾讯云 DNS 记录
type TencentCloudRecord struct {
	RecordId uint64 `json:"RecordId"`
	Name     string `json:"Name"`
	Type     string `json:"Type"`
	Value    string `json:"Value"`
	Line     string `json:"Line"`
	TTL      uint64 `json:"TTL"`
}

// TencentCloudAPIError 腾讯云 API 错误
type TencentCloudAPIError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

// TencentCloudAPIResponse 腾讯云通用响应
type TencentCloudAPIResponse struct {
	Response struct {
		Error     *TencentCloudAPIError `json:"Error,omitempty"`
		RequestId string                `json:"RequestId"`
	} `json:"Response"`
}

// DescribeRecordListResponse 查询 DNS 记录响应
type DescribeRecordListResponse struct {
	Response struct {
		RecordCountInfo struct {
			TotalCount int `json:"TotalCount"`
		} `json:"RecordCountInfo"`
		RecordList []TencentCloudRecord  `json:"RecordList"`
		Error      *TencentCloudAPIError `json:"Error,omitempty"`
		RequestId  string                `json:"RequestId"`
	} `json:"Response"`
}

// tencentRecordsByType 按类型分组的腾讯云记录映射
type tencentRecordsByType struct {
	cnameRecords []TencentCloudRecord
	otherRecords map[string]*TencentCloudRecord // Type -> Record
}

type createRecordRequest struct {
	Domain     string `json:"Domain"`
	SubDomain  string `json:"SubDomain"`
	RecordType string `json:"RecordType"`
	RecordLine string `json:"RecordLine"`
	Value      string `json:"Value"`
	TTL        uint64 `json:"TTL"`
}

type modifyRecordRequest struct {
	Domain     string `json:"Domain"`
	SubDomain  string `json:"SubDomain"`
	RecordType string `json:"RecordType"`
	RecordLine string `json:"RecordLine"`
	Value      string `json:"Value"`
	RecordId   uint64 `json:"RecordId"`
	TTL        uint64 `json:"TTL"`
}

func (t *TencentCloud) GetServiceName() string {
	if t.Group == nil {
		return ""
	}
	if t.Group.Name != "" {
		return t.Group.Name
	}
	return t.Group.Domain
}

// Init 初始化腾讯云 DDNS（批量处理模式）
func (t *TencentCloud) Init(group *config.DNSGroup, caches []*Cache) {
	t.Group = group
	t.Caches = caches

	if t.Group.Domain == "" || t.Group.AccessKey == "" || t.Group.AccessSecret == "" {
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", t.GetServiceName())
		return
	}

	if len(t.Caches) > 0 && !t.Caches[0].HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功，共 %d 条记录", t.GetServiceName(), len(t.Caches))
	}
}

// UpdateOrCreateRecords 批量更新或创建 DNS 记录（一次查询，处理所有记录）
func (t *TencentCloud) UpdateOrCreateRecords() []RecordResult {
	// 1. 预先过滤有效记录
	validRecords := make([]validRecord, 0, len(t.Group.Records))
	cacheIdx := 0
	for i := range t.Group.Records {
		if t.Group.Records[i].Value != "" {
			validRecords = append(validRecords, validRecord{
				record: &t.Group.Records[i],
				cache:  t.Caches[cacheIdx],
			})
			cacheIdx++
		}
	}

	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	// 2. 验证配置
	if t.Group.Domain == "" || t.Group.AccessKey == "" || t.Group.AccessSecret == "" {
		return t.createErrorResults(validRecords, InitFailed, "配置不完整")
	}

	// 3. 一次性查询该子域下的所有 DNS 记录
	allRecords, err := t.describeAllDomainRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", t.GetServiceName(), err)
		return t.createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	// 4. 将现有记录按类型分组
	existing := t.parseExistingRecords(allRecords)

	// 5. 逐条处理
	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		result := t.processRecord(vr.record, vr.cache, existing)
		results = append(results, result)
	}

	return results
}

// parseExistingRecords 将现有记录按类型分组（只遍历一次）
func (t *TencentCloud) parseExistingRecords(allRecords []TencentCloudRecord) *tencentRecordsByType {
	existing := &tencentRecordsByType{
		cnameRecords: make([]TencentCloudRecord, 0),
		otherRecords: make(map[string]*TencentCloudRecord),
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
func (t *TencentCloud) createErrorResults(validRecords []validRecord, status statusType, errMsg string) []RecordResult {
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
func (t *TencentCloud) processRecord(record *config.DNSRecord, cache *Cache, existing *tencentRecordsByType) RecordResult {
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
				helper.Error(helper.LogTypeDDNS, "[%s] [%s] 获取 IP 失败", t.GetServiceName(), record.Type)
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
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型: %s", t.GetServiceName(), record.Type)
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
			helper.Debug(helper.LogTypeDDNS, "[%s] [%s] 值未变化，跳过更新 [当前=%s, 剩余次数=%d]", t.GetServiceName(), record.Type, currentValue, cache.Times)
			return result
		}

		if forceUpdate && !valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 达到强制更新阈值，执行更新 [值=%s]", t.GetServiceName(), record.Type, currentValue)
		} else if valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 检测到值变化 [旧值=%s, 新值=%s]", t.GetServiceName(), record.Type, oldValue, currentValue)
		}
	}

	// 3. 处理记录类型变更（特殊处理 CNAME 类型冲突）
	var updateErr error

	if record.Type == RecordTypeCNAME {
		// 删除所有其他类型记录（非 CNAME）
		if len(existing.otherRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有非 CNAME 记录 [数量=%d]", t.GetServiceName(), len(existing.otherRecords))
			for _, rec := range existing.otherRecords {
				if deleteErr := t.deleteDomainRecord(rec.RecordId); deleteErr != nil {
					result.Status = UpdatedFailed
					result.ErrorMessage = deleteErr.Error()
					result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
					helper.Error(helper.LogTypeDDNS, "[%s] [CNAME] 删除 DNS 记录失败 [RecordId=%d, 类型=%s, 错误=%v]", t.GetServiceName(), rec.RecordId, rec.Type, deleteErr)
					return result
				}
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 已删除 DNS 记录 [RecordId=%d, 类型=%s, 值=%s]", t.GetServiceName(), rec.RecordId, rec.Type, rec.Value)
			}
		}

		// 更新或创建 CNAME 记录
		if len(existing.cnameRecords) > 0 {
			existingCNAME := existing.cnameRecords[0]
			if existingCNAME.Value == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 记录值未变化，无需更新 [RecordId=%d, 值=%s]", t.GetServiceName(), existingCNAME.RecordId, currentValue)
				updateErr = nil
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 更新已有 CNAME 记录 [RecordId=%d, 旧值=%s]", t.GetServiceName(), existingCNAME.RecordId, existingCNAME.Value)
				updateErr = t.updateDomainRecord(existingCNAME.RecordId, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", t.GetServiceName())
			updateErr = t.addDomainRecord(record.Type, currentValue)
		}
	} else {
		if len(existing.cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", t.GetServiceName(), record.Type, record.Type, len(existing.cnameRecords))
			if deleteErr := t.deleteRecords(existing.cnameRecords, record.Type); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}
		}

		targetRecord := existing.otherRecords[record.Type]
		if targetRecord != nil {
			if targetRecord.Value == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录值未变化，无需更新 [RecordId=%d, 值=%s]", t.GetServiceName(), record.Type, targetRecord.RecordId, currentValue)
				updateErr = nil
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录已存在 [RecordId=%d, 旧值=%s]", t.GetServiceName(), record.Type, targetRecord.RecordId, targetRecord.Value)
				updateErr = t.updateDomainRecord(targetRecord.RecordId, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录不存在，创建新记录", t.GetServiceName(), record.Type)
			updateErr = t.addDomainRecord(record.Type, currentValue)
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

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] DNS 记录更新成功 [值=%s]", t.GetServiceName(), record.Type, currentValue)
	return result
}

// describeAllDomainRecords 查询指定主机记录的所有 DNS 记录（所有类型）
func (t *TencentCloud) describeAllDomainRecords() ([]TencentCloudRecord, error) {
	requestBody := struct {
		Domain    string `json:"Domain"`
		Subdomain string `json:"Subdomain"`
	}{
		Domain:    t.getRootDomain(),
		Subdomain: t.getHostRecord(),
	}

	var response DescribeRecordListResponse
	err := t.request("DescribeRecordList", requestBody, &response)
	if err != nil {
		return nil, err
	}

	return response.Response.RecordList, nil
}

// updateDomainRecord 更新 DNS 记录
func (t *TencentCloud) updateDomainRecord(recordId uint64, recordType, value string) error {
	requestBody := modifyRecordRequest{
		Domain:     t.getRootDomain(),
		SubDomain:  t.getHostRecord(),
		RecordType: recordType,
		RecordLine: t.getRecordLine(),
		Value:      value,
		RecordId:   recordId,
		TTL:        t.parseTTL(),
	}

	var response TencentCloudAPIResponse
	err := t.request("ModifyRecord", requestBody, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录失败 [RecordId=%d, 错误=%v]", t.GetServiceName(), recordType, recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录成功 [RecordId=%d, 新值=%s]", t.GetServiceName(), recordType, recordId, value)
	return nil
}

// addDomainRecord 创建 DNS 记录
func (t *TencentCloud) addDomainRecord(recordType, value string) error {
	requestBody := createRecordRequest{
		Domain:     t.getRootDomain(),
		SubDomain:  t.getHostRecord(),
		RecordType: recordType,
		RecordLine: t.getRecordLine(),
		Value:      value,
		TTL:        t.parseTTL(),
	}

	var response TencentCloudAPIResponse
	err := t.request("CreateRecord", requestBody, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录失败 [域名=%s, 错误=%v]", t.GetServiceName(), recordType, t.Group.Domain, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录成功 [值=%s]", t.GetServiceName(), recordType, value)
	return nil
}

// deleteRecords 批量删除 DNS 记录
func (t *TencentCloud) deleteRecords(records []TencentCloudRecord, contextType string) error {
	for _, rec := range records {
		if deleteErr := t.deleteDomainRecord(rec.RecordId); deleteErr != nil {
			helper.Error(helper.LogTypeDDNS, "[%s] [%s] 删除 DNS 记录失败 [RecordId=%d, 类型=%s, 错误=%v]", t.GetServiceName(), contextType, rec.RecordId, rec.Type, deleteErr)
			return deleteErr
		}
		helper.Info(helper.LogTypeDDNS, "[%s] [%s] 已删除 DNS 记录 [RecordId=%d, 类型=%s, 值=%s]", t.GetServiceName(), contextType, rec.RecordId, rec.Type, rec.Value)
	}
	return nil
}

// deleteDomainRecord 删除 DNS 记录
func (t *TencentCloud) deleteDomainRecord(recordId uint64) error {
	requestBody := struct {
		Domain   string `json:"Domain"`
		RecordId uint64 `json:"RecordId"`
	}{
		Domain:   t.getRootDomain(),
		RecordId: recordId,
	}

	var response TencentCloudAPIResponse
	err := t.request("DeleteRecord", requestBody, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordId=%d, 错误=%v]", t.GetServiceName(), recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 删除 DNS 记录成功 [RecordId=%d]", t.GetServiceName(), recordId)
	return nil
}

// request 统一请求方法
func (t *TencentCloud) request(action string, body interface{}, result interface{}) error {
	jsonStr, _ := json.Marshal(body)

	req, err := http.NewRequest(http.MethodPost, tencentCloudDNSEndpoint, bytes.NewBuffer(jsonStr))
	if err != nil {
		return err
	}

	req.Header.Set("X-TC-Action", action)
	signer.TencentSigner(t.Group.AccessKey, t.Group.AccessSecret, tencentCloudDNSService, tencentCloudDNSHost, string(jsonStr), req)
	req.Header.Set("X-TC-Version", tencentCloudDNSVersion)

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err = helper.GetHTTPResponse(resp, err, result); err != nil {
		return err
	}

	// 检查腾讯云 API 业务错误
	if v, ok := result.(*TencentCloudAPIResponse); ok {
		if v.Response.Error != nil {
			return errors.New(v.Response.Error.Code + ": " + v.Response.Error.Message)
		}
	} else if v, ok := result.(*DescribeRecordListResponse); ok {
		if v.Response.Error != nil {
			// 腾讯云在无匹配记录时返回 ResourceNotFound.NoDataOfRecord，
			// 不视为失败，交给上层走"创建记录"逻辑
			if action == "DescribeRecordList" && v.Response.Error.Code == "ResourceNotFound.NoDataOfRecord" {
				return nil
			}
			return errors.New(v.Response.Error.Code + ": " + v.Response.Error.Message)
		}
	}

	return nil
}

// getRootDomain 获取根域名
// 例如: www.sub.example.com -> example.com
func (t *TencentCloud) getRootDomain() string {
	domain := t.Group.Domain
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
func (t *TencentCloud) getHostRecord() string {
	domain := t.Group.Domain
	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return "@"
	}
	return strings.Join(parts[:len(parts)-2], ".")
}

// getRecordLine 获取记录线路（腾讯云必填，默认为"默认"）
func (t *TencentCloud) getRecordLine() string {
	return "默认"
}

// parseTTL 解析 TTL 值
func (t *TencentCloud) parseTTL() uint64 {
	if t.Group.TTL == "" || t.Group.TTL == "AUTO" {
		return 600 // 默认 10 分钟
	}

	if ttl, err := strconv.ParseUint(t.Group.TTL, 10, 64); err == nil {
		return ttl
	}

	ttlStr := strings.ToLower(t.Group.TTL)
	if strings.HasSuffix(ttlStr, "s") {
		if ttl, err := strconv.ParseUint(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl
		}
	} else if strings.HasSuffix(ttlStr, "m") {
		if ttl, err := strconv.ParseUint(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl * 60
		}
	} else if strings.HasSuffix(ttlStr, "h") {
		if ttl, err := strconv.ParseUint(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl * 3600
		}
	}

	return 600
}
