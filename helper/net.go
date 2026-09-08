package helper

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// dnsResolverTimeout DNS解析器超时时间
	dnsResolverTimeout = 3 * time.Second
)

var applicationResolver atomic.Pointer[net.Resolver]

// ValidateDNSServer 验证自定义 DNS 服务器配置。
func ValidateDNSServer(dnsServer string) error {
	_, _, err := parseDNSServer(dnsServer)
	return err
}

func setApplicationResolver(resolver *net.Resolver) {
	applicationResolver.Store(resolver)
}

func currentResolver() *net.Resolver {
	if resolver := applicationResolver.Load(); resolver != nil {
		return resolver
	}
	return net.DefaultResolver
}

func newDNSResolver(dnsServer string) (*net.Resolver, error) {
	network, address, err := parseDNSServer(dnsServer)
	if err != nil {
		return nil, err
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, requestedNetwork, _ string) (net.Conn, error) {
			resolvedNetwork := requestedNetwork
			if network == "tcp" {
				resolvedNetwork = "tcp"
			}
			dialer := net.Dialer{Timeout: dnsResolverTimeout}
			return dialer.DialContext(ctx, resolvedNetwork, address)
		},
	}, nil
}

func isValidDNSHostname(host string) bool {
	host = strings.TrimSuffix(host, ".")
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !((char >= 'a' && char <= 'z') ||
				(char >= 'A' && char <= 'Z') ||
				(char >= '0' && char <= '9') || char == '-') {
				return false
			}
		}
	}
	return true
}

func parseDNSServer(dnsServer string) (network, address string, err error) {
	dnsServer = strings.TrimSpace(dnsServer)
	if dnsServer == "" {
		return "", "", errors.New("DNS 服务器地址为空")
	}

	if strings.Contains(dnsServer, "://") {
		parsed, parseErr := url.Parse(dnsServer)
		if parseErr != nil {
			return "", "", fmt.Errorf("解析 DNS 服务器地址失败: %w", parseErr)
		}
		switch parsed.Scheme {
		case "udp", "tcp":
			network = parsed.Scheme
		default:
			return "", "", fmt.Errorf("不支持的 DNS 协议: %s", parsed.Scheme)
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
			return "", "", fmt.Errorf("无效的 DNS 服务器地址: %s", dnsServer)
		}
		dnsServer = parsed.Host
	}

	host, port := "", "53"
	switch {
	case net.ParseIP(dnsServer) != nil:
		host = dnsServer
	case strings.HasPrefix(dnsServer, "[") && strings.HasSuffix(dnsServer, "]"):
		host = strings.TrimSuffix(strings.TrimPrefix(dnsServer, "["), "]")
		if net.ParseIP(host) == nil {
			return "", "", fmt.Errorf("无效的 DNS 服务器地址: %s", dnsServer)
		}
	default:
		var splitErr error
		host, port, splitErr = net.SplitHostPort(dnsServer)
		if splitErr != nil {
			if strings.Contains(dnsServer, ":") {
				return "", "", fmt.Errorf("无效的 DNS 服务器地址: %s", dnsServer)
			}
			host, port = dnsServer, "53"
		}
	}

	if net.ParseIP(host) == nil && !isValidDNSHostname(host) {
		return "", "", fmt.Errorf("无效的 DNS 服务器主机: %s", host)
	}
	portNumber, parseErr := strconv.Atoi(port)
	if parseErr != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", fmt.Errorf("无效的 DNS 服务器端口: %s", port)
	}
	return network, net.JoinHostPort(host, port), nil
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

// TrustedProxiesENV lists comma-separated proxy IPs or CIDRs. Empty trusts none.
const TrustedProxiesENV = "DNET_TRUSTED_PROXIES"

func isTrustedProxy(ip string) bool {
	address := net.ParseIP(ip)
	if address == nil {
		return false
	}
	for _, entry := range strings.Split(os.Getenv(TrustedProxiesENV), ",") {
		entry = strings.TrimSpace(entry)
		if proxy := net.ParseIP(entry); proxy != nil && proxy.Equal(address) {
			return true
		}
		if _, network, err := net.ParseCIDR(entry); err == nil && network.Contains(address) {
			return true
		}
	}
	return false
}

