package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
	"github.com/cxbdasheng/dnet/web"
)

// 配置文件路径
var configFilePath = flag.String("c", config.GetConfigFilePathDefault(), "Custom configuration file path")

// 监听地址
var listen = flag.String("l", ":9876", "Listen address")

// 服务管理
var serviceType = flag.String("s", "", "Service management (install|uninstall|restart)")

// 重置密码
var newPassword = flag.String("resetPassword", "", "Reset password to the one entered")

// 自定义 DNS 服务器
var customDNS = flag.String("dns", "", "Custom DNS server address, example: 8.8.8.8")

// Web 服务
var noWebService = flag.Bool("noweb", false, "No web service")

//go:embed static
var staticEmbeddedFiles embed.FS

//go:embed favicon.ico
var faviconEmbeddedFile embed.FS

// version
var version = "DEV"

func main() {
	flag.Parse()
	// 设置配置文件路径
	if *configFilePath != "" {
		absPath, err := filepath.Abs(*configFilePath)
		if err != nil {
			log.Fatalf("Failed to get absolute path: %v", err)
		}
		os.Setenv(config.PathENV, absPath)
	}
	// 检查监听地址
	if _, err := net.ResolveTCPAddr("tcp", *listen); err != nil {
		log.Fatalf("Parse listen address failed! Exception: %s", err)
	}
	// 设置版本号
	os.Setenv(web.VersionEnv, version)
	// 重置密码
	if *newPassword != "" {
		conf, err := config.GetConfigCached()
		if err == nil {
			err = conf.ResetPassword(*newPassword)
			if err != nil {
				fmt.Printf("重置密码失败: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("密码重置成功")
		} else {
			fmt.Printf("配置文件 %s 不存在, 可通过-c指定配置文件\n", *configFilePath)
			os.Exit(1)
		}
		return
	}
	// 设置自定义DNS
	if *customDNS != "" {
		helper.SetDNS(*customDNS)
	}
	switch *serviceType {
	case "install":
		installService()
	case "uninstall":
		uninstallService()
	case "restart":
		restartService()
	default:
		run()
	}
}
func staticFsFunc(writer http.ResponseWriter, request *http.Request) {
	http.FileServer(http.FS(staticEmbeddedFiles)).ServeHTTP(writer, request)
}

func faviconFsFunc(writer http.ResponseWriter, request *http.Request) {
	http.FileServer(http.FS(faviconEmbeddedFile)).ServeHTTP(writer, request)
}

func runWebServer() error {
	http.HandleFunc("/static/", web.AuthAssert(staticFsFunc))
	http.HandleFunc("/favicon.ico", web.AuthAssert(faviconFsFunc))

	http.HandleFunc("/", web.Auth(web.Home))
	http.HandleFunc("/webhook", web.Auth(web.Webhook))
	http.HandleFunc("/mock", web.Auth(web.Mock))
	http.HandleFunc("/settings", web.Auth(web.Settings))
	http.HandleFunc("/login", web.AuthAssert(web.Login))
	http.HandleFunc("/logout", web.AuthAssert(web.Logout))

	l, err := net.Listen("tcp", *listen)
	if err != nil {
		return errors.New("监听端口发生异常, 请检查端口是否被占用!" + err.Error())
	}
	return http.Serve(l, nil)
}

// run 运行主程序
func run() {
	fmt.Printf("D-NET 启动中...\n")
	fmt.Printf("Web界面: http://localhost%s\n", *listen)

	if !*noWebService {
		go func() {
			// 启动web服务
			err := runWebServer()
			if err != nil {
				log.Printf("Web服务启动失败: %v", err)
				time.Sleep(time.Minute)
				os.Exit(1)
			}
		}()
	}

	// 初始化备用DNS
	helper.InitBackupDNS(*customDNS)

	// 主循环，保持程序运行
	fmt.Println("D-NET 服务已启动，按 Ctrl+C 停止")

	// 创建一个通道用于接收系统信号
	done := make(chan bool)

	// 启动一个goroutine来处理程序逻辑
	go func() {
		// 这里可以添加定时任务或其他业务逻辑
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 每30秒执行一次健康检查或其他任务
				log.Println("D-NET 服务运行正常")
			case <-done:
				return
			}
		}
	}()

	// 阻塞等待
	select {}
}

// installService 安装系统服务
func installService() {
	fmt.Println("正在安装 D-NET 系统服务...")
	executeServiceAction("install")
}

// uninstallService 卸载系统服务
func uninstallService() {
	fmt.Println("正在卸载 D-NET 系统服务...")
	executeServiceAction("uninstall")
}

// restartService 重启系统服务
func restartService() {
	fmt.Println("正在重启 D-NET 系统服务...")
	executeServiceAction("restart")
}

