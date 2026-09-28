package strategy

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/monitor"
	"quantmesh/position"
)

type hourlyRiskClock struct {
	position.Clock
	nanos atomic.Int64
}

func newHourlyRiskClock(at time.Time) *hourlyRiskClock {
	c := &hourlyRiskClock{Clock: position.RealClock()}
	c.nanos.Store(at.UnixNano())
	return c
}
func (c *hourlyRiskClock) Now() time.Time          { return time.Unix(0, c.nanos.Load()).UTC() }
func (c *hourlyRiskClock) Advance(d time.Duration) { c.nanos.Add(int64(d)) }
func riskHourlyPoints(prices []float64, end time.Time) []indicators.PricePoint {
	p := make([]indicators.PricePoint, len(prices))
	for i, v := range prices {
		p[i] = indicators.PricePoint{Timestamp: end.Add(time.Duration(i-len(prices)+1) * time.Hour), Price: v, High: v, Low: v, Volume: 1}
	}
	return p
}

func newHourlyRiskAdjuster(t *testing.T) (*position.SuperPositionManager, *DynamicAdjuster, *hourlyRiskClock) {
	t.Helper()
	cfg, manager, _ := newBoundsTestAdjuster(t)
	cfg.Trading.DynamicAdjustment.VolatilityDetection.ShortPeriod = 3
	cfg.Trading.DynamicAdjustment.VolatilityDetection.MediumPeriod = 3
	cfg.Trading.DynamicAdjustment.VolatilityDetection.LongPeriod = 3
	cfg.Trading.DynamicAdjustment.VolatilityDetection.PriceRangePeriod = 3
	manager.SetOpenPositionControl(config.OpenPositionControl{BotRiskControl: &config.BotRiskControl{Enabled: true, VolatilityPauseEnabled: true, VolatilityPauseConfig: config.VolatilityPauseConfig{PauseOnExtremeVolatility: true, AutoResumeOnNormal: true}}})
	clock := newHourlyRiskClock(time.Date(2026, 9, 24, 10, 30, 0, 0, time.UTC))
	manager.SetClock(clock)
	da := NewDynamicAdjuster(cfg, nil, manager)
	t.Cleanup(da.Stop)
	return manager, da, clock
}

func TestHourlyRiskReloadRequiresFreshQuoteAndCurrentHour(t *testing.T) {
	manager, da, clock := newHourlyRiskAdjuster(t)
	prices := []float64{100, 100, 100, 100, 100, 100}
	if err := da.SetVolatilityHistoryLoader(func(ctx context.Context, n int, asOf time.Time) ([]indicators.PricePoint, error) {
		if n != 6 {
			t.Fatalf("requested %d bars", n)
		}
		return riskHourlyPoints(prices, asOf.Truncate(time.Hour)), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := da.reloadVolatilityHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("history substituted for a fresh live quote")
	}
	da.OnPriceChange(monitor.PriceChange{NewPrice: 100})
	if manager.IsVolatilityRiskPaused() {
		t.Fatal("complete closed history plus quote did not become ready")
	}
	clock.Advance(time.Hour)
	da.OnPriceChange(monitor.PriceChange{NewPrice: 100})
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("fresh tick hid missing completed hour")
	}
	if err := da.reloadVolatilityHistory(context.Background()); err != nil {
		t.Fatal(err)
	}
	if manager.IsVolatilityRiskPaused() {
		t.Fatal("hourly refresh did not release data hold")
	}
}

