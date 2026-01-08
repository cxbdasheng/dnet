package dcdn

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
	tencentCDNHost        string = "cdn.tencentcloudapi.com"
	tencentEdgeOneHost    string = "teo.tencentcloudapi.com"
	tencentCDNService     string = "cdn"
	tencentEdgeOneService string = "teo"
)

type Tencent struct {
	CDN           *config.CDN
	Cache         *Cache
	Status        statusType
	configChanged bool // 标记配置是否发生变化（用于触发保存）
}

func (tencent *Tencent) GetServiceStatus() string {
	return string(tencent.Status)
}

func (tencent *Tencent) GetServiceName() string {
	if tencent.CDN == nil {
		return ""
	}
	if tencent.CDN.Name != "" {
		return tencent.CDN.Name
	}
	return tencent.CDN.Domain
}

func (tencent *Tencent) ConfigChanged() bool {
	return tencent.configChanged
}

// getCDNTypeName 获取 CDN 类型的显示名称
func (tencent *Tencent) getCDNTypeName() string {
	cdnType := strings.ToUpper(tencent.CDN.CDNType)
	if cdnType == "EDGEONE" {
		return CDNTypeEdgeOne
	}
	return CDNTypeCDN
}

// TencentOrigin 腾讯云源站配置
type TencentOrigin struct {
	Origins            []string `json:"Origins"`
	OriginType         string   `json:"OriginType"`
	ServerName         string   `json:"ServerName,omitempty"`
	OriginPullProtocol string   `json:"OriginPullProtocol,omitempty"`
	BackupOrigins      []string `json:"BackupOrigins,omitempty"`
}

// TencentDomainInfo 腾讯云域名信息
type TencentDomainInfo struct {
	Domain      string        `json:"Domain"`
	Cname       string        `json:"Cname"`
	Status      string        `json:"Status"`
	ProjectId   int           `json:"ProjectId"`
	ServiceType string        `json:"ServiceType"`
	CreateTime  string        `json:"CreateTime"`
	UpdateTime  string        `json:"UpdateTime"`
	Origin      TencentOrigin `json:"Origin"`
}

// TencentResponse 腾讯云通用响应
type TencentResponse struct {
	Response struct {
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error,omitempty"`
		RequestId string                 `json:"RequestId"`
		Data      map[string]interface{} `json:",inline"`
	} `json:"Response"`
}

// DescribeDomainsResponse 查询域名列表响应
type DescribeDomainsResponse struct {
	Response struct {
		Domains     []TencentDomainInfo `json:"Domains"`
		TotalNumber int                 `json:"TotalNumber"`
		RequestId   string              `json:"RequestId"`
	} `json:"Response"`
}

// EdgeOneZoneInfo EdgeOne 站点信息
type EdgeOneZoneInfo struct {
	ZoneId      string `json:"ZoneId"`
	ZoneName    string `json:"ZoneName"`
	Status      string `json:"Status"`
	CnameStatus string `json:"CnameStatus"`
	CreatedOn   string `json:"CreatedOn"`
	ModifiedOn  string `json:"ModifiedOn"`
}

// DescribeZonesResponse EdgeOne 查询站点响应
type DescribeZonesResponse struct {
	Response struct {
		Zones      []EdgeOneZoneInfo `json:"Zones"`
		TotalCount int               `json:"TotalCount"`
		RequestId  string            `json:"RequestId"`
	} `json:"Response"`
}

// EdgeOneOriginDetail EdgeOne 源站详情
type EdgeOneOriginDetail struct {
	OriginType   string `json:"OriginType"`
	Origin       string `json:"Origin"`
	HostHeader   string `json:"HostHeader"`
	BackupOrigin string `json:"BackupOrigin,omitempty"`
}

// EdgeOneDomainInfo EdgeOne 加速域名信息
type EdgeOneDomainInfo struct {
	DomainName           string              `json:"DomainName"`
	Cname                string              `json:"Cname"`
	DomainStatus         string              `json:"DomainStatus"`
	OriginDetail         EdgeOneOriginDetail `json:"OriginDetail"`
	OriginProtocol       string              `json:"OriginProtocol"`
	HttpOriginPort       int                 `json:"HttpOriginPort"`
	HttpsOriginPort      int                 `json:"HttpsOriginPort"`
	ZoneId               string              `json:"ZoneId"`
	IdentificationStatus string              `json:"IdentificationStatus"`
	CreatedOn            string              `json:"CreatedOn"`
	ModifiedOn           string              `json:"ModifiedOn"`
}

