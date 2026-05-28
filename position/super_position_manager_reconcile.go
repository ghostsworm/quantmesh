package position

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// ========== 對账恢復 / 持倉同步 / 緊急平倉 / 网格移位 / 持倉打印 ==========

// RestoreReconciliationStats 從數據库恢複對账统计值
// storage 是對账存儲接口，exchange/symbol 用於精确定位历史記錄
func (spm *SuperPositionManager) RestoreReconciliationStats(storage ReconciliationStorage, exchange, symbol string) error {
	if storage == nil {
		return nil // 存儲服務不可用，不报錯
	}

	// 1. 獲取最新對账記錄
	latestHistoryInterface, err := storage.GetLatestReconciliationHistory(exchange, symbol)
	if err != nil {
		return fmt.Errorf("獲取最新對账記錄失败: %w", err)
	}

	// 2. 獲取對账次數
	reconcileCount, err := storage.GetReconciliationCount(exchange, symbol)
	if err != nil {
		return fmt.Errorf("獲取對账次數失败: %w", err)
	}

	// 3. 如果没有历史記錄，不恢複（保持默认值）
	if latestHistoryInterface == nil {
		logger.Info("📊 [對账恢複] 未找到历史對账記錄，使用默认值")
		return nil
	}

	// 4. 使用反射提取對账記錄字段（支持 *T 與 **T 等多層指針）
	v := reflect.ValueOf(latestHistoryInterface)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			logger.Info("📊 [對账恢複] 未找到有效對账記錄，使用默认值")
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("對账記錄類型錯误: %T", latestHistoryInterface)
	}

	// 提取字段的辅助函數
	getFloat64Field := func(name string) float64 {
		field := v.FieldByName(name)
		if field.IsValid() && field.CanFloat() {
			return field.Float()
		}
		return 0.0
	}

	getTimeField := func(name string) time.Time {
		field := v.FieldByName(name)
		if field.IsValid() && field.Kind() == reflect.Interface {
			if t, ok := field.Interface().(time.Time); ok {
				return t
			}
		} else if field.IsValid() && field.Type().String() == "time.Time" {
			if t, ok := field.Interface().(time.Time); ok {
				return t
			}
		}
		return time.Time{}
	}

	// 5. 恢複统计值
	totalBuyQty := getFloat64Field("TotalBuyQty")
	totalSellQty := getFloat64Field("TotalSellQty")
	lastReconcileTime := getTimeField("ReconcileTime")

	spm.totalBuyQty.Store(totalBuyQty)
	spm.totalSellQty.Store(totalSellQty)
	spm.reconcileCount.Store(reconcileCount)
	spm.lastReconcileTime.Store(lastReconcileTime)

	logger.Info("✅ [對账恢複] 已恢複對账统计: 次數=%d, 累计買入=%.4f, 累计賣出=%.4f, 最后對账時间=%s",
		reconcileCount, totalBuyQty, totalSellQty, lastReconcileTime.Format("2006-01-02 15:04:05"))

	return nil
}

// ===== 訂單清理功能已迁移到 safety.OrderCleaner =====
// StartOrderCleanup 和 cleanupOrders 方法已移至 safety/order_cleaner.go

// UpdateSlotOrderStatus 更新槽位订單状態（供 OrderCleaner 使用）
func (spm *SuperPositionManager) UpdateSlotOrderStatus(price float64, status string) {
	slot := spm.getOrCreateSlot(price)
	slot.mu.Lock()
	slot.OrderStatus = status
	slot.mu.Unlock()
}

// CancelAllBuyOrders 撤销所有買單（风控触发時使用）
func (spm *SuperPositionManager) CancelAllBuyOrders() {
	var buyOrderIDs []int64
	var buyPrices []float64

	// 🔥 修複：收集所有OrderID>0且OrderSide=BUY的订單，不管OrderStatus
	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.RLock()
		if slot.OrderSide == "BUY" && slot.OrderID > 0 {
			buyOrderIDs = append(buyOrderIDs, slot.OrderID)
			buyPrices = append(buyPrices, price)
		}
		slot.mu.RUnlock()
		return true
	})

	if len(buyOrderIDs) == 0 {
		return
	}

	logger.Info("🔄 [撤销買單] 准备撤销 %d 個買單以释放保证金", len(buyOrderIDs))

	// 🔥 重複尝試3次，确保撤單干净
	for attempt := 1; attempt <= 3; attempt++ {
		if len(buyOrderIDs) == 0 {
			break
		}

		logger.Info("🔄 [撤销買單] 第 %d 次尝試，剩餘 %d 個订單", attempt, len(buyOrderIDs))

		if err := spm.executor.BatchCancelOrders(buyOrderIDs); err != nil {
			logger.Error("❌ [撤销買單] 批量撤單失败: %v", err)
		}

		// 更新槽位状態
		for _, price := range buyPrices {
			slot := spm.getOrCreateSlot(price)
			slot.mu.Lock()
			slot.OrderStatus = OrderStatusCancelRequested
			slot.mu.Unlock()
		}

		// 等待2秒让撤單生效（WebSocket推送通知）
		time.Sleep(2 * time.Second)

		// 🔥 二次检查：重新扫描本地槽位状態
		if attempt < 3 {
			buyOrderIDs = nil
			buyPrices = nil

			spm.slots.Range(func(key, value interface{}) bool {
				price := key.(float64)
				slot := value.(*InventorySlot)

				slot.mu.RLock()
				// 如果OrderStatus不是CANCELED且OrderID>0，說明可能还有残留
				if slot.OrderSide == "BUY" && slot.OrderID > 0 &&
					slot.OrderStatus != OrderStatusCanceled {
					buyOrderIDs = append(buyOrderIDs, slot.OrderID)
					buyPrices = append(buyPrices, price)
				}
				slot.mu.RUnlock()
				return true
			})

			if len(buyOrderIDs) > 0 {
				logger.Warn("⚠️ [撤销買單] 检测到 %d 個残留買單，继续清理", len(buyOrderIDs))
			} else {
				logger.Info("✅ [撤销買單] 所有買單已清理完成")
				break
			}
		}
	}

	logger.Info("✅ [撤销買單] 清理完成")
}

