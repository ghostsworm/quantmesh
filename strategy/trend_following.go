package strategy

import (
	"context"
	"math"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

// TrendFollowingStrategy 趋势跟踪策略
type TrendFollowingStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg map[string]interface{}

	// 價格历史
	priceHistory  []float64
	orderUpdateMu sync.Mutex
	mu            sync.RWMutex

	// 均線
	shortMA []float64
	longMA  []float64

	// 持倉
	position   *Position
	entryPrice float64

	// 参數
	method      string // ma/ema
	shortPeriod int
	longPeriod  int
	stopLoss    float64 // 止损比例
	takeProfit  float64 // 止盈比例
	maxPosition float64 // 最大倉位比例
	orderAmount float64
	slippage    float64

	activeOrder   *Order
	pendingAction string
	stats         *StrategyStatistics

	isPaused          bool
	isRunning         bool
	eventBus          EventBus
	runtimeStateStore RuntimeStateStore
	runtimeStateErr   error

	ctx    context.Context
	cancel context.CancelFunc
}

// NewTrendFollowingStrategy 創建趋势跟踪策略
func NewTrendFollowingStrategy(
	name string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *TrendFollowingStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	tfs := &TrendFollowingStrategy{
		name:         name,
		cfg:          cfg,
		executor:     executor,
		exchange:     exchange,
		strategyCfg:  strategyCfg,
		priceHistory: make([]float64, 0, 100),
		shortMA:      make([]float64, 0, 100),
		longMA:       make([]float64, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
		stats:        &StrategyStatistics{},
	}

	// 從配置中读取参數
	if method, ok := strategyCfg["method"].(string); ok {
		tfs.method = method
	} else {
		tfs.method = "ema" // 預設 EMA
	}

	tfs.shortPeriod = signalStrategyInt(strategyCfg, "short_period", 10)
	tfs.longPeriod = signalStrategyInt(strategyCfg, "long_period", 30)

	if sl, ok := strategyCfg["stop_loss"].(float64); ok {
		tfs.stopLoss = sl
	} else {
		tfs.stopLoss = 0.02 // 預設 2%
	}

	if tp, ok := strategyCfg["take_profit"].(float64); ok {
		tfs.takeProfit = tp
	} else {
		tfs.takeProfit = 0.05 // 預設 5%
	}

	if mp, ok := strategyCfg["max_position"].(float64); ok {
		tfs.maxPosition = mp
	} else {
		tfs.maxPosition = 0.3 // 預設 30%
	}
	tfs.orderAmount = signalStrategyOrderAmount(strategyCfg)
	tfs.slippage = signalStrategySlippage(strategyCfg)

	return tfs
}

// Name 回傳策略名稱
func (tfs *TrendFollowingStrategy) Name() string {
	return tfs.name
}

// SetEventBus 設置事件總線
func (tfs *TrendFollowingStrategy) SetEventBus(bus EventBus) {
	tfs.mu.Lock()
	defer tfs.mu.Unlock()
	tfs.eventBus = bus
}

// Initialize 初始化策略
func (tfs *TrendFollowingStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	// 已在構造函數中初始化
	return nil
}

// Start 啟动策略
func (tfs *TrendFollowingStrategy) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := tfs.restoreRuntimeState(); err != nil {
		return err
	}
	if err := tfs.reconcileRuntimeOrder(ctx); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)

	tfs.mu.Lock()
	oldCancel := tfs.cancel
	tfs.ctx = runCtx
	tfs.cancel = cancel
	tfs.isRunning = true
	tfs.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}

	logger.Info("✅ [%s] 趋势跟踪策略已啟动自动交易 (短期:%d, 长期:%d, 方法:%s, 单笔金额:%.2f)",
		tfs.name, tfs.shortPeriod, tfs.longPeriod, tfs.method, tfs.orderAmount)
	return nil
}

// Stop 停止策略
func (tfs *TrendFollowingStrategy) Stop() error {
	tfs.mu.Lock()
	cancel := tfs.cancel
	tfs.isRunning = false
	tfs.cancel = nil
	tfs.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return nil
}