// DescribeAccelerationDomainsResponse EdgeOne 查询加速域名响应
type DescribeAccelerationDomainsResponse struct {
	Response struct {
		AccelerationDomains []EdgeOneDomainInfo `json:"AccelerationDomains"`
		TotalCount          int                 `json:"TotalCount"`
		RequestId           string              `json:"RequestId"`
	} `json:"Response"`
}

func (tencent *Tencent) Init(cdnConfig *config.CDN, cache *Cache) {
	tencent.CDN = cdnConfig
	tencent.Cache = cache
	tencent.Status = InitFailed

	if tencent.validateConfig() {
		tencent.Status = InitSuccess
		helper.Info(helper.LogTypeDCDN, "腾讯云 %s 初始化成功 [域名=%s, 源站数量=%d]", tencent.getCDNTypeName(), cdnConfig.Domain, len(cdnConfig.Sources))
	} else {
		helper.Error(helper.LogTypeDCDN, "腾讯云 %s 初始化失败：配置校验不通过 [域名=%s]", tencent.getCDNTypeName(), cdnConfig.Domain)
	}
}

// validateConfig 校验 CDN 配置是否有效
func (tencent *Tencent) validateConfig() bool {
	if tencent.CDN == nil {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：配置对象为空", tencent.getCDNTypeName())
		return false
	}
	// 检查必填的认证信息
	if tencent.CDN.AccessKey == "" || tencent.CDN.AccessSecret == "" {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：SecretId 或 SecretKey 为空 [域名=%s]", tencent.getCDNTypeName(), tencent.CDN.Domain)
		return false
	}
	// 检查域名
	if tencent.CDN.Domain == "" {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：域名为空", tencent.getCDNTypeName())
		return false
	}
	// 检查源站配置
	if len(tencent.CDN.Sources) == 0 {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：源站配置为空 [域名=%s]", tencent.getCDNTypeName(), tencent.CDN.Domain)
		return false
	}
	return true
}

func (tencent *Tencent) UpdateOrCreateSources() bool {
	// 初始化失败则不继续执行
	if tencent.Status == InitFailed {
		helper.Warn(helper.LogTypeDCDN, "腾讯云 %s 更新跳过：初始化失败 [域名=%s]", tencent.getCDNTypeName(), tencent.CDN.Domain)
		return false
	}

	helper.Debug(helper.LogTypeDCDN, "开始检查腾讯云 %s 源站 IP 变化 [域名=%s]", tencent.getCDNTypeName(), tencent.CDN.Domain)

	// 检查动态源的 IP 变化情况
	changedIPCount := 0
	for _, source := range tencent.CDN.Sources {
		// 跳过静态源
		if !IsDynamicType(source.Type) {
			continue
		}

		// 获取动态 IP
		addr, ok := helper.GetOrSetDynamicIPWithCache(source.Type, source.Value)
		if !ok {
			// IP 获取失败，标记状态并终止
			tencent.Status = InitGetIPFailed
			helper.Error(helper.LogTypeDCDN, "获取动态 IP 失败 [域名=%s, 源类型=%s, 配置值=%s]",
				tencent.CDN.Domain, source.Type, source.Value)
			return false
		}

		cacheKey := helper.GetIPCacheKey(source.Type, source.Value)

		// 检查 IP 是否发生变化
		ipChanged, oldIP := tencent.Cache.CheckIPChanged(cacheKey, addr)
		if ipChanged {
			// IP 发生变化，更新缓存
			tencent.Cache.UpdateDynamicIP(cacheKey, addr)
			changedIPCount++
			helper.Info(helper.LogTypeDCDN, "检测到源站 IP 变化 [域名=%s, 源类型=%s, 旧IP=%s, 新IP=%s]",
				tencent.CDN.Domain, source.Type, oldIP, addr)
		}
	}

	if changedIPCount > 0 {
		helper.Info(helper.LogTypeDCDN, "共检测到 %d 个源站 IP 发生变化 [域名=%s]", changedIPCount, tencent.CDN.Domain)
	} else {
		helper.Debug(helper.LogTypeDCDN, "未检测到源站 IP 变化 [域名=%s]", tencent.CDN.Domain)
	}

	// 判断是否需要更新 CDN
	shouldUpdate := tencent.shouldUpdateCDN(changedIPCount)
	if shouldUpdate {
		helper.Info(helper.LogTypeDCDN, "开始更新腾讯云 %s 配置 [域名=%s, IP变化数=%d, 计数器=%d]",
			tencent.getCDNTypeName(), tencent.CDN.Domain, changedIPCount, tencent.Cache.Times)
		tencent.updateOrCreateSite()
		tencent.Cache.ResetTimes()
	} else {
		tencent.Status = UpdatedNothing
		helper.Debug(helper.LogTypeDCDN, "无需更新 %s 配置 [域名=%s, 计数器剩余=%d]",
			tencent.getCDNTypeName(), tencent.CDN.Domain, tencent.Cache.Times)
	}
	return shouldUpdate
}

