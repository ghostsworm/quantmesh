package indicators

import (
	"math"
)

// ========== 趋势指标 ==========

// MACD 指数平滑异同移动平均线
type MACD struct {
	FastPeriod   int
	SlowPeriod   int
	SignalPeriod int
}

// NewMACD 创建 MACD 指标
func NewMACD(fast, slow, signal int) *MACD {
	return &MACD{
		FastPeriod:   fast,
		SlowPeriod:   slow,
		SignalPeriod: signal,
	}
}

// Name 指标名称
func (m *MACD) Name() string {
	return "MACD"
}

// Period 所需周期数
func (m *MACD) Period() int {
	return m.SlowPeriod + m.SignalPeriod
}

// Calculate 计算 MACD 线
func (m *MACD) Calculate(candles []Candle) []float64 {
	result := m.CalculateMulti(candles)
	if result == nil {
		return nil
	}
	return result["macd"]
}

// CalculateMulti 计算所有 MACD 组件
func (m *MACD) CalculateMulti(candles []Candle) map[string][]float64 {
	closes := ClosePrices(candles)
	if len(closes) < m.SlowPeriod+m.SignalPeriod {
		return nil
	}

	fastEMA := EMA(closes, m.FastPeriod)
	slowEMA := EMA(closes, m.SlowPeriod)

	if fastEMA == nil || slowEMA == nil {
		return nil
	}

	// 对齐长度
	offset := len(fastEMA) - len(slowEMA)
	macdLine := make([]float64, len(slowEMA))
	for i := range macdLine {
		macdLine[i] = fastEMA[i+offset] - slowEMA[i]
	}

	// 计算信号线
	signalLine := EMA(macdLine, m.SignalPeriod)
	if signalLine == nil {
		return nil
	}

	// 计算柱状图
	offset2 := len(macdLine) - len(signalLine)
	histogram := make([]float64, len(signalLine))
	for i := range histogram {
		histogram[i] = macdLine[i+offset2] - signalLine[i]
	}

	return map[string][]float64{
		"macd":      macdLine[offset2:],
		"signal":    signalLine,
		"histogram": histogram,
	}
}

// Signal 交易信号
func (m *MACD) Signal(candles []Candle) int {
	result := m.CalculateMulti(candles)
	if result == nil || len(result["macd"]) < 2 {
		return 0
	}

	macd := result["macd"]
	signal := result["signal"]

	if CrossOver(macd, signal) {
		return 1 // 买入信号
	}
	if CrossUnder(macd, signal) {
		return -1 // 卖出信号
	}

	return 0
}

// ADX 平均趋向指数
type ADX struct {
	period int
}

// NewADX 创建 ADX 指标
func NewADX(period int) *ADX {
	return &ADX{period: period}
}

// Name 指标名称
func (a *ADX) Name() string {
	return "ADX"
}

// Period 所需周期数
func (a *ADX) Period() int {
	return a.period*2 + 1
}

// Calculate 计算 ADX
func (a *ADX) Calculate(candles []Candle) []float64 {
	result := a.CalculateMulti(candles)
	if result == nil {
		return nil
	}
	return result["adx"]
}

