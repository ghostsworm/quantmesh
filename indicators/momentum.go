package indicators

import (
	"math"
)

// ========== 动量指标 ==========

// RSI 相对强弱指数
type RSI struct {
	period int
}

// NewRSI 创建 RSI 指标
func NewRSI(period int) *RSI {
	return &RSI{period: period}
}

// Name 指标名称
func (r *RSI) Name() string {
	return "RSI"
}

// Period 所需周期数
func (r *RSI) Period() int {
	return r.period + 1
}

// Calculate 计算 RSI
func (r *RSI) Calculate(candles []Candle) []float64 {
	closes := ClosePrices(candles)
	if len(closes) < r.period+1 {
		return nil
	}

	// 计算价格变化
	changes := make([]float64, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		changes[i-1] = closes[i] - closes[i-1]
	}

	// 分离上涨和下跌
	gains := make([]float64, len(changes))
	losses := make([]float64, len(changes))
	for i, change := range changes {
		if change > 0 {
			gains[i] = change
		} else {
			losses[i] = -change
		}
	}

	// 使用 EMA 平滑
	avgGain := EMA(gains, r.period)
	avgLoss := EMA(losses, r.period)

	if avgGain == nil || avgLoss == nil {
		return nil
	}

	result := make([]float64, len(avgGain))
	for i := range avgGain {
		if avgLoss[i] == 0 {
			result[i] = 100
		} else {
			rs := avgGain[i] / avgLoss[i]
			result[i] = 100 - 100/(1+rs)
		}
	}

	return result
}

// Signal 交易信号
func (r *RSI) Signal(candles []Candle) int {
	rsi := r.Calculate(candles)
	if rsi == nil || len(rsi) == 0 {
		return 0
	}

	current := rsi[len(rsi)-1]

	// RSI < 30: 超卖，买入
	if current < 30 {
		return 1
	}
	// RSI > 70: 超买，卖出
	if current > 70 {
		return -1
	}

	return 0
}

// StochasticOscillator 随机振荡器
type StochasticOscillator struct {
	KPeriod int
	DPeriod int
	Slowing int
}

// NewStochasticOscillator 创建随机振荡器
func NewStochasticOscillator(kPeriod, dPeriod, slowing int) *StochasticOscillator {
	return &StochasticOscillator{
		KPeriod: kPeriod,
		DPeriod: dPeriod,
		Slowing: slowing,
	}
}

// Name 指标名称
func (so *StochasticOscillator) Name() string {
	return "StochasticOscillator"
}

// Period 所需周期数
func (so *StochasticOscillator) Period() int {
	return so.KPeriod + so.DPeriod + so.Slowing
}

// Calculate 计算 %K 线
func (so *StochasticOscillator) Calculate(candles []Candle) []float64 {
	result := so.CalculateMulti(candles)
	if result == nil {
		return nil
	}
	return result["k"]
}

// CalculateMulti 计算 %K 和 %D
func (so *StochasticOscillator) CalculateMulti(candles []Candle) map[string][]float64 {
	if len(candles) < so.KPeriod {
		return nil
	}

	// 计算原始 %K
	rawK := make([]float64, len(candles)-so.KPeriod+1)
	for i := so.KPeriod - 1; i < len(candles); i++ {
		high := candles[i-so.KPeriod+1].High
		low := candles[i-so.KPeriod+1].Low
		for j := i - so.KPeriod + 2; j <= i; j++ {
			if candles[j].High > high {
				high = candles[j].High
			}
			if candles[j].Low < low {
				low = candles[j].Low
			}
		}

		close := candles[i].Close
		if high != low {
			rawK[i-so.KPeriod+1] = (close - low) / (high - low) * 100
		} else {
			rawK[i-so.KPeriod+1] = 50
		}
	}

	// 平滑 %K（Slowing）
	k := SMA(rawK, so.Slowing)
	if k == nil {
		return nil
	}

	// 计算 %D
	d := SMA(k, so.DPeriod)
	if d == nil {
		return nil
	}

	// 对齐长度
	offset := len(k) - len(d)

	return map[string][]float64{
		"k": k[offset:],
		"d": d,
	}
}

// Signal 交易信号
func (so *StochasticOscillator) Signal(candles []Candle) int {
	result := so.CalculateMulti(candles)
	if result == nil {
		return 0
	}

	k := result["k"]
	d := result["d"]

	if len(k) < 2 || len(d) < 2 {
		return 0
	}

	n := len(k) - 1

	// %K 上穿 %D 且在超卖区
	if CrossOver(k, d) && k[n] < 20 {
		return 1
	}
	// %K 下穿 %D 且在超买区
	if CrossUnder(k, d) && k[n] > 80 {
		return -1
	}

	return 0
}

