package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func intPtr(v int) *int { return &v }

func newR5GlobalConfig() *Config {
	c := &Config{}
	c.Trading.FeeAwareSpread = FeeAwareSpreadConfig{Enabled: BoolPtr(true), SafetyMarginRatio: 0.0003}
	c.Trading.PostOnlyRepriceMaxAttempts = 5
	c.Trading.RegimeFilter = RegimeFilterConfig{Enabled: true, KlineInterval: "4h", ADXPeriod: 20}
	c.Trading.AdaptiveInterval = AdaptiveIntervalConfig{Enabled: true, ATRMultiplier: 0.8, MinInterval: 10, MaxInterval: 100}
	c.Trading.UpperBoundFreeze = UpperBoundFreezeConfig{Enabled: true, ATRMultiplier: 2}
	c.Trading.InventorySkew = InventorySkewConfig{Enabled: true, Strength: 0.4}
	c.FundingRate.PricingEnabled = true
	c.FundingRate.PreSettlementPauseMinutes = 15
	return c
}

func TestApplyBotTradingOverrides(t *testing.T) {
	tests := []struct {
		name      string
		overrides *BotTradingOverrides
		check     func(t *testing.T, got Config, global *Config)
	}{
		{
			name:      "nil 覆蓋沿用全局",
			overrides: nil,
			check: func(t *testing.T, got Config, global *Config) {
				if !reflect.DeepEqual(got.Trading.RegimeFilter, global.Trading.RegimeFilter) ||
					got.Trading.PostOnlyRepriceMaxAttempts != 5 || !got.FundingRate.PricingEnabled {
					t.Fatalf("nil overrides changed config: %+v", got.Trading)
				}
			},
		},
		{
			name:      "空覆蓋沿用全局",
			overrides: &BotTradingOverrides{FundingRate: &BotFundingRateOverrides{}},
			check: func(t *testing.T, got Config, global *Config) {
				if got.FundingRate.PreSettlementPauseMinutes != 15 || !got.Trading.FeeAwareSpread.IsEnabled() {
					t.Fatalf("empty overrides changed config")
				}
			},
		},
		{
			name:      "fee_aware_spread 按字段合併：只關閉 enabled 保留全局安全邊際",
			overrides: &BotTradingOverrides{FeeAwareSpread: &FeeAwareSpreadConfig{Enabled: BoolPtr(false)}},
			check: func(t *testing.T, got Config, _ *Config) {
				if got.Trading.FeeAwareSpread.IsEnabled() {
					t.Fatal("fee_aware_spread should be disabled")
				}
				if got.Trading.FeeAwareSpread.SafetyMarginRatio != 0.0003 {
					t.Fatalf("safety margin = %v, want global 0.0003", got.Trading.FeeAwareSpread.SafetyMarginRatio)
				}
			},
		},
		{
			name:      "fee_aware_spread 只覆蓋安全邊際",
			overrides: &BotTradingOverrides{FeeAwareSpread: &FeeAwareSpreadConfig{SafetyMarginRatio: 0.001}},
			check: func(t *testing.T, got Config, _ *Config) {
				if !got.Trading.FeeAwareSpread.IsEnabled() || got.Trading.FeeAwareSpread.SafetyMarginRatio != 0.001 {
					t.Fatalf("got %+v", got.Trading.FeeAwareSpread)
				}
			},
		},
		{
			name:      "post_only_reprice_max_attempts 顯式 0 覆蓋為預設",
			overrides: &BotTradingOverrides{PostOnlyRepriceMaxAttempts: intPtr(0)},
			check: func(t *testing.T, got Config, _ *Config) {
				if got.Trading.PostOnlyRepriceMaxAttempts != 0 {
					t.Fatalf("got %d, want 0", got.Trading.PostOnlyRepriceMaxAttempts)
				}
			},
		},
		{
			name:      "regime_filter 整段替換",
			overrides: &BotTradingOverrides{RegimeFilter: &RegimeFilterConfig{Enabled: false}},
			check: func(t *testing.T, got Config, _ *Config) {
				if got.Trading.RegimeFilter != (RegimeFilterConfig{}) {
					t.Fatalf("regime_filter not replaced wholesale: %+v", got.Trading.RegimeFilter)
				}
				if !got.Trading.AdaptiveInterval.Enabled {
					t.Fatal("adaptive_interval should keep global")
				}
			},
		},
		{
			name: "adaptive_interval / upper_bound_freeze / inventory_skew 覆蓋",
			overrides: &BotTradingOverrides{
				AdaptiveInterval: &AdaptiveIntervalConfig{Enabled: false},
				UpperBoundFreeze: &UpperBoundFreezeConfig{Enabled: true, ATRMultiplier: 4},
				InventorySkew:    &InventorySkewConfig{Enabled: true, Strength: 0.9},
			},
			check: func(t *testing.T, got Config, _ *Config) {
				if got.Trading.AdaptiveInterval.Enabled || got.Trading.UpperBoundFreeze.ATRMultiplier != 4 ||
					got.Trading.InventorySkew.Strength != 0.9 {
					t.Fatalf("got %+v %+v %+v", got.Trading.AdaptiveInterval, got.Trading.UpperBoundFreeze, got.Trading.InventorySkew)
				}
			},
		},
		{
			name:      "funding_rate 按字段合併",
			overrides: &BotTradingOverrides{FundingRate: &BotFundingRateOverrides{PricingEnabled: BoolPtr(false)}},
			check: func(t *testing.T, got Config, _ *Config) {
				if got.FundingRate.PricingEnabled || got.FundingRate.PreSettlementPauseMinutes != 15 {
					t.Fatalf("got %+v", got.FundingRate)
				}
			},
		},
		{
			name:      "funding_rate 暫停分鐘覆蓋為 0",
			overrides: &BotTradingOverrides{FundingRate: &BotFundingRateOverrides{PreSettlementPauseMinutes: intPtr(0)}},
			check: func(t *testing.T, got Config, _ *Config) {
				if !got.FundingRate.PricingEnabled || got.FundingRate.PreSettlementPauseMinutes != 0 {
					t.Fatalf("got %+v", got.FundingRate)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			global := newR5GlobalConfig()
			snapshot := *newR5GlobalConfig()
			got := MergeBotTradingOverrides(global, tt.overrides)
			tt.check(t, got, global)
			if !reflect.DeepEqual(global.Trading.FeeAwareSpread, snapshot.Trading.FeeAwareSpread) ||
				!reflect.DeepEqual(global.Trading.RegimeFilter, snapshot.Trading.RegimeFilter) ||
				global.FundingRate != snapshot.FundingRate || global.Trading.PostOnlyRepriceMaxAttempts != 5 {
				t.Fatal("global config was mutated")
			}
		})
	}
}

func TestApplyBotTradingOverridesDoesNotAliasBotPointer(t *testing.T) {
	o := &BotTradingOverrides{FeeAwareSpread: &FeeAwareSpreadConfig{Enabled: BoolPtr(false)}}
	local := MergeBotTradingOverrides(newR5GlobalConfig(), o)
	*local.Trading.FeeAwareSpread.Enabled = true
	if *o.FeeAwareSpread.Enabled {
		t.Fatal("merged config aliases override pointer")
	}
}

func TestValidateBotTradingOverrides(t *testing.T) {
	tests := []struct {
		name      string
		global    *Config
		overrides *BotTradingOverrides
		wantErr   string
	}{
		{name: "nil 覆蓋", global: newR5GlobalConfig()},
		{name: "合法覆蓋", global: newR5GlobalConfig(), overrides: &BotTradingOverrides{InventorySkew: &InventorySkewConfig{Enabled: true, Strength: 1}}},
		{name: "庫存偏斜強度越界", global: newR5GlobalConfig(), overrides: &BotTradingOverrides{InventorySkew: &InventorySkewConfig{Strength: 1.5}}, wantErr: "inventory_skew.strength"},
		{name: "自適應間隔 min>max", global: nil, overrides: &BotTradingOverrides{AdaptiveInterval: &AdaptiveIntervalConfig{MinInterval: 50, MaxInterval: 10}}, wantErr: "min_interval"},
		{name: "上沿冻结負倍數", global: nil, overrides: &BotTradingOverrides{UpperBoundFreeze: &UpperBoundFreezeConfig{ATRMultiplier: -1}}, wantErr: "upper_bound_freeze"},
		{name: "ADX 閾值倒置", global: nil, overrides: &BotTradingOverrides{RegimeFilter: &RegimeFilterConfig{ADXEnterThreshold: 20, ADXExitThreshold: 30}}, wantErr: "adx_exit_threshold"},
		{name: "結算前暫停負數", global: newR5GlobalConfig(), overrides: &BotTradingOverrides{FundingRate: &BotFundingRateOverrides{PreSettlementPauseMinutes: intPtr(-1)}}, wantErr: "pre_settlement_pause_minutes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBotTradingOverrides(tt.global, tt.overrides)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), "trading_overrides") {
				t.Fatalf("error = %v, want contains %q", err, tt.wantErr)
			}
		})
	}
}

