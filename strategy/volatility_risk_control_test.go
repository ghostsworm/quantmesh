package strategy

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/monitor"
)

func TestVolatilityRiskUsesLiveBotSnapshotAndIndependentHold(t *testing.T) {
	cfg, manager, da := newVolatilityPauseAdjuster(t)
	// The stale first symbol intentionally disagrees with the effective Bot.
	cfg.Trading.Symbols[0].OpenPositionControl.BotRiskControl.VolatilityPauseEnabled = false
	manager.PauseOpening("manual")
	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeExtreme})
	manager.ResumeOpening()
	if !manager.IsVolatilityRiskPaused() || !manager.IsOpeningPaused() {
		t.Fatal("manual resume erased independent live volatility risk")
	}
	rc := manager.GetRiskControls()
	rc.Open.BotRiskControl.Enabled = false
	manager.SetRiskControls(rc)
	manager.PauseOpening("manual")
	da.RefreshRiskControls()
	if manager.IsVolatilityRiskPaused() || !manager.IsOpeningPaused() {
		t.Fatal("disable did not release only volatility-owned hold")
	}
}

func TestVolatilityRiskDirectionAndTimeWindow(t *testing.T) {
	for _, direction := range []string{"LONG", "SHORT", "BOTH"} {
		for _, trend := range []string{"up", "down"} {
			t.Run(direction+"/"+trend, func(t *testing.T) {
				cfg, manager, da := newVolatilityPauseAdjuster(t)
				cfg.Trading.Direction = direction
				rc := manager.GetRiskControls()
				rc.Open.BotRiskControl.VolatilityPauseConfig = config.VolatilityPauseConfig{PauseOnDowntrend: true, PauseOnUptrend: true, TrendCheckPeriod: 15, AutoResumeOnNormal: true}
				manager.SetRiskControls(rc)
				at := time.Now().Truncate(time.Minute)
				da.recordTrendLocked(at.Add(-15*time.Minute), 100)
				price := 95.0
				if trend == "up" {
					price = 105
				}
				for i := 0; i < 100; i++ {
					da.recordTrendLocked(at, price)
				}
				da.priceEvidenceAt = at
				da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeHigh, ShortVolatility: 6})
				want := direction == "BOTH" || (direction == "LONG" && trend == "down") || (direction == "SHORT" && trend == "up")
				if manager.IsVolatilityRiskPaused() != want {
					t.Fatalf("direction %s trend %s paused=%v", direction, trend, manager.IsVolatilityRiskPaused())
				}
				if len(da.trendHistory) != 2 {
					t.Fatal("ticks treated as minutes")
				}
			})
		}
	}
}

func TestVolatilityRiskResumeThresholdAndInvalidEvidence(t *testing.T) {
	_, manager, da := newVolatilityPauseAdjuster(t)
	rc := manager.GetRiskControls()
	rc.Open.BotRiskControl.VolatilityPauseConfig.ResumeThreshold = 1
	manager.SetRiskControls(rc)
	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeExtreme, ShortVolatility: 12})
	for _, value := range []float64{1, 2, math.NaN(), math.Inf(1)} {
		da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeNormal, ShortVolatility: value})
		if !manager.IsVolatilityRiskPaused() {
			t.Fatalf("invalid/premature resume at %v", value)
		}
	}
	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeNormal, ShortVolatility: 0.9})
	if manager.IsVolatilityRiskPaused() {
		t.Fatal("valid recovery did not release volatility hold")
	}
}

