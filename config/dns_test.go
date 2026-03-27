package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRestoreSensitiveFieldsForDDNS 测试 DDNS 敏感字段恢复
func TestRestoreSensitiveFieldsForDDNS(t *testing.T) {
	tests := []struct {
		name         string
		newConf      DDNSConfig
		oldConf      DDNSConfig
		expectKey    string
		expectSecret string
	}{
		{
			name: "未修改-应该恢复原始值",
			newConf: DDNSConfig{
				DDNSEnabled: true,
				DDNS: []DNSGroup{
					{
						ID:           "dns-1",
						AccessKey:    "LTAI********GhIj",                // 脱敏数据
						AccessSecret: "3yq7***********************u5Eg", // 脱敏数据
					},
				},
			},
			oldConf: DDNSConfig{
				DDNSEnabled: true,
				DDNS: []DNSGroup{
					{
						ID:           "dns-1",
						AccessKey:    "LTAI5tAbCdEfGhIj",                // 原始数据
						AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg", // 原始数据
					},
				},
			},
			expectKey:    "LTAI5tAbCdEfGhIj",
			expectSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
		},
		{
			name: "修改了AccessKey-应该使用新值",
			newConf: DDNSConfig{
				DDNSEnabled: true,
				DDNS: []DNSGroup{
					{
						ID:           "dns-1",
						AccessKey:    "LTAI5tNewKeyValue123",            // 新值
						AccessSecret: "3yq7***********************u5Eg", // 脱敏数据
					},
				},
			},
			oldConf: DDNSConfig{
				DDNSEnabled: true,
				DDNS: []DNSGroup{
					{
						ID:           "dns-1",
						AccessKey:    "LTAI5tAbCdEfGhIj",
						AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
					},
				},
			},
			expectKey:    "LTAI5tNewKeyValue123",
			expectSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
		},
		{
			name: "修改了AccessSecret-应该使用新值",
			newConf: DDNSConfig{
				DDNSEnabled: true,
				DDNS: []DNSGroup{
					{
						ID:           "dns-1",
						AccessKey:    "LTAI********GhIj",
						AccessSecret: "NewSecret1234567890AbCdEfGh12", // 新值
					},
				},
			},
			oldConf: DDNSConfig{
				DDNSEnabled: true,
				DDNS: []DNSGroup{
					{
						ID:           "dns-1",
						AccessKey:    "LTAI5tAbCdEfGhIj",
						AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
					},
				},
			},
			expectKey:    "LTAI5tAbCdEfGhIj",
			expectSecret: "NewSecret1234567890AbCdEfGh12",
		},
		{
			name: "ID不匹配-新增配置使用新值",
			newConf: DDNSConfig{
				DDNSEnabled: true,
				DDNS: []DNSGroup{
					{
						ID:           "dns-new",
						AccessKey:    "LTAI5tNewKeyValue123",
						AccessSecret: "NewSecret1234567890AbCd",
					},
				},
			},
			oldConf: DDNSConfig{
				DDNSEnabled: true,
				DDNS: []DNSGroup{
					{
						ID:           "dns-1",
						AccessKey:    "LTAI5tAbCdEfGhIj",
						AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
					},
				},
			},
			expectKey:    "LTAI5tNewKeyValue123",
			expectSecret: "NewSecret1234567890AbCd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RestoreSensitiveFieldsForDDNS(tt.newConf, tt.oldConf)

			if len(result.DDNS) == 0 {
				t.Fatal("result.DDNS is empty")
			}

			if result.DDNS[0].AccessKey != tt.expectKey {
				t.Errorf("AccessKey = %q, want %q", result.DDNS[0].AccessKey, tt.expectKey)
			}
			if result.DDNS[0].AccessSecret != tt.expectSecret {
				t.Errorf("AccessSecret = %q, want %q", result.DDNS[0].AccessSecret, tt.expectSecret)
			}
		})
	}
}

