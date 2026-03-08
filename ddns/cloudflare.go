package ddns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

const cloudflareAPIEndpoint = "https://api.cloudflare.com/client/v4"

type Cloudflare struct {
	DNS           *config.DNS
	Cache         *Cache
	Status        statusType
	configChanged bool   // 标记配置是否发生变化（用于触发保存）
	zoneID        string // Zone ID 缓存
}

// CloudflareAPIError Cloudflare API 错误
type CloudflareAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// CloudflareAPIMessage Cloudflare API 消息
type CloudflareAPIMessage struct {
	Message          string `json:"message"`
	DocumentationURL string `json:"documentation_url,omitempty"`
}

// CloudflareZonesResponse Zone 列表响应
type CloudflareZonesResponse struct {
	Success  bool                   `json:"success"`
	Errors   []CloudflareAPIError   `json:"errors"`
	Messages []CloudflareAPIMessage `json:"messages"`
	Result   []CloudflareZoneResult `json:"result"`
}

type CloudflareZoneResult struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CloudflareDNSRecord DNS 记录
type CloudflareDNSRecord struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Content    string                 `json:"content"`
	TTL        int                    `json:"ttl"`
	Proxied    bool                   `json:"proxied"`
	Proxiable  bool                   `json:"proxiable,omitempty"`   // A/AAAA/CNAME 可代理标识
	Settings   map[string]interface{} `json:"settings,omitempty"`    // CNAME 等类型的特殊设置
	Meta       map[string]interface{} `json:"meta,omitempty"`        // 元数据
	Comment    *string                `json:"comment,omitempty"`     // 注释
	Tags       []string               `json:"tags,omitempty"`        // 标签
	CreatedOn  string                 `json:"created_on,omitempty"`  // 创建时间
	ModifiedOn string                 `json:"modified_on,omitempty"` // 修改时间
}

// CloudflareDNSRecordResponse DNS 记录响应
type CloudflareDNSRecordResponse struct {
	Success    bool                   `json:"success"`
	Errors     []CloudflareAPIError   `json:"errors"`
	Messages   []CloudflareAPIMessage `json:"messages"`
	Result     *CloudflareDNSRecord   `json:"result,omitempty"`
	ResultInfo *CloudflareResultInfo  `json:"result_info,omitempty"`
}

// CloudflareDNSRecordsResponse DNS 记录列表响应
type CloudflareDNSRecordsResponse struct {
	Success    bool                   `json:"success"`
	Errors     []CloudflareAPIError   `json:"errors"`
	Messages   []CloudflareAPIMessage `json:"messages"`
	Result     []CloudflareDNSRecord  `json:"result"`
	ResultInfo *CloudflareResultInfo  `json:"result_info,omitempty"`
}

type CloudflareResultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
}

func (cf *Cloudflare) GetServiceStatus() string {
	return string(cf.Status)
}

func (cf *Cloudflare) GetServiceName() string {
	if cf.DNS == nil {
		return ""
	}
	if cf.DNS.Name != "" {
		return cf.DNS.Name
	}
	return cf.DNS.Domain
}

func (cf *Cloudflare) ShouldSendWebhook() bool {
	return cf.Status == UpdatedSuccess || cf.Status == UpdatedFailed
}

// Init 初始化 Cloudflare DDNS
func (cf *Cloudflare) Init(dnsConfig *config.DNS, cache *Cache) {
	cf.DNS = dnsConfig
	cf.Cache = cache

	// 验证配置
	if err := cf.validateConfig(); err != nil {
		cf.Status = InitFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 初始化失败：%v", cf.GetServiceName(), err)
		return
	}

	// 获取 Zone ID
	zoneID, err := cf.getZoneID()
	if err != nil {
		cf.Status = InitFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 获取 Zone ID 失败：%v", cf.GetServiceName(), err)
		return
	}
	cf.zoneID = zoneID

	cf.Status = InitSuccess
	helper.Info(helper.LogTypeDDNS, "[%s] 初始化成功 [ZoneID=%s]", cf.GetServiceName(), zoneID)
}

// validateConfig 验证配置
func (cf *Cloudflare) validateConfig() error {
	if cf.DNS.Domain == "" {
		return fmt.Errorf("域名为空")
	}
	if strings.TrimSpace(cf.DNS.AccessKey) == "" {
		return fmt.Errorf("API Token 为空")
	}
	return nil
}

