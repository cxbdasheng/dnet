package main

import (
	"fmt"
	"log"
	"os"

	"github.com/kardianos/service"
)

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
	case "windows-service":
		// 将 Windows 服务的启动类型设为自动(延迟启动)
		options["DelayedAutoStart"] = true
	default:
		// 向 Systemd 添加网络依赖
		depends = append(depends, "Requires=network.target",
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

	// 添加DNS配置参数
	if *customDNS != "" {
		svcConfig.Arguments = append(svcConfig.Arguments, "-dns", *customDNS)
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatalln(err)
	}
	return s
}

// installServiceNew 使用service库安装系统服务
func installServiceNew() {
	fmt.Println("正在安装 D-NET 系统服务...")

	s := getService()
	err := s.Install()
	if err != nil {
		fmt.Printf("D-NET 服务安装失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("D-NET 服务安装成功")
}

// uninstallServiceNew 使用service库卸载系统服务
func uninstallServiceNew() {
	fmt.Println("正在卸载 D-NET 系统服务...")

	s := getService()
	s.Stop()
	err := s.Uninstall()
	if err != nil {
		fmt.Printf("D-NET 服务卸载失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("D-NET 服务卸载成功")
}

// restartServiceNew 使用service库重启系统服务
func restartServiceNew() {
	fmt.Println("正在重启 D-NET 系统服务...")

	s := getService()
	err := s.Restart()
	if err != nil {
		fmt.Printf("D-NET 服务重启失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("D-NET 服务重启成功")
}