// shouldUpdateCDN 判断是否需要更新 CDN 配置
func (tencent *Tencent) shouldUpdateCDN(changedIPCount int) bool {
	// 第一次运行，需要初始化
	if !tencent.Cache.HasRun {
		tencent.Cache.HasRun = true
		helper.Info(helper.LogTypeDCDN, "首次运行，需要初始化腾讯云 %s 配置 [域名=%s]", tencent.getCDNTypeName(), tencent.CDN.Domain)
		return true
	}

	// 有 IP 发生变化，需要更新
	if changedIPCount > 0 {
		helper.Info(helper.LogTypeDCDN, "源站 IP 变化，需要更新腾讯云 %s [域名=%s, 变化数=%d]",
			tencent.getCDNTypeName(), tencent.CDN.Domain, changedIPCount)
		return true
	}

	// 递减计数器
	tencent.Cache.Times--

	// 计数器归零，需要强制更新
	if tencent.Cache.Times == 0 {
		helper.Info(helper.LogTypeDCDN, "计数器归零，强制更新腾讯云 %s [域名=%s]", tencent.getCDNTypeName(), tencent.CDN.Domain)
		return true
	}

	// 无需更新
	return false
}

func (tencent *Tencent) updateOrCreateSite() {
	// 查询域名是否已存在
	var domainInfo *TencentDomainInfo
	var err error
	var ZoneId string

	if tencent.getCDNTypeName() == CDNTypeEdgeOne {
		// EdgeOne：先查询站点信息获取 ZoneId
		ZoneId, err = tencent.describeEdgeOneSite()
		if err != nil {
			helper.Error(helper.LogTypeDCDN, "查询 EdgeOne 站点失败 [根域名=%s, 错误=%v]", tencent.CDN.GetRootDomain(), err)
			tencent.Status = UpdatedFailed
			return
		}
		if ZoneId == "" {
			helper.Error(helper.LogTypeDCDN, "未找到 EdgeOne 站点 [根域名=%s]，请先在腾讯云 EdgeOne 控制台创建站点", tencent.CDN.GetRootDomain())
			tencent.Status = UpdatedFailed
			return
		}
		// 使用 ZoneId 查询加速域名
		domainInfo, err = tencent.describeEdgeOneDomain(ZoneId)
	} else {
		// CDN：直接查询域名配置
		domainInfo, err = tencent.describeCDNDomain()
	}

	if err != nil {
		helper.Error(helper.LogTypeDCDN, "查询域名失败 [域名=%s, 错误=%v]", tencent.CDN.Domain, err)
		tencent.Status = UpdatedFailed
		return
	}

	// 如果查询到域名信息，保存 CNAME（仅当 CNAME 发生变化时）
	if domainInfo != nil {
		newCname := domainInfo.Cname
		if newCname != "" && newCname != tencent.CDN.CName {
			helper.Info(helper.LogTypeDCDN, "CNAME 发生变化 [域名=%s, 旧CNAME=%s, 新CNAME=%s]",
				tencent.CDN.Domain, tencent.CDN.CName, newCname)
			tencent.CDN.CName = newCname
			tencent.configChanged = true
		}
	}

	// 根据查询结果判断是创建还是修改
	if domainInfo == nil {
		// 域名不存在，需要创建
		helper.Info(helper.LogTypeDCDN, "域名不存在，开始创建腾讯云 %s [域名=%s]", tencent.getCDNTypeName(), tencent.CDN.Domain)
		if tencent.getCDNTypeName() == CDNTypeCDN {
			tencent.createCDN()
		} else {
			tencent.createEdgeOne(ZoneId)
		}
	} else {
		// 域名已存在，需要修改
		helper.Info(helper.LogTypeDCDN, "域名已存在，开始修改源站配置 [域名=%s, 状态=%s]",
			tencent.CDN.Domain, domainInfo.Status)
		if tencent.getCDNTypeName() == CDNTypeCDN {
			tencent.modifyCDN()
		} else {
			tencent.modifyEdgeOne(ZoneId)
		}
	}
}
func (tencent *Tencent) describeEdgeOneSite() (string, error) {
	// EdgeOne 查询站点信息
	action := "DescribeZones"
	requestBody := map[string]interface{}{
		"Filters": []map[string]interface{}{
			{
				"Name":   "zone-name",
				"Values": []string{tencent.CDN.GetRootDomain()},
			},
		},
	}
	var response DescribeZonesResponse
	err := tencent.request(action, requestBody, &response)
	if err != nil {
		return "", err
	}

	// 如果未找到站点，返回 nil
	if response.Response.TotalCount == 0 {
		return "", nil
	}
	return response.Response.Zones[0].ZoneId, nil
}

