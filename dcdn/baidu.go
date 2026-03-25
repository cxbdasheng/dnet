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
	baiduCDNEndpoint string = "https://cdn.baidubce.com"
)

type Baidu struct {
	BaseProvider
}

// getCDNTypeName 获取 CDN 类型的显示名称
func (baidu *Baidu) getCDNTypeName() string {
	cdnType := strings.ToUpper(baidu.CDN.CDNType)
	if cdnType == CDNTypeDCDN {
		return CDNTypeDCDN
	}
	if cdnType == CDNTypeDRCDN {
		return CDNTypeDRCDN
	}
	return CDNTypeCDN
}

// getFromValue 获取域名业务类型字段值
func (baidu *Baidu) getFromValue() string {
	cdnType := strings.ToUpper(baidu.CDN.CDNType)
	if cdnType == CDNTypeDRCDN {
		return "dynamic"
	}
	return "default"
}

// BaiduSource 百度云源站配置
type BaiduSource struct {
	Peer    string `json:"peer"`
	Backup  bool   `json:"backup"`
	Weight  int    `json:"weight,omitempty"`
	IspType string `json:"isp,omitempty"`
}

// BaiduOriginInit 百度云创建域名请求
type BaiduOriginInit struct {
	Origin []BaiduSource `json:"origin"`
}

// BaiduDomainInfo 百度云域名信息
type BaiduDomainInfo struct {
	Domain       string        `json:"domain"`
	Cname        string        `json:"cname"`
	Status       string        `json:"status"`
	CreateTime   string        `json:"createTime"`
	LastModify   string        `json:"lastModifyTime"`
	IsBan        string        `json:"isBan"`
	Form         string        `json:"form"`
	Origin       []BaiduSource `json:"origin"` // 查询时返回的字段名是 origin
	CacheTTL     []interface{} `json:"cacheTTL"`
	CacheFullUrl interface{}   `json:"cacheFullUrl"`        // 可能是 bool 或 string
	Code         string        `json:"code,omitempty"`      // 错误码
	Message      string        `json:"message,omitempty"`   // 错误消息
	RequestId    string        `json:"requestId,omitempty"` // 请求 ID
}

// BaiduDomainListResponse 百度云域名列表响应
type BaiduDomainListResponse struct {
	Domains   []BaiduDomainInfo `json:"domains"`
	Code      string            `json:"code,omitempty"`      // 错误码
	Message   string            `json:"message,omitempty"`   // 错误消息
	RequestId string            `json:"requestId,omitempty"` // 请求 ID
}

func (baidu *Baidu) Init(cdnConfig *config.CDN, cache *Cache) {
	baidu.CDN = cdnConfig
	baidu.Cache = cache
	baidu.Status = InitFailed

	if baidu.validateConfig() {
		baidu.Status = InitSuccess
		helper.Info(helper.LogTypeDCDN, "百度云 %s 初始化成功 [域名=%s, 源站数量=%d]", baidu.getCDNTypeName(), cdnConfig.Domain, len(cdnConfig.Sources))
	} else {
		helper.Error(helper.LogTypeDCDN, "百度云 %s 初始化失败：配置校验不通过 [域名=%s]", baidu.getCDNTypeName(), cdnConfig.Domain)
	}
}

// validateConfig 校验 CDN 配置是否有效
func (baidu *Baidu) validateConfig() bool {
	if baidu.CDN == nil {
		helper.Warn(helper.LogTypeDCDN, "百度云配置校验失败：配置对象为空")
		return false
	}
	return baidu.validateBaseConfig("百度云 " + baidu.getCDNTypeName())
}

func (baidu *Baidu) UpdateOrCreateSources() bool {
	return baidu.runUpdateOrCreate("百度云 "+baidu.getCDNTypeName(), baidu.updateOrCreateSite)
}

