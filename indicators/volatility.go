package indicators

import (
	"math"
)

// ========== 波动率指标 ==========

// ATR 平均真实波幅
type ATR struct {
	period int
}

// NewATR 创建 ATR 指标
func NewATR(period int) *ATR {
	return &ATR{period: period}
}

// Name 指标名称
func (a *ATR) Name() string {
	return "ATR"
}

// Period 所需周期数
func (a *ATR) Period() int {
	return a.period + 1
}

// Calculate 计算 ATR
func (a *ATR) Calculate(candles []Candle) []float64 {
	if len(candles) < a.period+1 {
		return nil
	}

	// 计算真实波幅序列
	tr := TrueRangeSeries(candles)
	if tr == nil {
		return nil
	}

	// 使用 EMA 平滑（也可以用 SMA，这里用 EMA 更常见）
	return EMA(tr, a.period)
}

// CurrentATR 获取当前 ATR 值
func (a *ATR) CurrentATR(candles []Candle) float64 {
	atr := a.Calculate(candles)
	if atr == nil || len(atr) == 0 {
		return 0
	}
	return atr[len(atr)-1]
}

// BollingerBands 布林带
type BollingerBands struct {
	period     int
	multiplier float64
}

// NewBollingerBands 创建布林带指标
func NewBollingerBands(period int, multiplier float64) *BollingerBands {
	return &BollingerBands{
		period:     period,
		multiplier: multiplier,
	}
}

// Name 指标名称
func (bb *BollingerBands) Name() string {
	return "BollingerBands"
}

// Period 所需周期数
func (bb *BollingerBands) Period() int {
	return bb.period
}

// Calculate 计算中轨
func (bb *BollingerBands) Calculate(candles []Candle) []float64 {
	result := bb.CalculateMulti(candles)
	if result == nil {
		return nil
	}
	return result["middle"]
}

// CalculateMulti 计算上轨、中轨、下轨
func (bb *BollingerBands) CalculateMulti(candles []Candle) map[string][]float64 {
	closes := ClosePrices(candles)
	if len(closes) < bb.period {
		return nil
	}

	middle := SMA(closes, bb.period)
	stdDev := StdDev(closes, bb.period)

	if middle == nil || stdDev == nil {
		return nil
	}

	upper := make([]float64, len(middle))
	lower := make([]float64, len(middle))
	width := make([]float64, len(middle))
	percentB := make([]float64, len(middle))

	for i := range middle {
		band := bb.multiplier * stdDev[i]
		upper[i] = middle[i] + band
		lower[i] = middle[i] - band
		if middle[i] != 0 {
			width[i] = (upper[i] - lower[i]) / middle[i] * 100
		}
		if upper[i] != lower[i] {
			// %B 计算，需要对应的收盘价
			closeIdx := i + bb.period - 1
			if closeIdx < len(closes) {
				percentB[i] = (closes[closeIdx] - lower[i]) / (upper[i] - lower[i])
			}
		}
	}

	return map[string][]float64{
		"upper":     upper,
		"middle":    middle,
		"lower":     lower,
		"width":     width,
		"percent_b": percentB,
	}
}

// Signal 交易信号
func (bb *BollingerBands) Signal(candles []Candle) int {
	result := bb.CalculateMulti(candles)
	if result == nil {
		return 0
	}

	upper := result["upper"]
	lower := result["lower"]
	closes := ClosePrices(candles)

	if len(upper) == 0 || len(closes) < bb.period {
		return 0
	}

	n := len(upper) - 1
	closeIdx := n + bb.period - 1

	// 价格触及下轨：超卖，买入信号
	if closes[closeIdx] <= lower[n] {
		return 1
	}
	// 价格触及上轨：超买，卖出信号
	if closes[closeIdx] >= upper[n] {
		return -1
	}

	return 0
}

// KeltnerChannel 肯特纳通道
type KeltnerChannel struct {
	EMAPeriod  int
	ATRPeriod  int
	Multiplier float64
}

// NewKeltnerChannel 创建肯特纳通道指标
func NewKeltnerChannel(emaPeriod, atrPeriod int, multiplier float64) *KeltnerChannel {
	return &KeltnerChannel{
		EMAPeriod:  emaPeriod,
		ATRPeriod:  atrPeriod,
		Multiplier: multiplier,
	}
}

// Name 指标名称
func (kc *KeltnerChannel) Name() string {
	return "KeltnerChannel"
}

