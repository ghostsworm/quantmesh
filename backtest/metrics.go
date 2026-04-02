package backtest

import (
	"math"
	"sort"
)

// Metrics 回测指標
type Metrics struct {
	// 收益指標
	TotalReturn      float64 `json:"total_return"`      // 總收益率 (%)
	AnnualizedReturn float64 `json:"annualized_return"` // 年化收益率 (%)

	// 风險指標
	MaxDrawdown         float64 `json:"max_drawdown"`          // 最大回撤 (%)
	MaxDrawdownDuration int     `json:"max_drawdown_duration"` // 最大回撤持续時间（天）
	Volatility          float64 `json:"volatility"`            // 波动率 (%)

	// 风險調整收益
	SharpeRatio  float64 `json:"sharpe_ratio"`  // 夏普比率
	SortinoRatio float64 `json:"sortino_ratio"` // 索提诺比率
	CalmarRatio  float64 `json:"calmar_ratio"`  // 卡玛比率

	// 交易指標
	TotalTrades  int     `json:"total_trades"`  // 總交易次數（成對筆數）
	BuyCount     int     `json:"buy_count"`     // 買入次數
	SellCount    int     `json:"sell_count"`    // 賣出次數
	WinRate      float64 `json:"win_rate"`      // 胜率 (%)
	ProfitFactor float64 `json:"profit_factor"` // 利润因子
	AvgWin       float64 `json:"avg_win"`       // 平均盈利
	AvgLoss      float64 `json:"avg_loss"`      // 平均亏损
	LargestWin   float64 `json:"largest_win"`   // 最大單笔盈利
	LargestLoss  float64 `json:"largest_loss"`  // 最大單笔亏损

	// 连续性指標
	MaxConsecutiveWins   int `json:"max_consecutive_wins"`   // 最大连续盈利次數
	MaxConsecutiveLosses int `json:"max_consecutive_losses"` // 最大连续亏损次數

	// 持倉（基幣數量，如 BTCUSDT 即最大持倉 BTC 數量）
	MaxPosition float64 `json:"max_position"` // 最大持倉（基幣）

	// 🔥 價格偏差（slippage）損失
	TotalSlippageLoss float64 `json:"total_slippage_loss"` // 累计slippage損失（USDT）

	// 交易所风格指標（基于平均持仓成本）
	ExchangeStyle ExchangeStyleMetrics `json:"exchange_style"` // 交易所风格指标
}

// CalculateMetrics 計算所有指標
func CalculateMetrics(equity []EquityPoint, trades []Trade, initialCapital float64, totalSlippageLoss float64) Metrics {
	return CalculateMetricsWithPrice(equity, trades, initialCapital, totalSlippageLoss, 0)
}

// CalculateMetricsWithPrice 計算所有指標（包含當前價格用於交易所風格指標）
func CalculateMetricsWithPrice(equity []EquityPoint, trades []Trade, initialCapital float64, totalSlippageLoss float64, currentPrice float64) Metrics {
	if len(equity) < 2 {
		return Metrics{
			TotalSlippageLoss: totalSlippageLoss,
		}
	}

	returns := calculateReturns(equity)
	periodsPerYear := inferPeriodsPerYear(equity)

	metrics := Metrics{
		// 收益指標
		TotalReturn:      calculateTotalReturn(equity, initialCapital),
		AnnualizedReturn: calculateAnnualizedReturn(equity, initialCapital),

		// 风險指標
		MaxDrawdown:         calculateMaxDrawdown(equity),
		MaxDrawdownDuration: calculateMaxDrawdownDuration(equity),
		Volatility:          calculateVolatility(returns, periodsPerYear),

		// 风險調整收益
		SharpeRatio:  calculateSharpeRatio(returns, periodsPerYear),
		SortinoRatio: calculateSortinoRatio(returns, periodsPerYear),
		CalmarRatio:  calculateCalmarRatio(equity, initialCapital),

		// 🔥 價格偏差損失
		TotalSlippageLoss: totalSlippageLoss,
	}

	if len(trades) > 0 {
		metrics.TotalTrades = calculateTotalTrades(trades)
		metrics.BuyCount = calculateBuyCount(trades)
		metrics.SellCount = calculateSellCount(trades)
		metrics.WinRate = calculateWinRate(trades)
		metrics.ProfitFactor = calculateProfitFactor(trades)
		metrics.AvgWin = calculateAvgWin(trades)
		metrics.AvgLoss = calculateAvgLoss(trades)
		metrics.LargestWin = calculateLargestWin(trades)
		metrics.LargestLoss = calculateLargestLoss(trades)
		metrics.MaxConsecutiveWins = calculateMaxConsecutiveWins(trades)
		metrics.MaxConsecutiveLosses = calculateMaxConsecutiveLosses(trades)
	}

	// 计算交易所风格指标
	metrics.ExchangeStyle = CalculateExchangeStyleMetrics(trades, currentPrice)

	return metrics
}

