package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// https://cloud.tencent.com/document/api/228/30977

const (
	TencentDateFormat = "2006-01-02"
	Algorithm         = "TC3-HMAC-SHA256"
)

// TencentSigner 腾讯云 API 3.0 签名方法
// service: 服务名称，如 "cdn" 或 "ecdn"
// host: 请求的域名，如 "cdn.tencentcloudapi.com"
// payload: 请求体内容（JSON 字符串）
func TencentSigner(secretId, secretKey, service, host, payload string, r *http.Request) {
	// 1. 先设置必要的请求头（这些头需要参与签名计算）
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Host", host)
	r.Host = host // Go HTTP 客户端使用 r.Host 而不是 Header["Host"]

	// 2. 获取当前时间
	now := time.Now().UTC()
	timestamp := fmt.Sprintf("%d", now.Unix())
	date := now.Format(TencentDateFormat)

	// 3. 构建规范请求串
	canonicalRequest := buildCanonicalRequest(r, payload)

	// 4. 构建待签名字符串
	credentialScope := fmt.Sprintf("%s/%s/tc3_request", date, service)
	hashedCanonicalRequest := sha256Hex(canonicalRequest)
	stringToSign := fmt.Sprintf("%s\n%s\n%s\n%s",
		Algorithm,
		timestamp,
		credentialScope,
		hashedCanonicalRequest)

	// 5. 计算签名
	secretDate := hmacSha256([]byte("TC3"+secretKey), date)
	secretService := hmacSha256(secretDate, service)
	secretSigning := hmacSha256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSha256(secretSigning, stringToSign))

	// 6. 构建 Authorization
	authorization := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		Algorithm,
		secretId,
		credentialScope,
		getSignedHeaders(r),
		signature)

	// 7. 设置签名相关的请求头
	r.Header.Set("Authorization", authorization)
	r.Header.Set("X-TC-Timestamp", timestamp)
	r.Header.Set("X-TC-Version", getAPIVersion(service))
}

// buildCanonicalRequest 构建规范请求串
func buildCanonicalRequest(r *http.Request, payload string) string {
	// HTTPRequestMethod
	method := r.Method

	// CanonicalURI
	uri := r.URL.Path
	if uri == "" {
		uri = "/"
	}

	// CanonicalQueryString
	query := r.URL.Query()
	var queryKeys []string
	for k := range query {
		queryKeys = append(queryKeys, k)
	}
	sort.Strings(queryKeys)

	var queryParts []string
	for _, k := range queryKeys {
		for _, v := range query[k] {
			queryParts = append(queryParts, fmt.Sprintf("%s=%s", k, v))
		}
	}
	queryString := strings.Join(queryParts, "&")

	// CanonicalHeaders
	canonicalHeaders := buildCanonicalHeaders(r)

	// SignedHeaders
	signedHeaders := getSignedHeaders(r)

	// HashedRequestPayload - 使用传入的 payload 参数
	hashedPayload := sha256Hex(payload)

	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n%s",
		method,
		uri,
		queryString,
		canonicalHeaders,
		signedHeaders,
		hashedPayload)
}

// buildCanonicalHeaders 构建规范请求头
func buildCanonicalHeaders(r *http.Request) string {
	// 获取需要签名的请求头
	headers := make(map[string]string)

	// 添加 content-type 和 host
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		headers["content-type"] = contentType
	}
	if host := r.Header.Get("Host"); host != "" {
		headers["host"] = strings.ToLower(host)
	} else if r.Host != "" {
		headers["host"] = strings.ToLower(r.Host)
	}

	// 按字典序排序
	var keys []string
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 拼接
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%s\n", k, headers[k]))
	}

	return strings.Join(parts, "")
}

// getSignedHeaders 获取签名的请求头列表
func getSignedHeaders(r *http.Request) string {
	headers := []string{"content-type", "host"}
	sort.Strings(headers)
	return strings.Join(headers, ";")
}

// getAPIVersion 根据服务获取 API 版本
func getAPIVersion(service string) string {
	versions := map[string]string{
		"cdn":  "2018-06-06",
		"ecdn": "2022-09-01",
		"teo":  "2022-09-01",
	}
	if v, ok := versions[service]; ok {
		return v
	}
	return "2018-06-06"
}

// hmacSha256 计算 HMAC-SHA256
func hmacSha256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

// sha256Hex 计算 SHA256 哈希并返回十六进制字符串
func sha256Hex(s string) string {
	h := sha256.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}