// UpdateOrCreateRecord 更新或创建 DNS 记录
func (cf *Cloudflare) UpdateOrCreateRecord() bool {
	if cf.Status == InitFailed {
		return false
	}

	// 1. 获取当前值（IP 地址或 CNAME 等）
	var currentValue string
	var ok bool

	switch cf.DNS.Type {
	case RecordTypeA, RecordTypeAAAA:
		// 动态 IP 类型
		if IsDynamicType(cf.DNS.IPType) {
			// 如果是 IPv6 接口类型且提供了正则表达式，使用支持正则的函数
			if cf.DNS.IPType == helper.DynamicIPv6Interface && cf.DNS.Regex != "" {
				currentValue, ok = helper.GetOrSetDynamicIPWithCacheAndRegex(cf.DNS.IPType, cf.DNS.Value, cf.DNS.Regex)
			} else {
				currentValue, ok = helper.GetOrSetDynamicIPWithCache(cf.DNS.IPType, cf.DNS.Value)
			}
			if !ok {
				cf.Status = InitGetIPFailed
				cf.Cache.TimesFailed++
				helper.Error(helper.LogTypeDDNS, "[%s] 获取 IP 失败", cf.GetServiceName())
				return false
			}
		} else {
			// 静态 IP
			currentValue = cf.DNS.Value
		}
	case RecordTypeCNAME, RecordTypeTXT:
		// CNAME 或 TXT 记录，直接使用配置值
		currentValue = cf.DNS.Value
	default:
		helper.Error(helper.LogTypeDDNS, "[%s] 不支持的记录类型：%s", cf.GetServiceName(), cf.DNS.Type)
		cf.Status = UpdatedFailed
		return false
	}

	// 2. 检查值是否变化（仅对动态类型检查）
	if IsDynamicType(cf.DNS.IPType) {
		// 使用正则感知的缓存键（用于 IPv6 接口类型）
		var cacheKey string
		if cf.DNS.IPType == helper.DynamicIPv6Interface && cf.DNS.Regex != "" {
			cacheKey = helper.GetIPCacheKeyWithRegex(cf.DNS.IPType, cf.DNS.Value, cf.DNS.Regex)
		} else {
			cacheKey = helper.GetIPCacheKey(cf.DNS.IPType, cf.DNS.Value)
		}
		valueChanged, oldValue := cf.Cache.CheckIPChanged(cacheKey, currentValue)

		// 如果没有变化且已经运行过，跳过更新
		if !valueChanged && cf.Cache.HasRun && !ForceCompareGlobal {
			cf.Status = UpdatedNothing
			helper.Debug(helper.LogTypeDDNS, "[%s] 值未变化，跳过更新 [当前=%s]", cf.GetServiceName(), currentValue)
			return false
		}

		helper.Info(helper.LogTypeDDNS, "[%s] 检测到值变化 [旧值=%s, 新值=%s]", cf.GetServiceName(), oldValue, currentValue)
	}

	// 3. 查询现有记录（先按当前类型查询）
	record, err := cf.getDNSRecord()
	if err != nil {
		cf.Status = UpdatedFailed
		helper.Error(helper.LogTypeDDNS, "[%s] 查询DNS记录失败：%v", cf.GetServiceName(), err)
		return false
	}

	// 4. 如果没找到记录，查询该域名下的所有记录（可能是类型变更）
	var allRecords []CloudflareDNSRecord
	if record == nil {
		allRecords, err = cf.getAllDNSRecords()
		if err != nil {
			cf.Status = UpdatedFailed
			helper.Error(helper.LogTypeDDNS, "[%s] 查询所有DNS记录失败：%v", cf.GetServiceName(), err)
			return false
		}
	}

	// 5. 更新或创建
	var updateErr error
	if record != nil {
		// 记录存在且类型匹配，直接更新
		helper.Info(helper.LogTypeDDNS, "[%s] 记录已存在 [RecordID=%s, 类型=%s, 旧值=%s]", cf.GetServiceName(), record.ID, record.Type, record.Content)
		updateErr = cf.updateDNSRecord(record.ID, currentValue)
	} else if len(allRecords) > 0 {
		// 域名存在但类型不同，直接修改记录类型和内容
		oldRecord := allRecords[0] // 使用第一条记录
		helper.Info(helper.LogTypeDDNS, "[%s] 检测到记录类型变更 [RecordID=%s, 旧类型=%s->新类型=%s]", cf.GetServiceName(), oldRecord.ID, oldRecord.Type, cf.DNS.Type)
		updateErr = cf.updateDNSRecord(oldRecord.ID, currentValue)

		// 如果有多条记录，删除其余的（避免冲突）
		if len(allRecords) > 1 {
			helper.Info(helper.LogTypeDDNS, "[%s] 发现多条记录，清理多余记录", cf.GetServiceName())
			for i := 1; i < len(allRecords); i++ {
				helper.Info(helper.LogTypeDDNS, "[%s] 删除多余记录 [RecordID=%s, 类型=%s]", cf.GetServiceName(), allRecords[i].ID, allRecords[i].Type)
				if err := cf.deleteDNSRecord(allRecords[i].ID); err != nil {
					helper.Warn(helper.LogTypeDDNS, "[%s] 删除多余记录失败：%v", cf.GetServiceName(), err)
				}
			}
		}
	} else {
		// 记录不存在，创建新记录
		helper.Info(helper.LogTypeDDNS, "[%s] 记录不存在，创建新记录", cf.GetServiceName())
		updateErr = cf.createDNSRecord(currentValue)
	}

	if updateErr != nil {
		cf.Status = UpdatedFailed
		return false
	}

	// 5. 更新缓存
	if IsDynamicType(cf.DNS.IPType) {
		// 使用正则感知的缓存键（用于 IPv6 接口类型）
		var cacheKey string
		if cf.DNS.IPType == helper.DynamicIPv6Interface && cf.DNS.Regex != "" {
			cacheKey = helper.GetIPCacheKeyWithRegex(cf.DNS.IPType, cf.DNS.Value, cf.DNS.Regex)
		} else {
			cacheKey = helper.GetIPCacheKey(cf.DNS.IPType, cf.DNS.Value)
		}
		cf.Cache.UpdateDynamicIP(cacheKey, currentValue)
	}

	cf.Cache.HasRun = true
	cf.Cache.TimesFailed = 0
	cf.Status = UpdatedSuccess
	cf.configChanged = true

	helper.Info(helper.LogTypeDDNS, "[%s] DNS 记录更新成功 [类型=%s, 值=%s]", cf.GetServiceName(), cf.DNS.Type, currentValue)
	return true
}

