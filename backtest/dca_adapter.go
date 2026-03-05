package backtest

import (
	"fmt"
	"time"

	"quantmesh/exchange"
)

// DCABacktestParams DCA 回测参數
type DCABacktestParams struct {
	IntervalDays   int     // 定投间隔（天）
	AmountPerTrade float64 // 每次投入金額 USDT
	TotalCapital   float64
	FeeRate        float64
}

// candlesPerDay 按 K 線周期返回每天根數（近似）
func candlesPerDay(interval string) int {
	switch interval {
	case "1m":
		return 24 * 60
	case "3m":
		return 24 * 20
	case "5m":
		return 24 * 12
	case "15m":
		return 24 * 4
	case "30m":
		return 24 * 2
	case "1h":
		return 24
	case "4h":
		return 6
	case "1d":
		return 1
	default:
		return 24 // 預設按 1h
	}
}

// RunDCABacktest 運行 DCA 定投策略回测：每隔 N 天買入固定金額，持有至結束
func RunDCABacktest(symbol, interval string, candles []*exchange.Candle, params DCABacktestParams, initialCapital float64) (*BacktestResult, error) {
	if len(candles) == 0 {
		return nil, fmt.Errorf("candles is empty")
	}
	if params.AmountPerTrade <= 0 || params.TotalCapital <= 0 || params.IntervalDays <= 0 {
		return nil, fmt.Errorf("invalid DCA params")
	}

	cpd := candlesPerDay(interval)
	candlesBetweenBuys := params.IntervalDays * cpd
	if candlesBetweenBuys <= 0 {
		candlesBetweenBuys = 1
	}

	cash := initialCapital
	var position float64
	var trades []Trade
	var equity []EquityPoint
	feeRate := params.FeeRate
	if feeRate <= 0 {
		feeRate = 0.0004
	}
	spent := 0.0

	for i, c := range candles {
		equity = append(equity, EquityPoint{
			Timestamp: c.Timestamp,
			Equity:    cash + position*c.Close,
		})

		if spent >= params.TotalCapital {
			continue
		}
		if i%candlesBetweenBuys != 0 && i > 0 {
			continue
		}

		amount := params.AmountPerTrade
		if amount > cash {
			amount = cash
		}
		if amount+spent > params.TotalCapital {
			amount = params.TotalCapital - spent
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
			totalCost = qty*price + fee
		}
		cash -= totalCost
		position += qty
		spent += totalCost
		trades = append(trades, Trade{
			Timestamp: c.Timestamp,
			Type:      "buy",
			Price:     price,
			Quantity:  qty,
			Fee:       fee,
			PnL:       0,
		})
	}

	finalEquity := cash + position*candles[len(candles)-1].Close
	// DCA策略未使用slippage，设为0
	metrics := CalculateMetrics(equity, trades, initialCapital, 0)
	riskMetrics := CalculateRiskMetrics(equity)

	return &BacktestResult{
		Symbol:         symbol,
		Strategy:       "dca",
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