// CancelExcessOpenOrders 達到最大持倉預警時，撤銷多餘的開倉單，使開倉單數不超過 maxAllowed。
// LONG：開倉單為買單，先撤委託價高的；SHORT：開倉單為賣單，先撤委託價低的。
func (spm *SuperPositionManager) CancelExcessOpenOrders(maxAllowed int) {
	if maxAllowed <= 0 {
		return
	}
	type slotOrder struct {
		price   float64
		orderID int64
	}
	var openOrders []slotOrder
	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		var match bool
		if spm.isBoth() {
			match = (slot.OrderSide == "BUY" || slot.OrderSide == "SELL") && slot.PositionStatus == PositionStatusEmpty &&
				slot.OrderID > 0 &&
				slot.OrderStatus != OrderStatusCanceled && slot.OrderStatus != OrderStatusCancelRequested
		} else {
			openSide := "BUY"
			if spm.isShort() {
				openSide = "SELL"
			}
			match = slot.OrderSide == openSide && slot.OrderID > 0 &&
				slot.OrderStatus != OrderStatusCanceled && slot.OrderStatus != OrderStatusCancelRequested
		}
		if match {
			openOrders = append(openOrders, slotOrder{price: price, orderID: slot.OrderID})
		}
		slot.mu.RUnlock()
		return true
	})
	if len(openOrders) <= maxAllowed {
		return
	}
	sort.Slice(openOrders, func(i, j int) bool {
		if spm.isBoth() {
			return openOrders[i].price > openOrders[j].price
		}
		if spm.isShort() {
			return openOrders[i].price < openOrders[j].price
		}
		return openOrders[i].price > openOrders[j].price
	})
	toCancel := len(openOrders) - maxAllowed
	var orderIDs []int64
	for i := 0; i < toCancel && i < len(openOrders); i++ {
		orderIDs = append(orderIDs, openOrders[i].orderID)
	}
	if len(orderIDs) == 0 {
		return
	}
	sideLabel := "買單"
	if spm.isShort() {
		sideLabel = "賣單"
	}
	if spm.isBoth() {
		sideLabel = "開倉單"
	}
	logger.Info("🔄 [最大持倉預警] 當前開倉單 %d 筆超過上限 %d，撤銷 %d 筆 %s（%s 先撤）",
		len(openOrders), maxAllowed, len(orderIDs), sideLabel, map[bool]string{false: "高價先撤", true: "低價先撤"}[spm.isShort()])
	if err := spm.executor.BatchCancelOrders(orderIDs); err != nil {
		logger.Error("❌ [最大持倉預警] 批量撤單失敗: %v", err)
		return
	}
	orderIDToPrice := make(map[int64]float64, len(openOrders))
	for _, s := range openOrders {
		orderIDToPrice[s.orderID] = s.price
	}
	for _, oid := range orderIDs {
		if price, ok := orderIDToPrice[oid]; ok {
			slot := spm.getOrCreateSlot(price)
			slot.mu.Lock()
			slot.OrderStatus = OrderStatusCancelRequested
			slot.mu.Unlock()
		}
	}
	logger.Info("✅ [最大持倉預警] 已提交撤銷 %d 筆 %s", len(orderIDs), sideLabel)
}

// LiquidateAll 全平倉位（风控或止损触发時使用）
func (spm *SuperPositionManager) LiquidateAll() {
	logger.Warn("🚨 [全平倉] 正在執行全平操作，撤销掛單並限價平倉持倉...")

	if spm.isBoth() {
		spm.CancelAllOpenOrders()
	} else {
		spm.CancelAllBuyOrders()
	}

	var closeOrders []*OrderRequest
	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.Lock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			if slot.OrderID > 0 {
				logger.Info("🔄 [全平倉] 撤销槽位 %s 的現有订單 %d", formatPrice(price, spm.priceDecimals), slot.OrderID)
				spm.executor.BatchCancelOrders([]int64{slot.OrderID})
			}

			slot.SlotStatus = SlotStatusPending

			lastPrice, _ := spm.lastMarketPrice.Load().(float64)
			if lastPrice <= 0 {
				lastPrice = price
			}

			leg := slot.PositionLeg
			if leg == PositionLegNone {
				leg = PositionLegLong
			}
			var side string
			var px float64
			if spm.isBoth() && leg == PositionLegShort {
				side = "BUY"
				px = lastPrice * 1.01
			} else {
				side = "SELL"
				px = lastPrice * 0.99
			}
			px = roundPrice(px, spm.priceDecimals)

			clientOID := spm.generateClientOrderID(price, side, "stop_loss")

			closeOrders = append(closeOrders, &OrderRequest{
				Symbol:        spm.config.Trading.Symbol,
				Side:          side,
				Price:         px,
				Quantity:      slot.PositionQty,
				PriceDecimals: spm.priceDecimals,
				ReduceOnly:    !spm.isSpot(),
				PostOnly:      false,
				ClientOrderID: clientOID,
				OrderSource:   "stop_loss",
			})
		}
		slot.mu.Unlock()
		return true
	})

	if len(closeOrders) > 0 {
		logger.Info("🔄 [全平倉] 提交 %d 個平倉單", len(closeOrders))
		result := spm.executor.BatchPlaceOrdersWithDetails(closeOrders)

		for _, ord := range result.PlacedOrders {
			price, _, valid := spm.parseClientOrderID(ord.ClientOrderID)
			if valid {
				slot := spm.getOrCreateSlot(price)
				slot.mu.Lock()
				slot.OrderID = ord.OrderID
				slot.ClientOID = ord.ClientOrderID
				slot.OrderSide = ord.Side
				slot.OrderStatus = OrderStatusPlaced
				slot.SlotStatus = SlotStatusLocked
				slot.mu.Unlock()
			}
		}
	} else {
		logger.Info("ℹ️ [全平倉] 没有发現需要平倉的持倉")
	}
}

