.PHONY: all build build-linux build-darwin build-windows build-all \
        docker docker-build docker-buildx docker-push \
        test test-race test-coverage \
        clean clean-all install run fmt lint vet help deps version

# ==================================================================================== #
# 变量定义
# ==================================================================================== #

# 项目信息
PROJECT_NAME=dnet
BIN_NAME=dnet
DOCKER_IMAGE=dnet

# 版本信息（如果找不到 tag 则使用 HEAD commit）
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")


# Go 构建配置
GO=go
GOENV=CGO_ENABLED=0
GOFLAGS=-trimpath
LDFLAGS=-ldflags="-s -w -X 'main.version=$(VERSION)' -extldflags '-static'"

# 目录配置
DIR_SRC=.
DIR_DIST=./dist
DIR_BIN=.

# Docker 配置
DOCKER_ENV=DOCKER_BUILDKIT=1
DOCKER=$(DOCKER_ENV) docker
DOCKERFILE_BUILD=Dockerfile_build
DOCKER_PLATFORMS=linux/amd64,linux/arm64,linux/arm/v7,linux/riscv64
DOCKER_OCI_ARCHIVE=$(DIR_DIST)/$(BIN_NAME)-$(VERSION).oci.tar

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
	@GOOS=linux GOARCH=riscv64 $(GOENV) $(GO) build $(GOFLAGS) $(LDFLAGS) -o $(DIR_DIST)/$(BIN_NAME)-linux-riscv64 $(DIR_SRC)
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

## docker: 构建本地 Docker 镜像

docker: docker-build

## docker-build: 使用容器内 Go 工具链构建本地 Docker 镜像
docker-build:
	@echo "正在构建本地 Docker 镜像: $(DOCKER_IMAGE):$(VERSION)..."
	@$(DOCKER) build \
		--build-arg VERSION=$(VERSION) \
		-f $(DOCKERFILE_BUILD) \
		-t $(DOCKER_IMAGE):$(VERSION) .
	@echo "Docker 镜像构建完成: $(DOCKER_IMAGE):$(VERSION)"

## docker-buildx: 构建四平台 OCI archive（不会推送）
docker-buildx:
	@platforms="$$($(DOCKER) buildx inspect --bootstrap 2>/dev/null | sed -n 's/^Platforms:[[:space:]]*//p' | tr ',' ' ')"; \
	missing=""; \
	for platform in $$(printf '%s' '$(DOCKER_PLATFORMS)' | tr ',' ' '); do \
		case " $$platforms " in *" $$platform "*) ;; *) missing="$$missing $$platform" ;; esac; \
	done; \
	if [ -n "$$missing" ]; then \
		echo "错误: 当前 Buildx builder 不支持平台:$$missing" >&2; \
		echo "请启用 Docker Desktop 的 QEMU/binfmt 仿真，或选择支持这些平台的 Buildx builder 后重试。" >&2; \
		exit 1; \
	fi
	@echo "正在构建多平台 OCI archive: $(DOCKER_OCI_ARCHIVE)..."
	@mkdir -p $(DIR_DIST)
	@$(DOCKER) buildx build \
		--platform $(DOCKER_PLATFORMS) \
		--build-arg VERSION=$(VERSION) \
		--output type=oci,dest=$(DOCKER_OCI_ARCHIVE) \
		-f $(DOCKERFILE_BUILD) .
	@echo "多平台 OCI archive 构建完成: $(DOCKER_OCI_ARCHIVE)"

## docker-push: 推送本地镜像（必须显式指定 PUSH_IMAGE）
docker-push:
	@test -n "$(PUSH_IMAGE)" || (echo "错误: 必须显式指定 PUSH_IMAGE，例如 make docker-push PUSH_IMAGE=example.com/user/dnet" >&2; exit 1)
	@echo "正在推送 Docker 镜像: $(PUSH_IMAGE):$(VERSION)..."
	@$(DOCKER) image inspect $(DOCKER_IMAGE):$(VERSION) >/dev/null
	@$(DOCKER) tag $(DOCKER_IMAGE):$(VERSION) $(PUSH_IMAGE):$(VERSION)
	@$(DOCKER) push $(PUSH_IMAGE):$(VERSION)
	@echo "Docker 镜像推送完成: $(PUSH_IMAGE):$(VERSION)"


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

## clean: 清理当前目录的 dnet 二进制
clean:
	@echo "正在清理 Go 缓存和 dnet 二进制..."
	@$(GO) clean ./...
	@rm -f ./dnet
	@echo "清理完成"

## clean-all: 清理所有构建产物（包括固定的 ./dist）
clean-all: clean
	@echo "正在清理所有构建产物..."
	@rm -rf ./dist
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
