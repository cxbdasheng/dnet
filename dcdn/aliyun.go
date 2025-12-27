package dcdn

import (
	"bytes"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
	"github.com/cxbdasheng/dnet/signer"
)

const (
	aliyunCDNEndpoint  string = "https://cdn.aliyuncs.com/"
	aliyunDCDNEndpoint string = "https://dcdn.aliyuncs.com/"
	aliyunESAEndpoint  string = "https://esa.cn-hangzhou.aliyuncs.com/"
)

type Aliyun struct {
	CDN           *config.CDN
	Cache         *Cache
	Status        statusType
	configChanged bool // 标记配置是否发生变化（用于触发保存）
}

func (aliyun *Aliyun) GetServiceStatus() string {
	return string(aliyun.Status)
}

func (aliyun *Aliyun) GetServiceName() string {
	if aliyun.CDN == nil {
		return ""
	}
	if aliyun.CDN.Name != "" {
		return aliyun.CDN.Name
	}
	return aliyun.CDN.Domain
}

func (aliyun *Aliyun) ConfigChanged() bool {
	return aliyun.configChanged
}

// AliyunSource 阿里云源站配置
type AliyunSource struct {
	Type     string `json:"Type"`
	Priority string `json:"Priority"`
	Content  string `json:"Content"`
	Port     int    `json:"Port"`
	Weight   string `json:"Weight"`
}

// DomainInfo 域名信息抽象接口
type DomainInfo interface {
	GetDomainName() string
	GetCname() string
	GetStatus() string
	GetSourceCount() int
	GetRecordId() int64 // 获取记录ID（CDN/DCDN 返回 DomainId，ESA 返回 RecordId）
}

// AliyunDomainInfo 阿里云域名信息（CDN/DCDN）
type AliyunDomainInfo struct {
	SslProtocol     string `json:"SslProtocol"`
	Description     string `json:"Description"`
	ResourceGroupId string `json:"ResourceGroupId"`
	DomainName      string `json:"DomainName"`
	GmtModified     string `json:"GmtModified"`
	Coverage        string `json:"Coverage"`
	GmtCreated      string `json:"GmtCreated"`
	Cname           string `json:"Cname"`
	Sources         struct {
		Source []AliyunSource `json:"Source"`
	} `json:"Sources"`
	Sandbox            string `json:"Sandbox"`
	DomainId           int    `json:"DomainId"`
	GlobalResourcePlan string `json:"GlobalResourcePlan"`
	CdnType            string `json:"CdnType"`
	DomainStatus       string `json:"DomainStatus"`
}

// 实现 DomainInfo 接口
func (d *AliyunDomainInfo) GetDomainName() string {
	return d.DomainName
}

func (d *AliyunDomainInfo) GetCname() string {
	return d.Cname
}

func (d *AliyunDomainInfo) GetStatus() string {
	return d.DomainStatus
}

func (d *AliyunDomainInfo) GetSourceCount() int {
	return len(d.Sources.Source)
}

func (d *AliyunDomainInfo) GetRecordId() int64 {
	return int64(d.DomainId)
}

// DescribeUserDomainsResponse 查询域名响应（CDN/DCDN）
type DescribeUserDomainsResponse struct {
	Domains struct {
		PageData []AliyunDomainInfo `json:"PageData"`
	} `json:"Domains"`
	TotalCount int    `json:"TotalCount"`
	RequestId  string `json:"RequestId"`
	PageSize   int    `json:"PageSize"`
	PageNumber int    `json:"PageNumber"`
}

// ESARecordInfo ESA 记录信息
type ESARecordInfo struct {
	RecordName       string                 `json:"RecordName"`
	Comment          string                 `json:"Comment"`
	SiteId           int64                  `json:"SiteId"`
	SiteName         string                 `json:"SiteName"`
	RecordSourceType string                 `json:"RecordSourceType"`
	CreateTime       string                 `json:"CreateTime"`
	Data             map[string]interface{} `json:"Data"`
	Ttl              int                    `json:"Ttl"`
	Proxied          bool                   `json:"Proxied"`
	RecordType       string                 `json:"RecordType"`
	UpdateTime       string                 `json:"UpdateTime"`
	BizName          string                 `json:"BizName"`
	HostPolicy       string                 `json:"HostPolicy"`
	RecordId         int64                  `json:"RecordId"`
	AuthConf         map[string]interface{} `json:"AuthConf"`
	RecordCname      string                 `json:"RecordCname"`
}

