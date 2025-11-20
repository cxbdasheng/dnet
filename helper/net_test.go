package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestIpv4Reg 测试 IPv4 正则表达式
func TestIpv4Reg(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		found    bool
	}{
		{"valid IPv4", "192.168.1.1", "192.168.1.1", true},
		{"IPv4 in text", "My IP is 10.0.0.1 today", "10.0.0.1", true},
		{"edge case 255", "255.255.255.255", "255.255.255.255", true},
		{"edge case 0", "0.0.0.0", "0.0.0.0", true},
		{"invalid IPv4", "256.1.1.1", "56.1.1.1", true}, // 正则会匹配部分内容
		{"not an IP", "hello world", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Ipv4Reg.FindString(tt.input)
			if tt.found && result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
			if !tt.found && result != "" {
				t.Errorf("expected no match, got %s", result)
			}
		})
	}
}

// TestIpv6Reg 测试 IPv6 正则表达式
func TestIpv6Reg(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		found    bool
	}{
		{"valid IPv6", "2001:0db8:85a3:0000:0000:8a2e:0370:7334", "2001:0db8:85a3:0000:0000:8a2e:0370:7334", true},
		{"compressed IPv6", "2001:db8::1", "2001:db8::1", true},
		{"loopback", "::1", "::1", true},
		{"IPv6 in text", "Address: fe80::1", "fe80::1", true},
		{"not an IPv6", "hello world", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Ipv6Reg.FindString(tt.input)
			if tt.found && result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
			if !tt.found && result != "" {
				t.Errorf("expected no match, got %s", result)
			}
		})
	}
}

// TestIsValidDomainName 测试域名验证
func TestIsValidDomainName(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected bool
	}{
		{"valid domain", "example.com", true},
		{"subdomain", "sub.example.com", true},
		{"with hyphen", "my-site.com", true},
		{"empty string", "", false},
		{"too long label", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.com", false},
		{"invalid char", "example$.com", false},
		{"starts with dot", ".example.com", false},
		{"ends with dot", "example.com.", false}, // 末尾点会导致空标签
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidDomainName(tt.domain)
			if result != tt.expected {
				t.Errorf("isValidDomainName(%s) = %v, expected %v", tt.domain, result, tt.expected)
			}
		})
	}
}

// TestIsValidDNSServer 测试 DNS 服务器地址验证
func TestIsValidDNSServer(t *testing.T) {
	tests := []struct {
		name     string
		dns      string
		expected bool
	}{
		{"valid IPv4", "8.8.8.8", true},
		{"valid IPv4 with port", "8.8.8.8:53", true},
		{"valid IPv6", "2001:4860:4860::8888", false}, // IPv6 地址需要用方括号括起来才能通过 SplitHostPort
		{"valid domain", "dns.google.com", true},
		{"invalid IP treated as domain", "256.1.1.1", true}, // ParseIP 失败后会当作域名检查
		{"invalid format", "not-a-dns", true},               // 域名格式有效
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidDNSServer(tt.dns)
			if result != tt.expected {
				t.Errorf("isValidDNSServer(%s) = %v, expected %v", tt.dns, result, tt.expected)
			}
		})
	}
}

// TestIsPrivateIP 测试私有 IP 检查
func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		expected bool
	}{
		{"private 192.168", "192.168.1.1", true},
		{"private 10", "10.0.0.1", true},
		{"private 172.16", "172.16.0.1", true},
		{"public IP", "8.8.8.8", false},
		{"public IP 2", "1.1.1.1", false},
		{"loopback", "127.0.0.1", false}, // IsPrivate() 不把 loopback 当作 private
		{"IPv6 private", "fd00::1", true},
		{"IPv6 loopback", "::1", false}, // IsPrivate() 不把 loopback 当作 private
		{"invalid IP", "not-an-ip", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPrivateIP(tt.ip)
			if result != tt.expected {
				t.Errorf("IsPrivateIP(%s) = %v, expected %v", tt.ip, result, tt.expected)
			}
		})
	}
}

