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

	// 策略
	Strategies []BacktestStrategy

	// 資金費率
	FundingRates []FundingRateRow

	// 信号总线（混合策略）
	signalBus *BacktestSignalBus

	runtimes []*StrategyRuntime

	// 運行时狀態
	mu               sync.RWMutex
	currentIndex     int
	isRunning        bool
	isPaused         bool
	progressCallback func(float64)

	// 結果
	trades          []TickTrade
	equityCurve     []EquityPoint
	completedTrades []CompletedTrade
	totalSlippage   float64
	totalFunding    float64
	finalEquity     float64

	// 統計
	statsByStrategy map[string]*StrategyStats

	// 混合策略配置
	hybridConfig *TaskHybridStrategyConfig
}

// EngineConfig 引擎配置
type EngineConfig struct {
	Symbol          string
	InitialCapital  float64
	CommissionRate  float64 // 手续費率
	Leverage        float64 // 杠杆倍数
	MaxCapitalRatio float64 // 最大资金占用比例 (0.1-1.0)
	StartDate       time.Time
	EndDate         time.Time
	PositionMode    string  // "long_only", "short_only", "both"
	MaxLongRatio    float64 // 最大多头倉位比例
	MaxShortRatio   float64 // 最大空头倵位比例
	EnableFunding   bool    // 是否启用資金費率
	DataDir         string  // 數據目錄
	MatcherConfig   MatcherConfig
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
	AccountID        string
	StrategyID       string
	StrategyName     string
	Symbol           string
	InitialBalance   float64
	AllocatedCapital float64
	Leverage         float64
	MaxCapital       float64 // 最大可用資金（考慮 max_capital_ratio）

	// 當前狀態
	mu                 sync.RWMutex
	Balance            float64 // 余额
	PositionSize       float64 // 净倉位（正=多，负=空）
	PositionEntryPrice float64 // 平均入场价
	UnrealizedPnL      float64 // 未實現盈亏
	RealizedPnL        float64 // 已實現盈亏
	MarginUsed         float64 // 已用保证金
	Equity             float64 // 權益
	PeakEquity         float64 // 歷史最高權益
	lastPrice          float64 // 最後價格

	// 統計
	TotalVolume      float64
	TotalFees        float64
	TotalSlippage    float64
	Liquidated       bool
	LiquidationPrice float64
}

// GetLastPrice 獲取最後價格
func (a *BacktestAccount) GetLastPrice() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastPrice
}

// SetLastPrice 設置最後價格
func (a *BacktestAccount) SetLastPrice(price float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastPrice = price
}

// Cash 獲取可用現金（別名）
func (a *BacktestAccount) Cash() float64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Balance
}

// StrategyStats 策略統計
type StrategyStats struct {
	StrategyID       string           `json:"strategy_id"`
	Name             string           `json:"name"`
	Type             string           `json:"type"`
	InitialCapital   float64          `json:"initial_capital"`
	FinalEquity      float64          `json:"final_equity"`
	OpenPositionSize float64          `json:"open_position_size"`
	TotalTrades      int              `json:"total_trades"`
	RealizedPnL      float64          `json:"realized_pnl"`
	SlippageCost     float64          `json:"slippage_cost"`
	FundingCost      float64          `json:"funding_cost"`
	WinRate          float64          `json:"win_rate"`
	MaxDrawdown      float64          `json:"max_drawdown"`
	CompletedTrades  []CompletedTrade `json:"completed_trades"`
}

type StrategyRuntime struct {
	strategy        BacktestStrategy
	account         *BacktestAccount
	matcher         *TickMatcher
	stats           *StrategyStats
	equityCurve     []EquityPoint
	trades          []TickTrade
	completedTrades []CompletedTrade
	totalSlippage   float64
	totalFunding    float64
	fundingIdx      int
	strategyID      string
	strategyName    string
	strategyType    string
	initialCapital  float64
}

