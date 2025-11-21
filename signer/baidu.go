package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// https://cloud.baidu.com/doc/Reference/s/Njwvz1wot

const (
	BaiduDateFormat  = "2006-01-02T15:04:05Z"
	expirationPeriod = "1800"
)

func HmacSha256Hex(secret, message string) string {
	key := []byte(secret)

	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))
	sha := hex.EncodeToString(h.Sum(nil))
	return sha
}

func BaiduCanonicalURI(r *http.Request) string {
	patterns := strings.Split(r.URL.Path, "/")
	var uri []string
	for _, v := range patterns {
		uri = append(uri, escape(v))
	}
	urlpath := strings.Join(uri, "/")
	return urlpath
}

// BaiduCanonicalQueryString 构建规范的查询字符串
// 根据百度云 BCE 签名规范：
// 1. 对查询参数按照参数名进行字典序排序
// 2. 参数名和参数值都需要进行 URI 编码
// 3. 多个参数用 & 连接
func BaiduCanonicalQueryString(r *http.Request) string {
	query := r.URL.Query()
	if len(query) == 0 {
		return ""
	}

	// 获取所有参数名并排序
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构建规范查询字符串
	var parts []string
	for _, key := range keys {
		encodedKey := url.QueryEscape(key)
		values := query[key]
		// 对每个值进行排序（如果同一个参数有多个值）
		sort.Strings(values)
		for _, value := range values {
			encodedValue := url.QueryEscape(value)
			parts = append(parts, encodedKey+"="+encodedValue)
		}
	}

	return strings.Join(parts, "&")
}

// BaiduSigner set Authorization header
func BaiduSigner(accessKeyID, accessSecret string, r *http.Request) {
	//format: bce-auth-v1/{accessKeyId}/{timestamp}/{expirationPeriodInSeconds}
	now := time.Now().UTC()
	timestamp := now.Format(BaiduDateFormat)
	authStringPrefix := "bce-auth-v1/" + accessKeyID + "/" + timestamp + "/" + expirationPeriod
	baiduCanonicalURL := BaiduCanonicalURI(r)
	baiduCanonicalQueryString := BaiduCanonicalQueryString(r)

	//// 设置 x-bce-date header（使用与 authStringPrefix 相同的时间戳）
	//r.Header.Set("x-bce-date", timestamp)

	// 构建 CanonicalHeaders，根据请求类型和请求头动态包含
	var canonicalHeaders string
	var signedHeaders string

	//contentType := r.Header.Get("Content-Type")
	// 使用 req.Host 而不是 Header.Get("Host")，因为 Go HTTP 客户端会特殊处理 Host
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	if host == "" {
		host = "cdn.baidubce.com"
	}

	//// 对于 PUT/POST 请求，包含 x-bce-date
	//if r.Method == "PUT" || r.Method == "POST" {
	//	if contentType != "" {
	//		// headers 按字典序排列：content-type, host, x-bce-date
	//		canonicalHeaders = "content-type:" + contentType + "\nhost:" + host + "\nx-bce-date:" + timestamp
	//		signedHeaders = "content-type;host;x-bce-date"
	//	} else {
	//		canonicalHeaders = "host:" + host + "\nx-bce-date:" + timestamp
	//		signedHeaders = "host;x-bce-date"
	//	}
	//} else {
	//	// GET 请求只包含 host
	//	canonicalHeaders = "host:" + host
	//	signedHeaders = "host"
	//}
	canonicalHeaders = "host:" + host
	signedHeaders = "host"
	//format: HTTP Method + "\n" + CanonicalURI + "\n" + CanonicalQueryString + "\n" + CanonicalHeaders
	CanonicalReq := fmt.Sprintf("%s\n%s\n%s\n%s", r.Method, baiduCanonicalURL, baiduCanonicalQueryString, canonicalHeaders)

	//// 调试输出
	//fmt.Printf("\n========== 百度云签名调试信息 ==========\n")
	//fmt.Printf("请求方法: %s\n", r.Method)
	//fmt.Printf("完整URL: %s\n", r.URL.String())
	//fmt.Printf("CanonicalURI: %s\n", baiduCanonicalURL)
	//fmt.Printf("CanonicalQueryString: %s\n", baiduCanonicalQueryString)
	//fmt.Printf("Host: %s\n", host)
	//fmt.Printf("Content-Type: %s\n", contentType)
	//fmt.Printf("SignedHeaders: %s\n", signedHeaders)
	//fmt.Printf("CanonicalHeaders:\n%s\n", canonicalHeaders)
	//fmt.Printf("authStringPrefix: %s\n", authStringPrefix)
	//fmt.Printf("CanonicalRequest:\n%s\n", CanonicalReq)

	signingKey := HmacSha256Hex(accessSecret, authStringPrefix)
	signature := HmacSha256Hex(signingKey, CanonicalReq)

	//fmt.Printf("SigningKey: %s\n", signingKey)
	//fmt.Printf("Signature: %s\n", signature)

	//format: authStringPrefix/{signedHeaders}/{signature}
	authString := authStringPrefix + "/" + signedHeaders + "/" + signature
	//fmt.Printf("Authorization: %s\n", authString)
	//fmt.Printf("========================================\n\n")

	r.Header.Set(HeaderAuthorization, authString)
}
