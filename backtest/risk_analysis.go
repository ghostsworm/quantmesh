package backtest

import (
	"math"
	"sort"
)

// RiskMetrics 风险指标
type RiskMetrics struct {
	VaR95  float64 `json:"var_95"`  // 95% 置信度的风险价值
	VaR99  float64 `json:"var_99"`  // 99% 置信度的风险价值
	CVaR95 float64 `json:"cvar_95"` // 95% 置信度的条件风险价值
	CVaR99 float64 `json:"cvar_99"` // 99% 置信度的条件风险价值
}

// CalculateRiskMetrics 计算风险指标
func CalculateRiskMetrics(equity []EquityPoint) RiskMetrics {
	if len(equity) < 2 {
		return RiskMetrics{}
	}

	// 计算收益率序列
	returns := make([]float64, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if equity[i-1].Equity > 0 {
			returns[i-1] = (equity[i].Equity - equity[i-1].Equity) / equity[i-1].Equity
		}
	}

	// 计算 VaR
	var95 := calculateHistoricalVaR(returns, 0.95)
	var99 := calculateHistoricalVaR(returns, 0.99)

	// 计算 CVaR
	cvar95 := calculateCVaR(returns, 0.95)
	cvar99 := calculateCVaR(returns, 0.99)

	return RiskMetrics{
		VaR95:  var95 * 100,  // 转换为百分比
		VaR99:  var99 * 100,
		CVaR95: cvar95 * 100,
		CVaR99: cvar99 * 100,
	}
}

// calculateHistoricalVaR 历史模拟法计算 VaR
func calculateHistoricalVaR(returns []float64, confidence float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	// 排序收益率
	sorted := make([]float64, len(returns))
	copy(sorted, returns)
	sort.Float64s(sorted)

	// 找到对应百分位数
	index := int(float64(len(sorted)) * (1 - confidence))
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	if index < 0 {
		index = 0
	}

	return math.Abs(sorted[index]) // VaR 是正数，表示损失
}

// calculateCVaR 计算条件风险价值（CVaR / Expected Shortfall）
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

	// 计算超过 VaR 的平均损失
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