// inferPeriodsPerYear 根據權益曲線相鄰點時間間隔的中位數，推斷每年有多少個「收益期」，
// 用於波動率與夏普/索提諾的年化。無法推斷時回退為 252（等價於日頻假設）。
func inferPeriodsPerYear(equity []EquityPoint) float64 {
	const (
		defaultPeriods = 252.0
		msPerYear      = 365.25 * 86400 * 1000
		minPeriods     = 4.0    // 至多季線級採樣
		maxPeriods     = 1.0e7 // 極細採樣上界，避免數值爆炸
	)
	if len(equity) < 2 {
		return defaultPeriods
	}
	var gaps []float64
	for i := 1; i < len(equity); i++ {
		d := float64(equity[i].Timestamp - equity[i-1].Timestamp)
		if d > 0 {
			gaps = append(gaps, d)
		}
	}
	if len(gaps) == 0 {
		return defaultPeriods
	}
	sort.Float64s(gaps)
	mid := len(gaps) / 2
	med := gaps[mid]
	if len(gaps)%2 == 0 {
		med = (gaps[mid-1] + gaps[mid]) / 2
	}
	if med <= 0 {
		return defaultPeriods
	}
	ppy := msPerYear / med
	if ppy < minPeriods {
		ppy = minPeriods
	}
	if ppy > maxPeriods {
		ppy = maxPeriods
	}
	return ppy
}

// calculateReturns 計算收益率序列
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

// calculateTotalReturn 計算總收益率
func calculateTotalReturn(equity []EquityPoint, initialCapital float64) float64 {
	if len(equity) == 0 || initialCapital == 0 {
		return 0
	}

	finalEquity := equity[len(equity)-1].Equity
	return (finalEquity - initialCapital) / initialCapital * 100
}

// calculateAnnualizedReturn 計算年化收益率（複利換算，返回值為百分比，與 TotalReturn 一致）
// 公式：(1 + 總收益率/100)^(365/天數) - 1，再乘 100 得到百分比
func calculateAnnualizedReturn(equity []EquityPoint, initialCapital float64) float64 {
	if len(equity) < 2 || initialCapital == 0 {
		return 0
	}

	startTime := equity[0].Timestamp
	endTime := equity[len(equity)-1].Timestamp
	days := float64(endTime-startTime) / (1000 * 86400)

	if days <= 0 {
		return 0
	}

	totalReturn := calculateTotalReturn(equity, initialCapital)
	// 複利年化後轉為百分比，便於報表與 TotalReturn 同為「%」顯示
	return (math.Pow(1+totalReturn/100, 365/days) - 1) * 100
}

// calculateMaxDrawdown 計算最大回撤
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

// calculateMaxDrawdownDuration 計算最大回撤持续時间（天）
// 使用實際時間差（毫秒 -> 天），避免按「數據點個數」統計導致 1m 回測顯示成千上萬天
func calculateMaxDrawdownDuration(equity []EquityPoint) int {
	if len(equity) == 0 {
		return 0
	}

	const msPerDay = 86400 * 1000
	maxDurationDays := 0
	drawdownStartMs := int64(0)
	peak := equity[0].Equity
	inDrawdown := false

	for _, point := range equity {
		if point.Equity > peak {
			peak = point.Equity
			if inDrawdown {
				days := int((point.Timestamp - drawdownStartMs) / msPerDay)
				if days > maxDurationDays {
					maxDurationDays = days
				}
				inDrawdown = false
			}
		} else if point.Equity < peak {
			if !inDrawdown {
				drawdownStartMs = point.Timestamp
				inDrawdown = true
			}
		}
	}

	if inDrawdown && len(equity) > 0 {
		days := int((equity[len(equity)-1].Timestamp - drawdownStartMs) / msPerDay)
		if days > maxDurationDays {
			maxDurationDays = days
		}
	}

	return maxDurationDays
}

