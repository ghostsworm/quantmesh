package position

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"time"

	"quantmesh/event"
	"quantmesh/logger"
)

// ========== 訂單調整主循環（AdjustOrders）==========

// AdjustOrders 調整订單（交易入口）
func (spm *SuperPositionManager) AdjustOrders(currentPrice float64) error {
	// 🔥 移除初始化检查：現在完全由 AdjustOrders 控制所有下單
	// 初始化只负责恢複持倉状態，不再下單

	spm.mu.Lock()
	defer spm.mu.Unlock()

	// 检查是否暂停
	if spm.IsPaused() {
		logger.Debug("⏸️ [%s] 交易已暂停，跳過订單調整", spm.logPrefix())
		return nil
	}

	// 驗证價格有效性
	if currentPrice <= 0 {
		logger.Warn("⚠️ 收到無效價格: %.2f，跳過订單調整", currentPrice)
		return nil
	}

	// 對當前價格進行精度处理
	currentPrice = roundPrice(currentPrice, spm.priceDecimals)

	// 更新最后市场價格（用於打印状態）
	spm.lastMarketPrice.Store(currentPrice)

	// 觸發價格：未達到時不放置任何訂單
	if spm.config.Trading.TriggerPrice > 0 {
		trigger := spm.config.Trading.TriggerPrice
		if spm.isShort() {
			if currentPrice < trigger {
				logger.Debug("⏳ [觸發價] 當前價 %.2f < 觸發價 %.2f，等待後再啟動網格", currentPrice, trigger)
				return nil
			}
		} else {
			if currentPrice > trigger {
				logger.Debug("⏳ [觸發價] 當前價 %.2f > 觸發價 %.2f，等待後再啟動網格", currentPrice, trigger)
				return nil
			}
		}
	}

	// === 网格风控逻辑开始 ===
	if spm.config.Trading.GridRiskControl.Enabled {
		// 1. 硬為止损检查
		stopLossRatio := spm.config.Trading.GridRiskControl.StopLossRatio
		if stopLossRatio > 0 {
			unrealizedPnL := spm.calculateUnrealizedPnL(currentPrice)
			totalValue := spm.calculateTotalPositionValue(currentPrice)
			if totalValue > 0 {
				pnlRatio := unrealizedPnL / totalValue
				if pnlRatio <= -stopLossRatio {
					logger.Error("🚨 [网格风控] 触发硬為止损! 當前浮亏率: %.2f%%, 阈值: %.2f%%", pnlRatio*100, -stopLossRatio*100)
					// 发布止损事件，触发飞书/邮件等通知
					if spm.eventBus != nil {
						spm.eventBus.Publish(&event.Event{
							Type:      event.EventTypeStopLoss,
							Timestamp: time.Now(),
							Data: map[string]interface{}{
								"bot_id":         spm.botID,
								"symbol":         spm.config.Trading.Symbol,
								"exchange":       spm.exchangeName,
								"reason":         "grid_risk_control",
								"pnl_ratio_pct":  pnlRatio * 100,
								"threshold_pct":  stopLossRatio * 100,
								"unrealized_pnl": unrealizedPnL,
								"total_value":    totalValue,
							},
						})
					}
					spm.LiquidateAll()
					return nil
				}
			}
		}

		// 2. 动態止盈 (盈利回撤止盈) 检查
		triggerRatio := spm.config.Trading.GridRiskControl.TakeProfitTriggerRatio
		trailingRatio := spm.config.Trading.GridRiskControl.TrailingTakeProfitRatio
		if triggerRatio > 0 && trailingRatio > 0 {
			unrealizedPnL := spm.calculateUnrealizedPnL(currentPrice)
			totalValue := spm.calculateTotalPositionValue(currentPrice)
			if totalValue > 0 {
				currentProfitRatio := unrealizedPnL / totalValue

				// 更新最高盈利
				if currentProfitRatio > spm.peakPnL {
					spm.peakPnL = currentProfitRatio
					logger.Debug("💰 [网格风控] 更新最高盈利率: %.2f%%", spm.peakPnL*100)
				}

				// 如果盈利已經超過触发阈值，且從最高点回撤超過 trailingRatio
				if spm.peakPnL >= triggerRatio {
					drawdown := spm.peakPnL - currentProfitRatio
					if drawdown >= trailingRatio {
						logger.Warn("📈 [网格风控] 触发盈利回撤止盈! 最高盈利率: %.2f%%, 當前盈利率: %.2f%%, 回撤: %.2f%%, 阈值: %.2f%%",
							spm.peakPnL*100, currentProfitRatio*100, drawdown*100, trailingRatio*100)
						spm.LiquidateAll()
						spm.peakPnL = -math.MaxFloat64 // 重置最高点
						return nil
					}
				}
			} else {
				// 無持倉時重置最高盈利点
				spm.peakPnL = -math.MaxFloat64
			}
		}
	}
	// === 网格风控逻辑結束 ===

	// === 關閉條件：滿足時平倉並停止 Bot ===
	if spm.config.Trading.GridRiskControl.CloseConditionEnabled {
		profitTarget := spm.config.Trading.GridRiskControl.CloseConditionProfitTarget
		lossLimit := spm.config.Trading.GridRiskControl.CloseConditionLossLimit
		if profitTarget > 0 || lossLimit > 0 {
			unrealizedPnL := spm.calculateUnrealizedPnL(currentPrice)
			totalValue := spm.calculateTotalPositionValue(currentPrice)
			if totalValue > 0 {
				pnlRatio := unrealizedPnL / totalValue
				triggered := false
				reason := ""
				if profitTarget > 0 && pnlRatio >= profitTarget {
					triggered = true
					reason = fmt.Sprintf("盈利率 %.2f%% 達到目標 %.2f%%", pnlRatio*100, profitTarget*100)
				} else if lossLimit > 0 && pnlRatio <= -lossLimit {
					triggered = true
					reason = fmt.Sprintf("虧損率 %.2f%% 達到限制 %.2f%%", -pnlRatio*100, lossLimit*100)
				}
				if triggered {
					logger.Info("🛑 [關閉條件] 觸發: %s，執行平倉並停止 Bot", reason)
					if spm.eventBus != nil {
						spm.eventBus.Publish(&event.Event{
							Type:      event.EventTypeStopLoss,
							Timestamp: time.Now(),
							Data: map[string]interface{}{
								"bot_id":    spm.botID,
								"symbol":    spm.config.Trading.Symbol,
								"exchange":  spm.exchangeName,
								"reason":    "close_condition",
								"detail":    reason,
								"pnl_ratio": pnlRatio,
							},
						})
					}
					spm.LiquidateAll()
					if spm.requestStopFunc != nil {
						go spm.requestStopFunc()
					}
					return nil
				}
			}
		}
	}
	// === 關閉條件結束 ===

	// 检查保证金不足状態
	if spm.insufficientMargin {
		if time.Since(spm.marginLockTime) >= spm.marginLockDuration {
			logger.Info("✅ [保证金恢複] 鎖定時间已過，恢複下單功能")
			spm.insufficientMargin = false
		} else {
			remainingTime := spm.marginLockDuration - time.Since(spm.marginLockTime)
			logger.Warn("⏸️ [暂停下單] 保证金不足，暂停下單中... (剩餘時间: %.0f秒)", remainingTime.Seconds())
			return nil
		}
	}

	// 單向淨持倉雙向網格（BOTH）：下方買開多、上方賣開空；平倉 reduce_only
	if spm.isBoth() {
		return spm.adjustOrdersBoth(currentPrice)
	}

	// 计算需要監控的價格範圍
	buyWindowSize := spm.config.Trading.BuyWindowSize
	sellWindowSize := spm.config.Trading.SellWindowSize
	profitSpread := spm.getEffectiveProfitSpread()

	// 动態计算网格價格
	currentGridPrice := spm.findNearestGridPrice(currentPrice)
	// logger.Debug("🔄 [實時調整] 當前價格: %s, 网格價格: %s, 買單窗口: %d, 賣單視窗: %d",
	// 	formatPrice(currentPrice, spm.priceDecimals), formatPrice(currentGridPrice, spm.priceDecimals), buyWindowSize, sellWindowSize)

	// 计算槽位價格：LONG 向下（買低賣高），SHORT 向上（賣高買低）
	slotDir := "down"
	if spm.isShort() {
		slotDir = "up"
	}
	slotPrices := spm.calculateSlotPrices(currentGridPrice, buyWindowSize, slotDir)

	// 價格範圍軟限制：將槽位價格裁剪到 [PriceLow, PriceHigh] 範圍內
	priceLow := spm.config.Trading.PriceLow
	priceHigh := spm.config.Trading.PriceHigh
	if priceLow > 0 || priceHigh > 0 {
		filtered := make([]float64, 0, len(slotPrices))
		for _, p := range slotPrices {
			if priceLow > 0 && p < priceLow {
				continue
			}
			if priceHigh > 0 && p > priceHigh {
				continue
			}
			filtered = append(filtered, p)
		}
		slotPrices = filtered
	}

	// 🔥 P2 新增：根據訂單簿深度優化槽位價格
	slotPrices = spm.optimizeSlotPricesWithOrderBook(context.Background(), spm.config.Trading.Symbol, slotPrices)

	// 🔥 開倉掛單數限制：單向做多/做空時每筆開倉委託佔用保證金，限制掛單數可節省資金
	openCtrl := spm.config.Trading.OpenPositionControl
	priceInterval := spm.config.Trading.PriceInterval
	if priceInterval <= 0 {
		priceInterval = 1
	}
	dir := "LONG"
	if spm.isShort() {
		dir = "SHORT"
	}
	maxOpenOrders := 0
	openOrderDist := 0.0
	if openCtrl.BotRiskControl != nil && openCtrl.BotRiskControl.MaxOpenOrders > 0 {
		maxOpenOrders = openCtrl.BotRiskControl.MaxOpenOrders
		openOrderDist = openCtrl.BotRiskControl.OpenOrderDistance
		if openOrderDist <= 0 {
			openOrderDist = 3
		}
	} else if spm.config.Trading.SmartOrder.Enabled && spm.config.Trading.SmartOrder.MaxOpenOrders > 0 {
		maxOpenOrders = spm.config.Trading.SmartOrder.MaxOpenOrders
		openOrderDist = spm.config.Trading.SmartOrder.OpenOrderDistance
		if openOrderDist <= 0 {
			openOrderDist = 3
		}
	}
	if maxOpenOrders > 0 {
		slotPrices = FilterSlotsByMaxOpenOrders(slotPrices, currentPrice, priceInterval, maxOpenOrders, openOrderDist, dir)
		logger.Debug("🧠 [開倉掛單限制] 篩選後槽位數: %d (max_open_orders=%d)", len(slotPrices), maxOpenOrders)
	}

	var ordersToPlace []*OrderRequest
	var activeBuyOrdersInWindow int

	// 统计當前所有订單數量（分别统计買單和賣單）
	var currentOrderCount int
	var currentBuyOrderCount int
	var currentSellOrderCount int
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed ||
			slot.OrderStatus == OrderStatusPartiallyFilled {
			currentOrderCount++
			// LONG: 開倉=BUY 平倉=SELL；SHORT: 開倉=SELL 平倉=BUY
			openSide := "BUY"
			if spm.isShort() {
				openSide = "SELL"
			}
			if slot.OrderSide == openSide {
				currentBuyOrderCount++
			} else if slot.OrderSide != "" {
				currentSellOrderCount++
			}
		}
		slot.mu.RUnlock()
		return true
	})

	// 计算允許創建的订單數量上限
	threshold := spm.config.Trading.OrderCleanupThreshold
	if threshold <= 0 {
		threshold = 100
	}

	// 🔥 核心改進：不預留空间，允許订單數达到threshold上限
	// 剩餘可用订單數 = 阈值 - 當前订單數
	remainingOrders := threshold - currentOrderCount
	if remainingOrders < 0 {
		remainingOrders = 0
	}

	// 買單允許的新增數量
	allowedNewBuyOrders := buyWindowSize
	if allowedNewBuyOrders > remainingOrders {
		allowedNewBuyOrders = remainingOrders
	}

	// 1. 处理買單
	buyOrdersToCreate := 0

	// 趨勢過濾與层數限制預检查
	skipBuying := false
	// 價格範圍軟限制：超出範圍時暫停新開倉，保留平倉單
	if priceLow > 0 && currentPrice < priceLow {
		logger.Debug("⏸️ [價格範圍] 當前價 %.2f < 下限 %.2f，暫停新開倉", currentPrice, priceLow)
		skipBuying = true
	}
	if priceHigh > 0 && currentPrice > priceHigh {
		logger.Debug("⏸️ [價格範圍] 當前價 %.2f > 上限 %.2f，暫停新開倉", currentPrice, priceHigh)
		skipBuying = true
	}
	// 開倉管理：檢查是否暫停開倉
	if spm.IsOpeningPaused() {
		skipBuying = true
	}
	if spm.config.Trading.GridRiskControl.Enabled {
		// 趨勢過濾
		if spm.config.Trading.GridRiskControl.TrendFilterEnabled && spm.trendDetector != nil {
			trend := spm.trendDetector.GetCurrentTrend()
			if trend == "down" {
				logger.Warn("📉 [趨勢過濾] 检测到下跌趋势，暂停買入")
				skipBuying = true
			}
		}

		// 层數限制
		maxLayers := spm.config.Trading.GridRiskControl.MaxGridLayers
		if maxLayers > 0 {
			currentLayers := spm.GetActiveLayers()
			if currentLayers >= maxLayers {
				logger.Warn("🚫 [层數限制] 當前持倉层數 (%d) 已达到最大值 (%d)，暂停買入", currentLayers, maxLayers)
				skipBuying = true
			}
		}
	}

	// 🔥 開倉管理：檢查 Bot 獨立風控的倉位限制
	openControl := spm.config.Trading.OpenPositionControl

	// 一次性計算所有需要的倉位統計數據（避免多次遍歷）
	type positionStats struct {
		totalQty    float64
		totalValue  float64
		totalLayers int
	}
	stats := positionStats{}
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			stats.totalQty += slot.PositionQty
			stats.totalLayers++
		}
		slot.mu.RUnlock()
		return true
	})
	stats.totalValue = stats.totalQty * currentPrice

	// 優先檢查 Bot 獨立風控
	if openControl.BotRiskControl != nil && openControl.BotRiskControl.Enabled {
		// 檢查暫停狀態
		if openControl.BotRiskControl.PauseOpening {
			logger.Warn("⏸️ [Bot風控] Bot 開倉已暫停（原因: %s）", openControl.BotRiskControl.PauseOpeningReason)
			skipBuying = true
		}

		// 檢查數量限制
		if openControl.BotRiskControl.MaxPositionQuantity > 0 && stats.totalQty >= openControl.BotRiskControl.MaxPositionQuantity {
			logger.Warn("🚫 [Bot風控] 當前持倉數量 (%.4f) 已達到 Bot 限制 (%.4f)，暂停開倉",
				stats.totalQty, openControl.BotRiskControl.MaxPositionQuantity)
			skipBuying = true
		}

		// 檢查價值限制
		if openControl.BotRiskControl.MaxPositionValue > 0 && stats.totalValue >= openControl.BotRiskControl.MaxPositionValue {
			logger.Warn("🚫 [Bot風控] 當前倉位價值 (%.2f) 已達到 Bot 限制 (%.2f)，暂停開倉",
				stats.totalValue, openControl.BotRiskControl.MaxPositionValue)
			skipBuying = true
		}

		// 檢查層數限制
		if openControl.BotRiskControl.MaxPositionLayers > 0 && stats.totalLayers >= openControl.BotRiskControl.MaxPositionLayers {
			logger.Warn("🚫 [Bot風控] 當前持倉層數 (%d) 已達到 Bot 限制 (%d)，暂停開倉",
				stats.totalLayers, openControl.BotRiskControl.MaxPositionLayers)
			skipBuying = true
		}
	} else {
		// 如果沒有啟用 Bot 獨立風控，檢查全局配置
		if openControl.MaxPositionQuantity > 0 && stats.totalQty >= openControl.MaxPositionQuantity {
			logger.Warn("🚫 [開倉管理] 當前持倉數量 (%.4f) 已達到限制 (%.4f)，暂停開倉",
				stats.totalQty, openControl.MaxPositionQuantity)
			skipBuying = true
		}

		if openControl.MaxPositionValue > 0 && stats.totalValue >= openControl.MaxPositionValue {
			logger.Warn("🚫 [開倉管理] 當前倉位價值 (%.2f) 已達到限制 (%.2f)，暂停開倉",
				stats.totalValue, openControl.MaxPositionValue)
			skipBuying = true
		}

		if openControl.MaxPositionLayers > 0 && stats.totalLayers >= openControl.MaxPositionLayers {
			logger.Warn("🚫 [開倉管理] 當前持倉層數 (%d) 已達到限制 (%d)，暂停開倉",
				stats.totalLayers, openControl.MaxPositionLayers)
			skipBuying = true
		}
	}

	// 資金費率偏向策略檢查
	if spm.fundingMonitor != nil && spm.config.FundingRate.BiasEnabled {
		buyBias := spm.fundingMonitor.GetBuyBias()

		if buyBias == 0 {
			// 極高費率：完全暫停買入
			rate := spm.fundingMonitor.GetCurrentRate()
			logger.Warn("💰 [資金費率] 費率過高 (%.4f%%)，暫停買入", rate*100)
			skipBuying = true
		} else if buyBias < 1.0 {
			// 高費率：減少買單數量
			originalOrders := allowedNewBuyOrders
			allowedNewBuyOrders = int(float64(allowedNewBuyOrders) * buyBias)
			if allowedNewBuyOrders < 1 && originalOrders > 0 {
				allowedNewBuyOrders = 1 // 至少保留一個買單
			}
			rate := spm.fundingMonitor.GetCurrentRate()
			logger.Info("💰 [資金費率] 費率 %.4f%%，買單數量從 %d 減少到 %d (偏向係數: %.2f)",
				rate*100, originalOrders, allowedNewBuyOrders, buyBias)
		} else if buyBias > 1.0 {
			// 負費率：可略微增加買入（但不超過剩餘訂單數）
			originalOrders := allowedNewBuyOrders
			allowedNewBuyOrders = int(float64(allowedNewBuyOrders) * buyBias)
			if allowedNewBuyOrders > remainingOrders {
				allowedNewBuyOrders = remainingOrders
			}
			rate := spm.fundingMonitor.GetCurrentRate()
			logger.Info("💰 [資金費率] 負費率 %.4f%%，買單數量從 %d 增加到 %d (偏向係數: %.2f)",
				rate*100, originalOrders, allowedNewBuyOrders, buyBias)
		}
	}

	// 🔥 P1 新增：資金費率與趨勢聯動邏輯
	if spm.config.FundingRate.TrendSyncEnabled &&
		spm.fundingMonitor != nil && spm.trendDetector != nil &&
		spm.config.FundingRate.BiasEnabled && spm.config.Trading.GridRiskControl.TrendFilterEnabled {

		buyBias := spm.fundingMonitor.GetBuyBias()
		trend := spm.trendDetector.GetCurrentTrend()

		if buyBias > 1 && trend == "up" {
			// 負費率 + 上漲趨勢：只放寬趨勢過濾限制，不再重複乘係數（之前已乘過 buyBias）
			if skipBuying {
				skipBuying = false
				if allowedNewBuyOrders == 0 {
					allowedNewBuyOrders = 1
				}
				logger.Info("🔥 [費率趨勢聯動] 負費率(%.2f) + 上漲趨勢：放寬趨勢過濾限制", buyBias)
			}
		} else if buyBias < 1 && trend == "down" {
			// 高正費率 + 下跌趨勢：強化賣出偏向
			skipBuying = true
			allowedNewBuyOrders = 0
			rate := spm.fundingMonitor.GetCurrentRate()
			logger.Warn("🔥 [費率趨勢聯動] 高費率(%.4f%%) + 下跌趨勢：強制暫停買入", rate*100)
		}
	}

	// 🔥 智能開倉掛單限制：開倉單數超過 max_open_orders 時，動態撤銷最遠的委託單
	// 適用於 smart_order 或 open_position_control.bot_risk_control 的 max_open_orders 配置
	if maxOpenOrders > 0 && currentBuyOrderCount > maxOpenOrders {
		spm.CancelExcessOpenOrders(maxOpenOrders)
		// 撤單後需重新統計，避免本輪繼續下新單；下一輪價格更新時會重新評估
		currentBuyOrderCount = maxOpenOrders
	}

	// 最大持倉預警：達到層數上限時，若開倉單數超過允許值，先撤多餘的開倉單（做多先撤高價買單，做空先撤低價賣單）
	if spm.config.Trading.GridRiskControl.Enabled {
		maxLayers := spm.config.Trading.GridRiskControl.MaxGridLayers
		maxOpenAtCap := spm.config.Trading.GridRiskControl.MaxOpenOrdersAtCap
		if maxLayers > 0 && maxOpenAtCap > 0 {
			currentLayers := spm.GetActiveLayers()
			if currentLayers >= maxLayers && currentBuyOrderCount > maxOpenAtCap {
				spm.CancelExcessOpenOrders(maxOpenAtCap)
			}
		}
	}

	for _, price := range slotPrices {
		if skipBuying {
			break
		}

		// 🔥 新增：槽位過濾檢查
		if !spm.isSlotEnabled(price) {
			logger.Debug("⏭️ [槽位過濾] 跳過被禁用的價格位: %.2f", price)
			continue
		}

		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()

		// 🔥 槽位鎖定检查：如果槽位正在被操作，跳過
		if slot.SlotStatus != SlotStatusFree {
			slot.mu.Unlock()
			continue
		}

		// 检查是否已有有效订單
		hasActiveOrder := false
		if slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed ||
			slot.OrderStatus == OrderStatusPartiallyFilled {
			hasActiveOrder = true
			openSide := "BUY"
			if spm.isShort() {
				openSide = "SELL"
			}
			if slot.OrderSide == openSide {
				activeBuyOrdersInWindow++
			}
		}

		// 🔥 買單条件：持倉状態=EMPTY + 槽位鎖=FREE + 無订單ID + 無ClientOID
		if slot.PositionStatus != PositionStatusEmpty {
			slot.mu.Unlock()
			continue
		}

		// 🔥 新逻辑：只检查槽位鎖状態、OrderID和ClientOID，不检查OrderSide
		shouldCreateBuyOrder := !hasActiveOrder &&
			slot.SlotStatus == SlotStatusFree &&
			slot.OrderID == 0 &&
			slot.ClientOID == "" &&
			buyOrdersToCreate < allowedNewBuyOrders

		if shouldCreateBuyOrder {
			// 安全检查：LONG 買單價格應低於當前價格；SHORT 賣單價格應高於當前價格
			safetyBuffer := spm.config.Trading.PriceInterval * 0.1
			if spm.isShort() {
				if price <= currentPrice+safetyBuffer {
					slot.mu.Unlock()
					continue
				}
			} else {
				if price >= currentPrice-safetyBuffer {
					slot.mu.Unlock()
					continue
				}
			}

			quantity := spm.config.Trading.OrderQuantity / price
			// 使用從交易所獲取的數量精度
			quantity = roundPrice(quantity, spm.quantityDecimals)

			// 如果數量過小被取整為 0，发布告警並暂停
			if quantity <= 0 && spm.quantityDecimals >= 0 {
				minQty := math.Pow10(-spm.quantityDecimals)
				logger.Error("🚨 [%s] 下單數量過小 (%.8f)，低於交易所最小精度 (%.8f)，交易已自动暂停！请在配置中調大 order_quantity",
					spm.config.Trading.Symbol, spm.config.Trading.OrderQuantity/price, minQty)

				// 发布事件
				if spm.eventBus != nil {
					spm.eventBus.Publish(&event.Event{
						Type:      event.EventTypePrecisionAdjustment,
						Timestamp: time.Now(),
						Data: map[string]interface{}{
							"symbol":         spm.config.Trading.Symbol,
							"exchange":       spm.exchangeName,
							"order_quantity": spm.config.Trading.OrderQuantity,
							"calculated_qty": spm.config.Trading.OrderQuantity / price,
							"min_qty":        minQty,
							"price":          price,
							"action":         "pause",
							"reason":         "下單數量低於交易所最小精度",
						},
					})
				}

				// 暂停交易
				spm.Pause()
				slot.mu.Unlock()
				continue
			}

			// 生成 ClientOrderID：LONG=BUY，SHORT=SELL
			openSide := "BUY"
			if spm.isShort() {
				openSide = "SELL"
			}
			clientOID := spm.generateClientOrderID(price, openSide, "")

			// 🔥 鎖定槽位：標記為PENDING状態，防止並发操作
			slot.SlotStatus = SlotStatusPending

			// 检查PostOnly失败计數，失败3次后不再使用PostOnly
			usePostOnly := slot.PostOnlyFailCount < 3

			ordersToPlace = append(ordersToPlace, &OrderRequest{
				Symbol:        spm.config.Trading.Symbol,
				Side:          openSide,
				Price:         price,
				Quantity:      quantity,
				PriceDecimals: spm.priceDecimals,
				PostOnly:      usePostOnly,
				ClientOrderID: clientOID,
			})
			buyOrdersToCreate++
		}

		slot.mu.Unlock()
	}

	// 2. 处理平倉單（LONG=賣單，SHORT=買單）
	type closeCandidate struct {
		SlotPrice     float64
		ClosePrice    float64 // LONG: 賣出價=slot+interval；SHORT: 買入價=slot-interval
		Quantity      float64
		DistanceToMid float64
	}
	var closeCandidates []closeCandidate

	// LONG: 賣單窗口 above；SHORT: 買單窗口 below（窗口範圍用 profitSpread）
	sellWindowMaxPrice := currentPrice + float64(sellWindowSize)*profitSpread
	sellWindowMaxPrice = roundPrice(sellWindowMaxPrice, spm.priceDecimals)
	buyWindowMinPrice := currentPrice - float64(sellWindowSize)*profitSpread
	buyWindowMinPrice = roundPrice(buyWindowMinPrice, spm.priceDecimals)

	spm.slots.Range(func(key, value interface{}) bool {
		slotPrice := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.Lock()
		defer slot.mu.Unlock()

		if slot.PositionStatus == PositionStatusFilled &&
			slot.SlotStatus == SlotStatusFree &&
			slot.OrderID == 0 &&
			slot.ClientOID == "" {

			// 🔥 ReduceOnly 冷却期：该槽位近期 ReduceOnly 失败过，跳过避免重复告警
			if spm.isReduceOnlyCooldown(slotPrice) {
				return true
			}

			// 三級火箭模式：每個槽位使用其檔位對應的利差
			slotSpread := spm.getProfitSpreadForSlot(slotPrice, currentGridPrice)
			var closePrice float64
			if spm.isShort() {
				// SHORT: 買低平倉。必須保證買回價 <= 實際開空均價，否則波動/滑點時可能虧損。
				// AvgBuyPrice 在 SHORT 模式下存儲的是實際賣出（開空）均價。
				basePrice := slotPrice
				if slot.AvgBuyPrice > 0 && slot.AvgBuyPrice < slotPrice {
					basePrice = slot.AvgBuyPrice
					logger.Debug("📊 [%s] 槽位 %.2f 實際開空價(%.2f) < 網格價，平倉基準價調整為 %.2f",
						spm.logPrefix(), slotPrice, slot.AvgBuyPrice, basePrice)
				}
				closePrice = basePrice - slotSpread
			} else {
				// LONG: 賣高平倉。必須保證賣出價 >= 實際買入均價，否則波動/滑點時可能虧損
				basePrice := slotPrice
				if slot.AvgBuyPrice > 0 && slot.AvgBuyPrice > slotPrice {
					basePrice = slot.AvgBuyPrice
					logger.Debug("📊 [%s] 槽位 %.2f 實際買入價(%.2f) > 網格價，平倉基準價調整為 %.2f",
						spm.logPrefix(), slotPrice, slot.AvgBuyPrice, basePrice)
				}
				closePrice = basePrice + slotSpread
			}
			closePrice = roundPrice(closePrice, spm.priceDecimals)

			// 窗口检查：LONG 跳過 slot 高於上限；SHORT 跳過 close 低於下限
			if spm.isShort() {
				if closePrice < buyWindowMinPrice {
					return true
				}
			} else {
				if slotPrice > sellWindowMaxPrice {
					return true
				}
			}

			// 最小名义價值检查
			orderValue := closePrice * slot.PositionQty
			minValue := spm.config.Trading.MinOrderValue
			if minValue <= 0 {
				minValue = 6.0
			}

			if orderValue >= minValue {
				distance := math.Abs(slotPrice - currentPrice)
				closeCandidates = append(closeCandidates, closeCandidate{
					SlotPrice:     slotPrice,
					ClosePrice:    closePrice,
					Quantity:      slot.PositionQty,
					DistanceToMid: distance,
				})
			}
		}
		return true
	})

	// 按距离排序
	sort.Slice(closeCandidates, func(i, j int) bool {
		return closeCandidates[i].DistanceToMid < closeCandidates[j].DistanceToMid
	})

	// 🔥 重新计算賣單的剩餘配額（扣除新增買單后的剩餘空间）
	remainingOrdersForSell := threshold - currentOrderCount - buyOrdersToCreate
	if remainingOrdersForSell < 0 {
		remainingOrdersForSell = 0
	}

	allowedNewSellOrders := sellWindowSize
	if allowedNewSellOrders > remainingOrdersForSell {
		allowedNewSellOrders = remainingOrdersForSell
	}

	// 生成賣單请求
	sellOrdersToCreate := 0
	// 🔥 調試日志: 显示订單配額计算详情（包含買賣單分布），含 bot ID 便於區分多實例
	logger.Debug("📊 [%s] [订單配額] 阈值:%d, 當前订單:%d(開:%d/平:%d), 剩餘:%d, 新增開倉:%d, 平倉候选:%d, 允許平倉:%d",
		spm.logPrefix(), threshold, currentOrderCount, currentBuyOrderCount, currentSellOrderCount, remainingOrders, buyOrdersToCreate, len(closeCandidates), allowedNewSellOrders)
	if allowedNewSellOrders > 0 {
		closeSide := "SELL"
		if spm.isShort() {
			closeSide = "BUY"
		}
		for i := 0; i < len(closeCandidates) && sellOrdersToCreate < allowedNewSellOrders; i++ {
			candidate := closeCandidates[i]

			// 🔥 关键修複：最终驗证PositionStatus必須為FILLED且有持倉，並且SlotStatus為FREE
			slot := spm.getOrCreateSlot(candidate.SlotPrice)
			slot.mu.Lock()

			// 🔥 双重检查：确保槽位仍然是FREE状態
			if slot.SlotStatus != SlotStatusFree {
				slot.mu.Unlock()
				continue
			}

			currentStatus := slot.PositionStatus
			currentQty := slot.PositionQty

			if currentStatus != PositionStatusFilled || currentQty <= 0 {
				slot.mu.Unlock()
				continue
			}

			// 🔥 立即鎖定槽位：標記為PENDING状態，防止並发操作
			slot.SlotStatus = SlotStatusPending
			// 检查PostOnly失败计數，失败3次后不再使用PostOnly
			usePostOnly := slot.PostOnlyFailCount < 3
			slot.mu.Unlock()

			// 生成 ClientOrderID
			clientOID := spm.generateClientOrderID(candidate.SlotPrice, closeSide, "")

			quantity := candidate.Quantity
			// 兜底检查：平倉單數量必須大於0
			if quantity <= 0 && spm.quantityDecimals >= 0 {
				minQty := math.Pow10(-spm.quantityDecimals)
				logger.Error("🚨 [%s] 平倉單數量异常 (%.8f)，低於交易所最小精度 (%.8f)，交易已自动暂停！",
					spm.config.Trading.Symbol, candidate.Quantity, minQty)

				// 发布事件
				if spm.eventBus != nil {
					spm.eventBus.Publish(&event.Event{
						Type:      event.EventTypePrecisionAdjustment,
						Timestamp: time.Now(),
						Data: map[string]interface{}{
							"symbol":   spm.config.Trading.Symbol,
							"exchange": spm.exchangeName,
							"quantity": candidate.Quantity,
							"min_qty":  minQty,
							"price":    candidate.ClosePrice,
							"action":   "pause",
							"reason":   "平倉單數量低於交易所最小精度",
						},
					})
				}

				// 暂停交易（slot 已在前面 unlock）
				spm.Pause()
				continue
			}

			ordersToPlace = append(ordersToPlace, &OrderRequest{
				Symbol:        spm.config.Trading.Symbol,
				Side:          closeSide,
				Price:         candidate.ClosePrice,
				Quantity:      quantity,
				PriceDecimals: spm.priceDecimals,
				ReduceOnly:    !spm.isSpot(), // 平倉單需要 ReduceOnly
				PostOnly:      usePostOnly,
				ClientOrderID: clientOID,
			})
			sellOrdersToCreate++
		}
	}

	// 🔥 去重检查：如果同一價格同時有開倉單和平倉單，移除開倉單（平倉優先）
	// 場景：LONG模式下，空倉槽位P挂買單，同時已持倉槽位(P-interval)的平倉價也是P
	// 同價掛買賣單毫無意義，且可能觸發自成交，因此移除開倉單
	openSideForDedup := "BUY"
	if spm.isShort() {
		openSideForDedup = "SELL"
	}
	closePriceSet := make(map[float64]bool)
	for _, order := range ordersToPlace {
		if order.Side != openSideForDedup {
			closePriceSet[order.Price] = true
		}
	}
	if len(closePriceSet) > 0 {
		var filteredOrders []*OrderRequest
		removedBuyCount := 0
		for _, order := range ordersToPlace {
			if order.Side == openSideForDedup && closePriceSet[order.Price] {
				// 同一價格有平倉單，跳過開倉單
				logger.Warn("⚠️ [%s] 同一價格 %s 同時有開倉和平倉單，移除開倉單（平倉優先）",
					spm.logPrefix(), formatPrice(order.Price, spm.priceDecimals))
				// 重置被移除的開倉單對應槽位狀態（之前被標記為PENDING）
				if slotRaw, ok := spm.slots.Load(order.Price); ok {
					pendingSlot := slotRaw.(*InventorySlot)
					pendingSlot.mu.Lock()
					if pendingSlot.SlotStatus == SlotStatusPending {
						pendingSlot.SlotStatus = SlotStatusFree
					}
					pendingSlot.mu.Unlock()
				}
				removedBuyCount++
				buyOrdersToCreate--
				continue
			}
			filteredOrders = append(filteredOrders, order)
		}
		if removedBuyCount > 0 {
			ordersToPlace = filteredOrders
			logger.Info("📊 [%s] 去重完成：移除了 %d 個與平倉單同價的開倉單",
				spm.logPrefix(), removedBuyCount)
		}
	}

	ordersToPlace = spm.clipSpotBuyOrdersByQuoteBudget(ordersToPlace, openSideForDedup)

	// 🔥 在下單前，先检查並調整资金限額（分级限額功能）
	// 计算當前持倉层數和未實現盈亏
	positionLayers := 0
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			positionLayers++
		}
		slot.mu.RUnlock()
		return true
	})
	unrealizedPnL := spm.calculateUnrealizedPnL(currentPrice)

	// 調用分级限額检查（可能會自动切换到紧急限額或恢複正常限額）
	spm.allocationManager.CheckAndAdjustLimit(
		spm.exchangeName,
		spm.config.Trading.Symbol,
		currentPrice,
		spm.anchorPrice(),
		positionLayers,
		unrealizedPnL,
	)

	// 執行下單前，检查资金分配
	if len(ordersToPlace) > 0 {
		// 獲取帳戶餘額（從交易所獲取實際餘額）
		var accountBalance float64 = 0
		var accountResult interface{} = nil
		ctx := context.Background()
		if spm.exchange != nil {
			var err error
			accountResult, err = spm.exchange.GetAccount(ctx)
			if err == nil && accountResult != nil {
				// 使用反射獲取 AvailableBalance 字段
				// 注意：不同交易所可能返回不同的類型，使用反射统一处理
				accountValue := reflect.ValueOf(accountResult)
				if accountValue.Kind() == reflect.Ptr {
					accountValue = accountValue.Elem()
				}
				if balanceField := accountValue.FieldByName("AvailableBalance"); balanceField.IsValid() && balanceField.CanInterface() {
					if balance, ok := balanceField.Interface().(float64); ok {
						accountBalance = balance
					}
				}
				// 使用可用餘額（AvailableBalance）進行资金分配检查
				// 注意：對於合約账戶，如果有持倉，AvailableBalance可能為0，这是正常的
				logger.Debug("💰 [%s] [资金分配] 账戶可用餘額: %.2f USDT", spm.logPrefix(), accountBalance)
			} else {
				logger.Warn("⚠️ [%s] [资金分配] 無法獲取帳戶餘額: %v，使用0作為默认值", spm.logPrefix(), err)
			}
		}

		// 獲取杠杆倍數（用於计算實際使用的保证金）
		// 注意：API 限流/失敗時 accountResult 可能為 (*T)(nil) 轉 interface{}，Elem() 後得 zero Value，對其 FieldByName 會 panic，需先檢查 IsValid
		leverage := 1 // 默认1倍（無杠杆）
		if accountResult != nil {
			accountValue := reflect.ValueOf(accountResult)
			if accountValue.Kind() == reflect.Ptr && !accountValue.IsNil() {
				accountValue = accountValue.Elem()
			}
			if accountValue.IsValid() && accountValue.Kind() == reflect.Struct {
				if leverageField := accountValue.FieldByName("AccountLeverage"); leverageField.IsValid() && leverageField.CanInterface() {
					if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
						leverage = lev
					}
				}
			}
		}
		// 如果從账戶中獲取不到，尝試從持倉中獲取
		if leverage == 1 && spm.exchange != nil {
			if positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol); err == nil && positionsInterface != nil {
				// 使用反射處理不同類型的持倉資訊
				positionsValue := reflect.ValueOf(positionsInterface)
				if positionsValue.Kind() == reflect.Slice {
					for i := 0; i < positionsValue.Len(); i++ {
						posValue := positionsValue.Index(i)
						if posValue.Kind() == reflect.Interface {
							posValue = posValue.Elem()
						}
						if posValue.Kind() == reflect.Ptr {
							posValue = posValue.Elem()
						}
						if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
							if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
								leverage = lev
								break
							}
						}
					}
				}
			}
		}

		// 過滤掉超出资金分配的订單
		var validOrders []*OrderRequest
		for _, req := range ordersToPlace {
			orderValue := req.Quantity * req.Price // 订單名义金額（倉位價值）
			// 對於有杠杆的交易，實際使用的保证金 = 訂單價值 / 杠杆倍數
			// 资金限額限制的是實際投入的资金，而不是倉位價值
			actualMargin := orderValue / float64(leverage)
			err := spm.allocationManager.CheckAndReserve(
				spm.exchangeName,
				spm.config.Trading.Symbol,
				actualMargin, // 使用實際保证金而不是訂單價值
				accountBalance,
			)

			if err != nil {
				logger.Warn("⚠️ [%s] [资金分配] %v (訂單價值: %.2f USDT, 實際保证金: %.2f USDT, 杠杆: %dx)",
					spm.logPrefix(), err, orderValue, actualMargin, leverage)
				// 触发告警事件
				if spm.eventBus != nil {
					spm.eventBus.Publish(&event.Event{
						Type: event.EventTypeAllocationExceeded,
						Data: map[string]interface{}{
							"exchange":      spm.exchangeName,
							"symbol":        spm.config.Trading.Symbol,
							"error":         err.Error(),
							"order_value":   orderValue,
							"actual_margin": actualMargin,
							"leverage":      leverage,
						},
					})
				}
				// 释放槽位鎖
				if price, _, valid := spm.parseClientOrderID(req.ClientOrderID); valid {
					slot := spm.getOrCreateSlot(price)
					slot.mu.Lock()
					if slot.SlotStatus == SlotStatusPending {
						slot.SlotStatus = SlotStatusFree
					}
					slot.mu.Unlock()
				}
				continue
			}
			validOrders = append(validOrders, req)
		}

		ordersToPlace = validOrders
	}

	// 執行下單
	if len(ordersToPlace) > 0 {
		logger.Debug("🔄 [%s] [實時調整] 需要新增: %d 個订單", spm.logPrefix(), len(ordersToPlace))
		result := spm.executor.BatchPlaceOrdersWithDetails(ordersToPlace)

		if result.HasMarginError {
			errLabel := "保证金不足"
			if spm.isSpot() {
				errLabel = "餘額不足"
			}
			logger.Warn("⚠️ [%s] 检测到錯误，暂停下單 %d 秒", errLabel, int(spm.marginLockDuration.Seconds()))
			spm.insufficientMargin = true
			spm.marginLockTime = time.Now()
			if spm.isBoth() {
				spm.CancelAllOpenOrders()
			} else {
				spm.CancelAllBuyOrders()
			}

			// 发送保证金/餘額不足告警事件
			if spm.eventBus != nil {
				spm.eventBus.Publish(&event.Event{
					Type: event.EventTypeMarginInsufficient,
					Data: map[string]interface{}{
						"exchange":      spm.exchangeName,
						"symbol":        spm.config.Trading.Symbol,
						"failed_orders": len(result.PlacedOrders),
						"error_message": errLabel + "，已暂停下單",
						"lock_duration": int(spm.marginLockDuration.Seconds()),
					},
				})
			}
		}

		// 🔥 構建成功订單的ClientOrderID集合
		placedClientOIDs := make(map[string]bool)
		for _, ord := range result.PlacedOrders {
			placedClientOIDs[ord.ClientOrderID] = true
		}

		// 🔥 处理 ReduceOnly 錯误：清空對应槽位的持倉
		for clientOID := range result.ReduceOnlyErrors {
			price, side, valid := spm.parseClientOrderID(clientOID)
			if !valid {
				// 解析失败时 fallback：从 ordersToPlace 中根据 ClientOrderID 反推槽位价格
				var fallbackPrice float64
				for _, req := range ordersToPlace {
					if req.ClientOrderID == clientOID {
						profitSpread := spm.getEffectiveProfitSpread()
						if req.Side == "SELL" {
							fallbackPrice = req.Price - profitSpread // LONG: closePrice = slotPrice + spread
						} else {
							fallbackPrice = req.Price + profitSpread // SHORT: closePrice = slotPrice - spread
						}
						break
					}
				}
				if fallbackPrice > 0 {
					price = fallbackPrice
					side = "SELL"
					if spm.isShort() {
						side = "BUY"
					}
					valid = true
					logger.Warn("⚠️ [ReduceOnly錯誤處理] ClientOrderID 解析失败，使用 fallback 反推槽位價格=%s", formatPrice(price, spm.priceDecimals))
				} else {
					logger.Warn("⚠️ [ReduceOnly錯誤處理] 無法解析 ClientOrderID=%s，跳過清空槽位（可能導致重複告警）", clientOID)
					continue
				}
			}
			if valid {
				if side == "SELL" {
					// SELL ReduceOnly：平多倉失败，清空槽位持倉状態
					slot := spm.getOrCreateSlot(price)
					slot.mu.Lock()
					if slot.PositionStatus == PositionStatusFilled {
						logger.Warn("⚠️ [ReduceOnly錯誤處理] 清空槽位持倉: 價格=%s, 原持倉=%.4f",
							formatPrice(price, spm.priceDecimals), slot.PositionQty)
						// 清空持倉状態
						slot.PositionStatus = PositionStatusEmpty
						slot.PositionQty = 0
						slot.SlotStatus = SlotStatusFree
					}
					slot.mu.Unlock()
					// 記錄冷却期，2 分钟内不再尝试该槽位平仓
					spm.reduceOnlyCooldown.Store(price, time.Now())
				} else if side == "BUY" {
					// BUY ReduceOnly：平空倉失败，账戶中無空倉（系统不管理空倉状態，僅記錄日志）
					logger.Warn("⚠️ [ReduceOnly錯誤處理] BUY平空倉订單被拒绝: 價格=%s, 账戶中無空倉",
						formatPrice(price, spm.priceDecimals))
					spm.reduceOnlyCooldown.Store(price, time.Now())
				}
			}
		}

		// 🔥 释放未成功提交订單的槽位鎖和资金
		for _, req := range ordersToPlace {
			if !placedClientOIDs[req.ClientOrderID] && !result.ReduceOnlyErrors[req.ClientOrderID] {
				// 這個订單没有成功提交（且不是ReduceOnly錯误，因為已經处理過了），需要释放槽位鎖和资金
				price, side, valid := spm.parseClientOrderID(req.ClientOrderID)
				if valid {
					slot := spm.getOrCreateSlot(price)
					slot.mu.Lock()
					if slot.SlotStatus == SlotStatusPending {
						slot.SlotStatus = SlotStatusFree
						logger.Debug("🔓 [释放槽位] 订單提交失败，释放槽位 %s 的鎖 (ClientOID: %s)",
							formatPrice(price, spm.priceDecimals), req.ClientOrderID)
					}
					slot.mu.Unlock()

					// 🔥 释放預留的资金（只有買單需要释放，賣單不占用资金）
					if side == "BUY" {
						orderValue := req.Quantity * req.Price
						actualMargin := spm.getActualMargin(orderValue)
						if actualMargin > 0 {
							spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
							logger.Debug("💰 [资金释放] 订單提交失败，释放預留资金: %.2f USDT (訂單價值: %.2f USDT)", actualMargin, orderValue)
						}
					}
				}
			}
		}

		for _, ord := range result.PlacedOrders {
			// 解析 ClientOrderID
			price, side, valid := spm.parseClientOrderID(ord.ClientOrderID)

			if !valid {
				logger.Warn("⚠️ [%s] [實時調整] 無法解析 ClientOID: %s", spm.logPrefix(), ord.ClientOrderID)
				continue
			}

			// 獲取槽位 (注意：無論是買單还是賣單，ID中编碼的都是 SlotPrice)
			slot := spm.getOrCreateSlot(price)
			slot.mu.Lock()

			// 🔥 关键修複：检查是否是秒成交场景（買單或賣單都可能）
			// 秒成交的特征:
			// 1. 買單秒成交: PositionStatus=FILLED (刚成交) 且 OrderID=0 (已被WebSocket清空) 且 OrderSide=""
			// 2. 賣單秒成交: PositionStatus=EMPTY (已清空) 且 OrderID=0 (已被WebSocket清空) 且 OrderSide=""
			isInstantFill := false
			if side == "BUY" {
				// 買單秒成交: 有持倉但订單ID為0且OrderSide已清空
				isInstantFill = (slot.PositionStatus == PositionStatusFilled && slot.OrderID == 0 && slot.OrderSide == "")
			} else if side == "SELL" {
				// 🔥 賣單秒成交: 持倉已清空且订單ID為0且OrderSide已清空
				isInstantFill = (slot.PositionStatus == PositionStatusEmpty && slot.OrderID == 0 && slot.OrderSide == "" && slot.SlotStatus == SlotStatusFree)
			}

			if !isInstantFill {
				// 正常情况: 更新订單状態
				// 🔥 检查OrderID冲突：只有當ClientOID已設置且不匹配時才是真正的冲突
				// 如果ClientOID為空或匹配，說明是正常的WebSocket先到或批量处理顺序问题
				if slot.OrderID != 0 && slot.OrderID != ord.OrderID {
					if slot.ClientOID != "" && slot.ClientOID != ord.ClientOrderID {
						// 真正的冲突：槽位已被其他订單占用
						logger.Warn("⚠️ [OrderID冲突] 槽位 %.2f: 下單返回OrderID=%d (ClientOID=%s)，但槽位已被OrderID=%d (ClientOID=%s)占用",
							price, ord.OrderID, ord.ClientOrderID, slot.OrderID, slot.ClientOID)
					} else {
						// WebSocket推送先到达，这是正常現象
						logger.Debug("📝 [覆盖OrderID] 槽位 %.2f: WebSocket已設置OrderID=%d，現用下單返回的OrderID=%d (ClientOID: %s)",
							price, slot.OrderID, ord.OrderID, ord.ClientOrderID)
					}
				}

				slot.OrderID = ord.OrderID
				slot.ClientOID = ord.ClientOrderID
				slot.OrderSide = side // "BUY" or "SELL"
				slot.OrderStatus = OrderStatusPlaced
				slot.OrderPrice = ord.Price
				slot.OrderCreatedAt = time.Now()
				// 🔥 订單提交成功，設置為LOCKED状態
				slot.SlotStatus = SlotStatusLocked
				// 保存策略信息
				slot.StrategyName = spm.strategyName
				slot.StrategyType = spm.strategyType
				// 注意：不在这里重置PostOnlyFailCount，因為订單可能立即被撤销
				// PostOnly计數只在订單真正成交時重置

				logger.Debug("✅ [實時新增] 槽位價格: %s, %s订單, 订單價格: %s, 订單ID: %d, ClientOID: %s",
					formatPrice(price, spm.priceDecimals), side, formatPrice(ord.Price, spm.priceDecimals), ord.OrderID, ord.ClientOrderID)
			} else {
				// 🔍 秒成交场景：WebSocket已經处理了FILLED,跳過状態更新
				logger.Debug("🔍 [%s單秒成交] 槽位 %s 的订單已被WebSocket处理，跳過状態更新 (持倉: %.4f, SlotStatus: %s)",
					side, formatPrice(price, spm.priceDecimals), slot.PositionQty, slot.SlotStatus)
			}

			slot.mu.Unlock()
		}
	}

	return nil
}
