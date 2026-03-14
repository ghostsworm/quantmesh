package web

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

// PositionSummary 持倉彙總信息
type PositionSummary struct {
	TotalQuantity float64        `json:"total_quantity"` // 總持倉數量
	TotalValue    float64        `json:"total_value"`    // 總持倉價值（當前價格 * 數量）
	PositionCount int            `json:"position_count"` // 持倉槽位數
	AveragePrice  float64        `json:"average_price"`  // 平均持倉價格
	CurrentPrice  float64        `json:"current_price"`  // 當前市場價格
	UnrealizedPnL float64        `json:"unrealized_pnl"` // 未實現盈亏
	PnlPercentage float64        `json:"pnl_percentage"` // 盈亏百分比
	ActualMargin  float64        `json:"actual_margin"`  // 實際资金占用（實際保证金）
	Leverage      int            `json:"leverage"`       // 杠杆倍數
	Positions     []PositionInfo `json:"positions"`      // 持倉列表
}

// PositionInfo 單個持倉資訊
type PositionInfo struct {
	Price         float64 `json:"price"`          // 持倉價格
	Quantity      float64 `json:"quantity"`       // 持倉數量
	Value         float64 `json:"value"`          // 持倉價值
	UnrealizedPnL float64 `json:"unrealized_pnl"` // 未實現盈亏
}

