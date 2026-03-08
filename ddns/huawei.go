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
	DNS    *config.DNS
	Cache  *Cache
	Status statusType
	zoneID string // Zone ID 缓存
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

// HuaweiRecordSetResponse 单个记录集响应
type HuaweiRecordSetResponse struct {
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

func (h *Huawei) GetServiceStatus() string {
	return string(h.Status)
}

func (h *Huawei) GetServiceName() string {
	if h.DNS == nil {
		return ""
	}
	if h.DNS.Name != "" {
		return h.DNS.Name
	}
	return h.DNS.Domain
}

// getCacheKey 获取缓存键（支持正则表达式）
func (h *Huawei) getCacheKey() string {
	if h.DNS.IPType == helper.DynamicIPv6Interface && h.DNS.Regex != "" {
		return helper.GetIPCacheKeyWithRegex(h.DNS.IPType, h.DNS.Value, h.DNS.Regex)
	}
	return helper.GetIPCacheKey(h.DNS.IPType, h.DNS.Value)
}

func (h *Huawei) ShouldSendWebhook() bool {
	// 成功时重置失败计数并发送通知
	if h.Status == UpdatedSuccess {
		h.Cache.TimesFailed = 0
		return true
	}

	// 失败时累计失败次数
	if h.Status == UpdatedFailed {
		h.Cache.TimesFailed++
		// 连续失败 3 次才发送 Webhook，避免频繁通知
		if h.Cache.TimesFailed >= 3 {
			helper.Warn(helper.LogTypeDDNS, "[%s] 连续失败 %d 次，发送 Webhook 通知", h.GetServiceName(), h.Cache.TimesFailed)
			return true
		}
		helper.Debug(helper.LogTypeDDNS, "[%s] 失败 %d 次，未达到通知阈值", h.GetServiceName(), h.Cache.TimesFailed)
		return false
	}

	return false
}

// Init 初始化华为云 DDNS
func (h *Huawei) Init(dnsConfig *config.DNS, cache *Cache) {
	h.DNS = dnsConfig
	h.Cache = cache

	// 验证配置
	if h.DNS.Domain == "" || h.DNS.AccessKey == "" || h.DNS.AccessSecret == "" {
		h.Status = InitFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", h.GetServiceName())
		return
	}

	// 获取 Zone ID
	zoneID, err := h.getZoneID()
	if err != nil {
		h.Status = InitFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 获取 Zone ID 失败: %v", h.GetServiceName(), err)
		return
	}
	h.zoneID = zoneID

	h.Status = InitSuccess
	// 只在第一次初始化时打印日志
	if !h.Cache.HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功 [ZoneID=%s]", h.GetServiceName(), zoneID)
	}
}