// 实现 DomainInfo 接口
func (e *ESARecordInfo) GetDomainName() string {
	return e.RecordName
}

func (e *ESARecordInfo) GetCname() string {
	return e.RecordCname
}

func (e *ESARecordInfo) GetStatus() string {
	if e.Proxied {
		return "online"
	}
	return "offline"
}

func (e *ESARecordInfo) GetSourceCount() int {
	// ESA 记录的 Data 字段包含源站信息
	// 这里返回 1 表示有一个源站配置
	return 1
}

func (e *ESARecordInfo) GetRecordId() int64 {
	return e.RecordId
}

// DescribeESARecordsResponse ESA 查询记录响应
type DescribeESARecordsResponse struct {
	TotalCount int             `json:"TotalCount"`
	RequestId  string          `json:"RequestId"`
	PageSize   int             `json:"PageSize"`
	PageNumber int             `json:"PageNumber"`
	Records    []ESARecordInfo `json:"Records"`
}

// ESASiteInfo ESA 站点信息
type ESASiteInfo struct {
	Status          string `json:"Status"`
	SiteId          int64  `json:"SiteId"`
	NameServerList  string `json:"NameServerList"`
	OfflineReason   string `json:"OfflineReason"`
	ResourceGroupId string `json:"ResourceGroupId"`
	SiteName        string `json:"SiteName"`
	VisitTime       string `json:"VisitTime"`
	InstanceId      string `json:"InstanceId"`
	CreateTime      string `json:"CreateTime"`
	Coverage        string `json:"Coverage"`
	PlanName        string `json:"PlanName"`
	VerifyCode      string `json:"VerifyCode"`
	UpdateTime      string `json:"UpdateTime"`
	CnameZone       string `json:"CnameZone"`
	AccessType      string `json:"AccessType"`
}

// DescribeESASitesResponse ESA 查询站点响应
type DescribeESASitesResponse struct {
	TotalCount int           `json:"TotalCount"`
	RequestId  string        `json:"RequestId"`
	PageSize   int           `json:"PageSize"`
	PageNumber int           `json:"PageNumber"`
	Sites      []ESASiteInfo `json:"Sites"`
}

// getCDNTypeName 获取 CDN 类型的显示名称
func (aliyun *Aliyun) getCDNTypeName() string {
	cdnType := strings.ToUpper(aliyun.CDN.CDNType)
	if cdnType == CDNTypeDCDN {
		return CDNTypeDCDN
	}
	if cdnType == CDNTypeDRCDN {
		return CDNTypeDRCDN
	}
	if cdnType == CDNTypeESA {
		return CDNTypeESA
	}
	return CDNTypeCDN
}

func (aliyun *Aliyun) Init(cdnConfig *config.CDN, cache *Cache) {
	aliyun.CDN = cdnConfig
	aliyun.Cache = cache
	aliyun.Status = InitFailed

	if aliyun.validateConfig() {
		aliyun.Status = InitSuccess
		helper.Info(helper.LogTypeDCDN, "阿里云 %s 初始化成功 [域名=%s, 源站数量=%d]", aliyun.getCDNTypeName(), cdnConfig.Domain, len(cdnConfig.Sources))
	} else {
		helper.Error(helper.LogTypeDCDN, "阿里云 %s 初始化失败：配置校验不通过 [域名=%s]", aliyun.getCDNTypeName(), cdnConfig.Domain)
	}
}

// validateConfig 校验 CDN 配置是否有效
func (aliyun *Aliyun) validateConfig() bool {
	if aliyun.CDN == nil {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：配置对象为空", aliyun.getCDNTypeName())
		return false
	}
	// 检查必填的认证信息
	if aliyun.CDN.AccessKey == "" || aliyun.CDN.AccessSecret == "" {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：AccessKey或AccessSecret为空 [域名=%s]", aliyun.getCDNTypeName(), aliyun.CDN.Domain)
		return false
	}
	// 检查域名
	if aliyun.CDN.Domain == "" {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：域名为空", aliyun.getCDNTypeName())
		return false
	}
	// 检查 CDN 类型
	if aliyun.CDN.CDNType == "" {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：类型为空 [域名=%s]", aliyun.getCDNTypeName(), aliyun.CDN.Domain)
		return false
	}
	// 验证 CDN 类型是否合法（不区分大小写）
	cdnType := strings.ToUpper(aliyun.CDN.CDNType)
	if cdnType != CDNTypeCDN && cdnType != CDNTypeDCDN && cdnType != CDNTypeESA {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：不支持的类型 [域名=%s, 类型=%s]", aliyun.getCDNTypeName(), aliyun.CDN.Domain, aliyun.CDN.CDNType)
		return false
	}
	// 检查源站配置
	if len(aliyun.CDN.Sources) == 0 {
		helper.Warn(helper.LogTypeDCDN, "%s 配置校验失败：源站配置为空 [域名=%s]", aliyun.getCDNTypeName(), aliyun.CDN.Domain)
		return false
	}
	return true
}

