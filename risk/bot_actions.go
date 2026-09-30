package risk

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/logger"
)

// DefaultBotActionTimeout 單個 Bot 撤單/平倉的預設超時。
// 場景或熔斷配置 timeout <= 0 時使用，避免 ctx 立即過期導致所有平倉單直接失敗。
const DefaultBotActionTimeout = 30 * time.Second

// 操作狀態（紧急中心 / 熔断器共用）
const (
	OperationStatusExecuting = "executing"
	OperationStatusCompleted = "completed"
	OperationStatusPartial   = "partial" // 部分 Bot 失败
	OperationStatusFailed    = "failed"  // 全部失败或动作本身失败
)

// botActionTimeout 將秒數轉為超時；<= 0 時回落到預設值
func botActionTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return DefaultBotActionTimeout
	}
	return time.Duration(seconds) * time.Second
}

// BotActionReport 單個動作在所有 Bot 上的執行彙總
type BotActionReport struct {
	Action    string
	Total     int
	Succeeded int
	Errors    []error
}

// Failed 失败的 Bot 數
func (r BotActionReport) Failed() int {
	return r.Total - r.Succeeded
}

// Status 根據成功/失败數推導狀態
func (r BotActionReport) Status() string {
	switch {
	case r.Failed() == 0:
		return OperationStatusCompleted
	case r.Succeeded == 0:
		return OperationStatusFailed
	default:
		return OperationStatusPartial
	}
}

// Err 聚合所有 Bot 的錯誤；全部成功時返回 nil
func (r BotActionReport) Err() error {
	if len(r.Errors) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %d/%d 个 Bot 失败: %w", r.Action, r.Failed(), r.Total, errors.Join(r.Errors...))
}

// Summary 人類可讀摘要
func (r BotActionReport) Summary(verb string) string {
	return fmt.Sprintf("%s %d/%d 个Bot", verb, r.Succeeded, r.Total)
}

// runOnBots 並行對每個 Bot 執行 fn，彙總錯誤（不吞錯）
func runOnBots(action string, bots []BotController, fn func(idx int, bot BotController) error) BotActionReport {
	report := BotActionReport{Action: action, Total: len(bots)}
	if len(bots) == 0 {
		return report
	}

	errs := make([]error, len(bots))
	var wg sync.WaitGroup
	for i, bot := range bots {
		wg.Add(1)
		go func(idx int, b BotController) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errs[idx] = fmt.Errorf("bot#%d %s panic: %v", idx, action, r)
				}
			}()
			if err := fn(idx, b); err != nil {
				errs[idx] = fmt.Errorf("bot#%d %s: %w", idx, action, err)
			}
		}(i, bot)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			report.Errors = append(report.Errors, err)
		}
	}
	report.Succeeded = report.Total - len(report.Errors)
	return report
}

// cancelOrdersOnBots 撤銷所有 Bot 掛單
func cancelOrdersOnBots(bots []BotController) BotActionReport {
	return runOnBots("cancel_all_orders", bots, func(_ int, bot BotController) error {
		return bot.CancelAllOpenOrders()
	})
}

