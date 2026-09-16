package optimizer

import (
	"math"

	"quantmesh/backtest"
)

const (
	// SharpeWeight 夏普比率項權重
	SharpeWeight = 0.2
	// GridNetProfitFeeWeight 「每格淨利 / 手續費」項權重：懲罰利差被手續費吃光的小間隔高頻參數
	GridNetProfitFeeWeight = 1.0
	// gridNetProfitFeeRatioCap 「每格淨利 / 手續費」截斷上下限，避免手續費極小時該項主導得分
	gridNetProfitFeeRatioCap = 10.0
)

// CalculateScore 计算目標函數得分
// Score = AnnualizedReturn(%) - λ×MaxDrawdown(%) + SharpeWeight×SharpeRatio + GridNetProfitFeeWeight×clamp(GridNetProfitToFeeRatio, ±cap)
// 目標：最大化 Score
func CalculateScore(metrics backtest.Metrics, lambda float64) float64 {
	cagr := metrics.AnnualizedReturn
	mdd := metrics.MaxDrawdown
	sharpe := metrics.SharpeRatio
	if math.IsNaN(sharpe) || math.IsInf(sharpe, 0) {
		sharpe = 0
	}
	score := cagr - lambda*mdd + SharpeWeight*sharpe + GridNetProfitFeeWeight*feeEfficiencyTerm(metrics)
	return score
}

// feeEfficiencyTerm 截斷後的「每格淨利 / 手續費」；無手續費或非有限值時為 0
func feeEfficiencyTerm(metrics backtest.Metrics) float64 {
	r := metrics.GridNetProfitToFeeRatio
	if metrics.TotalFees <= 0 || math.IsNaN(r) || math.IsInf(r, 0) {
		return 0
	}
	return math.Max(-gridNetProfitFeeRatioCap, math.Min(gridNetProfitFeeRatioCap, r))
}
