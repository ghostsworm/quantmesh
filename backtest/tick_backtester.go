package backtest

import (
	"fmt"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
)

// TickBacktester tick級回測引擎
// 支持使用真實 aggTrade 數據進行 tick 級撮合回測
type TickBacktester struct {
	// 配置
	symbol          string
	initialCapital  float64
	takerFee        float64
	makerFee        float64
	slippageBps     float64
	latencyMs       int64

	// 數據源
	aggTrades       []AggTradeRow
	candles         []*exchange.Candle

	// 撮合引擎
	tickMatcher     *RealTickMatcher

	// 账戶狀態
	cash            float64
	position        float64
	entryPrice      float64
	equity          []EquityPoint

	// 交易記錄
	trades          []Trade

	// 滑點損失
	totalSlippageLoss float64

	// 策略适配器
	strategy        StrategyAdapter

	// 回測配置
	matchMode       MatchMode
}

// MatchMode 撮合模式
type MatchMode string

const (
	MatchModeRealTick   MatchMode = "real_tick"   // 使用真實 tick 數據撮合
	MatchModeSimulated  MatchMode = "simulated"   // 使用模擬 tick 數據撮合（原有 TickMatcher）
	MatchModeKlineOnly  MatchMode = "kline_only"  // 僅使用 K 線撮合（最快）
)

// NewTickBacktester 創建 tick 級回測引擎
func NewTickBacktester(symbol string, initialCapital float64, matchMode MatchMode) *TickBacktester {
	return &TickBacktester{
		symbol:         symbol,
		initialCapital: initialCapital,
		cash:           initialCapital,
		takerFee:       0.0004, // Binance 合約 taker fee
		makerFee:       0.0002, // Binance 合約 maker fee
		slippageBps:    1.0,    // 預設 1 基點滑點
		latencyMs:      10,     // 預設 10 毫秒延遲
		matchMode:      matchMode,
		equity:         make([]EquityPoint, 0),
		trades:         make([]Trade, 0),
	}
}

// SetAggTrades 設置聚合交易數據
func (tb *TickBacktester) SetAggTrades(trades []AggTradeRow) {
	tb.aggTrades = trades
	tb.tickMatcher = NewRealTickMatcher(tb.slippageBps, tb.latencyMs)
	tb.tickMatcher.LoadAggTrades(trades)
}

// SetCandles 設置 K 線數據（用於策略信號）
func (tb *TickBacktester) SetCandles(candles []*exchange.Candle) {
	tb.candles = candles
}

// SetStrategy 設置策略
func (tb *TickBacktester) SetStrategy(strategy StrategyAdapter) {
	tb.strategy = strategy
}

// SetFeeConfig 設置手續費
func (tb *TickBacktester) SetFeeConfig(takerFee, makerFee float64) {
	tb.takerFee = takerFee
	tb.makerFee = makerFee
}

// SetSlippage 設置滑點
func (tb *TickBacktester) SetSlippage(slippageBps float64) {
	tb.slippageBps = slippageBps
	if tb.tickMatcher != nil {
		tb.tickMatcher.SlippageBps = slippageBps
	}
}

// Run 執行回測
func (tb *TickBacktester) Run() (*BacktestResult, error) {
	if tb.strategy == nil {
		return nil, fmt.Errorf("strategy not set")
	}

	if len(tb.aggTrades) == 0 && len(tb.candles) == 0 {
		return nil, fmt.Errorf("no data loaded")
	}

	logger.Info("Starting tick-level backtest for %s (mode: %s)", tb.symbol, tb.matchMode)

	startTime := time.Now()
	defer func() {
		logger.Info("Tick-level backtest completed in %v", time.Since(startTime))
	}()

	// 根據撮合模式執行回測
	switch tb.matchMode {
	case MatchModeRealTick:
		return tb.runWithRealTick()
	case MatchModeSimulated:
		return tb.runWithSimulatedTick()
	case MatchModeKlineOnly:
		return tb.runWithKlineOnly()
	default:
		return nil, fmt.Errorf("unknown match mode: %s", tb.matchMode)
	}
}

