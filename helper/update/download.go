package update

import (
	"fmt"
	"io"

	"github.com/cxbdasheng/dnet/helper"
)

// DownloadFile 从指定 URL 下载文件并返回 ReadCloser
func DownloadFile(url string) (rc io.ReadCloser, err error) {
	client := helper.CreateHTTPClient()
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("无法从 %s 下载文件: %v", url, err)
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("无法从 %s 下载文件，响应状态码: %d", url, resp.StatusCode)
	}

	return resp.Body, nil
}
