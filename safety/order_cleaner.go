package safety

import (
	"context"
	"opensqt/config"
	"opensqt/logger"
	"reflect"
	"sort"
	"time"
)

// OrderCleanerSlotInfo 订单清理所需的槽位信息
type OrderCleanerSlotInfo struct {
	Price       float64
	OrderID     int64
	OrderSide   string
	OrderStatus string
}

// IOrderExecutor 订单执行器接口（用于批量撤单）
type IOrderExecutor interface {
	BatchCancelOrders(orderIDs []int64) error
}

// IOrderCleanerPositionManager 订单清理所需的仓位管理器接口
type IOrderCleanerPositionManager interface {
	// 遍历所有槽位
	IterateSlots(fn func(price float64, slot interface{}) bool)
	// 更新槽位状态
	UpdateSlotOrderStatus(price float64, status string)
}

// OrderCleaner 订单清理器
type OrderCleaner struct {
	cfg      *config.Config
	executor IOrderExecutor
	pm       IOrderCleanerPositionManager
}

// NewOrderCleaner 创建订单清理器
func NewOrderCleaner(cfg *config.Config, executor IOrderExecutor, pm IOrderCleanerPositionManager) *OrderCleaner {
	return &OrderCleaner{
		cfg:      cfg,
		executor: executor,
		pm:       pm,
	}
}

// Start 启动订单清理协程
func (oc *OrderCleaner) Start(ctx context.Context) {
	go func() {
		cleanupInterval := time.Duration(oc.cfg.Timing.OrderCleanupInterval) * time.Second
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("⏹️ 订单清理协程已停止")
				return
			case <-ticker.C:
				oc.CleanupOrders()
			}
		}
	}()
	logger.Info("✅ 订单清理协程已启动")
}