// getPositions 獲取持倉列表（從槽位數據筛选）
func getPositions(c *gin.Context) {
	// 調試：記錄接收到的参數
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	resolvedKey := resolveSymbolKey(c)
	logger.Info("[DEBUG] getPositions called - exchange=%s, symbol=%s, resolvedKey=%s", exchange, symbol, resolvedKey)

	pmProvider := PickPositionProvider(c)
	priceProv := PickPriceProvider(c)

	if pmProvider == nil {
		// 與有 provider 時保持同一響應結構，避免前端報 "Invalid response format"
		c.JSON(http.StatusOK, gin.H{
			"summary": PositionSummary{
				TotalQuantity: 0,
				TotalValue:    0,
				PositionCount: 0,
				AveragePrice:  0,
				CurrentPrice:  0,
				UnrealizedPnL: 0,
				PnlPercentage: 0,
				ActualMargin:  0,
				Leverage:      1,
				Positions:     []PositionInfo{},
			},
		})
		return
	}

	slots := pmProvider.GetAllSlots()
	logger.Info("[DEBUG] getPositions - got %d slots for key=%s", len(slots), resolvedKey)
	var positions []PositionInfo
	currentPrice := 0.0
	if priceProv != nil {
		currentPrice = priceProv.GetLastPrice()
		logger.Info("[DEBUG] getPositions - [%s:%s] resolvedKey=%s, priceProvider!=nil, currentPrice=%.2f",
			exchange, symbol, resolvedKey, currentPrice)
	} else {
		logger.Warn("⚠️ [getPositions] [%s:%s] resolvedKey=%s, priceProvider is nil!",
			exchange, symbol, resolvedKey)
	}

	totalQuantity := 0.0
	totalValue := 0.0
	positionCount := 0

	// 筛选有持倉的槽位
	for _, slot := range slots {
		// 🔥 新增價格驗证：确保槽位價格有效（大於0且合理）
		if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
			// 🔥 價格合理性检查：如果當前價格可用，检查槽位價格是否在合理範圍内
			if currentPrice > 0 {
				priceRatio := slot.Price / currentPrice
				// 如果槽位價格是當前價格的100倍以上或0.01倍以下，可能是單位錯误
				if priceRatio > 100 || priceRatio < 0.01 {
					logger.Warn("⚠️ [getPositions] [%s:%s] 检测到异常槽位價格: slotPrice=%.2f, currentPrice=%.2f, 比例=%.2f, 數量=%.4f, resolvedKey=%s",
						exchange, symbol, slot.Price, currentPrice, priceRatio, slot.PositionQty, resolvedKey)
					// 继续处理，但記錄警告
				}
			}

			positionCount++
			totalQuantity += slot.PositionQty

			// 计算持倉價值（使用當前價格）
			value := slot.PositionQty * currentPrice
			if currentPrice == 0 {
				// 如果當前價格不可用，使用持倉價格
				value = slot.PositionQty * slot.Price
			}
			totalValue += value

			// 计算未實現盈亏
			unrealizedPnL := 0.0
			if currentPrice > 0 && slot.Price > 0 {
				// 🔥 新增價格合理性检查：如果當前價格相對於持倉價格偏差過大，可能是價格异常
				priceDeviation := (currentPrice - slot.Price) / slot.Price

				// 检查是否是單位问题（比如當前價格是持倉價格的100倍或0.01倍）
				priceRatio := currentPrice / slot.Price
				adjustedCurrentPrice := currentPrice
				if priceRatio > 50 {
					// 當前價格可能是持倉價格的100倍，尝試除以100
					adjustedPrice := currentPrice / 100
					if math.Abs(adjustedPrice-slot.Price)/slot.Price < 0.1 {
						logger.Warn("⚠️ [getPositions] [%s:%s] 检测到價格單位问题（當前價格可能是持倉價格的100倍），已自动修正: %.2f -> %.2f",
							exchange, symbol, currentPrice, adjustedPrice)
						adjustedCurrentPrice = adjustedPrice
					}
				} else if priceRatio < 0.02 {
					// 當前價格可能是持倉價格的0.01倍，尝試乘以100
					adjustedPrice := currentPrice * 100
					if math.Abs(adjustedPrice-slot.Price)/slot.Price < 0.1 {
						logger.Warn("⚠️ [getPositions] [%s:%s] 检测到價格單位问题（當前價格可能是持倉價格的0.01倍），已自动修正: %.2f -> %.2f",
							exchange, symbol, currentPrice, adjustedPrice)
						adjustedCurrentPrice = adjustedPrice
					}
				}

				// 重新计算價格偏差
				priceDeviation = (adjustedCurrentPrice - slot.Price) / slot.Price
				if priceDeviation > 0.5 || priceDeviation < -0.5 {
					// 價格偏差仍然過大，使用持倉價格（未實現盈亏為0）
					logger.Warn("⚠️ [getPositions] [%s:%s] 價格偏差過大，使用持倉價格计算（未實現盈亏設為0）: currentPrice=%.2f, slotPrice=%.2f, 偏差=%.2f%%, resolvedKey=%s",
						exchange, symbol, adjustedCurrentPrice, slot.Price, priceDeviation*100, resolvedKey)
					adjustedCurrentPrice = slot.Price
				}

				unrealizedPnL = (adjustedCurrentPrice - slot.Price) * slot.PositionQty
			}

			positions = append(positions, PositionInfo{
				Price:         slot.Price,
				Quantity:      slot.PositionQty,
				Value:         value,
				UnrealizedPnL: unrealizedPnL,
			})
		}
	}

	// 计算平均持倉價格
	averagePrice := 0.0
	if totalQuantity > 0 {
		totalCost := 0.0
		for _, pos := range positions {
			totalCost += pos.Price * pos.Quantity
		}
		averagePrice = totalCost / totalQuantity
	}

	// 计算總未實現盈亏
	totalUnrealizedPnL := 0.0
	if currentPrice > 0 {
		for _, pos := range positions {
			totalUnrealizedPnL += pos.UnrealizedPnL
		}
	}

	// 计算總持倉成本
	totalCost := 0.0
	for _, pos := range positions {
		totalCost += pos.Price * pos.Quantity
	}

	// 计算亏损率（相對於持倉成本的百分比）
	pnlPercentage := 0.0
	if totalCost > 0 {
		pnlPercentage = (totalUnrealizedPnL / totalCost) * 100.0
	}

	// 计算實際资金占用（實際保证金 = 總持倉價值 / 杠杆倍數）
	leverage := 1 // 默认1倍（無杠杆）
	if pmProvider != nil {
		leverage = pmProvider.GetLeverage()
	}
	actualMargin := 0.0
	if leverage > 0 && totalValue > 0 {
		actualMargin = totalValue / float64(leverage)
	}

	summary := PositionSummary{
		TotalQuantity: totalQuantity,
		TotalValue:    totalValue,
		PositionCount: positionCount,
		AveragePrice:  averagePrice,
		CurrentPrice:  currentPrice,
		UnrealizedPnL: totalUnrealizedPnL,
		PnlPercentage: pnlPercentage,
		ActualMargin:  actualMargin,
		Leverage:      leverage,
		Positions:     positions,
	}

	// 調試：在响应中包含请求的交易對信息
	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
		"_debug": gin.H{
			"exchange":    exchange,
			"symbol":      symbol,
			"resolvedKey": resolvedKey,
			"slotCount":   len(slots),
		},
	})
}

