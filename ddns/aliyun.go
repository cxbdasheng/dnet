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
	DNS           *config.DNS
	Cache         *Cache
	Status        statusType
	configChanged bool // 标记配置是否发生变化（用于触发保存）
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

func (a *Aliyun) ShouldSendWebhook() bool {
	return a.Status == UpdatedSuccess || a.Status == UpdatedFailed
}

// Init 初始化阿里云 DDNS
func (a *Aliyun) Init(dnsConfig *config.DNS, cache *Cache) {
	a.DNS = dnsConfig
	a.Cache = cache

	// 验证配置
	if a.DNS.Domain == "" || a.DNS.AccessKey == "" || a.DNS.AccessSecret == "" {
		a.Status = InitFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败：配置不完整", a.GetServiceName())
		return
	}

	a.Status = InitSuccess
	helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功", a.GetServiceName())
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
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型：%s", a.GetServiceName(), a.DNS.Type)
		a.Status = UpdatedFailed
		return false
	}

	// 2. 检查值是否变化（仅对动态类型检查）
	if IsDynamicType(a.DNS.IPType) {
		// 使用正则感知的缓存键（用于 IPv6 接口类型）
		var cacheKey string
		if a.DNS.IPType == helper.DynamicIPv6Interface && a.DNS.Regex != "" {
			cacheKey = helper.GetIPCacheKeyWithRegex(a.DNS.IPType, a.DNS.Value, a.DNS.Regex)
		} else {
			cacheKey = helper.GetIPCacheKey(a.DNS.IPType, a.DNS.Value)
		}
		valueChanged, oldValue := a.Cache.CheckIPChanged(cacheKey, currentValue)

		// 如果没有变化且已经运行过，跳过更新
		if !valueChanged && a.Cache.HasRun && !ForceCompareGlobal {
			a.Status = UpdatedNothing
			helper.Debug(helper.LogTypeDDNS, "[%s] 值未变化，跳过更新 [当前=%s]", a.GetServiceName(), currentValue)
			return false
		}

		helper.Info(helper.LogTypeDDNS, "[%s] 检测到值变化 [旧值=%s, 新值=%s]", a.GetServiceName(), oldValue, currentValue)
	}

	// 3. 查询现有记录
	record, err := a.describeDomainRecords()
	if err != nil {
		a.Status = UpdatedFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 查询DNS记录失败：%v", a.GetServiceName(), err)
		return false
	}

	// 4. 更新或创建
	var updateErr error
	if record != nil {
		// 记录存在，更新
		helper.Info(helper.LogTypeDDNS, "[%s] 记录已存在 [RecordId=%s, 旧值=%s]", a.GetServiceName(), record.RecordId, record.Value)
		updateErr = a.updateDomainRecord(record.RecordId, currentValue)
	} else {
		// 记录不存在，创建
		helper.Info(helper.LogTypeDDNS, "[%s] 记录不存在，创建新记录", a.GetServiceName())
		updateErr = a.addDomainRecord(currentValue)
	}

	if updateErr != nil {
		a.Status = UpdatedFailed
		return false
	}

	// 5. 更新缓存
	if IsDynamicType(a.DNS.IPType) {
		// 使用正则感知的缓存键（用于 IPv6 接口类型）
		var cacheKey string
		if a.DNS.IPType == helper.DynamicIPv6Interface && a.DNS.Regex != "" {
			cacheKey = helper.GetIPCacheKeyWithRegex(a.DNS.IPType, a.DNS.Value, a.DNS.Regex)
		} else {
			cacheKey = helper.GetIPCacheKey(a.DNS.IPType, a.DNS.Value)
		}
		a.Cache.UpdateDynamicIP(cacheKey, currentValue)
	}

	a.Cache.HasRun = true
	a.Cache.TimesFailed = 0
	a.Status = UpdatedSuccess
	a.configChanged = true

	helper.Info(helper.LogTypeDDNS, "[%s] DNS 记录更新成功 [类型=%s, 值=%s]", a.GetServiceName(), a.DNS.Type, currentValue)
	return true
}

// describeDomainRecords 查询 DNS 记录
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
		helper.Error(helper.LogTypeDDNS, "[%s] 更新DNS记录失败 [RecordId=%s, 错误=%v]", a.GetServiceName(), recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 更新DNS记录成功 [RecordId=%s, 新值=%s]", a.GetServiceName(), recordId, value)
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
		helper.Error(helper.LogTypeDDNS, "[%s] 创建DNS记录失败 [域名=%s, 错误=%v]", a.GetServiceName(), a.DNS.Domain, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 创建DNS记录成功 [RecordId=%s, 值=%s]", a.GetServiceName(), response.RecordId, value)
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
func (a *Aliyun) getHostRecord() string {
	domain := a.DNS.Domain

	// 处理泛域名
	if strings.HasPrefix(domain, "*.") {
		return "*"
	}

	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return "@" // 根域名使用 @
	}

	// 返回除根域名外的所有部分
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
