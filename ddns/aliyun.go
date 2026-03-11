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
	Group  *config.DNSGroup
	Caches []*Cache
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

func (a *Aliyun) GetServiceName() string {
	if a.Group == nil {
		return ""
	}
	if a.Group.Name != "" {
		return a.Group.Name
	}
	return a.Group.Domain
}

// Init 初始化阿里云 DDNS（批量处理模式）
func (a *Aliyun) Init(group *config.DNSGroup, caches []*Cache) {
	a.Group = group
	a.Caches = caches

	// 验证配置
	if a.Group.Domain == "" || a.Group.AccessKey == "" || a.Group.AccessSecret == "" {
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", a.GetServiceName())
		return
	}

	// 只在第一次初始化时打印日志（使用 caches 长度即为有效记录数，无需额外遍历）
	if len(a.Caches) > 0 && !a.Caches[0].HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功，共 %d 条记录", a.GetServiceName(), len(a.Caches))
	}
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
	// 1. 预先过滤有效记录（只遍历一次 Group.Records）
	validRecords := make([]validRecord, 0, len(a.Group.Records))

	cacheIdx := 0
	for i := range a.Group.Records {
		if a.Group.Records[i].Value != "" {
			validRecords = append(validRecords, validRecord{
				record: &a.Group.Records[i],
				cache:  a.Caches[cacheIdx],
			})
			cacheIdx++
		}
	}

	// 如果没有有效记录，直接返回
	if len(validRecords) == 0 {
		return []RecordResult{}
	}

	// 2. 验证配置
	if a.Group.Domain == "" || a.Group.AccessKey == "" || a.Group.AccessSecret == "" {
		return a.createErrorResults(validRecords, InitFailed, "配置不完整")
	}

	// 3. 一次性查询该域名下的所有 DNS 记录（关键优化点：只查询一次）
	allRecords, err := a.describeAllDomainRecords()
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", a.GetServiceName(), err)
		return a.createErrorResults(validRecords, UpdatedFailed, err.Error())
	}

	// 4. 预先解析现有记录（只遍历一次 allRecords）
	existingRecords := a.parseExistingRecords(allRecords)

	// 5. 遍历有效记录，逐一处理
	results := make([]RecordResult, 0, len(validRecords))
	for _, vr := range validRecords {
		result := a.processRecord(vr.record, vr.cache, existingRecords)
		results = append(results, result)
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
			// 只保留每种类型的第一条记录
			if _, exists := existing.otherRecords[allRecords[i].Type]; !exists {
				rec := allRecords[i]
				existing.otherRecords[allRecords[i].Type] = &rec
			}
		}
	}

	return existing
}