// runWithRealTick 使用真實 tick 數據回測
func (tb *TickBacktester) runWithRealTick() (*BacktestResult, error) {
	logger.Info("Running backtest with REAL tick data (%d trades)", len(tb.aggTrades))

	if len(tb.aggTrades) == 0 {
		return nil, fmt.Errorf("no aggTrades data available")
	}

	// 初始化撮合引擎
	tb.tickMatcher = NewRealTickMatcher(tb.slippageBps, tb.latencyMs)
	tb.tickMatcher.LoadAggTrades(tb.aggTrades)

	// 按時間順序處理
	pendingOrders := make(map[string]*TickOrder)
	currentCandleIndex := 0

	// 遍歷每個 tick
	for tickIdx := 0; tickIdx < len(tb.aggTrades); tickIdx++ {
		aggTrade := tb.aggTrades[tickIdx]
		tickTime := time.UnixMilli(aggTrade.Timestamp).UTC()

		// 更新當前 K 線
		for currentCandleIndex < len(tb.candles) {
			candleTime := time.Unix(0, 0).Add(time.Duration(tb.candles[currentCandleIndex].Timestamp) * time.Millisecond)
			if !candleTime.After(tickTime) {
				// 生成策略信號
				signal := tb.strategy.OnCandle(tb.candles[currentCandleIndex])

				// 根據信號創建訂單
				if signal.Action == "buy" || signal.Action == "sell" {
					order := &TickOrder{
						OrderID:    fmt.Sprintf("ORDER_%d_%d", tickIdx, len(pendingOrders)),
						Side:       signal.Action,
						Price:      signal.Price,
						Size:       tb.calculateOrderSize(signal.Price),
						Strategy:   tb.strategy.GetName(),
						StrategyID: "main",
						AccountID:  "default",
						IsGrid:     false,
						IsRiskCtrl: false,
					}
					pendingOrders[order.OrderID] = order
					logger.Debug("Placed %s order at %s (size: %.4f)", signal.Action, formatPrice(signal.Price), order.Size)
				}

				currentCandleIndex++
			} else {
				break
			}
		}

		// 檢查掛單是否成交
		for orderID, order := range pendingOrders {
			if tb.tickMatcher.shouldMatchOrder(*order, aggTrade) {
				// 成交
				fillPrice := tb.tickMatcher.calculateFillPrice(*order, aggTrade)
				tb.executeTrade(order.Side, fillPrice, order.Size, aggTrade.Timestamp)

				// 移除已成交訂單
				delete(pendingOrders, orderID)

				logger.Debug("Order filled: %s @ %s (size: %.4f)", order.Side, formatPrice(fillPrice), order.Size)
			}
		}

		// 更新權益
		if tickIdx%1000 == 0 || tickIdx == len(tb.aggTrades)-1 {
			currentEquity := tb.calculateEquity(aggTrade.Price)
			tb.equity = append(tb.equity, EquityPoint{
				Timestamp: aggTrade.Timestamp,
				Equity:    currentEquity,
			})

			// 輸出進度
			if tickIdx%10000 == 0 {
				progress := float64(tickIdx) / float64(len(tb.aggTrades)) * 100
				logger.Info("Progress: %.1f%% (%d/%d ticks), Equity: %.2f USDT",
					progress, tickIdx, len(tb.aggTrades), currentEquity)
			}
		}
	}

	return tb.buildResult(), nil
}

// runWithSimulatedTick 使用模擬 tick 數據回測
func (tb *TickBacktester) runWithSimulatedTick() (*BacktestResult, error) {
	logger.Info("Running backtest with SIMULATED tick data")

	// 使用原有的 TickMatcher
	tickMatcher := NewTickMatcher(DefaultMatcherConfig())

	for _, candle := range tb.candles {
		// 生成策略信號
		signal := tb.strategy.OnCandle(candle)

		// 根據信號創建訂單
		var orders []TickOrder
		if signal.Action == "buy" || signal.Action == "sell" {
			order := TickOrder{
				OrderID:    fmt.Sprintf("ORDER_%d", candle.Timestamp),
				Side:       signal.Action,
				Price:      signal.Price,
				Size:       tb.calculateOrderSize(signal.Price),
				Strategy:   tb.strategy.GetName(),
				StrategyID: "main",
				AccountID:  "default",
				IsGrid:     false,
				IsRiskCtrl: false,
			}
			orders = append(orders, order)
		}

		// 轉換為 TickKline
		tickKline := TickKline{
			Timestamp: candle.Timestamp * 1000000,
			Open:      candle.Open,
			High:      candle.High,
			Low:       candle.Low,
			Close:     candle.Close,
			Volume:    candle.Volume,
		}

		// 撮合訂單
		trades := tickMatcher.ProcessPath(&tickKline, orders, candle.Timestamp, tb.position)

		// 執行成交
		for _, trade := range trades {
			tb.executeTrade(trade.Side, trade.Price, trade.Size, trade.Timestamp)
		}

		// 更新權益
		currentEquity := tb.calculateEquity(candle.Close)
		tb.equity = append(tb.equity, EquityPoint{
			Timestamp: candle.Timestamp,
			Equity:    currentEquity,
		})
	}

	return tb.buildResult(), nil
}

