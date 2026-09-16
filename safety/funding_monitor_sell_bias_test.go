package safety

import (
	"testing"

	"quantmesh/config"
)

func TestFundingRateMonitorBuySellBiasMirror(t *testing.T) {
	cfg := &config.Config{}
	cfg.FundingRate.BiasEnabled = true
	cfg.FundingRate.HighRateThreshold = 0.001
	cfg.FundingRate.PauseBuyThreshold = 0.0015
	monitor := NewFundingRateMonitor(cfg, nil, "BTCUSDT")

	tests := []struct {
		rate     float64
		wantBuy  float64
		wantSell float64
	}{
		{-0.0020, 1.2, 0.0},
		{-0.0012, 1.2, 0.3},
		{-0.0008, 1.2, 0.7},
		{-0.0001, 1.2, 1.0},
		{0, 1.2, 1.0},
		{0.0001, 1.0, 1.2},
		{0.0008, 0.7, 1.2},
		{0.0012, 0.3, 1.2},
		{0.0020, 0.0, 1.2},
	}
	for _, tc := range tests {
		monitor.currentRate = tc.rate
		if got := monitor.GetBuyBias(); got != tc.wantBuy {
			t.Fatalf("rate=%f buy bias=%f want %f", tc.rate, got, tc.wantBuy)
		}
		if got := monitor.GetSellBias(); got != tc.wantSell {
			t.Fatalf("rate=%f sell bias=%f want %f", tc.rate, got, tc.wantSell)
		}
	}

	cfg.FundingRate.BiasEnabled = false
	if monitor.GetSellBias() != 1.0 {
		t.Fatalf("disabled sell bias should be neutral")
	}
}
