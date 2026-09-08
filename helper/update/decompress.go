// Based on https://github.com/creativeprojects/go-selfupdate/blob/v1.1.1/decompress.go

package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/cxbdasheng/dnet/helper"
)

var (
	errCannotDecompressFile        = errors.New("无法解压文件")
	errExecutableNotFoundInArchive = errors.New("未找到可执行文件")
)

// gzipTarReader 包装 tar.Reader 和 gzip.Reader，确保资源被正确释放
type gzipTarReader struct {
	*tar.Reader
	gzipReader io.ReadCloser
}

func (g *gzipTarReader) Close() error {
	return g.gzipReader.Close()
}

var fileTypes = map[string]func(src io.Reader, cmd string) (io.Reader, error){
	".zip":    unzip,
	".tar.gz": untar,
}

// extractExecutable 从压缩包中提取可执行文件。从 'url' 参数中自动检测存档和压缩格式，
// 'url' 参数表示 asset 的 URL 或简单的文件名（带扩展名）。
// 返回 reader，用于读取解压缩后与 'execName' 相应的可执行文件。支持 '.zip' 和 '.tar.gz'
//
// 可能返回以下封装过的错误：
//   - errCannotDecompressFile
//   - errExecutableNotFoundInArchive
func extractExecutable(src io.Reader, url, execName string) (io.Reader, error) {
	for ext, decompress := range fileTypes {
		if strings.HasSuffix(url, ext) {
			return decompress(src, execName)
		}
	}
	helper.Info(helper.LogTypeSystem, "不是压缩文件，跳过解压")
	return src, nil
}

func unzip(src io.Reader, cmd string) (io.Reader, error) {
	return unzipWithLimit(src, cmd, maxExecutableSize)
}

func unzipWithLimit(src io.Reader, cmd string, limit int64) (io.Reader, error) {
	readerAt, size, err := zipReaderAt(src)
	if err != nil {
		return nil, fmt.Errorf("%w zip 文件: %v", errCannotDecompressFile, err)
	}

	z, err := zip.NewReader(readerAt, size)
	if err != nil {
		return nil, fmt.Errorf("%w zip 文件: %s", errCannotDecompressFile, err)
	}

	for _, file := range z.File {
		_, name := filepath.Split(file.Name)
		if !file.FileInfo().IsDir() && isExecutableMatch(cmd, name) {
			if limit <= 0 || file.UncompressedSize64 > uint64(limit) {
				return nil, fmt.Errorf("解压后的可执行文件超过 %d 字节限制", limit)
			}
			return file.Open()
		}
	}

	return nil, fmt.Errorf("在 zip 文件中%w：%q", errExecutableNotFoundInArchive, cmd)
}

func zipReaderAt(src io.Reader) (io.ReaderAt, int64, error) {
	if readerAt, ok := src.(io.ReaderAt); ok {
		if seeker, ok := src.(io.Seeker); ok {
			current, err := seeker.Seek(0, io.SeekCurrent)
			if err != nil {
				return nil, 0, err
			}
			size, err := seeker.Seek(0, io.SeekEnd)
			if err != nil {
				return nil, 0, err
			}
			if _, err := seeker.Seek(current, io.SeekStart); err != nil {
				return nil, 0, err
			}
			return readerAt, size, nil
		}
	}

	buf, err := io.ReadAll(src)
	if err != nil {
		return nil, 0, err
	}
	reader := bytes.NewReader(buf)
	return reader, reader.Size(), nil
}

func untar(src io.Reader, cmd string) (io.Reader, error) {
	return untarWithLimit(src, cmd, maxExecutableSize)
}

func untarWithLimit(src io.Reader, cmd string, limit int64) (io.Reader, error) {
	gz, err := gzip.NewReader(src)
	if err != nil {
		return nil, fmt.Errorf("%w tar.gz 文件: %s", errCannotDecompressFile, err)
	}

	t := tar.NewReader(gz)
	for {
		h, err := t.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			gz.Close()
			return nil, fmt.Errorf("%w tar.gz 文件：%s", errCannotDecompressFile, err)
		}
		_, name := filepath.Split(h.Name)
		if isExecutableMatch(cmd, name) {
			if limit <= 0 || h.Size > limit {
				_ = gz.Close()
				return nil, fmt.Errorf("解压后的可执行文件超过 %d 字节限制", limit)
			}
			// 返回包装的 reader，确保 gzip.Reader 可以被关闭
			return &gzipTarReader{
				Reader:     t,
				gzipReader: gz,
			}, nil
		}
	}
	gz.Close()
	return nil, fmt.Errorf("在 tar.gz 文件中%w：%q", errExecutableNotFoundInArchive, cmd)
}

func isExecutableMatch(cmd, target string) bool {
	return cmd == target || cmd+".exe" == target
}