// describeCDNDomain 查询 CDN 域名配置
func (tencent *Tencent) describeCDNDomain() (*TencentDomainInfo, error) {
	action := "DescribeDomainsConfig"
	requestBody := map[string]interface{}{
		"Filters": []map[string]interface{}{
			{
				"Name":  "domain",
				"Value": []string{tencent.CDN.Domain},
			},
		},
	}

	var response DescribeDomainsResponse
	err := tencent.request(action, requestBody, &response)
	if err != nil {
		return nil, err
	}

	// 如果未找到域名，返回 nil
	if response.Response.TotalNumber == 0 {
		return nil, nil
	}

	return &response.Response.Domains[0], nil
}

// describeEdgeOneDomain 查询 EdgeOne 加速域名信息
func (tencent *Tencent) describeEdgeOneDomain(ZoneId string) (*TencentDomainInfo, error) {
	action := "DescribeAccelerationDomains"
	requestBody := map[string]interface{}{
		"ZoneId": ZoneId,
		"Filters": []map[string]interface{}{
			{
				"Name":   "domain-name",
				"Values": []string{tencent.CDN.Domain},
			},
		},
	}
	var response DescribeAccelerationDomainsResponse
	err := tencent.request(action, requestBody, &response)
	if err != nil {
		return nil, err
	}

	// 如果未找到域名，返回 nil
	if response.Response.TotalCount == 0 {
		return nil, nil
	}

	// 将 EdgeOne 域名信息转换为通用的 TencentDomainInfo
	edgeDomain := response.Response.AccelerationDomains[0]
	domainInfo := &TencentDomainInfo{
		Domain: edgeDomain.DomainName,
		Cname:  edgeDomain.Cname,
		Status: edgeDomain.DomainStatus,
	}

	return domainInfo, nil
}

// describeDomain 查询域名信息（统一入口）
func (tencent *Tencent) describeDomain() (*TencentDomainInfo, error) {
	if tencent.getCDNTypeName() == CDNTypeEdgeOne {
		// EdgeOne 需要先获取 ZoneId，然后查询加速域名
		ZoneId, err := tencent.describeEdgeOneSite()
		if err != nil {
			return nil, err
		}
		if ZoneId == "" {
			// 站点不存在
			return nil, nil
		}
		// 查询加速域名
		return tencent.describeEdgeOneDomain(ZoneId)
	}

	// CDN：直接调用 CDN 查询方法
	return tencent.describeCDNDomain()
}

