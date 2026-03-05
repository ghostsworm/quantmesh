package backtest

import (
	"fmt"
	"sync"
	"time"

	"quantmesh/logger"
)

// MultiStrategyEngine 多策略回测引擎
// 支援单个Bot中的多个策略组合进行回测
type MultiStrategyEngine struct {
	// 配置
	Config *EngineConfig

	// 數據
	Klines []TickKline

	// 组件
	Matcher *TickMatcher

	// 策略
	Strategies []BacktestStrategy

	// 帳戶狀態
	Account *BacktestAccount

	// 資金費率
	FundingRates []FundingRateRow

	// 運行时狀態
	mu             sync.RWMutex
	currentIndex   int
	isRunning      bool
	isPaused       bool
	progressCallback func(float64)

	// 結果
	trades        []TickTrade
	equityCurve   []EquityPoint
	completedTrades []CompletedTrade
	totalSlippage float64
	totalFunding  float64

	// 統計
	statsByStrategy map[string]*StrategyStats
}

// EngineConfig 引擎配置
type EngineConfig struct {
	Symbol              string
	InitialCapital      float64
	CommissionRate      float64 // 手续費率
	Leverage            float64 // 杠杆倍数
	StartDate           time.Time
	EndDate             time.Time
	PositionMode        string  // "long_only", "short_only", "both"
	MaxLongRatio        float64 // 最大多头倉位比例
	MaxShortRatio       float64 // 最大空头倉位比例
	EnableFunding       bool    // 是否启用資金費率
	DataDir             string  // 數據目錄
	MatcherConfig       MatcherConfig
}

// BacktestStrategy 回测策略接口
type BacktestStrategy interface {
	// OnInit 策略初始化
	OnInit(account *BacktestAccount, cfg interface{}) error

	// OnKline 收到K線时调用
	OnKline(kline TickKline, timestamp int64) ([]TickOrder, error)

	// OnTrade 成交回調
	OnTrade(trade TickTrade)

	// GetName 獲取策略名称
	GetName() string

	// GetType 獲取策略類型
	GetType() string

	// GetConfig 獲取策略配置
	GetConfig() map[string]interface{}
}

// BacktestAccount 回测帳戶
type BacktestAccount struct {
	// 基础信息
	Symbol       string
	InitialBalance float64
	Leverage     float64

	// 當前狀態
	mu               sync.RWMutex
	Balance          float64 // 余额
	PositionSize     float64 // 净倉位（正=多，负=空）
	PositionEntryPrice float64 // 平均入场价
	UnrealizedPnL    float64 // 未實現盈亏
	RealizedPnL      float64 // 已實現盈亏
	MarginUsed       float64 // 已用保证金
	Equity           float64 // 權益
	PeakEquity       float64 // 歷史最高權益

	// 統計
	TotalVolume      float64
	TotalFees        float64
	TotalSlippage    float64
	Liquidated       bool
	LiquidationPrice float64
}

// StrategyStats 策略統計
type StrategyStats struct {
	Name              string
	Type              string
	TotalTrades       int
	RealizedPnL       float64
	SlippageCost      float64
	FundingCost       float64
	WinRate           float64
	MaxDrawdown       float64
	CompletedTrades   []CompletedTrade
}

// NewMultiStrategyEngine 創建多策略回测引擎
func NewMultiStrategyEngine(cfg *EngineConfig) *MultiStrategyEngine {
	if cfg.CommissionRate == 0 {
		cfg.CommissionRate = 0.0004 // 預設0.04%
	}
	if cfg.Leverage == 0 {
		cfg.Leverage = 1.0
	}
	if cfg.MatcherConfig.BuySlippage == 0 {
		cfg.MatcherConfig = DefaultMatcherConfig()
	}

	return &MultiStrategyEngine{
		Config:        cfg,
		Matcher:       NewTickMatcher(cfg.MatcherConfig),
		Account:       NewBacktestAccount(cfg.Symbol, cfg.InitialCapital, cfg.Leverage),
		Strategies:    make([]BacktestStrategy, 0),
		trades:        make([]TickTrade, 0),
		equityCurve:   make([]EquityPoint, 0),
		completedTrades: make([]CompletedTrade, 0),
		statsByStrategy: make(map[string]*StrategyStats),
	}
}