func TestVolatilityRiskRealPricesWithoutDynamicAdjustment(t *testing.T) {
	cfg, manager, _ := newBoundsTestAdjuster(t)
	clock := newHourlyRiskClock(time.Now().UTC().Truncate(time.Hour))
	manager.SetClock(clock)
	cfg.Trading.DynamicAdjustment.Enabled = false
	cfg.Trading.DynamicAdjustment.VolatilityDetection.ShortPeriod = 3
	cfg.Trading.DynamicAdjustment.VolatilityDetection.MediumPeriod = 3
	cfg.Trading.DynamicAdjustment.VolatilityDetection.LongPeriod = 3
	cfg.Trading.DynamicAdjustment.VolatilityDetection.PriceRangePeriod = 3
	rc := &config.BotRiskControl{Enabled: true, VolatilityPauseEnabled: true, VolatilityPauseConfig: config.VolatilityPauseConfig{PauseOnExtremeVolatility: true, AutoResumeOnNormal: true}}
	manager.SetOpenPositionControl(config.OpenPositionControl{BotRiskControl: rc})
	da := NewDynamicAdjuster(cfg, nil, manager)
	da.StartWithExternalPrices()
	t.Cleanup(da.Stop)
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("missing warmup evidence accepted as safe")
	}
	for _, price := range []float64{100, 100, 100, 100} {
		da.OnPriceChange(monitor.PriceChange{NewPrice: price})
	}
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("ticks fabricated completed hourly warmup")
	}
	prices := []float64{100, 100, 100, 100, 100, 100}
	if err := da.ReplaceVolatilityHistory(riskHourlyPoints(prices, clock.Now())); err != nil {
		t.Fatal(err)
	}
	if manager.IsVolatilityRiskPaused() {
		t.Fatal("warmup complete but still held")
	}
	prices = append(prices, 140, 60, 150)
	clock.Advance(3 * time.Hour)
	if err := da.ReplaceVolatilityHistory(riskHourlyPoints(prices, clock.Now())); err != nil {
		t.Fatal(err)
	}
	da.OnPriceChange(monitor.PriceChange{NewPrice: 150})
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("real volatile sequence never reached runtime gate")
	}
	if release, err := manager.OpeningGate().Begin(); err == nil {
		release()
		t.Fatal("opening admitted in extreme regime")
	}
	if cfg.Trading.PriceInterval != 3 || cfg.Trading.OrderQuantity != 200 {
		t.Fatal("disabled parameter adjustment changed strategy")
	}
	for i := 0; i < 6; i++ {
		prices = append(prices, 100)
	}
	clock.Advance(6 * time.Hour)
	if err := da.ReplaceVolatilityHistory(riskHourlyPoints(prices, clock.Now())); err != nil {
		t.Fatal(err)
	}
	da.OnPriceChange(monitor.PriceChange{NewPrice: 100})
	if manager.IsVolatilityRiskPaused() {
		t.Fatal("stable prices did not recover")
	}
	da.OnPriceChange(monitor.PriceChange{NewPrice: math.NaN()})
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("invalid price silently treated as safe")
	}
	da.Stop()
	da.OnPriceChange(monitor.PriceChange{NewPrice: 100})
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("late callback modified stopped controller")
	}
}

func TestVolatilityRiskConcurrentHotUpdateAndStop(t *testing.T) {
	_, manager, da := newVolatilityPauseAdjuster(t)
	da.StartWithExternalPrices()
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			da.OnPriceChange(monitor.PriceChange{NewPrice: float64(100 + i%7)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			rc := manager.GetRiskControls()
			rc.Open.BotRiskControl.VolatilityPauseEnabled = i%2 == 0
			manager.SetRiskControls(rc)
			da.RefreshRiskControls()
		}
	}()
	go func() { defer wg.Done(); da.Stop() }()
	wg.Wait()
}

func TestVolatilityRiskStaleEvidenceAndExplicitRecovery(t *testing.T) {
	_, manager, da := newVolatilityPauseAdjuster(t)
	rc := manager.GetRiskControls()
	rc.Open.BotRiskControl.VolatilityPauseConfig.AutoResumeOnNormal = false
	manager.SetRiskControls(rc)
	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeExtreme})
	da.observation = &indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeNormal, ShortVolatility: 0.5}
	da.priceEvidenceAt = time.Now().Add(-3 * time.Minute)
	if err := da.ResumeVolatilityManually(); err == nil {
		t.Fatal("stale evidence allowed manual recovery")
	}
	da.priceEvidenceAt = time.Now()
	da.quoteValid = true
	da.historyThrough = time.Now().UTC().Truncate(time.Hour)
	if err := da.ResumeVolatilityManually(); err != nil {
		t.Fatal(err)
	}
	if manager.IsVolatilityRiskPaused() {
		t.Fatal("safe explicit recovery did not release historical latch")
	}
	da.priceEvidenceAt = time.Now().Add(-3 * time.Minute)
	da.RefreshRiskControls()
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("quiet stale stream left admission open")
	}
}

func TestVolatilityRiskStopContextCancelsLockWait(t *testing.T) {
	_, _, da := newVolatilityPauseAdjuster(t)
	da.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := da.StopContext(ctx)
	da.mu.Unlock()
	if err != context.Canceled {
		t.Fatalf("stop lock wait ignored context: %v", err)
	}
	da.Stop()
}

func TestVolatilityRiskRejectsInvalidPersistedPolicy(t *testing.T) {
	_, manager, da := newVolatilityPauseAdjuster(t)
	rc := manager.GetRiskControls()
	rc.Open.BotRiskControl.VolatilityPauseConfig.ResumeThreshold = math.NaN()
	manager.SetRiskControls(rc)
	da.checkVolatilityPause(indicators.VolatilityRegimeEvent{NewRegime: indicators.RegimeNormal, ShortVolatility: 0.5})
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("invalid persisted threshold admitted opening")
	}
}