func TestInheritStopLossBasis(t *testing.T) {
	tests := []struct {
		name, bot, global, want string
	}{
		{"空值繼承全局", "", StopLossBasisEquity, StopLossBasisEquity},
		{"空白繼承全局", "  ", StopLossBasisEquity, StopLossBasisEquity},
		{"Bot 值優先", StopLossBasisPosition, StopLossBasisEquity, StopLossBasisPosition},
		{"都為空", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := GridRiskControl{StopLossBasis: tt.bot}
			InheritStopLossBasis(&g, GridRiskControl{StopLossBasis: tt.global})
			if g.StopLossBasis != tt.want {
				t.Fatalf("got %q want %q", g.StopLossBasis, tt.want)
			}
		})
	}
	InheritStopLossBasis(nil, GridRiskControl{}) // nil 安全
}

func sampleOverrides() *BotTradingOverrides {
	return &BotTradingOverrides{
		FeeAwareSpread:             &FeeAwareSpreadConfig{Enabled: BoolPtr(false), SafetyMarginRatio: 0.0005},
		PostOnlyRepriceMaxAttempts: intPtr(2),
		RegimeFilter:               &RegimeFilterConfig{Enabled: true, KlineInterval: "15m"},
		AdaptiveInterval:           &AdaptiveIntervalConfig{Enabled: true, MinInterval: 5, MaxInterval: 50},
		UpperBoundFreeze:           &UpperBoundFreezeConfig{Enabled: true, ATRMultiplier: 2.5},
		InventorySkew:              &InventorySkewConfig{Enabled: true, Strength: 0.3},
		FundingRate:                &BotFundingRateOverrides{PricingEnabled: BoolPtr(true), PreSettlementPauseMinutes: intPtr(0)},
	}
}

