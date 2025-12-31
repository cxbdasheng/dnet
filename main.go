package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/cxbdasheng/dnet/bootstrap"
	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/dcdn"
	"github.com/cxbdasheng/dnet/helper"
	"github.com/cxbdasheng/dnet/web"
	"github.com/kardianos/service"
)

// 配置文件路径
var configFilePath = flag.String("c", config.GetConfigFilePathDefault(), "Custom configuration file path")

// 监听地址
var listen = flag.String("l", ":9877", "Listen address")

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

// 缓存次数
var dcdnCacheTimes = flag.Int("dcdnCacheTimes", 5, "dcdn Cache times")

// D-NET 版本
var showVersion = flag.Bool("v", false, "D-NET version")

//go:embed static
var staticEmbeddedFiles embed.FS

//go:embed favicon.ico
var faviconEmbeddedFile embed.FS

// version
var version = "DEV"

func main() {
	helper.InitLoggerWithConsole(helper.MaxSize, true)
	flag.Parse()

	// 显示版本
	if *showVersion {
		fmt.Println(version)
		return
	}
	// 设置配置文件路径
	if *configFilePath != "" {
		absPath, _ := filepath.Abs(*configFilePath)
		os.Setenv(config.PathENV, absPath)
	}

	if runtime.GOOS == "android" {
		helper.FixTimezone()
	}

	// 检查监听地址，查看是否合法
	if _, err := net.ResolveTCPAddr("tcp", *listen); err != nil {
		helper.Fatalf(helper.LogTypeSystem, "Parse listen address failed! Exception: %s", err)
	}

	// 设置版本号
	os.Setenv(web.VersionEnv, version)

	// 设置端口
	os.Setenv(config.DNETPort, *listen)

	// 重置密码
	if *newPassword != "" {
		conf, err := config.GetConfigCached()
		if err == nil {
			err = conf.ResetPassword(*newPassword)
			if err != nil {
				helper.Fatalf(helper.LogTypeSystem, "重置密码失败: %v\n", err)
			}
			helper.Info(helper.LogTypeSystem, "密码已重置")
		} else {
			helper.Fatalf(helper.LogTypeSystem, "配置文件 %s 不存在, 可通过 -c 指定配置文件\n", *configFilePath)
		}
		return
	}
	// 设置自定义DNS
	if *customDNS != "" {
		helper.SetDNS(*customDNS)
	}
	// 设置缓存次数
	os.Setenv(dcdn.CacheTimesENV, strconv.Itoa(*dcdnCacheTimes))

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
					helper.Info(helper.LogTypeSystem, "可使用 .\\dnet.exe -s install 安装服务运行")
				default:
					helper.Info(helper.LogTypeSystem, "可使用 sudo ./dnet -s install 安装服务运行")
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
	http.HandleFunc("/dcdn", web.Auth(web.DCDN))
	http.HandleFunc("/api/dcdn/config", web.Auth(web.DCDNConfigAPI))
	http.HandleFunc("/webhook", web.Auth(web.Webhook))
	http.HandleFunc("/mock", web.Auth(web.Mock))
	http.HandleFunc("/settings", web.Auth(web.Settings))
	http.HandleFunc("/logs", web.Auth(web.Logs))
	http.HandleFunc("/login", web.AuthAssert(web.Login))
	http.HandleFunc("/logout", web.AuthAssert(web.Logout))

	l, err := net.Listen("tcp", *listen)
	helper.Info(helper.LogTypeSystem, "监听 %s", *listen)
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
				helper.Info(helper.LogTypeSystem, "启动web服务失败: %v\n", err)
				time.Sleep(time.Minute)
				os.Exit(1)
			}
		}()
	}

	// 初始化备用DNS
	helper.InitBackupDNS(*customDNS)

	// 等待网络连接
	bootstrap.RunTimer(time.Duration(*every) * time.Second)
}

// program 实现 service.Interface 接口
type program struct{}

func (p *program) Start(s service.Service) error {
	// Start 不应该阻塞，异步执行实际工作
	go p.run()
	return nil
}

func (p *program) run() {
	run()
}

func (p *program) Stop(s service.Service) error {
	// Stop 应该快速返回
	return nil
}