// IsRunning 回傳策略是否已成功啟动
func (tfs *TrendFollowingStrategy) IsRunning() bool {
	tfs.mu.RLock()
	defer tfs.mu.RUnlock()
	return tfs.isRunning
}

// addPrice 新增價格
func (tfs *TrendFollowingStrategy) addPrice(price float64) {
	tfs.mu.Lock()
	defer tfs.mu.Unlock()

	tfs.priceHistory = append(tfs.priceHistory, price)

	// 保持历史記錄在合理範圍内
	maxHistory := tfs.longPeriod * 2
	if len(tfs.priceHistory) > maxHistory {
		// 使用 copy 而不是切片截取，避免記憶體泄漏
		newHistory := make([]float64, maxHistory)
		copy(newHistory, tfs.priceHistory[len(tfs.priceHistory)-maxHistory:])
		tfs.priceHistory = newHistory
	}
}

// calculateMA 计算移动平均
func (tfs *TrendFollowingStrategy) calculateMA(period int) float64 {
	tfs.mu.RLock()
	defer tfs.mu.RUnlock()
	return simpleMovingAverage(tfs.priceHistory, period)
}

// calculateEMA 计算指數移动平均（基于完整价格历史，见 exponentialMovingAverage）
func (tfs *TrendFollowingStrategy) calculateEMA(period int) float64 {
	tfs.mu.RLock()
	defer tfs.mu.RUnlock()
	return exponentialMovingAverage(tfs.priceHistory, period)
}

// detectTrend 检测趋势
func (tfs *TrendFollowingStrategy) detectTrend() Trend {
	var shortMA, longMA float64

	if tfs.method == "ema" {
		shortMA = tfs.calculateEMA(tfs.shortPeriod)
		longMA = tfs.calculateEMA(tfs.longPeriod)
	} else {
		shortMA = tfs.calculateMA(tfs.shortPeriod)
		longMA = tfs.calculateMA(tfs.longPeriod)
	}

	if shortMA == 0 || longMA == 0 {
		return TrendSide
	}

	tfs.mu.RLock()
	currentPrice := tfs.priceHistory[len(tfs.priceHistory)-1]
	tfs.mu.RUnlock()

	if shortMA > longMA && currentPrice > shortMA {
		return TrendUp
	} else if shortMA < longMA && currentPrice < shortMA {
		return TrendDown
	}

	return TrendSide
}

// OnPriceChange 價格變化处理
func (tfs *TrendFollowingStrategy) OnPriceChange(price float64) error {
	return tfs.onPriceChange(price, true)
}

// OnPriceChangeRiskOnly updates market/risk state and permits exits without opening positions.
func (tfs *TrendFollowingStrategy) OnPriceChangeRiskOnly(price float64) error {
	return tfs.onPriceChange(price, false)
}

