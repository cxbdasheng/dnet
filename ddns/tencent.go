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

type DeleteRecordRequest struct {
	Domain   string `json:"Domain"`
	RecordId uint64 `json:"RecordId"`
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

// getCacheKey 获取缓存键（支持正则表达式）
func (t *TencentCloud) getCacheKey() string {
	if t.DNS.IPType == helper.DynamicIPv6Interface && t.DNS.Regex != "" {
		return helper.GetIPCacheKeyWithRegex(t.DNS.IPType, t.DNS.Value, t.DNS.Regex)
	}
	return helper.GetIPCacheKey(t.DNS.IPType, t.DNS.Value)
}

func (t *TencentCloud) ShouldSendWebhook() bool {
	// 成功时重置失败计数并发送通知
	if t.Status == UpdatedSuccess {
		t.Cache.TimesFailed = 0
		return true
	}

	// 失败时累计失败次数
	if t.Status == UpdatedFailed {
		t.Cache.TimesFailed++
		// 连续失败 3 次才发送 Webhook，避免频繁通知
		if t.Cache.TimesFailed >= 3 {
			helper.Warn(helper.LogTypeDDNS, "[%s] 连续失败 %d 次，发送 Webhook 通知", t.GetServiceName(), t.Cache.TimesFailed)
			return true
		}
		helper.Debug(helper.LogTypeDDNS, "[%s] 失败 %d 次，未达到通知阈值", t.GetServiceName(), t.Cache.TimesFailed)
		return false
	}

	return false
}

// Init 初始化腾讯云 DDNS
func (t *TencentCloud) Init(dnsConfig *config.DNS, cache *Cache) {
	t.DNS = dnsConfig
	t.Cache = cache

	// 验证配置
	if t.DNS.Domain == "" || t.DNS.AccessKey == "" || t.DNS.AccessSecret == "" {
		t.Status = InitFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", t.GetServiceName())
		return
	}

	t.Status = InitSuccess
	// 只在第一次初始化时打印日志
	if !t.Cache.HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功", t.GetServiceName())
	}
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
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型: %s", t.GetServiceName(), t.DNS.Type)
		t.Status = UpdatedFailed
		return false
	}

	// 2. 检查值是否变化（仅对动态类型检查）
	if IsDynamicType(t.DNS.IPType) {
		cacheKey := t.getCacheKey()
		valueChanged, oldValue := t.Cache.CheckIPChanged(cacheKey, currentValue)

		// 检查是否需要强制更新（计数器归零）
		forceUpdate := t.Cache.Times <= 0

		// 如果没有变化且已经运行过且不需要强制更新，跳过更新
		if !valueChanged && t.Cache.HasRun && !ForceCompareGlobal && !forceUpdate {
			t.Status = UpdatedNothing
			t.Cache.Times--
			helper.Debug(helper.LogTypeDDNS, "[%s] 值未变化，跳过更新 [当前=%s, 剩余次数=%d]", t.GetServiceName(), currentValue, t.Cache.Times)
			return false
		}

		if forceUpdate && !valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] 达到强制更新阈值，执行更新 [值=%s]", t.GetServiceName(), currentValue)
		} else if valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] 检测到值变化 [旧值=%s, 新值=%s]", t.GetServiceName(), oldValue, currentValue)
		}
	}

	// 3. 查询该子域下的所有记录（所有类型）
	allRecords, err := t.describeAllDomainRecords()
	if err != nil {
		t.Status = UpdatedFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", t.GetServiceName(), err)
		return false
	}

	// 4. 处理记录类型变更（特殊处理 CNAME 类型冲突）
	var updateErr error

	// 如果目标类型是 CNAME，需要先删除该子域下的所有其他类型记录
	if t.DNS.Type == RecordTypeCNAME {
		if len(allRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] 创建 CNAME 记录前，需要删除该子域下的所有现有记录 [数量=%d]", t.GetServiceName(), len(allRecords))
			for _, rec := range allRecords {
				if deleteErr := t.deleteDomainRecord(rec.RecordId); deleteErr != nil {
					t.Status = UpdatedFailed
					helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordId=%d, 类型=%s, 错误=%v]", t.GetServiceName(), rec.RecordId, rec.Type, deleteErr)
					return false
				}
				helper.Info(helper.LogTypeDDNS, "[%s] 已删除 DNS 记录 [RecordId=%d, 类型=%s, 值=%s]", t.GetServiceName(), rec.RecordId, rec.Type, rec.Value)
			}
		}

		helper.Info(helper.LogTypeDDNS, "[%s] 创建新的 CNAME 记录", t.GetServiceName())
		updateErr = t.addDomainRecord(currentValue)
	} else {
		// 创建非 CNAME 类型记录，需要确保同子域下没有 CNAME 记录
		var cnameRecords []TencentCloudRecord
		var targetRecord *TencentCloudRecord

		for i := range allRecords {
			if allRecords[i].Type == RecordTypeCNAME {
				cnameRecords = append(cnameRecords, allRecords[i])
			} else if allRecords[i].Type == t.DNS.Type && targetRecord == nil {
				record := allRecords[i]
				targetRecord = &record
			}
		}

		// 如果存在 CNAME 记录，需要先删除
		if len(cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", t.GetServiceName(), t.DNS.Type, len(cnameRecords))
			for _, rec := range cnameRecords {
				if deleteErr := t.deleteDomainRecord(rec.RecordId); deleteErr != nil {
					t.Status = UpdatedFailed
					helper.Error(helper.LogTypeDDNS, "[%s] 删除 CNAME 记录失败 [RecordId=%d, 错误=%v]", t.GetServiceName(), rec.RecordId, deleteErr)
					return false
				}
				helper.Info(helper.LogTypeDDNS, "[%s] 已删除 CNAME 记录 [RecordId=%d, 值=%s]", t.GetServiceName(), rec.RecordId, rec.Value)
			}
			// 删除 CNAME 后，目标记录应该重新创建（不能使用旧的 targetRecord）
			targetRecord = nil
		}

		// 处理目标类型记录
		if targetRecord != nil {
			if targetRecord.Value == currentValue {
				helper.Info(helper.LogTypeDDNS, "[%s] 记录值未变化，无需更新 [RecordId=%d, 值=%s]", t.GetServiceName(), targetRecord.RecordId, currentValue)
				updateErr = nil
			} else {
				helper.Info(helper.LogTypeDDNS, "[%s] 记录已存在 [RecordId=%d, 类型=%s, 旧值=%s]", t.GetServiceName(), targetRecord.RecordId, targetRecord.Type, targetRecord.Value)
				updateErr = t.updateDomainRecord(targetRecord.RecordId, currentValue)
			}
		} else {
			helper.Info(helper.LogTypeDDNS, "[%s] 记录不存在，创建新记录", t.GetServiceName())
			updateErr = t.addDomainRecord(currentValue)
		}
	}

	if updateErr != nil {
		t.Status = UpdatedFailed
		return false
	}

	// 5. 更新缓存
	if IsDynamicType(t.DNS.IPType) {
		cacheKey := t.getCacheKey()
		t.Cache.UpdateDynamicIP(cacheKey, currentValue)
	}

	t.Cache.HasRun = true
	t.Cache.TimesFailed = 0
	t.Cache.ResetTimes()
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