// TestBotTradingOverridesRoundTrip 覆蓋項在 SymbolConfig ↔ BotConfig ↔ BotConfigFile 轉換與 JSON/YAML 序列化（bot_configs 表、app_config 快照、bots/*.yaml）中不丟失
func TestBotTradingOverridesRoundTrip(t *testing.T) {
	want := sampleOverrides()
	sc := SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", TradingOverrides: want}
	bc := SymbolConfigToBotConfig(sc, false)
	bf := ConvertFromBotConfig(bc)

	jsonBytes, err := json.Marshal(bf)
	if err != nil {
		t.Fatal(err)
	}
	var fromJSON BotConfigFile
	if err := json.Unmarshal(jsonBytes, &fromJSON); err != nil {
		t.Fatal(err)
	}
	yamlBytes, err := yaml.Marshal(&fromJSON)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML BotConfigFile
	if err := yaml.Unmarshal(yamlBytes, &fromYAML); err != nil {
		t.Fatal(err)
	}
	back := BotConfigToSymbolConfig(ConvertToBotConfig(&fromYAML))
	if !reflect.DeepEqual(back.TradingOverrides, want) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", back.TradingOverrides, want)
	}
	// 顯式 0 值必須保留（與「未設置」區分）
	if back.TradingOverrides.FundingRate.PreSettlementPauseMinutes == nil {
		t.Fatal("explicit zero pre_settlement_pause_minutes lost")
	}

	// 主配置 JSON 快照（app_config）中的 bots
	cfg := Config{Bots: []BotConfig{bc}}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var cfgBack Config
	if err := json.Unmarshal(cfgJSON, &cfgBack); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfgBack.Bots[0].TradingOverrides, want) {
		t.Fatalf("app_config JSON round trip mismatch: %+v", cfgBack.Bots[0].TradingOverrides)
	}

	// 未設置時不輸出字段
	empty, err := json.Marshal(ConvertFromBotConfig(BotConfig{ID: "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(empty), "trading_overrides") {
		t.Fatal("trading_overrides should be omitted when nil")
	}
}

func TestBotTradingOverridesYAMLInBotsSection(t *testing.T) {
	src := `
bots:
  - id: b1
    exchange: binance
    symbol: ETHUSDT
    trading_overrides:
      fee_aware_spread:
        safety_margin_ratio: 0.0004
      regime_filter:
        enabled: true
        kline_interval: 1h
      funding_rate:
        pricing_enabled: false
`
	var c Config
	if err := yaml.Unmarshal([]byte(src), &c); err != nil {
		t.Fatal(err)
	}
	o := c.Bots[0].TradingOverrides
	if o == nil || o.FeeAwareSpread == nil || o.FeeAwareSpread.SafetyMarginRatio != 0.0004 ||
		o.RegimeFilter == nil || o.RegimeFilter.KlineInterval != "1h" ||
		o.FundingRate == nil || o.FundingRate.PricingEnabled == nil || *o.FundingRate.PricingEnabled ||
		o.InventorySkew != nil || o.PostOnlyRepriceMaxAttempts != nil {
		t.Fatalf("unexpected overrides: %+v", o)
	}
}
