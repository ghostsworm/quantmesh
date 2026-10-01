package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/position"
)

type fakeComboSubStrategy struct {
	name         string
	prices       []float64
	started      bool
	stopped      bool
	eventBus     EventBus
	stats        *StrategyStatistics
	positions    []*Position
	riskPrices   []float64
	orders       []*Order
	visualData   map[string]interface{}
	startErr     error
	orderErr     error
	orderUpdates int
	mutateUpdate bool
	seenFeeKnown bool
	stateStore   RuntimeStateStore
}

func (f *fakeComboSubStrategy) Name() string { return f.name }
func (f *fakeComboSubStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	return nil
}
func (f *fakeComboSubStrategy) OnPriceChange(price float64) error {
	f.prices = append(f.prices, price)
	return nil
}
func (f *fakeComboSubStrategy) OnPriceChangeRiskOnly(price float64) error {
	f.riskPrices = append(f.riskPrices, price)
	return nil
}
func (f *fakeComboSubStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	f.orderUpdates++
	f.seenFeeKnown = update.CommissionKnown
	if f.mutateUpdate {
		update.CommissionKnown = true
		update.Commission = 42
	}
	return f.orderErr
}
func (f *fakeComboSubStrategy) GetPositions() []*Position          { return f.positions }
func (f *fakeComboSubStrategy) GetOrders() []*Order                { return f.orders }
func (f *fakeComboSubStrategy) GetStatistics() *StrategyStatistics { return f.stats }
func (f *fakeComboSubStrategy) Start(ctx context.Context) error {
	f.started = true
	return f.startErr
}

func TestComboStrategyStartPropagatesSubStrategyRecoveryFailureAndRollsBack(t *testing.T) {
	first := &fakeComboSubStrategy{name: "restored", stats: &StrategyStatistics{}}
	failure := errors.New("runtime state does not reconcile")
	second := &fakeComboSubStrategy{name: "corrupt", stats: &StrategyStatistics{}, startErr: failure}
	combo := &ComboStrategy{name: "combo", strategies: []Strategy{first, second}, strategyNames: []string{"restored", "corrupt"}}

	err := combo.Start(context.Background())
	if !errors.Is(err, failure) {
		t.Fatalf("sub-strategy recovery failure was swallowed: %v", err)
	}
	if !first.stopped || !second.stopped {
		t.Fatalf("failed startup was not rolled back: first=%v second=%v", first.stopped, second.stopped)
	}
	if combo.IsRunning() {
		t.Fatal("combo remained running after child recovery failed")
	}
}

func TestComboStrategyOnOrderUpdateReturnsChildErrorsAndContinuesDispatch(t *testing.T) {
	want := errors.New("child accounting requires reconciliation")
	failing := &fakeComboSubStrategy{name: "failing", orderErr: want, mutateUpdate: true}
	other := &fakeComboSubStrategy{name: "other"}
	combo := &ComboStrategy{name: "combo", strategies: []Strategy{failing, other}}

	err := combo.OnOrderUpdate(&position.OrderUpdate{OrderID: 88, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5})
	if !errors.Is(err, want) {
		t.Fatalf("OnOrderUpdate() error=%v, want child accounting error", err)
	}
	if other.orderUpdates != 1 {
		t.Fatalf("dispatch stopped after child failure; second child received %d updates", other.orderUpdates)
	}
	if other.seenFeeKnown {
		t.Fatal("second child observed the first child's mutation of the order update")
	}
}
func (f *fakeComboSubStrategy) Stop() error {
	f.stopped = true
	return nil
}
func (f *fakeComboSubStrategy) SetEventBus(bus EventBus) { f.eventBus = bus }
func (f *fakeComboSubStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	f.stateStore = store
}
func (f *fakeComboSubStrategy) GetVisualizationData() map[string]interface{} {
	return f.visualData
}

type comboStateCapture struct {
	values map[string]string
}

func (s *comboStateCapture) LoadRuntimeState(name string) (int, string, bool, error) {
	value, ok := s.values[name]
	return 1, value, ok, nil
}