// NewBacktestAccount 創建回测帳戶
func NewBacktestAccount(symbol string, initialBalance, leverage float64) *BacktestAccount {
	return &BacktestAccount{
		Symbol:          symbol,
		InitialBalance:  initialBalance,
		Leverage:        leverage,
		Balance:         initialBalance,
		Equity:          initialBalance,
		PeakEquity:      initialBalance,
		PositionSize:    0,
		PositionEntryPrice: 0,
	}
}

// AddStrategy 添加策略
func (e *MultiStrategyEngine) AddStrategy(strategy BacktestStrategy) error {
	// 初始化策略
	if err := strategy.OnInit(e.Account, strategy.GetConfig()); err != nil {
		return fmt.Errorf("failed to initialize strategy %s: %w", strategy.GetName(), err)
	}

	e.Strategies = append(e.Strategies, strategy)

	// 初始化統計
	e.statsByStrategy[strategy.GetName()] = &StrategyStats{
		Name:            strategy.GetName(),
		Type:            strategy.GetType(),
		CompletedTrades: make([]CompletedTrade, 0),
	}

	return nil
}

// LoadData 加載歷史數據
func (e *MultiStrategyEngine) LoadData() error {
	logger.Info("Loading historical data for %s from %s", e.Config.Symbol, e.Config.DataDir)

	loader := NewDataLoader(e.Config.DataDir, e.Config.Symbol)

	// 加載K線數據
	klineRows, err := loader.LoadKlinesFromDir()
	if err != nil {
		return fmt.Errorf("failed to load klines: %w", err)
	}

	// 驗證數據
	if err := ValidateKlines(klineRows); err != nil {
		return fmt.Errorf("invalid klines data: %w", err)
	}

	// 过滤時間範圍
	if !e.Config.StartDate.IsZero() || !e.Config.EndDate.IsZero() {
		klineRows = loader.FilterByTimeRange(klineRows, e.Config.StartDate, e.Config.EndDate)
	}

	if len(klineRows) == 0 {
		return fmt.Errorf("no klines data after filtering")
	}

	// 轉換为TickKline
	e.Klines = ConvertToTickKlines(klineRows)

	// 打印統計信息
	stats := GetDataStats(klineRows)
	logger.Info("Data loaded: %d klines, span: %s, price change: %.2f%%",
		stats.TotalKlines, stats.TimeSpan, stats.PriceChangePct)

	// 加載資金費率數據（如果启用）
	if e.Config.EnableFunding {
		fundingRates, err := loader.LoadFundingRatesFromDir()
		if err != nil {
			logger.Warn("Failed to load funding rates: %v (continuing without funding)", err)
		} else {
			e.FundingRates = fundingRates
			logger.Info("Loaded %d funding rate records", len(fundingRates))
		}
	}

	return nil
}

// Run 運行回测
func (e *MultiStrategyEngine) Run() (*MultiStrategyResult, error) {
	e.mu.Lock()
	e.isRunning = true
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.isRunning = false
		e.mu.Unlock()
	}()

	logger.Info("Starting backtest: %d strategies, %d klines", len(e.Strategies), len(e.Klines))

	startTime := time.Now()

	// 重置狀態
	e.reset()

	// 初始化資金費率索引
	fundingIdx := 0

	// 主循环：處理每根K線
	for i, kline := range e.Klines {
		e.mu.Lock()
		e.currentIndex = i
		e.mu.Unlock()

		// 檢查暂停
		for e.isPaused {
			time.Sleep(100 * time.Millisecond)
		}

		// 檢查強平
		if e.Account.Liquidated {
			logger.Warn("Account liquidated at kline %d", i)
			break
		}

		// 更新帳戶權益
		e.updateEquity(kline)

		// 處理資金費率
		if e.Config.EnableFunding && len(e.FundingRates) > 0 {
			e.processFundingRate(kline.Timestamp, &fundingIdx)
		}

		// 收集所有策略的訂單
		var allOrders []TickOrder
		for _, strategy := range e.Strategies {
			orders, err := strategy.OnKline(kline, kline.Timestamp)
			if err != nil {
				logger.Error("Strategy %s error at kline %d: %v", strategy.GetName(), i, err)
				continue
			}
			allOrders = append(allOrders, orders...)
		}

		// 撮合成交
		maxLongSize, maxShortSize := e.calculatePositionLimits(kline.Close)
		trades := e.Matcher.ProcessPathWithLimit(
			&kline,
			allOrders,
			kline.Timestamp,
			e.Account.PositionSize,
			maxLongSize,
			maxShortSize,
		)

		// 處理成交
		for _, trade := range trades {
			e.processTrade(&trade)
		}

		// 通知所有策略成交
		for _, strategy := range e.Strategies {
			for _, trade := range trades {
				strategy.OnTrade(trade)
			}
		}

		// 更新进度
		if e.progressCallback != nil && i%1000 == 0 {
			progress := float64(i) / float64(len(e.Klines)) * 100
			e.progressCallback(progress)
		}

		// 进度日志
		if i%10000 == 0 && i > 0 {
			logger.Info("Backtest progress: %.1f%% (%d/%d klines)",
				float64(i)/float64(len(e.Klines))*100, i, len(e.Klines))
		}
	}

	// 强制平倉剩餘倉位
	if len(e.Klines) > 0 && e.Account.PositionSize != 0 {
		lastKline := e.Klines[len(e.Klines)-1]
		e.forceClosePosition(lastKline.Close, int64(lastKline.Timestamp))
	}

	// 报告最终进度
	if e.progressCallback != nil {
		e.progressCallback(100.0)
	}

	elapsed := time.Since(startTime)
	logger.Info("Backtest completed in %s: %d trades, final equity: %.2f USDT",
		elapsed, len(e.trades), e.Account.Equity)

	return e.generateResult(), nil
}

