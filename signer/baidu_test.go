package signer

import (
	"net/http"
	"testing"
)

func TestBaiduCanonicalURI(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "/v2/domain/drcdn.it927.com",
			expected: "/v2/domain/drcdn.it927.com",
		},
		{
			name:     "path with dots",
			path:     "/v2/domain/test.example.com",
			expected: "/v2/domain/test.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "https://cdn.baidubce.com"+tt.path, nil)
			result := BaiduCanonicalURI(req)
			if result != tt.expected {
				t.Errorf("BaiduCanonicalURI() = %v, want %v", result, tt.expected)
			}
		})
	}
}