func (s *comboStateCapture) SaveRuntimeState(name string, _ int, payload string) error {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[name] = payload
	return nil
}

func TestComboStrategyInjectsNamespacedRuntimeStateStores(t *testing.T) {
	store := &comboStateCapture{}
	makeCombo := func(name string) (*ComboStrategy, *fakeComboSubStrategy) {
		child := &fakeComboSubStrategy{name: "dca"}
		combo := &ComboStrategy{name: name, strategies: []Strategy{child}, strategyNames: []string{"dca"}}
		if err := combo.SetRuntimeStateStore(store); err != nil {
			t.Fatal(err)
		}
		if child.stateStore == nil {
			t.Fatal("combo child did not receive runtime state store")
		}
		return combo, child
	}
	_, first := makeCombo("combo-one")
	_, second := makeCombo("combo-two")
	if err := first.stateStore.SaveRuntimeState("dca", 1, "first"); err != nil {
		t.Fatal(err)
	}
	if err := second.stateStore.SaveRuntimeState("dca", 1, "second"); err != nil {
		t.Fatal(err)
	}
	if len(store.values) != 2 {
		t.Fatalf("combo children collided in durable state store: %#v", store.values)
	}
	for key, value := range store.values {
		if !strings.HasPrefix(key, "combo:") || (value != "first" && value != "second") {
			t.Fatalf("unexpected scoped runtime state entry %q=%q", key, value)
		}
	}
}

func TestComboStrategyRestoresAndPersistsPeakEquity(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	store := &comboStateCapture{}
	firstChild := &fakeComboSubStrategy{name: "dca", stats: &StrategyStatistics{}, positions: []*Position{{Symbol: "BTCUSDT", Size: 1, CurrentPrice: 100}}}
	first := &ComboStrategy{name: "combo", cfg: cfg, strategyCfg: &ComboConfig{Symbol: "BTCUSDT", TotalCapital: 100, MaxDrawdown: 10}, strategies: []Strategy{firstChild}, strategyNames: []string{"dca"}, weights: []float64{1}}
	if err := first.SetRuntimeStateStore(store); err != nil {
		t.Fatal(err)
	}
	if allowed, _ := first.checkComboRiskLimits(100); !allowed {
		t.Fatal("initial peak equity sample unexpectedly blocked")
	}
	if store.values["combo"] == "" {
		t.Fatal("combo peak equity was not persisted")
	}

	secondChild := &fakeComboSubStrategy{name: "dca", stats: &StrategyStatistics{}, positions: []*Position{{Symbol: "BTCUSDT", Size: 1, CurrentPrice: 80, PnL: -20}}}
	second := &ComboStrategy{name: "combo", cfg: cfg, strategyCfg: &ComboConfig{Symbol: "BTCUSDT", TotalCapital: 100, MaxDrawdown: 10}, strategies: []Strategy{secondChild}, strategyNames: []string{"dca"}, weights: []float64{1}}
	if err := second.SetRuntimeStateStore(store); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(t.Context()); err != nil {
		t.Fatalf("restore combo peak equity: %v", err)
	}
	if allowed, reason := second.checkComboRiskLimits(80); allowed || reason == "" {
		t.Fatalf("restored high-water drawdown did not block openings: allowed=%v reason=%q", allowed, reason)
	}
	if second.peakEquity != 100 {
		t.Fatalf("recovered peak equity = %v, want 100", second.peakEquity)
	}
}

type failingComboStateStore struct{ comboStateCapture }

func (s *failingComboStateStore) SaveRuntimeState(string, int, string) error {
	return errors.New("storage unavailable")
}

