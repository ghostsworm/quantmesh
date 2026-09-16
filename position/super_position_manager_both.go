package position

import (
	"context"
	"math"
	"sort"
	"time"

	"quantmesh/event"
	"quantmesh/logger"
	"quantmesh/strategy/regime"
)

// effectiveShortOpenWindowSize BOTH：向上開空層數；0 時繼承 sell_window 再 buy_window
func (spm *SuperPositionManager) effectiveShortOpenWindowSize() int {
	n := spm.config.Trading.ShortOpenWindowSize
	if n > 0 {
		return n
	}
	if spm.config.Trading.SellWindowSize > 0 {
		return spm.config.Trading.SellWindowSize
	}
	if spm.config.Trading.BuyWindowSize > 0 {
		return spm.config.Trading.BuyWindowSize
	}
	return 1
}

// bothSideIsOpen 判斷當前成交是否為開倉腿（相對於槽位腿別）
func bothSideIsOpen(side string, slot *InventorySlot) bool {
	switch slot.PositionLeg {
	case PositionLegLong:
		return side == "BUY"
	case PositionLegShort:
		return side == "SELL"
	default:
		return slot.PositionStatus == PositionStatusEmpty || slot.PositionQty < 1e-12
	}
}

// adjustOrdersBoth 單向淨持倉雙向網格：下買開多、上賣開空；平多賣、平空買（reduce_only）
func (spm *SuperPositionManager) adjustOrdersBoth(currentPrice float64) error {
	buyWindowSize := spm.config.Trading.BuyWindowSize
	sellWindowSize := spm.config.Trading.SellWindowSize
	shortOpenW := spm.effectiveShortOpenWindowSize()
	profitSpread := spm.getEffectiveProfitSpread()

	// R5：按腿應用 regime 策略、庫存偏斜、資金費定價（均默認關閉）
	now := time.Now()
	regimeView := spm.loadRegimeTick()
	spm.logRegimeTransition(regimeView)
	longPlan := spm.planOpeningLeg(regime.DirectionLong, buyWindowSize, currentPrice, regimeView, now)
	shortPlan := spm.planOpeningLeg(regime.DirectionShort, shortOpenW, currentPrice, regimeView, now)
	buyWindowSize = longPlan.Window
	shortOpenW = shortPlan.Window

	currentGridPrice := spm.findNearestGridPrice(currentPrice)

	slotPricesDown := spm.calculateSlotPrices(currentGridPrice, buyWindowSize, "down")
	slotPricesUp := spm.calculateSlotPrices(currentGridPrice, shortOpenW, "up")

	priceLow, priceHigh := spm.effectivePriceBounds(regimeView)
	if priceLow > 0 || priceHigh > 0 {
		slotPricesDown = filterPricesInRange(slotPricesDown, priceLow, priceHigh)
		slotPricesUp = filterPricesInRange(slotPricesUp, priceLow, priceHigh)
	}
	slotPricesDown = spm.applyFreezeBound(regime.DirectionLong, longPlan.FreezeBound, slotPricesDown, currentGridPrice)
	slotPricesUp = spm.applyFreezeBound(regime.DirectionShort, shortPlan.FreezeBound, slotPricesUp, currentGridPrice)

	slotPricesDown = spm.optimizeSlotPricesWithOrderBook(context.Background(), spm.config.Trading.Symbol, slotPricesDown)
	slotPricesUp = spm.optimizeSlotPricesWithOrderBook(context.Background(), spm.config.Trading.Symbol, slotPricesUp)

	priceInterval := spm.config.Trading.PriceInterval
	if priceInterval <= 0 {
		priceInterval = 1
	}
	openCtrl := spm.config.Trading.OpenPositionControl
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
		slotPricesDown = FilterSlotsByMaxOpenOrders(slotPricesDown, currentPrice, priceInterval, maxOpenOrders, openOrderDist, "LONG")
		slotPricesUp = FilterSlotsByMaxOpenOrders(slotPricesUp, currentPrice, priceInterval, maxOpenOrders, openOrderDist, "SHORT")
	}

	var currentOrderCount int
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed ||
			slot.OrderStatus == OrderStatusPartiallyFilled {
			currentOrderCount++
		}
		slot.mu.RUnlock()
		return true
	})

	threshold := spm.config.Trading.OrderCleanupThreshold
	if threshold <= 0 {
		threshold = 100
	}
	remainingOrders := threshold - currentOrderCount
	if remainingOrders < 0 {
		remainingOrders = 0
	}

	allowedNewLongBuys := buyWindowSize
	if allowedNewLongBuys > remainingOrders {
		allowedNewLongBuys = remainingOrders
	}
	allowedNewShortSells := shortOpenW
	if allowedNewShortSells > remainingOrders {
		allowedNewShortSells = remainingOrders
	}

	skipLongBuy := false
	skipShortSell := false
	if priceLow > 0 && currentPrice < priceLow {
		logger.Debug("⏸️ [價格範圍] BOTH 當前價 %.2f < 下限 %.2f，暫停買開", currentPrice, priceLow)
		skipLongBuy = true
	}
	if priceHigh > 0 && currentPrice > priceHigh {
		logger.Debug("⏸️ [價格範圍] BOTH 當前價 %.2f > 上限 %.2f，暫停賣開", currentPrice, priceHigh)
		skipShortSell = true
	}
	if spm.IsOpeningPaused() {
		skipLongBuy = true
		skipShortSell = true
	}
	if longPlan.StopOpening {
		skipLongBuy = true
	}
	if shortPlan.StopOpening {
		skipShortSell = true
	}

	// 舊 tick 級趨勢過濾（已廢棄；啟用 regime_filter 時由 K 線 regime 策略取代）
	if spm.config.Trading.GridRiskControl.Enabled && spm.config.Trading.GridRiskControl.TrendFilterEnabled && spm.trendDetector != nil && !regimeView.filterActive() {
		trend := spm.trendDetector.GetCurrentTrend()
		if trend == "down" {
			logger.Warn("📉 [趨勢過濾:BOTH] 下跌趨勢，暫停買開")
			skipLongBuy = true
		}
		if trend == "up" {
			logger.Warn("📈 [趨勢過濾:BOTH] 上漲趨勢，暫停賣開")
			skipShortSell = true
		}
	}

	if spm.config.Trading.GridRiskControl.Enabled {
		maxLayers := spm.config.Trading.GridRiskControl.MaxGridLayers
		if maxLayers > 0 {
			cur := spm.GetActiveLayers()
			if cur >= maxLayers {
				logger.Warn("🚫 [层數限制:BOTH] 已达到 %d 層，暫停買開", maxLayers)
				skipLongBuy = true
			}
		}
	}

	if trend, trendOK := spm.syncTrend(regimeView); trendOK && spm.config.FundingRate.TrendSyncEnabled &&
		spm.fundingMonitor != nil && spm.config.FundingRate.BiasEnabled {
		buyBias := spm.fundingMonitor.GetBuyBias()
		if buyBias > 1 && trend == "up" {
			if skipLongBuy {
				skipLongBuy = false
				if allowedNewLongBuys == 0 {
					allowedNewLongBuys = 1
				}
				logger.Info("🔥 [費率趨勢聯動:BOTH] 負費率+上漲：放寬買開限制")
			}
		} else if buyBias < 1 && trend == "down" {
			skipLongBuy = true
			allowedNewLongBuys = 0
			logger.Warn("🔥 [費率趨勢聯動:BOTH] 高費率+下跌：暫停買開")
		}
	}

	var ordersToPlace []*OrderRequest
	longBuys := 0
	shortSells := 0

	safetyBuffer := spm.config.Trading.PriceInterval * 0.1

	// 買開多
	for _, price := range slotPricesDown {
		if skipLongBuy || longBuys >= allowedNewLongBuys {
			break
		}
		if !spm.isSlotEnabled(price) {
			continue
		}
		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()
		if slot.SlotStatus != SlotStatusFree {
			slot.mu.Unlock()
			continue
		}
		hasActive := slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed ||
			slot.OrderStatus == OrderStatusPartiallyFilled
		if slot.PositionStatus != PositionStatusEmpty {
			slot.mu.Unlock()
			continue
		}
		if price >= currentPrice-safetyBuffer {
			slot.mu.Unlock()
			continue
		}
		if !hasActive && slot.OrderID == 0 && slot.ClientOID == "" {
			orderPrice := spm.shiftedOpenPrice(price, longPlan.PriceShift, "BUY")
			qty := spm.config.Trading.OrderQuantity / orderPrice
			qty = roundPrice(qty, spm.quantityDecimals)
			if qty <= 0 && spm.quantityDecimals >= 0 {
				slot.mu.Unlock()
				continue
			}
			coid := spm.generateClientOrderID(price, "BUY", "")
			slot.SlotStatus = SlotStatusPending
			ordersToPlace = append(ordersToPlace, &OrderRequest{
				Symbol: spm.config.Trading.Symbol, Side: "BUY", Price: orderPrice, Quantity: qty,
				PriceDecimals: spm.priceDecimals, PostOnly: gridOrdersPostOnly, ClientOrderID: coid,
			})
			longBuys++
		}
		slot.mu.Unlock()
	}

	// 賣開空
	for _, price := range slotPricesUp {
		if skipShortSell || shortSells >= allowedNewShortSells || longBuys+shortSells >= remainingOrders {
			break
		}
		if !spm.isSlotEnabled(price) {
			continue
		}
		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()
		if slot.SlotStatus != SlotStatusFree {
			slot.mu.Unlock()
			continue
		}
		hasActive := slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed ||
			slot.OrderStatus == OrderStatusPartiallyFilled
		if slot.PositionStatus != PositionStatusEmpty {
			slot.mu.Unlock()
			continue
		}
		if price <= currentPrice+safetyBuffer {
			slot.mu.Unlock()
			continue
		}
		if !hasActive && slot.OrderID == 0 && slot.ClientOID == "" {
			orderPrice := spm.shiftedOpenPrice(price, shortPlan.PriceShift, "SELL")
			qty := spm.config.Trading.OrderQuantity / orderPrice
			qty = roundPrice(qty, spm.quantityDecimals)
			if qty <= 0 && spm.quantityDecimals >= 0 {
				slot.mu.Unlock()
				continue
			}
			coid := spm.generateClientOrderID(price, "SELL", "")
			slot.SlotStatus = SlotStatusPending
			ordersToPlace = append(ordersToPlace, &OrderRequest{
				Symbol: spm.config.Trading.Symbol, Side: "SELL", Price: orderPrice, Quantity: qty,
				PriceDecimals: spm.priceDecimals, PostOnly: gridOrdersPostOnly, ClientOrderID: coid,
			})
			shortSells++
		}
		slot.mu.Unlock()
	}

	type closeCand struct {
		SlotPrice     float64
		ClosePrice    float64
		Quantity      float64
		DistanceToMid float64
		CloseSide     string
	}
	var closes []closeCand

	sellWindowMaxPrice := currentPrice + float64(sellWindowSize)*profitSpread
	sellWindowMaxPrice = roundPrice(sellWindowMaxPrice, spm.priceDecimals)
	buyWindowMinPrice := currentPrice - float64(sellWindowSize)*profitSpread
	buyWindowMinPrice = roundPrice(buyWindowMinPrice, spm.priceDecimals)

	spm.slots.Range(func(key, value interface{}) bool {
		slotPrice := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.Lock()
		defer slot.mu.Unlock()

		if slot.PositionStatus != PositionStatusFilled || slot.PositionQty <= 0 ||
			slot.SlotStatus != SlotStatusFree || slot.OrderID != 0 || slot.ClientOID != "" {
			return true
		}
		if spm.isReduceOnlyCooldown(slotPrice) {
			return true
		}

		leg := slot.PositionLeg
		if leg == PositionLegNone {
			leg = PositionLegLong
		}

		var closePrice float64
		var closeSide string

		switch leg {
		case PositionLegShort:
			basePrice := slotPrice
			if slot.AvgBuyPrice > 0 && slot.AvgBuyPrice < slotPrice {
				basePrice = slot.AvgBuyPrice
			}
			closePrice = basePrice - spm.skewedCloseSpread(slotPrice, currentGridPrice, basePrice, shortPlan.CloseSpreadFactor) - shortPlan.CloseShift
			closePrice = roundPrice(closePrice, spm.priceDecimals)
			if closePrice < buyWindowMinPrice {
				return true
			}
			closeSide = "BUY"
		default:
			basePrice := slotPrice
			if slot.AvgBuyPrice > 0 && slot.AvgBuyPrice > slotPrice {
				basePrice = slot.AvgBuyPrice
			}
			closePrice = basePrice + spm.skewedCloseSpread(slotPrice, currentGridPrice, basePrice, longPlan.CloseSpreadFactor) + longPlan.CloseShift
			closePrice = roundPrice(closePrice, spm.priceDecimals)
			if slotPrice > sellWindowMaxPrice {
				return true
			}
			closeSide = "SELL"
		}
		// PostOnly 平倉價必須在盤口外側（價格已越過平倉價時掛到現價外一個 tick 起，連續被拒則逐步外移）
		closePrice = spm.makerSafeClosePrice(closePrice, currentPrice, closeSide, slot.PostOnlyFailCount)

		minVal := spm.config.Trading.MinOrderValue
		if minVal <= 0 {
			minVal = 6.0
		}
		if closePrice*slot.PositionQty >= minVal {
			closes = append(closes, closeCand{
				SlotPrice: slotPrice, ClosePrice: closePrice, Quantity: slot.PositionQty,
				DistanceToMid: math.Abs(slotPrice - currentPrice), CloseSide: closeSide,
			})
		}
		return true
	})

	sort.Slice(closes, func(i, j int) bool {
		return closes[i].DistanceToMid < closes[j].DistanceToMid
	})

	remainingForClose := threshold - currentOrderCount - longBuys - shortSells
	if remainingForClose < 0 {
		remainingForClose = 0
	}
	allowedClose := sellWindowSize
	if allowedClose > remainingForClose {
		allowedClose = remainingForClose
	}

	closeN := 0
	for i := 0; i < len(closes) && closeN < allowedClose; i++ {
		c := closes[i]
		slot := spm.getOrCreateSlot(c.SlotPrice)
		slot.mu.Lock()
		if slot.SlotStatus != SlotStatusFree || slot.PositionStatus != PositionStatusFilled || slot.PositionQty <= 0 {
			slot.mu.Unlock()
			continue
		}
		slot.SlotStatus = SlotStatusPending
		slot.mu.Unlock()

		coid := spm.generateClientOrderID(c.SlotPrice, c.CloseSide, "")
		ordersToPlace = append(ordersToPlace, &OrderRequest{
			Symbol: spm.config.Trading.Symbol, Side: c.CloseSide, Price: c.ClosePrice, Quantity: c.Quantity,
			PriceDecimals: spm.priceDecimals, ReduceOnly: !spm.isSpot(), PostOnly: gridOrdersPostOnly, ClientOrderID: coid,
		})
		closeN++
	}

	// 去重：任意 reduce 價位與同價開倉衝突時移除開倉
	closeAt := make(map[float64]bool)
	for _, o := range ordersToPlace {
		if o.ReduceOnly {
			closeAt[o.Price] = true
		}
	}
	if len(closeAt) > 0 {
		var filt []*OrderRequest
		for _, o := range ordersToPlace {
			if !o.ReduceOnly && closeAt[o.Price] {
				if price, _, valid := spm.parseClientOrderID(o.ClientOrderID); valid {
					sl := spm.getOrCreateSlot(price)
					sl.mu.Lock()
					if sl.SlotStatus == SlotStatusPending {
						sl.SlotStatus = SlotStatusFree
					}
					sl.mu.Unlock()
				}
				logger.Warn("⚠️ [%s] BOTH 同價 %s 平倉優先，移除開倉單", spm.logPrefix(), formatPrice(o.Price, spm.priceDecimals))
				continue
			}
			filt = append(filt, o)
		}
		ordersToPlace = filt
	}

	ordersToPlace = spm.clipSpotBuyOrdersByQuoteBudget(ordersToPlace, "BUY")

	spm.placeAdjustOrderBatch(ordersToPlace, currentPrice)
	return nil
}