func (aliyun *Aliyun) UpdateOrCreateSources() bool {
	// 初始化失败则不继续执行
	if aliyun.Status == InitFailed {
		helper.Warn(helper.LogTypeDCDN, "阿里云 %s 更新跳过：初始化失败 [域名=%s]", aliyun.getCDNTypeName(), aliyun.CDN.Domain)
		return false
	}

	helper.Debug(helper.LogTypeDCDN, "开始检查阿里云 %s 源站 IP 变化 [域名=%s]", aliyun.getCDNTypeName(), aliyun.CDN.Domain)

	// 检查动态源的 IP 变化情况
	changedIPCount := 0
	for _, source := range aliyun.CDN.Sources {
		// 跳过静态源
		if !IsDynamicType(source.Type) {
			continue
		}

		// 获取动态 IP
		addr, ok := helper.GetOrSetDynamicIPWithCache(source.Type, source.Value)
		if !ok {
			// IP 获取失败，标记状态并终止
			aliyun.Status = InitGetIPFailed
			helper.Error(helper.LogTypeDCDN, "获取动态 IP 失败 [域名=%s, 源类型=%s, 配置值=%s]",
				aliyun.CDN.Domain, source.Type, source.Value)
			return false
		}

		cacheKey := helper.GetIPCacheKey(source.Type, source.Value)

		// 检查 IP 是否发生变化
		ipChanged, oldIP := aliyun.Cache.CheckIPChanged(cacheKey, addr)
		if ipChanged {
			// IP 发生变化，更新缓存
			aliyun.Cache.UpdateDynamicIP(cacheKey, addr)
			changedIPCount++
			helper.Info(helper.LogTypeDCDN, "检测到源站IP变化 [域名=%s, 源类型=%s, 旧IP=%s, 新IP=%s]",
				aliyun.CDN.Domain, source.Type, oldIP, addr)
		}
	}

	if changedIPCount > 0 {
		helper.Info(helper.LogTypeDCDN, "共检测到 %d 个源站 IP 发生变化 [域名=%s]", changedIPCount, aliyun.CDN.Domain)
	} else {
		helper.Debug(helper.LogTypeDCDN, "未检测到源站 IP 变化 [域名=%s]", aliyun.CDN.Domain)
	}

	// 判断是否需要更新 CDN
	shouldUpdate := aliyun.shouldUpdateCDN(changedIPCount)
	if shouldUpdate {
		helper.Info(helper.LogTypeDCDN, "开始更新阿里云 %s 配置 [域名=%s, IP变化数=%d, 计数器=%d]",
			aliyun.getCDNTypeName(), aliyun.CDN.Domain, changedIPCount, aliyun.Cache.Times)
		aliyun.updateOrCreateSite()
		aliyun.Cache.ResetTimes()
	} else {
		aliyun.Status = UpdatedNothing
		helper.Debug(helper.LogTypeDCDN, "无需更新 %s 配置 [域名=%s, 计数器剩余=%d]",
			aliyun.getCDNTypeName(), aliyun.CDN.Domain, aliyun.Cache.Times)
	}
	return shouldUpdate
}