// getPositionsSummary 獲取持倉彙總
// GET /api/positions/summary
func getPositionsSummary(c *gin.Context) {
	pmProvider := PickPositionProvider(c)
	priceProv := PickPriceProvider(c)
	exchProv := pickExchangeProvider(c)

	// 獲取请求参數
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		if k := resolveSymbolKey(c); k != "" {
			parts := strings.SplitN(k, ":", 2)
			if len(parts) == 2 {
				exchange, symbol = parts[0], parts[1]
			}
		}
	}

	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"total_quantity": 0,
			"total_value":    0,
			"position_count": 0,
			"average_price":  0,
			"current_price":  0,
			"unrealized_pnl": 0,
			"pnl_percentage": 0,
			"actual_margin":  0,
			"leverage":       1,
		})
		return
	}

	slots := pmProvider.GetAllSlots()
	wsPrice := 0.0 // WebSocket 實時價格
	if priceProv != nil {
		wsPrice = priceProv.GetLastPrice()
	}

	// ========== 槽位计算部分 ==========
	slotTotalQuantity := 0.0
	slotTotalValue := 0.0
	slotPositionCount := 0
	slotTotalCost := 0.0

	// 筛选有持倉的槽位
	for _, slot := range slots {
		if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
			slotPositionCount++
			slotTotalQuantity += slot.PositionQty
			slotTotalCost += slot.Price * slot.PositionQty

			if wsPrice > 0 {
				slotTotalValue += slot.PositionQty * wsPrice
			} else {
				slotTotalValue += slot.PositionQty * slot.Price
			}
		}
	}

	// 槽位平均持倉價格
	slotAveragePrice := 0.0
	if slotTotalQuantity > 0 {
		slotAveragePrice = slotTotalCost / slotTotalQuantity
	}

	// 槽位计算的未實現盈亏
	slotUnrealizedPnL := 0.0
	if wsPrice > 0 && slotTotalQuantity > 0 && slotAveragePrice > 0 {
		slotUnrealizedPnL = (wsPrice - slotAveragePrice) * slotTotalQuantity
	}

	// ========== 交易所數據部分 ==========
	exchangeUnrealizedPnL := 0.0
	exchangeMarkPrice := 0.0
	exchangeEntryPrice := 0.0
	exchangePositionSize := 0.0
	exchangeLeverage := 0
	hasExchangeData := false

	if exchProv != nil && symbol != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		positions, err := exchProv.GetPositions(ctx, symbol)
		cancel()

		if err == nil && len(positions) > 0 {
			for _, pos := range positions {
				if pos.Size != 0 {
					exchangeUnrealizedPnL += pos.UnrealizedPNL
					exchangeMarkPrice = pos.MarkPrice
					exchangeEntryPrice = pos.EntryPrice
					exchangePositionSize += pos.Size // 累加（支援多倉位）
					exchangeLeverage = pos.Leverage
					hasExchangeData = true
				}
			}
		} else if err != nil {
			logger.Warn("⚠️ [getPositionsSummary] 從交易所獲取倉位失败: %v", err)
		}
	}

	// ========== 差异分析 ==========
	// 返回結構化原因供前端 i18n 格式化，不再硬編碼中文
	type reasonItem map[string]interface{}
	var discrepancyReasons []reasonItem
	pnlDiff := 0.0

	if hasExchangeData && slotTotalQuantity > 0 {
		pnlDiff = exchangeUnrealizedPnL - slotUnrealizedPnL

		// 1. 數量差异分析
		quantityDiff := exchangePositionSize - slotTotalQuantity
		if math.Abs(quantityDiff) > 0.000001 {
			diffPercent := (quantityDiff / slotTotalQuantity) * 100
			if math.Abs(diffPercent) > 1 {
				discrepancyReasons = append(discrepancyReasons, reasonItem{
					"type":     "quantity_diff",
					"exchange": exchangePositionSize,
					"slot":     slotTotalQuantity,
					"diff":     quantityDiff,
					"diff_pct": diffPercent,
				})
			}
		}

		// 2. 入场價格差异分析
		priceDiff := exchangeEntryPrice - slotAveragePrice
		if math.Abs(priceDiff) > 0.01 {
			diffPercent := (priceDiff / slotAveragePrice) * 100
			if math.Abs(diffPercent) > 0.1 {
				discrepancyReasons = append(discrepancyReasons, reasonItem{
					"type":     "entry_price_diff",
					"exchange": exchangeEntryPrice,
					"slot_avg": slotAveragePrice,
					"diff":     priceDiff,
					"diff_pct": diffPercent,
				})
			}
		}

		// 3. 當前價格差异分析（標記價格 vs WebSocket價格）
		if wsPrice > 0 && exchangeMarkPrice > 0 {
			markPriceDiff := exchangeMarkPrice - wsPrice
			if math.Abs(markPriceDiff) > 0.01 {
				diffPercent := (markPriceDiff / wsPrice) * 100
				discrepancyReasons = append(discrepancyReasons, reasonItem{
					"type":       "price_diff",
					"mark_price": exchangeMarkPrice,
					"ws_price":   wsPrice,
					"diff":       markPriceDiff,
					"diff_pct":   diffPercent,
				})
			}
		}

		// 4. 如果數量和價格都接近但盈亏差异大，可能是其他原因
		if len(discrepancyReasons) == 0 && math.Abs(pnlDiff) > 1 {
			discrepancyReasons = append(discrepancyReasons, reasonItem{
				"type":     "pnl_diff_other",
				"pnl_diff": pnlDiff,
			})
		}

		// 記錄详细日志
		logger.Info("📊 [getPositionsSummary] 盈亏對比分析:")
		logger.Info("  交易所: size=%.6f, entryPrice=%.2f, markPrice=%.2f, pnl=%.4f, leverage=%d",
			exchangePositionSize, exchangeEntryPrice, exchangeMarkPrice, exchangeUnrealizedPnL, exchangeLeverage)
		logger.Info("  槽位:   size=%.6f, avgPrice=%.2f, wsPrice=%.2f, pnl=%.4f",
			slotTotalQuantity, slotAveragePrice, wsPrice, slotUnrealizedPnL)
		logger.Info("  差异:   pnlDiff=%.4f USDT", pnlDiff)
		for i, r := range discrepancyReasons {
			logger.Info("  原因[%d]: type=%v", i, r["type"])
		}
	}

	// ========== 决定使用哪個數據作為主要显示 ==========
	// 优先使用交易所數據（因為这是真實的盈亏）
	displayUnrealizedPnL := slotUnrealizedPnL
	displayCurrentPrice := wsPrice
	if hasExchangeData {
		displayUnrealizedPnL = exchangeUnrealizedPnL
		if exchangeMarkPrice > 0 {
			displayCurrentPrice = exchangeMarkPrice
		}
	}

	// 计算亏损率（相對於持倉成本的百分比）
	pnlPercentage := 0.0
	if slotTotalCost > 0 {
		pnlPercentage = (displayUnrealizedPnL / slotTotalCost) * 100.0
	}

	// 计算實際资金占用
	leverage := 1
	if pmProvider != nil {
		leverage = pmProvider.GetLeverage()
	}
	// 如果交易所返回了杠杆，优先使用
	if exchangeLeverage > 0 {
		leverage = exchangeLeverage
	}
	actualMargin := 0.0
	if leverage > 0 && slotTotalValue > 0 {
		actualMargin = slotTotalValue / float64(leverage)
	}

	// 構建响应
	response := gin.H{
		// 維度標識（按交易所、币种、策略）
		"exchange": exchange,
		"symbol":   symbol,
		"strategy": "grid",
		// 主要显示數據（优先使用交易所數據）
		"total_quantity": slotTotalQuantity,
		"total_value":    slotTotalValue,
		"position_count": slotPositionCount,
		"average_price":  slotAveragePrice,
		"current_price":  displayCurrentPrice,
		"unrealized_pnl": displayUnrealizedPnL,
		"pnl_percentage": pnlPercentage,
		"actual_margin":  actualMargin,
		"leverage":       leverage,

		// 槽位计算數據
		"slot_data": gin.H{
			"quantity":       slotTotalQuantity,
			"average_price":  slotAveragePrice,
			"unrealized_pnl": slotUnrealizedPnL,
			"ws_price":       wsPrice,
		},

		// 交易所數據
		"exchange_data": gin.H{
			"has_data":       hasExchangeData,
			"quantity":       exchangePositionSize,
			"entry_price":    exchangeEntryPrice,
			"mark_price":     exchangeMarkPrice,
			"unrealized_pnl": exchangeUnrealizedPnL,
			"leverage":       exchangeLeverage,
		},

		// 差异分析
		"discrepancy": gin.H{
			"pnl_diff": pnlDiff,
			"reasons":  discrepancyReasons,
		},
	}

	c.JSON(http.StatusOK, response)
}

