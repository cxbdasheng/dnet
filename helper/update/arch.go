package update

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// BuildTarget describes the build that is currently running.
type BuildTarget struct {
	GOOS     string
	GOARCH   string
	GOARM    string
	GOMIPS   string
	GOMIPS64 string
}

func currentBuildTarget() BuildTarget {
	target := BuildTarget{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "GOARM":
				target.GOARM = setting.Value
			case "GOMIPS":
				target.GOMIPS = setting.Value
			case "GOMIPS64":
				target.GOMIPS64 = setting.Value
			}
		}
	}
	return target
}

func (target BuildTarget) archiveName(version string) (string, error) {
	if target.GOOS == "" || target.GOARCH == "" {
		return "", fmt.Errorf("构建目标缺少 GOOS 或 GOARCH")
	}
	arch, err := target.releaseArch()
	if err != nil {
		return "", err
	}
	ext := ".tar.gz"
	if target.GOOS == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("dnet_%s_%s_%s%s", version, target.GOOS, arch, ext), nil
}

func (target BuildTarget) releaseArch() (string, error) {
	switch target.GOARCH {
	case "386":
		return "i386", nil
	case "amd64":
		return "x86_64", nil
	case "arm":
		level := strings.Split(target.GOARM, ",")[0]
		switch level {
		case "5", "6", "7":
			return "armv" + level, nil
		default:
			return "", fmt.Errorf("无法确定 ARM ABI: GOARM=%q", target.GOARM)
		}
	case "mips", "mipsle":
		abi, err := mipsABI("GOMIPS", target.GOMIPS)
		if err != nil {
			return "", err
		}
		return target.GOARCH + "_" + abi, nil
	case "mips64", "mips64le":
		abi, err := mipsABI("GOMIPS64", target.GOMIPS64)
		if err != nil {
			return "", err
		}
		return target.GOARCH + "_" + abi, nil
	default:
		return target.GOARCH, nil
	}
}

func mipsABI(name, value string) (string, error) {
	switch value {
	case "hardfloat", "softfloat":
		return value, nil
	default:
		return "", fmt.Errorf("无法确定 MIPS ABI: %s=%q", name, value)
	}
}
