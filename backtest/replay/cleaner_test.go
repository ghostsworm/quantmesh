package replay

import (
	"testing"
	"time"
)

const (
	stallThreshold      = 12
	stallBatch          = 4
	stallCleanupSeconds = 60
	stallMaxLayers      = 6
)

// sawtoothUptrend 單邊上漲的鋸齒路徑：每輪先漲 rise 再回落 fall（每 tick 移動 step），淨上漲
func sawtoothUptrend(start, step float64, riseSteps, fallSteps, cycles int) []float64 {
	prices := []float64{start}
	p := start
	for c := 0; c < cycles; c++ {
		for i := 0; i < riseSteps; i++ {
			p += step
			prices = append(prices, p)
		}
		for i := 0; i < fallSteps; i++ {
			p -= step
			prices = append(prices, p)
		}
	}
	return prices
}

type stallStats struct {
	fillsFirstHalf, fillsSecondHalf int
	maxOpenOrders                   int
	cleanerRuns, canceled           int
	maxQty                          float64
}

func runStallScenario(t *testing.T, cleaner bool) stallStats {
	t.Helper()
	ticks := ticksFromPrices(sawtoothUptrend(2000, 1, 40, 20, 60), 0)
	cfg := testConfig(MatchingConfig{})
	cfg.Bot = testBotConfig(5)
	cfg.Bot.Trading.OrderCleanupThreshold = stallThreshold
	cfg.Bot.Trading.CleanupBatchSize = stallBatch
	cfg.Bot.Trading.OpenPositionControl.MaxPositionLayers = stallMaxLayers
	cfg.Bot.Timing.OrderCleanupInterval = stallCleanupSeconds
	cfg.OrderCleaner = cleaner
	var st stallStats
	var eng *Engine
	cfg.OnTick = func(_ time.Time, _ float64) {
		if n := eng.ex.openOrderCount(); n > st.maxOpenOrders {
			st.maxOpenOrders = n
		}
	}
	eng = NewEngine(cfg)
	res, err := eng.Run(ticks)
	if err != nil {
		t.Fatalf("run (cleaner=%v): %v", cleaner, err)
	}
	mid := ticks[len(ticks)/2].Timestamp
	for _, tr := range res.Backtest.Trades {
		if tr.Timestamp < mid {
			st.fillsFirstHalf++
		} else {
			st.fillsSecondHalf++
		}
	}
	st.cleanerRuns = res.Metrics.OrderCleanerRuns
	st.canceled = res.Metrics.OrdersCanceled
	st.maxQty = res.Metrics.Exposure.MaxAbsQty
	return st
}

// 單邊上漲中遠端買單累積到 order_cleanup_threshold：不運行清理器時網格停止開倉（復現校準報告缺陷 2），
// 運行清理器（模擬時間 60s 一輪）後持續開倉，且掛單數不超過閾值。
func TestReplay_OrderCleanerPreventsTrendStall(t *testing.T) {
	without := runStallScenario(t, false)
	with := runStallScenario(t, true)
	t.Logf("without cleaner: %+v", without)
	t.Logf("with cleaner:    %+v", with)

	if without.fillsFirstHalf == 0 {
		t.Fatalf("scenario must trade before stalling, got no fills in first half")
	}
	if without.fillsSecondHalf != 0 || without.cleanerRuns != 0 || without.canceled != 0 {
		t.Fatalf("without cleaner the grid should stall in the second half: %+v", without)
	}
	if with.cleanerRuns == 0 || with.canceled == 0 {
		t.Fatalf("cleaner must run on sim time and cancel far orders: %+v", with)
	}
	if with.fillsSecondHalf == 0 {
		t.Fatalf("with cleaner the grid must keep opening in the second half: %+v", with)
	}
	if with.maxOpenOrders > stallThreshold {
		t.Fatalf("open orders %d exceed order_cleanup_threshold %d", with.maxOpenOrders, stallThreshold)
	}
	// 每格 100 USDT、價格 ≥ 2000 → 每層 ≤ 0.05；持倉不得超過層數上限
	if maxQty := float64(stallMaxLayers) * testOrderUSDT / 2000; with.maxQty > maxQty {
		t.Fatalf("max position %.4f exceeds %d layers (%.4f)", with.maxQty, stallMaxLayers, maxQty)
	}
	if with.fillsFirstHalf+with.fillsSecondHalf <= without.fillsFirstHalf+without.fillsSecondHalf {
		t.Fatalf("cleaner run should trade more than the stalled run: with=%+v without=%+v", with, without)
	}
}
