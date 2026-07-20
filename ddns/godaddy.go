package ddns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

// goDaddyAPIEndpoint GoDaddy API 基础地址（var 以便测试时覆盖）
var goDaddyAPIEndpoint = "https://api.godaddy.com/v1"

// GoDaddy TTL 最小值为 600 秒
const goDaddyMinTTL = 600

type GoDaddy struct {
	BaseDNSProvider
}

// goDaddyRecord GoDaddy DNS 记录
type goDaddyRecord struct {
	Data string `json:"data"`
	Name string `json:"name"`
	Type string `json:"type"`
	TTL  int    `json:"ttl"`
}

// goDaddyError GoDaddy API 错误响应
type goDaddyError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// goDaddyRecordsByType 按类型分组的 GoDaddy 记录映射
type goDaddyRecordsByType struct {
	cnameRecords []goDaddyRecord
	otherRecords map[string]*goDaddyRecord
}

// Init 初始化 GoDaddy DDNS（AccessKey=API Key，AccessSecret=API Secret）
func (g *GoDaddy) Init(group *config.DNSGroup, caches []*Cache) {
	if !g.initConfig(group, caches) {
		return
	}
	if len(g.Caches) > 0 && !g.Caches[0].HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功，共 %d 条记录", g.GetServiceName(), len(g.Caches))
	}
}

// UpdateOrCreateRecords 批量更新或创建 DNS 记录
func (g *GoDaddy) UpdateOrCreateRecords() []RecordResult {
	validRecords := filterValidRecords(g.Group, g.Caches)
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	if g.Group.Domain == "" || g.Group.AccessKey == "" || g.Group.AccessSecret == "" {
		return createErrorResults(validRecords, InitFailed, "配置不完整（需要 API Key 和 API Secret）")
	}

	allRecords, err := g.listHostRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", g.GetServiceName(), err)
		return createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	existing := g.parseExistingRecords(allRecords)

	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, g.processRecord(vr.record, vr.cache, existing))
	}
	return results
}

// parseExistingRecords 将现有记录按类型分组
func (g *GoDaddy) parseExistingRecords(allRecords []goDaddyRecord) *goDaddyRecordsByType {
	existing := &goDaddyRecordsByType{
		cnameRecords: make([]goDaddyRecord, 0),
		otherRecords: make(map[string]*goDaddyRecord),
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
func (g *GoDaddy) processRecord(record *config.DNSRecord, cache *Cache, existing *goDaddyRecordsByType) RecordResult {
	// 1. 获取当前值
	currentValue, result, ok := getCurrentValue(g.GetServiceName(), record, cache)
	if !ok {
		return result
	}

	// 2. 检查缓存
	if skip, r := checkDynamicCache(g.GetServiceName(), record, cache, currentValue, &result); skip {
		return r
	}

	// 3. 处理记录（GoDaddy 的 PUT 为「替换」语义，天然幂等）
	var updateErr error

	if record.Type == RecordTypeCNAME {
		// CNAME 与任何其他类型互斥，先删除同名下的非 CNAME 记录
		for _, rec := range existing.otherRecords {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 前删除冲突记录 [类型=%s, 值=%s]", g.GetServiceName(), rec.Type, rec.Data)
			if deleteErr := g.deleteRecordType(rec.Type); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}
		}

		if len(existing.cnameRecords) > 0 && g.equalCNAME(existing.cnameRecords[0].Data, currentValue) {
			helper.Debug(helper.LogTypeDDNS, "[%s] [CNAME] 记录值未变化，无需更新 [值=%s]", g.GetServiceName(), currentValue)
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 设置 CNAME 记录 [值=%s]", g.GetServiceName(), currentValue)
			updateErr = g.putRecord(record.Type, currentValue)
		}
	} else {
		// A/AAAA/TXT 与 CNAME 互斥，先删除同名下的 CNAME 记录
		if len(existing.cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 %s 前删除冲突的 CNAME 记录", g.GetServiceName(), record.Type, record.Type)
			if deleteErr := g.deleteRecordType(RecordTypeCNAME); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}
		}

		targetRecord := existing.otherRecords[record.Type]
		if targetRecord != nil && targetRecord.Data == currentValue {
			helper.Debug(helper.LogTypeDDNS, "[%s] [%s] 记录值未变化，无需更新 [值=%s]", g.GetServiceName(), record.Type, currentValue)
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 设置 DNS 记录 [值=%s]", g.GetServiceName(), record.Type, currentValue)
			updateErr = g.putRecord(record.Type, currentValue)
		}
	}

	if updateErr != nil {
		result.Status = UpdatedFailed
		result.ErrorMessage = updateErr.Error()
		result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
		return result
	}

	// 4. 更新缓存
	finalizeSuccess(g.GetServiceName(), record, cache, currentValue, &result)
	return result
}

