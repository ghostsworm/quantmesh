package strategy

import (
	"context"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

// GridStrategy 網格策略包装
type GridStrategy struct {
	name     string
	cfg      *config.Config
	executor position.OrderExecutorInterface
	exchange position.IExchange
	manager  *position.SuperPositionManager
	eventBus EventBus

	mu        sync.RWMutex
	ctx       context.Context
	isRunning bool
	isPaused  bool // 暂停標志
}

// NewGridStrategy 創建網格策略
func NewGridStrategy(
	name string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	manager *position.SuperPositionManager,
) *GridStrategy {
	return &GridStrategy{
		name:     name,
		cfg:      cfg,
		executor: executor,
		exchange: exchange,
		manager:  manager,
		ctx:      context.Background(),
	}
}

// Name 回傳策略名稱
func (gs *GridStrategy) Name() string {
	return gs.name
}

// SetEventBus 設置事件總線
func (gs *GridStrategy) SetEventBus(bus EventBus) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.eventBus = bus
}

// Initialize 初始化策略
func (gs *GridStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	// 已在構造函數中初始化
	return nil
}

// Start 啟动策略
func (gs *GridStrategy) Start(ctx context.Context) error {
	gs.mu.Lock()
	gs.ctx = ctx
	gs.isRunning = true
	gs.mu.Unlock()

	logger.Info("✅ [%s] 網格策略已啟动", gs.name)
	return nil
}

// Stop 停止策略
func (gs *GridStrategy) Stop() error {
	gs.mu.Lock()
	gs.isRunning = false
	gs.mu.Unlock()

	logger.Info("⏹️ [%s] 網格策略已停止", gs.name)
	return nil
}

// IsRunning 回傳策略是否已成功啟动
func (gs *GridStrategy) IsRunning() bool {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return gs.isRunning
}

// OnPriceChange 價格變化处理
func (gs *GridStrategy) OnPriceChange(price float64) error {
	gs.mu.Lock()
	if gs.isPaused {
		gs.mu.Unlock()
		return nil
	}
	gs.mu.Unlock()

	// 調用 SuperPositionManager 的 AdjustOrders
	return gs.manager.AdjustOrders(price)
}

// OnOrderUpdate 订單更新处理
func (gs *GridStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// 調用 SuperPositionManager 的 OnOrderUpdate（需要傳遞值類型）
	gs.manager.OnOrderUpdate(*update)
	return nil
}

// GetPositions 獲取持倉
func (gs *GridStrategy) GetPositions() []*Position {
	if gs.manager == nil {
		return nil
	}
	symbol := ""
	if gs.cfg != nil {
		symbol = gs.cfg.Trading.Symbol
	}
	slots := gs.manager.GetAllSlotsDetailed()
	var totalQty float64
	var cost float64
	for _, s := range slots {
		if s.PositionQty <= 0 {
			continue
		}
		totalQty += s.PositionQty
		cost += s.Price * s.PositionQty
	}
	if totalQty <= 0 {
		return []*Position{}
	}
	entry := cost / totalQty
	ctx := gs.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	cur := 0.0
	if gs.exchange != nil && symbol != "" {
		if p, err := gs.exchange.GetLatestPrice(ctx, symbol); err == nil {
			cur = p
		}
	}
	pnl := gs.manager.GetUnrealizedPnL(cur)
	return []*Position{{
		Symbol:       symbol,
		Size:         totalQty,
		EntryPrice:   entry,
		CurrentPrice: cur,
		PnL:          pnl,
	}}
}

// GetOrders 獲取訂單
func (gs *GridStrategy) GetOrders() []*Order {
	if gs.manager == nil {
		return nil
	}
	symbol := ""
	if gs.cfg != nil {
		symbol = gs.cfg.Trading.Symbol
	}
	slots := gs.manager.GetAllSlotsDetailed()
	out := make([]*Order, 0)
	for _, s := range slots {
		if s.OrderID == 0 {
			continue
		}
		out = append(out, &Order{
			OrderID:  s.OrderID,
			Symbol:   symbol,
			Side:     s.OrderSide,
			Price:    s.OrderPrice,
			Quantity: s.OrderFilledQty,
			Status:   s.OrderStatus,
		})
	}
	return out
}

// GetStatistics 獲取统计
func (gs *GridStrategy) GetStatistics() *StrategyStatistics {
	if gs.manager == nil {
		return &StrategyStatistics{}
	}
	ctx := gs.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	sym := ""
	if gs.cfg != nil {
		sym = gs.cfg.Trading.Symbol
	}
	buyQ := gs.manager.GetTotalBuyQty()
	sellQ := gs.manager.GetTotalSellQty()
	st := &StrategyStatistics{
		TotalVolume: buyQ + sellQ,
	}
	if gs.exchange != nil && sym != "" {
		if p, err := gs.exchange.GetLatestPrice(ctx, sym); err == nil {
			st.TotalPnL = gs.manager.GetUnrealizedPnL(p)
		}
	}
	return st
}

// GetManager 獲取 SuperPositionManager（用於外部访问）
func (gs *GridStrategy) GetManager() *position.SuperPositionManager {
	return gs.manager
}

// GetVisualizationData 獲取策略可视化數據
func (gs *GridStrategy) GetVisualizationData() map[string]interface{} {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	data := make(map[string]interface{})

	if gs.manager == nil {
		return data
	}

	// 獲取所有槽位信息
	slots := gs.manager.GetAllSlotsDetailed()
	slotData := make([]map[string]interface{}, 0, len(slots))

	var minPrice, maxPrice float64
	filledCount := 0
	emptyCount := 0

	for i, slot := range slots {
		if i == 0 {
			minPrice = slot.Price
			maxPrice = slot.Price
		} else {
			if slot.Price < minPrice {
				minPrice = slot.Price
			}
			if slot.Price > maxPrice {
				maxPrice = slot.Price
			}
		}

		if slot.PositionStatus == "FILLED" {
			filledCount++
		} else {
			emptyCount++
		}

		slotData = append(slotData, map[string]interface{}{
			"price":          slot.Price,
			"positionStatus": slot.PositionStatus,
			"positionQty":    slot.PositionQty,
			"orderID":        slot.OrderID,
			"orderSide":      slot.OrderSide,
			"orderStatus":    slot.OrderStatus,
			"orderPrice":     slot.OrderPrice,
			"slotStatus":     slot.SlotStatus,
		})
	}

	data["slots"] = slotData
	data["slotCount"] = len(slots)
	data["filledCount"] = filledCount
	data["emptyCount"] = emptyCount

	// 网格价格区间
	if minPrice > 0 && maxPrice > 0 {
		data["minPrice"] = minPrice
		data["maxPrice"] = maxPrice
		data["priceRange"] = maxPrice - minPrice
	}

	// 价格间隔
	if gs.cfg != nil && gs.cfg.Trading.PriceInterval > 0 {
		data["priceInterval"] = gs.cfg.Trading.PriceInterval
	}

	// 当前价格（如果有的话）
	// 注意：SuperPositionManager可能没有直接暴露当前价格的方法
	// 这里可以从槽位中推断或从其他地方获取

	return data
}
