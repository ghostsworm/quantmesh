package indicators

import (
	"math"
	"sort"
)

// ========== 基础计算工具 ==========

// SMA 简单移动平均
func SMA(values []float64, period int) []float64 {
	if len(values) < period {
		return nil
	}

	result := make([]float64, len(values)-period+1)
	sum := 0.0

	// 计算第一个 SMA
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	result[0] = sum / float64(period)

	// 滑动计算后续 SMA
	for i := period; i < len(values); i++ {
		sum = sum - values[i-period] + values[i]
		result[i-period+1] = sum / float64(period)
	}

	return result
}

// EMA 指数移动平均
func EMA(values []float64, period int) []float64 {
	if len(values) < period {
		return nil
	}

	result := make([]float64, len(values))
	multiplier := 2.0 / (float64(period) + 1.0)

	// 第一个 EMA 使用 SMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	result[period-1] = sum / float64(period)

	// 计算后续 EMA
	for i := period; i < len(values); i++ {
		result[i] = (values[i] * multiplier) + (result[i-1] * (1 - multiplier))
	}

	return result[period-1:]
}

// WMA 加权移动平均
func WMA(values []float64, period int) []float64 {
	if len(values) < period {
		return nil
	}

	result := make([]float64, len(values)-period+1)
	weightSum := float64(period * (period + 1) / 2)

	for i := period - 1; i < len(values); i++ {
		sum := 0.0
		for j := 0; j < period; j++ {
			weight := float64(j + 1)
			sum += values[i-period+1+j] * weight
		}
		result[i-period+1] = sum / weightSum
	}

	return result
}

// DEMA 双指数移动平均
func DEMA(values []float64, period int) []float64 {
	ema1 := EMA(values, period)
	if ema1 == nil {
		return nil
	}
	ema2 := EMA(ema1, period)
	if ema2 == nil {
		return nil
	}

	// DEMA = 2 * EMA - EMA(EMA)
	offset := len(ema1) - len(ema2)
	result := make([]float64, len(ema2))
	for i := range result {
		result[i] = 2*ema1[i+offset] - ema2[i]
	}

	return result
}

// TEMA 三重指数移动平均
func TEMA(values []float64, period int) []float64 {
	ema1 := EMA(values, period)
	if ema1 == nil {
		return nil
	}
	ema2 := EMA(ema1, period)
	if ema2 == nil {
		return nil
	}
	ema3 := EMA(ema2, period)
	if ema3 == nil {
		return nil
	}

	// TEMA = 3 * EMA - 3 * EMA(EMA) + EMA(EMA(EMA))
	offset1 := len(ema1) - len(ema3)
	offset2 := len(ema2) - len(ema3)
	result := make([]float64, len(ema3))
	for i := range result {
		result[i] = 3*ema1[i+offset1] - 3*ema2[i+offset2] + ema3[i]
	}

	return result
}

// StdDev 标准差
func StdDev(values []float64, period int) []float64 {
	if len(values) < period {
		return nil
	}

	result := make([]float64, len(values)-period+1)

	for i := period - 1; i < len(values); i++ {
		slice := values[i-period+1 : i+1]
		mean := Mean(slice)
		variance := 0.0
		for _, v := range slice {
			diff := v - mean
			variance += diff * diff
		}
		result[i-period+1] = math.Sqrt(variance / float64(period))
	}

	return result
}

// Mean 平均值
func Mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// Max 最大值
func Max(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}

