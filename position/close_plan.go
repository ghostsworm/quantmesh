package position

import (
	"fmt"
	"math"
	"quantmesh/config"
	"quantmesh/utils"
)

// ClosePlan 平仓计划：方向 + 数量
type ClosePlan struct {
	Side     string  // BUY（平空）/ SELL（平多）
	Quantity float64 // 平仓数量（已按数量精度向下取整）
}

// PlanCloseOrder 根据 Bot 自身净持仓（多为正、空为负）计算平仓方向与数量
//   - exchangeNetQty/hasExchange：交易所该交易对的净持仓（可选），用于封顶，避免 reduceOnly 超量被拒
//   - ratio：平仓比例 0~1，0 或 1 表示全仓
//   - quantityDecimals：数量精度，向下取整，避免超过实际持仓
func PlanCloseOrder(botNetQty, exchangeNetQty float64, hasExchange bool, ratio float64, quantityDecimals int) (*ClosePlan, error) {
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
		return nil, fmt.Errorf("平仓比例无效: %.4f（应在 0~1 之间）", ratio)
	}
	if math.IsNaN(botNetQty) || math.IsInf(botNetQty, 0) || hasExchange && (math.IsNaN(exchangeNetQty) || math.IsInf(exchangeNetQty, 0)) {
		return nil, fmt.Errorf("non-finite position quantity")
	}
	if ratio == 0 {
		ratio = 1
	}
	if math.Abs(botNetQty) < closeQtyEpsilon {
		return nil, fmt.Errorf("没有可平的持仓")
	}

	side := "SELL"
	if botNetQty < 0 {
		side = "BUY"
	}
	qty := math.Abs(botNetQty)

	if hasExchange {
		if math.Abs(exchangeNetQty) < closeQtyEpsilon || (exchangeNetQty > 0) != (botNetQty > 0) {
			return nil, fmt.Errorf("交易所持仓与本地方向不一致: 本地=%.8f 交易所=%.8f", botNetQty, exchangeNetQty)
		}
		qty = math.Min(qty, math.Abs(exchangeNetQty))
	}

	qty *= ratio
	if quantityDecimals >= 0 {
		qty = utils.FloorToDecimals(qty, quantityDecimals)
	}
	if qty <= 0 {
		return nil, fmt.Errorf("平仓数量按精度取整后为 0（持仓=%.8f, 比例=%.4f, 精度=%d）", math.Abs(botNetQty), ratio, quantityDecimals)
	}
	return &ClosePlan{Side: side, Quantity: qty}, nil
}

// fallbackClosePriceDecimals 无法获取交易所价格精度时的兜底值
const fallbackClosePriceDecimals = 2

// closeQtyEpsilon 持仓数量视为 0 的阈值
const closeQtyEpsilon = 1e-12

// calculateLimitPrice 计算限价
func calculateLimitPrice(currentPrice float64, side string, offsetPercent float64) float64 {
	// offsetPercent: 负数=更激进（卖低价/买高价），正数=更保守
	if side == "SELL" {
		return currentPrice * (1 + offsetPercent/100)
	}
	return currentPrice * (1 - offsetPercent/100)
}

func validateCloseRequest(side string, qty float64, cfg config.ClosePositionConfig) error {
	if side != "SELL" && side != "BUY" || !positiveFinite(qty) {
		return fmt.Errorf("invalid close side or quantity")
	}
	if cfg.Method != "limit" && cfg.Method != "market" {
		return fmt.Errorf("invalid close method")
	}
	if cfg.TimeoutSec < 0 || cfg.TimeoutSec > 86400 || cfg.MaxRetries < 0 || math.IsNaN(cfg.PriceOffset) || math.IsInf(cfg.PriceOffset, 0) {
		return fmt.Errorf("invalid close timeout, retries or price offset")
	}
	return nil
}
