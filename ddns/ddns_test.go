package ddns

import (
	"os"
	"sync"
	"testing"

	"github.com/cxbdasheng/dnet/helper"
)

// TestNewCache 测试缓存初始化
func TestNewCache(t *testing.T) {
	t.Run("default times is 5", func(t *testing.T) {
		os.Unsetenv(CacheTimesENV)
		c := NewCache()
		if c.Times != 5 {
			t.Errorf("Times = %d, want 5", c.Times)
		}
		if c.DynamicIPs == nil {
			t.Error("DynamicIPs should be initialized")
		}
		if c.HasRun {
			t.Error("HasRun should be false")
		}
		if c.TimesFailed != 0 {
			t.Errorf("TimesFailed = %d, want 0", c.TimesFailed)
		}
	})

	t.Run("env var overrides default", func(t *testing.T) {
		os.Setenv(CacheTimesENV, "10")
		defer os.Unsetenv(CacheTimesENV)
		c := NewCache()
		if c.Times != 10 {
			t.Errorf("Times = %d, want 10", c.Times)
		}
	})

	t.Run("invalid env var falls back to 5", func(t *testing.T) {
		os.Setenv(CacheTimesENV, "not-a-number")
		defer os.Unsetenv(CacheTimesENV)
		c := NewCache()
		if c.Times != 5 {
			t.Errorf("Times = %d, want 5", c.Times)
		}
	})
}

// TestIsDynamicType 测试动态类型判断
func TestIsDynamicType(t *testing.T) {
	tests := []struct {
		name     string
		ipType   string
		expected bool
	}{
		{"dynamic ipv4 url", helper.DynamicIPv4URL, true},
		{"dynamic ipv4 interface", helper.DynamicIPv4Interface, true},
		{"dynamic ipv4 command", helper.DynamicIPv4Command, true},
		{"dynamic ipv6 url", helper.DynamicIPv6URL, true},
		{"dynamic ipv6 interface", helper.DynamicIPv6Interface, true},
		{"dynamic ipv6 command", helper.DynamicIPv6Command, true},
		{"static ip", "1.2.3.4", false},
		{"empty string", "", false},
		{"random string", "something", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsDynamicType(tt.ipType)
			if result != tt.expected {
				t.Errorf("IsDynamicType(%q) = %v, want %v", tt.ipType, result, tt.expected)
			}
		})
	}
}

// TestCacheCheckIPChanged 测试 IP 变化检测
func TestCacheCheckIPChanged(t *testing.T) {
	t.Run("first check returns changed=true", func(t *testing.T) {
		c := NewCache()
		changed, old := c.CheckIPChanged("key1", "1.2.3.4")
		if !changed {
			t.Error("first check should return changed=true")
		}
		if old != "" {
			t.Errorf("old value should be empty, got %q", old)
		}
	})

	t.Run("same IP returns changed=false", func(t *testing.T) {
		c := NewCache()
		c.UpdateDynamicIP("key1", "1.2.3.4")
		changed, old := c.CheckIPChanged("key1", "1.2.3.4")
		if changed {
			t.Error("same IP should return changed=false")
		}
		if old != "1.2.3.4" {
			t.Errorf("old = %q, want '1.2.3.4'", old)
		}
	})

	t.Run("different IP returns changed=true", func(t *testing.T) {
		c := NewCache()
		c.UpdateDynamicIP("key1", "1.2.3.4")
		changed, old := c.CheckIPChanged("key1", "5.6.7.8")
		if !changed {
			t.Error("different IP should return changed=true")
		}
		if old != "1.2.3.4" {
			t.Errorf("old = %q, want '1.2.3.4'", old)
		}
	})

	t.Run("different keys are independent", func(t *testing.T) {
		c := NewCache()
		c.UpdateDynamicIP("key1", "1.2.3.4")
		changed, _ := c.CheckIPChanged("key2", "1.2.3.4")
		if !changed {
			t.Error("key2 not in cache, should return changed=true")
		}
	})
}

// TestCacheUpdateAndGetDynamicIP 测试动态 IP 更新与获取
func TestCacheUpdateAndGetDynamicIP(t *testing.T) {
	t.Run("update and retrieve", func(t *testing.T) {
		c := NewCache()
		c.UpdateDynamicIP("mykey", "192.168.1.1")
		ip, exists := c.GetDynamicIP("mykey")
		if !exists {
			t.Error("GetDynamicIP() should return exists=true")
		}
		if ip != "192.168.1.1" {
			t.Errorf("ip = %q, want '192.168.1.1'", ip)
		}
	})

	t.Run("missing key returns empty", func(t *testing.T) {
		c := NewCache()
		ip, exists := c.GetDynamicIP("nonexistent")
		if exists {
			t.Error("GetDynamicIP() should return exists=false for missing key")
		}
		if ip != "" {
			t.Errorf("ip = %q, want empty", ip)
		}
	})

	t.Run("update overwrites existing", func(t *testing.T) {
		c := NewCache()
		c.UpdateDynamicIP("key", "1.1.1.1")
		c.UpdateDynamicIP("key", "2.2.2.2")
		ip, _ := c.GetDynamicIP("key")
		if ip != "2.2.2.2" {
			t.Errorf("ip = %q, want '2.2.2.2'", ip)
		}
	})
}

