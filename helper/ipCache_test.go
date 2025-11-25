package helper

import (
	"sync"
	"testing"
)

// TestGlobalIPCache_GetSet 测试缓存的基本 Get 和 Set 操作
func TestGlobalIPCache_GetSet(t *testing.T) {
	// 清空缓存
	GlobalIPCache.Clear()

	tests := []struct {
		name  string
		key   string
		value string
	}{
		{
			name:  "IPv4 URL",
			key:   "http://api.ipify.org",
			value: "192.168.1.1",
		},
		{
			name:  "IPv6 URL",
			key:   "http://api6.ipify.org",
			value: "2001:db8::1",
		},
		{
			name:  "Interface key",
			key:   "dynamic_ipv4_interface:eth0",
			value: "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 测试 Set
			GlobalIPCache.Set(tt.key, tt.value)

			// 测试 Get
			got, exists := GlobalIPCache.Get(tt.key)
			if !exists {
				t.Errorf("Get(%s) 键不存在", tt.key)
			}
			if got != tt.value {
				t.Errorf("Get(%s) = %v, want %v", tt.key, got, tt.value)
			}
		})
	}

	// 测试不存在的键
	t.Run("不存在的键", func(t *testing.T) {
		_, exists := GlobalIPCache.Get("nonexistent")
		if exists {
			t.Error("不存在的键应该返回 exists=false")
		}
	})
}

// TestGlobalIPCache_Clear 测试缓存清空
func TestGlobalIPCache_Clear(t *testing.T) {
	GlobalIPCache.Clear()

	// 添加一些数据
	GlobalIPCache.Set("key1", "192.168.1.1")
	GlobalIPCache.Set("key2", "192.168.1.2")

	// 验证数据存在
	if _, exists := GlobalIPCache.Get("key1"); !exists {
		t.Error("key1 应该存在")
	}

	// 清空缓存
	GlobalIPCache.Clear()

	// 验证数据已清空
	if _, exists := GlobalIPCache.Get("key1"); exists {
		t.Error("清空后 key1 不应该存在")
	}
	if _, exists := GlobalIPCache.Get("key2"); exists {
		t.Error("清空后 key2 不应该存在")
	}
}

// TestGetIPCacheKey 测试缓存键生成
func TestGetIPCacheKey(t *testing.T) {
	tests := []struct {
		name        string
		sourceType  string
		sourceValue string
		want        string
	}{
		{
			name:        "IPv4 Interface",
			sourceType:  DynamicIPv4Interface,
			sourceValue: "eth0",
			want:        "dynamic_ipv4_interface:eth0",
		},
		{
			name:        "IPv6 Interface",
			sourceType:  DynamicIPv6Interface,
			sourceValue: "wlan0",
			want:        "dynamic_ipv6_interface:wlan0",
		},
		{
			name:        "IPv4 URL",
			sourceType:  DynamicIPv4URL,
			sourceValue: "http://api.ipify.org",
			want:        "http://api.ipify.org",
		},
		{
			name:        "IPv6 URL",
			sourceType:  DynamicIPv6URL,
			sourceValue: "http://api6.ipify.org",
			want:        "http://api6.ipify.org",
		},
		{
			name:        "IPv4 Command",
			sourceType:  DynamicIPv4Command,
			sourceValue: "curl ifconfig.me",
			want:        "curl ifconfig.me",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetIPCacheKey(tt.sourceType, tt.sourceValue)
			if got != tt.want {
				t.Errorf("GetIPCacheKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestClearGlobalIPCache 测试全局缓存清空函数
func TestClearGlobalIPCache(t *testing.T) {
	GlobalIPCache.Set("test1", "value1")
	GlobalIPCache.Set("test2", "value2")

	ClearGlobalIPCache()

	if _, exists := GlobalIPCache.Get("test1"); exists {
		t.Error("清空后 test1 不应该存在")
	}
	if _, exists := GlobalIPCache.Get("test2"); exists {
		t.Error("清空后 test2 不应该存在")
	}
}

// TestGetDynamicIPWithCache 测试从缓存获取动态 IP
func TestGetDynamicIPWithCache(t *testing.T) {
	GlobalIPCache.Clear()

	sourceType := DynamicIPv4URL
	sourceValue := "http://test.example.com"
	expectedIP := "192.168.1.100"

	// 测试缓存未命中
	t.Run("缓存未命中", func(t *testing.T) {
		ip, exists := GetDynamicIPWithCache(sourceType, sourceValue)
		if exists {
			t.Error("空缓存应该返回 exists=false")
		}
		if ip != "" {
			t.Errorf("空缓存应该返回空 IP, got %v", ip)
		}
	})

	// 设置缓存
	SetGlobalIPCache(sourceType, sourceValue, expectedIP)

	// 测试缓存命中
	t.Run("缓存命中", func(t *testing.T) {
		ip, exists := GetDynamicIPWithCache(sourceType, sourceValue)
		if !exists {
			t.Error("缓存命中应该返回 exists=true")
		}
		if ip != expectedIP {
			t.Errorf("GetDynamicIPWithCache() = %v, want %v", ip, expectedIP)
		}
	})
}

// TestSetGlobalIPCache 测试设置全局 IP 缓存
func TestSetGlobalIPCache(t *testing.T) {
	GlobalIPCache.Clear()

	tests := []struct {
		name        string
		sourceType  string
		sourceValue string
		ip          string
	}{
		{
			name:        "设置 IPv4",
			sourceType:  DynamicIPv4URL,
			sourceValue: "http://test1.com",
			ip:          "1.2.3.4",
		},
		{
			name:        "设置 IPv6",
			sourceType:  DynamicIPv6Interface,
			sourceValue: "eth0",
			ip:          "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetGlobalIPCache(tt.sourceType, tt.sourceValue, tt.ip)

			// 验证设置成功
			ip, exists := GetDynamicIPWithCache(tt.sourceType, tt.sourceValue)
			if !exists {
				t.Error("设置后应该存在")
			}
			if ip != tt.ip {
				t.Errorf("IP = %v, want %v", ip, tt.ip)
			}
		})
	}
}

// TestGlobalIPCache_Concurrent 测试并发安全性
func TestGlobalIPCache_Concurrent(t *testing.T) {
	GlobalIPCache.Clear()

	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// 并发写入
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := "key-" + string(rune(id))
				value := "value-" + string(rune(j))
				GlobalIPCache.Set(key, value)
			}
		}(i)
	}

	// 并发读取
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := "key-" + string(rune(id))
				GlobalIPCache.Get(key)
			}
		}(i)
	}

	wg.Wait()

	// 测试并发清空
	var wg2 sync.WaitGroup
	wg2.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg2.Done()
			GlobalIPCache.Clear()
		}()
	}
	wg2.Wait()
}

