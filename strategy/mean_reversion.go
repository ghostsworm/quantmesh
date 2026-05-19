package strategy

import (
	"context"
	"math"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

// MeanReversionStrategy 均值回归策略
type MeanReversionStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg map[string]interface{}

	// 價格历史
	priceHistory []float64
	mu           sync.RWMutex

	// 参數
	period             int
	stdMultiplier      float64
	reversionThreshold float64
	orderAmount        float64
	slippage           float64

	// 持倉
	position      *Position
	entryPrice    float64
	activeOrder   *Order
	pendingAction string
	stats         *StrategyStatistics

	isPaused  bool
	isRunning bool
	eventBus  EventBus

	ctx    context.Context
	cancel context.CancelFunc
}

// NewMeanReversionStrategy 創建均值回归策略
func NewMeanReversionStrategy(
	name string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *MeanReversionStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	mrs := &MeanReversionStrategy{
		name:         name,
		cfg:          cfg,
		executor:     executor,
		exchange:     exchange,
		strategyCfg:  strategyCfg,
		priceHistory: make([]float64, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
		stats:        &StrategyStatistics{},
	}

	// 從配置中读取参數
	if p, ok := strategyCfg["period"].(int); ok {
		mrs.period = p
	} else {
		mrs.period = 20 // 預設 20
	}

	if sm, ok := strategyCfg["std_multiplier"].(float64); ok {
		mrs.stdMultiplier = sm
	} else {
		mrs.stdMultiplier = 2.0 // 預設 2 倍標準差
	}

	if rt, ok := strategyCfg["reversion_threshold"].(float64); ok {
		mrs.reversionThreshold = rt
	} else {
		mrs.reversionThreshold = 0.5 // 預設 0.5σ
	}
	mrs.orderAmount = signalStrategyOrderAmount(strategyCfg)
	mrs.slippage = signalStrategySlippage(strategyCfg)

	return mrs
}

// Name 回傳策略名稱
func (mrs *MeanReversionStrategy) Name() string {
	return mrs.name
}

// SetEventBus 設置事件總線
func (mrs *MeanReversionStrategy) SetEventBus(bus EventBus) {
	mrs.mu.Lock()
	defer mrs.mu.Unlock()
	mrs.eventBus = bus
}

// Initialize 初始化策略
func (mrs *MeanReversionStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	return nil
}

// Start 啟动策略
func (mrs *MeanReversionStrategy) Start(ctx context.Context) error {
	mrs.mu.Lock()
	mrs.isRunning = true
	mrs.mu.Unlock()

	logger.Info("✅ [%s] 均值回归策略已啟动自动交易 (周期:%d, 標准差倍數:%.2f, 单笔金额:%.2f)",
		mrs.name, mrs.period, mrs.stdMultiplier, mrs.orderAmount)
	return nil
}

// Stop 停止策略
func (mrs *MeanReversionStrategy) Stop() error {
	mrs.mu.Lock()
	mrs.isRunning = false
	mrs.mu.Unlock()

	if mrs.cancel != nil {
		mrs.cancel()
	}
	return nil
}

// IsRunning 回傳策略是否已成功啟动
func (mrs *MeanReversionStrategy) IsRunning() bool {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()
	return mrs.isRunning
}

// addPrice 新增價格
func (mrs *MeanReversionStrategy) addPrice(price float64) {
	mrs.mu.Lock()
	defer mrs.mu.Unlock()

	mrs.priceHistory = append(mrs.priceHistory, price)

	// 保持历史記錄
	maxHistory := mrs.period * 2
	if len(mrs.priceHistory) > maxHistory {
		// 使用 copy 而不是切片截取，避免記憶體泄漏
		newHistory := make([]float64, maxHistory)
		copy(newHistory, mrs.priceHistory[len(mrs.priceHistory)-maxHistory:])
		mrs.priceHistory = newHistory
	}
}

// calculateMA 计算移动平均
func (mrs *MeanReversionStrategy) calculateMA() float64 {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()

	if len(mrs.priceHistory) < mrs.period {
		return 0
	}

	start := len(mrs.priceHistory) - mrs.period
	prices := mrs.priceHistory[start:]

	var sum float64
	for _, price := range prices {
		sum += price
	}

	return sum / float64(len(prices))
}

// calculateStdDev 计算標准差
func (mrs *MeanReversionStrategy) calculateStdDev(ma float64) float64 {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()

	if len(mrs.priceHistory) < mrs.period {
		return 0
	}

	start := len(mrs.priceHistory) - mrs.period
	prices := mrs.priceHistory[start:]

	var variance float64
	for _, price := range prices {
		diff := price - ma
		variance += diff * diff
	}

	stdDev := math.Sqrt(variance / float64(len(prices)))
	return stdDev
}

// calculateBollingerBands 计算布林带
func (mrs *MeanReversionStrategy) calculateBollingerBands() (upper, middle, lower float64) {
	middle = mrs.calculateMA()
	if middle == 0 {
		return 0, 0, 0
	}

	stdDev := mrs.calculateStdDev(middle)
	upper = middle + stdDev*mrs.stdMultiplier
	lower = middle - stdDev*mrs.stdMultiplier

	return upper, middle, lower
}

// OnPriceChange 價格變化处理
func (mrs *MeanReversionStrategy) OnPriceChange(price float64) error {
	mrs.mu.RLock()
	if !mrs.isRunning || mrs.isPaused || mrs.activeOrder != nil {
		mrs.mu.RUnlock()
		return nil
	}
	mrs.mu.RUnlock()
	mrs.addPrice(price)

	upper, middle, lower := mrs.calculateBollingerBands()
	if upper == 0 || middle == 0 || lower == 0 {
		return nil
	}
	stdDev := mrs.calculateStdDev(middle)

	mrs.mu.Lock()
	defer mrs.mu.Unlock()

	// 價格低於下轨：買入信号
	if price < lower && mrs.position == nil {
		logger.Info("📊 [%s] 價格低於下轨，買入信号: 價格=%.2f, 下轨=%.2f", mrs.name, price, lower)
		return mrs.placeSignalOrder(signalActionOpenLong, price)
	}

	// 價格高於上轨：賣出信号
	if price > upper && mrs.position != nil {
		logger.Info("📊 [%s] 價格高於上轨，賣出信号: 價格=%.2f, 上轨=%.2f", mrs.name, price, upper)
		return mrs.placeSignalOrder(signalActionCloseLong, price)
	}

	// 價格回归中轨：平倉
	if mrs.position != nil {
		deviation := math.Abs(price - middle)
		if deviation < stdDev*mrs.reversionThreshold {
			logger.Info("📊 [%s] 價格回归中轨，平倉: 價格=%.2f, 中轨=%.2f", mrs.name, price, middle)
			return mrs.placeSignalOrder(signalActionCloseLong, price)
		}
	}

	return nil
}

func (mrs *MeanReversionStrategy) placeSignalOrder(action string, price float64) error {
	if mrs.executor == nil {
		return nil
	}
	symbol := signalStrategySymbol(mrs.cfg, mrs.strategyCfg)
	priceDecimals := signalPriceDecimals(mrs.exchange)
	qtyDecimals := signalQuantityDecimals(mrs.exchange)

	side := "BUY"
	orderPrice := signalRound(price*(1+mrs.slippage), priceDecimals)
	quantity := signalFloor(mrs.orderAmount/orderPrice, qtyDecimals)
	reduceOnly := false

	if action == signalActionCloseLong {
		if mrs.position == nil || mrs.position.Size <= 0 {
			return nil
		}
		side = "SELL"
		orderPrice = signalRound(price*(1-mrs.slippage), priceDecimals)
		quantity = signalFloor(mrs.position.Size, qtyDecimals)
		reduceOnly = signalIsFuturesMarket(mrs.cfg)
	}
	if orderPrice <= 0 || quantity <= 0 {
		logger.Warn("⚠️ [%s] 均值回归下单数量无效: action=%s price=%.8f qty=%.8f", mrs.name, action, orderPrice, quantity)
		return nil
	}

	order, err := mrs.executor.PlaceOrder(&position.OrderRequest{
		Symbol:        symbol,
		Side:          side,
		Price:         orderPrice,
		Quantity:      quantity,
		PriceDecimals: priceDecimals,
		ReduceOnly:    reduceOnly,
		PostOnly:      false,
		ClientOrderID: signalClientOrderID(mrs.name, action),
		StrategyName:  mrs.name,
		StrategyType:  "mean_reversion",
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
	mrs.activeOrder = tracked
	mrs.pendingAction = action
	if signalOrderStatusFilled(order.Status) {
		mrs.applyFilledOrderLocked(tracked, order.Quantity, order.Price)
	}
	return nil
}

// OnOrderUpdate 订單更新处理
func (mrs *MeanReversionStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	if update == nil {
		return nil
	}
	mrs.mu.Lock()
	defer mrs.mu.Unlock()

	if !signalOrderMatches(mrs.activeOrder, update) {
		return nil
	}
	mrs.activeOrder.Status = update.Status
	if signalOrderStatusTerminal(update.Status) {
		mrs.activeOrder = nil
		mrs.pendingAction = ""
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
		fillQty = mrs.activeOrder.Quantity
	}
	mrs.applyFilledOrderLocked(mrs.activeOrder, fillQty, fillPrice)
	return nil
}

func (mrs *MeanReversionStrategy) applyFilledOrderLocked(order *Order, quantity, price float64) {
	if order == nil {
		return
	}
	if price <= 0 {
		price = order.Price
	}
	if quantity <= 0 {
		quantity = order.Quantity
	}
	switch mrs.pendingAction {
	case signalActionOpenLong:
		mrs.entryPrice = price
		mrs.position = &Position{
			Symbol:       order.Symbol,
			Size:         quantity,
			EntryPrice:   price,
			CurrentPrice: price,
			PnL:          0,
		}
	case signalActionCloseLong:
		if mrs.position != nil && mrs.entryPrice > 0 {
			mrs.stats.TotalPnL += (price - mrs.entryPrice) * mrs.position.Size
		}
		mrs.position = nil
		mrs.entryPrice = 0
		mrs.stats.TotalTrades++
	}
	mrs.stats.TotalVolume += quantity * price
	mrs.activeOrder = nil
	mrs.pendingAction = ""
}

// GetPositions 獲取持倉
func (mrs *MeanReversionStrategy) GetPositions() []*Position {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()

	if mrs.position == nil {
		return []*Position{}
	}

	return []*Position{mrs.position}
}

// GetOrders 獲取訂單
func (mrs *MeanReversionStrategy) GetOrders() []*Order {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()
	if mrs.activeOrder == nil {
		return []*Order{}
	}
	return []*Order{mrs.activeOrder}
}

// GetStatistics 獲取统计
func (mrs *MeanReversionStrategy) GetStatistics() *StrategyStatistics {
	mrs.mu.RLock()
	defer mrs.mu.RUnlock()
	stats := *mrs.stats
	return &stats
}

// GetVisualizationData 獲取策略可视化數據
func (mrs *MeanReversionStrategy) GetVisualizationData() map[string]interface{} {
	data := make(map[string]interface{})

	// 计算布林带
	upper, middle, lower := mrs.calculateBollingerBands()
	data["upperBand"] = upper
	data["middleBand"] = middle
	data["lowerBand"] = lower

	// 当前价格
	currentPrice := 0.0
	mrs.mu.RLock()
	if len(mrs.priceHistory) > 0 {
		currentPrice = mrs.priceHistory[len(mrs.priceHistory)-1]
	}
	hasPosition := mrs.position != nil
	entryPrice := mrs.entryPrice
	isRunning := mrs.isRunning
	pendingAction := mrs.pendingAction
	mrs.mu.RUnlock()
	data["currentPrice"] = currentPrice

	// 价格在布林带中的位置（百分比）
	if upper > lower && currentPrice > 0 {
		positionPercent := ((currentPrice - lower) / (upper - lower)) * 100
		data["positionInBand"] = positionPercent

		// 判断是否触及上下轨
		data["touchesUpperBand"] = currentPrice >= upper*0.99 // 允许1%误差
		data["touchesLowerBand"] = currentPrice <= lower*1.01
	} else {
		data["positionInBand"] = 50.0 // 默认中间位置
		data["touchesUpperBand"] = false
		data["touchesLowerBand"] = false
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

	// 策略参数
	data["period"] = mrs.period
	data["stdMultiplier"] = mrs.stdMultiplier
	data["reversionThreshold"] = mrs.reversionThreshold
	data["executionMode"] = "auto_trade"
	data["autoTradingEnabled"] = true
	data["isRunning"] = isRunning
	data["orderAmount"] = mrs.orderAmount
	data["pendingAction"] = pendingAction

	// 买入/卖出信号判断
	if upper > 0 && lower > 0 && currentPrice > 0 {
		// 买入信号：价格触及下轨
		buySignal := currentPrice <= lower*1.01 && !hasPosition
		data["buySignal"] = buySignal

		// 卖出信号：价格触及上轨或回归均值
		sellSignal := (currentPrice >= upper*0.99 || currentPrice >= middle) && hasPosition
		data["sellSignal"] = sellSignal

		// 距离买入/卖出信号的距离
		if !hasPosition {
			distanceToBuy := ((currentPrice - lower) / currentPrice) * 100
			data["distanceToBuy"] = distanceToBuy
		} else {
			distanceToSell := ((upper - currentPrice) / currentPrice) * 100
			distanceToMean := ((middle - currentPrice) / currentPrice) * 100
			data["distanceToSell"] = math.Min(distanceToSell, distanceToMean)
		}
	}

	return data
}
