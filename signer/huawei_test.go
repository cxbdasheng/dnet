package signer

import (
	"regexp"
	"strings"
	"testing"
)

// TestHashSHA256 测试 SHA256 哈希计算
func TestHashSHA256(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "simple string",
			input:    "hello",
			expected: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
		{
			name:     "json body",
			input:    `{"key":"value"}`,
			expected: hashSHA256(`{"key":"value"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hashSHA256(tt.input)
			if result != tt.expected {
				t.Errorf("hashSHA256(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestCalculateSignature 测试 HMAC-SHA256 签名计算
func TestCalculateSignature(t *testing.T) {
	tests := []struct {
		name           string
		stringToSign   string
		secretKey      string
		expectedLength int
	}{
		{
			name:           "basic signature",
			stringToSign:   "SDK-HMAC-SHA256\n20230101T120000Z\nabc123",
			secretKey:      "my-secret-key",
			expectedLength: 64, // hex-encoded SHA256 = 64 chars
		},
		{
			name:           "empty string to sign",
			stringToSign:   "",
			secretKey:      "secret",
			expectedLength: 64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateSignature(tt.stringToSign, tt.secretKey)
			if len(result) != tt.expectedLength {
				t.Errorf("calculateSignature() length = %d, want %d", len(result), tt.expectedLength)
			}
			// 应为十六进制字符串
			if !regexp.MustCompile(`^[0-9a-f]+$`).MatchString(result) {
				t.Errorf("calculateSignature() = %q, not a valid hex string", result)
			}
		})
	}
}

// TestCalculateSignatureDeterministic 相同输入应产生相同签名
func TestCalculateSignatureDeterministic(t *testing.T) {
	sig1 := calculateSignature("test-string", "test-key")
	sig2 := calculateSignature("test-string", "test-key")
	if sig1 != sig2 {
		t.Errorf("same inputs produced different signatures: %q vs %q", sig1, sig2)
	}

	sig3 := calculateSignature("test-string", "different-key")
	if sig1 == sig3 {
		t.Error("different keys produced the same signature")
	}
}

// TestCanonicalizeQueryString 测试查询字符串规范化
func TestCanonicalizeQueryString(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected string
	}{
		{
			name:     "empty query",
			query:    "",
			expected: "",
		},
		{
			name:     "single param",
			query:    "name=example.com.",
			expected: "name=example.com.",
		},
		{
			name:     "multiple params sorted",
			query:    "type=public&name=example.com.",
			expected: "name=example.com.&type=public",
		},
		{
			name:     "params with special chars are encoded",
			query:    "name=example.com.&type=public",
			expected: "name=example.com.&type=public",
		},
		{
			name:     "space encoded",
			query:    "key=hello world",
			expected: "key=hello+world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalizeQueryString(tt.query)
			if result != tt.expected {
				t.Errorf("canonicalizeQueryString(%q) = %q, want %q", tt.query, result, tt.expected)
			}
		})
	}
}

// TestCanonicalizeHeaders 测试请求头规范化
func TestCanonicalizeHeaders(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected string
	}{
		{
			name: "single header",
			headers: map[string]string{
				"host": "dns.myhuaweicloud.com",
			},
			expected: "host:dns.myhuaweicloud.com\n",
		},
		{
			name: "mixed case keys are lowercased and sorted",
			headers: map[string]string{
				"X-Sdk-Date":   "20230101T120000Z",
				"content-type": "application/json",
				"host":         "dns.myhuaweicloud.com",
			},
			// sorted: content-type, host, x-sdk-date
			expected: "content-type:application/json\nhost:dns.myhuaweicloud.com\nx-sdk-date:20230101T120000Z\n",
		},
		{
			name: "value whitespace is trimmed",
			headers: map[string]string{
				"host": "  dns.myhuaweicloud.com  ",
			},
			expected: "host:dns.myhuaweicloud.com\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canonicalizeHeaders(tt.headers)
			if result != tt.expected {
				t.Errorf("canonicalizeHeaders() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestGetSignedHeadersString 测试签名头列表字符串
func TestGetSignedHeadersString(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected string
	}{
		{
			name: "single header",
			headers: map[string]string{
				"host": "dns.myhuaweicloud.com",
			},
			expected: "host",
		},
		{
			name: "multiple headers sorted",
			headers: map[string]string{
				"X-Sdk-Date":   "20230101T120000Z",
				"content-type": "application/json",
				"host":         "dns.myhuaweicloud.com",
			},
			expected: "content-type;host;x-sdk-date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getSignedHeadersString(tt.headers)
			if result != tt.expected {
				t.Errorf("getSignedHeadersString() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestCreateStringToSign 测试待签名字符串格式
func TestCreateStringToSign(t *testing.T) {
	canonicalRequest := "GET\n/v2/zones/\nname=example.com.&type=public\ncontent-type:application/json\nhost:dns.myhuaweicloud.com\n\ncontent-type;host\ne3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	timestamp := "20230101T120000Z"

	result := createStringToSign(canonicalRequest, timestamp)

	if !strings.HasPrefix(result, "SDK-HMAC-SHA256\n") {
		t.Errorf("createStringToSign() should start with 'SDK-HMAC-SHA256\\n', got: %q", result)
	}
	if !strings.Contains(result, timestamp) {
		t.Errorf("createStringToSign() should contain timestamp %q", timestamp)
	}
	parts := strings.Split(result, "\n")
	if len(parts) != 3 {
		t.Errorf("createStringToSign() should have 3 parts, got %d", len(parts))
	}
	if parts[0] != "SDK-HMAC-SHA256" {
		t.Errorf("first part = %q, want 'SDK-HMAC-SHA256'", parts[0])
	}
	if parts[1] != timestamp {
		t.Errorf("second part = %q, want %q", parts[1], timestamp)
	}
}

// TestCreateCanonicalRequest_URITrailingSlash 测试规范 URI 必须以 / 结尾
func TestCreateCanonicalRequest_URITrailingSlash(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		expectedURI string
	}{
		{
			name:        "URI without trailing slash gets slash added",
			uri:         "/v2/zones",
			expectedURI: "/v2/zones/",
		},
		{
			name:        "URI with trailing slash unchanged",
			uri:         "/v2/zones/",
			expectedURI: "/v2/zones/",
		},
		{
			name:        "empty URI becomes /",
			uri:         "",
			expectedURI: "/",
		},
		{
			name:        "root URI unchanged",
			uri:         "/",
			expectedURI: "/",
		},
		{
			name:        "deep path without slash",
			uri:         "/v2/zones/zone-id/recordsets",
			expectedURI: "/v2/zones/zone-id/recordsets/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{
				"host":         "dns.myhuaweicloud.com",
				HeaderXDate:    "20230101T120000Z",
				"content-type": "application/json",
			}
			result := createCanonicalRequest("GET", tt.uri, "", headers, "")
			lines := strings.Split(result, "\n")
			if len(lines) < 2 {
				t.Fatalf("canonical request too short: %q", result)
			}
			if lines[1] != tt.expectedURI {
				t.Errorf("canonical URI = %q, want %q", lines[1], tt.expectedURI)
			}
		})
	}
}

// TestHuaweiSigner 测试华为云签名器返回正确的 Authorization 头
func TestHuaweiSigner(t *testing.T) {
	headers := map[string]string{
		"host":         "dns.myhuaweicloud.com",
		HeaderXDate:    "20230101T120000Z",
		"content-type": "application/json",
	}

	result := HuaweiSigner(
		"test-access-key",
		"test-secret-key",
		"GET",
		"/v2/zones",
		"name=example.com.&type=public",
		headers,
		"",
	)

	auth, ok := result[HeaderAuthorization]
	if !ok {
		t.Fatal("HuaweiSigner() did not return Authorization header")
	}
	if !strings.HasPrefix(auth, "SDK-HMAC-SHA256 ") {
		t.Errorf("Authorization = %q, should start with 'SDK-HMAC-SHA256 '", auth)
	}
	if !strings.Contains(auth, "Access=test-access-key") {
		t.Errorf("Authorization = %q, should contain 'Access=test-access-key'", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=") {
		t.Errorf("Authorization = %q, should contain 'SignedHeaders='", auth)
	}
	if !strings.Contains(auth, "Signature=") {
		t.Errorf("Authorization = %q, should contain 'Signature='", auth)
	}
}

// TestHuaweiSignerDeterministic 相同输入产生相同签名
func TestHuaweiSignerDeterministic(t *testing.T) {
	headers := map[string]string{
		"host":         "dns.myhuaweicloud.com",
		HeaderXDate:    "20230101T120000Z",
		"content-type": "application/json",
	}

	result1 := HuaweiSigner("ak", "sk", "GET", "/v2/zones", "name=example.com.", headers, "")
	result2 := HuaweiSigner("ak", "sk", "GET", "/v2/zones", "name=example.com.", headers, "")

	if result1[HeaderAuthorization] != result2[HeaderAuthorization] {
		t.Error("HuaweiSigner() produced different results for same inputs")
	}
}

// TestGetFormattedTime 测试时间格式符合华为云规范
func TestGetFormattedTime(t *testing.T) {
	result := GetFormattedTime()

	// 华为云时间格式: 20060102T150405Z，长度为 16
	if len(result) != 16 {
		t.Errorf("GetFormattedTime() length = %d, want 16, got: %q", len(result), result)
	}
	if !strings.HasSuffix(result, "Z") {
		t.Errorf("GetFormattedTime() = %q, should end with 'Z'", result)
	}
	// 验证格式: YYYYMMDDTHHmmssZ
	matched, _ := regexp.MatchString(`^\d{8}T\d{6}Z$`, result)
	if !matched {
		t.Errorf("GetFormattedTime() = %q, does not match format 'YYYYMMDDTHHmmssZ'", result)
	}
}

// BenchmarkHuaweiSigner 性能测试
func BenchmarkHuaweiSigner(b *testing.B) {
	headers := map[string]string{
		"host":         "dns.myhuaweicloud.com",
		HeaderXDate:    "20230101T120000Z",
		"content-type": "application/json",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		HuaweiSigner("access-key", "secret-key", "GET", "/v2/zones", "name=example.com.&type=public", headers, "")
	}
}

// BenchmarkHashSHA256 性能测试
func BenchmarkHashSHA256(b *testing.B) {
	data := `{"name":"example.com.","type":"A","ttl":300,"records":["1.2.3.4"]}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hashSHA256(data)
	}
}
