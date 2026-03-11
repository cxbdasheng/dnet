package helper

import (
	"sync"
	"testing"
)

// newTestLogger 创建一个用于测试的独立 Logger 实例
func newTestLogger(maxSize int) *Logger {
	return &Logger{
		logs:    make([]LogEntry, 0, maxSize),
		maxSize: maxSize,
		enabled: true,
	}
}

// TestLoggerDebugInfoWarnError 测试各级别日志写入
func TestLoggerDebugInfoWarnError(t *testing.T) {
	tests := []struct {
		name  string
		level LogLevel
		logFn func(l *Logger)
	}{
		{
			name:  "Debug",
			level: LogLevelDEBUG,
			logFn: func(l *Logger) { l.Debug(LogTypeDDNS, "debug message %d", 1) },
		},
		{
			name:  "Info",
			level: LogLevelINFO,
			logFn: func(l *Logger) { l.Info(LogTypeSystem, "info message") },
		},
		{
			name:  "Warn",
			level: LogLevelWARN,
			logFn: func(l *Logger) { l.Warn(LogTypeDCDN, "warn message") },
		},
		{
			name:  "Error",
			level: LogLevelERROR,
			logFn: func(l *Logger) { l.Error(LogTypeWebhook, "error message") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := newTestLogger(10)
			tt.logFn(l)

			logs := l.GetLogs()
			if len(logs) != 1 {
				t.Fatalf("GetLogs() len = %d, want 1", len(logs))
			}
			if logs[0].Level != tt.level {
				t.Errorf("Level = %q, want %q", logs[0].Level, tt.level)
			}
		})
	}
}

// TestLoggerMessageFormat 测试日志消息格式化
func TestLoggerMessageFormat(t *testing.T) {
	l := newTestLogger(10)
	l.Info(LogTypeSystem, "hello %s %d", "world", 42)

	logs := l.GetLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	if logs[0].Message != "hello world 42" {
		t.Errorf("Message = %q, want 'hello world 42'", logs[0].Message)
	}
	if logs[0].Type != LogTypeSystem {
		t.Errorf("Type = %q, want %q", logs[0].Type, LogTypeSystem)
	}
	if logs[0].Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
}

// TestLoggerGetLogsReturnsCopy 测试 GetLogs 返回副本
func TestLoggerGetLogsReturnsCopy(t *testing.T) {
	l := newTestLogger(10)
	l.Info(LogTypeSystem, "original")

	logs := l.GetLogs()
	logs[0].Message = "modified"

	// 修改返回的切片不应影响内部日志
	internalLogs := l.GetLogs()
	if internalLogs[0].Message != "original" {
		t.Error("GetLogs() should return a copy, internal state was modified")
	}
}

// TestLoggerMaxSizeBuffer 测试 buffer 溢出时丢弃最旧的日志
func TestLoggerMaxSizeBuffer(t *testing.T) {
	maxSize := 3
	l := newTestLogger(maxSize)

	for i := 1; i <= 5; i++ {
		l.Info(LogTypeSystem, "message %d", i)
	}

	logs := l.GetLogs()
	if len(logs) != maxSize {
		t.Fatalf("GetLogs() len = %d, want %d", len(logs), maxSize)
	}
	// 应保留最新的 3 条（3、4、5）
	if logs[0].Message != "message 3" {
		t.Errorf("oldest retained = %q, want 'message 3'", logs[0].Message)
	}
	if logs[2].Message != "message 5" {
		t.Errorf("newest = %q, want 'message 5'", logs[2].Message)
	}
}

// TestLoggerGetCount 测试日志计数
func TestLoggerGetCount(t *testing.T) {
	l := newTestLogger(10)
	if l.GetCount() != 0 {
		t.Errorf("initial count = %d, want 0", l.GetCount())
	}

	l.Info(LogTypeSystem, "a")
	l.Info(LogTypeSystem, "b")
	if l.GetCount() != 2 {
		t.Errorf("count = %d, want 2", l.GetCount())
	}
}