// getZoneID 获取 Zone ID
func (cf *Cloudflare) getZoneID() (string, error) {
	// 从域名中提取根域名
	rootDomain := cf.getRootDomain()

	apiToken := strings.TrimSpace(cf.DNS.AccessKey)
	if apiToken == "" {
		return "", fmt.Errorf("API Token 为空")
	}

	url := fmt.Sprintf("%s/zones?name=%s", cloudflareAPIEndpoint, rootDomain)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	helper.Debug(helper.LogTypeDDNS, "正在获取 Zone ID [域名=%s, 根域名=%s]", cf.DNS.Domain, rootDomain)

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败 [错误=%v]", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败 [错误=%v]", err)
	}

	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDDNS, "Cloudflare API 响应状态码异常 [状态码=%d, 响应=%s]", resp.StatusCode, string(body))
	}

	var zonesResp CloudflareZonesResponse
	if err := json.Unmarshal(body, &zonesResp); err != nil {
		return "", fmt.Errorf("解析响应失败 [错误=%v, 响应=%s]", err, string(body))
	}

	if !zonesResp.Success {
		if len(zonesResp.Errors) > 0 {
			return "", fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", zonesResp.Errors[0].Code, zonesResp.Errors[0].Message)
		}
		return "", fmt.Errorf("获取 Zone ID 失败")
	}

	if len(zonesResp.Result) == 0 {
		return "", fmt.Errorf("未找到域名 %s 对应的 Zone", rootDomain)
	}

	return zonesResp.Result[0].ID, nil
}

// getAllDNSRecords 获取指定域名的所有 DNS 记录（不限类型）
func (cf *Cloudflare) getAllDNSRecords() ([]CloudflareDNSRecord, error) {
	apiToken := strings.TrimSpace(cf.DNS.AccessKey)
	if apiToken == "" {
		return nil, fmt.Errorf("API Token 为空")
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records?name=%s",
		cloudflareAPIEndpoint, cf.zoneID, cf.DNS.Domain)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	helper.Debug(helper.LogTypeDDNS, "正在查询所有 DNS 记录 [域名=%s]", cf.DNS.Domain)

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败 [错误=%v]", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败 [错误=%v]", err)
	}

	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDDNS, "Cloudflare API 响应状态码异常 [状态码=%d, 响应=%s]", resp.StatusCode, string(body))
	}

	var recordsResp CloudflareDNSRecordsResponse
	if err := json.Unmarshal(body, &recordsResp); err != nil {
		return nil, fmt.Errorf("解析响应失败 [错误=%v, 响应=%s]", err, string(body))
	}

	if !recordsResp.Success {
		if len(recordsResp.Errors) > 0 {
			return nil, fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordsResp.Errors[0].Code, recordsResp.Errors[0].Message)
		}
		return nil, fmt.Errorf("查询 DNS 记录失败")
	}

	return recordsResp.Result, nil
}

