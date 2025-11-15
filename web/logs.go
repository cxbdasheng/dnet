package web

import (
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"

	"github.com/cxbdasheng/dnet/helper"
)

//go:embed logs.html
var logsEmbedFile embed.FS

func Logs(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case "GET":
		handleLogsGet(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}

func handleLogsGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(logsEmbedFile, "logs.html")
	if err != nil {
		log.Printf("解析模板失败: %v", err)
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(writer, struct {
		Version string
	}{
		Version: os.Getenv(VersionEnv),
	})
	if err != nil {
		log.Printf("渲染模板失败: %v", err)
	}
}
