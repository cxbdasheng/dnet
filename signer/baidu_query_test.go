package signer

import (
	"net/http"
	"testing"
)

// TestBaiduCanonicalQueryString 测试百度云规范查询字符串构建
func TestBaiduCanonicalQueryString(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "empty query string",
			url:      "https://cdn.baidubce.com/v2/domain/test.com/config",
			expected: "",
		},
		{
			name:     "single parameter",
			url:      "https://cdn.baidubce.com/v2/domain?action=query",
			expected: "action=query",
		},
		{
			name:     "multiple parameters",
			url:      "https://cdn.baidubce.com/v2/domain?type=cdn&action=query",
			expected: "action=query&type=cdn", // 字典序排序
		},
		{
			name:     "parameters with special characters",
			url:      "https://cdn.baidubce.com/v2/domain?name=test value&key=abc+def",
			expected: "key=abc+def&name=test+value", // URI 编码：空格变为+，+保持为+
		},
		{
			name:     "parameters need sorting",
			url:      "https://cdn.baidubce.com/v2/domain?z=last&a=first&m=middle",
			expected: "a=first&m=middle&z=last",
		},
		{
			name:     "parameter with multiple values",
			url:      "https://cdn.baidubce.com/v2/domain?tag=tag2&tag=tag1&name=test",
			expected: "name=test&tag=tag1&tag=tag2", // 同名参数的值也要排序
		},
		{
			name:     "empty parameter value",
			url:      "https://cdn.baidubce.com/v2/domain?key=&name=test",
			expected: "key=&name=test",
		},
		{
			name:     "chinese characters",
			url:      "https://cdn.baidubce.com/v2/domain?name=测试",
			expected: "name=%E6%B5%8B%E8%AF%95", // 中文 URI 编码
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("GET", tt.url, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			result := BaiduCanonicalQueryString(req)
			if result != tt.expected {
				t.Errorf("BaiduCanonicalQueryString() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestBaiduSignerWithQueryString 测试带查询参数的签名
func TestBaiduSignerWithQueryString(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		method      string
		accessKeyID string
		accessKey   string
	}{
		{
			name:        "GET request with query parameters",
			url:         "https://cdn.baidubce.com/v2/domain/test.com/config?action=query&type=cdn",
			method:      "GET",
			accessKeyID: "test_ak",
			accessKey:   "test_sk",
		},
		{
			name:        "PUT request without query parameters",
			url:         "https://cdn.baidubce.com/v2/domain/test.com/config",
			method:      "PUT",
			accessKeyID: "test_ak",
			accessKey:   "test_sk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, tt.url, nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			req.Host = "cdn.baidubce.com"

			// 调用签名函数
			BaiduSigner(tt.accessKeyID, tt.accessKey, req)

			// 验证 Authorization header 已被设置
			authHeader := req.Header.Get("Authorization")
			if authHeader == "" {
				t.Error("Authorization header not set")
			}

			// 验证 Authorization header 格式
			if len(authHeader) < 10 {
				t.Errorf("Authorization header too short: %s", authHeader)
			}

			// 验证包含 bce-auth-v1 前缀
			if !contains(authHeader, "bce-auth-v1") {
				t.Errorf("Authorization header missing bce-auth-v1 prefix: %s", authHeader)
			}

			// 验证包含 accessKeyID
			if !contains(authHeader, tt.accessKeyID) {
				t.Errorf("Authorization header missing accessKeyID: %s", authHeader)
			}
		})
	}
}

// 辅助函数：检查字符串包含
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			len(s) > len(substr)+1 && containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
