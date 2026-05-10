# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 常用命令

```bash
# 构建
make build          # 构建当前平台的二进制文件
make build-all      # 构建所有平台（Linux、macOS、Windows），输出至 ./dist/

# 测试
make test           # 运行所有测试（详细输出）
make test-race      # 启用竞态检测运行测试
make test-coverage  # 生成 HTML 覆盖率报告至 ./dist/coverage.html

# 单包测试
go test -v ./dcdn/...
go test -v -run TestFunctionName ./config/...

# 代码质量
make fmt            # 格式化所有代码
make vet            # 运行 go vet
make lint           # 运行 golangci-lint（需单独安装）

# 运行
make run            # 构建并运行（默认监听 :9877）
./dnet -l :9877 -f 300 -c ~/.dnet_config.yaml

# 其他 CLI 标志
./dnet -v                          # 显示版本
./dnet -u                          # 自动更新到最新版本（从 GitHub Releases）
./dnet -s install|uninstall|restart  # 系统服务管理（需 sudo）
./dnet -resetPassword <新密码>     # 重置 Web UI 密码
./dnet -noweb                      # 禁用 Web 服务，仅运行同步逻辑
./dnet -dns 8.8.8.8                # 指定自定义 DNS 服务器
./dnet -dcdnCacheTimes 5           # DCDN 强制更新计数器初始值（也可通过 DCDN_CACHE_TIMES 环境变量设置）
./dnet -ddnsCacheTimes 5           # DDNS 强制更新计数器初始值（也可通过 DDNS_CACHE_TIMES 环境变量设置）
```

## 架构

**D-NET** 是一个动态网络管理守护进程，定期将 CDN 源站 IP 和 DNS 记录同步到各云服务商。以跨平台服务（systemd/launchd/Windows Service）方式运行，内置 Web 管理界面。

### 核心流程

1. `main.go` — 解析命令行参数，初始化 Web 服务器，在 goroutine 中启动 `bootstrap.RunTimer`
2. `bootstrap/bootstrap.go` — 每隔 `-f` 秒调用一次 `RunOnce()`：清除全局 IP 缓存，依次调用 `ProcessDCDNServices` 和 `ProcessDDNSServices`
3. 各服务遍历已配置条目，选择对应的服务商实现，调用 `Init()` 后再调用 `UpdateOrCreateSources()` / `UpdateOrCreateRecords()`
4. `config/config.go` — YAML 配置通过互斥锁缓存加载；CNAME 值发生变化时会将配置写回文件

### 包结构

| 包 | 用途 |
|---|---|
| `bootstrap` | 定时循环，分发至 dcdn/ddns 处理器 |
| `config` | YAML 配置读写、用户认证（SHA256）、CDN/DNS/Webhook 结构体 |
| `dcdn` | 动态 CDN 源站管理。`CDN` 接口 + `BaseProvider` 基础结构体；支持：阿里云、腾讯云、百度云、Cloudflare（又拍云已实现但暂未启用） |
| `ddns` | 动态 DNS 记录管理。`DNS` 接口 + `BaseDNSProvider` 基础结构体；支持：阿里云、腾讯云、百度云、Cloudflare、华为云、DNSPod |
| `helper/update` | 从 GitHub Releases 自动下载并替换可执行文件（`-u` 标志触发） |
| `helper` | 日志（`helper.Info/Warn/Error/Debug`）、IP 探测（URL/网卡/命令）、HTTP 客户端、DNS 解析器 |
| `signer` | 云服务商 API 请求签名（阿里云、腾讯云、百度云、华为云） |
| `web` | 内嵌 Web UI 的 HTTP 处理器（首页、dcdn、ddns、设置、日志、webhook、登录/登出） |

### 新增 CDN/DNS 服务商

**DCDN：** 在 `dcdn/` 中新建文件，定义嵌入 `BaseProvider` 的结构体，实现 `CDN` 接口（`Init`、`UpdateOrCreateSources`、`ShouldSendWebhook`、`GetServiceStatus`、`GetServiceName`、`ConfigChanged`），并在 `bootstrap/bootstrap.go:ProcessDCDNServices` 的 switch 中添加对应 case。

**DDNS：** 同上，在 `ddns/` 中实现 `DNS` 接口（`Init`、`UpdateOrCreateRecords`、`GetServiceName`），添加服务商常量，并在 `bootstrap/bootstrap.go:ProcessDDNSServices` 的 switch 中添加对应 case。

### 缓存与更新逻辑

- 每个配置条目对应一个 `Cache` 结构体，记录：距下次强制同步的剩余计数（`Times`，默认 5）、连续失败次数，以及动态源站到已解析 IP 的映射。
- 每次触发：IP 有变化 → 立即更新；无变化但 `Times` 归零 → 强制更新并重置计数器；成功时或连续失败 3 次后触发 Webhook。
- `ForceCompareGlobal` 标志（`dcdn.ForceCompareGlobal`、`ddns.ForceCompareGlobal`）会在下次运行时重置所有缓存（Web UI 保存配置后置为 `true`）。

### 配置文件

默认路径：`~/.dnet_config.yaml`，可通过 `-c` 覆盖。Web UI 通过 config 包读写该文件。`config.GetConfigCached()` 使用文件修改时间判断是否需要重新解析。

### 提交规范

使用语义化前缀：`feat`、`fix`、`docs`、`refactor`、`test`、`chore`。PR 标题应简洁（不超过 70 个字符）。