// getDNSRecord 获取 DNS 记录
func (cf *Cloudflare) getDNSRecord() (*CloudflareDNSRecord, error) {
	apiToken := strings.TrimSpace(cf.DNS.AccessKey)
	if apiToken == "" {
		return nil, fmt.Errorf("API Token 为空")
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records?type=%s&name=%s",
		cloudflareAPIEndpoint, cf.zoneID, cf.DNS.Type, cf.DNS.Domain)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	helper.Debug(helper.LogTypeDDNS, "正在查询 DNS 记录 [域名=%s, 类型=%s]", cf.DNS.Domain, cf.DNS.Type)

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败 [错误=%v]", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败 [错误=%v]", err)
	}

	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDDNS, "Cloudflare API 响应状态码异常 [状态码=%d, 响应=%s]", resp.StatusCode, string(body))
	}

	var recordsResp CloudflareDNSRecordsResponse
	if err := json.Unmarshal(body, &recordsResp); err != nil {
		return nil, fmt.Errorf("解析响应失败 [错误=%v, 响应=%s]", err, string(body))
	}

	if !recordsResp.Success {
		if len(recordsResp.Errors) > 0 {
			return nil, fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordsResp.Errors[0].Code, recordsResp.Errors[0].Message)
		}
		return nil, fmt.Errorf("查询 DNS 记录失败")
	}

	if len(recordsResp.Result) > 0 {
		return &recordsResp.Result[0], nil
	}

	return nil, nil
}

// createDNSRecord 创建 DNS 记录
func (cf *Cloudflare) createDNSRecord(content string) error {
	apiToken := strings.TrimSpace(cf.DNS.AccessKey)
	if apiToken == "" {
		return fmt.Errorf("API Token 为空")
	}

	data := map[string]interface{}{
		"type":    cf.DNS.Type,
		"name":    cf.DNS.Domain,
		"content": content,
		"ttl":     cf.parseTTL(),
		"proxied": false, // DDNS 一般不开启代理
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records", cloudflareAPIEndpoint, cf.zoneID)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	helper.Debug(helper.LogTypeDDNS, "正在创建 DNS 记录 [域名=%s, 类型=%s, 内容=%s]", cf.DNS.Domain, cf.DNS.Type, content)

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("创建 DNS 记录请求失败 [错误=%v]", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败 [错误=%v]", err)
	}

	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDDNS, "Cloudflare API 响应状态码异常 [状态码=%d, 响应=%s]", resp.StatusCode, string(body))
	}

	var recordResp CloudflareDNSRecordResponse
	if err := json.Unmarshal(body, &recordResp); err != nil {
		return fmt.Errorf("解析响应失败 [错误=%v, 响应=%s]", err, string(body))
	}

	if !recordResp.Success {
		if len(recordResp.Errors) > 0 {
			return fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordResp.Errors[0].Code, recordResp.Errors[0].Message)
		}
		return fmt.Errorf("创建 DNS 记录失败")
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 创建 DNS 记录成功 [值=%s]", cf.GetServiceName(), content)
	return nil
}

// deleteDNSRecord 删除 DNS 记录
func (cf *Cloudflare) deleteDNSRecord(recordID string) error {
	apiToken := strings.TrimSpace(cf.DNS.AccessKey)
	if apiToken == "" {
		return fmt.Errorf("API Token 为空")
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", cloudflareAPIEndpoint, cf.zoneID, recordID)
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	helper.Debug(helper.LogTypeDDNS, "正在删除 DNS 记录 [域名=%s, 记录ID=%s]", cf.DNS.Domain, recordID)

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("删除 DNS 记录请求失败 [错误=%v]", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败 [错误=%v]", err)
	}

	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDDNS, "Cloudflare API 响应状态码异常 [状态码=%d, 响应=%s]", resp.StatusCode, string(body))
	}

	var recordResp CloudflareDNSRecordResponse
	if err := json.Unmarshal(body, &recordResp); err != nil {
		return fmt.Errorf("解析响应失败 [错误=%v, 响应=%s]", err, string(body))
	}

	if !recordResp.Success {
		if len(recordResp.Errors) > 0 {
			return fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordResp.Errors[0].Code, recordResp.Errors[0].Message)
		}
		return fmt.Errorf("删除 DNS 记录失败")
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 删除 DNS 记录成功 [RecordID=%s]", cf.GetServiceName(), recordID)
	return nil
}

