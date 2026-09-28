package config

import "strings"

// 本文件存放交易風控/開倉控制相關的配置類型（自 config.go 拆出，保持單文件 < 3000 行）。

// GridRiskControl 網格策略風控配置
type GridRiskControl struct {
	Enabled                 bool    `yaml:"enabled" json:"enabled"`
	MaxGridLayers           int     `yaml:"max_grid_layers" json:"max_grid_layers"`                       // 最大持倉層數預警：達到此層數後不再新開倉，並可限制掛單數量
	MaxOpenOrdersAtCap      int     `yaml:"max_open_orders_at_cap" json:"max_open_orders_at_cap"`         // 達到預警時最多允許的開倉單數；超出則撤單。做多先撤高價買單，做空先撤低價賣單。0=僅不新開倉不撤單
	StopLossRatio           float64 `yaml:"stop_loss_ratio" json:"stop_loss_ratio"`                       // 單幣種最大浮虧比例（如 0.1 表示 10%）
	TakeProfitTriggerRatio  float64 `yaml:"take_profit_trigger_ratio" json:"take_profit_trigger_ratio"`   // 盈利達到此比例後開啟回撤止盈（如 0.08 表示 8%）
	TrailingTakeProfitRatio float64 `yaml:"trailing_take_profit_ratio" json:"trailing_take_profit_ratio"` // 盈利回撤比例（如 0.03 表示回撤 3% 止盈）
	TrendFilterEnabled      bool    `yaml:"trend_filter_enabled" json:"trend_filter_enabled"`             // 是否開啟趨勢過濾（舊 tick 級趨勢過濾，已廢棄，建議改用 trading.regime_filter）
	// 關閉條件：滿足時自動停止 Bot（平倉並停止運行）
	CloseConditionEnabled      bool    `yaml:"close_condition_enabled" json:"close_condition_enabled"`             // 是否啟用關閉條件
	CloseConditionProfitTarget float64 `yaml:"close_condition_profit_target" json:"close_condition_profit_target"` // 盈利率達到此值時停止 Bot（如 0.2 表示 20%）
	CloseConditionLossLimit    float64 `yaml:"close_condition_loss_limit" json:"close_condition_loss_limit"`       // 虧損率達到此值時停止 Bot（如 0.1 表示 10%）
	// StopLossBasis 硬止損比例的分母：position=按持倉名義價值（預設，兼容舊行為）/ equity=按帳戶權益
	StopLossBasis string `yaml:"stop_loss_basis,omitempty" json:"stop_loss_basis,omitempty"`
}

// 硬止損分母取值
const (
	StopLossBasisPosition = "position"
	StopLossBasisEquity   = "equity"
)

// GetStopLossBasis 返回歸一化後的止損分母；空或未知值回退為 position
func (g GridRiskControl) GetStopLossBasis() string {
	if strings.EqualFold(strings.TrimSpace(g.StopLossBasis), StopLossBasisEquity) {
		return StopLossBasisEquity
	}
	return StopLossBasisPosition
}

// DefaultFeeAwareSafetyMarginRatio 費率感知利差的預設安全邊際（按價格比例，0.0002 = 0.02%）
const DefaultFeeAwareSafetyMarginRatio = 0.0002

// DefaultPostOnlyRepriceMaxAttempts PostOnly 被拒後向遠離盤口方向重定價的預設最大次數
const DefaultPostOnlyRepriceMaxAttempts = 3

// FeeAwareSpreadConfig 費率感知最小利差：平倉利差不低於 entryPrice × (2×費率 + 安全邊際)
type FeeAwareSpreadConfig struct {
	// Enabled 是否啟用；未配置（nil）時預設啟用
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// SafetyMarginRatio 安全邊際（價格比例）；<=0 時使用 DefaultFeeAwareSafetyMarginRatio
	SafetyMarginRatio float64 `yaml:"safety_margin_ratio,omitempty" json:"safety_margin_ratio,omitempty"`
}

// IsEnabled 未配置時預設啟用
func (f FeeAwareSpreadConfig) IsEnabled() bool {
	return f.Enabled == nil || *f.Enabled
}

// GetSafetyMarginRatio 返回安全邊際，未配置或非正數時使用預設值
func (f FeeAwareSpreadConfig) GetSafetyMarginRatio() float64 {
	if f.SafetyMarginRatio <= 0 {
		return DefaultFeeAwareSafetyMarginRatio
	}
	return f.SafetyMarginRatio
}

// EffectivePostOnlyRepriceMaxAttempts 歸一化 PostOnly 重定價次數，<=0 時使用預設值
func EffectivePostOnlyRepriceMaxAttempts(n int) int {
	if n <= 0 {
		return DefaultPostOnlyRepriceMaxAttempts
	}
	return n
}

// RocketTieredGridConfig 三級火箭網格配置
// 小波動時用小間距（如 100），持倉層數增加後自動切換到大間距（如 300、600）
type RocketTieredGridConfig struct {
	Enabled bool         `yaml:"enabled" json:"enabled"`
	Tiers   []RocketTier `yaml:"tiers" json:"tiers"` // 檔位配置，按 filled_threshold 升序
}