// reset 重置引擎狀態
func (e *MultiStrategyEngine) reset() {
	e.Account.Balance = e.Config.InitialCapital
	e.Account.Equity = e.Config.InitialCapital
	e.Account.PeakEquity = e.Config.InitialCapital
	e.Account.PositionSize = 0
	e.Account.PositionEntryPrice = 0
	e.Account.RealizedPnL = 0
	e.Account.UnrealizedPnL = 0
	e.Account.Liquidated = false

	e.trades = make([]TickTrade, 0)
	e.equityCurve = make([]EquityPoint, 0)
	e.completedTrades = make([]CompletedTrade, 0)
	e.totalSlippage = 0
	e.totalFunding = 0

	for _, stats := range e.statsByStrategy {
		stats.TotalTrades = 0
		stats.RealizedPnL = 0
		stats.SlippageCost = 0
		stats.FundingCost = 0
		stats.CompletedTrades = make([]CompletedTrade, 0)
	}
}

// updateEquity 更新權益
func (e *MultiStrategyEngine) updateEquity(kline TickKline) {
	e.Account.mu.Lock()
	defer e.Account.mu.Unlock()

	// 計算未實現盈亏
	if e.Account.PositionSize != 0 {
		unrealizedPnL := 0.0
		if e.Account.PositionSize > 0 {
			// 多头倉位
			unrealizedPnL = (kline.Close - e.Account.PositionEntryPrice) * e.Account.PositionSize
		} else {
			// 空头倉位
			unrealizedPnL = (e.Account.PositionEntryPrice - kline.Close) * (-e.Account.PositionSize)
		}
		e.Account.UnrealizedPnL = unrealizedPnL
	}

	// 計算權益
	e.Account.Equity = e.Account.Balance + e.Account.UnrealizedPnL

	// 更新峰值權益
	if e.Account.Equity > e.Account.PeakEquity {
		e.Account.PeakEquity = e.Account.Equity
	}

	// 記錄權益曲線
	e.equityCurve = append(e.equityCurve, EquityPoint{
		Timestamp: kline.Timestamp,
		Equity:    e.Account.Equity,
	})
}

// processFundingRate 處理資金費率
func (e *MultiStrategyEngine) processFundingRate(timestamp int64, fundingIdx *int) {
	// 查找适用的資金費率
	for *fundingIdx < len(e.FundingRates) {
		funding := e.FundingRates[*fundingIdx]
		if funding.FundingTime > timestamp {
			break
		}

		// 計算資金费用
		if e.Account.PositionSize != 0 {
			positionValue := e.Account.PositionSize * e.Account.PositionEntryPrice
			fundingCost := positionValue * funding.FundingRate
			e.Account.Balance -= fundingCost
			e.totalFunding += fundingCost
		}

		*fundingIdx++
	}
}

