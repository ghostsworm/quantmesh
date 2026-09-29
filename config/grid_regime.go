package config

import (
	"fmt"
	"math"
)

// 本文件定義 R5 第二段網格改進（K 線 regime 過濾、ATR 自適應間隔、上沿冻结、庫存偏斜、資金費定價）的配置。
//
// 注意：strategy/regime 依賴 exchange，而 exchange 依賴 config，config 不能 import regime（循環依賴）。
// 因此 RegimeFilterConfig / AdaptiveIntervalConfig 在此逐字段鏡像 regime.RegimeConfig / regime.AdaptiveIntervalConfig
// （yaml/json 標籤一致），由 position 包轉換並調用 regime 的 WithDefaults/Validate；
// position 包內有反射測試保證兩邊字段不漂移。

// RegimeFilterConfig K 線級市場狀態過濾（鏡像 regime.RegimeConfig；零值字段由 regime.RegimeConfig.WithDefaults 補默認值）
type RegimeFilterConfig struct {
	Enabled               bool    `yaml:"enabled" json:"enabled"`
	KlineInterval         string  `yaml:"kline_interval" json:"kline_interval"`
	ADXPeriod             int     `yaml:"adx_period" json:"adx_period"`
	ADXEnterThreshold     float64 `yaml:"adx_enter_threshold" json:"adx_enter_threshold"`
	ADXExitThreshold      float64 `yaml:"adx_exit_threshold" json:"adx_exit_threshold"`
	EMAPeriod             int     `yaml:"ema_period" json:"ema_period"`
	EMASlopeLookback      int     `yaml:"ema_slope_lookback" json:"ema_slope_lookback"`
	EMASlopeMinATR        float64 `yaml:"ema_slope_min_atr" json:"ema_slope_min_atr"`
	MinDwellBars          int     `yaml:"min_dwell_bars" json:"min_dwell_bars"`
	ATRPeriod             int     `yaml:"atr_period" json:"atr_period"`
	ATRPercentileLookback int     `yaml:"atr_percentile_lookback" json:"atr_percentile_lookback"`
	BootstrapBars         int     `yaml:"bootstrap_bars" json:"bootstrap_bars"`
	PollIntervalSeconds   int     `yaml:"poll_interval_seconds" json:"poll_interval_seconds"`
	StaleMultiplier       float64 `yaml:"stale_multiplier" json:"stale_multiplier"`
}

// AdaptiveIntervalConfig ATR 自適應網格間隔（鏡像 regime.AdaptiveIntervalConfig；默認值見 regime.AdaptiveIntervalConfig.WithDefaults）
type AdaptiveIntervalConfig struct {
	Enabled              bool    `yaml:"enabled" json:"enabled"`
	ATRMultiplier        float64 `yaml:"atr_multiplier" json:"atr_multiplier"`
	MinInterval          float64 `yaml:"min_interval" json:"min_interval"`
	MaxInterval          float64 `yaml:"max_interval" json:"max_interval"`
	ChangeThresholdRatio float64 `yaml:"change_threshold_ratio" json:"change_threshold_ratio"`
}

// DefaultUpperBoundFreezeATRMultiplier 自動邊界 = EMA ± k×ATR 的默認 k
const DefaultUpperBoundFreezeATRMultiplier = 3.0

// UpperBoundFreezeConfig 基於 ATR 的自動價格邊界（LONG 自動 price_high = EMA + k×ATR；SHORT 自動 price_low = EMA − k×ATR）。
// 與手動 price_high/price_low 同為軟限制：超出後只保留平倉單，不在邊界外開倉。需要 K 線檢測器（無需 regime_filter.enabled）。
type UpperBoundFreezeConfig struct {
	Enabled       bool    `yaml:"enabled" json:"enabled"`
	ATRMultiplier float64 `yaml:"atr_multiplier" json:"atr_multiplier"` // k，<=0 時使用 DefaultUpperBoundFreezeATRMultiplier
}