type StrategyResult struct {
	StrategyID       string                    `json:"strategy_id"`
	StrategyName     string                    `json:"strategy_name"`
	Type             string                    `json:"type"`
	Weight           float64                   `json:"weight"`
	AccountID        string                    `json:"account_id"`
	InitialCapital   float64                   `json:"initial_capital"`
	FinalEquity      float64                   `json:"final_equity"`
	OpenPositionSize float64                   `json:"open_position_size"`
	Trades           []TickTrade               `json:"trades"`
	CompletedTrades  []CompletedTrade          `json:"completed_trades"`
	EquityCurve      []EquityPoint             `json:"equity_curve"`
	Stats            *StrategyStats            `json:"stats"`
	RiskMetrics      *MultiStrategyRiskMetrics `json:"risk_metrics"`
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
		Config:          cfg,
		Strategies:      make([]BacktestStrategy, 0),
		runtimes:        make([]*StrategyRuntime, 0),
		trades:          make([]TickTrade, 0),
		equityCurve:     make([]EquityPoint, 0),
		completedTrades: make([]CompletedTrade, 0),
		statsByStrategy: make(map[string]*StrategyStats),
	}
}

// NewBacktestAccount 創建回测帳戶
func NewBacktestAccount(symbol string, initialBalance, leverage, maxCapitalRatio float64) *BacktestAccount {
	// 計算最大可用資金
	maxCapital := initialBalance * maxCapitalRatio
	if maxCapital > initialBalance {
		maxCapital = initialBalance
	}
	if maxCapital <= 0 {
		maxCapital = initialBalance
	}

	return &BacktestAccount{
		Symbol:             symbol,
		InitialBalance:     initialBalance,
		AllocatedCapital:   initialBalance,
		Leverage:           leverage,
		MaxCapital:         maxCapital, // 最大可用資金
		Balance:            initialBalance,
		Equity:             initialBalance,
		PeakEquity:         initialBalance,
		PositionSize:       0,
		PositionEntryPrice: 0,
	}
}

