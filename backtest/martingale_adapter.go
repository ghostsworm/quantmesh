package backtest

import (
	"context"
	"fmt"
	"math"
	"time"

	"quantmesh/exchange"
)

// MartingaleBacktestParams 马丁格尔回测参數
type MartingaleBacktestParams struct {
	BaseAmount    float64 // 基础下單金額 USDT
	Multiplier    float64 // 亏损后加倍倍數
	TotalCapital  float64
	FeeRate       float64
	SlippageRatio float64
	TakeProfitPct float64 // 止盈百分比，如 1 表示 1%
	StopLossPct   float64 // 止损百分比，如 2 表示 2%
}

// RunMartingaleBacktest 運行马丁格尔回测（简化版：定期“下注”，亏损加倍，總資金限制）
func RunMartingaleBacktest(symbol, interval string, candles []*exchange.Candle, params MartingaleBacktestParams, initialCapital float64) (*BacktestResult, error) {
	return RunMartingaleBacktestContext(context.Background(), symbol, interval, candles, params, initialCapital)
}

func RunMartingaleBacktestContext(ctx context.Context, symbol, interval string, candles []*exchange.Candle, params MartingaleBacktestParams, initialCapital float64) (result *BacktestResult, resultErr error) {
	defer func() {
		if err := ctx.Err(); err != nil {
			result, resultErr = nil, err
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(candles) == 0 {
		return nil, fmt.Errorf("candles is empty")
	}
	if params.BaseAmount <= 0 || params.TotalCapital <= 0 ||
		math.IsNaN(params.SlippageRatio) || math.IsInf(params.SlippageRatio, 0) || params.SlippageRatio < 0 || params.SlippageRatio >= 1 {
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
	totalSlippageLoss := 0.0
	feeRate := params.FeeRate
	if feeRate <= 0 {
		feeRate = 0.0004
	}
	nextBet := params.BaseAmount
	consecutiveLosses := 0

	for i, c := range candles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// 有持倉：檢查止盈止损
		if position > 0 && entryPrice > 0 {
			ret := (c.Close - entryPrice) / entryPrice
			if ret >= tp {
				// 止盈
				qty := position
				position = 0
				price := c.Close * (1 - params.SlippageRatio)
				fee := qty * price * feeRate
				pnl := (price-entryPrice)*qty - fee
				cash += qty*price - fee
				totalSlippageLoss += (c.Close - price) * qty
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
				equity = append(equity, EquityPoint{Timestamp: c.Timestamp, Equity: cash})
				continue
			}
			if ret <= -sl {
				// 止损
				qty := position
				position = 0
				price := c.Close * (1 - params.SlippageRatio)
				fee := qty * price * feeRate
				pnl := (price-entryPrice)*qty - fee
				cash += qty*price - fee
				totalSlippageLoss += (c.Close - price) * qty
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
				equity = append(equity, EquityPoint{Timestamp: c.Timestamp, Equity: cash})
				continue
			}
		}

		// 無持倉：每隔一定 K 線數買入（简化：每 24 根 1h 買一次，即每天一次）
		cpd := candlesPerDay(interval)
		if cpd <= 0 {
			cpd = 24
		}
		if position > 0 {
			equity = append(equity, EquityPoint{Timestamp: c.Timestamp, Equity: cash + position*c.Close})
			continue
		}
		if i%cpd != 0 && i > 0 {
			equity = append(equity, EquityPoint{Timestamp: c.Timestamp, Equity: cash})
			continue
		}

		amount := nextBet
		if amount > cash {
			amount = cash
		}
		if amount < 1e-6 {
			equity = append(equity, EquityPoint{Timestamp: c.Timestamp, Equity: cash})
			continue
		}

		price := c.Close * (1 + params.SlippageRatio)
		qty := amount / (price * (1 + feeRate))
		fee := qty * price * feeRate
		totalCost := qty*price + fee
		if qty > 0 && totalCost <= cash+1e-9 {
			cash -= totalCost
			position = qty
			entryPrice = price
			totalSlippageLoss += (price - c.Close) * qty
			trades = append(trades, Trade{
				Timestamp: c.Timestamp,
				Type:      "buy",
				Price:     price,
				Quantity:  qty,
				Fee:       fee,
				PnL:       0,
			})
		}
		equity = append(equity, EquityPoint{Timestamp: c.Timestamp, Equity: cash + position*c.Close})
	}

	// 期末若仍有持倉，按最后價平倉
	if position > 0 && len(candles) > 0 {
		last := candles[len(candles)-1]
		price := last.Close * (1 - params.SlippageRatio)
		fee := position * price * feeRate
		pnl := (price-entryPrice)*position - fee
		cash += position*price - fee
		totalSlippageLoss += (last.Close - price) * position
		trades = append(trades, Trade{
			Timestamp: last.Timestamp,
			Type:      "sell",
			Price:     price,
			Quantity:  position,
			Fee:       fee,
			PnL:       pnl,
		})
		position = 0
		if len(equity) > 0 {
			equity[len(equity)-1].Equity = cash
		}
	}

	finalEquity := cash
	metrics := CalculateMetrics(equity, trades, initialCapital, totalSlippageLoss)
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
		PriceCurve:     ComputePriceCurveSummary(candles),
	}, nil
}