// RocketTier 單檔配置
type RocketTier struct {
	FilledThreshold int     `yaml:"filled_threshold" json:"filled_threshold"` // 達到此持倉層數後進入此檔（不含）
	Interval        float64 `yaml:"interval" json:"interval"`                 // 該檔網格間距（USDT）
	ProfitSpread    float64 `yaml:"profit_spread" json:"profit_spread"`       // 該檔平倉利差，0 則等於 Interval
}

// ScheduleRule 定時規則：在指定時段執行暫停/恢復開倉
type ScheduleRule struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Action   string `yaml:"action" json:"action"`               // "pause" 或 "resume"
	Time     string `yaml:"time" json:"time"`                   // "HH:MM" 格式（UTC）
	Weekdays []int  `yaml:"weekdays,omitempty" json:"weekdays"` // 0=週日..6=週六，空=每天
}

// PeriodicRule 週期規則：週期性開關倉
type PeriodicRule struct {
	Enabled          bool `yaml:"enabled" json:"enabled"`
	OpenDurationMin  int  `yaml:"open_duration_min" json:"open_duration_min"`   // 開倉持續分鐘數
	CloseDurationMin int  `yaml:"close_duration_min" json:"close_duration_min"` // 關倉持續分鐘數
}

// LogCleanupConfig 定期清理 INFO/WARN 日志（保留 ERROR/DEBUG 便於排查）
type LogCleanupConfig struct {
	Enabled       bool     `yaml:"enabled"`         // 是否啟用，預設 true
	Schedule      string   `yaml:"schedule"`        // 執行時间 HH:MM（如 02:00），預設 02:00
	RetentionDays int      `yaml:"retention_days"`  // 保留天數，超過此天數的指定級別日志將被清理，預設 7
	LevelsToClean []string `yaml:"levels_to_clean"` // 要清理的級別，如 ["INFO","WARN"]，預設 INFO/WARN
}

// OpenPositionControl 開倉管理配置
type OpenPositionControl struct {
	// 手動暫停開倉（運行時狀態，不持久化到 yaml）
	PauseOpening bool `yaml:"-" json:"pause_opening"`

	// 限倉：倉位價值上限（USDT），達到後停止開倉並撤銷開倉委託，0=不限
	MaxPositionValue float64 `yaml:"max_position_value" json:"max_position_value"`

	// 限倉：最大持倉數量（按幣種數量），達到後停止開倉
	MaxPositionQuantity float64 `yaml:"max_position_quantity" json:"max_position_quantity"`

	// 限倉：最大持倉層數（與 GridRiskControl.MaxGridLayers 獨立，0=不限）
	MaxPositionLayers int `yaml:"max_position_layers" json:"max_position_layers"`

	// 定時規則：在指定時段內關閉開倉
	ScheduleRules []ScheduleRule `yaml:"schedule_rules,omitempty" json:"schedule_rules"`

	// 週期規則：週期性開關倉
	PeriodicRule *PeriodicRule `yaml:"periodic_rule,omitempty" json:"periodic_rule"`

	// 🔥 新增：單獨 Bot 的風控策略（可覆蓋全局配置）
	BotRiskControl *BotRiskControl `yaml:"bot_risk_control,omitempty" json:"bot_risk_control,omitempty"`
}

// BotRiskControl 單獨 Bot 的風控配置
type BotRiskControl struct {
	Enabled bool `yaml:"enabled" json:"enabled"` // 是否啟用 Bot 獨立風控

	// 倉位數量限制
	MaxPositionQuantity float64 `yaml:"max_position_quantity" json:"max_position_quantity"` // 最大持倉數量（幣種）
	MaxPositionValue    float64 `yaml:"max_position_value" json:"max_position_value"`       // 最大倉位價值（USDT）
	MaxPositionLayers   int     `yaml:"max_position_layers" json:"max_position_layers"`     // 最大持倉層數

	// 開倉掛單限制（單向做多/做空時，每筆開倉委託佔用保證金，限制掛單數可節省資金）
	MaxOpenOrders     int     `yaml:"max_open_orders" json:"max_open_orders"`         // 最多開倉掛單數（每方向），0=不限制
	OpenOrderDistance float64 `yaml:"open_order_distance" json:"open_order_distance"` // 開倉單距離當前價的最大間隔數，0=用默認

	// 止損止盈
	StopLossRatio     float64 `yaml:"stop_loss_ratio" json:"stop_loss_ratio"`         // 止損比例
	TakeProfitRatio   float64 `yaml:"take_profit_ratio" json:"take_profit_ratio"`     // 止盈比例
	TrailingStopRatio float64 `yaml:"trailing_stop_ratio" json:"trailing_stop_ratio"` // 移動止盈比例

	// 暫停開倉
	PauseOpening       bool   `yaml:"pause_opening" json:"pause_opening"`                                   // 是否暫停開倉
	PauseOpeningReason string `yaml:"pause_opening_reason,omitempty" json:"pause_opening_reason,omitempty"` // 暫停原因
	AutoResumeAfter    int    `yaml:"auto_resume_after,omitempty" json:"auto_resume_after,omitempty"`       // 自動恢復時間（秒），0=不自動恢復

	// 趨勢過濾
	TrendFilterEnabled bool `yaml:"trend_filter_enabled" json:"trend_filter_enabled"` // 是否啟用趨勢過濾

	// 波動率暫停開倉（新增）
	VolatilityPauseEnabled bool                  `yaml:"volatility_pause_enabled" json:"volatility_pause_enabled"`                   // 是否啟用波動率暫停開倉
	VolatilityPauseConfig  VolatilityPauseConfig `yaml:"volatility_pause_config,omitempty" json:"volatility_pause_config,omitempty"` // 波動率暫停配置
}