// SetGridRiskControl 更新網格風控配置（運行時熱更新，供 API 調用）
func (spm *SuperPositionManager) SetGridRiskControl(grc config.GridRiskControl) {
	if spm.config == nil {
		return
	}
	spm.config.Trading.GridRiskControl = grc
	logger.Info("✅ [%s] 網格風控已熱更新: enabled=%v, stop_loss=%.1f%%", spm.botID, grc.Enabled, grc.StopLossRatio*100)
}

// ShiftGrid 整體移動網格錨點（上移或下移），並撤銷開倉委託以便下一輪按新錨點掛單
func (spm *SuperPositionManager) ShiftGrid(direction string, step float64) {
	if step <= 0 {
		step = spm.config.Trading.GridShiftStep
	}
	if step <= 0 {
		step = spm.config.Trading.PriceInterval
	}
	spm.mu.Lock()
	if direction == "up" {
		spm.anchorPrice += step
		logger.Info("📈 [網格上移] 錨點 +%.2f，新錨點=%.2f", step, spm.anchorPrice)
	} else {
		spm.anchorPrice -= step
		if spm.anchorPrice < 0 {
			spm.anchorPrice = 0
		}
		logger.Info("📉 [網格下移] 錨點 -%.2f，新錨點=%.2f", step, spm.anchorPrice)
	}
	spm.mu.Unlock()
	spm.CancelAllOpenOrders()
}

// ===== 對账功能已迁移到 safety.Reconciler =====
// StartReconciliation 和 Reconcile 方法已移至 safety/reconciler.go
// SetPauseChecker 也已移至 Reconciler

// CancelAllOrders 撤销所有订單（退出時使用）
// 委托给交易所适配器實現具体逻辑
func (spm *SuperPositionManager) CancelAllOrders() {
	ctx := context.Background()
	if err := spm.exchange.CancelAllOrders(ctx, spm.config.Trading.Symbol); err != nil {
		logger.Error("❌ [%s] 撤销所有订單失败: %v", spm.exchange.GetName(), err)
	} else {
		logger.Info("✅ [%s] 撤销所有订單完成", spm.exchange.GetName())
	}
}

// getExistingPosition 獲取當前持倉數量（容錯处理）
func (spm *SuperPositionManager) getExistingPosition() float64 {
	if config.IsSpotMarketType(spm.config.Trading.MarketType) &&
		config.NormalizeSpotInventoryPolicy(spm.config.Trading.SpotInventoryPolicy) != config.SpotInventoryPolicyAdoptAll {
		logger.Debug("🔍 [持倉恢複] 現貨庫存策略為 conservative，跳過從交易所收編基礎幣餘額")
		return 0
	}
	ctx := context.Background()
	positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol)
	if err != nil || positionsInterface == nil {
		logger.Debug("🔍 [持倉恢複] 無法獲取持倉信息: %v", err)
		return 0
	}

	// 尝試類型断言 - 假設返回的是包含 Size 字段的結構体切片
	// 持倉方向：LONG 時取正數，SHORT 時取負數的絕對值（交易所 short 持倉為負）
	rawSize := 0.0
	switch positions := positionsInterface.(type) {
	case []*PositionInfo:
		for _, pos := range positions {
			if pos != nil && pos.Symbol == spm.config.Trading.Symbol {
				rawSize = pos.Size
				break
			}
		}
	case []interface{}:
		for _, pos := range positions {
			if posInfo, ok := pos.(*PositionInfo); ok {
				if posInfo.Symbol == spm.config.Trading.Symbol {
					rawSize = posInfo.Size
					break
				}
			}
			if posMap, ok := pos.(map[string]interface{}); ok {
				if symbol, ok := posMap["Symbol"].(string); ok && symbol == spm.config.Trading.Symbol {
					if size, ok := posMap["Size"].(float64); ok {
						rawSize = size
						break
					}
				}
			}
		}
	default:
		logger.Debug("🔍 [持倉恢複] 持倉類型: %T，未找到匹配的持倉", positionsInterface)
		return 0
	}

	// 按方向過濾：LONG 取正數持倉，SHORT 取負數持倉的絕對值
	if spm.isShort() {
		if rawSize < 0 {
			logger.Debug("🔍 [持倉恢複] 找到做空持倉: %.4f", -rawSize)
			return -rawSize
		}
		return 0
	}
	if rawSize > 0 {
		logger.Debug("🔍 [持倉恢複] 找到做多持倉: %.4f", rawSize)
		return rawSize
	}
	logger.Debug("🔍 [持倉恢複] 未找到匹配的持倉")
	return 0
}

