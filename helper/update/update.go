package update

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/cxbdasheng/dnet/helper"
)

const (
	maxChecksumSize   = 1 << 20
	maxArchiveSize    = 256 << 20
	maxExecutableSize = 256 << 20
)

// Update downloads and verifies an archive URL before replacing cmdPath.
//
// Deprecated: use UpdateRelease with a release selected by
// GetLatestReleaseSelection. Update remains available for API compatibility and
// obtains checksums.txt from the archive's release directory.
func Update(assetURL, cmdPath string) error {
	release, err := releaseFromArchiveURL(assetURL)
	if err != nil {
		return err
	}
	return UpdateRelease(release, cmdPath)
}

func releaseFromArchiveURL(assetURL string) (*Release, error) {
	archiveURL, err := parseHTTPSURL(assetURL, "归档下载地址")
	if err != nil {
		return nil, err
	}

	archiveName := path.Base(archiveURL.Path)
	if archiveName == "." || archiveName == "/" || archiveName == "" {
		return nil, fmt.Errorf("归档下载地址缺少文件名")
	}

	checksumsURL := *archiveURL
	checksumsURL.Path = path.Join(path.Dir(archiveURL.Path), "checksums.txt")
	checksumsURL.RawPath = ""
	checksumsURL.Fragment = ""

	return &Release{
		Archive:   Asset{Name: archiveName, URL: archiveURL.String()},
		Checksums: Asset{Name: "checksums.txt", URL: checksumsURL.String()},
	}, nil
}

// UpdateRelease downloads and verifies the selected release before replacing
// cmdPath.
func UpdateRelease(release *Release, cmdPath string) error {
	if release == nil {
		return fmt.Errorf("发布选择不能为空")
	}
	if _, err := parseHTTPSURL(release.Archive.URL, "归档下载地址"); err != nil {
		return err
	}
	if _, err := parseHTTPSURL(release.Checksums.URL, "校验文件下载地址"); err != nil {
		return err
	}
	return updateWithClient(newHTTPClient(), release, cmdPath)
}

func newHTTPClient() *http.Client {
	client := helper.CreateStrictHTTPClient()
	client.CheckRedirect = validateRedirect
	return client
}

func validateRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("重定向次数超过限制")
	}
	if _, err := parseHTTPSURL(req.URL.String(), "重定向地址"); err != nil {
		return err
	}
	return nil
}

func parseHTTPSURL(rawURL, label string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("解析%s失败: %w", label, err)
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, fmt.Errorf("%s必须是有效的 HTTPS URL", label)
	}
	return parsed, nil
}

func updateWithClient(client *http.Client, release *Release, cmdPath string) error {
	if client == nil {
		return fmt.Errorf("HTTP 客户端不能为空")
	}
	if release == nil {
		return fmt.Errorf("发布选择不能为空")
	}
	if release.Archive.Name == "" || release.Archive.URL == "" {
		return fmt.Errorf("发布选择缺少归档名称或下载地址")
	}
	if release.Checksums.Name != "checksums.txt" || release.Checksums.URL == "" {
		return fmt.Errorf("发布选择缺少有效的 checksums.txt")
	}

	checksumData, err := downloadBytes(client, release.Checksums.URL, maxChecksumSize)
	if err != nil {
		return fmt.Errorf("下载 %s 失败: %w", release.Checksums.Name, err)
	}
	expected, err := archiveChecksum(checksumData, release.Archive.Name)
	if err != nil {
		return fmt.Errorf("解析 %s 失败: %w", release.Checksums.Name, err)
	}

	archive, actual, err := downloadArchive(client, release.Archive.URL, maxArchiveSize)
	if err != nil {
		return fmt.Errorf("下载 %s 失败: %w", release.Archive.Name, err)
	}
	defer func() {
		name := archive.Name()
		_ = archive.Close()
		_ = os.Remove(name)
	}()
	if actual != expected {
		return fmt.Errorf("验证更新归档失败: %q 的 SHA-256 校验失败", release.Archive.Name)
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("重置更新归档失败: %w", err)
	}

	return decompressAndUpdate(archive, release.Archive.Name, cmdPath)
}

func downloadBytes(client *http.Client, url string, limit int64) ([]byte, error) {
	resp, err := downloadResponse(client, url, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("响应超过 %d 字节限制", limit)
	}
	return data, nil
}

func downloadArchive(client *http.Client, url string, limit int64) (*os.File, [sha256.Size]byte, error) {
	resp, err := downloadResponse(client, url, limit)
	if err != nil {
		return nil, [sha256.Size]byte{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	archive, err := os.CreateTemp("", "dnet-update-*")
	if err != nil {
		return nil, [sha256.Size]byte{}, fmt.Errorf("创建临时归档失败: %w", err)
	}
	cleanup := func() {
		name := archive.Name()
		_ = archive.Close()
		_ = os.Remove(name)
	}

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(resp.Body, limit+1))
	if err != nil {
		cleanup()
		return nil, [sha256.Size]byte{}, fmt.Errorf("读取响应失败: %w", err)
	}
	if written > limit {
		cleanup()
		return nil, [sha256.Size]byte{}, fmt.Errorf("响应超过 %d 字节限制", limit)
	}

	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return archive, digest, nil
}

func downloadResponse(client *http.Client, url string, limit int64) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "dnet-updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("响应状态码: %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("响应 Content-Length %d 超过 %d 字节限制", resp.ContentLength, limit)
	}
	return resp, nil
}

func decompressAndUpdate(src io.Reader, assetName, cmdPath string) error {
	_, execName := filepath.Split(cmdPath)
	asset, err := extractExecutable(src, assetName, execName)
	if err != nil {
		return err
	}
	if closer, ok := asset.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}
	return apply(asset, cmdPath)
}
