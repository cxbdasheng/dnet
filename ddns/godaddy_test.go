package ddns

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxbdasheng/dnet/config"
)

// newGoDaddyGroup 构造一个 GoDaddy 测试配置组（单条静态记录）
func newGoDaddyGroup(recordType, ipType, value string) (*config.DNSGroup, []*Cache) {
	group := &config.DNSGroup{
		Domain:       "www.example.com",
		AccessKey:    "test-key",
		AccessSecret: "test-secret",
		TTL:          "600",
		Records: []config.DNSRecord{
			{Type: recordType, IPType: ipType, Value: value},
		},
	}
	c := NewCache()
	return group, []*Cache{&c}
}

// withGoDaddyEndpoint 临时替换 GoDaddy API 地址，返回还原函数
func withGoDaddyEndpoint(t *testing.T, url string) {
	t.Helper()
	old := goDaddyAPIEndpoint
	goDaddyAPIEndpoint = url
	t.Cleanup(func() { goDaddyAPIEndpoint = old })
}

func TestGoDaddyCreatesNewRecord(t *testing.T) {
	var putBody string
	putCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/domains/example.com/records":
			io.WriteString(w, "[]")
		case r.Method == http.MethodPut && r.URL.Path == "/domains/example.com/records/A/www":
			putCalled = true
			body, _ := io.ReadAll(r.Body)
			putBody = string(body)
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("意外请求: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	withGoDaddyEndpoint(t, srv.URL)

	group, caches := newGoDaddyGroup(RecordTypeA, "static_ipv4", "1.2.3.4")
	g := &GoDaddy{}
	g.Init(group, caches)
	results := g.UpdateOrCreateRecords()

	if len(results) != 1 || results[0].Status != UpdatedSuccess {
		t.Fatalf("期望 1 条成功结果, 实际: %+v", results)
	}
	if !putCalled {
		t.Fatal("期望调用 PUT 创建记录，但未调用")
	}
	if !strings.Contains(putBody, `"data":"1.2.3.4"`) || !strings.Contains(putBody, `"ttl":600`) {
		t.Errorf("PUT 请求体不正确: %s", putBody)
	}
}

func TestGoDaddySkipsUnchangedRecord(t *testing.T) {
	putCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/domains/example.com/records":
			io.WriteString(w, `[{"data":"1.2.3.4","name":"www","type":"A","ttl":600}]`)
		case r.Method == http.MethodPut:
			putCalled = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("意外请求: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	withGoDaddyEndpoint(t, srv.URL)

	group, caches := newGoDaddyGroup(RecordTypeA, "static_ipv4", "1.2.3.4")
	g := &GoDaddy{}
	g.Init(group, caches)
	results := g.UpdateOrCreateRecords()

	if len(results) != 1 || results[0].Status != UpdatedSuccess {
		t.Fatalf("期望 1 条成功结果, 实际: %+v", results)
	}
	if putCalled {
		t.Error("记录值未变化时不应调用 PUT")
	}
}

func TestGoDaddyDeletesConflictingCNAMEBeforeA(t *testing.T) {
	deletedType := ""
	putType := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/domains/example.com/records":
			io.WriteString(w, `[{"data":"old.example.net","name":"www","type":"CNAME","ttl":600}]`)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/domains/example.com/records/"):
			deletedType = strings.Split(r.URL.Path, "/")[4]
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/domains/example.com/records/"):
			putType = strings.Split(r.URL.Path, "/")[4]
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("意外请求: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	withGoDaddyEndpoint(t, srv.URL)

	group, caches := newGoDaddyGroup(RecordTypeA, "static_ipv4", "1.2.3.4")
	g := &GoDaddy{}
	g.Init(group, caches)
	results := g.UpdateOrCreateRecords()

	if len(results) != 1 || results[0].Status != UpdatedSuccess {
		t.Fatalf("期望 1 条成功结果, 实际: %+v", results)
	}
	if deletedType != RecordTypeCNAME {
		t.Errorf("期望删除冲突的 CNAME 记录, 实际删除类型: %q", deletedType)
	}
	if putType != RecordTypeA {
		t.Errorf("期望创建 A 记录, 实际 PUT 类型: %q", putType)
	}
}

func TestGoDaddyDeletesConflictingRecordsBeforeCNAME(t *testing.T) {
	deletedTypes := make(map[string]bool)
	putType := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/domains/example.com/records":
			io.WriteString(w, `[{"data":"1.2.3.4","name":"www","type":"A","ttl":600},{"data":"txt","name":"www","type":"TXT","ttl":600}]`)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/domains/example.com/records/"):
			deletedTypes[strings.Split(r.URL.Path, "/")[4]] = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/domains/example.com/records/"):
			putType = strings.Split(r.URL.Path, "/")[4]
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("意外请求: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	withGoDaddyEndpoint(t, srv.URL)

	group, caches := newGoDaddyGroup(RecordTypeCNAME, "", "target.example.net")
	g := &GoDaddy{}
	g.Init(group, caches)
	results := g.UpdateOrCreateRecords()

	if len(results) != 1 || results[0].Status != UpdatedSuccess {
		t.Fatalf("期望 1 条成功结果, 实际: %+v", results)
	}
	if !deletedTypes[RecordTypeA] || !deletedTypes[RecordTypeTXT] {
		t.Errorf("期望删除冲突的 A/TXT 记录, 实际: %v", deletedTypes)
	}
	if putType != RecordTypeCNAME {
		t.Errorf("期望创建 CNAME 记录, 实际 PUT 类型: %q", putType)
	}
}

func TestGoDaddyReturnsFailedOnAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"code":"ACCESS_DENIED","message":"Authenticated user is not allowed access"}`)
	}))
	defer srv.Close()
	withGoDaddyEndpoint(t, srv.URL)

	group, caches := newGoDaddyGroup(RecordTypeA, "static_ipv4", "1.2.3.4")
	g := &GoDaddy{}
	g.Init(group, caches)
	results := g.UpdateOrCreateRecords()

	if len(results) != 1 || results[0].Status != UpdatedFailed {
		t.Fatalf("期望 1 条失败结果, 实际: %+v", results)
	}
	if !strings.Contains(results[0].ErrorMessage, "ACCESS_DENIED") {
		t.Errorf("错误信息应包含 API 返回的 code, 实际: %s", results[0].ErrorMessage)
	}
}

func TestGoDaddyIncompleteConfig(t *testing.T) {
	group, caches := newGoDaddyGroup(RecordTypeA, "static_ipv4", "1.2.3.4")
	group.AccessSecret = "" // 缺少 API Secret
	g := &GoDaddy{}
	g.Init(group, caches)
	results := g.UpdateOrCreateRecords()

	if len(results) != 1 || results[0].Status != InitFailed {
		t.Fatalf("期望 1 条初始化失败结果, 实际: %+v", results)
	}
}

func TestGoDaddyParseTTL(t *testing.T) {
	tests := []struct {
		ttl  string
		want int
	}{
		{"", goDaddyMinTTL},
		{"AUTO", goDaddyMinTTL},
		{"300", goDaddyMinTTL}, // 低于最小值向上取
		{"600", 600},
		{"3600", 3600},
		{"10m", 600},
		{"1h", 3600},
		{"30s", goDaddyMinTTL},
	}
	for _, tt := range tests {
		g := &GoDaddy{}
		g.Group = &config.DNSGroup{TTL: tt.ttl}
		if got := g.parseTTL(); got != tt.want {
			t.Errorf("parseTTL(%q) = %d, 期望 %d", tt.ttl, got, tt.want)
		}
	}
}

func TestGoDaddyEqualCNAME(t *testing.T) {
	g := &GoDaddy{}
	if !g.equalCNAME("a.example.com.", "a.example.com") {
		t.Error("应忽略末尾点视为相等")
	}
	if g.equalCNAME("a.example.com", "b.example.com") {
		t.Error("不同值不应相等")
	}
}

// 保证测试中构造的 JSON 与 GoDaddy 记录结构可互相解析
func TestGoDaddyRecordUnmarshal(t *testing.T) {
	var recs []goDaddyRecord
	if err := json.Unmarshal([]byte(`[{"data":"1.1.1.1","name":"@","type":"A","ttl":600}]`), &recs); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(recs) != 1 || recs[0].Name != "@" || recs[0].Data != "1.1.1.1" {
		t.Errorf("解析结果不正确: %+v", recs)
	}
}
