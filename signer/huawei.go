package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	HeaderXDate         = "X-Sdk-Date"
	HeaderHost          = "host"
	HeaderAuthorization = "Authorization"
	HeaderContentSHA256 = "X-Sdk-Content-Sha256"
)

// HuaweiSigner 华为云签名器
// 参考文档: https://support.huaweicloud.com/devg-apisign/api-sign-algorithm.html
func HuaweiSigner(accessKey, secretKey, method, uri, query string, headers map[string]string, body string) map[string]string {
	// 1. 创建规范请求
	canonicalRequest := createCanonicalRequest(method, uri, query, headers, body)

	// 2. 创建待签名字符串
	stringToSign := createStringToSign(canonicalRequest)

	// 3. 计算签名
	signature := calculateSignature(stringToSign, secretKey)

	// 4. 添加签名到请求头
	signedHeaders := getSignedHeadersString(headers)
	authValue := fmt.Sprintf("SDK-HMAC-SHA256 Access=%s, SignedHeaders=%s, Signature=%s",
		accessKey, signedHeaders, signature)

	// 返回需要添加到请求头的信息
	result := make(map[string]string)
	result[HeaderAuthorization] = authValue

	return result
}

// createCanonicalRequest 创建规范请求
func createCanonicalRequest(method, uri, query string, headers map[string]string, body string) string {
	// 规范化 URI
	canonicalURI := uri
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	// 规范化查询字符串
	canonicalQueryString := canonicalizeQueryString(query)

	// 规范化请求头
	canonicalHeaders := canonicalizeHeaders(headers)

	// 签名头列表
	signedHeaders := getSignedHeadersString(headers)

	// 请求体哈希
	hashedPayload := hashSHA256(body)

	// 拼接规范请求
	return strings.Join([]string{
		method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		hashedPayload,
	}, "\n")
}

// canonicalizeQueryString 规范化查询字符串
func canonicalizeQueryString(query string) string {
	if query == "" {
		return ""
	}

	// 解析查询参数
	values, err := url.ParseQuery(query)
	if err != nil {
		return ""
	}

	// 对参数进行排序
	var keys []string
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 拼接规范化查询字符串
	var parts []string
	for _, k := range keys {
		for _, v := range values[k] {
			parts = append(parts, fmt.Sprintf("%s=%s",
				url.QueryEscape(k), url.QueryEscape(v)))
		}
	}

	return strings.Join(parts, "&")
}

// canonicalizeHeaders 规范化请求头
func canonicalizeHeaders(headers map[string]string) string {
	// 获取所有请求头的键并排序
	var keys []string
	for k := range headers {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Strings(keys)

	// 拼接规范化请求头
	var parts []string
	for _, k := range keys {
		// 查找原始键（可能是大小写混合的）
		var originalValue string
		for originalKey, value := range headers {
			if strings.ToLower(originalKey) == k {
				originalValue = strings.TrimSpace(value)
				break
			}
		}
		parts = append(parts, fmt.Sprintf("%s:%s", k, originalValue))
	}

	return strings.Join(parts, "\n") + "\n"
}

// getSignedHeadersString 获取签名头列表字符串
func getSignedHeadersString(headers map[string]string) string {
	var keys []string
	for k := range headers {
		keys = append(keys, strings.ToLower(k))
	}
	sort.Strings(keys)
	return strings.Join(keys, ";")
}

// createStringToSign 创建待签名字符串
func createStringToSign(canonicalRequest string) string {
	return fmt.Sprintf("SDK-HMAC-SHA256\n%s\n%s",
		time.Now().UTC().Format("20060102T150405Z"),
		hashSHA256(canonicalRequest))
}

// calculateSignature 计算签名
func calculateSignature(stringToSign, secretKey string) string {
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(stringToSign))
	return hex.EncodeToString(h.Sum(nil))
}

// hashSHA256 计算 SHA256 哈希值
func hashSHA256(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// GetFormattedTime 获取华为云 API 所需的时间格式
func GetFormattedTime() string {
	return time.Now().UTC().Format("20060102T150405Z")
}
