package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

var checksumLinePattern = regexp.MustCompile(`^([0-9A-Fa-f]{64})  (.+)$`)

func parseChecksums(data []byte) (map[string][sha256.Size]byte, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.HasSuffix(text, "\n") {
		text = strings.TrimSuffix(text, "\n")
	}
	if text == "" {
		return nil, fmt.Errorf("校验文件为空")
	}

	checksums := make(map[string][sha256.Size]byte)
	for lineNumber, line := range strings.Split(text, "\n") {
		match := checksumLinePattern.FindStringSubmatch(line)
		if match == nil {
			return nil, fmt.Errorf("校验文件第 %d 行格式无效", lineNumber+1)
		}

		name := match[2]
		if err := validateChecksumFilename(name); err != nil {
			return nil, fmt.Errorf("校验文件第 %d 行文件名无效: %w", lineNumber+1, err)
		}
		if _, exists := checksums[name]; exists {
			return nil, fmt.Errorf("校验文件包含重复文件名 %q", name)
		}

		digestBytes, err := hex.DecodeString(match[1])
		if err != nil {
			return nil, fmt.Errorf("校验文件第 %d 行 SHA-256 无效: %w", lineNumber+1, err)
		}
		var digest [sha256.Size]byte
		copy(digest[:], digestBytes)
		checksums[name] = digest
	}
	return checksums, nil
}

func validateChecksumFilename(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("文件名为空或为特殊路径")
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("文件名包含首尾空白")
	}
	if strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("文件名不能包含路径分隔符")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("文件名包含控制字符")
		}
	}
	return nil
}

func archiveChecksum(checksumData []byte, archiveName string) ([sha256.Size]byte, error) {
	checksums, err := parseChecksums(checksumData)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	expected, ok := checksums[archiveName]
	if !ok {
		return [sha256.Size]byte{}, fmt.Errorf("校验文件缺少 %q 的 SHA-256", archiveName)
	}
	return expected, nil
}

func verifyArchiveChecksum(checksumData []byte, archiveName string, archiveData []byte) error {
	expected, err := archiveChecksum(checksumData, archiveName)
	if err != nil {
		return err
	}
	actual := sha256.Sum256(archiveData)
	if actual != expected {
		return fmt.Errorf("%q 的 SHA-256 校验失败", archiveName)
	}
	return nil
}