// ForceSyncPositions 强制同步持倉（當對账发現重大不一致時調用）
func (spm *SuperPositionManager) ForceSyncPositions(exchangePosition float64) {
	// 注意：这里不需要全局鎖 spm.mu.Lock()，因為 slots 是 sync.Map，槽位更新有自己的鎖
	// 且我们不希望在對账時阻塞下單逻辑

	logger.Warn("🚨 [强制同步] 正在同步持倉状態，期望持倉: %.4f", exchangePosition)

	if exchangePosition <= 0.000001 {
		// 交易所持倉為空，清空本地所有槽位的持倉
		count := 0
		spm.slots.Range(func(key, value interface{}) bool {
			slot := value.(*InventorySlot)
			slot.mu.Lock()
			if slot.PositionStatus == PositionStatusFilled {
				logger.Info("🧹 [强制同步] 清空槽位價格 %s 的持倉 (原數量: %.4f)",
					formatPrice(slot.Price, spm.priceDecimals), slot.PositionQty)
				slot.PositionStatus = PositionStatusEmpty
				slot.PositionQty = 0
				slot.OrderID = 0
				slot.OrderStatus = OrderStatusNotPlaced
				slot.ClientOID = ""
				count++
			}
			slot.mu.Unlock()
			return true
		})

		if count > 0 {
			logger.Info("✅ [强制同步] 已成功清空 %d 個槽位的持倉數據", count)
		} else {
			logger.Debug("ℹ️ [强制同步] 本地本来就没有持倉，無需操作")
		}
	} else {
		// 交易所仍有持倉
		// 1. 若本地超出交易所：修剪多餘的本地槽位，防止平倉委託超出實際持倉
		// 2. 若本地少於交易所：以交易所為準，補齊本地持倉差額
		spm.trimExcessPositions(exchangePosition)
		spm.fillDeficitPositions(exchangePosition)
	}
}

// trimExcessPositions 修剪多餘的本地持倉槽位
// 當本地持倉总量 > 交易所實際持倉時，清除距離當前價格最遠的「幻影」槽位
func (spm *SuperPositionManager) trimExcessPositions(exchangePosition float64) {
	// 1. 收集所有 FILLED 槽位
	type filledSlot struct {
		Price    float64
		Qty      float64
		Distance float64 // 距離當前價格的距離
	}
	var filledSlots []filledSlot
	localTotal := 0.0

	// 獲取當前市場價格
	currentPrice, _ := spm.lastMarketPrice.Load().(float64)
	if currentPrice <= 0 {
		currentPrice = spm.anchorPrice
	}

	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			price := key.(float64)
			localTotal += slot.PositionQty
			filledSlots = append(filledSlots, filledSlot{
				Price:    price,
				Qty:      slot.PositionQty,
				Distance: math.Abs(price - currentPrice),
			})
		}
		slot.mu.RUnlock()
		return true
	})

	excess := localTotal - exchangePosition
	if excess <= 0.00000001 {
		logger.Info("✅ [强制同步] 本地持倉 %.6f 未超出交易所持倉 %.6f，無需修剪", localTotal, exchangePosition)
		return
	}

	logger.Warn("🚨 [强制同步] 本地持倉 %.6f > 交易所持倉 %.6f，多餘 %.6f，開始修剪幻影槽位",
		localTotal, exchangePosition, excess)

	// 2. 按距離當前價格的距離降序排序（最遠的排前面，優先清除）
	sort.Slice(filledSlots, func(i, j int) bool {
		return filledSlots[i].Distance > filledSlots[j].Distance
	})

	// 3. 從最遠的槽位開始清除，直到多餘量被消除
	trimmed := 0
	for _, fs := range filledSlots {
		if excess <= 0.00000001 {
			break
		}

		slotRaw, ok := spm.slots.Load(fs.Price)
		if !ok {
			continue
		}
		slot := slotRaw.(*InventorySlot)
		slot.mu.Lock()

		// 再次確認槽位仍然是 FILLED 狀態
		if slot.PositionStatus != PositionStatusFilled || slot.PositionQty <= 0 {
			slot.mu.Unlock()
			continue
		}

		if slot.PositionQty <= excess+0.00000001 {
			// 整個槽位都是多餘的，完全清除
			logger.Warn("🧹 [强制同步] 清除幻影槽位 價格=%s 數量=%.6f（距離當前價 %.2f）",
				formatPrice(slot.Price, spm.priceDecimals), slot.PositionQty, fs.Distance)
			excess -= slot.PositionQty
			slot.PositionStatus = PositionStatusEmpty
			slot.PositionQty = 0
			slot.OrderID = 0
			slot.OrderStatus = OrderStatusNotPlaced
			slot.OrderSide = ""
			slot.ClientOID = ""
			slot.SlotStatus = SlotStatusFree
			trimmed++
		} else {
			// 槽位數量大於多餘量，部分修剪（這種情況較少見）
			logger.Warn("✂️ [强制同步] 部分修剪槽位 價格=%s 數量 %.6f -> %.6f（扣除幻影 %.6f）",
				formatPrice(slot.Price, spm.priceDecimals), slot.PositionQty, slot.PositionQty-excess, excess)
			slot.PositionQty -= excess
			excess = 0
		}

		slot.mu.Unlock()
	}

	if trimmed > 0 {
		logger.Info("✅ [强制同步] 已修剪 %d 個幻影槽位，本地持倉已對齊交易所持倉 %.6f", trimmed, exchangePosition)
	}
}

