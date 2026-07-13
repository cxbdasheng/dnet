package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cxbdasheng/dnet/helper"
	"gopkg.in/yaml.v3"
)

const PathENV = "DNET_CONFIG_FILE_PATH"

const DNETPort = "DNET_PORT"

// 全局默认值：CLI flag、后端填充、模板展示均引用这些常量，作为唯一事实来源
const (
	DefaultEvery      = 300 // 同步循环间隔（秒）
	DefaultCacheTimes = 5   // DCDN / DDNS 强制同步计数器初始值
)

// CLI 显式传入时写入的环境变量，供 bootstrap / web 判断字段是否被命令行锁定
// 存在 = 已锁定，值 = CLI 传入的生效值
const (
	CLIEveryENV          = "DNET_CLI_EVERY"
	CLIDCDNCacheTimesENV = "DNET_CLI_DCDN_CACHE_TIMES"
	CLIDDNSCacheTimesENV = "DNET_CLI_DDNS_CACHE_TIMES"
)

func GetConfigFilePathDefault() string {
	dir, err := os.UserHomeDir()
	if err != nil {
		return "../.dnet_config.yaml"
	}
	return dir + string(os.PathSeparator) + ".dnet_config.yaml"
}

// GetDefaultPort 获取默认端口
func GetDefaultPort() string {
	return os.Getenv(DNETPort)
}

// GetConfigFilePath 获得配置文件路径
func GetConfigFilePath() string {
	configFilePath := os.Getenv(PathENV)
	if configFilePath != "" {
		return configFilePath
	}
	return GetConfigFilePathDefault()
}

type Config struct {
	Settings
	User
	Webhook
	DCDNConfig
	DDNSConfig
	// 语言
	Lang string
}

// ConfigCache 配置缓存结构
type ConfigCache struct {
	config   *Config
	err      error
	mu       sync.RWMutex
	filePath string
	modTime  time.Time
}

var globalCache = &ConfigCache{}

// GetConfigCached 获取缓存的配置，支持文件变化检测
func GetConfigCached() (Config, error) {
	globalCache.mu.RLock()
	configFilePath := GetConfigFilePath()

	// 检查文件是否改变
	if globalCache.config != nil && globalCache.filePath == configFilePath {
		if stat, err := os.Stat(configFilePath); err == nil {
			if !stat.ModTime().After(globalCache.modTime) {
				// 文件未改变，返回缓存
				defer globalCache.mu.RUnlock()
				return *globalCache.config, globalCache.err
			}
		}
	}
	globalCache.mu.RUnlock()

	// 需要重新加载配置
	return globalCache.loadConfig(configFilePath)
}

// loadConfig 加载配置文件
func (c *ConfigCache) loadConfig(configFilePath string) (Config, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 再次检查，避免重复加载
	if c.config != nil && c.filePath == configFilePath {
		if stat, err := os.Stat(configFilePath); err == nil {
			if !stat.ModTime().After(c.modTime) {
				return *c.config, c.err
			}
		}
	}

	c.config = &Config{}
	c.filePath = configFilePath

	stat, err := os.Stat(configFilePath)
	if err != nil {
		c.err = err
		return *c.config, err
	}
	c.modTime = stat.ModTime()

	data, err := os.ReadFile(configFilePath)
	if err != nil {
		c.err = err
		return *c.config, err
	}

	if err = yaml.Unmarshal(data, c.config); err != nil {
		c.err = err
		return *c.config, err
	}

	c.err = nil
	return *c.config, nil
}

// SaveConfig 保存配置
func (conf *Config) SaveConfig() error {
	globalCache.mu.Lock()
	defer globalCache.mu.Unlock()

	data, err := yaml.Marshal(conf)
	if err != nil {
		helper.Error(helper.LogTypeConfig, "序列化配置失败: %v", err)
		return err
	}

	configFilePath := GetConfigFilePath()
	if err = os.WriteFile(configFilePath, data, 0600); err != nil {
		helper.Error(helper.LogTypeConfig, "写入配置文件失败: %v", err)
		return err
	}

	// 更新缓存
	globalCache.config = conf
	globalCache.filePath = configFilePath
	globalCache.err = nil
	if stat, err := os.Stat(configFilePath); err == nil {
		globalCache.modTime = stat.ModTime()
	}
	helper.Info(helper.LogTypeConfig, "配置文件已保存在: %s", configFilePath)
	return nil
}
func (conf *Config) GetPort() string {
	if conf.Settings.Port != "" {
		return ":" + conf.Settings.Port
	}
	return ":"
}
func (conf *Config) GeneratePassword(newPassword string) (string, error) {
	if newPassword == "" {
		return "", fmt.Errorf("密码不能为空")
	}

	// 验证密码强度
	//if len(newPassword) < 6 {
	//	return "", fmt.Errorf("密码长度不能少于6位")
	//}

	// 使用SHA256加密密码
	hashedPassword := hashPassword(newPassword)
	return hashedPassword, nil
}

// ResetPassword 重置密码
func (conf *Config) ResetPassword(newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("密码不能为空")
	}

	// 验证密码强度
	if len(newPassword) < 6 {
		return fmt.Errorf("密码长度不能少于6位")
	}

	// 使用SHA256加密密码
	hashedPassword := hashPassword(newPassword)
	conf.Password = hashedPassword

	// 保存配置到文件
	err := conf.SaveConfig()
	if err != nil {
		return fmt.Errorf("保存密码失败: %v", err)
	}

	helper.Info(helper.LogTypeConfig, "密码重置成功")
	return nil
}

// VerifyPassword 验证密码
func (conf *Config) VerifyPassword(inputPassword string) bool {
	if inputPassword == "" || conf.Password == "" {
		return false
	}

	hashedInput := hashPassword(inputPassword)
	return hashedInput == conf.Password
}

// hashPassword 对密码进行SHA256加密
func hashPassword(password string) string {
	hash := sha256.Sum256([]byte(password))
	return fmt.Sprintf("%x", hash)
}
