package backtest

import (
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
)

// IntrabarBacktester K線内模拟回测器
type IntrabarBacktester struct {
	*Backtester
	ticksPerBar int // 每根K線内模拟的tick數量
}

// NewIntrabarBacktester 創建K線内模拟回测器
func NewIntrabarBacktester(
	symbol string,
	candles []*exchange.Candle,
	strategy StrategyAdapter,
	initialCapital float64,
	ticksPerBar int, // 每根K線内模拟多少次價格变动
) *IntrabarBacktester {
	return &IntrabarBacktester{
		Backtester:  NewBacktester(symbol, candles, strategy, initialCapital),
		ticksPerBar: ticksPerBar,
	}
}

// SetFees 設置交易费用
func (ibt *IntrabarBacktester) SetFees(takerFee, makerFee, slippage float64) {
	ibt.Backtester.SetFees(takerFee, makerFee, slippage)
}

// SimulateIntrabarPrices 模拟K線内部的價格路径
// 使用更真實的價格路径：Open → High → Low → Close
func (ibt *IntrabarBacktester) SimulateIntrabarPrices(candle *exchange.Candle) []IntrabarTick {
	ticks := make([]IntrabarTick, 0, ibt.ticksPerBar)

	// 计算時间间隔
	timeStep := int64(180000 / ibt.ticksPerBar) // 3分钟 = 180000毫秒

	// 根據 OHLC 关系确定價格路径
	// 情况1: Open < Close (上涨K線)
	// 路径: Open → High → Low → Close
	// 情况2: Open > Close (下跌K線)
	// 路径: Open → Low → High → Close
	// 情况3: Open == Close (十字星)
	// 路径: Open → High → Low → Close

	isUpBar := candle.Close >= candle.Open

	if isUpBar {
		// 上涨K線: Open → High → Low → Close
		// 第一段: Open → High (25%)
		step1 := ibt.ticksPerBar / 4
		for i := 0; i < step1; i++ {
			ratio := float64(i) / float64(step1)
			price := candle.Open + (candle.High-candle.Open)*ratio
			ticks = append(ticks, IntrabarTick{
				Price:     price,
				Timestamp: candle.Timestamp + int64(i)*timeStep,
			})
		}

		// 第二段: High → Low (25%)
		step2 := ibt.ticksPerBar / 4
		for i := 0; i < step2; i++ {
			ratio := float64(i) / float64(step2)
			price := candle.High + (candle.Low-candle.High)*ratio
			ticks = append(ticks, IntrabarTick{
				Price:     price,
				Timestamp: candle.Timestamp + int64(step1+i)*timeStep,
			})
		}

		// 第三段: Low → Close (50%)
		step3 := ibt.ticksPerBar - step1 - step2
		for i := 0; i < step3; i++ {
			ratio := float64(i) / float64(step3)
			price := candle.Low + (candle.Close-candle.Low)*ratio
			ticks = append(ticks, IntrabarTick{
				Price:     price,
				Timestamp: candle.Timestamp + int64(step1+step2+i)*timeStep,
			})
		}
	} else {
		// 下跌K線: Open → Low → High → Close
		// 第一段: Open → Low (25%)
		step1 := ibt.ticksPerBar / 4
		for i := 0; i < step1; i++ {
			ratio := float64(i) / float64(step1)
			price := candle.Open + (candle.Low-candle.Open)*ratio
			ticks = append(ticks, IntrabarTick{
				Price:     price,
				Timestamp: candle.Timestamp + int64(i)*timeStep,
			})
		}

		// 第二段: Low → High (25%)
		step2 := ibt.ticksPerBar / 4
		for i := 0; i < step2; i++ {
			ratio := float64(i) / float64(step2)
			price := candle.Low + (candle.High-candle.Low)*ratio
			ticks = append(ticks, IntrabarTick{
				Price:     price,
				Timestamp: candle.Timestamp + int64(step1+i)*timeStep,
			})
		}

		// 第三段: High → Close (50%)
		step3 := ibt.ticksPerBar - step1 - step2
		for i := 0; i < step3; i++ {
			ratio := float64(i) / float64(step3)
			price := candle.High + (candle.Close-candle.High)*ratio
			ticks = append(ticks, IntrabarTick{
				Price:     price,
				Timestamp: candle.Timestamp + int64(step1+step2+i)*timeStep,
			})
		}
	}

	return ticks
}

// IntrabarTick K線内的模拟tick
type IntrabarTick struct {
	Price     float64
	Timestamp int64
}

