package replay

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/position"
	"quantmesh/strategy/regime"
)

func TestEngine_SetupAndOnTickHooks(t *testing.T) {
	ticks := ticksFromPrices(touchAndCrossPrices, 10)
	cfg := testConfig(MatchingConfig{})
	var setupCalls int
	var setupClockNow time.Time
	var seen []time.Time
	var eng *Engine
	cfg.Setup = func(spm *position.SuperPositionManager, clock position.Clock) error {
		setupCalls++
		if spm == nil || clock == nil {
			return errors.New("nil spm or clock")
		}
		if _, ok := clock.(*SimClock); !ok {
			return errors.New("clock is not the engine SimClock")
		}
		if spm.Clock() != clock {
			return errors.New("setup called before the sim clock was injected")
		}
		// Initialize 尚未調用：還沒有任何下單
		if eng.ex.stats.ordersPlaced != 0 {
			return errors.New("setup called after Initialize")
		}
		setupClockNow = clock.Now()
		return nil
	}
	adjustBefore := -1
	cfg.OnTick = func(now time.Time, price float64) {
		seen = append(seen, now)
		if len(seen) == 1 {
			adjustBefore = eng.adjustCalls
		}
	}
	eng = NewEngine(cfg)
	if _, err := eng.Run(ticks); err != nil {
		t.Fatalf("run: %v", err)
	}
	if setupCalls != 1 || !setupClockNow.Equal(time.UnixMilli(ticks[0].Timestamp)) {
		t.Fatalf("setup calls=%d clock=%v, want 1 call at first tick time", setupCalls, setupClockNow)
	}
	if len(seen) != len(ticks) {
		t.Fatalf("OnTick calls=%d, want %d", len(seen), len(ticks))
	}
	for i, ts := range seen {
		if !ts.Equal(time.UnixMilli(ticks[i].Timestamp)) {
			t.Fatalf("OnTick[%d] now=%v, want tick time %v", i, ts, time.UnixMilli(ticks[i].Timestamp))
		}
	}
	if adjustBefore != 0 {
		t.Fatalf("first OnTick must run before the first AdjustOrders, adjustCalls=%d", adjustBefore)
	}

	cfg2 := testConfig(MatchingConfig{})
	wantErr := errors.New("boom")
	cfg2.Setup = func(*position.SuperPositionManager, position.Clock) error { return wantErr }
	if _, err := RunTicks(cfg2, ticks); !errors.Is(err, wantErr) {
		t.Fatalf("setup error must fail Run, got %v", err)
	}
}

func TestSimFundingMonitor_NoLookaheadRateAndSettlement(t *testing.T) {
	h := int64(time.Hour / time.Millisecond)
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	series := []FundingPoint{{Timestamp: day + 8*h, Rate: 0.0002}, {Timestamp: day, Rate: 0.0001}, {Timestamp: day + 16*h, Rate: -0.0003}}
	clk := NewSimClock(time.UnixMilli(day - h))
	m := NewSimFundingMonitor(config.FundingRateConfig{}, series, 0.00005, clk)

	cases := []struct {
		name     string
		at       int64
		rate     float64
		nextHour int64
	}{
		{"before series uses fallback", day - h, 0.00005, 0},
		{"exactly at settlement sees that settlement", day, 0.0001, 8},
		{"between settlements keeps last settled", day + 7*h, 0.0001, 8},
		{"second settlement", day + 8*h + 1, 0.0002, 16},
		{"negative rate", day + 20*h, -0.0003, 24},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk.AdvanceToMillis(tc.at)
			if got := m.GetCurrentRate(); math.Abs(got-tc.rate) > testFloatDelta {
				t.Fatalf("rate at %d = %v, want %v", tc.at, got, tc.rate)
			}
			if got := m.GetNextFundingTime().UnixMilli(); got != day+tc.nextHour*h {
				t.Fatalf("next funding = %v, want %v", time.UnixMilli(got).UTC(), time.UnixMilli(day+tc.nextHour*h).UTC())
			}
			if m.GetBuyBias() != 1 || m.GetSellBias() != 1 || m.ShouldPauseBuying() {
				t.Fatalf("bias must be neutral when bias_enabled=false")
			}
		})
	}
}

func TestSimFundingMonitor_BiasMatchesLiveThresholds(t *testing.T) {
	clk := NewSimClock(time.UnixMilli(testBaseTs))
	cases := []struct {
		rate      float64
		buy, sell float64
		high      bool
	}{
		{-0.0001, 1.2, 1.0, false},
		{0, 1.2, 1.0, false},
		{0.0004, 1.0, 1.2, false},
		{0.0008, 0.7, 1.2, false},
		{0.0012, 0.3, 1.2, true},
		{0.002, 0.0, 1.2, true},
	}
	for _, tc := range cases {
		m := NewSimFundingMonitor(config.FundingRateConfig{BiasEnabled: true}, []FundingPoint{{Timestamp: testBaseTs, Rate: tc.rate}}, 0, clk)
		if m.GetBuyBias() != tc.buy || m.GetSellBias() != tc.sell || m.IsHighRate() != tc.high || m.ShouldPauseBuying() != (tc.buy == 0) {
			t.Fatalf("rate %v: buy=%v sell=%v high=%v, want %v %v %v", tc.rate, m.GetBuyBias(), m.GetSellBias(), m.IsHighRate(), tc.buy, tc.sell, tc.high)
		}
	}
}

