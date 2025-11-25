package signer

import (
	"net/url"
	"strings"
	"testing"
)

// TestHmacSign 测试 HMAC 签名
func TestHmacSign(t *testing.T) {
	tests := []struct {
		name         string
		signMethod   string
		httpMethod   string
		appKeySecret string
		params       url.Values
		wantNotEmpty bool
		wantLength   int
	}{
		{
			name:         "HMAC-SHA1 签名",
			signMethod:   "HMAC-SHA1",
			httpMethod:   "GET",
			appKeySecret: "test-secret",
			params: url.Values{
				"Action": []string{"DescribeCdnService"},
				"Format": []string{"JSON"},
			},
			wantNotEmpty: true,
			wantLength:   20, // SHA1 输出 20 字节
		},
		{
			name:         "HMAC-SHA256 签名",
			signMethod:   "HMAC-SHA256",
			httpMethod:   "GET",
			appKeySecret: "test-secret",
			params: url.Values{
				"Action": []string{"DescribeCdnService"},
			},
			wantNotEmpty: true,
			wantLength:   32, // SHA256 输出 32 字节
		},
		{
			name:         "HMAC-MD5 签名",
			signMethod:   "HMAC-MD5",
			httpMethod:   "GET",
			appKeySecret: "test-secret",
			params: url.Values{
				"Action": []string{"DescribeCdnService"},
			},
			wantNotEmpty: true,
			wantLength:   16, // MD5 输出 16 字节
		},
		{
			name:         "未知签名方法默认使用 SHA1",
			signMethod:   "UNKNOWN",
			httpMethod:   "GET",
			appKeySecret: "test-secret",
			params: url.Values{
				"Action": []string{"Test"},
			},
			wantNotEmpty: true,
			wantLength:   20, // 默认 SHA1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HmacSign(tt.signMethod, tt.httpMethod, tt.appKeySecret, tt.params)

			if tt.wantNotEmpty && len(got) == 0 {
				t.Error("HmacSign() 返回空结果")
			}

			if len(got) != tt.wantLength {
				t.Errorf("HmacSign() 长度 = %d, want %d", len(got), tt.wantLength)
			}

			// 测试相同输入产生相同输出
			got2 := HmacSign(tt.signMethod, tt.httpMethod, tt.appKeySecret, tt.params)
			if string(got) != string(got2) {
				t.Error("相同输入应该产生相同签名")
			}
		})
	}
}

// TestHmacSign_DifferentSecrets 测试不同密钥产生不同签名
func TestHmacSign_DifferentSecrets(t *testing.T) {
	params := url.Values{
		"Action": []string{"Test"},
	}

	sig1 := HmacSign("HMAC-SHA1", "GET", "secret1", params)
	sig2 := HmacSign("HMAC-SHA1", "GET", "secret2", params)

	if string(sig1) == string(sig2) {
		t.Error("不同密钥应该产生不同签名")
	}
}

// TestHmacSign_DifferentParams 测试不同参数产生不同签名
func TestHmacSign_DifferentParams(t *testing.T) {
	secret := "test-secret"

	params1 := url.Values{
		"Action": []string{"Action1"},
	}
	params2 := url.Values{
		"Action": []string{"Action2"},
	}

	sig1 := HmacSign("HMAC-SHA1", "GET", secret, params1)
	sig2 := HmacSign("HMAC-SHA1", "GET", secret, params2)

	if string(sig1) == string(sig2) {
		t.Error("不同参数应该产生不同签名")
	}
}

// TestHmacSignToB64 测试 Base64 编码的签名
func TestHmacSignToB64(t *testing.T) {
	tests := []struct {
		name         string
		signMethod   string
		httpMethod   string
		appKeySecret string
		params       url.Values
	}{
		{
			name:         "基本测试",
			signMethod:   "HMAC-SHA1",
			httpMethod:   "GET",
			appKeySecret: "test-secret",
			params: url.Values{
				"Action":    []string{"DescribeCdnService"},
				"Version":   []string{"2018-05-10"},
				"Timestamp": []string{"2024-01-01T00:00:00Z"},
			},
		},
		{
			name:         "空参数",
			signMethod:   "HMAC-SHA1",
			httpMethod:   "POST",
			appKeySecret: "secret",
			params:       url.Values{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HmacSignToB64(tt.signMethod, tt.httpMethod, tt.appKeySecret, tt.params)

			if got == "" {
				t.Error("HmacSignToB64() 返回空字符串")
			}

			// 验证是有效的 Base64
			// Base64 字符集：A-Z, a-z, 0-9, +, /, =
			for _, c := range got {
				if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
					(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=') {
					t.Errorf("HmacSignToB64() 包含非 Base64 字符: %c", c)
				}
			}

			// 测试一致性
			got2 := HmacSignToB64(tt.signMethod, tt.httpMethod, tt.appKeySecret, tt.params)
			if got != got2 {
				t.Error("相同输入应该产生相同的 Base64 签名")
			}
		})
	}
}

