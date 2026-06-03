package backtest

import (
	"testing"

	"quantmesh/exchange"
)

type sequenceStrategy struct {
	name    string
	actions []string
	index   int
}

func (s *sequenceStrategy) OnCandle(candle *exchange.Candle) Signal {
	if s.index >= len(s.actions) {
		return Signal{Action: "hold"}
	}
	action := s.actions[s.index]
	s.index++
	return Signal{Action: action}
}

func (s *sequenceStrategy) GetName() string {
	if s.name == "" {
		return "sequence"
	}
	return s.name
}

func TestIntrabarPriceSimulationPaths(t *testing.T) {
	strategy := &sequenceStrategy{}
	up := NewIntrabarBacktester("BTCUSDT", nil, strategy, 1000, 8)
	upTicks := up.SimulateIntrabarPrices(&exchange.Candle{Open: 100, High: 120, Low: 90, Close: 110, Timestamp: 1000})
	if len(upTicks) != 8 {
		t.Fatalf("up SimulateIntrabarPrices() length = %d, want 8", len(upTicks))
	}
	if upTicks[0].Price != 100 || upTicks[2].Price != 120 || upTicks[4].Price != 90 {
		t.Fatalf("up ticks = %+v, want open-high-low progression", upTicks)
	}

	down := NewIntrabarBacktester("BTCUSDT", nil, strategy, 1000, 8)
	downTicks := down.SimulateIntrabarPrices(&exchange.Candle{Open: 110, High: 120, Low: 90, Close: 100, Timestamp: 2000})
	if len(downTicks) != 8 {
		t.Fatalf("down SimulateIntrabarPrices() length = %d, want 8", len(downTicks))
	}
	if downTicks[0].Price != 110 || downTicks[2].Price != 90 || downTicks[4].Price != 120 {
		t.Fatalf("down ticks = %+v, want open-low-high progression", downTicks)
	}
}

func TestIntrabarExecuteBuySellAndFees(t *testing.T) {
	ibt := NewIntrabarBacktester("BTCUSDT", nil, &sequenceStrategy{}, 1000, 8)
	ibt.SetFees(0.001, 0.0005, 0.0002)
	if ibt.takerFee != 0.001 || ibt.makerFee != 0.0005 || ibt.slippage != 0.0002 {
		t.Fatalf("SetFees() taker/maker/slippage = %.4f/%.4f/%.4f", ibt.takerFee, ibt.makerFee, ibt.slippage)
	}

	ibt.cash = 1001
	ibt.executeBuyAtPrice(100, 1)
	if ibt.position <= 0 || ibt.cash < 0 || ibt.cash >= 2 || len(ibt.trades) != 1 {
		t.Fatalf("after buy cash=%v position=%v trades=%d", ibt.cash, ibt.position, len(ibt.trades))
	}
	if ibt.trades[0].Type != "buy" || ibt.entryPrice != 100 {
		t.Fatalf("buy trade = %+v entry=%v", ibt.trades[0], ibt.entryPrice)
	}

	ibt.executeSellAtPrice(110, 2)
	if ibt.position != 0 || ibt.cash <= 1000 || len(ibt.trades) != 2 {
		t.Fatalf("after sell cash=%v position=%v trades=%d", ibt.cash, ibt.position, len(ibt.trades))
	}
	if ibt.trades[1].Type != "sell" || ibt.trades[1].PnL <= 0 {
		t.Fatalf("sell trade = %+v, want positive pnl", ibt.trades[1])
	}

	ibt.executeSellAtPrice(120, 3)
	if len(ibt.trades) != 2 {
		t.Fatalf("executeSellAtPrice() without position appended trade")
	}
	ibt.cash = 0
	ibt.executeBuyAtPrice(100, 4)
	if len(ibt.trades) != 2 {
		t.Fatalf("executeBuyAtPrice() without cash appended trade")
	}
}

func TestIntrabarRunSmallBacktest(t *testing.T) {
	candles := []*exchange.Candle{
		{Symbol: "BTCUSDT", Open: 100, High: 110, Low: 90, Close: 105, Volume: 80, Timestamp: 60_000},
		{Symbol: "BTCUSDT", Open: 105, High: 120, Low: 100, Close: 115, Volume: 80, Timestamp: 120_000},
	}
	strategy := &sequenceStrategy{name: "scripted", actions: []string{"buy", "hold", "sell"}}
	ibt := NewIntrabarBacktester("BTCUSDT", candles, strategy, 1000, 4)
	ibt.SetFees(0, 0, 0)

	result, err := ibt.Run()
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Symbol != "BTCUSDT" || result.Strategy != "scripted" {
		t.Fatalf("result identity = %+v", result)
	}
	if len(result.Equity) != len(candles)*4 {
		t.Fatalf("equity length = %d, want %d", len(result.Equity), len(candles)*4)
	}
	if len(result.Trades) < 2 {
		t.Fatalf("trades length = %d, want buy/sell trades", len(result.Trades))
	}
	if result.FinalCapital <= 0 || result.PriceCurve == nil {
		t.Fatalf("result final capital/price curve = %.2f/%+v", result.FinalCapital, result.PriceCurve)
	}
}
