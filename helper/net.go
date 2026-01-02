package helper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Ipv4Reg IPv4正则
var Ipv4Reg = regexp.MustCompile(`((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])`)

// Ipv6Reg IPv6正则
var Ipv6Reg = regexp.MustCompile(`((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|((:[0-9A-Fa-f]{1,4})?:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|((:[0-9A-Fa-f]{1,4}){0,2}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|((:[0-9A-Fa-f]{1,4}){0,3}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|((:[0-9A-Fa-f]{1,4}){0,4}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|((:[0-9A-Fa-f]{1,4}){0,5}:((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}))|:)))`)

const (
	IPv4 = "ipv4"
	IPv6 = "ipv6"

	// maxResponseBodySize 最大响应体大小（约1MB）
	maxResponseBodySize = 1024000
	// dnsTestTimeout DNS连接测试超时时间
	dnsTestTimeout = 1 * time.Second
	// dnsResolverTimeout DNS解析器超时时间
	dnsResolverTimeout = 3 * time.Second
)

// SetDNS 设置自定义DNS服务器
func SetDNS(dnsServer string) {
	if dnsServer == "" {
		Info(LogTypeSystem, "DNS服务器地址为空，跳过设置")
		return
	}

	// 验证DNS服务器地址格式
	if !isValidDNSServer(dnsServer) {
		Info(LogTypeSystem, "无效的DNS服务器地址: %s", dnsServer)
		return
	}

	// 添加默认端口
	if !strings.Contains(dnsServer, ":") {
		dnsServer = dnsServer + ":53"
	}

	// 测试DNS服务器连通性
	if !testDNSConnectivity(dnsServer) {
		Info(LogTypeSystem, "DNS服务器 %s 连接测试失败", dnsServer)
		return
	}

	// 设置自定义DNS解析器
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: dnsResolverTimeout,
			}
			return d.DialContext(ctx, network, dnsServer)
		},
	}
	Info(LogTypeSystem, "已设置自定义 DNS 服务器: %s", dnsServer)
}

// isValidDNSServer 验证DNS服务器地址格式
func isValidDNSServer(dnsServer string) bool {
	// 移除端口部分进行IP验证
	host := dnsServer
	if strings.Contains(dnsServer, ":") {
		host, _, _ = net.SplitHostPort(dnsServer)
	}

	// 验证是否为有效IP地址
	ip := net.ParseIP(host)
	if ip == nil {
		// 如果不是IP，检查是否为有效域名
		if !isValidDomainName(host) {
			return false
		}
	}

	return true
}

// isValidDomainName 验证域名格式
func isValidDomainName(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}

	// 简单的域名格式检查
	labels := strings.Split(domain, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		// 检查是否只包含字母、数字和连字符
		for _, char := range label {
			if !((char >= 'a' && char <= 'z') ||
				(char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9') ||
				char == '-') {
				return false
			}
		}
	}

	return true
}

