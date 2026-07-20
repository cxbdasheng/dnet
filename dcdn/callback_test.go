package dcdn

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxbdasheng/dnet/config"
)

// newCallbackCDN 构造一个 Callback 测试配置（两个静态源站）
func newCallbackCDN(url, body string) (*config.CDN, *Cache) {
	cdn := &config.CDN{
		Domain:       "cdn.example.com",
		AccessKey:    url,  // 回调 URL
		AccessSecret: body, // 请求内容（可选）
		Sources: []config.Source{
			{Type: "ipv4", Value: "1.2.3.4", Priority: "1", Weight: "10", Port: "80", HttpsPort: "443", Protocol: "http"},
			{Type: "ipv4", Value: "5.6.7.8", Priority: "2", Weight: "20", Port: "80", HttpsPort: "443", Protocol: "https"},
		},
	}
	c := NewCache()
	return cdn, &c
}

func TestDCDNCallbackGETWithVariables(t *testing.T) {
	var gotMethod, gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	url := srv.URL + "/hook?domain=#{domain}&ips=#{ips}"
	cdn, cache := newCallbackCDN(url, "")
	c := &Callback{}
	c.Init(cdn, cache)
	c.UpdateOrCreateSources()

	if c.GetServiceStatus() != string(UpdatedSuccess) {
		t.Fatalf("期望成功, 实际状态: %s", c.GetServiceStatus())
	}
	if gotMethod != http.MethodGet {
		t.Errorf("期望 GET 请求, 实际: %s", gotMethod)
	}
	want := "domain=cdn.example.com&ips=1.2.3.4,5.6.7.8"
	if gotQuery != want {
		t.Errorf("查询参数变量替换不正确\n期望: %s\n实际: %s", want, gotQuery)
	}
}

func TestDCDNCallbackPOSTWithSourcesJSON(t *testing.T) {
	var gotMethod, gotBody, gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := `{"domain":"#{domain}","sources":#{sources}}`
	cdn, cache := newCallbackCDN(srv.URL+"/hook", body)
	c := &Callback{}
	c.Init(cdn, cache)
	c.UpdateOrCreateSources()

	if c.GetServiceStatus() != string(UpdatedSuccess) {
		t.Fatalf("期望成功, 实际状态: %s", c.GetServiceStatus())
	}
	if gotMethod != http.MethodPost {
		t.Errorf("有请求体时应为 POST, 实际: %s", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("合法 JSON 应使用 application/json, 实际: %s", gotContentType)
	}
	// 校验源站 JSON 展开正确
	if !strings.Contains(gotBody, `"addr":"1.2.3.4"`) || !strings.Contains(gotBody, `"addr":"5.6.7.8"`) {
		t.Errorf("请求体应包含两个源站地址, 实际: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"priority":"1"`) || !strings.Contains(gotBody, `"protocol":"https"`) {
		t.Errorf("请求体应包含源站元数据, 实际: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"domain":"cdn.example.com"`) {
		t.Errorf("请求体应包含域名, 实际: %s", gotBody)
	}
}

func TestDCDNCallbackFailsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		io.WriteString(w, "upstream error")
	}))
	defer srv.Close()

	cdn, cache := newCallbackCDN(srv.URL+"/hook", "")
	c := &Callback{}
	c.Init(cdn, cache)
	c.UpdateOrCreateSources()

	if c.GetServiceStatus() != string(UpdatedFailed) {
		t.Fatalf("期望失败, 实际状态: %s", c.GetServiceStatus())
	}
}

func TestDCDNCallbackIncompleteConfig(t *testing.T) {
	cdn, cache := newCallbackCDN("", "") // 缺少回调 URL
	c := &Callback{}
	c.Init(cdn, cache)

	if c.GetServiceStatus() != string(InitFailed) {
		t.Fatalf("缺少 URL 应初始化失败, 实际状态: %s", c.GetServiceStatus())
	}
	// 初始化失败时不应发起回调
	if c.UpdateOrCreateSources() {
		t.Error("初始化失败时不应更新")
	}
}

func TestDCDNCallbackBuildSourcesJSON(t *testing.T) {
	cdn, cache := newCallbackCDN("http://x", "")
	c := &Callback{}
	c.Init(cdn, cache)

	got := c.buildSourcesJSON()
	want := `[{"addr":"1.2.3.4","priority":"1","weight":"10","port":"80","https_port":"443","protocol":"http"},` +
		`{"addr":"5.6.7.8","priority":"2","weight":"20","port":"80","https_port":"443","protocol":"https"}]`
	if got != want {
		t.Errorf("buildSourcesJSON 不正确\n期望: %s\n实际: %s", want, got)
	}
}

func TestDCDNCallbackBuildIPs(t *testing.T) {
	cdn, cache := newCallbackCDN("http://x", "")
	c := &Callback{}
	c.Init(cdn, cache)

	if got := c.buildIPs(); got != "1.2.3.4,5.6.7.8" {
		t.Errorf("buildIPs = %q, 期望 %q", got, "1.2.3.4,5.6.7.8")
	}
}