// CalculateMulti 计算 ADX 及 +DI/-DI
func (a *ADX) CalculateMulti(candles []Candle) map[string][]float64 {
	if len(candles) < a.period*2+1 {
		return nil
	}

	// 计算 +DM, -DM, TR
	plusDM := make([]float64, len(candles)-1)
	minusDM := make([]float64, len(candles)-1)
	tr := make([]float64, len(candles)-1)

	for i := 1; i < len(candles); i++ {
		high := candles[i].High
		low := candles[i].Low
		prevHigh := candles[i-1].High
		prevLow := candles[i-1].Low
		prevClose := candles[i-1].Close

		upMove := high - prevHigh
		downMove := prevLow - low

		if upMove > downMove && upMove > 0 {
			plusDM[i-1] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM[i-1] = downMove
		}

		tr[i-1] = TrueRange(high, low, prevClose)
	}

	// 平滑 +DM, -DM, TR
	smoothPlusDM := EMA(plusDM, a.period)
	smoothMinusDM := EMA(minusDM, a.period)
	smoothTR := EMA(tr, a.period)

	if smoothPlusDM == nil || smoothMinusDM == nil || smoothTR == nil {
		return nil
	}

	// 计算 +DI, -DI
	length := len(smoothTR)
	plusDI := make([]float64, length)
	minusDI := make([]float64, length)
	dx := make([]float64, length)

	for i := 0; i < length; i++ {
		if smoothTR[i] != 0 {
			plusDI[i] = 100 * smoothPlusDM[i] / smoothTR[i]
			minusDI[i] = 100 * smoothMinusDM[i] / smoothTR[i]
		}
		diSum := plusDI[i] + minusDI[i]
		if diSum != 0 {
			dx[i] = 100 * math.Abs(plusDI[i]-minusDI[i]) / diSum
		}
	}

	// 计算 ADX
	adx := EMA(dx, a.period)

	return map[string][]float64{
		"adx":      adx,
		"plus_di":  plusDI[len(plusDI)-len(adx):],
		"minus_di": minusDI[len(minusDI)-len(adx):],
	}
}

// Signal 交易信号（ADX > 25 表示趋势强）
func (a *ADX) Signal(candles []Candle) int {
	result := a.CalculateMulti(candles)
	if result == nil || len(result["adx"]) == 0 {
		return 0
	}

	adx := result["adx"]
	plusDI := result["plus_di"]
	minusDI := result["minus_di"]
	n := len(adx) - 1

	// ADX > 25 表示趋势强
	if adx[n] > 25 {
		if plusDI[n] > minusDI[n] {
			return 1 // 上涨趋势
		}
		return -1 // 下跌趋势
	}

	return 0 // 无明显趋势
}

// ParabolicSAR 抛物线转向指标
type ParabolicSAR struct {
	AFStart float64 // 加速因子起始值
	AFStep  float64 // 加速因子增量
	AFMax   float64 // 加速因子最大值
}

// NewParabolicSAR 创建 Parabolic SAR 指标
func NewParabolicSAR(afStart, afStep, afMax float64) *ParabolicSAR {
	return &ParabolicSAR{
		AFStart: afStart,
		AFStep:  afStep,
		AFMax:   afMax,
	}
}

// Name 指标名称
func (p *ParabolicSAR) Name() string {
	return "Parabolic SAR"
}

// Period 所需周期数
func (p *ParabolicSAR) Period() int {
	return 2
}

// Calculate 计算 Parabolic SAR
func (p *ParabolicSAR) Calculate(candles []Candle) []float64 {
	if len(candles) < 2 {
		return nil
	}

	sar := make([]float64, len(candles))
	isUpTrend := candles[1].Close > candles[0].Close
	af := p.AFStart
	ep := candles[0].High
	if !isUpTrend {
		ep = candles[0].Low
	}
	sar[0] = candles[0].Low
	if !isUpTrend {
		sar[0] = candles[0].High
	}

	for i := 1; i < len(candles); i++ {
		if isUpTrend {
			sar[i] = sar[i-1] + af*(ep-sar[i-1])
			sar[i] = math.Min(sar[i], candles[i-1].Low)
			if i >= 2 {
				sar[i] = math.Min(sar[i], candles[i-2].Low)
			}

			if candles[i].Low < sar[i] {
				// 转为下跌趋势
				isUpTrend = false
				sar[i] = ep
				ep = candles[i].Low
				af = p.AFStart
			} else {
				if candles[i].High > ep {
					ep = candles[i].High
					af = math.Min(af+p.AFStep, p.AFMax)
				}
			}
		} else {
			sar[i] = sar[i-1] + af*(ep-sar[i-1])
			sar[i] = math.Max(sar[i], candles[i-1].High)
			if i >= 2 {
				sar[i] = math.Max(sar[i], candles[i-2].High)
			}

			if candles[i].High > sar[i] {
				// 转为上涨趋势
				isUpTrend = true
				sar[i] = ep
				ep = candles[i].High
				af = p.AFStart
			} else {
				if candles[i].Low < ep {
					ep = candles[i].Low
					af = math.Min(af+p.AFStep, p.AFMax)
				}
			}
		}
	}

	return sar
}

