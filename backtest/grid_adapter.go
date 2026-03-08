package backtest

import (
	"fmt"
	"math"
	"sort"
	"strings"
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
	Direction     string  `json:"direction"` // LONG/SHORT/BOTH，預設 LONG。做空時：價格上漲開空(賣)，價格下跌平空(買)
}

// RunGridBacktest 運行網格策略回测（独立於 StrategyAdapter，多檔位多笔交易）
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

	direction := normalizeGridDirection(params.Direction)
	isShort := direction == "SHORT"

	cash := initialCapital
	positions := make(map[float64]float64) // LONG: level->持倉量；SHORT: level->空頭量
	var trades []Trade
	var equity []EquityPoint
	maxPositionQty := 0.0 // 最大持倉（基幣數量，做空時為空頭量）
	totalSlippageLoss := 0.0

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
		riskSkipBuy := false
		if riskSimulator != nil {
			skipBuy, _ := riskSimulator.Check(candles, candleIdx)
			riskSkipBuy = skipBuy
		}

		// 權益：LONG=現金+持倉市值；SHORT=現金+空頭浮盈（持倉為負，市值為負）
		positionValue := 0.0
		totalQty := 0.0
		for _, qty := range positions {
			if isShort {
				positionValue -= qty * c.Close // 空頭：負持倉
				totalQty += qty
			} else {
				positionValue += qty * c.Close
				totalQty += qty
			}
		}
		if totalQty > maxPositionQty {
			maxPositionQty = totalQty
		}
		equity = append(equity, EquityPoint{Timestamp: c.Timestamp, Equity: cash + positionValue})

		closePrice := c.Close
		crossed := getCrossedLevelsIntrabar(prevClose, c.Low, c.High, closePrice, gridLevels)
		for _, cl := range crossed {
			level := cl.level
			if isShort {
				// 做空：價格上漲賣出開空，價格下跌買入平空
				if cl.isBuy {
					// 價格下行：買入平空
					buyLevel := findLevelAbove(gridLevels, level)
					if buyLevel < 0 {
						continue
					}
					qty, ok := positions[buyLevel]
					if !ok || qty <= 0 {
						continue
					}
					execPrice := level * (1 + slippage)
					fee := qty * execPrice * feeRate
					pnl := (buyLevel - execPrice) * qty // 做空：高賣低買盈利
					buySlippageLoss := (execPrice - level) * qty
					totalSlippageLoss += buySlippageLoss
					cash -= qty*execPrice + fee
					delete(positions, buyLevel)
					trades = append(trades, Trade{
						Timestamp: c.Timestamp,
						Type:      "buy",
						Price:     execPrice,
						Quantity:  qty,
						Fee:       fee,
						PnL:       pnl - fee,
					})
				} else {
					// 價格上行：賣出開空
					if riskSkipBuy {
						riskSimulator.RecordSkippedBuy()
						continue
					}
					if level < 1e-12 {
						continue
					}
					if positions[level] > 0 {
						continue
					}
					costUSDT := params.OrderQuantity
					if costUSDT > cash {
						costUSDT = cash
					}
					if costUSDT < 1e-6 {
						continue
					}
					execPrice := level * (1 - slippage)
					sellQty := costUSDT / execPrice
					fee := sellQty * execPrice * feeRate
					totalCost := sellQty*execPrice + fee
					if totalCost > cash {
						sellQty = (cash - fee) / execPrice
						if sellQty <= 0 {
							continue
						}
						fee = sellQty * execPrice * feeRate
						totalCost = sellQty*execPrice + fee
					}
					sellSlippageLoss := (level - execPrice) * sellQty
					totalSlippageLoss += sellSlippageLoss
					cash += sellQty*execPrice - fee
					positions[level] = positions[level] + sellQty
					totalQtyAfter := 0.0
					for _, q := range positions {
						totalQtyAfter += q
					}
					if totalQtyAfter > maxPositionQty {
						maxPositionQty = totalQtyAfter
					}
					trades = append(trades, Trade{
						Timestamp: c.Timestamp,
						Type:      "sell",
						Price:     execPrice,
						Quantity:  sellQty,
						Fee:       fee,
						PnL:       0,
					})
				}
			} else {
				// 做多：價格下跌買入，價格上漲賣出
				if cl.isBuy {
					if riskSkipBuy {
						riskSimulator.RecordSkippedBuy()
						continue
					}
					if level < 1e-12 {
						continue
					}
					if positions[level] > 0 {
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
					buySlippageLoss := (execPrice - level) * buyQty
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
				} else {
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
					sellSlippageLoss := (level - execPrice) * qty
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
				}
			}
		}
		prevClose = closePrice
	}

	lastClose := candles[len(candles)-1].Close
	finalEquity := cash
	for level, qty := range positions {
		if isShort {
			finalEquity -= qty * lastClose
		} else {
			finalEquity += qty * lastClose
		}
		_ = level
	}

	metrics := CalculateMetricsWithPrice(equity, trades, initialCapital, totalSlippageLoss, lastClose)
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

// crossedLevel 帶方向的穿越檔位
type crossedLevel struct {
	level float64
	isBuy bool // true=向下穿越(買入), false=向上穿越(賣出)
}

// getCrossedLevelsIntrabar 利用 K 線 High/Low 檢測檔位穿越（修復：原邏輯僅用收盤價，1 分鐘內很少波動 130+ 導致零交易）
// 若檔位在 [Low, High] 區間內且與 prevClose 在兩側，則認為該 K 線內穿越了該檔位
func getCrossedLevelsIntrabar(prevClose, low, high, closePrice float64, levels []float64) []crossedLevel {
	var result []crossedLevel
	for _, l := range levels {
		if l < low-1e-12 || l > high+1e-12 {
			continue
		}
		if prevClose > l+1e-12 {
			result = append(result, crossedLevel{level: l, isBuy: true})
		} else if prevClose < l-1e-12 {
			result = append(result, crossedLevel{level: l, isBuy: false})
		}
	}
	// 按穿越順序：向下穿越從高到低，向上穿越從低到高；同根 K 線內先買後賣
	sort.Slice(result, func(i, j int) bool {
		if result[i].isBuy != result[j].isBuy {
			return result[i].isBuy
		}
		if result[i].isBuy {
			return result[i].level > result[j].level
		}
		return result[i].level < result[j].level
	})
	return result
}

// getCrossedLevels 返回從 prev 到 curr 穿越的网格價格（按穿越顺序，僅用收盤價，供測試或兼容）
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

func findLevelAbove(levels []float64, target float64) float64 {
	const eps = 1e-12
	for i := 0; i < len(levels); i++ {
		if levels[i] > target+eps {
			return levels[i]
		}
	}
	return -1
}

func normalizeGridDirection(d string) string {
	switch strings.ToUpper(strings.TrimSpace(d)) {
	case "SHORT":
		return "SHORT"
	case "BOTH":
		return "BOTH"
	}
	return "LONG"
}