// Run 运行K線内模拟回测
func (ibt *IntrabarBacktester) Run() (*BacktestResult, error) {
	ibt.cash = ibt.initialCapital
	ibt.position = 0

	logger.Info("🚀 开始K線内模拟回测: %s 策略, %d 根K線, 每根K線 %d 次tick",
		ibt.strategy.GetName(), len(ibt.candles), ibt.ticksPerBar)
	logger.Info("📊 總计模拟: %d 次價格变动", len(ibt.candles)*ibt.ticksPerBar)

	totalTicks := 0

	for i, candle := range ibt.candles {
		// 模拟K線内部的價格变动
		intrabarTicks := ibt.SimulateIntrabarPrices(candle)

		for _, tick := range intrabarTicks {
			totalTicks++

			// 創建模拟的K線（用於策略决策）
			simulatedCandle := &exchange.Candle{
				Symbol:    candle.Symbol,
				Open:      tick.Price,
				High:      tick.Price,
				Low:       tick.Price,
				Close:     tick.Price,
				Volume:    candle.Volume / float64(ibt.ticksPerBar), // 平均分配成交量
				Timestamp: tick.Timestamp,
				IsClosed:  false,
			}

			// 更新权益
			currentEquity := ibt.cash + ibt.position*tick.Price
			ibt.equity = append(ibt.equity, EquityPoint{
				Timestamp: tick.Timestamp,
				Equity:    currentEquity,
			})

			// 調用策略
			signal := ibt.strategy.OnCandle(simulatedCandle)

			// 執行交易
			if signal.Action == "buy" && ibt.position == 0 {
				ibt.executeBuyAtPrice(tick.Price, tick.Timestamp)
			} else if signal.Action == "sell" && ibt.position > 0 {
				ibt.executeSellAtPrice(tick.Price, tick.Timestamp)
			}
		}

		// 進度显示
		if i%1000 == 0 && i > 0 {
			progress := float64(i) / float64(len(ibt.candles)) * 100
			logger.Info("⏳ 回测進度: %.1f%% (%d ticks)", progress, totalTicks)
		}
	}

	// 如果还有持倉，按最后價格平倉
	if ibt.position > 0 {
		lastCandle := ibt.candles[len(ibt.candles)-1]
		ibt.executeSellAtPrice(lastCandle.Close, lastCandle.Timestamp)
		logger.Info("📊 回测結束，强制平倉")
	}

	logger.Info("✅ K線内模拟回测完成: %d 笔交易, %d 次tick", len(ibt.trades), totalTicks)

	// 计算指標
	metrics := CalculateMetrics(ibt.equity, ibt.trades, ibt.initialCapital)
	riskMetrics := CalculateRiskMetrics(ibt.equity)

	return &BacktestResult{
		Symbol:         ibt.symbol,
		Strategy:       ibt.strategy.GetName(),
		StartTime:      time.Unix(ibt.candles[0].Timestamp/1000, 0),
		EndTime:        time.Unix(ibt.candles[len(ibt.candles)-1].Timestamp/1000, 0),
		InitialCapital: ibt.initialCapital,
		FinalCapital:   ibt.cash + ibt.position*ibt.candles[len(ibt.candles)-1].Close,
		Equity:         ibt.equity,
		Trades:         ibt.trades,
		Metrics:        metrics,
		RiskMetrics:    riskMetrics,
	}, nil
}

// executeBuyAtPrice 在指定價格買入
func (ibt *IntrabarBacktester) executeBuyAtPrice(price float64, timestamp int64) {
	if ibt.cash <= 0 {
		return
	}

	// 计算可買數量（扣除手续费）
	quantity := ibt.cash / (price * (1 + ibt.takerFee))

	if quantity <= 0 {
		return
	}

	// 计算成本
	cost := quantity * price
	fee := cost * ibt.takerFee
	totalCost := cost + fee

	if totalCost > ibt.cash {
		return
	}

	// 更新状態
	ibt.cash -= totalCost
	ibt.position = quantity
	ibt.entryPrice = price

	// 記錄交易
	ibt.trades = append(ibt.trades, Trade{
		Timestamp: timestamp,
		Type:      "buy",
		Price:     price,
		Quantity:  quantity,
		Fee:       fee,
		PnL:       0,
	})

	if len(ibt.trades) <= 10 || len(ibt.trades)%1000 == 0 {
		logger.Info("📈 買入: 價格=%.2f, 數量=%.4f, 手续费=%.2f, 剩餘現金=%.2f", price, quantity, fee, ibt.cash)
	}
}

// executeSellAtPrice 在指定價格賣出
func (ibt *IntrabarBacktester) executeSellAtPrice(price float64, timestamp int64) {
	if ibt.position <= 0 {
		return
	}

	quantity := ibt.position

	// 计算收益
	revenue := quantity * price
	fee := revenue * ibt.takerFee
	cost := quantity * ibt.entryPrice
	pnl := revenue - fee - cost

	// 更新状態
	ibt.cash += (revenue - fee)
	ibt.position = 0
	ibt.entryPrice = 0

	// 記錄交易
	ibt.trades = append(ibt.trades, Trade{
		Timestamp: timestamp,
		Type:      "sell",
		Price:     price,
		Quantity:  quantity,
		Fee:       fee,
		PnL:       pnl,
	})

	if len(ibt.trades) <= 10 || len(ibt.trades)%1000 == 0 {
		logger.Info("📉 賣出: 價格=%.2f, 數量=%.4f, 手续费=%.2f, 盈亏=%.2f, 現金=%.2f", price, quantity, fee, pnl, ibt.cash)
	}
}