// Signal 交易信号
func (p *ParabolicSAR) Signal(candles []Candle) int {
	if len(candles) < 3 {
		return 0
	}

	sar := p.Calculate(candles)
	if sar == nil {
		return 0
	}

	n := len(candles) - 1
	// 价格从下方突破 SAR
	if candles[n-1].Close < sar[n-1] && candles[n].Close > sar[n] {
		return 1
	}
	// 价格从上方跌破 SAR
	if candles[n-1].Close > sar[n-1] && candles[n].Close < sar[n] {
		return -1
	}

	return 0
}

// Ichimoku 一目均衡表
type Ichimoku struct {
	TenkanPeriod  int // 转换线周期
	KijunPeriod   int // 基准线周期
	SenkouBPeriod int // 先行带 B 周期
	Displacement  int // 位移
}

// NewIchimoku 创建一目均衡表指标
func NewIchimoku(tenkan, kijun, senkouB, displacement int) *Ichimoku {
	return &Ichimoku{
		TenkanPeriod:  tenkan,
		KijunPeriod:   kijun,
		SenkouBPeriod: senkouB,
		Displacement:  displacement,
	}
}

// Name 指标名称
func (ich *Ichimoku) Name() string {
	return "Ichimoku"
}

// Period 所需周期数
func (ich *Ichimoku) Period() int {
	return ich.SenkouBPeriod + ich.Displacement
}

// Calculate 计算转换线
func (ich *Ichimoku) Calculate(candles []Candle) []float64 {
	result := ich.CalculateMulti(candles)
	if result == nil {
		return nil
	}
	return result["tenkan"]
}

// CalculateMulti 计算所有组件
func (ich *Ichimoku) CalculateMulti(candles []Candle) map[string][]float64 {
	if len(candles) < ich.SenkouBPeriod {
		return nil
	}

	// 转换线：(最高价 + 最低价) / 2，周期 9
	tenkan := ich.calculateMiddleLine(candles, ich.TenkanPeriod)

	// 基准线：(最高价 + 最低价) / 2，周期 26
	kijun := ich.calculateMiddleLine(candles, ich.KijunPeriod)

	// 先行带 A：(转换线 + 基准线) / 2
	offset := len(tenkan) - len(kijun)
	senkouA := make([]float64, len(kijun))
	for i := range senkouA {
		senkouA[i] = (tenkan[i+offset] + kijun[i]) / 2
	}

	// 先行带 B：(最高价 + 最低价) / 2，周期 52
	senkouB := ich.calculateMiddleLine(candles, ich.SenkouBPeriod)

	// 迟行带：收盘价
	chikou := ClosePrices(candles)

	return map[string][]float64{
		"tenkan":   tenkan,
		"kijun":    kijun,
		"senkou_a": senkouA,
		"senkou_b": senkouB,
		"chikou":   chikou,
	}
}

// calculateMiddleLine 计算中线 (最高 + 最低) / 2
func (ich *Ichimoku) calculateMiddleLine(candles []Candle, period int) []float64 {
	if len(candles) < period {
		return nil
	}

	result := make([]float64, len(candles)-period+1)
	for i := period - 1; i < len(candles); i++ {
		high := candles[i-period+1].High
		low := candles[i-period+1].Low
		for j := i - period + 2; j <= i; j++ {
			if candles[j].High > high {
				high = candles[j].High
			}
			if candles[j].Low < low {
				low = candles[j].Low
			}
		}
		result[i-period+1] = (high + low) / 2
	}

	return result
}

