package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestRiskAnomalyDetectorConfigResolve(t *testing.T) {
	tests := []struct {
		name      string
		cfg       RiskAnomalyDetectorConfig
		monitors  int
		recovery  int
		wantDrop  float64
		wantK     float64
		wantPanic int
		wantRecov int
		wantTol   float64
		wantStale int
	}{
		{"單交易對默認：跌幅門檻×2，恢復閾值夾緊到1", RiskAnomalyDetectorConfig{}, 1, 3, 1.0, 3, 1, 1, 0.1, 3},
		{"五交易對默認：需全部異常", RiskAnomalyDetectorConfig{}, 5, 3, 0.5, 3, 5, 3, 0.1, 3},
		{"min_panic_symbols=1 多交易對時提升到 2", RiskAnomalyDetectorConfig{MinPanicSymbols: 1}, 3, 0, 0.5, 3, 2, 1, 0.1, 3},
		{"min_panic_symbols 超過監控數夾緊", RiskAnomalyDetectorConfig{MinPanicSymbols: 9}, 3, 9, 0.5, 3, 3, 3, 0.1, 3},
		{"負數關閉門檻", RiskAnomalyDetectorConfig{MinPriceDropPct: -1, VolatilityMultiplier: -1, SingleSymbolDropFactor: -1, RecoveryMaxDropPct: -1, StaleBars: -1}, 1, 1, 0, 0, 1, 1, 0, 0},
		{"顯式值", RiskAnomalyDetectorConfig{MinPriceDropPct: 0.8, VolatilityMultiplier: 4, SingleSymbolDropFactor: 1.5, RecoveryMaxDropPct: 0.2, StaleBars: 5}, 1, 1, 1.2, 4, 1, 1, 0.2, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.cfg.Resolve(tt.monitors, tt.recovery)
			if abs(r.MinPriceDropPct-tt.wantDrop) > 1e-9 || r.VolatilityMultiplier != tt.wantK || r.MinPanicSymbols != tt.wantPanic ||
				r.RecoveryThreshold != tt.wantRecov || r.RecoveryMaxDropPct != tt.wantTol || r.StaleBars != tt.wantStale {
				t.Fatalf("Resolve=%+v", r)
			}
		})
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestRiskControlYAMLInlineKeys(t *testing.T) {
	var cfg Config
	src := `
risk_control:
  enabled: true
  volume_multiplier: 4
  recovery_threshold: 2
  min_price_drop_pct: 0.7
  volatility_multiplier: 2.5
  single_symbol_drop_factor: 1.5
  min_panic_symbols: 2
  recovery_max_drop_pct: 0.15
  stale_bars: 4
`
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatal(err)
	}
	rc := cfg.RiskControl
	if rc.VolumeMultiplier != 4 || rc.RecoveryThreshold != 2 || rc.MinPriceDropPct != 0.7 || rc.VolatilityMultiplier != 2.5 ||
		rc.SingleSymbolDropFactor != 1.5 || rc.MinPanicSymbols != 2 || rc.RecoveryMaxDropPct != 0.15 || rc.StaleBars != 4 {
		t.Fatalf("解析結果不符: %+v", rc)
	}
}
