// Package webservice manages domain-routed HTTP(S) services.
package webservice

import (
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cxbdasheng/dnet/certificates"
	"github.com/cxbdasheng/dnet/helper"
	"golang.org/x/crypto/bcrypt"
)

type Rule struct {
	ForceHTTPS    bool   `json:"force_https" yaml:"force_https,omitempty"`
	CertificateID string `json:"certificate_id" yaml:"certificate_id,omitempty"`
	managedCert   *tls.Certificate
	Type          string `json:"type" yaml:"type,omitempty"`
	RedirectCode  int    `json:"redirect_code" yaml:"redirect_code,omitempty"`
	ID            string `json:"id" yaml:"id"`
	Name          string `json:"name" yaml:"name"`
	Domain        string `json:"domain" yaml:"domain"`
	Network       string `json:"network" yaml:"network"`
	ListenAddress string `json:"listen_address" yaml:"listen_address"`
	ListenPort    int    `json:"listen_port" yaml:"listen_port"`
	Target        string `json:"target" yaml:"target"`
	PreserveHost  bool   `json:"preserve_host" yaml:"preserve_host"`
	AuthEnabled   bool   `json:"auth_enabled" yaml:"auth_enabled"`
	Username      string `json:"username" yaml:"username"`
	Password      string `json:"password,omitempty" yaml:"-"`
	PasswordHash  string `json:"-" yaml:"password_hash,omitempty"`
	HasPassword   bool   `json:"has_password" yaml:"-"`
	TLS           bool   `json:"tls" yaml:"tls"`
	Certificate   string `json:"certificate" yaml:"certificate"`
	Key           string `json:"key" yaml:"key"`
	TimeoutSec    int    `json:"timeout_sec" yaml:"timeout_sec"`
}

func (r Rule) serviceType() string {
	if r.Type == "" {
		return "proxy"
	}
	return r.Type
}

