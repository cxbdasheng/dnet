package ddns

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

const (
	dnspodRecordListAPI   = "https://dnsapi.cn/Record.List"
	dnspodRecordCreateAPI = "https://dnsapi.cn/Record.Create"
	dnspodRecordModifyAPI = "https://dnsapi.cn/Record.Modify"
	dnspodRecordRemoveAPI = "https://dnsapi.cn/Record.Remove"
)

type Dnspod struct {
	BaseDNSProvider
}

// DnspodRecord DNSPod DNS 记录
type DnspodRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Value   string `json:"value"`
	Enabled string `json:"enabled"`
	Line    string `json:"line"`
	TTL     string `json:"ttl"`
}

// DnspodStatus DNSPod 响应状态
type DnspodStatus struct {
	Status struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"status"`
}

// DnspodRecordListResp 查询记录列表响应
type DnspodRecordListResp struct {
	DnspodStatus
	Records []DnspodRecord `json:"records"`
}

// dnspodRecordsByType 按类型分组的记录映射
type dnspodRecordsByType struct {
	cnameRecords []DnspodRecord
	otherRecords map[string]*DnspodRecord
}

// UpdateOrCreateRecords 批量更新或创建 DNS 记录
func (d *Dnspod) UpdateOrCreateRecords() []RecordResult {
	validRecords := filterValidRecords(d.Group, d.Caches)
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	if d.Group.Domain == "" || d.Group.AccessKey == "" || d.Group.AccessSecret == "" {
		return createErrorResults(validRecords, InitFailed, "配置不完整")
	}

	allRecords, err := d.describeAllDomainRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", d.GetServiceName(), err)
		return createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	existing := d.parseExistingRecords(allRecords)

	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, d.processRecord(vr.record, vr.cache, existing))
	}
	return results
}

// parseExistingRecords 将现有记录按类型分组
func (d *Dnspod) parseExistingRecords(allRecords []DnspodRecord) *dnspodRecordsByType {
	existing := &dnspodRecordsByType{
		cnameRecords: make([]DnspodRecord, 0),
		otherRecords: make(map[string]*DnspodRecord),
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
func (d *Dnspod) processRecord(record *config.DNSRecord, cache *Cache, existing *dnspodRecordsByType) RecordResult {
	// 1. 获取当前值
	currentValue, result, ok := getCurrentValue(d.GetServiceName(), record, cache)
	if !ok {
		return result
	}

	// 2. 检查缓存
	if skip, r := checkDynamicCache(d.GetServiceName(), record, cache, currentValue); skip {
		return r
	}

	// 3. 处理记录类型变更
	var updateErr error

	if record.Type == RecordTypeCNAME {
		if len(existing.otherRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有非 CNAME 记录 [数量=%d]", d.GetServiceName(), len(existing.otherRecords))
			for _, rec := range existing.otherRecords {
				if deleteErr := d.deleteDomainRecord(rec.ID); deleteErr != nil {
					result.Status = UpdatedFailed
					result.ErrorMessage = deleteErr.Error()
					result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
					helper.Error(helper.LogTypeDDNS, "[%s] [CNAME] 删除 DNS 记录失败 [RecordId=%s, 类型=%s, 错误=%v]", d.GetServiceName(), rec.ID, rec.Type, deleteErr)
					return result
				}
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 已删除 DNS 记录 [RecordId=%s, 类型=%s, 值=%s]", d.GetServiceName(), rec.ID, rec.Type, rec.Value)
			}
		}

		if len(existing.cnameRecords) > 0 {
			existingCNAME := existing.cnameRecords[0]
			if existingCNAME.Value == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 记录值未变化，无需更新 [RecordId=%s, 值=%s]", d.GetServiceName(), existingCNAME.ID, currentValue)
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 更新已有 CNAME 记录 [RecordId=%s, 旧值=%s]", d.GetServiceName(), existingCNAME.ID, existingCNAME.Value)
				updateErr = d.updateDomainRecord(existingCNAME.ID, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", d.GetServiceName())
			updateErr = d.addDomainRecord(record.Type, currentValue)
		}
	} else {
		if len(existing.cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", d.GetServiceName(), record.Type, record.Type, len(existing.cnameRecords))
			if deleteErr := d.deleteRecords(existing.cnameRecords, record.Type); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}
		}

		targetRecord := existing.otherRecords[record.Type]
		if targetRecord != nil {
			if targetRecord.Value == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录值未变化，无需更新 [RecordId=%s, 值=%s]", d.GetServiceName(), record.Type, targetRecord.ID, currentValue)
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录已存在 [RecordId=%s, 旧值=%s]", d.GetServiceName(), record.Type, targetRecord.ID, targetRecord.Value)
				updateErr = d.updateDomainRecord(targetRecord.ID, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录不存在，创建新记录", d.GetServiceName(), record.Type)
			updateErr = d.addDomainRecord(record.Type, currentValue)
		}
	}

	if updateErr != nil {
		result.Status = UpdatedFailed
		result.ErrorMessage = updateErr.Error()
		result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
		return result
	}

	// 4. 更新缓存
	finalizeSuccess(d.GetServiceName(), record, cache, currentValue, &result)
	return result
}

// describeAllDomainRecords 查询指定主机记录的所有 DNS 记录
func (d *Dnspod) describeAllDomainRecords() ([]DnspodRecord, error) {
	params := d.baseParams()
	params.Set("domain", getRootDomain(d.Group.Domain))
	params.Set("sub_domain", getHostRecord(d.Group.Domain))

	var response DnspodRecordListResp
	if err := d.request(dnspodRecordListAPI, params, &response); err != nil {
		return nil, err
	}
	return response.Records, nil
}

// updateDomainRecord 更新 DNS 记录
func (d *Dnspod) updateDomainRecord(recordId, recordType, value string) error {
	params := d.baseParams()
	params.Set("domain", getRootDomain(d.Group.Domain))
	params.Set("sub_domain", getHostRecord(d.Group.Domain))
	params.Set("record_id", recordId)
	params.Set("record_type", recordType)
	params.Set("record_line", "默认")
	params.Set("value", value)
	params.Set("ttl", d.parseTTL())

	var response DnspodStatus
	if err := d.request(dnspodRecordModifyAPI, params, &response); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录失败 [RecordId=%s, 错误=%v]", d.GetServiceName(), recordType, recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录成功 [RecordId=%s, 新值=%s]", d.GetServiceName(), recordType, recordId, value)
	return nil
}

// addDomainRecord 创建 DNS 记录
func (d *Dnspod) addDomainRecord(recordType, value string) error {
	params := d.baseParams()
	params.Set("domain", getRootDomain(d.Group.Domain))
	params.Set("sub_domain", getHostRecord(d.Group.Domain))
	params.Set("record_type", recordType)
	params.Set("record_line", "默认")
	params.Set("value", value)
	params.Set("ttl", d.parseTTL())

	var response DnspodStatus
	if err := d.request(dnspodRecordCreateAPI, params, &response); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录失败 [域名=%s, 错误=%v]", d.GetServiceName(), recordType, d.Group.Domain, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录成功 [值=%s]", d.GetServiceName(), recordType, value)
	return nil
}

// deleteRecords 批量删除 DNS 记录
func (d *Dnspod) deleteRecords(records []DnspodRecord, contextType string) error {
	for _, rec := range records {
		if deleteErr := d.deleteDomainRecord(rec.ID); deleteErr != nil {
			helper.Error(helper.LogTypeDDNS, "[%s] [%s] 删除 DNS 记录失败 [RecordId=%s, 类型=%s, 错误=%v]", d.GetServiceName(), contextType, rec.ID, rec.Type, deleteErr)
			return deleteErr
		}
		helper.Info(helper.LogTypeDDNS, "[%s] [%s] 已删除 DNS 记录 [RecordId=%s, 类型=%s, 值=%s]", d.GetServiceName(), contextType, rec.ID, rec.Type, rec.Value)
	}
	return nil
}

// deleteDomainRecord 删除 DNS 记录
func (d *Dnspod) deleteDomainRecord(recordId string) error {
	params := d.baseParams()
	params.Set("domain", getRootDomain(d.Group.Domain))
	params.Set("record_id", recordId)

	var response DnspodStatus
	if err := d.request(dnspodRecordRemoveAPI, params, &response); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordId=%s, 错误=%v]", d.GetServiceName(), recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 删除 DNS 记录成功 [RecordId=%s]", d.GetServiceName(), recordId)
	return nil
}

// request 统一请求方法（DNSPod 使用 form POST + JSON 响应）
func (d *Dnspod) request(apiURL string, params url.Values, result interface{}) error {
	client := helper.CreateHTTPClient()
	resp, err := client.PostForm(apiURL, params)
	if err := helper.GetHTTPResponse(resp, err, result); err != nil {
		return err
	}

	if v, ok := result.(*DnspodStatus); ok {
		if v.Status.Code != "1" {
			return errors.New(v.Status.Message)
		}
	} else if v, ok := result.(*DnspodRecordListResp); ok {
		if v.Status.Code != "1" {
			return errors.New(v.Status.Message)
		}
	}
	return nil
}

// baseParams 构建基础请求参数（login_token + format）
func (d *Dnspod) baseParams() url.Values {
	params := url.Values{}
	params.Set("login_token", d.Group.AccessKey+","+d.Group.AccessSecret)
	params.Set("format", "json")
	return params
}

// parseTTL 解析 TTL 值，返回字符串
func (d *Dnspod) parseTTL() string {
	if d.Group.TTL == "" || d.Group.TTL == "AUTO" {
		return "600"
	}
	ttlStr := strings.ToLower(d.Group.TTL)
	if strings.HasSuffix(ttlStr, "s") {
		if ttl, err := strconv.ParseInt(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return strconv.FormatInt(ttl, 10)
		}
	} else if strings.HasSuffix(ttlStr, "m") {
		if ttl, err := strconv.ParseInt(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return strconv.FormatInt(ttl*60, 10)
		}
	} else if strings.HasSuffix(ttlStr, "h") {
		if ttl, err := strconv.ParseInt(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return strconv.FormatInt(ttl*3600, 10)
		}
	}
	// 纯数字
	if _, err := strconv.ParseInt(d.Group.TTL, 10, 64); err == nil {
		return d.Group.TTL
	}
	return "600"
}
