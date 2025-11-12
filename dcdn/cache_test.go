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
