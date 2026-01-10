package indicators

import (
	"math"
)

// ========== 成交量指标 ==========

// OBV 能量潮
type OBV struct{}

// NewOBV 创建 OBV 指标
func NewOBV() *OBV {
	return &OBV{}
}

// Name 指标名称
func (o *OBV) Name() string {
	return "OBV"
}

// Period 所需周期数
func (o *OBV) Period() int {
	return 2
}

// Calculate 计算 OBV
func (o *OBV) Calculate(candles []Candle) []float64 {
	if len(candles) < 2 {
		return nil
	}

	result := make([]float64, len(candles))
	result[0] = candles[0].Volume

	for i := 1; i < len(candles); i++ {
		if candles[i].Close > candles[i-1].Close {
			result[i] = result[i-1] + candles[i].Volume
		} else if candles[i].Close < candles[i-1].Close {
			result[i] = result[i-1] - candles[i].Volume
		} else {
			result[i] = result[i-1]
		}
	}

	return result
}

// Signal 交易信号（基于 OBV 和价格的背离）
func (o *OBV) Signal(candles []Candle) int {
	obv := o.Calculate(candles)
	if obv == nil || len(obv) < 10 {
		return 0
	}

	n := len(obv) - 1
	closes := ClosePrices(candles)

	// 简单的趋势判断
	obvTrend := obv[n] - obv[n-5]
	priceTrend := closes[n] - closes[n-5]

	// 看涨背离：价格下跌但 OBV 上涨
	if priceTrend < 0 && obvTrend > 0 {
		return 1
	}
	// 看跌背离：价格上涨但 OBV 下跌
	if priceTrend > 0 && obvTrend < 0 {
		return -1
	}

	return 0
}

// VWAP 成交量加权平均价格
type VWAP struct{}

// NewVWAP 创建 VWAP 指标
func NewVWAP() *VWAP {
	return &VWAP{}
}

// Name 指标名称
func (v *VWAP) Name() string {
	return "VWAP"
}

// Period 所需周期数
func (v *VWAP) Period() int {
	return 1
}

// Calculate 计算 VWAP（从当日开始累积）
func (v *VWAP) Calculate(candles []Candle) []float64 {
	if len(candles) == 0 {
		return nil
	}

	result := make([]float64, len(candles))
	cumVolume := 0.0
	cumVolumePrice := 0.0

	for i, c := range candles {
		tp := (c.High + c.Low + c.Close) / 3
		cumVolume += c.Volume
		cumVolumePrice += tp * c.Volume

		if cumVolume != 0 {
			result[i] = cumVolumePrice / cumVolume
		}
	}

	return result
}

// Signal 交易信号
func (v *VWAP) Signal(candles []Candle) int {
	vwap := v.Calculate(candles)
	if vwap == nil || len(vwap) == 0 {
		return 0
	}

	n := len(vwap) - 1
	close := candles[n].Close

	// 价格在 VWAP 上方：看涨
	if close > vwap[n] {
		return 1
	}
	// 价格在 VWAP 下方：看跌
	if close < vwap[n] {
		return -1
	}

	return 0
}

// VolumeProfile 成交量分布
type VolumeProfile struct {
	period int
	bins   int // 价格区间数量
}

// NewVolumeProfile 创建成交量分布指标
func NewVolumeProfile(period, bins int) *VolumeProfile {
	return &VolumeProfile{
		period: period,
		bins:   bins,
	}
}

// Name 指标名称
func (vp *VolumeProfile) Name() string {
	return "VolumeProfile"
}

// Period 所需周期数
func (vp *VolumeProfile) Period() int {
	return vp.period
}