// shouldUpdateCDN 判断是否需要更新 CDN 配置
func (aliyun *Aliyun) shouldUpdateCDN(changedIPCount int) bool {
	// 第一次运行，需要初始化
	if !aliyun.Cache.HasRun {
		aliyun.Cache.HasRun = true
		helper.Info(helper.LogTypeDCDN, "首次运行，需要初始化 %s 配置 [域名=%s]", aliyun.getCDNTypeName(), aliyun.CDN.Domain)
		return true
	}

	// 有 IP 发生变化，需要更新
	if changedIPCount > 0 {
		helper.Info(helper.LogTypeDCDN, "源站 IP 变化，需要更新 %s [域名=%s, 变化数=%d]",
			aliyun.getCDNTypeName(), aliyun.CDN.Domain, changedIPCount)
		return true
	}

	// 递减计数器
	aliyun.Cache.Times--

	// 计数器归零，需要强制更新
	if aliyun.Cache.Times == 0 {
		helper.Info(helper.LogTypeDCDN, "计数器归零，强制更新 %s [域名=%s]", aliyun.getCDNTypeName(), aliyun.CDN.Domain)
		return true
	}

	// 无需更新
	return false
}

func (aliyun *Aliyun) updateOrCreateSite() {
	// 查询域名是否已存在
	var domainInfo DomainInfo
	var err error
	var SiteId int64
	cdnType := strings.ToUpper(aliyun.CDN.CDNType)
	if cdnType == CDNTypeDCDN {
		// 注意：Go 接口 nil 判断陷阱
		// 当具体类型的 nil 指针赋值给接口时，接口本身不等于 nil
		// 因此需要在赋值前检查具体类型是否为 nil
		var result *AliyunDomainInfo
		result, err = aliyun.describeDCDNDomain()
		if result != nil {
			domainInfo = result
		}
	} else if cdnType == CDNTypeCDN {
		var result *AliyunDomainInfo
		result, err = aliyun.describeCDNDomain()
		if result != nil {
			domainInfo = result
		}
	} else {
		// ESA：先查询站点信息获取 SiteId
		siteInfo, siteErr := aliyun.describeESASite()
		if siteErr != nil {
			helper.Error(helper.LogTypeDCDN, "查询 ESA 站点失败 [根域名=%s, 错误=%v]", aliyun.CDN.GetRootDomain(), siteErr)
			return
		}
		if siteInfo == nil {
			helper.Error(helper.LogTypeDCDN, "未找到 ESA 站点 [根域名=%s]，请先在阿里云 ESA 控制台创建站点", aliyun.CDN.GetRootDomain())
			return
		}
		SiteId = siteInfo.SiteId
		// 使用 SiteId 查询域名记录
		var result *ESARecordInfo
		result, err = aliyun.describeESADomain(SiteId)
		if result != nil {
			domainInfo = result
		}
	}

	if err != nil {
		helper.Error(helper.LogTypeDCDN, "查询域名失败 [域名=%s, 错误=%v]", aliyun.CDN.Domain, err)
		return
	}

	// 如果查询到域名信息，保存 CNAME（仅当 CNAME 发生变化时）
	if domainInfo != nil {
		newCname := domainInfo.GetCname()
		if newCname != "" && newCname != aliyun.CDN.CName {
			helper.Info(helper.LogTypeDCDN, "CNAME 发生变化 [域名=%s, 旧CNAME=%s, 新CNAME=%s]",
				aliyun.CDN.Domain, aliyun.CDN.CName, newCname)
			aliyun.CDN.CName = newCname
			aliyun.configChanged = true
		}
	}

	// 根据查询结果判断是创建还是修改
	if domainInfo == nil {
		// 域名不存在，需要创建
		helper.Info(helper.LogTypeDCDN, "域名不存在，开始创建 %s [域名=%s]", aliyun.getCDNTypeName(), aliyun.CDN.Domain)
		if cdnType == CDNTypeDCDN {
			aliyun.createDCDN()
		} else if cdnType == CDNTypeCDN {
			aliyun.createCDN()
		} else {
			aliyun.createESA(SiteId)
		}
	} else {
		// 域名已存在，需要修改
		helper.Info(helper.LogTypeDCDN, "域名已存在，开始修改源站配置 [域名=%s, 状态=%s, 当前源站数=%d]",
			aliyun.CDN.Domain, domainInfo.GetStatus(), domainInfo.GetSourceCount())
		if cdnType == CDNTypeDCDN {
			aliyun.modifyDCDN()
		} else if cdnType == CDNTypeCDN {
			aliyun.modifyCDN()
		} else {
			aliyun.modifyESA(domainInfo.GetRecordId())
		}
	}
}

