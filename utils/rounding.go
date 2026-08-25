package utils

import (
	"math"
	"strconv"
	"strings"
)

// 交易所數量/價格對齊的公共實現。
//
// 背景：直接寫 math.Floor(qty/step)*step 會被 IEEE754 誤差坑死。
// 例如 0.3/0.1 在雙精度下等於 2.9999999999999996，Floor 之後變成 2，
// 最終下單量只剩 0.2——憑空少下整整一個 step（33%）。
// 價格側同理，8.70/0.01 = 869.9999999999999，買單會被壓到 8.69。
//
// 解法：先判斷比值是否已「極接近」某個整數，是則直接採用該整數，
// 否則才做真正的 Floor/Ceil；最後按 step 的小數位數收斂結果，
// 避免 29*0.1 = 2.9000000000000004 這類回乘誤差流到下單參數裡。
const (
	// alignTolerance 判定「已對齊」的相對容差。
	// 取 1e-13（雙精度下約數百個 ULP）：真實的表示誤差只有 1~4 個 ULP
	// （0.3/0.1 的誤差相對量級是 1.5e-16），這個容差足夠吸收；
	// 同時又遠小於「真正需要截斷」的差距，不會把 1.4443329 誤判成 1.444333。
	// 容差若放寬到 1e-9，在 1e6 量級上絕對誤差會達到 1e-3，反而製造新的多下單 bug。
	alignTolerance = 1e-13
	// maxStepDecimals 支援的最大小數位數（覆蓋交易所常見的 1e-8 精度）
	maxStepDecimals = 12
)

// stepDecimals 推算步長的小數位數
func stepDecimals(step float64) int {
	s := strconv.FormatFloat(step, 'f', -1, 64)
	i := strings.IndexByte(s, '.')
	if i < 0 {
		return 0
	}
	if d := len(s) - i - 1; d < maxStepDecimals {
		return d
	}
	return maxStepDecimals
}

// quantize 將數值收斂到指定小數位，消除回乘誤差
func quantize(value float64, decimals int) float64 {
	factor := math.Pow10(decimals)
	return math.Round(value*factor) / factor
}

// nearestIfAligned 若 scaled 已極接近某整數則返回該整數，否則用 fallback 取整。
// 容差按 scaled 自身量級縮放，只吸收 IEEE754 表示誤差。
func nearestIfAligned(scaled float64, fallback func(float64) float64) float64 {
	tolerance := math.Max(math.Abs(scaled), 1) * alignTolerance
	if nearest := math.Round(scaled); math.Abs(scaled-nearest) < tolerance {
		return nearest
	}
	return fallback(scaled)
}

// alignToStep 將 value 對齊到 step 的整數倍，取整方向由 fallback 決定
func alignToStep(value, step float64, fallback func(float64) float64) float64 {
	if step <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	ratio := nearestIfAligned(value/step, fallback)
	return quantize(ratio*step, stepDecimals(step))
}

// FloorToStep 向下對齊到 step 的整數倍。
// 用於下單數量：寧可少下一點，也不能超出餘額/持倉。
func FloorToStep(value, step float64) float64 {
	return alignToStep(value, step, math.Floor)
}

// CeilToStep 向上對齊到 step 的整數倍
func CeilToStep(value, step float64) float64 {
	return alignToStep(value, step, math.Ceil)
}

// RoundToStep 就近對齊到 step 的整數倍
func RoundToStep(value, step float64) float64 {
	return alignToStep(value, step, math.Round)
}

// clampDecimals 將小數位數限制在合法範圍
func clampDecimals(decimals int) int {
	if decimals < 0 {
		return 0
	}
	if decimals > maxStepDecimals {
		return maxStepDecimals
	}
	return decimals
}

// FloorToDecimals 向下取整到指定小數位。
// 用於下單數量：寧可少下一點，也不能超出餘額/持倉。
func FloorToDecimals(value float64, decimals int) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	decimals = clampDecimals(decimals)
	factor := math.Pow10(decimals)
	return nearestIfAligned(value*factor, math.Floor) / factor
}

// RoundToDecimals 就近取整到指定小數位。
// 用於價格等不受餘額約束的場景；數量請用 FloorToDecimals。
func RoundToDecimals(value float64, decimals int) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return value
	}
	decimals = clampDecimals(decimals)
	factor := math.Pow10(decimals)
	return math.Round(value*factor) / factor
}
