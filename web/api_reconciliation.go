package web

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/storage"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

// ========== 對账相关API ==========

// ReconciliationStatus 對账状態
type ReconciliationStatus struct {
	Exchange             string    `json:"exchange,omitempty"`
	Symbol               string    `json:"symbol,omitempty"`
	MarketType           string    `json:"market_type,omitempty"`
	DataScopeVerified    bool      `json:"data_scope_verified"`
	HistoryScopeVerified bool      `json:"history_scope_verified"`
	ReconcileCount       int64     `json:"reconcile_count"`      // 對账次數（運行時自增，重啟後歸零）
	HistoryRecordCount   int64     `json:"history_record_count"` // 對账歷史記錄數（數據庫，與下方列表一致）
	LastReconcileTime    time.Time `json:"last_reconcile_time"`  // 最后對账時间
	LocalPosition        float64   `json:"local_position"`       // 本地持倉
	TotalBuyQty          float64   `json:"total_buy_qty"`        // 累计買入
	TotalSellQty         float64   `json:"total_sell_qty"`       // 累计賣出
	EstimatedProfit      float64   `json:"estimated_profit"`     // 預计盈利
	ActualProfit         float64   `json:"actual_profit"`        // 實際盈利（来自 trades 表）
}

type marketScopedReconciliationStorage interface {
	GetActualProfitBySymbolMarketScope(exchange, marketType, symbol, account, accountScope string, beforeTime time.Time, botID string) (float64, error)
	GetTotalBuySellQtyByMarketScope(exchange, marketType, symbol, account, accountScope, botID string) (float64, float64, error)
	HasUnclassifiedMarketTrades(exchange, symbol, account, accountScope, botID string) (bool, error)
	QueryReconciliationHistoryByScope(exchange, symbol, account, marketType, accountScope, botID string, startTime, endTime time.Time, limit, offset int) ([]*storage.ReconciliationHistory, error)
	GetReconciliationCountByScope(exchange, symbol, account, marketType, accountScope, botID string) (int64, error)
	HasUnscopedReconciliationHistory(exchange, symbol, account string) (bool, error)
}

// ReconciliationHistoryInfo 對账历史信息
type ReconciliationHistoryInfo struct {
	ID               int64     `json:"id"`
	Exchange         string    `json:"exchange"`
	Symbol           string    `json:"symbol"`
	AccountScope     string    `json:"account_scope,omitempty"`
	MarketType       string    `json:"market_type,omitempty"`
	BotID            string    `json:"bot_id,omitempty"`
	ReconcileTime    time.Time `json:"reconcile_time"`
	LocalPosition    float64   `json:"local_position"`
	ExchangePosition float64   `json:"exchange_position"`
	PositionDiff     float64   `json:"position_diff"`
	ActiveBuyOrders  int       `json:"active_buy_orders"`
	ActiveSellOrders int       `json:"active_sell_orders"`
	PendingSellQty   float64   `json:"pending_sell_qty"`
	TotalBuyQty      float64   `json:"total_buy_qty"`
	TotalSellQty     float64   `json:"total_sell_qty"`
	EstimatedProfit  float64   `json:"estimated_profit"`
	ActualProfit     float64   `json:"actual_profit"`
	CreatedAt        time.Time `json:"created_at"`
}

