package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyWithLimitRejectsOversizedExecutable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dnet")
	const oldContent = "old executable"
	const oldPerm = 0751

	if err := os.WriteFile(target, []byte(oldContent), oldPerm); err != nil {
		t.Fatalf("写入旧可执行文件失败: %v", err)
	}

	err := applyWithLimit(strings.NewReader("123456"), target, 5)
	if err == nil || !strings.Contains(err.Error(), "超过 5 字节限制") {
		t.Fatalf("applyWithLimit() error = %v, want size limit error", err)
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("读取旧可执行文件失败: %v", err)
	}
	if string(content) != oldContent {
		t.Fatalf("旧可执行文件内容 = %q, want %q", content, oldContent)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("读取旧可执行文件信息失败: %v", err)
	}
	if got := info.Mode().Perm(); got != oldPerm {
		t.Fatalf("旧可执行文件权限 = %o, want %o", got, oldPerm)
	}

	for _, suffix := range []string{".new", ".old"} {
		if _, err := os.Stat(target + suffix); !os.IsNotExist(err) {
			t.Fatalf("临时文件 %q 不应存在: %v", target+suffix, err)
		}
	}
}

func TestApplyWithLimitRejectsInvalidLimit(t *testing.T) {
	target := filepath.Join(t.TempDir(), "dnet")
	if err := os.WriteFile(target, []byte("old"), 0755); err != nil {
		t.Fatalf("写入旧可执行文件失败: %v", err)
	}

	if err := applyWithLimit(strings.NewReader("new"), target, 0); err == nil {
		t.Fatal("applyWithLimit() error = nil, want invalid limit error")
	}
}
