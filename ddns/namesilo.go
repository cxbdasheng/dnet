package ddns

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

const (
	nameSiloListRecordEndpoint   = "https://www.namesilo.com/api/dnsListRecords?version=1&type=xml&key=%s&domain=%s"
	nameSiloAddRecordEndpoint    = "https://www.namesilo.com/api/dnsAddRecord?version=1&type=xml&key=%s&domain=%s&rrhost=%s&rrtype=%s&rrvalue=%s&rrttl=3600"
	nameSiloUpdateRecordEndpoint = "https://www.namesilo.com/api/dnsUpdateRecord?version=1&type=xml&key=%s&domain=%s&rrhost=%s&rrid=%s&rrvalue=%s&rrttl=3600"
	nameSiloDeleteRecordEndpoint = "https://www.namesilo.com/api/dnsDeleteRecord?version=1&type=xml&key=%s&domain=%s&rrid=%s"
)

type NameSilo struct {
	BaseDNSProvider
}

// nameSiloResp 通用操作响应
type nameSiloResp struct {
	XMLName xml.Name            `xml:"namesilo"`
	Reply   nameSiloReplySimple `xml:"reply"`
}

type nameSiloReplySimple struct {
	Code     int    `xml:"code"`
	Detail   string `xml:"detail"`
	RecordID string `xml:"record_id"`
}

// nameSiloListResp 查询记录列表响应
type nameSiloListResp struct {
	XMLName xml.Name          `xml:"namesilo"`
	Reply   nameSiloReplyList `xml:"reply"`
}

type nameSiloReplyList struct {
	Code          int                    `xml:"code"`
	Detail        string                 `xml:"detail"`
	ResourceItems []nameSiloResourceItem `xml:"resource_record"`
}

type nameSiloResourceItem struct {
	RecordID string `xml:"record_id"`
	Type     string `xml:"type"`
	Host     string `xml:"host"`
	Value    string `xml:"value"`
	TTL      int    `xml:"ttl"`
}

// nameSiloRecordsByType 按类型分组的记录映射
type nameSiloRecordsByType struct {
	cnameRecords []nameSiloResourceItem
	otherRecords map[string]*nameSiloResourceItem
}

// Init 初始化（NameSilo 只需要 Domain 和 AccessSecret 作为 API Key）
func (n *NameSilo) Init(group *config.DNSGroup, caches []*Cache) {
	n.Group = group
	n.Caches = caches
}

// UpdateOrCreateRecords 批量更新或创建 DNS 记录
func (n *NameSilo) UpdateOrCreateRecords() []RecordResult {
	validRecords := filterValidRecords(n.Group, n.Caches)
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	if n.Group.Domain == "" || n.Group.AccessSecret == "" {
		return createErrorResults(validRecords, InitFailed, "配置不完整（需要 Domain 和 API Key）")
	}

	allRecords, err := n.listAllRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", n.GetServiceName(), err)
		return createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	existing := n.parseExistingRecords(allRecords)

	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		results = append(results, n.processRecord(vr.record, vr.cache, existing))
	}
	return results
}