// CleanupOrders 清理订单
func (oc *OrderCleaner) CleanupOrders() {
	// 订单状态常量
	const (
		OrderStatusPlaced          = "PLACED"
		OrderStatusConfirmed       = "CONFIRMED"
		OrderStatusCancelRequested = "CANCEL_REQUESTED"
	)

	// 统计当前订单数
	totalOrders := 0
	var buyOrders []struct {
		Price   float64
		OrderID int64
	}
	var sellOrders []struct {
		Price   float64
		OrderID int64
	}

	oc.pm.IterateSlots(func(price float64, slotRaw interface{}) bool {
		// 使用反射提取槽位字段
		v := reflect.ValueOf(slotRaw)
		if v.Kind() != reflect.Struct {
			return true
		}

		// 提取字段
		getStringField := func(name string) string {
			field := v.FieldByName(name)
			if field.IsValid() && field.Kind() == reflect.String {
				return field.String()
			}
			return ""
		}

		getInt64Field := func(name string) int64 {
			field := v.FieldByName(name)
			if field.IsValid() && field.CanInt() {
				return field.Int()
			}
			return 0
		}

		orderID := getInt64Field("OrderID")
		orderSide := getStringField("OrderSide")
		orderStatus := getStringField("OrderStatus")

		// 🔥 修复：排除部分成交的订单（PARTIALLY_FILLED不能撤销，会造成资金悬空）
		if orderStatus == OrderStatusPlaced || orderStatus == OrderStatusConfirmed {
			totalOrders++
			if orderSide == "BUY" {
				buyOrders = append(buyOrders, struct {
					Price   float64
					OrderID int64
				}{Price: price, OrderID: orderID})
			} else if orderSide == "SELL" {
				sellOrders = append(sellOrders, struct {
					Price   float64
					OrderID int64
				}{Price: price, OrderID: orderID})
			}
		}
		return true
	})

	threshold := oc.cfg.Trading.OrderCleanupThreshold
	if threshold <= 0 {
		threshold = 100
	}

	batchSize := oc.cfg.Trading.CleanupBatchSize
	if batchSize <= 0 {
		batchSize = 10
	}

	// 🔥 核心策略：达到阈值才清理，不提前
	// 清理时优先清理数量多的一方（买单或卖单）
	if totalOrders >= threshold {
		canceledCount := 0

		logger.Info("🧹 [订单清理] 当前订单数: %d (买单: %d, 卖单: %d), 阈值: %d, 批次大小: %d",
			totalOrders, len(buyOrders), len(sellOrders), threshold, batchSize)

		// 🔥 新策略：优先清理数量多的一方
		// 如果买单多，就清理买单；如果卖单多，就清理卖单
		buyOrdersToCancel := 0
		sellOrdersToCancel := 0

		if len(buyOrders) > len(sellOrders) {
			// 买单多，清理买单
			buyOrdersToCancel = batchSize
			logger.Info("📊 [清理策略] 买单数量多于卖单，清理 %d 个买单", buyOrdersToCancel)
		} else if len(sellOrders) > len(buyOrders) {
			// 卖单多，清理卖单
			sellOrdersToCancel = batchSize
			logger.Info("📊 [清理策略] 卖单数量多于买单，清理 %d 个卖单", sellOrdersToCancel)
		} else {
			// 数量相等，平均清理
			buyOrdersToCancel = batchSize / 2
			sellOrdersToCancel = batchSize - buyOrdersToCancel
			logger.Info("📊 [清理策略] 买卖单数量相等，平均清理 (买单: %d, 卖单: %d)", buyOrdersToCancel, sellOrdersToCancel)
		}

		// 清理买单：清理价格最低的（离当前价格最远的）
		if len(buyOrders) > 0 && buyOrdersToCancel > 0 {
			// 按价格从低到高排序，清理最低的
			sort.Slice(buyOrders, func(i, j int) bool {
				return buyOrders[i].Price < buyOrders[j].Price
			})

			cancelCount := buyOrdersToCancel
			if cancelCount > len(buyOrders) {
				cancelCount = len(buyOrders)
			}

			if cancelCount > 0 {
				orderIDs := make([]int64, 0, cancelCount)
				prices := make([]float64, 0, cancelCount)
				for i := 0; i < cancelCount; i++ {
					orderIDs = append(orderIDs, buyOrders[i].OrderID)
					prices = append(prices, buyOrders[i].Price)
				}

				logger.Info("🧹 [订单清理-买单] 买单数: %d, 取消价格最低的 %d 个 (%.2f ~ %.2f)",
					len(buyOrders), cancelCount, buyOrders[0].Price, buyOrders[cancelCount-1].Price)

				if err := oc.executor.BatchCancelOrders(orderIDs); err != nil {
					logger.Error("❌ [订单清理-买单] 批量撤单失败: %v", err)
				} else {
					// 更新槽位状态为已申请撤单
					for _, price := range prices {
						oc.pm.UpdateSlotOrderStatus(price, OrderStatusCancelRequested)
					}
					canceledCount += cancelCount
				}
			}
		}

		// 清理卖单：清理价格最高的（离当前价格最远的）
		if len(sellOrders) > 0 && sellOrdersToCancel > 0 {
			// 按价格从高到低排序，清理最高的
			sort.Slice(sellOrders, func(i, j int) bool {
				return sellOrders[i].Price > sellOrders[j].Price
			})

			cancelCount := sellOrdersToCancel
			if cancelCount > len(sellOrders) {
				cancelCount = len(sellOrders)
			}

			if cancelCount > 0 {
				orderIDs := make([]int64, 0, cancelCount)
				prices := make([]float64, 0, cancelCount)
				for i := 0; i < cancelCount; i++ {
					orderIDs = append(orderIDs, sellOrders[i].OrderID)
					prices = append(prices, sellOrders[i].Price)
				}

				logger.Info("🧹 [订单清理-卖单] 卖单数: %d, 取消价格最高的 %d 个 (%.2f ~ %.2f)",
					len(sellOrders), cancelCount, sellOrders[0].Price, sellOrders[cancelCount-1].Price)

				if err := oc.executor.BatchCancelOrders(orderIDs); err != nil {
					logger.Error("❌ [订单清理-卖单] 批量撤单失败: %v", err)
				} else {
					// 更新槽位状态为已申请撤单
					for _, price := range prices {
						oc.pm.UpdateSlotOrderStatus(price, OrderStatusCancelRequested)
					}
					canceledCount += cancelCount
				}
			}
		}

		logger.Info("✅ [订单清理完成] 清理了 %d 个订单，剩余: %d", canceledCount, totalOrders-canceledCount)
	} else {
		logger.Debug("ℹ️ [订单清理] 总订单数: %d (阈值: %d，无需清理)", totalOrders, threshold)
	}
}