// Signal 交易信号
func (ich *Ichimoku) Signal(candles []Candle) int {
	result := ich.CalculateMulti(candles)
	if result == nil {
		return 0
	}

	tenkan := result["tenkan"]
	kijun := result["kijun"]

	if len(tenkan) < 2 || len(kijun) < 2 {
		return 0
	}

	// 转换线上穿基准线
	offset := len(tenkan) - len(kijun)
	tenkanAligned := tenkan[offset:]

	if CrossOver(tenkanAligned, kijun) {
		return 1
	}
	if CrossUnder(tenkanAligned, kijun) {
		return -1
	}

	return 0
}

// Aroon 阿隆指标
type Aroon struct {
	period int
}

// NewAroon 创建阿隆指标
func NewAroon(period int) *Aroon {
	return &Aroon{period: period}
}

// Name 指标名称
func (a *Aroon) Name() string {
	return "Aroon"
}

// Period 所需周期数
func (a *Aroon) Period() int {
	return a.period + 1
}

// Calculate 计算 Aroon 振荡器
func (a *Aroon) Calculate(candles []Candle) []float64 {
	result := a.CalculateMulti(candles)
	if result == nil {
		return nil
	}
	return result["oscillator"]
}

// CalculateMulti 计算 Aroon Up, Down 和振荡器
func (a *Aroon) CalculateMulti(candles []Candle) map[string][]float64 {
	if len(candles) < a.period+1 {
		return nil
	}

	aroonUp := make([]float64, len(candles)-a.period)
	aroonDown := make([]float64, len(candles)-a.period)
	oscillator := make([]float64, len(candles)-a.period)

	for i := a.period; i < len(candles); i++ {
		highIdx := 0
		lowIdx := 0
		high := candles[i-a.period].High
		low := candles[i-a.period].Low

		for j := 1; j <= a.period; j++ {
			idx := i - a.period + j
			if candles[idx].High >= high {
				high = candles[idx].High
				highIdx = j
			}
			if candles[idx].Low <= low {
				low = candles[idx].Low
				lowIdx = j
			}
		}

		aroonUp[i-a.period] = float64(highIdx) / float64(a.period) * 100
		aroonDown[i-a.period] = float64(lowIdx) / float64(a.period) * 100
		oscillator[i-a.period] = aroonUp[i-a.period] - aroonDown[i-a.period]
	}

	return map[string][]float64{
		"aroon_up":   aroonUp,
		"aroon_down": aroonDown,
		"oscillator": oscillator,
	}
}

// Signal 交易信号
func (a *Aroon) Signal(candles []Candle) int {
	result := a.CalculateMulti(candles)
	if result == nil {
		return 0
	}

	up := result["aroon_up"]
	down := result["aroon_down"]

	if len(up) == 0 {
		return 0
	}

	n := len(up) - 1
	// Aroon Up > 70 且 Aroon Down < 30
	if up[n] > 70 && down[n] < 30 {
		return 1
	}
	// Aroon Down > 70 且 Aroon Up < 30
	if down[n] > 70 && up[n] < 30 {
		return -1
	}

	return 0
}

// SuperTrend 超级趋势指标
type SuperTrend struct {
	period     int
	multiplier float64
}

// NewSuperTrend 创建超级趋势指标
func NewSuperTrend(period int, multiplier float64) *SuperTrend {
	return &SuperTrend{
		period:     period,
		multiplier: multiplier,
	}
}

// Name 指标名称
func (st *SuperTrend) Name() string {
	return "SuperTrend"
}

// Period 所需周期数
func (st *SuperTrend) Period() int {
	return st.period + 1
}

// Calculate 计算 SuperTrend
func (st *SuperTrend) Calculate(candles []Candle) []float64 {
	result := st.CalculateMulti(candles)
	if result == nil {
		return nil
	}
	return result["supertrend"]
}