func TestComboStrategyStatePersistenceFailureReportsRiskHold(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	child := &fakeComboSubStrategy{name: "dca", stats: &StrategyStatistics{}}
	combo := &ComboStrategy{name: "combo", cfg: cfg, strategyCfg: &ComboConfig{Symbol: "BTCUSDT", TotalCapital: 100, MaxDrawdown: 10}, strategies: []Strategy{child}}
	store := &failingComboStateStore{}
	if err := combo.SetRuntimeStateStore(store); err != nil {
		t.Fatal(err)
	}
	var reported error
	combo.SetRuntimeStateErrorHandler(func(err error) { reported = err })
	if allowed, _ := combo.checkComboRiskLimits(100); allowed {
		t.Fatal("combo permitted opening after peak-equity persistence failed")
	}
	if reported == nil {
		t.Fatal("persistence failure was not reported to the shared opening gate handler")
	}
}

func TestComboStrategyMissingDrawdownStateRejectsRecoveredHistory(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	child := &fakeComboSubStrategy{name: "dca", stats: &StrategyStatistics{TotalTrades: 3}, positions: []*Position{{Symbol: "BTCUSDT", Size: 1}}}
	combo := &ComboStrategy{name: "combo", cfg: cfg, strategyCfg: &ComboConfig{Symbol: "BTCUSDT", TotalCapital: 100, MaxDrawdown: 10}, strategies: []Strategy{child}, strategyNames: []string{"dca"}, weights: []float64{1}}
	if err := combo.SetRuntimeStateStore(&comboStateCapture{}); err != nil {
		t.Fatal(err)
	}
	var gateErr error
	combo.SetRuntimeStateErrorHandler(func(err error) { gateErr = err })
	if err := combo.Start(t.Context()); err == nil {
		t.Fatal("missing combo checkpoint with restored economic history must reject startup")
	}
	if child.started && !child.stopped {
		t.Fatal("child with incomplete combo risk baseline was not rolled back")
	}
	if gateErr == nil || combo.runtimeStateFound {
		t.Fatalf("missing checkpoint did not remain blocked: gateErr=%v found=%v", gateErr, combo.runtimeStateFound)
	}
}

func TestComboStrategyInitialDrawdownBaselineIsPersistedBeforeOpening(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	store := &comboStateCapture{}
	child := &fakeComboSubStrategy{name: "dca", stats: &StrategyStatistics{}}
	combo := &ComboStrategy{name: "combo", cfg: cfg, strategyCfg: &ComboConfig{Symbol: "BTCUSDT", TotalCapital: 100, MaxDrawdown: 10}, strategies: []Strategy{child}, strategyNames: []string{"dca"}, weights: []float64{1}}
	if err := combo.SetRuntimeStateStore(store); err != nil {
		t.Fatal(err)
	}
	var gateCleared bool
	combo.SetRuntimeStateErrorHandler(func(err error) { gateCleared = err == nil })
	if err := combo.Start(t.Context()); err != nil {
		t.Fatalf("initialize new combo checkpoint: %v", err)
	}
	if store.values["combo"] == "" || combo.peakEquity != 100 || !gateCleared {
		t.Fatalf("initial baseline was not durably established before opening: state=%q peak=%v gateCleared=%v", store.values["combo"], combo.peakEquity, gateCleared)
	}
}

func TestComboRiskLimitsFailClosedOnInvalidEconomicEvidence(t *testing.T) {
	tests := []struct {
		name    string
		capital float64
		price   float64
		pos     *Position
		stats   *StrategyStatistics
	}{
		{name: "invalid capital", capital: math.NaN(), price: 100, stats: &StrategyStatistics{}},
		{name: "invalid mark price", capital: 100, price: math.NaN(), pos: &Position{Size: 1, PnL: 0}, stats: &StrategyStatistics{}},
		{name: "invalid size", capital: 100, price: 100, pos: &Position{Size: math.NaN(), CurrentPrice: 100}, stats: &StrategyStatistics{}},
		{name: "invalid unrealized pnl", capital: 100, price: 100, pos: &Position{Size: 1, CurrentPrice: 100, PnL: math.Inf(1)}, stats: &StrategyStatistics{}},
		{name: "invalid realized pnl", capital: 100, price: 100, stats: &StrategyStatistics{TotalPnL: math.NaN()}},
		{name: "notional overflow", capital: 100, price: 100, pos: &Position{Size: math.MaxFloat64, CurrentPrice: 2}, stats: &StrategyStatistics{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			child := &fakeComboSubStrategy{name: "child", stats: tc.stats}
			if tc.pos != nil {
				child.positions = []*Position{tc.pos}
			}
			combo := &ComboStrategy{strategyCfg: &ComboConfig{TotalCapital: tc.capital, MaxExposure: 0.5, MaxDrawdown: 10}, strategies: []Strategy{child}}
			allowed, reason := combo.checkComboRiskLimits(tc.price)
			if allowed || reason == "" {
				t.Fatalf("invalid risk evidence allowed opening: allowed=%v reason=%q", allowed, reason)
			}
		})
	}
}

