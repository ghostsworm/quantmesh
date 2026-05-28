package web

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

// ========== 對账相关API ==========

// ReconciliationStatus 對账状態
type ReconciliationStatus struct {
	ReconcileCount     int64     `json:"reconcile_count"`      // 對账次數（運行時自增，重啟後歸零）
	HistoryRecordCount int64     `json:"history_record_count"` // 對账歷史記錄數（數據庫，與下方列表一致）
	LastReconcileTime  time.Time `json:"last_reconcile_time"`  // 最后對账時间
	LocalPosition      float64   `json:"local_position"`       // 本地持倉
	TotalBuyQty        float64   `json:"total_buy_qty"`        // 累计買入
	TotalSellQty       float64   `json:"total_sell_qty"`       // 累计賣出
	EstimatedProfit    float64   `json:"estimated_profit"`     // 預计盈利
	ActualProfit       float64   `json:"actual_profit"`        // 實際盈利（来自 trades 表）
}

// ReconciliationHistoryInfo 對账历史信息
type ReconciliationHistoryInfo struct {
	ID               int64     `json:"id"`
	Exchange         string    `json:"exchange"`
	Symbol           string    `json:"symbol"`
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
	if symbol == "" {
		if st := pickStatus(c); st != nil {
			symbol = st.Symbol
			if exchange == "" {
				exchange = st.Exchange
			}
		}
	}

	historyRecordCount := int64(0)
	if symbol != "" && storageProv != nil && storageProv.GetStorage() != nil {
		accountID := GetCurrentAccountID()
		if cnt, err := storageProv.GetStorage().GetReconciliationCount(exchange, symbol, accountID); err == nil {
			historyRecordCount = cnt
		}
	}

	pmProvider := PickPositionProvider(c)
	if pmProvider == nil {
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
		// 從數據库直接计算累计買入和累计賣出（更高效）
		accountID := GetCurrentAccountID()
		buyQty, sellQty, err := storageProv.GetStorage().GetTotalBuySellQty(symbol, accountID, reconcileBotID)
		if err == nil {
			totalBuyQty = buyQty
			totalSellQty = sellQty
			logger.Info("📊 [對账状態] 從數據库查詢: symbol=%s, accountID=%s, bot_id=%s, 累计買入=%.4f, 累计賣出=%.4f", symbol, accountID, reconcileBotID, buyQty, sellQty)
		} else {
			logger.Warn("⚠️ 查詢累计買賣數量失败: symbol=%s, accountID=%s, error=%v", symbol, accountID, err)
		}

		// 如果數據库查詢返回0，尝試不限制account再查詢一次（兼容舊數據）；bot_id 仍保留以免混入其他 Bot
		if totalBuyQty == 0 && totalSellQty == 0 && accountID != "" {
			buyQty2, sellQty2, err2 := storageProv.GetStorage().GetTotalBuySellQty(symbol, "", reconcileBotID)
			if err2 == nil && (buyQty2 > 0 || sellQty2 > 0) {
				totalBuyQty = buyQty2
				totalSellQty = sellQty2
				logger.Info("📊 [對账状態] 從數據库查詢(無account限制): symbol=%s, bot_id=%s, 累计買入=%.4f, 累计賣出=%.4f", symbol, reconcileBotID, buyQty2, sellQty2)
			}
		}
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
		if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 {
			localPosition += slot.PositionQty
		}
	}

	// 獲取實際盈利
	actualProfit := 0.0
	if symbol != "" && storageProv != nil && storageProv.GetStorage() != nil {
		// 查詢截止到現在的累计實際盈利
		accountID := GetCurrentAccountID()
		actualProfit, _ = storageProv.GetStorage().GetActualProfitBySymbol(symbol, accountID, time.Now().UTC(), reconcileBotID)
	}

	status := ReconciliationStatus{
		ReconcileCount:     reconcileCount,
		HistoryRecordCount: historyRecordCount,
		LastReconcileTime:  utils.ToUTC8(lastReconcileTime),
		LocalPosition:      localPosition,
		TotalBuyQty:        totalBuyQty,
		TotalSellQty:       totalSellQty,
		EstimatedProfit:    estimatedProfit,
		ActualProfit:       actualProfit,
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

	// 查詢對账历史
	histories, err := storage.QueryReconciliationHistory(exchangeName, symbol, accountID, startTime, endTime, limit, offset)
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

	c.JSON(http.StatusOK, gin.H{"history": result})
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

	// 查詢對账历史（獲取所有數據用於聚合）
	histories, err := storage.QueryReconciliationHistory(exchangeName, symbol, accountID, startTime, endTime, 10000, 0)
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

	c.JSON(http.StatusOK, gin.H{"data": result})
}