// CCI 商品通道指数
type CCI struct {
	period int
}

// NewCCI 创建 CCI 指标
func NewCCI(period int) *CCI {
	return &CCI{period: period}
}

// Name 指标名称
func (c *CCI) Name() string {
	return "CCI"
}

// Period 所需周期数
func (c *CCI) Period() int {
	return c.period
}

// Calculate 计算 CCI
func (c *CCI) Calculate(candles []Candle) []float64 {
	if len(candles) < c.period {
		return nil
	}

	// 典型价格
	tp := TypicalPrice(candles)

	// SMA of TP
	smaTP := SMA(tp, c.period)
	if smaTP == nil {
		return nil
	}

	result := make([]float64, len(smaTP))

	for i := range smaTP {
		// 计算平均偏差
		startIdx := i
		endIdx := i + c.period
		meanDev := 0.0
		for j := startIdx; j < endIdx && j < len(tp); j++ {
			meanDev += math.Abs(tp[j] - smaTP[i])
		}
		meanDev /= float64(c.period)

		// CCI = (TP - SMA) / (0.015 * Mean Deviation)
		if meanDev != 0 {
			result[i] = (tp[i+c.period-1] - smaTP[i]) / (0.015 * meanDev)
		}
	}

	return result
}

// Signal 交易信号
func (c *CCI) Signal(candles []Candle) int {
	cci := c.Calculate(candles)
	if cci == nil || len(cci) == 0 {
		return 0
	}

	current := cci[len(cci)-1]

	// CCI < -100: 超卖
	if current < -100 {
		return 1
	}
	// CCI > 100: 超买
	if current > 100 {
		return -1
	}

	return 0
}

// WilliamsR 威廉指标
type WilliamsR struct {
	period int
}

// NewWilliamsR 创建威廉指标
func NewWilliamsR(period int) *WilliamsR {
	return &WilliamsR{period: period}
}

// Name 指标名称
func (w *WilliamsR) Name() string {
	return "WilliamsR"
}

// Period 所需周期数
func (w *WilliamsR) Period() int {
	return w.period
}

// Calculate 计算威廉指标
func (w *WilliamsR) Calculate(candles []Candle) []float64 {
	if len(candles) < w.period {
		return nil
	}

	result := make([]float64, len(candles)-w.period+1)

	for i := w.period - 1; i < len(candles); i++ {
		high := candles[i-w.period+1].High
		low := candles[i-w.period+1].Low
		for j := i - w.period + 2; j <= i; j++ {
			if candles[j].High > high {
				high = candles[j].High
			}
			if candles[j].Low < low {
				low = candles[j].Low
			}
		}

		close := candles[i].Close
		if high != low {
			result[i-w.period+1] = (high - close) / (high - low) * -100
		}
	}

	return result
}

// Signal 交易信号
func (w *WilliamsR) Signal(candles []Candle) int {
	wr := w.Calculate(candles)
	if wr == nil || len(wr) == 0 {
		return 0
	}

	current := wr[len(wr)-1]

	// %R < -80: 超卖
	if current < -80 {
		return 1
	}
	// %R > -20: 超买
	if current > -20 {
		return -1
	}

	return 0
}

// MFI 资金流量指数
type MFI struct {
	period int
}

// NewMFI 创建 MFI 指标
func NewMFI(period int) *MFI {
	return &MFI{period: period}
}

// Name 指标名称
func (m *MFI) Name() string {
	return "MFI"
}

// Period 所需周期数
func (m *MFI) Period() int {
	return m.period + 1
}

// Calculate 计算 MFI
func (m *MFI) Calculate(candles []Candle) []float64 {
	if len(candles) < m.period+1 {
		return nil
	}

	// 计算典型价格
	tp := TypicalPrice(candles)

	// 计算资金流量
	result := make([]float64, len(candles)-m.period)

	for i := m.period; i < len(candles); i++ {
		positiveFlow := 0.0
		negativeFlow := 0.0

		for j := i - m.period + 1; j <= i; j++ {
			moneyFlow := tp[j] * candles[j].Volume
			if tp[j] > tp[j-1] {
				positiveFlow += moneyFlow
			} else if tp[j] < tp[j-1] {
				negativeFlow += moneyFlow
			}
		}

		if negativeFlow == 0 {
			result[i-m.period] = 100
		} else {
			mfr := positiveFlow / negativeFlow
			result[i-m.period] = 100 - 100/(1+mfr)
		}
	}

	return result
}

