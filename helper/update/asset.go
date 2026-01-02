package update

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/cxbdasheng/dnet/helper"
)

// Asset 表示 GitHub Release 中的一个资源文件
type Asset struct {
	Name string
	URL  string
}

// findAsset 从 release 中查找适合当前系统架构的 asset
func findAsset(rel *GitHubRelease) (asset *Asset, version *Version, found bool) {
	if rel == nil {
		helper.Warn(helper.LogTypeSystem, "没有找到发布信息")
		return nil, nil, false
	}
	for _, arch := range generateAdditionalArch() {
		if a, v, ok := findAssetFromRelease(rel, buildSuffixes(arch)); ok {
			return a, v, true
		}
	}
	return nil, nil, false
}

// findAssetFromRelease 从 release 的 assets 中查找匹配指定后缀的文件
func findAssetFromRelease(rel *GitHubRelease, suffixes []string) (*Asset, *Version, bool) {
	if rel == nil {
		helper.Warn(helper.LogTypeSystem, "没有找到发布信息")
		return nil, nil, false
	}

	for _, asset := range rel.Assets {
		if matchesAssetSuffixes(asset.Name, suffixes) {
			ver, err := NewVersion(rel.TagName)
			if err != nil {
				helper.Warn(helper.LogTypeSystem, "无法解析语义化版本: %s", rel.TagName)
				return nil, nil, false
			}
			return &Asset{Name: asset.Name, URL: asset.BrowserDownloadURL}, ver, true
		}
	}

	helper.Warn(helper.LogTypeSystem, "在版本 %s 中未找到合适的文件", rel.TagName)
	return nil, nil, false
}

// assetMatchSuffixes 检查 asset 名称是否匹配任一后缀
func matchesAssetSuffixes(name string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// buildSuffixes 构建所有要与 asset 进行检查的候选后缀
// TODO: 由于缺失获取 MIPS 架构 float 的方法，所以目前无法正确获取 MIPS 架构的后缀。
func buildSuffixes(arch string) []string {
	suffixes := make([]string, 0, 2)
	for _, ext := range []string{".zip", ".tar.gz"} {
		suffix := fmt.Sprintf("%s_%s%s", runtime.GOOS, arch, ext)
		suffixes = append(suffixes, suffix)
	}
	return suffixes
}
