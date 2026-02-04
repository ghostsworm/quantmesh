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
	Action string // "buy", "sell", "hold"
	Price  float64
	Reason string
}

// Backtester 回测器
type Backtester struct {
	candles        []*exchange.Candle
	initialCapital float64

	// 交易成本（Binance 合約）
	takerFee float64 // 0.0004 (0.04%)
	makerFee float64 // 0.0002 (0.02%)
	slippage float64 // 0.0003 (0.03%)

	// 账戶状態
	cash       float64
	position   float64
	entryPrice float64
	equity     []EquityPoint

	// 交易記錄
	trades []Trade

	// 🔥 价格偏差（slippage）损失累计
	totalSlippageLoss float64 // 累计slippage损失（USDT）

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

// Trade 交易記錄
type Trade struct {
	Timestamp int64   `json:"timestamp"`
	Type      string  `json:"type"` // "buy" or "sell"
	Price     float64 `json:"price"`
	Quantity  float64 `json:"quantity"`
	Fee       float64 `json:"fee"`
	PnL       float64 `json:"pnl"` // 僅 sell 時有值
}

// BacktestResult 回测結果
type BacktestResult struct {
	// 基本信息
	Symbol         string    `json:"symbol"`
	Strategy       string    `json:"strategy"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	InitialCapital float64   `json:"initial_capital"`
	FinalCapital   float64   `json:"final_capital"`

	// 权益曲線
	Equity []EquityPoint `json:"equity"`

	// 交易記錄
	Trades []Trade `json:"trades"`

	// 指標（由 metrics.go 计算）
	Metrics Metrics `json:"metrics"`

	// 风險指標
	RiskMetrics RiskMetrics `json:"risk_metrics"`

	// 價格曲線摘要（拐点、起止价、最大连续涨跌价差，由 K 線計算）
	PriceCurve *PriceCurveSummary `json:"price_curve,omitempty"`

	// 风控相关
	RiskEnabled       bool               `json:"risk_enabled"`       // 是否启用风控
	RiskInterventions []RiskIntervention `json:"risk_interventions"` // 风控介入记录
}

// ComparisonResult 无风控 vs 有风控对比结果
type ComparisonResult struct {
	NoRiskResult   *BacktestResult    `json:"no_risk_result"`
	WithRiskResult *BacktestResult    `json:"with_risk_result"`
	Comparison     *ComparisonMetrics `json:"comparison"`
}

// ComparisonMetrics 对比指标
type ComparisonMetrics struct {
	ReturnDiff            float64 `json:"return_diff"`             // 收益率差异（有风控 - 无风控）
	DrawdownDiff          float64 `json:"drawdown_diff"`           // 最大回撤差异
	TradeCountDiff        int     `json:"trade_count_diff"`        // 交易次数差异
	RiskInterventionCount int     `json:"risk_intervention_count"` // 风控介入总次数
	SkippedSignals        int     `json:"skipped_signals"`         // 因风控跳过的信号数
}

// NewBacktester 創建回测器
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
		takerFee:       0.0004, // Binance 合約 Taker 费率
		makerFee:       0.0002, // Binance 合約 Maker 费率
		slippage:       0.0003, // 0.03% 滑点
		equity:            make([]EquityPoint, 0),
		trades:            make([]Trade, 0),
		totalSlippageLoss: 0,
	}
}

// SetFees 設置交易费用
func (bt *Backtester) SetFees(takerFee, makerFee, slippage float64) {
	bt.takerFee = takerFee
	bt.makerFee = makerFee
	bt.slippage = slippage
}

// Run 运行回测
func (bt *Backtester) Run() (*BacktestResult, error) {
	// Bug Fix 1: 检查 candles 是否為空
	if len(bt.candles) == 0 {
		logger.Error("❌ 回测失败: K線數據為空")
		return nil, fmt.Errorf("candles data is empty")
	}

	bt.cash = bt.initialCapital
	bt.position = 0

	logger.Info("🚀 开始回测: %s 策略, %d 根K線", bt.strategy.GetName(), len(bt.candles))

	for i, candle := range bt.candles {
		// 1. 更新权益
		currentEquity := bt.cash + bt.position*candle.Close
		bt.equity = append(bt.equity, EquityPoint{
			Timestamp: candle.Timestamp,
			Equity:    currentEquity,
		})

		// 2. 調用策略
		signal := bt.strategy.OnCandle(candle)

		// 3. 執行交易
		if signal.Action == "buy" && bt.position == 0 {
			bt.executeBuy(candle)
		} else if signal.Action == "sell" && bt.position > 0 {
			bt.executeSell(candle)
		}

		// 4. 進度显示
		if i%10000 == 0 && i > 0 {
			progress := float64(i) / float64(len(bt.candles)) * 100
			logger.Info("⏳ 回测進度: %.1f%%", progress)
		}
	}

	// 如果还有持倉，按最后價格平倉
	if bt.position > 0 && len(bt.candles) > 0 {
		lastCandle := bt.candles[len(bt.candles)-1]
		bt.executeSell(lastCandle)
		logger.Info("📊 回测結束，强制平倉")
	}

	logger.Info("✅ 回测完成: %d 笔交易", len(bt.trades))

	// 计算指標（传入slippage损失）
	metrics := CalculateMetrics(bt.equity, bt.trades, bt.initialCapital, bt.totalSlippageLoss)

	// 计算风險指標
	riskMetrics := CalculateRiskMetrics(bt.equity)

	// Bug Fix 1: 检查 equity 是否為空（雖然理論上不會，但加上防御性检查）
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
		PriceCurve:     ComputePriceCurveSummary(bt.candles),
	}, nil
}

// executeBuy 執行買入
func (bt *Backtester) executeBuy(candle *exchange.Candle) {
	price := candle.Close * (1 + bt.slippage)
	quantity := (bt.cash * 0.95) / price // 使用 95% 资金
	fee := quantity * price * bt.takerFee

	// 🔥 计算买入slippage损失：实际买入价 - 理想买入价（收盘价）
	buySlippageLoss := (price - candle.Close) * quantity // 等于 candle.Close * bt.slippage * quantity
	bt.totalSlippageLoss += buySlippageLoss

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

	logger.Info("📈 買入: 價格=%.2f, 數量=%.4f, 手续费=%.2f, slippage損失=%.4f", price, quantity, fee, buySlippageLoss)
}

// executeSell 執行賣出
func (bt *Backtester) executeSell(candle *exchange.Candle) {
	price := candle.Close * (1 - bt.slippage)
	quantity := bt.position
	fee := quantity * price * bt.takerFee

	// 🔥 计算卖出slippage损失：理想卖出价（收盘价）- 实际卖出价
	sellSlippageLoss := (candle.Close - price) * quantity // 等于 candle.Close * bt.slippage * quantity
	bt.totalSlippageLoss += sellSlippageLoss

	// Bug Fix 2: 计算盈亏時检查 trades 是否為空
	buyFee := 0.0
	if len(bt.trades) > 0 {
		// 找到最近的買入交易
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

	logger.Info("📉 賣出: 價格=%.2f, 數量=%.4f, 手续费=%.2f, 盈亏=%.2f, slippage損失=%.4f", price, quantity, fee, pnl, sellSlippageLoss)
}

// BuildComparisonResult 构建无风控 vs 有风控的对比结果
func BuildComparisonResult(noRisk, withRisk *BacktestResult, interventions []RiskIntervention) *ComparisonResult {
	if noRisk == nil || withRisk == nil {
		return nil
	}
	comp := &ComparisonMetrics{
		RiskInterventionCount: len(interventions),
	}
	comp.ReturnDiff = withRisk.Metrics.TotalReturn - noRisk.Metrics.TotalReturn
	comp.DrawdownDiff = withRisk.Metrics.MaxDrawdown - noRisk.Metrics.MaxDrawdown
	comp.TradeCountDiff = withRisk.Metrics.TotalTrades - noRisk.Metrics.TotalTrades
	for _, inv := range interventions {
		comp.SkippedSignals += inv.SkippedBuys
	}
	return &ComparisonResult{
		NoRiskResult:   noRisk,
		WithRiskResult: withRisk,
		Comparison:     comp,
	}
}