func TestHourlyRiskReloadFailureCannotBeClearedByTicks(t *testing.T) {
	manager, da, clock := newHourlyRiskAdjuster(t)
	_ = da.SetVolatilityHistoryLoader(func(context.Context, int, time.Time) ([]indicators.PricePoint, error) {
		return nil, errors.New("history unavailable")
	})
	if err := da.reloadVolatilityHistory(context.Background()); err == nil {
		t.Fatal("missing history reported success")
	}
	for i := 0; i < 100; i++ {
		da.OnPriceChange(monitor.PriceChange{NewPrice: 100})
	}
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("tick frequency fabricated history")
	}
	points := riskHourlyPoints([]float64{100, 100, 100, 100, 100, 100}, clock.Now().Truncate(time.Hour))
	if err := da.ReplaceVolatilityHistory(points); err != nil {
		t.Fatal(err)
	}
	if manager.IsVolatilityRiskPaused() {
		t.Fatal("valid repair did not recover")
	}
	points[len(points)-1].Timestamp = points[len(points)-1].Timestamp.Add(time.Hour)
	if err := da.ReplaceVolatilityHistory(points); err == nil {
		t.Fatal("future bar accepted")
	}
	da.OnPriceChange(monitor.PriceChange{NewPrice: 100})
	if !manager.IsVolatilityRiskPaused() {
		t.Fatal("failed replacement lost its independent hold")
	}
}

func TestHourlyRiskHistoryWorkerStopsWithRuntime(t *testing.T) {
	_, da, _ := newHourlyRiskAdjuster(t)
	entered, exited := make(chan struct{}), make(chan struct{})
	_ = da.SetVolatilityHistoryLoader(func(ctx context.Context, _ int, _ time.Time) ([]indicators.PricePoint, error) {
		close(entered)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	})
	da.StartWithExternalPrices()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("history loader not started")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := da.StopContext(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("stop returned while loader still active")
	}
}

func TestHourlyRiskHistoryCannotRepairInvalidQuote(t *testing.T) {
	manager, da, clock := newHourlyRiskAdjuster(t)
	points := riskHourlyPoints([]float64{100, 100, 100, 100, 100, 100}, clock.Now().Truncate(time.Hour))
	da.OnPriceChange(monitor.PriceChange{NewPrice: 100, HighPrice: 1, LowPrice: 2})
	if err := da.ReplaceVolatilityHistory(points); err != nil {
		t.Fatal(err)
	}
	if !manager.IsVolatilityRiskPaused() || da.GetVolatilityEvidenceStatus().Ready {
		t.Fatal("history refresh fabricated a valid quote")
	}
	da.OnPriceChange(monitor.PriceChange{NewPrice: 100})
	if !da.GetVolatilityEvidenceStatus().Ready || manager.IsVolatilityRiskPaused() {
		t.Fatal("valid independent quote did not complete evidence")
	}
}

func TestHourlyRiskFetchCrossingBoundaryCannotPublishStaleHour(t *testing.T) {
	manager, da, clock := newHourlyRiskAdjuster(t)
	da.OnPriceChange(monitor.PriceChange{NewPrice: 100})
	_ = da.SetVolatilityHistoryLoader(func(_ context.Context, _ int, asOf time.Time) ([]indicators.PricePoint, error) {
		clock.Advance(time.Hour)
		return riskHourlyPoints([]float64{100, 100, 100, 100, 100, 100}, asOf.Truncate(time.Hour)), nil
	})
	if da.reloadVolatilityHistory(context.Background()) == nil {
		t.Fatal("fetch crossing boundary published stale evidence")
	}
	if !manager.IsVolatilityRiskPaused() || da.GetVolatilityEvidenceStatus().Reason != "hourly_history_unverified" {
		t.Fatal("failed fetch falsely reported readiness")
	}
}

func TestHourlyRiskIndependentOfLiveTickFrequency(t *testing.T) {
	var snapshots []indicators.VolatilityPoint
	for _, ticks := range []int{1, 1000} {
		manager, da, clock := newHourlyRiskAdjuster(t)
		points := riskHourlyPoints([]float64{100, 200, 50, 200, 50, 200}, clock.Now().Truncate(time.Hour))
		if err := da.ReplaceVolatilityHistory(points); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < ticks; i++ {
			da.OnPriceChange(monitor.PriceChange{NewPrice: 200})
		}
		if !manager.IsVolatilityRiskPaused() {
			t.Fatal("same hourly risk changed with tick frequency")
		}
		snapshots = append(snapshots, *da.volatilityAlert.GetLatestVolatility())
	}
	if snapshots[0] != snapshots[1] {
		t.Fatal("tick frequency changed hourly statistics")
	}
}