// WithDefaults 返回補齊默認值後的副本
func (c UpperBoundFreezeConfig) WithDefaults() UpperBoundFreezeConfig {
	if c.ATRMultiplier <= 0 || math.IsNaN(c.ATRMultiplier) || math.IsInf(c.ATRMultiplier, 0) {
		c.ATRMultiplier = DefaultUpperBoundFreezeATRMultiplier
	}
	return c
}

// DefaultInventorySkewStrength 庫存偏斜默認強度
const DefaultInventorySkewStrength = 0.5

// maxInventorySkewStrength 庫存偏斜強度上限
const maxInventorySkewStrength = 1.0

// InventorySkewConfig 庫存偏斜：持倉越多越少開倉、開倉價越遠、平倉利差越小（不低於手續費下界）
type InventorySkewConfig struct {
	Enabled  bool    `yaml:"enabled" json:"enabled"`
	Strength float64 `yaml:"strength" json:"strength"` // 0..1，<=0 時使用 DefaultInventorySkewStrength，>1 截斷為 1
}

// WithDefaults 返回補齊默認值並截斷到合法範圍的副本
func (c InventorySkewConfig) WithDefaults() InventorySkewConfig {
	if c.Strength <= 0 || math.IsNaN(c.Strength) {
		c.Strength = DefaultInventorySkewStrength
	}
	if c.Strength > maxInventorySkewStrength {
		c.Strength = maxInventorySkewStrength
	}
	return c
}

// validateGridR5Features 校驗 R5 第二段網格配置中可在 config 包內判斷的部分；
// regime 相關的完整校驗在啟動時由 regime.NewDetector 完成（失敗會阻止 Bot 啟動）。
func (c *Config) validateGridR5Features() error {
	t := c.Trading
	if err := t.GridRiskControl.Validate("trading.grid_risk_control"); err != nil {
		return err
	}
	for i, symbol := range t.Symbols {
		if err := symbol.GridRiskControl.Validate(fmt.Sprintf("trading.symbols[%d].grid_risk_control", i)); err != nil {
			return err
		}
	}
	if t.InventorySkew.Strength < 0 || t.InventorySkew.Strength > maxInventorySkewStrength {
		return fmt.Errorf("trading.inventory_skew.strength 必須在 [0, 1] 範圍內，當前值: %v", t.InventorySkew.Strength)
	}
	if t.UpperBoundFreeze.ATRMultiplier < 0 {
		return fmt.Errorf("trading.upper_bound_freeze.atr_multiplier 不能為負數，當前值: %v", t.UpperBoundFreeze.ATRMultiplier)
	}
	a := t.AdaptiveInterval
	if a.ATRMultiplier < 0 || a.MinInterval < 0 || a.MaxInterval < 0 || a.ChangeThresholdRatio < 0 {
		return fmt.Errorf("trading.adaptive_interval 數值不能為負數 (atr_multiplier=%v min=%v max=%v change_threshold_ratio=%v)",
			a.ATRMultiplier, a.MinInterval, a.MaxInterval, a.ChangeThresholdRatio)
	}
	if a.MaxInterval > 0 && a.MinInterval > a.MaxInterval {
		return fmt.Errorf("trading.adaptive_interval.min_interval (%v) 不能大於 max_interval (%v)", a.MinInterval, a.MaxInterval)
	}
	r := t.RegimeFilter
	if r.ADXEnterThreshold > 0 && r.ADXExitThreshold > r.ADXEnterThreshold {
		return fmt.Errorf("trading.regime_filter.adx_exit_threshold (%v) 不能大於 adx_enter_threshold (%v)", r.ADXExitThreshold, r.ADXEnterThreshold)
	}
	if c.FundingRate.PreSettlementPauseMinutes < 0 {
		return fmt.Errorf("funding_rate.pre_settlement_pause_minutes 不能為負數，當前值: %d", c.FundingRate.PreSettlementPauseMinutes)
	}
	return nil
}