// buildOriginConfig 构建源站配置
func (tencent *Tencent) buildOriginConfig() TencentOrigin {
	var origins []string
	var backupOrigins []string

	// 第一个源站确定源站类型
	firstSource := tencent.CDN.Sources[0]
	//originType := "ip_ipv6_domain_domainv6"
	var originType string
	if IsDomainType(firstSource.Type) {
		originType = "domain"
	} else {
		originType = "ip"
	}

	// 遍历所有源站
	for _, source := range tencent.CDN.Sources {
		// 获取实际的源站地址
		content := tencent.getSourceContent(&source)

		// 根据协议构建完整的源站地址
		protocol := strings.ToLower(source.Protocol)
		if protocol == "" || protocol == "http" {
			// HTTP 协议或默认
			port := source.Port
			if port == "" {
				port = "80"
			}
			content = content + ":" + port
		} else if protocol == "https" {
			// HTTPS 协议
			port := source.HttpsPort
			if port == "" {
				port = "443"
			}
			content = content + ":" + port
		} else if protocol == "auto" {
			// AUTO 协议跟随，不指定端口
			// 腾讯云会根据访问协议自动选择
		}
		if len(tencent.CDN.Sources) > 1 {
			content = content + ":" + source.Weight
		}
		// 根据优先级分配到主源站或备源站
		if source.Priority == "backup" {
			backupOrigins = append(backupOrigins, content)
		} else {
			origins = append(origins, content)
		}
	}

	origin := TencentOrigin{
		Origins:    origins,
		OriginType: originType,
	}

	if len(backupOrigins) > 0 {
		origin.BackupOrigins = backupOrigins
	}

	return origin
}

// getSourceContent 获取源站的实际内容（处理动态IP）
func (tencent *Tencent) getSourceContent(source *config.Source) string {
	if IsDynamicType(source.Type) {
		// 对于动态类型，从缓存获取IP
		cacheKey := helper.GetIPCacheKey(source.Type, source.Value)
		if ip, ok := tencent.Cache.DynamicIPs[cacheKey]; ok {
			return ip
		}
		// 如果缓存中没有，尝试获取
		if addr, ok := helper.GetOrSetDynamicIPWithCache(source.Type, source.Value); ok {
			return addr
		}
		helper.Warn(helper.LogTypeDCDN, "无法获取动态源站IP [类型=%s, 值=%s]，使用配置值", source.Type, source.Value)
	}
	// 静态类型直接返回配置的值
	return source.Value
}

