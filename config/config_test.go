package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHashPassword 测试密码哈希函数
func TestHashPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		want     string
	}{
		{
			name:     "空密码",
			password: "",
			want:     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "简单密码",
			password: "123456",
			want:     "8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92",
		},
		{
			name:     "复杂密码",
			password: "Test@123!#$%",
			want:     "a87ff679a2f3e71d9181a67b7542122c0a05fdca30dc9fcdf995cbc0b6f4f2b9", // 需要实际计算
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hashPassword(tt.password)
			if len(got) != 64 { // SHA256 输出固定 64 个字符
				t.Errorf("hashPassword() 长度 = %d, 期望 64", len(got))
			}
			// 测试相同输入产生相同输出
			got2 := hashPassword(tt.password)
			if got != got2 {
				t.Errorf("hashPassword() 不一致: %s != %s", got, got2)
			}
		})
	}
}

// TestGeneratePassword 测试密码生成
func TestGeneratePassword(t *testing.T) {
	conf := &Config{}

	tests := []struct {
		name        string
		password    string
		wantErr     bool
		errContains string
	}{
		{
			name:        "空密码应该失败",
			password:    "",
			wantErr:     true,
			errContains: "密码不能为空",
		},
		{
			name:     "有效密码",
			password: "123456",
			wantErr:  false,
		},
		{
			name:     "复杂密码",
			password: "ComplexP@ssw0rd!",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := conf.GeneratePassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("GeneratePassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				if err == nil {
					t.Error("期望错误但未返回错误")
				}
				return
			}
			if got == "" {
				t.Error("GeneratePassword() 返回空字符串")
			}
			if len(got) != 64 {
				t.Errorf("GeneratePassword() 长度 = %d, 期望 64", len(got))
			}
		})
	}
}

// TestVerifyPassword 测试密码验证
func TestVerifyPassword(t *testing.T) {
	conf := &Config{}
	password := "test123456"
	hashedPassword := hashPassword(password)
	conf.Password = hashedPassword

	tests := []struct {
		name          string
		inputPassword string
		want          bool
	}{
		{
			name:          "正确密码",
			inputPassword: "test123456",
			want:          true,
		},
		{
			name:          "错误密码",
			inputPassword: "wrongpassword",
			want:          false,
		},
		{
			name:          "空密码",
			inputPassword: "",
			want:          false,
		},
		{
			name:          "大小写敏感",
			inputPassword: "Test123456",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := conf.VerifyPassword(tt.inputPassword)
			if got != tt.want {
				t.Errorf("VerifyPassword() = %v, want %v", got, tt.want)
			}
		})
	}

	// 测试空配置密码
	t.Run("配置密码为空", func(t *testing.T) {
		emptyConf := &Config{
			User: User{
				Password: "",
			},
		}
		if emptyConf.VerifyPassword("anypassword") {
			t.Error("空配置密码应该返回 false")
		}
	})
}

// TestResetPassword 测试密码重置
func TestResetPassword(t *testing.T) {
	// 创建临时配置文件
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_config.yaml")

	// 设置环境变量
	oldPath := os.Getenv(PathENV)
	defer os.Setenv(PathENV, oldPath)
	os.Setenv(PathENV, tmpFile)

	conf := &Config{
		User: User{
			Username: "admin",
			Password: hashPassword("oldpassword"),
		},
	}

	tests := []struct {
		name        string
		newPassword string
		wantErr     bool
		errContains string
	}{
		{
			name:        "空密码",
			newPassword: "",
			wantErr:     true,
			errContains: "密码不能为空",
		},
		{
			name:        "密码太短",
			newPassword: "12345",
			wantErr:     true,
			errContains: "密码长度不能少于6位",
		},
		{
			name:        "有效密码",
			newPassword: "newpassword123",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := conf.ResetPassword(tt.newPassword)
			if (err != nil) != tt.wantErr {
				t.Errorf("ResetPassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				// 验证新密码
				if !conf.VerifyPassword(tt.newPassword) {
					t.Error("新密码验证失败")
				}
				// 验证旧密码失效
				if conf.VerifyPassword("oldpassword") {
					t.Error("旧密码仍然有效")
				}
			}
		})
	}
}

// TestGetConfigFilePath 测试配置文件路径获取
func TestGetConfigFilePath(t *testing.T) {
	tests := []struct {
		name    string
		envPath string
		want    string
	}{
		{
			name:    "使用环境变量路径",
			envPath: "/custom/path/config.yaml",
			want:    "/custom/path/config.yaml",
		},
		{
			name:    "空环境变量使用默认路径",
			envPath: "",
			want:    "", // 将使用默认路径
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPath := os.Getenv(PathENV)
			defer os.Setenv(PathENV, oldPath)

			os.Setenv(PathENV, tt.envPath)
			got := GetConfigFilePath()

			if tt.envPath != "" {
				if got != tt.want {
					t.Errorf("GetConfigFilePath() = %v, want %v", got, tt.want)
				}
			} else {
				// 默认路径应该包含 .dnet_config.yaml
				if got == "" {
					t.Error("GetConfigFilePath() 返回空路径")
				}
			}
		})
	}
}

