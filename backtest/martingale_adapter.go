package backtest

import (
	"fmt"
	"math"
	"time"

	"quantmesh/exchange"
)

// MartingaleBacktestParams 马丁格尔回测参数
type MartingaleBacktestParams struct {
	BaseAmount    float64 // 基础下单金额 USDT
	Multiplier    float64 // 亏损后加倍倍数
	TotalCapital  float64
	FeeRate       float64
	TakeProfitPct float64 // 止盈百分比，如 1 表示 1%
	StopLossPct   float64 // 止损百分比，如 2 表示 2%
}

// RunMartingaleBacktest 运行马丁格尔回测（简化版：定期“下注”，亏损加倍，总资金限制）
func RunMartingaleBacktest(symbol, interval string, candles []*exchange.Candle, params MartingaleBacktestParams, initialCapital float64) (*BacktestResult, error) {
	if len(candles) == 0 {
		return nil, fmt.Errorf("candles is empty")
	}
	if params.BaseAmount <= 0 || params.TotalCapital <= 0 {
		return nil, fmt.Errorf("invalid martingale params")
	}
	tp := params.TakeProfitPct / 100
	if tp <= 0 {
		tp = 0.01
	}
	sl := params.StopLossPct / 100
	if sl <= 0 {
		sl = 0.02
	}
	mult := params.Multiplier
	if mult < 1 {
		mult = 2
	}

	cash := initialCapital
	var position float64
	var entryPrice float64
	var trades []Trade
	var equity []EquityPoint
	feeRate := params.FeeRate
	if feeRate <= 0 {
		feeRate = 0.0004
	}
	nextBet := params.BaseAmount
	consecutiveLosses := 0

	for i, c := range candles {
		equity = append(equity, EquityPoint{
			Timestamp: c.Timestamp,
			Equity:   cash + position*c.Close,
		})

		// 有持仓：检查止盈止损
		if position > 0 && entryPrice > 0 {
			ret := (c.Close - entryPrice) / entryPrice
			if ret >= tp {
				// 止盈
				qty := position
				position = 0
				price := c.Close
				fee := qty * price * feeRate
				pnl := (price-entryPrice)*qty - fee
				cash += qty*price - fee
				trades = append(trades, Trade{
					Timestamp: c.Timestamp,
					Type:      "sell",
					Price:     price,
					Quantity:  qty,
					Fee:       fee,
					PnL:       pnl,
				})
				consecutiveLosses = 0
				nextBet = params.BaseAmount
				entryPrice = 0
				continue
			}
			if ret <= -sl {
				// 止损
				qty := position
				position = 0
				price := c.Close
				fee := qty * price * feeRate
				pnl := (price-entryPrice)*qty - fee
				cash += qty*price - fee
				trades = append(trades, Trade{
					Timestamp: c.Timestamp,
					Type:      "sell",
					Price:     price,
					Quantity:  qty,
					Fee:       fee,
					PnL:       pnl,
				})
				consecutiveLosses++
				nextBet = params.BaseAmount * math.Pow(mult, float64(consecutiveLosses))
				if nextBet > cash*0.95 {
					nextBet = cash * 0.95
				}
				entryPrice = 0
				continue
			}
		}

		// 无持仓：每隔一定 K 线数买入（简化：每 24 根 1h 买一次，即每天一次）
		cpd := candlesPerDay(interval)
		if cpd <= 0 {
			cpd = 24
		}
		if position > 0 {
			continue
		}
		if i%cpd != 0 && i > 0 {
			continue
		}

		amount := nextBet
		if amount > cash {
			amount = cash
		}
		if amount < 1e-6 {
			continue
		}

		price := c.Close
		qty := amount / price
		fee := qty * price * feeRate
		totalCost := qty*price + fee
		if totalCost > cash {
			qty = (cash - fee) / price
			if qty <= 0 {
				continue
			}
			fee = qty * price * feeRate
		}
		cash -= totalCost
		position = qty
		entryPrice = price
		trades = append(trades, Trade{
			Timestamp: c.Timestamp,
			Type:      "buy",
			Price:     price,
			Quantity:  qty,
			Fee:       fee,
			PnL:       0,
		})
	}

	// 期末若仍有持仓，按最后价平仓
	if position > 0 && len(candles) > 0 {
		last := candles[len(candles)-1]
		fee := position * last.Close * feeRate
		pnl := (last.Close-entryPrice)*position - fee
		cash += position*last.Close - fee
		trades = append(trades, Trade{
			Timestamp: last.Timestamp,
			Type:      "sell",
			Price:     last.Close,
			Quantity:  position,
			Fee:       fee,
			PnL:       pnl,
		})
		position = 0
	}

	finalEquity := cash
	metrics := CalculateMetrics(equity, trades, initialCapital)
	riskMetrics := CalculateRiskMetrics(equity)

	return &BacktestResult{
		Symbol:         symbol,
		Strategy:       "martingale",
		StartTime:      time.Unix(candles[0].Timestamp/1000, 0),
		EndTime:        time.Unix(candles[len(candles)-1].Timestamp/1000, 0),
		InitialCapital: initialCapital,
		FinalCapital:   finalEquity,
		Equity:         equity,
		Trades:         trades,
		Metrics:        metrics,
		RiskMetrics:    riskMetrics,
	}, nil
}