// AddStrategy 添加策略
func (e *MultiStrategyEngine) AddStrategy(strategy BacktestStrategy) error {
	cfg := strategy.GetConfig()
	initialCapital := getFloatParam(cfg, "total_capital", e.Config.InitialCapital)
	strategyID := getStringParam(cfg, "strategy_id")
	if strategyID == "" {
		strategyID = strategy.GetName()
	}
	strategyName := getStringParam(cfg, "strategy_name")
	if strategyName == "" {
		strategyName = strategy.GetName()
	}
	accountID := getStringParam(cfg, "account_id")
	if accountID == "" {
		accountID = strategyAccountID(strategyID)
	}
	account := NewBacktestAccount(e.Config.Symbol, initialCapital, e.Config.Leverage, e.Config.MaxCapitalRatio)
	account.AccountID = accountID
	account.StrategyID = strategyID
	account.StrategyName = strategyName
	account.AllocatedCapital = initialCapital
	if err := strategy.OnInit(account, cfg); err != nil {
		return fmt.Errorf("failed to initialize strategy %s: %w", strategy.GetName(), err)
	}

	e.Strategies = append(e.Strategies, strategy)

	stats := &StrategyStats{
		StrategyID:      strategyID,
		Name:            strategyName,
		Type:            strategy.GetType(),
		InitialCapital:  initialCapital,
		FinalEquity:     initialCapital,
		CompletedTrades: make([]CompletedTrade, 0),
	}
	e.statsByStrategy[strategyID] = stats
	e.runtimes = append(e.runtimes, &StrategyRuntime{
		strategy:        strategy,
		account:         account,
		matcher:         NewTickMatcher(e.Config.MatcherConfig),
		stats:           stats,
		equityCurve:     make([]EquityPoint, 0),
		trades:          make([]TickTrade, 0),
		completedTrades: make([]CompletedTrade, 0),
		strategyID:      strategyID,
		strategyName:    strategyName,
		strategyType:    strategy.GetType(),
		initialCapital:  initialCapital,
	})

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

	if e.IsHybridMode() {
		logger.Info("🔄 混合策略回测模式已启用")
	}

	startTime := time.Now()

	// 重置狀態
	e.reset()

	// 主循环：處理每根K線
	for i, kline := range e.Klines {
		e.mu.Lock()
		e.currentIndex = i
		e.mu.Unlock()

		// 檢查暂停
		for e.isPaused {
			time.Sleep(100 * time.Millisecond)
		}

		totalEquity := 0.0
		for _, runtime := range e.runtimes {
			if runtime.account.Liquidated {
				totalEquity += runtime.account.Equity
				continue
			}

			e.updateEquity(runtime, kline)

			if e.Config.EnableFunding && len(e.FundingRates) > 0 {
				e.processFundingRate(runtime, kline.Timestamp)
			}

			orders, err := runtime.strategy.OnKline(kline, kline.Timestamp)
			if err != nil {
				logger.Error("Strategy %s error at kline %d: %v", runtime.strategyName, i, err)
				totalEquity += runtime.account.Equity
				continue
			}
			for j := range orders {
				if orders[j].Strategy == "" {
					orders[j].Strategy = runtime.strategyName
				}
				if orders[j].StrategyID == "" {
					orders[j].StrategyID = runtime.strategyID
				}
				if orders[j].AccountID == "" {
					orders[j].AccountID = runtime.account.AccountID
				}
			}

			// 混合策略：评估协作规则
			if e.IsHybridMode() && e.signalBus != nil {
				// 评估规则并获取动作
				actions := e.signalBus.EvaluateRules()
				if len(actions) > 0 {
					e.applyRuleActions(actions, kline, i)
				}
			}

			maxLongSize, maxShortSize := e.calculatePositionLimits(runtime, kline.Close)
			trades := runtime.matcher.ProcessPathWithLimit(
				&kline,
				orders,
				kline.Timestamp,
				runtime.account.PositionSize,
				maxLongSize,
				maxShortSize,
			)

			for _, trade := range trades {
				e.processTrade(runtime, &trade)
				runtime.strategy.OnTrade(trade)
			}

			totalEquity += runtime.account.Equity
		}

		e.finalEquity = totalEquity
		e.equityCurve = append(e.equityCurve, EquityPoint{
			Timestamp: kline.Timestamp,
			Equity:    totalEquity,
		})

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

	// 期末强制平倉剩餘倉位，確保最終收益為已實現盈虧
	if len(e.Klines) > 0 {
		lastKline := e.Klines[len(e.Klines)-1]
		forceClosedCount := 0
		for _, runtime := range e.runtimes {
			if runtime.account.PositionSize != 0 {
				e.forceClosePosition(runtime, lastKline.Close, int64(lastKline.Timestamp))
				forceClosedCount++
			}
		}
		if forceClosedCount > 0 {
			logger.Info("📊 期末强制平倉: %d 個策略有剩餘倉位已平倉，最終權益為已實現盈虧", forceClosedCount)
		}
	}

	e.finalEquity = 0
	for _, runtime := range e.runtimes {
		e.finalEquity += runtime.account.Equity
	}

	// 追加權益曲線終點（平倉後），確保報告與導出的最終權益一致
	if len(e.Klines) > 0 {
		lastKline := e.Klines[len(e.Klines)-1]
		e.equityCurve = append(e.equityCurve, EquityPoint{
			Timestamp: lastKline.Timestamp,
			Equity:    e.finalEquity,
		})
	}

	// 报告最终进度
	if e.progressCallback != nil {
		e.progressCallback(100.0)
	}

	elapsed := time.Since(startTime)
	logger.Info("Backtest completed in %s: %d trades, final equity: %.2f USDT",
		elapsed, len(e.trades), e.finalEquity)

	return e.generateResult(), nil
}

// reset 重置引擎狀態
func (e *MultiStrategyEngine) reset() {
	e.trades = make([]TickTrade, 0)
	e.equityCurve = make([]EquityPoint, 0)
	e.completedTrades = make([]CompletedTrade, 0)
	e.totalSlippage = 0
	e.totalFunding = 0
	e.finalEquity = 0

	for _, runtime := range e.runtimes {
		runtime.account.Balance = runtime.initialCapital
		runtime.account.Equity = runtime.initialCapital
		runtime.account.PeakEquity = runtime.initialCapital
		runtime.account.PositionSize = 0
		runtime.account.PositionEntryPrice = 0
		runtime.account.RealizedPnL = 0
		runtime.account.UnrealizedPnL = 0
		runtime.account.TotalVolume = 0
		runtime.account.TotalFees = 0
		runtime.account.TotalSlippage = 0
		runtime.account.Liquidated = false
		runtime.account.LiquidationPrice = 0
		runtime.equityCurve = make([]EquityPoint, 0)
		runtime.trades = make([]TickTrade, 0)
		runtime.completedTrades = make([]CompletedTrade, 0)
		runtime.totalSlippage = 0
		runtime.totalFunding = 0
		runtime.fundingIdx = 0
		runtime.stats.TotalTrades = 0
		runtime.stats.RealizedPnL = 0
		runtime.stats.SlippageCost = 0
		runtime.stats.FundingCost = 0
		runtime.stats.FinalEquity = runtime.initialCapital
		runtime.stats.OpenPositionSize = 0
		runtime.stats.CompletedTrades = make([]CompletedTrade, 0)
	}
}

// updateEquity 更新權益
func (e *MultiStrategyEngine) updateEquity(runtime *StrategyRuntime, kline TickKline) {
	runtime.account.mu.Lock()
	defer runtime.account.mu.Unlock()

	// 更新最後價格
	runtime.account.lastPrice = kline.Close

	// 計算未實現盈亏
	if runtime.account.PositionSize != 0 {
		unrealizedPnL := 0.0
		if runtime.account.PositionSize > 0 {
			// 多头倉位
			unrealizedPnL = (kline.Close - runtime.account.PositionEntryPrice) * runtime.account.PositionSize
		} else {
			// 空头倉位
			unrealizedPnL = (runtime.account.PositionEntryPrice - kline.Close) * (-runtime.account.PositionSize)
		}
		runtime.account.UnrealizedPnL = unrealizedPnL
	}

	// 計算權益
	runtime.account.Equity = runtime.account.Balance + runtime.account.UnrealizedPnL

	// 更新峰值權益
	if runtime.account.Equity > runtime.account.PeakEquity {
		runtime.account.PeakEquity = runtime.account.Equity
	}

	// 記錄權益曲線
	runtime.equityCurve = append(runtime.equityCurve, EquityPoint{
		Timestamp: kline.Timestamp,
		Equity:    runtime.account.Equity,
	})
}

// processFundingRate 處理資金費率
func (e *MultiStrategyEngine) processFundingRate(runtime *StrategyRuntime, timestamp int64) {
	// 查找适用的資金費率
	for runtime.fundingIdx < len(e.FundingRates) {
		funding := e.FundingRates[runtime.fundingIdx]
		if funding.FundingTime > timestamp {
			break
		}

		// 計算資金费用
		if runtime.account.PositionSize != 0 {
			positionValue := abs(runtime.account.PositionSize) * runtime.account.PositionEntryPrice
			fundingCost := positionValue * funding.FundingRate
			runtime.account.Balance -= fundingCost
			runtime.totalFunding += fundingCost
			e.totalFunding += fundingCost
			runtime.stats.FundingCost += fundingCost
		}

		runtime.fundingIdx++
	}
}

// calculatePositionLimits 計算倉位限制
func (e *MultiStrategyEngine) calculatePositionLimits(runtime *StrategyRuntime, price float64) (maxLongSize, maxShortSize float64) {
	maxLongRatio := e.Config.MaxLongRatio
	maxShortRatio := e.Config.MaxShortRatio

	if maxLongRatio == 0 {
		maxLongRatio = 1.0
	}
	if maxShortRatio == 0 {
		maxShortRatio = 1.0
	}

	maxLongValue := runtime.initialCapital * runtime.account.Leverage * maxLongRatio
	maxShortValue := runtime.initialCapital * runtime.account.Leverage * maxShortRatio

	if price > 0 {
		maxLongSize = maxLongValue / price
		maxShortSize = maxShortValue / price
	}

	return
}

// isLongOnly 是否单向做多模式（LONG、long_only 等）
func isLongOnly(mode string) bool {
	return mode == "LONG" || mode == "long_only"
}

// isShortOnly 是否单向做空模式
func isShortOnly(mode string) bool {
	return mode == "SHORT" || mode == "short_only"
}

// processTrade 處理成交
func (e *MultiStrategyEngine) processTrade(runtime *StrategyRuntime, trade *TickTrade) {
	runtime.account.mu.Lock()
	defer runtime.account.mu.Unlock()

	// 单向模式防护：禁止开反向仓
	if trade.Side == "sell" && runtime.account.PositionSize <= 0 && isLongOnly(e.Config.PositionMode) {
		logger.Warn("[回测] 单向做多模式下拒绝开空，跳过成交: %s size=%.6f", trade.OrderID, trade.Size)
		return
	}
	if trade.Side == "buy" && runtime.account.PositionSize >= 0 && isShortOnly(e.Config.PositionMode) {
		logger.Warn("[回测] 单向做空模式下拒绝开多，跳过成交: %s size=%.6f", trade.OrderID, trade.Size)
		return
	}

	// 計算手续费
	fee := trade.Price * trade.Size * e.Config.CommissionRate

	// 記錄交易
	e.trades = append(e.trades, *trade)
	runtime.trades = append(runtime.trades, *trade)
	e.totalSlippage += trade.Slippage
	runtime.totalSlippage += trade.Slippage
	runtime.account.TotalFees += fee
	runtime.account.TotalSlippage += trade.Slippage

	// 更新帳戶
	if trade.Side == "buy" {
		// 买入（开多或平空）
		if runtime.account.PositionSize < 0 {
			// 平空
			closeSize := min(-runtime.account.PositionSize, trade.Size)
			closePnL := (runtime.account.PositionEntryPrice - trade.Price) * closeSize
			runtime.account.Balance += closePnL - fee
			runtime.account.RealizedPnL += closePnL
			runtime.account.PositionSize += closeSize

			// 記錄完成交易
			e.recordCompletedTrade(runtime, trade, "short", closeSize, closePnL, fee, trade.Slippage)

			// 剩餘部分开多（單向做空時禁止）
			remaining := trade.Size - closeSize
			if remaining > 0 && !isShortOnly(e.Config.PositionMode) {
				e.openPosition(runtime, trade, remaining, "buy", fee)
			}
		} else {
			// 开多或加多
			e.openPosition(runtime, trade, trade.Size, "buy", fee)
		}
	} else {
		// 卖出（开空或平多）
		if runtime.account.PositionSize > 0 {
			// 平多
			closeSize := min(runtime.account.PositionSize, trade.Size)
			closePnL := (trade.Price - runtime.account.PositionEntryPrice) * closeSize
			runtime.account.Balance += closePnL - fee
			runtime.account.RealizedPnL += closePnL
			runtime.account.PositionSize -= closeSize

			// 記錄完成交易
			e.recordCompletedTrade(runtime, trade, "long", closeSize, closePnL, fee, trade.Slippage)

			// 剩餘部分开空（單向做多時禁止）
			remaining := trade.Size - closeSize
			if remaining > 0 && !isLongOnly(e.Config.PositionMode) {
				e.openPosition(runtime, trade, remaining, "sell", fee)
			}
		} else {
			// 开空或加空
			e.openPosition(runtime, trade, trade.Size, "sell", fee)
		}
	}

	// 更新策略統計
	runtime.stats.TotalTrades++
	runtime.stats.SlippageCost += trade.Slippage
	runtime.stats.FinalEquity = runtime.account.Equity
	runtime.stats.OpenPositionSize = runtime.account.PositionSize

	// 檢查強平
	e.checkLiquidation(runtime, trade.Price)
}

// openPosition 开仓
func (e *MultiStrategyEngine) openPosition(runtime *StrategyRuntime, trade *TickTrade, size float64, side string, fee float64) {
	cost := trade.Price*size + fee

	// 更新平均入场价
	if runtime.account.PositionSize == 0 {
		runtime.account.PositionEntryPrice = trade.Price
		runtime.account.PositionSize = size
		if side == "sell" {
			runtime.account.PositionSize = -size
		}
	} else {
		// 加仓时重新計算平均价
		totalCost := runtime.account.PositionEntryPrice*abs(runtime.account.PositionSize) + cost
		totalSize := abs(runtime.account.PositionSize) + size
		runtime.account.PositionEntryPrice = totalCost / totalSize

		if side == "buy" {
			runtime.account.PositionSize += size
		} else {
			runtime.account.PositionSize -= size
		}
	}

	runtime.account.Balance -= cost
	runtime.account.TotalVolume += cost
}

// recordCompletedTrade 記錄完成交易
func (e *MultiStrategyEngine) recordCompletedTrade(runtime *StrategyRuntime, trade *TickTrade, side string, size float64, pnl, fee, slippage float64) {
	completed := CompletedTrade{
		Timestamp:  trade.Timestamp,
		Side:       side,
		EntryPrice: runtime.account.PositionEntryPrice,
		ExitPrice:  trade.Price,
		Size:       size,
		PnL:        pnl,
		Fee:        fee,
		Slippage:   slippage,
		Strategy:   trade.Strategy,
		StrategyID: trade.StrategyID,
		AccountID:  trade.AccountID,
		GridLevel:  trade.GridLevel,
	}

	e.completedTrades = append(e.completedTrades, completed)
	runtime.completedTrades = append(runtime.completedTrades, completed)
	runtime.stats.CompletedTrades = append(runtime.stats.CompletedTrades, completed)
	runtime.stats.RealizedPnL += pnl
}

// checkLiquidation 檢查強平
func (e *MultiStrategyEngine) checkLiquidation(runtime *StrategyRuntime, price float64) {
	if runtime.account.PositionSize == 0 {
		return
	}

	maintenanceMarginRatio := 0.005 // 维持保证金率0.5%

	positionValue := abs(runtime.account.PositionSize) * price
	maintenanceMargin := positionValue * maintenanceMarginRatio
	remainingMargin := runtime.account.Balance

	if remainingMargin <= maintenanceMargin {
		runtime.account.Liquidated = true
		runtime.account.LiquidationPrice = price
		logger.Warn("Liquidation triggered at price %.2f", price)
	}
}

// forceClosePosition 强制平倉
func (e *MultiStrategyEngine) forceClosePosition(runtime *StrategyRuntime, price float64, timestamp int64) {
	if runtime.account.PositionSize == 0 {
		return
	}

	logger.Info("Forcing close position: strategy=%s, size=%.4f, price=%.2f", runtime.strategyName, runtime.account.PositionSize, price)

	// 創建平倉訂單
	side := "sell"
	if runtime.account.PositionSize < 0 {
		side = "buy"
	}

	trade := &TickTrade{
		TradeID:    fmt.Sprintf("FORCE_CLOSE_%d", timestamp),
		OrderID:    "FORCE_CLOSE",
		Side:       side,
		Price:      price,
		Size:       abs(runtime.account.PositionSize),
		Strategy:   runtime.strategyName + " [期末强制平仓]",
		StrategyID: runtime.strategyID,
		AccountID:  runtime.account.AccountID,
		Timestamp:  timestamp,
	}

	e.processTrade(runtime, trade)
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
	Symbol    string    `json:"symbol"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Duration  string    `json:"duration"`

	// 帳戶信息
	InitialCapital float64 `json:"initial_capital"`
	FinalEquity    float64 `json:"final_equity"`
	TotalReturn    float64 `json:"total_return"`
	TotalReturnPct float64 `json:"total_return_pct"`

	// 統計信息
	TotalTrades   int     `json:"total_trades"`
	TotalVolume   float64 `json:"total_volume"`
	TotalFees     float64 `json:"total_fees"`
	TotalSlippage float64 `json:"total_slippage"`
	TotalFunding  float64 `json:"total_funding"`

	// 權益曲線
	EquityCurve []EquityPoint `json:"equity_curve"`

	// 交易記錄
	Trades          []TickTrade      `json:"trades"`
	CompletedTrades []CompletedTrade `json:"completed_trades"`

	// 策略統計
	StatsByStrategy map[string]*StrategyStats `json:"stats_by_strategy"`
	StrategyResults []StrategyResult          `json:"strategy_results"`

	// 风险指標
	RiskMetrics *MultiStrategyRiskMetrics `json:"risk_metrics"`

	// 交易配置
	Leverage        float64 `json:"leverage"`         // 杠杆倍数
	MaxCapitalRatio float64 `json:"max_capital_ratio"` // 最大资金占用比例 (0.1-1.0)
}

// MultiStrategyRiskMetrics 风险指標
type MultiStrategyRiskMetrics struct {
	MaxDrawdown    float64 `json:"max_drawdown"`
	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	SharpeRatio    float64 `json:"sharpe_ratio"`
	WinRate        float64 `json:"win_rate"`
	ProfitFactor   float64 `json:"profit_factor"`
	AvgWin         float64 `json:"avg_win"`
	AvgLoss        float64 `json:"avg_loss"`
	LargestWin     float64 `json:"largest_win"`
	LargestLoss    float64 `json:"largest_loss"`
}

// generateResult 生成回测結果
func (e *MultiStrategyEngine) generateResult() *MultiStrategyResult {
	if len(e.Klines) == 0 {
		return nil
	}

	startTime := time.Unix(e.Klines[0].Timestamp/1000, 0)
	endTime := time.Unix(e.Klines[len(e.Klines)-1].Timestamp/1000, 0)
	duration := endTime.Sub(startTime)

	totalReturn := e.finalEquity - e.Config.InitialCapital
	totalReturnPct := (totalReturn / e.Config.InitialCapital) * 100

	strategyResults := make([]StrategyResult, 0, len(e.runtimes))
	for _, runtime := range e.runtimes {
		runtime.stats.FinalEquity = runtime.account.Equity
		runtime.stats.OpenPositionSize = runtime.account.PositionSize
		riskMetrics := calculateRiskMetricsFrom(runtime.equityCurve, runtime.completedTrades)
		runtime.stats.WinRate = riskMetrics.WinRate
		runtime.stats.MaxDrawdown = riskMetrics.MaxDrawdownPct
		weight := 0.0
		if e.Config.InitialCapital > 0 {
			weight = runtime.initialCapital / e.Config.InitialCapital
		}
		strategyResults = append(strategyResults, StrategyResult{
			StrategyID:       runtime.strategyID,
			StrategyName:     runtime.strategyName,
			Type:             runtime.strategyType,
			Weight:           weight,
			AccountID:        runtime.account.AccountID,
			InitialCapital:   runtime.initialCapital,
			FinalEquity:      runtime.account.Equity,
			OpenPositionSize: runtime.account.PositionSize,
			Trades:           append([]TickTrade(nil), runtime.trades...),
			CompletedTrades:  append([]CompletedTrade(nil), runtime.completedTrades...),
			EquityCurve:      append([]EquityPoint(nil), runtime.equityCurve...),
			Stats:            runtime.stats,
			RiskMetrics:      riskMetrics,
		})
	}

	result := &MultiStrategyResult{
		Symbol:          e.Config.Symbol,
		StartTime:       startTime,
		EndTime:         endTime,
		Duration:        duration.String(),
		InitialCapital:  e.Config.InitialCapital,
		FinalEquity:     e.finalEquity,
		TotalReturn:     totalReturn,
		TotalReturnPct:  totalReturnPct,
		TotalTrades:     len(e.trades),
		TotalVolume:     sumRuntimeVolume(e.runtimes),
		TotalFees:       sumRuntimeFees(e.runtimes),
		TotalSlippage:   e.totalSlippage,
		TotalFunding:    e.totalFunding,
		EquityCurve:     e.equityCurve,
		Trades:          e.trades,
		CompletedTrades: e.completedTrades,
		StatsByStrategy: e.statsByStrategy,
		StrategyResults: strategyResults,
		RiskMetrics:     e.calculateRiskMetrics(),
		Leverage:        e.Config.Leverage,
		MaxCapitalRatio: e.Config.MaxCapitalRatio,
	}

	return result
}

// calculateRiskMetrics 計算风险指標
func (e *MultiStrategyEngine) calculateRiskMetrics() *MultiStrategyRiskMetrics {
	return calculateRiskMetricsFrom(e.equityCurve, e.completedTrades)
}

// CompletedTrade 完成交易記錄
type CompletedTrade struct {
	Timestamp  int64   `json:"timestamp"`
	Side       string  `json:"side"` // "long" or "short"
	EntryPrice float64 `json:"entry_price"`
	ExitPrice  float64 `json:"exit_price"`
	Size       float64 `json:"size"`
	PnL        float64 `json:"pnl"`
	Fee        float64 `json:"fee"`
	Slippage   float64 `json:"slippage"`
	Strategy   string  `json:"strategy"`
	StrategyID string  `json:"strategy_id,omitempty"`
	AccountID  string  `json:"account_id,omitempty"`
	GridLevel  int     `json:"grid_level,omitempty"`
}

func calculateRiskMetricsFrom(equityCurve []EquityPoint, completedTrades []CompletedTrade) *MultiStrategyRiskMetrics {
	if len(equityCurve) == 0 {
		return &MultiStrategyRiskMetrics{}
	}

	peak := equityCurve[0].Equity
	maxDD := 0.0
	var maxDDPct float64

	for _, point := range equityCurve {
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

	wins := 0
	totalCompleted := len(completedTrades)
	var totalWin, totalLoss float64
	largestWin := 0.0
	largestLoss := 0.0

	for _, trade := range completedTrades {
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

	profitFactor := 0.0
	if totalLoss != 0 {
		profitFactor = totalWin / (-totalLoss)
	}

	return &MultiStrategyRiskMetrics{
		MaxDrawdown:    maxDD,
		MaxDrawdownPct: maxDDPct,
		SharpeRatio:    0,
		WinRate:        winRate,
		ProfitFactor:   profitFactor,
		AvgWin:         avgWin,
		AvgLoss:        avgLoss,
		LargestWin:     largestWin,
		LargestLoss:    largestLoss,
	}
}

func sumRuntimeVolume(runtimes []*StrategyRuntime) float64 {
	total := 0.0
	for _, runtime := range runtimes {
		total += runtime.account.TotalVolume
	}
	return total
}

func sumRuntimeFees(runtimes []*StrategyRuntime) float64 {
	total := 0.0
	for _, runtime := range runtimes {
		total += runtime.account.TotalFees
	}
	return total
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// SetHybridStrategyConfig 设置混合策略配置
func (e *MultiStrategyEngine) SetHybridStrategyConfig(config *TaskHybridStrategyConfig) {
	e.hybridConfig = config
	if config != nil && len(config.CollaborationRules) > 0 {
		e.signalBus = NewBacktestSignalBus(config.CollaborationRules)
		logger.Info("✅ 混合策略模式已启用: %d 个子策略, %d 条协作规则",
			len(config.SubStrategies), len(config.CollaborationRules))
	}
}

// IsHybridMode 检查是否为混合策略模式
func (e *MultiStrategyEngine) IsHybridMode() bool {
	return e.hybridConfig != nil && e.signalBus != nil && e.signalBus.IsEnabled()
}

// publishSignal 发布策略信号
func (e *MultiStrategyEngine) publishSignal(strategyID, signalType string, value interface{}, metadata map[string]interface{}, klineIndex int) {
	if e.signalBus == nil {
		return
	}

	signal := &BacktestSignal{
		ID:        fmt.Sprintf("%s_%s_%d", strategyID, signalType, klineIndex),
		Type:      signalType,
		Source:    strategyID,
		Value:     value,
		Metadata:  metadata,
		KlineIndex: klineIndex,
	}

	e.signalBus.Publish(signal)
}

// applyRuleActions 应用协作规则动作
func (e *MultiStrategyEngine) applyRuleActions(actions map[string][]RuleAction, kline TickKline, klineIndex int) {
	if len(actions) == 0 {
		return
	}

	// 记录被应用的规则
	for targetID, actionList := range actions {
		for _, action := range actionList {
			logger.Debug("应用协作规则 [%s]: %s -> %s (操作: %s, 优先级: %d)",
				action.RuleName, action.RuleID, targetID, action.Operation, action.RulePriority)

			// 找到目标策略的runtime
			var targetRuntime *StrategyRuntime
			for _, runtime := range e.runtimes {
				if runtime.strategyID == targetID {
					targetRuntime = runtime
					break
				}
			}

			if targetRuntime == nil {
				continue
			}

			// 应用动作
			switch action.Operation {
			case "deny_open":
				// 阻止开仓 - 设置一个标记，在下一根K线时检查
				if targetRuntime.account != nil {
					targetRuntime.account.mu.Lock()
					// 这里可以使用 account 的 metadata 来存储临时标记
					targetRuntime.account.mu.Unlock()
				}

			case "adjust_size":
				// 调整仓位大小
				if _, ok := action.Params["size_multiplier"].(float64); ok {
					// 在策略内部处理
				}

			case "close_position":
				// 强制平仓
				if targetRuntime.account.PositionSize != 0 {
					e.forceClosePosition(targetRuntime, kline.Close, int64(kline.Timestamp))
				}

			case "skip_next":
				// 跳过下一次信号
			}
		}
	}
}
