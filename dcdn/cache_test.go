package dcdn

import (
	"sync"
	"testing"
)

// TestCacheConcurrency 测试 Cache 的并发安全性
func TestCacheConcurrency(t *testing.T) {
	cache := NewCache()
	var wg sync.WaitGroup

	// 并发写入测试
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := "test-key"
			value := "192.168.1." + string(rune('0'+idx%10))
			cache.UpdateDynamicIP(key, value)
		}(i)
	}

	// 并发读取测试
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.CheckIPChanged("test-key", "192.168.1.100")
		}()
	}

	// 并发获取副本测试
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cache.GetDynamicIPs()
		}()
	}

	wg.Wait()
}

// TestCheckIPChanged 测试 IP 变化检测
func TestCheckIPChanged(t *testing.T) {
	cache := NewCache()

	// 第一次检查,应该返回变化
	changed, oldIP := cache.CheckIPChanged("eth0", "192.168.1.1")
	if !changed {
		t.Error("首次检查应该返回 changed=true")
	}
	if oldIP != "" {
		t.Error("首次检查 oldIP 应该为空")
	}

	// 更新 IP
	cache.UpdateDynamicIP("eth0", "192.168.1.1")

	// 第二次检查相同 IP,应该不变化
	changed, oldIP = cache.CheckIPChanged("eth0", "192.168.1.1")
	if changed {
		t.Error("相同 IP 应该返回 changed=false")
	}
	if oldIP != "192.168.1.1" {
		t.Errorf("oldIP 应该是 '192.168.1.1', 实际是 '%s'", oldIP)
	}

	// 第三次检查不同 IP,应该变化
	changed, oldIP = cache.CheckIPChanged("eth0", "192.168.1.2")
	if !changed {
		t.Error("不同 IP 应该返回 changed=true")
	}
	if oldIP != "192.168.1.1" {
		t.Errorf("oldIP 应该是 '192.168.1.1', 实际是 '%s'", oldIP)
	}
}

// TestGetDynamicIPs 测试获取 IP 副本
func TestGetDynamicIPs(t *testing.T) {
	cache := NewCache()

	// 添加一些 IP
	cache.UpdateDynamicIP("eth0", "192.168.1.1")
	cache.UpdateDynamicIP("eth1", "192.168.1.2")

	// 获取副本
	ips := cache.GetDynamicIPs()

	// 验证副本内容
	if len(ips) != 2 {
		t.Errorf("期望 2 个 IP, 实际 %d 个", len(ips))
	}
	if ips["eth0"] != "192.168.1.1" {
		t.Errorf("eth0 IP 错误: %s", ips["eth0"])
	}
	if ips["eth1"] != "192.168.1.2" {
		t.Errorf("eth1 IP 错误: %s", ips["eth1"])
	}

	// 修改副本不应影响原始数据
	ips["eth0"] = "10.0.0.1"
	changed, _ := cache.CheckIPChanged("eth0", "192.168.1.1")
	if changed {
		t.Error("修改副本不应影响原始缓存")
	}
}

// TestNewCache 测试创建新缓存
func TestNewCache(t *testing.T) {
	cache := NewCache()

	if cache.Times <= 0 {
		t.Error("NewCache() Times 应该大于 0")
	}

	if cache.DynamicIPs == nil {
		t.Error("NewCache() DynamicIPs 不应为 nil")
	}

	if cache.TimesFailed != 0 {
		t.Errorf("NewCache() TimesFailed 应该为 0, 实际 %d", cache.TimesFailed)
	}

	if cache.HasRun {
		t.Error("NewCache() HasRun 应该为 false")
	}
}

// TestResetTimes 测试重置计数器
func TestResetTimes(t *testing.T) {
	cache := NewCache()

	// 修改 Times
	cache.Times = 0

	// 重置
	cache.ResetTimes()

	if cache.Times <= 0 {
		t.Error("ResetTimes() 后 Times 应该大于 0")
	}
}

// TestIsDynamicType 测试动态类型判断
func TestIsDynamicType(t *testing.T) {
	tests := []struct {
		name       string
		sourceType string
		want       bool
	}{
		{
			name:       "IPv4 URL 是动态类型",
			sourceType: "dynamic_ipv4_url",
			want:       true,
		},
		{
			name:       "IPv4 Interface 是动态类型",
			sourceType: "dynamic_ipv4_interface",
			want:       true,
		},
		{
			name:       "IPv4 Command 是动态类型",
			sourceType: "dynamic_ipv4_command",
			want:       true,
		},
		{
			name:       "IPv6 URL 是动态类型",
			sourceType: "dynamic_ipv6_url",
			want:       true,
		},
		{
			name:       "IPv6 Interface 是动态类型",
			sourceType: "dynamic_ipv6_interface",
			want:       true,
		},
		{
			name:       "IPv6 Command 是动态类型",
			sourceType: "dynamic_ipv6_command",
			want:       true,
		},
		{
			name:       "静态类型",
			sourceType: "static",
			want:       false,
		},
		{
			name:       "未知类型",
			sourceType: "unknown",
			want:       false,
		},
		{
			name:       "空字符串",
			sourceType: "",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsDynamicType(tt.sourceType)
			if got != tt.want {
				t.Errorf("IsDynamicType(%v) = %v, want %v", tt.sourceType, got, tt.want)
			}
		})
	}
}

