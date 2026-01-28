package config

import (
	"testing"
)

// TestMaskSensitiveString 测试敏感字符串脱敏函数
func TestMaskSensitiveString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "空字符串",
			input:    "",
			expected: "",
		},
		{
			name:     "长度为1",
			input:    "a",
			expected: "*",
		},
		{
			name:     "长度为8",
			input:    "12345678",
			expected: "********",
		},
		{
			name:     "长度为9-刚好开始保留前后4位",
			input:    "123456789",
			expected: "1234*6789",
		},
		{
			name:     "长度为16-正常AccessKey长度",
			input:    "LTAI5tAbCdEfGhIj",
			expected: "LTAI********GhIj",
		},
		{
			name:     "长度为35-正常AccessSecret长度",
			input:    "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg", // 35个字符
			expected: "3yq7***********************u5Eg",
		},
		{
			name:     "包含特殊字符",
			input:    "Key@#$%^&*()_+123456",
			expected: "Key@************3456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskSensitiveString(tt.input)
			if result != tt.expected {
				t.Errorf("maskSensitiveString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestRestoreSensitiveFields 测试恢复敏感字段函数
func TestRestoreSensitiveFields(t *testing.T) {
	tests := []struct {
		name         string
		newConf      DCDNConfig
		oldConf      DCDNConfig
		expectKey    string
		expectSecret string
	}{
		{
			name: "未修改-应该恢复原始值",
			newConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI********GhIj", // 脱敏数据
						AccessSecret: "3yq7***********************u5Eg",
					},
				},
			},
			oldConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI5tAbCdEfGhIj", // 原始数据
						AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
					},
				},
			},
			expectKey:    "LTAI5tAbCdEfGhIj", // 应该恢复原始值
			expectSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
		},
		{
			name: "修改了AccessKey-应该使用新值",
			newConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI5tNewKeyValue123", // 新的完整值
						AccessSecret: "3yq7***********************u5Eg",
					},
				},
			},
			oldConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI5tAbCdEfGhIj",
						AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
					},
				},
			},
			expectKey:    "LTAI5tNewKeyValue123",            // 使用新值
			expectSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg", // 恢复原始值
		},
		{
			name: "修改了AccessSecret-应该使用新值",
			newConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI********GhIj",
						AccessSecret: "NewSecret1234567890AbCdEfGh12", // 新的完整值
					},
				},
			},
			oldConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI5tAbCdEfGhIj",
						AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
					},
				},
			},
			expectKey:    "LTAI5tAbCdEfGhIj",              // 恢复原始值
			expectSecret: "NewSecret1234567890AbCdEfGh12", // 使用新值
		},
		{
			name: "全部修改-应该使用新值",
			newConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI5tNewKeyValue123",
						AccessSecret: "NewSecret1234567890AbCdEfGh12",
					},
				},
			},
			oldConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI5tAbCdEfGhIj",
						AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
					},
				},
			},
			expectKey:    "LTAI5tNewKeyValue123",
			expectSecret: "NewSecret1234567890AbCdEfGh12",
		},
		{
			name: "安全漏洞测试-脱敏数据加额外字符",
			newConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI************GhIj123", // 脱敏数据 + 额外字符
						AccessSecret: "3yq7********************5EgXXX",
					},
				},
			},
			oldConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI5tAbCdEfGhIj",
						AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
					},
				},
			},
			expectKey:    "LTAI************GhIj123", // 不应该恢复，使用新值（防止安全漏洞）
			expectSecret: "3yq7********************5EgXXX",
		},
		{
			name: "新增CDN配置-ID不存在于旧配置",
			newConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-2",
						AccessKey:    "LTAI5tNewCDNKey12345",
						AccessSecret: "NewCDNSecret1234567890AbCd",
					},
				},
			},
			oldConf: DCDNConfig{
				DCDNEnabled: true,
				DCDN: []CDN{
					{
						ID:           "cdn-1",
						AccessKey:    "LTAI5tAbCdEfGhIj",
						AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
					},
				},
			},
			expectKey:    "LTAI5tNewCDNKey12345", // 新增配置，使用新值
			expectSecret: "NewCDNSecret1234567890AbCd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RestoreSensitiveFields(tt.newConf, tt.oldConf)

			if len(result.DCDN) == 0 {
				t.Fatal("result.DCDN is empty")
			}

			if result.DCDN[0].AccessKey != tt.expectKey {
				t.Errorf("AccessKey = %q, want %q", result.DCDN[0].AccessKey, tt.expectKey)
			}
			if result.DCDN[0].AccessSecret != tt.expectSecret {
				t.Errorf("AccessSecret = %q, want %q", result.DCDN[0].AccessSecret, tt.expectSecret)
			}
		})
	}
}

