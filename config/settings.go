package config

type Settings struct {
	// 启动端口
	Port              string
	NotAllowWanAccess bool
	// 同步间隔（秒），0 表示未配置，由 CLI 或默认值决定
	Every int
}
