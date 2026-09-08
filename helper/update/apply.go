package update

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// apply 使用给定的 io.Reader 的内容来更新 targetPath 的可执行文件。
//
// apply 执行以下操作以确保安全的跨平台更新：
//
// 1. 创建新文件 /path/to/target.new，并将更新文件的内容写入其中
//
// 2. 将 /path/to/target 重命名为 /path/to/target.old
//
// 3. 将 /path/to/target.new 重命名为 /path/to/target
//
// 4.如果最终的重命名成功，删除 /path/to/target.old 并返回无错误。
//
// 5. 如果最终重命名失败，尝试通过将 /path/to/target.old 重命名会
// /path/to/target 进行回滚。
//
// 如果回滚操作失败，文件系统将处于不一致状态（第 4 步和第 5 步之间），
// 既没有新的可执行文件，并且旧的可执行文件无法移动回其原始位置。在这种情况下，
// 应该通知用户这个坏消息，并要求他们手动恢复。
func apply(update io.Reader, targetPath string) error {
	return applyWithLimit(update, targetPath, maxExecutableSize)
}

func applyWithLimit(update io.Reader, targetPath string, limit int64) error {
	if limit <= 0 {
		return fmt.Errorf("可执行文件大小限制必须大于零")
	}

	// 获取原文件权限，以便保持一致
	perm := os.FileMode(0755) // 默认权限
	if info, err := os.Stat(targetPath); err == nil {
		perm = info.Mode().Perm()
	}

	// 获取可执行文件所在的目录
	updateDir := filepath.Dir(targetPath)
	filename := filepath.Base(targetPath)

	// 将新二进制的内容复制到新可执行文件中
	newPath := filepath.Join(updateDir, fmt.Sprintf("%s.new", filename))
	fp, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("创建新文件失败: %w", err)
	}

	// 直接流式复制，避免将整个文件加载到内存。多读取一个字节以检测超限。
	written, err := io.Copy(fp, io.LimitReader(update, limit+1))
	closeErr := fp.Close()

	if err != nil {
		_ = os.Remove(newPath) // 尽力清理，忽略错误
		return fmt.Errorf("写入新文件失败: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(newPath) // 尽力清理，忽略错误
		return fmt.Errorf("关闭新文件失败: %w", closeErr)
	}
	if written > limit {
		_ = os.Remove(newPath)
		return fmt.Errorf("解压后的可执行文件超过 %d 字节限制", limit)
	}

	// 验证文件是否写入成功（非空）
	if written == 0 {
		_ = os.Remove(newPath) // 尽力清理，忽略错误
		return fmt.Errorf("写入的文件为空")
	}

	// 这是我们将要移动可执行文件的位置，以便可以将更新的文件替代进来
	oldPath := filepath.Join(updateDir, fmt.Sprintf("%s.old", filename))

	// 删除任何现有的旧可执行文件 - 这在 Windows 上是必要的，原因有两个：
	// 1. 成功更新后，Windows 无法删除 .old 文件，因为进程仍在运行
	// 2. 如果目标文件已存在，Windows 重命名操作将失败
	_ = os.Remove(oldPath)

	// 将现有的可执行文件移到同一目录下的新文件中
	err = os.Rename(targetPath, oldPath)
	if err != nil {
		_ = os.Remove(newPath)
		return err
	}

	// 将新可执行文件移到目标位置
	err = os.Rename(newPath, targetPath)

	if err != nil {
		// 移动失败
		//
		// 文件系统现在处于不良状态。我们已成功将现有的二进制文件移动到新位置，
		// 但无法将新二进制文件移动到原来的位置。这意味着当前可执行文件的位置上没有文件！
		// 尝试通过将旧二进制文件恢复到原始路径来回滚。
		rerr := os.Rename(oldPath, targetPath)
		if rerr != nil {
			return fmt.Errorf("更新失败且回滚也失败: 更新错误=%v, 回滚错误=%v", err, rerr)
		}

		return fmt.Errorf("更新失败: %w", err)
	}

	// 移动成功，删除旧的二进制文件
	err = os.Remove(oldPath)
	if err != nil {
		if runtime.GOOS == "windows" {
			// Windows 无法删除仍在运行的旧可执行文件。使用固定的
			// PowerShell 脚本和环境变量传递路径，避免将路径拼接进 shell 命令。
			powershellCleanup := exec.Command(
				"powershell.exe",
				"-NoProfile",
				"-NonInteractive",
				"-Command",
				"Start-Sleep -Seconds 1; Remove-Item -LiteralPath $env:DNET_UPDATE_OLD_PATH -Force",
			)
			cleanupEnv := append(os.Environ(), "DNET_UPDATE_OLD_PATH="+oldPath)
			powershellCleanup.Env = cleanupEnv
			if startErr := powershellCleanup.Start(); startErr == nil {
				return nil
			} else {
				cmdCleanup := exec.Command(
					"cmd.exe",
					"/d",
					"/c",
					"ping 127.0.0.1 -n 2 > NUL & del /f /q \"%DNET_UPDATE_OLD_PATH%\"",
				)
				cmdCleanup.Env = cleanupEnv
				if fallbackErr := cmdCleanup.Start(); fallbackErr != nil {
					return fmt.Errorf("更新已完成，但无法启动旧文件清理进程: PowerShell=%v, cmd.exe=%v", startErr, fallbackErr)
				}
				return nil
			}
		}

		return err
	}

	return nil
}
