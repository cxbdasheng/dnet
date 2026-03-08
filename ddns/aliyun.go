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
	DNS    *config.DNS
	Cache  *Cache
	Status statusType
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

// UpdateDomainRecordResponse 更新 DNS 记录响应
type UpdateDomainRecordResponse struct {
	RequestId string `json:"RequestId"`
	RecordId  string `json:"RecordId"`
}

// AddDomainRecordResponse 创建 DNS 记录响应
type AddDomainRecordResponse struct {
	RequestId string `json:"RequestId"`
	RecordId  string `json:"RecordId"`
}

func (a *Aliyun) GetServiceStatus() string {
	return string(a.Status)
}

func (a *Aliyun) GetServiceName() string {
	if a.DNS == nil {
		return ""
	}
	if a.DNS.Name != "" {
		return a.DNS.Name
	}
	return a.DNS.Domain
}

// getCacheKey 获取缓存键（支持正则表达式）
func (a *Aliyun) getCacheKey() string {
	if a.DNS.IPType == helper.DynamicIPv6Interface && a.DNS.Regex != "" {
		return helper.GetIPCacheKeyWithRegex(a.DNS.IPType, a.DNS.Value, a.DNS.Regex)
	}
	return helper.GetIPCacheKey(a.DNS.IPType, a.DNS.Value)
}

func (a *Aliyun) ShouldSendWebhook() bool {
	// 成功时重置失败计数并发送通知
	if a.Status == UpdatedSuccess {
		a.Cache.TimesFailed = 0
		return true
	}

	// 失败时累计失败次数
	if a.Status == UpdatedFailed {
		a.Cache.TimesFailed++
		// 连续失败 3 次才发送 Webhook，避免频繁通知
		if a.Cache.TimesFailed >= 3 {
			helper.Warn(helper.LogTypeDDNS, "[%s] 连续失败 %d 次，发送 Webhook 通知", a.GetServiceName(), a.Cache.TimesFailed)
			return true
		}
		helper.Debug(helper.LogTypeDDNS, "[%s] 失败 %d 次，未达到通知阈值", a.GetServiceName(), a.Cache.TimesFailed)
		return false
	}

	return false
}

// Init 初始化阿里云 DDNS
func (a *Aliyun) Init(dnsConfig *config.DNS, cache *Cache) {
	a.DNS = dnsConfig
	a.Cache = cache

	// 验证配置
	if a.DNS.Domain == "" || a.DNS.AccessKey == "" || a.DNS.AccessSecret == "" {
		a.Status = InitFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", a.GetServiceName())
		return
	}

	a.Status = InitSuccess
	// 只在第一次初始化时打印日志
	if !a.Cache.HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功", a.GetServiceName())
	}
}

