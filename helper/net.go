package helper

import (
	"context"
	"log"
	"net"
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
	conn, err := net.DialTimeout("udp", dnsServer, time.Second*3)
	if err != nil {
		return false
	}
	defer conn.Close()

	// 发送一个简单的DNS查询包测试
	// 这里简化处理，只测试连接
	return true
}

// InitBackupDNS 初始化备用DNS
func InitBackupDNS(customDNS string) {
	if customDNS != "" {
		SetDNS(customDNS)
		log.Printf("使用自定义DNS: %s", customDNS)
	} else {
		// 设置默认的备用DNS服务器
		defaultDNS := []string{"8.8.8.8", "1.1.1.1", "114.114.114.114"}
		for _, dns := range defaultDNS {
			if testDNSConnectivity(dns + ":53") {
				SetDNS(dns)
				log.Printf("使用备用DNS: %s", dns)
				return
			}
		}
		log.Println("所有备用DNS服务器均不可用，使用系统默认DNS")
	}
}
