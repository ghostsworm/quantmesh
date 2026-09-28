package indicators

import (
	"testing"
	"time"
)

func TestVolatilityRegimeConfirmsConsecutiveCandidate(t *testing.T) {
	for _, count := range []int{1, 2, 3} {
		cfg := DefaultVolatilityRegimeConfig()
		cfg.ShortPeriod, cfg.MediumPeriod, cfg.LongPeriod, cfg.PriceRangePeriod = 3, 3, 3, 3
		cfg.ConsecutivePeriods = count
		d := NewVolatilityRegimeDetector(cfg)
		for i, p := range []float64{100, 200, 50, 200, 50, 200} {
			at := time.Date(2026, 1, 1, i, 0, 0, 0, time.UTC)
			d.priceHistory = append(d.priceHistory, PricePoint{Timestamp: at, Price: p, High: p, Low: p})
			d.detectRegime(at)
			if i < 3 {
				continue
			}
			want := RegimeNormal
			if i-2 >= count {
				want = RegimeExtreme
			}
			if d.GetCurrentRegime() != want {
				t.Fatalf("confirmation=%d index=%d got=%v want=%v", count, i, d.GetCurrentRegime(), want)
			}
		}
	}
}
