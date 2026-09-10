package web

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cxbdasheng/dnet/forward"
	"github.com/cxbdasheng/dnet/helper"
)

// ForwardProbe tests the current draft without saving or enabling its listener.
func (s *Server) ForwardProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var rule forward.Rule
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rule); err != nil {
		helper.ReturnError(w, "配置格式错误")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		helper.ReturnError(w, "请求只能包含一个 JSON 对象")
		return
	}
	if err := forward.Validate([]forward.Rule{rule}); err != nil {
		helper.ReturnError(w, err.Error())
		return
	}
	if strings.HasPrefix(rule.Network, "udp") {
		helper.ReturnSuccess(w, "UDP 无握手，通用探测无法判断目标是否可用；请通过实际客户端请求并查看规则日志。", map[string]any{"supported": false})
		return
	}
	timeout := min(time.Duration(rule.DialTimeoutSec)*time.Second, 5*time.Second)
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	start := time.Now()
	target := net.JoinHostPort(rule.TargetHost, strconv.Itoa(rule.TargetPort))
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
	if err != nil {
		helper.RuleLog(helper.LogLevelWARN, rule.ID, "[%s] TCP 目标探测失败 [目标=%s]: %v", rule.Name, target, err)
		helper.ReturnError(w, "无法建立 TCP 连接："+err.Error())
		return
	}
	conn.Close()
	helper.RuleLog(helper.LogLevelINFO, rule.ID, "[%s] TCP 目标探测成功 [目标=%s]", rule.Name, target)
	helper.ReturnSuccess(w, "TCP 连接成功，仅确认端口可连接，不代表应用或证书正常。", map[string]any{"supported": true, "elapsed_ms": time.Since(start).Milliseconds()})
}

func (s *Server) ForwardLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" || len(id) > 256 {
		helper.ReturnError(w, "请选择配置")
		return
	}
	entries := make([]helper.LogEntry, 0)
	for _, entry := range helper.GetLogger().GetLogsByType(helper.LogTypeDPF) {
		if entry.RuleID == id {
			entries = append(entries, entry)
		}
	}
	helper.ReturnSuccess(w, "", entries)
}