// closePositionsOnBots 平掉所有 Bot 倉位；每個 Bot 使用獨立超時 ctx，互不擠占
func closePositionsOnBots(parent context.Context, bots []BotController, method string, timeoutSec int) BotActionReport {
	if parent == nil {
		parent = context.Background()
	}
	timeout := botActionTimeout(timeoutSec)
	effectiveSec := int(timeout / time.Second)
	type scopedBot interface{ CloseScopeKey() string }
	type botEntry struct {
		index int
		bot   BotController
	}
	groups := make(map[string][]botEntry)
	for i, bot := range bots {
		key := ""
		if scoped, ok := bot.(scopedBot); ok {
			key = scoped.CloseScopeKey()
		}
		if key == "" {
			key = fmt.Sprintf("unscoped:%d", i)
		}
		groups[key] = append(groups[key], botEntry{index: i, bot: bot})
	}
	errs := make([]error, len(bots))
	var succeeded atomic.Int32
	var wg sync.WaitGroup
	for _, group := range groups {
		group := group
		wg.Add(1)
		go func() {
			defer wg.Done()
			for position, entry := range group {
				func() {
					defer func() {
						if recovered := recover(); recovered != nil {
							errs[entry.index] = fmt.Errorf("bot#%d close_all_positions panic: %v", entry.index, recovered)
						}
					}()
					ctx, cancel := context.WithTimeout(parent, timeout)
					defer cancel()
					if err := entry.bot.CloseAllPositions(ctx, method, effectiveSec); err != nil {
						errs[entry.index] = fmt.Errorf("bot#%d close_all_positions: %w", entry.index, err)
					}
				}()
				if errs[entry.index] != nil {
					for _, skipped := range group[position+1:] {
						errs[skipped.index] = fmt.Errorf("same account close skipped after prior Bot result was unverified")
					}
					break
				}
				succeeded.Add(1)
			}
		}()
	}
	wg.Wait()
	report := BotActionReport{Action: "close_all_positions", Total: len(bots), Succeeded: int(succeeded.Load())}
	for _, err := range errs {
		if err != nil {
			report.Errors = append(report.Errors, err)
		}
	}
	return report
}

// OpeningPauseCoordinator 多來源暫停開倉協調器。
// BotController 的暫停只有單一旗標，熔斷器與複合風控各自恢復時會互相覆蓋；
// 這裡按來源記錄暫停，只有所有自動風控來源都解除後才真正 ResumeOpening。
// 注意：Web 手動恢復 / 紧急中心仍直接調用 Bot，不經過此協調器。
type OpeningPauseCoordinator struct {
	mu      sync.Mutex
	holders map[string]string // source -> reason
}

type nonAutoResumingBot interface {
	PauseOpeningWithoutAutoResume(reason string)
}

func pauseBotWithoutAutoResume(bot BotController, reason string) {
	if bot == nil {
		return
	}
	if pauser, ok := bot.(nonAutoResumingBot); ok {
		pauser.PauseOpeningWithoutAutoResume(reason)
		return
	}
	bot.PauseOpening(reason)
}

// NewOpeningPauseCoordinator 創建協調器
func NewOpeningPauseCoordinator() *OpeningPauseCoordinator {
	return &OpeningPauseCoordinator{holders: make(map[string]string)}
}

// Pause 以 source 身份暫停所有 Bot 開倉
func (c *OpeningPauseCoordinator) Pause(source, reason string, bots []BotController) {
	c.mu.Lock()
	c.holders[source] = reason
	c.mu.Unlock()
	for _, bot := range bots {
		pauseBotWithoutAutoResume(bot, reason)
	}
}

// Release 解除 source 的暫停；若仍有其他來源持有暫停則不恢復，返回是否真正恢復
func (c *OpeningPauseCoordinator) Release(source string, bots []BotController) bool {
	c.mu.Lock()
	delete(c.holders, source)
	remaining := make([]string, 0, len(c.holders))
	for s := range c.holders {
		remaining = append(remaining, s)
	}
	c.mu.Unlock()

	if len(remaining) > 0 {
		sort.Strings(remaining)
		logger.Warn("⏸️ [风控协调] %s 已解除，但仍被 %v 暂停开仓，暂不恢复", source, remaining)
		return false
	}
	for _, bot := range bots {
		bot.ResumeOpening()
	}
	return true
}

// IsHeldBy source 是否持有暫停
func (c *OpeningPauseCoordinator) IsHeldBy(source string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.holders[source]
	return ok
}

// Holders 當前持有暫停的來源快照
func (c *OpeningPauseCoordinator) Holders() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.holders))
	for k, v := range c.holders {
		out[k] = v
	}
	return out
}
