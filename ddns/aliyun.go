package ddns

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
	"github.com/cxbdasheng/dnet/signer"
)

const aliyunDNSEndpoint = "https://alidns.aliyuncs.com/"

type Aliyun struct {
	BaseDNSProvider
}

// DomainRecord 阿里云 DNS 记录
type DomainRecord struct {
	RecordId   string `json:"RecordId"`
	RR         string `json:"RR"`    // 主机记录
	Type       string `json:"Type"`  // 记录类型
	Value      string `json:"Value"` // 记录值
	TTL        int64  `json:"TTL"`
	Status     string `json:"Status"`
	DomainName string `json:"DomainName"`
}

// DescribeDomainRecordsResponse 查询 DNS 记录响应
type DescribeDomainRecordsResponse struct {
	RequestId     string `json:"RequestId"`
	TotalCount    int    `json:"TotalCount"`
	PageNumber    int    `json:"PageNumber"`
	PageSize      int    `json:"PageSize"`
	DomainRecords struct {
		Record []DomainRecord `json:"Record"`
	} `json:"DomainRecords"`
}

// DomainRecordResponse DNS 记录操作响应（更新/创建/删除通用）
type DomainRecordResponse struct {
	RequestId string `json:"RequestId"`
	RecordId  string `json:"RecordId"`
}

// validRecord 有效记录结构（用于批量处理）
type validRecord struct {
	record *config.DNSRecord
	cache  *Cache
}

// recordsByType 按类型分组的记录映射
type recordsByType struct {
	cnameRecords []DomainRecord           // CNAME 记录
	otherRecords map[string]*DomainRecord // 其他类型记录（Type -> Record）
}

// UpdateOrCreateRecords 批量更新或创建 DNS 记录（一次查询，处理所有记录）
func (a *Aliyun) UpdateOrCreateRecords() []RecordResult {
	validRecords := filterValidRecords(a.Group, a.Caches)
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	if a.Group.Domain == "" || a.Group.AccessKey == "" || a.Group.AccessSecret == "" {
		return createErrorResults(validRecords, InitFailed, "配置不完整")
	}

	allRecords, err := a.describeAllDomainRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", a.GetServiceName(), err)
		return createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	existing := a.parseExistingRecords(allRecords)

	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, a.processRecord(vr.record, vr.cache, existing))
	}
	return results
}