// describeAllDomainRecords 查询指定主机记录的所有 DNS 记录（所有类型）
func (t *TencentCloud) describeAllDomainRecords() ([]TencentCloudRecord, error) {
	requestBody := map[string]string{
		"Domain":    t.getRootDomain(),
		"Subdomain": t.getHostRecord(),
	}

	var response DescribeRecordListResponse
	err := t.request("DescribeRecordList", requestBody, &response)
	if err != nil {
		return nil, err
	}

	return response.Response.RecordList, nil
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
		helper.Error(helper.LogTypeDDNS, "[%s] 更新 DNS 记录失败 [RecordId=%d, 错误=%v]", t.GetServiceName(), recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 更新 DNS 记录成功 [RecordId=%d, 新值=%s]", t.GetServiceName(), recordId, value)
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
		helper.Error(helper.LogTypeDDNS, "[%s] 创建 DNS 记录失败 [域名=%s, 错误=%v]", t.GetServiceName(), t.DNS.Domain, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 创建 DNS 记录成功 [值=%s]", t.GetServiceName(), value)
	return nil
}

// deleteDomainRecord 删除 DNS 记录
func (t *TencentCloud) deleteDomainRecord(recordId uint64) error {
	requestBody := DeleteRecordRequest{
		Domain:   t.getRootDomain(),
		RecordId: recordId,
	}

	var response TencentCloudAPIResponse
	err := t.request("DeleteRecord", requestBody, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordId=%d, 错误=%v]", t.GetServiceName(), recordId, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 删除 DNS 记录成功 [RecordId=%d]", t.GetServiceName(), recordId)
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
// 例如: *.a.example.com -> *.a
func (t *TencentCloud) getHostRecord() string {
	domain := t.DNS.Domain

	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return "@" // 根域名使用 @
	}

	// 处理泛域名：返回除根域名外的所有部分（包括 * 号）
	// 例如: *.a.example.com -> *.a
	// 例如: *.example.com -> *
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
