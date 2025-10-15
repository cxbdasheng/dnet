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
	"path/filepath"
	"time"

	"github.com/cxbdasheng/dnet/cdn"
	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/helper"
	"github.com/cxbdasheng/dnet/web"
	"github.com/kardianos/service"
)

// 配置文件路径
var configFilePath = flag.String("c", config.GetConfigFilePathDefault(), "Custom configuration file path")

// 监听地址
var listen = flag.String("l", ":9876", "Listen address")

// 更新频率(秒)
var every = flag.Int("f", 300, "Update frequency(seconds)")

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
		absPath, _ := filepath.Abs(*configFilePath)
		os.Setenv(config.PathENV, absPath)
	}

	// 检查监听地址，查看是否合法
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
		if helper.IsRunInDocker() {
			run()
		} else {
			s := getService()
			status, _ := s.Status()
			if status != service.StatusUnknown {
				// 以服务方式运行
				s.Run()
			} else {
				// 非服务方式运行
				switch s.Platform() {
				case "windows-service":
					fmt.Println("可使用 .\\dnet.exe -s install 安装服务运行")
				default:
					fmt.Println("可使用 sudo ./dnet -s install 安装服务运行")
				}
				run()
			}
		}
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
func run() {

	if !*noWebService {
		go func() {
			// 启动web服务
			err := runWebServer()
			if err != nil {
				log.Println(err)
				time.Sleep(time.Minute)
				os.Exit(1)
			}
		}()
	}

	// 初始化备用DNS
	helper.InitBackupDNS(*customDNS)

	// 等待网络连接
	cdn.RunTimer(time.Duration(*every) * time.Second)
}