func (tfs *TrendFollowingStrategy) onPriceChange(price float64, allowOpening bool) error {
	tfs.mu.Lock()
	stateErr := tfs.runtimeStateErr
	shouldEvaluate := tfs.isRunning && (!tfs.isPaused || !allowOpening) && tfs.activeOrder == nil
	priceErr := updateSignalPositionMark(tfs.position, price)
	tfs.mu.Unlock()
	if priceErr != nil {
		return priceErr
	}
	if stateErr != nil {
		return signalRuntimeStateDecisionError(tfs.name, stateErr)
	}
	if !shouldEvaluate {
		return nil
	}
	tfs.addPrice(price)

	trend := tfs.detectTrend()

	tfs.mu.Lock()
	defer tfs.mu.Unlock()

	// 检查止损止盈：必须先于震荡判定执行，否则震荡行情中持仓的止损会被跳过
	if tfs.position != nil && tfs.entryPrice > 0 {
		currentPrice := price
		pnlPercent := (currentPrice - tfs.entryPrice) / tfs.entryPrice

		// 止损
		if pnlPercent <= -tfs.stopLoss {
			logger.Warn("🛑 [%s] 触发止损: 入场價=%.2f, 當前價=%.2f, 亏损=%.2f%%",
				tfs.name, tfs.entryPrice, currentPrice, pnlPercent*100)
			return tfs.placeSignalOrder(signalActionCloseLong, price)
		}

		// 止盈
		if pnlPercent >= tfs.takeProfit {
			logger.Info("💰 [%s] 触发止盈: 入场價=%.2f, 當前價=%.2f, 盈利=%.2f%%",
				tfs.name, tfs.entryPrice, currentPrice, pnlPercent*100)
			return tfs.placeSignalOrder(signalActionCloseLong, price)
		}
	}

	// 趋势向上：开多倉或加倉
	if trend == TrendUp {
		if allowOpening && tfs.position == nil {
			logger.Info("📈 [%s] 上涨趋势，准备自动开多倉", tfs.name)
			return tfs.placeSignalOrder(signalActionOpenLong, price)
		}
	} else if trend == TrendDown {
		// 趋势向下：平倉
		if tfs.position != nil {
			logger.Info("📉 [%s] 下跌趋势，准备自动平倉", tfs.name)
			return tfs.placeSignalOrder(signalActionCloseLong, price)
		}
	}

	return nil
}

func (tfs *TrendFollowingStrategy) placeSignalOrder(action string, price float64) error {
	if action == signalActionOpenLong && signalOpeningPaused(tfs.executor, tfs.cfg) {
		return nil
	}
	if tfs.executor == nil {
		return nil
	}
	symbol := signalStrategySymbol(tfs.cfg, tfs.strategyCfg)
	priceDecimals := signalPriceDecimals(tfs.exchange)
	qtyDecimals := signalQuantityDecimals(tfs.exchange)

	side := "BUY"
	orderPrice := signalRound(price*(1+tfs.slippage), priceDecimals)
	quantity := signalFloor(tfs.orderAmount/orderPrice, qtyDecimals)
	reduceOnly := false

	if action == signalActionCloseLong {
		if tfs.position == nil || tfs.position.Size <= 0 {
			return nil
		}
		side = "SELL"
		orderPrice = signalRound(price*(1-tfs.slippage), priceDecimals)
		quantity = signalFloor(tfs.position.Size, qtyDecimals)
		reduceOnly = signalIsFuturesMarket(tfs.cfg)
	}
	if orderPrice <= 0 || quantity <= 0 {
		logger.Warn("⚠️ [%s] 趋势跟踪下单数量无效: action=%s price=%.8f qty=%.8f", tfs.name, action, orderPrice, quantity)
		return nil
	}

	req := &position.OrderRequest{
		Symbol:        symbol,
		Side:          side,
		Price:         orderPrice,
		Quantity:      quantity,
		PriceDecimals: priceDecimals,
		ReduceOnly:    reduceOnly,
		PositionSide:  position.PositionSideLong,
		PostOnly:      false,
		ClientOrderID: signalClientOrderID(tfs.name, action),
		StrategyName:  tfs.name,
		StrategyType:  "trend",
	}
	venue := ""
	if tfs.exchange != nil {
		venue = tfs.exchange.GetName()
	}
	return submitSignalOrder(tfs.executor, venue, req, action, &tfs.activeOrder, &tfs.pendingAction, &tfs.position, &tfs.entryPrice, tfs.stats, tfs.exchange, tfs.persistRuntimeStateLocked)
}

// OnOrderUpdate 订單更新处理
func (tfs *TrendFollowingStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	tfs.orderUpdateMu.Lock()
	defer tfs.orderUpdateMu.Unlock()
	if update == nil {
		return nil
	}
	tfs.mu.RLock()
	active := signalOrderSnapshot(tfs.activeOrder)
	tfs.mu.RUnlock()
	if err := resolveSignalOrderCommission(tfs.exchange, active, update); err != nil {
		tfs.mu.Lock()
		defer tfs.mu.Unlock()
		return markSignalOrderForReconciliation(&tfs.activeOrder, tfs.executor, update, tfs.persistRuntimeStateLocked, err)
	}
	tfs.mu.Lock()
	defer tfs.mu.Unlock()

	applySignalOrderUpdate(&tfs.activeOrder, &tfs.pendingAction, &tfs.position, &tfs.entryPrice, tfs.stats, tfs.exchange, tfs.executor, update)
	return tfs.persistRuntimeStateLocked()
}