// parseExistingRecords 解析现有 DNS 记录（只遍历一次 allRecords）
func (a *Aliyun) parseExistingRecords(allRecords []DomainRecord) *recordsByType {
	existing := &recordsByType{
		cnameRecords: make([]DomainRecord, 0),
		otherRecords: make(map[string]*DomainRecord),
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
func (a *Aliyun) processRecord(record *config.DNSRecord, cache *Cache, existing *recordsByType) RecordResult {
	// 1. 获取当前值
	currentValue, result, ok := getCurrentValue(a.GetServiceName(), record, cache)
	if !ok {
		return result
	}

	// 2. 检查缓存
	if skip, r := checkDynamicCache(a.GetServiceName(), record, cache, currentValue); skip {
		return r
	}

	// 3. 处理记录类型变更（特殊处理 CNAME 类型冲突）
	var updateErr error

	if record.Type == RecordTypeCNAME {
		// 删除所有其他类型记录（非 CNAME）
		if len(existing.otherRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有非 CNAME 记录 [数量=%d]", a.GetServiceName(), len(existing.otherRecords))
			for _, rec := range existing.otherRecords {
				if deleteErr := a.deleteDomainRecord(rec.RecordId); deleteErr != nil {
					result.Status = UpdatedFailed
					result.ErrorMessage = deleteErr.Error()
					result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
					helper.Error(helper.LogTypeDDNS, "[%s] [CNAME] 删除 DNS 记录失败 [RecordId=%s, 类型=%s, 错误=%v]", a.GetServiceName(), rec.RecordId, rec.Type, deleteErr)
					return result
				}
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 已删除 DNS 记录 [RecordId=%s, 类型=%s, 值=%s]", a.GetServiceName(), rec.RecordId, rec.Type, rec.Value)
			}
		}

		// 更新或创建 CNAME 记录
		if len(existing.cnameRecords) > 0 {
			existingCNAME := existing.cnameRecords[0]
			if existingCNAME.Value == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 记录值未变化，无需更新 [RecordId=%s, 值=%s]", a.GetServiceName(), existingCNAME.RecordId, currentValue)
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 更新已有 CNAME 记录 [RecordId=%s, 旧值=%s]", a.GetServiceName(), existingCNAME.RecordId, existingCNAME.Value)
				updateErr = a.updateDomainRecord(existingCNAME.RecordId, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", a.GetServiceName())
			updateErr = a.addDomainRecord(record.Type, currentValue)
		}
	} else {
		// 创建非 CNAME 类型记录，需要确保同子域下没有 CNAME 记录
		if len(existing.cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", a.GetServiceName(), record.Type, record.Type, len(existing.cnameRecords))
			if deleteErr := a.deleteRecords(existing.cnameRecords, record.Type); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}
		}

		targetRecord := existing.otherRecords[record.Type]
		if targetRecord != nil {
			if targetRecord.Value == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录值未变化，无需更新 [RecordId=%s, 值=%s]", a.GetServiceName(), record.Type, targetRecord.RecordId, currentValue)
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录已存在 [RecordId=%s, 旧值=%s]", a.GetServiceName(), record.Type, targetRecord.RecordId, targetRecord.Value)
				updateErr = a.updateDomainRecord(targetRecord.RecordId, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录不存在，创建新记录", a.GetServiceName(), record.Type)
			updateErr = a.addDomainRecord(record.Type, currentValue)
		}
	}

	if updateErr != nil {
		result.Status = UpdatedFailed
		result.ErrorMessage = updateErr.Error()
		result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
		return result
	}

	// 4. 更新缓存
	finalizeSuccess(a.GetServiceName(), record, cache, currentValue, &result)
	return result
}

// describeAllDomainRecords 查询指定主机记录的所有 DNS 记录（所有类型）
func (a *Aliyun) describeAllDomainRecords() ([]DomainRecord, error) {
	params := url.Values{}
	params.Set("Action", "DescribeDomainRecords")
	params.Set("DomainName", getRootDomain(a.Group.Domain))
	params.Set("RRKeyWord", getHostRecord(a.Group.Domain))
	params.Set("Version", "2015-01-09")

	var response DescribeDomainRecordsResponse
	err := a.request(http.MethodGet, params, &response)
	if err != nil {
		return nil, err
	}

	// RRKeyWord 为模糊匹配，需精确过滤，避免返回不相关的子域名记录
	hostRecord := getHostRecord(a.Group.Domain)
	exact := response.DomainRecords.Record[:0]
	for _, r := range response.DomainRecords.Record {
		if r.RR == hostRecord {
			exact = append(exact, r)
		}
	}
	return exact, nil
}

// updateDomainRecord 更新 DNS 记录
func (a *Aliyun) updateDomainRecord(recordId, recordType, value string) error {
	params := url.Values{}
	params.Set("Action", "UpdateDomainRecord")
	params.Set("RecordId", recordId)
	params.Set("RR", getHostRecord(a.Group.Domain))
	params.Set("Type", recordType)
	params.Set("Value", value)

	ttl := a.parseTTL()
	if ttl > 0 {
		params.Set("TTL", strconv.FormatInt(ttl, 10))
	}
	params.Set("Version", "2015-01-09")

	var response DomainRecordResponse
	err := a.request(http.MethodGet, params, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录失败 [RecordId=%s, 错误=%v]", a.GetServiceName(), recordType, recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录成功 [RecordId=%s, 新值=%s]", a.GetServiceName(), recordType, recordId, value)
	return nil
}

// addDomainRecord 创建 DNS 记录
func (a *Aliyun) addDomainRecord(recordType, value string) error {
	params := url.Values{}
	params.Set("Action", "AddDomainRecord")
	params.Set("DomainName", getRootDomain(a.Group.Domain))
	params.Set("RR", getHostRecord(a.Group.Domain))
	params.Set("Type", recordType)
	params.Set("Value", value)

	ttl := a.parseTTL()
	if ttl > 0 {
		params.Set("TTL", strconv.FormatInt(ttl, 10))
	}
	params.Set("Version", "2015-01-09")

	var response DomainRecordResponse
	err := a.request(http.MethodGet, params, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录失败 [域名=%s, 错误=%v]", a.GetServiceName(), recordType, a.Group.Domain, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录成功 [RecordId=%s, 值=%s]", a.GetServiceName(), recordType, response.RecordId, value)
	return nil
}

// deleteRecords 批量删除 DNS 记录
func (a *Aliyun) deleteRecords(records []DomainRecord, contextType string) error {
	for _, rec := range records {
		if deleteErr := a.deleteDomainRecord(rec.RecordId); deleteErr != nil {
			helper.Error(helper.LogTypeDDNS, "[%s] [%s] 删除 DNS 记录失败 [RecordId=%s, 类型=%s, 错误=%v]", a.GetServiceName(), contextType, rec.RecordId, rec.Type, deleteErr)
			return deleteErr
		}
		helper.Info(helper.LogTypeDDNS, "[%s] [%s] 已删除 DNS 记录 [RecordId=%s, 类型=%s, 值=%s]", a.GetServiceName(), contextType, rec.RecordId, rec.Type, rec.Value)
	}
	return nil
}

// deleteDomainRecord 删除 DNS 记录
func (a *Aliyun) deleteDomainRecord(recordId string) error {
	params := url.Values{}
	params.Set("Action", "DeleteDomainRecord")
	params.Set("RecordId", recordId)
	params.Set("Version", "2015-01-09")

	var response DomainRecordResponse
	err := a.request(http.MethodGet, params, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordId=%s, 错误=%v]", a.GetServiceName(), recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 删除 DNS 记录成功 [RecordId=%s]", a.GetServiceName(), recordId)
	return nil
}

// request 统一请求方法
func (a *Aliyun) request(method string, params url.Values, result interface{}) error {
	signer.AliyunSigner(a.Group.AccessKey, a.Group.AccessSecret, &params, method)

	req, err := http.NewRequest(method, aliyunDNSEndpoint, nil)
	if err != nil {
		return err
	}
	req.URL.RawQuery = params.Encode()

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	return helper.GetHTTPResponse(resp, err, result)
}

// parseTTL 解析 TTL 值
func (a *Aliyun) parseTTL() int64 {
	if a.Group.TTL == "" || a.Group.TTL == "AUTO" {
		return 600
	}
	if ttl, err := strconv.ParseInt(a.Group.TTL, 10, 64); err == nil {
		return ttl
	}
	ttlStr := strings.ToLower(a.Group.TTL)
	if strings.HasSuffix(ttlStr, "s") {
		if ttl, err := strconv.ParseInt(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl
		}
	} else if strings.HasSuffix(ttlStr, "m") {
		if ttl, err := strconv.ParseInt(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl * 60
		}
	} else if strings.HasSuffix(ttlStr, "h") {
		if ttl, err := strconv.ParseInt(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl * 3600
		}
	}
	return 600
}