// listHostRecords 查询根域名下与当前主机记录匹配的所有 DNS 记录
func (g *GoDaddy) listHostRecords() ([]goDaddyRecord, error) {
	rootDomain := getRootDomain(g.Group.Domain)
	host := getHostRecord(g.Group.Domain)

	var records []goDaddyRecord
	if err := g.request(http.MethodGet, fmt.Sprintf("/domains/%s/records", rootDomain), nil, &records); err != nil {
		return nil, err
	}

	matched := make([]goDaddyRecord, 0, len(records))
	for _, rec := range records {
		if rec.Name == host {
			matched = append(matched, rec)
		}
	}
	return matched, nil
}

// putRecord 替换指定类型的记录（PUT 为覆盖语义）
func (g *GoDaddy) putRecord(recordType, value string) error {
	rootDomain := getRootDomain(g.Group.Domain)
	host := getHostRecord(g.Group.Domain)
	body := []map[string]interface{}{
		{"data": value, "ttl": g.parseTTL()},
	}

	urlPath := fmt.Sprintf("/domains/%s/records/%s/%s", rootDomain, recordType, url.PathEscape(host))
	if err := g.request(http.MethodPut, urlPath, body, nil); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 设置 DNS 记录失败 [值=%s, 错误=%v]", g.GetServiceName(), recordType, value, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 设置 DNS 记录成功 [值=%s]", g.GetServiceName(), recordType, value)
	return nil
}

// deleteRecordType 删除同名下指定类型的所有记录
func (g *GoDaddy) deleteRecordType(recordType string) error {
	rootDomain := getRootDomain(g.Group.Domain)
	host := getHostRecord(g.Group.Domain)

	urlPath := fmt.Sprintf("/domains/%s/records/%s/%s", rootDomain, recordType, url.PathEscape(host))
	if err := g.request(http.MethodDelete, urlPath, nil, nil); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 删除 DNS 记录失败 [错误=%v]", g.GetServiceName(), recordType, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 删除 DNS 记录成功", g.GetServiceName(), recordType)
	return nil
}

// request 统一请求方法
func (g *GoDaddy) request(method, urlPath string, body interface{}, result interface{}) error {
	reqURL := goDaddyAPIEndpoint + urlPath

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

	req.Header.Set("Authorization", fmt.Sprintf("sso-key %s:%s", g.Group.AccessKey, g.Group.AccessSecret))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

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

	helper.Debug(helper.LogTypeDDNS, "[%s] API 响应 [状态码=%d, 长度=%d]", g.GetServiceName(), resp.StatusCode, len(responseBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr goDaddyError
		if json.Unmarshal(responseBody, &apiErr) == nil && apiErr.Message != "" {
			return fmt.Errorf("GoDaddy API 错误 [状态码=%d, code=%s, 消息=%s]", resp.StatusCode, apiErr.Code, apiErr.Message)
		}
		helper.Warn(helper.LogTypeDDNS, "[%s] API 响应状态码异常 [状态码=%d, 响应=%s]", g.GetServiceName(), resp.StatusCode, string(responseBody))
		return fmt.Errorf("请求失败 [状态码=%d]", resp.StatusCode)
	}

	// PUT/DELETE 通常返回空体，无需解析
	if result == nil || len(responseBody) == 0 {
		return nil
	}

	if err := json.Unmarshal(responseBody, result); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 解析响应失败: %v", g.GetServiceName(), err)
		helper.Debug(helper.LogTypeDDNS, "[%s] 响应内容: %s", g.GetServiceName(), string(responseBody))
		return fmt.Errorf("解析响应失败: %v", err)
	}
	return nil
}

// equalCNAME 比较 CNAME 值（GoDaddy 返回值可能带末尾点）
func (g *GoDaddy) equalCNAME(a, b string) bool {
	return strings.TrimSuffix(a, ".") == strings.TrimSuffix(b, ".")
}

// parseTTL 解析 TTL 值（GoDaddy 最小 600 秒，默认 600）
func (g *GoDaddy) parseTTL() int {
	ttlStr := strings.ToLower(strings.TrimSpace(g.Group.TTL))
	if ttlStr == "" || ttlStr == "auto" {
		return goDaddyMinTTL
	}

	var seconds int
	switch {
	case strings.HasSuffix(ttlStr, "h"):
		if v, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			seconds = v * 3600
		}
	case strings.HasSuffix(ttlStr, "m"):
		if v, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			seconds = v * 60
		}
	case strings.HasSuffix(ttlStr, "s"):
		if v, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			seconds = v
		}
	default:
		if v, err := strconv.Atoi(ttlStr); err == nil {
			seconds = v
		}
	}

	if seconds < goDaddyMinTTL {
		return goDaddyMinTTL
	}
	return seconds
}