// CalculateMulti 计算 SuperTrend 及趋势方向
func (st *SuperTrend) CalculateMulti(candles []Candle) map[string][]float64 {
	if len(candles) < st.period+1 {
		return nil
	}

	// 计算 ATR
	atr := NewATR(st.period)
	atrValues := atr.Calculate(candles)
	if atrValues == nil {
		return nil
	}

	hl2 := HL2(candles)
	offset := len(hl2) - len(atrValues)

	upperBand := make([]float64, len(atrValues))
	lowerBand := make([]float64, len(atrValues))
	supertrend := make([]float64, len(atrValues))
	direction := make([]float64, len(atrValues)) // 1=上涨，-1=下跌

	for i := 0; i < len(atrValues); i++ {
		basicUpper := hl2[i+offset] + st.multiplier*atrValues[i]
		basicLower := hl2[i+offset] - st.multiplier*atrValues[i]

		if i == 0 {
			upperBand[i] = basicUpper
			lowerBand[i] = basicLower
			direction[i] = 1
			supertrend[i] = lowerBand[i]
		} else {
			// 上轨
			if basicUpper < upperBand[i-1] || candles[i+offset-1].Close > upperBand[i-1] {
				upperBand[i] = basicUpper
			} else {
				upperBand[i] = upperBand[i-1]
			}

			// 下轨
			if basicLower > lowerBand[i-1] || candles[i+offset-1].Close < lowerBand[i-1] {
				lowerBand[i] = basicLower
			} else {
				lowerBand[i] = lowerBand[i-1]
			}

			// 趋势方向
			if direction[i-1] == 1 {
				if candles[i+offset].Close < lowerBand[i] {
					direction[i] = -1
				} else {
					direction[i] = 1
				}
			} else {
				if candles[i+offset].Close > upperBand[i] {
					direction[i] = 1
				} else {
					direction[i] = -1
				}
			}

			if direction[i] == 1 {
				supertrend[i] = lowerBand[i]
			} else {
				supertrend[i] = upperBand[i]
			}
		}
	}

	return map[string][]float64{
		"supertrend": supertrend,
		"direction":  direction,
		"upper_band": upperBand,
		"lower_band": lowerBand,
	}
}

// Signal 交易信号
func (st *SuperTrend) Signal(candles []Candle) int {
	result := st.CalculateMulti(candles)
	if result == nil {
		return 0
	}

	direction := result["direction"]
	if len(direction) < 2 {
		return 0
	}

	n := len(direction) - 1
	if direction[n-1] == -1 && direction[n] == 1 {
		return 1 // 转为上涨
	}
	if direction[n-1] == 1 && direction[n] == -1 {
		return -1 // 转为下跌
	}

	return 0
}

// 注册趋势指标
func init() {
	RegisterIndicator("MACD", func(params map[string]interface{}) Indicator {
		fast := getIntParam(params, "fast", 12)
		slow := getIntParam(params, "slow", 26)
		signal := getIntParam(params, "signal", 9)
		return NewMACD(fast, slow, signal)
	})

	RegisterIndicator("ADX", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 14)
		return NewADX(period)
	})

	RegisterIndicator("ParabolicSAR", func(params map[string]interface{}) Indicator {
		afStart := getFloatParam(params, "af_start", 0.02)
		afStep := getFloatParam(params, "af_step", 0.02)
		afMax := getFloatParam(params, "af_max", 0.2)
		return NewParabolicSAR(afStart, afStep, afMax)
	})

	RegisterIndicator("Ichimoku", func(params map[string]interface{}) Indicator {
		tenkan := getIntParam(params, "tenkan", 9)
		kijun := getIntParam(params, "kijun", 26)
		senkouB := getIntParam(params, "senkou_b", 52)
		displacement := getIntParam(params, "displacement", 26)
		return NewIchimoku(tenkan, kijun, senkouB, displacement)
	})

	RegisterIndicator("Aroon", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 25)
		return NewAroon(period)
	})

	RegisterIndicator("SuperTrend", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 10)
		multiplier := getFloatParam(params, "multiplier", 3.0)
		return NewSuperTrend(period, multiplier)
	})
}

// 辅助函数
func getIntParam(params map[string]interface{}, key string, defaultVal int) int {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		}
	}
	return defaultVal
}

func getFloatParam(params map[string]interface{}, key string, defaultVal float64) float64 {
	if v, ok := params[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		}
	}
	return defaultVal
}
