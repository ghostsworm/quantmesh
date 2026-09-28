package backtest

import (
	"math"
	"testing"
)

func TestMultiStrategyTradeCashflowsPreserveMarkedEquity(t *testing.T) {
	tests := []struct {
		name       string
		openSide   string
		closeSide  string
		closePrice float64
		wantEquity float64
		wantFees   float64
		wantVolume float64
	}{
		{name: "long round trip", openSide: "buy", closeSide: "sell", closePrice: 110, wantEquity: 1019.58, wantFees: 0.42, wantVolume: 420},
		{name: "short round trip", openSide: "sell", closeSide: "buy", closePrice: 90, wantEquity: 1019.62, wantFees: 0.38, wantVolume: 380},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validEngineConfigForValidation()
			cfg.CommissionRate = 0.001
			engine := NewMultiStrategyEngine(cfg)
			account := NewBacktestAccount("BTCUSDT", 1000, 1, 1)
			account.lastPrice = 100
			runtime := &StrategyRuntime{
				account:     account,
				stats:       &StrategyStats{},
				equityCurve: []EquityPoint{{Timestamp: 1000, Equity: 1000}},
			}

			engine.processTrade(runtime, &TickTrade{TradeID: "open", Side: tt.openSide, Price: 100, Size: 2, Timestamp: 1000})
			if math.Abs(account.Equity-999.8) > 1e-9 {
				t.Fatalf("equity after open = %.8f, want 999.8 (entry fee only)", account.Equity)
			}
			if account.PositionEntryPrice != 100 {
				t.Fatalf("entry price = %v, want fee-independent price 100", account.PositionEntryPrice)
			}

			account.lastPrice = tt.closePrice
			engine.processTrade(runtime, &TickTrade{TradeID: "close", Side: tt.closeSide, Price: tt.closePrice, Size: 2, Timestamp: 2000})
			if math.Abs(account.Equity-tt.wantEquity) > 1e-9 || math.Abs(account.Balance-tt.wantEquity) > 1e-9 {
				t.Fatalf("closed balance/equity = %.8f/%.8f, want %.8f", account.Balance, account.Equity, tt.wantEquity)
			}
			if account.PositionSize != 0 || account.UnrealizedPnL != 0 || account.PositionEntryPrice != 0 {
				t.Fatalf("closed account retained position state: size=%v unrealized=%v entry=%v", account.PositionSize, account.UnrealizedPnL, account.PositionEntryPrice)
			}
			if math.Abs(account.TotalFees-tt.wantFees) > 1e-9 {
				t.Fatalf("fees = %.8f, want %.8f", account.TotalFees, tt.wantFees)
			}
			if math.Abs(account.TotalVolume-tt.wantVolume) > 1e-9 {
				t.Fatalf("volume = %.8f, want full entry+exit notional %.8f", account.TotalVolume, tt.wantVolume)
			}
			if len(runtime.completedTrades) != 1 || math.Abs(runtime.completedTrades[0].Fee-tt.wantFees) > 1e-9 {
				t.Fatalf("round-trip completed trade fees = %+v, want total entry+exit fees %.8f", runtime.completedTrades, tt.wantFees)
			}
			if len(runtime.equityCurve) != 1 || math.Abs(runtime.equityCurve[0].Equity-tt.wantEquity) > 1e-9 {
				t.Fatalf("latest strategy equity point = %+v, want one point at %.8f", runtime.equityCurve, tt.wantEquity)
			}
		})
	}
}

