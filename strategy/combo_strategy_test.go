package strategy

import (
	"context"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/position"
)

type fakeComboSubStrategy struct {
	name       string
	prices     []float64
	started    bool
	stopped    bool
	eventBus   EventBus
	stats      *StrategyStatistics
	positions  []*Position
	orders     []*Order
	visualData map[string]interface{}
}

func (f *fakeComboSubStrategy) Name() string { return f.name }
func (f *fakeComboSubStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	return nil
}
func (f *fakeComboSubStrategy) OnPriceChange(price float64) error {
	f.prices = append(f.prices, price)
	return nil
}
func (f *fakeComboSubStrategy) OnOrderUpdate(update *position.OrderUpdate) error { return nil }
func (f *fakeComboSubStrategy) GetPositions() []*Position                        { return f.positions }
func (f *fakeComboSubStrategy) GetOrders() []*Order                              { return f.orders }
func (f *fakeComboSubStrategy) GetStatistics() *StrategyStatistics               { return f.stats }
func (f *fakeComboSubStrategy) Start(ctx context.Context) error {
	f.started = true
	return nil
}
func (f *fakeComboSubStrategy) Stop() error {
	f.stopped = true
	return nil
}
func (f *fakeComboSubStrategy) SetEventBus(bus EventBus) { f.eventBus = bus }
func (f *fakeComboSubStrategy) GetVisualizationData() map[string]interface{} {
	return f.visualData
}

func TestParseComboConfigDefaultsAndCustomValues(t *testing.T) {
	defaultCfg := parseComboConfig(nil)
	if defaultCfg.Symbol != "BTCUSDT" || len(defaultCfg.Strategies) != 0 {
		t.Fatalf("unexpected default combo config: %#v", defaultCfg)
	}

	custom := parseComboConfig(map[string]interface{}{
		"symbol":               "ETHUSDT",
		"market_detection":     false,
		"trend_period":         int64(8),
		"volatility_period":    5.0,
		"volatility_threshold": 2,
		"adaptive_weights":     false,
		"rebalance_interval":   30.0,
		"hedge_enabled":        false,
		"hedge_ratio":          0.2,
		"max_drawdown":         int64(7),
		"total_capital":        2500,
		"max_exposure":         0.5,
		"strategies": []interface{}{
			map[string]interface{}{
				"name":             "trend-one",
				"type":             "trend",
				"weight":           0.7,
				"direction":        "SHORT",
				"parameters":       map[string]interface{}{"lookback": 5},
				"preferred_market": []interface{}{"bearish", "volatile"},
			},
		},
	})

	if custom.Symbol != "ETHUSDT" || custom.MarketDetection || custom.TrendPeriod != 8 {
		t.Fatalf("custom basics not parsed: %#v", custom)
	}
	if custom.VolatilityPeriod != 5 || custom.VolatilityThreshold != 2 || custom.RebalanceInterval != 30 {
		t.Fatalf("custom numeric fields not parsed: %#v", custom)
	}
	if custom.HedgeEnabled || custom.HedgeRatio != 0.2 || custom.MaxDrawdown != 7 {
		t.Fatalf("custom hedge fields not parsed: %#v", custom)
	}
	if len(custom.Strategies) != 1 || custom.Strategies[0].PreferredMarket[0] != MarketBearish {
		t.Fatalf("custom strategies not parsed: %#v", custom.Strategies)
	}
}

