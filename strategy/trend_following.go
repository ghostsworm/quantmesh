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
	priceHistory []float64
	mu           sync.RWMutex

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

	isPaused  bool
	isRunning bool
	eventBus  EventBus

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

	if sp, ok := strategyCfg["short_period"].(int); ok {
		tfs.shortPeriod = sp
	} else {
		tfs.shortPeriod = 10 // 預設 10
	}

	if lp, ok := strategyCfg["long_period"].(int); ok {
		tfs.longPeriod = lp
	} else {
		tfs.longPeriod = 30 // 預設 30
	}

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
	tfs.mu.Lock()
	tfs.isRunning = true
	tfs.mu.Unlock()

	logger.Info("✅ [%s] 趋势跟踪策略已啟动自动交易 (短期:%d, 长期:%d, 方法:%s, 单笔金额:%.2f)",
		tfs.name, tfs.shortPeriod, tfs.longPeriod, tfs.method, tfs.orderAmount)
	return nil
}

// Stop 停止策略
func (tfs *TrendFollowingStrategy) Stop() error {
	tfs.mu.Lock()
	tfs.isRunning = false
	tfs.mu.Unlock()

	if tfs.cancel != nil {
		tfs.cancel()
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

	if len(tfs.priceHistory) < period {
		return 0
	}

	start := len(tfs.priceHistory) - period
	prices := tfs.priceHistory[start:]

	var sum float64
	for _, price := range prices {
		sum += price
	}

	return sum / float64(len(prices))
}

// calculateEMA 计算指數移动平均
func (tfs *TrendFollowingStrategy) calculateEMA(period int) float64 {
	tfs.mu.RLock()
	defer tfs.mu.RUnlock()

	if len(tfs.priceHistory) < period {
		return 0
	}

	start := len(tfs.priceHistory) - period
	prices := tfs.priceHistory[start:]

	// 初始值使用简單移动平均
	var sum float64
	for i := 0; i < period && i < len(prices); i++ {
		sum += prices[i]
	}
	ema := sum / float64(period)

	// 计算平滑因子
	multiplier := 2.0 / (float64(period) + 1.0)

	// 计算EMA
	for i := period; i < len(prices); i++ {
		ema = (prices[i] * multiplier) + (ema * (1 - multiplier))
	}

	return ema
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
	tfs.mu.RLock()
	if !tfs.isRunning || tfs.isPaused || tfs.activeOrder != nil {
		tfs.mu.RUnlock()
		return nil
	}
	tfs.mu.RUnlock()
	tfs.addPrice(price)

	trend := tfs.detectTrend()
	if trend == TrendSide {
		return nil
	}

	tfs.mu.Lock()
	defer tfs.mu.Unlock()

	// 检查止损止盈
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
		if tfs.position == nil {
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

	order, err := tfs.executor.PlaceOrder(&position.OrderRequest{
		Symbol:        symbol,
		Side:          side,
		Price:         orderPrice,
		Quantity:      quantity,
		PriceDecimals: priceDecimals,
		ReduceOnly:    reduceOnly,
		PostOnly:      false,
		ClientOrderID: signalClientOrderID(tfs.name, action),
		StrategyName:  tfs.name,
		StrategyType:  "trend",
	})
	if err != nil {
		return err
	}
	if order == nil {
		return nil
	}
	tracked := &Order{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          order.Side,
		Price:         order.Price,
		Quantity:      order.Quantity,
		Status:        order.Status,
	}
	tfs.activeOrder = tracked
	tfs.pendingAction = action
	if signalOrderStatusFilled(order.Status) {
		tfs.applyFilledOrderLocked(tracked, order.Quantity, order.Price)
	}
	return nil
}

// OnOrderUpdate 订單更新处理
func (tfs *TrendFollowingStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	if update == nil {
		return nil
	}
	tfs.mu.Lock()
	defer tfs.mu.Unlock()

	if !signalOrderMatches(tfs.activeOrder, update) {
		return nil
	}
	tfs.activeOrder.Status = update.Status
	if signalOrderStatusTerminal(update.Status) {
		tfs.activeOrder = nil
		tfs.pendingAction = ""
		return nil
	}
	if !signalOrderStatusFilled(update.Status) {
		return nil
	}
	fillPrice := update.AvgPrice
	if fillPrice <= 0 {
		fillPrice = update.Price
	}
	fillQty := update.ExecutedQty
	if fillQty <= 0 {
		fillQty = tfs.activeOrder.Quantity
	}
	tfs.applyFilledOrderLocked(tfs.activeOrder, fillQty, fillPrice)
	return nil
}

func (tfs *TrendFollowingStrategy) applyFilledOrderLocked(order *Order, quantity, price float64) {
	if order == nil {
		return
	}
	if price <= 0 {
		price = order.Price
	}
	if quantity <= 0 {
		quantity = order.Quantity
	}
	switch tfs.pendingAction {
	case signalActionOpenLong:
		tfs.entryPrice = price
		tfs.position = &Position{
			Symbol:       order.Symbol,
			Size:         quantity,
			EntryPrice:   price,
			CurrentPrice: price,
			PnL:          0,
		}
	case signalActionCloseLong:
		if tfs.position != nil && tfs.entryPrice > 0 {
			tfs.stats.TotalPnL += (price - tfs.entryPrice) * tfs.position.Size
		}
		tfs.position = nil
		tfs.entryPrice = 0
		tfs.stats.TotalTrades++
	}
	tfs.stats.TotalVolume += quantity * price
	tfs.activeOrder = nil
	tfs.pendingAction = ""
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