// Calculate 计算成交量分布（返回 POC - Point of Control）
func (vp *VolumeProfile) Calculate(candles []Candle) []float64 {
	if len(candles) < vp.period {
		return nil
	}

	result := make([]float64, len(candles)-vp.period+1)

	for i := vp.period - 1; i < len(candles); i++ {
		// 获取周期内的价格范围
		periodCandles := candles[i-vp.period+1 : i+1]
		high := periodCandles[0].High
		low := periodCandles[0].Low
		for _, c := range periodCandles {
			if c.High > high {
				high = c.High
			}
			if c.Low < low {
				low = c.Low
			}
		}

		priceRange := high - low
		if priceRange == 0 {
			result[i-vp.period+1] = (high + low) / 2
			continue
		}

		binSize := priceRange / float64(vp.bins)
		volumes := make([]float64, vp.bins)

		// 分配成交量到各个价格区间
		for _, c := range periodCandles {
			tp := (c.High + c.Low + c.Close) / 3
			binIdx := int((tp - low) / binSize)
			if binIdx >= vp.bins {
				binIdx = vp.bins - 1
			}
			if binIdx < 0 {
				binIdx = 0
			}
			volumes[binIdx] += c.Volume
		}

		// 找到成交量最大的区间（POC）
		maxVolume := volumes[0]
		maxIdx := 0
		for j := 1; j < len(volumes); j++ {
			if volumes[j] > maxVolume {
				maxVolume = volumes[j]
				maxIdx = j
			}
		}

		// POC 价格
		result[i-vp.period+1] = low + float64(maxIdx)*binSize + binSize/2
	}

	return result
}

// CMF Chaikin 资金流量
type CMF struct {
	period int
}

// NewCMF 创建 CMF 指标
func NewCMF(period int) *CMF {
	return &CMF{period: period}
}

// Name 指标名称
func (c *CMF) Name() string {
	return "CMF"
}

// Period 所需周期数
func (c *CMF) Period() int {
	return c.period
}

// Calculate 计算 CMF
func (c *CMF) Calculate(candles []Candle) []float64 {
	if len(candles) < c.period {
		return nil
	}

	// 计算资金流量乘数和资金流量
	mfv := make([]float64, len(candles))
	volume := make([]float64, len(candles))

	for i, candle := range candles {
		hlRange := candle.High - candle.Low
		if hlRange != 0 {
			mfm := ((candle.Close - candle.Low) - (candle.High - candle.Close)) / hlRange
			mfv[i] = mfm * candle.Volume
		}
		volume[i] = candle.Volume
	}

	// 计算 CMF
	result := make([]float64, len(candles)-c.period+1)
	for i := c.period - 1; i < len(candles); i++ {
		sumMFV := 0.0
		sumVolume := 0.0
		for j := i - c.period + 1; j <= i; j++ {
			sumMFV += mfv[j]
			sumVolume += volume[j]
		}
		if sumVolume != 0 {
			result[i-c.period+1] = sumMFV / sumVolume
		}
	}

	return result
}

// Signal 交易信号
func (c *CMF) Signal(candles []Candle) int {
	cmf := c.Calculate(candles)
	if cmf == nil || len(cmf) == 0 {
		return 0
	}

	current := cmf[len(cmf)-1]

	// CMF > 0.05: 资金流入
	if current > 0.05 {
		return 1
	}
	// CMF < -0.05: 资金流出
	if current < -0.05 {
		return -1
	}

	return 0
}

// ADL 累积派发线
type ADL struct{}

// NewADL 创建 ADL 指标
func NewADL() *ADL {
	return &ADL{}
}

// Name 指标名称
func (a *ADL) Name() string {
	return "ADL"
}

// Period 所需周期数
func (a *ADL) Period() int {
	return 1
}

// Calculate 计算 ADL
func (a *ADL) Calculate(candles []Candle) []float64 {
	if len(candles) == 0 {
		return nil
	}

	result := make([]float64, len(candles))
	adl := 0.0

	for i, c := range candles {
		hlRange := c.High - c.Low
		if hlRange != 0 {
			mfm := ((c.Close - c.Low) - (c.High - c.Close)) / hlRange
			adl += mfm * c.Volume
		}
		result[i] = adl
	}

	return result
}

// ChaikinOscillator Chaikin 振荡器
type ChaikinOscillator struct {
	FastPeriod int
	SlowPeriod int
}

