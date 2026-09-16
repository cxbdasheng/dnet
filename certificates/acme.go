package certificates

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
)

const ZeroSSLCA = "https://acme.zerossl.com/v2/DV90"

const ProductionCA = "https://acme-v02.api.letsencrypt.org/directory"
const StagingCA = "https://acme-staging-v02.api.letsencrypt.org/directory"

func ValidateACME(c Certificate) error {
	if c.CA != ProductionCA && c.CA != StagingCA && c.CA != ZeroSSLCA {
		return fmt.Errorf("请选择支持的证书颁发机构")
	}
	if c.CA == ZeroSSLCA && !c.EABDisabled {
		if _, err := externalBinding(c); err != nil {
			return err
		}
	}
	if _, err := mail.ParseAddress(c.Email); err != nil || strings.ContainsAny(c.Email, "<>\r\n") {
		return fmt.Errorf("请填写有效邮箱")
	}
	if !c.AcceptTerms {
		return fmt.Errorf("请确认同意证书机构的服务条款")
	}
	if c.RenewDays < 1 || c.RenewDays > 60 {
		return fmt.Errorf("提前续期天数必须为 1–60")
	}
	if len(c.Domains) == 0 || len(c.Domains) > 20 {
		return fmt.Errorf("请填写 1–20 个域名")
	}
	seen := map[string]bool{}
	for _, d := range c.Domains {
		if seen[d] {
			return fmt.Errorf("域名重复")
		}
		seen[d] = true
		name := strings.TrimPrefix(d, "*.")
		if name != d && !dnsProvider(c.Challenge) {
			return fmt.Errorf("通配符证书需要 DNS 验证")
		}
		if net.ParseIP(name) != nil || !strings.Contains(name, ".") || len(name) > 253 {
			return fmt.Errorf("请填写完整域名，中文域名使用 Punycode")
		}
		for _, label := range strings.Split(name, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return fmt.Errorf("域名格式错误")
			}
			for _, v := range label {
				if !(v >= 'a' && v <= 'z' || v >= '0' && v <= '9' || v == '-') {
					return fmt.Errorf("域名请使用小写字母、数字和连字符")
				}
			}
		}
	}
	switch c.Challenge {
	case "http", "tls-alpn":
		if _, err := net.ResolveTCPAddr("tcp", c.Listen); err != nil || c.Listen == "" {
			return fmt.Errorf("HTTP 验证监听地址无效")
		}
	case "aliyun", "tencent":
		if c.AccessKey == "" || c.APIToken == "" {
			return fmt.Errorf("请填写 DNS 服务商的密钥 ID 和密钥")
		}
		if c.ZoneID != "" && (strings.ContainsAny(c.ZoneID, " /:*\\") || !strings.Contains(c.ZoneID, ".")) {
			return fmt.Errorf("请填写 DNS 托管区域域名，例如 example.com")
		}
		for _, d := range c.Domains {
			d = strings.TrimPrefix(d, "*.")
			if c.ZoneID != "" && d != c.ZoneID && !strings.HasSuffix(d, "."+c.ZoneID) {
				return fmt.Errorf("申请域名必须属于指定 DNS 托管区域")
			}
		}
	case "cloudflare":
		if c.APIToken == "" {
			return fmt.Errorf("请填写 Cloudflare API Token")
		}
	default:
		return fmt.Errorf("验证方式无效")
	}
	return nil
}
func EnsureAccount(c *Certificate) error {
	if c.AccountKey != "" {
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	b, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	c.AccountKey = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}))
	return nil
}

type challengeResponses struct {
	mu     sync.RWMutex
	values map[string]string
}

func (h *challengeResponses) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	v, ok := h.values[r.Host+"\n"+r.URL.Path]
	if !ok {
		host, _, _ := net.SplitHostPort(r.Host)
		v, ok = h.values[host+"\n"+r.URL.Path]
	}
	h.mu.RUnlock()
	if !ok || r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, v)
}
func Issue(ctx context.Context, c Certificate) (Certificate, error) {
	return issue(ctx, c, &http.Client{Timeout: 45 * time.Second})
}