// describeCDNDomain 查询 CDN 域名信息
func (aliyun *Aliyun) describeCDNDomain() (*AliyunDomainInfo, error) {
	params := url.Values{}
	params.Set("Action", "DescribeUserDomains")
	params.Set("DomainName", aliyun.CDN.Domain)

	var response DescribeUserDomainsResponse
	err := aliyun.request(http.MethodGet, params, &response)
	if err != nil {
		return nil, err
	}

	// 如果未找到域名，返回 nil
	if response.TotalCount == 0 {
		return nil, nil
	}

	return &response.Domains.PageData[0], nil
}

// describeDCDNDomain 查询 DCDN 域名信息
func (aliyun *Aliyun) describeDCDNDomain() (*AliyunDomainInfo, error) {
	params := url.Values{}
	params.Set("Action", "DescribeDcdnUserDomains")
	params.Set("DomainName", aliyun.CDN.Domain)

	var response DescribeUserDomainsResponse
	err := aliyun.request(http.MethodGet, params, &response)
	if err != nil {
		return nil, err
	}
	// 如果未找到域名，返回 nil
	if response.TotalCount == 0 {
		return nil, nil
	}

	return &response.Domains.PageData[0], nil
}

// describeESASite 查询 ESA 站点信息
func (aliyun *Aliyun) describeESASite() (*ESASiteInfo, error) {
	params := url.Values{}
	params.Set("Action", "ListSites")
	params.Set("SiteName", aliyun.CDN.GetRootDomain())

	var response DescribeESASitesResponse
	err := aliyun.request(http.MethodGet, params, &response)
	if err != nil {
		return nil, err
	}

	// 如果未找到站点，返回 nil
	if response.TotalCount == 0 {
		return nil, nil
	}

	// 直接返回 ESA 站点信息
	return &response.Sites[0], nil
}

// describeESADomain 查询 ESA 域名记录信息
func (aliyun *Aliyun) describeESADomain(siteId int64) (*ESARecordInfo, error) {
	params := url.Values{}
	params.Set("Action", "ListRecords")
	params.Set("RecordName", aliyun.CDN.Domain)
	params.Set("SiteId", strconv.FormatInt(siteId, 10))

	var response DescribeESARecordsResponse
	err := aliyun.request(http.MethodGet, params, &response)
	if err != nil {
		return nil, err
	}
	// 如果未找到域名，返回 nil
	if response.TotalCount == 0 || len(response.Records) == 0 {
		return nil, nil
	}

	// 直接返回 ESA 记录信息
	return &response.Records[0], nil
}

// buildSourcesParam 构建源站参数字符串
func (aliyun *Aliyun) buildSourcesParam() string {
	sources := "["
	for i, source := range aliyun.CDN.Sources {
		if i > 0 {
			sources += ","
		}

		// 获取实际的源站地址
		content := aliyun.getSourceContent(&source)

		// 根据源站类型判断是 IP 还是域名
		sourceType := aliyun.getSourceType(&source, content)

		// 获取优先级（main 或 backup）
		priority := aliyun.getSourcePriority(source.Priority)

		// 设置默认端口
		port := source.Port
		if port == "" {
			port = "80"
		}

		// 设置默认权重
		weight := source.Weight
		if weight == "" {
			weight = "10"
		}

		sources += `{"content":"` + content + `","type":"` + sourceType + `","priority":"` + priority + `","port":` + port + `,"weight":"` + weight + `"}`
	}
	sources += "]"
	return sources
}

// getSourcePriority 将优先级转换为阿里云 API 要求的格式
func (aliyun *Aliyun) getSourcePriority(priority string) string {
	// 如果已经是数字格式 "20" 或 "30"，直接返回
	if priority == "20" || priority == "30" {
		return priority
	}

	// 将文本格式转换为数字格式
	if priority == "main" {
		return "20"
	}

	if priority == "backup" {
		return "30"
	}

	// 如果是空字符串，默认为主源站
	if priority == "" {
		return "20"
	}

	// 其他情况默认为主源站
	return "20"
}

