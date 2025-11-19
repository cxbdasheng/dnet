package dcdn

import (
	"bytes"
	"net/http"
	"net/url"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
	"github.com/cxbdasheng/dnet/signer"
)

const (
	aliyunCDNEndpoint  string = "https://cdn.aliyuncs.com/"
	aliyunDCDNEndpoint string = "https://dcdn.aliyuncs.com/"

	// CDN 类型常量
	CDNTypeCDN  string = "CDN"
	CDNTypeDCDN string = "DCDN"
)

type Aliyun struct {
	CDN    *config.CDN
	Cache  *Cache
	Status statusType
}

var AliyunCDNType string

// AliyunSource 阿里云源站配置
type AliyunSource struct {
	Type     string `json:"Type"`
	Priority string `json:"Priority"`
	Content  string `json:"Content"`
	Port     int    `json:"Port"`
	Weight   string `json:"Weight"`
}

// AliyunDomainInfo 阿里云域名信息
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

// DescribeUserDomainsResponse 查询域名响应
type DescribeUserDomainsResponse struct {
	Domains struct {
		PageData []AliyunDomainInfo `json:"PageData"`
	} `json:"Domains"`
	TotalCount int    `json:"TotalCount"`
	RequestId  string `json:"RequestId"`
	PageSize   int    `json:"PageSize"`
	PageNumber int    `json:"PageNumber"`
}

// getCDNTypeName 获取 CDN 类型的显示名称
func (aliyun *Aliyun) getCDNTypeName() string {
	if aliyun.CDN.CDNType == CDNTypeDCDN {
		return CDNTypeDCDN
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
		helper.Warn(helper.LogTypeDCDN, "CDN 配置校验失败：配置对象为空")
		return false
	}
	// 检查必填的认证信息
	if aliyun.CDN.AccessKey == "" || aliyun.CDN.AccessSecret == "" {
		helper.Warn(helper.LogTypeDCDN, "CDN 配置校验失败：AccessKey或AccessSecret为空 [域名=%s]", aliyun.CDN.Domain)
		return false
	}
	// 检查域名
	if aliyun.CDN.Domain == "" {
		helper.Warn(helper.LogTypeDCDN, "CDN 配置校验失败：域名为空")
		return false
	}
	// 检查 CDN 类型
	if aliyun.CDN.CDNType == "" {
		helper.Warn(helper.LogTypeDCDN, "CDN 配置校验失败：CDN 类型为空 [域名=%s]", aliyun.CDN.Domain)
		return false
	}
	// 验证 CDN 类型是否合法
	if aliyun.CDN.CDNType != CDNTypeCDN && aliyun.CDN.CDNType != CDNTypeDCDN {
		helper.Warn(helper.LogTypeDCDN, "CDN 配置校验失败：不支持的 CDN 类型 [域名=%s, 类型=%s]", aliyun.CDN.Domain, aliyun.CDN.CDNType)
		return false
	}
	// 检查源站配置
	if len(aliyun.CDN.Sources) == 0 {
		helper.Warn(helper.LogTypeDCDN, "CDN 配置校验失败：源站配置为空 [域名=%s]", aliyun.CDN.Domain)
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
	var domainInfo *AliyunDomainInfo
	var err error

	if aliyun.CDN.CDNType == CDNTypeDCDN {
		domainInfo, err = aliyun.describeDCDNDomain()
	} else {
		domainInfo, err = aliyun.describeCDNDomain()
	}

	if err != nil {
		helper.Error(helper.LogTypeDCDN, "查询域名失败 [域名=%s, 错误=%v]", aliyun.CDN.Domain, err)
		return
	}

	// 根据查询结果判断是创建还是修改
	if domainInfo == nil {
		// 域名不存在，需要创建
		helper.Info(helper.LogTypeDCDN, "域名不存在，开始创建 %s [域名=%s]", aliyun.getCDNTypeName(), aliyun.CDN.Domain)
		if aliyun.CDN.CDNType == CDNTypeDCDN {
			aliyun.createDCDN()
		} else {
			aliyun.createCDN()
		}
	} else {
		// 域名已存在，需要修改
		helper.Info(helper.LogTypeDCDN, "域名已存在，开始修改源站配置 [域名=%s, 状态=%s, 当前源站数=%d]",
			aliyun.CDN.Domain, domainInfo.DomainStatus, len(domainInfo.Sources.Source))

		if aliyun.CDN.CDNType == CDNTypeDCDN {
			aliyun.modifyDCDN()
		} else {
			aliyun.modifyCDN()
		}
	}
}

// describeCDNDomain 查询 CDN 域名信息
func (aliyun *Aliyun) describeCDNDomain() (*AliyunDomainInfo, error) {
	params := url.Values{}
	params.Set("Action", "DescribeUserDomains")
	params.Set("DomainName", aliyun.CDN.Domain)

	var response DescribeUserDomainsResponse
	err := aliyun.request(params, &response)
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
	err := aliyun.request(params, &response)
	if err != nil {
		return nil, err
	}

	// 如果未找到域名，返回 nil
	if response.TotalCount == 0 {
		return nil, nil
	}

	return &response.Domains.PageData[0], nil
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

		sources += `{"Content":"` + content + `","Type":"ipaddr","Priority":"` + source.Priority + `","Port":` + port + `,"Weight":"` + weight + `"}`
	}
	sources += "]"
	return sources
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
	err := aliyun.request(params, &result)
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "创建 CDN 域名失败 [域名=%s, 错误=%v]", aliyun.CDN.Domain, err)
		return
	}

	helper.Info(helper.LogTypeDCDN, "创建 CDN 域名成功 [域名=%s, RequestId=%v]", aliyun.CDN.Domain, result["RequestId"])
}