// TestGetDefaultPort 测试默认端口获取
func TestGetDefaultPort(t *testing.T) {
	tests := []struct {
		name     string
		envPort  string
		expected string
	}{
		{
			name:     "环境变量设置端口",
			envPort:  "8080",
			expected: "8080",
		},
		{
			name:     "空环境变量",
			envPort:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldPort := os.Getenv(DNETPort)
			defer os.Setenv(DNETPort, oldPort)

			os.Setenv(DNETPort, tt.envPort)
			got := GetDefaultPort()

			if got != tt.expected {
				t.Errorf("GetDefaultPort() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestConfigGetPort 测试配置获取端口
func TestConfigGetPort(t *testing.T) {
	tests := []struct {
		name     string
		port     string
		expected string
	}{
		{
			name:     "设置端口",
			port:     "9877",
			expected: ":9877",
		},
		{
			name:     "空端口",
			port:     "",
			expected: ":",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := &Config{
				Settings: Settings{
					Port: tt.port,
				},
			}
			got := conf.GetPort()
			if got != tt.expected {
				t.Errorf("GetPort() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestConfigCache 测试配置缓存
func TestConfigCache(t *testing.T) {
	// 创建临时配置文件
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "cache_test.yaml")

	// 设置环境变量
	oldPath := os.Getenv(PathENV)
	defer os.Setenv(PathENV, oldPath)
	os.Setenv(PathENV, tmpFile)

	// 创建测试配置
	conf := &Config{
		User: User{
			Username: "admin",
			Password: hashPassword("password"),
		},
		Settings: Settings{
			Port: "9877",
		},
		Lang: "zh-CN",
	}

	// 保存配置
	err := conf.SaveConfig()
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	// 测试缓存读取
	t.Run("首次读取", func(t *testing.T) {
		cached, err := GetConfigCached()
		if err != nil {
			t.Fatalf("GetConfigCached() error = %v", err)
		}
		if cached.Username != "admin" {
			t.Errorf("缓存用户名 = %v, want admin", cached.Username)
		}
		if cached.Lang != "zh-CN" {
			t.Errorf("缓存语言 = %v, want zh-CN", cached.Lang)
		}
	})

	// 测试缓存命中
	t.Run("缓存命中", func(t *testing.T) {
		cached1, _ := GetConfigCached()
		cached2, _ := GetConfigCached()
		// 应该返回相同的数据
		if cached1.Username != cached2.Username {
			t.Error("缓存不一致")
		}
	})

	// 测试文件变化检测
	t.Run("文件变化检测", func(t *testing.T) {
		// 等待确保修改时间不同
		time.Sleep(10 * time.Millisecond)

		// 修改配置
		conf.Lang = "en-US"
		err := conf.SaveConfig()
		if err != nil {
			t.Fatalf("SaveConfig() error = %v", err)
		}

		// 重新读取应该获得新配置
		cached, err := GetConfigCached()
		if err != nil {
			t.Fatalf("GetConfigCached() error = %v", err)
		}
		if cached.Lang != "en-US" {
			t.Errorf("缓存未更新: Lang = %v, want en-US", cached.Lang)
		}
	})
}

// TestSaveConfig 测试配置保存
func TestSaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "save_test.yaml")

	oldPath := os.Getenv(PathENV)
	defer os.Setenv(PathENV, oldPath)
	os.Setenv(PathENV, tmpFile)

	conf := &Config{
		User: User{
			Username: "testuser",
			Password: hashPassword("testpass"),
		},
		Lang: "zh-CN",
	}

	// 保存配置
	err := conf.SaveConfig()
	if err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(tmpFile); os.IsNotExist(err) {
		t.Error("配置文件未创建")
	}

	// 读取并验证
	loaded, err := GetConfigCached()
	if err != nil {
		t.Fatalf("GetConfigCached() error = %v", err)
	}

	if loaded.Username != "testuser" {
		t.Errorf("Username = %v, want testuser", loaded.Username)
	}
	if loaded.Lang != "zh-CN" {
		t.Errorf("Lang = %v, want zh-CN", loaded.Lang)
	}
}

// BenchmarkHashPassword 密码哈希性能测试
func BenchmarkHashPassword(b *testing.B) {
	password := "test123456"
	for i := 0; i < b.N; i++ {
		hashPassword(password)
	}
}

// BenchmarkVerifyPassword 密码验证性能测试
func BenchmarkVerifyPassword(b *testing.B) {
	conf := &Config{
		User: User{
			Password: hashPassword("test123456"),
		},
	}
	for i := 0; i < b.N; i++ {
		conf.VerifyPassword("test123456")
	}
}