// getService 获取服务配置
func getService() service.Service {
	options := make(service.KeyValue)
	var depends []string

	// 确保服务等待网络就绪后再启动
	switch service.ChosenSystem().String() {
	case "unix-systemv":
		// System V init 脚本配置
		options["SysvScript"] = sysvScript
		options["UserService"] = false
	case "unix-upstart":
		// Upstart 配置
		options["UserService"] = false
	case "linux-systemd":
		// Systemd 配置
		depends = append(depends,
			"Requires=network.target",
			"After=network-online.target syslog.target")
		// 失败时自动重启
		options["Restart"] = "on-failure"
		// 启动失败重试间隔
		options["RestartSec"] = 10
		// 文件描述符限制
		options["LimitNOFILE"] = 65536
	case "darwin-launchd":
		// macOS LaunchDaemon 配置
		options["KeepAlive"] = true
		options["RunAtLoad"] = true
		options["UserService"] = false
	case "windows-service":
		// 将 Windows 服务的启动类型设为自动(延迟启动)
		options["DelayedAutoStart"] = true
		// 失败时自动重启
		options["OnFailure"] = "restart"
		// 延迟启动失败时重试间隔
		options["OnFailureDelayDuration"] = "10s"
		options["OnFailureResetPeriod"] = 60
	default:
		// 默认 Systemd 配置
		depends = append(depends,
			"Requires=network.target",
			"After=network-online.target")
	}

	svcConfig := &service.Config{
		Name:         "dnet",
		DisplayName:  "D-NET Service",
		Description:  "D-NET - Dynamic Network Management System",
		Arguments:    []string{"-l", *listen, "-c", *configFilePath},
		Dependencies: depends,
		Option:       options,
	}

	// 非 Web 运行
	if *noWebService {
		svcConfig.Arguments = append(svcConfig.Arguments, "-noweb")
	}
	// 添加DNS配置参数
	if *customDNS != "" {
		svcConfig.Arguments = append(svcConfig.Arguments, "-dns", *customDNS)
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		helper.Fatalf(helper.LogTypeSystem, "创建系统服务失败: %v", err)
	}
	return s
}

// installService 使用service库安装系统服务
func installService() {
	helper.Info(helper.LogTypeSystem, "正在安装 D-NET 系统服务...")

	s := getService()
	status, err := s.Status()
	if err != nil && status == service.StatusUnknown {
		// 服务未知，创建服务
		if err = s.Install(); err == nil {
			if startErr := s.Start(); startErr != nil {
				helper.Error(helper.LogTypeSystem, "服务安装成功但启动失败: %v", startErr)
			}
			helper.Info(helper.LogTypeSystem, "安装 D-NET 服务成功! 请打开浏览器并进行配置")

			// System V init 系统需要额外配置开机自启
			if service.ChosenSystem().String() == "unix-systemv" {
				serviceName := "dnet"
				// 尝试使用 update-rc.d (Debian/Ubuntu)
				if _, err := exec.LookPath("update-rc.d"); err == nil {
					if out, err := exec.Command("update-rc.d", serviceName, "defaults").CombinedOutput(); err != nil {
						helper.Error(helper.LogTypeSystem, "update-rc.d 配置失败: %v, 输出: %s", err, out)
					} else {
						helper.Info(helper.LogTypeSystem, "已配置开机自启 (update-rc.d)")
					}
				} else if _, err := exec.LookPath("chkconfig"); err == nil {
					// 尝试使用 chkconfig (RedHat/CentOS)
					if out, err := exec.Command("chkconfig", "--add", serviceName).CombinedOutput(); err != nil {
						helper.Error(helper.LogTypeSystem, "chkconfig --add 失败: %v, 输出: %s", err, out)
					} else {
						if out, err := exec.Command("chkconfig", serviceName, "on").CombinedOutput(); err != nil {
							helper.Error(helper.LogTypeSystem, "chkconfig on 失败: %v, 输出: %s", err, out)
						} else {
							helper.Info(helper.LogTypeSystem, "已配置开机自启 (chkconfig)")
						}
					}
				}
			}
			return
		}
		helper.Error(helper.LogTypeSystem, "安装 D-NET 服务失败, 异常信息: %v", err)
	}

	if status != service.StatusUnknown {
		helper.Info(helper.LogTypeSystem, "D-NET 服务已安装, 无需再次安装")
	}
}

