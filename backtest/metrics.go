package backtest

import (
	"math"
)

// Metrics 回测指标
type Metrics struct {
	// 收益指标
	TotalReturn      float64 `json:"total_return"`      // 总收益率 (%)
	AnnualizedReturn float64 `json:"annualized_return"` // 年化收益率 (%)

	// 风险指标
	MaxDrawdown         float64 `json:"max_drawdown"`          // 最大回撤 (%)
	MaxDrawdownDuration int     `json:"max_drawdown_duration"` // 最大回撤持续时间（天）
	Volatility          float64 `json:"volatility"`            // 波动率 (%)

	// 风险调整收益
	SharpeRatio  float64 `json:"sharpe_ratio"`  // 夏普比率
	SortinoRatio float64 `json:"sortino_ratio"` // 索提诺比率
	CalmarRatio  float64 `json:"calmar_ratio"`  // 卡玛比率

	// 交易指标
	TotalTrades  int     `json:"total_trades"`  // 总交易次数
	WinRate      float64 `json:"win_rate"`      // 胜率 (%)
	ProfitFactor float64 `json:"profit_factor"` // 利润因子
	AvgWin       float64 `json:"avg_win"`       // 平均盈利
	AvgLoss      float64 `json:"avg_loss"`      // 平均亏损
	LargestWin   float64 `json:"largest_win"`   // 最大单笔盈利
	LargestLoss  float64 `json:"largest_loss"`  // 最大单笔亏损

	// 连续性指标
	MaxConsecutiveWins   int `json:"max_consecutive_wins"`   // 最大连续盈利次数
	MaxConsecutiveLosses int `json:"max_consecutive_losses"` // 最大连续亏损次数
}

// CalculateMetrics 计算所有指标
func CalculateMetrics(equity []EquityPoint, trades []Trade, initialCapital float64) Metrics {
	if len(equity) == 0 || len(trades) == 0 {
		return Metrics{}
	}

	returns := calculateReturns(equity)

	metrics := Metrics{
		// 收益指标
		TotalReturn:      calculateTotalReturn(equity, initialCapital),
		AnnualizedReturn: calculateAnnualizedReturn(equity, initialCapital),

		// 风险指标
		MaxDrawdown:         calculateMaxDrawdown(equity),
		MaxDrawdownDuration: calculateMaxDrawdownDuration(equity),
		Volatility:          calculateVolatility(returns),

		// 风险调整收益
		SharpeRatio:  calculateSharpeRatio(returns),
		SortinoRatio: calculateSortinoRatio(returns),
		CalmarRatio:  calculateCalmarRatio(equity, initialCapital),

		// 交易指标
		TotalTrades:  len(trades) / 2, // 买入+卖出算一笔完整交易
		WinRate:      calculateWinRate(trades),
		ProfitFactor: calculateProfitFactor(trades),
		AvgWin:       calculateAvgWin(trades),
		AvgLoss:      calculateAvgLoss(trades),
		LargestWin:   calculateLargestWin(trades),
		LargestLoss:  calculateLargestLoss(trades),

		// 连续性指标
		MaxConsecutiveWins:   calculateMaxConsecutiveWins(trades),
		MaxConsecutiveLosses: calculateMaxConsecutiveLosses(trades),
	}

	return metrics
}

// calculateReturns 计算收益率序列
func calculateReturns(equity []EquityPoint) []float64 {
	if len(equity) < 2 {
		return []float64{}
	}

	returns := make([]float64, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if equity[i-1].Equity > 0 {
			returns[i-1] = (equity[i].Equity - equity[i-1].Equity) / equity[i-1].Equity
		}
	}

	return returns
}

// calculateTotalReturn 计算总收益率
func calculateTotalReturn(equity []EquityPoint, initialCapital float64) float64 {
	if len(equity) == 0 || initialCapital == 0 {
		return 0
	}

	finalEquity := equity[len(equity)-1].Equity
	return (finalEquity - initialCapital) / initialCapital * 100
}

// calculateAnnualizedReturn 计算年化收益率
func calculateAnnualizedReturn(equity []EquityPoint, initialCapital float64) float64 {
	if len(equity) < 2 || initialCapital == 0 {
		return 0
	}

	startTime := equity[0].Timestamp
	endTime := equity[len(equity)-1].Timestamp
	days := float64(endTime-startTime) / (1000 * 86400)

	if days == 0 {
		return 0
	}

	totalReturn := calculateTotalReturn(equity, initialCapital)
	return math.Pow(1+totalReturn/100, 365/days) - 1
}

// calculateMaxDrawdown 计算最大回撤
func calculateMaxDrawdown(equity []EquityPoint) float64 {
	if len(equity) == 0 {
		return 0
	}

	maxDrawdown := 0.0
	peak := equity[0].Equity

	for _, point := range equity {
		if point.Equity > peak {
			peak = point.Equity
		}

		if peak > 0 {
			drawdown := (peak - point.Equity) / peak * 100
			if drawdown > maxDrawdown {
				maxDrawdown = drawdown
			}
		}
	}

	return maxDrawdown
}

// calculateMaxDrawdownDuration 计算最大回撤持续时间（天）
func calculateMaxDrawdownDuration(equity []EquityPoint) int {
	if len(equity) == 0 {
		return 0
	}

	maxDuration := 0
	currentDuration := 0
	peak := equity[0].Equity
	inDrawdown := false

	for _, point := range equity {
		if point.Equity > peak {
			peak = point.Equity
			if inDrawdown {
				if currentDuration > maxDuration {
					maxDuration = currentDuration
				}
				currentDuration = 0
				inDrawdown = false
			}
		} else if point.Equity < peak {
			inDrawdown = true
			currentDuration++
		}
	}

	if currentDuration > maxDuration {
		maxDuration = currentDuration
	}

	return maxDuration
}

