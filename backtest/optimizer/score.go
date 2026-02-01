package optimizer

import (
	"math"

	"quantmesh/backtest"
)

// CalculateScore 计算目标函数得分
// Score = AnnualizedReturn(%) - λ×MaxDrawdown(%) + 0.2×SharpeRatio
// 目标：最大化 Score
func CalculateScore(metrics backtest.Metrics, lambda float64) float64 {
	cagr := metrics.AnnualizedReturn
	mdd := metrics.MaxDrawdown
	sharpe := metrics.SharpeRatio
	if math.IsNaN(sharpe) || math.IsInf(sharpe, 0) {
		sharpe = 0
	}
	score := cagr - lambda*mdd + 0.2*sharpe
	return score
}
