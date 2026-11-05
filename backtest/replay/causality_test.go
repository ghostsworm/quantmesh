package replay

import (
	"context"
	"reflect"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
)

func TestClosedKlineSourceWarmupExcludesUnclosedAndFutureBars(t *testing.T) {
	boundary := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	clock := NewSimClock(boundary.Add(-time.Millisecond))
	bars := []*exchange.Candle{
		boundedCandle(boundary.Add(-2 * time.Hour)),
		boundedCandle(boundary.Add(-time.Hour)),
		boundedCandle(boundary),
	}
	source, err := NewClosedKlineSource(testSymbol, "1h", bars, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	before, err := source.GetHistoricalKlines(context.Background(), testSymbol, "1h", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0].Timestamp != bars[0].Timestamp {
		t.Fatalf("warm-up saw an unclosed candle: %+v", before)
	}
	// 改動未收盤及未來 K 線不得改變同一歷史前綴的指標輸入。
	bars[1].Close, bars[1].High, bars[2].Close = 9000, 10000, 12000
	after, err := source.GetHistoricalKlines(context.Background(), testSymbol, "1h", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("future suffix changed warm-up history")
	}
	clock.AdvanceTo(boundary)
	closed, err := source.GetHistoricalKlines(context.Background(), testSymbol, "1h", 10)
	if err != nil || len(closed) != 2 || closed[1].Close != 9000 {
		t.Fatalf("bar must become visible exactly at close: bars=%+v error=%v", closed, err)
	}
}

func TestEngineSleepSkipsStrategyButPreservesMarketAccounting(t *testing.T) {
	start := time.Date(2026, 8, 1, 7, 59, 59, 0, time.UTC)
	cfg := testConfig(MatchingConfig{})
	var clock *SimClock
	var monitor *SimFundingMonitor
	var seen []int64
	cfg.Setup = func(_ *position.SuperPositionManager, injected position.Clock) error {
		clock = injected.(*SimClock)
		monitor = NewSimFundingMonitor(config.FundingRateConfig{}, []FundingPoint{{Timestamp: start.Add(time.Second).UnixMilli(), Rate: 0.9}}, 0.001, clock)
		return nil
	}
	cfg.OnTick = func(now time.Time, _ float64) {
		seen = append(seen, now.UnixMilli())
		if len(seen) == 2 {
			clock.Sleep(2 * time.Second)
			if got := monitor.GetCurrentRate(); got != 0.001 {
				t.Fatalf("Sleep exposed future settlement rate: %v", got)
			}
			if !clock.MarketNow().Equal(now) || !clock.Now().Equal(now.Add(2*time.Second)) {
				t.Fatal("market time must stay fixed while operation clock advances")
			}
		}
	}
	ticks := []Tick{
		{Timestamp: start.UnixMilli(), Price: 2000, Quantity: 10},
		{Timestamp: start.Add(500 * time.Millisecond).UnixMilli(), Price: 2000, Quantity: 10},
		{Timestamp: start.Add(time.Second).UnixMilli(), Price: 1989, Quantity: 10},
		{Timestamp: start.Add(2500 * time.Millisecond).UnixMilli(), Price: 2000, Quantity: 10},
	}
	engine := NewEngine(cfg)
	result, err := engine.RunContext(context.Background(), ticks)
	if err != nil {
		t.Fatal(err)
	}
	wantSeen := []int64{ticks[0].Timestamp, ticks[1].Timestamp, ticks[3].Timestamp}
	if !reflect.DeepEqual(seen, wantSeen) || engine.adjustCalls != 2 {
		t.Fatalf("old ticks refreshed strategy: seen=%v adjustCalls=%d", seen, engine.adjustCalls)
	}
	if result.Metrics.Fills == 0 {
		t.Fatal("resting orders must still fill during strategy wait")
	}
	if got := monitor.GetCurrentRate(); got != 0.9 {
		t.Fatalf("processed settlement stayed invisible: %v", got)
	}
	if clock.Now().UnixMilli() != ticks[3].Timestamp {
		t.Fatal("operation clock regressed or drifted")
	}
}

func TestFeatureHooksSleepDoesNotFinishWarmupEarly(t *testing.T) {
	cfg := testConfig(MatchingConfig{})
	cfg.Bot.Trading.RegimeFilter.Enabled = true
	cfg.Bot.Trading.RegimeFilter.KlineInterval = "1m"
	required := position.RegimeConfigFromConfig(cfg.Bot.Trading.RegimeFilter).WithDefaults().RequiredBars()
	boundary := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	bars := oscillatingTrendCandles(required)
	for i, bar := range bars {
		bar.Timestamp = boundary.Add(-time.Duration(required-i) * time.Minute).UnixMilli()
	}
	stats, err := InstallFeatureHooks(&cfg, FeatureOptions{RegimeKlines: bars})
	if err != nil {
		t.Fatal(err)
	}
	setup, onTick := cfg.Setup, cfg.OnTick
	var clock *SimClock
	cfg.Setup = func(spm *position.SuperPositionManager, injected position.Clock) error {
		clock = injected.(*SimClock)
		return setup(spm, injected)
	}
	calls := 0
	cfg.OnTick = func(now time.Time, price float64) {
		calls++
		if calls == 1 {
			clock.Sleep(2 * time.Second)
		}
		onTick(now, price)
		if calls == 1 && (stats.RefreshErrors != 0 || stats.RegimeCounts["unknown"] != 1) {
			t.Fatalf("unclosed final warm-up candle was visible after Sleep: %+v", stats)
		}
	}
	// 第一個 tick 還差 1s 才有足夠已閉合 K 線；Sleep 雖跨過閉合點，也不能提前完成預熱。
	ticks := []Tick{
		{Timestamp: boundary.Add(-time.Second).UnixMilli(), Price: 2000, Quantity: 10},
		{Timestamp: boundary.Add(-500 * time.Millisecond).UnixMilli(), Price: 2000, Quantity: 10},
		{Timestamp: boundary.Add(time.Minute).UnixMilli(), Price: 2000, Quantity: 10},
	}
	if _, err := NewEngine(cfg).RunContext(context.Background(), ticks); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || stats.RefreshErrors != 0 || stats.Samples != 2 || stats.RegimeCounts["unknown"] != 1 {
		t.Fatalf("warm-up must recover only after market time reaches close: calls=%d stats=%+v", calls, stats)
	}
}
