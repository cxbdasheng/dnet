package update

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const latestReleaseAPIURL = "https://api.github.com/repos/cxbdasheng/dnet/releases/latest"

// GitHubRelease is the subset of a GitHub release used by the updater.
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// GetLatestRelease returns the latest version and archive URL.
//
// Deprecated: use GetLatestReleaseSelection to also obtain the checksums asset
// required by the verified update flow.
func GetLatestRelease() (version *Version, downloadURL string, err error) {
	release, err := GetLatestReleaseSelection()
	if err != nil {
		return nil, "", err
	}
	return release.Version, release.Archive.URL, nil
}

// GetLatestReleaseSelection returns the archive and checksum assets selected
// exactly for the build that is currently running.
func GetLatestReleaseSelection() (*Release, error) {
	return getLatestRelease(newHTTPClient(), latestReleaseAPIURL, currentBuildTarget())
}

func getLatestRelease(client *http.Client, apiURL string, target BuildTarget) (*Release, error) {
	release, err := getLatest(client, apiURL)
	if err != nil {
		return nil, err
	}
	return selectRelease(release, target)
}

func getLatest(client *http.Client, apiURL string) (*GitHubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dnet-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API 返回错误状态码: %d", resp.StatusCode)
	}
	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	return &release, nil
}
