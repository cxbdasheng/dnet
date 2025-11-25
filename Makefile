.PHONY: all build build-linux build-darwin build-windows build-all \
        docker docker-build docker-push docker-compose-up docker-compose-down \
        test test-race test-coverage \
        clean clean-all install fmt lint help

# ==================================================================================== #
# 变量定义
# ==================================================================================== #

# 项目信息
PROJECT_NAME=dnet
BIN_NAME=dnet
DOCKER_IMAGE=cxbdasheng/dnet
DOCKER_REGISTRY=docker.io

# 版本信息（如果找不到 tag 则使用 HEAD commit）
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")


# Go 构建配置
GO=go
GOENV=CGO_ENABLED=0
GOFLAGS=-trimpath
LDFLAGS=-ldflags="-s -w \
	-X 'main.version=$(VERSION)' \
	-X 'main.buildTime=$(BUILD_TIME)' \
	-X 'main.gitCommit=$(GIT_COMMIT)' \
	-extldflags '-static'"

# 目录配置
DIR_SRC=.
DIR_DIST=./dist
DIR_BIN=.

# Docker 配置
DOCKER_ENV=DOCKER_BUILDKIT=1
DOCKER=$(DOCKER_ENV) docker
DOCKERFILE=Dockerfile

# 平台配置
PLATFORMS=linux/amd64 linux/arm64 linux/arm/v7 darwin/amd64 darwin/arm64 windows/amd64

# ==================================================================================== #
# 开发任务
# ==================================================================================== #

## help: 显示帮助信息
help:
	@echo '使用方法:'
	@echo '  make <target>'
	@echo ''
	@echo '可用目标:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

## all: 运行 fmt、lint 和 build
all: fmt lint build

## build: 构建当前平台的二进制文件
build:
	@echo "正在构建 $(PROJECT_NAME) $(VERSION)..."
	@mkdir -p $(DIR_BIN)
	@$(GOENV) $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIR_BIN)/$(BIN_NAME) $(DIR_SRC)
	@echo "构建完成: $(DIR_BIN)/$(BIN_NAME)"

## build-linux: 构建 Linux 平台二进制文件
build-linux:
	@echo "正在构建 Linux 版本..."
	@mkdir -p $(DIR_DIST)
	@GOOS=linux GOARCH=amd64 $(GOENV) $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIR_DIST)/$(BIN_NAME)-linux-amd64 $(DIR_SRC)
	@GOOS=linux GOARCH=arm64 $(GOENV) $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIR_DIST)/$(BIN_NAME)-linux-arm64 $(DIR_SRC)
	@GOOS=linux GOARCH=arm GOARM=7 $(GOENV) $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIR_DIST)/$(BIN_NAME)-linux-armv7 $(DIR_SRC)
	@echo "Linux 版本构建完成"

## build-darwin: 构建 macOS 平台二进制文件
build-darwin:
	@echo "正在构建 macOS 版本..."
	@mkdir -p $(DIR_DIST)
	@GOOS=darwin GOARCH=amd64 $(GOENV) $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIR_DIST)/$(BIN_NAME)-darwin-amd64 $(DIR_SRC)
	@GOOS=darwin GOARCH=arm64 $(GOENV) $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIR_DIST)/$(BIN_NAME)-darwin-arm64 $(DIR_SRC)
	@echo "macOS 版本构建完成"

## build-windows: 构建 Windows 平台二进制文件
build-windows:
	@echo "正在构建 Windows 版本..."
	@mkdir -p $(DIR_DIST)
	@GOOS=windows GOARCH=amd64 $(GOENV) $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIR_DIST)/$(BIN_NAME)-windows-amd64.exe $(DIR_SRC)
	@echo "Windows 版本构建完成"

## build-all: 构建所有平台的二进制文件
build-all: build-linux build-darwin build-windows
	@echo "所有平台构建完成，输出目录: $(DIR_DIST)"

