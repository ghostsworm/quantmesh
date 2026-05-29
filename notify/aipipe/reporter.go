// Package aipipe 把 quantmesh 的运行时错误异步上报到 17push 平台。
//
// 设计要点：
//   - 用户没填 API Key → 全程 no-op，不产生任何网络/磁盘开销
//   - 配置热重载：UI 改完 key/endpoint/enabled 调用 Reload 即可
//   - 上报路径自身的失败不打 ERROR 日志，避免「ERROR → 上报失败 → 又是 ERROR」死循环
//   - 进程退出前调用 Close，把队列里没发完的落盘
package aipipe

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "github.com/redacted/aipipe-go-sdk"
)

const (
	// 三个 system_settings key（与前端、文档约定一致）
	SettingKeyAPIKey   = "aipipe_api_key"
	SettingKeyEndpoint = "aipipe_endpoint"
	SettingKeyEnabled  = "aipipe_enabled"

	DefaultEndpoint = "https://17push.com/api/v1"
)

// Config 加载自 system_settings；为空 / Enabled=false 时 reporter 是 no-op。
type Config struct {
	APIKey    string
	Endpoint  string
	Enabled   bool
	AgentName string // 默认空（SDK 用 hostname）；若有 instance id 可显式传
}

var (
	mu        sync.RWMutex
	current   *sdk.Client
	currentCf Config
	suppress  atomic.Bool // 上报内部错误时置 true，防止 logger hook 再次入队
)

// Reload 用最新配置替换 reporter；旧的会被 Close。
//
// 调用方：web 设置保存后；启动时初始化时。
// 返回 error 只代表"按这份配置启动 SDK 失败"，不会 panic。
func Reload(cfg Config) error {
	cfg.Endpoint = normalizeEndpoint(cfg.Endpoint)

	mu.Lock()
	defer mu.Unlock()

	if sameConfigLocked(cfg) && current != nil {
		return nil
	}

	old := current
	current = nil
	currentCf = Config{}

	if !cfg.Enabled || cfg.APIKey == "" {
		closeAsync(old)
		return nil
	}

	c, err := sdk.New(sdk.Config{
		APIKey:        cfg.APIKey,
		Endpoint:      cfg.Endpoint,
		Source:        "quantmesh",
		Format:        "go",
		BatchSize:     20,
		BatchInterval: 2 * time.Second,
		MaxRetries:    3,
		ChannelCap:    256,
		MaxPending:    10000,
		AgentName:     cfg.AgentName,
	})
	if err != nil {
		closeAsync(old)
		return err
	}

	current = c
	currentCf = cfg
	closeAsync(old)
	return nil
}

// Disable 主动关闭（用户关 enabled、或进程退出）。
func Disable() {
	mu.Lock()
	defer mu.Unlock()
	old := current
	current = nil
	currentCf = Config{}
	closeAsync(old)
}

// Close 进程退出钩子，把内存里没发完的落盘。
func Close() error {
	mu.Lock()
	c := current
	current = nil
	currentCf = Config{}
	mu.Unlock()
	if c == nil {
		return nil
	}
	return c.Close()
}

// IsEnabled 给上层 hook 做快路径判断。
func IsEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current != nil
}

// ReportError 主入口。err 为空、reporter 未启用、或正处于上报内部错误的"静默期"，都直接 return。
//
// topic 用来在 17push 后台分组，可空：
//   - "panic"     —— recover 捕获
//   - "log"       —— logger ERROR/FATAL
//   - "http5xx"   —— gin 5xx
//   - "strategy"  —— 策略/下单失败
func ReportError(err error, topic string, extra string) {
	if err == nil {
		return
	}
	if suppress.Load() {
		return
	}
	mu.RLock()
	c := current
	mu.RUnlock()
	if c == nil {
		return
	}

	opts := []sdk.LogOption{}
	if topic != "" {
		opts = append(opts, sdk.WithTopic(topic))
	}
	if extra != "" {
		opts = append(opts, sdk.WithExtra(extra))
	}
	c.LogError(err, opts...)
}

// ReportMessage 字符串入口（logger hook 用，message 已经格式化好了）。
func ReportMessage(level, message, topic string) {
	if message == "" {
		return
	}
	// 上报自己产生的日志不应反过来再次上报
	if strings.Contains(message, "aipipe:") || strings.Contains(message, "[aipipe]") {
		return
	}
	ReportError(errors.New(message), topic, "level="+level)
}

// PanicGuard recover 包装：在 goroutine 起手 `defer aipipe.PanicGuard("worker-name")` 即可。
// 上报后会重新 panic，保留原有崩溃语义。
func PanicGuard(where string) {
	r := recover()
	if r == nil {
		return
	}
	msg := fmt.Sprintf("panic in %s: %v", where, r)
	ReportError(errors.New(msg), "panic", "")
	panic(r)
}

// PanicGuardNoRethrow 同上，但吞掉 panic。仅用于"后台无关紧要"的 goroutine。
func PanicGuardNoRethrow(where string) {
	r := recover()
	if r == nil {
		return
	}
	msg := fmt.Sprintf("panic in %s: %v", where, r)
	ReportError(errors.New(msg), "panic", "")
}

// TestConfig 在不替换全局 reporter 的情况下做一次连通性检测。
// 用于设置页的"测试连接"按钮。
func TestConfig(cfg Config) error {
	cfg.Endpoint = normalizeEndpoint(cfg.Endpoint)
	if cfg.APIKey == "" {
		return errors.New("API Key 为空")
	}
	c, err := sdk.New(sdk.Config{
		APIKey:        cfg.APIKey,
		Endpoint:      cfg.Endpoint,
		Source:        "quantmesh",
		Format:        "go",
		BatchSize:     1,
		BatchInterval: time.Second,
		MaxRetries:    0,
		ChannelCap:    4,
		MaxPending:    16,
		AgentName:     cfg.AgentName,
	})
	if err != nil {
		return err
	}
	defer c.Close()
	c.LogError(errors.New("quantmesh aipipe connectivity test"),
		sdk.WithTopic("test"),
		sdk.WithLevel("INFO"),
	)
	c.Flush()
	// SDK 是异步的，连通性问题会在 Close 时同步上报。给点时间。
	time.Sleep(800 * time.Millisecond)
	return nil
}

// WithSuppress 在 fn 执行期间禁用上报。用于上报失败回写日志这类内部路径。
func WithSuppress(fn func()) {
	suppress.Store(true)
	defer suppress.Store(false)
	fn()
}

// CurrentConfig 调试用：返回当前生效的配置副本（不含 key）。
func CurrentConfig() Config {
	mu.RLock()
	defer mu.RUnlock()
	c := currentCf
	c.APIKey = ""
	return c
}

func normalizeEndpoint(ep string) string {
	ep = strings.TrimSpace(ep)
	if ep == "" {
		return DefaultEndpoint
	}
	return strings.TrimRight(ep, "/")
}

func sameConfigLocked(c Config) bool {
	return currentCf.APIKey == c.APIKey &&
		currentCf.Endpoint == c.Endpoint &&
		currentCf.Enabled == c.Enabled &&
		currentCf.AgentName == c.AgentName
}

func closeAsync(c *sdk.Client) {
	if c == nil {
		return
	}
	go func() {
		defer func() { _ = recover() }()
		_ = c.Close()
	}()
}