// fillDeficitPositions 補齊本地持倉差額（當本地持倉 < 交易所持倉時，以交易所為準）
// 將差額分配到距離當前價格最近的已填充槽位；若無任何槽位則觸發完整持倉恢復
func (spm *SuperPositionManager) fillDeficitPositions(exchangePosition float64) {
	type filledSlot struct {
		Price    float64
		Qty      float64
		Distance float64
	}
	var filledSlots []filledSlot
	localTotal := 0.0

	currentPrice, _ := spm.lastMarketPrice.Load().(float64)
	if currentPrice <= 0 {
		currentPrice = spm.anchorPrice
	}

	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			price := key.(float64)
			localTotal += slot.PositionQty
			filledSlots = append(filledSlots, filledSlot{
				Price:    price,
				Qty:      slot.PositionQty,
				Distance: math.Abs(price - currentPrice),
			})
		}
		slot.mu.RUnlock()
		return true
	})

	deficit := exchangePosition - localTotal
	if deficit <= 0.00000001 {
		return
	}

	if len(filledSlots) == 0 {
		// 本地無任何持倉槽位，觸發完整持倉恢復
		logger.Warn("🚨 [强制同步] 本地持倉為 0，交易所持倉 %.6f，觸發完整持倉恢復", exchangePosition)
		spm.initializeSellSlotsFromPosition(exchangePosition)
		return
	}

	// 按距離當前價格升序排序，取最近的槽位補齊差額
	sort.Slice(filledSlots, func(i, j int) bool {
		return filledSlots[i].Distance < filledSlots[j].Distance
	})

	deficit = roundPrice(deficit, spm.quantityDecimals)
	if deficit <= 0 {
		return
	}

	nearestPrice := filledSlots[0].Price
	slotRaw, ok := spm.slots.Load(nearestPrice)
	if !ok {
		return
	}
	slot := slotRaw.(*InventorySlot)
	slot.mu.Lock()
	defer slot.mu.Unlock()

	if slot.PositionStatus != PositionStatusFilled {
		return
	}

	slot.PositionQty += deficit
	logger.Info("✅ [强制同步] 以交易所為準補齊持倉：槽位 %s 增加 %.6f，本地持倉 %.6f -> %.6f",
		formatPrice(slot.Price, spm.priceDecimals), deficit, localTotal, localTotal+deficit)
}

