package update

import (
	"fmt"
	"runtime"
)

const (
	minARM = 5
	maxARM = 7
)

// 生成额外的架构
func generateAdditionalArch() []string {
	arch := make([]string, 0, 4)

	switch runtime.GOARCH {
	case "arm":
		if goarm >= minARM && goarm <= maxARM {
			for v := goarm; v >= minARM; v-- {
				arch = append(arch, fmt.Sprintf("armv%d", v))
			}
		}
	case "amd64":
		arch = append(arch, "x86_64")
	case "riscv64":
		arch = append(arch, "riscv64")
	}

	arch = append(arch, runtime.GOARCH)
	return arch
}
