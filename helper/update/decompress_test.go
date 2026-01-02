package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func TestIsExecutableMatch(t *testing.T) {
	tests := []struct {
		name   string
		cmd    string
		target string
		want   bool
	}{
		{
			name:   "完全匹配",
			cmd:    "dnet",
			target: "dnet",
			want:   true,
		},
		{
			name:   "匹配 .exe 后缀",
			cmd:    "dnet",
			target: "dnet.exe",
			want:   true,
		},
		{
			name:   "不匹配",
			cmd:    "dnet",
			target: "other",
			want:   false,
		},
		{
			name:   "不匹配不同的 .exe",
			cmd:    "dnet",
			target: "other.exe",
			want:   false,
		},
		{
			name:   "cmd 已有 .exe 后缀",
			cmd:    "dnet.exe",
			target: "dnet.exe",
			want:   true,
		},
		{
			name:   "cmd 有 .exe 但 target 没有",
			cmd:    "dnet.exe",
			target: "dnet",
			want:   false,
		},
		{
			name:   "空字符串",
			cmd:    "",
			target: "",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExecutableMatch(tt.cmd, tt.target); got != tt.want {
				t.Errorf("isExecutableMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}

// createTestZip 创建一个包含指定文件的测试 ZIP 存档
func createTestZip(files map[string]string) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write([]byte(content)); err != nil {
			return nil, err
		}
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf, nil
}

// createTestTarGz 创建一个包含指定文件的测试 TAR.GZ 存档
func createTestTarGz(files map[string]string) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)
	gw := gzip.NewWriter(buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0755,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf, nil
}

func TestUnzip(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		cmd     string
		wantErr bool
	}{
		{
			name: "找到可执行文件",
			files: map[string]string{
				"dnet":      "executable content",
				"README.md": "readme content",
			},
			cmd:     "dnet",
			wantErr: false,
		},
		{
			name: "找到 .exe 文件",
			files: map[string]string{
				"dnet.exe":  "executable content",
				"README.md": "readme content",
			},
			cmd:     "dnet",
			wantErr: false,
		},
		{
			name: "未找到可执行文件",
			files: map[string]string{
				"other":     "other content",
				"README.md": "readme content",
			},
			cmd:     "dnet",
			wantErr: true,
		},
		{
			name: "带路径的文件名",
			files: map[string]string{
				"bin/dnet":  "executable content",
				"README.md": "readme content",
			},
			cmd:     "dnet",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zipBuf, err := createTestZip(tt.files)
			if err != nil {
				t.Fatalf("创建测试 ZIP 失败: %v", err)
			}

			reader, err := unzip(zipBuf, tt.cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("unzip() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && reader == nil {
				t.Error("unzip() 返回的 reader 不应该为 nil")
			}

			if !tt.wantErr && reader != nil {
				// 验证可以读取内容
				content, err := io.ReadAll(reader)
				if err != nil {
					t.Errorf("读取解压后的内容失败: %v", err)
				}
				if len(content) == 0 {
					t.Error("解压后的内容不应该为空")
				}
			}
		})
	}
}

func TestUntar(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		cmd     string
		wantErr bool
	}{
		{
			name: "找到可执行文件",
			files: map[string]string{
				"dnet":      "executable content",
				"README.md": "readme content",
			},
			cmd:     "dnet",
			wantErr: false,
		},
		{
			name: "找到 .exe 文件",
			files: map[string]string{
				"dnet.exe":  "executable content",
				"README.md": "readme content",
			},
			cmd:     "dnet",
			wantErr: false,
		},
		{
			name: "未找到可执行文件",
			files: map[string]string{
				"other":     "other content",
				"README.md": "readme content",
			},
			cmd:     "dnet",
			wantErr: true,
		},
		{
			name: "带路径的文件名",
			files: map[string]string{
				"bin/dnet":  "executable content",
				"README.md": "readme content",
			},
			cmd:     "dnet",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tarGzBuf, err := createTestTarGz(tt.files)
			if err != nil {
				t.Fatalf("创建测试 TAR.GZ 失败: %v", err)
			}

			reader, err := untar(tarGzBuf, tt.cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("untar() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && reader == nil {
				t.Error("untar() 返回的 reader 不应该为 nil")
			}

			if !tt.wantErr && reader != nil {
				// 验证可以读取内容
				content, err := io.ReadAll(reader)
				if err != nil {
					t.Errorf("读取解压后的内容失败: %v", err)
				}
				if len(content) == 0 {
					t.Error("解压后的内容不应该为空")
				}

				// 验证 gzipTarReader 可以被关闭
				if closer, ok := reader.(io.Closer); ok {
					if err := closer.Close(); err != nil {
						t.Errorf("关闭 reader 失败: %v", err)
					}
				}
			}
		})
	}
}

func TestExtractExecutable(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string]string
		url      string
		execName string
		wantErr  bool
	}{
		{
			name: "从 .zip 文件提取",
			files: map[string]string{
				"dnet": "executable content",
			},
			url:      "https://example.com/release_linux_amd64.zip",
			execName: "dnet",
			wantErr:  false,
		},
		{
			name: "从 .tar.gz 文件提取",
			files: map[string]string{
				"dnet": "executable content",
			},
			url:      "https://example.com/release_linux_amd64.tar.gz",
			execName: "dnet",
			wantErr:  false,
		},
		{
			name:     "非压缩文件直接返回",
			files:    nil,
			url:      "https://example.com/dnet",
			execName: "dnet",
			wantErr:  false,
		},
		{
			name: "未找到可执行文件",
			files: map[string]string{
				"other": "other content",
			},
			url:      "https://example.com/release.zip",
			execName: "dnet",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var src io.Reader

			if tt.files != nil {
				if strings.HasSuffix(tt.url, ".zip") {
					zipBuf, err := createTestZip(tt.files)
					if err != nil {
						t.Fatalf("创建测试 ZIP 失败: %v", err)
					}
					src = zipBuf
				} else if strings.HasSuffix(tt.url, ".tar.gz") {
					tarGzBuf, err := createTestTarGz(tt.files)
					if err != nil {
						t.Fatalf("创建测试 TAR.GZ 失败: %v", err)
					}
					src = tarGzBuf
				}
			} else {
				src = strings.NewReader("raw executable content")
			}

			reader, err := extractExecutable(src, tt.url, tt.execName)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractExecutable() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && reader == nil {
				t.Error("extractExecutable() 返回的 reader 不应该为 nil")
			}

			if !tt.wantErr && reader != nil {
				// 验证可以读取内容
				content, err := io.ReadAll(reader)
				if err != nil {
					t.Errorf("读取提取后的内容失败: %v", err)
				}
				if len(content) == 0 {
					t.Error("提取后的内容不应该为空")
				}
			}
		})
	}
}