// TestGetClientIP 测试客户端 IP 获取
func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name          string
		remoteAddr    string
		xForwardedFor string
		xRealIP       string
		expectedIP    string
	}{
		{
			name:       "direct connection",
			remoteAddr: "1.2.3.4:12345",
			expectedIP: "1.2.3.4",
		},
		{
			name:       "with X-Real-IP",
			remoteAddr: "192.168.1.1:12345",
			xRealIP:    "5.6.7.8",
			expectedIP: "5.6.7.8",
		},
		{
			name:          "with X-Forwarded-For single",
			remoteAddr:    "192.168.1.1:12345",
			xForwardedFor: "9.10.11.12",
			expectedIP:    "9.10.11.12",
		},
		{
			name:          "with X-Forwarded-For multiple",
			remoteAddr:    "192.168.1.1:12345",
			xForwardedFor: "13.14.15.16, 192.168.1.2, 10.0.0.1",
			expectedIP:    "13.14.15.16",
		},
		{
			name:          "X-Forwarded-For all private",
			remoteAddr:    "192.168.1.1:12345",
			xForwardedFor: "192.168.1.2, 10.0.0.1",
			expectedIP:    "192.168.1.2",
		},
		{
			name:       "IPv6 address",
			remoteAddr: "[2001:db8::1]:12345",
			expectedIP: "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &http.Request{
				RemoteAddr: tt.remoteAddr,
				Header:     http.Header{},
			}
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}

			result := GetClientIP(req)
			if result != tt.expectedIP {
				t.Errorf("GetClientIP() = %s, expected %s", result, tt.expectedIP)
			}
		})
	}
}

// TestGetAddrFromUrl 测试从 URL 获取地址
func TestGetAddrFromUrl(t *testing.T) {
	// 创建 IPv4 测试服务器
	server4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("Your IP is 203.0.113.1"))
	}))
	defer server4.Close()

	// 创建返回无效内容的服务器
	serverInvalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("No IP address here"))
	}))
	defer serverInvalid.Close()

	tests := []struct {
		name           string
		urlsStr        string
		addrType       string
		expectNonEmpty bool
	}{
		{
			name:           "IPv4 from URL",
			urlsStr:        server4.URL,
			addrType:       IPv4,
			expectNonEmpty: true,
		},
		{
			name:           "multiple URLs with fallback",
			urlsStr:        "http://invalid-url-12345.com," + server4.URL,
			addrType:       IPv4,
			expectNonEmpty: true,
		},
		{
			name:           "invalid URL returns empty",
			urlsStr:        serverInvalid.URL,
			addrType:       IPv4,
			expectNonEmpty: false,
		},
		{
			name:           "empty URL string",
			urlsStr:        "",
			addrType:       IPv4,
			expectNonEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetAddrFromUrl(tt.urlsStr, tt.addrType)
			if tt.expectNonEmpty && result == "" {
				t.Errorf("GetAddrFromUrl() expected non-empty result, got empty")
			}
			if !tt.expectNonEmpty && result != "" {
				t.Errorf("GetAddrFromUrl() expected empty result, got %s", result)
			}

			// 验证返回的是正确类型的地址
			if result != "" {
				if tt.addrType == IPv4 && !Ipv4Reg.MatchString(result) {
					t.Errorf("expected IPv4 address, got %s", result)
				}
				if tt.addrType == IPv6 && !Ipv6Reg.MatchString(result) {
					t.Errorf("expected IPv6 address, got %s", result)
				}
			}
		})
	}
}

