package indicators

import (
	"strings"
	"testing"
	"time"
)

func TestVolatilityRegimeDetectorClassifiesAndReportsState(t *testing.T) {
	cfg := DefaultVolatilityRegimeConfig()
	cfg.ShortPeriod = 3
	cfg.MediumPeriod = 4
	cfg.LongPeriod = 5
	cfg.PriceRangePeriod = 4
	cfg.ConsecutivePeriods = 1
	cfg.LowThreshold = 0.2
	cfg.NormalThreshold = 1.0
	cfg.HighThreshold = 3.0
	cfg.ExtremeThreshold = 8.0
	cfg.PriceRangeThreshold = 0.5
	detector := NewVolatilityRegimeDetector(cfg)

	for _, regime := range []VolatilityRegime{RegimeLow, RegimeNormal, RegimeHigh, RegimeExtreme, VolatilityRegime(99)} {
		if regime.String() == "" || regime.Color() == "" {
			t.Fatalf("empty string/color for regime %d", regime)
		}
	}
	if detector.GetLatestVolatility() != nil {
		t.Fatalf("new detector should not have volatility")
	}
	if changed, reason := detector.DetectSuddenChange(); changed || reason != "" {
		t.Fatalf("new detector sudden change=%v reason=%q", changed, reason)
	}

	eventCh := make(chan VolatilityRegimeEvent, 1)
	detector.SetRegimeChangeCallback(func(event VolatilityRegimeEvent) {
		eventCh <- event
	})
	prices := []float64{100, 100.1, 100.2, 111, 95, 120}
	for _, p := range prices {
		detector.UpdatePrice(p, p*1.01, p*0.99, 1000)
	}
	detector.triggerRegimeChange(VolatilityPoint{
		Timestamp:        time.Now(),
		ShortVolatility:  9,
		MediumVolatility: 4,
		LongVolatility:   2,
		PriceRange:       6,
		Regime:           RegimeHigh,
	})
	var gotEvent VolatilityRegimeEvent
	select {
	case gotEvent = <-eventCh:
	case <-time.After(time.Second):
		t.Fatalf("expected regime change callback")
	}
	if gotEvent.NewRegime == RegimeNormal || gotEvent.Severity == "" || len(gotEvent.Recommendations) == 0 {
		t.Fatalf("unexpected event=%#v", gotEvent)
	}
	if detector.GetCurrentRegime() != gotEvent.NewRegime {
		t.Fatalf("current regime did not update")
	}
	if latest := detector.GetLatestVolatility(); latest == nil || latest.ShortVolatility == 0 {
		t.Fatalf("latest volatility=%#v", latest)
	}
	if history := detector.GetVolatilityHistory(2); len(history) != 2 {
		t.Fatalf("history len=%d", len(history))
	}
	if history := detector.GetVolatilityHistory(0); len(history) == 0 {
		t.Fatalf("full history should not be empty")
	}

	detector.volatilityHistory = append(detector.volatilityHistory,
		VolatilityPoint{ShortVolatility: 1},
		VolatilityPoint{ShortVolatility: 3},
	)
	if changed, reason := detector.DetectSuddenChange(); !changed || !strings.Contains(reason, "增加") {
		t.Fatalf("increase change=%v reason=%q", changed, reason)
	}
	detector.volatilityHistory = append(detector.volatilityHistory, VolatilityPoint{ShortVolatility: 0.2})
	if changed, reason := detector.DetectSuddenChange(); !changed || !strings.Contains(reason, "减少") {
		t.Fatalf("decrease change=%v reason=%q", changed, reason)
	}
	if !detector.IsGridFriendly() && detector.GetRiskLevel() == 0 {
		t.Fatalf("risk helpers should return stable values")
	}
}

func TestVolatilityRegimeInternalHelpers(t *testing.T) {
	d := NewVolatilityRegimeDetector(DefaultVolatilityRegimeConfig())
	d.config.LowThreshold = 1
	d.config.HighThreshold = 5
	d.config.ExtremeThreshold = 10
	d.config.PriceRangeThreshold = 1.5

	cases := []struct {
		shortVol   float64
		mediumVol  float64
		priceRange float64
		want       VolatilityRegime
	}{
		{12, 1, 5, RegimeExtreme},
		{0.4, 0.5, 0.8, RegimeLow},
		{6, 2, 4, RegimeHigh},
		{0.5, 3, 4, RegimeLow},
		{2, 2, 4, RegimeNormal},
	}
	for _, tc := range cases {
		if got := d.classifyRegime(tc.shortVol, tc.mediumVol, 0, tc.priceRange); got != tc.want {
			t.Fatalf("classify=%s want %s", got, tc.want)
		}
	}
	for _, pair := range [][2]VolatilityRegime{
		{RegimeLow, RegimeHigh}, {RegimeNormal, RegimeExtreme}, {RegimeNormal, RegimeHigh}, {RegimeNormal, RegimeLow},
	} {
		if d.determineSeverity(pair[0], pair[1]) == "" {
			t.Fatalf("severity empty for %v", pair)
		}
		if len(d.generateRecommendations(pair[0], pair[1], VolatilityPoint{ShortVolatility: 9})) == 0 {
			t.Fatalf("recommendations empty for %v", pair)
		}
		if d.buildTriggerReason(pair[0], pair[1], VolatilityPoint{ShortVolatility: 9, MediumVolatility: 0.5, PriceRange: 0.7}) == "" {
			t.Fatalf("reason empty for %v", pair)
		}
	}

	d.priceHistory = []PricePoint{
		{Price: 100, High: 101, Low: 99},
		{Price: 102, High: 103, Low: 100},
		{Price: 101, High: 104, Low: 98},
	}
	if d.calculateVolatility(3) == 0 {
		t.Fatalf("volatility should be positive")
	}
	if d.calculateVolatility(10) != 0 || d.calculatePriceRange(10) != 0 {
		t.Fatalf("insufficient data should produce zero")
	}
	if d.calculatePriceRange(3) == 0 {
		t.Fatalf("price range should be positive")
	}
}
