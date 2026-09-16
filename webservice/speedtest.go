package webservice

import (
	"crypto/rand"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strconv"
	"sync"
	"time"
)

//go:embed speedtest.html
var speedTestPage []byte

//go:embed favicon.ico
var speedTestIcon []byte

//go:embed librespeed/*
var libreSpeedAssets embed.FS

var speedPayload = sync.OnceValue(func() []byte {
	data := make([]byte, 1<<20)
	if _, err := rand.Read(data); err != nil {
		panic(err)
	}
	return data
})

func speedTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	action := r.URL.Query().Get("dws_test")
	if action == "" {
		switch path.Base(r.URL.Path) {
		case "speedtest.js", "speedtest_worker.js":
			if r.Method != http.MethodGet {
				w.WriteHeader(405)
				return
			}
			data, err := libreSpeedAssets.ReadFile("librespeed/" + path.Base(r.URL.Path))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Write(data)
			return
		}
	}
	method := http.MethodGet
	if action == "upload" || (action == "empty" && r.Method == http.MethodPost) {
		method = http.MethodPost
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		http.Error(w, "请求方法不支持", 405)
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Now().Add(20 * time.Second))
	_ = rc.SetWriteDeadline(time.Now().Add(20 * time.Second))
	defer rc.SetReadDeadline(time.Time{})
	defer rc.SetWriteDeadline(time.Time{})
	switch action {
	case "":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(speedTestPage)
	case "favicon":
		w.Header().Set("Content-Type", "image/x-icon")
		w.Write(speedTestIcon)
	case "license":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		data, _ := libreSpeedAssets.ReadFile("librespeed/LICENSE")
		w.Write(data)
		w.Write([]byte("\n\n"))
		copying, _ := libreSpeedAssets.ReadFile("librespeed/COPYING")
		w.Write(copying)
	case "empty":
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "上传失败或超过 32 MiB", 413)
			return
		}
		w.WriteHeader(http.StatusOK)
	case "garbage":
		chunks := 4
		if value := r.URL.Query().Get("ckSize"); value != "" {
			var err error
			chunks, err = strconv.Atoi(value)
			if err != nil || chunks < 1 || chunks > 64 {
				http.Error(w, "下载大小必须为 1–64 MiB", 400)
				return
			}
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(chunks*(1<<20)))
		for i := 0; i < chunks; i++ {
			if r.Context().Err() != nil {
				return
			}
			if _, err := w.Write(speedPayload()); err != nil {
				return
			}
		}
	case "ping":
		w.WriteHeader(http.StatusNoContent)
	case "download":
		size, err := strconv.Atoi(r.URL.Query().Get("bytes"))
		if err != nil || size < 1 || size > 16<<20 {
			http.Error(w, "下载大小必须为 1–16777216 字节", 400)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(size))
		io.CopyN(w, rand.Reader, int64(size))
	case "upload":
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			http.Error(w, "上传失败或超过 16 MiB", http.StatusRequestEntityTooLarge)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int64{"bytes": n})
	default:
		http.NotFound(w, r)
	}
}