// TestLoggerClear 测试清空日志
func TestLoggerClear(t *testing.T) {
	l := newTestLogger(10)
	l.Info(LogTypeSystem, "msg1")
	l.Info(LogTypeSystem, "msg2")

	l.Clear()
	if l.GetCount() != 0 {
		t.Errorf("after Clear(), count = %d, want 0", l.GetCount())
	}
	if len(l.GetLogs()) != 0 {
		t.Error("after Clear(), GetLogs() should return empty slice")
	}
}

// TestLoggerGetRecentLogs 测试获取最近 N 条日志
func TestLoggerGetRecentLogs(t *testing.T) {
	l := newTestLogger(10)
	for i := 1; i <= 5; i++ {
		l.Info(LogTypeSystem, "msg %d", i)
	}

	tests := []struct {
		n        int
		expected int
	}{
		{3, 3},
		{5, 5},
		{10, 5}, // 超过总数，返回全部
		{0, 5},  // 0 返回全部
		{-1, 5}, // 负数返回全部
	}

	for _, tt := range tests {
		recent := l.GetRecentLogs(tt.n)
		if len(recent) != tt.expected {
			t.Errorf("GetRecentLogs(%d) len = %d, want %d", tt.n, len(recent), tt.expected)
		}
	}

	// 验证是最近的日志
	recent := l.GetRecentLogs(2)
	if recent[0].Message != "msg 4" || recent[1].Message != "msg 5" {
		t.Errorf("GetRecentLogs(2) = [%q, %q], want ['msg 4', 'msg 5']",
			recent[0].Message, recent[1].Message)
	}
}

// TestLoggerGetLogsByLevel 测试按级别过滤日志
func TestLoggerGetLogsByLevel(t *testing.T) {
	l := newTestLogger(20)
	l.Info(LogTypeSystem, "info 1")
	l.Warn(LogTypeSystem, "warn 1")
	l.Info(LogTypeSystem, "info 2")
	l.Error(LogTypeSystem, "error 1")
	l.Warn(LogTypeSystem, "warn 2")

	infoLogs := l.GetLogsByLevel(LogLevelINFO)
	if len(infoLogs) != 2 {
		t.Errorf("GetLogsByLevel(INFO) len = %d, want 2", len(infoLogs))
	}

	warnLogs := l.GetLogsByLevel(LogLevelWARN)
	if len(warnLogs) != 2 {
		t.Errorf("GetLogsByLevel(WARN) len = %d, want 2", len(warnLogs))
	}

	debugLogs := l.GetLogsByLevel(LogLevelDEBUG)
	if len(debugLogs) != 0 {
		t.Errorf("GetLogsByLevel(DEBUG) len = %d, want 0", len(debugLogs))
	}
}

// TestLoggerGetLogsByType 测试按类型过滤日志
func TestLoggerGetLogsByType(t *testing.T) {
	l := newTestLogger(20)
	l.Info(LogTypeDDNS, "ddns 1")
	l.Info(LogTypeDCDN, "dcdn 1")
	l.Warn(LogTypeDDNS, "ddns 2")
	l.Error(LogTypeSystem, "system 1")

	ddnsLogs := l.GetLogsByType(LogTypeDDNS)
	if len(ddnsLogs) != 2 {
		t.Errorf("GetLogsByType(DDNS) len = %d, want 2", len(ddnsLogs))
	}

	systemLogs := l.GetLogsByType(LogTypeSystem)
	if len(systemLogs) != 1 {
		t.Errorf("GetLogsByType(System) len = %d, want 1", len(systemLogs))
	}

	webhookLogs := l.GetLogsByType(LogTypeWebhook)
	if len(webhookLogs) != 0 {
		t.Errorf("GetLogsByType(Webhook) len = %d, want 0", len(webhookLogs))
	}
}

