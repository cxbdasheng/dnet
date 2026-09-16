package web

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cxbdasheng/dnet/certificates"
	"github.com/cxbdasheng/dnet/helper"
)

//go:embed certificates.html
var certificatePage embed.FS

func (s *Server) CertificatesPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	b, _ := certificatePage.ReadFile("certificates.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(b)
}
func (s *Server) CertificatesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if r.Method == "POST" {
		media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "application/json" {
			http.Error(w, "请使用 JSON 提交", 415)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Host != r.Host {
				http.Error(w, "请求来源不匹配", 403)
				return
			}
		}
		var input struct {
			Action      string                   `json:"action"`
			Certificate certificates.Certificate `json:"certificate"`
			CertPEM     string                   `json:"cert_pem"`
			KeyPEM      string                   `json:"key_pem"`
			EABHMACKey  string                   `json:"eab_hmac_key"`
			Token       string                   `json:"api_token"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 3<<20))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&input); err != nil {
			helper.ReturnError(w, "提交格式错误")
			return
		}
		if dec.Decode(new(any)) != io.EOF {
			helper.ReturnError(w, "请求只能包含一个 JSON 对象")
			return
		}
		if input.Action == "issue" {
			if err := s.startCertificateJob(input.Certificate.ID); err != nil {
				helper.ReturnError(w, err.Error())
				return
			}
			helper.ReturnSuccess(w, "申请任务已启动", nil)
			return
		}
		s.webServiceMu.Lock()
		defer s.webServiceMu.Unlock()
		conf, err := s.configRepo.Load()
		if err != nil {
			helper.ReturnError(w, "加载配置失败")
			return
		}
		if s.certificateJobs[input.Certificate.ID] {
			helper.ReturnError(w, "该证书正在申请，请等待任务结束")
			return
		}
		index := -1
		var old certificates.Certificate
		for i, c := range conf.Certificates {
			if c.ID == input.Certificate.ID {
				index = i
				old = c
				break
			}
		}
		switch input.Action {
		case "save":
			c := input.Certificate
			if c.ID == "" {
				if len(conf.Certificates) >= 128 {
					helper.ReturnError(w, "最多支持 128 张证书")
					return
				}
				b := make([]byte, 16)
				rand.Read(b)
				c.ID = hex.EncodeToString(b)
			} else if index < 0 {
				helper.ReturnError(w, "证书不存在")
				return
			}
			if c.Challenge == "aliyun" || c.Challenge == "tencent" || c.Challenge == "cloudflare" {
				c.ZoneID = ""
			}
			c.CertPEM = old.CertPEM
			c.KeyPEM = old.KeyPEM
			c.APIToken = old.APIToken
			if c.Challenge != old.Challenge {
				c.APIToken = ""
			}
			c.AccountKey = old.AccountKey
			c.EABHMACKey = old.EABHMACKey
			if c.EABDisabled && c.CA == old.CA {
				c.EABKeyID = old.EABKeyID
			}
			if c.CA != old.CA || c.EABKeyID != old.EABKeyID {
				c.EABHMACKey = ""
			}
			if input.EABHMACKey != "" && !c.EABDisabled {
				c.EABHMACKey = strings.TrimSpace(input.EABHMACKey)
			}
			if c.CA != certificates.ZeroSSLCA || c.Source != "acme" {
				c.EABKeyID = ""
				c.EABHMACKey = ""
			}
			c.LastAttempt = old.LastAttempt
			c.LastError = old.LastError
			if input.CertPEM != "" {
				c.CertPEM = input.CertPEM
			}
			if input.KeyPEM != "" {
				c.KeyPEM = input.KeyPEM
			}
			if input.Token != "" {
				c.APIToken = input.Token
			}
			if c.CA != old.CA || c.EABKeyID != old.EABKeyID || c.EABHMACKey != old.EABHMACKey {
				c.AccountKey = ""
			}
			if c.Source == "path" {
				c.CertPEM = ""
				c.KeyPEM = ""
			}
			if c.Source != "acme" {
				c.APIToken = ""
				c.AccountKey = ""
				c.AutoRenew = false
			}
			for i := range c.Domains {
				c.Domains[i] = strings.ToLower(strings.TrimSpace(c.Domains[i]))
			}
			if err = certificates.Validate(c); err != nil {
				helper.ReturnError(w, err.Error())
				return
			}
			if index < 0 {
				conf.Certificates = append(conf.Certificates, c)
			} else {
				conf.Certificates[index] = c
			}
		case "delete":
			if index < 0 {
				helper.ReturnError(w, "证书不存在")
				return
			}
			for _, rule := range conf.WebServiceRules {
				if rule.CertificateID == old.ID {
					helper.ReturnError(w, "证书正被 DWS 配置引用，请先解除引用")
					return
				}
			}
			conf.Certificates = append(conf.Certificates[:index], conf.Certificates[index+1:]...)
		default:
			helper.ReturnError(w, "操作无效")
			return
		}
		if s.WebServices != nil && conf.WebServiceEnabled {
			err = s.WebServices.ApplyCertificates(conf.WebServiceRules, conf.Certificates, func() error { return s.configRepo.Save(&conf) })
		} else {
			err = s.configRepo.Save(&conf)
		}
		if err != nil {
			helper.ReturnError(w, err.Error())
			return
		}
		helper.ReturnSuccess(w, "证书配置已保存", nil)
		return
	}
	s.webServiceMu.Lock()
	defer s.webServiceMu.Unlock()
	conf, err := s.configRepo.Load()
	if err != nil {
		helper.ReturnError(w, "加载配置失败")
		return
	}
	type entry struct {
		certificates.Summary
		Running   bool `json:"running"`
		HasEABKey bool `json:"has_eab_key"`
		HasToken  bool `json:"has_token"`
	}
	list := []entry{}
	for _, c := range conf.Certificates {
		item := entry{Summary: certificates.Describe(c, time.Now()), Running: s.certificateJobs[c.ID], HasEABKey: c.EABHMACKey != "", HasToken: c.APIToken != ""}
		for _, rule := range conf.WebServiceRules {
			if rule.CertificateID == c.ID {
				item.UsedBy = append(item.UsedBy, rule.Name)
			}
		}
		list = append(list, item)
	}
	helper.ReturnSuccess(w, "", list)
}
func (s *Server) startCertificateJob(id string) error {
	s.webServiceMu.Lock()
	defer s.webServiceMu.Unlock()
	if s.certificateJobs == nil {
		s.certificateJobs = map[string]bool{}
	}
	for _, running := range s.certificateJobs {
		if running {
			return fmt.Errorf("已有证书申请任务正在运行，请稍后重试")
		}
	}
	conf, err := s.configRepo.Load()
	if err != nil {
		return err
	}
	index := -1
	for i, c := range conf.Certificates {
		if c.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("证书不存在")
	}
	c := conf.Certificates[index]
	if c.Source != "acme" {
		return fmt.Errorf("仅 ACME 证书支持自动申请")
	}
	if err = certificates.ValidateACME(c); err != nil {
		return err
	}
	if err = certificates.EnsureAccount(&c); err != nil {
		return err
	}
	c.LastAttempt = time.Now()
	conf.Certificates[index] = c
	if err = s.configRepo.Save(&conf); err != nil {
		return err
	}
	s.certificateJobs[id] = true
	helper.Info(helper.LogTypeSSL, "开始申请证书 [%s]", c.Name)
	base := s.certificateContext
	if base == nil {
		base = context.Background()
	}
	go func() {
		ctx, cancel := context.WithTimeout(base, 10*time.Minute)
		defer cancel()
		issuer := s.certificateIssuer
		if issuer == nil {
			issuer = certificates.Issue
		}
		result, issueErr := issuer(ctx, c)
		s.webServiceMu.Lock()
		defer s.webServiceMu.Unlock()
		delete(s.certificateJobs, id)
		latest, err := s.configRepo.Load()
		if err != nil {
			return
		}
		for i, current := range latest.Certificates {
			if current.ID == id {
				if issueErr != nil {
					current.LastError = issueErr.Error()
					if current.EABHMACKey != "" {
						current.LastError = strings.ReplaceAll(current.LastError, current.EABHMACKey, "[redacted]")
					}
					if current.APIToken != "" {
						current.LastError = strings.ReplaceAll(current.LastError, current.APIToken, "[redacted]")
					}
				} else {
					current.CertPEM = result.CertPEM
					current.KeyPEM = result.KeyPEM
					current.LastError = ""
				}
				if issueErr != nil {
					helper.Error(helper.LogTypeSSL, "证书申请失败 [%s]: %s", current.Name, current.LastError)
				} else {
					helper.Info(helper.LogTypeSSL, "证书申请完成 [%s]", current.Name)
				}
				latest.Certificates[i] = current
				if err = s.configRepo.Save(&latest); err != nil {
					helper.Error(helper.LogTypeSSL, "保存证书失败: %v", err)
					return
				}
				if issueErr == nil && s.WebServices != nil && latest.WebServiceEnabled {
					if err = s.WebServices.ApplyCertificates(latest.WebServiceRules, latest.Certificates, nil); err != nil {
						current.LastError = "证书已签发，DWS 加载失败: " + err.Error()
						latest.Certificates[i] = current
						s.configRepo.Save(&latest)
					}
				}
				return
			}
		}
	}()
	return nil
}
func (s *Server) RunCertificateMaintenance(ctx context.Context) {
	s.webServiceMu.Lock()
	s.certificateContext = ctx
	s.webServiceMu.Unlock()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	var fingerprint [32]byte
	for {
		if ctx.Err() != nil {
			return
		}
		conf, err := s.configRepo.Load()
		if err == nil {
			for _, c := range conf.Certificates {
				if certificates.Due(c, time.Now()) {
					s.startCertificateJob(c.ID)
					break
				}
			}
			// Reload path-backed certificates only when their actual certificate material changes.
			h := sha256.New()
			for _, c := range conf.Certificates {
				if c.Source == "path" {
					if pair, e := certificates.Load(c); e == nil {
						h.Write([]byte(c.ID))
						for _, der := range pair.Certificate {
							h.Write(der)
						}
					}
				}
			}
			var next [32]byte
			copy(next[:], h.Sum(nil))
			if next != fingerprint {
				s.webServiceMu.Lock()
				latest, e := s.configRepo.Load()
				if e == nil && s.WebServices != nil && latest.WebServiceEnabled {
					e = s.WebServices.ApplyCertificates(latest.WebServiceRules, latest.Certificates, nil)
				}
				s.webServiceMu.Unlock()
				if e == nil {
					fingerprint = next
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