// executeServiceAction 执行服务操作
func executeServiceAction(action string) {
	serviceName := "dnet"
	manager, err := getServiceManager()
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	switch action {
	case "install":
		execPath, err := os.Executable()
		if err != nil {
			fmt.Printf("获取可执行文件路径失败: %v\n", err)
			os.Exit(1)
		}
		if err := manager.Install(serviceName, execPath); err != nil {
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}
	case "uninstall":
		if err := manager.Uninstall(serviceName); err != nil {
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}
	case "restart":
		if err := manager.Restart(serviceName); err != nil {
			fmt.Printf("%v\n", err)
			os.Exit(1)
		}
	}
}

// ServiceManager 服务管理接口
type ServiceManager interface {
	Install(serviceName, execPath string) error
	Uninstall(serviceName string) error
	Restart(serviceName string) error
}

// WindowsServiceManager Windows服务管理器
type WindowsServiceManager struct{}

func (w *WindowsServiceManager) Install(serviceName, execPath string) error {
	cmd := exec.Command("sc", "create", serviceName, "binPath=", execPath, "start=", "auto")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("安装Windows服务失败: %v", err)
	}
	fmt.Println("Windows服务安装成功")
	return nil
}

func (w *WindowsServiceManager) Uninstall(serviceName string) error {
	exec.Command("sc", "stop", serviceName).Run()
	cmd := exec.Command("sc", "delete", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("卸载Windows服务失败: %v", err)
	}
	fmt.Println("Windows服务卸载成功")
	return nil
}

func (w *WindowsServiceManager) Restart(serviceName string) error {
	exec.Command("sc", "stop", serviceName).Run()
	cmd := exec.Command("sc", "start", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("重启Windows服务失败: %v", err)
	}
	fmt.Println("Windows服务重启成功")
	return nil
}

// LinuxServiceManager Linux服务管理器
type LinuxServiceManager struct{}

func (l *LinuxServiceManager) Install(serviceName, execPath string) error {
	serviceContent := fmt.Sprintf(`[Unit]
Description=D-NET Service
After=network.target

[Service]
Type=simple
User=root
ExecStart=%s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`, execPath)

	serviceFile := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
	if err := os.WriteFile(serviceFile, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("创建systemd服务文件失败: %v", err)
	}

	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", serviceName).Run()
	fmt.Println("Linux服务安装成功")
	return nil
}

func (l *LinuxServiceManager) Uninstall(serviceName string) error {
	exec.Command("systemctl", "stop", serviceName).Run()
	exec.Command("systemctl", "disable", serviceName).Run()

	serviceFile := fmt.Sprintf("/etc/systemd/system/%s.service", serviceName)
	os.Remove(serviceFile)
	exec.Command("systemctl", "daemon-reload").Run()
	fmt.Println("Linux服务卸载成功")
	return nil
}

func (l *LinuxServiceManager) Restart(serviceName string) error {
	cmd := exec.Command("systemctl", "restart", serviceName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("重启Linux服务失败: %v", err)
	}
	fmt.Println("Linux服务重启成功")
	return nil
}

// MacServiceManager macOS服务管理器
type MacServiceManager struct{}

func (m *MacServiceManager) Install(serviceName, execPath string) error {
	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.dnet.%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
`, serviceName, execPath)

	plistFile := fmt.Sprintf("/Library/LaunchDaemons/com.dnet.%s.plist", serviceName)
	if err := os.WriteFile(plistFile, []byte(plistContent), 0644); err != nil {
		return fmt.Errorf("创建LaunchDaemon plist文件失败: %v", err)
	}

	cmd := exec.Command("launchctl", "load", plistFile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("加载macOS服务失败: %v", err)
	}
	fmt.Println("macOS服务安装成功")
	return nil
}

func (m *MacServiceManager) Uninstall(serviceName string) error {
	plistFile := fmt.Sprintf("/Library/LaunchDaemons/com.dnet.%s.plist", serviceName)
	exec.Command("launchctl", "unload", plistFile).Run()
	os.Remove(plistFile)
	fmt.Println("macOS服务卸载成功")
	return nil
}

func (m *MacServiceManager) Restart(serviceName string) error {
	plistFile := fmt.Sprintf("/Library/LaunchDaemons/com.dnet.%s.plist", serviceName)
	exec.Command("launchctl", "unload", plistFile).Run()
	cmd := exec.Command("launchctl", "load", plistFile)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("重启macOS服务失败: %v", err)
	}
	fmt.Println("macOS服务重启成功")
	return nil
}

// getServiceManager 根据操作系统获取服务管理器
func getServiceManager() (ServiceManager, error) {
	switch runtime.GOOS {
	case "windows":
		return &WindowsServiceManager{}, nil
	case "linux":
		return &LinuxServiceManager{}, nil
	case "darwin":
		return &MacServiceManager{}, nil
	default:
		return nil, fmt.Errorf("不支持的操作系统: %s", runtime.GOOS)
	}
}