// TestCacheResetTimes 测试计数器重置
func TestCacheResetTimes(t *testing.T) {
	t.Run("resets to default 5", func(t *testing.T) {
		os.Unsetenv(CacheTimesENV)
		c := NewCache()
		c.Times = 0
		c.ResetTimes()
		if c.Times != 5 {
			t.Errorf("Times = %d, want 5 after reset", c.Times)
		}
	})

	t.Run("resets to env value", func(t *testing.T) {
		os.Setenv(CacheTimesENV, "20")
		defer os.Unsetenv(CacheTimesENV)
		c := NewCache()
		c.Times = 0
		c.ResetTimes()
		if c.Times != 20 {
			t.Errorf("Times = %d, want 20 after reset", c.Times)
		}
	})
}

// TestCacheConcurrency 测试缓存并发安全
func TestCacheConcurrency(t *testing.T) {
	c := NewCache()
	const goroutines = 50
	var wg sync.WaitGroup

	// 并发写入
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "key"
			c.UpdateDynamicIP(key, "1.2.3.4")
		}(i)
	}

	// 并发读取
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.GetDynamicIP("key")
			c.CheckIPChanged("key", "1.2.3.4")
		}()
	}

	wg.Wait() // 无 panic 或 race 即为通过
}

// TestGetCacheKey 测试缓存键生成
func TestGetCacheKey(t *testing.T) {
	t.Run("non-interface type ignores regex", func(t *testing.T) {
		key1 := getCacheKey(helper.DynamicIPv4URL, "http://api.ipify.org", ".*")
		key2 := getCacheKey(helper.DynamicIPv4URL, "http://api.ipify.org", "")
		if key1 != key2 {
			t.Errorf("non-interface type: key with regex (%q) != key without regex (%q)", key1, key2)
		}
	})

	t.Run("IPv6 interface with regex produces different key", func(t *testing.T) {
		keyNoRegex := getCacheKey(helper.DynamicIPv6Interface, "eth0", "")
		keyWithRegex := getCacheKey(helper.DynamicIPv6Interface, "eth0", `2001:.*`)
		if keyNoRegex == keyWithRegex {
			t.Error("IPv6 interface with regex should produce different key than without")
		}
	})

	t.Run("interface types include type prefix in key", func(t *testing.T) {
		// URL 类型直接使用值作为 key（设计如此），interface 类型包含类型前缀
		key1 := getCacheKey(helper.DynamicIPv4Interface, "eth0", "")
		key2 := getCacheKey(helper.DynamicIPv6Interface, "eth0", "")
		if key1 == key2 {
			t.Error("ipv4 and ipv6 interface keys should differ")
		}
	})
}

// TestShouldSendWebhook 测试 Webhook 发送判断逻辑
func TestShouldSendWebhook(t *testing.T) {
	t.Run("success always sends webhook and resets TimesFailed", func(t *testing.T) {
		c := NewCache()
		c.TimesFailed = 5
		result := shouldSendWebhook(&c, UpdatedSuccess)
		if !result {
			t.Error("UpdatedSuccess should send webhook")
		}
		if c.TimesFailed != 0 {
			t.Errorf("TimesFailed = %d, want 0 after success", c.TimesFailed)
		}
	})

	t.Run("first and second failure do not send webhook", func(t *testing.T) {
		c := NewCache()
		for i := 1; i <= 2; i++ {
			result := shouldSendWebhook(&c, UpdatedFailed)
			if result {
				t.Errorf("failure #%d should NOT send webhook", i)
			}
			if c.TimesFailed != i {
				t.Errorf("TimesFailed = %d, want %d", c.TimesFailed, i)
			}
		}
	})

	t.Run("third consecutive failure sends webhook", func(t *testing.T) {
		c := NewCache()
		shouldSendWebhook(&c, UpdatedFailed)
		shouldSendWebhook(&c, UpdatedFailed)
		result := shouldSendWebhook(&c, UpdatedFailed)
		if !result {
			t.Error("third consecutive failure should send webhook")
		}
	})

	t.Run("InitGetIPFailed counts toward failure threshold", func(t *testing.T) {
		c := NewCache()
		shouldSendWebhook(&c, InitGetIPFailed)
		shouldSendWebhook(&c, InitGetIPFailed)
		result := shouldSendWebhook(&c, InitGetIPFailed)
		if !result {
			t.Error("three InitGetIPFailed should send webhook")
		}
	})

	t.Run("other statuses do not send webhook", func(t *testing.T) {
		c := NewCache()
		if shouldSendWebhook(&c, UpdatedNothing) {
			t.Error("UpdatedNothing should not send webhook")
		}
		if shouldSendWebhook(&c, InitFailed) {
			t.Error("InitFailed should not send webhook")
		}
	})
}