// TestRestoreSensitiveFields_EmptyOldConfig 测试旧配置为空的情况
func TestRestoreSensitiveFields_EmptyOldConfig(t *testing.T) {
	newConf := DCDNConfig{
		DCDNEnabled: true,
		DCDN: []CDN{
			{
				ID:           "cdn-1",
				AccessKey:    "LTAI5tNewKeyValue123",
				AccessSecret: "NewSecret1234567890AbCdEfGh12",
			},
		},
	}
	oldConf := DCDNConfig{
		DCDNEnabled: false,
		DCDN:        []CDN{}, // 空数组
	}

	result := RestoreSensitiveFields(newConf, oldConf)

	// 旧配置为空，应该直接使用新值
	if result.DCDN[0].AccessKey != "LTAI5tNewKeyValue123" {
		t.Errorf("AccessKey = %q, want %q", result.DCDN[0].AccessKey, "LTAI5tNewKeyValue123")
	}
	if result.DCDN[0].AccessSecret != "NewSecret1234567890AbCdEfGh12" {
		t.Errorf("AccessSecret = %q, want %q", result.DCDN[0].AccessSecret, "NewSecret1234567890AbCdEfGh12")
	}
}

// TestRestoreSensitiveFields_MultipleConfigs 测试多个CDN配置
func TestRestoreSensitiveFields_MultipleConfigs(t *testing.T) {
	newConf := DCDNConfig{
		DCDNEnabled: true,
		DCDN: []CDN{
			{
				ID:           "cdn-1",
				AccessKey:    "LTAI********GhIj", // 未修改
				AccessSecret: "3yq7***********************u5Eg",
			},
			{
				ID:           "cdn-2",
				AccessKey:    "LTAI5tNewKeyValue123", // 修改了
				AccessSecret: "NewSecret1234567890AbCdEfGh12",
			},
		},
	}
	oldConf := DCDNConfig{
		DCDNEnabled: true,
		DCDN: []CDN{
			{
				ID:           "cdn-1",
				AccessKey:    "LTAI5tAbCdEfGhIj",
				AccessSecret: "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg",
			},
			{
				ID:           "cdn-2",
				AccessKey:    "LTAI5tOldKeyValue456",
				AccessSecret: "OldSecret0987654321ZyXwVuTs10",
			},
		},
	}

	result := RestoreSensitiveFields(newConf, oldConf)

	// cdn-1: 未修改，应该恢复原始值
	if result.DCDN[0].AccessKey != "LTAI5tAbCdEfGhIj" {
		t.Errorf("CDN-1 AccessKey = %q, want %q", result.DCDN[0].AccessKey, "LTAI5tAbCdEfGhIj")
	}
	if result.DCDN[0].AccessSecret != "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg" {
		t.Errorf("CDN-1 AccessSecret = %q, want %q", result.DCDN[0].AccessSecret, "3yq7XaB9kFdP8sL2VnM1RtC4WzYu5Eg")
	}

	// cdn-2: 修改了，应该使用新值
	if result.DCDN[1].AccessKey != "LTAI5tNewKeyValue123" {
		t.Errorf("CDN-2 AccessKey = %q, want %q", result.DCDN[1].AccessKey, "LTAI5tNewKeyValue123")
	}
	if result.DCDN[1].AccessSecret != "NewSecret1234567890AbCdEfGh12" {
		t.Errorf("CDN-2 AccessSecret = %q, want %q", result.DCDN[1].AccessSecret, "NewSecret1234567890AbCdEfGh12")
	}
}
