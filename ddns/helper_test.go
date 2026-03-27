package ddns

import (
	"testing"

	"github.com/cxbdasheng/dnet/config"
)

// TestGetRootDomain 测试根域名提取
func TestGetRootDomain(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected string
	}{
		{
			name:     "二级域名原样返回",
			domain:   "example.com",
			expected: "example.com",
		},
		{
			name:     "三级域名",
			domain:   "www.example.com",
			expected: "example.com",
		},
		{
			name:     "四级域名",
			domain:   "cdn.sub.example.com",
			expected: "example.com",
		},
		{
			name:     "通配符域名",
			domain:   "*.example.com",
			expected: "example.com",
		},
		{
			name:     "单段域名原样返回",
			domain:   "localhost",
			expected: "localhost",
		},
		{
			name:     "带连字符的域名",
			domain:   "my-sub.example-site.io",
			expected: "example-site.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getRootDomain(tt.domain)
			if got != tt.expected {
				t.Errorf("getRootDomain(%q) = %q, want %q", tt.domain, got, tt.expected)
			}
		})
	}
}

// TestGetHostRecord 测试主机记录提取
func TestGetHostRecord(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected string
	}{
		{
			name:     "二级域名返回 @",
			domain:   "example.com",
			expected: "@",
		},
		{
			name:     "www 子域名",
			domain:   "www.example.com",
			expected: "www",
		},
		{
			name:     "多级子域名",
			domain:   "cdn.sub.example.com",
			expected: "cdn.sub",
		},
		{
			name:     "单段域名返回 @",
			domain:   "localhost",
			expected: "@",
		},
		{
			name:     "四级域名",
			domain:   "a.b.c.example.com",
			expected: "a.b.c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getHostRecord(tt.domain)
			if got != tt.expected {
				t.Errorf("getHostRecord(%q) = %q, want %q", tt.domain, got, tt.expected)
			}
		})
	}
}

// TestFilterValidRecords 测试过滤有效记录
func TestFilterValidRecords(t *testing.T) {
	t.Run("所有记录有效", func(t *testing.T) {
		group := &config.DNSGroup{
			Records: []config.DNSRecord{
				{Type: "A", Value: "1.2.3.4"},
				{Type: "AAAA", Value: "::1"},
			},
		}
		cache1, cache2 := NewCache(), NewCache()
		caches := []*Cache{&cache1, &cache2}

		valid := filterValidRecords(group, caches)
		if len(valid) != 2 {
			t.Errorf("len = %d, want 2", len(valid))
		}
	})

	t.Run("过滤 Value 为空的记录", func(t *testing.T) {
		group := &config.DNSGroup{
			Records: []config.DNSRecord{
				{Type: "A", Value: "1.2.3.4"},
				{Type: "AAAA", Value: ""},
				{Type: "TXT", Value: "some-text"},
			},
		}
		cache1, cache2 := NewCache(), NewCache()
		caches := []*Cache{&cache1, &cache2}

		valid := filterValidRecords(group, caches)
		if len(valid) != 2 {
			t.Errorf("len = %d, want 2", len(valid))
		}
		if valid[0].record.Type != "A" {
			t.Errorf("第一条有效记录类型应为 A, got %q", valid[0].record.Type)
		}
		if valid[1].record.Type != "TXT" {
			t.Errorf("第二条有效记录类型应为 TXT, got %q", valid[1].record.Type)
		}
	})

	t.Run("所有记录 Value 为空", func(t *testing.T) {
		group := &config.DNSGroup{
			Records: []config.DNSRecord{
				{Type: "A", Value: ""},
				{Type: "AAAA", Value: ""},
			},
		}
		valid := filterValidRecords(group, []*Cache{})
		if len(valid) != 0 {
			t.Errorf("len = %d, want 0", len(valid))
		}
	})

	t.Run("空记录列表", func(t *testing.T) {
		group := &config.DNSGroup{Records: []config.DNSRecord{}}
		valid := filterValidRecords(group, []*Cache{})
		if len(valid) != 0 {
			t.Errorf("len = %d, want 0", len(valid))
		}
	})

	t.Run("有效记录与缓存正确关联", func(t *testing.T) {
		group := &config.DNSGroup{
			Records: []config.DNSRecord{
				{Type: "A", Value: "1.2.3.4"},
				{Type: "AAAA", Value: ""}, // 无效，跳过
				{Type: "TXT", Value: "v=spf1"},
			},
		}
		cache1, cache2 := NewCache(), NewCache()
		cache1.HasRun = true // 标记以区分
		caches := []*Cache{&cache1, &cache2}

		valid := filterValidRecords(group, caches)
		if len(valid) != 2 {
			t.Fatalf("len = %d, want 2", len(valid))
		}
		// 第一条有效记录关联 cache1
		if !valid[0].cache.HasRun {
			t.Error("第一条有效记录应关联 cache1 (HasRun=true)")
		}
		// 第二条有效记录关联 cache2
		if valid[1].cache.HasRun {
			t.Error("第二条有效记录应关联 cache2 (HasRun=false)")
		}
	})
}

