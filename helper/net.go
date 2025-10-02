package helper

import (
	"context"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// SetDNS 设置自定义DNS服务器
func SetDNS(dnsServer string) {
	if dnsServer == "" {
		log.Println("DNS服务器地址为空，跳过设置")
		return
	}

	// 验证DNS服务器地址格式
	if !isValidDNSServer(dnsServer) {
		log.Printf("无效的DNS服务器地址: %s", dnsServer)
		return
	}

	// 添加默认端口
	if !strings.Contains(dnsServer, ":") {
		dnsServer = dnsServer + ":53"
	}

	// 测试DNS服务器连通性
	if !testDNSConnectivity(dnsServer) {
		log.Printf("DNS服务器 %s 连接测试失败", dnsServer)
		return
	}

	// 设置自定义DNS解析器
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Second * 3,
			}
			return d.DialContext(ctx, network, dnsServer)
		},
	}

	log.Printf("已设置自定义DNS服务器: %s", dnsServer)
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

// testDNSConnectivity 测试DNS服务器连通性
func testDNSConnectivity(dnsServer string) bool {
	// 并发测试TCP和UDP连接，提高成功率
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 使用channel收集结果
	resultChan := make(chan bool, 2)

	// 测试UDP连接
	go func() {
		conn, err := net.DialTimeout("udp", dnsServer, time.Second)
		if err == nil {
			conn.Close()
			resultChan <- true
			return
		}
		resultChan <- false
	}()

	// 测试TCP连接(作为备用)
	go func() {
		conn, err := net.DialTimeout("tcp", dnsServer, time.Second)
		if err == nil {
			conn.Close()
			resultChan <- true
			return
		}
		resultChan <- false
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
		log.Printf("使用自定义DNS: %s", customDNS)
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
				log.Printf("使用备用DNS: %s", result.dns)
				return
			}
		case <-ctx.Done():
			log.Println("DNS测试超时，使用系统默认DNS")
			return
		}
	}

	log.Println("所有备用DNS服务器均不可用，使用系统默认DNS")
}

// IsPrivateIP 检查IP地址是否为私有地址
func IsPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}

	// 检查IPv4私有地址
	if ip.To4() != nil {
		// 10.0.0.0/8
		if ip[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip[0] == 192 && ip[1] == 168 {
			return true
		}
		// 127.0.0.0/8 (环回地址)
		if ip[0] == 127 {
			return true
		}
		return false
	}

	// 检查IPv6私有地址
	// fe80::/10 (链路本地地址)
	if ip[0] == 0xfe && (ip[1]&0xc0) == 0x80 {
		return true
	}
	// fc00::/7 (唯一本地地址)
	if (ip[0] & 0xfe) == 0xfc {
		return true
	}
	// ::1/128 (环回地址)
	if ip.Equal(net.IPv6loopback) {
		return true
	}

	return false
}

// GetClientIP 获取客户端真实IP地址
func GetClientIP(r *http.Request) string {
	// 检查X-Forwarded-For头，获取最原始的客户端IP
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			if ip != "" && !IsPrivateIP(ip) {
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

	// 使用RemoteAddr，正确处理IPv6地址
	ip := r.RemoteAddr
	if strings.Contains(ip, ":") {
		// 处理IPv6地址格式 [ip]:port 或 ip:port
		if strings.HasPrefix(ip, "[") {
			// IPv6格式: [2001:db8::1]:8080
			if idx := strings.LastIndex(ip, "]:"); idx != -1 {
				return ip[1:idx]
			}
		} else {
			// IPv4格式: 192.168.1.1:8080 或纯IPv6
			if host, _, err := net.SplitHostPort(ip); err == nil {
				return host
			}
		}
	}
	return ip
}