// initializeSellSlotsFromPosition 從現有持倉初始化賣單槽位（用於程序重啟后恢複状態）
func (spm *SuperPositionManager) initializeSellSlotsFromPosition(totalPosition float64) {
	if totalPosition <= 0 {
		return
	}

	// 0. 獲取杠杆倍數（用於计算實際使用的保证金）
	leverage := 1
	if spm.isSpot() {
		leverage = 1
	} else {
		ctx := context.Background()
		// 先尝試從帳戶資訊中的持倉獲取杠杆倍數（GetAccount 返回的持倉資訊通常包含杠杆）
		if accountResult, err := spm.exchange.GetAccount(ctx); err == nil && accountResult != nil {
			accountValue := reflect.ValueOf(accountResult)
			if accountValue.Kind() == reflect.Ptr {
				accountValue = accountValue.Elem()
			}
			// 尝試從 Account.Positions 字段獲取持倉信息
			if positionsField := accountValue.FieldByName("Positions"); positionsField.IsValid() && positionsField.CanInterface() {
				positionsValue := reflect.ValueOf(positionsField.Interface())
				if positionsValue.Kind() == reflect.Slice {
					for i := 0; i < positionsValue.Len(); i++ {
						posValue := positionsValue.Index(i)
						if posValue.Kind() == reflect.Ptr {
							posValue = posValue.Elem()
						} else if posValue.Kind() == reflect.Interface {
							posValue = posValue.Elem()
						}
						// 检查 Symbol 是否匹配
						if symbolField := posValue.FieldByName("Symbol"); symbolField.IsValid() && symbolField.CanInterface() {
							if symbol, ok := symbolField.Interface().(string); ok && symbol == spm.config.Trading.Symbol {
								// 尝試獲取 Leverage 字段
								if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
									if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
										leverage = lev
										logger.Debug("🔍 [持倉恢複] 從账戶持倉資訊中獲取到杠杆倍數: %dx", leverage)
										break
									}
								}
							}
						}
					}
				}
			}
			// 如果從持倉中獲取不到，尝試從账戶级别的杠杆字段獲取
			if leverage == 1 {
				if leverageField := accountValue.FieldByName("AccountLeverage"); leverageField.IsValid() && leverageField.CanInterface() {
					if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
						leverage = lev
						logger.Debug("🔍 [持倉恢複] 從账戶级别獲取到杠杆倍數: %dx", leverage)
					}
				}
			}
		}
		// 如果從账戶中獲取不到，尝試從 GetPositions 獲取
		if leverage == 1 {
			if positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol); err == nil && positionsInterface != nil {
				positionsValue := reflect.ValueOf(positionsInterface)
				if positionsValue.Kind() == reflect.Slice {
					for i := 0; i < positionsValue.Len(); i++ {
						posValue := positionsValue.Index(i)
						if posValue.Kind() == reflect.Ptr {
							posValue = posValue.Elem()
						} else if posValue.Kind() == reflect.Interface {
							posValue = posValue.Elem()
						}
						if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
							if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
								leverage = lev
								logger.Debug("🔍 [持倉恢複] 從 GetPositions 獲取到杠杆倍數: %dx", leverage)
								break
							}
						}
					}
				}
			}
		}
	}

	logger.Info("🔍 [持倉恢複] 检测到杠杆倍數: %dx，將使用實際保证金（倉位價值 / 杠杆）计算已用资金", leverage)

	// 1. 计算每單的理論數量（基於當前價格）
	// 使用锚点價格作為参考價格，使用從交易所獲取的數量精度

	// 每單的理論數量 = 目標金額 / 锚点價格
	theoryQtyPerSlot := spm.config.Trading.OrderQuantity / spm.anchorPrice
	theoryQtyPerSlot = roundPrice(theoryQtyPerSlot, spm.quantityDecimals)

	// 2. 计算需要創建的總槽位數
	totalSlotsNeeded := int(math.Ceil(totalPosition / theoryQtyPerSlot))
	logger.Info("🔄 [持倉恢複] 總持倉: %.4f，每單理論數量: %.4f，需要創建 %d 個槽位",
		totalPosition, theoryQtyPerSlot, totalSlotsNeeded)

	// 3. 确定窗口大小（前N個槽位可以立即挂賣單）
	sellWindowSize := spm.config.Trading.SellWindowSize
	if sellWindowSize <= 0 {
		sellWindowSize = spm.config.Trading.BuyWindowSize // 默认與買單窗口相同
	}

	// 4. 计算賣單槽位價格（從锚点價格 + 利潤間距开始）
	// 賣單最低價 = 锚点價格 + 利潤間距（避免與買單最高價冲突）
	sellStartPrice := spm.anchorPrice + spm.getEffectiveProfitSpread()
	sellPrices := spm.calculateSlotPrices(sellStartPrice, totalSlotsNeeded, "up")
	sellPrices = spm.optimizeSlotPricesWithOrderBook(context.Background(), spm.config.Trading.Symbol, sellPrices)

	logger.Info("🔄 [持倉恢複] 從價格 %s 向上創建 %d 個槽位（前 %d 個將挂賣單）",
		formatPrice(sellStartPrice, spm.priceDecimals), totalSlotsNeeded, sellWindowSize)

	// 5. 先计算所有槽位的理論數量總和（固定金額模式）
	var totalTheoryQty float64
	theoryQtys := make([]float64, len(sellPrices))
	for i, price := range sellPrices {
		theoryQty := spm.config.Trading.OrderQuantity / price
		theoryQty = roundPrice(theoryQty, spm.quantityDecimals)
		theoryQtys[i] = theoryQty
		totalTheoryQty += theoryQty
	}

	logger.Debug("🔍 [持倉恢複] 理論總數量: %.4f, 實際持倉: %.4f, 比例: %.4f",
		totalTheoryQty, totalPosition, totalPosition/totalTheoryQty)

	// 6. 按比例分配實際持倉到各個槽位，並累加已用资金
	var allocatedQty float64
	var totalUsedAmount float64 // 累加已用资金

	for i, price := range sellPrices {
		// 计算這個槽位应該分配的數量
		var slotQty float64
		if i == len(sellPrices)-1 {
			// 最后一個槽位：分配剩餘的所有持倉（避免舍入误差）
			slotQty = totalPosition - allocatedQty
		} else {
			// 按比例分配：實際數量 = 理論數量 × (總持倉 / 理論總數量)
			slotQty = theoryQtys[i] * (totalPosition / totalTheoryQty)
			slotQty = roundPrice(slotQty, spm.quantityDecimals)

			// 确保不超過剩餘持倉
			remaining := totalPosition - allocatedQty
			if slotQty > remaining {
				slotQty = remaining
			}
		}

		if slotQty <= 0 {
			logger.Warn("⚠️ [持倉恢複] 槽位 %s 分配數量過小 %.4f，跳過（已分配: %.4f / 總计: %.4f）",
				formatPrice(price, spm.priceDecimals), slotQty, allocatedQty, totalPosition)
			continue
		}

		// 7. 創建或更新槽位
		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()

		// 設置為有倉状態
		slot.PositionStatus = PositionStatusFilled
		slot.PositionQty = slotQty

		// 🔥 設置平均买入价格（恢复持仓时，使用槽位价格作为平均买入价格）
		// 因为无法知道实际买入价格，使用槽位价格作为近似值
		if slot.AvgBuyPrice <= 0 {
			slot.AvgBuyPrice = price
		}

		// 清空订單信息，但設置方向為SELL（因為这是恢複的持倉，將来要挂賣單）
		slot.OrderID = 0
		slot.OrderStatus = OrderStatusNotPlaced
		slot.OrderSide = "SELL" // 恢複持倉時標記為賣單方向
		slot.ClientOID = ""
		slot.OrderFilledQty = 0

		slot.mu.Unlock()

		allocatedQty += slotQty
		// 累加已用资金：使用實際保证金（倉位價值 / 杠杆倍數）而不是倉位價值
		// 锚点價格是市场當前價格，接近實際買入的平均價格
		// 不能用賣出價格（sellPrice），因為賣出價格是目標價，會高估成本
		// 對於有杠杆的交易，實際使用的保证金 = 倉位價值 / 杠杆倍數
		positionValue := spm.anchorPrice * slotQty        // 倉位價值
		actualMargin := positionValue / float64(leverage) // 實際使用的保证金
		totalUsedAmount += actualMargin

		// 日志標記：是否在窗口内（只打印前10個和最后10個）
		if i < 10 || i >= len(sellPrices)-10 {
			inWindow := ""
			if i < sellWindowSize {
				inWindow = " [可挂單]"
			} else {
				inWindow = " [暂不挂單]"
			}
			logger.Info("✅ [持倉恢複] 槽位 %s: 分配持倉 %.4f (理論: %.4f)%s",
				formatPrice(price, spm.priceDecimals), slotQty, theoryQtys[i], inWindow)
		} else if i == 10 {
			logger.Info("... （省略中间 %d 個槽位）", len(sellPrices)-20)
		}
	}

	logger.Info("✅ [持倉恢複] 完成持倉恢複，總持倉: %.4f，已分配: %.4f，差异: %.4f",
		totalPosition, allocatedQty, totalPosition-allocatedQty)

	// 🔥 初始化已用资金：使用實際保证金（倉位價值 / 杠杆倍數）而不是倉位價值
	// 这样资金限額限制的是實際投入的资金，而不是倉位價值
	if totalUsedAmount > 0 {
		spm.allocationManager.SetUsedAmount(spm.exchangeName, spm.config.Trading.Symbol, totalUsedAmount)
		positionValue := spm.anchorPrice * totalPosition // 總倉位價值
		logger.Info("💰 [%s] [资金分配] 恢複持倉，初始化已用资金: %.2f USDT (實際保证金，杠杆 %dx，倉位價值: %.2f USDT)",
			spm.logPrefix(), totalUsedAmount, leverage, positionValue)
	}

	// 8. 提示用戶后续會自动下賣單
	logger.Info("💡 [持倉恢複] 前 %d 個槽位的賣單將在價格調整時自动創建", sellWindowSize)
	logger.Info("💡 [持倉恢複] 其餘 %d 個槽位保持有倉状態，價格接近時自动挂單", totalSlotsNeeded-sellWindowSize)
}

