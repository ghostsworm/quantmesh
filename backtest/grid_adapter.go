package backtest

import (
	"fmt"
	"math"
	"sort"
	"time"

	"quantmesh/exchange"
)

// GridBacktestParams 网格回测参数
type GridBacktestParams struct {
	PriceLow       float64 `json:"price_low"`
	PriceHigh      float64 `json:"price_high"`
	GridCount      int     `json:"grid_count"`      // 0 表示按间距推算格子数
	OrderQuantity  float64 `json:"order_quantity"` // 单笔订单 USDT
	TotalCapital   float64 `json:"total_capital"`
	FeeRate        float64 `json:"fee_rate"`
	SlippageRatio  float64 `json:"slippage_ratio"`
}

// RunGridBacktest 运行网格策略回测（独立于 StrategyAdapter，多档位多笔交易）
func RunGridBacktest(symbol string, candles []*exchange.Candle, params GridBacktestParams, initialCapital float64) (*BacktestResult, error) {
	if len(candles) == 0 {
		return nil, fmt.Errorf("candles is empty")
	}
	if params.PriceHigh <= params.PriceLow {
		return nil, fmt.Errorf("price_high must be greater than price_low")
	}
	if params.OrderQuantity <= 0 || params.TotalCapital <= 0 {
		return nil, fmt.Errorf("order_quantity and total_capital must be positive")
	}

	gridLevels := buildGridLevels(params.PriceLow, params.PriceHigh, params.GridCount)
	if len(gridLevels) == 0 {
		return nil, fmt.Errorf("no grid levels")
	}

	cash := initialCapital
	positions := make(map[float64]float64) // price -> quantity
	var trades []Trade
	var equity []EquityPoint

	feeRate := params.FeeRate
	if feeRate <= 0 {
		feeRate = 0.0004
	}
	slippage := params.SlippageRatio
	if slippage <= 0 {
		slippage = 0.0003
	}

	prevClose := candles[0].Close
	for _, c := range candles {
		// 权益 = 现金 + 各档位持仓市值（按当前收盘价）
		positionValue := 0.0
		for _, qty := range positions {
			positionValue += qty * c.Close
		}
		equity = append(equity, EquityPoint{Timestamp: c.Timestamp, Equity: cash + positionValue})

		closePrice := c.Close
		// 从 prevClose 到 closePrice 穿越的网格线
		crossed := getCrossedLevels(prevClose, closePrice, gridLevels)
		for _, level := range crossed {
			if closePrice > prevClose {
				// 价格上行：在 level 卖出（卖出的是 level 下方一档的持仓）
				sellLevel := findLevelBelow(gridLevels, level)
				if sellLevel >= 0 {
					qty, ok := positions[sellLevel]
					if !ok || qty <= 0 {
						continue
					}
					execPrice := level * (1 - slippage)
					fee := qty * execPrice * feeRate
					pnl := (execPrice - sellLevel) * qty
					cash += qty*execPrice - fee
					delete(positions, sellLevel)
					trades = append(trades, Trade{
						Timestamp: c.Timestamp,
						Type:      "sell",
						Price:     execPrice,
						Quantity:  qty,
						Fee:       fee,
						PnL:       pnl - fee,
					})
				}
			} else {
				// 价格下行：在 level 买入
				if level < 1e-12 {
					continue
				}
				costUSDT := params.OrderQuantity
				if costUSDT > cash {
					costUSDT = cash
				}
				if costUSDT < 1e-6 {
					continue
				}
				execPrice := level * (1 + slippage)
				buyQty := costUSDT / execPrice
				buyFee := buyQty * execPrice * feeRate
				totalCost := buyQty*execPrice + buyFee
				if totalCost > cash {
					buyQty = (cash - buyFee) / execPrice
					if buyQty <= 0 {
						continue
					}
					buyFee = buyQty * execPrice * feeRate
					totalCost = buyQty*execPrice + buyFee
				}
				cash -= totalCost
				positions[level] = positions[level] + buyQty
				trades = append(trades, Trade{
					Timestamp: c.Timestamp,
					Type:      "buy",
					Price:     execPrice,
					Quantity:  buyQty,
					Fee:       buyFee,
					PnL:       0,
				})
			}
		}
		prevClose = closePrice
	}

	// 期末权益：现金 + 持仓按最后收盘价计价
	lastClose := candles[len(candles)-1].Close
	finalEquity := cash
	for level, qty := range positions {
		finalEquity += qty * lastClose
		_ = level
	}

	metrics := CalculateMetrics(equity, trades, initialCapital)
	riskMetrics := CalculateRiskMetrics(equity)

	return &BacktestResult{
		Symbol:         symbol,
		Strategy:       "grid",
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

func buildGridLevels(low, high float64, gridCount int) []float64 {
	if gridCount <= 0 {
		gridCount = 20
	}
	step := (high - low) / float64(gridCount)
	var levels []float64
	for i := 0; i <= gridCount; i++ {
		p := low + step*float64(i)
		if p > 0 {
			levels = append(levels, roundPrice(p, 8))
		}
	}
	sort.Float64s(levels)
	return levels
}

func roundPrice(p float64, decimals int) float64 {
	f := math.Pow(10, float64(decimals))
	return math.Round(p*f) / f
}

// getCrossedLevels 返回从 prev 到 curr 穿越的网格价格（按穿越顺序）
func getCrossedLevels(prev, curr float64, levels []float64) []float64 {
	var crossed []float64
	if prev <= curr {
		for _, l := range levels {
			if l > prev && l <= curr {
				crossed = append(crossed, l)
			}
		}
	} else {
		for i := len(levels) - 1; i >= 0; i-- {
			l := levels[i]
			if l < prev && l >= curr {
				crossed = append(crossed, l)
			}
		}
	}
	return crossed
}

func findLevelBelow(levels []float64, target float64) float64 {
	const eps = 1e-12
	for i := len(levels) - 1; i >= 0; i-- {
		if levels[i] < target-eps {
			return levels[i]
		}
	}
	return -1
}
