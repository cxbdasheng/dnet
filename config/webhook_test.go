package config

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestHasJSONPrefix 测试 hasJSONPrefix 函数
func TestHasJSONPrefix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "JSON对象",
			input: `{"key": "value"}`,
			want:  true,
		},
		{
			name:  "JSON数组",
			input: `["item1", "item2"]`,
			want:  true,
		},
		{
			name:  "空JSON对象",
			input: `{}`,
			want:  true,
		},
		{
			name:  "空JSON数组",
			input: `[]`,
			want:  true,
		},
		{
			name:  "普通字符串",
			input: `plain text`,
			want:  false,
		},
		{
			name:  "空字符串",
			input: "",
			want:  false,
		},
		{
			name:  "以空格开头的JSON",
			input: " {\"key\": \"value\"}",
			want:  false,
		},
		{
			name:  "包含JSON但不是前缀",
			input: "text {\"key\": \"value\"}",
			want:  false,
		},
		{
			name:  "只有左花括号",
			input: "{",
			want:  true,
		},
		{
			name:  "只有左方括号",
			input: "[",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasJSONPrefix(tt.input)
			if got != tt.want {
				t.Errorf("hasJSONPrefix(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// TestExtractHeaders 测试 extractHeaders 函数
func TestExtractHeaders(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name:  "单个Header",
			input: "Content-Type: application/json",
			want: map[string]string{
				"Content-Type": "application/json",
			},
		},
		{
			name: "多个Headers",
			input: `Content-Type: application/json
Authorization: Bearer token123
X-Custom-Header: custom-value`,
			want: map[string]string{
				"Content-Type":    "application/json",
				"Authorization":   "Bearer token123",
				"X-Custom-Header": "custom-value",
			},
		},
		{
			name:  "空字符串",
			input: "",
			want:  map[string]string{},
		},
		{
			name:  "只有空白字符",
			input: "   \n\t  ",
			want:  map[string]string{},
		},
		{
			name: "包含空行",
			input: `Content-Type: application/json

Authorization: Bearer token123`,
			want: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer token123",
			},
		},
		{
			name: "带有额外空白的Headers",
			input: `  Content-Type  :  application/json
  Authorization  :  Bearer token123  `,
			want: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer token123",
			},
		},
		{
			name: "格式错误的Header（无冒号）",
			input: `Content-Type application/json
Authorization: Bearer token123`,
			want: map[string]string{
				"Authorization": "Bearer token123",
			},
		},
		{
			name:  "格式错误的Header（多个冒号）",
			input: "Content-Type: application/json: extra",
			want:  map[string]string{},
		},
		{
			name: "混合正确和错误的Headers",
			input: `Content-Type: application/json
InvalidHeader
Authorization: Bearer token123`,
			want: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer token123",
			},
		},
		{
			name: "Header值包含冒号",
			input: `X-Time: 2024:01:01:12:00:00
Authorization: Bearer token123`,
			want: map[string]string{
				"Authorization": "Bearer token123",
			},
		},
		{
			name:  "空的Header键或值",
			input: ": value\nkey: ",
			want: map[string]string{
				"":    "value",
				"key": "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHeaders(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractHeaders() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestReplacePara 测试 replacePara 函数
func TestReplacePara(t *testing.T) {
	tests := []struct {
		name          string
		orgPara       string
		serviceType   string
		serviceName   string
		serviceStatus string
		want          string
	}{
		{
			name:          "替换所有占位符",
			orgPara:       "Type: #{serviceType}, Name: #{serviceName}, Status: #{serviceStatus}",
			serviceType:   "DCDN",
			serviceName:   "example.com",
			serviceStatus: "success",
			want:          "Type: DCDN, Name: example.com, Status: success",
		},
		{
			name:          "只有一个占位符",
			orgPara:       "Service type is #{serviceType}",
			serviceType:   "CDN",
			serviceName:   "test.com",
			serviceStatus: "failed",
			want:          "Service type is CDN",
		},
		{
			name:          "无占位符",
			orgPara:       "This is a plain text",
			serviceType:   "DCDN",
			serviceName:   "example.com",
			serviceStatus: "success",
			want:          "This is a plain text",
		},
		{
			name:          "空字符串",
			orgPara:       "",
			serviceType:   "DCDN",
			serviceName:   "example.com",
			serviceStatus: "success",
			want:          "",
		},
		{
			name:          "重复的占位符",
			orgPara:       "#{serviceType} - #{serviceType} - #{serviceName}",
			serviceType:   "CDN",
			serviceName:   "test.com",
			serviceStatus: "success",
			want:          "CDN - CDN - test.com",
		},
		{
			name:          "JSON格式的请求体",
			orgPara:       `{"type": "#{serviceType}", "name": "#{serviceName}", "status": "#{serviceStatus}"}`,
			serviceType:   "DCDN",
			serviceName:   "example.com",
			serviceStatus: "success",
			want:          `{"type": "DCDN", "name": "example.com", "status": "success"}`,
		},
		{
			name:          "URL查询参数",
			orgPara:       "https://example.com/webhook?type=#{serviceType}&name=#{serviceName}&status=#{serviceStatus}",
			serviceType:   "CDN",
			serviceName:   "test.com",
			serviceStatus: "failed",
			want:          "https://example.com/webhook?type=CDN&name=test.com&status=failed",
		},
		{
			name:          "包含特殊字符的参数值",
			orgPara:       "Name: #{serviceName}",
			serviceType:   "DCDN",
			serviceName:   "例子.com",
			serviceStatus: "成功",
			want:          "Name: 例子.com",
		},
		{
			name:          "部分占位符不完整",
			orgPara:       "#{serviceType} - #serviceName} - {serviceStatus}",
			serviceType:   "CDN",
			serviceName:   "test.com",
			serviceStatus: "success",
			want:          "CDN - #serviceName} - {serviceStatus}",
		},
		{
			name:          "空参数值",
			orgPara:       "Type: #{serviceType}, Name: #{serviceName}",
			serviceType:   "",
			serviceName:   "",
			serviceStatus: "",
			want:          "Type: , Name: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := replacePara(tt.orgPara, tt.serviceType, tt.serviceName, tt.serviceStatus)
			if got != tt.want {
				t.Errorf("replacePara() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestExecWebhook 测试 ExecWebhook 函数
func TestExecWebhook(t *testing.T) {
	tests := []struct {
		name          string
		conf          *Webhook
		serviceType   string
		serviceName   string
		serviceStatus string
		setupServer   func() *httptest.Server
		want          bool
	}{
		{
			name: "成功的GET请求",
			conf: &Webhook{
				WebhookEnabled: true,
				WebhookURL:     "", // 将在 setupServer 中设置
			},
			serviceType:   "DCDN",
			serviceName:   "example.com",
			serviceStatus: "success",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						t.Errorf("Expected GET request, got %s", r.Method)
					}
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("OK"))
				}))
			},
			want: true,
		},
		{
			name: "成功的POST请求（JSON）",
			conf: &Webhook{
				WebhookEnabled:     true,
				WebhookURL:         "", // 将在 setupServer 中设置
				WebhookRequestBody: `{"type": "#{serviceType}", "name": "#{serviceName}", "status": "#{serviceStatus}"}`,
			},
			serviceType:   "DCDN",
			serviceName:   "test.com",
			serviceStatus: "success",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost {
						t.Errorf("Expected POST request, got %s", r.Method)
					}
					contentType := r.Header.Get("Content-Type")
					if contentType != "application/json" {
						t.Errorf("Expected Content-Type: application/json, got %s", contentType)
					}
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("OK"))
				}))
			},
			want: true,
		},
		{
			name: "成功的POST请求（表单）",
			conf: &Webhook{
				WebhookEnabled:     true,
				WebhookURL:         "", // 将在 setupServer 中设置
				WebhookRequestBody: "type=#{serviceType}&name=#{serviceName}",
			},
			serviceType:   "CDN",
			serviceName:   "example.com",
			serviceStatus: "failed",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost {
						t.Errorf("Expected POST request, got %s", r.Method)
					}
					contentType := r.Header.Get("Content-Type")
					if contentType != "application/x-www-form-urlencoded" {
						t.Errorf("Expected Content-Type: application/x-www-form-urlencoded, got %s", contentType)
					}
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("OK"))
				}))
			},
			want: true,
		},
		{
			name: "带自定义Headers",
			conf: &Webhook{
				WebhookEnabled: true,
				WebhookURL:     "", // 将在 setupServer 中设置
				WebhookHeaders: "Authorization: Bearer token123\nX-Custom-Header: custom-value",
			},
			serviceType:   "DCDN",
			serviceName:   "test.com",
			serviceStatus: "success",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					auth := r.Header.Get("Authorization")
					if auth != "Bearer token123" {
						t.Errorf("Expected Authorization: Bearer token123, got %s", auth)
					}
					custom := r.Header.Get("X-Custom-Header")
					if custom != "custom-value" {
						t.Errorf("Expected X-Custom-Header: custom-value, got %s", custom)
					}
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("OK"))
				}))
			},
			want: true,
		},
		{
			name: "空的WebhookURL",
			conf: &Webhook{
				WebhookEnabled: true,
				WebhookURL:     "",
			},
			serviceType:   "DCDN",
			serviceName:   "test.com",
			serviceStatus: "success",
			setupServer: func() *httptest.Server {
				return nil
			},
			want: false,
		},
		{
			name: "服务器返回错误",
			conf: &Webhook{
				WebhookEnabled: true,
				WebhookURL:     "", // 将在 setupServer 中设置
			},
			serviceType:   "DCDN",
			serviceName:   "test.com",
			serviceStatus: "success",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte("Internal Server Error"))
				}))
			},
			want: false,
		},
		{
			name: "URL包含占位符",
			conf: &Webhook{
				WebhookEnabled: true,
				WebhookURL:     "", // 将在 setupServer 中设置，然后添加查询参数
			},
			serviceType:   "DCDN",
			serviceName:   "example.com",
			serviceStatus: "success",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					// 验证查询参数
					if r.URL.Query().Get("type") != "DCDN" {
						t.Errorf("Expected type=DCDN, got %s", r.URL.Query().Get("type"))
					}
					if r.URL.Query().Get("name") != "example.com" {
						t.Errorf("Expected name=example.com, got %s", r.URL.Query().Get("name"))
					}
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("OK"))
				}))
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 设置测试服务器
			server := tt.setupServer()
			if server != nil {
				defer server.Close()

				// 更新 URL
				if tt.conf.WebhookURL == "" {
					if tt.name == "URL包含占位符" {
						tt.conf.WebhookURL = server.URL + "?type=#{serviceType}&name=#{serviceName}"
					} else if tt.name != "空的WebhookURL" {
						tt.conf.WebhookURL = server.URL
					}
				}
			}

			got := ExecWebhook(tt.conf, tt.serviceType, tt.serviceName, tt.serviceStatus)
			if got != tt.want {
				t.Errorf("ExecWebhook() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestExecWebhook_InvalidURL 测试无效URL的情况
func TestExecWebhook_InvalidURL(t *testing.T) {
	conf := &Webhook{
		WebhookEnabled: true,
		WebhookURL:     "ht!tp://invalid url with spaces",
	}

	got := ExecWebhook(conf, "DCDN", "test.com", "success")
	if got != false {
		t.Errorf("ExecWebhook() with invalid URL should return false, got %v", got)
	}
}

// TestExecWebhook_InvalidJSON 测试无效JSON的情况
func TestExecWebhook_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))
	defer server.Close()

	conf := &Webhook{
		WebhookEnabled:     true,
		WebhookURL:         server.URL,
		WebhookRequestBody: `{"invalid json`,
	}

	// 这应该仍然发送请求，但会记录JSON无效的日志
	got := ExecWebhook(conf, "DCDN", "test.com", "success")
	// 由于服务器返回200，所以应该返回true
	if got != true {
		t.Errorf("ExecWebhook() with invalid JSON should still succeed if server responds OK, got %v", got)
	}
}