// Signal 交易信号
func (m *MFI) Signal(candles []Candle) int {
	mfi := m.Calculate(candles)
	if mfi == nil || len(mfi) == 0 {
		return 0
	}

	current := mfi[len(mfi)-1]

	// MFI < 20: 超卖
	if current < 20 {
		return 1
	}
	// MFI > 80: 超买
	if current > 80 {
		return -1
	}

	return 0
}

// ROC 变化率
type ROC struct {
	period int
}

// NewROC 创建 ROC 指标
func NewROC(period int) *ROC {
	return &ROC{period: period}
}

// Name 指标名称
func (r *ROC) Name() string {
	return "ROC"
}

// Period 所需周期数
func (r *ROC) Period() int {
	return r.period + 1
}

// Calculate 计算 ROC
func (r *ROC) Calculate(candles []Candle) []float64 {
	closes := ClosePrices(candles)
	return RateOfChange(closes, r.period)
}

// Momentum 动量指标
type Momentum struct {
	period int
}

// NewMomentum 创建动量指标
func NewMomentum(period int) *Momentum {
	return &Momentum{period: period}
}

// Name 指标名称
func (m *Momentum) Name() string {
	return "Momentum"
}

// Period 所需周期数
func (m *Momentum) Period() int {
	return m.period + 1
}

// Calculate 计算动量
func (m *Momentum) Calculate(candles []Candle) []float64 {
	closes := ClosePrices(candles)
	return Diff(closes, m.period)
}

// TRIX 三重指数平滑移动平均
type TRIX struct {
	period int
}

// NewTRIX 创建 TRIX 指标
func NewTRIX(period int) *TRIX {
	return &TRIX{period: period}
}

// Name 指标名称
func (t *TRIX) Name() string {
	return "TRIX"
}

// Period 所需周期数
func (t *TRIX) Period() int {
	return t.period * 3
}

// Calculate 计算 TRIX
func (t *TRIX) Calculate(candles []Candle) []float64 {
	closes := ClosePrices(candles)
	if len(closes) < t.period*3 {
		return nil
	}

	// 三重 EMA
	ema1 := EMA(closes, t.period)
	if ema1 == nil {
		return nil
	}
	ema2 := EMA(ema1, t.period)
	if ema2 == nil {
		return nil
	}
	ema3 := EMA(ema2, t.period)
	if ema3 == nil {
		return nil
	}

	// TRIX = (EMA3 - EMA3_prev) / EMA3_prev * 100
	result := make([]float64, len(ema3)-1)
	for i := 1; i < len(ema3); i++ {
		if ema3[i-1] != 0 {
			result[i-1] = (ema3[i] - ema3[i-1]) / ema3[i-1] * 100
		}
	}

	return result
}

// UltimateOscillator 终极振荡器
type UltimateOscillator struct {
	Period1 int
	Period2 int
	Period3 int
}

// NewUltimateOscillator 创建终极振荡器
func NewUltimateOscillator(p1, p2, p3 int) *UltimateOscillator {
	return &UltimateOscillator{
		Period1: p1,
		Period2: p2,
		Period3: p3,
	}
}

// Name 指标名称
func (uo *UltimateOscillator) Name() string {
	return "UltimateOscillator"
}

// Period 所需周期数
func (uo *UltimateOscillator) Period() int {
	return uo.Period3 + 1
}

// Calculate 计算终极振荡器
func (uo *UltimateOscillator) Calculate(candles []Candle) []float64 {
	if len(candles) < uo.Period3+1 {
		return nil
	}

	// 计算 BP (Buying Pressure) 和 TR
	bp := make([]float64, len(candles)-1)
	tr := make([]float64, len(candles)-1)

	for i := 1; i < len(candles); i++ {
		low := math.Min(candles[i].Low, candles[i-1].Close)
		bp[i-1] = candles[i].Close - low
		tr[i-1] = TrueRange(candles[i].High, candles[i].Low, candles[i-1].Close)
	}

	// 计算各周期的 BP 和 TR 之和
	calculateSum := func(values []float64, period int, idx int) float64 {
		sum := 0.0
		for j := idx - period + 1; j <= idx; j++ {
			if j >= 0 && j < len(values) {
				sum += values[j]
			}
		}
		return sum
	}

	result := make([]float64, len(bp)-uo.Period3+1)

	for i := uo.Period3 - 1; i < len(bp); i++ {
		bpSum1 := calculateSum(bp, uo.Period1, i)
		trSum1 := calculateSum(tr, uo.Period1, i)
		bpSum2 := calculateSum(bp, uo.Period2, i)
		trSum2 := calculateSum(tr, uo.Period2, i)
		bpSum3 := calculateSum(bp, uo.Period3, i)
		trSum3 := calculateSum(tr, uo.Period3, i)

		avg1, avg2, avg3 := 0.0, 0.0, 0.0
		if trSum1 != 0 {
			avg1 = bpSum1 / trSum1
		}
		if trSum2 != 0 {
			avg2 = bpSum2 / trSum2
		}
		if trSum3 != 0 {
			avg3 = bpSum3 / trSum3
		}

		// UO = 100 * (4*Avg1 + 2*Avg2 + Avg3) / 7
		result[i-uo.Period3+1] = 100 * (4*avg1 + 2*avg2 + avg3) / 7
	}

	return result
}

