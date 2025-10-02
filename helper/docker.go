package helper

import (
	"os"
	"strings"
)

// DockerEnvFile Docker容器中包含的文件
const DockerEnvFile string = "/.dockerenv"

// IsRunInDocker 检查是否在Docker容器中运行
func IsRunInDocker() bool {
	if _, err := os.Stat(DockerEnvFile); err == nil {
		return true
	}

	// 检查/proc/1/cgroup文件中是否包含docker
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		return strings.Contains(string(data), "docker") || strings.Contains(string(data), "containerd")
	}

	return false
}