// initializeBuySlotsFromPosition 從現有做空持倉初始化買單平倉槽位（SHORT 方向專用）
func (spm *SuperPositionManager) initializeBuySlotsFromPosition(totalPosition float64) {
	if totalPosition <= 0 {
		return
	}
	// 做空持倉：槽位價格 = 開倉賣價（高於錨點），平倉買價 = 槽位價格 - interval
	theoryQtyPerSlot := spm.config.Trading.OrderQuantity / spm.anchorPrice
	theoryQtyPerSlot = roundPrice(theoryQtyPerSlot, spm.quantityDecimals)
	totalSlotsNeeded := int(math.Ceil(totalPosition / theoryQtyPerSlot))
	sellWindowSize := spm.config.Trading.SellWindowSize
	if sellWindowSize <= 0 {
		sellWindowSize = spm.config.Trading.BuyWindowSize
	}
	sellStartPrice := spm.anchorPrice + spm.getEffectiveProfitSpread()
	sellPrices := spm.calculateSlotPrices(sellStartPrice, totalSlotsNeeded, "up")
	sellPrices = spm.optimizeSlotPricesWithOrderBook(context.Background(), spm.config.Trading.Symbol, sellPrices)

	var totalTheoryQty float64
	theoryQtys := make([]float64, len(sellPrices))
	for i, price := range sellPrices {
		theoryQty := spm.config.Trading.OrderQuantity / price
		theoryQty = roundPrice(theoryQty, spm.quantityDecimals)
		theoryQtys[i] = theoryQty
		totalTheoryQty += theoryQty
	}

	var allocatedQty float64
	for i, price := range sellPrices {
		var slotQty float64
		if i == len(sellPrices)-1 {
			slotQty = totalPosition - allocatedQty
		} else {
			slotQty = theoryQtys[i] * (totalPosition / totalTheoryQty)
			slotQty = roundPrice(slotQty, spm.quantityDecimals)
			if slotQty > totalPosition-allocatedQty {
				slotQty = totalPosition - allocatedQty
			}
		}
		if slotQty <= 0 {
			continue
		}
		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()
		slot.PositionStatus = PositionStatusFilled
		slot.PositionQty = slotQty
		if slot.AvgBuyPrice <= 0 {
			slot.AvgBuyPrice = price
		}
		slot.OrderID = 0
		slot.OrderStatus = OrderStatusNotPlaced
		slot.OrderSide = "BUY" // 做空平倉為買單
		slot.ClientOID = ""
		slot.OrderFilledQty = 0
		slot.mu.Unlock()
		allocatedQty += slotQty
	}
	logger.Info("✅ [持倉恢複] 做空持倉恢複完成，總持倉: %.4f，已分配: %.4f", totalPosition, allocatedQty)
}

// ===== 状態打印功能 =====

