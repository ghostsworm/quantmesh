package safety

import (
	"context"
	"quantmesh/config"
	"quantmesh/logger"
	"reflect"
	"sort"
	"time"
)

// OrderCleanerSlotInfo 訂單清理所需的槽位信息
type OrderCleanerSlotInfo struct {
	Price       float64
	OrderID     int64
	OrderSide   string
	OrderStatus string
}

// IOrderExecutor 订單執行器介面（用於批量撤單）
type IOrderExecutor interface {
	BatchCancelOrders(orderIDs []int64) error
}

// IOrderCleanerPositionManager 訂單清理所需的倉位管理器接口
type IOrderCleanerPositionManager interface {
	// 遍历所有槽位
	IterateSlots(fn func(price float64, slot interface{}) bool)
	// 更新槽位状態
	UpdateSlotOrderStatus(price float64, status string)
}

// OrderCleaner 訂單清理器
type OrderCleaner struct {
	cfg      *config.Config
	executor IOrderExecutor
	pm       IOrderCleanerPositionManager
}

// NewOrderCleaner 創建訂單清理器
func NewOrderCleaner(cfg *config.Config, executor IOrderExecutor, pm IOrderCleanerPositionManager) *OrderCleaner {
	return &OrderCleaner{
		cfg:      cfg,
		executor: executor,
		pm:       pm,
	}
}

// Start 啟動訂單清理协程
func (oc *OrderCleaner) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		cleanupInterval := time.Duration(oc.cfg.Timing.OrderCleanupInterval) * time.Second
		if cleanupInterval <= 0 {
			cleanupInterval = 30 * time.Second
			logger.Warn("⚠️ 訂單清理間隔配置無效，使用默认值 %v", cleanupInterval)
		}
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("⏹️ 訂單清理协程已停止")
				return
			case <-ticker.C:
				oc.CleanupOrders()
			}
		}
	}()
	logger.Info("✅ 訂單清理协程已啟动")
}

// CleanupOrders 清理订單
func (oc *OrderCleaner) CleanupOrders() {
	// 订單状態常量
	const (
		OrderStatusPlaced          = "PLACED"
		OrderStatusConfirmed       = "CONFIRMED"
		OrderStatusCancelRequested = "CANCEL_REQUESTED"
	)

	// 统计當前订單數
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

		// 🔥 修複：排除部分成交的订單（PARTIALLY_FILLED不能撤销，會造成资金悬空）
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
	// 清理時优先清理數量多的一方（買單或賣單）
	if totalOrders >= threshold {
		canceledCount := 0

		logger.Info("🧹 [訂單清理] 當前订單數: %d (買單: %d, 賣單: %d), 阈值: %d, 批次大小: %d",
			totalOrders, len(buyOrders), len(sellOrders), threshold, batchSize)

		// 🔥 新策略：优先清理數量多的一方
		// 如果買單多，就清理買單；如果賣單多，就清理賣單
		buyOrdersToCancel := 0
		sellOrdersToCancel := 0

		if len(buyOrders) > len(sellOrders) {
			// 買單多，清理買單
			buyOrdersToCancel = batchSize
			logger.Info("📊 [清理策略] 買單數量多於賣單，清理 %d 個買單", buyOrdersToCancel)
		} else if len(sellOrders) > len(buyOrders) {
			// 賣單多，清理賣單
			sellOrdersToCancel = batchSize
			logger.Info("📊 [清理策略] 賣單數量多於買單，清理 %d 個賣單", sellOrdersToCancel)
		} else {
			// 數量相等，平均清理
			buyOrdersToCancel = batchSize / 2
			sellOrdersToCancel = batchSize - buyOrdersToCancel
			logger.Info("📊 [清理策略] 買賣單數量相等，平均清理 (買單: %d, 賣單: %d)", buyOrdersToCancel, sellOrdersToCancel)
		}

		// 清理買單：清理價格最低的（离當前價格最远的）
		if len(buyOrders) > 0 && buyOrdersToCancel > 0 {
			// 按價格從低到高排序，清理最低的
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

				logger.Info("🧹 [訂單清理-買單] 買單數: %d, 取消價格最低的 %d 個 (%.2f ~ %.2f)",
					len(buyOrders), cancelCount, buyOrders[0].Price, buyOrders[cancelCount-1].Price)

				if err := oc.executor.BatchCancelOrders(orderIDs); err != nil {
					logger.Error("❌ [訂單清理-買單] 批量撤單失败: %v", err)
				} else {
					// 更新槽位状態為已申请撤單
					for _, price := range prices {
						oc.pm.UpdateSlotOrderStatus(price, OrderStatusCancelRequested)
					}
					canceledCount += cancelCount
				}
			}
		}

		// 清理賣單：清理價格最高的（离當前價格最远的）
		if len(sellOrders) > 0 && sellOrdersToCancel > 0 {
			// 按價格從高到低排序，清理最高的
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

				logger.Info("🧹 [訂單清理-賣單] 賣單數: %d, 取消價格最高的 %d 個 (%.2f ~ %.2f)",
					len(sellOrders), cancelCount, sellOrders[0].Price, sellOrders[cancelCount-1].Price)

				if err := oc.executor.BatchCancelOrders(orderIDs); err != nil {
					logger.Error("❌ [訂單清理-賣單] 批量撤單失败: %v", err)
				} else {
					// 更新槽位状態為已申请撤單
					for _, price := range prices {
						oc.pm.UpdateSlotOrderStatus(price, OrderStatusCancelRequested)
					}
					canceledCount += cancelCount
				}
			}
		}

		logger.Info("✅ [訂單清理完成] 清理了 %d 個订單，剩餘: %d", canceledCount, totalOrders-canceledCount)
	} else {
		logger.Debug("ℹ️ [訂單清理] 總订單數: %d (阈值: %d，無需清理)", totalOrders, threshold)
	}
}
