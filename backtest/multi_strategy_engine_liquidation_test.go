package backtest

import (
	"math"
	"testing"
)

type liquidationRegressionStrategy struct {
	account *BacktestAccount
	side    string
	id      string
	checks  int
	fills   int
}

func (s *liquidationRegressionStrategy) OnInit(account *BacktestAccount, _ interface{}) error {
	s.account = account
	return nil
}

func (s *liquidationRegressionStrategy) OnKline(kline TickKline, _ int64) ([]TickOrder, error) {
	s.checks++
	if kline.Timestamp != 1000 {
		return nil, nil
	}
	return []TickOrder{{OrderID: "open", Side: s.side, Price: 100, Size: 2}}, nil
}

func (s *liquidationRegressionStrategy) OnTrade(TickTrade) { s.fills++ }
func (s *liquidationRegressionStrategy) GetName() string   { return s.id }
func (*liquidationRegressionStrategy) GetType() string     { return "test" }
func (s *liquidationRegressionStrategy) GetConfig() map[string]interface{} {
	return map[string]interface{}{"total_capital": 100.0, "strategy_id": s.id}
}

func runLiquidationRegression(t *testing.T, side string, klines []TickKline) (*MultiStrategyResult, *liquidationRegressionStrategy) {
	t.Helper()
	cfg := validEngineConfigForValidation()
	cfg.InitialCapital = 100
	cfg.Leverage = 2
	cfg.CommissionRate = 0.001
	engine := NewMultiStrategyEngine(cfg)
	engine.Klines = klines
	strategy := &liquidationRegressionStrategy{side: side, id: "liquidation-regression"}
	if err := engine.AddStrategy(strategy); err != nil {
		t.Fatalf("AddStrategy() error = %v", err)
	}
	result, err := engine.Run()
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	return result, strategy
}

func TestMultiStrategyResultKeepsEveryStrategyLiquidation(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.InitialCapital = 200
	cfg.Leverage = 2
	engine := NewMultiStrategyEngine(cfg)
	engine.Klines = []TickKline{
		{Timestamp: 1000, Open: 101, High: 102, Low: 99, Close: 100, Volume: 100},
		{Timestamp: 61000, Open: 100, High: 101, Low: 49, Close: 50, Volume: 100},
	}
	for _, id := range []string{"liq-one", "liq-two"} {
		if err := engine.AddStrategy(&liquidationRegressionStrategy{side: "buy", id: id}); err != nil {
			t.Fatalf("AddStrategy(%s) error = %v", id, err)
		}
	}
	result, err := engine.Run()
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Liquidations) != 2 || len(result.EndSettlement.Liquidations) != 2 {
		t.Fatalf("result did not retain all liquidations: %+v", result.EndSettlement)
	}
	if result.Liquidations[0].StrategyID == result.Liquidations[1].StrategyID {
		t.Fatalf("liquidation strategy identities are not distinct: %+v", result.Liquidations)
	}
}

func TestMultiStrategyLiquidationDetailsDoNotAliasDuplicateStrategyIDs(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.InitialCapital = 200
	cfg.Leverage = 2
	engine := NewMultiStrategyEngine(cfg)
	engine.Klines = []TickKline{
		{Timestamp: 1000, Open: 101, High: 102, Low: 99, Close: 100, Volume: 100},
		{Timestamp: 61000, Open: 100, High: 151, Low: 49, Close: 100, Volume: 100},
	}
	for _, side := range []string{"buy", "sell"} {
		if err := engine.AddStrategy(&liquidationRegressionStrategy{side: side, id: "duplicate-id"}); err != nil {
			t.Fatalf("AddStrategy(%s) error = %v", side, err)
		}
	}
	result, err := engine.Run()
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(result.Liquidations) != 2 {
		t.Fatalf("liquidation count = %d, want two: %+v", len(result.Liquidations), result.Liquidations)
	}
	long, short := result.Liquidations[0], result.Liquidations[1]
	if long.TriggerPrice != 49 || long.ExecutionPrice != 49*cfg.MatcherConfig.SellSlippage || long.Fee <= 0 || long.Slippage <= 0 {
		t.Fatalf("long liquidation detail was overwritten: %+v", long)
	}
	if short.TriggerPrice != 151 || short.ExecutionPrice != 151*cfg.MatcherConfig.BuySlippage || short.Fee <= 0 || short.Slippage <= 0 {
		t.Fatalf("short liquidation detail was lost or aliased: %+v", short)
	}
}