func TestComboDrawdownRejectsMissingPositiveHighWaterBaseline(t *testing.T) {
	child := &fakeComboSubStrategy{name: "child", stats: &StrategyStatistics{TotalPnL: -200}}
	combo := &ComboStrategy{
		strategyCfg: &ComboConfig{TotalCapital: 100, MaxDrawdown: 10},
		strategies:  []Strategy{child},
	}
	if allowed, reason := combo.checkComboRiskLimits(100); allowed || !strings.Contains(reason, "高水位") {
		t.Fatalf("missing high-water baseline must block drawdown risk evaluation: allowed=%v reason=%q", allowed, reason)
	}
}

func TestComboStartupRejectsZeroPersistedDrawdownBaseline(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-a", "BTCUSDT"
	state, err := json.Marshal(comboRuntimeState{BotID: "bot-a", StrategyName: "combo", Symbol: "BTCUSDT", PeakEquity: 0})
	if err != nil {
		t.Fatal(err)
	}
	store := &comboStateCapture{values: map[string]string{"combo": string(state)}}
	child := &fakeComboSubStrategy{name: "child", stats: &StrategyStatistics{}}
	combo := &ComboStrategy{name: "combo", cfg: cfg, strategyCfg: &ComboConfig{Symbol: "BTCUSDT", TotalCapital: 100, MaxDrawdown: 10}, strategies: []Strategy{child}}
	if err := combo.SetRuntimeStateStore(store); err != nil {
		t.Fatal(err)
	}
	if err := combo.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "positive drawdown high-water") {
		t.Fatalf("startup must reject zero drawdown checkpoint: %v", err)
	}
	if child.started {
		t.Fatal("child strategy started despite corrupt drawdown checkpoint")
	}
}

type comboSubStrategyWithoutRiskOnly struct{ Strategy }

func TestComboRiskGatesRejectSubStrategiesWithoutRiskOnlySupport(t *testing.T) {
	child := &fakeComboSubStrategy{name: "custom", stats: &StrategyStatistics{}, positions: []*Position{{Symbol: "BTCUSDT", Size: 1}}}
	wrapped := &comboSubStrategyWithoutRiskOnly{Strategy: child}
	combo := &ComboStrategy{
		name: "combo", strategyCfg: &ComboConfig{Strategies: []StrategyConfig{{Name: "custom", PreferredMarket: []MarketState{MarketBullish}}}},
		strategies: []Strategy{wrapped}, strategyNames: []string{"custom"},
	}
	if err := combo.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "does not support risk-only") {
		t.Fatalf("combo should reject a gated child without risk-only mode: %v", err)
	}
	if child.started {
		t.Fatal("unsupported child was started before risk-only capability validation")
	}
	if err := combo.runRiskOnly(wrapped, 100); err == nil {
		t.Fatal("unsupported child with positions should report missing risk-only handling")
	}
	if len(child.prices) != 0 {
		t.Fatal("risk gating must not dispatch the unrestricted price handler")
	}
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
	// bear 不匹配市况但持有仓位：只运行 risk-only 退出检查，不派发完整开仓逻辑。
	if len(first.prices) != 1 || len(second.prices) != 0 || len(second.riskPrices) != 1 {
		t.Fatalf("unexpected gated strategy execution: first=%v second=%v riskOnly=%v", first.prices, second.prices, second.riskPrices)
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