// TestCreateErrorResults 测试批量生成错误结果
func TestCreateErrorResults(t *testing.T) {
	t.Run("为所有有效记录生成错误结果", func(t *testing.T) {
		group := &config.DNSGroup{
			Records: []config.DNSRecord{
				{Type: "A", Value: "1.2.3.4"},
				{Type: "AAAA", Value: "::1"},
			},
		}
		cache1, cache2 := NewCache(), NewCache()
		validRecords := filterValidRecords(group, []*Cache{&cache1, &cache2})

		results := createErrorResults(validRecords, InitFailed, "初始化失败")

		if len(results) != 2 {
			t.Fatalf("len = %d, want 2", len(results))
		}
		for i, r := range results {
			if r.Status != InitFailed {
				t.Errorf("results[%d].Status = %q, want %q", i, r.Status, InitFailed)
			}
			if r.ErrorMessage != "初始化失败" {
				t.Errorf("results[%d].ErrorMessage = %q, want '初始化失败'", i, r.ErrorMessage)
			}
			if r.ShouldWebhook {
				t.Errorf("results[%d].ShouldWebhook 应为 false", i)
			}
		}
		if results[0].RecordType != "A" {
			t.Errorf("results[0].RecordType = %q, want A", results[0].RecordType)
		}
		if results[1].RecordType != "AAAA" {
			t.Errorf("results[1].RecordType = %q, want AAAA", results[1].RecordType)
		}
	})

	t.Run("空有效记录列表返回空结果", func(t *testing.T) {
		results := createErrorResults([]validRecord{}, InitFailed, "error")
		if len(results) != 0 {
			t.Errorf("len = %d, want 0", len(results))
		}
	})
}

// TestBaseDNSProviderGetServiceName 测试获取服务名称
func TestBaseDNSProviderGetServiceName(t *testing.T) {
	tests := []struct {
		name     string
		group    *config.DNSGroup
		expected string
	}{
		{
			name:     "Group 为 nil 时返回空字符串",
			group:    nil,
			expected: "",
		},
		{
			name: "有 Name 时返回 Name",
			group: &config.DNSGroup{
				Name:   "我的DNS",
				Domain: "example.com",
			},
			expected: "我的DNS",
		},
		{
			name: "Name 为空时返回 Domain",
			group: &config.DNSGroup{
				Name:   "",
				Domain: "example.com",
			},
			expected: "example.com",
		},
		{
			name: "Name 和 Domain 均为空",
			group: &config.DNSGroup{
				Name:   "",
				Domain: "",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &BaseDNSProvider{Group: tt.group}
			got := b.GetServiceName()
			if got != tt.expected {
				t.Errorf("GetServiceName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestBaseDNSProviderInitConfig 测试 initConfig 配置初始化校验
func TestBaseDNSProviderInitConfig(t *testing.T) {
	t.Run("配置完整时返回 true", func(t *testing.T) {
		group := &config.DNSGroup{
			Domain:       "example.com",
			AccessKey:    "mykey",
			AccessSecret: "mysecret",
		}
		cache := NewCache()
		b := &BaseDNSProvider{}

		ok := b.initConfig(group, []*Cache{&cache})
		if !ok {
			t.Error("配置完整时应返回 true")
		}
		if b.Group != group {
			t.Error("initConfig 应设置 Group 字段")
		}
	})

	t.Run("Domain 为空时返回 false", func(t *testing.T) {
		group := &config.DNSGroup{
			Domain:       "",
			AccessKey:    "mykey",
			AccessSecret: "mysecret",
		}
		b := &BaseDNSProvider{}
		ok := b.initConfig(group, []*Cache{})
		if ok {
			t.Error("Domain 为空时应返回 false")
		}
	})

	t.Run("AccessKey 为空时返回 false", func(t *testing.T) {
		group := &config.DNSGroup{
			Domain:       "example.com",
			AccessKey:    "",
			AccessSecret: "mysecret",
		}
		b := &BaseDNSProvider{}
		ok := b.initConfig(group, []*Cache{})
		if ok {
			t.Error("AccessKey 为空时应返回 false")
		}
	})

	t.Run("AccessSecret 为空时返回 false", func(t *testing.T) {
		group := &config.DNSGroup{
			Domain:       "example.com",
			AccessKey:    "mykey",
			AccessSecret: "",
		}
		b := &BaseDNSProvider{}
		ok := b.initConfig(group, []*Cache{})
		if ok {
			t.Error("AccessSecret 为空时应返回 false")
		}
	})
}