// Period 所需周期数
func (kc *KeltnerChannel) Period() int {
	if kc.EMAPeriod > kc.ATRPeriod {
		return kc.EMAPeriod
	}
	return kc.ATRPeriod
}

// Calculate 计算中轨
func (kc *KeltnerChannel) Calculate(candles []Candle) []float64 {
	result := kc.CalculateMulti(candles)
	if result == nil {
		return nil
	}
	return result["middle"]
}

// CalculateMulti 计算上轨、中轨、下轨
func (kc *KeltnerChannel) CalculateMulti(candles []Candle) map[string][]float64 {
	closes := ClosePrices(candles)
	if len(closes) < kc.Period() {
		return nil
	}

	// 中轨：EMA
	middle := EMA(closes, kc.EMAPeriod)

	// ATR
	atr := NewATR(kc.ATRPeriod)
	atrValues := atr.Calculate(candles)

	if middle == nil || atrValues == nil {
		return nil
	}

	// 对齐长度
	length := len(middle)
	if len(atrValues) < length {
		length = len(atrValues)
	}

	upper := make([]float64, length)
	lower := make([]float64, length)

	offsetMiddle := len(middle) - length
	offsetATR := len(atrValues) - length

	for i := 0; i < length; i++ {
		band := kc.Multiplier * atrValues[i+offsetATR]
		upper[i] = middle[i+offsetMiddle] + band
		lower[i] = middle[i+offsetMiddle] - band
	}

	return map[string][]float64{
		"upper":  upper,
		"middle": middle[offsetMiddle:],
		"lower":  lower,
	}
}

// DonchianChannel 唐奇安通道
type DonchianChannel struct {
	period int
}

// NewDonchianChannel 创建唐奇安通道指标
func NewDonchianChannel(period int) *DonchianChannel {
	return &DonchianChannel{period: period}
}

// Name 指标名称
func (dc *DonchianChannel) Name() string {
	return "DonchianChannel"
}

// Period 所需周期数
func (dc *DonchianChannel) Period() int {
	return dc.period
}

// Calculate 计算中轨
func (dc *DonchianChannel) Calculate(candles []Candle) []float64 {
	result := dc.CalculateMulti(candles)
	if result == nil {
		return nil
	}
	return result["middle"]
}

// CalculateMulti 计算上轨、中轨、下轨
func (dc *DonchianChannel) CalculateMulti(candles []Candle) map[string][]float64 {
	if len(candles) < dc.period {
		return nil
	}

	upper := HighestHigh(candles, dc.period)
	lower := LowestLow(candles, dc.period)

	if upper == nil || lower == nil {
		return nil
	}

	middle := make([]float64, len(upper))
	for i := range middle {
		middle[i] = (upper[i] + lower[i]) / 2
	}

	return map[string][]float64{
		"upper":  upper,
		"middle": middle,
		"lower":  lower,
	}
}

// Signal 交易信号（突破买入/卖出）
func (dc *DonchianChannel) Signal(candles []Candle) int {
	result := dc.CalculateMulti(candles)
	if result == nil || len(candles) < dc.period+1 {
		return 0
	}

	upper := result["upper"]
	lower := result["lower"]
	n := len(upper) - 1

	// 新高突破
	if candles[len(candles)-1].High > upper[n-1] {
		return 1
	}
	// 新低突破
	if candles[len(candles)-1].Low < lower[n-1] {
		return -1
	}

	return 0
}

// StandardDeviation 标准差
type StandardDeviation struct {
	period int
}

// NewStandardDeviation 创建标准差指标
func NewStandardDeviation(period int) *StandardDeviation {
	return &StandardDeviation{period: period}
}

// Name 指标名称
func (sd *StandardDeviation) Name() string {
	return "StandardDeviation"
}

// Period 所需周期数
func (sd *StandardDeviation) Period() int {
	return sd.period
}

// Calculate 计算标准差
func (sd *StandardDeviation) Calculate(candles []Candle) []float64 {
	closes := ClosePrices(candles)
	return StdDev(closes, sd.period)
}

// HistoricalVolatility 历史波动率
type HistoricalVolatility struct {
	period int
}

// NewHistoricalVolatility 创建历史波动率指标
func NewHistoricalVolatility(period int) *HistoricalVolatility {
	return &HistoricalVolatility{period: period}
}

// Name 指标名称
func (hv *HistoricalVolatility) Name() string {
	return "HistoricalVolatility"
}

// Period 所需周期数
func (hv *HistoricalVolatility) Period() int {
	return hv.period + 1
}