func (baidu *Baidu) updateOrCreateSite() {
	// 查询域名是否已存在
	domainInfo, err := baidu.describeDomain()
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "查询域名失败 [域名=%s, 错误=%v]", baidu.CDN.Domain, err)
		baidu.Status = UpdatedFailed
		return
	}

	// 如果查询到域名信息，保存 CNAME（仅当 CNAME 发生变化时）
	if domainInfo != nil {
		baidu.updateCnameIfChanged(domainInfo.Cname)
	}

	// 根据查询结果判断是创建还是修改
	if domainInfo == nil {
		// 域名不存在，需要创建
		helper.Info(helper.LogTypeDCDN, "域名不存在，开始创建百度云 %s [域名=%s]", baidu.getCDNTypeName(), baidu.CDN.Domain)
		baidu.createCDN()
	} else {
		// 域名已存在，需要修改
		helper.Info(helper.LogTypeDCDN, "域名已存在，开始修改源站配置 [域名=%s, 状态=%s, 当前源站数=%d]",
			baidu.CDN.Domain, domainInfo.Status, len(domainInfo.Origin))
		baidu.modifyCDN()
	}
}

// describeDomain 查询域名信息
func (baidu *Baidu) describeDomain() (*BaiduDomainInfo, error) {
	path := "/v2/domain/" + baidu.CDN.Domain + "/config"

	var result BaiduDomainInfo
	err := baidu.request("GET", path, nil, &result)
	if err != nil {
		// 如果是 404 错误，说明域名不存在
		errStr := err.Error()
		if strings.Contains(errStr, "404") || strings.Contains(errStr, "not found") {
			return nil, nil
		}
		return nil, err
	}

	return &result, nil
}

// buildSourcesConfig 构建源站配置
func (baidu *Baidu) buildSourcesConfig() []BaiduSource {
	var sources []BaiduSource

	for _, source := range baidu.CDN.Sources {
		protocol := strings.ToUpper(source.Protocol)
		isBackup := source.Priority == "backup"

		// 如果协议为 AUTO，生成两条记录（HTTP 和 HTTPS）
		if protocol == "AUTO" {
			// 生成 HTTP 记录
			httpContent := baidu.getSourceContentWithProtocol(&source, "http", source.Port)
			httpSource := BaiduSource{
				Peer:   httpContent,
				Backup: isBackup,
			}
			if source.Weight != "" && source.Weight != "0" {
				weight := 10
				if w, err := strconv.Atoi(source.Weight); err == nil {
					weight = w
				}
				httpSource.Weight = weight
			}
			sources = append(sources, httpSource)

			// 生成 HTTPS 记录
			httpsContent := baidu.getSourceContentWithProtocol(&source, "https", source.HttpsPort)
			httpsSource := BaiduSource{
				Peer:   httpsContent,
				Backup: isBackup,
			}
			if source.Weight != "" && source.Weight != "0" {
				weight := 10
				if w, err := strconv.Atoi(source.Weight); err == nil {
					weight = w
				}
				httpsSource.Weight = weight
			}
			sources = append(sources, httpsSource)
		} else {
			// 非 AUTO 协议，使用原有逻辑
			content := baidu.getSourceContent(&source)
			baiduSource := BaiduSource{
				Peer:   content,
				Backup: isBackup,
			}

			// 设置权重（如果有）
			if source.Weight != "" && source.Weight != "0" {
				weight := 10 // 默认权重
				if w, err := strconv.Atoi(source.Weight); err == nil {
					weight = w
				}
				baiduSource.Weight = weight
			}

			sources = append(sources, baiduSource)
		}
	}

	return sources
}

