// Package regime 基于已收盘 K 線的市場状态（趋势/震荡）识别，以及 ATR 自适应网格间隔工具。
//
// 本包刻意只依赖 exchange / indicators / logger，不依赖 strategy、position，
// 以便 position（网格）等上层包直接引用而不产生循环依赖。
// 设计说明见 docs/decisions/2026-09-17-kline-regime-filter.md。
package regime

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ========== 默认值 ==========

const (
	// DefaultKlineInterval 默认 K 線周期
	DefaultKlineInterval = "1h"
	// DefaultADXPeriod 默认 ADX 周期
	DefaultADXPeriod = 14
	// DefaultADXEnterThreshold ADX 进入趋势阈值
	DefaultADXEnterThreshold = 25.0
	// DefaultADXExitThreshold ADX 退出趋势阈值（滞回，需低于进入阈值）
	DefaultADXExitThreshold = 20.0
	// DefaultEMAPeriod 默认 EMA 周期
	DefaultEMAPeriod = 50
	// DefaultEMASlopeLookback EMA 斜率回看根数
	DefaultEMASlopeLookback = 5
	// DefaultEMASlopeMinATR 进入趋势所需的最小 EMA 斜率（单位：每根 K 線移动多少个 ATR）
	DefaultEMASlopeMinATR = 0.05
	// DefaultMinDwellBars 切换状态前候选状态需连续出现的已收盘 K 線根数
	DefaultMinDwellBars = 3
	// DefaultATRPeriod 默认 ATR 周期
	DefaultATRPeriod = 14
	// DefaultATRPercentileLookback ATR 分位数回看根数
	DefaultATRPercentileLookback = 100
	// DefaultBootstrapBars 启动时拉取的历史 K 線根数（需覆盖指標预热 + Wilder 平滑收敛）
	DefaultBootstrapBars = 500
	// DefaultStaleMultiplier 超过多少个 K 線周期未收到新 K 線即视为过期
	DefaultStaleMultiplier = 2.0

	// DefaultATRMultiplier ATR 自适应间隔系数 k（interval = k × ATR）
	DefaultATRMultiplier = 0.5
	// DefaultChangeThresholdRatio 间隔变化不足该比例时不调整（防抖）
	DefaultChangeThresholdRatio = 0.25
	// DefaultMaxIntervalMultiple 未配置 MaxInterval 时，上限 = base × 该倍数
	DefaultMaxIntervalMultiple = 8.0

	// maxBootstrapBars 单次拉取上限（Binance 合约 klines limit 上限 1500）
	maxBootstrapBars = 1500
)

// ErrInvalidConfig 配置非法
var ErrInvalidConfig = errors.New("regime: invalid config")

// RegimeConfig K 線级市場状态识别配置。
// 零值字段在 WithDefaults 中补默认值；Enabled 不做默认（默认关闭）。
type RegimeConfig struct {
	Enabled               bool    `yaml:"enabled" json:"enabled"`
	KlineInterval         string  `yaml:"kline_interval" json:"kline_interval"`                   // K 線周期，如 15m/1h/4h/1d（仅支持分钟/小时/天）
	ADXPeriod             int     `yaml:"adx_period" json:"adx_period"`                           // ADX 周期
	ADXEnterThreshold     float64 `yaml:"adx_enter_threshold" json:"adx_enter_threshold"`         // ADX ≥ 该值才可能进入趋势
	ADXExitThreshold      float64 `yaml:"adx_exit_threshold" json:"adx_exit_threshold"`           // 已在趋势中时 ADX < 该值才退出
	EMAPeriod             int     `yaml:"ema_period" json:"ema_period"`                           // EMA 周期
	EMASlopeLookback      int     `yaml:"ema_slope_lookback" json:"ema_slope_lookback"`           // EMA 斜率回看根数
	EMASlopeMinATR        float64 `yaml:"ema_slope_min_atr" json:"ema_slope_min_atr"`             // 进入趋势的最小 |斜率|（ATR/根）
	MinDwellBars          int     `yaml:"min_dwell_bars" json:"min_dwell_bars"`                   // 新状态需连续确认的根数（1=立即切换）
	ATRPeriod             int     `yaml:"atr_period" json:"atr_period"`                           // ATR 周期
	ATRPercentileLookback int     `yaml:"atr_percentile_lookback" json:"atr_percentile_lookback"` // ATR 分位数回看根数
	BootstrapBars         int     `yaml:"bootstrap_bars" json:"bootstrap_bars"`                   // 启动/重同步时拉取的历史根数，同时是内存缓冲上限
	PollIntervalSeconds   int     `yaml:"poll_interval_seconds" json:"poll_interval_seconds"`     // 轮询新 K 線的间隔，0=按 K 線周期自动推导
	StaleMultiplier       float64 `yaml:"stale_multiplier" json:"stale_multiplier"`               // 超过 N 个周期无新 K 線即 Stale
}

