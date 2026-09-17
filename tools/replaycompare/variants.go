package main

import (
	"fmt"
	"math"

	"quantmesh/backtest/replay"
	"quantmesh/config"
)

// 基礎網格默認值（見報告「假設」一節）
const (
	defaultWindowSize        = 10
	defaultMaxPositionLayers = 20
	defaultInitialCapital    = 10000.0
	defaultLeverage          = 5
	defaultMakerFee          = 0.0002
	defaultTakerFee          = 0.0005
	defaultSkewStrength      = 0.5
	// regimeKlineInterval 校準使用的 regime K 線周期（regime.DefaultKlineInterval）
	regimeKlineInterval = "1h"
)

// SymbolSpec 交易對規格（Binance USD-M 精度）
type SymbolSpec struct {
	Symbol           string  `json:"symbol"`
	PriceDecimals    int     `json:"price_decimals"`
	QuantityDecimals int     `json:"quantity_decimals"`
	OrderQuantity    float64 `json:"order_quantity_usdt"`
}

// Variant 功能變體
type Variant struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Blocked 非空表示該變體在當前引擎上無法運行（原因）
	Blocked string `json:"blocked,omitempty"`
	apply   func(*config.Config)
}

func boolPtr(b bool) *bool { return &b }

func enableFeeAware(c *config.Config) { c.Trading.FeeAwareSpread.Enabled = boolPtr(true) }

func enableRegimeFilter(c *config.Config) {
	c.Trading.RegimeFilter.Enabled = true
	c.Trading.RegimeFilter.KlineInterval = regimeKlineInterval
}

func enableAdaptive(c *config.Config) {
	c.Trading.AdaptiveInterval.Enabled = true
	c.Trading.RegimeFilter.KlineInterval = regimeKlineInterval
}

func enableUpperBoundFreeze(c *config.Config) {
	c.Trading.UpperBoundFreeze.Enabled = true
	c.Trading.RegimeFilter.KlineInterval = regimeKlineInterval
}

func enableSkew(c *config.Config) {
	c.Trading.InventorySkew = config.InventorySkewConfig{Enabled: true, Strength: defaultSkewStrength}
}

// enableFundingPricing 與實盤一致：funding_rate.enabled 才創建監控器；只開連續定價，不開偏向與結算前暫停
func enableFundingPricing(c *config.Config) {
	c.FundingRate.Enabled = true
	c.FundingRate.PricingEnabled = true
}

// AllVariants 校準矩陣（與基線逐一對比）
func AllVariants() []Variant {
	return []Variant{
		{Name: "baseline", Description: "fee_aware_spread 關閉，R5 全部關閉", apply: func(*config.Config) {}},
		{Name: "fee_aware_spread", Description: "fee_aware_spread.enabled=true（默認安全邊際 0.02%）", apply: enableFeeAware},
		{Name: "regime_filter", Description: "regime_filter.enabled=true（1h，默認閾值）", apply: enableRegimeFilter},
		{Name: "adaptive_interval", Description: "adaptive_interval.enabled=true（1h ATR，默認 k=0.5；不開 regime_filter）", apply: enableAdaptive},
		{Name: "upper_bound_freeze", Description: "upper_bound_freeze.enabled=true（EMA+3×ATR，1h）", apply: enableUpperBoundFreeze},
		{Name: "inventory_skew", Description: "inventory_skew.enabled=true strength=0.5（max_position_layers=20）", apply: enableSkew},
		{Name: "funding_pricing", Description: "funding_rate.enabled + pricing_enabled=true（模擬監控器：上一期已結算費率）", apply: enableFundingPricing},
		{Name: "all_on", Description: "全部開啟：fee_aware_spread + regime_filter + adaptive_interval + upper_bound_freeze + inventory_skew + funding_pricing", apply: func(c *config.Config) {
			enableFeeAware(c)
			enableRegimeFilter(c)
			enableAdaptive(c)
			enableUpperBoundFreeze(c)
			enableSkew(c)
			enableFundingPricing(c)
		}},
	}
}

// BaseSpec 基礎網格
type BaseSpec struct {
	Name          string  `json:"name"`
	IntervalRatio float64 `json:"interval_ratio"` // 間隔 = 起始價 × ratio
}

// roundTo 按小數位四捨五入
func roundTo(v float64, decimals int) float64 {
	p := math.Pow10(decimals)
	return math.Round(v*p) / p
}