// UpdateOrCreateRecord 更新或创建 DNS 记录
func (h *Huawei) UpdateOrCreateRecord() bool {
	if h.Status == InitFailed {
		return false
	}

	// 1. 获取当前值（IP 地址或 CNAME 等）
	var currentValue string
	var ok bool

	switch h.DNS.Type {
	case RecordTypeA, RecordTypeAAAA:
		// 动态 IP 类型
		if IsDynamicType(h.DNS.IPType) {
			// 如果是 IPv6 接口类型且提供了正则表达式，使用支持正则的函数
			if h.DNS.IPType == helper.DynamicIPv6Interface && h.DNS.Regex != "" {
				currentValue, ok = helper.GetOrSetDynamicIPWithCacheAndRegex(h.DNS.IPType, h.DNS.Value, h.DNS.Regex)
			} else {
				currentValue, ok = helper.GetOrSetDynamicIPWithCache(h.DNS.IPType, h.DNS.Value)
			}
			if !ok {
				h.Status = InitGetIPFailed
				h.Cache.TimesFailed++
				helper.Error(helper.LogTypeDDNS, "[%s] 获取 IP 失败", h.GetServiceName())
				return false
			}
		} else {
			// 静态 IP
			currentValue = h.DNS.Value
		}
	case RecordTypeCNAME, RecordTypeTXT:
		// CNAME 或 TXT 记录，直接使用配置值
		currentValue = h.DNS.Value
	default:
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型: %s", h.GetServiceName(), h.DNS.Type)
		h.Status = UpdatedFailed
		return false
	}

	// 2. 检查值是否变化（仅对动态类型检查）
	if IsDynamicType(h.DNS.IPType) {
		cacheKey := h.getCacheKey()
		valueChanged, oldValue := h.Cache.CheckIPChanged(cacheKey, currentValue)

		// 检查是否需要强制更新（计数器归零）
		forceUpdate := h.Cache.Times <= 0

		// 如果没有变化且已经运行过且不需要强制更新，跳过更新
		if !valueChanged && h.Cache.HasRun && !ForceCompareGlobal && !forceUpdate {
			h.Status = UpdatedNothing
			h.Cache.Times--
			helper.Debug(helper.LogTypeDDNS, "[%s] 值未变化，跳过更新 [当前=%s, 剩余次数=%d]", h.GetServiceName(), currentValue, h.Cache.Times)
			return false
		}

		if forceUpdate && !valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] 达到强制更新阈值，执行更新 [值=%s]", h.GetServiceName(), currentValue)
		} else if valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] 检测到值变化 [旧值=%s, 新值=%s]", h.GetServiceName(), oldValue, currentValue)
		}
	}

	// 3. 查询该子域下的所有记录（所有类型）
	allRecords, err := h.describeAllRecordSets()
	if err != nil {
		h.Status = UpdatedFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", h.GetServiceName(), err)
		return false
	}

	// 4. 处理记录类型变更（特殊处理 CNAME 类型冲突）
	var updateErr error

	// 如果目标类型是 CNAME，需要先删除该子域下的所有其他类型记录
	if h.DNS.Type == RecordTypeCNAME {
		// 如果存在任何记录，先全部删除
		if len(allRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] 创建 CNAME 记录前，需要删除该子域下的所有现有记录 [数量=%d]", h.GetServiceName(), len(allRecords))
			for _, rec := range allRecords {
				if deleteErr := h.deleteRecordSet(rec.ID); deleteErr != nil {
					h.Status = UpdatedFailed
					helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordID=%s, 类型=%s, 错误=%v]", h.GetServiceName(), rec.ID, rec.Type, deleteErr)
					return false
				}
				helper.Info(helper.LogTypeDDNS, "[%s] 已删除 DNS 记录 [RecordID=%s, 类型=%s, 值=%v]", h.GetServiceName(), rec.ID, rec.Type, rec.Records)
			}
		}

		// 删除后创建新的 CNAME 记录
		helper.Info(helper.LogTypeDDNS, "[%s] 创建新的 CNAME 记录", h.GetServiceName())
		updateErr = h.createRecordSet(currentValue)
	} else {
		// 创建非 CNAME 类型记录，需要确保同子域下没有 CNAME 记录
		var cnameRecords []HuaweiRecordSet
		var targetRecord *HuaweiRecordSet

		for i := range allRecords {
			if allRecords[i].Type == RecordTypeCNAME {
				cnameRecords = append(cnameRecords, allRecords[i])
			} else if allRecords[i].Type == h.DNS.Type && targetRecord == nil {
				// 只取第一条匹配的记录
				record := allRecords[i]
				targetRecord = &record
			}
		}

		// 如果存在 CNAME 记录，需要先删除
		if len(cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", h.GetServiceName(), h.DNS.Type, len(cnameRecords))
			for _, rec := range cnameRecords {
				if deleteErr := h.deleteRecordSet(rec.ID); deleteErr != nil {
					h.Status = UpdatedFailed
					helper.Error(helper.LogTypeDDNS, "[%s] 删除 CNAME 记录失败 [RecordID=%s, 错误=%v]", h.GetServiceName(), rec.ID, deleteErr)
					return false
				}
				helper.Info(helper.LogTypeDDNS, "[%s] 已删除 CNAME 记录 [RecordID=%s, 值=%v]", h.GetServiceName(), rec.ID, rec.Records)
			}
		}

		// 处理目标类型记录
		if targetRecord != nil {
			// 目标类型记录已存在，检查值是否真的需要更新
			if len(targetRecord.Records) == 1 && targetRecord.Records[0] == currentValue {
				// 值完全相同，跳过更新
				helper.Info(helper.LogTypeDDNS, "[%s] 记录值未变化，无需更新 [RecordID=%s, 值=%s]", h.GetServiceName(), targetRecord.ID, currentValue)
				updateErr = nil
			} else {
				// 值不同，执行更新
				helper.Info(helper.LogTypeDDNS, "[%s] 记录已存在 [RecordID=%s, 类型=%s, 旧值=%v]", h.GetServiceName(), targetRecord.ID, targetRecord.Type, targetRecord.Records)
				updateErr = h.updateRecordSet(targetRecord.ID, currentValue)
			}
		} else {
			// 目标类型记录不存在，创建新记录
			helper.Info(helper.LogTypeDDNS, "[%s] 记录不存在，创建新记录", h.GetServiceName())
			updateErr = h.createRecordSet(currentValue)
		}
	}

	if updateErr != nil {
		h.Status = UpdatedFailed
		return false
	}

	// 5. 更新缓存
	if IsDynamicType(h.DNS.IPType) {
		cacheKey := h.getCacheKey()
		h.Cache.UpdateDynamicIP(cacheKey, currentValue)
	}

	h.Cache.HasRun = true
	h.Cache.TimesFailed = 0
	h.Cache.ResetTimes()
	h.Status = UpdatedSuccess

	helper.Info(helper.LogTypeDDNS, "[%s] DNS 记录更新成功 [类型=%s, 值=%s]", h.GetServiceName(), h.DNS.Type, currentValue)
	return true
}

