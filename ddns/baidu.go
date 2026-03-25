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
	BaseDNSProvider
}

// BaiduRecord 百度云 DNS 记录
type BaiduRecord struct {
	RecordID    string `json:"recordId"`
	Domain      string `json:"domain"`
	View        string `json:"view"`
	Rdtype      string `json:"rdtype"` // 记录类型 A/AAAA/CNAME/TXT
	TTL         int    `json:"ttl"`
	Rdata       string `json:"rdata"` // 记录值
	ZoneName    string `json:"zoneName"`
	Status      string `json:"status"`
	Description string `json:"description"`
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

// UpdateOrCreateRecords 批量更新或创建 DNS 记录
func (b *Baidu) UpdateOrCreateRecords() []RecordResult {
	validRecords := filterValidRecords(b.Group, b.Caches)
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	if b.Group.Domain == "" || b.Group.AccessKey == "" || b.Group.AccessSecret == "" {
		return createErrorResults(validRecords, InitFailed, "配置不完整")
	}

	allRecords, err := b.listAllRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", b.GetServiceName(), err)
		return createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	existing := b.parseExistingRecords(allRecords)

	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, b.processRecord(vr.record, vr.cache, existing))
	}
	return results
}

// parseExistingRecords 将现有记录按类型分组
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

// processRecord 处理单条 DNS 记录
func (b *Baidu) processRecord(record *config.DNSRecord, cache *Cache, existing *baiduRecordsByType) RecordResult {
	// 1. 获取当前值
	currentValue, result, ok := getCurrentValue(b.GetServiceName(), record, cache)
	if !ok {
		return result
	}

	// 2. 检查缓存
	if skip, r := checkDynamicCache(b.GetServiceName(), record, cache, currentValue); skip {
		return r
	}

	// 3. 处理记录类型变更
	var updateErr error

	if record.Type == RecordTypeCNAME {
		if len(existing.otherRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有非 CNAME 记录 [数量=%d]", b.GetServiceName(), len(existing.otherRecords))
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

		if len(existing.cnameRecords) > 0 {
			existingCNAME := existing.cnameRecords[0]
			if existingCNAME.Rdata == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 记录值未变化，无需更新 [RecordID=%s, 值=%s]", b.GetServiceName(), existingCNAME.RecordID, currentValue)
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 更新已有 CNAME 记录 [RecordID=%s, 旧值=%s]", b.GetServiceName(), existingCNAME.RecordID, existingCNAME.Rdata)
				updateErr = b.updateRecord(existingCNAME.RecordID, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", b.GetServiceName())
			updateErr = b.addRecord(record.Type, currentValue)
		}
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
	finalizeSuccess(b.GetServiceName(), record, cache, currentValue, &result)
	return result
}

// listAllRecords 查询指定域名的所有 DNS 记录
func (b *Baidu) listAllRecords() ([]BaiduRecord, error) {
	zoneName := getRootDomain(b.Group.Domain)
	rr := getHostRecord(b.Group.Domain)

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
	zoneName := getRootDomain(b.Group.Domain)

	data := map[string]interface{}{
		"rr":    getHostRecord(b.Group.Domain),
		"type":  recordType,
		"value": value,
		"ttl":   b.parseTTL(),
		"line":  "default",
	}

	var response BaiduRecordResponse
	err := b.request(http.MethodPost, fmt.Sprintf("/v1/dns/zone/%s/record", zoneName), data, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录失败 [Zone=%s, 错误=%v]", b.GetServiceName(), recordType, zoneName, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录成功 [RecordID=%s, 值=%s]", b.GetServiceName(), recordType, response.RecordID, value)
	return nil
}

// updateRecord 更新 DNS 记录
func (b *Baidu) updateRecord(recordID, recordType, value string) error {
	zoneName := getRootDomain(b.Group.Domain)

	data := map[string]interface{}{
		"rr":    getHostRecord(b.Group.Domain),
		"type":  recordType,
		"value": value,
		"ttl":   b.parseTTL(),
	}

	err := b.request(http.MethodPut, fmt.Sprintf("/v1/dns/zone/%s/record/%s", zoneName, recordID), data, nil)
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
	zoneName := getRootDomain(b.Group.Domain)
	err := b.request(http.MethodDelete, fmt.Sprintf("/v1/dns/zone/%s/record/%s", zoneName, recordID), nil, nil)
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