// getSourceType 判断源站类型是 ipaddr 还是 domain
func (aliyun *Aliyun) getSourceType(source *config.Source, content string) string {
	// 如果源类型是静态 IP 类型，返回 ipaddr
	if source.Type == "ipv4" || source.Type == "ipv6" {
		return "ipaddr"
	}

	// 如果是动态类型，需要检查解析出的内容是 IP 还是域名
	if IsDynamicType(source.Type) {
		// 尝试解析为 IP 地址
		if helper.Ipv4Reg.MatchString(content) || helper.Ipv6Reg.MatchString(content) {
			return "ipaddr"
		}
		// 不是 IP，说明是域名
		return "domain"
	}

	// 默认情况：检查 content 是否为 IP
	if helper.Ipv4Reg.MatchString(content) || helper.Ipv6Reg.MatchString(content) {
		return "ipaddr"
	}

	// 其他情况视为域名
	return "domain"
}

// getSourceContent 获取源站的实际内容（处理动态IP）
func (aliyun *Aliyun) getSourceContent(source *config.Source) string {
	if IsDynamicType(source.Type) {
		// 对于动态类型，从缓存获取IP
		cacheKey := helper.GetIPCacheKey(source.Type, source.Value)
		if ip, ok := aliyun.Cache.DynamicIPs[cacheKey]; ok {
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

func (aliyun *Aliyun) createCDN() {
	params := url.Values{}
	params.Set("Action", "AddCdnDomain")
	params.Set("DomainName", aliyun.CDN.Domain)
	params.Set("CdnType", "web")

	// 构建源站配置
	sources := aliyun.buildSourcesParam()
	params.Set("Sources", sources)

	var result map[string]interface{}
	err := aliyun.request(http.MethodGet, params, &result)
	if err != nil {
		aliyun.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "创建 CDN 域名失败 [域名=%s, 错误=%v]", aliyun.CDN.Domain, err)
		return
	}

	aliyun.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "创建 CDN 域名成功 [域名=%s, RequestId=%v]", aliyun.CDN.Domain, result["RequestId"])
}

func (aliyun *Aliyun) modifyCDN() {
	params := url.Values{}
	params.Set("Action", "ModifyCdnDomain")
	params.Set("DomainName", aliyun.CDN.Domain)

	// 构建源站配置
	sources := aliyun.buildSourcesParam()
	params.Set("Sources", sources)

	var result map[string]interface{}
	err := aliyun.request(http.MethodGet, params, &result)
	if err != nil {
		aliyun.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "修改 CDN 源站配置失败 [域名=%s, 错误=%v]", aliyun.CDN.Domain, err)
		return
	}

	aliyun.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "修改 CDN 源站配置成功 [域名=%s, RequestId=%v]", aliyun.CDN.Domain, result["RequestId"])
}

func (aliyun *Aliyun) createDCDN() {
	params := url.Values{}
	params.Set("Action", "AddDcdnDomain")
	params.Set("DomainName", aliyun.CDN.Domain)
	//params.Set("Scope", "overseas")

	// 构建源站配置
	sources := aliyun.buildSourcesParam()
	params.Set("Sources", sources)

	var result map[string]interface{}
	err := aliyun.request(http.MethodGet, params, &result)
	if err != nil {
		aliyun.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "创建 DCDN 域名失败 [域名=%s, 错误=%v]", aliyun.CDN.Domain, err)
		return
	}

	aliyun.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "创建 DCDN 域名成功 [域名=%s, RequestId=%v]", aliyun.CDN.Domain, result["RequestId"])
}

func (aliyun *Aliyun) modifyDCDN() {
	params := url.Values{}
	params.Set("Action", "UpdateDcdnDomain")
	params.Set("DomainName", aliyun.CDN.Domain)

	// 构建源站配置
	sources := aliyun.buildSourcesParam()
	params.Set("Sources", sources)

	var result map[string]interface{}
	err := aliyun.request(http.MethodGet, params, &result)
	if err != nil {
		aliyun.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "修改 DCDN 源站配置失败 [域名=%s, 错误=%v]", aliyun.CDN.Domain, err)
		return
	}

	aliyun.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "修改 DCDN 源站配置成功 [域名=%s, RequestId=%v]", aliyun.CDN.Domain, result["RequestId"])
}

// setESASourceParams 设置 ESA 源站参数（提取 createESA 和 modifyESA 的公共逻辑）
func (aliyun *Aliyun) setESASourceParams(params url.Values) {
	if IsDomainType(aliyun.CDN.Sources[0].Type) {
		params.Set("Type", "CNAME")
		params.Set("SourceType", "Domain")
	} else {
		params.Set("Type", "A/AAAA")
	}
	// 获取实际的源站地址
	content := aliyun.getSourceContent(&aliyun.CDN.Sources[0])
	params.Set("Data", `{"Value":"`+content+`"}`)
}

