package position

import (
	"sync/atomic"
	"time"
)

const (
	// cancelSettleWait 批量撤單後等待撤單回報生效的時間（每輪）
	cancelSettleWait = 2 * time.Second
	// cancelRetryAttempts 撤開倉單/買單的最大輪數
	cancelRetryAttempts = 3
	// pauseResidualCancelDelay 暫停開倉後再向交易所核對殘留開倉單前的等待
	pauseResidualCancelDelay = 1 * time.Second
	// pauseResidualCancelTimeout 暫停開倉後查詢交易所掛單的網絡超時（牆鐘）
	pauseResidualCancelTimeout = 10 * time.Second
	// fillHistoryRetention 成交時間戳保留時長
	fillHistoryRetention = 2 * time.Minute
	// fillRateWindow 成交頻率統計窗口
	fillRateWindow = 1 * time.Minute
)

// Clock 倉位管理器使用的時鐘抽象。
//
// 實盤默認使用牆鐘（RealClock）；回放回測注入由 tick 時間戳驅動的模擬時鐘，
// 使保證金鎖、reduce-only 冷卻、去抖兜底、帳戶/槓桿緩存 TTL、撤單等待、
// 定時/週期開倉規則等與交易決策相關的時間邏輯按模擬時間生效。
//
// 實現必須並發安全。
type Clock interface {
	// Now 當前時間
	Now() time.Time
	// Sleep 阻塞 d（模擬時鐘可立即返回並推進模擬時間）
	Sleep(d time.Duration)
	// After 在 d 之後向返回的通道發送當時時間（語義同 time.After）
	After(d time.Duration) <-chan time.Time
	// NewTicker 週期觸發器（語義同 time.NewTicker）
	NewTicker(d time.Duration) Ticker
}

// Ticker 週期觸發器抽象（語義同 *time.Ticker）
type Ticker interface {
	// C 觸發通道
	C() <-chan time.Time
	// Stop 停止觸發（不關閉通道）
	Stop()
}

// realClock 牆鐘實現
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) Sleep(d time.Duration)                  { time.Sleep(d) }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (realClock) NewTicker(d time.Duration) Ticker       { return realTicker{t: time.NewTicker(d)} }

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }

// RealClock 返回牆鐘實現（實盤默認）
func RealClock() Clock { return realClock{} }

// clockBox atomic.Value 要求每次存入相同具體類型，故包一層
type clockBox struct{ c Clock }

// clockHolder 可原子替換的時鐘；零值為牆鐘
type clockHolder struct{ v atomic.Value }

func (h *clockHolder) get() Clock {
	if b, ok := h.v.Load().(clockBox); ok && b.c != nil {
		return b.c
	}
	return realClock{}
}

func (h *clockHolder) set(c Clock) {
	if c == nil {
		c = realClock{}
	}
	h.v.Store(clockBox{c: c})
}

// SetClock 注入時鐘（nil 恢復牆鐘）。並發安全，但應在 Initialize、啟動後台協程
// （智能掛單、自動重建、regime 控制循環、開倉控制器）之前調用：已在等待中的
// 牆鐘定時器不會遷移到新時鐘。同時傳遞給資金分配管理器。
func (spm *SuperPositionManager) SetClock(c Clock) {
	spm.clk.set(c)
	if spm.allocationManager != nil {
		spm.allocationManager.SetClock(c)
	}
}

// Clock 返回當前使用的時鐘
func (spm *SuperPositionManager) Clock() Clock { return spm.clk.get() }

// now 當前時間（按注入時鐘）
func (spm *SuperPositionManager) now() time.Time { return spm.clk.get().Now() }

// since 距 t 的時長（按注入時鐘）
func (spm *SuperPositionManager) since(t time.Time) time.Duration { return spm.now().Sub(t) }

// sleep 等待 d（按注入時鐘）
func (spm *SuperPositionManager) sleep(d time.Duration) { spm.clk.get().Sleep(d) }
