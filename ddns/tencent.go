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
	BaseDNSProvider
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
	otherRecords map[string]*TencentCloudRecord
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

// UpdateOrCreateRecords 批量更新或创建 DNS 记录
func (t *TencentCloud) UpdateOrCreateRecords() []RecordResult {
	validRecords := filterValidRecords(t.Group, t.Caches)
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	if t.Group.Domain == "" || t.Group.AccessKey == "" || t.Group.AccessSecret == "" {
		return createErrorResults(validRecords, InitFailed, "配置不完整")
	}

	allRecords, err := t.describeAllDomainRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", t.GetServiceName(), err)
		return createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	existing := t.parseExistingRecords(allRecords)

	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, t.processRecord(vr.record, vr.cache, existing))
	}
	return results
}

// parseExistingRecords 将现有记录按类型分组
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

// processRecord 处理单条 DNS 记录
func (t *TencentCloud) processRecord(record *config.DNSRecord, cache *Cache, existing *tencentRecordsByType) RecordResult {
	// 1. 获取当前值
	currentValue, result, ok := getCurrentValue(t.GetServiceName(), record, cache)
	if !ok {
		return result
	}

	// 2. 检查缓存
	if skip, r := checkDynamicCache(t.GetServiceName(), record, cache, currentValue); skip {
		return r
	}

	// 3. 处理记录类型变更
	var updateErr error

	if record.Type == RecordTypeCNAME {
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

		if len(existing.cnameRecords) > 0 {
			existingCNAME := existing.cnameRecords[0]
			if existingCNAME.Value == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 记录值未变化，无需更新 [RecordId=%d, 值=%s]", t.GetServiceName(), existingCNAME.RecordId, currentValue)
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
	finalizeSuccess(t.GetServiceName(), record, cache, currentValue, &result)
	return result
}

// describeAllDomainRecords 查询指定主机记录的所有 DNS 记录
func (t *TencentCloud) describeAllDomainRecords() ([]TencentCloudRecord, error) {
	requestBody := struct {
		Domain    string `json:"Domain"`
		Subdomain string `json:"Subdomain"`
	}{
		Domain:    getRootDomain(t.Group.Domain),
		Subdomain: getHostRecord(t.Group.Domain),
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
		Domain:     getRootDomain(t.Group.Domain),
		SubDomain:  getHostRecord(t.Group.Domain),
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
		Domain:     getRootDomain(t.Group.Domain),
		SubDomain:  getHostRecord(t.Group.Domain),
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
		Domain:   getRootDomain(t.Group.Domain),
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

	if v, ok := result.(*TencentCloudAPIResponse); ok {
		if v.Response.Error != nil {
			return errors.New(v.Response.Error.Code + ": " + v.Response.Error.Message)
		}
	} else if v, ok := result.(*DescribeRecordListResponse); ok {
		if v.Response.Error != nil {
			if action == "DescribeRecordList" && v.Response.Error.Code == "ResourceNotFound.NoDataOfRecord" {
				return nil
			}
			return errors.New(v.Response.Error.Code + ": " + v.Response.Error.Message)
		}
	}
	return nil
}

// getRecordLine 获取记录线路（腾讯云必填，默认为"默认"）
func (t *TencentCloud) getRecordLine() string {
	return "默认"
}

// parseTTL 解析 TTL 值
func (t *TencentCloud) parseTTL() uint64 {
	if t.Group.TTL == "" || t.Group.TTL == "AUTO" {
		return 600
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