// TestRestoreSensitiveFieldsForDDNS_EmptyOldConfig 测试旧配置为空的情况
func TestRestoreSensitiveFieldsForDDNS_EmptyOldConfig(t *testing.T) {
	newConf := DDNSConfig{
		DDNSEnabled: true,
		DDNS: []DNSGroup{
			{
				ID:           "dns-1",
				AccessKey:    "LTAI5tNewKeyValue123",
				AccessSecret: "NewSecret1234567890AbCdEfGh12",
			},
		},
	}
	oldConf := DDNSConfig{
		DDNSEnabled: false,
		DDNS:        []DNSGroup{},
	}

	result := RestoreSensitiveFieldsForDDNS(newConf, oldConf)

	if result.DDNS[0].AccessKey != "LTAI5tNewKeyValue123" {
		t.Errorf("AccessKey = %q, want %q", result.DDNS[0].AccessKey, "LTAI5tNewKeyValue123")
	}
	if result.DDNS[0].AccessSecret != "NewSecret1234567890AbCdEfGh12" {
		t.Errorf("AccessSecret = %q, want %q", result.DDNS[0].AccessSecret, "NewSecret1234567890AbCdEfGh12")
	}
}

// TestRestoreSensitiveFieldsForDDNS_MultipleGroups 测试多个 DNS 配置组
func TestRestoreSensitiveFieldsForDDNS_MultipleGroups(t *testing.T) {
	newConf := DDNSConfig{
		DDNSEnabled: true,
		DDNS: []DNSGroup{
			{
				ID:           "dns-1",
				AccessKey:    "LTAI********GhIj",                // 未修改
				AccessSecret: "3yq7***********************u5Eg", // 未修改
			},
			{
				ID:           "dns-2",
				AccessKey:    "LTAI5tNewKeyValue123", // 修改了
				AccessSecret: "NewSecret1234567890AbCdEfGh12",
			},
		},
	}
	oldConf := DDNSConfig{
		DDNSEnabled: true,
		DDNS: []DNSGroup{
			{
				ID:           "dns-1",
				AccessKey:    "LTAI5tAbCdEfGhIj",
				AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
			},
			{
				ID:           "dns-2",
				AccessKey:    "LTAI5tOldKeyValue456",
				AccessSecret: "OldSecret0987654321ZyXwVuTs10",
			},
		},
	}

	result := RestoreSensitiveFieldsForDDNS(newConf, oldConf)

	// dns-1: 未修改，应该恢复原始值
	if result.DDNS[0].AccessKey != "LTAI5tAbCdEfGhIj" {
		t.Errorf("DNS-1 AccessKey = %q, want %q", result.DDNS[0].AccessKey, "LTAI5tAbCdEfGhIj")
	}
	if result.DDNS[0].AccessSecret != "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg" {
		t.Errorf("DNS-1 AccessSecret = %q, want %q", result.DDNS[0].AccessSecret, "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg")
	}

	// dns-2: 修改了，应该使用新值
	if result.DDNS[1].AccessKey != "LTAI5tNewKeyValue123" {
		t.Errorf("DNS-2 AccessKey = %q, want %q", result.DDNS[1].AccessKey, "LTAI5tNewKeyValue123")
	}
	if result.DDNS[1].AccessSecret != "NewSecret1234567890AbCdEfGh12" {
		t.Errorf("DNS-2 AccessSecret = %q, want %q", result.DDNS[1].AccessSecret, "NewSecret1234567890AbCdEfGh12")
	}
}