// calculatePositionLimits 計算倉位限制
func (e *MultiStrategyEngine) calculatePositionLimits(price float64) (maxLongSize, maxShortSize float64) {
	maxLongRatio := e.Config.MaxLongRatio
	maxShortRatio := e.Config.MaxShortRatio

	if maxLongRatio == 0 {
		maxLongRatio = 1.0
	}
	if maxShortRatio == 0 {
		maxShortRatio = 1.0
	}

	maxLongValue := e.Config.InitialCapital * e.Account.Leverage * maxLongRatio
	maxShortValue := e.Config.InitialCapital * e.Account.Leverage * maxShortRatio

	if price > 0 {
		maxLongSize = maxLongValue / price
		maxShortSize = maxShortValue / price
	}

	return
}

// processTrade 處理成交
func (e *MultiStrategyEngine) processTrade(trade *TickTrade) {
	e.Account.mu.Lock()
	defer e.Account.mu.Unlock()

	// 計算手续费
	fee := trade.Price * trade.Size * e.Config.CommissionRate

	// 記錄交易
	e.trades = append(e.trades, *trade)
	e.totalSlippage += trade.Slippage
	e.Account.TotalFees += fee
	e.Account.TotalSlippage += trade.Slippage

	// 更新帳戶
	if trade.Side == "buy" {
		// 买入（开多或平空）
		if e.Account.PositionSize < 0 {
			// 平空
			closeSize := min(-e.Account.PositionSize, trade.Size)
			closePnL := (e.Account.PositionEntryPrice - trade.Price) * closeSize
			e.Account.Balance += closePnL - fee
			e.Account.RealizedPnL += closePnL
			e.Account.PositionSize += closeSize

			// 記錄完成交易
			e.recordCompletedTrade(trade, "short", closeSize, closePnL, fee, trade.Slippage)

			// 剩餘部分开多
			remaining := trade.Size - closeSize
			if remaining > 0 {
				e.openPosition(trade, remaining, "buy", fee)
			}
		} else {
			// 开多或加多
			e.openPosition(trade, trade.Size, "buy", fee)
		}
	} else {
		// 卖出（开空或平多）
		if e.Account.PositionSize > 0 {
			// 平多
			closeSize := min(e.Account.PositionSize, trade.Size)
			closePnL := (trade.Price - e.Account.PositionEntryPrice) * closeSize
			e.Account.Balance += closePnL - fee
			e.Account.RealizedPnL += closePnL
			e.Account.PositionSize -= closeSize

			// 記錄完成交易
			e.recordCompletedTrade(trade, "long", closeSize, closePnL, fee, trade.Slippage)

			// 剩餘部分开空
			remaining := trade.Size - closeSize
			if remaining > 0 {
				e.openPosition(trade, remaining, "sell", fee)
			}
		} else {
			// 开空或加空
			e.openPosition(trade, trade.Size, "sell", fee)
		}
	}

	// 更新策略統計
	if stats, ok := e.statsByStrategy[trade.Strategy]; ok {
		stats.TotalTrades++
		stats.SlippageCost += trade.Slippage
	}

	// 檢查強平
	e.checkLiquidation(trade.Price)
}

// openPosition 开仓
func (e *MultiStrategyEngine) openPosition(trade *TickTrade, size float64, side string, fee float64) {
	cost := trade.Price * size + fee

	// 更新平均入场价
	if e.Account.PositionSize == 0 {
		e.Account.PositionEntryPrice = trade.Price
		e.Account.PositionSize = size
		if side == "sell" {
			e.Account.PositionSize = -size
		}
	} else {
		// 加仓时重新計算平均价
		totalCost := e.Account.PositionEntryPrice*abs(e.Account.PositionSize) + cost
		totalSize := abs(e.Account.PositionSize) + size
		e.Account.PositionEntryPrice = totalCost / totalSize

		if side == "buy" {
			e.Account.PositionSize += size
		} else {
			e.Account.PositionSize -= size
		}
	}

	e.Account.Balance -= cost
	e.Account.TotalVolume += cost
}

