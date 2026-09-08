package update

import (
	"fmt"
	"io"
)

type limitedReadCloser struct {
	reader io.Reader
	body   io.ReadCloser
}

func (r *limitedReadCloser) Read(p []byte) (int, error) {
	return r.reader.Read(p)
}

func (r *limitedReadCloser) Close() error {
	return r.body.Close()
}

// DownloadFile downloads a URL with the updater's strict TLS client.
//
// Deprecated: use Update or UpdateRelease so the downloaded archive is checked
// against the release's checksums.txt before it is used.
func DownloadFile(url string) (io.ReadCloser, error) {
	if _, err := parseHTTPSURL(url, "下载地址"); err != nil {
		return nil, err
	}
	resp, err := downloadResponse(newHTTPClient(), url, maxArchiveSize)
	if err != nil {
		return nil, fmt.Errorf("无法从 %s 下载文件: %w", url, err)
	}
	return &limitedReadCloser{
		reader: io.LimitReader(resp.Body, maxArchiveSize),
		body:   resp.Body,
	}, nil
}