// Signal 交易信号
func (uo *UltimateOscillator) Signal(candles []Candle) int {
	values := uo.Calculate(candles)
	if values == nil || len(values) == 0 {
		return 0
	}

	current := values[len(values)-1]

	// UO < 30: 超卖
	if current < 30 {
		return 1
	}
	// UO > 70: 超买
	if current > 70 {
		return -1
	}

	return 0
}

// AwesomeOscillator 动量振荡器
type AwesomeOscillator struct {
	FastPeriod int
	SlowPeriod int
}

// NewAwesomeOscillator 创建动量振荡器
func NewAwesomeOscillator(fast, slow int) *AwesomeOscillator {
	return &AwesomeOscillator{
		FastPeriod: fast,
		SlowPeriod: slow,
	}
}

// Name 指标名称
func (ao *AwesomeOscillator) Name() string {
	return "AwesomeOscillator"
}

// Period 所需周期数
func (ao *AwesomeOscillator) Period() int {
	return ao.SlowPeriod
}

// Calculate 计算动量振荡器
func (ao *AwesomeOscillator) Calculate(candles []Candle) []float64 {
	hl2 := HL2(candles)
	if len(hl2) < ao.SlowPeriod {
		return nil
	}

	fastSMA := SMA(hl2, ao.FastPeriod)
	slowSMA := SMA(hl2, ao.SlowPeriod)

	if fastSMA == nil || slowSMA == nil {
		return nil
	}

	offset := len(fastSMA) - len(slowSMA)
	result := make([]float64, len(slowSMA))

	for i := range slowSMA {
		result[i] = fastSMA[i+offset] - slowSMA[i]
	}

	return result
}

// Signal 交易信号
func (ao *AwesomeOscillator) Signal(candles []Candle) int {
	values := ao.Calculate(candles)
	if values == nil || len(values) < 3 {
		return 0
	}

	n := len(values)
	// 连续两根正值且递增
	if values[n-1] > 0 && values[n-2] > 0 && values[n-1] > values[n-2] {
		return 1
	}
	// 连续两根负值且递减
	if values[n-1] < 0 && values[n-2] < 0 && values[n-1] < values[n-2] {
		return -1
	}

	return 0
}

// 注册动量指标
func init() {
	RegisterIndicator("RSI", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 14)
		return NewRSI(period)
	})

	RegisterIndicator("StochasticOscillator", func(params map[string]interface{}) Indicator {
		kPeriod := getIntParam(params, "k_period", 14)
		dPeriod := getIntParam(params, "d_period", 3)
		slowing := getIntParam(params, "slowing", 3)
		return NewStochasticOscillator(kPeriod, dPeriod, slowing)
	})

	RegisterIndicator("CCI", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 20)
		return NewCCI(period)
	})

	RegisterIndicator("WilliamsR", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 14)
		return NewWilliamsR(period)
	})

	RegisterIndicator("MFI", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 14)
		return NewMFI(period)
	})

	RegisterIndicator("ROC", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 12)
		return NewROC(period)
	})

	RegisterIndicator("Momentum", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 10)
		return NewMomentum(period)
	})

	RegisterIndicator("TRIX", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 15)
		return NewTRIX(period)
	})

	RegisterIndicator("UltimateOscillator", func(params map[string]interface{}) Indicator {
		p1 := getIntParam(params, "period1", 7)
		p2 := getIntParam(params, "period2", 14)
		p3 := getIntParam(params, "period3", 28)
		return NewUltimateOscillator(p1, p2, p3)
	})

	RegisterIndicator("AwesomeOscillator", func(params map[string]interface{}) Indicator {
		fast := getIntParam(params, "fast", 5)
		slow := getIntParam(params, "slow", 34)
		return NewAwesomeOscillator(fast, slow)
	})
}