// Profile 引擎側運行口徑
type Profile struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// OrderCleanupThreshold trading.order_cleanup_threshold；0 = 實盤默認 100
	OrderCleanupThreshold int `json:"order_cleanup_threshold"`
	Leverage              int `json:"leverage"`
	// OrderCleaner 是否在模擬時間上運行 safety.OrderCleaner
	OrderCleaner bool `json:"order_cleaner"`
	// OrderCleanupIntervalSec timing.order_cleanup_interval
	OrderCleanupIntervalSec int `json:"order_cleanup_interval_sec"`
}

// 口徑常量
const (
	liveDefaultCleanupThreshold = 100
	// liveDefaultCleanupIntervalSec 實盤配置默認 timing.order_cleanup_interval
	liveDefaultCleanupIntervalSec = 60
	// bypassCleanupThreshold 繞過口徑的掛單上限：足夠大，使「掛單數達上限後不再開倉」在回放期內不觸發
	bypassCleanupThreshold = 100000
	// bypassLeverage 繞過口徑的槓桿：遠端殘留掛單佔用的模擬保證金不至於觸發保證金拒單
	bypassLeverage = 20
	// profileLiveDefault 默認口徑名
	profileLiveDefault = "live_default"
)

// AllProfiles 全部口徑：默認口徑 + 兩個僅用於對照的舊口徑（-profiles 指定時運行）
func AllProfiles() []Profile {
	return []Profile{
		{Name: profileLiveDefault, Description: "order_cleanup_threshold=100、cleanup_batch_size=10、order_cleanup_interval=60s（實盤默認）、槓桿 5x；在模擬時間上運行 safety.OrderCleaner",
			OrderCleanupThreshold: liveDefaultCleanupThreshold, Leverage: defaultLeverage, OrderCleaner: true, OrderCleanupIntervalSec: liveDefaultCleanupIntervalSec},
		{Name: "no_cleaner", Description: "同 live_default 但不運行 OrderCleaner（上一版報告的 live_default，用於復現單邊行情停擺）",
			OrderCleanupThreshold: liveDefaultCleanupThreshold, Leverage: defaultLeverage},
		{Name: "no_order_cap", Description: "order_cleanup_threshold=100000、槓桿 20x、不運行 OrderCleaner（上一版報告的繞過口徑）",
			OrderCleanupThreshold: bypassCleanupThreshold, Leverage: bypassLeverage},
	}
}

// DefaultProfileNames 未指定 -profiles 時運行的口徑
func DefaultProfileNames() []string { return []string{profileLiveDefault} }

// BuildConfig 構造一次運行的回放配置
func BuildConfig(spec SymbolSpec, base BaseSpec, v Variant, prof Profile, startPrice float64, funding []replay.FundingPoint) (replay.Config, float64, error) {
	interval := roundTo(startPrice*base.IntervalRatio, spec.PriceDecimals)
	if interval <= 0 {
		return replay.Config{}, 0, fmt.Errorf("build config %s/%s: interval rounds to 0 (start=%.8f ratio=%v)", spec.Symbol, base.Name, startPrice, base.IntervalRatio)
	}
	bot := &config.Config{}
	bot.Trading.Symbol = spec.Symbol
	bot.Trading.MarketType = "futures"
	bot.Trading.Direction = "LONG"
	bot.Trading.PriceInterval = interval
	bot.Trading.OrderQuantity = spec.OrderQuantity
	bot.Trading.BuyWindowSize = defaultWindowSize
	bot.Trading.SellWindowSize = defaultWindowSize
	bot.Trading.OpenPositionControl.MaxPositionLayers = defaultMaxPositionLayers
	bot.Trading.FeeAwareSpread.Enabled = boolPtr(false)
	bot.Trading.OrderCleanupThreshold = prof.OrderCleanupThreshold
	bot.Timing.OrderCleanupInterval = prof.OrderCleanupIntervalSec
	if v.apply != nil {
		v.apply(bot)
	}
	leverage := prof.Leverage
	if leverage <= 0 {
		leverage = defaultLeverage
	}
	cfg := replay.Config{
		Bot:              bot,
		InitialCapital:   defaultInitialCapital,
		Leverage:         leverage,
		PriceDecimals:    spec.PriceDecimals,
		QuantityDecimals: spec.QuantityDecimals,
		Matching: replay.MatchingConfig{
			MakerFeeRate: defaultMakerFee,
			TakerFeeRate: defaultTakerFee,
		},
		FundingEnabled: len(funding) > 0,
		FundingSeries:  funding,
		EnforceMargin:  true,
		EquitySampleMs: MinuteMs * 60,
		OrderCleaner:   prof.OrderCleaner,
	}
	return cfg, interval, nil
}