// calculateVolatility 計算波动率（年化）；periodsPerYear 為每年收益期數（由權益曲線採樣間隔推斷）
func calculateVolatility(returns []float64, periodsPerYear float64) float64 {
	if len(returns) == 0 || periodsPerYear <= 0 {
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

	return math.Sqrt(variance) * math.Sqrt(periodsPerYear) * 100
}

// calculateSharpeRatio 計算夏普比率（年化無風險利率 2%，按 periodsPerYear 分攤到每期）
func calculateSharpeRatio(returns []float64, periodsPerYear float64) float64 {
	if len(returns) == 0 || periodsPerYear <= 0 {
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

	riskFreePerPeriod := 0.02 / periodsPerYear
	return (mean - riskFreePerPeriod) / stdDev * math.Sqrt(periodsPerYear)
}

// calculateSortinoRatio 計算索提诺比率（只考虑下行波动）
func calculateSortinoRatio(returns []float64, periodsPerYear float64) float64 {
	if len(returns) == 0 || periodsPerYear <= 0 {
		return 0
	}

	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))

	// 只計算负收益的方差
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

	riskFreePerPeriod := 0.02 / periodsPerYear
	return (mean - riskFreePerPeriod) / downStdDev * math.Sqrt(periodsPerYear)
}

// calculateCalmarRatio 計算卡玛比率（年化收益率 / 最大回撤）
func calculateCalmarRatio(equity []EquityPoint, initialCapital float64) float64 {
	annualizedReturn := calculateAnnualizedReturn(equity, initialCapital)
	maxDrawdown := calculateMaxDrawdown(equity)

	if maxDrawdown == 0 {
		return 0
	}

	return annualizedReturn / maxDrawdown
}

// calculateBuyCount 計算買入次數
func calculateBuyCount(trades []Trade) int {
	n := 0
	for _, t := range trades {
		if t.Type == "buy" {
			n++
		}
	}
	return n
}

// calculateSellCount 計算賣出次數
func calculateSellCount(trades []Trade) int {
	n := 0
	for _, t := range trades {
		if t.Type == "sell" {
			n++
		}
	}
	return n
}

// calculateTotalTrades 計算成對交易筆數（買入+賣出算一筆）
func calculateTotalTrades(trades []Trade) int {
	buyCount := calculateBuyCount(trades)
	sellCount := calculateSellCount(trades)
	if buyCount < sellCount {
		return buyCount
	}
	return sellCount
}

// calculateWinRate 計算胜率
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

// calculateProfitFactor 計算利润因子（總盈利 / 總亏损）
// 返回 -1 表示無已平倉交易（指標不適用），0 表示全部虧損無盈利
func calculateProfitFactor(trades []Trade) float64 {
	totalProfit := 0.0
	totalLoss := 0.0
	hasSell := false

	for _, trade := range trades {
		if trade.Type == "sell" {
			hasSell = true
			if trade.PnL > 0 {
				totalProfit += trade.PnL
			} else {
				totalLoss += math.Abs(trade.PnL)
			}
		}
	}

	if !hasSell {
		return -1
	}

	if totalLoss == 0 {
		if totalProfit > 0 {
			return math.Inf(1)
		}
		return 0
	}

	return totalProfit / totalLoss
}

// calculateAvgWin 計算平均盈利
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

// calculateAvgLoss 計算平均亏损
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

// calculateLargestWin 計算最大單笔盈利
func calculateLargestWin(trades []Trade) float64 {
	largestWin := 0.0

	for _, trade := range trades {
		if trade.Type == "sell" && trade.PnL > largestWin {
			largestWin = trade.PnL
		}
	}

	return largestWin
}

// calculateLargestLoss 計算最大單笔亏损
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

// calculateMaxConsecutiveWins 計算最大连续盈利次數
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

// calculateMaxConsecutiveLosses 計算最大连续亏损次數
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