// TestLoggerSetEnabled 测试启用/禁用日志记录
func TestLoggerSetEnabled(t *testing.T) {
	l := newTestLogger(10)

	l.SetEnabled(false)
	if l.IsEnabled() {
		t.Error("IsEnabled() should return false after SetEnabled(false)")
	}

	l.Info(LogTypeSystem, "should not be recorded")
	if l.GetCount() != 0 {
		t.Errorf("disabled logger recorded %d logs, want 0", l.GetCount())
	}

	l.SetEnabled(true)
	if !l.IsEnabled() {
		t.Error("IsEnabled() should return true after SetEnabled(true)")
	}

	l.Info(LogTypeSystem, "should be recorded")
	if l.GetCount() != 1 {
		t.Errorf("re-enabled logger should record logs, count = %d", l.GetCount())
	}
}

// TestLoggerSetMaxSize 测试动态调整最大日志条数
func TestLoggerSetMaxSize(t *testing.T) {
	l := newTestLogger(10)

	l.SetMaxSize(5)
	if l.GetMaxSize() != 5 {
		t.Errorf("GetMaxSize() = %d, want 5", l.GetMaxSize())
	}

	// 写入 10 条日志后调整为 3，应裁剪旧日志
	for i := 1; i <= 10; i++ {
		l.Info(LogTypeSystem, "msg %d", i)
	}
	l.SetMaxSize(3)
	if l.GetCount() != 3 {
		t.Errorf("after SetMaxSize(3), count = %d, want 3", l.GetCount())
	}

	// 小于等于 0 的值应重置为 1000
	l.SetMaxSize(0)
	if l.GetMaxSize() != 1000 {
		t.Errorf("SetMaxSize(0) should set to 1000, got %d", l.GetMaxSize())
	}
}

// TestLoggerSetConsoleOutput 测试控制台输出开关
func TestLoggerSetConsoleOutput(t *testing.T) {
	l := newTestLogger(10)

	if l.IsConsoleOutputEnabled() {
		t.Error("console output should be disabled by default")
	}

	l.SetConsoleOutput(true)
	if !l.IsConsoleOutputEnabled() {
		t.Error("console output should be enabled after SetConsoleOutput(true)")
	}

	l.SetConsoleOutput(false)
	if l.IsConsoleOutputEnabled() {
		t.Error("console output should be disabled after SetConsoleOutput(false)")
	}
}

// TestLoggerConcurrency 测试并发日志写入安全性
func TestLoggerConcurrency(t *testing.T) {
	l := newTestLogger(200)
	const goroutines = 50
	const logsPerGoroutine = 3

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < logsPerGoroutine; j++ {
				l.Info(LogTypeDDNS, "goroutine %d log %d", id, j)
			}
		}(i)
	}

	// 并发读取
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.GetLogs()
			l.GetCount()
		}()
	}

	wg.Wait()

	count := l.GetCount()
	expected := goroutines * logsPerGoroutine
	if count != expected {
		t.Errorf("after concurrent writes, count = %d, want %d", count, expected)
	}
}

// TestGetLogger 测试全局 Logger 获取
func TestGetLogger(t *testing.T) {
	// DefaultLogger 可能已被其他测试或 init 初始化，只验证非空
	logger := GetLogger()
	if logger == nil {
		t.Error("GetLogger() should never return nil")
	}
}

// BenchmarkLoggerInfo 性能测试：写入日志
func BenchmarkLoggerInfo(b *testing.B) {
	l := newTestLogger(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.Info(LogTypeDDNS, "benchmark log message %d", i)
	}
}

// BenchmarkLoggerGetLogsByLevel 性能测试：按级别过滤
func BenchmarkLoggerGetLogsByLevel(b *testing.B) {
	l := newTestLogger(1000)
	for i := 0; i < 1000; i++ {
		if i%2 == 0 {
			l.Info(LogTypeSystem, "info %d", i)
		} else {
			l.Warn(LogTypeSystem, "warn %d", i)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.GetLogsByLevel(LogLevelINFO)
	}
}
