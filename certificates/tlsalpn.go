package certificates

import (
	"crypto/tls"
	"fmt"
	"golang.org/x/crypto/acme"
	"net"
	"net/http"
	"strings"
	"time"
)

func presentTLSALPN(client *acme.Client, address, domain, token string) (func(), error) {
	certificate, err := client.TLSALPN01ChallengeCert(token, domain)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("TLS 验证端口监听失败: %w", err)
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{acme.ALPNProto}, GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if !strings.EqualFold(hello.ServerName, domain) || len(hello.SupportedProtos) != 1 || hello.SupportedProtos[0] != acme.ALPNProto {
			return nil, fmt.Errorf("无匹配的 TLS 验证任务")
		}
		return &certificate, nil
	}}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second, TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){acme.ALPNProto: func(_ *http.Server, _ *tls.Conn, _ http.Handler) {}}}
	go server.Serve(tls.NewListener(listener, config))
	return func() { server.Close(); listener.Close() }, nil
}
