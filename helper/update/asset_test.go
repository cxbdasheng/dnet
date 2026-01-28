package update

import (
	"runtime"
	"testing"
)

func TestMatchesAssetSuffixes(t *testing.T) {
	tests := []struct {
		name      string
		assetName string
		suffixes  []string
		want      bool
	}{
		{
			name:      "匹配 .zip 后缀",
			assetName: "dnet_linux_amd64.zip",
			suffixes:  []string{".zip", ".tar.gz"},
			want:      true,
		},
		{
			name:      "匹配 .tar.gz 后缀",
			assetName: "dnet_linux_arm64.tar.gz",
			suffixes:  []string{".zip", ".tar.gz"},
			want:      true,
		},
		{
			name:      "匹配完整后缀",
			assetName: "dnet_darwin_amd64.zip",
			suffixes:  []string{"darwin_amd64.zip"},
			want:      true,
		},
		{
			name:      "不匹配任何后缀",
			assetName: "dnet_windows_386.exe",
			suffixes:  []string{".zip", ".tar.gz"},
			want:      false,
		},
		{
			name:      "空后缀列表",
			assetName: "dnet_linux_amd64.zip",
			suffixes:  []string{},
			want:      false,
		},
		{
			name:      "部分匹配不算匹配",
			assetName: "dnet_linux_amd64.zip.md5",
			suffixes:  []string{".zip"},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesAssetSuffixes(tt.assetName, tt.suffixes); got != tt.want {
				t.Errorf("matchesAssetSuffixes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildSuffixes(t *testing.T) {
	tests := []struct {
		name string
		arch string
		want []string
	}{
		{
			name: "amd64 架构",
			arch: "amd64",
			want: []string{
				runtime.GOOS + "_amd64.zip",
				runtime.GOOS + "_amd64.tar.gz",
			},
		},
		{
			name: "arm64 架构",
			arch: "arm64",
			want: []string{
				runtime.GOOS + "_arm64.zip",
				runtime.GOOS + "_arm64.tar.gz",
			},
		},
		{
			name: "armv7 架构",
			arch: "armv7",
			want: []string{
				runtime.GOOS + "_armv7.zip",
				runtime.GOOS + "_armv7.tar.gz",
			},
		},
		{
			name: "386 架构",
			arch: "386",
			want: []string{
				runtime.GOOS + "_386.zip",
				runtime.GOOS + "_386.tar.gz",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSuffixes(tt.arch)
			if len(got) != len(tt.want) {
				t.Errorf("buildSuffixes() 返回 %d 个后缀，期望 %d 个", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("buildSuffixes()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestFindAssetFromRelease(t *testing.T) {
	tests := []struct {
		name      string
		release   *GitHubRelease
		suffixes  []string
		wantAsset bool
		wantName  string
		wantVer   string
	}{
		{
			name: "找到匹配的 asset",
			release: &GitHubRelease{
				TagName: "v1.2.3",
				Assets: []struct {
					Name               string `json:"name"`
					BrowserDownloadURL string `json:"browser_download_url"`
				}{
					{
						Name:               "dnet_linux_amd64.zip",
						BrowserDownloadURL: "https://example.com/dnet_linux_amd64.zip",
					},
					{
						Name:               "dnet_darwin_amd64.zip",
						BrowserDownloadURL: "https://example.com/dnet_darwin_amd64.zip",
					},
				},
			},
			suffixes:  []string{"linux_amd64.zip"},
			wantAsset: true,
			wantName:  "dnet_linux_amd64.zip",
			wantVer:   "1.2.3",
		},
		{
			name: "未找到匹配的 asset",
			release: &GitHubRelease{
				TagName: "v1.0.0",
				Assets: []struct {
					Name               string `json:"name"`
					BrowserDownloadURL string `json:"browser_download_url"`
				}{
					{
						Name:               "dnet_windows_amd64.zip",
						BrowserDownloadURL: "https://example.com/dnet_windows_amd64.zip",
					},
				},
			},
			suffixes:  []string{"linux_amd64.zip"},
			wantAsset: false,
		},
		{
			name:      "release 为 nil",
			release:   nil,
			suffixes:  []string{"linux_amd64.zip"},
			wantAsset: false,
		},
		{
			name: "无效的版本号",
			release: &GitHubRelease{
				TagName: "invalid-version",
				Assets: []struct {
					Name               string `json:"name"`
					BrowserDownloadURL string `json:"browser_download_url"`
				}{
					{
						Name:               "dnet_linux_amd64.zip",
						BrowserDownloadURL: "https://example.com/dnet_linux_amd64.zip",
					},
				},
			},
			suffixes:  []string{"linux_amd64.zip"},
			wantAsset: false,
		},
		{
			name: "多个匹配时返回第一个",
			release: &GitHubRelease{
				TagName: "v2.0.0",
				Assets: []struct {
					Name               string `json:"name"`
					BrowserDownloadURL string `json:"browser_download_url"`
				}{
					{
						Name:               "dnet_linux_amd64.zip",
						BrowserDownloadURL: "https://example.com/first.zip",
					},
					{
						Name:               "dnet_linux_amd64.tar.gz",
						BrowserDownloadURL: "https://example.com/second.tar.gz",
					},
				},
			},
			suffixes:  []string{"linux_amd64.zip", "linux_amd64.tar.gz"},
			wantAsset: true,
			wantName:  "dnet_linux_amd64.zip",
			wantVer:   "2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset, ver, found := findAssetFromRelease(tt.release, tt.suffixes)

			if found != tt.wantAsset {
				t.Errorf("findAssetFromRelease() found = %v, want %v", found, tt.wantAsset)
				return
			}

			if !found {
				return
			}

			if asset.Name != tt.wantName {
				t.Errorf("findAssetFromRelease() asset.Name = %v, want %v", asset.Name, tt.wantName)
			}

			if ver.String() != tt.wantVer {
				t.Errorf("findAssetFromRelease() version = %v, want %v", ver.String(), tt.wantVer)
			}
		})
	}
}

func TestFindAsset(t *testing.T) {
	// 创建包含当前系统架构的 asset 的 release
	currentArch := runtime.GOARCH
	currentOS := runtime.GOOS
	assetName := "dnet_" + currentOS + "_" + currentArch + ".zip"

	tests := []struct {
		name      string
		release   *GitHubRelease
		wantFound bool
	}{
		{
			name: "找到当前系统的 asset",
			release: &GitHubRelease{
				TagName: "v1.5.0",
				Assets: []struct {
					Name               string `json:"name"`
					BrowserDownloadURL string `json:"browser_download_url"`
				}{
					{
						Name:               assetName,
						BrowserDownloadURL: "https://example.com/" + assetName,
					},
				},
			},
			wantFound: true,
		},
		{
			name: "未找到当前系统的 asset",
			release: &GitHubRelease{
				TagName: "v1.0.0",
				Assets: []struct {
					Name               string `json:"name"`
					BrowserDownloadURL string `json:"browser_download_url"`
				}{
					{
						Name:               "dnet_unknownos_unknownarch.zip",
						BrowserDownloadURL: "https://example.com/unknown.zip",
					},
				},
			},
			wantFound: false,
		},
		{
			name:      "release 为 nil",
			release:   nil,
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset, ver, found := findAsset(tt.release)

			if found != tt.wantFound {
				t.Errorf("findAsset() found = %v, want %v", found, tt.wantFound)
				return
			}

			if !found {
				return
			}

			if asset == nil {
				t.Error("findAsset() asset 不应该为 nil")
			}

			if ver == nil {
				t.Error("findAsset() version 不应该为 nil")
			}
		})
	}
}
