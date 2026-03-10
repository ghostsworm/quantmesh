package position

import (
	"context"
	"sort"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// SmartOrderManager 智能挂单管理器
// 定期检查订单状态，动态调整挂单位置，避免订单偏离当前价格太远
type SmartOrderManager struct {
	spm *SuperPositionManager
	cfg *config.SmartOrderConfig

	// 运行控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
}

// NewSmartOrderManager 创建智能挂单管理器
func NewSmartOrderManager(spm *SuperPositionManager, cfg *config.SmartOrderConfig) *SmartOrderManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &SmartOrderManager{
		spm:    spm,
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start 启动智能挂单管理器
func (som *SmartOrderManager) Start() {
	if !som.cfg.Enabled {
		logger.Info("🧠 [%s] 智能挂单未启用", som.spm.logPrefix())
		return
	}

	som.once.Do(func() {
		som.wg.Add(1)
		go som.run()
		logger.Info("✅ [%s] 智能挂单已启动 (MaxOpenOrders=%d, Distance=%.1f)",
			som.spm.logPrefix(), som.getMaxOpenOrders(), som.getMaxDistance())
	})
}

// Stop 停止智能挂单管理器
func (som *SmartOrderManager) Stop() {
	if som.cancel != nil {
		som.cancel()
	}
	som.wg.Wait()
	logger.Info("🛑 [%s] 智能挂单已停止", som.spm.logPrefix())
}

// run 主循环
func (som *SmartOrderManager) run() {
	defer som.wg.Done()

	// 检查间隔：默认 60 秒
	checkInterval := 60 * time.Second
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	// 启动时立即检查一次
	som.checkAndAdjustOrders()

	for {
		select {
		case <-som.ctx.Done():
			return
		case <-ticker.C:
			som.checkAndAdjustOrders()
		}
	}
}

// checkAndAdjustOrders 检查并调整订单
func (som *SmartOrderManager) checkAndAdjustOrders() {
	currentPrice := som.spm.lastMarketPrice.Load().(float64)
	if currentPrice <= 0 {
		return
	}

	// 检查是否需要调整订单
	if som.shouldAdjustOrders(currentPrice) {
		logger.Info("🧠 [%s] 智能挂单：检测到价格变化，开始调整订单", som.spm.logPrefix())
		som.adjustOrders(currentPrice)
	}
}

// shouldAdjustOrders 判断是否需要调整订单
func (som *SmartOrderManager) shouldAdjustOrders(currentPrice float64) bool {
	maxOrders := som.getMaxOpenOrders()
	maxDistance := som.getMaxDistance()

	// 统计开仓单数量
	var openOrderCount int
	direction := "LONG"
	if som.spm.isShort() {
		direction = "SHORT"
	}

	som.spm.slots.Range(func(key, value interface{}) bool {
		_ = key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.RLock()
		defer slot.mu.RUnlock()

		var isOpeningOrder bool
		if direction == "LONG" && slot.OrderSide == "BUY" {
			isOpeningOrder = true
		} else if direction == "SHORT" && slot.OrderSide == "SELL" {
			isOpeningOrder = true
		}

		if isOpeningOrder && (slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed) {
			openOrderCount++
		}
		return true
	})

	// 开仓单数量超过上限：需要撤掉最远的
	if maxOrders > 0 && openOrderCount > maxOrders {
		return true
	}

	// 检查是否有订单超出最大距离
	if maxDistance <= 0 {
		return false
	}

	hasFarOrders := false
	som.spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.RLock()
		defer slot.mu.RUnlock()

		var isOpeningOrder bool
		if direction == "LONG" && slot.OrderSide == "BUY" {
			isOpeningOrder = true
		} else if direction == "SHORT" && slot.OrderSide == "SELL" {
			isOpeningOrder = true
		}

		if isOpeningOrder && (slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed) {
			var distance float64
			if direction == "LONG" {
				distance = currentPrice - price
			} else {
				distance = price - currentPrice
			}

			if distance > maxDistance {
				hasFarOrders = true
				logger.Debug("🧠 [%s] 发现远距离订单: %.2f (距离: %.2f)",
					som.spm.logPrefix(), price, distance)
				return false
			}
		}
		return true
	})

	return hasFarOrders
}

