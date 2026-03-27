package dcdn

import (
	"testing"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
)

// TestBaseProviderGetServiceName 测试获取服务名称
func TestBaseProviderGetServiceName(t *testing.T) {
	tests := []struct {
		name     string
		cdn      *config.CDN
		expected string
	}{
		{
			name:     "CDN 为 nil 时返回空字符串",
			cdn:      nil,
			expected: "",
		},
		{
			name: "有 Name 时返回 Name",
			cdn: &config.CDN{
				Name:   "我的CDN",
				Domain: "example.com",
			},
			expected: "我的CDN",
		},
		{
			name: "Name 为空时返回 Domain",
			cdn: &config.CDN{
				Name:   "",
				Domain: "example.com",
			},
			expected: "example.com",
		},
		{
			name: "Name 和 Domain 都为空时返回空字符串",
			cdn: &config.CDN{
				Name:   "",
				Domain: "",
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &BaseProvider{CDN: tt.cdn}
			got := b.GetServiceName()
			if got != tt.expected {
				t.Errorf("GetServiceName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestBaseProviderGetServiceStatus 测试获取服务状态
func TestBaseProviderGetServiceStatus(t *testing.T) {
	statuses := []statusType{
		InitFailed,
		InitSuccess,
		InitGetIPFailed,
		UpdatedNothing,
		UpdatedFailed,
		UpdatedSuccess,
	}

	for _, status := range statuses {
		b := &BaseProvider{Status: status}
		got := b.GetServiceStatus()
		if got != string(status) {
			t.Errorf("GetServiceStatus() = %q, want %q", got, string(status))
		}
	}
}

// TestBaseProviderConfigChanged 测试配置变化标志
func TestBaseProviderConfigChanged(t *testing.T) {
	b := &BaseProvider{}

	if b.ConfigChanged() {
		t.Error("初始状态 ConfigChanged() 应返回 false")
	}

	b.configChanged = true
	if !b.ConfigChanged() {
		t.Error("设置 configChanged=true 后 ConfigChanged() 应返回 true")
	}
}

// TestBaseProviderShouldSendWebhook 测试 Webhook 触发逻辑
func TestBaseProviderShouldSendWebhook(t *testing.T) {
	t.Run("UpdatedSuccess 立即触发 Webhook 并重置失败计数", func(t *testing.T) {
		cache := NewCache()
		cache.TimesFailed = 5
		b := &BaseProvider{
			CDN:    &config.CDN{Domain: "example.com"},
			Cache:  &cache,
			Status: UpdatedSuccess,
		}
		if !b.ShouldSendWebhook() {
			t.Error("UpdatedSuccess 应触发 Webhook")
		}
		if b.Cache.TimesFailed != 0 {
			t.Errorf("成功后 TimesFailed 应归零, got %d", b.Cache.TimesFailed)
		}
	})

	t.Run("UpdatedFailed 第1次不触发", func(t *testing.T) {
		cache := NewCache()
		b := &BaseProvider{
			CDN:    &config.CDN{Domain: "example.com"},
			Cache:  &cache,
			Status: UpdatedFailed,
		}
		if b.ShouldSendWebhook() {
			t.Error("第1次失败不应触发 Webhook")
		}
		if b.Cache.TimesFailed != 1 {
			t.Errorf("TimesFailed 应为 1, got %d", b.Cache.TimesFailed)
		}
	})

	t.Run("UpdatedFailed 第2次不触发", func(t *testing.T) {
		cache := NewCache()
		cache.TimesFailed = 1
		b := &BaseProvider{
			CDN:    &config.CDN{Domain: "example.com"},
			Cache:  &cache,
			Status: UpdatedFailed,
		}
		if b.ShouldSendWebhook() {
			t.Error("第2次失败不应触发 Webhook")
		}
		if b.Cache.TimesFailed != 2 {
			t.Errorf("TimesFailed 应为 2, got %d", b.Cache.TimesFailed)
		}
	})

	t.Run("UpdatedFailed 第3次触发 Webhook 并重置计数", func(t *testing.T) {
		cache := NewCache()
		cache.TimesFailed = 2
		b := &BaseProvider{
			CDN:    &config.CDN{Domain: "example.com"},
			Cache:  &cache,
			Status: UpdatedFailed,
		}
		if !b.ShouldSendWebhook() {
			t.Error("连续3次失败应触发 Webhook")
		}
		if b.Cache.TimesFailed != 0 {
			t.Errorf("触发后 TimesFailed 应归零, got %d", b.Cache.TimesFailed)
		}
	})

	t.Run("其他状态不触发 Webhook", func(t *testing.T) {
		nonTriggerStatuses := []statusType{InitFailed, InitSuccess, InitGetIPFailed, UpdatedNothing}
		for _, status := range nonTriggerStatuses {
			cache := NewCache()
			b := &BaseProvider{
				CDN:    &config.CDN{Domain: "example.com"},
				Cache:  &cache,
				Status: status,
			}
			if b.ShouldSendWebhook() {
				t.Errorf("状态 %q 不应触发 Webhook", status)
			}
		}
	})
}

// TestBaseProviderUpdateCnameIfChanged 测试 CNAME 更新逻辑
func TestBaseProviderUpdateCnameIfChanged(t *testing.T) {
	t.Run("CNAME 发生变化时更新并标记配置变化", func(t *testing.T) {
		cdn := &config.CDN{Domain: "example.com", CName: "old-cname.cdn.com"}
		cache := NewCache()
		b := &BaseProvider{CDN: cdn, Cache: &cache}

		b.updateCnameIfChanged("new-cname.cdn.com")

		if b.CDN.CName != "new-cname.cdn.com" {
			t.Errorf("CName = %q, want %q", b.CDN.CName, "new-cname.cdn.com")
		}
		if !b.configChanged {
			t.Error("CNAME 变化后 configChanged 应为 true")
		}
	})

	t.Run("CNAME 未变化时不更新", func(t *testing.T) {
		cdn := &config.CDN{Domain: "example.com", CName: "same-cname.cdn.com"}
		cache := NewCache()
		b := &BaseProvider{CDN: cdn, Cache: &cache}

		b.updateCnameIfChanged("same-cname.cdn.com")

		if b.configChanged {
			t.Error("CNAME 未变化时 configChanged 应为 false")
		}
	})

	t.Run("新 CNAME 为空时不更新", func(t *testing.T) {
		cdn := &config.CDN{Domain: "example.com", CName: "existing-cname.cdn.com"}
		cache := NewCache()
		b := &BaseProvider{CDN: cdn, Cache: &cache}

		b.updateCnameIfChanged("")

		if b.CDN.CName != "existing-cname.cdn.com" {
			t.Error("新 CNAME 为空时不应修改原有 CName")
		}
		if b.configChanged {
			t.Error("新 CNAME 为空时 configChanged 应为 false")
		}
	})

	t.Run("初始 CNAME 为空，设置新 CNAME", func(t *testing.T) {
		cdn := &config.CDN{Domain: "example.com", CName: ""}
		cache := NewCache()
		b := &BaseProvider{CDN: cdn, Cache: &cache}

		b.updateCnameIfChanged("new-cname.cdn.com")

		if b.CDN.CName != "new-cname.cdn.com" {
			t.Errorf("CName = %q, want %q", b.CDN.CName, "new-cname.cdn.com")
		}
		if !b.configChanged {
			t.Error("设置新 CNAME 后 configChanged 应为 true")
		}
	})
}

// TestBaseProviderHasDynamicSources 测试动态源站检测
func TestBaseProviderHasDynamicSources(t *testing.T) {
	tests := []struct {
		name     string
		sources  []config.Source
		expected bool
	}{
		{
			name:     "无源站",
			sources:  []config.Source{},
			expected: false,
		},
		{
			name: "只有静态源站",
			sources: []config.Source{
				{Type: "static", Value: "1.2.3.4"},
				{Type: "domain", Value: "origin.example.com"},
			},
			expected: false,
		},
		{
			name: "有动态 IPv4 URL 源站",
			sources: []config.Source{
				{Type: helper.DynamicIPv4URL, Value: "http://ip.example.com"},
			},
			expected: true,
		},
		{
			name: "有动态 IPv4 接口源站",
			sources: []config.Source{
				{Type: helper.DynamicIPv4Interface, Value: "eth0"},
			},
			expected: true,
		},
		{
			name: "混合静态和动态源站",
			sources: []config.Source{
				{Type: "static", Value: "1.2.3.4"},
				{Type: helper.DynamicIPv4URL, Value: "http://ip.example.com"},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cdn := &config.CDN{Domain: "example.com", Sources: tt.sources}
			cache := NewCache()
			b := &BaseProvider{CDN: cdn, Cache: &cache}

			got := b.hasDynamicSources()
			if got != tt.expected {
				t.Errorf("hasDynamicSources() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestBaseProviderValidateBaseConfig 测试配置校验
func TestBaseProviderValidateBaseConfig(t *testing.T) {
	tests := []struct {
		name     string
		cdn      *config.CDN
		expected bool
	}{
		{
			name:     "CDN 为 nil",
			cdn:      nil,
			expected: false,
		},
		{
			name: "AccessKey 为空",
			cdn: &config.CDN{
				Domain:       "example.com",
				AccessKey:    "",
				AccessSecret: "secret",
				Sources:      []config.Source{{Value: "1.2.3.4"}},
			},
			expected: false,
		},
		{
			name: "AccessSecret 为空",
			cdn: &config.CDN{
				Domain:       "example.com",
				AccessKey:    "key",
				AccessSecret: "",
				Sources:      []config.Source{{Value: "1.2.3.4"}},
			},
			expected: false,
		},
		{
			name: "Domain 为空",
			cdn: &config.CDN{
				Domain:       "",
				AccessKey:    "key",
				AccessSecret: "secret",
				Sources:      []config.Source{{Value: "1.2.3.4"}},
			},
			expected: false,
		},
		{
			name: "Sources 为空",
			cdn: &config.CDN{
				Domain:       "example.com",
				AccessKey:    "key",
				AccessSecret: "secret",
				Sources:      []config.Source{},
			},
			expected: false,
		},
		{
			name: "配置完整",
			cdn: &config.CDN{
				Domain:       "example.com",
				AccessKey:    "key",
				AccessSecret: "secret",
				Sources:      []config.Source{{Value: "1.2.3.4"}},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewCache()
			b := &BaseProvider{CDN: tt.cdn, Cache: &cache}

			got := b.validateBaseConfig("TestProvider")
			if got != tt.expected {
				t.Errorf("validateBaseConfig() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestBaseProviderShouldUpdate 测试更新决策逻辑
func TestBaseProviderShouldUpdate(t *testing.T) {
	t.Run("首次运行应更新", func(t *testing.T) {
		cache := NewCache()
		cdn := &config.CDN{Domain: "example.com"}
		b := &BaseProvider{CDN: cdn, Cache: &cache}

		got := b.shouldUpdate("TestProvider", 0)
		if !got {
			t.Error("首次运行应返回 true")
		}
		if !b.Cache.HasRun {
			t.Error("调用后 HasRun 应为 true")
		}
	})

	t.Run("IP 有变化时应更新", func(t *testing.T) {
		cache := NewCache()
		cache.HasRun = true
		cdn := &config.CDN{Domain: "example.com"}
		b := &BaseProvider{CDN: cdn, Cache: &cache}

		got := b.shouldUpdate("TestProvider", 1)
		if !got {
			t.Error("IP 有变化时应返回 true")
		}
	})

	t.Run("无动态源站时不因计数器触发更新", func(t *testing.T) {
		cache := NewCache()
		cache.HasRun = true
		cache.Times = 1
		cdn := &config.CDN{
			Domain:  "example.com",
			Sources: []config.Source{{Type: "static", Value: "1.2.3.4"}},
		}
		b := &BaseProvider{CDN: cdn, Cache: &cache}

		got := b.shouldUpdate("TestProvider", 0)
		if got {
			t.Error("无动态源站时不应因计数器触发更新")
		}
	})

	t.Run("有动态源站且计数器归零时强制更新", func(t *testing.T) {
		cache := NewCache()
		cache.HasRun = true
		cache.Times = 1
		cdn := &config.CDN{
			Domain:  "example.com",
			Sources: []config.Source{{Type: helper.DynamicIPv4URL, Value: "http://ip.example.com"}},
		}
		b := &BaseProvider{CDN: cdn, Cache: &cache}

		got := b.shouldUpdate("TestProvider", 0)
		if !got {
			t.Error("计数器归零时应强制更新")
		}
	})

	t.Run("有动态源站且计数器未归零时不更新", func(t *testing.T) {
		cache := NewCache()
		cache.HasRun = true
		cache.Times = 3
		cdn := &config.CDN{
			Domain:  "example.com",
			Sources: []config.Source{{Type: helper.DynamicIPv4URL, Value: "http://ip.example.com"}},
		}
		b := &BaseProvider{CDN: cdn, Cache: &cache}

		got := b.shouldUpdate("TestProvider", 0)
		if got {
			t.Error("计数器未归零时不应更新")
		}
		if b.Cache.Times != 2 {
			t.Errorf("Times 应减1变为 2, got %d", b.Cache.Times)
		}
	})
}
