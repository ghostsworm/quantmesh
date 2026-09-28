package risk

import (
	"context"
	"testing"
	"time"
)

func TestAuditDrawdownMustObserveAccountDebits(t *testing.T) {
	now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	source := &fakeEquitySource{equity: 1000}
	sink := &fakeSink{}
	f := NewMetricsFeeder(sink, &fakeTradeSource{}, source, nil, MetricsFeederOptions{Now: func() time.Time { return now }})
	if _, err := f.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	source.equity = 800
	now = now.Add(time.Minute)
	snap, err := f.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.MaxDrawdownPct < 19.99 {
		t.Fatalf("equity fell 1000 -> 800 without a grid trade; reported drawdown=%v%%, equity calls=%d", snap.MaxDrawdownPct, source.calls)
	}
}