// getReconciliationStatus 獲取對账状態
// GET /api/reconciliation/status
func getReconciliationStatus(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	symbol := c.Query("symbol")
	exchange := c.Query("exchange")
	marketType := strings.TrimSpace(c.Query("market_type"))
	statusProvider := pickStatus(c)
	accountScope := ""
	if statusProvider != nil {
		if symbol == "" {
			symbol = statusProvider.Symbol
		}
		if exchange == "" && strings.EqualFold(symbol, statusProvider.Symbol) {
			exchange = statusProvider.Exchange
		}
		if marketType == "" && strings.EqualFold(symbol, statusProvider.Symbol) && strings.EqualFold(exchange, statusProvider.Exchange) {
			marketType = strings.TrimSpace(statusProvider.MarketType)
		}
		if strings.EqualFold(symbol, statusProvider.Symbol) && strings.EqualFold(exchange, statusProvider.Exchange) && strings.EqualFold(marketType, statusProvider.MarketType) {
			accountScope = strings.TrimSpace(statusProvider.AccountScope)
		}
	}
	exchange = strings.TrimSpace(exchange)
	if symbol != "" {
		if exchange == "" || marketType == "" || accountScope == "" || strings.TrimSpace(c.Query("bot_id")) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "reconciliation requires exchange, symbol, market_type, verified account scope, and bot_id"})
			return
		}
		if storageProv == nil || storageProv.GetStorage() == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "market-scoped reconciliation storage is unavailable"})
			return
		}
		scopedStore, ok := storageProv.GetStorage().(marketScopedReconciliationStorage)
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage does not support market-scoped reconciliation"})
			return
		}
		legacy, err := scopedStore.HasUnclassifiedMarketTrades(exchange, symbol, GetCurrentAccountID(), accountScope, strings.TrimSpace(c.Query("bot_id")))
		if err != nil {
			logger.Error("check legacy reconciliation market scope failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify reconciliation market attribution"})
			return
		}
		if legacy {
			c.JSON(http.StatusConflict, gin.H{"error": "legacy trade rows lack market attribution and must be reconciled before scoped totals can be shown", "exchange": exchange, "symbol": symbol, "market_type": marketType, "data_scope_verified": false})
			return
		}
	}

	historyRecordCount := int64(0)
	historyScopeVerified := false
	if symbol != "" {
		scopedStore := storageProv.GetStorage().(marketScopedReconciliationStorage)
		unscoped, err := scopedStore.HasUnscopedReconciliationHistory(exchange, symbol, GetCurrentAccountID())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify reconciliation history scope"})
			return
		}
		historyScopeVerified = !unscoped
		count, err := scopedStore.GetReconciliationCountByScope(exchange, symbol, GetCurrentAccountID(), marketType, accountScope, strings.TrimSpace(c.Query("bot_id")))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query scoped reconciliation history count"})
			return
		}
		historyRecordCount = count
	}

	pmProvider := PickPositionProvider(c)
	if pmProvider == nil {
		if symbol != "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "position reconciliation provider is unavailable", "exchange": exchange, "symbol": symbol, "market_type": marketType, "data_scope_verified": false})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"reconcile_count":      0,
			"history_record_count": historyRecordCount,
			"last_reconcile_time":  time.Time{},
			"local_position":       0,
			"total_buy_qty":        0,
			"total_sell_qty":       0,
			"estimated_profit":     0,
			"actual_profit":        0,
		})
		return
	}

	// 從 PositionManager 獲取對账统计
	reconcileCount := pmProvider.GetReconcileCount()
	lastReconcileTime := pmProvider.GetLastReconcileTime()
	profitSpread := pmProvider.GetProfitSpread()

	// 單 Bot 對賬頁傳 bot_id 時，僅統計該 Bot 的配對成交，避免同帳戶同交易對多 Bot 累加導致「預計盈利」暴漲
	reconcileBotID := strings.TrimSpace(c.Query("bot_id"))

	// 优先從數據库實時计算累计買入和累计賣出（更准确，不受重啟影响）
	totalBuyQty := 0.0
	totalSellQty := 0.0

	if symbol != "" && storageProv != nil && storageProv.GetStorage() != nil {
		accountID := GetCurrentAccountID()
		scopedStore := storageProv.GetStorage().(marketScopedReconciliationStorage)
		buyQty, sellQty, err := scopedStore.GetTotalBuySellQtyByMarketScope(exchange, marketType, symbol, accountID, accountScope, reconcileBotID)
		if err != nil {
			logger.Error("query scoped reconciliation quantities failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query market-scoped reconciliation quantities"})
			return
		}
		totalBuyQty, totalSellQty = buyQty, sellQty
	}

	// 如果數據库中没有數據，尝試從記憶體獲取（作為后备）
	if totalBuyQty == 0 && totalSellQty == 0 {
		memBuyQty := pmProvider.GetTotalBuyQty()
		memSellQty := pmProvider.GetTotalSellQty()
		if memBuyQty > 0 || memSellQty > 0 {
			totalBuyQty = memBuyQty
			totalSellQty = memSellQty
			logger.Info("📊 [對账状態] 從記憶體獲取: symbol=%s, 累计買入=%.4f, 累计賣出=%.4f", symbol, memBuyQty, memSellQty)
		}
	}

	estimatedProfit := totalSellQty * profitSpread

	// 计算本地持倉
	slots := pmProvider.GetAllSlots()
	localPosition := 0.0
	for _, slot := range slots {
		if symbol != "" && (!strings.EqualFold(slot.Symbol, symbol) || !strings.EqualFold(slot.Exchange, exchange)) {
			continue
		}
		if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 {
			localPosition += slot.PositionQty
		}
	}

	// 獲取實際盈利
	actualProfit := 0.0
	if symbol != "" && storageProv != nil && storageProv.GetStorage() != nil {
		accountID := GetCurrentAccountID()
		scopedStore := storageProv.GetStorage().(marketScopedReconciliationStorage)
		profit, err := scopedStore.GetActualProfitBySymbolMarketScope(exchange, marketType, symbol, accountID, accountScope, time.Now().UTC(), reconcileBotID)
		if err != nil {
			logger.Error("query scoped reconciliation PnL failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query market-scoped reconciliation PnL"})
			return
		}
		actualProfit = profit
	}

	status := ReconciliationStatus{
		Exchange:             exchange,
		Symbol:               symbol,
		MarketType:           marketType,
		DataScopeVerified:    symbol != "",
		HistoryScopeVerified: historyScopeVerified,
		ReconcileCount:       reconcileCount,
		HistoryRecordCount:   historyRecordCount,
		LastReconcileTime:    utils.ToUTC8(lastReconcileTime),
		LocalPosition:        localPosition,
		TotalBuyQty:          totalBuyQty,
		TotalSellQty:         totalSellQty,
		EstimatedProfit:      estimatedProfit,
		ActualProfit:         actualProfit,
	}

	c.JSON(http.StatusOK, status)
}