// 通過 Setup 注入模擬資金費監控器後，funding_rate.pricing_enabled 在回放中生效（付費一側開倉價外移）
func TestEngine_SetupInjectsFundingPricing(t *testing.T) {
	var prices []float64
	for i := 0; i < 40; i++ {
		prices = append(prices, 2000, 1985, 1970, 1985, 2000, 2015, 2030, 2015)
	}
	ticks := ticksFromPrices(prices, 10)
	run := func(inject bool) *Result {
		cfg := testConfig(MatchingConfig{})
		cfg.Bot.Trading.Direction = "LONG"
		cfg.Bot.FundingRate = config.FundingRateConfig{Enabled: true, PricingEnabled: true}
		var mon *SimFundingMonitor
		if inject {
			cfg.Setup = func(spm *position.SuperPositionManager, clock position.Clock) error {
				mon = NewSimFundingMonitor(cfg.Bot.FundingRate, []FundingPoint{{Timestamp: 0, Rate: 0.004}}, 0, clock)
				spm.SetFundingMonitor(mon)
				return nil
			}
		}
		res, err := RunTicks(cfg, ticks)
		if err != nil {
			t.Fatalf("run inject=%v: %v", inject, err)
		}
		if inject && mon == nil {
			t.Fatalf("setup hook not invoked")
		}
		return res
	}
	plain, priced := run(false), run(true)
	var offGrid int
	for _, tr := range priced.Backtest.Trades {
		if tr.Type == "buy" && math.Abs(math.Mod(tr.Price, testInterval)) > testFloatDelta {
			offGrid++
		}
	}
	if offGrid == 0 {
		t.Fatalf("funding pricing should shift paying-side open prices off the grid; trades=%v", priced.Backtest.Trades)
	}
	for _, tr := range plain.Backtest.Trades {
		if tr.Type == "buy" && math.Abs(math.Mod(tr.Price, testInterval)) > testFloatDelta {
			t.Fatalf("without a funding monitor buys must stay on the grid, got %.2f", tr.Price)
		}
	}
}

// countingProvider 記錄 Snapshot 調用次數；每次返回新的 BarOpenTime，使 RefreshRegimeInterval 每次都重新評估
type countingProvider struct{ calls atomic.Int64 }

func (p *countingProvider) Snapshot() regime.Snapshot {
	n := p.calls.Add(1)
	return regime.Snapshot{Ready: true, BarOpenTime: time.UnixMilli(testBaseTs + n)}
}

// RunRegimeControlLoop 使用 Clock.NewTicker：注入 SimClock 後按模擬時間觸發（每 30s 模擬時間評估一次）
func TestSimClock_DrivesRegimeControlLoop(t *testing.T) {
	cfg, err := testConfig(MatchingConfig{}).normalized(2000)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	ex := newSimExchange(cfg)
	spm := position.NewSuperPositionManager(cfg.Bot, newSimExecutor(ex, cfg.Bot), ex, testPriceDec, testQtyDec)
	clk := NewSimClock(time.UnixMilli(testBaseTs))
	spm.SetClock(clk)
	prov := &countingProvider{}
	spm.ConfigureRegimeControl(prov, position.RegimeControlOptions{FilterEnabled: true})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		spm.RunRegimeControlLoop(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()

	waitFor := func(want int64) {
		deadline := time.Now().Add(5 * time.Second)
		for prov.calls.Load() < want {
			if time.Now().After(deadline) {
				t.Fatalf("regime loop evaluated %d times, want >= %d", prov.calls.Load(), want)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitFor(1) // 啟動時評估一次
	// 牆鐘等待不推進模擬時間：不應觸發
	time.Sleep(50 * time.Millisecond)
	if got := prov.calls.Load(); got != 1 {
		t.Fatalf("loop fired without sim time advancing: calls=%d", got)
	}
	// ticker 在首次評估之後才創建：輪詢推進模擬時間直到觸發
	for want := int64(2); want <= 4; want++ {
		deadline := time.Now().Add(5 * time.Second)
		for prov.calls.Load() < want {
			if time.Now().After(deadline) {
				t.Fatalf("sim ticker did not fire: calls=%d want %d", prov.calls.Load(), want)
			}
			clk.Sleep(30 * time.Second)
			time.Sleep(2 * time.Millisecond)
		}
	}
}
