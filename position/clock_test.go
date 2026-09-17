package position

import (
	"strings"
	"sync"
	"testing"
	"time"

	"quantmesh/utils"
)

const (
	clockTestSlotPrice     = 50000.0
	clockTestOrderCount    = 5000
	clockTestWallBudget    = 200 * time.Millisecond
	binanceClientIDMaxLen  = 36
	okxClientIDMaxLen      = 32
	clockTestCooldownSlack = time.Second
)

// manualClock 測試用手動時鐘：Sleep 立即推進時間；After/NewTicker 在 Advance 時觸發
type manualClock struct {
	mu      sync.Mutex
	now     time.Time
	sleeps  int
	waiters []*manualWaiter
}

type manualWaiter struct {
	at     time.Time
	period time.Duration
	ch     chan time.Time
	stop   bool
}

func newManualClock() *manualClock {
	return &manualClock{now: time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)}
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.sleeps++
	c.mu.Unlock()
	c.Advance(d)
}

func (c *manualClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := &manualWaiter{at: c.now.Add(d), ch: make(chan time.Time, 1)}
	c.waiters = append(c.waiters, w)
	return w.ch
}

func (c *manualClock) NewTicker(d time.Duration) Ticker {
	c.mu.Lock()
	defer c.mu.Unlock()
	w := &manualWaiter{at: c.now.Add(d), period: d, ch: make(chan time.Time, 1)}
	c.waiters = append(c.waiters, w)
	return manualTicker{c: c, w: w}
}

type manualTicker struct {
	c *manualClock
	w *manualWaiter
}

func (t manualTicker) C() <-chan time.Time { return t.w.ch }
func (t manualTicker) Stop() {
	t.c.mu.Lock()
	t.w.stop = true
	t.c.mu.Unlock()
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for _, w := range c.waiters {
		for !w.stop && !w.at.After(c.now) {
			select {
			case w.ch <- w.at:
			default:
			}
			if w.period <= 0 {
				w.stop = true
				break
			}
			w.at = w.at.Add(w.period)
		}
	}
}

func TestSPMClock_DefaultsToRealClock(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	if _, ok := spm.Clock().(realClock); !ok {
		t.Fatalf("default clock must be wall clock, got %T", spm.Clock())
	}
	mc := newManualClock()
	spm.SetClock(mc)
	if spm.Clock() != Clock(mc) || spm.allocationManager.clk.get() != Clock(mc) {
		t.Fatalf("SetClock must apply to manager and allocation manager")
	}
	spm.SetClock(nil)
	if _, ok := spm.Clock().(realClock); !ok {
		t.Fatalf("SetClock(nil) must restore wall clock, got %T", spm.Clock())
	}
}

func TestSPMClock_ReduceOnlyCooldownUsesSimulatedTime(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	mc := newManualClock()
	spm.SetClock(mc)

	spm.handleReduceOnlyRejection(clockTestSlotPrice, "SELL", spm.generateClientOrderID(clockTestSlotPrice, "SELL", ""))
	if !spm.isReduceOnlyCooldown(clockTestSlotPrice) {
		t.Fatal("cooldown must start at rejection")
	}
	mc.Advance(reduceOnlyCooldownDuration - clockTestCooldownSlack)
	if !spm.isReduceOnlyCooldown(clockTestSlotPrice) {
		t.Fatalf("cooldown must still hold %s of simulated time after rejection", reduceOnlyCooldownDuration-clockTestCooldownSlack)
	}
	mc.Advance(clockTestCooldownSlack)
	if spm.isReduceOnlyCooldown(clockTestSlotPrice) {
		t.Fatalf("cooldown must expire after %s of simulated time", reduceOnlyCooldownDuration)
	}
}

func TestSPMClock_MarginLockExpiresInSimulatedTime(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	mc := newManualClock()
	spm.SetClock(mc)
	spm.insufficientMargin = true
	spm.marginLockTime = mc.Now()

	mc.Advance(spm.marginLockDuration - time.Second)
	if err := spm.AdjustOrders(clockTestSlotPrice); err != nil {
		t.Fatalf("AdjustOrders during margin lock: %v", err)
	}
	if !spm.insufficientMargin {
		t.Fatalf("margin lock must hold before %s of simulated time", spm.marginLockDuration)
	}
	mc.Advance(time.Second)
	_ = spm.AdjustOrders(clockTestSlotPrice)
	if spm.insufficientMargin {
		t.Fatalf("margin lock must expire after %s of simulated time", spm.marginLockDuration)
	}
}

func TestSPMClock_CancelAllOpenOrdersDoesNotWaitWallClock(t *testing.T) {
	spm, exec := newStateTestSPM("LONG", "futures")
	mc := newManualClock()
	spm.SetClock(mc)
	slot := spm.getOrCreateSlot(clockTestSlotPrice)
	slot.mu.Lock()
	slot.OrderID = 1
	slot.OrderSide = "BUY"
	slot.OrderStatus = OrderStatusPlaced
	slot.mu.Unlock()

	start := time.Now()
	simStart := mc.Now()
	spm.CancelAllOpenOrders()
	if elapsed := time.Since(start); elapsed > clockTestWallBudget {
		t.Fatalf("CancelAllOpenOrders took %s wall time with simulated clock", elapsed)
	}
	if len(exec.CancelledOrderIDs) == 0 {
		t.Fatal("open order must be canceled")
	}
	if got := mc.Now().Sub(simStart); got < cancelSettleWait {
		t.Fatalf("cancel settle wait must consume simulated time, advanced %s", got)
	}
}

func TestSPMClock_ClientOrderIDsUniqueWithinOneSimulatedSecond(t *testing.T) {
	for _, exName := range []string{"binance", "okx"} {
		t.Run(exName, func(t *testing.T) {
			spm, _ := newStateTestSPM("LONG", "futures")
			spm.exchangeName = exName
			spm.SetClock(newManualClock()) // 模擬時間凍結在同一秒

			seen := make(map[string]struct{}, clockTestOrderCount)
			for i := 0; i < clockTestOrderCount; i++ {
				id := spm.generateClientOrderID(clockTestSlotPrice, "BUY", "")
				if _, dup := seen[id]; dup {
					t.Fatalf("duplicate client order id %q at #%d", id, i)
				}
				seen[id] = struct{}{}
				switch exName {
				case "okx":
					if len(id) > okxClientIDMaxLen || strings.Contains(id, "_") {
						t.Fatalf("okx clOrdId %q violates length/charset rules", id)
					}
				default:
					if full := utils.AddBrokerPrefix(exName, id); len(full) > binanceClientIDMaxLen {
						t.Fatalf("binance client id %q exceeds %d chars", full, binanceClientIDMaxLen)
					}
				}
				if p, side, ok := spm.parseClientOrderID(id); !ok || p != clockTestSlotPrice || side != "BUY" {
					t.Fatalf("id %q must round-trip, got price=%v side=%s ok=%v", id, p, side, ok)
				}
			}
		})
	}
}