// parseExistingRecords 将现有记录按类型分组
func (n *NameSilo) parseExistingRecords(allRecords []nameSiloResourceItem) *nameSiloRecordsByType {
	existing := &nameSiloRecordsByType{
		cnameRecords: make([]nameSiloResourceItem, 0),
		otherRecords: make(map[string]*nameSiloResourceItem),
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
func (n *NameSilo) processRecord(record *config.DNSRecord, cache *Cache, existing *nameSiloRecordsByType) RecordResult {
	// 1. 获取当前值
	currentValue, result, ok := getCurrentValue(n.GetServiceName(), record, cache)
	if !ok {
		return result
	}

	// 2. 检查缓存
	if skip, r := checkDynamicCache(n.GetServiceName(), record, cache, currentValue); skip {
		return r
	}

	// 3. 处理记录
	var updateErr error

	if record.Type == RecordTypeCNAME {
		if len(existing.otherRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有非 CNAME 记录 [数量=%d]", n.GetServiceName(), len(existing.otherRecords))
			for _, rec := range existing.otherRecords {
				if deleteErr := n.deleteDomainRecord(rec.RecordID); deleteErr != nil {
					result.Status = UpdatedFailed
					result.ErrorMessage = deleteErr.Error()
					result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
					helper.Error(helper.LogTypeDDNS, "[%s] [CNAME] 删除 DNS 记录失败 [RecordId=%s, 类型=%s, 错误=%v]", n.GetServiceName(), rec.RecordID, rec.Type, deleteErr)
					return result
				}
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 已删除 DNS 记录 [RecordId=%s, 类型=%s, 值=%s]", n.GetServiceName(), rec.RecordID, rec.Type, rec.Value)
			}
		}

		if len(existing.cnameRecords) > 0 {
			existingCNAME := existing.cnameRecords[0]
			if existingCNAME.Value == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 记录值未变化，无需更新 [RecordId=%s, 值=%s]", n.GetServiceName(), existingCNAME.RecordID, currentValue)
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 更新已有 CNAME 记录 [RecordId=%s, 旧值=%s]", n.GetServiceName(), existingCNAME.RecordID, existingCNAME.Value)
				updateErr = n.updateDomainRecord(existingCNAME.RecordID, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", n.GetServiceName())
			updateErr = n.addDomainRecord(record.Type, currentValue)
		}
	} else {
		if len(existing.cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", n.GetServiceName(), record.Type, record.Type, len(existing.cnameRecords))
			if deleteErr := n.deleteRecords(existing.cnameRecords, record.Type); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}
		}

		targetRecord := existing.otherRecords[record.Type]
		if targetRecord != nil {
			if targetRecord.Value == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录值未变化，无需更新 [RecordId=%s, 值=%s]", n.GetServiceName(), record.Type, targetRecord.RecordID, currentValue)
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录已存在 [RecordId=%s, 旧值=%s]", n.GetServiceName(), record.Type, targetRecord.RecordID, targetRecord.Value)
				updateErr = n.updateDomainRecord(targetRecord.RecordID, record.Type, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录不存在，创建新记录", n.GetServiceName(), record.Type)
			updateErr = n.addDomainRecord(record.Type, currentValue)
		}
	}

	if updateErr != nil {
		result.Status = UpdatedFailed
		result.ErrorMessage = updateErr.Error()
		result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
		return result
	}

	// 4. 更新缓存
	finalizeSuccess(n.GetServiceName(), record, cache, currentValue, &result)
	return result
}

// listAllRecords 查询指定域名下与当前主机记录匹配的所有 DNS 记录
func (n *NameSilo) listAllRecords() ([]nameSiloResourceItem, error) {
	rootDomain := getRootDomain(n.Group.Domain)
	apiURL := fmt.Sprintf(nameSiloListRecordEndpoint, n.Group.AccessSecret, rootDomain)

	var resp nameSiloListResp
	if err := n.request(apiURL, &resp); err != nil {
		return nil, err
	}
	if resp.Reply.Code != 300 {
		return nil, fmt.Errorf("NameSilo API 错误 [code=%d, detail=%s]", resp.Reply.Code, resp.Reply.Detail)
	}

	// 精确匹配 host 字段（API 返回 FQDN，如 www.example.com）
	expectedHost := n.getExpectedHost()
	var matched []nameSiloResourceItem
	for _, item := range resp.Reply.ResourceItems {
		if item.Host == expectedHost {
			matched = append(matched, item)
		}
	}
	return matched, nil
}

// addDomainRecord 创建 DNS 记录
func (n *NameSilo) addDomainRecord(recordType, value string) error {
	rootDomain := getRootDomain(n.Group.Domain)
	host := getHostRecord(n.Group.Domain)
	apiURL := fmt.Sprintf(nameSiloAddRecordEndpoint, n.Group.AccessSecret, rootDomain, host, recordType, value)

	var resp nameSiloResp
	if err := n.request(apiURL, &resp); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录失败 [域名=%s, 错误=%v]", n.GetServiceName(), recordType, n.Group.Domain, err)
		return err
	}
	if resp.Reply.Code != 300 {
		err := fmt.Errorf("NameSilo API 错误 [code=%d, detail=%s]", resp.Reply.Code, resp.Reply.Detail)
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录失败 [域名=%s, 错误=%v]", n.GetServiceName(), recordType, n.Group.Domain, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 创建 DNS 记录成功 [RecordId=%s, 值=%s]", n.GetServiceName(), recordType, resp.Reply.RecordID, value)
	return nil
}

// updateDomainRecord 更新 DNS 记录
func (n *NameSilo) updateDomainRecord(recordID, recordType, value string) error {
	rootDomain := getRootDomain(n.Group.Domain)
	host := getHostRecord(n.Group.Domain)
	apiURL := fmt.Sprintf(nameSiloUpdateRecordEndpoint, n.Group.AccessSecret, rootDomain, host, recordID, value)

	var resp nameSiloResp
	if err := n.request(apiURL, &resp); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录失败 [RecordId=%s, 错误=%v]", n.GetServiceName(), recordType, recordID, err)
		return err
	}
	if resp.Reply.Code != 300 {
		err := fmt.Errorf("NameSilo API 错误 [code=%d, detail=%s]", resp.Reply.Code, resp.Reply.Detail)
		helper.Error(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录失败 [RecordId=%s, 错误=%v]", n.GetServiceName(), recordType, recordID, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] 更新 DNS 记录成功 [RecordId=%s, 新值=%s]", n.GetServiceName(), recordType, recordID, value)
	return nil
}

// deleteDomainRecord 删除 DNS 记录
func (n *NameSilo) deleteDomainRecord(recordID string) error {
	rootDomain := getRootDomain(n.Group.Domain)
	apiURL := fmt.Sprintf(nameSiloDeleteRecordEndpoint, n.Group.AccessSecret, rootDomain, recordID)

	var resp nameSiloResp
	if err := n.request(apiURL, &resp); err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordId=%s, 错误=%v]", n.GetServiceName(), recordID, err)
		return err
	}
	if resp.Reply.Code != 300 {
		err := fmt.Errorf("NameSilo API 错误 [code=%d, detail=%s]", resp.Reply.Code, resp.Reply.Detail)
		helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordId=%s, 错误=%v]", n.GetServiceName(), recordID, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 删除 DNS 记录成功 [RecordId=%s]", n.GetServiceName(), recordID)
	return nil
}