// recordCompletedTrade 記錄完成交易
func (e *MultiStrategyEngine) recordCompletedTrade(trade *TickTrade, side string, size float64, pnl, fee, slippage float64) {
	completed := CompletedTrade{
		Timestamp:     trade.Timestamp,
		Side:          side,
		EntryPrice:    e.Account.PositionEntryPrice,
		ExitPrice:     trade.Price,
		Size:          size,
		PnL:           pnl,
		Fee:           fee,
		Slippage:      slippage,
		Strategy:      trade.Strategy,
		GridLevel:     trade.GridLevel,
	}

	e.completedTrades = append(e.completedTrades, completed)

	// 更新策略統計
	if stats, ok := e.statsByStrategy[trade.Strategy]; ok {
		stats.CompletedTrades = append(stats.CompletedTrades, completed)
		stats.RealizedPnL += pnl
	}
}

// checkLiquidation 檢查強平
func (e *MultiStrategyEngine) checkLiquidation(price float64) {
	if e.Account.PositionSize == 0 {
		return
	}

	maintenanceMarginRatio := 0.005 // 维持保证金率0.5%

	positionValue := abs(e.Account.PositionSize) * price
	maintenanceMargin := positionValue * maintenanceMarginRatio
	remainingMargin := e.Account.Balance

	if remainingMargin <= maintenanceMargin {
		e.Account.Liquidated = true
		e.Account.LiquidationPrice = price
		logger.Warn("Liquidation triggered at price %.2f", price)
	}
}

// forceClosePosition 强制平倉
func (e *MultiStrategyEngine) forceClosePosition(price float64, timestamp int64) {
	if e.Account.PositionSize == 0 {
		return
	}

	logger.Info("Forcing close position: size=%.4f, price=%.2f", e.Account.PositionSize, price)

	// 創建平倉訂單
	side := "sell"
	if e.Account.PositionSize < 0 {
		side = "buy"
	}

	trade := &TickTrade{
		TradeID:   fmt.Sprintf("FORCE_CLOSE_%d", timestamp),
		OrderID:   "FORCE_CLOSE",
		Side:      side,
		Price:     price,
		Size:      abs(e.Account.PositionSize),
		Strategy:  "FORCE_CLOSE",
		Timestamp: timestamp,
	}

	e.processTrade(trade)
}

// Pause 暂停回测
func (e *MultiStrategyEngine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.isPaused = true
}

// Resume 恢复回测
func (e *MultiStrategyEngine) Resume() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.isPaused = false
}

// Stop 停止回测
func (e *MultiStrategyEngine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.isRunning = false
}

// IsRunning 是否正在運行
func (e *MultiStrategyEngine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isRunning
}

// GetProgress 獲取进度
func (e *MultiStrategyEngine) GetProgress() float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.Klines) == 0 {
		return 0
	}
	return float64(e.currentIndex) / float64(len(e.Klines)) * 100
}

// SetProgressCallback 设置进度回調
func (e *MultiStrategyEngine) SetProgressCallback(callback func(float64)) {
	e.progressCallback = callback
}