// getSourceContent 获取源站的实际内容（处理动态IP）
// 返回完整的源站 URL 格式：protocol://address:port
func (baidu *Baidu) getSourceContent(source *config.Source) string {
	// 获取实际的地址（IP或域名）
	var addr string
	if IsDynamicType(source.Type) {
		// 对于动态类型，从缓存获取IP
		cacheKey := helper.GetIPCacheKey(source.Type, source.Value)
		if ip, ok := baidu.Cache.DynamicIPs[cacheKey]; ok {
			addr = ip
		} else {
			// 如果缓存中没有，尝试获取
			if ip, ok := helper.GetOrSetDynamicIPWithCache(source.Type, source.Value); ok {
				addr = ip
			} else {
				helper.Warn(helper.LogTypeDCDN, "无法获取动态源站IP [类型=%s, 值=%s]，使用配置值", source.Type, source.Value)
				addr = source.Value
			}
		}
	} else {
		// 静态类型直接使用配置的值
		addr = source.Value
	}

	// 确定协议，默认为 http
	protocol := source.Protocol
	if protocol == "" {
		protocol = "http"
	}
	// 规范化协议为小写
	protocol = strings.ToLower(protocol)

	// 确定端口
	port := source.Port
	if port == "" {
		// 根据协议设置默认端口
		if protocol == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	// 判断是否为 IPv6 地址（需要用方括号包裹）
	if helper.Ipv6Reg.MatchString(addr) {
		// IPv6 格式：http://[2001:db8::1]:80
		return protocol + "://[" + addr + "]:" + port
	}

	// IPv4 或域名格式：http://192.168.1.1:80 或 http://example.com:80
	return protocol + "://" + addr + ":" + port
}

// getSourceContentWithProtocol 使用指定的协议和端口获取源站内容
// 用于 AUTO 协议时强制指定具体的协议和端口
func (baidu *Baidu) getSourceContentWithProtocol(source *config.Source, protocol string, port string) string {
	// 获取实际的地址（IP或域名）
	var addr string
	if IsDynamicType(source.Type) {
		// 对于动态类型，从缓存获取IP
		cacheKey := helper.GetIPCacheKey(source.Type, source.Value)
		if ip, ok := baidu.Cache.DynamicIPs[cacheKey]; ok {
			addr = ip
		} else {
			// 如果缓存中没有，尝试获取
			if ip, ok := helper.GetOrSetDynamicIPWithCache(source.Type, source.Value); ok {
				addr = ip
			} else {
				helper.Warn(helper.LogTypeDCDN, "无法获取动态源站IP [类型=%s, 值=%s]，使用配置值", source.Type, source.Value)
				addr = source.Value
			}
		}
	} else {
		// 静态类型直接使用配置的值
		addr = source.Value
	}

	// 规范化协议为小写
	protocol = strings.ToLower(protocol)

	// 如果端口为空，根据协议设置默认端口
	if port == "" {
		if protocol == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	// 判断是否为 IPv6 地址（需要用方括号包裹）
	if helper.Ipv6Reg.MatchString(addr) {
		// IPv6 格式：http://[2001:db8::1]:80
		return protocol + "://[" + addr + "]:" + port
	}

	// IPv4 或域名格式：http://192.168.1.1:80 或 http://example.com:80
	return protocol + "://" + addr + ":" + port
}

func (baidu *Baidu) createCDN() {
	path := "/v2/domain/" + baidu.CDN.Domain

	// 构建源站配置
	sources := baidu.buildSourcesConfig()
	requestBody := map[string]interface{}{
		"origin": sources,
		"form":   baidu.getFromValue(),
	}

	// DRCDN 类型需要添加 dsa 配置
	cdnType := strings.ToUpper(baidu.CDN.CDNType)
	if cdnType == CDNTypeDRCDN {
		requestBody["productType"] = 1
		requestBody["dsa"] = map[string]interface{}{
			"enabled": true,
		}
	}

	var result map[string]interface{}
	err := baidu.request("PUT", path, requestBody, &result)
	if err != nil {
		baidu.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "创建百度云 %s 域名失败 [域名=%s, 错误=%v]", baidu.getCDNTypeName(), baidu.CDN.Domain, err)
		return
	}

	baidu.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "创建百度云 %s 域名成功 [域名=%s]", baidu.getCDNTypeName(), baidu.CDN.Domain)

	// 创建成功后查询域名信息获取 CNAME
	domainInfo, err := baidu.describeDomain()
	if err != nil {
		helper.Warn(helper.LogTypeDCDN, "创建百度云 %s 域名后查询 CNAME 失败 [域名=%s, 错误=%v]", baidu.getCDNTypeName(), baidu.CDN.Domain, err)
		return
	}
	if domainInfo != nil {
		baidu.updateCnameIfChanged(domainInfo.Cname)
	}
}