## install: 安装到 $GOPATH/bin
install:
	@echo "正在安装 $(PROJECT_NAME) 到 GOPATH/bin..."
	@$(GOENV) $(GO) install $(GOFLAGS) $(LDFLAGS) $(DIR_SRC)
	@echo "安装完成"

## run: 运行程序
run: build
	@echo "正在运行 $(PROJECT_NAME)..."
	@$(DIR_BIN)/$(BIN_NAME)

# ==================================================================================== #
# Docker 任务
# ==================================================================================== #

## docker: 构建并推送 Docker 镜像（需要先 build）
docker: docker-build docker-push

## docker-build: 构建 Docker 镜像
docker-build: build
	@echo "正在构建 Docker 镜像: $(DOCKER_IMAGE):$(VERSION)..."
	@$(DOCKER) build -f $(DOCKERFILE) -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest .
	@echo "Docker 镜像构建完成"

## docker-push: 推送 Docker 镜像到仓库
docker-push:
	@echo "正在推送 Docker 镜像..."
	@$(DOCKER) push $(DOCKER_IMAGE):$(VERSION)
	@$(DOCKER) push $(DOCKER_IMAGE):latest
	@echo "Docker 镜像推送完成"

## docker-buildx: 使用 buildx 构建多平台 Docker 镜像
docker-buildx: build
	@echo "正在构建多平台 Docker 镜像..."
	@$(DOCKER) buildx build \
		--platform linux/amd64,linux/arm64,linux/arm/v7 \
		-t $(DOCKER_IMAGE):$(VERSION) \
		-t $(DOCKER_IMAGE):latest \
		--push \
		-f $(DOCKERFILE) .
	@echo "多平台 Docker 镜像构建并推送完成"


# ==================================================================================== #
# 测试任务
# ==================================================================================== #

## test: 运行所有测试
test:
	@echo "正在运行测试..."
	@$(GO) test -v ./...

## test-race: 运行竞态检测测试
test-race:
	@echo "正在运行竞态检测测试..."
	@$(GO) test -race -v ./...

## test-coverage: 运行测试并生成覆盖率报告
test-coverage:
	@echo "正在生成测试覆盖率报告..."
	@mkdir -p $(DIR_DIST)
	@$(GO) test -coverprofile=$(DIR_DIST)/coverage.out ./...
	@$(GO) tool cover -html=$(DIR_DIST)/coverage.out -o $(DIR_DIST)/coverage.html
	@echo "覆盖率报告已生成: $(DIR_DIST)/coverage.html"

# ==================================================================================== #
# 质量检查任务
# ==================================================================================== #

## fmt: 格式化代码
fmt:
	@echo "正在格式化代码..."
	@$(GO) fmt ./...
	@echo "代码格式化完成"

## lint: 运行代码检查（需要安装 golangci-lint）
lint:
	@echo "正在运行代码检查..."
	@which golangci-lint > /dev/null || (echo "请先安装 golangci-lint: https://golangci-lint.run/usage/install/" && exit 1)
	@golangci-lint run ./...
	@echo "代码检查完成"

## vet: 运行 go vet
vet:
	@echo "正在运行 go vet..."
	@$(GO) vet ./...
	@echo "go vet 完成"

# ==================================================================================== #
# 清理任务
# ==================================================================================== #

## clean: 清理构建产物
clean:
	@echo "正在清理构建产物..."
	@$(GO) clean ./...
	@rm -rf $(DIR_BIN)
	@echo "清理完成"

## clean-all: 清理所有构建产物（包括 dist）
clean-all: clean
	@echo "正在清理所有构建产物..."
	@rm -rf $(DIR_DIST)
	@echo "清理完成"

# ==================================================================================== #
# 其他任务
# ==================================================================================== #

## deps: 下载依赖
deps:
	@echo "正在下载依赖..."
	@$(GO) mod download
	@$(GO) mod tidy
	@echo "依赖下载完成"

## version: 显示版本信息
version:
	@echo "版本: $(VERSION)"
	@echo "提交: $(GIT_COMMIT)"
	@echo "构建时间: $(BUILD_TIME)"
