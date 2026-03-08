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
	DNS    *config.DNS
	Cache  *Cache
	Status statusType
}

// BaiduRecord 百度云 DNS 记录
type BaiduRecord struct {
	RecordID    string `json:"recordId"`    // 记录ID
	Domain      string `json:"domain"`      // 域名
	View        string `json:"view"`        // 视图(默认为空)
	Rdtype      string `json:"rdtype"`      // 记录类型 A/AAAA/CNAME/TXT
	TTL         int    `json:"ttl"`         // TTL值
	Rdata       string `json:"rdata"`       // 记录值
	ZoneName    string `json:"zoneName"`    // Zone名称
	Status      string `json:"status"`      // 状态
	Description string `json:"description"` // 描述
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

func (b *Baidu) GetServiceStatus() string {
	return string(b.Status)
}

func (b *Baidu) GetServiceName() string {
	if b.DNS == nil {
		return ""
	}
	if b.DNS.Name != "" {
		return b.DNS.Name
	}
	return b.DNS.Domain
}

// getCacheKey 获取缓存键（支持正则表达式）
func (b *Baidu) getCacheKey() string {
	if b.DNS.IPType == helper.DynamicIPv6Interface && b.DNS.Regex != "" {
		return helper.GetIPCacheKeyWithRegex(b.DNS.IPType, b.DNS.Value, b.DNS.Regex)
	}
	return helper.GetIPCacheKey(b.DNS.IPType, b.DNS.Value)
}

func (b *Baidu) ShouldSendWebhook() bool {
	// 成功时重置失败计数并发送通知
	if b.Status == UpdatedSuccess {
		b.Cache.TimesFailed = 0
		return true
	}

	// 失败时累计失败次数
	if b.Status == UpdatedFailed {
		b.Cache.TimesFailed++
		// 连续失败 3 次才发送 Webhook，避免频繁通知
		if b.Cache.TimesFailed >= 3 {
			helper.Warn(helper.LogTypeDDNS, "[%s] 连续失败 %d 次，发送 Webhook 通知", b.GetServiceName(), b.Cache.TimesFailed)
			return true
		}
		helper.Debug(helper.LogTypeDDNS, "[%s] 失败 %d 次，未达到通知阈值", b.GetServiceName(), b.Cache.TimesFailed)
		return false
	}

	return false
}

// Init 初始化百度云 DDNS
func (b *Baidu) Init(dnsConfig *config.DNS, cache *Cache) {
	b.DNS = dnsConfig
	b.Cache = cache

	// 验证配置
	if b.DNS.Domain == "" || b.DNS.AccessKey == "" || b.DNS.AccessSecret == "" {
		b.Status = InitFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败: 配置不完整", b.GetServiceName())
		return
	}

	b.Status = InitSuccess
	// 只在第一次初始化时打印日志
	if !b.Cache.HasRun {
		helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功", b.GetServiceName())
	}
}

// UpdateOrCreateRecord 更新或创建 DNS 记录
func (b *Baidu) UpdateOrCreateRecord() bool {
	if b.Status == InitFailed {
		return false
	}

	// 1. 获取当前值（IP 地址或 CNAME 等）
	var currentValue string
	var ok bool

	switch b.DNS.Type {
	case RecordTypeA, RecordTypeAAAA:
		// 动态 IP 类型
		if IsDynamicType(b.DNS.IPType) {
			// 如果是 IPv6 接口类型且提供了正则表达式，使用支持正则的函数
			if b.DNS.IPType == helper.DynamicIPv6Interface && b.DNS.Regex != "" {
				currentValue, ok = helper.GetOrSetDynamicIPWithCacheAndRegex(b.DNS.IPType, b.DNS.Value, b.DNS.Regex)
			} else {
				currentValue, ok = helper.GetOrSetDynamicIPWithCache(b.DNS.IPType, b.DNS.Value)
			}
			if !ok {
				b.Status = InitGetIPFailed
				b.Cache.TimesFailed++
				helper.Error(helper.LogTypeDDNS, "[%s] 获取 IP 失败", b.GetServiceName())
				return false
			}
		} else {
			// 静态 IP
			currentValue = b.DNS.Value
		}
	case RecordTypeCNAME, RecordTypeTXT:
		// CNAME 或 TXT 记录，直接使用配置值
		currentValue = b.DNS.Value
	default:
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型: %s", b.GetServiceName(), b.DNS.Type)
		b.Status = UpdatedFailed
		return false
	}

	// 2. 检查值是否变化（仅对动态类型检查）
	if IsDynamicType(b.DNS.IPType) {
		cacheKey := b.getCacheKey()
		valueChanged, oldValue := b.Cache.CheckIPChanged(cacheKey, currentValue)

		// 检查是否需要强制更新（计数器归零）
		forceUpdate := b.Cache.Times <= 0

		// 如果没有变化且已经运行过且不需要强制更新，跳过更新
		if !valueChanged && b.Cache.HasRun && !ForceCompareGlobal && !forceUpdate {
			b.Status = UpdatedNothing
			b.Cache.Times--
			helper.Debug(helper.LogTypeDDNS, "[%s] 值未变化，跳过更新 [当前=%s, 剩余次数=%d]", b.GetServiceName(), currentValue, b.Cache.Times)
			return false
		}

		if forceUpdate && !valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] 达到强制更新阈值，执行更新 [值=%s]", b.GetServiceName(), currentValue)
		} else if valueChanged {
			helper.Info(helper.LogTypeDDNS, "[%s] 检测到值变化 [旧值=%s, 新值=%s]", b.GetServiceName(), oldValue, currentValue)
		}
	}

	// 3. 查询该子域下的所有记录（所有类型）
	allRecords, err := b.listAllRecords()
	if err != nil {
		b.Status = UpdatedFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 查询所有 DNS 记录失败: %v", b.GetServiceName(), err)
		return false
	}

	// 4. 处理记录类型变更（特殊处理 CNAME 类型冲突）
	var updateErr error

	// 如果目标类型是 CNAME，需要先删除该子域下的所有其他类型记录
	if b.DNS.Type == RecordTypeCNAME {
		// 如果存在任何记录，先全部删除
		if len(allRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] 创建 CNAME 记录前，需要删除该子域下的所有现有记录 [数量=%d]", b.GetServiceName(), len(allRecords))
			for _, rec := range allRecords {
				if deleteErr := b.deleteRecord(rec.RecordID); deleteErr != nil {
					b.Status = UpdatedFailed
					helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordID=%s, 类型=%s, 错误=%v]", b.GetServiceName(), rec.RecordID, rec.Rdtype, deleteErr)
					return false
				}
				helper.Info(helper.LogTypeDDNS, "[%s] 已删除 DNS 记录 [RecordID=%s, 类型=%s, 值=%s]", b.GetServiceName(), rec.RecordID, rec.Rdtype, rec.Rdata)
			}
		}

		// 删除后创建新的 CNAME 记录
		helper.Info(helper.LogTypeDDNS, "[%s] 创建新的 CNAME 记录", b.GetServiceName())
		updateErr = b.addRecord(currentValue)
	} else {
		// 创建非 CNAME 类型记录，需要确保同子域下没有 CNAME 记录
		var cnameRecords []BaiduRecord
		var targetRecord *BaiduRecord

		for i := range allRecords {
			if allRecords[i].Rdtype == RecordTypeCNAME {
				cnameRecords = append(cnameRecords, allRecords[i])
			} else if allRecords[i].Rdtype == b.DNS.Type && targetRecord == nil {
				// 只取第一条匹配的记录
				record := allRecords[i]
				targetRecord = &record
			}
		}

		// 如果存在 CNAME 记录，需要先删除
		if len(cnameRecords) > 0 {
			helper.Info(helper.LogTypeDDNS, "[%s] 创建 %s 记录前，检测到 CNAME 记录，需要删除 [数量=%d]", b.GetServiceName(), b.DNS.Type, len(cnameRecords))
			for _, rec := range cnameRecords {
				if deleteErr := b.deleteRecord(rec.RecordID); deleteErr != nil {
					b.Status = UpdatedFailed
					helper.Error(helper.LogTypeDDNS, "[%s] 删除 CNAME 记录失败 [RecordID=%s, 错误=%v]", b.GetServiceName(), rec.RecordID, deleteErr)
					return false
				}
				helper.Info(helper.LogTypeDDNS, "[%s] 已删除 CNAME 记录 [RecordID=%s, 值=%s]", b.GetServiceName(), rec.RecordID, rec.Rdata)
			}
		}

		// 处理目标类型记录
		if targetRecord != nil {
			// 目标类型记录已存在，检查值是否真的需要更新
			if targetRecord.Rdata == currentValue {
				// 值完全相同，跳过更新
				helper.Info(helper.LogTypeDDNS, "[%s] 记录值未变化，无需更新 [RecordID=%s, 值=%s]", b.GetServiceName(), targetRecord.RecordID, currentValue)
				updateErr = nil
			} else {
				// 值不同，执行更新
				helper.Info(helper.LogTypeDDNS, "[%s] 记录已存在 [RecordID=%s, 类型=%s, 旧值=%s]", b.GetServiceName(), targetRecord.RecordID, targetRecord.Rdtype, targetRecord.Rdata)
				updateErr = b.updateRecord(targetRecord.RecordID, currentValue)
			}
		} else {
			// 目标类型记录不存在，创建新记录
			helper.Info(helper.LogTypeDDNS, "[%s] 记录不存在，创建新记录", b.GetServiceName())
			updateErr = b.addRecord(currentValue)
		}
	}

	if updateErr != nil {
		b.Status = UpdatedFailed
		return false
	}

	// 5. 更新缓存
	if IsDynamicType(b.DNS.IPType) {
		cacheKey := b.getCacheKey()
		b.Cache.UpdateDynamicIP(cacheKey, currentValue)
	}

	b.Cache.HasRun = true
	b.Cache.TimesFailed = 0
	b.Cache.ResetTimes()
	b.Status = UpdatedSuccess

	helper.Info(helper.LogTypeDDNS, "[%s] DNS 记录更新成功 [类型=%s, 值=%s]", b.GetServiceName(), b.DNS.Type, currentValue)
	return true
}

