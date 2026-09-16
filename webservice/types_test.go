package webservice

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceTypes(t *testing.T) {
	for _, kind := range []string{"redirect", "jump", "speedtest"} {
		t.Run(kind, func(t *testing.T) {
			r := testRule(t, "https://example.com/dest?q=1#part")
			r.Type = kind
			r.TimeoutSec = 0
			if kind == "speedtest" {
				r.Target = ""
			}
			if err := Validate([]Rule{r}); err != nil {
				t.Fatal(err)
			}
			s := newSite(r, nil)
			defer s.transport.CloseIdleConnections()
			w := httptest.NewRecorder()
			s.handler.ServeHTTP(w, httptest.NewRequest("GET", "http://one.example/original", nil))
			switch kind {
			case "redirect":
				if w.Code != 302 || w.Header().Get("Location") != r.Target {
					t.Fatal(w)
				}
			case "jump":
				if w.Code != 200 || !strings.Contains(w.Body.String(), "window.location.replace") || w.Header().Get("Location") != "" {
					t.Fatal(w)
				}
			case "speedtest":
				if w.Code != 200 || !strings.Contains(w.Body.String(), "在线测速") {
					t.Fatal(w)
				}
			}
			r.AuthEnabled = true
			r.Username = "reader"
			r.Password = "secret"
			rules, err := Prepare([]Rule{r}, nil)
			if err != nil {
				t.Fatal(err)
			}
			secured := newSite(rules[0], nil)
			defer secured.transport.CloseIdleConnections()
			w = httptest.NewRecorder()
			secured.handler.ServeHTTP(w, httptest.NewRequest("GET", "http://one.example/?dws_test=download&bytes=8", nil))
			if w.Code != 401 {
				t.Fatal("authentication bypass", w.Code)
			}
		})
	}
}
func TestRedirectCodesAndValidation(t *testing.T) {
	r := testRule(t, "https://example.com/dest")
	r.Type = "redirect"
	for _, code := range []int{301, 302, 307, 308} {
		r.RedirectCode = code
		if err := Validate([]Rule{r}); err != nil {
			t.Fatal(err)
		}
		s := newSite(r, nil)
		w := httptest.NewRecorder()
		s.handler.ServeHTTP(w, httptest.NewRequest("POST", "http://one.example/", strings.NewReader("body")))
		s.transport.CloseIdleConnections()
		if w.Code != code {
			t.Fatal(w.Code)
		}
	}
	r.RedirectCode = 200
	if Validate([]Rule{r}) == nil {
		t.Fatal("accepted invalid redirect")
	}
	r.RedirectCode = 302
	r.Target = "javascript:alert(1)"
	if Validate([]Rule{r}) == nil {
		t.Fatal("accepted unsafe target")
	}
	r.Type = "unknown"
	if Validate([]Rule{r}) == nil {
		t.Fatal("accepted unknown type")
	}
}
func TestSpeedTransfer(t *testing.T) {
	for _, tc := range []struct {
		method, query, body string
		code                int
	}{
		{"GET", "ping", "", 204}, {"GET", "download&bytes=1024", "", 200}, {"GET", "download&bytes=16777217", "", 400},
		{"POST", "upload", "test-data", 200}, {"GET", "upload", "", 405}, {"GET", "missing", "", 404},
	} {
		w := httptest.NewRecorder()
		speedTest(w, httptest.NewRequest(tc.method, "http://one.example/?dws_test="+tc.query, strings.NewReader(tc.body)))
		if w.Code != tc.code {
			t.Fatalf("%s: %d", tc.query, w.Code)
		}
		if strings.HasPrefix(tc.query, "download") && tc.code == 200 && w.Body.Len() != 1024 {
			t.Fatal("wrong download size")
		}
		if tc.query == "upload" && tc.code == 200 && !strings.Contains(w.Body.String(), `"bytes":9`) {
			t.Fatal(w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://one.example/?dws_test=upload", io.LimitReader(infiniteReader{}, (16<<20)+1))
	speedTest(w, req)
	if w.Code != 413 {
		t.Fatal("upload limit ignored", w.Code)
	}
}

type infiniteReader struct{}

func (infiniteReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestLibreSpeedEndpoints(t *testing.T) {
	for _, resource := range []string{"speedtest.js", "speedtest_worker.js", "nested/speedtest_worker.js"} {
		w := httptest.NewRecorder()
		speedTest(w, httptest.NewRequest("GET", "http://one.example/"+resource+"?r=1", nil))
		if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "javascript") || !strings.Contains(w.Body.String(), "LibreSpeed") {
			t.Fatalf("asset %s unavailable", resource)
		}
	}
	for _, method := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		speedTest(w, httptest.NewRequest(method, "http://one.example/?dws_test=empty", strings.NewReader("upload")))
		if w.Code != 200 || w.Body.Len() != 0 {
			t.Fatal("empty endpoint incompatible", w.Code)
		}
	}
	w := httptest.NewRecorder()
	speedTest(w, httptest.NewRequest("GET", "http://one.example/?dws_test=garbage&ckSize=2", nil))
	if w.Code != 200 || w.Body.Len() != 2<<20 || w.Header().Get("Content-Length") != "2097152" {
		t.Fatal("chunk size mismatch")
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("cache enabled")
	}
	for _, n := range []string{"0", "-1", "65", "bad"} {
		w := httptest.NewRecorder()
		speedTest(w, httptest.NewRequest("GET", "http://one.example/?dws_test=garbage&ckSize="+n, nil))
		if w.Code != 400 {
			t.Fatal("invalid chunks accepted", n)
		}
	}
	w = httptest.NewRecorder()
	speedTest(w, httptest.NewRequest("POST", "http://one.example/?dws_test=empty", io.LimitReader(infiniteReader{}, (32<<20)+1)))
	if w.Code != 413 {
		t.Fatal("upload bound ignored")
	}
}
