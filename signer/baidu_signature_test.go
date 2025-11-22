package signer

import (
	"bytes"
	"net/http"
	"testing"
)

// TestBaiduSignerStructure 测试签名结构是否正确
func TestBaiduSignerStructure(t *testing.T) {
	// 模拟一个 PUT 请求
	body := []byte(`{"origin":[{"peer":"1.2.3.4","backup":false}],"from":"dynamic"}`)
	req, err := http.NewRequest("PUT", "https://cdn.baidubce.com/v2/domain/drcdn.it927.com", bytes.NewBuffer(body))
	if err != nil {
		t.Fatal(err)
	}

	// 设置必要的 headers（在签名之前）
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Host", "cdn.baidubce.com")

	// 使用测试密钥
	accessKeyID := "test-access-key"
	accessSecret := "test-secret-key"

	// 调用签名函数
	BaiduSigner(accessKeyID, accessSecret, req)

	// 验证必要的 headers 被设置
	if req.Header.Get("Authorization") == "" {
		t.Error("Authorization header not set")
	}
	if req.Header.Get("x-bce-date") == "" {
		t.Error("x-bce-date header not set")
	}

	// 验证 Authorization 格式
	auth := req.Header.Get("Authorization")
	if len(auth) < 10 {
		t.Errorf("Authorization header seems invalid: %s", auth)
	}

	t.Logf("Authorization: %s", auth)
	t.Logf("x-bce-date: %s", req.Header.Get("x-bce-date"))
}