// deleteRecords 批量删除 DNS 记录
func (n *NameSilo) deleteRecords(records []nameSiloResourceItem, contextType string) error {
	for _, rec := range records {
		if deleteErr := n.deleteDomainRecord(rec.RecordID); deleteErr != nil {
			helper.Error(helper.LogTypeDDNS, "[%s] [%s] 删除 DNS 记录失败 [RecordId=%s, 类型=%s, 错误=%v]", n.GetServiceName(), contextType, rec.RecordID, rec.Type, deleteErr)
			return deleteErr
		}
		helper.Info(helper.LogTypeDDNS, "[%s] [%s] 已删除 DNS 记录 [RecordId=%s, 类型=%s, 值=%s]", n.GetServiceName(), contextType, rec.RecordID, rec.Type, rec.Value)
	}
	return nil
}

// request 发送 GET 请求并解析 XML 响应
func (n *NameSilo) request(apiURL string, result interface{}) error {
	req, err := http.NewRequest(http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return err
	}

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return xml.Unmarshal(body, result)
}

// getExpectedHost 获取 NameSilo API 返回记录中 host 字段的期望值（FQDN）
func (n *NameSilo) getExpectedHost() string {
	host := getHostRecord(n.Group.Domain)
	rootDomain := getRootDomain(n.Group.Domain)
	if host == "@" {
		return rootDomain
	}
	return strings.Join([]string{host, rootDomain}, ".")
}
