package indicators

import (
	"math"
	"testing"
	"time"
)

func hourlyTestPoints(prices []float64, end time.Time) []PricePoint {
	points := make([]PricePoint, len(prices))
	for i, p := range prices {
		points[i] = PricePoint{Timestamp: end.Add(-time.Duration(len(prices)-1-i) * time.Hour), Price: p, High: p, Low: p, Volume: 1}
	}
	return points
}

func hourlyTestConfig() VolatilityRegimeConfig {
	c := DefaultVolatilityRegimeConfig()
	c.ShortPeriod, c.MediumPeriod, c.LongPeriod, c.PriceRangePeriod = 3, 3, 3, 3
	return c
}

func TestHourlyVolatilityUsesReturnsNotTickCount(t *testing.T) {
	d := NewVolatilityRegimeDetector(hourlyTestConfig())
	end := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	points := hourlyTestPoints([]float64{100, 100, 100, 110, 99, 108.9}, end)
	if err := d.ReplaceHourlyHistory(points, end); err != nil {
		t.Fatal(err)
	}
	// Last three hourly returns are +10%, -10%, +10%.
	want := math.Sqrt((math.Pow(10-10.0/3, 2)*2 + math.Pow(-10-10.0/3, 2)) / 3)
	if got := d.GetLatestVolatility().ShortVolatility; math.Abs(got-want) > 1e-9 {
		t.Fatalf("volatility=%v want=%v", got, want)
	}
	before := d.GetCurrentRegime()
	for i := 0; i < 500; i++ {
		if err := d.ReplaceHourlyHistory(points, end.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	if len(d.GetVolatilityHistory(0)) != 3 || d.GetCurrentRegime() != before {
		t.Fatal("repeated refresh advanced hourly confirmations/history")
	}
}

func TestHourlyVolatilityRejectsBadEvidenceAtomically(t *testing.T) {
	end := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	base := hourlyTestPoints([]float64{100, 101, 99, 102, 98, 103}, end)
	for _, name := range []string{"missing", "duplicate", "reverse", "future", "stale", "nan", "bad_high", "changed", "unaligned"} {
		t.Run(name, func(t *testing.T) {
			d := NewVolatilityRegimeDetector(hourlyTestConfig())
			if err := d.ReplaceHourlyHistory(base, end); err != nil {
				t.Fatal(err)
			}
			before := *d.GetLatestVolatility()
			p := append([]PricePoint(nil), base...)
			switch name {
			case "missing":
				p = append(p[:2], p[3:]...)
			case "duplicate":
				p[3].Timestamp = p[2].Timestamp
			case "reverse":
				p[0], p[1] = p[1], p[0]
			case "future":
				p[len(p)-1].Timestamp = end.Add(time.Hour)
			case "stale":
				for i := range p {
					p[i].Timestamp = p[i].Timestamp.Add(-time.Hour)
				}
			case "nan":
				p[1].Price = math.NaN()
			case "bad_high":
				p[1].High = 1
			case "changed":
				p[1].Price = 100
				p[1].Low = 100
			case "unaligned":
				p[1].Timestamp = p[1].Timestamp.Add(time.Minute)
			}
			if d.ReplaceHourlyHistory(p, end) == nil {
				t.Fatal("accepted invalid hourly history")
			}
			if *d.GetLatestVolatility() != before {
				t.Fatal("failed evidence changed published observation")
			}
		})
	}
}

func TestHourlyVolatilityNeedsEveryConfiguredWindow(t *testing.T) {
	c := hourlyTestConfig()
	c.LongPeriod = 24
	d := NewVolatilityRegimeDetector(c)
	end := time.Now().UTC().Truncate(time.Hour)
	if d.ReplaceHourlyHistory(hourlyTestPoints([]float64{100, 100, 100, 100, 100, 100}, end), end) == nil {
		t.Fatal("short history fabricated long-window zero")
	}
	if d.GetLatestVolatility() != nil {
		t.Fatal("insufficient history published safe volatility")
	}
}

func TestHourlyVolatilityResumeMatchesIncrementalReplay(t *testing.T) {
	c := hourlyTestConfig()
	end := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	prices := []float64{100, 200, 50, 200, 50, 200}
	incremental := NewVolatilityRegimeDetector(c)
	if err := incremental.ReplaceHourlyHistory(hourlyTestPoints(prices, end), end); err != nil {
		t.Fatal(err)
	}
	for _, p := range []float64{200, 200, 200, 200, 200, 200} {
		prices = append(prices, p)
		end = end.Add(time.Hour)
		if err := incremental.ReplaceHourlyHistory(hourlyTestPoints(prices, end), end); err != nil {
			t.Fatal(err)
		}
	}
	restarted := NewVolatilityRegimeDetector(c)
	if err := restarted.ReplaceHourlyHistory(hourlyTestPoints(prices, end), end); err != nil {
		t.Fatal(err)
	}
	if *restarted.GetLatestVolatility() != *incremental.GetLatestVolatility() || restarted.GetCurrentRegime() != incremental.GetCurrentRegime() {
		t.Fatal("restart and forward history disagree")
	}
}