// MultiStrategyResult 多策略回测結果
type MultiStrategyResult struct {
	// 基本信息
	Symbol         string    `json:"symbol"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Duration       string    `json:"duration"`

	// 帳戶信息
	InitialCapital float64 `json:"initial_capital"`
	FinalEquity    float64 `json:"final_equity"`
	TotalReturn    float64 `json:"total_return"`
	TotalReturnPct float64 `json:"total_return_pct"`

	// 統計信息
	TotalTrades       int     `json:"total_trades"`
	TotalVolume       float64 `json:"total_volume"`
	TotalFees         float64 `json:"total_fees"`
	TotalSlippage     float64 `json:"total_slippage"`
	TotalFunding      float64 `json:"total_funding"`

	// 權益曲線
	EquityCurve []EquityPoint `json:"equity_curve"`

	// 交易記錄
	Trades          []TickTrade      `json:"trades"`
	CompletedTrades []CompletedTrade `json:"completed_trades"`

	// 策略統計
	StatsByStrategy map[string]*StrategyStats `json:"stats_by_strategy"`

	// 风险指標
	RiskMetrics *MultiStrategyRiskMetrics `json:"risk_metrics"`
}

// MultiStrategyRiskMetrics 风险指標
type MultiStrategyRiskMetrics struct {
	MaxDrawdown      float64 `json:"max_drawdown"`
	MaxDrawdownPct   float64 `json:"max_drawdown_pct"`
	SharpeRatio      float64 `json:"sharpe_ratio"`
	WinRate          float64 `json:"win_rate"`
	ProfitFactor     float64 `json:"profit_factor"`
	AvgWin           float64 `json:"avg_win"`
	AvgLoss          float64 `json:"avg_loss"`
	LargestWin       float64 `json:"largest_win"`
	LargestLoss      float64 `json:"largest_loss"`
}

// generateResult 生成回测結果
func (e *MultiStrategyEngine) generateResult() *MultiStrategyResult {
	if len(e.Klines) == 0 {
		return nil
	}

	startTime := time.Unix(e.Klines[0].Timestamp/1000, 0)
	endTime := time.Unix(e.Klines[len(e.Klines)-1].Timestamp/1000, 0)
	duration := endTime.Sub(startTime)

	totalReturn := e.Account.Equity - e.Config.InitialCapital
	totalReturnPct := (totalReturn / e.Config.InitialCapital) * 100

	result := &MultiStrategyResult{
		Symbol:           e.Config.Symbol,
		StartTime:        startTime,
		EndTime:          endTime,
		Duration:         duration.String(),
		InitialCapital:   e.Config.InitialCapital,
		FinalEquity:      e.Account.Equity,
		TotalReturn:      totalReturn,
		TotalReturnPct:   totalReturnPct,
		TotalTrades:      len(e.trades),
		TotalVolume:      e.Account.TotalVolume,
		TotalFees:        e.Account.TotalFees,
		TotalSlippage:    e.totalSlippage,
		TotalFunding:     e.totalFunding,
		EquityCurve:      e.equityCurve,
		Trades:           e.trades,
		CompletedTrades:  e.completedTrades,
		StatsByStrategy:  e.statsByStrategy,
		RiskMetrics:      e.calculateRiskMetrics(),
	}

	return result
}

// calculateRiskMetrics 計算风险指標
func (e *MultiStrategyEngine) calculateRiskMetrics() *MultiStrategyRiskMetrics {
	if len(e.equityCurve) == 0 {
		return &MultiStrategyRiskMetrics{}
	}

	// 計算最大回撤
	peak := e.equityCurve[0].Equity
	maxDD := 0.0
	var maxDDPct float64

	for _, point := range e.equityCurve {
		if point.Equity > peak {
			peak = point.Equity
		}
		drawdown := peak - point.Equity
		if drawdown > maxDD {
			maxDD = drawdown
			if peak > 0 {
				maxDDPct = (drawdown / peak) * 100
			}
		}
	}

	// 計算胜率
	wins := 0
	totalCompleted := len(e.completedTrades)
	var totalWin, totalLoss float64
	largestWin := 0.0
	largestLoss := 0.0

	for _, trade := range e.completedTrades {
		if trade.PnL > 0 {
			wins++
			totalWin += trade.PnL
			if trade.PnL > largestWin {
				largestWin = trade.PnL
			}
		} else {
			totalLoss += trade.PnL
			if trade.PnL < largestLoss {
				largestLoss = trade.PnL
			}
		}
	}

	winRate := 0.0
	if totalCompleted > 0 {
		winRate = float64(wins) / float64(totalCompleted) * 100
	}

	avgWin := 0.0
	avgLoss := 0.0
	if wins > 0 {
		avgWin = totalWin / float64(wins)
	}
	if totalCompleted-wins > 0 {
		avgLoss = totalLoss / float64(totalCompleted-wins)
	}

	// 盈亏比
	profitFactor := 0.0
	if totalLoss != 0 {
		profitFactor = totalWin / (-totalLoss)
	}

	return &MultiStrategyRiskMetrics{
		MaxDrawdown:    maxDD,
		MaxDrawdownPct: maxDDPct,
		SharpeRatio:    0, // 需要更复杂的計算
		WinRate:        winRate,
		ProfitFactor:   profitFactor,
		AvgWin:         avgWin,
		AvgLoss:        avgLoss,
		LargestWin:     largestWin,
		LargestLoss:    largestLoss,
	}
}

// CompletedTrade 完成交易記錄
type CompletedTrade struct {
	Timestamp  int64   `json:"timestamp"`
	Side       string  `json:"side"`       // "long" or "short"
	EntryPrice float64 `json:"entry_price"`
	ExitPrice  float64 `json:"exit_price"`
	Size       float64 `json:"size"`
	PnL        float64 `json:"pnl"`
	Fee        float64 `json:"fee"`
	Slippage   float64 `json:"slippage"`
	Strategy   string  `json:"strategy"`
	GridLevel  int     `json:"grid_level,omitempty"`
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
