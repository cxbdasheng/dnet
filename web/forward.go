package web

import (
	"embed"
	"encoding/json"
	"io"
	"net/http"

	"github.com/cxbdasheng/dnet/forward"
	"github.com/cxbdasheng/dnet/helper"
)

//go:embed forward.html
var forwardFiles embed.FS

func (s *Server) ForwardPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, _ := forwardFiles.ReadFile("forward.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}
func (s *Server) ForwardAPI(w http.ResponseWriter, r *http.Request) {
	if s.Forwarder == nil {
		http.Error(w, "转发服务未初始化", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.forwardMu.Lock()
	defer s.forwardMu.Unlock()
	conf, err := s.configRepo.Load()
	if err != nil {
		helper.ReturnError(w, "加载配置失败")
		return
	}
	if r.Method == http.MethodPost {
		var payload struct {
			Enabled bool           `json:"enabled"`
			Rules   []forward.Rule `json:"rules"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&payload); err != nil {
			helper.ReturnError(w, "配置格式错误: "+err.Error())
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			helper.ReturnError(w, "请求只能包含一个 JSON 对象")
			return
		}
		if err := forward.Validate(payload.Rules); err != nil {
			helper.Warn(helper.LogTypeDPF, "配置校验失败: %v", err)
			helper.ReturnError(w, err.Error())
			return
		}
		conf.ForwardEnabled = payload.Enabled
		conf.ForwardRules = payload.Rules
		if err := s.Forwarder.Apply(conf.ActiveForwardRules(), func() error { return s.configRepo.Save(&conf) }); err != nil {
			helper.ReturnError(w, err.Error())
			return
		}
		helper.Info(helper.LogTypeDPF, "配置保存并应用成功 [全局开启=%t, 规则数=%d]", payload.Enabled, len(payload.Rules))
	}
	helper.ReturnSuccess(w, "配置已生效", struct {
		Enabled bool             `json:"enabled"`
		Rules   []forward.Rule   `json:"rules"`
		Status  []forward.Status `json:"status"`
	}{conf.ForwardEnabled, conf.ForwardRules, s.Forwarder.Status()})
}