// adjustOrders 调整订单
func (som *SmartOrderManager) adjustOrders(currentPrice float64) {
	maxOrders := som.getMaxOpenOrders()
	maxDistance := som.getMaxDistance()

	// 1. 优先处理：开仓单数量超过上限，撤掉最远的（做多撤高價買單，做空撤低價賣單）
	if maxOrders > 0 {
		som.spm.CancelExcessOpenOrders(maxOrders)
	}

	// 2. 收集超出距离的订单ID并撤销
	if maxDistance <= 0 {
		return
	}

	direction := "LONG"
	if som.spm.isShort() {
		direction = "SHORT"
	}

	var orderIDsToCancel []int64
	som.spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.RLock()
		defer slot.mu.RUnlock()

		var isOpeningOrder bool
		if direction == "LONG" && slot.OrderSide == "BUY" {
			isOpeningOrder = true
		} else if direction == "SHORT" && slot.OrderSide == "SELL" {
			isOpeningOrder = true
		}

		if isOpeningOrder && (slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed) {
			var distance float64
			if direction == "LONG" {
				distance = currentPrice - price
			} else {
				distance = price - currentPrice
			}

			if distance > maxDistance {
				orderIDsToCancel = append(orderIDsToCancel, slot.OrderID)
			}
		}
		return true
	})

	if len(orderIDsToCancel) > 0 {
		logger.Info("🧠 [%s] 撤销 %d 个远距离订单", som.spm.logPrefix(), len(orderIDsToCancel))
		som.spm.executor.BatchCancelOrders(orderIDsToCancel)
	}
}

// getMaxOpenOrders 获取最大开仓订单数
func (som *SmartOrderManager) getMaxOpenOrders() int {
	if som.cfg.MaxOpenOrders > 0 {
		return som.cfg.MaxOpenOrders
	}
	return 3 // 默认 3 个
}

// getMaxDistance 获取最大距离
func (som *SmartOrderManager) getMaxDistance() float64 {
	priceInterval := som.spm.config.Trading.PriceInterval
	if priceInterval <= 0 {
		return 0
	}

	// OpenOrderDistance 是间隔数的倍数
	distance := som.cfg.OpenOrderDistance * priceInterval
	if distance <= 0 {
		// 默认 5 个间隔
		distance = 5 * priceInterval
	}
	return distance
}

// CalculateOpenSlots 计算应该挂开仓单的槽位
// 核心思想：只在当前价附近的槽位挂单，避免一次性挂太多
func (som *SmartOrderManager) CalculateOpenSlots(
	currentPrice float64,
	allSlotPrices []float64,
	direction string, // LONG/SHORT
) []float64 {
	if !som.cfg.Enabled {
		// 未启用：返回所有槽位
		return allSlotPrices
	}

	// 1. 按价格排序
	sortedPrices := make([]float64, len(allSlotPrices))
	copy(sortedPrices, allSlotPrices)
	som.sortFloat64s(sortedPrices, direction)

	// 2. 找到当前价附近的槽位
	var nearbySlots []float64
	maxDistance := som.cfg.OpenOrderDistance * som.spm.config.Trading.PriceInterval
	if maxDistance <= 0 {
		maxDistance = 3 * som.spm.config.Trading.PriceInterval // 默认3个间隔
	}

	for _, price := range sortedPrices {
		var distance float64
		if direction == "LONG" {
			// 做多：关注低于当前价的槽位
			if price < currentPrice {
				distance = currentPrice - price
				if distance <= maxDistance {
					nearbySlots = append(nearbySlots, price)
				}
			}
		} else {
			// 做空：关注高于当前价的槽位
			if price > currentPrice {
				distance = price - currentPrice
				if distance <= maxDistance {
					nearbySlots = append(nearbySlots, price)
				}
			}
		}
	}

	// 3. 限制最大数量
	maxOrders := som.cfg.MaxOpenOrders
	if maxOrders <= 0 {
		maxOrders = 3 // 默认最多3个开仓单
	}

	if len(nearbySlots) > maxOrders {
		// 取最近的几个槽位
		if direction == "LONG" {
			// 做多：取最高的几个（最接近当前价）
			nearbySlots = nearbySlots[len(nearbySlots)-maxOrders:]
		} else {
			// 做空：取最低的几个（最接近当前价）
			nearbySlots = nearbySlots[:maxOrders]
		}
	}

	logger.Debug("🧠 [智能挂单] 当前价:%.2f 方式:%s 候选槽位:%d 选中槽位:%d",
		currentPrice, direction, len(allSlotPrices), len(nearbySlots))

	return nearbySlots
}