// TestGetDDNSConfigJSON 测试 DDNS 配置 JSON 序列化（带脱敏）
func TestGetDDNSConfigJSON(t *testing.T) {
	t.Run("nil DDNS 数组应序列化为空数组", func(t *testing.T) {
		conf := DDNSConfig{
			DDNSEnabled: false,
			DDNS:        nil,
		}
		got := GetDDNSConfigJSON(conf)

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(got), &parsed); err != nil {
			t.Fatalf("JSON 解析失败: %v", err)
		}
		ddnsArr, ok := parsed["ddns"].([]interface{})
		if !ok {
			t.Fatal("ddns 字段不是数组")
		}
		if len(ddnsArr) != 0 {
			t.Errorf("ddns 数组应为空, got len=%d", len(ddnsArr))
		}
	})

	t.Run("敏感字段应被脱敏", func(t *testing.T) {
		conf := DDNSConfig{
			DDNSEnabled: true,
			DDNS: []DNSGroup{
				{
					ID:           "dns-1",
					Name:         "测试DNS",
					Domain:       "example.com",
					Service:      "alidns",
					AccessKey:    "LTAI5tAbCdEfGhIj",
					AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
					TTL:          "600",
				},
			},
		}
		got := GetDDNSConfigJSON(conf)

		// 不应包含原始密钥
		if contains(got, "LTAI5tAbCdEfGhIj") {
			t.Error("JSON 不应包含原始 AccessKey")
		}
		if contains(got, "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg") {
			t.Error("JSON 不应包含原始 AccessSecret")
		}

		// 应包含脱敏后的 * 字符
		if !contains(got, "****") {
			t.Error("JSON 应包含脱敏后的 * 字符")
		}
	})

	t.Run("非敏感字段应原样保留", func(t *testing.T) {
		conf := DDNSConfig{
			DDNSEnabled: true,
			DDNS: []DNSGroup{
				{
					ID:      "dns-1",
					Name:    "MyDNS",
					Domain:  "example.com",
					Service: "alidns",
					TTL:     "300",
					Records: []DNSRecord{
						{Type: "A", IPType: "static_ipv4", Value: "1.2.3.4"},
					},
				},
			},
		}
		got := GetDDNSConfigJSON(conf)

		if !contains(got, "dns-1") {
			t.Error("JSON 应包含 ID")
		}
		if !contains(got, "MyDNS") {
			t.Error("JSON 应包含 Name")
		}
		if !contains(got, "example.com") {
			t.Error("JSON 应包含 Domain")
		}
	})

	t.Run("原始配置不应被修改", func(t *testing.T) {
		conf := DDNSConfig{
			DDNSEnabled: true,
			DDNS: []DNSGroup{
				{
					ID:           "dns-1",
					AccessKey:    "LTAI5tAbCdEfGhIj",
					AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
				},
			},
		}
		_ = GetDDNSConfigJSON(conf)

		// 原始配置不应被脱敏
		if conf.DDNS[0].AccessKey != "LTAI5tAbCdEfGhIj" {
			t.Error("GetDDNSConfigJSON 不应修改原始配置的 AccessKey")
		}
		if conf.DDNS[0].AccessSecret != "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg" {
			t.Error("GetDDNSConfigJSON 不应修改原始配置的 AccessSecret")
		}
	})
}

// TestGetDCDNConfigJSON 测试 DCDN 配置 JSON 序列化（带脱敏）
func TestGetDCDNConfigJSON(t *testing.T) {
	t.Run("nil DCDN 数组应序列化为空数组", func(t *testing.T) {
		conf := DCDNConfig{
			DCDNEnabled: false,
			DCDN:        nil,
		}
		got := GetDCDNConfigJSON(conf)

		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(got), &parsed); err != nil {
			t.Fatalf("JSON 解析失败: %v", err)
		}
		dcdnArr, ok := parsed["dcdn"].([]interface{})
		if !ok {
			t.Fatal("dcdn 字段不是数组")
		}
		if len(dcdnArr) != 0 {
			t.Errorf("dcdn 数组应为空, got len=%d", len(dcdnArr))
		}
	})

	t.Run("敏感字段应被脱敏", func(t *testing.T) {
		conf := DCDNConfig{
			DCDNEnabled: true,
			DCDN: []CDN{
				{
					ID:           "cdn-1",
					Name:         "测试CDN",
					Domain:       "cdn.example.com",
					Service:      "aliyun",
					AccessKey:    "LTAI5tAbCdEfGhIj",
					AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
				},
			},
		}
		got := GetDCDNConfigJSON(conf)

		if contains(got, "LTAI5tAbCdEfGhIj") {
			t.Error("JSON 不应包含原始 AccessKey")
		}
		if contains(got, "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg") {
			t.Error("JSON 不应包含原始 AccessSecret")
		}
		if !contains(got, "****") {
			t.Error("JSON 应包含脱敏后的 * 字符")
		}
	})

	t.Run("原始配置不应被修改", func(t *testing.T) {
		conf := DCDNConfig{
			DCDNEnabled: true,
			DCDN: []CDN{
				{
					ID:           "cdn-1",
					AccessKey:    "LTAI5tAbCdEfGhIj",
					AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
				},
			},
		}
		_ = GetDCDNConfigJSON(conf)

		if conf.DCDN[0].AccessKey != "LTAI5tAbCdEfGhIj" {
			t.Error("GetDCDNConfigJSON 不应修改原始配置的 AccessKey")
		}
		if conf.DCDN[0].AccessSecret != "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg" {
			t.Error("GetDCDNConfigJSON 不应修改原始配置的 AccessSecret")
		}
	})
}

