package web

import (
	"embed"
	"net/http"
)

var staticEmbeddedFiles embed.FS
var faviconEmbeddedFile embed.FS

func SetEmbeddedAssets(staticFS embed.FS, faviconFS embed.FS) {
	staticEmbeddedFiles = staticFS
	faviconEmbeddedFile = faviconFS
}

func staticFsFunc(writer http.ResponseWriter, request *http.Request) {
	http.FileServer(http.FS(staticEmbeddedFiles)).ServeHTTP(writer, request)
}

func faviconFsFunc(writer http.ResponseWriter, request *http.Request) {
	http.FileServer(http.FS(faviconEmbeddedFile)).ServeHTTP(writer, request)
}