// getZoneID 获取 Zone ID
func (h *Huawei) getZoneID() (string, error) {
	// 从域名中提取根域名
	rootDomain := h.getRootDomain()

	helper.Debug(helper.LogTypeDDNS, "[%s] 正在获取 Zone ID [域名=%s, 根域名=%s]", h.GetServiceName(), h.DNS.Domain, rootDomain)

	// 查询 Zone 列表，华为云需要在域名后加 "."
	zoneName := rootDomain
	if !strings.HasSuffix(zoneName, ".") {
		zoneName += "."
	}

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
	// 华为云 DNS 的完整域名需要带 "."
	fullDomain := h.DNS.Domain
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
func (h *Huawei) createRecordSet(value string) error {
	// 华为云 DNS 的完整域名需要带 "."
	fullDomain := h.DNS.Domain
	if !strings.HasSuffix(fullDomain, ".") {
		fullDomain += "."
	}

	helper.Debug(helper.LogTypeDDNS, "[%s] 正在创建 DNS 记录 [域名=%s, 类型=%s, 值=%s]", h.GetServiceName(), fullDomain, h.DNS.Type, value)

	data := map[string]interface{}{
		"name":    fullDomain,
		"type":    h.DNS.Type,
		"ttl":     h.parseTTL(),
		"records": []string{value},
	}

	var recordResp HuaweiRecordSetResponse
	err := h.request(http.MethodPost, fmt.Sprintf("/v2/zones/%s/recordsets", h.zoneID), data, &recordResp)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 创建 DNS 记录失败 [域名=%s, 错误=%v]", h.GetServiceName(), h.DNS.Domain, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 创建 DNS 记录成功 [RecordID=%s, 值=%s]", h.GetServiceName(), recordResp.ID, value)
	return nil
}

// updateRecordSet 更新 DNS 记录
func (h *Huawei) updateRecordSet(recordID, value string) error {
	helper.Debug(helper.LogTypeDDNS, "[%s] 正在更新 DNS 记录 [RecordID=%s, 类型=%s, 值=%s]", h.GetServiceName(), recordID, h.DNS.Type, value)

	data := map[string]interface{}{
		"records": []string{value},
		"ttl":     h.parseTTL(),
	}

	var recordResp HuaweiRecordSetResponse
	err := h.request(http.MethodPut, fmt.Sprintf("/v2/zones/%s/recordsets/%s", h.zoneID, recordID), data, &recordResp)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 更新 DNS 记录失败 [RecordID=%s, 错误=%v]", h.GetServiceName(), recordID, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 更新 DNS 记录成功 [RecordID=%s, 新值=%s]", h.GetServiceName(), recordID, value)
	return nil
}

// deleteRecordSet 删除 DNS 记录
func (h *Huawei) deleteRecordSet(recordID string) error {
	helper.Debug(helper.LogTypeDDNS, "[%s] 正在删除 DNS 记录 [RecordID=%s]", h.GetServiceName(), recordID)

	var recordResp HuaweiRecordSetResponse
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
	url := huaweiDNSEndpoint + path

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

	// 构建请求
	var req *http.Request
	if reqBody != nil {
		req, err = http.NewRequest(method, url, bytes.NewBuffer(reqBody))
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	// 设置基本请求头
	req.Header.Set("Content-Type", "application/json")

	// 提取 URI 和 Query
	uri := req.URL.Path
	query := req.URL.RawQuery

	// 准备签名需要的请求头
	signHeaders := map[string]string{
		"host":         req.Host,
		"content-type": "application/json",
	}

	// 添加时间戳
	timestamp := signer.GetFormattedTime()
	signHeaders[signer.HeaderXDate] = timestamp
	req.Header.Set(signer.HeaderXDate, timestamp)

	// 调用签名器
	authHeaders := signer.HuaweiSigner(
		h.DNS.AccessKey,
		h.DNS.AccessSecret,
		method,
		uri,
		query,
		signHeaders,
		bodyStr,
	)

	// 添加签名到请求头
	for k, v := range authHeaders {
		req.Header.Set(k, v)
	}

	// 发送请求
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
	domain := h.DNS.Domain

	// 处理泛域名（*.example.com -> example.com）
	if strings.HasPrefix(domain, "*.") {
		domain = domain[2:]
	}

	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return domain
	}

	// 返回最后两部分作为根域名
	return parts[len(parts)-2] + "." + parts[len(parts)-1]
}

// parseTTL 解析 TTL 值
func (h *Huawei) parseTTL() int {
	if h.DNS.TTL == "" || h.DNS.TTL == "AUTO" {
		return 300 // 默认 5 分钟
	}

	// 尝试解析数字（秒）
	if ttl, err := strconv.Atoi(h.DNS.TTL); err == nil {
		// 华为云 TTL 范围: 300-2147483647 秒
		if ttl < 300 {
			return 300
		}
		return ttl
	}

	// 解析时间单位（例如：1m, 10m, 1h）
	ttlStr := strings.ToLower(h.DNS.TTL)
	if strings.HasSuffix(ttlStr, "s") {
		// 秒
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			if ttl < 300 {
				return 300
			}
			return ttl
		}
	} else if strings.HasSuffix(ttlStr, "m") {
		// 分钟
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			seconds := ttl * 60
			if seconds < 300 {
				return 300
			}
			return seconds
		}
	} else if strings.HasSuffix(ttlStr, "h") {
		// 小时
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			return ttl * 3600
		}
	}

	return 300 // 默认值
}