func (baidu *Baidu) modifyCDN() {
	path := "/v2/domain/" + baidu.CDN.Domain + "/config?origin"

	// 构建源站配置
	sources := baidu.buildSourcesConfig()
	requestBody := map[string]interface{}{
		"origin": sources,
		"form":   baidu.getFromValue(),
	}

	var result map[string]interface{}
	err := baidu.request("PUT", path, requestBody, &result)
	if err != nil {
		baidu.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "修改百度云 %s 源站配置失败 [域名=%s, 错误=%v]", baidu.getCDNTypeName(), baidu.CDN.Domain, err)
		return
	}

	baidu.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "修改百度云 %s 源站配置成功 [域名=%s]", baidu.getCDNTypeName(), baidu.CDN.Domain)
}

// request 统一请求接口
func (baidu *Baidu) request(method, path string, body interface{}, result interface{}) error {
	endpoint := baiduCDNEndpoint + path
	jsonStr := make([]byte, 0)
	if body != nil {
		jsonStr, _ = json.Marshal(body)
		helper.Debug(helper.LogTypeDCDN, "请求体内容: %s", string(jsonStr))
	}

	req, err := http.NewRequest(
		method,
		endpoint,
		bytes.NewBuffer(jsonStr),
	)

	if err != nil {
		return err
	}

	// 设置请求头（必须在签名之前设置）
	// 只有在有请求体时才设置 Content-Type
	req.Header.Set("Content-Type", "application/json")
	// 设置 Host 字段（Go HTTP 客户端会使用 req.Host 而不是 Header["Host"]）
	req.Host = req.URL.Host

	// 调用签名函数
	signer.BaiduSigner(baidu.CDN.AccessKey, baidu.CDN.AccessSecret, req)

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	err = helper.GetHTTPResponse(resp, err, result)

	// 检查百度云 API 错误
	if err == nil {
		var code, message string
		// 使用类型断言检查是否有 code 字段
		if v, ok := result.(*BaiduDomainInfo); ok && v.Code != "" {
			code = v.Code
			message = v.Message
		} else if v, ok := result.(*BaiduDomainListResponse); ok && v.Code != "" {
			code = v.Code
			message = v.Message
		} else if v, ok := result.(*map[string]interface{}); ok {
			// 对于 map 类型，检查是否包含 code 字段
			if codeVal, exists := (*v)["code"]; exists {
				if codeStr, isStr := codeVal.(string); isStr && codeStr != "" {
					code = codeStr
					if msgVal, msgExists := (*v)["message"]; msgExists {
						if msgStr, isMsgStr := msgVal.(string); isMsgStr {
							message = msgStr
						}
					}
				}
			}
		}

		// 如果存在错误码，记录日志并返回错误
		if code != "" {
			err = errors.New(code + ": " + message)
			// 根据错误类型打印不同级别的日志
			if strings.Contains(code, "AccessDenied") || strings.Contains(code, "InvalidAccessKeyId") {
				helper.Error(helper.LogTypeDCDN, "百度云 API 认证失败：请检查 AccessKey 和 AccessSecret 配置 [错误码=%s, 消息=%s]", code, message)
			} else if strings.Contains(code, "SignatureDoesNotMatch") {
				helper.Error(helper.LogTypeDCDN, "百度云 API 签名错误：请检查 AccessSecret 配置 [错误码=%s, 消息=%s]", code, message)
			} else if strings.Contains(code, "Forbidden") || strings.Contains(code, "Unauthorized") {
				helper.Error(helper.LogTypeDCDN, "百度云 API 权限不足 [错误码=%s, 消息=%s]", code, message)
			} else {
				helper.Warn(helper.LogTypeDCDN, "百度云 API 调用失败 [错误码=%s, 消息=%s]", code, message)
			}
		}
	}

	return err
}
