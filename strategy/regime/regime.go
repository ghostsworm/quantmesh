package regime

import "time"

// Regime 市場状态
type Regime int

const (
	// Unknown 数据不足、未启动或数据过期
	Unknown Regime = iota
	// Range 震荡
	Range
	// TrendUp 上升趋势
	TrendUp
	// TrendDown 下降趋势
	TrendDown
)

// String 返回稳定的小写标识（可用于日志、指标 label、API）
func (r Regime) String() string {
	switch r {
	case Range:
		return "range"
	case TrendUp:
		return "trend_up"
	case TrendDown:
		return "trend_down"
	default:
		return "unknown"
	}
}

// Snapshot 某一时刻的状态快照（值类型，可安全跨 goroutine 传递）。
type Snapshot struct {
	Symbol   string
	Interval string

	// Regime 已确认（经过滞回与驻留确认）的状态；未就绪时为 Unknown。
	// 消费方应优先使用 Effective()，它会把过期数据降级为 Unknown。
	Regime Regime
	Ready  bool // 已完成预热，Regime 有意义

	ADX           float64 // Wilder ADX
	PlusDI        float64
	MinusDI       float64
	EMA           float64
	EMASlope      float64 // (EMA[t]-EMA[t-lookback]) / (lookback × ATR[t])，单位：ATR/根
	ATR           float64 // Wilder ATR（价格单位），可直接喂给 AdaptiveInterval
	ATRPercentile float64 // 当前 ATR 在最近 lookback 根 ATR 中的百分位（0-100）
	Close         float64 // 最后一根已收盘 K 線的收盘价

	BarOpenTime  time.Time // 最后一根已处理 K 線的开盘时间
	RegimeSince  time.Time // 当前 Regime 被确认时那根 K 線的开盘时间
	BarsInRegime int       // 自确认以来经过的已收盘 K 線根数（含确认那根）
	UpdatedAt    time.Time // 检测器最后一次处理新 K 線的本地时间

	// Stale 距离最后一根 K 線收盘已超过 StaleMultiplier × 周期（读取时计算）
	Stale bool
}

// Effective 返回可用于决策的状态：未就绪或过期时返回 Unknown。
func (s Snapshot) Effective() Regime {
	if !s.Ready || s.Stale {
		return Unknown
	}
	return s.Regime
}

// Change 状态切换事件
type Change struct {
	From     Regime
	To       Regime
	Snapshot Snapshot
}

// Direction 网格方向
type Direction string

const (
	// DirectionLong 做多网格（低买高卖）
	DirectionLong Direction = "long"
	// DirectionShort 做空网格（高卖低买）
	DirectionShort Direction = "short"
)

// GridPolicy 网格对某个状态的建议调整。仅是建议值，由接入方决定如何落地。
type GridPolicy struct {
	// EntryWindowScale 开仓挂单窗口缩放（LONG 作用于 buy_window_size，SHORT 作用于 sell_window_size）。
	// 结果应向下取整且至少保留 1 档（若仍允许开仓）。
	EntryWindowScale float64
	// IntervalScale 网格间隔放大倍数；放大后需再用 QuantizeInterval 量化到 base 的整数倍以保持锚点对齐。
	IntervalScale float64
	// FreezeFavorableBound 冻结顺势一侧边界：LONG 冻结上沿（价格创新高时不上移买窗、不在高位重建仓，
	// 只保留已有持仓的平仓单）；SHORT 冻结下沿。
	FreezeFavorableBound bool
}

const (
	fullScale            = 1.0
	adverseEntryScale    = 0.5
	adverseIntervalScale = 1.5
	unknownEntryScale    = 0.5
	unknownIntervalScale = 1.0
)

// PolicyFor 返回给定状态与方向下的默认网格建议。
//
//   - Range：满铺（全部 1.0，不冻结）
//   - 逆势（LONG+TrendDown / SHORT+TrendUp）：开仓窗口减半、间隔 ×1.5，降低接飞刀速度
//   - 顺势（LONG+TrendUp / SHORT+TrendDown）：窗口与间隔不变，但冻结顺势边界，不追高/追低重建仓
//   - Unknown（未就绪/过期）：保守——窗口减半并冻结边界，间隔不变
func PolicyFor(r Regime, dir Direction) GridPolicy {
	adverse, favorable := TrendDown, TrendUp
	if dir == DirectionShort {
		adverse, favorable = TrendUp, TrendDown
	}
	switch r {
	case Range:
		return GridPolicy{EntryWindowScale: fullScale, IntervalScale: fullScale}
	case adverse:
		return GridPolicy{EntryWindowScale: adverseEntryScale, IntervalScale: adverseIntervalScale}
	case favorable:
		return GridPolicy{EntryWindowScale: fullScale, IntervalScale: fullScale, FreezeFavorableBound: true}
	default:
		return GridPolicy{EntryWindowScale: unknownEntryScale, IntervalScale: unknownIntervalScale, FreezeFavorableBound: true}
	}
}
