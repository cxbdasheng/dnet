package update

import (
	"runtime"
	"testing"
)

func TestGenerateAdditionalArch(t *testing.T) {
	arch := generateAdditionalArch()

	// 验证返回的架构列表不为空
	if len(arch) == 0 {
		t.Error("generateAdditionalArch() 返回空列表")
	}

	// 验证最后一个元素总是 runtime.GOARCH
	if arch[len(arch)-1] != runtime.GOARCH {
		t.Errorf("最后一个架构应该是 %s，实际是 %s", runtime.GOARCH, arch[len(arch)-1])
	}

	// 根据当前架构验证特定行为
	switch runtime.GOARCH {
	case "amd64":
		// amd64 应该包含 x86_64 和 amd64
		if len(arch) != 2 {
			t.Errorf("amd64 应该返回 2 个架构，实际返回 %d 个", len(arch))
		}
		if arch[0] != "x86_64" {
			t.Errorf("amd64 的第一个架构应该是 x86_64，实际是 %s", arch[0])
		}

	case "arm":
		// arm 架构应该包含 armv5-armv7（取决于 goarm 值）和 arm
		if len(arch) < 2 {
			t.Errorf("arm 架构应该返回至少 2 个架构，实际返回 %d 个", len(arch))
		}
		// 验证包含 armv 前缀的架构
		foundArmv := false
		for _, a := range arch[:len(arch)-1] {
			if len(a) > 4 && a[:4] == "armv" {
				foundArmv = true
				break
			}
		}
		if !foundArmv {
			t.Error("arm 架构应该包含 armv* 变体")
		}

	case "riscv64":
		// riscv64 应该返回 2 个相同的架构
		if len(arch) != 2 {
			t.Errorf("riscv64 应该返回 2 个架构，实际返回 %d 个", len(arch))
		}
		if arch[0] != "riscv64" {
			t.Errorf("riscv64 的第一个架构应该是 riscv64，实际是 %s", arch[0])
		}

	default:
		// 其他架构应该只返回 runtime.GOARCH
		if len(arch) != 1 {
			t.Errorf("未知架构 %s 应该只返回 1 个架构，实际返回 %d 个", runtime.GOARCH, len(arch))
		}
	}
}

func TestGenerateAdditionalArch_NoEmpty(t *testing.T) {
	arch := generateAdditionalArch()

	// 验证没有空字符串
	for i, a := range arch {
		if a == "" {
			t.Errorf("位置 %d 的架构不应该为空字符串", i)
		}
	}
}

func TestGenerateAdditionalArch_NoDuplicates(t *testing.T) {
	arch := generateAdditionalArch()

	// 验证除了特殊情况外没有重复
	seen := make(map[string]bool)
	duplicates := []string{}

	for _, a := range arch {
		if seen[a] {
			duplicates = append(duplicates, a)
		}
		seen[a] = true
	}

	// riscv64 架构会有重复，这是预期的
	if runtime.GOARCH != "riscv64" && len(duplicates) > 0 {
		t.Errorf("发现重复的架构: %v", duplicates)
	}
}

func TestGenerateAdditionalArch_Consistency(t *testing.T) {
	// 多次调用应该返回相同的结果
	arch1 := generateAdditionalArch()
	arch2 := generateAdditionalArch()

	if len(arch1) != len(arch2) {
		t.Errorf("两次调用返回的架构数量不一致: %d vs %d", len(arch1), len(arch2))
	}

	for i := range arch1 {
		if arch1[i] != arch2[i] {
			t.Errorf("位置 %d 的架构不一致: %s vs %s", i, arch1[i], arch2[i])
		}
	}
}
