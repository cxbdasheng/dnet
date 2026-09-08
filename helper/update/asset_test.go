package update

import (
	"strings"
	"testing"
)

func releaseWithAssets(tag string, assets ...Asset) *GitHubRelease {
	release := &GitHubRelease{TagName: tag}
	for _, asset := range assets {
		release.Assets = append(release.Assets, struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{Name: asset.Name, BrowserDownloadURL: asset.URL})
	}
	return release
}

func TestSelectReleaseExactArchiveAndChecksums(t *testing.T) {
	archive := Asset{Name: "dnet_1.2.3_linux_x86_64.tar.gz", URL: "https://example.test/archive"}
	checksums := Asset{Name: "checksums.txt", URL: "https://example.test/checksums"}
	release := releaseWithAssets("v1.2.3",
		Asset{Name: "prefix_" + archive.Name, URL: "https://example.test/prefix"},
		Asset{Name: archive.Name + ".sig", URL: "https://example.test/sig"},
		archive,
		checksums,
	)

	got, err := selectRelease(release, BuildTarget{GOOS: "linux", GOARCH: "amd64"})
	if err != nil {
		t.Fatalf("selectRelease() error = %v", err)
	}
	if got.Version.String() != "1.2.3" || got.Archive != archive || got.Checksums != checksums {
		t.Fatalf("selectRelease() = %+v", got)
	}
}

func TestSelectReleaseRejectsMissingOrDuplicateAssets(t *testing.T) {
	archive := Asset{Name: "dnet_1.2.3_linux_x86_64.tar.gz", URL: "https://example.test/archive"}
	checksums := Asset{Name: "checksums.txt", URL: "https://example.test/checksums"}
	tests := []struct {
		name   string
		assets []Asset
		want   string
	}{
		{name: "missing archive", assets: []Asset{checksums}, want: "缺少资源"},
		{name: "duplicate archive", assets: []Asset{archive, archive, checksums}, want: "重复"},
		{name: "missing checksums", assets: []Asset{archive}, want: "缺少资源"},
		{name: "duplicate checksums", assets: []Asset{archive, checksums, checksums}, want: "重复"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := selectRelease(releaseWithAssets("v1.2.3", tt.assets...), BuildTarget{GOOS: "linux", GOARCH: "amd64"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("selectRelease() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestSelectReleaseRejectsInvalidVersionAndEmptyURL(t *testing.T) {
	target := BuildTarget{GOOS: "linux", GOARCH: "amd64"}
	if _, err := selectRelease(releaseWithAssets("not-semver"), target); err == nil {
		t.Fatal("selectRelease() accepted invalid version")
	}

	release := releaseWithAssets("v1.2.3",
		Asset{Name: "dnet_1.2.3_linux_x86_64.tar.gz"},
		Asset{Name: "checksums.txt", URL: "https://example.test/checksums"},
	)
	if _, err := selectRelease(release, target); err == nil {
		t.Fatal("selectRelease() accepted empty archive URL")
	}
}
