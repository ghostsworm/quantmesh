package regime

import (
	"math"

	"quantmesh/indicators"
)

// series 与 bars 等长、按下标对齐的指標序列；预热不足的位置为 NaN。
type series struct {
	adx     []float64
	plusDI  []float64
	minusDI []float64
	ema     []float64
	atr     []float64
}

// padFront 把尾部对齐的指標结果左侧补 NaN 到 n 长度
func padFront(vals []float64, n int) []float64 {
	out := make([]float64, n)
	offset := n - len(vals)
	for i := 0; i < offset; i++ {
		out[i] = math.NaN()
	}
	copy(out[offset:], vals)
	return out
}

func computeSeries(bars []indicators.Candle, cfg RegimeConfig) series {
	n := len(bars)
	adx, pdi, mdi := indicators.WilderADX(bars, cfg.ADXPeriod)
	return series{
		adx:     padFront(adx, n),
		plusDI:  padFront(pdi, n),
		minusDI: padFront(mdi, n),
		ema:     padFront(indicators.EMA(indicators.ClosePrices(bars), cfg.EMAPeriod), n),
		atr:     padFront(indicators.WilderATR(bars, cfg.ATRPeriod), n),
	}
}

// metrics 单根 K 線上的分类输入
type metrics struct {
	ok            bool
	adx           float64
	plusDI        float64
	minusDI       float64
	ema           float64
	slope         float64
	atr           float64
	atrPercentile float64
}

func anyNaN(vals ...float64) bool {
	for _, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return true
		}
	}
	return false
}

func metricsAt(s series, i int, cfg RegimeConfig) metrics {
	prev := i - cfg.EMASlopeLookback
	winStart := i - cfg.ATRPercentileLookback + 1
	if i < 0 || i >= len(s.adx) || prev < 0 || winStart < 0 {
		return metrics{}
	}
	m := metrics{
		adx:     s.adx[i],
		plusDI:  s.plusDI[i],
		minusDI: s.minusDI[i],
		ema:     s.ema[i],
		atr:     s.atr[i],
	}
	emaPrev := s.ema[prev]
	window := s.atr[winStart : i+1]
	if anyNaN(m.adx, m.ema, emaPrev, m.atr) || anyNaN(window...) || m.atr <= 0 {
		return metrics{}
	}
	m.slope = (m.ema - emaPrev) / (float64(cfg.EMASlopeLookback) * m.atr)
	m.atrPercentile = indicators.PercentileRank(window, m.atr)
	m.ok = true
	return m
}

// classifier 带滞回与驻留确认的状态机（非并发安全，由 Detector 加锁保护）
type classifier struct {
	cfg          RegimeConfig
	regime       Regime
	pending      Regime
	pendingCount int
	since        int64 // 确认时 K 線开盘时间（ms）
	barsIn       int
}

func newClassifier(cfg RegimeConfig) *classifier {
	return &classifier{cfg: cfg}
}

// candidate 计算本根 K 線的候选状态。已在趋势中时使用较低的退出阈值，
// 且只要求斜率方向不反转（不要求仍满足进入强度），形成滞回。
func (c *classifier) candidate(m metrics) Regime {
	switch c.regime {
	case TrendUp:
		if m.adx >= c.cfg.ADXExitThreshold && m.slope > 0 {
			return TrendUp
		}
	case TrendDown:
		if m.adx >= c.cfg.ADXExitThreshold && m.slope < 0 {
			return TrendDown
		}
	}
	if m.adx >= c.cfg.ADXEnterThreshold {
		switch {
		case m.slope >= c.cfg.EMASlopeMinATR:
			return TrendUp
		case m.slope <= -c.cfg.EMASlopeMinATR:
			return TrendDown
		}
	}
	return Range
}

// step 处理一根已收盘 K 線，返回处理后的已确认状态。
// 首次就绪（Unknown → X）立即确认；其余切换需候选状态连续 MinDwellBars 根。
func (c *classifier) step(m metrics, barOpenMs int64) Regime {
	cand := c.candidate(m)
	switch {
	case c.regime == Unknown:
		c.commit(cand, barOpenMs)
	case cand == c.regime:
		c.pending, c.pendingCount = Unknown, 0
		c.barsIn++
	default:
		if cand == c.pending {
			c.pendingCount++
		} else {
			c.pending, c.pendingCount = cand, 1
		}
		if c.pendingCount >= c.cfg.MinDwellBars {
			c.commit(cand, barOpenMs)
		} else {
			c.barsIn++
		}
	}
	return c.regime
}

func (c *classifier) commit(r Regime, barOpenMs int64) {
	c.regime = r
	c.pending, c.pendingCount = Unknown, 0
	c.since = barOpenMs
	c.barsIn = 1
}