// listAllRecords 查询指定域名的所有 DNS 记录
func (b *Baidu) listAllRecords() ([]BaiduRecord, error) {
	zoneName := b.getRootDomain()
	rr := b.getHostRecord()

	helper.Debug(helper.LogTypeDDNS, "[%s] 正在查询所有 DNS 记录 [Zone=%s, RR=%s]", b.GetServiceName(), zoneName, rr)

	// 构建查询参数
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

// addRecord 添加 DNS 记录
func (b *Baidu) addRecord(value string) error {
	zoneName := b.getRootDomain()

	helper.Debug(helper.LogTypeDDNS, "[%s] 正在创建 DNS 记录 [Zone=%s, 类型=%s, 值=%s]", b.GetServiceName(), zoneName, b.DNS.Type, value)

	// 构建请求数据
	data := map[string]interface{}{
		"rr":    b.getHostRecord(),
		"type":  b.DNS.Type,
		"value": value,
		"ttl":   b.parseTTL(),
		"line":  "default", // 默认线路
	}

	path := fmt.Sprintf("/v1/dns/zone/%s/record", zoneName)

	var response BaiduRecordResponse
	err := b.request(http.MethodPost, path, data, &response)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 创建 DNS 记录失败 [Zone=%s, 错误=%v]", b.GetServiceName(), zoneName, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 创建 DNS 记录成功 [RecordID=%s, 值=%s]", b.GetServiceName(), response.RecordID, value)
	return nil
}

