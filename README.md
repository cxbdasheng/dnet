<div align="center">

# D-NET 动态网络解析管理系统

一款轻量级动态网络管理工具，为动态 IP 环境设计的一体化解决方案

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.23.0-blue.svg)](https://golang.org/)
[![Release](https://img.shields.io/github/v/release/cxbdasheng/dnet)](https://github.com/cxbdasheng/dnet/releases)

[功能特性](#功能特性) • [快速开始](#快速开始) • [使用文档](#使用文档) • [开发指南](#开发指南) • [问题排查](#问题排查)
</div>

---

## 项目介绍
### 主要功能

- **动态 CDN 管理 (DCDN)** - 自动更新 CDN 源站配置，支持多家 CDN 服务商，适用于动态公网 IPv6 转 IPv4/IPv6 场景
- **动态 DNS 管理 (DDNS)** - 自动更新域名解析记录，适用于家庭宽带等动态 IP 场景（V2 版本规划中）
- **Webhook 通知** - 支持多种通知方式，实时推送 IP 变更和更新信息
- **Web 管理界面** - 提供直观的 Web 管理界面，无需命令行操作

### 设计初衷
在国内网络环境下，运营商分配的动态公网 IPv6 地址经常变化，且无法直接支持 IPv4 访问。D-NET 正是为解决这一痛点而生，让动态 IPv6 也能实现稳定的双栈访问。

其次，在市面上虽然有许多成熟的方案如 DDNS、FRP 等，但这些都是独立的解决方案。D-NET 旨在提供轻量级的一体化集成方案，这也是项目的核心目标和持续规划方向。

### 界面
![界面](https://raw.githubusercontent.com/cxbdasheng/dnet/refs/heads/main/dnet.png)

## 功能特性

### 核心功能

- **多平台支持** - Linux、Windows、macOS、Docker 全平台支持
- **系统服务** - 支持作为系统服务运行，开机自启
- **自动更新** - 定时检测 IP 变化并自动更新 DNS 记录
- **配置管理** - 基于 YAML 的配置文件，简单易用
- **日志系统** - 完善的日志记录，便于问题排查
- **安全认证** - 内置用户认证系统，保护管理界面

### Web 管理界面

- 仪表盘 - 实时监控系统状态
- DCDN 管理 - 统一管理多家 CDN 服务
- Webhook 配置 - 灵活的通知方式设置
- 系统设置 - 可视化配置管理
- 日志查看 - 在线查看运行日志

## 快速开始

### 方式一：使用二进制文件

#### 1. 下载安装

从 [Releases](https://github.com/cxbdasheng/dnet/releases) 页面下载适合您系统的版本并解压。

#### 2. 安装为系统服务

**Mac/Linux:**
```bash
sudo ./dnet -s install
```

**Windows:**

以管理员身份打开命令提示符（cmd），然后执行：
```bash
.\dnet.exe -s install
```

#### 3. 访问 Web 界面

打开浏览器访问 `http://localhost:9877` 进行初始化配置。

#### 服务管理

**卸载服务：**

Mac/Linux: `sudo ./dnet -s uninstall`

Windows: `.\dnet.exe -s uninstall`（以管理员身份运行）

**重启服务：**

Mac/Linux: `sudo ./dnet -s restart`

Windows: `.\dnet.exe -s restart`（以管理员身份运行）

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

**使用示例：**

```bash
# 10 分钟同步一次，并指定配置文件路径
./dnet -s install -f 600 -c /Users/name/.dnet_config.yaml

# 每 10 秒检查 IP 变化，每 30 分钟（180 次）与服务商比对一次
# 实现即时响应且避免服务商限流
./dnet -s install -f 10 -dcdnCacheTimes 180

# 重置密码
./dnet -resetPassword 123456
./dnet -resetPassword 123456 -c /Users/name/.dnet_config.yaml
```

### 方式二：使用 Docker

#### 基本用法

```bash
# 拉取镜像
docker pull cxbdasheng/dnet:latest

# 运行容器（推荐方式）
docker run -d \
  --name dnet \
  -p 9877:9877 \
  -v $(pwd)/config:/root \
  --restart unless-stopped \
  cxbdasheng/dnet:latest

# 访问 Web 界面
# 在浏览器中打开 http://localhost:9877
```

#### 使用 GitHub 容器镜像

如果 Docker Hub 访问不畅，可以使用 GitHub Container Registry：

```bash
docker pull ghcr.io/cxbdasheng/dnet:latest

docker run -d \
  --name dnet \
  --restart=always \
  --net=host \
  -v /opt/dnet:/root \
  ghcr.io/cxbdasheng/dnet:latest
```

#### Docker 高级选项

**使用 host 网络模式：**

```bash
docker run -d \
  --name dnet \
  --restart=always \
  --net=host \
  -v /opt/dnet:/root \
  cxbdasheng/dnet:latest
```

**自定义参数启动：**

```bash
# 自定义监听地址和同步间隔
docker run -d \
  --name dnet \
  --restart=always \
  --net=host \
  -v /opt/dnet:/root \
  cxbdasheng/dnet:latest \
  -l :9877 -f 600
```

**重置密码：**

```bash
docker exec dnet ./dnet -resetPassword 123456
docker restart dnet
```
## 使用文档

### 命令行参数

```bash
./dnet [选项]

选项：
  -l string
        监听地址（默认 ":9877"）
  -c string
        配置文件路径（默认 "~/.dnet_config.yaml"）
  -f int
        更新频率，单位秒（默认 300）
  -s string
        服务管理（install|uninstall|restart）
  -dns string
        自定义 DNS 服务器地址，例如：8.8.8.8
  -noweb
        禁用 Web 服务
  -resetPassword string
        重置密码
  -dcdnCacheTimes int
        DCDN 缓存次数（默认 5）
```
### Webhook 通知配置

D-NET 支持 Webhook 通知功能。当域名更新成功或失败时，会向配置的 URL 发送通知。

#### 支持的变量

在 Webhook URL 或 RequestBody 中可以使用以下变量：

| 变量名 | 说明 | 示例值 |
|--------|------|--------|
| `#{serviceType}` | 服务类型 | DCDN、DDNS |
| `#{serviceName}` | 服务名称 | www.example.com |
| `#{serviceStatus}` | 更新结果 | 未改变、失败、成功 |

#### 请求方式

- 如果 RequestBody 为空，则发送 GET 请求
- 如果 RequestBody 不为空，则发送 POST 请求

#### 配置示例

<details>
<summary>Server酱</summary>

直接在 URL 中使用变量：

```
https://sctapi.ftqq.com/[SendKey].send?title=DNET通知&desp=服务：#{serviceName}，服务类型：#{serviceType}，结果：#{serviceStatus}
```

</details>

<details>
<summary>钉钉群机器人</summary>

**配置步骤：**

1. 钉钉电脑端 → 群设置 → 智能群助手 → 添加机器人 → 自定义
2. 只勾选 `自定义关键词`，输入关键词（必须包含在 RequestBody 的 content 中），例如：`DNET 通知`
3. 在 D-NET 的 Webhook URL 中输入钉钉提供的 Webhook 地址
4. 在 RequestBody 中输入以下内容：

```json
{
  "msgtype": "markdown",
  "markdown": {
    "title": "DNET 通知",
    "text": "您的服务：#{serviceName}，服务类型：#{serviceType}，结果：#{serviceStatus}"
  }
}
```

</details>

## 从源码构建
```bash
# 或使用 Make（如果可用）
make build
# 构建所有平台（使用 GoReleaser）
goreleaser build --snapshot --clean
# 运行测试
go test ./...
# 直接运行
go run main.go
```
## 贡献指南
如果您想为 D-NET 贡献代码、报告问题或提出建议，请阅读我们的 [贡献指南](CONTRIBUTING.md)。

## 许可证
本项目采用 [MIT](LICENSE) 许可证

## 致谢

感谢以下开源项目为 D-NET 提供支持：

- [kardianos/service](https://github.com/kardianos/service) - 跨平台系统服务管理
- [go-yaml/yaml](https://github.com/go-yaml/yaml) - YAML 配置文件解析
- [GoReleaser](https://goreleaser.com/) - 自动化构建和发布工具