// UpdateOrCreateRecord 更新或创建 DNS 记录
func (a *Aliyun) UpdateOrCreateRecord() bool {
	if a.Status == InitFailed {
		return false
	}

	// 1. 获取当前值（IP 地址或 CNAME 等）
	var currentValue string
	var ok bool

	switch a.DNS.Type {
	case RecordTypeA, RecordTypeAAAA:
		// 动态 IP 类型
		if IsDynamicType(a.DNS.IPType) {
			// 如果是 IPv6 接口类型且提供了正则表达式，使用支持正则的函数
			if a.DNS.IPType == helper.DynamicIPv6Interface && a.DNS.Regex != "" {
				currentValue, ok = helper.GetOrSetDynamicIPWithCacheAndRegex(a.DNS.IPType, a.DNS.Value, a.DNS.Regex)
			} else {
				currentValue, ok = helper.GetOrSetDynamicIPWithCache(a.DNS.IPType, a.DNS.Value)
			}
			if !ok {
				a.Status = InitGetIPFailed
				a.Cache.TimesFailed++
				helper.Error(helper.LogTypeDDNS, "[%s] 获取 IP 失败", a.GetServiceName())
				return false
			}
		} else {
			// 静态 IP
			currentValue = a.DNS.Value
		}
	case RecordTypeCNAME, RecordTypeTXT:
		// CNAME 或 TXT 记录，直接使用配置值
		currentValue = a.DNS.Value
	default:
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型: %s", a.GetServiceName(), a.DNS.Type)
		a.Status = UpdatedFailed
		return false
	}

	// 2. 检查值是否变化（仅对动态类型检查）
	if IsDynamicType(a.DNS.IPType) {
		cacheKey := a.getCacheKey()
		valueChanged, oldValue := a.Cache.CheckIPChanged(cacheKey, currentValue)

		// 检查是否需要强制更新（计数器归零）
		forceUpdate := a.Cache.Times <= 0

		// 如果没有变化且已经运行过且不需要强制更新，跳过更新
		if !valueChanged && a.Cache.HasRun && !ForceCompareGlobal && !forceUpdate {
			a.Status = UpdatedNothing
			a.Cache.Times--
			helper.Debug(helper.LogTypeDDNS, "[%s] 值未变化，跳过更新 [当前=%s, 剩余次数=%d]", a.GetServiceName(), currentValue, a.Cache.Times)
			return false
		}

		if forceUpdate && !valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] 达到强制更新阈值，执行更新 [值=%s]", a.GetServiceName(), currentValue)
		} else if valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] 检测到值变化 [旧值=%s, 新值=%s]", a.GetServiceName(), oldValue, currentValue)
		}
	}

	// 3. 查询该子域下的所有记录（所有类型）
	allRecords, err := a.describeAllDomainRecords()
	if err != nil {
		a.Status = UpdatedFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", a.GetServiceName(), err)
		return false
	}

	// 4. 处理记录类型变更（特殊处理 CNAME 类型冲突）
	var updateErr error

	// 如果目标类型是 CNAME，需要先删除该子域下的所有其他类型记录
	if a.DNS.Type == RecordTypeCNAME {
		// 如果存在任何记录，先全部删除
		if len(allRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] 创建 CNAME 记录前，需要删除该子域下的所有现有记录 [数量=%d]", a.GetServiceName(), len(allRecords))
			for _, rec := range allRecords {
				if deleteErr := a.deleteDomainRecord(rec.RecordId); deleteErr != nil {
					a.Status = UpdatedFailed
					helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordId=%s, 类型=%s, 错误=%v]", a.GetServiceName(), rec.RecordId, rec.Type, deleteErr)
					return false
				}
				helper.Info(helper.LogTypeDDNS, "[%s] 已删除 DNS 记录 [RecordId=%s, 类型=%s, 值=%s]", a.GetServiceName(), rec.RecordId, rec.Type, rec.Value)
			}
		}

		// 删除后创建新的 CNAME 记录
		helper.Info(helper.LogTypeDDNS, "[%s] 创建新的 CNAME 记录", a.GetServiceName())
		updateErr = a.addDomainRecord(currentValue)
	} else {
		// 创建非 CNAME 类型记录，需要确保同子域下没有 CNAME 记录
		var cnameRecords []DomainRecord
		var targetRecord *DomainRecord

		for i := range allRecords {
			if allRecords[i].Type == RecordTypeCNAME {
				cnameRecords = append(cnameRecords, allRecords[i])
			} else if allRecords[i].Type == a.DNS.Type && targetRecord == nil {
				// 只取第一条匹配的记录
				record := allRecords[i]
				targetRecord = &record
			}
		}

		// 如果存在 CNAME 记录，需要先删除
		if len(cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", a.GetServiceName(), a.DNS.Type, len(cnameRecords))
			for _, rec := range cnameRecords {
				if deleteErr := a.deleteDomainRecord(rec.RecordId); deleteErr != nil {
					a.Status = UpdatedFailed
					helper.Error(helper.LogTypeDDNS, "[%s] 删除 CNAME 记录失败 [RecordId=%s, 错误=%v]", a.GetServiceName(), rec.RecordId, deleteErr)
					return false
				}
				helper.Info(helper.LogTypeDDNS, "[%s] 已删除 CNAME 记录 [RecordId=%s, 值=%s]", a.GetServiceName(), rec.RecordId, rec.Value)
			}
			// 删除 CNAME 后，目标记录应该重新创建（不能使用旧的 targetRecord）
			targetRecord = nil
		}

		// 处理目标类型记录
		if targetRecord != nil {
			// 目标类型记录已存在，检查值是否真的需要更新
			if targetRecord.Value == currentValue {
				// 值完全相同，跳过更新（阿里云 API 会返回 DomainRecordDuplicate 错误）
				helper.Info(helper.LogTypeDDNS, "[%s] 记录值未变化，无需更新 [RecordId=%s, 值=%s]", a.GetServiceName(), targetRecord.RecordId, currentValue)
				updateErr = nil
			} else {
				// 值不同，执行更新
				helper.Info(helper.LogTypeDDNS, "[%s] 记录已存在 [RecordId=%s, 类型=%s, 旧值=%s]", a.GetServiceName(), targetRecord.RecordId, targetRecord.Type, targetRecord.Value)
				updateErr = a.updateDomainRecord(targetRecord.RecordId, currentValue)
			}
		} else {
			// 目标类型记录不存在，创建新记录
			helper.Info(helper.LogTypeDDNS, "[%s] 记录不存在，创建新记录", a.GetServiceName())
			updateErr = a.addDomainRecord(currentValue)
		}
	}

	if updateErr != nil {
		a.Status = UpdatedFailed
		return false
	}

	// 5. 更新缓存
	if IsDynamicType(a.DNS.IPType) {
		cacheKey := a.getCacheKey()
		a.Cache.UpdateDynamicIP(cacheKey, currentValue)
	}

	a.Cache.HasRun = true
	a.Cache.TimesFailed = 0
	a.Cache.ResetTimes()
	a.Status = UpdatedSuccess

	helper.Info(helper.LogTypeDDNS, "[%s] DNS 记录更新成功 [类型=%s, 值=%s]", a.GetServiceName(), a.DNS.Type, currentValue)
	return true
}