func TestMultiStrategyReversalChargesTradeFeeOnce(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.CommissionRate = 0.001
	engine := NewMultiStrategyEngine(cfg)
	account := NewBacktestAccount("BTCUSDT", 1000, 1, 1)
	account.lastPrice = 100
	runtime := &StrategyRuntime{account: account, stats: &StrategyStats{}}

	engine.processTrade(runtime, &TickTrade{TradeID: "open-long", Side: "buy", Price: 100, Size: 1, Timestamp: 1000})
	account.lastPrice = 110
	engine.processTrade(runtime, &TickTrade{TradeID: "reverse-short", Side: "sell", Price: 110, Size: 2, Timestamp: 2000})

	if math.Abs(account.Equity-1009.68) > 1e-9 || account.PositionSize != -1 {
		t.Fatalf("reversal equity/position = %.8f/%v, want 1009.68/-1", account.Equity, account.PositionSize)
	}
	if math.Abs(account.TotalFees-0.32) > 1e-9 {
		t.Fatalf("total fees = %.8f, want 0.32", account.TotalFees)
	}
	if math.Abs(account.TotalVolume-320) > 1e-9 {
		t.Fatalf("reversal volume = %.8f, want 320 across both fills", account.TotalVolume)
	}
	if len(runtime.completedTrades) != 1 || math.Abs(runtime.completedTrades[0].Fee-0.21) > 1e-9 {
		t.Fatalf("closed leg fee = %+v, want allocated entry+exit fee 0.21", runtime.completedTrades)
	}
}

func TestMultiStrategyRiskMetricsUseNetCompletedTradePnL(t *testing.T) {
	metrics := calculateRiskMetricsFrom(
		[]EquityPoint{{Timestamp: 1000, Equity: 1000}, {Timestamp: 2000, Equity: 1000}},
		[]CompletedTrade{
			{PnL: 1, Fee: 1.1}, // 毛利但扣除往返費用後虧損
			{PnL: -1, Fee: 0.1},
		},
	)
	if metrics.WinRate != 0 || metrics.ProfitFactor != 0 || math.Abs(metrics.AvgLoss-(-0.6)) > 1e-9 || math.Abs(metrics.LargestLoss-(-1.1)) > 1e-9 {
		t.Fatalf("risk metrics ignored completed-trade fees: %+v", metrics)
	}
}

func TestMultiStrategyPartialCloseAllocatesEntryFeesByClosedQuantity(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.CommissionRate = 0.001
	engine := NewMultiStrategyEngine(cfg)
	account := NewBacktestAccount("BTCUSDT", 1000, 1, 1)
	account.lastPrice = 100
	runtime := &StrategyRuntime{account: account, stats: &StrategyStats{}}

	engine.processTrade(runtime, &TickTrade{TradeID: "open-long", Side: "buy", Price: 100, Size: 4, Timestamp: 1000})
	account.lastPrice = 110
	engine.processTrade(runtime, &TickTrade{TradeID: "partial-close", Side: "sell", Price: 110, Size: 2, Timestamp: 2000})
	if len(runtime.completedTrades) != 1 || math.Abs(runtime.completedTrades[0].Fee-0.42) > 1e-9 {
		t.Fatalf("partial completed-trade fee = %+v, want half entry fee plus exit fee 0.42", runtime.completedTrades)
	}
	if account.PositionSize != 2 || math.Abs(account.PositionEntryFees-0.2) > 1e-9 {
		t.Fatalf("remaining position/entry fee = %v/%.8f, want 2/0.2", account.PositionSize, account.PositionEntryFees)
	}

	account.lastPrice = 90
	engine.processTrade(runtime, &TickTrade{TradeID: "close-rest", Side: "sell", Price: 90, Size: 2, Timestamp: 3000})
	if len(runtime.completedTrades) != 2 || math.Abs(runtime.completedTrades[1].Fee-0.38) > 1e-9 {
		t.Fatalf("final completed-trade fee = %+v, want remaining entry fee plus exit fee 0.38", runtime.completedTrades)
	}
	if math.Abs(account.Equity-999.2) > 1e-9 || math.Abs(account.TotalFees-0.8) > 1e-9 {
		t.Fatalf("final equity/fees = %.8f/%.8f, want 999.2/0.8", account.Equity, account.TotalFees)
	}
}