// testProtocolConnectivity 测试特定协议的连接性
func testProtocolConnectivity(protocol, address string, timeout time.Duration) bool {
	conn, err := net.DialTimeout(protocol, address, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// testDNSConnectivity 测试DNS服务器连通性
func testDNSConnectivity(dnsServer string) bool {
	// 并发测试TCP和UDP连接，提高成功率
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 使用channel收集结果
	resultChan := make(chan bool, 2)

	// 测试UDP连接
	go func() {
		resultChan <- testProtocolConnectivity("udp", dnsServer, dnsTestTimeout)
	}()

	// 测试TCP连接(作为备用)
	go func() {
		resultChan <- testProtocolConnectivity("tcp", dnsServer, dnsTestTimeout)
	}()

	// 等待任意一个连接成功
	select {
	case result := <-resultChan:
		if result {
			return true
		}
		// 等待第二个结果
		select {
		case result2 := <-resultChan:
			return result2
		case <-ctx.Done():
			return false
		}
	case <-ctx.Done():
		return false
	}
}

// InitBackupDNS 初始化备用DNS，使用并发测试提高速度
func InitBackupDNS(customDNS string) {
	if customDNS != "" {
		SetDNS(customDNS)
		Info(LogTypeSystem, "使用自定义DNS: %s", customDNS)
		return
	}

	// 设置默认的备用DNS服务器
	defaultDNS := []string{"223.5.5.5", "114.114.114.114", "119.29.29.29"}

	// 并发测试所有DNS服务器
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type dnsResult struct {
		dns   string
		works bool
	}

	resultChan := make(chan dnsResult, len(defaultDNS))

	// 并发测试所有DNS
	for _, dns := range defaultDNS {
		go func(dnsAddr string) {
			works := testDNSConnectivity(dnsAddr + ":53")
			select {
			case resultChan <- dnsResult{dns: dnsAddr, works: works}:
			case <-ctx.Done():
			}
		}(dns)
	}

	// 选择第一个可用的DNS
	for i := 0; i < len(defaultDNS); i++ {
		select {
		case result := <-resultChan:
			if result.works {
				SetDNS(result.dns)
				Info(LogTypeSystem, "使用备用 DNS: %s", result.dns)
				return
			}
		case <-ctx.Done():
			Info(LogTypeSystem, "DNS 测试超时，使用系统默认 DNS")
			return
		}
	}
	Info(LogTypeSystem, "所有备用 DNS 服务器均不可用，使用系统默认 DNS")
}

// IsLocalAddress 检查IP地址是否为私有地址
func IsLocalAddress(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	// 使用Go 1.17+标准库的IsPrivate方法
	return ip.IsPrivate()
}

// getRegexByAddrType 根据地址类型返回对应的正则表达式
func getRegexByAddrType(addrType string) *regexp.Regexp {
	if addrType == IPv6 {
		return Ipv6Reg
	}
	return Ipv4Reg
}

// getAddrTypeConfig 根据地址类型返回相应的配置
type addrTypeConfig struct {
	network      string
	regex        *regexp.Regexp
	addrTypeName string
}

func getAddrTypeConfig(addrType string) addrTypeConfig {
	if addrType == IPv6 {
		return addrTypeConfig{
			network:      "tcp6",
			regex:        Ipv6Reg,
			addrTypeName: "IPv6",
		}
	}
	return addrTypeConfig{
		network:      "tcp4",
		regex:        Ipv4Reg,
		addrTypeName: "IPv4",
	}
}

// GetClientIP 获取客户端真实IP地址
func GetClientIP(r *http.Request) string {
	// 检查X-Forwarded-For头，获取最原始的客户端IP
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			if ip != "" && !IsLocalAddress(ip) {
				return ip
			}
		}
		// 如果没有找到公网IP，返回第一个非空IP
		if len(ips) > 0 && strings.TrimSpace(ips[0]) != "" {
			return strings.TrimSpace(ips[0])
		}
	}

	// 检查X-Real-IP头
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	// 使用RemoteAddr，统一使用net.SplitHostPort处理
	// 这个方法可以正确处理IPv4和IPv6地址(包括[::1]:8080这种格式)
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}

	// 如果SplitHostPort失败，直接返回原始地址(可能是没有端口的情况)
	return r.RemoteAddr
}

// GetAddrFromUrl 从 URL 中获取地址
func GetAddrFromUrl(urlsStr string, addrType string) string {
	// 根据地址类型获取配置
	config := getAddrTypeConfig(addrType)

	// 创建对应的 HTTP 客户端
	client := CreateNoProxyHTTPClient(config.network)

	// 遍历所有 URL
	urls := strings.Split(urlsStr, ",")
	for _, url := range urls {
		url = strings.TrimSpace(url)
		if url == "" {
			continue
		}

		// 发送 HTTP 请求
		resp, err := client.Get(url)
		if err != nil {
			Info(LogTypeSystem, "通过接口获取 %s 失败! 接口地址: %s", config.addrTypeName, url)
			Warn(LogTypeSystem, "异常信息: %s", err)
			continue
		}

		// 读取响应体
		lr := io.LimitReader(resp.Body, maxResponseBodySize)
		body, err := io.ReadAll(lr)
		_ = resp.Body.Close()

		if err != nil {
			Warn(LogTypeSystem, "读取响应失败: %s", err)
			continue
		}

		// 使用正则提取地址
		result := config.regex.FindString(string(body))
		if result == "" {
			Info(LogTypeSystem, "获取 %s 结果失败! 接口: %s, 返回值: %s", config.addrTypeName, url, string(body))
			continue
		}

		// 找到有效地址，返回
		return result
	}

	// 所有 URL 都失败
	return ""
}