// PrintPositions 打印持倉状態（由 main.go 定期調用和退出時調用）
// 注意：該方法内部使用 totalBuyQty 和 totalSellQty 统计數據
func (spm *SuperPositionManager) PrintPositions() {
	// 從配置中獲取交易對信息
	symbol := spm.config.Trading.Symbol
	currentPositionsMsg := logger.Translate("log.position.current_positions", map[string]interface{}{"Symbol": symbol})
	logger.Info("%s", currentPositionsMsg)
	total := 0.0
	count := 0

	// 收集所有持倉數據
	type positionInfo struct {
		Price          float64
		Qty            float64
		OrderStatus    string
		OrderSide      string
		OrderID        int64
		SlotStatus     string
		OrderCreatedAt time.Time
	}
	var positions []positionInfo

	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0.001 {
			positions = append(positions, positionInfo{
				Price:          price,
				Qty:            slot.PositionQty,
				OrderStatus:    slot.OrderStatus,
				OrderSide:      slot.OrderSide,
				OrderID:        slot.OrderID,
				SlotStatus:     slot.SlotStatus,
				OrderCreatedAt: slot.OrderCreatedAt,
			})
			total += slot.PositionQty
			count++
		}
		slot.mu.RUnlock()
		return true
	})

	// 按價格從高到低排序
	sort.Slice(positions, func(i, j int) bool {
		return positions[i].Price > positions[j].Price
	})

	// 從交易所接口獲取基础币种（支援U本位和币本位合約）
	baseCurrency := spm.exchange.GetBaseAsset()

	// 打印持倉（從高到低）
	for _, pos := range positions {
		statusIcon := "🟢" // 有持倉
		priceStr := formatPrice(pos.Price, spm.priceDecimals)

		// 使用翻譯函數獲取持倉信息
		positionDesc := logger.Translate("log.position.position_info", map[string]interface{}{
			"Qty":      fmt.Sprintf("%.4f", pos.Qty),
			"Currency": baseCurrency,
		})

		orderInfo := ""
		if pos.OrderStatus != OrderStatusNotPlaced && pos.OrderStatus != "" {
			orderInfo = ", " + logger.Translate("log.position.order_info", map[string]interface{}{
				"Side":    pos.OrderSide,
				"Status":  pos.OrderStatus,
				"OrderID": pos.OrderID,
			})
		}

		// 🔥 總是显示槽位状態,便於調試
		slotStatusInfo := ""
		if pos.SlotStatus != "" {
			slotStatusInfo = " [" + logger.Translate("log.position.slot_status", map[string]interface{}{
				"Status": pos.SlotStatus,
			}) + "]"
		} else {
			slotStatusInfo = " [" + logger.Translate("log.position.slot_empty") + "]"
		}

		// 格式化買入時间（使用订單創建時间作為買入時间参考）
		buyTimeStr := ""
		if !pos.OrderCreatedAt.IsZero() {
			buyTimeStr = ", " + logger.Translate("log.position.buy_time", map[string]interface{}{
				"Time": pos.OrderCreatedAt.Format("2006/01/02 15:04:05"),
			})
		}

		// 添加交易所、币种、策略信息
		strategyName := logger.Translate("log.position.strategy_grid")
		exchangeSymbolInfo := fmt.Sprintf("[%s:%s:%s]", spm.exchangeName, spm.config.Trading.Symbol, strategyName)

		logger.Info("  %s %s %s: %s%s%s%s",
			statusIcon, exchangeSymbolInfo, priceStr, positionDesc, buyTimeStr, orderInfo, slotStatusInfo)
	}

	positionSummaryMsg := logger.Translate("log.position.position_summary", map[string]interface{}{
		"Symbol":   spm.config.Trading.Symbol,
		"Total":    fmt.Sprintf("%.4f", total),
		"Currency": baseCurrency,
		"Count":    count,
	})
	logger.Info("%s", positionSummaryMsg)
	totalBuyQty := spm.totalBuyQty.Load().(float64)
	totalSellQty := spm.totalSellQty.Load().(float64)
	// 預计盈利 = 累计賣出數量 × 利潤間距（每笔盈利 = 利潤間距 × 數量）
	estimatedProfit := totalSellQty * spm.getEffectiveProfitSpread()
	logger.Info("[%s] 累计買入: %.2f, 累计賣出: %.2f, 預计盈利: %.2f U",
		spm.config.Trading.Symbol, totalBuyQty, totalSellQty, estimatedProfit)

	// === 新增：打印買單窗口详细信息 ===
	logger.Info("🔍 ===== 買單窗口状態 [%s] =====", spm.logPrefix())

	// 獲取最后的市场價格
	lastPrice, ok := spm.lastMarketPrice.Load().(float64)
	if !ok || lastPrice <= 0 {
		lastPrice = spm.anchorPrice // 如果没有更新過，使用锚点價格
	}
	logger.Info("[%s] 當前市場價格: %s", spm.logPrefix(), formatPrice(lastPrice, spm.priceDecimals))

	// 收集所有槽位信息（包括買單和空槽位）
	type slotInfo struct {
		Price          float64
		PositionStatus string
		PositionQty    float64
		OrderSide      string
		OrderStatus    string
		OrderID        int64
		ClientOID      string
		SlotStatus     string
	}
	var allSlots []slotInfo

	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		allSlots = append(allSlots, slotInfo{
			Price:          price,
			PositionStatus: slot.PositionStatus,
			PositionQty:    slot.PositionQty,
			OrderSide:      slot.OrderSide,
			OrderStatus:    slot.OrderStatus,
			OrderID:        slot.OrderID,
			ClientOID:      slot.ClientOID,
			SlotStatus:     slot.SlotStatus,
		})
		slot.mu.RUnlock()
		return true
	})

	// 按價格從高到低排序
	sort.Slice(allSlots, func(i, j int) bool {
		return allSlots[i].Price > allSlots[j].Price
	})

	// 找到最接近當前價格的网格價格
	currentGridPrice := spm.findNearestGridPrice(lastPrice)
	logger.Info("[%s] 當前网格價格: %s", spm.logPrefix(), formatPrice(currentGridPrice, spm.priceDecimals))

	// 计算買單窗口範圍（當前网格價格下方的買單窗口）
	buyWindowSize := spm.config.Trading.BuyWindowSize
	buyWindowPrices := spm.calculateSlotPrices(currentGridPrice, buyWindowSize, "down")

	// 創建價格查找表
	buyWindowPriceMap := make(map[string]bool)
	for _, p := range buyWindowPrices {
		buyWindowPriceMap[formatPrice(p, spm.priceDecimals)] = true
	}

	// 打印買單窗口内的所有槽位
	logger.Info("[%s] 買單窗口大小: %d 個槽位 (當前网格價格下方)", spm.logPrefix(), buyWindowSize)
	buyOrderCount := 0
	emptySlotCount := 0
	filledSlotCount := 0

	for _, slot := range allSlots {
		priceStr := formatPrice(slot.Price, spm.priceDecimals)
		// 只打印買單窗口内的槽位
		if buyWindowPriceMap[priceStr] {
			statusIcon := "⚪" // 空槽位
			statusDesc := ""

			if slot.PositionStatus == PositionStatusFilled {
				statusIcon = "🟢" // 有持倉
				statusDesc = fmt.Sprintf("持倉: %.4f %s", slot.PositionQty, baseCurrency)
				filledSlotCount++
			} else {
				statusDesc = "無持倉"
				emptySlotCount++
			}

			orderInfo := ""
			if slot.OrderStatus != OrderStatusNotPlaced && slot.OrderStatus != "" {
				orderInfo = fmt.Sprintf(", 订單: %s/%s (ID:%d)", slot.OrderSide, slot.OrderStatus, slot.OrderID)
				if slot.OrderSide == "BUY" && (slot.OrderStatus == OrderStatusPlaced ||
					slot.OrderStatus == OrderStatusConfirmed ||
					slot.OrderStatus == OrderStatusPartiallyFilled) {
					buyOrderCount++
				}
			}

			// 🔥 總是显示槽位状態,便於調試
			slotStatusInfo := ""
			if slot.SlotStatus != "" {
				slotStatusInfo = fmt.Sprintf(" [槽位:%s]", slot.SlotStatus)
			} else {
				slotStatusInfo = " [槽位:空]"
			}

			logger.Info("  %s %s: %s%s%s",
				statusIcon, priceStr, statusDesc, orderInfo, slotStatusInfo)
		}
	}

	logger.Info("[%s] 窗口统计: %d 個買單活跃, %d 個已持倉, %d 個空槽位",
		spm.logPrefix(), buyOrderCount, filledSlotCount, emptySlotCount)
	logger.Info("==========================")
}