// getExchangePositionsSummary 獲取交易所持倉彙總（不依賴運行中的 Bot，用於已停止 Bot 的概覽）
// GET /api/positions/exchange-summary?exchange=xxx&symbol=xxx&market_type=xxx
func getExchangePositionsSummary(c *gin.Context) {
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")
	marketType := c.DefaultQuery("market_type", "futures")

	if exchangeName == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 exchange 或 symbol 參數"})
		return
	}

	ex, err := getExchangeForCancel(exchangeName, symbol, marketType)
	if err != nil {
		logger.Warn("❌ [exchange-positions] 獲取交易所失敗: exchange=%s, symbol=%s, error=%v", exchangeName, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取交易所失敗: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		logger.Warn("⚠️ [exchange-positions] 從交易所獲取倉位失敗: exchange=%s, symbol=%s, error=%v", exchangeName, symbol, err)
		c.JSON(http.StatusOK, gin.H{
			"has_data":       false,
			"quantity":       0,
			"entry_price":    0,
			"mark_price":     0,
			"unrealized_pnl": 0,
			"leverage":       0,
			"current_price":  0,
		})
		return
	}

	var totalSize, unrealizedPnL float64
	var markPrice, entryPrice float64
	leverage := 0

	for _, pos := range positions {
		if pos.Size != 0 {
			totalSize += pos.Size
			unrealizedPnL += pos.UnrealizedPNL
			markPrice = pos.MarkPrice
			entryPrice = pos.EntryPrice
			if pos.Leverage > 0 {
				leverage = pos.Leverage
			}
		}
	}

	currentPrice := markPrice
	if currentPrice == 0 {
		currentPrice = entryPrice
	}

	c.JSON(http.StatusOK, gin.H{
		"has_data":       totalSize != 0,
		"quantity":       totalSize,
		"entry_price":    entryPrice,
		"mark_price":     markPrice,
		"unrealized_pnl": unrealizedPnL,
		"leverage":       leverage,
		"current_price":  currentPrice,
		"total_value":    totalSize * currentPrice,
	})
}

