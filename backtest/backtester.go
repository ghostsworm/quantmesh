package backtest

import (
	"fmt"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
)

// StrategyAdapter 策略适配器接口
type StrategyAdapter interface {
	OnCandle(candle *exchange.Candle) Signal
	GetName() string
}

// Signal 交易信号
type Signal struct {
	Action string  // "buy", "sell", "hold"
	Price  float64
	Reason string
}

// Backtester 回测器
type Backtester struct {
	candles        []*exchange.Candle
	initialCapital float64

	// 交易成本（Binance 合约）
	takerFee float64 // 0.0004 (0.04%)
	makerFee float64 // 0.0002 (0.02%)
	slippage float64 // 0.0003 (0.03%)

	// 账户状态
	cash       float64
	position   float64
	entryPrice float64
	equity     []EquityPoint

	// 交易记录
	trades []Trade

	// 策略适配器
	strategy StrategyAdapter

	// 配置
	symbol string
}

// EquityPoint 权益点
type EquityPoint struct {
	Timestamp int64   `json:"timestamp"`
	Equity    float64 `json:"equity"`
}

// Trade 交易记录
type Trade struct {
	Timestamp int64   `json:"timestamp"`
	Type      string  `json:"type"` // "buy" or "sell"
	Price     float64 `json:"price"`
	Quantity  float64 `json:"quantity"`
	Fee       float64 `json:"fee"`
	PnL       float64 `json:"pnl"` // 仅 sell 时有值
}

// BacktestResult 回测结果
type BacktestResult struct {
	// 基本信息
	Symbol         string    `json:"symbol"`
	Strategy       string    `json:"strategy"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	InitialCapital float64   `json:"initial_capital"`
	FinalCapital   float64   `json:"final_capital"`

	// 权益曲线
	Equity []EquityPoint `json:"equity"`

	// 交易记录
	Trades []Trade `json:"trades"`

	// 指标（由 metrics.go 计算）
	Metrics Metrics `json:"metrics"`

	// 风险指标
	RiskMetrics RiskMetrics `json:"risk_metrics"`
}

// NewBacktester 创建回测器
func NewBacktester(
	symbol string,
	candles []*exchange.Candle,
	strategy StrategyAdapter,
	initialCapital float64,
) *Backtester {
	return &Backtester{
		symbol:         symbol,
		candles:        candles,
		strategy:       strategy,
		initialCapital: initialCapital,
		takerFee:       0.0004, // Binance 合约 Taker 费率
		makerFee:       0.0002, // Binance 合约 Maker 费率
		slippage:       0.0003, // 0.03% 滑点
		equity:         make([]EquityPoint, 0),
		trades:         make([]Trade, 0),
	}
}

// SetFees 设置交易费用
func (bt *Backtester) SetFees(takerFee, makerFee, slippage float64) {
	bt.takerFee = takerFee
	bt.makerFee = makerFee
	bt.slippage = slippage
}

// Run 运行回测
func (bt *Backtester) Run() (*BacktestResult, error) {
	// Bug Fix 1: 检查 candles 是否为空
	if len(bt.candles) == 0 {
		logger.Error("❌ 回测失败: K线数据为空")
		return nil, fmt.Errorf("candles data is empty")
	}

	bt.cash = bt.initialCapital
	bt.position = 0

	logger.Info("🚀 开始回测: %s 策略, %d 根K线", bt.strategy.GetName(), len(bt.candles))

	for i, candle := range bt.candles {
		// 1. 更新权益
		currentEquity := bt.cash + bt.position*candle.Close
		bt.equity = append(bt.equity, EquityPoint{
			Timestamp: candle.Timestamp,
			Equity:    currentEquity,
		})

		// 2. 调用策略
		signal := bt.strategy.OnCandle(candle)

		// 3. 执行交易
		if signal.Action == "buy" && bt.position == 0 {
			bt.executeBuy(candle)
		} else if signal.Action == "sell" && bt.position > 0 {
			bt.executeSell(candle)
		}

		// 4. 进度显示
		if i%10000 == 0 && i > 0 {
			progress := float64(i) / float64(len(bt.candles)) * 100
			logger.Info("⏳ 回测进度: %.1f%%", progress)
		}
	}

	// 如果还有持仓，按最后价格平仓
	if bt.position > 0 && len(bt.candles) > 0 {
		lastCandle := bt.candles[len(bt.candles)-1]
		bt.executeSell(lastCandle)
		logger.Info("📊 回测结束，强制平仓")
	}

	logger.Info("✅ 回测完成: %d 笔交易", len(bt.trades))

	// 计算指标
	metrics := CalculateMetrics(bt.equity, bt.trades, bt.initialCapital)

	// 计算风险指标
	riskMetrics := CalculateRiskMetrics(bt.equity)

	// Bug Fix 1: 检查 equity 是否为空（虽然理论上不会，但加上防御性检查）
	finalCapital := bt.initialCapital
	if len(bt.equity) > 0 {
		finalCapital = bt.equity[len(bt.equity)-1].Equity
	}

	return &BacktestResult{
		Symbol:         bt.symbol,
		Strategy:       bt.strategy.GetName(),
		StartTime:      time.Unix(bt.candles[0].Timestamp/1000, 0),
		EndTime:        time.Unix(bt.candles[len(bt.candles)-1].Timestamp/1000, 0),
		InitialCapital: bt.initialCapital,
		FinalCapital:   finalCapital,
		Equity:         bt.equity,
		Trades:         bt.trades,
		Metrics:        metrics,
		RiskMetrics:    riskMetrics,
	}, nil
}

// executeBuy 执行买入
func (bt *Backtester) executeBuy(candle *exchange.Candle) {
	price := candle.Close * (1 + bt.slippage)
	quantity := (bt.cash * 0.95) / price // 使用 95% 资金
	fee := quantity * price * bt.takerFee

	bt.position = quantity
	bt.entryPrice = price
	bt.cash -= (quantity*price + fee)

	bt.trades = append(bt.trades, Trade{
		Timestamp: candle.Timestamp,
		Type:      "buy",
		Price:     price,
		Quantity:  quantity,
		Fee:       fee,
		PnL:       0,
	})

	logger.Info("📈 买入: 价格=%.2f, 数量=%.4f, 手续费=%.2f", price, quantity, fee)
}

// executeSell 执行卖出
func (bt *Backtester) executeSell(candle *exchange.Candle) {
	price := candle.Close * (1 - bt.slippage)
	quantity := bt.position
	fee := quantity * price * bt.takerFee

	// Bug Fix 2: 计算盈亏时检查 trades 是否为空
	buyFee := 0.0
	if len(bt.trades) > 0 {
		// 找到最近的买入交易
		for i := len(bt.trades) - 1; i >= 0; i-- {
			if bt.trades[i].Type == "buy" {
				buyFee = bt.trades[i].Fee
				break
			}
		}
	}
	pnl := (price-bt.entryPrice)*quantity - fee - buyFee

	bt.cash += quantity*price - fee
	bt.position = 0

	bt.trades = append(bt.trades, Trade{
		Timestamp: candle.Timestamp,
		Type:      "sell",
		Price:     price,
		Quantity:  quantity,
		Fee:       fee,
		PnL:       pnl,
	})

	logger.Info("📉 卖出: 价格=%.2f, 数量=%.4f, 手续费=%.2f, 盈亏=%.2f", price, quantity, fee, pnl)
}