// GetAddrFromCmd 从命令输出中获取地址
func GetAddrFromCmd(cmd string, addrType string) string {
	if cmd == "" {
		Info(LogTypeSystem, "命令为空，无法获取地址")
		return ""
	}

	// 获取正则表达式
	regex := getRegexByAddrType(addrType)

	// 根据操作系统选择shell
	var execCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		execCmd = exec.Command("powershell", "-Command", cmd)
	} else {
		// 优先使用bash，不存在则使用sh
		_, err := exec.LookPath("bash")
		if err != nil {
			execCmd = exec.Command("sh", "-c", cmd)
		} else {
			execCmd = exec.Command("bash", "-c", cmd)
		}
	}

	// 执行命令
	out, err := execCmd.CombinedOutput()
	if err != nil {
		Info(LogTypeSystem, "执行命令失败: %s, 错误: %v", cmd, err)
		return ""
	}

	// 使用正则提取地址
	result := regex.FindString(string(out))
	if result == "" {
		Info(LogTypeSystem, "未能从命令输出中提取%s地址: %s", addrType, cmd)
	}
	return result
}

// findAddrInInterfaces 在接口列表中查找地址
func findAddrInInterfaces(interfaces []NetInterface, interfaceName string) string {
	for _, netInterface := range interfaces {
		if netInterface.Name == interfaceName && len(netInterface.Address) > 0 {
			return netInterface.Address[0]
		}
	}
	return ""
}

// GetAddrFromInterface 从网络接口获取地址
func GetAddrFromInterface(interfaceName string, addrType string) string {
	ipv4, ipv6, err := GetNetInterface()
	if err != nil {
		Info(LogTypeSystem, "获取网络接口失败: %v", err)
		return ""
	}

	var result string
	if addrType == IPv4 {
		result = findAddrInInterfaces(ipv4, interfaceName)
		if result == "" {
			Info(LogTypeSystem, "未找到IPv4接口: %s", interfaceName)
		}
	} else if addrType == IPv6 {
		result = findAddrInInterfaces(ipv6, interfaceName)
		if result == "" {
			Info(LogTypeSystem, "未找到IPv6接口: %s", interfaceName)
		}
	}

	return result
}

const (
	httpClientTimeout     = 30 * time.Second
	dialerTimeout         = 30 * time.Second
	dialerKeepAlive       = 30 * time.Second
	idleConnTimeout       = 90 * time.Second
	tlsHandshakeTimeout   = 10 * time.Second
	expectContinueTimeout = 1 * time.Second
)

var dialer = &net.Dialer{
	Timeout:   dialerTimeout,
	KeepAlive: dialerKeepAlive,
}

var defaultTransport = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, address)
	},
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          100,
	MaxIdleConnsPerHost:   10, // 限制每个主机的最大空闲连接数
	IdleConnTimeout:       idleConnTimeout,
	TLSHandshakeTimeout:   tlsHandshakeTimeout,
	ExpectContinueTimeout: expectContinueTimeout,
}

// CreateHTTPClient Create Default HTTP Client
func CreateHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   httpClientTimeout,
		Transport: defaultTransport,
	}
}

// GetHTTPResponse 处理HTTP结果，返回序列化的json
func GetHTTPResponse(resp *http.Response, err error, result interface{}) error {
	body, err := GetHTTPResponseOrg(resp, err)
	if err != nil {
		return err
	}

	// 空响应体不需要解析
	if len(body) == 0 {
		return nil
	}

	// 尝试解析JSON
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("JSON 解析失败: %w", err)
	}

	return nil
}

// GetHTTPResponseOrg 处理HTTP结果，返回byte
func GetHTTPResponseOrg(resp *http.Response, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	lr := io.LimitReader(resp.Body, maxResponseBodySize)
	body, err := io.ReadAll(lr)

	if err != nil {
		return nil, err
	}

	// 300及以上状态码都算异常
	if resp.StatusCode >= 300 {
		err = fmt.Errorf("HTTP 请求失败 [%d]: %s", resp.StatusCode, string(body))
	}

	return body, err
}

// createNoProxyTransport 创建无代理的 HTTP Transport
func createNoProxyTransport(network string) *http.Transport {
	return &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
	}
}

var (
	noProxyTcp4Transport = createNoProxyTransport("tcp4")
	noProxyTcp6Transport = createNoProxyTransport("tcp6")
)

// CreateNoProxyHTTPClient Create NoProxy HTTP Client
func CreateNoProxyHTTPClient(network string) *http.Client {
	transport := noProxyTcp4Transport
	if network == "tcp6" {
		transport = noProxyTcp6Transport
	}

	return &http.Client{
		Timeout:   httpClientTimeout,
		Transport: transport,
	}
}
