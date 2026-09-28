package replay

import (
	"math"
	"testing"
	"time"
)

const (
	clockTestWallBudget   = 200 * time.Millisecond
	clockTestStopLoss     = 0.002
	clockTestLiqWindow    = 5
	clockTestLiqTickGapMs = int64(500)
	clockTestMarginCap    = 150.0
	clockTestFlatPrice    = 2000.0
	clockTestMarginLockS  = 10
	clockTestShortRunSecs = 6
	clockTestLongRunSecs  = 30
)

func TestSimClock_TimersAndSleepFollowSimulatedTime(t *testing.T) {
	start := time.UnixMilli(testBaseTs)
	c := NewSimClock(start)

	after := c.After(5 * time.Second)
	ticker := c.NewTicker(2 * time.Second)
	defer ticker.Stop()

	c.AdvanceTo(start.Add(4 * time.Second))
	select {
	case <-after:
		t.Fatal("After(5s) fired at +4s")
	default:
	}
	select {
	case <-ticker.C():
	default:
		t.Fatal("ticker(2s) must fire by +4s")
	}

	wallStart := time.Now()
	c.Sleep(time.Second) // +5s
	if time.Since(wallStart) > clockTestWallBudget {
		t.Fatal("SimClock.Sleep must not block on wall clock")
	}
	if got := c.Now().Sub(start); got != 5*time.Second {
		t.Fatalf("Sleep must advance simulated time, now=+%s", got)
	}
	select {
	case <-after:
	default:
		t.Fatal("After(5s) must fire once simulated time reaches +5s")
	}

	// 時鐘單調：更早的 tick 不回撥
	c.AdvanceTo(start.Add(time.Second))
	if got := c.Now().Sub(start); got != 5*time.Second {
		t.Fatalf("clock must be monotonic, now=+%s", got)
	}
	ticker.Stop()
	c.AdvanceTo(start.Add(time.Minute))
	if n := c.pendingTimers(); n != 0 {
		t.Fatalf("stopped/fired timers must be released, pending=%d", n)
	}
}

// flatTicks 每秒一筆、價格不變，共 secs+1 筆
func flatTicks(secs int, price float64) []Tick {
	prices := make([]float64, secs+1)
	for i := range prices {
		prices[i] = price
	}
	return ticksFromPrices(prices, 1)
}

func TestReplay_LiquidateAllDoesNotWaitWallClock(t *testing.T) {
	cfg := testConfig(MatchingConfig{})
	cfg.Bot = testBotConfig(clockTestLiqWindow)
	cfg.Bot.Trading.GridRiskControl.Enabled = true
	cfg.Bot.Trading.GridRiskControl.StopLossRatio = clockTestStopLoss
	// 1990 買單成交後價格跌到 1975（1980 也成交），浮虧超過 0.2% 觸發硬止損 →
	// 核實任務在 tick 後排空：模擬交易所即刻回報撤單與吃單終態，不需舊路徑固定等 2s。
	ticks := ticksFromPrices([]float64{2000, 1985, 1975, 1975}, 10)
	for i := range ticks {
		ticks[i].Timestamp = testBaseTs + int64(i)*clockTestLiqTickGapMs
	}

	wallStart := time.Now()
	eng := NewEngine(cfg)
	res, err := eng.Run(ticks)
	elapsed := time.Since(wallStart)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if res.Metrics.TakerFills == 0 || res.Metrics.OrdersCanceled == 0 {
		t.Fatalf("stop loss must cancel open orders and liquidate with taker fills, metrics=%+v", res.Metrics)
	}
	if elapsed > clockTestWallBudget {
		t.Fatalf("replay with liquidation took %s wall time, want < %s", elapsed, clockTestWallBudget)
	}
	lastTs := time.UnixMilli(ticks[len(ticks)-1].Timestamp)
	if !eng.Clock().Now().Equal(lastTs) {
		t.Fatalf("immediate verified terminal must not invent a future tick: clock=%s last tick=%s", eng.Clock().Now(), lastTs)
	}
	if eng.spm.GetProtectiveLiquidationStatus().State != "completed" || math.Abs(eng.spm.GetNetPositionQty()) > testFloatDelta || math.Abs(eng.ex.snapshot().netQty) > testFloatDelta {
		t.Fatal("stop loss returned without verified local/exchange settlement")
	}
}

func marginRejectsAfter(t *testing.T, secs int) int {
	t.Helper()
	cfg := testConfig(MatchingConfig{})
	cfg.InitialCapital = clockTestMarginCap
	cfg.EnforceMargin = true
	res, err := RunTicks(cfg, flatTicks(secs, clockTestFlatPrice))
	if err != nil {
		t.Fatalf("replay %ds: %v", secs, err)
	}
	return res.Metrics.MarginRejects
}

func TestReplay_MarginLockExpiresInSimulatedTime(t *testing.T) {
	wallStart := time.Now()
	short := marginRejectsAfter(t, clockTestShortRunSecs)
	withinLock := marginRejectsAfter(t, clockTestMarginLockS-1)
	long := marginRejectsAfter(t, clockTestLongRunSecs)
	if short == 0 {
		t.Fatalf("capital %.0f must trigger a margin rejection", clockTestMarginCap)
	}
	if withinLock != short {
		t.Fatalf("no orders may be attempted during the %ds simulated margin lock: rejects %d at %ds vs %d at %ds",
			clockTestMarginLockS, short, clockTestShortRunSecs, withinLock, clockTestMarginLockS-1)
	}
	if long <= short {
		t.Fatalf("margin lock must expire after %ds of simulated time and orders be retried: rejects %d at %ds vs %d at %ds",
			clockTestMarginLockS, short, clockTestShortRunSecs, long, clockTestLongRunSecs)
	}
	if elapsed := time.Since(wallStart); elapsed > time.Second {
		t.Fatalf("margin-lock replays took %s wall time", elapsed)
	}
}

func TestConfigFromTask_EnforceMarginDefaultsOn(t *testing.T) {
	if !defaultTaskEnforceMargin {
		t.Fatal("enforce_margin must default to true now that the margin lock follows simulated time")
	}
}
