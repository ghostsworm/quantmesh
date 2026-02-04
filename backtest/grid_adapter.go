package backtest

import (
	"fmt"
	"math"
	"sort"
	"time"

	"quantmesh/exchange"
)

// GridBacktestParams 网格回测参數
type GridBacktestParams struct {
	PriceLow      float64 `json:"price_low"`
	PriceHigh     float64 `json:"price_high"`
	GridCount     int     `json:"grid_count"`     // 格子數量；當 grid_spacing > 0 時可為 0，由間距推算
	GridSpacing   float64 `json:"grid_spacing"`   // 可選：網格間距（價格差，如 200 表示每檔差 200）。>0 時檔位為 price_low, price_low+200, ... 直至不超過 price_high
	OrderQuantity float64 `json:"order_quantity"` // 單笔订單 USDT
	TotalCapital  float64 `json:"total_capital"`
	FeeRate       float64 `json:"fee_rate"`
	SlippageRatio float64 `json:"slippage_ratio"`
}

// RunGridBacktest 运行網格策略回测（独立於 StrategyAdapter，多檔位多笔交易）
// 回测時可不填價格上下限：若 price_low/price_high 未填或為 0，則從 K 線數據推導回測區間的實際最低價/最高價，更貼近「實盤不知未來高低」的假設。
// riskSimulator 可選，為 nil 時不啟用風控；非 nil 時在觸發風控時跳過買入信號。
func RunGridBacktest(symbol string, candles []*exchange.Candle, params GridBacktestParams, initialCapital float64, riskSimulator *RiskSimulator) (*BacktestResult, error) {
	if len(candles) == 0 {
		return nil, fmt.Errorf("candles is empty")
	}
	if params.OrderQuantity <= 0 || params.TotalCapital <= 0 {
		return nil, fmt.Errorf("order_quantity and total_capital must be positive")
	}

	priceLow := params.PriceLow
	priceHigh := params.PriceHigh
	needLow := priceLow <= 0
	needHigh := priceHigh <= 0
	if needLow || needHigh {
		// 從 K 線推導回測區間的實際最低價/最高價（未填的一端或兩端）
		derivedLow := candles[0].Low
		derivedHigh := candles[0].High
		for _, c := range candles[1:] {
			if c.Low > 0 && c.Low < derivedLow {
				derivedLow = c.Low
			}
			if c.High > 0 && c.High > derivedHigh {
				derivedHigh = c.High
			}
		}
		if needLow && derivedLow > 0 {
			priceLow = derivedLow
		}
		if needHigh && derivedHigh > 0 {
			priceHigh = derivedHigh
		}
		if priceLow <= 0 || priceHigh <= 0 {
			return nil, fmt.Errorf("could not derive price range from candles (need at least one valid Low/High)")
		}
		if priceHigh <= priceLow {
			priceHigh = priceLow * 1.01 // 單一價時給一點區間
		}
	} else if priceHigh <= priceLow {
		return nil, fmt.Errorf("price_high must be greater than price_low")
	}

	var gridLevels []float64
	if params.GridSpacing > 0 {
		gridLevels = buildGridLevelsBySpacing(priceLow, priceHigh, params.GridSpacing, params.GridCount)
	} else {
		gridLevels = buildGridLevels(priceLow, priceHigh, params.GridCount)
	}
	if len(gridLevels) == 0 {
		return nil, fmt.Errorf("no grid levels (set grid_spacing or grid_count)")
	}

	cash := initialCapital
	positions := make(map[float64]float64) // price -> quantity
	var trades []Trade
	var equity []EquityPoint
	maxPositionQty := 0.0 // 最大持倉（基幣數量）
	totalSlippageLoss := 0.0 // 🔥 累计slippage损失

	feeRate := params.FeeRate
	if feeRate <= 0 {
		feeRate = 0.0004
	}
	slippage := params.SlippageRatio
	if slippage <= 0 {
		slippage = 0.0003
	}

	prevClose := candles[0].Close
	for candleIdx, c := range candles {
		// 检查风控状态（仅在风控模拟器启用时）
		riskSkipBuy := false
		if riskSimulator != nil {
			skipBuy, _ := riskSimulator.Check(candles, candleIdx)
			riskSkipBuy = skipBuy
		}

		// 权益 = 現金 + 各檔位持倉市值（按當前收盘價）
		positionValue := 0.0
		totalQty := 0.0
		for _, qty := range positions {
			positionValue += qty * c.Close
			totalQty += qty
		}
		if totalQty > maxPositionQty {
			maxPositionQty = totalQty
		}
		equity = append(equity, EquityPoint{Timestamp: c.Timestamp, Equity: cash + positionValue})

		closePrice := c.Close
		// 從 prevClose 到 closePrice 穿越的网格線
		crossed := getCrossedLevels(prevClose, closePrice, gridLevels)
		for _, level := range crossed {
			if closePrice > prevClose {
				// 價格上行：賣出該檔或下方一檔的持倉。若只有一檔（無下方一檔），則在穿越的該檔平倉
				sellLevel := findLevelBelow(gridLevels, level)
				if sellLevel < 0 {
					sellLevel = level
				}
				qty, ok := positions[sellLevel]
				if !ok || qty <= 0 {
					continue
				}
				execPrice := level * (1 - slippage)
				fee := qty * execPrice * feeRate
				pnl := (execPrice - sellLevel) * qty
				// 🔥 计算卖出slippage损失：理想价格（level）- 实际价格
				sellSlippageLoss := (level - execPrice) * qty // 等于 level * slippage * qty
				totalSlippageLoss += sellSlippageLoss
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
			} else {
				// 價格下行：在 level 買入
				if riskSkipBuy {
					riskSimulator.RecordSkippedBuy()
					continue
				}
				if level < 1e-12 {
					continue
				}
				if positions[level] > 0 {
					continue // 該檔位已有持倉，須等賣出後才能再次買入
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
				// 🔥 计算买入slippage损失：实际价格 - 理想价格（level）
				buySlippageLoss := (execPrice - level) * buyQty // 等于 level * slippage * buyQty
				totalSlippageLoss += buySlippageLoss
				cash -= totalCost
				positions[level] = positions[level] + buyQty
				totalQtyAfter := 0.0
				for _, q := range positions {
					totalQtyAfter += q
				}
				if totalQtyAfter > maxPositionQty {
					maxPositionQty = totalQtyAfter
				}
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

	// 期末权益：現金 + 持倉按最后收盘價计價
	lastClose := candles[len(candles)-1].Close
	finalEquity := cash
	for level, qty := range positions {
		finalEquity += qty * lastClose
		_ = level
	}

	metrics := CalculateMetrics(equity, trades, initialCapital, totalSlippageLoss)
	metrics.MaxPosition = maxPositionQty
	riskMetrics := CalculateRiskMetrics(equity)

	res := &BacktestResult{
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
		PriceCurve:     ComputePriceCurveSummary(candles),
	}
	if riskSimulator != nil {
		res.RiskEnabled = true
		res.RiskInterventions = riskSimulator.GetInterventions()
	}
	return res, nil
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

// buildGridLevelsBySpacing 按固定價格間距生成檔位：price_low, price_low+spacing, ... 直至不超過 price_high
// maxCount：若 >0 則最多只保留 maxCount 個檔位（從下限起）；0 表示不限制
// 例如 low=77600, high=78000, spacing=200, maxCount=0 → 77600, 77800, 78000
// 若 spacing=200, maxCount=10 且區間很大，則只取前 10 檔
func buildGridLevelsBySpacing(low, high, spacing float64, maxCount int) []float64 {
	if spacing <= 0 {
		return nil
	}
	var levels []float64
	for p := low; p <= high+1e-9; p += spacing {
		if p > 0 {
			levels = append(levels, roundPrice(p, 8))
		}
		if maxCount > 0 && len(levels) >= maxCount {
			break
		}
	}
	if len(levels) == 0 && low > 0 {
		levels = append(levels, roundPrice(low, 8))
	}
	sort.Float64s(levels)
	return levels
}

func roundPrice(p float64, decimals int) float64 {
	f := math.Pow(10, float64(decimals))
	return math.Round(p*f) / f
}

// getCrossedLevels 返回從 prev 到 curr 穿越的网格價格（按穿越顺序）
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
