package dcdn

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

const (
	upyunAPIEndpoint = "https://api.upyun.com"
)

type Upyun struct {
	BaseProvider
}

// UpyunDomainBucketData GET /domains/buckets 返回的数据
type UpyunDomainBucketData struct {
	BucketName   string `json:"bucket_name"`
	Domain       string `json:"domain"`
	DomainStatus string `json:"domain_status"`
}

// UpyunSourceServer 又拍云 CDN 源站服务器配置
type UpyunSourceServer struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Weight      int    `json:"weight"`
	MaxFails    int    `json:"max_fails"`
	FailTimeout int    `json:"fail_timeout"`
	Backup      bool   `json:"backup,omitempty"`
}

func (upyun *Upyun) Init(cdnConfig *config.CDN, cache *Cache) {
	upyun.CDN = cdnConfig
	upyun.Cache = cache
	upyun.Status = InitFailed

	if upyun.validateConfig() {
		upyun.Status = InitSuccess
		helper.Info(helper.LogTypeDCDN, "又拍云 CDN 初始化成功 [域名=%s, 源站数量=%d]", cdnConfig.Domain, len(cdnConfig.Sources))
	} else {
		helper.Error(helper.LogTypeDCDN, "又拍云 CDN 初始化失败：配置校验不通过 [域名=%s]", cdnConfig.Domain)
	}
}

func (upyun *Upyun) validateConfig() bool {
	if upyun.CDN == nil {
		helper.Warn(helper.LogTypeDCDN, "又拍云配置校验失败：配置对象为空")
		return false
	}
	if upyun.CDN.AccessKey == "" {
		helper.Warn(helper.LogTypeDCDN, "又拍云 CDN 配置校验失败：Token 为空 [域名=%s]", upyun.CDN.Domain)
		return false
	}
	if upyun.CDN.Domain == "" {
		helper.Warn(helper.LogTypeDCDN, "又拍云 CDN 配置校验失败：域名为空")
		return false
	}
	if len(upyun.CDN.Sources) == 0 {
		helper.Warn(helper.LogTypeDCDN, "又拍云 CDN 配置校验失败：源站配置为空 [域名=%s]", upyun.CDN.Domain)
		return false
	}
	return true
}

func (upyun *Upyun) UpdateOrCreateSources() bool {
	return upyun.runUpdateOrCreate("又拍云 CDN", upyun.updateOrCreateSite)
}

func (upyun *Upyun) updateOrCreateSite() {
	bucketName, cname, err := upyun.queryBucketByDomain()
	if err != nil {
		helper.Error(helper.LogTypeDCDN, "查询又拍云 CDN 域名关联服务失败 [域名=%s, 错误=%v]", upyun.CDN.Domain, err)
		upyun.Status = UpdatedFailed
		return
	}

	if bucketName == "" {
		newBucket := upyun.generateBucketName()
		helper.Info(helper.LogTypeDCDN, "域名未绑定，创建服务 [服务=%s]", newBucket)
		if err := upyun.createBucket(newBucket); err != nil {
			helper.Error(helper.LogTypeDCDN, "创建又拍云服务失败 [服务=%s, 错误=%v]", newBucket, err)
			upyun.Status = UpdatedFailed
			return
		}
		helper.Info(helper.LogTypeDCDN, "服务创建成功，添加域名 [域名=%s, 服务=%s]", upyun.CDN.Domain, newBucket)
		if err := upyun.bindDomain(newBucket); err != nil {
			helper.Error(helper.LogTypeDCDN, "添加又拍云域名失败 [域名=%s, 服务=%s, 错误=%v]", upyun.CDN.Domain, newBucket, err)
			upyun.Status = UpdatedFailed
			return
		}
		bucketName = newBucket
		cname = bucketName + ".b0.aicdn.com"
		helper.Info(helper.LogTypeDCDN, "域名添加成功 [域名=%s, 服务=%s]", upyun.CDN.Domain, bucketName)
	}

	helper.Info(helper.LogTypeDCDN, "域名已关联服务，开始更新源站配置 [域名=%s, 服务=%s]", upyun.CDN.Domain, bucketName)
	upyun.updateCnameIfChanged(cname)
	upyun.updateSources(bucketName)
}

// generateBucketName 生成又拍云服务名：优先使用配置的 Service，否则用 "dnet-" + 域名（点替换为横线）
func (upyun *Upyun) generateBucketName() string {
	if upyun.CDN.Name != "" {
		return upyun.CDN.Name
	}
	return "dnet-" + strings.ReplaceAll(upyun.CDN.Domain, ".", "-")
}

