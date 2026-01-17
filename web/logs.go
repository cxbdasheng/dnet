package web

import (
	"embed"
	"html/template"
	"net/http"

	"github.com/cxbdasheng/dnet/helper"
)

//go:embed logs.html
var logsEmbedFile embed.FS

func Logs(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		handleLogsGet(writer, request)
	case http.MethodDelete:
		handleLogsAPIDelete(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}

// LogsPageData 日志页面数据模型
type LogsPageData struct {
	Logs       []helper.LogEntry // 日志列表
	TotalCount int               // 日志总数
}

func handleLogsGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(logsEmbedFile, "logs.html")
	if err != nil {
		helper.Error(helper.LogTypeSystem, "解析日志页面模板失败: %v", err)
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 获取日志数据
	memLogger := helper.GetLogger()
	logs := memLogger.GetLogs()

	// 构造页面数据
	data := LogsPageData{
		Logs:       logs,
		TotalCount: len(logs),
	}

	if err = tmpl.Execute(writer, data); err != nil {
		helper.Error(helper.LogTypeSystem, "渲染日志页面失败 [路径=%s]: %v", request.URL.Path, err)
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
	}
}

// handleLogsAPIDelete 清空日志
func handleLogsAPIDelete(writer http.ResponseWriter, request *http.Request) {
	memLogger := helper.GetLogger()

	// 记录清空前的日志数量
	oldCount := memLogger.GetCount()

	// 清空日志
	memLogger.Clear()

	// 记录清空操作
	helper.Info(helper.LogTypeSystem, "日志已清空 [操作者IP = %s, 清空数量 = %d ]", helper.GetClientIP(request), oldCount)

	// 返回成功响应
	helper.ReturnSuccess(writer, "日志已清空", nil)
}

// LogsCount 获取日志数量和最新日志
func LogsCount(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}

	memLogger := helper.GetLogger()
	count := memLogger.GetCount()
	logs := memLogger.GetLogs()

	// 获取最新的10条日志
	var latestLogs []helper.LogEntry
	if len(logs) > 0 {
		start := 0
		if len(logs) > 10 {
			start = len(logs) - 10
		}
		latestLogs = logs[start:]
	} else {
		latestLogs = []helper.LogEntry{}
	}

	// 返回日志数量和最新日志
	helper.ReturnSuccess(writer, "", map[string]interface{}{
		"count":  count,
		"latest": latestLogs,
	})
}