func issue(ctx context.Context, c Certificate, httpClient *http.Client) (Certificate, error) {
	if err := ValidateACME(c); err != nil {
		return c, err
	}
	if err := EnsureAccount(&c); err != nil {
		return c, err
	}
	block, _ := pem.Decode([]byte(c.AccountKey))
	if block == nil {
		return c, fmt.Errorf("ACME 账号密钥无效")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return c, err
	}
	key, ok := parsed.(crypto.Signer)
	if !ok {
		return c, fmt.Errorf("ACME 账号密钥不支持")
	}
	client := &acme.Client{Key: key, DirectoryURL: c.CA, HTTPClient: httpClient, UserAgent: "D-NET ACME"}
	account := &acme.Account{Contact: []string{"mailto:" + c.Email}}
	if c.CA == ZeroSSLCA && !c.EABDisabled {
		account.ExternalAccountBinding, err = externalBinding(c)
		if err != nil {
			return c, err
		}
	}
	if c.CA == ZeroSSLCA && c.EABDisabled {
		if _, err = client.GetReg(ctx, ""); err != nil {
			return c, fmt.Errorf("无法复用 ZeroSSL 账户，请开启 EAB 认证完成账户绑定")
		}
	} else {
		if _, err = client.Register(ctx, account, func(string) bool { return c.AcceptTerms }); err != nil && err != acme.ErrAccountAlreadyExists {
			return c, err
		}
	}
	challenges := &challengeResponses{values: map[string]string{}}
	if c.Challenge == "http" {
		listener, e := net.Listen("tcp", c.Listen)
		if e != nil {
			return c, fmt.Errorf("HTTP 验证端口监听失败: %w", e)
		}
		server := &http.Server{Handler: challenges, ReadHeaderTimeout: 10 * time.Second}
		defer server.Close()
		go server.Serve(listener)
	}
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(c.Domains...))
	if err != nil {
		return c, err
	}
	for _, authURL := range order.AuthzURLs {
		auth, e := client.GetAuthorization(ctx, authURL)
		if e != nil {
			return c, e
		}
		if auth.Status == acme.StatusValid {
			continue
		}
		kind := "http-01"
		if c.Challenge == "tls-alpn" {
			kind = "tls-alpn-01"
		}
		if dnsProvider(c.Challenge) {
			kind = "dns-01"
		}
		var chosen *acme.Challenge
		for _, challenge := range auth.Challenges {
			if challenge.Type == kind {
				chosen = challenge
				break
			}
		}
		if chosen == nil {
			return c, fmt.Errorf("证书机构未提供所需验证方式")
		}
		cleanup := func() {}
		if kind == "http-01" {
			value, e := client.HTTP01ChallengeResponse(chosen.Token)
			if e != nil {
				return c, e
			}
			challenges.mu.Lock()
			challenges.values[auth.Identifier.Value+"\n"+client.HTTP01ChallengePath(chosen.Token)] = value
			challenges.mu.Unlock()
		} else if kind == "tls-alpn-01" {
			cleanup, e = presentTLSALPN(client, c.Listen, auth.Identifier.Value, chosen.Token)
			if e != nil {
				return c, e
			}
		} else {
			value, e := client.DNS01ChallengeRecord(chosen.Token)
			if e != nil {
				return c, e
			}
			name := "_acme-challenge." + strings.TrimPrefix(auth.Identifier.Value, "*.")
			cleanup, e = presentDNS(ctx, c, name, value)
			if e != nil {
				return c, e
			}
			if e = waitTXT(ctx, name, value); e != nil {
				cleanup()
				return c, e
			}
		}
		_, e = client.Accept(ctx, chosen)
		if e == nil {
			_, e = client.WaitAuthorization(ctx, authURL)
		}
		cleanup()
		if e != nil {
			return c, e
		}
	}
	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return c, err
	}
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return c, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: c.Domains}, certKey)
	if err != nil {
		return c, err
	}
	chain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return c, err
	}
	c.CertPEM = ""
	for _, der := range chain {
		c.CertPEM += string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	}
	raw, err := x509.MarshalPKCS8PrivateKey(certKey)
	if err != nil {
		return c, err
	}
	c.KeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}))
	_, err = Load(c)
	return c, err
}
func waitTXT(ctx context.Context, name, value string) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		records, _ := net.DefaultResolver.LookupTXT(ctx, name)
		for _, record := range records {
			if record == value {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("DNS 验证记录尚未生效: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// Due uses a fraction of validity for short-lived certificates, avoiding immediate renewal loops.
func Due(c Certificate, now time.Time) bool {
	if c.Source != "acme" || !c.AutoRenew || now.Sub(c.LastAttempt) < 6*time.Hour {
		return false
	}
	pair, err := Load(c)
	if err != nil {
		return true
	}
	before := time.Duration(c.RenewDays) * 24 * time.Hour
	if third := pair.Leaf.NotAfter.Sub(pair.Leaf.NotBefore) / 3; before > third {
		before = third
	}
	return !now.Before(pair.Leaf.NotAfter.Add(-before))
}

func externalBinding(c Certificate) (*acme.ExternalAccountBinding, error) {
	if strings.TrimSpace(c.EABKeyID) == "" || strings.TrimSpace(c.EABHMACKey) == "" {
		return nil, fmt.Errorf("ZeroSSL 需要 EAB Key ID 和 HMAC Key")
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(c.EABHMACKey), "="))
	if err != nil || len(key) == 0 {
		return nil, fmt.Errorf("EAB HMAC Key 格式无效，请填写 Base64URL 编码的密钥")
	}
	return &acme.ExternalAccountBinding{KID: strings.TrimSpace(c.EABKeyID), Key: key}, nil
}
