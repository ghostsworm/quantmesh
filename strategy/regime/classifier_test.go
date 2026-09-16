package regime

import (
	"math"
	"testing"
)

func testClassifierConfig(dwell int) RegimeConfig {
	return RegimeConfig{
		ADXEnterThreshold: 25,
		ADXExitThreshold:  20,
		EMASlopeMinATR:    0.05,
		MinDwellBars:      dwell,
	}
}

func m(adx, slope float64) metrics {
	return metrics{ok: true, adx: adx, slope: slope, atr: 1}
}

func TestClassifierSequences(t *testing.T) {
	tests := []struct {
		name  string
		dwell int
		steps []metrics
		want  []Regime
	}{
		{
			name:  "first ready commits immediately",
			dwell: 3,
			steps: []metrics{m(30, 0.1)},
			want:  []Regime{TrendUp},
		},
		{
			name:  "enter needs both adx and slope",
			dwell: 1,
			steps: []metrics{m(10, 0), m(30, 0.01), m(24.9, 0.5), m(25, 0.05), m(25, -0.05)},
			want:  []Regime{Range, Range, Range, TrendUp, TrendDown},
		},
		{
			name:  "hysteresis keeps trend between exit and enter",
			dwell: 1,
			steps: []metrics{m(30, 0.1), m(22, 0.01), m(20, 0.001), m(19.9, 0.1)},
			want:  []Regime{TrendUp, TrendUp, TrendUp, Range},
		},
		{
			name:  "slope reversal leaves trend",
			dwell: 1,
			steps: []metrics{m(30, -0.1), m(30, 0.001)},
			want:  []Regime{TrendDown, Range},
		},
		{
			name:  "strong reversal flips directly",
			dwell: 1,
			steps: []metrics{m(30, 0.1), m(30, -0.2)},
			want:  []Regime{TrendUp, TrendDown},
		},
		{
			name:  "dwell requires consecutive confirmation",
			dwell: 3,
			steps: []metrics{m(10, 0), m(30, 0.1), m(30, 0.1), m(10, 0), m(30, 0.1), m(30, 0.1), m(30, 0.1)},
			want:  []Regime{Range, Range, Range, Range, Range, Range, TrendUp},
		},
		{
			name:  "changing candidate resets pending count",
			dwell: 2,
			steps: []metrics{m(10, 0), m(30, 0.1), m(30, -0.1), m(30, -0.1)},
			want:  []Regime{Range, Range, Range, TrendDown},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newClassifier(testClassifierConfig(tt.dwell))
			for i, s := range tt.steps {
				if got := c.step(s, int64(i)); got != tt.want[i] {
					t.Fatalf("step %d: got %s want %s", i, got, tt.want[i])
				}
			}
		})
	}
}

func TestClassifierDwellBookkeeping(t *testing.T) {
	c := newClassifier(testClassifierConfig(2))
	c.step(m(10, 0), 100)
	c.step(m(10, 0), 200)
	c.step(m(30, 0.1), 300) // pending
	if c.regime != Range || c.since != 100 || c.barsIn != 3 {
		t.Fatalf("unexpected state regime=%s since=%d barsIn=%d", c.regime, c.since, c.barsIn)
	}
	c.step(m(30, 0.1), 400) // commit
	if c.regime != TrendUp || c.since != 400 || c.barsIn != 1 {
		t.Fatalf("unexpected state after commit regime=%s since=%d barsIn=%d", c.regime, c.since, c.barsIn)
	}
}

func TestMetricsAtWarmup(t *testing.T) {
	cfg := RegimeConfig{EMASlopeLookback: 2, ATRPercentileLookback: 3}
	nan := math.NaN()
	s := series{
		adx:     []float64{nan, 30, 30, 30, 30},
		plusDI:  []float64{nan, 1, 1, 1, 1},
		minusDI: []float64{nan, 1, 1, 1, 1},
		ema:     []float64{nan, 100, 101, 102, 103},
		atr:     []float64{nan, 2, 1, 3, 2},
	}
	tests := []struct {
		i         int
		wantOK    bool
		wantSlope float64
		wantPct   float64
	}{
		{i: 0}, {i: 2}, // EMA prev / ATR window NaN
		{i: 3, wantOK: true, wantSlope: (102.0 - 100) / (2 * 3), wantPct: 100 * 2.5 / 3},
		{i: 4, wantOK: true, wantSlope: (103.0 - 101) / (2 * 2), wantPct: 50},
		{i: 5}, {i: -1},
	}
	for _, tt := range tests {
		got := metricsAt(s, tt.i, cfg)
		if got.ok != tt.wantOK {
			t.Fatalf("i=%d ok=%v want %v", tt.i, got.ok, tt.wantOK)
		}
		if !tt.wantOK {
			continue
		}
		if math.Abs(got.slope-tt.wantSlope) > 1e-9 || math.Abs(got.atrPercentile-tt.wantPct) > 1e-9 {
			t.Fatalf("i=%d slope=%v pct=%v want slope=%v pct=%v", tt.i, got.slope, got.atrPercentile, tt.wantSlope, tt.wantPct)
		}
	}
}

func TestRegimeStringAndPolicy(t *testing.T) {
	names := map[Regime]string{Unknown: "unknown", Range: "range", TrendUp: "trend_up", TrendDown: "trend_down", Regime(99): "unknown"}
	for r, want := range names {
		if r.String() != want {
			t.Errorf("%d.String()=%s want %s", r, r.String(), want)
		}
	}

	tests := []struct {
		r    Regime
		dir  Direction
		want GridPolicy
	}{
		{Range, DirectionLong, GridPolicy{EntryWindowScale: 1, IntervalScale: 1}},
		{Range, DirectionShort, GridPolicy{EntryWindowScale: 1, IntervalScale: 1}},
		{TrendDown, DirectionLong, GridPolicy{EntryWindowScale: 0.5, IntervalScale: 1.5}},
		{TrendUp, DirectionShort, GridPolicy{EntryWindowScale: 0.5, IntervalScale: 1.5}},
		{TrendUp, DirectionLong, GridPolicy{EntryWindowScale: 1, IntervalScale: 1, FreezeFavorableBound: true}},
		{TrendDown, DirectionShort, GridPolicy{EntryWindowScale: 1, IntervalScale: 1, FreezeFavorableBound: true}},
		{Unknown, DirectionLong, GridPolicy{EntryWindowScale: 0.5, IntervalScale: 1, FreezeFavorableBound: true}},
	}
	for _, tt := range tests {
		if got := PolicyFor(tt.r, tt.dir); got != tt.want {
			t.Errorf("PolicyFor(%s,%s)=%+v want %+v", tt.r, tt.dir, got, tt.want)
		}
	}

	s := Snapshot{Regime: TrendUp, Ready: true}
	if s.Effective() != TrendUp {
		t.Error("ready fresh snapshot should be effective")
	}
	s.Stale = true
	if s.Effective() != Unknown {
		t.Error("stale snapshot should be Unknown")
	}
	if (Snapshot{Regime: Range}).Effective() != Unknown {
		t.Error("not ready snapshot should be Unknown")
	}
}
