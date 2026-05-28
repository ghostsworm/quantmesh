package web

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"quantmesh/storage"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

// ========== 盈亏统计相关API ==========

// PnLSummaryResponse 盈亏彙總响应
type PnLSummaryResponse struct {
	Symbol        string  `json:"symbol"`
	TotalPnL      float64 `json:"total_pnl"`
	TotalTrades   int     `json:"total_trades"`
	TotalVolume   float64 `json:"total_volume"`
	WinRate       float64 `json:"win_rate"`
	WinningTrades int     `json:"winning_trades"`
	LosingTrades  int     `json:"losing_trades"`
}

// getPnLBySymbol 按币种對查詢盈亏數據
// GET /api/statistics/pnl/symbol
func getPnLBySymbol(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		respondError(c, http.StatusOK, "error.storage_unavailable")
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		respondError(c, http.StatusOK, "error.storage_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_symbol_param")
		return
	}

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
		// 默认最近30天
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

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()

	// 查詢盈亏數據
	summary, err := storage.GetPnLBySymbol(symbol, accountID, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := PnLSummaryResponse{
		Symbol:        summary.Symbol,
		TotalPnL:      summary.TotalPnL,
		TotalTrades:   summary.TotalTrades,
		TotalVolume:   summary.TotalVolume,
		WinRate:       summary.WinRate,
		WinningTrades: summary.WinningTrades,
		LosingTrades:  summary.LosingTrades,
	}

	c.JSON(http.StatusOK, response)
}

// PnLBySymbolResponse 按币种對的盈亏數據
type PnLBySymbolResponse struct {
	Symbol        string  `json:"symbol"`
	TotalPnL      float64 `json:"total_pnl"`
	TotalTrades   int     `json:"total_trades"`
	TotalVolume   float64 `json:"total_volume"`
	WinRate       float64 `json:"win_rate"`
	UnrealizedPnL float64 `json:"unrealized_pnl,omitempty"` // 時段內最後一天的收盤未實現盈虧（來自每日快照）
}

// getPnLByTimeRange 按時间区间查詢盈亏數據（按币种對分组）
// GET /api/statistics/pnl/time-range
func getPnLByTimeRange(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": []interface{}{}})
		return
	}

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
		// 默认最近30天
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

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()

	// 查詢盈亏數據
	results, err := storage.GetPnLByTimeRange(accountID, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 轉换為 API 响应格式，並補齊未實現盈虧（取自時段最後一天的每日快照）
	response := make([]PnLBySymbolResponse, len(results))
	endDate := time.Date(endTime.Year(), endTime.Month(), endTime.Day(), 0, 0, 0, 0, endTime.Location())
	for i, r := range results {
		resp := PnLBySymbolResponse{
			Symbol:      r.Symbol,
			TotalPnL:    r.TotalPnL,
			TotalTrades: r.TotalTrades,
			TotalVolume: r.TotalVolume,
			WinRate:     r.WinRate,
		}
		if snap, err := storage.GetDailySnapshot(r.Exchange, r.Symbol, accountID, endDate); err == nil && snap != nil {
			resp.UnrealizedPnL = snap.UnrealizedPnL
		}
		response[i] = resp
	}

	c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": response})
}

// ExchangePnLResponse 按交易所分组的盈亏响应
type ExchangePnLResponse struct {
	Exchange    string          `json:"exchange"`
	TotalPnL    float64         `json:"total_pnl"`
	TotalTrades int             `json:"total_trades"`
	TotalVolume float64         `json:"total_volume"`
	WinRate     float64         `json:"win_rate"`
	Symbols     []SymbolPnLInfo `json:"symbols"`
}

// SymbolPnLInfo 币种盈亏信息
type SymbolPnLInfo struct {
	Symbol      string  `json:"symbol"`
	TotalPnL    float64 `json:"total_pnl"`
	TotalTrades int     `json:"total_trades"`
	TotalVolume float64 `json:"total_volume"`
	WinRate     float64 `json:"win_rate"`
}

// maxPnLExchangeQueryRange 按交易所聚合盈亏時允許的最大時间跨度（防止一次掃描過大時間區間拖慢 MySQL）
const maxPnLExchangeQueryRange = 90 * 24 * time.Hour