// Min 最小值
func Min(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	min := values[0]
	for _, v := range values[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

// Sum 求和
func Sum(values []float64) float64 {
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum
}

// Median 中位数
func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	n := len(sorted)
	if n%2 == 0 {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return sorted[n/2]
}

// TrueRange 真实波幅
func TrueRange(high, low, prevClose float64) float64 {
	tr1 := high - low
	tr2 := math.Abs(high - prevClose)
	tr3 := math.Abs(low - prevClose)
	return math.Max(tr1, math.Max(tr2, tr3))
}

// TrueRangeSeries 真实波幅序列
func TrueRangeSeries(candles []Candle) []float64 {
	if len(candles) < 2 {
		return nil
	}

	result := make([]float64, len(candles)-1)
	for i := 1; i < len(candles); i++ {
		result[i-1] = TrueRange(candles[i].High, candles[i].Low, candles[i-1].Close)
	}

	return result
}

// HighestHigh 最高价的最高值
func HighestHigh(candles []Candle, period int) []float64 {
	if len(candles) < period {
		return nil
	}

	result := make([]float64, len(candles)-period+1)
	for i := period - 1; i < len(candles); i++ {
		high := candles[i-period+1].High
		for j := i - period + 2; j <= i; j++ {
			if candles[j].High > high {
				high = candles[j].High
			}
		}
		result[i-period+1] = high
	}

	return result
}

// LowestLow 最低价的最低值
func LowestLow(candles []Candle, period int) []float64 {
	if len(candles) < period {
		return nil
	}

	result := make([]float64, len(candles)-period+1)
	for i := period - 1; i < len(candles); i++ {
		low := candles[i-period+1].Low
		for j := i - period + 2; j <= i; j++ {
			if candles[j].Low < low {
				low = candles[j].Low
			}
		}
		result[i-period+1] = low
	}

	return result
}

// ClosePrices 提取收盘价序列
func ClosePrices(candles []Candle) []float64 {
	result := make([]float64, len(candles))
	for i, c := range candles {
		result[i] = c.Close
	}
	return result
}

// HighPrices 提取最高价序列
func HighPrices(candles []Candle) []float64 {
	result := make([]float64, len(candles))
	for i, c := range candles {
		result[i] = c.High
	}
	return result
}

// LowPrices 提取最低价序列
func LowPrices(candles []Candle) []float64 {
	result := make([]float64, len(candles))
	for i, c := range candles {
		result[i] = c.Low
	}
	return result
}

// OpenPrices 提取开盘价序列
func OpenPrices(candles []Candle) []float64 {
	result := make([]float64, len(candles))
	for i, c := range candles {
		result[i] = c.Open
	}
	return result
}

// Volumes 提取成交量序列
func Volumes(candles []Candle) []float64 {
	result := make([]float64, len(candles))
	for i, c := range candles {
		result[i] = c.Volume
	}
	return result
}

// TypicalPrice 典型价格 (H+L+C)/3
func TypicalPrice(candles []Candle) []float64 {
	result := make([]float64, len(candles))
	for i, c := range candles {
		result[i] = (c.High + c.Low + c.Close) / 3
	}
	return result
}

// HLC3 (H+L+C)/3 同 TypicalPrice
func HLC3(candles []Candle) []float64 {
	return TypicalPrice(candles)
}

// OHLC4 (O+H+L+C)/4
func OHLC4(candles []Candle) []float64 {
	result := make([]float64, len(candles))
	for i, c := range candles {
		result[i] = (c.Open + c.High + c.Low + c.Close) / 4
	}
	return result
}

// HL2 (H+L)/2
func HL2(candles []Candle) []float64 {
	result := make([]float64, len(candles))
	for i, c := range candles {
		result[i] = (c.High + c.Low) / 2
	}
	return result
}

// CrossOver 判断是否金叉（line1 上穿 line2）
func CrossOver(line1, line2 []float64) bool {
	if len(line1) < 2 || len(line2) < 2 {
		return false
	}
	n := len(line1)
	return line1[n-2] <= line2[n-2] && line1[n-1] > line2[n-1]
}

// CrossUnder 判断是否死叉（line1 下穿 line2）
func CrossUnder(line1, line2 []float64) bool {
	if len(line1) < 2 || len(line2) < 2 {
		return false
	}
	n := len(line1)
	return line1[n-2] >= line2[n-2] && line1[n-1] < line2[n-1]
}

// RateOfChange 变化率
func RateOfChange(values []float64, period int) []float64 {
	if len(values) <= period {
		return nil
	}

	result := make([]float64, len(values)-period)
	for i := period; i < len(values); i++ {
		if values[i-period] != 0 {
			result[i-period] = (values[i] - values[i-period]) / values[i-period] * 100
		}
	}

	return result
}

// Diff 差分
func Diff(values []float64, period int) []float64 {
	if len(values) <= period {
		return nil
	}

	result := make([]float64, len(values)-period)
	for i := period; i < len(values); i++ {
		result[i-period] = values[i] - values[i-period]
	}

	return result
}

// Shift 位移（滞后）
func Shift(values []float64, period int) []float64 {
	if len(values) <= period {
		return nil
	}
	return values[:len(values)-period]
}

// Percentile 百分位数
func Percentile(values []float64, p float64) float64 {
	if len(values) == 0 || p < 0 || p > 100 {
		return 0
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	n := len(sorted)
	rank := (p / 100) * float64(n-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))

	if lower == upper {
		return sorted[lower]
	}

	weight := rank - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}
