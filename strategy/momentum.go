package strategy

import (
	"context"
	"math"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

// MomentumStrategy 动量策略
type MomentumStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg map[string]interface{}

	// 價格历史
	priceHistory []float64
	mu           sync.RWMutex

	// RSI 相关
	rsiPeriod         int
	rsiValues         []float64
	overbought        float64
	oversold          float64
	momentumThreshold float64
	orderAmount       float64
	slippage          float64

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

// NewMomentumStrategy 創建动量策略
func NewMomentumStrategy(
	name string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *MomentumStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	ms := &MomentumStrategy{
		name:         name,
		cfg:          cfg,
		executor:     executor,
		exchange:     exchange,
		strategyCfg:  strategyCfg,
		priceHistory: make([]float64, 0, 100),
		rsiValues:    make([]float64, 0, 100),
		ctx:          ctx,
		cancel:       cancel,
		stats:        &StrategyStatistics{},
	}

	// 從配置中读取参數
	if rp, ok := strategyCfg["rsi_period"].(int); ok {
		ms.rsiPeriod = rp
	} else {
		ms.rsiPeriod = 14 // 預設 14
	}

	if ob, ok := strategyCfg["overbought"].(float64); ok {
		ms.overbought = ob
	} else {
		ms.overbought = 70 // 預設 70
	}

	if os, ok := strategyCfg["oversold"].(float64); ok {
		ms.oversold = os
	} else {
		ms.oversold = 30 // 預設 30
	}

	if mt, ok := strategyCfg["momentum_threshold"].(float64); ok {
		ms.momentumThreshold = mt
	} else {
		ms.momentumThreshold = 0.5 // 預設 0.5
	}
	ms.orderAmount = signalStrategyOrderAmount(strategyCfg)
	ms.slippage = signalStrategySlippage(strategyCfg)

	return ms
}

// Name 回傳策略名稱
func (ms *MomentumStrategy) Name() string {
	return ms.name
}

// SetEventBus 設置事件總線
func (ms *MomentumStrategy) SetEventBus(bus EventBus) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.eventBus = bus
}

// Initialize 初始化策略
func (ms *MomentumStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	return nil
}

// Start 啟动策略
func (ms *MomentumStrategy) Start(ctx context.Context) error {
	ms.mu.Lock()
	ms.isRunning = true
	ms.mu.Unlock()

	logger.Info("✅ [%s] 动量策略已啟动自动交易 (RSI周期:%d, 超買:%d, 超賣:%d, 单笔金额:%.2f)",
		ms.name, ms.rsiPeriod, int(ms.overbought), int(ms.oversold), ms.orderAmount)
	return nil
}

// Stop 停止策略
func (ms *MomentumStrategy) Stop() error {
	ms.mu.Lock()
	ms.isRunning = false
	ms.mu.Unlock()

	if ms.cancel != nil {
		ms.cancel()
	}
	return nil
}

// IsRunning 回傳策略是否已成功啟动
func (ms *MomentumStrategy) IsRunning() bool {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.isRunning
}

// addPrice 新增價格
func (ms *MomentumStrategy) addPrice(price float64) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.priceHistory = append(ms.priceHistory, price)

	// 保持历史記錄
	maxHistory := ms.rsiPeriod * 2
	if len(ms.priceHistory) > maxHistory {
		// 使用 copy 而不是切片截取，避免記憶體泄漏
		newHistory := make([]float64, maxHistory)
		copy(newHistory, ms.priceHistory[len(ms.priceHistory)-maxHistory:])
		ms.priceHistory = newHistory
	}
}

