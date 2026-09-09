// Package forward manages optional, application-level TCP forwarding listeners.
package forward

import (
	"context"

	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cxbdasheng/dnet/helper"
)

type Rule struct {
	Sequence       int      `json:"sequence,omitempty"`
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	CustomName     bool     `json:"custom_name,omitempty"`
	Enabled        bool     `json:"enabled"`
	Network        string   `json:"network"`
	ListenAddress  string   `json:"listen_address"`
	ListenPort     int      `json:"listen_port"`
	TargetHost     string   `json:"target_host"`
	TargetPort     int      `json:"target_port"`
	DialTimeoutSec int      `json:"dial_timeout_sec"`
	IdleTimeoutSec int      `json:"idle_timeout_sec"`
	MaxConnections int      `json:"max_connections"`
	AllowCIDRs     []string `json:"allow_cidrs"`
}

func Clone(rules []Rule) []Rule {
	out := slices.Clone(rules)
	for i := range out {
		out[i].AllowCIDRs = slices.Clone(out[i].AllowCIDRs)
	}
	return out
}
func (r Rule) address() string { return net.JoinHostPort(r.ListenAddress, strconv.Itoa(r.ListenPort)) }

// validTargetHost accepts an IP literal or a DNS hostname, without scheme or port.
func validTargetHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if host == "" || strings.TrimSpace(host) != host || len(host) > 253 {
		return false
	}
	host = strings.TrimSuffix(host, ".")
	if strings.Trim(host, "0123456789.") == "" {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func Validate(rules []Rule) error {
	if len(rules) > 128 {
		return fmt.Errorf("最多支持 128 条规则")
	}
	ids, addresses := map[string]bool{}, map[string]bool{}
	for _, r := range rules {
		if strings.TrimSpace(r.ID) == "" || ids[r.ID] {
			return fmt.Errorf("规则 ID 为空或重复")
		}
		ids[r.ID] = true
		if r.Network != "tcp4" && r.Network != "tcp6" {
			return fmt.Errorf("%s: 协议必须为 tcp4 或 tcp6", r.Name)
		}
		ip := net.ParseIP(r.ListenAddress)
		if ip == nil || (r.Network == "tcp4") != (ip.To4() != nil) {
			return fmt.Errorf("%s: 监听 IP 与协议不匹配", r.Name)
		}
		if r.ListenPort < 1 || r.ListenPort > 65535 || r.TargetPort < 1 || r.TargetPort > 65535 {
			return fmt.Errorf("%s: 端口范围为 1–65535", r.Name)
		}
		if !validTargetHost(r.TargetHost) {
			return fmt.Errorf("%s: 目标地址无效", r.Name)
		}
		if r.DialTimeoutSec < 1 || r.DialTimeoutSec > 300 || r.IdleTimeoutSec < 0 || r.IdleTimeoutSec > 86400 || r.MaxConnections < 1 || r.MaxConnections > 10000 {
			return fmt.Errorf("%s: 超时或连接数超出范围", r.Name)
		}
		if r.TargetPort == r.ListenPort && r.TargetHost == r.ListenAddress {
			return fmt.Errorf("%s: 不能转发到自身", r.Name)
		}
		for _, cidr := range r.AllowCIDRs {
			if net.ParseIP(cidr) == nil {
				if _, _, err := net.ParseCIDR(cidr); err != nil {
					return fmt.Errorf("%s: 无效白名单 %q", r.Name, cidr)
				}
			}
		}
		key := r.Network + "/" + r.address()
		if r.Enabled && addresses[key] {
			return fmt.Errorf("%s: 重复监听地址", r.Name)
		}
		if r.Enabled {
			addresses[key] = true
		}
	}
	return nil
}

type Status struct {
	ID          string `json:"id"`
	Listening   bool   `json:"listening"`
	Connections int    `json:"connections"`
	Upload      int64  `json:"upload"`
	Download    int64  `json:"download"`
	LastError   string `json:"last_error"`
}
type Manager struct {
	mu      sync.Mutex
	entries map[string]*entry
	closed  bool
}

func NewManager() *Manager { return &Manager{entries: map[string]*entry{}} }

// Apply stages all new sockets before saving. Binding/save failures leave the
// old runtime untouched. Overlapping address changes must be disabled first.
func (m *Manager) Apply(rules []Rule, save func() error) (applyErr error) {
	defer func() {
		if applyErr != nil {
			helper.Error(helper.LogTypeDPF, "配置应用失败，原有转发保持不变: %v", applyErr)
		}
	}()
	if err := Validate(rules); err != nil {
		return err
	}
	rules = Clone(rules)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("端口转发已停止")
	}
	next := map[string]*entry{}
	var created []*entry
	rollback := func() {
		for _, e := range created {
			e.listener.Close()
			e.cancel()
		}
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if old := m.entries[r.ID]; old != nil && old.rule.Network == r.Network && old.rule.address() == r.address() {
			next[r.ID] = old
			continue
		}
		l, err := net.Listen(r.Network, r.address())
		if err != nil {
			rollback()
			return fmt.Errorf("%s: 监听失败（地址重叠时请先停用旧规则）: %w", r.Name, err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		e := &entry{rule: r, listener: l, ctx: ctx, cancel: cancel, clients: map[net.Conn]bool{}, listening: true}
		created = append(created, e)
		next[r.ID] = e
	}
	if save != nil {
		if err := save(); err != nil {
			rollback()
			return err
		}
	}
	for _, r := range rules {
		if e := next[r.ID]; e != nil {
			e.mu.Lock()
			if !slices.Equal(e.rule.AllowCIDRs, r.AllowCIDRs) || e.rule.TargetHost != r.TargetHost || e.rule.TargetPort != r.TargetPort || e.rule.MaxConnections != r.MaxConnections || e.rule.DialTimeoutSec != r.DialTimeoutSec || e.rule.IdleTimeoutSec != r.IdleTimeoutSec {
				helper.Info(helper.LogTypeDPF, "[%s] 转发配置已更新 [监听=%s, 目标=%s]", r.Name, r.address(), net.JoinHostPort(r.TargetHost, strconv.Itoa(r.TargetPort)))
			}
			e.rule = r
			e.mu.Unlock()
		}
	}
	for id, e := range m.entries {
		if next[id] != e {
			e.stop()
		}
	}
	m.entries = next
	for _, e := range created {
		helper.Info(helper.LogTypeDPF, "[%s] 开始监听 [协议=%s, 监听=%s, 目标=%s]", e.rule.Name, e.rule.Network, e.rule.address(), net.JoinHostPort(e.rule.TargetHost, strconv.Itoa(e.rule.TargetPort)))
		e.wg.Add(1)
		go e.accept()
	}
	return nil
}
func (m *Manager) Status() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.entries))
	for id, e := range m.entries {
		e.mu.Lock()
		out = append(out, Status{id, e.listening, len(e.clients), e.upload.Load(), e.download.Load(), e.lastError})
		e.mu.Unlock()
	}
	return out
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	entries := m.entries
	for _, e := range entries {
		e.stop()
	}
	m.entries = map[string]*entry{}
	m.mu.Unlock()
	for _, e := range entries {
		e.wg.Wait()
	}
}