// getReconciliationHistory 獲取對账历史
// GET /api/reconciliation/history
func getReconciliationHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	// 解析参數
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 默认最近30天，确保能查詢到更多历史記錄
		startTime = time.Now().AddDate(0, 0, -30)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	offset := 0
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()
	marketType := strings.TrimSpace(c.Query("market_type"))
	botID := strings.TrimSpace(c.Query("bot_id"))
	statusProvider := pickStatus(c)
	accountScope := ""
	if statusProvider != nil && strings.EqualFold(symbol, statusProvider.Symbol) && strings.EqualFold(exchangeName, statusProvider.Exchange) && strings.EqualFold(marketType, statusProvider.MarketType) {
		accountScope = strings.TrimSpace(statusProvider.AccountScope)
	}
	if exchangeName == "" || symbol == "" || marketType == "" || accountScope == "" || botID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scoped reconciliation history requires exchange, symbol, market_type, verified account scope, and bot_id"})
		return
	}
	scopedStorage, ok := storage.(marketScopedReconciliationStorage)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage does not support scoped reconciliation history"})
		return
	}

	// 查詢對账历史
	histories, err := scopedStorage.QueryReconciliationHistoryByScope(exchangeName, symbol, accountID, marketType, accountScope, botID, startTime, endTime, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 轉换為 API 响应格式
	result := make([]ReconciliationHistoryInfo, len(histories))
	for i, h := range histories {
		result[i] = ReconciliationHistoryInfo{
			ID:               h.ID,
			Exchange:         h.Exchange,
			Symbol:           h.Symbol,
			AccountScope:     h.AccountScope,
			MarketType:       h.MarketType,
			BotID:            h.BotID,
			ReconcileTime:    utils.ToUTC8(h.ReconcileTime),
			LocalPosition:    h.LocalPosition,
			ExchangePosition: h.ExchangePosition,
			PositionDiff:     h.PositionDiff,
			ActiveBuyOrders:  h.ActiveBuyOrders,
			ActiveSellOrders: h.ActiveSellOrders,
			PendingSellQty:   h.PendingSellQty,
			TotalBuyQty:      h.TotalBuyQty,
			TotalSellQty:     h.TotalSellQty,
			EstimatedProfit:  h.EstimatedProfit,
			ActualProfit:     h.ActualProfit,
			CreatedAt:        utils.ToUTC8(h.CreatedAt),
		}
	}

	unscoped, err := scopedStorage.HasUnscopedReconciliationHistory(exchangeName, symbol, accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify legacy reconciliation history"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"history": result, "scope_verified": !unscoped, "unscoped_legacy_records": unscoped})
}

// ReconciliationAggregatedData 聚合的對账數據
type ReconciliationAggregatedData struct {
	Date                string  `json:"date"`                  // 日期（格式根據聚合類型：2026-01-25、2026-W04、2026-01）
	AvgLocalPosition    float64 `json:"avg_local_position"`    // 平均本地持倉
	AvgExchangePosition float64 `json:"avg_exchange_position"` // 平均交易所持倉
	AvgPositionDiff     float64 `json:"avg_position_diff"`     // 平均持倉差异
	TotalBuyQty         float64 `json:"total_buy_qty"`         // 累计買入
	TotalSellQty        float64 `json:"total_sell_qty"`        // 累计賣出
	EstimatedProfit     float64 `json:"estimated_profit"`      // 預计盈利
	ActualProfit        float64 `json:"actual_profit"`         // 實際盈利
	RecordCount         int     `json:"record_count"`          // 記錄數量
}

// getReconciliationAggregated 獲取聚合的對账數據
// GET /api/reconciliation/aggregated
// 参數: period=day|week|month, exchange, symbol, start_time, end_time
func getReconciliationAggregated(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"data": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"data": []interface{}{}})
		return
	}

	// 解析参數
	period := c.DefaultQuery("period", "day") // day, week, month
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 根據聚合周期設置默认時间範圍
		switch period {
		case "month":
			startTime = time.Now().AddDate(0, -12, 0) // 最近12個月
		case "week":
			startTime = time.Now().AddDate(0, 0, -90) // 最近90天
		default: // day
			startTime = time.Now().AddDate(0, 0, -30) // 最近30天
		}
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()
	marketType := strings.TrimSpace(c.Query("market_type"))
	botID := strings.TrimSpace(c.Query("bot_id"))
	statusProvider := pickStatus(c)
	accountScope := ""
	if statusProvider != nil && strings.EqualFold(symbol, statusProvider.Symbol) && strings.EqualFold(exchangeName, statusProvider.Exchange) && strings.EqualFold(marketType, statusProvider.MarketType) {
		accountScope = strings.TrimSpace(statusProvider.AccountScope)
	}
	if exchangeName == "" || symbol == "" || marketType == "" || accountScope == "" || botID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "scoped reconciliation aggregation requires exchange, symbol, market_type, verified account scope, and bot_id"})
		return
	}
	scopedStorage, ok := storage.(marketScopedReconciliationStorage)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage does not support scoped reconciliation aggregation"})
		return
	}

	// 查詢對账历史（獲取所有數據用於聚合）
	histories, err := scopedStorage.QueryReconciliationHistoryByScope(exchangeName, symbol, accountID, marketType, accountScope, botID, startTime, endTime, 10000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 按時间聚合數據
	aggregatedMap := make(map[string]*ReconciliationAggregatedData)

	for _, h := range histories {
		var dateKey string
		t := h.ReconcileTime

		switch period {
		case "month":
			dateKey = t.Format("2006-01")
		case "week":
			year, week := t.ISOWeek()
			dateKey = fmt.Sprintf("%d-W%02d", year, week)
		default: // day
			dateKey = t.Format("2006-01-02")
		}

		if _, exists := aggregatedMap[dateKey]; !exists {
			aggregatedMap[dateKey] = &ReconciliationAggregatedData{
				Date: dateKey,
			}
		}

		agg := aggregatedMap[dateKey]
		agg.AvgLocalPosition += h.LocalPosition
		agg.AvgExchangePosition += h.ExchangePosition
		agg.AvgPositionDiff += h.PositionDiff

		// 對於累计值，取該時间段内的最大值（因為是累计的）
		if h.TotalBuyQty > agg.TotalBuyQty {
			agg.TotalBuyQty = h.TotalBuyQty
		}
		if h.TotalSellQty > agg.TotalSellQty {
			agg.TotalSellQty = h.TotalSellQty
		}
		if h.EstimatedProfit > agg.EstimatedProfit {
			agg.EstimatedProfit = h.EstimatedProfit
		}
		if h.ActualProfit > agg.ActualProfit {
			agg.ActualProfit = h.ActualProfit
		}

		agg.RecordCount++
	}

	// 计算平均值
	result := make([]ReconciliationAggregatedData, 0, len(aggregatedMap))
	for _, agg := range aggregatedMap {
		if agg.RecordCount > 0 {
			agg.AvgLocalPosition /= float64(agg.RecordCount)
			agg.AvgExchangePosition /= float64(agg.RecordCount)
			agg.AvgPositionDiff /= float64(agg.RecordCount)
		}
		result = append(result, *agg)
	}

	// 按日期排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date < result[j].Date
	})

	unscoped, err := scopedStorage.HasUnscopedReconciliationHistory(exchangeName, symbol, accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify legacy reconciliation history"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result, "scope_verified": !unscoped, "unscoped_legacy_records": unscoped})
}
