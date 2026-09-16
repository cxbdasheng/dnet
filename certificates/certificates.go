// Package certificates stores and validates managed TLS certificates.
package certificates

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"
)

type Certificate struct {
	EABDisabled bool      `json:"eab_disabled" yaml:"eab_disabled,omitempty"`
	EABKeyID    string    `json:"eab_key_id,omitempty" yaml:"eab_key_id,omitempty"`
	EABHMACKey  string    `json:"-" yaml:"eab_hmac_key,omitempty"`
	Email       string    `json:"email" yaml:"email,omitempty"`
	Domains     []string  `json:"domains" yaml:"domains,omitempty"`
	CA          string    `json:"ca" yaml:"ca,omitempty"`
	Challenge   string    `json:"challenge" yaml:"challenge,omitempty"`
	Listen      string    `json:"listen" yaml:"listen,omitempty"`
	AccessKey   string    `json:"access_key,omitempty" yaml:"access_key,omitempty"`
	ZoneID      string    `json:"zone_id" yaml:"zone_id,omitempty"`
	APIToken    string    `json:"-" yaml:"api_token,omitempty"`
	AccountKey  string    `json:"-" yaml:"account_key,omitempty"`
	AutoRenew   bool      `json:"auto_renew" yaml:"auto_renew"`
	RenewDays   int       `json:"renew_days" yaml:"renew_days,omitempty"`
	AcceptTerms bool      `json:"accept_terms" yaml:"accept_terms"`
	LastAttempt time.Time `json:"last_attempt" yaml:"last_attempt,omitempty"`
	LastError   string    `json:"last_error,omitempty" yaml:"last_error,omitempty"`

	ID       string `json:"id" yaml:"id"`
	Name     string `json:"name" yaml:"name"`
	Source   string `json:"source" yaml:"source"`
	CertPath string `json:"cert_path" yaml:"cert_path,omitempty"`
	KeyPath  string `json:"key_path" yaml:"key_path,omitempty"`
	CertPEM  string `json:"-" yaml:"cert_pem,omitempty"`
	KeyPEM   string `json:"-" yaml:"key_pem,omitempty"`
}
type Summary struct {
	Certificate
	Domains   []string  `json:"covered_domains"`
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	DaysLeft  int       `json:"days_left"`
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	UsedBy    []string  `json:"used_by"`
}

func Load(c Certificate) (tls.Certificate, error) {
	var pair tls.Certificate
	var err error
	switch c.Source {
	case "upload", "acme":
		pair, err = tls.X509KeyPair([]byte(c.CertPEM), []byte(c.KeyPEM))
	case "path":
		for _, p := range []string{c.CertPath, c.KeyPath} {
			info, e := os.Stat(p)
			if e != nil {
				return pair, e
			}
			if !info.Mode().IsRegular() || info.Size() > 1<<20 {
				return pair, fmt.Errorf("证书和私钥必须为不超过 1 MiB 的普通文件")
			}
		}
		pair, err = tls.LoadX509KeyPair(c.CertPath, c.KeyPath)
	default:
		return pair, fmt.Errorf("证书来源无效")
	}
	if err != nil {
		return pair, fmt.Errorf("证书或私钥格式错误，或两者不匹配")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return pair, fmt.Errorf("无法解析证书")
	}
	pair.Leaf = leaf
	return pair, nil
}
func Validate(c Certificate) error {
	if c.ID == "" || strings.TrimSpace(c.Name) == "" {
		return fmt.Errorf("证书 ID 和名称不能为空")
	}
	if c.Source == "acme" {
		return ValidateACME(c)
	}
	_, err := Load(c)
	return err
}
func Describe(c Certificate, now time.Time) Summary {
	s := Summary{Certificate: c, Domains: []string{}, UsedBy: []string{}}
	s.APIToken = ""
	s.AccountKey = ""
	s.EABHMACKey = ""
	s.CertPEM = ""
	s.KeyPEM = ""
	if c.Source == "acme" && c.CertPEM == "" {
		s.State = "not_issued"
		return s
	}
	pair, err := Load(c)
	if err != nil {
		s.State = "invalid"
		s.Error = err.Error()
		return s
	}
	leaf := pair.Leaf
	s.Domains = append(s.Domains, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		s.Domains = append(s.Domains, ip.String())
	}
	s.Issuer = leaf.Issuer.String()
	s.NotBefore = leaf.NotBefore
	s.NotAfter = leaf.NotAfter
	s.DaysLeft = int(leaf.NotAfter.Sub(now).Hours() / 24)
	s.State = "valid"
	if now.Before(leaf.NotBefore) {
		s.State = "pending"
	} else if !now.Before(leaf.NotAfter) {
		s.State = "expired"
	} else if leaf.NotAfter.Sub(now) < 30*24*time.Hour {
		s.State = "expiring"
	}
	return s
}

func Clone(in []Certificate) []Certificate {
	out := append([]Certificate(nil), in...)
	for i := range out {
		out[i].Domains = append([]string(nil), out[i].Domains...)
	}
	return out
}
