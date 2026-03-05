package backtest

import (
	"math"
	"sort"
)

// RiskMetrics 风險指標
type RiskMetrics struct {
	VaR95  float64 `json:"var_95"`  // 95% 置信度的风險價值
	VaR99  float64 `json:"var_99"`  // 99% 置信度的风險價值
	CVaR95 float64 `json:"cvar_95"` // 95% 置信度的条件风險價值
	CVaR99 float64 `json:"cvar_99"` // 99% 置信度的条件风險價值
}

// CalculateRiskMetrics 計算风險指標
func CalculateRiskMetrics(equity []EquityPoint) RiskMetrics {
	if len(equity) < 2 {
		return RiskMetrics{}
	}

	// 計算收益率序列
	returns := make([]float64, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if equity[i-1].Equity > 0 {
			returns[i-1] = (equity[i].Equity - equity[i-1].Equity) / equity[i-1].Equity
		}
	}

	// 計算 VaR
	var95 := calculateHistoricalVaR(returns, 0.95)
	var99 := calculateHistoricalVaR(returns, 0.99)

	// 計算 CVaR
	cvar95 := calculateCVaR(returns, 0.95)
	cvar99 := calculateCVaR(returns, 0.99)

	return RiskMetrics{
		VaR95:  var95 * 100, // 轉换為百分比
		VaR99:  var99 * 100,
		CVaR95: cvar95 * 100,
		CVaR99: cvar99 * 100,
	}
}

// calculateHistoricalVaR 歷史模拟法計算 VaR
func calculateHistoricalVaR(returns []float64, confidence float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	// 排序收益率
	sorted := make([]float64, len(returns))
	copy(sorted, returns)
	sort.Float64s(sorted)

	// 找到對应百分位數
	index := int(float64(len(sorted)) * (1 - confidence))
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	if index < 0 {
		index = 0
	}

	return math.Abs(sorted[index]) // VaR 是正數，表示損失
}

// calculateCVaR 計算条件风險價值（CVaR / Expected Shortfall）
func calculateCVaR(returns []float64, confidence float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	sorted := make([]float64, len(returns))
	copy(sorted, returns)
	sort.Float64s(sorted)

	// 找到 VaR 阈值
	index := int(float64(len(sorted)) * (1 - confidence))
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	if index < 0 {
		return 0
	}

	// 計算超過 VaR 的平均損失
	sum := 0.0
	count := 0
	for i := 0; i <= index; i++ {
		sum += sorted[i]
		count++
	}

	if count == 0 {
		return 0
	}

	return math.Abs(sum / float64(count))
}
