package web

import (
	"embed"
	"html/template"
	"net/http"
	"os"

	"github.com/cxbdasheng/dnet/helper"
)

const VersionEnv = "DNET_VERSION"

//go:embed home.html
var homeEmbedFile embed.FS

func Home(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case "GET":
		handleHomeGet(writer, request)
	default:
		helper.ReturnError(writer, "不支持的请求方法")
		return
	}
}
func handleHomeGet(writer http.ResponseWriter, request *http.Request) {
	tmpl, err := template.ParseFS(homeEmbedFile, "home.html")
	if err != nil {
		helper.Error(helper.LogTypeSystem, "解析首页模板失败: %v", err)
		http.Error(writer, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(writer, struct {
		Version string
	}{
		Version: os.Getenv(VersionEnv),
	})
	if err != nil {
		helper.Error(helper.LogTypeSystem, "渲染首页失败 [路径=%s]: %v", request.URL.Path, err)
	}
}