// TestUpdateDynamicIP_NilMap 测试 nil map 的处理
func TestUpdateDynamicIP_NilMap(t *testing.T) {
	cache := Cache{
		DynamicIPs: nil, // 故意设置为 nil
	}

	// 应该能够正常更新而不 panic
	cache.UpdateDynamicIP("test", "192.168.1.1")

	if cache.DynamicIPs == nil {
		t.Error("UpdateDynamicIP 应该初始化 DynamicIPs map")
	}

	if cache.DynamicIPs["test"] != "192.168.1.1" {
		t.Error("UpdateDynamicIP 应该正确设置 IP")
	}
}

// TestCheckIPChanged_EmptyKey 测试空键的处理
func TestCheckIPChanged_EmptyKey(t *testing.T) {
	cache := NewCache()

	changed, oldIP := cache.CheckIPChanged("", "192.168.1.1")
	if !changed {
		t.Error("空键首次检查应该返回 changed=true")
	}
	if oldIP != "" {
		t.Error("空键 oldIP 应该为空")
	}
}

// TestCache_MultipleKeys 测试多个键的独立性
func TestCache_MultipleKeys(t *testing.T) {
	cache := NewCache()

	// 设置多个不同的键
	keys := []string{"eth0", "eth1", "wlan0", "lo"}
	ips := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3", "127.0.0.1"}

	for i, key := range keys {
		cache.UpdateDynamicIP(key, ips[i])
	}

	// 验证每个键都是独立的
	for i, key := range keys {
		changed, oldIP := cache.CheckIPChanged(key, ips[i])
		if changed {
			t.Errorf("键 %s 应该没有变化", key)
		}
		if oldIP != ips[i] {
			t.Errorf("键 %s 的 IP 错误: got %s, want %s", key, oldIP, ips[i])
		}
	}

	// 修改一个键不应该影响其他键
	cache.UpdateDynamicIP("eth0", "10.0.0.1")

	changed, _ := cache.CheckIPChanged("eth1", "192.168.1.2")
	if changed {
		t.Error("修改 eth0 不应该影响 eth1")
	}
}

// TestCache_IPv6Addresses 测试 IPv6 地址
func TestCache_IPv6Addresses(t *testing.T) {
	cache := NewCache()

	ipv6Addresses := []string{
		"2001:db8::1",
		"fe80::1",
		"::1",
		"2001:0db8:85a3:0000:0000:8a2e:0370:7334",
	}

	for i, addr := range ipv6Addresses {
		key := "test-" + string(rune('0'+i))
		cache.UpdateDynamicIP(key, addr)

		changed, oldIP := cache.CheckIPChanged(key, addr)
		if changed {
			t.Errorf("IPv6 地址 %s 应该没有变化", addr)
		}
		if oldIP != addr {
			t.Errorf("IPv6 地址错误: got %s, want %s", oldIP, addr)
		}
	}
}

// TestStatusType_Constants 测试状态常量
func TestStatusType_Constants(t *testing.T) {
	tests := []struct {
		name     string
		status   statusType
		expected string
	}{
		{"初始化失败", InitFailed, "初始化失败"},
		{"初始化成功", InitSuccess, "初始化成功"},
		{"IP获取失败", InitGetIPFailed, "IP 获取失败"},
		{"未改变", UpdatedNothing, "未改变"},
		{"更新失败", UpdatedFailed, "失败"},
		{"更新成功", UpdatedSuccess, "成功"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("状态常量 %s = %v, want %v", tt.name, tt.status, tt.expected)
			}
		})
	}
}

// TestCDNType_Constants 测试 CDN 类型常量
func TestCDNType_Constants(t *testing.T) {
	if CDNTypeCDN != "CDN" {
		t.Errorf("CDNTypeCDN = %v, want CDN", CDNTypeCDN)
	}
	if CDNTypeDCDN != "DCDN" {
		t.Errorf("CDNTypeDCDN = %v, want DCDN", CDNTypeDCDN)
	}
	if CDNTypeDRCDN != "DRCDN" {
		t.Errorf("CDNTypeDRCDN = %v, want DRCDN", CDNTypeDRCDN)
	}
}

// BenchmarkCheckIPChanged 性能测试
func BenchmarkCheckIPChanged(b *testing.B) {
	cache := NewCache()
	cache.UpdateDynamicIP("test-key", "192.168.1.1")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.CheckIPChanged("test-key", "192.168.1.1")
	}
}

// BenchmarkUpdateDynamicIP 性能测试
func BenchmarkUpdateDynamicIP(b *testing.B) {
	cache := NewCache()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.UpdateDynamicIP("test-key", "192.168.1.1")
	}
}

// BenchmarkGetDynamicIPs 性能测试
func BenchmarkGetDynamicIPs(b *testing.B) {
	cache := NewCache()
	for i := 0; i < 100; i++ {
		key := "key-" + string(rune('0'+i%10))
		cache.UpdateDynamicIP(key, "192.168.1."+string(rune('0'+i%10)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.GetDynamicIPs()
	}
}

// BenchmarkIsDynamicType 性能测试
func BenchmarkIsDynamicType(b *testing.B) {
	for i := 0; i < b.N; i++ {
		IsDynamicType("dynamic_ipv4_url")
	}
}
