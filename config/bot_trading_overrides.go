package config

import (
	"fmt"
	"strings"
)

// 本文件定義 R5 新配置（費率感知利差、PostOnly 重定價、K 線 regime、自適應間隔、上沿冻结、庫存偏斜、資金費定價）的按 Bot 覆蓋。
//
// 合併規則：Bot 上設置了某項（指針非 nil）則使用 Bot 的值，否則沿用全局 trading.* / funding_rate.*。
// 結構體類配置（regime_filter / adaptive_interval / upper_bound_freeze / inventory_skew）按整段替換：
// Bot 寫了該段，則該段全部字段取 Bot 的值（未寫的字段為零值，由各自默認值補齊），不與全局逐字段混合。
// fee_aware_spread 按字段合併：enabled 非 nil 時覆蓋，safety_margin_ratio > 0 時覆蓋。

// BotTradingOverrides 單個 Bot 對 R5 全局配置的覆蓋（全部可選）
type BotTradingOverrides struct {
	// FeeAwareSpread 覆蓋 trading.fee_aware_spread（按字段合併）
	FeeAwareSpread *FeeAwareSpreadConfig `yaml:"fee_aware_spread,omitempty" json:"fee_aware_spread,omitempty"`
	// PostOnlyRepriceMaxAttempts 覆蓋 trading.post_only_reprice_max_attempts（0=預設 3）
	PostOnlyRepriceMaxAttempts *int `yaml:"post_only_reprice_max_attempts,omitempty" json:"post_only_reprice_max_attempts,omitempty"`
	// RegimeFilter 覆蓋 trading.regime_filter（整段替換）
	RegimeFilter *RegimeFilterConfig `yaml:"regime_filter,omitempty" json:"regime_filter,omitempty"`
	// AdaptiveInterval 覆蓋 trading.adaptive_interval（整段替換）
	AdaptiveInterval *AdaptiveIntervalConfig `yaml:"adaptive_interval,omitempty" json:"adaptive_interval,omitempty"`
	// UpperBoundFreeze 覆蓋 trading.upper_bound_freeze（整段替換）
	UpperBoundFreeze *UpperBoundFreezeConfig `yaml:"upper_bound_freeze,omitempty" json:"upper_bound_freeze,omitempty"`
	// InventorySkew 覆蓋 trading.inventory_skew（整段替換）
	InventorySkew *InventorySkewConfig `yaml:"inventory_skew,omitempty" json:"inventory_skew,omitempty"`
	// FundingRate 覆蓋 funding_rate.pricing_enabled / pre_settlement_pause_minutes（按字段合併）
	FundingRate *BotFundingRateOverrides `yaml:"funding_rate,omitempty" json:"funding_rate,omitempty"`
}

// BotFundingRateOverrides 資金費定價的按 Bot 覆蓋
type BotFundingRateOverrides struct {
	PricingEnabled            *bool `yaml:"pricing_enabled,omitempty" json:"pricing_enabled,omitempty"`
	PreSettlementPauseMinutes *int  `yaml:"pre_settlement_pause_minutes,omitempty" json:"pre_settlement_pause_minutes,omitempty"`
}

// IsEmpty 是否沒有任何覆蓋項
func (o *BotTradingOverrides) IsEmpty() bool {
	if o == nil {
		return true
	}
	fundingEmpty := o.FundingRate == nil ||
		(o.FundingRate.PricingEnabled == nil && o.FundingRate.PreSettlementPauseMinutes == nil)
	return o.FeeAwareSpread == nil && o.PostOnlyRepriceMaxAttempts == nil && o.RegimeFilter == nil &&
		o.AdaptiveInterval == nil && o.UpperBoundFreeze == nil && o.InventorySkew == nil && fundingEmpty
}

// ApplyBotTradingOverrides 把 Bot 級覆蓋寫入本 Bot 的局部配置 local（local 應為全局配置的副本，不得是全局指針本身）。
// o 為 nil 時不做任何修改；不會修改 o。
func ApplyBotTradingOverrides(local *Config, o *BotTradingOverrides) {
	if local == nil || o == nil {
		return
	}
	t := &local.Trading
	if f := o.FeeAwareSpread; f != nil {
		if f.Enabled != nil {
			enabled := *f.Enabled
			t.FeeAwareSpread.Enabled = &enabled
		}
		if f.SafetyMarginRatio > 0 {
			t.FeeAwareSpread.SafetyMarginRatio = f.SafetyMarginRatio
		}
	}
	if o.PostOnlyRepriceMaxAttempts != nil {
		t.PostOnlyRepriceMaxAttempts = *o.PostOnlyRepriceMaxAttempts
	}
	if o.RegimeFilter != nil {
		t.RegimeFilter = *o.RegimeFilter
	}
	if o.AdaptiveInterval != nil {
		t.AdaptiveInterval = *o.AdaptiveInterval
	}
	if o.UpperBoundFreeze != nil {
		t.UpperBoundFreeze = *o.UpperBoundFreeze
	}
	if o.InventorySkew != nil {
		t.InventorySkew = *o.InventorySkew
	}
	if fr := o.FundingRate; fr != nil {
		if fr.PricingEnabled != nil {
			local.FundingRate.PricingEnabled = *fr.PricingEnabled
		}
		if fr.PreSettlementPauseMinutes != nil {
			local.FundingRate.PreSettlementPauseMinutes = *fr.PreSettlementPauseMinutes
		}
	}
}

// InheritStopLossBasis Bot 級 grid_risk_control.stop_loss_basis 為空時沿用全局值。
// config.Validate 只對 trading.symbols 做了該繼承，按 bots 啟動的路徑需在構造局部配置時調用。
func InheritStopLossBasis(bot *GridRiskControl, global GridRiskControl) {
	if bot == nil || strings.TrimSpace(bot.StopLossBasis) != "" {
		return
	}
	bot.StopLossBasis = global.StopLossBasis
}

// MergeBotTradingOverrides 返回「全局配置副本 + Bot 覆蓋」後的配置，不修改 global。
func MergeBotTradingOverrides(global *Config, o *BotTradingOverrides) Config {
	var merged Config
	if global != nil {
		merged = *global
	}
	ApplyBotTradingOverrides(&merged, o)
	return merged
}

// ValidateGridR5Features 校驗 R5 網格配置（供按 Bot 合併後的局部配置複用全局校驗）
func (c *Config) ValidateGridR5Features() error {
	if c == nil {
		return nil
	}
	return c.validateGridR5Features()
}

// ValidateBotTradingOverrides 按「全局 + Bot 覆蓋」合併後校驗；global 為 nil 時只校驗覆蓋項本身。
// 錯誤信息帶 bot 級前綴，便於定位是哪個 Bot 的覆蓋非法。
func ValidateBotTradingOverrides(global *Config, o *BotTradingOverrides) error {
	if o.IsEmpty() {
		return nil
	}
	merged := MergeBotTradingOverrides(global, o)
	if err := merged.validateGridR5Features(); err != nil {
		return fmt.Errorf("trading_overrides（按 Bot 覆蓋）: %w", err)
	}
	return nil
}
