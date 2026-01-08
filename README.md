<div align="center">

# D-NET 动态网络解析管理系统
一款轻量级动态网络管理工具，支持多平台的 CDN、DNS 和 内网穿透自动化管理与监控。

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.23.0-blue.svg)](https://golang.org/)
[![Release](https://img.shields.io/github/v/release/cxbdasheng/dnet)](https://github.com/cxbdasheng/dnet/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/cxbdasheng/dnet)](https://goreportcard.com/report/github.com/cxbdasheng/dnet)
[![Docker Pulls](https://img.shields.io/docker/pulls/cxbdasheng/dnet)](https://hub.docker.com/r/cxbdasheng/dnet)
[![GitHub Downloads](https://img.shields.io/github/downloads/cxbdasheng/dnet/total)](https://github.com/cxbdasheng/dnet/releases)

</div>

---

## 主要功能

- **动态 CDN 管理 (DCDN)：** 支持阿里云（CDN、DCDN、ESA）、腾讯云（CDN、EdgeOne）、百度云（CDN、DRCDN）
- **动态 DNS 管理 (DDNS)：** 自动更新域名解析记录（V2 版本规划中）
- **内网穿透管理：** 从外网访问内网服务（V3 版本规划中）
- **Webhook 通知：** 实时推送 IP 变更通知
- **Web 管理界面：** 可视化配置和管理

### 界面
![界面](https://raw.githubusercontent.com/cxbdasheng/dnet/refs/heads/main/dnet.png)

## 快速开始

> 更多使用示例和详细配置说明，请查看 [Wiki 文档](https://github.com/cxbdasheng/dnet/wiki)

### 方式一：使用二进制文件

#### 1. 下载安装

从 [Releases](https://github.com/cxbdasheng/dnet/releases) 页面下载适合您系统的版本并解压。

#### 2. 安装为系统服务

**Mac/Linux:**
```bash
sudo ./dnet -s install
```

**Windows（管理员权限）：**
```bash
.\dnet.exe -s install
```

#### 3. 访问 Web 界面

浏览器访问 `http://localhost:9877` 进行配置。

#### 服务管理

```bash
# 卸载服务
sudo ./dnet -s uninstall          # Mac/Linux
.\dnet.exe -s uninstall           # Windows (管理员)

# 重启服务
sudo ./dnet -s restart            # Mac/Linux
.\dnet.exe -s restart             # Windows (管理员)
```

#### 高级选项

安装服务时可以指定以下参数：

| 参数 | 说明 | 示例                        |
|------|------|---------------------------|
| `-l` | 监听地址 | `-l :9877`                |
| `-f` | 同步间隔时间（秒） | `-f 600`                  |
| `-dcdnCacheTimes` | 间隔 N 次与服务商比对 | `-dcdnCacheTimes 10`      |
| `-c` | 自定义配置文件路径 | `-c /path/to/config.yaml` |
| `-noweb` | 不启动 Web 服务 | `-noweb`                  |
| `-skipVerify` | 跳过 HTTPS 证书验证 | `-skipVerify`             |
| `-dns` | 自定义 DNS 服务器 | `-dns 8.8.8.8`            |
| `-resetPassword` | 重置密码 | `-resetPassword newpass`  |

> 更多使用参数，请查看 [Wiki 文档 - D‐NET 使用指南](https://github.com/cxbdasheng/dnet/wiki/D%E2%80%90NET-%E4%BD%BF%E7%94%A8%E6%8C%87%E5%8D%97#%E5%91%BD%E4%BB%A4%E5%8F%82%E6%95%B0)

**使用示例：**
```bash
# 自定义同步间隔和配置文件路径
./dnet -s install -f 600 -c /path/to/config.yaml

# 重置密码
./dnet -resetPassword 123456
```
### 方式二：使用 Docker

**Linux（推荐 Host 模式）：**
```bash
docker run -d --name dnet --net=host -v /opt/dnet:/root --restart=always cxbdasheng/dnet:latest
```

**macOS / Windows（端口映射）：**
```bash
docker run -d --name dnet -p 9877:9877 -v /opt/dnet:/root --restart=always cxbdasheng/dnet:latest
```

> **说明：** Host 模式支持 IPv6 地址检测（仅 Linux）；**端口映射无法直接获取宿主机的网卡信息，可能无法检测 IPv6**。

**常用命令：**
```bash
# 重置密码
docker exec dnet ./dnet -resetPassword 123456 && docker restart dnet

# 查看日志
docker logs -f dnet
```
### 方式三：从源码构建
```bash
make build                              # 构建当前平台
goreleaser build --snapshot --clean     # 构建所有平台
go run main.go                          # 直接运行
```

## Webhook 通知

支持的变量：`#{serviceType}`（服务类型）、`#{serviceName}`（服务名称）、`#{serviceStatus}`（更新结果）

<details>
<summary>配置示例</summary>

**Server酱：**
```
https://sctapi.ftqq.com/[SendKey].send?title=DNET通知&desp=#{serviceName} - #{serviceStatus}
```

**钉钉机器人：**
```json
{
  "msgtype": "markdown",
  "markdown": {
    "title": "DNET 通知",
    "text": "#{serviceName} - #{serviceStatus}"
  }
}
```
</details>

## 贡献与许可

欢迎贡献代码或提出建议，详见 [贡献指南](CONTRIBUTING.md)。本项目采用 [MIT](LICENSE) 许可证。
