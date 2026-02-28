package position

import (
	"sort"

	"quantmesh/config"
	"quantmesh/logger"
)

// SmartOrderManager 智能挂单管理器
type SmartOrderManager struct {
	spm *SuperPositionManager
	cfg *config.SmartOrderConfig
}

// NewSmartOrderManager 创建智能挂单管理器
func NewSmartOrderManager(spm *SuperPositionManager, cfg *config.SmartOrderConfig) *SmartOrderManager {
	return &SmartOrderManager{
		spm: spm,
		cfg: cfg,
	}
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