// ShouldAddNewSlot 判断是否应该添加新的挂单槽位
// 当价格移动导致原有槽位成交后，判断是否添加新槽位
func (som *SmartOrderManager) ShouldAddNewSlot(
	currentPrice float64,
	direction string,
) bool {
	if !som.cfg.Enabled || !som.cfg.ProgressivePlacement {
		return true // 未启用渐进式，允许添加
	}

	// 统计当前开仓单数量
	var openOrderCount int
	spm := som.spm

	spm.slots.Range(func(key, value interface{}) bool {
		_ = key.(float64) // 价格未使用
		slot := value.(*InventorySlot)

		slot.mu.RLock()
		defer slot.mu.RUnlock()

		// 检查是否有有效的开仓单
		if slot.OrderStatus == OrderStatusPlaced ||
			slot.OrderStatus == OrderStatusConfirmed {
			// 判断是否是开仓单
			var isOpeningOrder bool
			if direction == "LONG" && slot.OrderSide == "BUY" {
				isOpeningOrder = true
			} else if direction == "SHORT" && slot.OrderSide == "SELL" {
				isOpeningOrder = true
			}

			if isOpeningOrder {
				openOrderCount++
			}
		}
		return true
	})

	// 如果开仓单数量少于最大值，允许添加
	maxOrders := som.cfg.MaxOpenOrders
	if maxOrders <= 0 {
		maxOrders = 3
	}

	return openOrderCount < maxOrders
}

// sortFloat64s 按方向排序
func (som *SmartOrderManager) sortFloat64s(arr []float64, direction string) {
	if direction == "LONG" {
		// 做多：升序
		sort.Float64s(arr)
	} else {
		// 做空：降序
		sort.Slice(arr, func(i, j int) bool {
			return arr[i] > arr[j]
		})
	}
}

// FilterSlotsByMaxOpenOrders 根據 max_open_orders 和 open_order_distance 篩選開倉槽位
// 用於開倉管理中的掛單數限制，避免單向做多/做空時過多開倉委託佔用保證金
func FilterSlotsByMaxOpenOrders(slotPrices []float64, currentPrice, priceInterval float64, maxOrders int, maxDistance float64, direction string) []float64 {
	if maxOrders <= 0 || len(slotPrices) == 0 {
		return slotPrices
	}
	if maxDistance <= 0 {
		maxDistance = 3
	}
	dist := maxDistance * priceInterval

	sorted := make([]float64, len(slotPrices))
	copy(sorted, slotPrices)
	if direction == "LONG" {
		sort.Float64s(sorted)
	} else {
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] > sorted[j] })
	}

	var nearby []float64
	for _, p := range sorted {
		if direction == "LONG" {
			if p < currentPrice && currentPrice-p <= dist {
				nearby = append(nearby, p)
			}
		} else {
			if p > currentPrice && p-currentPrice <= dist {
				nearby = append(nearby, p)
			}
		}
	}

	if len(nearby) <= maxOrders {
		return nearby
	}
	// 取最接近當前價的 maxOrders 個：LONG 取最高的幾個（下方最近），SHORT 取最低的幾個（上方最近）
	return nearby[len(nearby)-maxOrders:]
}