func TestMultiStrategyLiquidationUsesAdverseIntrabarExtreme(t *testing.T) {
	base := TickKline{Timestamp: 1000, Open: 101, High: 102, Low: 99, Close: 100, Volume: 100}
	cases := []struct {
		name      string
		side      string
		bar       TickKline
		wantPrice float64
	}{
		{name: "long downside wick", side: "buy", bar: TickKline{Timestamp: 61000, Open: 100, High: 101, Low: 49, Close: 100, Volume: 100}, wantPrice: 49},
		{name: "short upside wick", side: "sell", bar: TickKline{Timestamp: 61000, Open: 100, High: 151, Low: 99, Close: 100, Volume: 100}, wantPrice: 151},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, _ := runLiquidationRegression(t, tc.side, []TickKline{base, tc.bar})
			if len(result.Liquidations) != 1 || result.Liquidations[0].TriggerPrice != tc.wantPrice {
				t.Fatalf("liquidation from adverse OHLC extreme = %+v, want trigger %.2f", result.Liquidations, tc.wantPrice)
			}
		})
	}
}

func TestMultiStrategyRejectedMatcherTradeDoesNotReachOnTrade(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.PositionMode = "LONG"
	engine := NewMultiStrategyEngine(cfg)
	engine.Klines = []TickKline{{Timestamp: 1000, Open: 101, High: 102, Low: 99, Close: 100, Volume: 100}}
	strategy := &liquidationRegressionStrategy{side: "sell", id: "long-only-rejection"}
	if err := engine.AddStrategy(strategy); err != nil {
		t.Fatalf("AddStrategy() error = %v", err)
	}
	result, err := engine.Run()
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if strategy.fills != 0 || len(result.Trades) != 0 {
		t.Fatalf("rejected trade leaked into strategy callbacks/results: fills=%d trades=%+v", strategy.fills, result.Trades)
	}
}

func TestMultiStrategyLiquidationClosesAtTriggerAndCannotRecover(t *testing.T) {
	base := TickKline{Timestamp: 1000, Open: 101, High: 102, Low: 99, Close: 100, Volume: 100}
	tests := []struct {
		name         string
		side         string
		trigger      TickKline
		triggerPrice float64
		recovery     TickKline
		wantAdverse  func(LiquidationEvent) bool
	}{
		{
			name: "long crash then recovery", side: "buy",
			triggerPrice: 49,
			trigger:      TickKline{Timestamp: 61000, Open: 100, High: 101, Low: 49, Close: 50, Volume: 100},
			recovery:     TickKline{Timestamp: 121000, Open: 50, High: 201, Low: 49, Close: 200, Volume: 100},
			wantAdverse:  func(event LiquidationEvent) bool { return event.ExecutionPrice < event.TriggerPrice },
		},
		{
			name: "short squeeze then reversal", side: "sell",
			triggerPrice: 151,
			trigger:      TickKline{Timestamp: 61000, Open: 100, High: 151, Low: 99, Close: 150, Volume: 100},
			recovery:     TickKline{Timestamp: 121000, Open: 150, High: 151, Low: 49, Close: 50, Volume: 100},
			wantAdverse:  func(event LiquidationEvent) bool { return event.ExecutionPrice > event.TriggerPrice },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withoutRecovery, _ := runLiquidationRegression(t, tc.side, []TickKline{base, tc.trigger})
			withRecovery, strategy := runLiquidationRegression(t, tc.side, []TickKline{base, tc.trigger, tc.recovery})
			if !withRecovery.EndSettlement.Liquidated || len(withRecovery.Liquidations) != 1 {
				t.Fatalf("liquidation result = %+v, want one liquidation", withRecovery.EndSettlement)
			}
			event := withRecovery.Liquidations[0]
			if event.StrategyID != "liquidation-regression" || event.Timestamp != tc.trigger.Timestamp {
				t.Fatalf("liquidation identity/time = %+v", event)
			}
			if event.TriggerPrice != tc.triggerPrice || !tc.wantAdverse(event) || event.Fee <= 0 || event.Slippage <= 0 {
				t.Fatalf("liquidation costs/prices = %+v", event)
			}
			if math.Abs(withRecovery.FinalEquity-withoutRecovery.FinalEquity) > 1e-9 {
				t.Fatalf("recovery candle changed liquidated equity: with=%.8f without=%.8f", withRecovery.FinalEquity, withoutRecovery.FinalEquity)
			}
			if withRecovery.StrategyResults[0].OpenPositionSize != 0 || !strategy.account.Liquidated {
				t.Fatalf("post liquidation account state: position=%v liquidated=%v", withRecovery.StrategyResults[0].OpenPositionSize, strategy.account.Liquidated)
			}
			if strategy.checks != 1 {
				t.Fatalf("strategy received %d K-lines after liquidation; only the pre-liquidation K-line should be delivered", strategy.checks)
			}
			if len(withRecovery.EndSettlement.Liquidations) != 1 {
				t.Fatalf("end settlement omitted per-strategy liquidation: %+v", withRecovery.EndSettlement)
			}
		})
	}
}