// getPnLByExchange 按交易所分组查詢盈亏數據
// GET /api/statistics/pnl/exchange
func getPnLByExchange(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"exchanges": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"exchanges": []interface{}{}})
		return
	}

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
		// 默认最近30天
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

	if endTime.Before(startTime) {
		respondError(c, http.StatusBadRequest, "error.invalid_time_range")
		return
	}

	rangeClamped := false
	if endTime.Sub(startTime) > maxPnLExchangeQueryRange {
		startTime = endTime.Add(-maxPnLExchangeQueryRange)
		rangeClamped = true
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()

	// 查詢所有币种的盈亏數據（現在包含 exchange 字段）
	results, err := storage.GetPnLByTimeRange(accountID, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 按交易所分组（直接使用 exchange 字段）
	exchangeMap := make(map[string]*ExchangePnLResponse)
	for _, r := range results {
		exchange := strings.ToLower(r.Exchange)
		if exchange == "" {
			// 兼容舊數據：如果没有 exchange，默认為 binance
			exchange = "binance"
		}

		if _, exists := exchangeMap[exchange]; !exists {
			exchangeMap[exchange] = &ExchangePnLResponse{
				Exchange:    exchange,
				TotalPnL:    0,
				TotalTrades: 0,
				TotalVolume: 0,
				WinRate:     0,
				Symbols:     []SymbolPnLInfo{},
			}
		}

		exData := exchangeMap[exchange]
		exData.TotalPnL += r.TotalPnL
		exData.TotalTrades += r.TotalTrades
		exData.TotalVolume += r.TotalVolume

		// 添加币种信息
		exData.Symbols = append(exData.Symbols, SymbolPnLInfo{
			Symbol:      r.Symbol,
			TotalPnL:    r.TotalPnL,
			TotalTrades: r.TotalTrades,
			TotalVolume: r.TotalVolume,
			WinRate:     r.WinRate,
		})
	}

	// 计算每個交易所的胜率
	for _, exData := range exchangeMap {
		if exData.TotalTrades > 0 {
			winningTrades := 0
			for _, sym := range exData.Symbols {
				winningTrades += int(float64(sym.TotalTrades) * sym.WinRate)
			}
			exData.WinRate = float64(winningTrades) / float64(exData.TotalTrades)
		}
	}

	// 轉换為列表
	response := make([]ExchangePnLResponse, 0, len(exchangeMap))
	for _, exData := range exchangeMap {
		response = append(response, *exData)
	}

	// 按交易所名称排序
	sort.Slice(response, func(i, j int) bool {
		return response[i].Exchange < response[j].Exchange
	})

	out := gin.H{
		"exchanges": response,
	}
	if rangeClamped {
		out["range_clamped"] = true
		out["effective_start_time"] = startTime.UTC().Format(time.RFC3339)
		out["effective_end_time"] = endTime.UTC().Format(time.RFC3339)
	}
	c.JSON(http.StatusOK, out)
}

// getAnomalousTrades 检查异常交易記錄（用於調試盈亏计算问题）
// GET /api/statistics/anomalous-trades
func getAnomalousTrades(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"anomalous_trades": []interface{}{}})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"anomalous_trades": []interface{}{}})
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_symbol_param")
		return
	}

	// 查詢所有交易記錄
	trades, err := st.QueryTrades(time.Time{}, time.Now(), 1000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var anomalousTrades []map[string]interface{}
	for _, trade := range trades {
		if trade.Symbol != symbol {
			continue
		}

		// 计算订單金額
		orderAmount := trade.BuyPrice * trade.Quantity

		// 检查是否异常：盈亏超過订單金額的50%可能是錯误的
		if orderAmount > 0 && math.Abs(trade.PnL) > orderAmount*0.5 {
			anomalousTrades = append(anomalousTrades, map[string]interface{}{
				"buy_order_id":  trade.BuyOrderID,
				"sell_order_id": trade.SellOrderID,
				"symbol":        trade.Symbol,
				"buy_price":     trade.BuyPrice,
				"sell_price":    trade.SellPrice,
				"quantity":      trade.Quantity,
				"pnl":           trade.PnL,
				"order_amount":  orderAmount,
				"pnl_rate":      (trade.PnL / orderAmount) * 100,
				"created_at":    utils.ToUTC8(trade.CreatedAt),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"anomalous_trades": anomalousTrades,
		"count":            len(anomalousTrades),
	})
}

// getExchangePnLDiagnosis 诊断交易所盈亏數據，對比網格盈虧與交易所盈虧的差異
// GET /api/statistics/pnl/diagnosis?exchange=&symbol=&start_time=&end_time=
func getExchangePnLDiagnosis(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"error": "存儲服務未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"error": "存儲接口未就绪"})
		return
	}

	exchangeID := strings.ToLower(c.DefaultQuery("exchange", "binance"))
	symbolID := c.Query("symbol") // 可選，用於篩選交易對
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
		// 默认查詢所有历史數據
		startTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
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

	// 查詢該交易所的所有交易記錄
	trades, err := st.QueryTrades(startTime, endTime, 100000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 過滤指定交易所的交易
	var filteredTrades []*storage.Trade
	totalPnL := 0.0
	totalTrades := 0
	totalVolume := 0.0
	winningTrades := 0
	losingTrades := 0

	// 按币种分组统计
	symbolStats := make(map[string]map[string]interface{})

	// 按日期分组统计
	dateStats := make(map[string]map[string]interface{})

	for _, trade := range trades {
		tradeExchange := strings.ToLower(trade.Exchange)
		if tradeExchange == "" {
			tradeExchange = "binance" // 兼容舊數據
		}

		if tradeExchange != exchangeID {
			continue
		}
		if symbolID != "" && !strings.EqualFold(trade.Symbol, symbolID) {
			continue
		}

		filteredTrades = append(filteredTrades, trade)
		totalPnL += trade.PnL
		totalTrades++
		totalVolume += trade.Quantity

		if trade.PnL > 0 {
			winningTrades++
		} else if trade.PnL < 0 {
			losingTrades++
		}

		// 按币种统计
		if _, exists := symbolStats[trade.Symbol]; !exists {
			symbolStats[trade.Symbol] = map[string]interface{}{
				"total_pnl":      0.0,
				"total_trades":   0,
				"total_volume":   0.0,
				"winning_trades": 0,
				"losing_trades":  0,
			}
		}
		stats := symbolStats[trade.Symbol]
		stats["total_pnl"] = stats["total_pnl"].(float64) + trade.PnL
		stats["total_trades"] = stats["total_trades"].(int) + 1
		stats["total_volume"] = stats["total_volume"].(float64) + trade.Quantity
		if trade.PnL > 0 {
			stats["winning_trades"] = stats["winning_trades"].(int) + 1
		} else if trade.PnL < 0 {
			stats["losing_trades"] = stats["losing_trades"].(int) + 1
		}

		// 按日期统计
		dateStr := trade.CreatedAt.Format("2006-01-02")
		if _, exists := dateStats[dateStr]; !exists {
			dateStats[dateStr] = map[string]interface{}{
				"total_pnl":    0.0,
				"total_trades": 0,
			}
		}
		dateStat := dateStats[dateStr]
		dateStat["total_pnl"] = dateStat["total_pnl"].(float64) + trade.PnL
		dateStat["total_trades"] = dateStat["total_trades"].(int) + 1
	}

	// 计算平均盈亏
	avgPnL := 0.0
	if totalTrades > 0 {
		avgPnL = totalPnL / float64(totalTrades)
	}

	// 计算胜率
	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(winningTrades) / float64(totalTrades)
	}

	// 找出最大的單笔盈亏
	maxProfit := 0.0
	maxLoss := 0.0
	for _, trade := range filteredTrades {
		if trade.PnL > maxProfit {
			maxProfit = trade.PnL
		}
		if trade.PnL < maxLoss {
			maxLoss = trade.PnL
		}
	}

	// 轉换為列表格式
	symbolList := make([]map[string]interface{}, 0, len(symbolStats))
	for symbol, stats := range symbolStats {
		symbolList = append(symbolList, map[string]interface{}{
			"symbol":         symbol,
			"total_pnl":      stats["total_pnl"],
			"total_trades":   stats["total_trades"],
			"total_volume":   stats["total_volume"],
			"winning_trades": stats["winning_trades"],
			"losing_trades":  stats["losing_trades"],
		})
	}

	// 按日期排序
	dateList := make([]map[string]interface{}, 0, len(dateStats))
	for date, stats := range dateStats {
		dateList = append(dateList, map[string]interface{}{
			"date":         date,
			"total_pnl":    stats["total_pnl"],
			"total_trades": stats["total_trades"],
		})
	}
	sort.Slice(dateList, func(i, j int) bool {
		return dateList[i]["date"].(string) < dateList[j]["date"].(string)
	})

	// 🔥 對比網格盈虧與交易所盈虧
	gridPnL := math.Round((totalPnL)*100) / 100
	exchangePnL := 0.0
	orderStatsWithPnL := 0
	orderStatsMissingPnL := 0
	if epGetter, ok := st.(interface {
		GetExchangePnLTotal(exchange, symbol, botID string) (float64, error)
	}); ok {
		if ep, err := epGetter.GetExchangePnLTotal(exchangeID, symbolID, ""); err == nil {
			exchangePnL = math.Round(ep*100) / 100
		}
	}
	if statsGetter, ok := st.(interface {
		GetExchangePnLOrderStats(exchange, symbol string) (withPnLCount, missingPnLCount int, totalPnL float64, err error)
	}); ok {
		if withCnt, missingCnt, _, err := statsGetter.GetExchangePnLOrderStats(exchangeID, symbolID); err == nil {
			orderStatsWithPnL = withCnt
			orderStatsMissingPnL = missingCnt
		}
	}
	discrepancy := math.Round((gridPnL-exchangePnL)*100) / 100
	discrepancyExplanation := ""
	if math.Abs(discrepancy) > 1 {
		if (gridPnL > 0 && exchangePnL < 0) || (gridPnL < 0 && exchangePnL > 0) {
			discrepancyExplanation = "盈虧性質相反：網格按槽位買賣配對計算（每格低買高賣），交易所按持倉加權均價計算。若持倉均價高於多數賣出價，交易所會顯示虧損，而網格可能顯示盈利。"
		} else {
			discrepancyExplanation = "差異較大：計算口徑不同。網格=按槽位配對；交易所=按持倉加權均價。持倉結構（買入價分佈）會導致兩者差異。"
		}
		if orderStatsMissingPnL > 0 {
			discrepancyExplanation += fmt.Sprintf(" 另：有 %d 筆 FILLED 賣單缺少 realized_pnl，可能漏記交易所數據。", orderStatsMissingPnL)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"exchange": exchangeID,
		"symbol":   symbolID,
		"time_range": gin.H{
			"start": startTime.Format(time.RFC3339),
			"end":   endTime.Format(time.RFC3339),
		},
		"pnl_comparison": gin.H{
			"grid_pnl":                 gridPnL,
			"exchange_pnl":             exchangePnL,
			"discrepancy":              discrepancy,
			"discrepancy_explanation":  discrepancyExplanation,
			"orders_with_realized_pnl": orderStatsWithPnL,
			"sell_orders_missing_pnl":  orderStatsMissingPnL,
		},
		"summary": gin.H{
			"total_pnl":      gridPnL,
			"total_trades":   totalTrades,
			"total_volume":   math.Round(totalVolume*100) / 100,
			"winning_trades": winningTrades,
			"losing_trades":  losingTrades,
			"win_rate":       math.Round(winRate*10000) / 100,
			"avg_pnl":        math.Round(avgPnL*100) / 100,
			"max_profit":     math.Round(maxProfit*100) / 100,
			"max_loss":       math.Round(maxLoss*100) / 100,
		},
		"by_symbol": symbolList,
		"by_date":   dateList,
		"note":      "網格盈虧按買賣配對計算（未扣手續費）；交易所盈虧為交易所 API 返回的已實現盈虧。兩者計算口徑不同，存在差異屬正常。",
	})
}
