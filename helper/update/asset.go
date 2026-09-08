package update

import (
	"fmt"
	"strings"
)

// Asset represents a downloadable GitHub release asset.
type Asset struct {
	Name string
	URL  string
}

// Release is the complete, verified-input selection needed by the updater.
type Release struct {
	Version   *Version
	Archive   Asset
	Checksums Asset
}

func selectRelease(rel *GitHubRelease, target BuildTarget) (*Release, error) {
	if rel == nil {
		return nil, fmt.Errorf("没有找到发布信息")
	}

	version, err := NewVersion(rel.TagName)
	if err != nil {
		return nil, fmt.Errorf("无法解析语义化版本 %q: %w", rel.TagName, err)
	}
	archiveName, err := target.archiveName(strings.TrimPrefix(rel.TagName, "v"))
	if err != nil {
		return nil, err
	}

	archive, err := findUniqueAsset(rel, archiveName)
	if err != nil {
		return nil, fmt.Errorf("选择更新归档失败: %w", err)
	}
	checksums, err := findUniqueAsset(rel, "checksums.txt")
	if err != nil {
		return nil, fmt.Errorf("选择校验文件失败: %w", err)
	}

	return &Release{Version: version, Archive: archive, Checksums: checksums}, nil
}

func findUniqueAsset(rel *GitHubRelease, name string) (Asset, error) {
	var match Asset
	count := 0
	for _, candidate := range rel.Assets {
		if candidate.Name != name {
			continue
		}
		count++
		match = Asset{Name: candidate.Name, URL: candidate.BrowserDownloadURL}
	}
	switch count {
	case 0:
		return Asset{}, fmt.Errorf("缺少资源 %q", name)
	case 1:
		if match.URL == "" {
			return Asset{}, fmt.Errorf("资源 %q 缺少下载地址", name)
		}
		return match, nil
	default:
		return Asset{}, fmt.Errorf("资源 %q 重复出现 %d 次", name, count)
	}
}