func TestComboStrategyMarketStateWeightsAndExecution(t *testing.T) {
	first := &fakeComboSubStrategy{
		name:       "bull",
		stats:      &StrategyStatistics{TotalTrades: 2, WinRate: 0.5, TotalPnL: 10, TotalVolume: 100},
		positions:  []*Position{{Symbol: "BTCUSDT"}},
		orders:     []*Order{{OrderID: 1}},
		visualData: map[string]interface{}{"kind": "bull"},
	}
	second := &fakeComboSubStrategy{
		name:       "bear",
		stats:      &StrategyStatistics{TotalTrades: 3, WinRate: 1, TotalPnL: 15, TotalVolume: 150},
		positions:  []*Position{{Symbol: "ETHUSDT"}},
		orders:     []*Order{{OrderID: 2}},
		visualData: map[string]interface{}{"kind": "bear"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	combo := &ComboStrategy{
		name:          "combo",
		strategies:    []Strategy{first, second},
		strategyNames: []string{"bull", "bear"},
		weights:       []float64{0.8, 0.2},
		marketState:   MarketSideways,
		priceHistory:  make([]float64, 0, 200),
		candles:       make([]indicators.Candle, 0, 200),
		ctx:           ctx,
		cancel:        cancel,
		strategyCfg: &ComboConfig{
			Symbol:              "BTCUSDT",
			MarketDetection:     false,
			AdaptiveWeights:     false,
			TrendPeriod:         3,
			VolatilityPeriod:    3,
			VolatilityThreshold: 100,
			Strategies: []StrategyConfig{
				{Name: "bull", Weight: 0.8, PreferredMarket: []MarketState{MarketBullish}},
				{Name: "bear", Weight: 0.2, PreferredMarket: []MarketState{MarketBearish}},
			},
		},
		stats: &StrategyStatistics{},
	}

	if combo.Name() != "combo" {
		t.Fatalf("Name = %s, want combo", combo.Name())
	}
	if err := combo.Initialize(&config.Config{}, nil, nil); err != nil {
		t.Fatalf("Initialize returned error: %v", err)
	}
	if err := combo.Start(ctx); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	if !combo.IsRunning() || !first.started || !second.started {
		t.Fatal("combo and sub strategies should be running")
	}

	combo.marketState = MarketBullish
	if err := combo.OnPriceChange(100); err != nil {
		t.Fatalf("OnPriceChange returned error: %v", err)
	}
	if len(first.prices) != 1 || len(second.prices) != 0 {
		t.Fatalf("unexpected strategy execution: first=%v second=%v", first.prices, second.prices)
	}

	combo.detectMarketState()
	if combo.GetMarketState() != MarketBullish {
		t.Fatalf("market state with short history = %s, want unchanged bullish", combo.GetMarketState())
	}
	for i := 1; i <= 8; i++ {
		combo.priceHistory = append(combo.priceHistory, float64(100+i*3))
		combo.candles = append(combo.candles, indicators.Candle{
			Time:   int64(i),
			Open:   float64(100 + i*3),
			High:   float64(101 + i*3),
			Low:    float64(99 + i*3),
			Close:  float64(100 + i*3),
			Volume: 1,
		})
		combo.lastPrice = float64(100 + i*3)
	}
	combo.detectMarketState()
	if combo.GetMarketState() != MarketBullish {
		t.Fatalf("market state = %s, want bullish", combo.GetMarketState())
	}

	combo.rebalanceWeights()
	weights := combo.GetStrategyWeights()
	if weights["bull"] != 1.0 || weights["bear"] != 0.1 {
		t.Fatalf("unexpected weights after rebalance: %#v", weights)
	}

	stats := combo.GetStatistics()
	if stats.TotalTrades != 5 || stats.TotalPnL != 25 || stats.TotalVolume != 250 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if stats.WinRate != 0.8 {
		t.Fatalf("win rate = %f, want 0.8", stats.WinRate)
	}
	if len(combo.GetPositions()) != 2 || len(combo.GetOrders()) != 2 {
		t.Fatal("expected positions and orders from both sub strategies")
	}
	if data := combo.GetVisualizationData(); data["strategyCount"] != 2 || data["marketState"] != "bullish" {
		t.Fatalf("unexpected visualization data: %#v", data)
	}

	combo.SetEventBus(nil)
	if first.eventBus != nil || second.eventBus != nil {
		t.Fatal("event bus should be propagated")
	}
	combo.isPaused = true
	if err := combo.OnPriceChange(120); err != nil {
		t.Fatalf("paused OnPriceChange returned error: %v", err)
	}
	if len(first.prices) != 1 {
		t.Fatal("paused combo should not forward price changes")
	}
	if err := combo.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if combo.IsRunning() || !first.stopped || !second.stopped {
		t.Fatal("combo and sub strategies should be stopped")
	}
}

func TestComboStrategyUpdateCandleRollsWindow(t *testing.T) {
	combo := &ComboStrategy{candles: make([]indicators.Candle, 0, 201)}
	combo.updateCandle(100)
	if len(combo.candles) != 1 || combo.candles[0].Close != 100 {
		t.Fatalf("first candle = %#v", combo.candles)
	}
	combo.updateCandle(101)
	if combo.candles[0].High != 101 || combo.candles[0].Volume != 2 {
		t.Fatalf("updated candle = %#v", combo.candles[0])
	}

	for i := 0; i < 205; i++ {
		combo.candles = append(combo.candles, indicators.Candle{Time: time.Now().Add(-2 * time.Minute).Unix(), Close: float64(i)})
		combo.updateCandle(float64(i + 1))
	}
	if len(combo.candles) != 200 {
		t.Fatalf("rolled candle length = %d, want 200", len(combo.candles))
	}
}
