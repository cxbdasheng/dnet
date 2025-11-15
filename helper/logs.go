package helper

import (
	"fmt"
	"sync"
	"time"
)

// LogLevel 日志级别
type LogLevel string

const (
	LogLevelDEBUG LogLevel = "DEBUG"
	LogLevelINFO  LogLevel = "INFO"
	LogLevelWARN  LogLevel = "WARN"
	LogLevelERROR LogLevel = "ERROR"
)

var MaxSize = 100

// LogType 日志类型
type LogType string

const (
	LogTypeSystem  LogType = "系统"
	LogTypeDCDN    LogType = "DCDN"
	LogTypeDDNS    LogType = "DDNS"
	LogTypeWebhook LogType = "Webhook"
	LogTypeAuth    LogType = "认证"
	LogTypeNetwork LogType = "网络"
	LogTypeConfig  LogType = "配置"
)

// LogEntry 日志条目
type LogEntry struct {
	Timestamp string   `json:"timestamp"` // 时间戳
	Level     LogLevel `json:"level"`     // 日志级别
	Type      LogType  `json:"type"`      // 日志类型
	Message   string   `json:"message"`   // 日志消息
}

// Logger 日志管理器
type Logger struct {
	mu      sync.RWMutex
	logs    []LogEntry
	maxSize int  // 最大日志条数
	enabled bool // 是否启用日志记录
}

var (
	// DefaultLogger 全局默认日志实例
	DefaultLogger *Logger
	once          sync.Once
)

// InitLogger 初始化日志系统
func InitLogger(maxSize int) {
	once.Do(func() {
		if maxSize <= 0 {
			maxSize = MaxSize
		}
		DefaultLogger = &Logger{
			logs:    make([]LogEntry, 0, maxSize),
			maxSize: maxSize,
			enabled: true,
		}
	})
}

// GetLogger 获取全局日志实例
func GetLogger() *Logger {
	if DefaultLogger == nil {
		InitLogger(MaxSize)
	}
	return DefaultLogger
}

// addLog 添加日志（内部方法）
func (l *Logger) addLog(level LogLevel, logType LogType, format string, args ...interface{}) {
	if !l.enabled {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	// 格式化消息
	message := fmt.Sprintf(format, args...)

	// 创建日志条目
	entry := LogEntry{
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
		Type:      logType,
		Level:     level,
		Message:   message,
	}

	// 如果超过最大条数，删除最旧的日志
	if len(l.logs) >= l.maxSize {
		// 移除最旧的日志（从头部删除）
		l.logs = l.logs[1:]
	}

	// 添加新日志到尾部
	l.logs = append(l.logs, entry)
}

// Debug 记录调试日志
func (l *Logger) Debug(logType LogType, format string, args ...interface{}) {
	l.addLog(LogLevelDEBUG, logType, format, args...)
}

// Info 记录信息日志
func (l *Logger) Info(logType LogType, format string, args ...interface{}) {
	l.addLog(LogLevelINFO, logType, format, args...)
}

// Warn 记录警告日志
func (l *Logger) Warn(logType LogType, format string, args ...interface{}) {
	l.addLog(LogLevelWARN, logType, format, args...)
}

// Error 记录错误日志
func (l *Logger) Error(logType LogType, format string, args ...interface{}) {
	l.addLog(LogLevelERROR, logType, format, args...)
}

// GetLogs 获取所有日志（返回副本）
func (l *Logger) GetLogs() []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// 返回日志副本，避免外部修改
	logsCopy := make([]LogEntry, len(l.logs))
	copy(logsCopy, l.logs)
	return logsCopy
}

// GetRecentLogs 获取最近的N条日志
func (l *Logger) GetRecentLogs(n int) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if n <= 0 || n > len(l.logs) {
		n = len(l.logs)
	}

	// 获取最后N条日志
	start := len(l.logs) - n
	logsCopy := make([]LogEntry, n)
	copy(logsCopy, l.logs[start:])
	return logsCopy
}

// GetLogsByLevel 根据日志级别获取日志
func (l *Logger) GetLogsByLevel(level LogLevel) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	filtered := make([]LogEntry, 0)
	for _, log := range l.logs {
		if log.Level == level {
			filtered = append(filtered, log)
		}
	}
	return filtered
}

// GetLogsByType 根据日志类型获取日志
func (l *Logger) GetLogsByType(logType LogType) []LogEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	filtered := make([]LogEntry, 0)
	for _, log := range l.logs {
		if log.Type == logType {
			filtered = append(filtered, log)
		}
	}
	return filtered
}

// Clear 清空所有日志
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = make([]LogEntry, 0, l.maxSize)
}

// GetCount 获取当前日志条数
func (l *Logger) GetCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.logs)
}

// SetEnabled 设置是否启用日志记录
func (l *Logger) SetEnabled(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.enabled = enabled
}

// IsEnabled 检查日志记录是否启用
func (l *Logger) IsEnabled() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.enabled
}

// SetMaxSize 设置最大日志条数
func (l *Logger) SetMaxSize(maxSize int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if maxSize <= 0 {
		maxSize = 1000
	}

	l.maxSize = maxSize

	// 如果当前日志数超过新的最大值，删除旧日志
	if len(l.logs) > maxSize {
		l.logs = l.logs[len(l.logs)-maxSize:]
	}
}

// GetMaxSize 获取最大日志条数
func (l *Logger) GetMaxSize() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.maxSize
}

// 全局便捷方法

// Debug 全局调试日志
func Debug(logType LogType, format string, args ...interface{}) {
	GetLogger().Debug(logType, format, args...)
}

// Info 全局信息日志
func Info(logType LogType, format string, args ...interface{}) {
	GetLogger().Info(logType, format, args...)
}

// Warn 全局警告日志
func Warn(logType LogType, format string, args ...interface{}) {
	GetLogger().Warn(logType, format, args...)
}

// Error 全局错误日志
func Error(logType LogType, format string, args ...interface{}) {
	GetLogger().Error(logType, format, args...)
}

// GetAllLogs 获取所有日志
func GetAllLogs() []LogEntry {
	return GetLogger().GetLogs()
}

// ClearLogs 清空所有日志
func ClearLogs() {
	GetLogger().Clear()
}
