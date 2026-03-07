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
	DNS           *config.DNS
	Cache         *Cache
	Status        statusType
	configChanged bool // 标记配置是否发生变化（用于触发保存）
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

type DescribeRecordListRequest struct {
	Domain     string `json:"Domain"`
	Subdomain  string `json:"Subdomain"`
	RecordType string `json:"RecordType"`
	RecordLine string `json:"RecordLine"`
}

type CreateRecordRequest struct {
	Domain     string `json:"Domain"`
	SubDomain  string `json:"SubDomain"`
	RecordType string `json:"RecordType"`
	RecordLine string `json:"RecordLine"`
	Value      string `json:"Value"`
	TTL        uint64 `json:"TTL"`
}

type ModifyRecordRequest struct {
	Domain     string `json:"Domain"`
	SubDomain  string `json:"SubDomain"`
	RecordType string `json:"RecordType"`
	RecordLine string `json:"RecordLine"`
	Value      string `json:"Value"`
	RecordId   uint64 `json:"RecordId"`
	TTL        uint64 `json:"TTL"`
}

func (t *TencentCloud) GetServiceStatus() string {
	return string(t.Status)
}

func (t *TencentCloud) GetServiceName() string {
	if t.DNS == nil {
		return ""
	}
	if t.DNS.Name != "" {
		return t.DNS.Name
	}
	return t.DNS.Domain
}

func (t *TencentCloud) ShouldSendWebhook() bool {
	return t.Status == UpdatedSuccess || t.Status == UpdatedFailed
}

// Init 初始化腾讯云 DDNS
func (t *TencentCloud) Init(dnsConfig *config.DNS, cache *Cache) {
	t.DNS = dnsConfig
	t.Cache = cache

	// 验证配置
	if t.DNS.Domain == "" || t.DNS.AccessKey == "" || t.DNS.AccessSecret == "" {
		t.Status = InitFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败：配置不完整", t.GetServiceName())
		return
	}

	t.Status = InitSuccess
	helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功", t.GetServiceName())
}