// TestGetAddrFromCmd 测试从命令获取地址
func TestGetAddrFromCmd(t *testing.T) {
	tests := []struct {
		name           string
		cmd            string
		addrType       string
		expectNonEmpty bool
	}{
		{
			name:           "empty command",
			cmd:            "",
			addrType:       IPv4,
			expectNonEmpty: false,
		},
		{
			name:           "echo IPv4",
			cmd:            "echo 192.168.1.1",
			addrType:       IPv4,
			expectNonEmpty: true,
		},
		{
			name:           "echo IPv6",
			cmd:            "echo 2001:db8::1",
			addrType:       IPv6,
			expectNonEmpty: true,
		},
		{
			name:           "command with no IP",
			cmd:            "echo hello",
			addrType:       IPv4,
			expectNonEmpty: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetAddrFromCmd(tt.cmd, tt.addrType)
			if tt.expectNonEmpty && result == "" {
				t.Errorf("GetAddrFromCmd() expected non-empty result, got empty")
			}
			if !tt.expectNonEmpty && result != "" {
				t.Errorf("GetAddrFromCmd() expected empty result, got %s", result)
			}
		})
	}
}

// TestCreateHTTPClient 测试创建 HTTP 客户端
func TestCreateHTTPClient(t *testing.T) {
	client := CreateHTTPClient()

	if client == nil {
		t.Fatal("CreateHTTPClient() returned nil")
	}

	if client.Timeout != httpClientTimeout {
		t.Errorf("client timeout = %v, expected %v", client.Timeout, httpClientTimeout)
	}

	if client.Transport != defaultTransport {
		t.Errorf("client transport != defaultTransport")
	}
}

// TestCreateNoProxyHTTPClient 测试创建无代理 HTTP 客户端
func TestCreateNoProxyHTTPClient(t *testing.T) {
	tests := []struct {
		name     string
		network  string
		expected *http.Transport
	}{
		{
			name:     "tcp4 client",
			network:  "tcp4",
			expected: noProxyTcp4Transport,
		},
		{
			name:     "tcp6 client",
			network:  "tcp6",
			expected: noProxyTcp6Transport,
		},
		{
			name:     "default to tcp4",
			network:  "tcp",
			expected: noProxyTcp4Transport,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := CreateNoProxyHTTPClient(tt.network)

			if client == nil {
				t.Fatal("CreateNoProxyHTTPClient() returned nil")
			}

			if client.Timeout != httpClientTimeout {
				t.Errorf("client timeout = %v, expected %v", client.Timeout, httpClientTimeout)
			}

			if client.Transport != tt.expected {
				t.Errorf("client transport doesn't match expected transport")
			}
		})
	}
}

// TestGetHTTPResponse 测试 HTTP 响应处理
func TestGetHTTPResponse(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		body        string
		expectError bool
	}{
		{
			name:        "success 200",
			statusCode:  200,
			body:        `{"status":"ok"}`,
			expectError: false,
		},
		{
			name:        "error 400",
			statusCode:  400,
			body:        `{"error":"bad request"}`,
			expectError: true,
		},
		{
			name:        "error 500",
			statusCode:  500,
			body:        `{"error":"server error"}`,
			expectError: true,
		},
		{
			name:        "empty body 200",
			statusCode:  200,
			body:        "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := CreateHTTPClient()
			resp, err := client.Get(server.URL)

			var result map[string]interface{}
			err = GetHTTPResponse(resp, err, &result)

			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestGetHTTPResponseOrg 测试原始 HTTP 响应处理
func TestGetHTTPResponseOrg(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		body         string
		expectError  bool
		expectedBody string
	}{
		{
			name:         "success 200",
			statusCode:   200,
			body:         "response body",
			expectError:  false,
			expectedBody: "response body",
		},
		{
			name:         "error 404",
			statusCode:   404,
			body:         "not found",
			expectError:  true,
			expectedBody: "not found",
		},
		{
			name:         "redirect 301",
			statusCode:   301,
			body:         "moved",
			expectError:  true,
			expectedBody: "moved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := CreateHTTPClient()
			resp, err := client.Get(server.URL)

			body, err := GetHTTPResponseOrg(resp, err)

			if tt.expectError && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
			if string(body) != tt.expectedBody {
				t.Errorf("body = %s, expected %s", string(body), tt.expectedBody)
			}
		})
	}
}