// NewChaikinOscillator 创建 Chaikin 振荡器
func NewChaikinOscillator(fast, slow int) *ChaikinOscillator {
	return &ChaikinOscillator{
		FastPeriod: fast,
		SlowPeriod: slow,
	}
}

// Name 指标名称
func (co *ChaikinOscillator) Name() string {
	return "ChaikinOscillator"
}

// Period 所需周期数
func (co *ChaikinOscillator) Period() int {
	return co.SlowPeriod
}

// Calculate 计算 Chaikin 振荡器
func (co *ChaikinOscillator) Calculate(candles []Candle) []float64 {
	adl := NewADL().Calculate(candles)
	if adl == nil {
		return nil
	}

	fastEMA := EMA(adl, co.FastPeriod)
	slowEMA := EMA(adl, co.SlowPeriod)

	if fastEMA == nil || slowEMA == nil {
		return nil
	}

	offset := len(fastEMA) - len(slowEMA)
	result := make([]float64, len(slowEMA))

	for i := range slowEMA {
		result[i] = fastEMA[i+offset] - slowEMA[i]
	}

	return result
}

// Signal 交易信号
func (co *ChaikinOscillator) Signal(candles []Candle) int {
	values := co.Calculate(candles)
	if values == nil || len(values) < 2 {
		return 0
	}

	n := len(values) - 1
	// 从负转正
	if values[n-1] < 0 && values[n] > 0 {
		return 1
	}
	// 从正转负
	if values[n-1] > 0 && values[n] < 0 {
		return -1
	}

	return 0
}

// ForceIndex 力度指数
type ForceIndex struct {
	period int
}

// NewForceIndex 创建力度指数
func NewForceIndex(period int) *ForceIndex {
	return &ForceIndex{period: period}
}

// Name 指标名称
func (fi *ForceIndex) Name() string {
	return "ForceIndex"
}

// Period 所需周期数
func (fi *ForceIndex) Period() int {
	return fi.period + 1
}

// Calculate 计算力度指数
func (fi *ForceIndex) Calculate(candles []Candle) []float64 {
	if len(candles) < 2 {
		return nil
	}

	// 计算原始力度指数
	raw := make([]float64, len(candles)-1)
	for i := 1; i < len(candles); i++ {
		raw[i-1] = (candles[i].Close - candles[i-1].Close) * candles[i].Volume
	}

	// EMA 平滑
	return EMA(raw, fi.period)
}

// Signal 交易信号
func (fi *ForceIndex) Signal(candles []Candle) int {
	values := fi.Calculate(candles)
	if values == nil || len(values) < 2 {
		return 0
	}

	n := len(values) - 1
	// 从负转正
	if values[n-1] < 0 && values[n] > 0 {
		return 1
	}
	// 从正转负
	if values[n-1] > 0 && values[n] < 0 {
		return -1
	}

	return 0
}

// NVI 负成交量指数
type NVI struct{}

// NewNVI 创建负成交量指数
func NewNVI() *NVI {
	return &NVI{}
}

// Name 指标名称
func (n *NVI) Name() string {
	return "NVI"
}

// Period 所需周期数
func (n *NVI) Period() int {
	return 2
}

// Calculate 计算 NVI
func (n *NVI) Calculate(candles []Candle) []float64 {
	if len(candles) < 2 {
		return nil
	}

	result := make([]float64, len(candles))
	result[0] = 1000 // 初始值

	for i := 1; i < len(candles); i++ {
		if candles[i].Volume < candles[i-1].Volume {
			// 成交量减少时更新
			priceChange := (candles[i].Close - candles[i-1].Close) / candles[i-1].Close
			result[i] = result[i-1] * (1 + priceChange)
		} else {
			result[i] = result[i-1]
		}
	}

	return result
}

// PVI 正成交量指数
type PVI struct{}

// NewPVI 创建正成交量指数
func NewPVI() *PVI {
	return &PVI{}
}

// Name 指标名称
func (p *PVI) Name() string {
	return "PVI"
}

// Period 所需周期数
func (p *PVI) Period() int {
	return 2
}