// TestSpecialUrlEncode 测试特殊 URL 编码
func TestSpecialUrlEncode(t *testing.T) {
	// 这个函数是内部函数，通过 HmacSign 间接测试
	tests := []struct {
		name   string
		params url.Values
	}{
		{
			name: "包含特殊字符",
			params: url.Values{
				"Name": []string{"test~name"},
				"Path": []string{"/path/to/resource"},
			},
		},
		{
			name: "包含空格",
			params: url.Values{
				"Message": []string{"hello world"},
			},
		},
		{
			name: "包含等号和&",
			params: url.Values{
				"Query": []string{"key=value&foo=bar"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 通过生成签名来测试编码
			sig := HmacSign("HMAC-SHA1", "GET", "secret", tt.params)
			if len(sig) == 0 {
				t.Error("特殊字符编码失败")
			}
		})
	}
}

// TestAliyunSigner 测试阿里云签名器
func TestAliyunSigner(t *testing.T) {
	tests := []struct {
		name          string
		accessKeyID   string
		accessSecret  string
		initialParams url.Values
	}{
		{
			name:         "基本参数",
			accessKeyID:  "test-key-id",
			accessSecret: "test-secret",
			initialParams: url.Values{
				"Action":  []string{"DescribeCdnService"},
				"Version": []string{"2018-05-10"},
			},
		},
		{
			name:          "空初始参数",
			accessKeyID:   "key123",
			accessSecret:  "secret123",
			initialParams: url.Values{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := make(url.Values)
			for k, v := range tt.initialParams {
				params[k] = v
			}

			AliyunSigner(tt.accessKeyID, tt.accessSecret, &params)

			// 验证必需的公共参数
			requiredParams := []string{
				"SignatureMethod",
				"SignatureNonce",
				"AccessKeyId",
				"SignatureVersion",
				"Timestamp",
				"Format",
				"Signature",
			}

			for _, param := range requiredParams {
				if params.Get(param) == "" {
					t.Errorf("缺少必需参数: %s", param)
				}
			}

			// 验证参数值
			if params.Get("SignatureMethod") != "HMAC-SHA1" {
				t.Errorf("SignatureMethod = %v, want HMAC-SHA1", params.Get("SignatureMethod"))
			}

			if params.Get("SignatureVersion") != "1.0" {
				t.Errorf("SignatureVersion = %v, want 1.0", params.Get("SignatureVersion"))
			}

			if params.Get("AccessKeyId") != tt.accessKeyID {
				t.Errorf("AccessKeyId = %v, want %v", params.Get("AccessKeyId"), tt.accessKeyID)
			}

			if params.Get("Format") != "JSON" {
				t.Errorf("Format = %v, want JSON", params.Get("Format"))
			}

			// 验证 Timestamp 格式 (RFC3339 格式)
			timestamp := params.Get("Timestamp")
			if !strings.Contains(timestamp, "T") || !strings.Contains(timestamp, "Z") {
				t.Errorf("Timestamp 格式不正确: %v", timestamp)
			}

			// 验证 SignatureNonce 不为空
			if params.Get("SignatureNonce") == "" {
				t.Error("SignatureNonce 不应为空")
			}

			// 验证 Signature 不为空
			if params.Get("Signature") == "" {
				t.Error("Signature 不应为空")
			}
		})
	}
}

// TestAliyunSigner_UniqueNonce 测试每次调用生成唯一的 Nonce
func TestAliyunSigner_UniqueNonce(t *testing.T) {
	accessKeyID := "test-key"
	accessSecret := "test-secret"

	params1 := make(url.Values)
	AliyunSigner(accessKeyID, accessSecret, &params1)

	// 等待一纳秒确保时间不同
	params2 := make(url.Values)
	AliyunSigner(accessKeyID, accessSecret, &params2)

	nonce1 := params1.Get("SignatureNonce")
	nonce2 := params2.Get("SignatureNonce")

	if nonce1 == nonce2 {
		t.Error("两次调用应该生成不同的 Nonce")
	}
}

// TestAliyunSigner_PreserveUserParams 测试保留用户原有参数
func TestAliyunSigner_PreserveUserParams(t *testing.T) {
	accessKeyID := "test-key"
	accessSecret := "test-secret"

	params := url.Values{
		"Action":     []string{"DescribeCdnService"},
		"Version":    []string{"2018-05-10"},
		"DomainName": []string{"example.com"},
	}

	AliyunSigner(accessKeyID, accessSecret, &params)

	// 验证用户参数仍然存在
	if params.Get("Action") != "DescribeCdnService" {
		t.Error("用户参数 Action 丢失或被修改")
	}
	if params.Get("Version") != "2018-05-10" {
		t.Error("用户参数 Version 丢失或被修改")
	}
	if params.Get("DomainName") != "example.com" {
		t.Error("用户参数 DomainName 丢失或被修改")
	}
}

// TestHmacSign_HTTPMethods 测试不同 HTTP 方法产生不同签名
func TestHmacSign_HTTPMethods(t *testing.T) {
	params := url.Values{
		"Action": []string{"Test"},
	}
	secret := "test-secret"

	sigGET := HmacSign("HMAC-SHA1", "GET", secret, params)
	sigPOST := HmacSign("HMAC-SHA1", "POST", secret, params)

	if string(sigGET) == string(sigPOST) {
		t.Error("不同 HTTP 方法应该产生不同签名")
	}
}

// BenchmarkHmacSign 签名性能测试
func BenchmarkHmacSign(b *testing.B) {
	params := url.Values{
		"Action":    []string{"DescribeCdnService"},
		"Version":   []string{"2018-05-10"},
		"Timestamp": []string{"2024-01-01T00:00:00Z"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HmacSign("HMAC-SHA1", "GET", "test-secret", params)
	}
}

// BenchmarkHmacSignToB64 Base64 签名性能测试
func BenchmarkHmacSignToB64(b *testing.B) {
	params := url.Values{
		"Action":    []string{"DescribeCdnService"},
		"Version":   []string{"2018-05-10"},
		"Timestamp": []string{"2024-01-01T00:00:00Z"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HmacSignToB64("HMAC-SHA1", "GET", "test-secret", params)
	}
}

// BenchmarkAliyunSigner 阿里云签名器性能测试
func BenchmarkAliyunSigner(b *testing.B) {
	params := url.Values{
		"Action":  []string{"DescribeCdnService"},
		"Version": []string{"2018-05-10"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := make(url.Values)
		for k, v := range params {
			p[k] = v
		}
		AliyunSigner("test-key", "test-secret", &p)
	}
}
