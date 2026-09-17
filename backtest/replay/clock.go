package replay

import (
	"sync"
	"time"

	"quantmesh/position"
)

// SimClock 由 tick 時間戳驅動的模擬時鐘，實現 position.Clock。
//
//   - Now 返回模擬時間；引擎在處理每個 tick 前調用 AdvanceTo(tick 時間)；
//   - Sleep 不真實等待，立即把模擬時間推進 d（倉位管理器撤單等待 2s 在回放中不消耗牆鐘，
//     但仍消耗模擬時間，與實盤「價格循環被阻塞 2s」同向）；
//   - After / NewTicker 在模擬時間到達觸發點時投遞（通道緩衝 1，與 time.Ticker 一樣在消費不及時丟棄）。
//
// 模擬時間單調不減：AdvanceTo 早於當前模擬時間時忽略（Sleep 推進後，後續更早的 tick 不回撥時鐘）。
// 並發安全。
type SimClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []*simWaiter
}

// simWaiter 一次性（period=0）或週期觸發器
type simWaiter struct {
	at      time.Time
	period  time.Duration
	ch      chan time.Time
	stopped bool
}

var _ position.Clock = (*SimClock)(nil)

// NewSimClock 創建起始於 start 的模擬時鐘
func NewSimClock(start time.Time) *SimClock {
	return &SimClock{now: start}
}

// Now 當前模擬時間
func (c *SimClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Sleep 立即返回並將模擬時間推進 d（d<=0 時不推進）
func (c *SimClock) Sleep(d time.Duration) {
	if d <= 0 {
		return
	}
	c.mu.Lock()
	target := c.now.Add(d)
	c.mu.Unlock()
	c.AdvanceTo(target)
}

// After 模擬時間經過 d 後觸發一次；d<=0 立即觸發
func (c *SimClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := &simWaiter{at: c.now.Add(d), ch: make(chan time.Time, 1)}
	if d <= 0 {
		w.ch <- c.now
		return w.ch
	}
	c.waiters = append(c.waiters, w)
	return w.ch
}

// NewTicker 模擬時間每經過 d 觸發一次；d<=0 時 panic（與 time.NewTicker 一致）
func (c *SimClock) NewTicker(d time.Duration) position.Ticker {
	if d <= 0 {
		panic("replay: SimClock.NewTicker requires a positive interval")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	w := &simWaiter{at: c.now.Add(d), period: d, ch: make(chan time.Time, 1)}
	c.waiters = append(c.waiters, w)
	return &simTicker{clock: c, w: w}
}

// AdvanceTo 把模擬時間推進到 t 並觸發到期的定時器；t 不晚於當前模擬時間時不變
func (c *SimClock) AdvanceTo(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !t.After(c.now) {
		return
	}
	c.now = t
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if w.stopped {
			continue
		}
		if w.at.After(t) {
			kept = append(kept, w)
			continue
		}
		select {
		case w.ch <- w.at:
		default: // 消費不及時：丟棄本次觸發
		}
		if w.period <= 0 {
			continue
		}
		// 週期觸發器：跳過已錯過的週期，下一次觸發點嚴格晚於 t
		missed := t.Sub(w.at)/w.period + 1
		w.at = w.at.Add(missed * w.period)
		kept = append(kept, w)
	}
	for i := len(kept); i < len(c.waiters); i++ {
		c.waiters[i] = nil
	}
	c.waiters = kept
}

// AdvanceToMillis 按毫秒時間戳推進
func (c *SimClock) AdvanceToMillis(ts int64) { c.AdvanceTo(time.UnixMilli(ts)) }

// pendingTimers 未觸發的定時器數（測試用）
func (c *SimClock) pendingTimers() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

type simTicker struct {
	clock *SimClock
	w     *simWaiter
}

func (t *simTicker) C() <-chan time.Time { return t.w.ch }

func (t *simTicker) Stop() {
	t.clock.mu.Lock()
	t.w.stopped = true
	t.clock.mu.Unlock()
}
