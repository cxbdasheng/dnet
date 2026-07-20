package ddns

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cxbdasheng/dnet/config"
)

// newCallbackGroup 构造一个 Callback 测试配置组（单条静态记录）
func newCallbackGroup(url, body string) (*config.DNSGroup, []*Cache) {
	group := &config.DNSGroup{
		Domain:       "www.example.com",
		AccessKey:    url,  // 回调 URL
		AccessSecret: body, // 请求内容（可选）
		TTL:          "600",
		Records: []config.DNSRecord{
			{Type: RecordTypeA, IPType: "static_ipv4", Value: "1.2.3.4"},
		},
	}
	c := NewCache()
	return group, []*Cache{&c}
}

func TestCallbackGETWithVariables(t *testing.T) {
	var gotMethod, gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	url := srv.URL + "/hook?ip=#{ip}&domain=#{domain}&type=#{recordType}&ttl=#{ttl}"
	group, caches := newCallbackGroup(url, "")
	c := &Callback{}
	c.Init(group, caches)
	results := c.UpdateOrCreateRecords()

	if len(results) != 1 || results[0].Status != UpdatedSuccess {
		t.Fatalf("期望 1 条成功结果, 实际: %+v", results)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("期望 GET 请求, 实际: %s", gotMethod)
	}
	want := "ip=1.2.3.4&domain=www.example.com&type=A&ttl=600"
	if gotQuery != want {
		t.Errorf("查询参数变量替换不正确\n期望: %s\n实际: %s", want, gotQuery)
	}
}

func TestCallbackPOSTWithJSONBody(t *testing.T) {
	var gotMethod, gotBody, gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	body := `{"ip":"#{ip}","domain":"#{domain}","type":"#{recordType}"}`
	group, caches := newCallbackGroup(srv.URL+"/hook", body)
	c := &Callback{}
	c.Init(group, caches)
	results := c.UpdateOrCreateRecords()

	if len(results) != 1 || results[0].Status != UpdatedSuccess {
		t.Fatalf("期望 1 条成功结果, 实际: %+v", results)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("有请求体时应为 POST, 实际: %s", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("合法 JSON 应使用 application/json, 实际: %s", gotContentType)
	}
	want := `{"ip":"1.2.3.4","domain":"www.example.com","type":"A"}`
	if gotBody != want {
		t.Errorf("请求体变量替换不正确\n期望: %s\n实际: %s", want, gotBody)
	}
}

func TestCallbackFailsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "boom")
	}))
	defer srv.Close()

	group, caches := newCallbackGroup(srv.URL+"/hook", "")
	c := &Callback{}
	c.Init(group, caches)
	results := c.UpdateOrCreateRecords()

	if len(results) != 1 || results[0].Status != UpdatedFailed {
		t.Fatalf("期望 1 条失败结果, 实际: %+v", results)
	}
}

func TestCallbackIncompleteConfig(t *testing.T) {
	group, caches := newCallbackGroup("", "") // 缺少回调 URL
	c := &Callback{}
	c.Init(group, caches)
	results := c.UpdateOrCreateRecords()

	if len(results) != 1 || results[0].Status != InitFailed {
		t.Fatalf("期望 1 条初始化失败结果, 实际: %+v", results)
	}
}

func TestCallbackReplaceVars(t *testing.T) {
	c := &Callback{}
	c.Group = &config.DNSGroup{Domain: "sub.example.com", TTL: "300"}
	record := &config.DNSRecord{Type: RecordTypeAAAA}
	got := c.replaceVars("#{ip}|#{value}|#{domain}|#{recordType}|#{ttl}", record, "::1")
	want := "::1|::1|sub.example.com|AAAA|300"
	if got != want {
		t.Errorf("replaceVars = %q, 期望 %q", got, want)
	}
}