func (r Rule) address() string   { return net.JoinHostPort(r.ListenAddress, strconv.Itoa(r.ListenPort)) }
func (r Rule) groupKey() string  { return r.Network + "/" + r.address() }
func domain(value string) string { return strings.ToLower(strings.TrimSuffix(value, ".")) }
func validDomain(value string) bool {
	if value == "" || len(value) > 253 || strings.TrimSpace(value) != value {
		return false
	}
	for _, label := range strings.Split(strings.TrimSuffix(value, "."), ".") {
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

// listenerRules expands dual-stack rules into explicit IPv4 and IPv6 sockets.
// Both sockets must bind successfully before any configuration is committed.
func listenerRules(rules []Rule) []Rule {
	out := make([]Rule, 0, len(rules)*2)
	for _, r := range rules {
		if r.Network == "tcp" {
			r.Network, r.ListenAddress = "tcp4", "0.0.0.0"
			out = append(out, r)
			r.Network, r.ListenAddress = "tcp6", "::"
		}
		out = append(out, r)
	}
	return out
}

func PublicRules(rules []Rule) []Rule {
	out := slices.Clone(rules)
	for i := range out {
		out[i].HasPassword = out[i].PasswordHash != ""
		out[i].PasswordHash = ""
		out[i].Password = ""
	}
	return out
}
func Prepare(rules, previous []Rule) ([]Rule, error) {
	out := slices.Clone(rules)
	for i := range out {
		r := &out[i]
		r.PasswordHash = ""
		r.HasPassword = false
		r.Domain = domain(r.Domain)
		for _, old := range previous {
			if old.ID == r.ID {
				r.PasswordHash = old.PasswordHash
				break
			}
		}
		if r.Password != "" {
			if len(r.Password) > 72 {
				return nil, fmt.Errorf("%s: 密码不能超过 72 字节", r.Name)
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(r.Password), bcrypt.DefaultCost)
			if err != nil {
				return nil, err
			}
			r.PasswordHash = string(hash)
		}
		r.Password = ""
	}
	return out, Validate(out)
}
func Validate(rules []Rule) error {
	if len(rules) > 128 {
		return fmt.Errorf("最多支持 128 条 Web 服务配置")
	}
	ids, routes, groups := map[string]bool{}, map[string]bool{}, map[string]Rule{}
	for _, r := range rules {
		if r.ID == "" || ids[r.ID] {
			return fmt.Errorf("配置 ID 为空或重复")
		}
		ids[r.ID] = true
		if strings.TrimSpace(r.Name) == "" {
			return fmt.Errorf("配置名称不能为空")
		}
		if !validDomain(r.Domain) {
			return fmt.Errorf("%s: 请填写域名，不含协议、端口、路径或通配符", r.Name)
		}
		if r.Network != "tcp" && r.Network != "tcp4" && r.Network != "tcp6" {
			return fmt.Errorf("%s: 监听协议无效", r.Name)
		}
		ip := net.ParseIP(r.ListenAddress)
		if r.Network == "tcp" && r.ListenAddress != "::" {
			return fmt.Errorf("%s: IPv4 + IPv6 模式的监听地址必须为 ::（全部接口）", r.Name)
		}
		if ip == nil || (r.Network != "tcp" && (r.Network == "tcp4") != (ip.To4() != nil)) {
			return fmt.Errorf("%s: 监听 IP 与协议不匹配", r.Name)
		}
		if r.ListenPort < 1 || r.ListenPort > 65535 {
			return fmt.Errorf("%s: 端口必须为 1–65535", r.Name)
		}
		if r.serviceType() != "proxy" && r.serviceType() != "redirect" && r.serviceType() != "jump" && r.serviceType() != "speedtest" {
			return fmt.Errorf("%s: 服务类型无效", r.Name)
		}
		if r.serviceType() == "redirect" && r.RedirectCode != 0 && r.RedirectCode != 301 && r.RedirectCode != 302 && r.RedirectCode != 307 && r.RedirectCode != 308 {
			return fmt.Errorf("%s: 重定向状态码必须为 301、302、307 或 308", r.Name)
		}
		if r.serviceType() != "speedtest" {
			target, err := url.Parse(r.Target)
			if err != nil || target.Hostname() == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil || (r.serviceType() == "proxy" && (target.Fragment != "" || target.RawQuery != "")) {
				return fmt.Errorf("%s: 目标必须为 HTTP/HTTPS 地址且不含账号；反向代理目标不能含查询参数或片段", r.Name)
			}
			if net.ParseIP(target.Hostname()) == nil && !validDomain(target.Hostname()) {
				return fmt.Errorf("%s: 目标主机名无效", r.Name)
			}
			if port := target.Port(); port != "" {
				p, err := strconv.Atoi(port)
				if err != nil || p < 1 || p > 65535 {
					return fmt.Errorf("%s: 目标端口无效", r.Name)
				}
			}
		}
		if r.serviceType() == "proxy" && (r.TimeoutSec < 1 || r.TimeoutSec > 300) {
			return fmt.Errorf("%s: 响应头超时必须为 1–300 秒", r.Name)
		}
		if r.AuthEnabled && (r.Username == "" || strings.Contains(r.Username, ":") || r.PasswordHash == "") {
			return fmt.Errorf("%s: 请设置访问账号和密码", r.Name)
		}
		if r.ForceHTTPS && !r.TLS {
			return fmt.Errorf("%s: 强制 HTTPS 需要先启用 HTTPS", r.Name)
		}
		if r.TLS && r.CertificateID == "" && (r.Certificate == "" || r.Key == "") {
			return fmt.Errorf("%s: 请设置 PEM 证书及私钥路径", r.Name)
		}
		for _, r := range listenerRules([]Rule{r}) {
			key := r.groupKey()
			route := key + "/" + domain(r.Domain)
			if routes[route] {
				return fmt.Errorf("%s: 同一监听入口的域名重复", r.Name)
			}
			routes[route] = true
			if old, ok := groups[key]; ok && (old.TLS != r.TLS || old.Certificate != r.Certificate || old.Key != r.Key || old.CertificateID != r.CertificateID) {
				return fmt.Errorf("%s: 同一监听入口必须使用相同的 HTTPS 设置和证书", r.Name)
			}
			groups[key] = r
		}
	}
	return nil
}

type site struct {
	rule      Rule
	handler   http.Handler
	transport *http.Transport
}

var jumpPage = template.Must(template.New("jump").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><title>正在跳转</title></head><body><p>正在跳转，如未自动跳转，请<a href="{{.}}">点击继续</a>。</p><script>window.location.replace({{.}});</script></body></html>`))

func newSite(r Rule, rules []Rule) *site {
	target, _ := url.Parse(r.Target)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	listeners := slices.Clone(rules)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		remote := conn.RemoteAddr().(*net.TCPAddr)
		local := conn.LocalAddr().(*net.TCPAddr)
		for _, listener := range listeners {
			ip := net.ParseIP(listener.ListenAddress)
			if remote.Port == listener.ListenPort && (remote.IP.Equal(ip) || (ip.IsUnspecified() && remote.IP.Equal(local.IP))) {
				conn.Close()
				return nil, fmt.Errorf("目标指向 Web 服务监听入口")
			}
		}
		return conn, nil
	}
	transport.ResponseHeaderTimeout = time.Duration(r.TimeoutSec) * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(target)
			if r.PreserveHost {
				p.Out.Host = p.In.Host
			}
			p.SetXForwarded()
			p.Out.Header.Del("X-Real-IP")
			if host, _, err := net.SplitHostPort(p.In.RemoteAddr); err == nil {
				p.Out.Header.Set("X-Real-IP", host)
			}
			if r.AuthEnabled {
				p.Out.Header.Del("Authorization")
			}
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			if req.Context().Err() != nil {
				return
			}
			helper.Info(helper.LogTypeWebService, "[%s] 代理请求失败: %v", r.Name, err)
			http.Error(w, "目标服务暂时不可用", http.StatusBadGateway)
		},
	}
	s := &site{rule: r, transport: transport}
	auth := newAccessAuth()
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.AuthEnabled {
			if code := auth.check(req, r, time.Now()); code != 0 {
				if code == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "60")
					http.Error(w, "认证尝试过于频繁，请稍后重试", code)
				} else {
					w.Header().Set("WWW-Authenticate", `Basic realm="D-NET Web", charset="UTF-8"`)
					http.Error(w, "需要访问账号认证", code)
				}
				return
			}
		}
		switch r.serviceType() {
		case "speedtest":
			speedTestManaged(w, req)
		case "redirect":
			code := r.RedirectCode
			if code == 0 {
				code = http.StatusFound
			}
			http.Redirect(w, req, r.Target, code)
		case "jump":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			if req.Method != http.MethodHead {
				_ = jumpPage.Execute(w, r.Target)
			}
		default:
			proxy.ServeHTTP(w, req)
		}
	})
	return s
}

type group struct {
	mu          sync.RWMutex
	rule        Rule
	sites       map[string]*site
	tlsConfig   *tls.Config
	listener    net.Listener
	server      *http.Server
	connections map[net.Conn]bool
	closing     bool
	listening   bool
}

// Track sockets below TLS so shutdown also closes hijacked WebSocket streams.
type trackedListener struct {
	net.Listener
	group *group
}
type trackedConn struct {
	net.Conn
	group *group
	once  sync.Once
}

func (c *trackedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.group.mu.Lock(); delete(c.group.connections, c); c.group.mu.Unlock() })
	return err
}
func (l trackedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	t := &trackedConn{Conn: c, group: l.group}
	l.group.mu.Lock()
	if l.group.closing {
		l.group.mu.Unlock()
		c.Close()
		return nil, net.ErrClosed
	}
	l.group.connections[t] = true
	l.group.mu.Unlock()
	return t, nil
}
func (g *group) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	g.mu.RLock()
	s := g.sites[domain(host)]
	g.mu.RUnlock()
	if s == nil {
		http.NotFound(w, r)
		return
	}
	if s.rule.TLS && r.TLS == nil {
		if !s.rule.ForceHTTPS {
			http.Error(w, "Please use HTTPS", http.StatusBadRequest)
			return
		}
		host := domain(s.rule.Domain)
		if _, port, err := net.SplitHostPort(r.Host); err == nil {
			p, e := strconv.Atoi(port)
			if e != nil || p < 1 || p > 65535 {
				http.Error(w, "Invalid Host port", http.StatusBadRequest)
				return
			}
			host = net.JoinHostPort(host, port)
		}
		target := &url.URL{Scheme: "https", Host: host, Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery, ForceQuery: r.URL.ForceQuery}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, target.String(), http.StatusTemporaryRedirect)
		return
	}
	s.handler.ServeHTTP(w, r)
}
func (g *group) close() {
	g.mu.Lock()
	g.closing = true
	g.listening = false
	connections := make([]net.Conn, 0, len(g.connections))
	for c := range g.connections {
		connections = append(connections, c)
	}
	sites := g.sites
	g.mu.Unlock()
	g.server.Close()
	g.listener.Close()
	for _, c := range connections {
		c.Close()
	}
	for _, s := range sites {
		s.transport.CloseIdleConnections()
	}
}

type Status struct {
	ID        string `json:"id"`
	Listening bool   `json:"listening"`
}
type Manager struct {
	mu     sync.Mutex
	groups map[string]*group
	closed bool
}

func NewManager() *Manager { return &Manager{groups: map[string]*group{}} }
func (m *Manager) Status() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Status{}
	indexes := map[string]int{}
	for _, g := range m.groups {
		g.mu.RLock()
		for _, s := range g.sites {
			if index, ok := indexes[s.rule.ID]; ok {
				out[index].Listening = out[index].Listening && g.listening
			} else {
				indexes[s.rule.ID] = len(out)
				out = append(out, Status{s.rule.ID, g.listening})
			}
		}
		g.mu.RUnlock()
	}
	return out
}
func (m *Manager) Apply(rules []Rule, save func() error) error {
	if err := Validate(rules); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("Web 服务已停止")
	}
	rules = listenerRules(rules)
	next := map[string]*group{}
	routes := map[string]map[string]*site{}
	certs := map[string]*tls.Config{}
	created := []*group{}
	rollback := func() {
		for _, g := range created {
			g.listener.Close()
		}
		for _, ss := range routes {
			for _, s := range ss {
				s.transport.CloseIdleConnections()
			}
		}
	}
	for _, r := range rules {
		key := r.groupKey()
		if routes[key] == nil {
			routes[key] = map[string]*site{}
		}
		routes[key][domain(r.Domain)] = newSite(r, rules)
		if next[key] != nil {
			continue
		}
		if r.TLS {
			var cert tls.Certificate
			var err error
			if r.CertificateID != "" {
				if r.managedCert == nil {
					err = fmt.Errorf("托管证书未加载")
				} else {
					cert = *r.managedCert
				}
			} else {
				cert, err = tls.LoadX509KeyPair(r.Certificate, r.Key)
			}
			if err != nil {
				rollback()
				return fmt.Errorf("%s: 证书或私钥无法加载: %w", r.Name, err)
			}
			certs[key] = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
		}
		if old := m.groups[key]; old != nil {
			if old.rule.TLS != r.TLS {
				rollback()
				return fmt.Errorf("%s: 切换 HTTP/HTTPS 前请先关闭全局开关并提交", r.Name)
			}
			next[key] = old
			continue
		}
		listener, err := net.Listen(r.Network, r.address())
		if err != nil {
			rollback()
			return fmt.Errorf("%s: 监听失败: %w", r.Name, err)
		}
		g := &group{rule: r, connections: map[net.Conn]bool{}, listening: true}
		g.listener = trackedListener{listener, g}
		if r.TLS {
			g.listener = newMixedListener(g.listener, &tls.Config{MinVersion: tls.VersionTLS12, GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
				g.mu.RLock()
				defer g.mu.RUnlock()
				return g.tlsConfig, nil
			}})
		}
		g.server = &http.Server{Handler: g, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20, BaseContext: func(net.Listener) context.Context { return context.Background() }}
		next[key] = g
		created = append(created, g)
	}
	if save != nil {
		if err := save(); err != nil {
			rollback()
			return err
		}
	}
	for key, g := range next {
		g.mu.Lock()
		old := g.sites
		g.sites = routes[key]
		g.tlsConfig = certs[key]
		g.mu.Unlock()
		for _, s := range old {
			s.transport.CloseIdleConnections()
		}
	}
	for key, g := range m.groups {
		if next[key] != g {
			g.close()
			helper.Info(helper.LogTypeWebService, "停止监听 %s", g.rule.address())
		}
	}
	m.groups = next
	for _, g := range created {
		helper.Info(helper.LogTypeWebService, "开始监听 %s", g.rule.address())
		go func(g *group) {
			err := g.server.Serve(g.listener)
			g.mu.Lock()
			g.listening = false
			g.mu.Unlock()
			if err != nil && err != http.ErrServerClosed {
				helper.Error(helper.LogTypeWebService, "监听异常: %v", err)
			}
		}(g)
	}
	return nil
}
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for _, g := range m.groups {
		g.close()
	}
	m.groups = map[string]*group{}
}

// ApplyCertificates resolves shared certificate IDs without persisting certificate material in rules.
func (m *Manager) ApplyCertificates(rules []Rule, certs []certificates.Certificate, save func() error) error {
	resolved, err := ResolveCertificates(rules, certs)
	if err != nil {
		return err
	}
	return m.Apply(resolved, save)
}
func ResolveCertificates(rules []Rule, certs []certificates.Certificate) ([]Rule, error) {
	out := slices.Clone(rules)
	loaded := map[string]*tls.Certificate{}
	for i := range out {
		r := &out[i]
		if !r.TLS || r.CertificateID == "" {
			continue
		}
		pair := loaded[r.CertificateID]
		if pair == nil {
			var found *certificates.Certificate
			for j := range certs {
				if certs[j].ID == r.CertificateID {
					found = &certs[j]
					break
				}
			}
			if found == nil {
				return nil, fmt.Errorf("%s: 所选证书不存在", r.Name)
			}
			value, err := certificates.Load(*found)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", r.Name, err)
			}
			pair = &value
			loaded[r.CertificateID] = pair
		}
		if err := pair.Leaf.VerifyHostname(r.Domain); err != nil {
			return nil, fmt.Errorf("%s: 证书未覆盖域名 %s", r.Name, r.Domain)
		}
		if time.Now().Before(pair.Leaf.NotBefore) || !time.Now().Before(pair.Leaf.NotAfter) {
			return nil, fmt.Errorf("%s: 证书尚未生效或已过期", r.Name)
		}
		r.managedCert = pair
	}
	return out, nil
}