// TestCreateNoProxyTransport 测试创建无代理传输
func TestCreateNoProxyTransport(t *testing.T) {
	tests := []struct {
		name    string
		network string
	}{
		{"tcp4 transport", "tcp4"},
		{"tcp6 transport", "tcp6"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := createNoProxyTransport(tt.network)

			if transport == nil {
				t.Fatal("createNoProxyTransport() returned nil")
			}

			if !transport.DisableKeepAlives {
				t.Error("expected DisableKeepAlives to be true")
			}

			if transport.IdleConnTimeout != idleConnTimeout {
				t.Errorf("IdleConnTimeout = %v, expected %v", transport.IdleConnTimeout, idleConnTimeout)
			}

			if transport.TLSHandshakeTimeout != tlsHandshakeTimeout {
				t.Errorf("TLSHandshakeTimeout = %v, expected %v", transport.TLSHandshakeTimeout, tlsHandshakeTimeout)
			}

			// 测试 DialContext 是否使用正确的网络
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			// 使用一个已知存在的地址进行测试（这里使用 localhost）
			// 注意：这个测试可能在某些环境下失败，因为它依赖于网络配置
			_, err := transport.DialContext(ctx, "", "localhost:80")
			// 我们不关心连接是否成功，只关心没有 panic
			_ = err
		})
	}
}

// TestConstants 测试常量定义
func TestConstants(t *testing.T) {
	if httpClientTimeout != 30*time.Second {
		t.Errorf("httpClientTimeout = %v, expected 30s", httpClientTimeout)
	}

	if dialerTimeout != 30*time.Second {
		t.Errorf("dialerTimeout = %v, expected 30s", dialerTimeout)
	}

	if dialerKeepAlive != 30*time.Second {
		t.Errorf("dialerKeepAlive = %v, expected 30s", dialerKeepAlive)
	}

	if idleConnTimeout != 90*time.Second {
		t.Errorf("idleConnTimeout = %v, expected 90s", idleConnTimeout)
	}

	if tlsHandshakeTimeout != 10*time.Second {
		t.Errorf("tlsHandshakeTimeout = %v, expected 10s", tlsHandshakeTimeout)
	}

	if expectContinueTimeout != 1*time.Second {
		t.Errorf("expectContinueTimeout = %v, expected 1s", expectContinueTimeout)
	}
}

// TestDialer 测试全局 dialer 配置
func TestDialer(t *testing.T) {
	if dialer.Timeout != dialerTimeout {
		t.Errorf("dialer.Timeout = %v, expected %v", dialer.Timeout, dialerTimeout)
	}

	if dialer.KeepAlive != dialerKeepAlive {
		t.Errorf("dialer.KeepAlive = %v, expected %v", dialer.KeepAlive, dialerKeepAlive)
	}
}

// BenchmarkGetClientIP 性能测试：获取客户端 IP
func BenchmarkGetClientIP(b *testing.B) {
	req := &http.Request{
		RemoteAddr: "1.2.3.4:12345",
		Header:     http.Header{},
	}
	req.Header.Set("X-Forwarded-For", "5.6.7.8, 192.168.1.1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GetClientIP(req)
	}
}

// BenchmarkIpv4Reg 性能测试：IPv4 正则匹配
func BenchmarkIpv4Reg(b *testing.B) {
	text := "The server IP is 192.168.1.100 and backup is 10.0.0.50"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Ipv4Reg.FindString(text)
	}
}

// BenchmarkIpv6Reg 性能测试：IPv6 正则匹配
func BenchmarkIpv6Reg(b *testing.B) {
	text := "IPv6 address: 2001:0db8:85a3:0000:0000:8a2e:0370:7334"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Ipv6Reg.FindString(text)
	}
}

// BenchmarkCreateHTTPClient 性能测试：创建 HTTP 客户端
func BenchmarkCreateHTTPClient(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CreateHTTPClient()
	}
}

// BenchmarkCreateNoProxyHTTPClient 性能测试：创建无代理 HTTP 客户端
func BenchmarkCreateNoProxyHTTPClient(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CreateNoProxyHTTPClient("tcp4")
	}
}
