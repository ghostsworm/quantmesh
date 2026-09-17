package main

import (
	"math"
	"testing"
	"time"

	"quantmesh/backtest/replay"
	"quantmesh/exchange"
)

const (
	hookTestDays     = 8
	hookTestSegStart = 6 // 前 6 天只作 regime 預熱歷史
)

// trendingMinuteCandles 帶波動的上漲 1m K 線：每小時 +0.3%，小時內 ±0.4% 正弦擺動
func trendingMinuteCandles(start int64, days int) []*exchange.Candle {
	n := days * 24 * 60
	out := make([]*exchange.Candle, 0, n)
	price := 2000.0
	for i := 0; i < n; i++ {
		drift := 2000.0 * math.Pow(1.003, float64(i)/60)
		wave := drift * 0.004 * math.Sin(float64(i)/7)
		open := price
		cl := drift + wave
		hi, lo := math.Max(open, cl)*1.0005, math.Min(open, cl)*0.9995
		out = append(out, &exchange.Candle{Symbol: fixtureSymbol, Timestamp: start + int64(i)*MinuteMs, Open: open, High: hi, Low: lo, Close: cl, Volume: 10, IsClosed: true})
		price = cl
	}
	return out
}

func hookTestJob(t *testing.T, variant string, profile string) (Job, []*exchange.Candle) {
	t.Helper()
	candles := trendingMinuteCandles(fixtureDay, hookTestDays)
	hourly, err := AggregateCandles(candles, hourMs)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	seg := newSegment("tail", fixtureDay+hookTestSegStart*DayMs, fixtureDay+hookTestDays*DayMs)
	var v Variant
	for _, cand := range AllVariants() {
		if cand.Name == variant {
			v = cand
		}
	}
	var prof Profile
	for _, p := range AllProfiles() {
		if p.Name == profile {
			prof = p
		}
	}
	if v.Name == "" || prof.Name == "" {
		t.Fatalf("unknown variant %q or profile %q", variant, profile)
	}
	funding := []replay.FundingPoint{{Timestamp: fixtureDay, Rate: 0.0003}}
	spec := SymbolSpec{Symbol: fixtureSymbol, PriceDecimals: 2, QuantityDecimals: 3, OrderQuantity: 25}
	return Job{Profile: prof, Spec: spec, Base: BaseSpec{Name: "0.15%", IntervalRatio: 0.0015}, Segment: seg, Variant: v,
		Candles: sliceCandles(candles, seg.StartMs, seg.EndMs), Hourly: hourly, Funding: funding, Steps: 1}, candles
}

func TestRunJob_RegimeVariantsInjected(t *testing.T) {
	for _, name := range []string{"regime_filter", "adaptive_interval", "upper_bound_freeze", "all_on"} {
		t.Run(name, func(t *testing.T) {
			job, _ := hookTestJob(t, name, profileLiveDefault)
			s := RunJob(job)
			if s.Error != "" || s.Blocked != "" {
				t.Fatalf("run failed: error=%q blocked=%q", s.Error, s.Blocked)
			}
			if s.RegimeSharePct == nil {
				t.Fatalf("regime detector was not injected")
			}
			if s.RegimeRefreshErrors != 0 {
				t.Fatalf("detector had pre-segment history, want no refresh errors, got %d", s.RegimeRefreshErrors)
			}
			if s.RegimeSharePct["unknown"] > 5 {
				t.Fatalf("detector should be ready with warm-up history, unknown share=%.1f%%", s.RegimeSharePct["unknown"])
			}
			if name == "adaptive_interval" || name == "all_on" {
				if s.IntervalChanges == 0 || s.MeanIntervalMultiple <= 1 {
					t.Fatalf("adaptive interval should widen the grid on a volatile trend: changes=%d multiple=%.2f", s.IntervalChanges, s.MeanIntervalMultiple)
				}
			}
			if s.OrderCleanerRuns == 0 {
				t.Fatalf("live_default must run the order cleaner in sim time")
			}
		})
	}
}

func TestRunJob_FundingPricingAndBaselineDiffer(t *testing.T) {
	baseJob, _ := hookTestJob(t, "baseline", profileLiveDefault)
	fundJob, _ := hookTestJob(t, "funding_pricing", profileLiveDefault)
	base, fund := RunJob(baseJob), RunJob(fundJob)
	if base.Error != "" || fund.Error != "" {
		t.Fatalf("errors: baseline=%q funding=%q", base.Error, fund.Error)
	}
	if base.RegimeSharePct != nil {
		t.Fatalf("baseline must not inject a regime detector")
	}
	if base.NetPnL == fund.NetPnL && base.Fills == fund.Fills {
		t.Fatalf("funding pricing with rate 0.03%%/8h should change the replay (net=%.4f fills=%d)", base.NetPnL, base.Fills)
	}
}

func TestRunJob_Deterministic(t *testing.T) {
	job, _ := hookTestJob(t, "all_on", profileLiveDefault)
	a, b := RunJob(job), RunJob(job)
	if a.NetPnL != b.NetPnL || a.Fills != b.Fills || a.IntervalChanges != b.IntervalChanges || a.OrdersCanceled != b.OrdersCanceled {
		t.Fatalf("hooked replay not deterministic: %+v vs %+v", a, b)
	}
}

func TestAdvancePast(t *testing.T) {
	t0 := time.UnixMilli(fixtureDay)
	if got := advancePast(t0, t0, regimeControlCheckInterval, true); !got.Equal(t0.Add(regimeControlCheckInterval)) {
		t.Fatalf("first schedule = %v", got)
	}
	next := t0.Add(regimeControlCheckInterval)
	now := t0.Add(95 * time.Second)
	if got := advancePast(next, now, regimeControlCheckInterval, false); !got.Equal(t0.Add(120 * time.Second)) {
		t.Fatalf("skip missed periods: got %v", got)
	}
}