// updateRecord 更新 DNS 记录
func (b *Baidu) updateRecord(recordID, value string) error {
	zoneName := b.getRootDomain()

	helper.Debug(helper.LogTypeDDNS, "[%s] 正在更新 DNS 记录 [RecordID=%s, 类型=%s, 值=%s]", b.GetServiceName(), recordID, b.DNS.Type, value)

	// 构建请求数据
	data := map[string]interface{}{
		"rr":    b.getHostRecord(),
		"type":  b.DNS.Type,
		"value": value,
		"ttl":   b.parseTTL(),
	}

	path := fmt.Sprintf("/v1/dns/zone/%s/record/%s", zoneName, recordID)

	err := b.request(http.MethodPut, path, data, nil)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 更新 DNS 记录失败 [RecordID=%s, 错误=%v]", b.GetServiceName(), recordID, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 更新 DNS 记录成功 [RecordID=%s, 新值=%s]", b.GetServiceName(), recordID, value)
	return nil
}

// deleteRecord 删除 DNS 记录
func (b *Baidu) deleteRecord(recordID string) error {
	zoneName := b.getRootDomain()

	helper.Debug(helper.LogTypeDDNS, "[%s] 正在删除 DNS 记录 [RecordID=%s]", b.GetServiceName(), recordID)

	path := fmt.Sprintf("/v1/dns/zone/%s/record/%s", zoneName, recordID)

	err := b.request(http.MethodDelete, path, nil, nil)
	if err != nil {
		helper.Error(helper.LogTypeDDNS, "[%s] 删除 DNS 记录失败 [RecordID=%s, 错误=%v]", b.GetServiceName(), recordID, err)
		return err
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 删除 DNS 记录成功 [RecordID=%s]", b.GetServiceName(), recordID)
	return nil
}

// request 统一请求方法
func (b *Baidu) request(method, path string, body interface{}, result interface{}) error {
	url := baiduDNSEndpoint + path

	var reqBody []byte
	var err error

	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("序列化请求数据失败: %v", err)
		}
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

	// 调用百度云签名器
	signer.BaiduSigner(b.DNS.AccessKey, b.DNS.AccessSecret, req)

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

	helper.Debug(helper.LogTypeDDNS, "[%s] API 响应 [状态码=%d, 长度=%d]", b.GetServiceName(), resp.StatusCode, len(responseBody))

	// 百度云 API 返回 2xx 表示成功
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

// getRootDomain 获取根域名
// 例如: www.sub.example.com -> example.com
func (b *Baidu) getRootDomain() string {
	domain := b.DNS.Domain

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
func (b *Baidu) getHostRecord() string {
	domain := b.DNS.Domain

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
func (b *Baidu) parseTTL() int {
	if b.DNS.TTL == "" || b.DNS.TTL == "AUTO" {
		return 600 // 默认 10 分钟
	}

	// 尝试解析数字（秒）
	if ttl, err := strconv.Atoi(b.DNS.TTL); err == nil {
		// 百度云 TTL 范围通常是 60-86400 秒
		if ttl < 60 {
			return 60
		}
		if ttl > 86400 {
			return 86400
		}
		return ttl
	}

	// 解析时间单位（例如：1m, 10m, 1h）
	ttlStr := strings.ToLower(b.DNS.TTL)
	if strings.HasSuffix(ttlStr, "s") {
		// 秒
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			if ttl < 60 {
				return 60
			}
			if ttl > 86400 {
				return 86400
			}
			return ttl
		}
	} else if strings.HasSuffix(ttlStr, "m") {
		// 分钟
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			seconds := ttl * 60
			if seconds < 60 {
				return 60
			}
			if seconds > 86400 {
				return 86400
			}
			return seconds
		}
	} else if strings.HasSuffix(ttlStr, "h") {
		// 小时
		if ttl, err := strconv.Atoi(ttlStr[:len(ttlStr)-1]); err == nil {
			seconds := ttl * 3600
			if seconds > 86400 {
				return 86400
			}
			return seconds
		}
	}

	return 600 // 默认值
}