// GetPositions 獲取持倉
func (tfs *TrendFollowingStrategy) GetPositions() []*Position {
	tfs.mu.RLock()
	defer tfs.mu.RUnlock()

	if tfs.position == nil {
		return []*Position{}
	}

	return []*Position{tfs.position}
}

// GetOrders 獲取訂單
func (tfs *TrendFollowingStrategy) GetOrders() []*Order {
	tfs.mu.RLock()
	defer tfs.mu.RUnlock()
	if tfs.activeOrder == nil {
		return []*Order{}
	}
	return []*Order{tfs.activeOrder}
}

// GetStatistics 獲取统计
func (tfs *TrendFollowingStrategy) GetStatistics() *StrategyStatistics {
	tfs.mu.RLock()
	defer tfs.mu.RUnlock()
	stats := *tfs.stats
	return &stats
}

// GetVisualizationData 獲取策略可视化數據
func (tfs *TrendFollowingStrategy) GetVisualizationData() map[string]interface{} {
	data := make(map[string]interface{})

	// 计算快慢均线
	var fastMA, slowMA float64
	if tfs.method == "ema" {
		fastMA = tfs.calculateEMA(tfs.shortPeriod)
		slowMA = tfs.calculateEMA(tfs.longPeriod)
	} else {
		fastMA = tfs.calculateMA(tfs.shortPeriod)
		slowMA = tfs.calculateMA(tfs.longPeriod)
	}

	data["fastMA"] = fastMA
	data["slowMA"] = slowMA
	data["method"] = tfs.method
	data["shortPeriod"] = tfs.shortPeriod
	data["longPeriod"] = tfs.longPeriod

	// 当前价格
	currentPrice := 0.0
	tfs.mu.RLock()
	if len(tfs.priceHistory) > 0 {
		currentPrice = tfs.priceHistory[len(tfs.priceHistory)-1]
	}
	hasPosition := tfs.position != nil
	entryPrice := tfs.entryPrice
	isRunning := tfs.isRunning
	pendingAction := tfs.pendingAction
	tfs.mu.RUnlock()
	data["currentPrice"] = currentPrice

	// 趋势方向
	trend := tfs.detectTrend()
	trendStr := "side"
	if trend == TrendUp {
		trendStr = "up"
	} else if trend == TrendDown {
		trendStr = "down"
	}
	data["trend"] = trendStr

	// 均线差值
	if fastMA > 0 && slowMA > 0 {
		maDiff := ((fastMA - slowMA) / slowMA) * 100
		data["maDiff"] = maDiff
		data["maDiffAbs"] = math.Abs(maDiff)
	}

	// 持仓状态
	if hasPosition {
		data["hasPosition"] = true
		data["entryPrice"] = entryPrice
		if currentPrice > 0 && entryPrice > 0 {
			pnlPercent := ((currentPrice - entryPrice) / entryPrice) * 100
			data["pnlPercent"] = pnlPercent
		}
	} else {
		data["hasPosition"] = false
		data["entryPrice"] = 0
	}

	// 止损止盈
	data["stopLoss"] = tfs.stopLoss * 100 // 转换为百分比
	data["takeProfit"] = tfs.takeProfit * 100
	data["executionMode"] = "auto_trade"
	data["autoTradingEnabled"] = true
	data["isRunning"] = isRunning
	data["orderAmount"] = tfs.orderAmount
	data["pendingAction"] = pendingAction

	// 金叉/死叉判断
	if fastMA > 0 && slowMA > 0 {
		isGoldenCross := fastMA > slowMA
		data["isGoldenCross"] = isGoldenCross
		data["isDeathCross"] = !isGoldenCross
	}

	return data
}