// UpdateOrCreateRecord 更新或创建 DNS 记录
func (t *TencentCloud) UpdateOrCreateRecord() bool {
	if t.Status == InitFailed {
		return false
	}

	// 1. 获取当前值（IP 地址或 CNAME 等）
	var currentValue string
	var ok bool

	switch t.DNS.Type {
	case RecordTypeA, RecordTypeAAAA:
		// 动态 IP 类型
		if IsDynamicType(t.DNS.IPType) {
			// 如果是 IPv6 接口类型且提供了正则表达式，使用支持正则的函数
			if t.DNS.IPType == helper.DynamicIPv6Interface && t.DNS.Regex != "" {
				currentValue, ok = helper.GetOrSetDynamicIPWithCacheAndRegex(t.DNS.IPType, t.DNS.Value, t.DNS.Regex)
			} else {
				currentValue, ok = helper.GetOrSetDynamicIPWithCache(t.DNS.IPType, t.DNS.Value)
			}
			if !ok {
				t.Status = InitGetIPFailed
				t.Cache.TimesFailed++
				helper.Error(helper.LogTypeDDNS, "[%s] 获取 IP 失败", t.GetServiceName())
				return false
			}
		} else {
			// 静态 IP
			currentValue = t.DNS.Value
		}
	case RecordTypeCNAME, RecordTypeTXT:
		// CNAME 或 TXT 记录，直接使用配置值
		currentValue = t.DNS.Value
	default:
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型：%s", t.GetServiceName(), t.DNS.Type)
		t.Status = UpdatedFailed
		return false
	}

	// 2. 检查值是否变化（仅对动态类型检查）
	if IsDynamicType(t.DNS.IPType) {
		// 使用正则感知的缓存键（用于 IPv6 接口类型）
		var cacheKey string
		if t.DNS.IPType == helper.DynamicIPv6Interface && t.DNS.Regex != "" {
			cacheKey = helper.GetIPCacheKeyWithRegex(t.DNS.IPType, t.DNS.Value, t.DNS.Regex)
		} else {
			cacheKey = helper.GetIPCacheKey(t.DNS.IPType, t.DNS.Value)
		}
		valueChanged, oldValue := t.Cache.CheckIPChanged(cacheKey, currentValue)

		// 如果没有变化且已经运行过，跳过更新
		if !valueChanged && t.Cache.HasRun && !ForceCompareGlobal {
			t.Status = UpdatedNothing
			helper.Debug(helper.LogTypeDDNS, "[%s] 值未变化，跳过更新 [当前=%s]", t.GetServiceName(), currentValue)
			return false
		}

		helper.Info(helper.LogTypeDDNS, "[%s] 检测到值变化 [旧值=%s, 新值=%s]", t.GetServiceName(), oldValue, currentValue)
	}

	// 3. 查询现有记录
	record, err := t.describeDomainRecords()
	if err != nil {
		t.Status = UpdatedFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 查询DNS记录失败：%v", t.GetServiceName(), err)
		return false
	}

	// 4. 更新或创建
	var updateErr error
	if record != nil {
		// 记录存在，更新
		helper.Info(helper.LogTypeDDNS, "[%s] 记录已存在 [RecordId=%d, 旧值=%s]", t.GetServiceName(), record.RecordId, record.Value)
		updateErr = t.updateDomainRecord(record.RecordId, currentValue)
	} else {
		// 记录不存在，创建
		helper.Info(helper.LogTypeDDNS, "[%s] 记录不存在，创建新记录", t.GetServiceName())
		updateErr = t.addDomainRecord(currentValue)
	}

	if updateErr != nil {
		t.Status = UpdatedFailed
		return false
	}

	// 5. 更新缓存
	if IsDynamicType(t.DNS.IPType) {
		// 使用正则感知的缓存键（用于 IPv6 接口类型）
		var cacheKey string
		if t.DNS.IPType == helper.DynamicIPv6Interface && t.DNS.Regex != "" {
			cacheKey = helper.GetIPCacheKeyWithRegex(t.DNS.IPType, t.DNS.Value, t.DNS.Regex)
		} else {
			cacheKey = helper.GetIPCacheKey(t.DNS.IPType, t.DNS.Value)
		}
		t.Cache.UpdateDynamicIP(cacheKey, currentValue)
	}

	t.Cache.HasRun = true
	t.Cache.TimesFailed = 0
	t.Status = UpdatedSuccess
	t.configChanged = true

	helper.Info(helper.LogTypeDDNS, "[%s] DNS 记录更新成功 [类型=%s, 值=%s]", t.GetServiceName(), t.DNS.Type, currentValue)
	return true
}

// describeDomainRecords 查询 DNS 记录
func (t *TencentCloud) describeDomainRecords() (*TencentCloudRecord, error) {
	requestBody := DescribeRecordListRequest{
		Domain:     t.getRootDomain(),
		Subdomain:  t.getHostRecord(),
		RecordType: t.DNS.Type,
		RecordLine: t.getRecordLine(),
	}

	var response DescribeRecordListResponse
	err := t.request("DescribeRecordList", requestBody, &response)
	if err != nil {
		return nil, err
	}

	// 返回第一条记录（如果存在）
	if response.Response.RecordCountInfo.TotalCount > 0 && len(response.Response.RecordList) > 0 {
		return &response.Response.RecordList[0], nil
	}

	return nil, nil
}

// updateDomainRecord 更新 DNS 记录
func (t *TencentCloud) updateDomainRecord(recordId uint64, value string) error {
	requestBody := ModifyRecordRequest{
		Domain:     t.getRootDomain(),
		SubDomain:  t.getHostRecord(),
		RecordType: t.DNS.Type,
		RecordLine: t.getRecordLine(),
		Value:      value,
		RecordId:   recordId,
		TTL:        t.parseTTL(),
	}

	var response TencentCloudAPIResponse
	err := t.request("ModifyRecord", requestBody, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 更新DNS记录失败 [RecordId=%d, 错误=%v]", t.GetServiceName(), recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 更新DNS记录成功 [RecordId=%d, 新值=%s]", t.GetServiceName(), recordId, value)
	return nil
}

