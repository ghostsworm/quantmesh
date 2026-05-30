package mcp

import (
	"context"

	"quantmesh/backtest"
	"quantmesh/storage"
)

// Providers 在 main 启动时填好后传给 Register*Tools 系列函数。
// 任何字段都允许为 nil —— 对应那一组工具就跳过不注册。
//
// 设计取舍：直接传 storage.Storage 让工具就近写 SQL，比起每个能力都包一层
// 接口要简单很多；mcp 包反正只用 storage 的查询方法，没有循环依赖隐患。
type Providers struct {
	// Version quantmesh 版本号，用于 initialize 响应
	Version string

	// Storage 主存储（SQLite/MySQL/PG）；nil 则禁用绝大多数只读工具
	Storage storage.Storage

	// LogStorage 日志专用库
	LogStorage *storage.LogStorage

	// BacktestTasks 回测任务存储；nil 时不注册回测工具
	BacktestTasks backtest.TaskStore

	// SystemSettings 取/改 system_settings 表（aipipe key 之类）
	SystemSettings SystemSettingsReader

	// 写工具依赖项 —— 只在 allow_write=true 时才需要
	BotControl BotController
}

// SystemSettingsReader 读 system_settings 的最小接口（避免拽 web 包）。
type SystemSettingsReader interface {
	GetSystemSetting(ctx context.Context, key string) (*storage.SystemSetting, error)
	GetSystemSettings(ctx context.Context, filter *storage.SystemSettingFilter) ([]*storage.SystemSetting, error)
	SetSystemSettingString(ctx context.Context, key, value string) error
	SetSystemSettingBool(ctx context.Context, key string, value bool) error
}

// BotController 写工具用：启停 bot。main 包注入实现（包装 bot_manager）。
type BotController interface {
	ListBotIDs() []string
	EnableBot(botID, reason string) error
	DisableBot(botID, reason string) error
}