// WithDefaults 返回补齐默认值后的副本（不修改接收者）。
func (c RegimeConfig) WithDefaults() RegimeConfig {
	if strings.TrimSpace(c.KlineInterval) == "" {
		c.KlineInterval = DefaultKlineInterval
	}
	if c.ADXPeriod <= 0 {
		c.ADXPeriod = DefaultADXPeriod
	}
	if c.ADXEnterThreshold <= 0 {
		c.ADXEnterThreshold = DefaultADXEnterThreshold
	}
	if c.ADXExitThreshold <= 0 {
		c.ADXExitThreshold = math.Min(DefaultADXExitThreshold, c.ADXEnterThreshold)
	}
	if c.EMAPeriod <= 0 {
		c.EMAPeriod = DefaultEMAPeriod
	}
	if c.EMASlopeLookback <= 0 {
		c.EMASlopeLookback = DefaultEMASlopeLookback
	}
	if c.EMASlopeMinATR <= 0 {
		c.EMASlopeMinATR = DefaultEMASlopeMinATR
	}
	if c.MinDwellBars <= 0 {
		c.MinDwellBars = DefaultMinDwellBars
	}
	if c.ATRPeriod <= 0 {
		c.ATRPeriod = DefaultATRPeriod
	}
	if c.ATRPercentileLookback <= 0 {
		c.ATRPercentileLookback = DefaultATRPercentileLookback
	}
	if c.BootstrapBars <= 0 {
		c.BootstrapBars = DefaultBootstrapBars
	}
	if c.StaleMultiplier <= 0 {
		c.StaleMultiplier = DefaultStaleMultiplier
	}
	return c
}

// RequiredBars 产出第一个有效状态所需的最少已收盘 K 線根数。
func (c RegimeConfig) RequiredBars() int {
	adxBars := 2 * c.ADXPeriod
	emaBars := c.EMAPeriod + c.EMASlopeLookback
	atrBars := c.ATRPeriod + c.ATRPercentileLookback
	return max(adxBars, emaBars, atrBars)
}

// Validate 校验配置（应在 WithDefaults 之后调用）。
func (c RegimeConfig) Validate() error {
	if _, err := ParseKlineInterval(c.KlineInterval); err != nil {
		return err
	}
	switch {
	case c.ADXPeriod <= 0, c.EMAPeriod <= 0, c.EMASlopeLookback <= 0, c.MinDwellBars <= 0,
		c.ATRPeriod <= 0, c.ATRPercentileLookback <= 0, c.BootstrapBars <= 0:
		return fmt.Errorf("%w: periods/lookbacks must be positive (adx=%d ema=%d slope_lookback=%d dwell=%d atr=%d atr_lookback=%d bootstrap=%d)",
			ErrInvalidConfig, c.ADXPeriod, c.EMAPeriod, c.EMASlopeLookback, c.MinDwellBars,
			c.ATRPeriod, c.ATRPercentileLookback, c.BootstrapBars)
	case c.ADXEnterThreshold <= 0 || c.ADXExitThreshold <= 0:
		return fmt.Errorf("%w: adx thresholds must be positive (enter=%v exit=%v)", ErrInvalidConfig, c.ADXEnterThreshold, c.ADXExitThreshold)
	case c.ADXExitThreshold > c.ADXEnterThreshold:
		return fmt.Errorf("%w: adx_exit_threshold (%v) must be <= adx_enter_threshold (%v)", ErrInvalidConfig, c.ADXExitThreshold, c.ADXEnterThreshold)
	case c.EMASlopeMinATR <= 0:
		return fmt.Errorf("%w: ema_slope_min_atr must be positive (got %v)", ErrInvalidConfig, c.EMASlopeMinATR)
	case c.StaleMultiplier < 1:
		return fmt.Errorf("%w: stale_multiplier must be >= 1 (got %v)", ErrInvalidConfig, c.StaleMultiplier)
	case c.PollIntervalSeconds < 0:
		return fmt.Errorf("%w: poll_interval_seconds must be >= 0 (got %d)", ErrInvalidConfig, c.PollIntervalSeconds)
	case c.BootstrapBars > maxBootstrapBars:
		return fmt.Errorf("%w: bootstrap_bars %d exceeds max %d", ErrInvalidConfig, c.BootstrapBars, maxBootstrapBars)
	case c.BootstrapBars < c.RequiredBars():
		return fmt.Errorf("%w: bootstrap_bars %d < required warmup bars %d", ErrInvalidConfig, c.BootstrapBars, c.RequiredBars())
	}
	return nil
}