func (tencent *Tencent) createEdgeOne(ZoneId string) {
	action := "CreateAccelerationDomain"
	originProtocol := tencent.CDN.Sources[0].Protocol
	if originProtocol == "AUTO" {
		originProtocol = "FOLLOW"
	}

	// 转换端口为 uint64
	httpPort := uint64(80)
	if tencent.CDN.Sources[0].Port != "" {
		if port, err := strconv.ParseUint(tencent.CDN.Sources[0].Port, 10, 64); err == nil {
			httpPort = port
		}
	}

	httpsPort := uint64(443)
	if tencent.CDN.Sources[0].HttpsPort != "" {
		if port, err := strconv.ParseUint(tencent.CDN.Sources[0].HttpsPort, 10, 64); err == nil {
			httpsPort = port
		}
	}

	requestBody := map[string]interface{}{
		"ZoneId":     ZoneId,
		"DomainName": tencent.CDN.Domain,
		"OriginInfo": map[string]interface{}{
			"OriginType": "IP_DOMAIN",
			"Origin":     tencent.getSourceContent(&tencent.CDN.Sources[0]),
		},
		"OriginProtocol":  originProtocol,
		"HttpOriginPort":  httpPort,
		"HttpsOriginPort": httpsPort,
	}
	var response TencentResponse
	err := tencent.request(action, requestBody, &response)
	if err != nil {
		tencent.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "创建 %s 域名失败 [域名=%s, 错误=%v]", tencent.getCDNTypeName(), tencent.CDN.Domain, err)
		return
	}
	tencent.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "创建 %s 域名成功 [域名=%s, RequestId=%v]", tencent.getCDNTypeName(), tencent.CDN.Domain, response.Response.RequestId)
	domainInfo, err := tencent.describeEdgeOneDomain(ZoneId)
	if err != nil {
		helper.Warn(helper.LogTypeDCDN, "创建 %s 域名后查询 CNAME 失败 [域名=%s, 错误=%v]", tencent.getCDNTypeName(), tencent.CDN.Domain, err)
		return
	}
	if domainInfo != nil {
		newCname := domainInfo.Cname
		if newCname != "" && newCname != tencent.CDN.CName {
			helper.Info(helper.LogTypeDCDN, "获取到 CNAME [域名=%s, CNAME=%s]", tencent.CDN.Domain, newCname)
			tencent.CDN.CName = newCname
			tencent.configChanged = true
		}
	}
}
func (tencent *Tencent) createCDN() {
	action := "AddCdnDomain"
	origin := tencent.buildOriginConfig()

	requestBody := map[string]interface{}{
		"Domain":      tencent.CDN.Domain,
		"ServiceType": "web", // 只支持这个选项，其他选项会提示升级
		"Origin":      origin,
	}

	var result TencentResponse
	err := tencent.request(action, requestBody, &result)
	if err != nil {
		tencent.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "创建腾讯云 %s 域名失败 [域名=%s, 错误=%v]", tencent.getCDNTypeName(), tencent.CDN.Domain, err)
		return
	}

	tencent.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "创建腾讯云 %s 域名成功 [域名=%s, RequestId=%s]", tencent.getCDNTypeName(), tencent.CDN.Domain, result.Response.RequestId)

	// 创建成功后查询域名信息获取 CNAME
	domainInfo, err := tencent.describeDomain()
	if err != nil {
		helper.Warn(helper.LogTypeDCDN, "创建腾讯云 %s 域名后查询 CNAME 失败 [域名=%s, 错误=%v]", tencent.getCDNTypeName(), tencent.CDN.Domain, err)
		return
	}
	if domainInfo != nil {
		newCname := domainInfo.Cname
		if newCname != "" && newCname != tencent.CDN.CName {
			helper.Info(helper.LogTypeDCDN, "获取到 CNAME [域名=%s, CNAME=%s]", tencent.CDN.Domain, newCname)
			tencent.CDN.CName = newCname
			tencent.configChanged = true
		}
	}
}

func (tencent *Tencent) modifyCDN() {
	action := "UpdateDomainConfig"
	origin := tencent.buildOriginConfig()

	requestBody := map[string]interface{}{
		"Domain": tencent.CDN.Domain,
		"Origin": origin,
	}

	var result TencentResponse
	err := tencent.request(action, requestBody, &result)
	if err != nil {
		tencent.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "修改腾讯云 %s 源站配置失败 [域名=%s, 错误=%v]", tencent.getCDNTypeName(), tencent.CDN.Domain, err)
		return
	}

	tencent.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "修改腾讯云 %s 源站配置成功 [域名=%s, RequestId=%s]", tencent.getCDNTypeName(), tencent.CDN.Domain, result.Response.RequestId)
}

// ShouldSendWebhook 判断是否应该发送 webhook
func (tencent *Tencent) ShouldSendWebhook() bool {
	// 更新成功，重置失败计数器并发送 webhook
	if tencent.Status == UpdatedSuccess {
		tencent.Cache.TimesFailed = 0
		return true
	}

	// 只有明确的更新失败才计数
	if tencent.Status == UpdatedFailed {
		tencent.Cache.TimesFailed++
		if tencent.Cache.TimesFailed >= 3 {
			helper.Warn(helper.LogTypeDCDN, "连续更新失败 %d 次，触发 Webhook 通知 [域名=%s]", tencent.Cache.TimesFailed, tencent.CDN.Domain)
			tencent.Cache.TimesFailed = 0
			return true
		}
		helper.Warn(helper.LogTypeDCDN, "更新失败，将不会触发 Webhook，仅在连续失败 3 次时触发，当前失败次数：%d [域名=%s]", tencent.Cache.TimesFailed, tencent.CDN.Domain)
		return false
	}

	// 其他状态（如 UpdatedNothing、InitGetIPFailed 等）不发送 webhook
	return false
}