// uninstallService 使用 service 库卸载系统服务
func uninstallService() {
	helper.Info(helper.LogTypeSystem, "正在卸载 D-NET 系统服务...")

	s := getService()
	if stopErr := s.Stop(); stopErr != nil {
		helper.Warn(helper.LogTypeSystem, "停止服务时出现警告: %v", stopErr)
	}

	// System V init 系统需要额外清理
	if service.ChosenSystem().String() == "unix-systemv" {
		serviceName := "dnet"
		// 尝试使用 update-rc.d 移除 (Debian/Ubuntu)
		if _, err := exec.LookPath("update-rc.d"); err == nil {
			if out, err := exec.Command("update-rc.d", "-f", serviceName, "remove").CombinedOutput(); err != nil {
				helper.Error(helper.LogTypeSystem, "update-rc.d remove 失败: %v, 输出: %s", err, out)
			}
		} else if _, err := exec.LookPath("chkconfig"); err == nil {
			// 尝试使用 chkconfig 移除 (RedHat/CentOS)
			if out, err := exec.Command("chkconfig", "--del", serviceName).CombinedOutput(); err != nil {
				helper.Error(helper.LogTypeSystem, "chkconfig --del 失败: %v, 输出: %s", err, out)
			}
		}
	}

	if err := s.Uninstall(); err != nil {
		helper.Fatal(helper.LogTypeSystem, "D-NET 服务卸载失败: %v", err)
	}
	helper.Info(helper.LogTypeSystem, "D-NET 服务卸载成功")
}

// restartService 使用service库重启系统服务
func restartService() {
	helper.Info(helper.LogTypeSystem, "正在重启 D-NET 系统服务...")

	s := getService()
	status, err := s.Status()
	if err != nil {
		helper.Fatal(helper.LogTypeSystem, "D-NET 服务未安装, 请先安装服务")
	}

	switch status {
	case service.StatusRunning:
		// 服务正在运行，执行重启
		if err = s.Restart(); err != nil {
			helper.Fatal(helper.LogTypeSystem, "D-NET 服务重启失败: %v", err)
		}
		helper.Info(helper.LogTypeSystem, "D-NET 服务重启成功")
	case service.StatusStopped:
		// 服务已停止，执行启动
		if err = s.Start(); err != nil {
			helper.Fatal(helper.LogTypeSystem, "D-NET 服务启动失败: %v", err)
		}
		helper.Info(helper.LogTypeSystem, "D-NET 服务启动成功")
	default:
		helper.Fatal(helper.LogTypeSystem, "D-NET 服务状态未知: %v", status)
	}
}

// sysvScript 定义 System V init 脚本模板
const sysvScript = `#!/bin/sh
### BEGIN INIT INFO
# Provides:          {{.Name}}
# Required-Start:    $network $remote_fs $syslog
# Required-Stop:     $network $remote_fs $syslog
# Default-Start:     2 3 4 5
# Default-Stop:      0 1 6
# Short-Description: {{.DisplayName}}
# Description:       {{.Description}}
### END INIT INFO

cmd="{{.Path}}{{range .Arguments}} {{.}}{{end}}"

name=$(basename $(readlink -f $0))
pid_file="/var/run/$name.pid"
stdout_log="/var/log/$name.log"
stderr_log="/var/log/$name.err"

get_pid() {
    cat "$pid_file"
}

is_running() {
    [ -f "$pid_file" ] && ps -p $(get_pid) > /dev/null 2>&1
}

case "$1" in
    start)
        if is_running; then
            echo "Already started"
        else
            echo "Starting $name"
            $cmd >> "$stdout_log" 2>> "$stderr_log" &
            echo $! > "$pid_file"
        fi
        ;;
    stop)
        if is_running; then
            echo "Stopping $name"
            kill $(get_pid)
            rm -f "$pid_file"
        else
            echo "Not running"
        fi
        ;;
    restart)
        $0 stop
        $0 start
        ;;
    status)
        if is_running; then
            echo "Running"
        else
            echo "Stopped"
            exit 1
        fi
        ;;
    *)
        echo "Usage: $0 {start|stop|restart|status}"
        exit 1
        ;;
esac

exit 0
`