// addDomainRecord 创建 DNS 记录
func (t *TencentCloud) addDomainRecord(value string) error {
	requestBody := CreateRecordRequest{
		Domain:     t.getRootDomain(),
		SubDomain:  t.getHostRecord(),
		RecordType: t.DNS.Type,
		RecordLine: t.getRecordLine(),
		Value:      value,
		TTL:        t.parseTTL(),
	}

	var response TencentCloudAPIResponse
	err := t.request("CreateRecord", requestBody, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 创建DNS记录失败 [域名=%s, 错误=%v]", t.GetServiceName(), t.DNS.Domain, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 创建DNS记录成功 [值=%s]", t.GetServiceName(), value)
	return nil
}

// request 统一请求方法
func (t *TencentCloud) request(action string, body interface{}, result interface{}) error {
	jsonStr, _ := json.Marshal(body)

	req, err := http.NewRequest(
		http.MethodPost,
		tencentCloudDNSEndpoint,
		bytes.NewBuffer(jsonStr),
	)
	if err != nil {
		return err
	}

	// 设置 Action
	req.Header.Set("X-TC-Action", action)

	// 调用签名函数（会设置 Content-Type / Host / Authorization / X-TC-Timestamp）
	signer.TencentSigner(t.DNS.AccessKey, t.DNS.AccessSecret, tencentCloudDNSService, tencentCloudDNSHost, string(jsonStr), req)

	// DNSPod API 版本固定为 2021-03-23
	req.Header.Set("X-TC-Version", tencentCloudDNSVersion)

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err = helper.GetHTTPResponse(resp, err, result); err != nil {
		return err
	}

	// 检查腾讯云 API 业务错误
	if v, ok := result.(*TencentCloudAPIResponse); ok {
		if v.Response.Error != nil {
			return errors.New(v.Response.Error.Code + ": " + v.Response.Error.Message)
		}
	} else if v, ok := result.(*DescribeRecordListResponse); ok {
		if v.Response.Error != nil {
			// 腾讯云在无匹配记录时会返回 ResourceNotFound.NoDataOfRecord，
			// 这不应视为失败，应交给上层走“创建记录”逻辑。
			if action == "DescribeRecordList" && v.Response.Error.Code == "ResourceNotFound.NoDataOfRecord" {
				return nil
			}
			return errors.New(v.Response.Error.Code + ": " + v.Response.Error.Message)
		}
	}

	return nil
}

// getRootDomain 获取根域名
// 例如: www.sub.example.com -> example.com
func (t *TencentCloud) getRootDomain() string {
	domain := t.DNS.Domain

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
func (t *TencentCloud) getHostRecord() string {
	domain := t.DNS.Domain

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

// getRecordLine 获取记录线路
func (t *TencentCloud) getRecordLine() string {
	return "默认"
}

// parseTTL 解析 TTL 值
func (t *TencentCloud) parseTTL() uint64 {
	if t.DNS.TTL == "" || t.DNS.TTL == "AUTO" {
		return 600 // 默认 10 分钟
	}

	// 尝试解析数字（秒）
	if ttl, err := strconv.ParseUint(t.DNS.TTL, 10, 64); err == nil {
		return ttl
	}

	// 解析时间单位（例如：1m, 10m, 1h）
	ttlStr := strings.ToLower(t.DNS.TTL)
	if strings.HasSuffix(ttlStr, "s") {
		// 秒
		if ttl, err := strconv.ParseUint(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl
		}
	} else if strings.HasSuffix(ttlStr, "m") {
		// 分钟
		if ttl, err := strconv.ParseUint(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl * 60
		}
	} else if strings.HasSuffix(ttlStr, "h") {
		// 小时
		if ttl, err := strconv.ParseUint(ttlStr[:len(ttlStr)-1], 10, 64); err == nil {
			return ttl * 3600
		}
	}

	return 600 // 默认值
}
