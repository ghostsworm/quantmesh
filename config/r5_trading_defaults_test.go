package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGridRiskControlGetStopLossBasis(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", StopLossBasisPosition},
		{"position", StopLossBasisPosition},
		{" Equity ", StopLossBasisEquity},
		{"unknown", StopLossBasisPosition},
	}
	for _, tt := range tests {
		if got := (GridRiskControl{StopLossBasis: tt.in}).GetStopLossBasis(); got != tt.want {
			t.Errorf("GetStopLossBasis(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFeeAwareSpreadAndPostOnlyDefaults(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantOn     bool
		wantMargin float64
		wantReprc  int
	}{
		{name: "未配置時預設啟用", yaml: "trading:\n  symbol: ETHUSDT\n", wantOn: true, wantMargin: DefaultFeeAwareSafetyMarginRatio, wantReprc: DefaultPostOnlyRepriceMaxAttempts},
		{name: "只配置邊際", yaml: "trading:\n  fee_aware_spread:\n    safety_margin_ratio: 0.001\n  post_only_reprice_max_attempts: 5\n", wantOn: true, wantMargin: 0.001, wantReprc: 5},
		{name: "顯式關閉", yaml: "trading:\n  fee_aware_spread:\n    enabled: false\n", wantOn: false, wantMargin: DefaultFeeAwareSafetyMarginRatio, wantReprc: DefaultPostOnlyRepriceMaxAttempts},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			if err := yaml.Unmarshal([]byte(tt.yaml), &cfg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			fa := cfg.Trading.FeeAwareSpread
			if fa.IsEnabled() != tt.wantOn || fa.GetSafetyMarginRatio() != tt.wantMargin {
				t.Fatalf("fee_aware_spread enabled=%v margin=%v, want %v %v", fa.IsEnabled(), fa.GetSafetyMarginRatio(), tt.wantOn, tt.wantMargin)
			}
			if got := EffectivePostOnlyRepriceMaxAttempts(cfg.Trading.PostOnlyRepriceMaxAttempts); got != tt.wantReprc {
				t.Fatalf("post_only_reprice_max_attempts = %d, want %d", got, tt.wantReprc)
			}
		})
	}
}
