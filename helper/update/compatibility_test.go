package update

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

var (
	_ func(string, string) error          = Update
	_ func() (*Version, string, error)    = GetLatestRelease
	_ func(string) (io.ReadCloser, error) = DownloadFile
)

func TestReleaseFromArchiveURLDerivesChecksumsSibling(t *testing.T) {
	release, err := releaseFromArchiveURL("https://example.com/releases/v1/dnet.tar.gz?download=1&token=secret#fragment")
	if err != nil {
		t.Fatalf("releaseFromArchiveURL() error = %v", err)
	}
	if release.Archive.Name != "dnet.tar.gz" {
		t.Fatalf("archive name = %q, want %q", release.Archive.Name, "dnet.tar.gz")
	}
	if release.Checksums.URL != "https://example.com/releases/v1/checksums.txt?download=1&token=secret" {
		t.Fatalf("checksums URL = %q", release.Checksums.URL)
	}
	archiveURL, err := url.Parse(release.Archive.URL)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if archiveURL.RawQuery != "download=1&token=secret" || archiveURL.Fragment != "fragment" {
		t.Fatalf("archive URL lost query or fragment: %q", release.Archive.URL)
	}
}

func TestReleaseFromArchiveURLRejectsInsecureURL(t *testing.T) {
	_, err := releaseFromArchiveURL("http://example.com/dnet.tar.gz")
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("releaseFromArchiveURL() error = %v, want HTTPS validation error", err)
	}
}

func TestPublicUpdateAPIsRejectInsecureURLs(t *testing.T) {
	if err := Update("http://example.com/dnet.tar.gz", t.TempDir()+"/dnet"); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("Update() error = %v, want HTTPS validation error", err)
	}

	release := &Release{
		Archive:   Asset{Name: "dnet.tar.gz", URL: "http://example.com/dnet.tar.gz"},
		Checksums: Asset{Name: "checksums.txt", URL: "https://example.com/checksums.txt"},
	}
	if err := UpdateRelease(release, t.TempDir()+"/dnet"); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("UpdateRelease() error = %v, want HTTPS validation error", err)
	}
}

func TestValidateRedirectRejectsInsecureOrCredentialedURLs(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "HTTP downgrade", url: "http://example.com/archive.tar.gz"},
		{name: "embedded credentials", url: "https://user:secret@example.com/archive.tar.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tt.url, nil)
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			if err := validateRedirect(req, nil); err == nil {
				t.Fatalf("validateRedirect() accepted %q", tt.url)
			}
		})
	}
}

func TestValidateRedirectAcceptsHTTPSAndLimitsRedirects(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://objects.example.com/archive.tar.gz", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if err := validateRedirect(req, nil); err != nil {
		t.Fatalf("validateRedirect() error = %v", err)
	}

	via := make([]*http.Request, 10)
	if err := validateRedirect(req, via); err == nil || !strings.Contains(err.Error(), "重定向次数") {
		t.Fatalf("validateRedirect() error = %v, want redirect limit error", err)
	}
}