// calculateVolatility 计算波动率（年化）
func calculateVolatility(returns []float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		diff := r - mean
		variance += diff * diff
	}
	variance /= float64(len(returns))

	// 年化波动率（假设每天一个数据点）
	return math.Sqrt(variance) * math.Sqrt(252) * 100
}

// calculateSharpeRatio 计算夏普比率
func calculateSharpeRatio(returns []float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	variance := 0.0
	for _, r := range returns {
		diff := r - mean
		variance += diff * diff
	}
	variance /= float64(len(returns))
	stdDev := math.Sqrt(variance)

	if stdDev == 0 {
		return 0
	}

	riskFreeRate := 0.02 / 252 // 日化无风险利率（假设年化2%）
	return (mean - riskFreeRate) / stdDev * math.Sqrt(252)
}

// calculateSortinoRatio 计算索提诺比率（只考虑下行波动）
func calculateSortinoRatio(returns []float64) float64 {
	if len(returns) == 0 {
		return 0
	}

	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	// 只计算负收益的方差
	downVariance := 0.0
	downCount := 0
	for _, r := range returns {
		if r < 0 {
			downVariance += r * r
			downCount++
		}
	}

	if downCount == 0 {
		return 0
	}

	downVariance /= float64(downCount)
	downStdDev := math.Sqrt(downVariance)

	if downStdDev == 0 {
		return 0
	}

	riskFreeRate := 0.02 / 252
	return (mean - riskFreeRate) / downStdDev * math.Sqrt(252)
}

// calculateCalmarRatio 计算卡玛比率（年化收益率 / 最大回撤）
func calculateCalmarRatio(equity []EquityPoint, initialCapital float64) float64 {
	annualizedReturn := calculateAnnualizedReturn(equity, initialCapital)
	maxDrawdown := calculateMaxDrawdown(equity)

	if maxDrawdown == 0 {
		return 0
	}

	return annualizedReturn / maxDrawdown
}

// calculateWinRate 计算胜率
func calculateWinRate(trades []Trade) float64 {
	if len(trades) == 0 {
		return 0
	}

	winCount := 0
	totalTrades := 0

	for _, trade := range trades {
		if trade.Type == "sell" {
			totalTrades++
			if trade.PnL > 0 {
				winCount++
			}
		}
	}

	if totalTrades == 0 {
		return 0
	}

	return float64(winCount) / float64(totalTrades) * 100
}

// calculateProfitFactor 计算利润因子（总盈利 / 总亏损）
func calculateProfitFactor(trades []Trade) float64 {
	totalProfit := 0.0
	totalLoss := 0.0

	for _, trade := range trades {
		if trade.Type == "sell" {
			if trade.PnL > 0 {
				totalProfit += trade.PnL
			} else {
				totalLoss += math.Abs(trade.PnL)
			}
		}
	}

	if totalLoss == 0 {
		return 0
	}

	return totalProfit / totalLoss
}

// calculateAvgWin 计算平均盈利
func calculateAvgWin(trades []Trade) float64 {
	totalWin := 0.0
	winCount := 0

	for _, trade := range trades {
		if trade.Type == "sell" && trade.PnL > 0 {
			totalWin += trade.PnL
			winCount++
		}
	}

	if winCount == 0 {
		return 0
	}

	return totalWin / float64(winCount)
}

// calculateAvgLoss 计算平均亏损
func calculateAvgLoss(trades []Trade) float64 {
	totalLoss := 0.0
	lossCount := 0

	for _, trade := range trades {
		if trade.Type == "sell" && trade.PnL < 0 {
			totalLoss += math.Abs(trade.PnL)
			lossCount++
		}
	}

	if lossCount == 0 {
		return 0
	}

	return totalLoss / float64(lossCount)
}

// calculateLargestWin 计算最大单笔盈利
func calculateLargestWin(trades []Trade) float64 {
	largestWin := 0.0

	for _, trade := range trades {
		if trade.Type == "sell" && trade.PnL > largestWin {
			largestWin = trade.PnL
		}
	}

	return largestWin
}

// calculateLargestLoss 计算最大单笔亏损
func calculateLargestLoss(trades []Trade) float64 {
	largestLoss := 0.0

	for _, trade := range trades {
		if trade.Type == "sell" && trade.PnL < 0 {
			loss := math.Abs(trade.PnL)
			if loss > largestLoss {
				largestLoss = loss
			}
		}
	}

	return largestLoss
}

// calculateMaxConsecutiveWins 计算最大连续盈利次数
func calculateMaxConsecutiveWins(trades []Trade) int {
	maxWins := 0
	currentWins := 0

	for _, trade := range trades {
		if trade.Type == "sell" {
			if trade.PnL > 0 {
				currentWins++
				if currentWins > maxWins {
					maxWins = currentWins
				}
			} else {
				currentWins = 0
			}
		}
	}

	return maxWins
}

// calculateMaxConsecutiveLosses 计算最大连续亏损次数
func calculateMaxConsecutiveLosses(trades []Trade) int {
	maxLosses := 0
	currentLosses := 0

	for _, trade := range trades {
		if trade.Type == "sell" {
			if trade.PnL < 0 {
				currentLosses++
				if currentLosses > maxLosses {
					maxLosses = currentLosses
				}
			} else {
				currentLosses = 0
			}
		}
	}

	return maxLosses
}
