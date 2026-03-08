package backtest

import (
	"fmt"
)

// ExchangeStyleMetrics 交易所风格的盈亏指标（基于平均持仓成本）
type ExchangeStyleMetrics struct {
	// 基于平均成本的盈亏
	AverageCostPnL        float64 `json:"average_cost_pnl"`         // 基于平均成本的未实现盈亏
	AverageCostRealizedPnL float64 `json:"average_cost_realized_pnl"` // 基于平均成本的已实现盈亏
	AverageCost           float64 `json:"average_cost"`              // 平均持仓成本
	CurrentPosition       float64 `json:"current_position"`          // 当前持仓量
	CurrentPositionValue  float64 `json:"current_position_value"`    // 当前持仓价值（市价）

	// 交易所风格的胜率
	ExchangeWinRate       float64 `json:"exchange_win_rate"`       // 交易所风格胜率（基于整体盈亏）
	ExchangeTotalProfit   float64 `json:"exchange_total_profit"`   // 总盈利次数
	ExchangeTotalLoss     float64 `json:"exchange_total_loss"`     // 总亏损次数
	ExchangeProfitFactor  float64 `json:"exchange_profit_factor"`  // 交易所风格盈亏比
}

// CalculateExchangeStyleMetrics 计算交易所风格的指标
// 这种计算方式模拟交易所的盈亏计算：
// - 计算所有买入的平均成本
// - 基于当前市价计算未实现盈亏
// - 基于卖出时的平均成本计算已实现盈亏
func CalculateExchangeStyleMetrics(trades []Trade, currentPrice float64) ExchangeStyleMetrics {
	if len(trades) == 0 {
		return ExchangeStyleMetrics{}
	}

	// 计算平均持仓成本和当前持仓
	var totalBuyCost float64    // 总买入成本（含手续费）
	var totalBuyQty float64     // 总买入数量
	var totalSellValue float64  // 总卖出价值（含手续费）
	var totalSellQty float64    // 总卖出数量

	for _, trade := range trades {
		if trade.Type == "buy" {
			totalBuyCost += trade.Price*trade.Quantity + trade.Fee
			totalBuyQty += trade.Quantity
		} else if trade.Type == "sell" {
			totalSellValue += trade.Price*trade.Quantity - trade.Fee
			totalSellQty += trade.Quantity
		}
	}

	// 当前持仓 = 总买入 - 总卖出
	currentPosition := totalBuyQty - totalSellQty
	if currentPosition < 0 {
		// 卖空的情况暂不考虑
		currentPosition = 0
	}

	// 平均成本 = 总买入成本 / 总买入数量
	averageCost := 0.0
	if totalBuyQty > 0 {
		averageCost = totalBuyCost / totalBuyQty
	}

	// 当前持仓价值 = 当前持仓量 * 当前价格
	currentPositionValue := currentPosition * currentPrice

	// 未实现盈亏 = (当前价格 - 平均成本) * 当前持仓量
	unrealizedPnL := (currentPrice - averageCost) * currentPosition

	// 已实现盈亏 = 总卖出价值 - (平均成本 * 总卖出量)
	// 注意：这里使用平均成本来计算，简化处理
	// 更精确的方式应该追踪每笔卖出的成本基础
	realizedPnL := 0.0
	if totalSellQty > 0 {
		realizedPnL = totalSellValue - (averageCost * totalSellQty)
	}

	// 计算交易所风格的胜率
	// 这里我们定义为：有持仓的期间，价格上涨则为盈利期间
	// 由于回测是连续的，我们用另一种方式：
	// 统计所有卖出交易中，卖出价高于买入成本的比例
	winCount := 0
	lossCount := 0
	totalProfit := 0.0
	totalLoss := 0.0

	// 遍历所有卖出交易，检查是否盈利
	// 注意：这是简化版本，应该追踪每笔卖出的实际成本基础
	for _, trade := range trades {
		if trade.Type == "sell" {
			// 使用 trade.PnL 来判断（这是基于网格的计算）
			if trade.PnL > 0 {
				winCount++
				totalProfit += trade.PnL
			} else if trade.PnL < 0 {
				lossCount++
				totalLoss += -trade.PnL
			}
		}
	}

	winRate := 0.0
	if winCount+lossCount > 0 {
		winRate = float64(winCount) / float64(winCount+lossCount) * 100
	}

	profitFactor := 0.0
	if totalLoss > 0 {
		profitFactor = totalProfit / totalLoss
	}

	return ExchangeStyleMetrics{
		AverageCostPnL:        unrealizedPnL,
		AverageCostRealizedPnL: realizedPnL,
		AverageCost:           averageCost,
		CurrentPosition:       currentPosition,
		CurrentPositionValue:  currentPositionValue,
		ExchangeWinRate:       winRate,
		ExchangeTotalProfit:   float64(winCount),
		ExchangeTotalLoss:     float64(lossCount),
		ExchangeProfitFactor:  profitFactor,
	}
}

// String 返回交易所风格指标的字符串表示
func (m ExchangeStyleMetrics) String() string {
	return fmt.Sprintf(
		"平均成本: %.4f | 当前持仓: %.4f | 未实现盈亏: %.2f | 已实现盈亏: %.2f | 交易所胜率: %.2f%%",
		m.AverageCost, m.CurrentPosition, m.AverageCostPnL, m.AverageCostRealizedPnL, m.ExchangeWinRate,
	)
}