// VolatilityPauseConfig 波動率暫停開倉配置
type VolatilityPauseConfig struct {
	// 暫停觸發條件
	PauseOnHighVolatility    bool `yaml:"pause_on_high_volatility" json:"pause_on_high_volatility"`       // 高波動時暫停開倉
	PauseOnExtremeVolatility bool `yaml:"pause_on_extreme_volatility" json:"pause_on_extreme_volatility"` // 極端波動時暫停開倉
	PauseOnSuddenIncrease    bool `yaml:"pause_on_sudden_increase" json:"pause_on_sudden_increase"`       // 波動率突增時暫停開倉

	// 策略方向過濾（根據策略方向和市場行情決定是否暫停）
	PauseOnDowntrend bool `yaml:"pause_on_downtrend" json:"pause_on_downtrend"` // 做多策略在高波動下跌行情中暫停開倉
	PauseOnUptrend   bool `yaml:"pause_on_uptrend" json:"pause_on_uptrend"`     // 做空策略在高波動上漲行情中暫停開倉

	// 自動恢復
	AutoResumeOnNormal bool    `yaml:"auto_resume_on_normal" json:"auto_resume_on_normal"` // 波動率回歸正常時自動恢復開倉
	ResumeThreshold    float64 `yaml:"resume_threshold" json:"resume_threshold"`           // 恢復開倉的波動率閾值（%），低於此值時恢復

	// 趨勢判斷配置（用於判斷上漲/下跌行情）
	TrendCheckPeriod   int     `yaml:"trend_check_period" json:"trend_check_period"`     // 趨勢檢查週期（分鐘），預設 15
	TrendDownThreshold float64 `yaml:"trend_down_threshold" json:"trend_down_threshold"` // 下跌趨勢閾值（%），低於此值視為下跌
	TrendUpThreshold   float64 `yaml:"trend_up_threshold" json:"trend_up_threshold"`     // 上漲趨勢閾值（%），高於此值視為上漲
}

// FundingRateConfig 資金費率監控與套利配置
type FundingRateConfig struct {
	Enabled         bool    `yaml:"enabled" json:"enabled"`                   // 是否啟用資金費率監控
	MonitorInterval int     `yaml:"monitor_interval" json:"monitor_interval"` // 監控間隔（秒），預設 60
	AlertThreshold  float64 `yaml:"alert_threshold" json:"alert_threshold"`   // 告警閾值，預設 0.001 (0.1%)

	// 偏向策略配置
	BiasEnabled       bool    `yaml:"bias_enabled" json:"bias_enabled"`               // 是否啟用費率偏向策略
	HighRateThreshold float64 `yaml:"high_rate_threshold" json:"high_rate_threshold"` // 高費率閾值，預設 0.001 (0.1%)
	PauseBuyThreshold float64 `yaml:"pause_buy_threshold" json:"pause_buy_threshold"` // 暫停買入閾值，預設 0.0015 (0.15%)
	TrendSyncEnabled  bool    `yaml:"trend_sync_enabled" json:"trend_sync_enabled"`   // 是否啟用費率與趨勢聯動，預設 true

	// PricingEnabled 資金費進定價（連續偏移）：付費一側開倉價遠離、平倉價外移 rate×price×距結算小時/8，封頂 0.5×間隔。預設 false
	PricingEnabled bool `yaml:"pricing_enabled" json:"pricing_enabled"`
	// PreSettlementPauseMinutes 結算前 N 分鐘暫停付費一側的新開倉；0=關閉（預設）
	PreSettlementPauseMinutes int `yaml:"pre_settlement_pause_minutes" json:"pre_settlement_pause_minutes"`

	// 期現套利配置
	ArbitrageEnabled   bool    `yaml:"arbitrage_enabled" json:"arbitrage_enabled"`       // 是否啟用期現套利
	HedgeMinPosition   float64 `yaml:"hedge_min_position" json:"hedge_min_position"`     // 最小對沖倉位（USDT），預設 100
	HedgeRateThreshold float64 `yaml:"hedge_rate_threshold" json:"hedge_rate_threshold"` // 開啟對沖的費率閾值，預設 0.001
	MaxSpreadPercent   float64 `yaml:"max_spread_percent" json:"max_spread_percent"`     // 最大價差百分比，超過則暫停對沖，預設 0.5%
}
