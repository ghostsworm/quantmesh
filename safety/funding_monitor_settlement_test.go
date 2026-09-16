package safety

import (
	"context"
	"testing"
	"time"

	"quantmesh/config"
)

func TestEstimateNextFundingTime(t *testing.T) {
	day := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{name: "剛過零點", now: day.Add(time.Minute), want: day.Add(8 * time.Hour)},
		{name: "恰好結算點取下一個", now: day.Add(8 * time.Hour), want: day.Add(16 * time.Hour)},
		{name: "下午", now: day.Add(15*time.Hour + 59*time.Minute), want: day.Add(16 * time.Hour)},
		{name: "跨日", now: day.Add(23 * time.Hour), want: day.Add(24 * time.Hour)},
		{name: "非 UTC 時區輸入", now: day.Add(9 * time.Hour).In(time.FixedZone("CST", 8*3600)), want: day.Add(16 * time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EstimateNextFundingTime(tt.now); !got.Equal(tt.want) {
				t.Fatalf("EstimateNextFundingTime(%s) = %s, want %s", tt.now, got, tt.want)
			}
		})
	}
}

func TestFetchFundingRateEstimatesNextFundingTimeOnlyWhenUnsetOrExpired(t *testing.T) {
	cfg := &config.Config{}
	cfg.FundingRate.Enabled = true
	monitor := NewFundingRateMonitor(cfg, &fundingMonitorExchange{rate: 0.0003}, "BTCUSDT")

	if err := monitor.fetchFundingRate(context.Background()); err != nil {
		t.Fatalf("fetch funding: %v", err)
	}
	next := monitor.GetNextFundingTime()
	if !next.After(time.Now()) || time.Until(next) > 8*time.Hour {
		t.Fatalf("estimated next funding time out of range: %s", next)
	}

	custom := time.Now().Add(42 * time.Minute)
	monitor.SetNextFundingTime(custom)
	if err := monitor.fetchFundingRate(context.Background()); err != nil {
		t.Fatalf("fetch funding: %v", err)
	}
	if got := monitor.GetNextFundingTime(); !got.Equal(custom) {
		t.Fatalf("future next funding time should be kept, got %s want %s", got, custom)
	}

	monitor.SetNextFundingTime(time.Now().Add(-time.Minute))
	if err := monitor.fetchFundingRate(context.Background()); err != nil {
		t.Fatalf("fetch funding: %v", err)
	}
	if got := monitor.GetNextFundingTime(); !got.After(time.Now()) {
		t.Fatalf("expired next funding time should be re-estimated, got %s", got)
	}
}