func TestMultiStrategyLongOnlyOverCloseClampsCashflowToHeldPosition(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.PositionMode = "LONG"
	cfg.CommissionRate = 0.001
	engine := NewMultiStrategyEngine(cfg)
	account := NewBacktestAccount("BTCUSDT", 1000, 1, 1)
	account.lastPrice = 100
	runtime := &StrategyRuntime{account: account, stats: &StrategyStats{}}

	engine.processTrade(runtime, &TickTrade{TradeID: "open-long", Side: "buy", Price: 100, Size: 1, Slippage: 0.05, Timestamp: 1000})
	account.lastPrice = 110
	oversizedExit := TickTrade{TradeID: "oversized-exit", Side: "sell", Price: 110, Size: 2, Slippage: 0.04, Timestamp: 2000}
	engine.processTrade(runtime, &oversizedExit)

	if oversizedExit.Size != 1 || account.PositionSize != 0 {
		t.Fatalf("sell-to-close size/position = %v/%v, want 1/0", oversizedExit.Size, account.PositionSize)
	}
	if math.Abs(account.TotalFees-0.21) > 1e-9 || math.Abs(account.TotalVolume-210) > 1e-9 {
		t.Fatalf("fees/volume = %.8f/%.8f, want 0.21/210", account.TotalFees, account.TotalVolume)
	}
	if math.Abs(oversizedExit.Slippage-0.02) > 1e-9 || math.Abs(account.TotalSlippage-0.07) > 1e-9 {
		t.Fatalf("clamped trade/account slippage = %.8f/%.8f, want 0.02/0.07", oversizedExit.Slippage, account.TotalSlippage)
	}
	if math.Abs(account.Equity-1009.79) > 1e-9 {
		t.Fatalf("equity = %.8f, want 1009.79", account.Equity)
	}
}

func TestMultiStrategyForcedCloseUpdatesFinalEquityWithoutDuplicateTimeSample(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.CommissionRate = 0.001
	engine := NewMultiStrategyEngine(cfg)
	engine.Klines = []TickKline{
		{Timestamp: 1000, Open: 100, High: 105, Low: 98, Close: 110, Volume: 100},
		{Timestamp: 61000, Open: 110, High: 121, Low: 109, Close: 120, Volume: 100},
	}
	if err := engine.AddStrategy(&fundingPriceTimingStrategy{}); err != nil {
		t.Fatalf("AddStrategy() error = %v", err)
	}

	result, err := engine.Run()
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.EquityCurve) != len(engine.Klines) {
		t.Fatalf("equity curve has %d points for %d klines; same-time forced-close sample must replace the final point", len(result.EquityCurve), len(engine.Klines))
	}
	if len(result.EquityCurve) < 2 || result.EquityCurve[len(result.EquityCurve)-1].Timestamp == result.EquityCurve[len(result.EquityCurve)-2].Timestamp {
		t.Fatalf("final equity timestamps are not strictly increasing: %+v", result.EquityCurve)
	}
	if math.Abs(result.EquityCurve[len(result.EquityCurve)-1].Equity-result.FinalEquity) > 1e-9 {
		t.Fatalf("final equity point = %.8f, FinalEquity = %.8f", result.EquityCurve[len(result.EquityCurve)-1].Equity, result.FinalEquity)
	}
	if len(result.StrategyResults) != 1 || len(result.StrategyResults[0].EquityCurve) != len(engine.Klines) {
		t.Fatalf("strategy equity curve was not finalized in place: %+v", result.StrategyResults)
	}
	wantEquity := result.InitialCapital - result.TotalFees
	for _, trade := range result.Trades {
		cashflow := trade.Price * trade.Size
		if trade.Side == "buy" {
			wantEquity -= cashflow
		} else {
			wantEquity += cashflow
		}
	}
	if math.Abs(result.StrategyResults[0].FinalEquity-wantEquity) > 1e-9 {
		t.Fatalf("strategy final equity = %.8f, want cashflow ledger %.8f", result.StrategyResults[0].FinalEquity, wantEquity)
	}
}