type entry struct {
	mu               sync.Mutex
	rule             Rule
	listener         net.Listener
	ctx              context.Context
	cancel           context.CancelFunc
	clients          map[net.Conn]bool
	wg               sync.WaitGroup
	upload, download atomic.Int64
	listening        bool
	lastError        string
	lastErrorLog     time.Time
}

func (e *entry) stop() {
	e.cancel()
	e.listener.Close()
	e.mu.Lock()
	e.listening = false
	helper.Info(helper.LogTypeDPF, "[%s] 停止监听 [监听=%s, 关闭连接数=%d]", e.rule.Name, e.rule.address(), len(e.clients))
	for c := range e.clients {
		c.Close()
	}
	e.mu.Unlock()
}
func (e *entry) fail(err error) {
	e.mu.Lock()
	e.lastError = err.Error()
	name := e.rule.Name
	emit := time.Since(e.lastErrorLog) >= 5*time.Second
	if emit {
		e.lastErrorLog = time.Now()
	}
	e.mu.Unlock()
	if emit {
		helper.Error(helper.LogTypeDPF, "[%s] 转发运行错误: %v", name, err)
	}
}
func allowed(addr net.Addr, r Rule) bool {
	if len(r.AllowCIDRs) == 0 {
		return true
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, v := range r.AllowCIDRs {
		if p := net.ParseIP(v); p != nil && p.Equal(ip) {
			return true
		}
		if _, n, err := net.ParseCIDR(v); err == nil && n.Contains(ip) {
			return true
		}
	}
	return false
}
func (e *entry) accept() {
	defer e.wg.Done()
	for {
		c, err := e.listener.Accept()
		if err != nil {
			if e.ctx.Err() == nil {
				e.fail(err)
			}
			e.mu.Lock()
			e.listening = false
			e.mu.Unlock()
			return
		}
		e.mu.Lock()
		r := e.rule
		if e.ctx.Err() != nil || len(e.clients) >= r.MaxConnections || !allowed(c.RemoteAddr(), r) {
			e.mu.Unlock()
			c.Close()
			helper.Debug(helper.LogTypeDPF, "[%s] 拒绝连接 [来源=%s, 原因=规则停止、连接数限制或来源白名单]", r.Name, c.RemoteAddr())
			continue
		}
		e.clients[c] = true
		e.wg.Add(1)
		e.mu.Unlock()
		go e.relay(c, r)
	}
}

// trackedWriter intentionally hides TCPConn's ReaderFrom fast path so activity
// and byte counters are updated for both directions.
type trackedWriter struct {
	conn            net.Conn
	total, activity *atomic.Int64
}

func (w trackedWriter) Write(p []byte) (int, error) {
	n, err := w.conn.Write(p)
	if n > 0 {
		w.total.Add(int64(n))
		w.activity.Store(time.Now().UnixNano())
	}
	return n, err
}
func (e *entry) relay(client net.Conn, r Rule) {
	defer e.wg.Done()
	defer client.Close()
	defer func() { e.mu.Lock(); delete(e.clients, client); e.mu.Unlock() }()
	ctx, cancel := context.WithCancel(e.ctx)
	defer cancel()
	target, err := (&net.Dialer{Timeout: time.Duration(r.DialTimeoutSec) * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(r.TargetHost, strconv.Itoa(r.TargetPort)))
	if err != nil {
		if ctx.Err() == nil {
			e.fail(err)
		}
		return
	}
	defer target.Close()
	helper.Debug(helper.LogTypeDPF, "[%s] 连接已建立 [来源=%s, 目标=%s]", r.Name, client.RemoteAddr(), target.RemoteAddr())
	defer helper.Debug(helper.LogTypeDPF, "[%s] 连接已关闭 [来源=%s]", r.Name, client.RemoteAddr())
	// Prevent obvious aliases/wildcard listeners from feeding themselves.
	if target.RemoteAddr().String() == client.LocalAddr().String() {
		e.fail(fmt.Errorf("目标指向转发入口"))
		return
	}
	var activity atomic.Int64
	activity.Store(time.Now().UnixNano())
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				client.Close()
				target.Close()
				return
			case <-ticker.C:
				if r.IdleTimeoutSec > 0 && time.Since(time.Unix(0, activity.Load())) >= time.Duration(r.IdleTimeoutSec)*time.Second {
					client.Close()
					target.Close()
					return
				}
			}
		}
	}()
	copyOne := func(dst, src net.Conn, total *atomic.Int64) error {
		_, err := io.Copy(trackedWriter{dst, total, &activity}, src)
		if err == nil {
			if tcp, ok := dst.(*net.TCPConn); ok {
				tcp.CloseWrite()
			}
		}
		return err
	}
	results := make(chan error, 2)
	go func() { results <- copyOne(target, client, &e.upload) }()
	go func() { results <- copyOne(client, target, &e.download) }()
	for range 2 {
		if err := <-results; err != nil {
			client.Close()
			target.Close()
			if ctx.Err() == nil {
				e.fail(err)
			}
		}
	}
}
