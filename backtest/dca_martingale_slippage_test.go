package backtest

import (
	"math"
	"testing"

	"quantmesh/exchange"
)

func TestDCABacktestAppliesSlippageAndMarksEquityAfterFill(t *testing.T) {
	candles := []*exchange.Candle{
		{Timestamp: 1_790_000_000_000, Close: 100},
		{Timestamp: 1_790_086_400_000, Close: 110},
	}
	result, err := RunDCABacktest("BTCUSDT", "1d", candles, DCABacktestParams{
		IntervalDays: 1, AmountPerTrade: 100, TotalCapital: 100, FeeRate: 0.001, SlippageRatio: 0.01,
	}, 1000)
	if err != nil {
		t.Fatalf("RunDCABacktest: %v", err)
	}
	if len(result.Trades) != 1 || math.Abs(result.Trades[0].Price-101) > 1e-9 {
		t.Fatalf("buy should execute with 1%% adverse slippage: %+v", result.Trades)
	}
	if result.Metrics.TotalSlippageLoss <= 0 || result.Equity[0].Equity >= 1000 {
		t.Fatalf("slippage loss or post-fill mark missing: slippage=%v equity=%+v", result.Metrics.TotalSlippageLoss, result.Equity)
	}
	if math.Abs(result.Equity[0].Equity-(1000-100+result.Trades[0].Quantity*100)) > 1e-8 {
		t.Fatalf("first equity point must reflect the executed buy: got %v", result.Equity[0].Equity)
	}
}

func TestMartingaleBacktestChargesAdverseEntryAndExitSlippage(t *testing.T) {
	candles := []*exchange.Candle{
		{Timestamp: 1_790_000_000_000, Close: 100},
		{Timestamp: 1_790_003_600_000, Close: 102},
	}
	result, err := RunMartingaleBacktest("BTCUSDT", "1h", candles, MartingaleBacktestParams{
		BaseAmount: 100, Multiplier: 2, TotalCapital: 500, FeeRate: 0.001,
		TakeProfitPct: 0.5, StopLossPct: 2, SlippageRatio: 0.01,
	}, 1000)
	if err != nil {
		t.Fatalf("RunMartingaleBacktest: %v", err)
	}
	if len(result.Trades) != 2 || result.Trades[0].Price <= candles[0].Close || result.Trades[1].Price >= candles[1].Close {
		t.Fatalf("expected adverse buy and sell fills: %+v", result.Trades)
	}
	if result.Metrics.TotalSlippageLoss <= 0 || result.FinalCapital >= 1000 {
		t.Fatalf("costs were not reflected in the result: slippage=%v final=%v", result.Metrics.TotalSlippageLoss, result.FinalCapital)
	}
}

func TestBacktestRejectsInvalidSlippageRatio(t *testing.T) {
	candles := []*exchange.Candle{{Timestamp: 1_790_000_000_000, Close: 100}}
	for _, slip := range []float64{-0.01, 1, math.NaN(), math.Inf(1)} {
		if _, err := RunDCABacktest("BTCUSDT", "1d", candles, DCABacktestParams{IntervalDays: 1, AmountPerTrade: 10, TotalCapital: 10, SlippageRatio: slip}, 100); err == nil {
			t.Errorf("DCA accepted slippage ratio %v", slip)
		}
		if _, err := RunMartingaleBacktest("BTCUSDT", "1h", candles, MartingaleBacktestParams{BaseAmount: 10, TotalCapital: 10, SlippageRatio: slip}, 100); err == nil {
			t.Errorf("martingale accepted slippage ratio %v", slip)
		}
	}
}