// getPositionsSummaryAll 獲取所有交易對的持倉彙總（按交易所、币种、策略列出）
// GET /api/positions/summary/all
func getPositionsSummaryAll(c *gin.Context) {
	// 建立 exchange:symbol:marketType -> bot_id 的映射
	botIDByKey := make(map[string]string)
	if botManagerProvider != nil {
		for _, b := range botManagerProvider.ListBots() {
			mt := b.MarketType
			if mt == "" {
				mt = "futures"
			}
			k := makeSymbolKey(strings.ToLower(b.Exchange), b.Symbol, mt)
			botIDByKey[k] = b.BotID
		}
	}

	// 從策略運行時獲取各 key 對應的策略名稱（用於替換硬編碼 "grid"）
	strategyByKey := make(map[string]string)
	if strategyRuntimeProvider != nil {
		allData, err := strategyRuntimeProvider.GetAllStrategyStatusAll()
		if err == nil {
			for _, item := range allData {
				k := makeSymbolKey(item.Exchange, item.Symbol, item.MarketType)
				strategyName := "grid"
				for _, s := range item.Strategies {
					if s.IsEnabled && (s.PositionCount > 0 || s.OrderCount > 0) {
						strategyName = s.Name
						break
					}
					if strategyName == "grid" && s.Name != "" {
						strategyName = s.Name
					}
				}
				strategyByKey[k] = strategyName
				// 兼容不含 marketType 的舊 key
				strategyByKey[makeSymbolKeyCompat(item.Exchange, item.Symbol)] = strategyName
			}
		}
	}

	providersMu.RLock()
	keys := make([]string, 0, len(positionProviders))
	for k := range positionProviders {
		keys = append(keys, k)
	}
	providersMu.RUnlock()

	var result []gin.H
	for _, key := range keys {
		parts := strings.Split(key, ":")
		var exchangeName, symbol, marketType string
		if len(parts) >= 3 {
			exchangeName, symbol, marketType = parts[0], parts[1], parts[2]
		} else if len(parts) == 2 {
			exchangeName, symbol, marketType = parts[0], parts[1], "futures"
		} else {
			continue
		}

		providersMu.RLock()
		pmProvider := positionProviders[key]
		priceProv := priceProviders[key]
		exchProv := exchangeProviders[key]
		providersMu.RUnlock()

		if pmProvider == nil {
			continue
		}

		slots := pmProvider.GetAllSlots()
		wsPrice := 0.0
		if priceProv != nil {
			wsPrice = priceProv.GetLastPrice()
		}

		// 槽位计算
		slotTotalQuantity := 0.0
		slotTotalValue := 0.0
		slotPositionCount := 0
		slotTotalCost := 0.0
		for _, slot := range slots {
			if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
				slotPositionCount++
				slotTotalQuantity += slot.PositionQty
				slotTotalCost += slot.Price * slot.PositionQty
				if wsPrice > 0 {
					slotTotalValue += slot.PositionQty * wsPrice
				} else {
					slotTotalValue += slot.PositionQty * slot.Price
				}
			}
		}
		// 仅返回有持倉的
		if slotPositionCount == 0 {
			continue
		}

		slotAveragePrice := 0.0
		if slotTotalQuantity > 0 {
			slotAveragePrice = slotTotalCost / slotTotalQuantity
		}
		slotUnrealizedPnL := 0.0
		if wsPrice > 0 && slotTotalQuantity > 0 && slotAveragePrice > 0 {
			slotUnrealizedPnL = (wsPrice - slotAveragePrice) * slotTotalQuantity
		}

		// 交易所數據
		exchangeUnrealizedPnL := 0.0
		exchangeMarkPrice := 0.0
		exchangeEntryPrice := 0.0
		exchangePositionSize := 0.0
		exchangeLeverage := 0
		hasExchangeData := false
		if exchProv != nil && symbol != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			positions, err := exchProv.GetPositions(ctx, symbol)
			cancel()
			if err == nil && len(positions) > 0 {
				for _, pos := range positions {
					if pos.Size != 0 {
						exchangeUnrealizedPnL += pos.UnrealizedPNL
						exchangeMarkPrice = pos.MarkPrice
						exchangeEntryPrice = pos.EntryPrice
						exchangePositionSize += pos.Size
						exchangeLeverage = pos.Leverage
						hasExchangeData = true
					}
				}
			}
		}

		displayUnrealizedPnL := slotUnrealizedPnL
		displayCurrentPrice := wsPrice
		if hasExchangeData {
			displayUnrealizedPnL = exchangeUnrealizedPnL
			if exchangeMarkPrice > 0 {
				displayCurrentPrice = exchangeMarkPrice
			}
		}

		leverage := pmProvider.GetLeverage()
		if exchangeLeverage > 0 {
			leverage = exchangeLeverage
		}
		actualMargin := 0.0
		if leverage > 0 && slotTotalValue > 0 {
			actualMargin = slotTotalValue / float64(leverage)
		}
		pnlPercentage := 0.0
		if slotTotalCost > 0 {
			pnlPercentage = (displayUnrealizedPnL / slotTotalCost) * 100.0
		}

		strategyName := "grid"
		if n, ok := strategyByKey[key]; ok && n != "" {
			strategyName = n
		} else if n, ok := strategyByKey[makeSymbolKeyCompat(exchangeName, symbol)]; ok && n != "" {
			strategyName = n
		}
		botKey := makeSymbolKey(strings.ToLower(exchangeName), symbol, marketType)
		botID := botIDByKey[botKey]

		result = append(result, gin.H{
			"bot_id":         botID,
			"exchange":       exchangeName,
			"symbol":         symbol,
			"market_type":    marketType,
			"strategy":       strategyName,
			"total_quantity": slotTotalQuantity,
			"total_value":    slotTotalValue,
			"position_count": slotPositionCount,
			"average_price":  slotAveragePrice,
			"current_price":  displayCurrentPrice,
			"unrealized_pnl": displayUnrealizedPnL,
			"pnl_percentage": pnlPercentage,
			"actual_margin":  actualMargin,
			"leverage":       leverage,
			"exchange_data": gin.H{
				"has_data":       hasExchangeData,
				"quantity":       exchangePositionSize,
				"entry_price":    exchangeEntryPrice,
				"mark_price":     exchangeMarkPrice,
				"unrealized_pnl": exchangeUnrealizedPnL,
				"leverage":       exchangeLeverage,
			},
		})
	}

	// 按交易所、币种排序
	sort.Slice(result, func(i, j int) bool {
		ei, _ := result[i]["exchange"].(string)
		ej, _ := result[j]["exchange"].(string)
		if ei != ej {
			return ei < ej
		}
		si, _ := result[i]["symbol"].(string)
		sj, _ := result[j]["symbol"].(string)
		return strings.ToLower(si) < strings.ToLower(sj)
	})

	c.JSON(http.StatusOK, gin.H{"positions": result})
}