// Calculate 计算 PVI
func (p *PVI) Calculate(candles []Candle) []float64 {
	if len(candles) < 2 {
		return nil
	}

	result := make([]float64, len(candles))
	result[0] = 1000 // 初始值

	for i := 1; i < len(candles); i++ {
		if candles[i].Volume > candles[i-1].Volume {
			// 成交量增加时更新
			priceChange := (candles[i].Close - candles[i-1].Close) / candles[i-1].Close
			result[i] = result[i-1] * (1 + priceChange)
		} else {
			result[i] = result[i-1]
		}
	}

	return result
}

// EaseOfMovement 简易波动指标
type EaseOfMovement struct {
	period int
}

// NewEaseOfMovement 创建简易波动指标
func NewEaseOfMovement(period int) *EaseOfMovement {
	return &EaseOfMovement{period: period}
}

// Name 指标名称
func (eom *EaseOfMovement) Name() string {
	return "EaseOfMovement"
}

// Period 所需周期数
func (eom *EaseOfMovement) Period() int {
	return eom.period + 1
}

// Calculate 计算 EOM
func (eom *EaseOfMovement) Calculate(candles []Candle) []float64 {
	if len(candles) < 2 {
		return nil
	}

	raw := make([]float64, len(candles)-1)

	for i := 1; i < len(candles); i++ {
		dm := ((candles[i].High + candles[i].Low) - (candles[i-1].High + candles[i-1].Low)) / 2
		br := candles[i].Volume / 10000 / (candles[i].High - candles[i].Low)
		if br != 0 && !math.IsInf(br, 0) {
			raw[i-1] = dm / br
		}
	}

	// SMA 平滑
	return SMA(raw, eom.period)
}

// Signal 交易信号
func (eom *EaseOfMovement) Signal(candles []Candle) int {
	values := eom.Calculate(candles)
	if values == nil || len(values) == 0 {
		return 0
	}

	current := values[len(values)-1]

	if current > 0 {
		return 1
	}
	if current < 0 {
		return -1
	}

	return 0
}

// VolumeRateOfChange 成交量变化率
type VolumeRateOfChange struct {
	period int
}

// NewVolumeRateOfChange 创建成交量变化率指标
func NewVolumeRateOfChange(period int) *VolumeRateOfChange {
	return &VolumeRateOfChange{period: period}
}

// Name 指标名称
func (vroc *VolumeRateOfChange) Name() string {
	return "VolumeROC"
}

// Period 所需周期数
func (vroc *VolumeRateOfChange) Period() int {
	return vroc.period + 1
}

// Calculate 计算 VROC
func (vroc *VolumeRateOfChange) Calculate(candles []Candle) []float64 {
	volumes := Volumes(candles)
	return RateOfChange(volumes, vroc.period)
}

// 注册成交量指标
func init() {
	RegisterIndicator("OBV", func(params map[string]interface{}) Indicator {
		return NewOBV()
	})

	RegisterIndicator("VWAP", func(params map[string]interface{}) Indicator {
		return NewVWAP()
	})

	RegisterIndicator("VolumeProfile", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 20)
		bins := getIntParam(params, "bins", 12)
		return NewVolumeProfile(period, bins)
	})

	RegisterIndicator("CMF", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 20)
		return NewCMF(period)
	})

	RegisterIndicator("ADL", func(params map[string]interface{}) Indicator {
		return NewADL()
	})

	RegisterIndicator("ChaikinOscillator", func(params map[string]interface{}) Indicator {
		fast := getIntParam(params, "fast", 3)
		slow := getIntParam(params, "slow", 10)
		return NewChaikinOscillator(fast, slow)
	})

	RegisterIndicator("ForceIndex", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 13)
		return NewForceIndex(period)
	})

	RegisterIndicator("NVI", func(params map[string]interface{}) Indicator {
		return NewNVI()
	})

	RegisterIndicator("PVI", func(params map[string]interface{}) Indicator {
		return NewPVI()
	})

	RegisterIndicator("EaseOfMovement", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 14)
		return NewEaseOfMovement(period)
	})

	RegisterIndicator("VolumeROC", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 12)
		return NewVolumeRateOfChange(period)
	})
}