// createBucket 在又拍云创建 CDN 服务（bucket）
func (upyun *Upyun) createBucket(bucketName string) error {
	body := map[string]interface{}{
		"bucket_name": bucketName,
		"type":        "cdn",
	}
	var result map[string]interface{}
	return upyun.request(http.MethodPut, "/buckets", body, &result)
}

// bindDomain 将域名添加到指定的又拍云服务
func (upyun *Upyun) bindDomain(bucketName string) error {
	body := map[string]interface{}{
		"bucket_name": bucketName,
		"domain":      upyun.CDN.Domain,
	}
	var result map[string]interface{}
	return upyun.request(http.MethodPut, "/buckets/domains", body, &result)
}

// queryBucketByDomain 通过域名查询关联的又拍云服务名和 CNAME
func (upyun *Upyun) queryBucketByDomain() (bucketName, cname string, err error) {
	var result struct {
		Result bool                  `json:"result"`
		Data   UpyunDomainBucketData `json:"data"`
	}
	path := "/domains/buckets?domain=" + url.QueryEscape(upyun.CDN.Domain)
	err = upyun.request(http.MethodGet, path, nil, &result)
	if err != nil {
		if strings.Contains(err.Error(), "DomainNotFound") || strings.Contains(err.Error(), "[404]") {
			return "", "", nil
		}
		return "", "", err
	}
	if !result.Result || result.Data.BucketName == "" {
		return "", "", nil
	}
	bucketName = result.Data.BucketName
	cname = bucketName + ".b0.aicdn.com"
	return bucketName, cname, nil
}

// buildServers 构建又拍云 CDN 源站服务器列表
func (upyun *Upyun) buildServers() []UpyunSourceServer {
	var servers []UpyunSourceServer
	for _, source := range upyun.CDN.Sources {
		addr := upyun.getSourceAddr(&source)

		portStr := source.Port
		if strings.ToUpper(source.Protocol) == "HTTPS" {
			portStr = source.HttpsPort
		}
		port := 80
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			port = p
		}

		weight := 10
		if w, err := strconv.Atoi(source.Weight); err == nil && w > 0 {
			weight = w
		}

		servers = append(servers, UpyunSourceServer{
			Host:        addr,
			Port:        port,
			Weight:      weight,
			MaxFails:    3,
			FailTimeout: 10,
			Backup:      source.Priority == "backup",
		})
	}
	return servers
}

// getSourceType 根据第一个源站协议确定又拍云回源协议类型
func (upyun *Upyun) getSourceType() string {
	if len(upyun.CDN.Sources) == 0 {
		return "http"
	}
	switch strings.ToUpper(upyun.CDN.Sources[0].Protocol) {
	case "HTTPS":
		return "https"
	case "AUTO":
		return "protocol_follow"
	default:
		return "http"
	}
}

func (upyun *Upyun) updateSources(bucketName string) {
	body := map[string]interface{}{
		"bucket_name": bucketName,
		"source_type": upyun.getSourceType(),
		"cdn": map[string]interface{}{
			"servers": upyun.buildServers(),
		},
	}

	var result map[string]interface{}
	err := upyun.request(http.MethodPost, "/v2/buckets/cdn/source", body, &result)
	if err != nil {
		upyun.Status = UpdatedFailed
		helper.Error(helper.LogTypeDCDN, "更新又拍云 CDN 源站配置失败 [域名=%s, 错误=%v]", upyun.CDN.Domain, err)
		return
	}

	upyun.Status = UpdatedSuccess
	helper.Info(helper.LogTypeDCDN, "更新又拍云 CDN 源站配置成功 [域名=%s, 服务=%s]", upyun.CDN.Domain, bucketName)
}

// buildAuth 构建又拍云 Bearer Token 认证头
func (upyun *Upyun) buildAuth() string {
	return "Bearer " + upyun.CDN.AccessKey
}

// request 统一请求接口
func (upyun *Upyun) request(method, path string, body interface{}, result interface{}) error {
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	helper.Debug(helper.LogTypeDCDN, "又拍云请求 [%s %s]: %s", method, path, string(bodyBytes))

	req, err := http.NewRequest(method, upyunAPIEndpoint+path, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", upyun.buildAuth())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))

	client := helper.CreateHTTPClient()
	resp, err := client.Do(req)
	err = helper.GetHTTPResponse(resp, err, result)

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "[401]") || strings.Contains(errStr, "[403]") {
			helper.Error(helper.LogTypeDCDN, "又拍云 API 认证失败：请检查 Token 配置 [错误=%v]", err)
		} else if !strings.Contains(errStr, "[404]") {
			helper.Warn(helper.LogTypeDCDN, "又拍云 API 调用失败 [错误=%v]", err)
		}
	}

	return err
}
