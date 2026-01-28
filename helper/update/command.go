package update

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/cxbdasheng/dnet/helper"
)

// CheckAndUpdate 检查并执行更新流程
// currentVersion: 当前版本号
// autoRestart: 更新成功后是否自动重启
func CheckAndUpdate(currentVersion string, autoRestart bool) error {
	helper.Info(helper.LogTypeSystem, "正在检查更新...")
	helper.Info(helper.LogTypeSystem, "当前版本: %s", currentVersion)

	// 检查当前版本是否为语义化版本
	v, err := NewVersion(currentVersion)
	if err != nil {
		helper.Warn(helper.LogTypeSystem, "当前版本 '%s' 不是语义化版本格式，无法自动更新", currentVersion)
		helper.Info(helper.LogTypeSystem, "请手动下载最新版本: https://github.com/cxbdasheng/dnet/releases/latest")
		return fmt.Errorf("版本格式不正确: %w", err)
	}

	// 获取最新版本信息
	latestVersion, downloadURL, err := GetLatestRelease()
	if err != nil {
		return fmt.Errorf("获取最新版本失败: %w", err)
	}

	helper.Info(helper.LogTypeSystem, "最新版本: %s", latestVersion)

	// 比较版本
	if v.GreaterThanOrEqual(latestVersion) {
		helper.Info(helper.LogTypeSystem, "当前已是最新版本，无需更新")
		return nil
	}

	// 显示版本变化
	helper.Info(helper.LogTypeSystem, "发现新版本: %s -> %s", currentVersion, latestVersion.String())

	// 询问用户是否确认更新
	if !confirmUpdate() {
		helper.Info(helper.LogTypeSystem, "已取消更新")
		return nil
	}

	// 获取当前可执行文件路径
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	// 下载并替换可执行文件
	helper.Info(helper.LogTypeSystem, "正在下载最新版本...")
	if err := Update(downloadURL, exePath); err != nil {
		helper.Error(helper.LogTypeSystem, "更新失败: %v", err)
		helper.Info(helper.LogTypeSystem, "请尝试手动下载: %s", downloadURL)
		return fmt.Errorf("更新失败: %w", err)
	}

	helper.Info(helper.LogTypeSystem, "✓ 更新成功! 版本 %s -> %s", currentVersion, latestVersion.String())

	// 处理重启
	if autoRestart {
		helper.Info(helper.LogTypeSystem, "正在重启程序...")
		return restartProgram(exePath)
	}

	helper.Info(helper.LogTypeSystem, "请重启程序以使用新版本")
	return nil
}

// confirmUpdate 询问用户是否确认更新
func confirmUpdate() bool {
	fmt.Print("是否确认更新? [Y/n]: ")
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	input = strings.TrimSpace(strings.ToLower(input))
	return input == "" || input == "y" || input == "yes"
}

// restartProgram 重启当前程序
func restartProgram(exePath string) error {
	args := os.Args

	// 启动新进程
	cmd := os.Args[0]
	env := os.Environ()

	// 使用 StartProcess 而不是 Exec，以便当前进程能正常退出
	attr := &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Env:   env,
	}

	process, err := os.StartProcess(cmd, args, attr)
	if err != nil {
		return fmt.Errorf("重启程序失败: %w", err)
	}

	// 释放新进程，让其独立运行
	if err := process.Release(); err != nil {
		return fmt.Errorf("释放新进程失败: %w", err)
	}

	// 退出当前进程
	os.Exit(0)
	return nil
}
