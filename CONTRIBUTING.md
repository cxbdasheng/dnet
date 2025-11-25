# 贡献指南

感谢您对 D-NET 项目的关注！我们欢迎任何形式的贡献，包括但不限于：

- 报告 Bug
- 提出新功能建议
- 改进文档
- 提交代码修复或新功能

## 行为准则

请注意，参与此项目即表示您同意遵守我们的行为准则。请友善、尊重地对待所有贡献者。

## 如何贡献

### 报告 Bug

如果您发现了 Bug，请：

1. 先在 [Issues](https://github.com/cxbdasheng/dnet/issues) 中搜索，确认问题尚未被报告
2. 创建新的 Issue，包含以下信息：
   - 清晰的标题和描述
   - 复现步骤
   - 预期行为和实际行为
   - 系统环境信息（操作系统、Go 版本等）
   - 相关日志或截图

### 提出功能建议

如果您有新功能的想法：

1. 先在 [Issues](https://github.com/cxbdasheng/dnet/issues) 中搜索，确认建议尚未被提出
2. 创建新的 Issue，详细描述：
   - 功能的使用场景
   - 预期的实现方式
   - 为什么这个功能对项目有价值

### 提交代码

#### 开发环境设置

1. **Fork 项目**

   点击页面右上角的 Fork 按钮，将项目 Fork 到您的 GitHub 账号。

2. **克隆仓库**

   ```bash
   git clone https://github.com/YOUR_USERNAME/dnet.git
   cd dnet
   ```

3. **安装依赖**

   ```bash
   go mod download
   ```

4. **创建分支**

   ```bash
   git checkout -b feature/your-feature-name
   # 或
   git checkout -b fix/your-bug-fix
   ```

####  项目结构

```
dnet/
├── main.go              # 程序入口
├── config/              # 配置管理模块
│   ├── config.go        # 配置结构定义
│   ├── cdn.go           # CDN 配置
│   ├── webhook.go       # Webhook 配置
│   └── user.go          # 用户配置
├── dcdn/                # DCDN 服务实现
│   ├── dcdn.go          # 通用接口定义
│   ├── aliyun.go        # 阿里云 CDN 实现
│   └── baidu.go         # 百度云 CDN 实现
├── signer/              # 签名工具
├── web/                 # Web 界面和 API
├── helper/              # 工具函数
├── bootstrap/           # 应用启动器
├── static/              # 静态资源文件
├── .goreleaser.yaml     # GoReleaser 配置
├── Dockerfile           # Docker 镜像构建文件
├── go.mod               # Go 模块定义
└── go.sum               # 依赖版本锁定
```
#### 开发流程

1. **编写代码**
   - 遵循项目的代码风格（见下文）
   - 确保代码通过所有测试
   - 为新功能添加测试

2. **运行测试**

   ```bash
   # 运行所有测试
   go test ./...

   # 运行特定包的测试
   go test ./config

   # 查看测试覆盖率
   go test -cover ./...
   ```

3. **格式化代码**

   ```bash
   # 格式化所有代码
   go fmt ./...

   # 或使用 gofmt
   gofmt -s -w .
   ```

4. **代码检查**

   ```bash
   # 使用 go vet 检查常见错误
   go vet ./...

   # 推荐使用 golangci-lint（如果已安装）
   golangci-lint run
   ```

#### 提交更改

1. **提交代码**

   ```bash
   git add .
   git commit -m "feat: add amazing feature"
   ```

   提交信息格式请参考下文的"提交信息规范"。

2. **推送到 GitHub**

   ```bash
   git push origin feature/your-feature-name
   ```

3. **创建 Pull Request**
   - 在 GitHub 上打开您的 Fork
   - 点击 "New Pull Request"
   - 填写 PR 描述，说明：
     - 这个 PR 解决了什么问题
     - 采用了什么方案
     - 是否有破坏性变更
     - 相关的 Issue 编号（如果有）

## 代码规范

### Go 代码风格

- 遵循 [Effective Go](https://go.dev/doc/effective_go) 规范
- 使用 `gofmt` 格式化代码
- 变量和函数命名使用驼峰式（camelCase 或 PascalCase）
- 导出的函数、类型、常量使用大写字母开头
- 为导出的函数和类型添加注释

### 提交信息规范

使用语义化的提交信息格式：

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Type 类型：**

- `feat`: 新功能
- `fix`: Bug 修复
- `docs`: 文档更新
- `style`: 代码格式调整（不影响代码运行）
- `refactor`: 重构（既不是新功能也不是 Bug 修复）
- `perf`: 性能优化
- `test`: 测试相关
- `chore`: 构建过程或辅助工具的变动

**Scope 范围（可选）：**

- `config`: 配置相关
- `dcdn`: DCDN 功能
- `web`: Web 界面
- `docker`: Docker 相关

**示例：**

```
feat(dcdn): 添加腾讯云 CDN 支持

- 实现腾讯云 CDN API 调用
- 添加配置项和文档
- 增加单元测试

Closes #123
```

### 测试规范

- 为新功能编写单元测试
- 测试文件命名为 `*_test.go`
- 测试函数命名为 `TestXxx`
- 保持测试覆盖率在合理范围内
- 测试应该是独立的，不依赖特定的执行顺序

**示例：**

```go
func TestConfigLoad(t *testing.T) {
    config, err := LoadConfig("testdata/config.yaml")
    if err != nil {
        t.Fatalf("Failed to load config: %v", err)
    }

    if config.User.Username != "admin" {
        t.Errorf("Expected username 'admin', got '%s'", config.User.Username)
    }
}
```

### 文档规范

- 为新功能更新 README.md
- 为复杂的功能添加详细的注释
- 更新相关的配置示例
- 如果是破坏性变更，需要在文档中明确说明

## Pull Request 检查清单

在提交 Pull Request 之前，请确认：

- [ ] 代码已通过 `go fmt` 格式化
- [ ] 代码已通过 `go vet` 检查
- [ ] 所有测试都通过（`go test ./...`）
- [ ] 添加了必要的测试用例
- [ ] 更新了相关文档
- [ ] 提交信息符合规范
- [ ] PR 描述清晰，说明了改动的原因和内容

## 代码审查

所有的 Pull Request 都需要经过代码审查。审查者可能会：

- 提出修改建议
- 要求补充测试
- 要求改进代码风格
- 讨论实现方案

请耐心等待审查，并及时响应审查意见。

## 发布流程

项目维护者会定期发布新版本：

1. 更新版本号
2. 生成 Changelog
3. 创建 Git Tag
4. 使用 GoReleaser 构建和发布

## 获取帮助

如果您在贡献过程中遇到问题：

- 查看 [Issues](https://github.com/cxbdasheng/dnet/issues) 中的讨论
- 创建新的 Issue 寻求帮助
- 参考项目文档和代码示例

## 许可证

提交代码即表示您同意将您的贡献按照 [MIT 许可证](LICENSE) 进行授权。

---

再次感谢您的贡献！每一个贡献都让 D-NET 变得更好。
