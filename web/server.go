package web

import (
	"context"
	"net/http"
	"sync"

	"github.com/cxbdasheng/dnet/certificates"
	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/forward"
	"github.com/cxbdasheng/dnet/webservice"
)

type SyncService interface {
	TriggerDCDNSyncAsync()
	TriggerDDNSSyncAsync()
}

type Server struct {
	certificateIssuer  func(context.Context, certificates.Certificate) (certificates.Certificate, error)
	certificateJobs    map[string]bool
	certificateContext context.Context
	WebServices        *webservice.Manager
	webServiceMu       sync.Mutex
	Forwarder          *forward.Manager
	forwardMu          sync.Mutex
	configRepo         config.Repository
	syncer             SyncService
}

func NewServer(configRepo config.Repository, syncer SyncService) *Server {
	return &Server{
		configRepo: configRepo,
		syncer:     syncer,
	}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/static/", s.AuthAssert(staticFsFunc))
	mux.HandleFunc("/favicon.ico", s.AuthAssert(faviconFsFunc))

	mux.HandleFunc("/", s.Auth(s.Home))
	mux.HandleFunc("/dcdn", s.Auth(s.DCDN))
	mux.HandleFunc("/certificates", s.Auth(s.CertificatesPage))
	mux.HandleFunc("/api/certificates", s.Auth(s.CertificatesAPI))
	mux.HandleFunc("/webservice", s.Auth(s.WebServicePage))
	mux.HandleFunc("/api/webservice", s.Auth(s.WebServiceAPI))
	mux.HandleFunc("/forward", s.Auth(s.ForwardPage))
	mux.HandleFunc("/api/forward", s.Auth(s.ForwardAPI))
	mux.HandleFunc("/api/forward/probe", s.Auth(s.ForwardProbe))
	mux.HandleFunc("/api/forward/logs", s.Auth(s.ForwardLogs))
	mux.HandleFunc("/ddns", s.Auth(s.DDNS))
	mux.HandleFunc("/api/dcdn/config", s.Auth(s.DCDNConfigAPI))
	mux.HandleFunc("/api/dcdn/upyun/token", s.Auth(s.UpyunToken))
	mux.HandleFunc("/dcdn/upyun/token-dialog", s.Auth(s.UpyunTokenDialog))
	mux.HandleFunc("/webhook", s.Auth(s.Webhook))
	mux.HandleFunc("/mock", s.Auth(s.Mock))
	mux.HandleFunc("/settings", s.Auth(s.Settings))
	mux.HandleFunc("/logs/count", s.Auth(s.LogsCount))
	mux.HandleFunc("/logs", s.Auth(s.Logs))
	mux.HandleFunc("/login", s.AuthAssert(s.Login))
	mux.HandleFunc("/logout", s.AuthAssert(s.Logout))
}