// runWithKlineOnly 僅使用 K 線回測（最快）
func (tb *TickBacktester) runWithKlineOnly() (*BacktestResult, error) {
	logger.Info("Running backtest with KLINE only (fastest mode)")

	for _, candle := range tb.candles {
		// 生成策略信號
		signal := tb.strategy.OnCandle(candle)

		// 直接以收盤價成交
		if signal.Action == "buy" || signal.Action == "sell" {
			size := tb.calculateOrderSize(signal.Price)
			tb.executeTrade(signal.Action, signal.Price, size, candle.Timestamp)
		}

		// 更新權益
		currentEquity := tb.calculateEquity(candle.Close)
		tb.equity = append(tb.equity, EquityPoint{
			Timestamp: candle.Timestamp,
			Equity:    currentEquity,
		})
	}

	return tb.buildResult(), nil
}

// calculateOrderSize 計算訂單數量
func (tb *TickBacktester) calculateOrderSize(price float64) float64 {
	// 簡單實現：使用固定比例的現金
	orderValue := tb.cash * 0.95 // 使用 95% 的現金
	size := orderValue / price
	return size
}

// executeTrade 執行交易
func (tb *TickBacktester) executeTrade(side string, price, size float64, timestamp int64) {
	feeRate := tb.takerFee
	fee := price * size * feeRate

	if side == "buy" {
		// 買入
		actualSize := size
		cost := price*actualSize + fee

		if cost > tb.cash {
			actualSize = (tb.cash - fee) / price
			cost = tb.cash
		}

		tb.cash -= cost
		tb.position += actualSize

		if tb.position > 0 {
			// 更新持倉成本（加權平均）
			totalValue := tb.entryPrice*float64(tb.position-actualSize) + price*actualSize
			tb.entryPrice = totalValue / float64(tb.position)
		}

		tb.trades = append(tb.trades, Trade{
			Timestamp: timestamp,
			Type:      "buy",
			Price:     price,
			Quantity:  actualSize,
			Fee:       fee,
		})
	} else {
		// 賣出
		if tb.position <= 0 {
			return // 没有持倉可賣
		}

		sellSize := size
		if sellSize > tb.position {
			sellSize = tb.position
		}

		revenue := price * sellSize - fee
		pnl := (price-tb.entryPrice)*sellSize - fee

		tb.cash += revenue
		tb.position -= sellSize

		tb.trades = append(tb.trades, Trade{
			Timestamp: timestamp,
			Type:      "sell",
			Price:     price,
			Quantity:  sellSize,
			Fee:       fee,
			PnL:       pnl,
		})

		// 如果全部平倉，重置進場價
		if tb.position <= 0 {
			tb.entryPrice = 0
		}
	}
}

// calculateEquity 計算當前權益
func (tb *TickBacktester) calculateEquity(currentPrice float64) float64 {
	equity := tb.cash
	if tb.position > 0 {
		equity += tb.position * currentPrice
	}
	return equity
}

// buildResult 構建回測結果
func (tb *TickBacktester) buildResult() *BacktestResult {
	result := &BacktestResult{
		Symbol:         tb.symbol,
		Strategy:       tb.strategy.GetName(),
		InitialCapital: tb.initialCapital,
		FinalCapital:   tb.calculateEquity(tb.aggTrades[len(tb.aggTrades)-1].Price),
		Equity:          tb.equity,
		Trades:          tb.trades,
	}

	if len(tb.candles) > 0 {
		result.StartTime = time.Unix(0, 0).Add(time.Duration(tb.candles[0].Timestamp) * time.Millisecond)
		result.EndTime = time.Unix(0, 0).Add(time.Duration(tb.candles[len(tb.candles)-1].Timestamp) * time.Millisecond)
	}

	// 計算指標
	result.Metrics = CalculateMetrics(tb.equity, tb.trades, tb.initialCapital, tb.totalSlippageLoss)

	return result
}

// formatPrice 格式化價格顯示
func formatPrice(price float64) string {
	return fmt.Sprintf("%.2f", price)
}