// TestGetOrSetDynamicIPWithCache_Interface 测试接口类型
func TestGetOrSetDynamicIPWithCache_Interface(t *testing.T) {
	GlobalIPCache.Clear()

	// 注意：这个测试可能需要真实的网络接口
	// 这里只测试缓存逻辑，不测试实际 IP 获取
	t.Run("缓存命中直接返回", func(t *testing.T) {
		sourceType := DynamicIPv4Interface
		sourceValue := "test-interface"
		expectedIP := "192.168.1.1"

		// 预设缓存
		SetGlobalIPCache(sourceType, sourceValue, expectedIP)

		// 调用函数应该直接返回缓存值
		ip, exists := GetOrSetDynamicIPWithCache(sourceType, sourceValue)
		if !exists {
			t.Error("缓存命中应该返回 true")
		}
		if ip != expectedIP {
			t.Errorf("IP = %v, want %v", ip, expectedIP)
		}
	})
}

// TestGetOrSetDynamicIPWithCache_InvalidType 测试无效类型
func TestGetOrSetDynamicIPWithCache_InvalidType(t *testing.T) {
	GlobalIPCache.Clear()

	ip, exists := GetOrSetDynamicIPWithCache("invalid_type", "some_value")
	if exists {
		t.Error("无效类型应该返回 exists=false")
	}
	if ip != "" {
		t.Errorf("无效类型应该返回空 IP, got %v", ip)
	}
}

// TestIPCacheKey_DifferentSources 测试不同源的缓存键唯一性
func TestIPCacheKey_DifferentSources(t *testing.T) {
	GlobalIPCache.Clear()

	// Interface 类型应该包含类型前缀
	key1 := GetIPCacheKey(DynamicIPv4Interface, "eth0")
	key2 := GetIPCacheKey(DynamicIPv6Interface, "eth0")

	if key1 == key2 {
		t.Error("不同类型的相同接口名应该生成不同的缓存键")
	}

	// URL 类型不包含类型前缀
	key3 := GetIPCacheKey(DynamicIPv4URL, "http://api.ipify.org")
	key4 := GetIPCacheKey(DynamicIPv6URL, "http://api.ipify.org")

	if key3 != key4 {
		t.Error("URL 类型应该只使用 URL 作为缓存键")
	}
}

// BenchmarkGlobalIPCache_Get 缓存读取性能测试
func BenchmarkGlobalIPCache_Get(b *testing.B) {
	GlobalIPCache.Clear()
	GlobalIPCache.Set("test-key", "192.168.1.1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GlobalIPCache.Get("test-key")
	}
}

// BenchmarkGlobalIPCache_Set 缓存写入性能测试
func BenchmarkGlobalIPCache_Set(b *testing.B) {
	GlobalIPCache.Clear()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GlobalIPCache.Set("test-key", "192.168.1.1")
	}
}

// BenchmarkGetIPCacheKey 缓存键生成性能测试
func BenchmarkGetIPCacheKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GetIPCacheKey(DynamicIPv4Interface, "eth0")
	}
}

// BenchmarkGlobalIPCache_Concurrent 并发性能测试
func BenchmarkGlobalIPCache_Concurrent(b *testing.B) {
	GlobalIPCache.Clear()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			GlobalIPCache.Set("bench-key", "192.168.1.1")
			GlobalIPCache.Get("bench-key")
		}
	})
}
