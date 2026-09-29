package config

import (
	"math"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGridR5FeaturesYAMLAndDefaults(t *testing.T) {
	var empty Config
	if err := yaml.Unmarshal([]byte("trading:\n  symbol: ETHUSDT\n"), &empty); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tr := empty.Trading
	if tr.RegimeFilter.Enabled || tr.AdaptiveInterval.Enabled || tr.UpperBoundFreeze.Enabled || tr.InventorySkew.Enabled ||
		empty.FundingRate.PricingEnabled || empty.FundingRate.PreSettlementPauseMinutes != 0 {
		t.Fatalf("all R5b features must default to off: %+v %+v", tr, empty.FundingRate)
	}
	if got := tr.InventorySkew.WithDefaults().Strength; got != DefaultInventorySkewStrength {
		t.Fatalf("default strength = %v", got)
	}
	if got := tr.UpperBoundFreeze.WithDefaults().ATRMultiplier; got != DefaultUpperBoundFreezeATRMultiplier {
		t.Fatalf("default atr multiplier = %v", got)
	}

	src := `
trading:
  regime_filter:
    enabled: true
    kline_interval: 4h
    min_dwell_bars: 2
  adaptive_interval:
    enabled: true
    atr_multiplier: 0.8
  upper_bound_freeze:
    enabled: true
    atr_multiplier: 2.5
  inventory_skew:
    enabled: true
    strength: 1.7
funding_rate:
  pricing_enabled: true
  pre_settlement_pause_minutes: 15
`
	var cfg Config
	if err := yaml.Unmarshal([]byte(src), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	tr = cfg.Trading
	if !tr.RegimeFilter.Enabled || tr.RegimeFilter.KlineInterval != "4h" || tr.RegimeFilter.MinDwellBars != 2 {
		t.Fatalf("regime_filter = %+v", tr.RegimeFilter)
	}
	if !tr.AdaptiveInterval.Enabled || tr.AdaptiveInterval.ATRMultiplier != 0.8 {
		t.Fatalf("adaptive_interval = %+v", tr.AdaptiveInterval)
	}
	if !tr.UpperBoundFreeze.Enabled || tr.UpperBoundFreeze.WithDefaults().ATRMultiplier != 2.5 {
		t.Fatalf("upper_bound_freeze = %+v", tr.UpperBoundFreeze)
	}
	if got := tr.InventorySkew.WithDefaults().Strength; got != 1 {
		t.Fatalf("strength should clamp to 1, got %v", got)
	}
	if !cfg.FundingRate.PricingEnabled || cfg.FundingRate.PreSettlementPauseMinutes != 15 {
		t.Fatalf("funding_rate = %+v", cfg.FundingRate)
	}
}

func TestValidateGridR5Features(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr string
	}{
		{name: "默認合法", mutate: func(c *Config) {}},
		{name: "強度超出範圍", mutate: func(c *Config) { c.Trading.InventorySkew.Strength = 1.5 }, wantErr: "inventory_skew.strength"},
		{name: "強度為負", mutate: func(c *Config) { c.Trading.InventorySkew.Strength = -0.1 }, wantErr: "inventory_skew.strength"},
		{name: "自動邊界係數為負", mutate: func(c *Config) { c.Trading.UpperBoundFreeze.ATRMultiplier = -1 }, wantErr: "upper_bound_freeze"},
		{name: "自適應 min > max", mutate: func(c *Config) {
			c.Trading.AdaptiveInterval.MinInterval = 5
			c.Trading.AdaptiveInterval.MaxInterval = 2
		}, wantErr: "min_interval"},
		{name: "ADX 退出閾值高於進入", mutate: func(c *Config) {
			c.Trading.RegimeFilter.ADXEnterThreshold = 20
			c.Trading.RegimeFilter.ADXExitThreshold = 25
		}, wantErr: "adx_exit_threshold"},
		{name: "結算前暫停分鐘為負", mutate: func(c *Config) { c.FundingRate.PreSettlementPauseMinutes = -1 }, wantErr: "pre_settlement_pause_minutes"},
		{name: "止損比例為 NaN", mutate: func(c *Config) { c.Trading.GridRiskControl.StopLossRatio = math.NaN() }, wantErr: "stop_loss_ratio"},
		{name: "止盈比例為正無窮", mutate: func(c *Config) { c.Trading.GridRiskControl.TakeProfitTriggerRatio = math.Inf(1) }, wantErr: "take_profit_trigger_ratio"},
		{name: "Bot 止盈比例越界", mutate: func(c *Config) {
			c.Trading.Symbols = []SymbolConfig{{GridRiskControl: GridRiskControl{TrailingTakeProfitRatio: 1.1}}}
		}, wantErr: "trailing_take_profit_ratio"},
		{name: "未知止損口徑", mutate: func(c *Config) { c.Trading.GridRiskControl.StopLossBasis = "equitty" }, wantErr: "stop_loss_basis"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{}
			tt.mutate(c)
			err := c.validateGridR5Features()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