// describeDomainRecords 查询 DNS 记录（返回第一条匹配的记录）
func (a *Aliyun) describeDomainRecords() (*DomainRecord, error) {
	params := url.Values{}
	params.Set("Action", "DescribeDomainRecords")
	params.Set("DomainName", a.getRootDomain())
	params.Set("RRKeyWord", a.getHostRecord())
	params.Set("TypeKeyWord", a.DNS.Type)
	params.Set("Version", "2015-01-09")

	var response DescribeDomainRecordsResponse
	err := a.request(http.MethodGet, params, &response)
	if err != nil {
		return nil, err
	}

	// 返回第一条记录（如果存在）
	if response.TotalCount > 0 {
		return &response.DomainRecords.Record[0], nil
	}

	return nil, nil
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

	return response.DomainRecords.Record, nil
}

// updateDomainRecord 更新 DNS 记录
func (a *Aliyun) updateDomainRecord(recordId, value string) error {
	params := url.Values{}
	params.Set("Action", "UpdateDomainRecord")
	params.Set("RecordId", recordId)
	params.Set("RR", a.getHostRecord())
	params.Set("Type", a.DNS.Type)
	params.Set("Value", value)

	// 处理 TTL
	ttl := a.parseTTL()
	if ttl > 0 {
		params.Set("TTL", strconv.FormatInt(ttl, 10))
	}

	params.Set("Version", "2015-01-09")

	var response UpdateDomainRecordResponse
	err := a.request(http.MethodGet, params, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 更新 DNS 记录失败 [RecordId=%s, 错误=%v]", a.GetServiceName(), recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 更新 DNS 记录成功 [RecordId=%s, 新值=%s]", a.GetServiceName(), recordId, value)
	return nil
}

// addDomainRecord 创建 DNS 记录
func (a *Aliyun) addDomainRecord(value string) error {
	params := url.Values{}
	params.Set("Action", "AddDomainRecord")
	params.Set("DomainName", a.getRootDomain())
	params.Set("RR", a.getHostRecord())
	params.Set("Type", a.DNS.Type)
	params.Set("Value", value)

	// 处理 TTL
	ttl := a.parseTTL()
	if ttl > 0 {
		params.Set("TTL", strconv.FormatInt(ttl, 10))
	}

	params.Set("Version", "2015-01-09")

	var response AddDomainRecordResponse
	err := a.request(http.MethodGet, params, &response)
	if err != nil {
		// 如果记录已存在，尝试查询并更新
		if strings.Contains(err.Error(), "DomainRecordDuplicate") {
			helper.Warn(helper.LogTypeDDNS, "[%s] DNS 记录已存在，尝试查询并更新", a.GetServiceName())

			// 重新查询该类型的记录
			record, queryErr := a.describeDomainRecords()
			if queryErr != nil {
				helper.Error(helper.LogTypeDDNS, "[%s] 查询 DNS 记录失败: %v", a.GetServiceName(), queryErr)
				return err // 返回原始错误
			}

			if record != nil {
				// 找到记录，执行更新
				helper.Info(helper.LogTypeDDNS, "[%s] 找到已存在的记录，执行更新 [RecordId=%s]", a.GetServiceName(), record.RecordId)
				return a.updateDomainRecord(record.RecordId, value)
			}
		}

		helper.Error(helper.LogTypeDDNS, "[%s] 创建 DNS 记录失败 [域名=%s, 错误=%v]", a.GetServiceName(), a.DNS.Domain, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 创建 DNS 记录成功 [RecordId=%s, 值=%s]", a.GetServiceName(), response.RecordId, value)
	return nil
}

// DeleteDomainRecordResponse 删除 DNS 记录响应
type DeleteDomainRecordResponse struct {
	RequestId string `json:"RequestId"`
	RecordId  string `json:"RecordId"`
}

// deleteDomainRecord 删除 DNS 记录
func (a *Aliyun) deleteDomainRecord(recordId string) error {
	params := url.Values{}
	params.Set("Action", "DeleteDomainRecord")
	params.Set("RecordId", recordId)
	params.Set("Version", "2015-01-09")

	var response DeleteDomainRecordResponse
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
	signer.AliyunSigner(a.DNS.AccessKey, a.DNS.AccessSecret, &params, method)

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
	domain := a.DNS.Domain

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
	domain := a.DNS.Domain

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
	if a.DNS.TTL == "" || a.DNS.TTL == "AUTO" {
		return 600 // 默认 10 分钟
	}

	// 尝试解析数字（秒）
	if ttl, err := strconv.ParseInt(a.DNS.TTL, 10, 64); err == nil {
		return ttl
	}

	// 解析时间单位（例如：1m, 10m, 1h）
	ttlStr := strings.ToLower(a.DNS.TTL)
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