// ParseKlineInterval 解析交易所风格的 K 線周期字符串（如 "15m"、"1h"、"4h"、"1d"）。
// 仅支持 m/h/d 单位：这些周期在 UTC 下与 Unix 纪元对齐，可用取整把
// "开盘时间"或"收盘时间(=下一根开盘-1ms)"两种时间戳口径统一成开盘时间。
func ParseKlineInterval(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, fmt.Errorf("%w: kline_interval %q", ErrInvalidConfig, s)
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%w: kline_interval %q: bad number", ErrInvalidConfig, s)
	}
	var unit time.Duration
	switch s[len(s)-1] {
	case 'm':
		unit = time.Minute
	case 'h':
		unit = time.Hour
	case 'd':
		unit = 24 * time.Hour
	default:
		return 0, fmt.Errorf("%w: kline_interval %q: unit must be m/h/d", ErrInvalidConfig, s)
	}
	return time.Duration(n) * unit, nil
}

// AdaptiveIntervalConfig ATR 自适应网格间隔配置。
type AdaptiveIntervalConfig struct {
	Enabled              bool    `yaml:"enabled" json:"enabled"`
	ATRMultiplier        float64 `yaml:"atr_multiplier" json:"atr_multiplier"`                 // k：目标间隔 = k × ATR
	MinInterval          float64 `yaml:"min_interval" json:"min_interval"`                     // 价格单位；0 = base_interval
	MaxInterval          float64 `yaml:"max_interval" json:"max_interval"`                     // 价格单位；0 = base_interval × DefaultMaxIntervalMultiple
	ChangeThresholdRatio float64 `yaml:"change_threshold_ratio" json:"change_threshold_ratio"` // 相对变化 ≥ 该比例才切换
}

// WithDefaults 返回补齐默认值后的副本。MinInterval/MaxInterval 依赖 base，在 ResolveBounds 中解析。
func (c AdaptiveIntervalConfig) WithDefaults() AdaptiveIntervalConfig {
	if c.ATRMultiplier <= 0 {
		c.ATRMultiplier = DefaultATRMultiplier
	}
	if c.ChangeThresholdRatio <= 0 {
		c.ChangeThresholdRatio = DefaultChangeThresholdRatio
	}
	return c
}

// Validate 校验配置。
func (c AdaptiveIntervalConfig) Validate() error {
	switch {
	case c.ATRMultiplier <= 0 || math.IsInf(c.ATRMultiplier, 0) || math.IsNaN(c.ATRMultiplier):
		return fmt.Errorf("%w: atr_multiplier must be positive finite (got %v)", ErrInvalidConfig, c.ATRMultiplier)
	case c.MinInterval < 0 || c.MaxInterval < 0:
		return fmt.Errorf("%w: min/max interval must be >= 0 (min=%v max=%v)", ErrInvalidConfig, c.MinInterval, c.MaxInterval)
	case c.MaxInterval > 0 && c.MinInterval > c.MaxInterval:
		return fmt.Errorf("%w: min_interval (%v) > max_interval (%v)", ErrInvalidConfig, c.MinInterval, c.MaxInterval)
	case c.ChangeThresholdRatio < 0:
		return fmt.Errorf("%w: change_threshold_ratio must be >= 0 (got %v)", ErrInvalidConfig, c.ChangeThresholdRatio)
	}
	return nil
}
