package replay

import (
	"math"
	"strings"
	"testing"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

func TestRunGridTask_CandleFallback(t *testing.T) {
	var candles []*exchange.Candle
	// 20 根 1 分鐘 K 線在 1980~2020 間來回，K 線內路徑穿越多個 10 USDT 網格
	for i := 0; i < 20; i++ {
		open := 2000.0
		close := 2005.0
		if i%2 == 1 {
			open, close = 2005, 2000
		}
		candles = append(candles, &exchange.Candle{Timestamp: testBaseTs + int64(i)*60000, Open: open, High: 2021, Low: 1979, Close: close, Volume: 50})
	}
	task := &backtest.BacktestTask{
		ID:           "bt_replay_candles",
		Strategy:     "grid",
		Symbol:       "ethusdt",
		TotalCapital: testCapital,
		Leverage:     2,
		Params: map[string]interface{}{
			backtest.ParamKeyEngine: backtest.EngineReplay,
			"grid_spacing":          10.0,
			"grid_count":            float64(3),
			"order_quantity":        100.0,
			"maker_fee_rate":        0.0002,
			"taker_fee_rate":        0.0005,
			"price_decimals":        float64(2),
			"quantity_decimals":     float64(3),
		},
	}
	res, extra, err := RunGridTask(task, candles)
	if err != nil {
		t.Fatalf("run grid task: %v", err)
	}
	m, ok := extra.(Metrics)
	if !ok {
		t.Fatalf("extra metrics type %T", extra)
	}
	if m.Fills == 0 || m.ClosedGrids == 0 || res.Metrics.TotalFees <= 0 {
		t.Fatalf("expected fills and closed grids on oscillating candles, got fills=%d closed=%d fees=%v", m.Fills, m.ClosedGrids, res.Metrics.TotalFees)
	}
	if res.Symbol != "ETHUSDT" || res.Strategy != "grid_replay" {
		t.Fatalf("unexpected result header: %s %s", res.Symbol, res.Strategy)
	}

	cfg, err := ConfigFromTask(&backtest.BacktestTask{Symbol: "ETHUSDT", Params: map[string]interface{}{}})
	if err == nil {
		t.Fatalf("missing grid_spacing must be rejected, got config %+v", cfg)
	}
}

func TestConfigFromTask_FeatureParams(t *testing.T) {
	base := func(extra map[string]interface{}) *backtest.BacktestTask {
		p := map[string]interface{}{"grid_spacing": 10.0}
		for k, v := range extra {
			p[k] = v
		}
		return &backtest.BacktestTask{Symbol: "ETHUSDT", Params: p}
	}

	// 缺省：新功能全部關閉，fee_aware_spread 按配置默認開啟，無資金費
	cfg, err := ConfigFromTask(base(nil))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	tr := cfg.Bot.Trading
	if tr.RegimeFilter.Enabled || tr.AdaptiveInterval.Enabled || tr.UpperBoundFreeze.Enabled || tr.InventorySkew.Enabled ||
		cfg.Bot.FundingRate.Enabled || cfg.FundingEnabled || !tr.FeeAwareSpread.IsEnabled() {
		t.Fatalf("unexpected defaults: %+v funding=%+v", tr, cfg.Bot.FundingRate)
	}

	// 布爾簡寫與對象形式（與配置文件同鍵）
	cfg, err = ConfigFromTask(base(map[string]interface{}{
		"regime_filter":       true,
		"adaptive_interval":   map[string]interface{}{"enabled": true, "atr_multiplier": 0.8},
		"upper_bound_freeze":  "true",
		"inventory_skew":      map[string]interface{}{"enabled": true, "strength": 0.3},
		"fee_aware_spread":    false,
		"max_position_layers": float64(20),
		"funding_pricing":     true,
		"funding_rate":        0.0003,
	}))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	tr = cfg.Bot.Trading
	if !tr.RegimeFilter.Enabled || !tr.AdaptiveInterval.Enabled || tr.AdaptiveInterval.ATRMultiplier != 0.8 || !tr.UpperBoundFreeze.Enabled ||
		!tr.InventorySkew.Enabled || tr.InventorySkew.Strength != 0.3 || tr.FeeAwareSpread.IsEnabled() || tr.OpenPositionControl.MaxPositionLayers != 20 {
		t.Fatalf("feature params not applied: %+v", tr)
	}
	if !cfg.Bot.FundingRate.Enabled || !cfg.Bot.FundingRate.PricingEnabled || cfg.FundingRate != 0.0003 || !cfg.FundingEnabled {
		t.Fatalf("funding pricing shorthand not applied: %+v rate=%v enabled=%v", cfg.Bot.FundingRate, cfg.FundingRate, cfg.FundingEnabled)
	}

	// funding_rate 為配置段對象時，固定費率取 funding_constant_rate；funding_series 解析並排序
	cfg, err = ConfigFromTask(base(map[string]interface{}{
		"funding_rate":          map[string]interface{}{"enabled": true, "bias_enabled": true, "pricing_enabled": true},
		"funding_constant_rate": 0.0001,
		"funding_series": []interface{}{
			map[string]interface{}{"timestamp": float64(testBaseTs + 8*3600*1000), "rate": 0.0002, "mark_price": 150.0},
			map[string]interface{}{"timestamp": float64(testBaseTs), "rate": 0.0001},
		},
	}))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !cfg.Bot.FundingRate.BiasEnabled || !cfg.Bot.FundingRate.PricingEnabled || cfg.FundingRate != 0.0001 ||
		len(cfg.FundingSeries) != 2 || cfg.FundingSeries[0].Timestamp != testBaseTs || !cfg.FundingEnabled {
		t.Fatalf("funding object/series not applied: %+v rate=%v series=%+v", cfg.Bot.FundingRate, cfg.FundingRate, cfg.FundingSeries)
	}
	if cfg.FundingSeries[1].MarkPrice == nil || *cfg.FundingSeries[1].MarkPrice != 150 {
		t.Fatalf("settlement mark price was not preserved: %+v", cfg.FundingSeries[1])
	}

	for name, extra := range map[string]map[string]interface{}{
		"unknown field":      {"regime_filter": map[string]interface{}{"enabeld": true}},
		"bad type":           {"adaptive_interval": 3.0},
		"skew out of range":  {"inventory_skew": map[string]interface{}{"enabled": true, "strength": 1.5}},
		"bad kline interval": {"regime_filter": map[string]interface{}{"enabled": true, "kline_interval": "7s"}},
		"pricing object":     {"funding_pricing": map[string]interface{}{"enabled": true}},
		"bad series":         {"funding_series": []interface{}{map[string]interface{}{"ts": 1.0}}},
		"duplicate series settlement": {"funding_series": []interface{}{
			map[string]interface{}{"timestamp": float64(testBaseTs), "rate": 0.001},
			map[string]interface{}{"timestamp": float64(testBaseTs), "rate": 0.002},
		}},
		"non-finite series rate": {"funding_series": []interface{}{
			map[string]interface{}{"timestamp": float64(testBaseTs), "rate": math.Inf(1)},
		}},
		"invalid mark price": {"funding_series": []interface{}{
			map[string]interface{}{"timestamp": float64(testBaseTs), "rate": 0.001, "mark_price": 0.0},
		}},
	} {
		if _, err := ConfigFromTask(base(extra)); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

// oscillatingTrendCandles 1m K 線：緩慢上漲並帶正弦擺動，K 線內路徑穿越多個網格
func oscillatingTrendCandles(n int) []*exchange.Candle {
	out := make([]*exchange.Candle, 0, n)
	prev := 2000.0
	for i := 0; i < n; i++ {
		drift := 2000.0 * math.Pow(1.0002, float64(i))
		cl := drift + drift*0.006*math.Sin(float64(i)/5)
		hi, lo := math.Max(prev, cl)*1.001, math.Min(prev, cl)*0.999
		out = append(out, &exchange.Candle{Timestamp: testBaseTs + int64(i)*60000, Open: prev, High: hi, Low: lo, Close: cl, Volume: 50})
		prev = cl
	}
	return out
}

func replayFeatureTask(id string, extra map[string]interface{}) *backtest.BacktestTask {
	p := map[string]interface{}{
		backtest.ParamKeyEngine: backtest.EngineReplay,
		"grid_spacing":          4.0,
		"grid_count":            float64(5),
		"order_quantity":        50.0,
		"price_decimals":        float64(2),
		"quantity_decimals":     float64(3),
		"fee_aware_spread":      false,
	}
	for k, v := range extra {
		p[k] = v
	}
	return &backtest.BacktestTask{ID: id, Strategy: "grid", Symbol: "ETHUSDT", TotalCapital: testCapital, Leverage: 5, Params: p}
}

func runFeatureTask(t *testing.T, id string, candles []*exchange.Candle, extra map[string]interface{}) (*backtest.BacktestResult, Metrics) {
	t.Helper()
	res, extraOut, err := RunGridTask(replayFeatureTask(id, extra), candles)
	if err != nil {
		t.Fatalf("%s: run: %v", id, err)
	}
	m, ok := extraOut.(Metrics)
	if !ok {
		t.Fatalf("%s: extra metrics type %T", id, extraOut)
	}
	if m.Features == nil {
		t.Fatalf("%s: feature report missing", id)
	}
	return res, m
}

// Web 回測（RunGridTask）接入 regime 檢測器與自適應間隔：K 線由任務 K 線聚合，預熱後狀態可用、間隔被調整
func TestRunGridTask_RegimeFeaturesWired(t *testing.T) {
	candles := oscillatingTrendCandles(600)
	regimeCfg := map[string]interface{}{"enabled": false, "kline_interval": "1m"}

	_, plain := runFeatureTask(t, "bt_plain", candles, nil)
	if plain.Features.Stats.RegimeEnabled || plain.Features.RegimeSharePct != nil {
		t.Fatalf("regime must stay off by default: %+v", plain.Features)
	}

	_, m := runFeatureTask(t, "bt_adaptive", candles, map[string]interface{}{
		"regime_filter":     regimeCfg,
		"adaptive_interval": map[string]interface{}{"enabled": true, "atr_multiplier": 2.0},
	})
	f := m.Features
	if !f.AdaptiveInterval || f.RegimeFilter || f.RegimeKlineInterval != "1m" || f.RegimeKlineBars != len(candles) {
		t.Fatalf("unexpected report header: %+v", f)
	}
	if f.RegimeRequiredBars <= 0 || f.RegimeKlineBars < f.RegimeRequiredBars {
		t.Fatalf("test needs enough warm-up bars: %+v", f)
	}
	if f.RegimeSharePct == nil || f.RegimeSharePct["unknown"] >= 90 {
		t.Fatalf("detector should become ready after warm-up: share=%v errors=%d last=%q", f.RegimeSharePct, f.Stats.RefreshErrors, f.Stats.LastRefreshError)
	}
	if f.Stats.IntervalChanges == 0 || f.MeanIntervalMultiple <= 1 {
		t.Fatalf("adaptive interval should widen a 4 USDT grid on volatile candles: changes=%d multiple=%.3f", f.Stats.IntervalChanges, f.MeanIntervalMultiple)
	}
	if m.Fills == plain.Fills && m.NetPnL == plain.NetPnL {
		t.Fatalf("adaptive interval should change the replay (fills=%d net=%.4f)", m.Fills, m.NetPnL)
	}

	// 預熱不足時仍可運行，但在結果中披露
	_, short := runFeatureTask(t, "bt_short", candles[:60], map[string]interface{}{"regime_filter": map[string]interface{}{"enabled": true, "kline_interval": "1m"}})
	if len(short.Features.Notes) == 0 || short.Features.RegimeSharePct["unknown"] != 100 {
		t.Fatalf("insufficient warm-up must be disclosed: %+v", short.Features)
	}

	// K 線周期比檢測器周期粗時無法聚合，任務失敗並說明原因
	hourly, err := AggregateCandles(candles, 3600*1000)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	if _, _, err := RunGridTask(replayFeatureTask("bt_coarse", map[string]interface{}{"upper_bound_freeze": true, "regime_filter": regimeCfg}), hourly); err == nil {
		t.Fatalf("1h candles cannot feed a 1m detector, want error")
	}
}

// Web 回測接入資金費定價：沒有資金費率序列時使用固定費率並在結果中披露
func TestRunGridTask_FundingPricingConstantRateDisclosed(t *testing.T) {
	candles := oscillatingTrendCandles(240)
	plainRes, _ := runFeatureTask(t, "bt_funding_off", candles, map[string]interface{}{"funding_rate": 0.004})
	pricedRes, m := runFeatureTask(t, "bt_funding_on", candles, map[string]interface{}{"funding_rate": 0.004, "funding_pricing": true})

	f := m.Features
	if !f.FundingPricing || f.FundingSource != FundingSourceConstant || f.FundingConstantRate != 0.004 || !f.Stats.FundingMonitorEnabled {
		t.Fatalf("unexpected funding report: %+v", f)
	}
	disclosed := false
	for _, n := range f.Notes {
		if strings.Contains(n, "固定") {
			disclosed = true
		}
	}
	if !disclosed {
		t.Fatalf("constant funding rate must be disclosed, notes=%v", f.Notes)
	}
	offGrid := func(res *backtest.BacktestResult) int {
		n := 0
		for _, tr := range res.Trades {
			if tr.Type == "buy" && math.Abs(math.Remainder(tr.Price, 4.0)) > 1e-6 {
				n++
			}
		}
		return n
	}
	if offGrid(pricedRes) == 0 {
		t.Fatalf("funding pricing should move paying-side buys off the grid")
	}
	if n := offGrid(plainRes); n != 0 {
		t.Fatalf("without funding pricing buys must stay on the grid, got %d off-grid", n)
	}

	// 提供資金費率序列時使用序列
	_, withSeries := runFeatureTask(t, "bt_funding_series", candles, map[string]interface{}{
		"funding_pricing": true,
		"funding_series":  []interface{}{map[string]interface{}{"timestamp": float64(testBaseTs - 1000), "rate": 0.004}},
	})
	if withSeries.Features.FundingSource != FundingSourceSeries || withSeries.Features.FundingSeriesPoints != 1 {
		t.Fatalf("series source not reported: %+v", withSeries.Features)
	}
}

func TestConfigFromTask_OrderCleanerDefaults(t *testing.T) {
	cfg, err := ConfigFromTask(&backtest.BacktestTask{Symbol: "ETHUSDT", Params: map[string]interface{}{"grid_spacing": 10.0}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if !cfg.OrderCleaner || cfg.Bot.Timing.OrderCleanupInterval != defaultTaskOrderCleanupIntervalSec {
		t.Fatalf("task default must run the order cleaner every %ds, got enabled=%v interval=%d",
			defaultTaskOrderCleanupIntervalSec, cfg.OrderCleaner, cfg.Bot.Timing.OrderCleanupInterval)
	}
	cfg, err = ConfigFromTask(&backtest.BacktestTask{Symbol: "ETHUSDT", Params: map[string]interface{}{
		"grid_spacing": 10.0, "order_cleaner": false, "order_cleanup_threshold": float64(40), "cleanup_batch_size": float64(5), "order_cleanup_interval": float64(15),
	}})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if cfg.OrderCleaner || cfg.Bot.Trading.OrderCleanupThreshold != 40 || cfg.Bot.Trading.CleanupBatchSize != 5 || cfg.Bot.Timing.OrderCleanupInterval != 15 {
		t.Fatalf("explicit cleaner params not applied: enabled=%v threshold=%d batch=%d interval=%d", cfg.OrderCleaner,
			cfg.Bot.Trading.OrderCleanupThreshold, cfg.Bot.Trading.CleanupBatchSize, cfg.Bot.Timing.OrderCleanupInterval)
	}
}
