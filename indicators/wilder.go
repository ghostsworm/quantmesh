package indicators

import "math"

// ========== Wilder 平滑系列（RMA / ATR / ADX） ==========
//
// 本文件提供按 J. Welles Wilder 原始定義实现的指標，與交易所/TradingView
// 默认口径一致：平滑因子 alpha = 1/period（而非 EMA 的 2/(period+1)）。
// 既有的 ATR / ADX 结构体使用 EMA 平滑，数值偏"快"，为兼容旧调用方保留不动。

// percentScale 百分比换算系數
const percentScale = 100.0

// RMA Wilder 平滑移动平均（又称 SMMA）。
// 首值为前 period 个值的 SMA，之后 rma[i] = (rma[i-1]*(period-1) + v[i]) / period。
// 返回长度为 len(values)-period+1，最后一个元素对应 values 的最后一个元素；
// 数据不足或 period<=0 时返回 nil。
func RMA(values []float64, period int) []float64 {
	if period <= 0 || len(values) < period {
		return nil
	}

	result := make([]float64, len(values)-period+1)
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	result[0] = sum / float64(period)

	p := float64(period)
	for i := period; i < len(values); i++ {
		j := i - period + 1
		result[j] = (result[j-1]*(p-1) + values[i]) / p
	}
	return result
}

// WilderATR Wilder 口径的平均真實波幅。
// 返回长度为 len(candles)-period，最后一个元素对应最后一根 K 線；
// 数据不足（需要 period+1 根）时返回 nil。
func WilderATR(candles []Candle, period int) []float64 {
	if period <= 0 || len(candles) < period+1 {
		return nil
	}
	return RMA(TrueRangeSeries(candles), period)
}

// WilderADX Wilder 口径的 ADX 及 +DI / -DI。
// 三个切片等长且尾部对齐，最后一个元素对应最后一根 K 線；
// 需要至少 2*period 根 K 線，否则返回 nil。
func WilderADX(candles []Candle, period int) (adx, plusDI, minusDI []float64) {
	if period <= 0 || len(candles) < 2*period {
		return nil, nil, nil
	}

	n := len(candles) - 1
	plusDM := make([]float64, n)
	minusDM := make([]float64, n)
	tr := make([]float64, n)
	for i := 1; i < len(candles); i++ {
		upMove := candles[i].High - candles[i-1].High
		downMove := candles[i-1].Low - candles[i].Low
		if upMove > downMove && upMove > 0 {
			plusDM[i-1] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM[i-1] = downMove
		}
		tr[i-1] = TrueRange(candles[i].High, candles[i].Low, candles[i-1].Close)
	}

	// RMA 对"和"与"均值"做平滑只差常數倍，DI 为比值，结果相同
	smTR := RMA(tr, period)
	smPlus := RMA(plusDM, period)
	smMinus := RMA(minusDM, period)
	if smTR == nil {
		return nil, nil, nil
	}

	length := len(smTR)
	pdi := make([]float64, length)
	mdi := make([]float64, length)
	dx := make([]float64, length)
	for i := 0; i < length; i++ {
		if smTR[i] > 0 {
			pdi[i] = percentScale * smPlus[i] / smTR[i]
			mdi[i] = percentScale * smMinus[i] / smTR[i]
		}
		if sum := pdi[i] + mdi[i]; sum > 0 {
			dx[i] = percentScale * math.Abs(pdi[i]-mdi[i]) / sum
		}
	}

	adx = RMA(dx, period)
	if adx == nil {
		return nil, nil, nil
	}
	offset := length - len(adx)
	return adx, pdi[offset:], mdi[offset:]
}

// PercentileRank 返回 x 在 values 中的百分位排名（0-100）：
// 小于 x 的个數 + 等于 x 的个數的一半，除以总数。
// values 为空或含 NaN 的 x 时返回 NaN。
func PercentileRank(values []float64, x float64) float64 {
	if len(values) == 0 || math.IsNaN(x) {
		return math.NaN()
	}
	less, equal := 0, 0
	for _, v := range values {
		switch {
		case v < x:
			less++
		case v == x:
			equal++
		}
	}
	return percentScale * (float64(less) + float64(equal)/2) / float64(len(values))
}
