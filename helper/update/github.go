package update

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cxbdasheng/dnet/helper"
)

// GitHubRelease GitHub Release 结构
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// GetLatestRelease 从 GitHub 获取最新版本信息
func GetLatestRelease() (version *Version, downloadURL string, err error) {
	const apiURL = "https://api.github.com/repos/cxbdasheng/dnet/releases/latest"
	release, err := getLatest(apiURL)
	if err != nil {
		return nil, "", err
	}
	asset, ver, found := findAsset(release)
	if !found {
		return nil, "", fmt.Errorf("未找到适用于当前系统的二进制文件")
	}
	return ver, asset.URL, nil
}

// getLatest 从 GitHub API 获取最新的 release 信息
func getLatest(apiURL string) (*GitHubRelease, error) {
	client := helper.CreateHTTPClient()
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	// 设置 User-Agent
	req.Header.Set("User-Agent", "dnet-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 返回错误状态码: %d", resp.StatusCode)
	}
	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return &release, nil
}
