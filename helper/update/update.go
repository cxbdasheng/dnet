package update

import (
	"io"
	"path/filepath"
)

// Update 执行自动更新流程：下载、解压并替换可执行文件
func Update(assetURL, cmdPath string) error {
	src, err := DownloadFile(assetURL)
	if err != nil {
		return err
	}
	defer src.Close()

	// 直接传递 URL，extractExecutable 会从中提取文件扩展名
	return decompressAndUpdate(src, assetURL, cmdPath)
}

// decompressAndUpdate 解压并更新可执行文件
func decompressAndUpdate(src io.Reader, assetName, cmdPath string) error {
	_, execName := filepath.Split(cmdPath)
	asset, err := extractExecutable(src, assetName, execName)
	if err != nil {
		return err
	}
	return apply(asset, cmdPath)
}
