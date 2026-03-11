package signer

import (
	"net/http"
	"strings"
	"testing"
)

// TestSha256Hex 测试 SHA256 十六进制哈希
func TestSha256Hex(t *testing.T) {
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
			name:     "json payload",
			input:    `{"Domain":"example.com","SubDomain":"www"}`,
			expected: sha256Hex(`{"Domain":"example.com","SubDomain":"www"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sha256Hex(tt.input)
			if result != tt.expected {
				t.Errorf("sha256Hex(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestSha256HexDeterministic 相同输入产生相同哈希
func TestSha256HexDeterministic(t *testing.T) {
	input := "test payload"
	h1 := sha256Hex(input)
	h2 := sha256Hex(input)
	if h1 != h2 {
		t.Errorf("sha256Hex() not deterministic: %q vs %q", h1, h2)
	}
	if h1 == sha256Hex("different payload") {
		t.Error("different inputs should produce different hashes")
	}
}

// TestHmacSha256 测试 HMAC-SHA256 计算
func TestHmacSha256(t *testing.T) {
	tests := []struct {
		name           string
		key            []byte
		data           string
		expectedLength int
	}{
		{
			name:           "basic",
			key:            []byte("TC3secret-key"),
			data:           "2023-01-01",
			expectedLength: 32, // SHA256 输出 32 字节
		},
		{
			name:           "empty data",
			key:            []byte("key"),
			data:           "",
			expectedLength: 32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hmacSha256(tt.key, tt.data)
			if len(result) != tt.expectedLength {
				t.Errorf("hmacSha256() length = %d, want %d", len(result), tt.expectedLength)
			}
		})
	}
}

// TestHmacSha256DeterministicAndDifferent 相同输入产生相同结果，不同输入产生不同结果
func TestHmacSha256DeterministicAndDifferent(t *testing.T) {
	key := []byte("TC3secret")
	r1 := hmacSha256(key, "2023-01-01")
	r2 := hmacSha256(key, "2023-01-01")
	for i := range r1 {
		if r1[i] != r2[i] {
			t.Error("hmacSha256() not deterministic")
			break
		}
	}

	r3 := hmacSha256(key, "2023-01-02")
	equal := true
	for i := range r1 {
		if r1[i] != r3[i] {
			equal = false
			break
		}
	}
	if equal {
		t.Error("different dates should produce different HMAC")
	}
}

// TestGetAPIVersion 测试根据服务获取 API 版本
func TestGetAPIVersion(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected string
	}{
		{"cdn service", "cdn", "2018-06-06"},
		{"ecdn service", "ecdn", "2022-09-01"},
		{"teo service", "teo", "2022-09-01"},
		{"unknown defaults to cdn", "dnspod", "2018-06-06"},
		{"empty defaults to cdn", "", "2018-06-06"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getAPIVersion(tt.service)
			if result != tt.expected {
				t.Errorf("getAPIVersion(%q) = %q, want %q", tt.service, result, tt.expected)
			}
		})
	}
}

// TestGetSignedHeaders 测试签名头列表
func TestGetSignedHeaders(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://dnspod.tencentcloudapi.com/", nil)
	result := getSignedHeaders(req)

	if result != "content-type;host" {
		t.Errorf("getSignedHeaders() = %q, want 'content-type;host'", result)
	}
}

// TestBuildCanonicalHeaders 测试规范请求头构建
func TestBuildCanonicalHeaders(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://dnspod.tencentcloudapi.com/", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", "dnspod.tencentcloudapi.com")

	result := buildCanonicalHeaders(req)

	if !strings.Contains(result, "content-type:application/json\n") {
		t.Errorf("buildCanonicalHeaders() missing content-type, got: %q", result)
	}
	if !strings.Contains(result, "host:dnspod.tencentcloudapi.com\n") {
		t.Errorf("buildCanonicalHeaders() missing host, got: %q", result)
	}
}

// TestBuildCanonicalHeadersUseReqHost 使用 req.Host 作为 fallback
func TestBuildCanonicalHeadersUseReqHost(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://dnspod.tencentcloudapi.com/", nil)
	req.Host = "dnspod.tencentcloudapi.com"
	req.Header.Set("Content-Type", "application/json")
	// 不设置 Header["Host"]，使用 req.Host fallback

	result := buildCanonicalHeaders(req)
	if !strings.Contains(result, "host:dnspod.tencentcloudapi.com\n") {
		t.Errorf("buildCanonicalHeaders() should fall back to req.Host, got: %q", result)
	}
}

// TestBuildCanonicalRequest 测试规范请求串构建
func TestBuildCanonicalRequest(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://dnspod.tencentcloudapi.com/", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", "dnspod.tencentcloudapi.com")
	payload := `{"Domain":"example.com","SubDomain":"www"}`

	result := buildCanonicalRequest(req, payload)
	lines := strings.Split(result, "\n")

	// 格式: method\nuri\nquery\nheader1\nheader2\n\nsignedHeaders\nhash
	// canonical headers 末尾带 \n，与 signedHeaders 拼接后多出一个空行
	if lines[0] != "POST" {
		t.Errorf("method = %q, want 'POST'", lines[0])
	}
	if lines[1] != "/" {
		t.Errorf("uri = %q, want '/'", lines[1])
	}
	if lines[2] != "" {
		t.Errorf("query = %q, want empty", lines[2])
	}
	// 最后一行为 hashed payload
	expectedHash := sha256Hex(payload)
	if lines[len(lines)-1] != expectedHash {
		t.Errorf("hashed payload = %q, want %q", lines[len(lines)-1], expectedHash)
	}
	// 倒数第二行为 signedHeaders
	if lines[len(lines)-2] != "content-type;host" {
		t.Errorf("signedHeaders = %q, want 'content-type;host'", lines[len(lines)-2])
	}
}

// TestTencentSigner 测试腾讯云签名器设置正确的请求头
func TestTencentSigner(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://dnspod.tencentcloudapi.com/", nil)
	req.Header.Set("X-TC-Action", "DescribeRecordList")

	payload := `{"Domain":"example.com","SubDomain":"www"}`
	TencentSigner("test-secret-id", "test-secret-key", "dnspod", "dnspod.tencentcloudapi.com", payload, req)

	// 验证 Authorization 头
	auth := req.Header.Get("Authorization")
	if auth == "" {
		t.Fatal("TencentSigner() did not set Authorization header")
	}
	if !strings.HasPrefix(auth, "TC3-HMAC-SHA256 ") {
		t.Errorf("Authorization = %q, should start with 'TC3-HMAC-SHA256 '", auth)
	}
	if !strings.Contains(auth, "Credential=test-secret-id/") {
		t.Errorf("Authorization = %q, should contain 'Credential=test-secret-id/'", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=content-type;host") {
		t.Errorf("Authorization = %q, should contain 'SignedHeaders=content-type;host'", auth)
	}
	if !strings.Contains(auth, "Signature=") {
		t.Errorf("Authorization = %q, should contain 'Signature='", auth)
	}

	// 验证 X-TC-Timestamp 头
	if req.Header.Get("X-TC-Timestamp") == "" {
		t.Error("TencentSigner() did not set X-TC-Timestamp header")
	}

	// 验证 Content-Type 头
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want 'application/json'", req.Header.Get("Content-Type"))
	}

	// 验证 Host 设置
	if req.Host != "dnspod.tencentcloudapi.com" {
		t.Errorf("req.Host = %q, want 'dnspod.tencentcloudapi.com'", req.Host)
	}
}

// TestTencentSignerCredentialScope 验证凭证范围格式
func TestTencentSignerCredentialScope(t *testing.T) {
	req, _ := http.NewRequest("POST", "https://dnspod.tencentcloudapi.com/", nil)
	TencentSigner("my-id", "my-key", "dnspod", "dnspod.tencentcloudapi.com", "{}", req)

	auth := req.Header.Get("Authorization")
	// CredentialScope 格式应为: date/service/tc3_request
	if !strings.Contains(auth, "/dnspod/tc3_request") {
		t.Errorf("Authorization = %q, should contain '/dnspod/tc3_request'", auth)
	}
}

// BenchmarkTencentSigner 性能测试
func BenchmarkTencentSigner(b *testing.B) {
	payload := `{"Domain":"example.com","SubDomain":"www","RecordType":"A","RecordLine":"默认","Value":"1.2.3.4","TTL":600}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "https://dnspod.tencentcloudapi.com/", nil)
		TencentSigner("secret-id", "secret-key", "dnspod", "dnspod.tencentcloudapi.com", payload, req)
	}
}