// calculateRSI 计算RSI
func (ms *MomentumStrategy) calculateRSI() float64 {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if len(ms.priceHistory) < ms.rsiPeriod+1 {
		return 50 // 預設中性
	}

	// 计算價格變化
	gains := make([]float64, 0)
	losses := make([]float64, 0)

	for i := len(ms.priceHistory) - ms.rsiPeriod; i < len(ms.priceHistory); i++ {
		if i == 0 {
			continue
		}
		change := ms.priceHistory[i] - ms.priceHistory[i-1]
		if change > 0 {
			gains = append(gains, change)
			losses = append(losses, 0)
		} else {
			gains = append(gains, 0)
			losses = append(losses, -change)
		}
	}

	// 计算平均上涨和平均下跌
	var avgGain, avgLoss float64
	for _, gain := range gains {
		avgGain += gain
	}
	for _, loss := range losses {
		avgLoss += loss
	}

	avgGain /= float64(len(gains))
	avgLoss /= float64(len(losses))

	if avgLoss == 0 {
		return 100 // 没有下跌，RSI為100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// OnPriceChange 價格變化处理
func (ms *MomentumStrategy) OnPriceChange(price float64) error {
	ms.mu.RLock()
	if !ms.isRunning || ms.isPaused || ms.activeOrder != nil {
		ms.mu.RUnlock()
		return nil
	}
	ms.mu.RUnlock()
	ms.addPrice(price)

	rsi := ms.calculateRSI()
	if rsi == 50 {
		return nil // 數據不足
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	// RSI < 30：超賣，買入信号
	if rsi < ms.oversold && ms.position == nil {
		logger.Info("📊 [%s] RSI超賣，買入信号: RSI=%.2f, 價格=%.2f", ms.name, rsi, price)
		return ms.placeSignalOrder(signalActionOpenLong, price)
	}

	// RSI > 70：超買，賣出信号
	if rsi > ms.overbought && ms.position != nil {
		logger.Info("📊 [%s] RSI超買，賣出信号: RSI=%.2f, 價格=%.2f", ms.name, rsi, price)
		return ms.placeSignalOrder(signalActionCloseLong, price)
	}

	return nil
}

func (ms *MomentumStrategy) placeSignalOrder(action string, price float64) error {
	if ms.executor == nil {
		return nil
	}
	symbol := signalStrategySymbol(ms.cfg, ms.strategyCfg)
	priceDecimals := signalPriceDecimals(ms.exchange)
	qtyDecimals := signalQuantityDecimals(ms.exchange)

	side := "BUY"
	orderPrice := signalRound(price*(1+ms.slippage), priceDecimals)
	quantity := signalFloor(ms.orderAmount/orderPrice, qtyDecimals)
	reduceOnly := false

	if action == signalActionCloseLong {
		if ms.position == nil || ms.position.Size <= 0 {
			return nil
		}
		side = "SELL"
		orderPrice = signalRound(price*(1-ms.slippage), priceDecimals)
		quantity = signalFloor(ms.position.Size, qtyDecimals)
		reduceOnly = signalIsFuturesMarket(ms.cfg)
	}
	if orderPrice <= 0 || quantity <= 0 || math.IsNaN(quantity) {
		logger.Warn("⚠️ [%s] 动量策略下单数量无效: action=%s price=%.8f qty=%.8f", ms.name, action, orderPrice, quantity)
		return nil
	}

	order, err := ms.executor.PlaceOrder(&position.OrderRequest{
		Symbol:        symbol,
		Side:          side,
		Price:         orderPrice,
		Quantity:      quantity,
		PriceDecimals: priceDecimals,
		ReduceOnly:    reduceOnly,
		PostOnly:      false,
		ClientOrderID: signalClientOrderID(ms.name, action),
		StrategyName:  ms.name,
		StrategyType:  "momentum",
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
	ms.activeOrder = tracked
	ms.pendingAction = action
	if signalOrderStatusFilled(order.Status) {
		ms.applyFilledOrderLocked(tracked, order.Quantity, order.Price)
	}
	return nil
}

// OnOrderUpdate 订單更新处理
func (ms *MomentumStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	if update == nil {
		return nil
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()

	if !signalOrderMatches(ms.activeOrder, update) {
		return nil
	}
	ms.activeOrder.Status = update.Status
	if signalOrderStatusTerminal(update.Status) {
		ms.activeOrder = nil
		ms.pendingAction = ""
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
		fillQty = ms.activeOrder.Quantity
	}
	ms.applyFilledOrderLocked(ms.activeOrder, fillQty, fillPrice)
	return nil
}

func (ms *MomentumStrategy) applyFilledOrderLocked(order *Order, quantity, price float64) {
	if order == nil {
		return
	}
	if price <= 0 {
		price = order.Price
	}
	if quantity <= 0 {
		quantity = order.Quantity
	}
	switch ms.pendingAction {
	case signalActionOpenLong:
		ms.entryPrice = price
		ms.position = &Position{
			Symbol:       order.Symbol,
			Size:         quantity,
			EntryPrice:   price,
			CurrentPrice: price,
			PnL:          0,
		}
	case signalActionCloseLong:
		if ms.position != nil && ms.entryPrice > 0 {
			ms.stats.TotalPnL += (price - ms.entryPrice) * ms.position.Size
		}
		ms.position = nil
		ms.entryPrice = 0
		ms.stats.TotalTrades++
	}
	ms.stats.TotalVolume += quantity * price
	ms.activeOrder = nil
	ms.pendingAction = ""
}

// GetPositions 獲取持倉
func (ms *MomentumStrategy) GetPositions() []*Position {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	if ms.position == nil {
		return []*Position{}
	}

	return []*Position{ms.position}
}

// GetOrders 獲取訂單
func (ms *MomentumStrategy) GetOrders() []*Order {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	if ms.activeOrder == nil {
		return []*Order{}
	}
	return []*Order{ms.activeOrder}
}

// GetStatistics 獲取统计
func (ms *MomentumStrategy) GetStatistics() *StrategyStatistics {
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	stats := *ms.stats
	return &stats
}

// GetVisualizationData 獲取策略可视化數據
func (ms *MomentumStrategy) GetVisualizationData() map[string]interface{} {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	currentPrice := 0.0
	if len(ms.priceHistory) > 0 {
		currentPrice = ms.priceHistory[len(ms.priceHistory)-1]
	}
	hasPosition := ms.position != nil
	entryPrice := ms.entryPrice
	pendingAction := ms.pendingAction

	return map[string]interface{}{
		"currentPrice":       currentPrice,
		"rsiPeriod":          ms.rsiPeriod,
		"overbought":         ms.overbought,
		"oversold":           ms.oversold,
		"momentumThreshold":  ms.momentumThreshold,
		"hasPosition":        hasPosition,
		"entryPrice":         entryPrice,
		"executionMode":      "auto_trade",
		"autoTradingEnabled": true,
		"isRunning":          ms.isRunning,
		"orderAmount":        ms.orderAmount,
		"pendingAction":      pendingAction,
	}
}