// Calculate 计算历史波动率（年化）
func (hv *HistoricalVolatility) Calculate(candles []Candle) []float64 {
	if len(candles) < hv.period+1 {
		return nil
	}

	// 计算对数收益率
	closes := ClosePrices(candles)
	logReturns := make([]float64, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		if closes[i-1] > 0 && closes[i] > 0 {
			logReturns[i-1] = math.Log(closes[i] / closes[i-1])
		}
	}

	// 计算标准差
	stdDevs := StdDev(logReturns, hv.period)
	if stdDevs == nil {
		return nil
	}

	// 年化（假设 365 天）
	annualizeFactor := math.Sqrt(365)
	result := make([]float64, len(stdDevs))
	for i, sd := range stdDevs {
		result[i] = sd * annualizeFactor * 100 // 转为百分比
	}

	return result
}

// NATR 标准化 ATR（百分比形式）
type NATR struct {
	period int
}

// NewNATR 创建 NATR 指标
func NewNATR(period int) *NATR {
	return &NATR{period: period}
}

// Name 指标名称
func (n *NATR) Name() string {
	return "NATR"
}

// Period 所需周期数
func (n *NATR) Period() int {
	return n.period + 1
}

// Calculate 计算 NATR
func (n *NATR) Calculate(candles []Candle) []float64 {
	atr := NewATR(n.period)
	atrValues := atr.Calculate(candles)
	if atrValues == nil {
		return nil
	}

	closes := ClosePrices(candles)
	offset := len(closes) - len(atrValues)

	result := make([]float64, len(atrValues))
	for i := range atrValues {
		closeIdx := i + offset
		if closes[closeIdx] != 0 {
			result[i] = atrValues[i] / closes[closeIdx] * 100
		}
	}

	return result
}

// UlcerIndex 溃疡指数（下行波动率）
type UlcerIndex struct {
	period int
}

// NewUlcerIndex 创建溃疡指数指标
func NewUlcerIndex(period int) *UlcerIndex {
	return &UlcerIndex{period: period}
}

// Name 指标名称
func (ui *UlcerIndex) Name() string {
	return "UlcerIndex"
}

// Period 所需周期数
func (ui *UlcerIndex) Period() int {
	return ui.period
}

// Calculate 计算溃疡指数
func (ui *UlcerIndex) Calculate(candles []Candle) []float64 {
	closes := ClosePrices(candles)
	if len(closes) < ui.period {
		return nil
	}

	result := make([]float64, len(closes)-ui.period+1)

	for i := ui.period - 1; i < len(closes); i++ {
		// 找到周期内的最高收盘价
		maxClose := closes[i-ui.period+1]
		for j := i - ui.period + 2; j <= i; j++ {
			if closes[j] > maxClose {
				maxClose = closes[j]
			}
		}

		// 计算百分比回撤的平方和
		sumSquared := 0.0
		for j := i - ui.period + 1; j <= i; j++ {
			drawdown := (closes[j] - maxClose) / maxClose * 100
			sumSquared += drawdown * drawdown
		}

		result[i-ui.period+1] = math.Sqrt(sumSquared / float64(ui.period))
	}

	return result
}

// 注册波动率指标
func init() {
	RegisterIndicator("ATR", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 14)
		return NewATR(period)
	})

	RegisterIndicator("BollingerBands", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 20)
		multiplier := getFloatParam(params, "multiplier", 2.0)
		return NewBollingerBands(period, multiplier)
	})

	RegisterIndicator("KeltnerChannel", func(params map[string]interface{}) Indicator {
		emaPeriod := getIntParam(params, "ema_period", 20)
		atrPeriod := getIntParam(params, "atr_period", 10)
		multiplier := getFloatParam(params, "multiplier", 2.0)
		return NewKeltnerChannel(emaPeriod, atrPeriod, multiplier)
	})

	RegisterIndicator("DonchianChannel", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 20)
		return NewDonchianChannel(period)
	})

	RegisterIndicator("StandardDeviation", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 20)
		return NewStandardDeviation(period)
	})

	RegisterIndicator("HistoricalVolatility", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 20)
		return NewHistoricalVolatility(period)
	})

	RegisterIndicator("NATR", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 14)
		return NewNATR(period)
	})

	RegisterIndicator("UlcerIndex", func(params map[string]interface{}) Indicator {
		period := getIntParam(params, "period", 14)
		return NewUlcerIndex(period)
	})
}