// GetClientIP trusts forwarding headers only from explicitly configured proxies.
func GetClientIP(r *http.Request) string {
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}
	if !isTrustedProxy(peer) {
		return peer
	}
	if values := r.Header.Values("X-Forwarded-For"); len(values) > 0 {
		hops := strings.Split(strings.Join(values, ","), ",")
		// Walk from the nearest proxy; never accept a spoofed prefix before an
		// untrusted hop. Malformed chains fail closed for WAN authorization.
		for i := len(hops) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(hops[i]))
			if ip == nil {
				return ""
			}
			if !isTrustedProxy(ip.String()) || i == 0 {
				return ip.String()
			}
		}
	}
	if value := r.Header.Get("X-Real-IP"); value != "" {
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
			return ip.String()
		}
		return ""
	}
	// A proxy without client metadata cannot establish private-client access.
	return ""
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
			Warn(LogTypeNetwork, "通过接口获取 %s 失败! 接口地址: %s", config.addrTypeName, url)
			Warn(LogTypeNetwork, "异常信息: %s", err)
			continue
		}

		// 读取响应体
		lr := io.LimitReader(resp.Body, maxResponseBodySize)
		body, err := io.ReadAll(lr)
		_ = resp.Body.Close()

		if err != nil {
			Warn(LogTypeNetwork, "读取响应失败: %s", err)
			continue
		}

		// 使用正则提取地址
		result := config.regex.FindString(string(body))
		if result == "" {
			Warn(LogTypeNetwork, "获取 %s 结果失败! 接口: %s, 返回值: %s", config.addrTypeName, url, string(body))
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
		Warn(LogTypeNetwork, "命令为空，无法获取地址")
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
		Warn(LogTypeNetwork, "执行命令失败: %s, 错误: %v", cmd, err)
		return ""
	}

	// 使用正则提取地址
	result := regex.FindString(string(out))
	if result == "" {
		Warn(LogTypeNetwork, "未能从命令输出中提取%s地址: %s", addrType, cmd)
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

// findAddrInInterfacesWithRegex 在接口列表中使用正则匹配查找地址
func findAddrInInterfacesWithRegex(interfaces []NetInterface, interfaceName, regexStr string) string {
	for _, netInterface := range interfaces {
		if netInterface.Name == interfaceName && len(netInterface.Address) > 0 {
			// 如果没有提供正则，返回第一个地址
			if regexStr == "" {
				return netInterface.Address[0]
			}

			// 处理 @n 格式（例如 @1 表示第一个地址，@2 表示第二个地址）
			if strings.HasPrefix(regexStr, "@") {
				indexStr := strings.TrimPrefix(regexStr, "@")
				if index, err := strconv.Atoi(indexStr); err == nil {
					// 索引从 1 开始，转换为数组索引（从 0 开始）
					if index >= 1 && index <= len(netInterface.Address) {
						return netInterface.Address[index-1]
					}
				}
			}

			// 使用正则表达式匹配
			regex, err := regexp.Compile(regexStr)
			if err != nil {
				Warn(LogTypeNetwork, "正则表达式编译失败: %s, 错误: %v", regexStr, err)
				// 正则错误时返回第一个地址
				return netInterface.Address[0]
			}

			// 遍历所有地址，返回第一个匹配的
			for _, addr := range netInterface.Address {
				if regex.MatchString(addr) {
					return addr
				}
			}

			// 没有匹配到，返回第一个地址作为后备
			Warn(LogTypeNetwork, "正则表达式未匹配到地址: %s, 使用第一个地址", regexStr)
			return netInterface.Address[0]
		}
	}
	return ""
}

// GetAddrFromInterface 从网络接口获取地址
func GetAddrFromInterface(interfaceName string, addrType string) string {
	ipv4, ipv6, err := GetNetInterface()
	if err != nil {
		Warn(LogTypeNetwork, "获取网络接口失败: %v", err)
		return ""
	}

	var result string
	if addrType == IPv4 {
		result = findAddrInInterfaces(ipv4, interfaceName)
		if result == "" {
			Warn(LogTypeNetwork, "未找到IPv4接口: %s", interfaceName)
		}
	} else if addrType == IPv6 {
		result = findAddrInInterfaces(ipv6, interfaceName)
		if result == "" {
			Warn(LogTypeNetwork, "未找到IPv6接口: %s", interfaceName)
		}
	}

	return result
}

// GetAddrFromInterfaceWithRegex 从网络接口获取地址（支持正则匹配）
func GetAddrFromInterfaceWithRegex(interfaceName string, addrType string, regexStr string) string {
	ipv4, ipv6, err := GetNetInterface()
	if err != nil {
		Warn(LogTypeNetwork, "获取网络接口失败: %v", err)
		return ""
	}

	var result string
	if addrType == IPv4 {
		result = findAddrInInterfacesWithRegex(ipv4, interfaceName, regexStr)
		if result == "" {
			Warn(LogTypeNetwork, "未找到IPv4接口: %s", interfaceName)
		}
	} else if addrType == IPv6 {
		result = findAddrInInterfacesWithRegex(ipv6, interfaceName, regexStr)
		if result == "" {
			Warn(LogTypeNetwork, "未找到IPv6接口: %s", interfaceName)
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

func dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	requestDialer := *dialer
	requestDialer.Resolver = currentResolver()
	return requestDialer.DialContext(ctx, network, address)
}

func tlsClientConfig(skipVerify bool) *tls.Config {
	if !skipVerify {
		return nil
	}
	// #nosec G402 -- 仅在用户显式传入 -skipVerify 时启用。
	return &tls.Config{InsecureSkipVerify: true}
}

func createDefaultTransport(skipVerify bool) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialContext(ctx, network, address)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		TLSClientConfig:       tlsClientConfig(skipVerify),
	}
}

// createNoProxyTransport 创建无代理的 HTTP Transport。
func createNoProxyTransport(network string, skipVerify bool) *http.Transport {
	return &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			return dialContext(ctx, network, address)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       idleConnTimeout,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ExpectContinueTimeout: expectContinueTimeout,
		TLSClientConfig:       tlsClientConfig(skipVerify),
	}
}