func (aliyun *Aliyun) createESA(SiteId int64) {
	params := url.Values{}
	params.Set("Action", "CreateRecord")
	params.Set("RecordName", aliyun.CDN.Domain)
	params.Set("SiteId", strconv.FormatInt(SiteId, 10))
	params.Set("Proxied", "true")
	params.Set("BizName", "web")
	params.Set("Ttl", "1")

	// 设置源站参数
	aliyun.setESASourceParams(params)

	var result map[string]interface{}
	err := aliyun.request(http.MethodPost, params, &result)
	if err != nil {
		aliyun.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "创建 %s 域名失败 [域名=%s, 错误=%v]", aliyun.getCDNTypeName(), aliyun.CDN.Domain, err)
		return
	}

	aliyun.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "创建 %s 域名成功 [域名=%s, RequestId=%v]", aliyun.getCDNTypeName(), aliyun.CDN.Domain, result["RequestId"])
}

func (aliyun *Aliyun) modifyESA(RecordId int64) {
	params := url.Values{}
	params.Set("Action", "UpdateRecord")
	params.Set("RecordId", strconv.FormatInt(RecordId, 10))

	// 设置源站参数
	aliyun.setESASourceParams(params)

	var result map[string]interface{}
	err := aliyun.request(http.MethodPost, params, &result)
	if err != nil {
		aliyun.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "修改 %s 源站配置失败 [域名=%s, 错误=%v]", aliyun.getCDNTypeName(), aliyun.CDN.Domain, err)
		return
	}

	aliyun.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "修改 %s 源站配置成功 [域名=%s, RequestId=%v]", aliyun.getCDNTypeName(), aliyun.CDN.Domain, result["RequestId"])
}

// ShouldSendWebhook 判断是否应该发送 webhook
func (aliyun *Aliyun) ShouldSendWebhook() bool {
	// 更新成功，重置失败计数器并发送 webhook
	if aliyun.Status == UpdatedSuccess {
		aliyun.Cache.TimesFailed = 0
		return true
	}

	aliyun.Cache.TimesFailed++
	if aliyun.Cache.TimesFailed >= 3 {
		helper.Warn(helper.LogTypeDCDN, "连续更新失败 %d 次，触发 Webhook 通知 [域名=%s]", aliyun.Cache.TimesFailed, aliyun.CDN.Domain)
		aliyun.Cache.TimesFailed = 0
		return true
	}
	helper.Warn(helper.LogTypeDCDN, "更新失败，将不会触发 Webhook，仅在连续失败 3 次时触发，当前失败次数：%d [域名=%s]", aliyun.Cache.TimesFailed, aliyun.CDN.Domain)
	return false
}

// request 统一请求接口
func (aliyun *Aliyun) request(method string, params url.Values, result interface{}) (err error) {
	// 根据 CDN 类型选择对应的 endpoint 和 API 版本
	var endpoint string
	var apiVersion string
	cdnType := strings.ToUpper(aliyun.CDN.CDNType)
	switch cdnType {
	case CDNTypeDCDN:
		endpoint = aliyunDCDNEndpoint
		apiVersion = "2018-01-15"
	case CDNTypeCDN:
		endpoint = aliyunCDNEndpoint
		apiVersion = "2018-05-10"
	case CDNTypeESA:
		endpoint = aliyunESAEndpoint
		apiVersion = "2024-09-10"
	default:
		// 默认使用 CDN endpoint
		endpoint = aliyunCDNEndpoint
		apiVersion = "2018-05-10"
		helper.Warn(helper.LogTypeDCDN, "未识别的 CDN 类型 [类型=%s]，使用默认CDN端点", aliyun.CDN.CDNType)
	}

	// 设置 API 版本
	params.Set("Version", apiVersion)

	// 调用签名函数
	signer.AliyunSigner(aliyun.CDN.AccessKey, aliyun.CDN.AccessSecret, &params, method)

	req, err := http.NewRequest(
		method,
		endpoint,
		bytes.NewBuffer(nil),
	)
	if err != nil {
		return
	}

	req.URL.RawQuery = params.Encode()

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	err = helper.GetHTTPResponse(resp, err, result)

	return
}
