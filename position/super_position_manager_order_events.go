package position

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"quantmesh/logger"
	"quantmesh/storage"
)

func gridTradeExecutionKey(spm *SuperPositionManager, orderID int64, cumulativeQty float64) string {
	return GridTradeExecutionKey(spm.botID, spm.exchangeName, spm.config.Trading.MarketType, orderID, spm.config.Trading.Symbol, cumulativeQty)
}

// GridTradeExecutionKey creates a stable identity for one cumulative order fill.
func GridTradeExecutionKey(botID, exchangeName, marketType string, orderID int64, symbol string, cumulativeQty float64) string {
	identity := fmt.Sprintf("grid|%s|%s|%s|%d|%s|%s", strings.TrimSpace(botID), strings.ToLower(strings.TrimSpace(exchangeName)), strings.ToLower(strings.TrimSpace(marketType)), orderID, strings.TrimSpace(symbol), strconv.FormatFloat(cumulativeQty, 'f', -1, 64))
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func (spm *SuperPositionManager) saveGridTradeIdempotently(trade *storage.Trade) error {
	writer, ok := spm.tradeStorage.(interface{ SaveTradeIdempotent(*storage.Trade) error })
	if !ok {
		return fmt.Errorf("trade storage does not support idempotent writes")
	}
	return writer.SaveTradeIdempotent(trade)
}

func (spm *SuperPositionManager) requireTradeLedgerReconciliation(update OrderUpdate, cause error, tradeRecord ...*storage.Trade) {
	spm.openingGate.Block("trade_ledger_unverified")
	var payload []byte
	if len(tradeRecord) > 0 && tradeRecord[0] != nil {
		encoded, err := json.Marshal(tradeRecord[0])
		if err != nil {
			logger.Error("[%s] 成交账本待核账凭据序列化失败：order=%d cid=%s err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, err)
		} else {
			payload = encoded
		}
	}
	if len(payload) > 0 {
		if tracker, ok := spm.executor.(interface {
			MarkTradeLedgerRecordReconciliationRequired(int64, string, string, []byte) error
		}); ok {
			if err := tracker.MarkTradeLedgerRecordReconciliationRequired(update.OrderID, update.ClientOrderID, cause.Error(), payload); err != nil {
				logger.Error("[%s] 成交账本失败且可重放凭据未能持久化：order=%d cid=%s err=%v tracker_err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, cause, err)
				return
			}
			logger.Error("[%s] 成交账本失败；已持久化可重放凭据與經濟對賬鎖：order=%d cid=%s err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, cause)
			return
		}
	}
	if tracker, ok := spm.executor.(interface {
		MarkTradeLedgerReconciliationRequired(int64, string, string) error
	}); ok {
		if err := tracker.MarkTradeLedgerReconciliationRequired(update.OrderID, update.ClientOrderID, cause.Error()); err != nil {
			logger.Error("[%s] 成交账本失败；经济对账状态持久化失败：order=%d cid=%s err=%v tracker_err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, cause, err)
			return
		}
		logger.Error("[%s] 成交账本失败；订单结果仍可核实，已持久化经济对账锁：order=%d cid=%s err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, cause)
		return
	}
	tracker, ok := spm.executor.(interface {
		MarkOrderReconciliationRequired(int64, string, string) error
	})
	if !ok {
		logger.Error("[%s] 成交账本写入失败且执行器不支持持久对账锁：order=%d cid=%s err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, cause)
		return
	}
	if err := tracker.MarkOrderReconciliationRequired(update.OrderID, update.ClientOrderID, cause.Error()); err != nil {
		logger.Error("[%s] 成交账本写入失败；持久化执行意图对账锁失败：order=%d cid=%s err=%v tracker_err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, cause, err)
		return
	}
	logger.Error("[%s] 成交账本写入失败；已持久化对账锁并暂停新开仓：order=%d cid=%s err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, cause)
}

// ========== 訂單更新事件處理（OnOrderUpdate）==========

// OnOrderUpdate 订單更新回呼（异步订單同步流）
func (spm *SuperPositionManager) OnOrderUpdate(update OrderUpdate) bool {
	update.Status = normalizeOrderStatus(update.Status)
	spm.onOrderUpdate(update)
	spm.refreshCostBasisOpeningGate()
	if !spm.persistGridRuntimeStateOrHold(update) {
		spm.gridRuntimeStateMu.RLock()
		storeMissing := spm.gridRuntimeStateStore == nil
		spm.gridRuntimeStateMu.RUnlock()
		if storeMissing {
			// Isolated/unit runtimes without a persistent store retain the
			// historical asynchronous behavior. Production startup remains
			// blocked by its required exposure journal when storage is absent.
			spm.startPendingFeeSupplements()
		}
		return false
	}
	spm.startPendingFeeSupplements()
	if update.ExecutedQty != 0 || (update.Status != "CANCELED" && update.Status != "EXPIRED" && update.Status != "REJECTED") {
		return false
	}
	price, _, valid := spm.parseClientOrderID(update.ClientOrderID)
	if !valid {
		return false
	}
	raw, exists := spm.slots.Load(price)
	if !exists {
		return false
	}
	slot := raw.(*InventorySlot)
	slot.mu.RLock()
	accounted := slot.OrderID == 0 && slot.ClientOID == "" && slot.OrderStatus == OrderStatusCanceled &&
		spm.sameClientOrderID(slot.lastFilledClientOID, update.ClientOrderID)
	slot.mu.RUnlock()
	return accounted
}

func (spm *SuperPositionManager) persistGridRuntimeStateOrHold(update OrderUpdate) bool {
	spm.gridRuntimeStateMu.RLock()
	hasStore := spm.gridRuntimeStateStore != nil
	spm.gridRuntimeStateMu.RUnlock()
	if !hasStore {
		return false
	}
	if err := spm.PersistGridRuntimeState(); err != nil {
		spm.openingGate.Block("grid_runtime_state_unverified")
		if tracker, ok := spm.executor.(interface {
			MarkOrderReconciliationRequired(int64, string, string) error
		}); ok {
			if markErr := tracker.MarkOrderReconciliationRequired(update.OrderID, update.ClientOrderID, err.Error()); markErr != nil {
				logger.Error("[%s] 网格运行态快照失败且无法持久化订单核账锁: order=%d cid=%s snapshot_err=%v journal_err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, err, markErr)
			} else {
				logger.Error("[%s] 网格运行态快照失败，已持久化订单核账锁并暂停新开仓: order=%d cid=%s err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, err)
			}
		} else {
			logger.Error("[%s] 网格运行态快照失败且执行器不支持持久化订单核账锁: order=%d cid=%s err=%v", spm.logPrefix(), update.OrderID, update.ClientOrderID, err)
		}
		return false
	}
	return true
}

func (spm *SuperPositionManager) onOrderUpdate(update OrderUpdate) {
	update.Status = normalizeOrderStatus(update.Status)
	// 任何訂單/成交事件都要求下一個 tick 全量重算掛單（AdjustOrders 去抖）
	spm.markAdjustDirty()

	// 🔥 重構：完全依赖 ClientOrderID 解析
	price, side, valid := spm.parseClientOrderID(update.ClientOrderID)

	var slot *InventorySlot
	if !valid {
		// 兜底：若 ClientOrderID 無法解析，改用 OrderID 在現有槽位中反查，避免因前綴截斷直接丟回報
		foundSlot, foundPrice, ok := spm.findSlotByClientOrderID(update.ClientOrderID)
		if !ok {
			foundSlot, foundPrice, ok = spm.findSlotByOrderID(update.OrderID)
		}
		if !ok {
			logger.Debug("⏳ [忽略] 無法识别的订單更新: ID=%d, ClientOID=%s", update.OrderID, update.ClientOrderID)
			return
		}
		slot = foundSlot
		price = foundPrice
		slot.mu.RLock()
		if slot.OrderSide != "" {
			side = slot.OrderSide
		}
		slot.mu.RUnlock()
	} else {
		slot = spm.getOrCreateSlot(price)
	}
	if side == "" && update.Side != "" {
		side = strings.ToUpper(update.Side)
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	// Normalize broker aliases before matching ownership and fee cursors.
	if spm.sameClientOrderID(slot.ClientOID, update.ClientOrderID) {
		update.ClientOrderID = slot.ClientOID
	} else if spm.sameClientOrderID(slot.lastFilledClientOID, update.ClientOrderID) {
		update.ClientOrderID = slot.lastFilledClientOID
	}

	// 校驗：确保這個更新属於當前的订單 (防止舊订單的延迟推送干扰新订單)
	// 优先使用 ClientOrderID 匹配 (某些交易所如 Gate.io 的 OrderID 可能略有差异)
	if slot.ClientOID != "" && slot.ClientOID != update.ClientOrderID {
		// ClientOrderID 不匹配，忽略此更新
		logger.Info("⚠️ [订單更新被忽略] 槽位 %.2f: ClientOID不匹配 (槽位: %s, 推送: %s, OrderID: %d)",
			price, slot.ClientOID, update.ClientOrderID, update.OrderID)
		return
	}
	// 已完全成交並重置的訂單再次推送（WS 重放/重連補推/延遲到達）：不得再次記賬持倉與手續費
	if slot.ClientOID == "" && update.ClientOrderID != "" && update.ClientOrderID == slot.lastFilledClientOID {
		logger.Debug("⏭️ [订單更新被忽略] 槽位 %.2f: 訂單 %s 已完全成交，忽略重複推送 (status=%s)",
			price, update.ClientOrderID, update.Status)
		return
	}

	// 更新订單ID (如果是首個推送)
	if slot.OrderID == 0 {
		logger.Debug("📝 [首次設置OrderID] 槽位 %.2f: OrderID=%d, ClientOID=%s", price, update.OrderID, update.ClientOrderID)
		slot.OrderID = update.OrderID
		slot.ClientOID = update.ClientOrderID
		slot.OrderSide = side
	} else if slot.OrderID != update.OrderID {
		// OrderID 不一致但 ClientOrderID 匹配，更新 OrderID (Gate.io 批量下單可能出現此情况)
		logger.Debug("📝 [更新OrderID] 槽位 %.2f: %d -> %d (ClientOID: %s)", price, slot.OrderID, update.OrderID, update.ClientOrderID)
		slot.OrderID = update.OrderID
	}

	// 資金預留記賬 key：優先推送中的 ClientOrderID，兜底用槽位記錄的
	orderClientOID := update.ClientOrderID
	if orderClientOID == "" {
		orderClientOID = slot.ClientOID
	}
	fillBearingStatus := update.Status == "PARTIALLY_FILLED" || update.Status == "FILLED" || update.Status == "CANCELED" || update.Status == "EXPIRED" || update.Status == "REJECTED"
	if fillBearingStatus && update.ExecutedQty > slot.OrderFilledQty && !positiveFinite(update.AvgPrice) {
		slot.OrderStatus = OrderStatusUnknown
		slot.SlotStatus = SlotStatusLocked
		spm.openingGate.Block("unknown_orders")
		if tracker, ok := spm.executor.(interface {
			MarkOrderReconciliationRequired(int64, string, string) error
		}); ok {
			if err := tracker.MarkOrderReconciliationRequired(update.OrderID, orderClientOID, "grid fill has no valid cumulative average price"); err != nil {
				logger.Error("[%s] 网格成交均价缺失且无法持久化对账锁: order=%d cid=%s err=%v", spm.logPrefix(), update.OrderID, orderClientOID, err)
			}
		}
		logger.Error("[%s] 网格订单 #%d 新增成交缺少有效累计均价，保留订单/槽位待对账", spm.logPrefix(), update.OrderID)
		return
	}

	terminal := update.Status == "CANCELED" || update.Status == "EXPIRED" || update.Status == "REJECTED"
	// 終態回報也可能攜帶尚未收到的成交；先處理成交，再清理未成交部分。
	switch update.Status {
	case "NEW":
		if slot.OrderStatus == OrderStatusPlaced {
			slot.OrderStatus = OrderStatusConfirmed
		}

	case "PARTIALLY_FILLED", "FILLED", "CANCELED", "EXPIRED", "REJECTED":
		progress := FillProgress{Quantity: slot.OrderFilledQty, Notional: slot.OrderFilledNotional}
		deltaQty, incrementalPrice := progress.Advance(update.ExecutedQty, update.AvgPrice, 0)
		if math.IsNaN(update.ExecutedQty) || math.IsInf(update.ExecutedQty, 0) || update.ExecutedQty < 0 ||
			((terminal || update.Status == "FILLED") && update.ExecutedQty < slot.OrderFilledQty) ||
			(update.Status == "FILLED" && update.ExecutedQty == 0) ||
			(update.ExecutedQty > slot.OrderFilledQty && progress.Quantity != update.ExecutedQty) {
			// A terminal label cannot release an unconsumable execution record.
			slot.OrderStatus = OrderStatusUnknown
			slot.SlotStatus = SlotStatusLocked
			spm.openingGate.Block("unknown_orders")
			logger.Error("[%s] 訂單 #%d 成交資料無效，保留歸屬並等待對賬", spm.logPrefix(), update.OrderID)
			return
		}
		slot.OrderFilledQty, slot.OrderFilledNotional = progress.Quantity, progress.Notional
		if terminal || update.Status == "FILLED" {
			slot.lastTerminalFill = progress
		}

		// 手續費按「每筆成交」累計：交易所在每次 PARTIALLY_FILLED/FILLED 推送中攜帶的是本筆成交的手續費。
		// 僅在成交數量有正增量時記賬，重複/重放的同一推送（增量為 0）不會重複累加。
		fillCommission := 0.0
		if deltaQty > 0 {
			openingFill := spm.isOpenLegOrderSide(side, slot)
			var feeKnown bool
			fillCommission, feeKnown = spm.commissionInQuote(update, incrementalPrice)
			if !feeKnown {
				slot.feeValuationUnknown = true
				spm.requireTradeLedgerReconciliation(update, fmt.Errorf("execution fee in %s has no verified quote-asset conversion", update.CommissionAsset))
			} else if openingFill && fillCommission == 0 && strings.TrimSpace(update.CommissionAsset) == "" {
				// 零值且未提供費用幣種無法區分真實零費用與缺失回報；終態 REST 補查前不將浮盈視為完整。
				slot.feeValuationUnknown = true
			}
			slot.addOrderCommissionLocked(orderClientOID, fillCommission, update.BaseFeeQty)
		}

		// 根據方向更新持倉：LONG 時 BUY=開倉(加倉) SELL=平倉(減倉)；SHORT 時 SELL=開倉 BUY=平倉
		// BOTH：依槽位 PositionLeg 判斷開倉/平倉
		if spm.isOpenLegOrderSide(side, slot) {
			if deltaQty > 0 {
				if spm.isBoth() && slot.PositionLeg == "" {
					if side == "BUY" {
						slot.PositionLeg = PositionLegLong
					} else {
						slot.PositionLeg = PositionLegShort
					}
				}
				// 🔥 更新平均买入价格（使用实际成交价格）
				actualBuyPrice := incrementalPrice

				// 🔥 监控价格偏差：实际成交价格与委托价格的差异
				if slot.OrderPrice > 0 && actualBuyPrice > 0 {
					priceDeviation := (actualBuyPrice - slot.OrderPrice) / slot.OrderPrice * 100
					// 如果价格偏差超过0.1%（买入价格高于委托价格），记录警告
					if priceDeviation > 0.1 {
						logger.Warn("⚠️ [價格偏差警告] 買單實際成交價高於委託價: 委託價=%.2f, 實際價=%.2f, 偏差=%.4f%%, 數量=%.4f, OrderID=%d",
							slot.OrderPrice, actualBuyPrice, priceDeviation, deltaQty, update.OrderID)
					} else if priceDeviation < -0.1 {
						// 买入价格低于委托价格（有利偏差），记录信息
						logger.Info("💰 [價格偏差] 買單實際成交價低於委託價（有利）: 委託價=%.2f, 實際價=%.2f, 偏差=%.4f%%, 數量=%.4f",
							slot.OrderPrice, actualBuyPrice, priceDeviation, deltaQty)
					}
				}

				// 現貨買入且手續費以基礎幣扣收：實際到帳 = 成交增量 − 基礎幣手續費
				receivedQty := spm.netReceivedQty(side, deltaQty, update.BaseFeeQty)
				// 计算新的平均买入价格
				if slot.CostBasisUnverified {
					slot.AvgBuyPrice = 0
				} else if slot.PositionQty > 0 && slot.AvgBuyPrice > 0 {
					// 加权平均：(旧价格 * 旧数量 + 新价格 * 新数量) / 总数量
					totalCost := slot.AvgBuyPrice*slot.PositionQty + actualBuyPrice*receivedQty
					if total := slot.PositionQty + receivedQty; total > 0 {
						slot.AvgBuyPrice = totalCost / total
					}
				} else {
					// 首次买入或之前没有持仓，直接使用当前买入价格
					slot.AvgBuyPrice = actualBuyPrice
				}

				slot.PositionQty += receivedQty
				if receivedQty != deltaQty {
					// 訂單結束時再向下取整到數量精度（見 floorBaseFeePositionLocked），部分成交期間保留精確值避免逐筆截斷累積損失
					slot.baseFeeUnfloored = true
					logger.Info("🪙 [現貨基礎幣手續費] 槽位 %s: 成交 %.8f, 基礎幣手續費 %.8f, 實際到帳 %.8f",
						formatPrice(price, spm.priceDecimals), deltaQty, update.BaseFeeQty, receivedQty)
				}
				// 🔥 累計開倉手續費（平倉時按比例攤銷）
				slot.BuyFee += fillCommission
				if fillCommission != 0 {
					slot.FeeAsset = spm.feeQuoteAsset()
				}
				// D3：成交部分的預留资金轉為槽位持倉占用（平倉成交時再按比例釋放）
				spm.applyOpeningFillAllocationLocked(slot, orderClientOID, deltaQty, actualBuyPrice, update.Status == "FILLED")
				// 累加统计
				oldTotal := spm.totalBuyQty.Load().(float64)
				spm.totalBuyQty.Store(oldTotal + deltaQty)
			}

			if update.Status == "FILLED" {
				if deltaQty <= 0 {
					// 最後一筆成交增量已在 PARTIALLY_FILLED 中處理：把剩餘預留全部轉為持倉占用
					spm.applyOpeningFillAllocationLocked(slot, orderClientOID, 0, 0, true)
				}
				// 🔥 整筆訂單的推送都未帶手續費（如 Bybit order topic），異步查詢補充一次（按訂單全部成交匯總）
				spm.startFeeSupplementLocked(slot, update, orderClientOID, side, true)
				spm.floorBaseFeePositionLocked(slot)
				slot.OrderStatus = OrderStatusNotPlaced // 重置订單状態
				slot.OrderID = 0
				slot.ClientOID = ""
				slot.OrderSide = "" // 🔥 清除订單方向，避免误判
				slot.OrderFilledQty = 0
				slot.OrderFilledNotional = 0
				slot.lastFilledClientOID = orderClientOID

				slot.PositionStatus = PositionStatusFilled // 標記為有倉
				if spm.isBoth() {
					if side == "BUY" {
						slot.PositionLeg = PositionLegLong
					} else {
						slot.PositionLeg = PositionLegShort
					}
				}
				// 🔥 释放槽位鎖：買單成交，允許后续挂賣單
				slot.SlotStatus = SlotStatusFree
				// 🔥 買單成交，重置PostOnly失败计數
				slot.PostOnlyFailCount = 0

				logger.Info("✅ [買單成交] 價格: %s, 持倉: %.4f, 槽位状態: %s -> %s, 订單状態: %s -> %s, SlotStatus: FREE",
					formatPrice(price, spm.priceDecimals), slot.PositionQty,
					PositionStatusEmpty, PositionStatusFilled,
					"FILLED", OrderStatusNotPlaced)
				logger.Debug("🔍 [買單成交后] 等待下次AdjustOrders調用時挂出賣單...")

				spm.recordFill()

				// 通知套利管理器：買入成交（正數表示買入）
				if spm.arbitrageManager != nil && update.ExecutedQty > 0 {
					spm.arbitrageManager.OnGridPositionChange(update.ExecutedQty, update.Price)
				}
			} else {
				slot.OrderStatus = OrderStatusPartiallyFilled
			}

		} else { // SELL
			if deltaQty > 0 {
				// 🔥 关键修复：在减少 PositionQty 之前，先计算买入手续费摊销
				// 这样可以正确处理全平仓的情况（止损单全平时，PositionQty 会变为0）
				var feeFromBuy float64
				positionQtyBeforeSell := slot.PositionQty // 保存卖出前的持仓数量
				if positionQtyBeforeSell > 0 {
					feeFromBuy = slot.BuyFee * (deltaQty / positionQtyBeforeSell)
				} else {
					// 如果卖出前持仓为0，说明是异常情况，使用全部买入手续费
					feeFromBuy = slot.BuyFee
				}

				slot.PositionQty -= deltaQty
				if slot.PositionQty < 0 {
					slot.PositionQty = 0
				}
				// D3：平倉成交按比例釋放該槽位的持倉资金占用（只用內存數據，不做網絡請求）
				if released := spm.releaseSlotAllocationLocked(slot, deltaQty, positionQtyBeforeSell); released > 0 {
					logger.Debug("💰 [资金释放] 平倉成交，释放持倉占用: %.2f USDT (減倉: %.4f)", released, deltaQty)
				}
				// 累加统计
				oldTotal := spm.totalSellQty.Load().(float64)
				spm.totalSellQty.Store(oldTotal + deltaQty)

				// 🔥 保存交易記錄（買賣配對完成）
				if spm.tradeStorage != nil {
					// 🔥 使用实际平均买入价格（而不是槽位基准价格）
					// 这样可以准确反映实际盈亏，特别是当实际买入价格与槽位价格不同时
					buyPrice := slot.AvgBuyPrice
					if buyPrice <= 0 {
						logger.Error("[%s] 槽位缺少实际开仓均价，拒绝用槽位委托价伪造已实现盈亏", spm.logPrefix())
					}

					// 賣出價格使用成交均價，如果没有则使用订單價格
					sellPrice := incrementalPrice

					// 🔥 监控卖出价格偏差：实际成交价格与委托价格的差异
					if slot.OrderPrice > 0 && sellPrice > 0 {
						sellPriceDeviation := (sellPrice - slot.OrderPrice) / slot.OrderPrice * 100
						// 如果卖出价格低于委托价格超过0.1%（不利偏差），记录警告
						if sellPriceDeviation < -0.1 {
							logger.Warn("⚠️ [價格偏差警告] 賣單實際成交價低於委託價: 委託價=%.2f, 實際價=%.2f, 偏差=%.4f%%, 數量=%.4f, OrderID=%d",
								slot.OrderPrice, sellPrice, sellPriceDeviation, deltaQty, update.OrderID)
						} else if sellPriceDeviation > 0.1 {
							// 卖出价格高于委托价格（有利偏差），记录信息
							logger.Info("💰 [價格偏差] 賣單實際成交價高於委託價（有利）: 委託價=%.2f, 實際價=%.2f, 偏差=%.4f%%, 數量=%.4f",
								slot.OrderPrice, sellPrice, sellPriceDeviation, deltaQty)
						}
					}

					// 🔥 驗证價格和數量的合理性
					if slot.feeValuationUnknown {
						spm.requireTradeLedgerReconciliation(update, fmt.Errorf("grid cycle contains an execution fee without verified quote conversion"))
					} else if buyPrice <= 0 || sellPrice <= 0 || deltaQty <= 0 {
						logger.Warn("⚠️ [交易記錄异常] 買入價: %.2f, 賣出價: %.2f, 數量: %.4f, 跳過保存",
							buyPrice, sellPrice, deltaQty)
						spm.requireTradeLedgerReconciliation(update, fmt.Errorf("invalid realized trade economics: buy=%.8f sell=%.8f quantity=%.8f", buyPrice, sellPrice, deltaQty))
					} else {
						// 计算盈亏：(賣出價格 - 實際買入價格) * 數量（毛利，未扣手續費）
						// 注意：對於USDT本位合約（如BTCUSDT），價格是USDT，數量是BTC，盈亏單位是USDT
						pnl := (sellPrice - buyPrice) * deltaQty

						// 🔥 检查价格偏差对策略的影响：如果实际盈亏与理论盈亏差异过大，警告
						theoreticalPnL := (slot.OrderPrice - slot.Price) * deltaQty // 理论盈亏（基于槽位价格）
						if spm.isShort() || (spm.isBoth() && slot.PositionLeg == PositionLegShort) {
							pnl = -pnl
							theoreticalPnL = -theoreticalPnL
						}
						if theoreticalPnL > 0 && pnl < 0 {
							// 理论应该盈利，但实际亏损了（价格偏差导致策略失效）
							logger.Error("🚨 [策略失效警告] 理論應盈利但實際虧損: 槽位價=%.2f, 委託賣價=%.2f, 實際買價=%.2f, 實際賣價=%.2f, 理論盈虧=%.4f, 實際盈虧=%.4f, 數量=%.4f",
								slot.Price, slot.OrderPrice, buyPrice, sellPrice, theoreticalPnL, pnl, deltaQty)
						}

						// 🔥 手續費：買入攤銷 + 賣出本次手續費（feeFromBuy 已在上面计算）
						totalFee := feeFromBuy + fillCommission
						// BuyFee and fillCommission are both stored in quote-asset units,
						// regardless of the venue's original commission token.
						feeAsset := spm.feeQuoteAsset()
						if feeAsset == "" {
							feeAsset = slot.FeeAsset
						}

						// 🔥 添加合理性检查：如果盈亏异常大，記錄警告
						// 對於BTCUSDT，如果價格差是100 USDT，數量是0.01 BTC，盈亏应該是1 USDT
						// 如果盈亏超過订單金額的50%，可能是计算錯误
						orderAmount := buyPrice * deltaQty
						if orderAmount > 0 && math.Abs(pnl) > orderAmount*0.5 {
							logger.Warn("⚠️ [盈亏异常] 買入價: %.2f, 賣出價: %.2f, 數量: %.4f, 盈亏: %.2f, 订單金額: %.2f, 盈亏率: %.2f%%",
								buyPrice, sellPrice, deltaQty, pnl, orderAmount, (pnl/orderAmount)*100)
						}

						// 🔥 计算价格偏差（实际成交价格 - 委托价格）
						// 买入价格偏差：实际平均买入价格 - 槽位基准价格（槽位价格是买入委托价格）
						buyPriceDeviation := (buyPrice - slot.Price) * deltaQty // USDT单位

						// 卖出价格偏差：实际卖出价格 - 委托卖出价格
						sellPriceDeviation := (sellPrice - slot.OrderPrice) * deltaQty // USDT单位

						// 保存交易記錄（買入订單ID設為0，因為無法追溯历史订單）
						buyOrderID := int64(0)
						sellOrderID := update.OrderID
						// 🔥 添加详细日志，特别是对于亏损交易
						if pnl < 0 {
							logger.Warn("🛑 [亏损交易] 槽位價格: %s, 實際買入價: %s, 賣出價: %s, 數量: %.4f, 盈亏: %.4f, 手續費: %.4f %s, OrderID: %d, 買入偏差: %.4f, 賣出偏差: %.4f",
								formatPrice(slot.Price, spm.priceDecimals), formatPrice(buyPrice, spm.priceDecimals), formatPrice(sellPrice, spm.priceDecimals), deltaQty, pnl, totalFee, feeAsset, sellOrderID, buyPriceDeviation, sellPriceDeviation)
						}

						// 🔥 获取交易所计算的已实现盈亏（从订单更新中获取）
						exchangePnL := update.RealizedPnL

						trade := &storage.Trade{
							ExecutionKey: gridTradeExecutionKey(spm, sellOrderID, slot.OrderFilledQty),
							BuyOrderID:   buyOrderID, SellOrderID: sellOrderID, BotID: spm.botID,
							Exchange: spm.exchangeName, MarketType: spm.config.Trading.MarketType, PnLAsset: spm.feeQuoteAsset(), Symbol: update.Symbol,
							BuyPrice: buyPrice, SellPrice: sellPrice, Quantity: deltaQty, PnL: pnl, ExchangePnL: exchangePnL,
							Fee: totalFee, FeeAsset: feeAsset, BuyPriceDeviation: buyPriceDeviation,
							SellPriceDeviation: sellPriceDeviation, CreatedAt: spm.now(),
						}
						if err := spm.saveGridTradeIdempotently(trade); err != nil {
							spm.requireTradeLedgerReconciliation(update, err, trade)
						}
						slot.BuyFee -= feeFromBuy
					}
				}
			} else if deltaQty > 0 {
				spm.requireTradeLedgerReconciliation(update, fmt.Errorf("trade storage unavailable"))
			}

			if update.Status == "FILLED" {
				// 🔥 整筆平倉單推送都未帶手續費，異步查詢補充一次（交易記錄已保存，補查結果寫入更正記錄）
				spm.startFeeSupplementLocked(slot, update, orderClientOID, side, false)
				slot.OrderStatus = OrderStatusNotPlaced // 重置订單状態
				slot.OrderID = 0
				slot.ClientOID = ""
				slot.OrderSide = "" // 🔥 清除订單方向，避免误判
				slot.OrderFilledQty = 0
				slot.OrderFilledNotional = 0
				slot.lastFilledClientOID = orderClientOID

				if slot.PositionQty < 0.000001 {
					slot.PositionStatus = PositionStatusEmpty // 標記為空倉
					slot.resetPositionCycleLocked()
					if spm.isBoth() {
						slot.PositionLeg = PositionLegNone
					}
				}
				// 🔥 释放槽位鎖：賣單成交，允許后续挂買單
				slot.SlotStatus = SlotStatusFree
				// 🔥 賣單成交，重置PostOnly失败计數
				slot.PostOnlyFailCount = 0

				// 持倉已清空：釋放槽位剩餘的资金占用（處理精度殘留）
				if slot.PositionStatus == PositionStatusEmpty {
					spm.releaseSlotAllocationLocked(slot, 0, 0)
				}

				logger.Info("✅ [賣單成交] 價格: %s, 剩餘持倉: %.4f, 槽位状態: %s, 订單状態: %s, SlotStatus: FREE",
					formatPrice(price, spm.priceDecimals), slot.PositionQty, slot.PositionStatus, slot.OrderStatus)

				spm.recordFill()

				// 通知套利管理器：賣出成交（負數表示賣出）
				if spm.arbitrageManager != nil && update.ExecutedQty > 0 {
					spm.arbitrageManager.OnGridPositionChange(-update.ExecutedQty, update.Price)
				}
			} else {
				slot.OrderStatus = OrderStatusPartiallyFilled
			}
		}

	}
	if terminal {
		logger.Info("⚠️ [订單%s] 價格: %s, 方向: %s, 原因: %s, 已成交: %.4f",
			update.Status, formatPrice(price, spm.priceDecimals), side, update.Status, slot.OrderFilledQty)

		// 🔥 释放资金（D3）：按 ClientOrderID 歸還該訂單尚未轉為持倉的預留；
		// 已成交部分已在成交回報中轉為槽位持倉占用。平倉單沒有預留，這裡為 0。
		if released := spm.releaseOrderReservation(orderClientOID); released > 0 {
			logger.Debug("💰 [资金释放] 订單%s，释放未成交預留资金: %.2f USDT (方向: %s, 已成交: %.4f)",
				update.Status, released, side, slot.OrderFilledQty)
		}

		// 🔥 核心修複：按開倉腿/平倉腿處理槽位状態（LONG: BUY=開倉；SHORT: SELL=開倉；BOTH: 依槽位腿）
		if side != "BUY" && side != "SELL" {
			logger.Warn("⚠️ [订單%s] 價格: %s, 無法識別訂單方向 %q，僅清空訂單信息",
				update.Status, formatPrice(price, spm.priceDecimals), side)
		} else if spm.isOpenLegOrderSide(side, slot) {
			// 開倉單被取消/拒绝
			if slot.PositionQty > 0 || slot.OrderFilledQty > 0 {
				// 部分成交后被取消：保留持倉，允許后续挂平倉單
				logger.Info("💡 [開倉單部分成交后取消] 價格: %s, 方向: %s, 持倉: %.4f, 轉為有倉状態",
					formatPrice(price, spm.priceDecimals), side, slot.PositionQty)
				spm.floorBaseFeePositionLocked(slot)
				slot.PositionStatus = PositionStatusFilled
				slot.SlotStatus = SlotStatusFree // 允許挂平倉單
				// 部分成交的推送都未帶手續費：補查一次已成交部分的手續費
				if slot.OrderFilledQty > 0 {
					spm.startFeeSupplementLocked(slot, update, orderClientOID, side, true)
				}
			} else {
				// 完全未成交被取消：重置為空槽位
				logger.Info("🔄 [開倉單未成交取消] 價格: %s, 方向: %s, 重置槽位為空闲",
					formatPrice(price, spm.priceDecimals), side)
				slot.PositionStatus = PositionStatusEmpty
				if spm.isBoth() {
					slot.PositionLeg = PositionLegNone
				}
				slot.SlotStatus = SlotStatusFree // 允許重新挂開倉單
			}
		} else {
			// 平倉單被取消/拒绝：应該还持有倉位，保持持倉状態
			if slot.PositionQty > 0 {
				// 增加PostOnly连续被拒计數（订單被交易所撤销通常是PostOnly/GTX過期）：
				// 下次平倉價會按此計數逐 tick 遠離盤口（封頂 post_only_reprice_max_attempts），始終保持 PostOnly
				slot.PostOnlyFailCount++
				logger.Info("🔄 [平倉單取消] 價格: %s, 方向: %s, 保持持倉状態: %.4f, 等待重挂, PostOnly失败计數: %d",
					formatPrice(price, spm.priceDecimals), side, slot.PositionQty, slot.PostOnlyFailCount)
				slot.PositionStatus = PositionStatusFilled
				slot.SlotStatus = SlotStatusFree // 允許重新挂平倉單
			} else {
				// 异常情况：平倉單取消但没有持倉，重置為空
				logger.Warn("⚠️ [异常] 平倉單取消但無持倉，價格: %s, 方向: %s, 重置為空",
					formatPrice(price, spm.priceDecimals), side)
				slot.PositionStatus = PositionStatusEmpty
				slot.resetPositionCycleLocked()
				if spm.isBoth() {
					slot.PositionLeg = PositionLegNone
				}
				spm.releaseSlotAllocationLocked(slot, 0, 0)
				slot.SlotStatus = SlotStatusFree
			}
		}

		// 清空订單信息
		slot.OrderStatus = OrderStatusCanceled
		slot.OrderID = 0
		slot.ClientOID = ""
		slot.OrderFilledQty = 0
		slot.OrderFilledNotional = 0
		slot.lastFilledClientOID = orderClientOID
		// 保留 OrderSide 用於日志調試
	}
}