// createErrorResults 创建批量错误结果（避免重复代码）
func (a *Aliyun) createErrorResults(validRecords []validRecord, status statusType, errMsg string) []RecordResult {
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
func (a *Aliyun) processRecord(record *config.DNSRecord, cache *Cache, existing *recordsByType) RecordResult {
	result := RecordResult{
		RecordType:    record.Type,
		Status:        UpdatedNothing,
		ShouldWebhook: false,
	}

	// 1. 获取当前值（IP 地址或 CNAME 等）
	var currentValue string
	var ok bool

	switch record.Type {
	case RecordTypeA, RecordTypeAAAA:
		// 动态 IP 类型
		if IsDynamicType(record.IPType) {
			// 如果是 IPv6 接口类型且提供了正则表达式，使用支持正则的函数
			if record.IPType == helper.DynamicIPv6Interface && record.Regex != "" {
				currentValue, ok = helper.GetOrSetDynamicIPWithCacheAndRegex(record.IPType, record.Value, record.Regex)
			} else {
				currentValue, ok = helper.GetOrSetDynamicIPWithCache(record.IPType, record.Value)
			}
			if !ok {
				result.Status = InitGetIPFailed
				result.ShouldWebhook = shouldSendWebhook(cache, InitGetIPFailed)
				result.ErrorMessage = "获取 IP 失败"
				helper.Error(helper.LogTypeDDNS, "[%s] [%s] 获取 IP 失败", a.GetServiceName(), record.Type)
				return result
			}
		} else {
			// 静态 IP
			currentValue = record.Value
		}
	case RecordTypeCNAME, RecordTypeTXT:
		// CNAME 或 TXT 记录，直接使用配置值
		currentValue = record.Value
	default:
		result.Status = UpdatedFailed
		result.ErrorMessage = "不支持的记录类型"
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型: %s", a.GetServiceName(), record.Type)
		return result
	}

	// 2. 检查值是否变化（仅对动态类型检查）
	if IsDynamicType(record.IPType) {
		cacheKey := getCacheKey(record.IPType, record.Value, record.Regex)
		valueChanged, oldValue := cache.CheckIPChanged(cacheKey, currentValue)

		// 检查是否需要强制更新（计数器归零）
		forceUpdate := cache.Times <= 0

		// 如果没有变化且已经运行过且不需要强制更新，跳过更新
		if !valueChanged && cache.HasRun && !ForceCompareGlobal && !forceUpdate {
			result.Status = UpdatedNothing
			result.ShouldWebhook = false
			cache.Times--
			helper.Debug(helper.LogTypeDDNS, "[%s] [%s] 值未变化，跳过更新 [当前=%s, 剩余次数=%d]", a.GetServiceName(), record.Type, currentValue, cache.Times)
			return result
		}

		if forceUpdate && !valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 达到强制更新阈值，执行更新 [值=%s]", a.GetServiceName(), record.Type, currentValue)
		} else if valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] [%s] 检测到值变化 [旧值=%s, 新值=%s]", a.GetServiceName(), record.Type, oldValue, currentValue)
		}
	}

	// 3. 处理记录类型变更（特殊处理 CNAME 类型冲突）
	var updateErr error

	// 如果目标类型是 CNAME，需要先删除该子域下的所有其他类型记录
	if record.Type == RecordTypeCNAME {
		// 计算需要删除的记录数
		totalRecords := len(existing.cnameRecords) + len(existing.otherRecords)
		if totalRecords > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建 CNAME 记录前，需要删除该子域下的所有现有记录 [数量=%d]", a.GetServiceName(), totalRecords)

			// 删除所有 CNAME 记录
			if deleteErr := a.deleteRecords(existing.cnameRecords, "CNAME"); deleteErr != nil {
				result.Status = UpdatedFailed
				result.ErrorMessage = deleteErr.Error()
				result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
				return result
			}

			// 删除所有其他类型记录
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

		// 删除后创建新的 CNAME 记录
		helper.Info(helper.LogTypeDDNS, "[%s] [CNAME] 创建新的 CNAME 记录", a.GetServiceName())
		updateErr = a.addDomainRecord(record.Type, currentValue)
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

		// 处理目标类型记录
		targetRecord := existing.otherRecords[record.Type]
		if targetRecord != nil {
			// 目标类型记录已存在，检查值是否真的需要更新
			if targetRecord.Value == currentValue {
				// 值完全相同，跳过更新（阿里云 API 会返回 DomainRecordDuplicate 错误）
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录值未变化，无需更新 [RecordId=%s, 值=%s]", a.GetServiceName(), record.Type, targetRecord.RecordId, currentValue)
				updateErr = nil
			} else {
				// 值不同，执行更新
				helper.Info(helper.LogTypeDDNS, "[%s] [%s] 记录已存在 [RecordId=%s, 旧值=%s]", a.GetServiceName(), record.Type, targetRecord.RecordId, targetRecord.Value)
				updateErr = a.updateDomainRecord(targetRecord.RecordId, record.Type, currentValue)
			}
		} else {
			// 目标类型记录不存在，创建新记录
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
	if IsDynamicType(record.IPType) {
		cacheKey := getCacheKey(record.IPType, record.Value, record.Regex)
		cache.UpdateDynamicIP(cacheKey, currentValue)
	}

	cache.HasRun = true
	cache.TimesFailed = 0
	cache.ResetTimes()
	result.Status = UpdatedSuccess
	result.ShouldWebhook = shouldSendWebhook(cache, UpdatedSuccess)

	helper.Info(helper.LogTypeDDNS, "[%s] [%s] DNS 记录更新成功 [值=%s]", a.GetServiceName(), record.Type, currentValue)
	return result
}

// getCacheKey 获取缓存键（支持正则表达式）
func getCacheKey(ipType, value, regex string) string {
	if ipType == helper.DynamicIPv6Interface && regex != "" {
		return helper.GetIPCacheKeyWithRegex(ipType, value, regex)
	}
	return helper.GetIPCacheKey(ipType, value)
}

// shouldSendWebhook 判断单条记录是否需要发送 Webhook
func shouldSendWebhook(cache *Cache, status statusType) bool {
	// 成功时重置失败计数并发送通知
	if status == UpdatedSuccess {
		cache.TimesFailed = 0
		return true
	}

	// 失败时累计失败次数
	if status == UpdatedFailed || status == InitGetIPFailed {
		cache.TimesFailed++
		// 连续失败 3 次才发送 Webhook，避免频繁通知
		if cache.TimesFailed >= 3 {
			return true
		}
		return false
	}

	return false
}

// describeAllDomainRecords 查询指定主机记录的所有 DNS 记录（所有类型）
func (a *Aliyun) describeAllDomainRecords() ([]DomainRecord, error) {
	params := url.Values{}
	params.Set("Action", "DescribeDomainRecords")
	params.Set("DomainName", a.getRootDomain())
	params.Set("RRKeyWord", a.getHostRecord())
	params.Set("Version", "2015-01-09")

	var response DescribeDomainRecordsResponse
	err := a.request(http.MethodGet, params, &response)
	if err != nil {
		return nil, err
	}

	// RRKeyWord 为模糊匹配，需精确过滤，避免返回不相关的子域名记录
	hostRecord := a.getHostRecord()
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
	params.Set("RR", a.getHostRecord())
	params.Set("Type", recordType)
	params.Set("Value", value)

	// 处理 TTL
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
	params.Set("DomainName", a.getRootDomain())
	params.Set("RR", a.getHostRecord())
	params.Set("Type", recordType)
	params.Set("Value", value)

	// 处理 TTL
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

// deleteRecords 批量删除 DNS 记录（避免重复代码）
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
	// 调用签名
	signer.AliyunSigner(a.Group.AccessKey, a.Group.AccessSecret, &params, method)

	// 构建请求
	req, err := http.NewRequest(method, aliyunDNSEndpoint, nil)
	if err != nil {
		return err
	}

	req.URL.RawQuery = params.Encode()

	// 发送请求
	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	return helper.GetHTTPResponse(resp, err, result)
}

// getRootDomain 获取根域名
// 例如: www.sub.example.com -> example.com
func (a *Aliyun) getRootDomain() string {
	domain := a.Group.Domain

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

// getHostRecord 获取主机记录
// 例如: www.sub.example.com -> www.sub
// 例如: example.com -> @
// 例如: *.example.com -> *
// 例如: *.a.example.com -> *.a
func (a *Aliyun) getHostRecord() string {
	domain := a.Group.Domain

	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return "@" // 根域名使用 @
	}

	// 处理泛域名：返回除根域名外的所有部分（包括 * 号）
	// 例如: *.a.example.com -> *.a
	// 例如: *.example.com -> *
	return strings.Join(parts[:len(parts)-2], ".")
}

// parseTTL 解析 TTL 值
func (a *Aliyun) parseTTL() int64 {
	if a.Group.TTL == "" || a.Group.TTL == "AUTO" {
		return 600 // 默认 10 分钟
	}

	// 尝试解析数字（秒）
	if ttl, err := strconv.ParseInt(a.Group.TTL, 10, 64); err == nil {
		return ttl
	}

	// 解析时间单位（例如：1m, 10m, 1h）
	ttlStr := strings.ToLower(a.Group.TTL)
	if strings.HasSuffix(ttlStr, "s") {
		// 秒
		if ttl, err := strconv.ParseInt(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl
		}
	} else if strings.HasSuffix(ttlStr, "m") {
		// 分钟
		if ttl, err := strconv.ParseInt(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl * 60
		}
	} else if strings.HasSuffix(ttlStr, "h") {
		// 小时
		if ttl, err := strconv.ParseInt(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl * 3600
		}
	}

	return 600 // 默认值
}