// TestBuildDNSConfig 测试 BuildDNSConfig 函数
func TestBuildDNSConfig(t *testing.T) {
	group := DNSGroup{
		ID:           "grp-1",
		Name:         "My Group",
		Domain:       "example.com",
		Service:      "alidns",
		AccessKey:    "myAccessKey",
		AccessSecret: "myAccessSecret",
		TTL:          "600",
	}
	record := DNSRecord{
		Type:   "A",
		IPType: "dynamic_ipv4_url",
		Value:  "http://ip.example.com",
		Regex:  "",
	}

	result := group.BuildDNSConfig(&record)

	if result.ID != group.ID {
		t.Errorf("ID = %q, want %q", result.ID, group.ID)
	}
	if result.Name != group.Name {
		t.Errorf("Name = %q, want %q", result.Name, group.Name)
	}
	if result.Domain != group.Domain {
		t.Errorf("Domain = %q, want %q", result.Domain, group.Domain)
	}
	if result.Service != group.Service {
		t.Errorf("Service = %q, want %q", result.Service, group.Service)
	}
	if result.AccessKey != group.AccessKey {
		t.Errorf("AccessKey = %q, want %q", result.AccessKey, group.AccessKey)
	}
	if result.AccessSecret != group.AccessSecret {
		t.Errorf("AccessSecret = %q, want %q", result.AccessSecret, group.AccessSecret)
	}
	if result.TTL != group.TTL {
		t.Errorf("TTL = %q, want %q", result.TTL, group.TTL)
	}
	if result.Type != record.Type {
		t.Errorf("Type = %q, want %q", result.Type, record.Type)
	}
	if result.IPType != record.IPType {
		t.Errorf("IPType = %q, want %q", result.IPType, record.IPType)
	}
	if result.Value != record.Value {
		t.Errorf("Value = %q, want %q", result.Value, record.Value)
	}
	if result.Regex != record.Regex {
		t.Errorf("Regex = %q, want %q", result.Regex, record.Regex)
	}
}

// TestBuildDNSConfig_DifferentRecordTypes 测试不同记录类型的 BuildDNSConfig
func TestBuildDNSConfig_DifferentRecordTypes(t *testing.T) {
	group := DNSGroup{
		ID:     "grp-1",
		Domain: "example.com",
	}

	recordTypes := []struct {
		recType string
		ipType  string
		value   string
		regex   string
	}{
		{"A", "static_ipv4", "1.2.3.4", ""},
		{"AAAA", "dynamic_ipv6_interface", "eth0", `2001:.*`},
		{"CNAME", "", "target.example.com", ""},
		{"TXT", "", "v=spf1 include:example.com ~all", ""},
	}

	for _, rt := range recordTypes {
		record := DNSRecord{
			Type:   rt.recType,
			IPType: rt.ipType,
			Value:  rt.value,
			Regex:  rt.regex,
		}
		result := group.BuildDNSConfig(&record)

		if result.Type != rt.recType {
			t.Errorf("Type = %q, want %q", result.Type, rt.recType)
		}
		if result.Value != rt.value {
			t.Errorf("Value = %q, want %q", result.Value, rt.value)
		}
		if result.Regex != rt.regex {
			t.Errorf("Regex = %q, want %q", result.Regex, rt.regex)
		}
	}
}

// contains 辅助函数
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
