# GitHub 自动化

## 检查与发布

| 工作流 | 触发方式 | 检查内容 |
| --- | --- | --- |
| `test.yml` | 向 main 推送、目标为 main 的 PR | 三系统 Go 测试、Linux race、vet、跨平台 snapshot 与校验和；调用以下三项检查 |
| `quality.yml` | Test / Release 调用，或手动运行 | `gofmt`、`go mod tidy -diff`、`go mod verify`、actionlint（含 ShellCheck） |
| `security.yml` | Test / Release 调用、手动运行、每周一 02:23 UTC | `govulncheck ./...`；可达漏洞使任务失败 |
| `docker-test.yml` | Test / Release 调用，或手动运行 | 两个 Dockerfile 的 Linux amd64 镜像构建及容器冒烟检查，不推送镜像 |
| `release.yml` | 推送 `v*` 标签 | 发布前测试、snapshot 校验、质量 / 漏洞 / Docker 检查全部成功后，发布 Release 和镜像 |
| `dockerhub-description.yml` | main 的 README 或该工作流变化、手动运行 | 同步 Docker Hub 描述 |

检查任务仅需 `contents: read`，无需 Docker Hub 或其他云服务密钥。定时漏洞检查可以在没有新提交时发现新披露的问题。工具版本固定在工作流中；更新 actionlint / govulncheck 版本后应重新验证工作流。

`govulncheck` 当前在 Linux 执行，依据选定 Go 工具链和扫描时的漏洞数据库判断；无可达漏洞不代表所有平台和所有依赖都没有漏洞。修复标准库漏洞时应同时更新 `go.mod` 的最低 Go 版本、`Dockerfile_build` 的构建镜像和 README 版本提示。

## 容器检查覆盖范围

测试使用独立 Docker 网络、本地 echo 后端和临时配置挂载：

- Web 登录页能响应，初始账号可建立。
- 同一个数字端口同时提供 TCP / UDP，分别经过 Docker 端口映射并验证回包。
- 两个 UDP 客户端的回包独立，数据报边界保持。
- 配置实际写入挂载目录，重启后账号及转发规则仍可使用。
- TCP 目标测试成功，UDP 返回不支持通用探测；健康检查命令可执行。
- 成功或失败均清理测试容器和网络，失败输出容器日志。

此检查覆盖 IPv4 单机转发，不替代现有 Go IPv6、白名单、限流和回滚测试；也不验证全部发布架构的容器运行情况。

本地运行需 Go、Python 3 和已启动的 Docker。以 Linux amd64 为例（其他宿主机需相应的仿真支持）：

```bash
# 源码镜像
docker buildx build --load --platform linux/amd64 \
  --build-arg VERSION=ci -f Dockerfile_build -t dnet-smoke:ci .

# 测试后端与冒烟检查
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -o /tmp/dnet-smoke-backend scripts/ci/echo-backend.go
python3 scripts/ci/docker-smoke.py dnet-smoke:ci /tmp/dnet-smoke-backend
```

工作流文件本身不会设置仓库分支保护。需要阻止检查失败的 PR 合并时，应在 GitHub ruleset 中将相关检查设为必需；不要仅依赖跨平台打包任务的结果。