func (aliyun *Aliyun) modifyCDN() {
	params := url.Values{}
	params.Set("Action", "SetCdnDomainSources")
	params.Set("DomainName", aliyun.CDN.Domain)
	params.Set("SourceType", "ipaddr")

	// 构建源站配置
	sources := aliyun.buildSourcesParam()
	params.Set("Sources", sources)

	var result map[string]interface{}
	err := aliyun.request(params, &result)
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "修改 CDN 源站配置失败 [域名=%s, 错误=%v]", aliyun.CDN.Domain, err)
		return
	}

	helper.Info(helper.LogTypeDCDN, "修改 CDN 源站配置成功 [域名=%s, RequestId=%v]", aliyun.CDN.Domain, result["RequestId"])
}

func (aliyun *Aliyun) createDCDN() {
	params := url.Values{}
	params.Set("Action", "AddDcdnDomain")
	params.Set("DomainName", aliyun.CDN.Domain)
	params.Set("Scope", "overseas")

	// 构建源站配置
	sources := aliyun.buildSourcesParam()
	params.Set("Sources", sources)

	var result map[string]interface{}
	err := aliyun.request(params, &result)
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "创建 DCDN 域名失败 [域名=%s, 错误=%v]", aliyun.CDN.Domain, err)
		return
	}

	helper.Info(helper.LogTypeDCDN, "创建 DCDN 域名成功 [域名=%s, RequestId=%v]", aliyun.CDN.Domain, result["RequestId"])
}

func (aliyun *Aliyun) modifyDCDN() {
	params := url.Values{}
	params.Set("Action", "SetDcdnDomainSources")
	params.Set("DomainName", aliyun.CDN.Domain)
	params.Set("SourceType", "ipaddr")

	// 构建源站配置
	sources := aliyun.buildSourcesParam()
	params.Set("Sources", sources)

	var result map[string]interface{}
	err := aliyun.request(params, &result)
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "修改 DCDN 源站配置失败 [域名=%s, 错误=%v]", aliyun.CDN.Domain, err)
		return
	}

	helper.Info(helper.LogTypeDCDN, "修改 DCDN 源站配置成功 [域名=%s, RequestId=%v]", aliyun.CDN.Domain, result["RequestId"])
}

// request 统一请求接口
func (aliyun *Aliyun) request(params url.Values, result interface{}) (err error) {

	signer.AliyunSigner(aliyun.CDN.AccessKey, aliyun.CDN.Service, &params)

	// 根据 CDN 类型选择对应的 endpoint
	var endpoint string
	switch aliyun.CDN.CDNType {
	case CDNTypeDCDN:
		endpoint = aliyunDCDNEndpoint
	case CDNTypeCDN:
		endpoint = aliyunCDNEndpoint
	default:
		// 默认使用 CDN endpoint
		endpoint = aliyunCDNEndpoint
		helper.Warn(helper.LogTypeDCDN, "未识别的 CDN 类型 [类型=%s]，使用默认CDN端点", aliyun.CDN.CDNType)
	}

	req, err := http.NewRequest(
		"GET",
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