var (
	transportMu          sync.RWMutex
	defaultTransport     = createDefaultTransport(false)
	noProxyTcp4Transport = createNoProxyTransport("tcp4", false)
	noProxyTcp6Transport = createNoProxyTransport("tcp6", false)
	strictTransport      = createDefaultTransport(false)
)

// ConfigureHTTPClients 配置业务 HTTP 客户端。应在启动任何业务请求前调用。
func ConfigureHTTPClients(skipVerify bool) {
	newDefault := createDefaultTransport(skipVerify)
	newTCP4 := createNoProxyTransport("tcp4", skipVerify)
	newTCP6 := createNoProxyTransport("tcp6", skipVerify)

	transportMu.Lock()
	oldDefault := defaultTransport
	oldTCP4 := noProxyTcp4Transport
	oldTCP6 := noProxyTcp6Transport
	defaultTransport = newDefault
	noProxyTcp4Transport = newTCP4
	noProxyTcp6Transport = newTCP6
	transportMu.Unlock()

	oldDefault.CloseIdleConnections()
	oldTCP4.CloseIdleConnections()
	oldTCP6.CloseIdleConnections()
}

// CreateHTTPClient Create Default HTTP Client
func CreateHTTPClient() *http.Client {
	transportMu.RLock()
	transport := defaultTransport
	transportMu.RUnlock()
	return &http.Client{
		Timeout:   httpClientTimeout,
		Transport: transport,
	}
}

// CreateStrictHTTPClient 创建始终严格校验证书的 HTTP 客户端。
func CreateStrictHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   httpClientTimeout,
		Transport: strictTransport,
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

// GetHTTPResponseOrg 处理 HTTP 结果，返回 byte
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

// CreateNoProxyHTTPClient Create NoProxy HTTP Client
func CreateNoProxyHTTPClient(network string) *http.Client {
	transportMu.RLock()
	transport := noProxyTcp4Transport
	if network == "tcp6" {
		transport = noProxyTcp6Transport
	}
	transportMu.RUnlock()

	return &http.Client{
		Timeout:   httpClientTimeout,
		Transport: transport,
	}
}