func filterPricesInRange(prices []float64, low, high float64) []float64 {
	out := make([]float64, 0, len(prices))
	for _, p := range prices {
		if low > 0 && p < low {
			continue
		}
		if high > 0 && p > high {
			continue
		}
		out = append(out, p)
	}
	return out
}

// placeAdjustOrderBatch 與單向網格共用的下單尾部（資金、批量下單、回寫槽位）
func (spm *SuperPositionManager) placeAdjustOrderBatch(ordersToPlace []*OrderRequest, currentPrice float64) {
	if len(ordersToPlace) == 0 {
		return
	}

	positionLayers := 0
	unrealizedPnL := 0.0
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			positionLayers++
			if currentPrice > 0 {
				leg := slot.PositionLeg
				if leg == PositionLegNone {
					leg = PositionLegLong
				}
				switch leg {
				case PositionLegShort:
					if slot.AvgBuyPrice > 0 {
						unrealizedPnL += (slot.AvgBuyPrice - currentPrice) * slot.PositionQty
					}
				default:
					if slot.Price > 0 {
						unrealizedPnL += (currentPrice - slot.Price) * slot.PositionQty
					}
				}
			}
		}
		slot.mu.RUnlock()
		return true
	})

	spm.allocationManager.CheckAndAdjustLimit(
		spm.exchangeName, spm.config.Trading.Symbol, currentPrice, spm.anchorPrice(),
		positionLayers, unrealizedPnL,
	)

	ctx := context.Background()
	var accountBalance float64
	var accountResult interface{}
	if spm.exchange != nil {
		var err error
		// 帳戶信息緩存 accountCacheTTL，避免每輪調整都打 GetAccount REST
		accountResult, err = spm.getAccountCached(ctx)
		if err == nil && accountResult != nil {
			accountBalance = accountAvailableBalance(accountResult)
		}
	}

	leverage := spm.resolveLeverage(ctx, accountResult)

	// D3：只有開倉腿預留资金（按 ClientOrderID 記賬），reduce-only 平倉單不受资金分配限制
	var validOrders []*OrderRequest
	for _, req := range ordersToPlace {
		if _, err := spm.reserveOrderAllocation(req, leverage, accountBalance); err != nil {
			logger.Warn("⚠️ [%s] [BOTH 资金分配] %v (ClientOID: %s)", spm.logPrefix(), err, req.ClientOrderID)
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

	if len(ordersToPlace) == 0 {
		return
	}

	logger.Debug("🔄 [%s] [BOTH 實時調整] 需要新增: %d 個订單", spm.logPrefix(), len(ordersToPlace))
	result := spm.executor.BatchPlaceOrdersWithDetails(ordersToPlace)

	if result.HasMarginError {
		errLabel := "保证金不足"
		if spm.isSpot() {
			errLabel = "餘額不足"
		}
		logger.Warn("⚠️ [%s] 检测到錯误，暂停下單 %d 秒", errLabel, int(spm.marginLockDuration.Seconds()))
		spm.insufficientMargin = true
		spm.marginLockTime = time.Now()
		spm.invalidateAccountCache()
		spm.CancelAllOpenOrders()
		if spm.eventBus != nil {
			spm.eventBus.Publish(&event.Event{
				Type: event.EventTypeMarginInsufficient,
				Data: map[string]interface{}{
					"exchange": spm.exchangeName, "symbol": spm.config.Trading.Symbol,
					"failed_orders": len(result.PlacedOrders), "error_message": errLabel + "，已暂停下單",
					"lock_duration": int(spm.marginLockDuration.Seconds()),
				},
			})
		}
	}

	placedClientOIDs := make(map[string]bool)
	for _, ord := range result.PlacedOrders {
		placedClientOIDs[ord.ClientOrderID] = true
	}

	for clientOID := range result.ReduceOnlyErrors {
		price, side, valid := spm.parseClientOrderID(clientOID)
		if !valid {
			var fallbackPrice float64
			var fbSide string
			for _, req := range ordersToPlace {
				if req.ClientOrderID == clientOID {
					ps := spm.getEffectiveProfitSpread()
					if req.Side == "SELL" {
						fallbackPrice = req.Price - ps
						fbSide = "SELL"
					} else {
						fallbackPrice = req.Price + ps
						fbSide = "BUY"
					}
					break
				}
			}
			if fallbackPrice > 0 {
				price = fallbackPrice
				side = fbSide
				valid = true
			} else {
				continue
			}
		}
		if valid {
			// E6：不清空本地持倉，只冷卻並交給對賬決定
			spm.handleReduceOnlyRejection(price, side, clientOID)
		}
	}

	for _, req := range ordersToPlace {
		if !placedClientOIDs[req.ClientOrderID] && !result.ReduceOnlyErrors[req.ClientOrderID] {
			if price, _, valid := spm.parseClientOrderID(req.ClientOrderID); valid {
				slot := spm.getOrCreateSlot(price)
				slot.mu.Lock()
				if slot.SlotStatus == SlotStatusPending {
					slot.SlotStatus = SlotStatusFree
				}
				slot.mu.Unlock()
			}
			// 按 ClientOrderID 歸還預留（開多 BUY / 開空 SELL 一致處理）
			if released := spm.releaseOrderReservation(req.ClientOrderID); released > 0 {
				logger.Debug("💰 [BOTH 资金释放] 订單提交失败，释放預留资金: %.2f USDT (ClientOID: %s)", released, req.ClientOrderID)
			}
		}
	}

	for _, ord := range result.PlacedOrders {
		price, side, valid := spm.parseClientOrderID(ord.ClientOrderID)
		if !valid {
			continue
		}
		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()
		slot.OrderID = ord.OrderID
		slot.ClientOID = ord.ClientOrderID
		slot.OrderSide = side
		slot.OrderStatus = OrderStatusPlaced
		slot.OrderPrice = ord.Price
		slot.OrderCreatedAt = time.Now()
		slot.SlotStatus = SlotStatusLocked
		slot.StrategyName = spm.strategyName
		slot.StrategyType = spm.strategyType
		slot.mu.Unlock()
	}
}
