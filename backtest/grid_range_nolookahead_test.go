package backtest

import (
	"math"
	"testing"

	"quantmesh/exchange"
)

// oscillatingCandles 在 base 附近 ±amp 擺動的 K 線
func oscillatingCandles(n int, base, amp float64, startTs int64) []*exchange.Candle {
	out := make([]*exchange.Candle, n)
	for i := 0; i < n; i++ {
		p := base + amp*math.Sin(float64(i)/3)
		out[i] = &exchange.Candle{Open: p, High: p + amp/4, Low: p - amp/4, Close: p, Volume: 10, Timestamp: startTs + int64(i)*60000}
	}
	return out
}

func TestDeriveGridRangeNoLookahead_UsesOnlyStartPrice(t *testing.T) {
	first := &exchange.Candle{Open: 2000, Close: 2010, Timestamp: 1}
	low, high, err := deriveGridRangeNoLookahead(first, 0, 0, 0)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if math.Abs(low-2000*(1-DefaultGridAutoRangeRatio)) > 1e-9 || math.Abs(high-2000*(1+DefaultGridAutoRangeRatio)) > 1e-9 {
		t.Fatalf("range [%v,%v] must be open price ±%v", low, high, DefaultGridAutoRangeRatio)
	}
	if low, high, _ = deriveGridRangeNoLookahead(first, 1500, 0, 0.05); low != 1500 || math.Abs(high-2100) > 1e-9 {
		t.Fatalf("explicit low must be kept: [%v,%v]", low, high)
	}
	if _, _, err := deriveGridRangeNoLookahead(&exchange.Candle{}, 0, 0, 0); err == nil {
		t.Fatal("candle without price must error")
	}
}

// 兩組數據前半段完全相同、後半段一組崩盤：未填區間時，前半段的成交必須完全一致（不讀取未來最低價）
func TestRunGridBacktest_AutoRangeHasNoFutureLeakage(t *testing.T) {
	const half = 60
	prefix := oscillatingCandles(half, 2000, 40, 0)
	calm := append(append([]*exchange.Candle{}, prefix...), oscillatingCandles(half, 2000, 40, int64(half)*60000)...)
	crash := append(append([]*exchange.Candle{}, prefix...), oscillatingCandles(half, 1000, 40, int64(half)*60000)...)

	params := GridBacktestParams{GridCount: 20, OrderQuantity: 100, TotalCapital: 10000, FeeRate: 0.0004}
	resCalm, err := RunGridBacktest("ETHUSDT", calm, params, 10000, nil)
	if err != nil {
		t.Fatalf("calm: %v", err)
	}
	resCrash, err := RunGridBacktest("ETHUSDT", crash, params, 10000, nil)
	if err != nil {
		t.Fatalf("crash: %v", err)
	}
	cutoff := int64(half) * 60000
	prefixTrades := func(r *BacktestResult) []Trade {
		var out []Trade
		for _, tr := range r.Trades {
			if tr.Timestamp < cutoff {
				out = append(out, tr)
			}
		}
		return out
	}
	a, b := prefixTrades(resCalm), prefixTrades(resCrash)
	if len(a) == 0 {
		t.Fatal("expected trades in the shared prefix")
	}
	if len(a) != len(b) {
		t.Fatalf("prefix trades differ (%d vs %d): grid range depends on future prices", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("prefix trade %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}