func (tencent *Tencent) modifyEdgeOne(ZoneId string) {
	action := "ModifyAccelerationDomain"
	originProtocol := tencent.CDN.Sources[0].Protocol
	if originProtocol == "AUTO" {
		originProtocol = "FOLLOW"
	}

	// 转换端口为 uint64
	httpPort := uint64(80)
	if tencent.CDN.Sources[0].Port != "" {
		if port, err := strconv.ParseUint(tencent.CDN.Sources[0].Port, 10, 64); err == nil {
			httpPort = port
		}
	}

	httpsPort := uint64(443)
	if tencent.CDN.Sources[0].HttpsPort != "" {
		if port, err := strconv.ParseUint(tencent.CDN.Sources[0].HttpsPort, 10, 64); err == nil {
			httpsPort = port
		}
	}

	requestBody := map[string]interface{}{
		"ZoneId":     ZoneId,
		"DomainName": tencent.CDN.Domain,
		"OriginInfo": map[string]interface{}{
			"OriginType": "IP_DOMAIN",
			"Origin":     tencent.getSourceContent(&tencent.CDN.Sources[0]),
		},
		"OriginProtocol":  originProtocol,
		"HttpOriginPort":  httpPort,
		"HttpsOriginPort": httpsPort,
	}
	var response TencentResponse
	err := tencent.request(action, requestBody, &response)
	if err != nil {
		tencent.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "修改 %s 源站配置失败 [域名=%s, 错误=%v]", tencent.getCDNTypeName(), tencent.CDN.Domain, err)
		return
	}
	tencent.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "修改 %s 源站配置成功 [域名=%s, RequestId=%v]", tencent.getCDNTypeName(), tencent.CDN.Domain, response.Response.RequestId)
	domainInfo, err := tencent.describeEdgeOneDomain(ZoneId)
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "获取 %s 源站配置失败 [域名=%s, 错误=%v]", tencent.getCDNTypeName(), tencent.CDN.Domain, err)
		return
	}
	if domainInfo != nil {
		newCname := domainInfo.Cname
		if newCname != "" && newCname != tencent.CDN.CName {
			helper.Info(helper.LogTypeDCDN, "获取到 CNAME [域名=%s, CNAME=%s]", tencent.CDN.Domain, newCname)
			tencent.CDN.CName = newCname
			tencent.configChanged = true
		}
	}
}

// request 统一请求接口
func (tencent *Tencent) request(action string, body interface{}, result interface{}) error {
	// 根据 CDN 类型选择对应的 host 和 service
	var host, service string
	if tencent.getCDNTypeName() == CDNTypeEdgeOne {
		host = tencentEdgeOneHost
		service = tencentEdgeOneService
	} else {
		host = tencentCDNHost
		service = tencentCDNService
	}

	endpoint := "https://" + host + "/"

	// 构建请求体
	jsonStr, _ := json.Marshal(body)
	helper.Debug(helper.LogTypeDCDN, "请求体内容: %s", string(jsonStr))

	req, err := http.NewRequest(
		http.MethodPost,
		endpoint,
		bytes.NewBuffer(jsonStr),
	)
	if err != nil {
		return err
	}

	// 设置 Action
	req.Header.Set("X-TC-Action", action)

	// 调用签名函数
	signer.TencentSigner(tencent.CDN.AccessKey, tencent.CDN.AccessSecret, service, host, string(jsonStr), req)

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	err = helper.GetHTTPResponse(resp, err, result)

	// 检查腾讯云 API 错误
	if err == nil {
		// 使用类型断言检查是否有 Error 字段
		if v, ok := result.(*TencentResponse); ok && v.Response.Error != nil {
			err = errors.New(v.Response.Error.Code + ": " + v.Response.Error.Message)
		} else if v, ok := result.(*DescribeDomainsResponse); ok && v.Response.RequestId != "" {
			// 查询接口成功，无需额外处理
		}
	}

	return err
}