// updateDNSRecord 更新 DNS 记录
func (cf *Cloudflare) updateDNSRecord(recordID string, content string) error {
	apiToken := strings.TrimSpace(cf.DNS.AccessKey)
	if apiToken == "" {
		return fmt.Errorf("API Token 为空")
	}

	data := map[string]interface{}{
		"type":    cf.DNS.Type,
		"name":    cf.DNS.Domain,
		"content": content,
		"ttl":     cf.parseTTL(),
		"proxied": false, // DDNS 一般不开启代理
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", cloudflareAPIEndpoint, cf.zoneID, recordID)
	req, err := http.NewRequest("PUT", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+apiToken)
	req.Header.Set("Content-Type", "application/json")

	helper.Debug(helper.LogTypeDDNS, "正在更新 DNS 记录 [域名=%s, 记录ID=%s, 类型=%s, 内容=%s]", cf.DNS.Domain, recordID, cf.DNS.Type, content)

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("更新 DNS 记录请求失败 [错误=%v]", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败 [错误=%v]", err)
	}

	helper.Debug(helper.LogTypeDDNS, "Cloudflare API 响应 [状态码=%d, 响应长度=%d]", resp.StatusCode, len(body))

	if resp.StatusCode != 200 {
		helper.Warn(helper.LogTypeDDNS, "Cloudflare API 响应状态码异常 [状态码=%d, 响应=%s]", resp.StatusCode, string(body))
		return fmt.Errorf("更新 DNS 记录失败 [状态码=%d]", resp.StatusCode)
	}

	var recordResp CloudflareDNSRecordResponse
	if err := json.Unmarshal(body, &recordResp); err != nil {
		helper.Error(helper.LogTypeDDNS, "解析 JSON 失败 [错误=%v]", err)
		helper.Debug(helper.LogTypeDDNS, "响应内容: %s", string(body))
		return fmt.Errorf("解析响应失败 [错误=%v, 响应=%s]", err, string(body))
	}

	helper.Debug(helper.LogTypeDDNS, "解析结果 [Success=%v, Errors数量=%d, Messages数量=%d]", recordResp.Success, len(recordResp.Errors), len(recordResp.Messages))

	// 输出 Cloudflare 返回的消息（如废弃警告等）
	if len(recordResp.Messages) > 0 {
		//for _, msg := range recordResp.Messages {
		//	if msg.DocumentationURL != "" {
		//		helper.Warn(helper.LogTypeDDNS, "Cloudflare 消息: %s (详见: %s)", msg.Message, msg.DocumentationURL)
		//	} else {
		//		helper.Info(helper.LogTypeDDNS, "Cloudflare 消息: %s", msg.Message)
		//	}
		//}
	}

	if !recordResp.Success {
		if len(recordResp.Errors) > 0 {
			helper.Error(helper.LogTypeDDNS, "Cloudflare 返回错误 [代码=%d, 消息=%s]", recordResp.Errors[0].Code, recordResp.Errors[0].Message)
			return fmt.Errorf("Cloudflare API 错误 [代码=%d, 消息=%s]", recordResp.Errors[0].Code, recordResp.Errors[0].Message)
		}
		helper.Error(helper.LogTypeDDNS, "Cloudflare 返回 Success=false，但没有错误详情")
		return fmt.Errorf("更新 DNS 记录失败")
	}

	helper.Info(helper.LogTypeDDNS, "[%s] 更新 DNS 记录成功 [RecordID=%s, 新值=%s]", cf.GetServiceName(), recordID, content)
	return nil
}

// getRootDomain 获取根域名
// 例如: www.sub.example.com -> example.com
func (cf *Cloudflare) getRootDomain() string {
	domain := cf.DNS.Domain

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
func (cf *Cloudflare) parseTTL() int {
	if cf.DNS.TTL == "" || cf.DNS.TTL == "AUTO" {
		return 1 // Cloudflare 的 1 表示自动 TTL
	}

	// 尝试解析为整数
	var ttl int
	_, err := fmt.Sscanf(cf.DNS.TTL, "%d", &ttl)
	if err == nil {
		// Cloudflare 要求 TTL 最小值为 60（除了 1 表示自动）
		if ttl > 1 && ttl < 60 {
			ttl = 60
		}
		return ttl
	}

	return 1 // 默认自动 TTL
}

// isIPv4 判断是否为 IPv4 地址
func isIPv4(ip string) bool {
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && parsedIP.To4() != nil
}

// isIPv6 判断是否为 IPv6 地址
func isIPv6(ip string) bool {
	parsedIP := net.ParseIP(ip)
	return parsedIP != nil && parsedIP.To4() == nil
}
