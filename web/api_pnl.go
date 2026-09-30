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
	Exchange      string  `json:"exchange"`
	MarketType    string  `json:"market_type"`
	PnLAsset      string  `json:"pnl_asset"`
	TotalPnL      float64 `json:"total_pnl"`
	TotalTrades   int     `json:"total_trades"`
	TotalVolume   float64 `json:"total_volume"`
	WinRate       float64 `json:"win_rate"`
	WinningTrades int     `json:"winning_trades"`
	LosingTrades  int     `json:"losing_trades"`
}

func requestedPnLAsset(c *gin.Context) (string, error) {
	asset := strings.ToUpper(strings.TrimSpace(c.Query("pnl_asset")))
	if asset == "" {
		asset = profitSummaryAsset
	}
	if len(asset) > 16 {
		return "", fmt.Errorf("invalid pnl_asset")
	}
	for _, r := range asset {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return "", fmt.Errorf("invalid pnl_asset")
		}
	}
	return asset, nil
}

func queryScopedPnLStreams(st storage.Storage, exchangeID, asset string, startTime, endTime time.Time) ([]*storage.PnLBySymbol, []profitAccountScope, error) {
	scopes, err := resolveProfitAccountScopes(exchangeID)
	if err != nil {
		return nil, nil, err
	}
	reader, ok := st.(interface {
		GetPnLByAccountScopeAndAsset(exchange, accountScope, asset string, startTime, endTime time.Time) ([]*storage.PnLBySymbol, error)
	})
	if !ok {
		return nil, nil, fmt.Errorf("storage does not support account-scope and asset verified PnL")
	}
	var all []*storage.PnLBySymbol
	for _, scope := range scopes {
		rows, queryErr := reader.GetPnLByAccountScopeAndAsset(scope.exchange, scope.scope, asset, startTime, endTime)
		if queryErr != nil {
			return nil, nil, fmt.Errorf("query verified PnL for %s: %w", scope.exchange, queryErr)
		}
		all = append(all, rows...)
	}
	return all, scopes, nil
}

func validatePnLDiagnosisTrade(trade *storage.Trade, exchangeID, accountScope, marketType, asset string) (float64, error) {
	if trade == nil || strings.TrimSpace(trade.Symbol) == "" ||
		!strings.EqualFold(strings.TrimSpace(trade.Exchange), exchangeID) || trade.AccountScope != accountScope ||
		!strings.EqualFold(strings.TrimSpace(trade.MarketType), marketType) || !strings.EqualFold(strings.TrimSpace(trade.PnLAsset), asset) {
		return 0, fmt.Errorf("diagnostic trade identity is missing or outside the requested scope")
	}
	if math.IsNaN(trade.PnL) || math.IsInf(trade.PnL, 0) || math.IsNaN(trade.Fee) || math.IsInf(trade.Fee, 0) ||
		math.IsNaN(trade.Quantity) || math.IsInf(trade.Quantity, 0) || trade.Quantity < 0 ||
		(trade.Fee != 0 && !strings.EqualFold(strings.TrimSpace(trade.FeeAsset), asset)) {
		return 0, fmt.Errorf("diagnostic trade contains an invalid amount or fee denomination")
	}
	netPnL, ok := addFiniteProfitValues(trade.PnL, -trade.Fee)
	if !ok {
		return 0, fmt.Errorf("diagnostic trade net PnL overflowed")
	}
	return netPnL, nil
}

func addPnLDiagnosticFloat(stats map[string]interface{}, key string, value float64) error {
	current, ok := stats[key].(float64)
	if !ok {
		return fmt.Errorf("diagnostic %s accumulator is invalid", key)
	}
	total, ok := addFiniteProfitValues(current, value)
	if !ok {
		return fmt.Errorf("diagnostic %s accumulator overflowed", key)
	}
	stats[key] = total
	return nil
}

// getPnLBySymbol 按币种對查詢盈亏數據
// GET /api/statistics/pnl/symbol
func getPnLBySymbol(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		respondError(c, http.StatusOK, "error.storage_unavailable")
		return
	}

	store := storageProv.GetStorage()
	if store == nil {
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

	asset, err := requestedPnLAsset(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_pnl_asset")
		return
	}

	// A symbol can refer to different exchanges and markets. Never merge those ledgers.
	exchange := strings.ToLower(strings.TrimSpace(c.Query("exchange")))
	marketType := strings.ToLower(strings.TrimSpace(c.Query("market_type")))
	var summary *storage.PnLSummary
	if exchange != "" || marketType != "" {
		if exchange == "" || marketType == "" {
			respondError(c, http.StatusBadRequest, "error.exchange_market_required")
			return
		}
	}
	rows, _, queryErr := queryScopedPnLStreams(store, exchange, asset, startTime, endTime)
	if queryErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "无法按账户作用域和计价币核验 PnL: " + queryErr.Error()})
		return
	}
	var matches []*storage.PnLBySymbol
	for _, row := range rows {
		if !strings.EqualFold(row.Symbol, symbol) {
			continue
		}
		if exchange != "" && (!strings.EqualFold(row.Exchange, exchange) || !strings.EqualFold(row.MarketType, marketType)) {
			continue
		}
		matches = append(matches, row)
	}
	if len(matches) > 1 {
		respondError(c, http.StatusBadRequest, "error.exchange_market_required")
		return
	}
	summary = &storage.PnLSummary{Symbol: symbol, PnLAsset: asset}
	if len(matches) == 1 {
		row := matches[0]
		summary.Exchange = row.Exchange
		summary.MarketType = row.MarketType
		summary.PnLAsset = row.PnLAsset
		summary.TotalPnL = row.TotalPnL
		summary.ExchangePnL = row.ExchangePnL
		summary.TotalTrades = row.TotalTrades
		summary.TotalVolume = row.TotalVolume
		summary.WinRate = row.WinRate
		summary.ExchangeWinRate = row.ExchangeWinRate
		summary.WinningTrades = row.WinningTrades
		summary.LosingTrades = row.LosingTrades
	}

	response := PnLSummaryResponse{
		Symbol:        summary.Symbol,
		Exchange:      summary.Exchange,
		MarketType:    summary.MarketType,
		PnLAsset:      summary.PnLAsset,
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
	Exchange      string   `json:"exchange"`
	Symbol        string   `json:"symbol"`
	MarketType    string   `json:"market_type"`
	PnLAsset      string   `json:"pnl_asset"`
	TotalPnL      float64  `json:"total_pnl"`
	TotalTrades   int      `json:"total_trades"`
	TotalVolume   float64  `json:"total_volume"`
	WinRate       float64  `json:"win_rate"`
	UnrealizedPnL *float64 `json:"unrealized_pnl,omitempty"` // 未實現盈虧需快照明確持久化計價資產後才可返回
}

// getPnLByTimeRange 按時间区间查詢盈亏數據（按币种對分组）
// GET /api/statistics/pnl/time-range
func getPnLByTimeRange(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": []interface{}{}})
		return
	}

	store := storageProv.GetStorage()
	if store == nil {
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

	asset, err := requestedPnLAsset(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_pnl_asset")
		return
	}
	results, scopes, err := queryScopedPnLStreams(store, "", asset, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "无法按账户作用域和计价币核验 PnL: " + err.Error()})
		return
	}
	// 轉换為 API 响应格式；快照只有在计价资产与已实现 PnL 相同时才可合并。
	scopeByExchange := make(map[string]string, len(scopes))
	for _, scope := range scopes {
		scopeByExchange[strings.ToLower(strings.TrimSpace(scope.exchange))] = scope.scope
	}
	response := make([]PnLBySymbolResponse, len(results))
	for i, r := range results {
		resp := PnLBySymbolResponse{
			Exchange:    r.Exchange,
			Symbol:      r.Symbol,
			MarketType:  r.MarketType,
			PnLAsset:    r.PnLAsset,
			TotalPnL:    r.TotalPnL,
			TotalTrades: r.TotalTrades,
			TotalVolume: r.TotalVolume,
			WinRate:     r.WinRate,
		}
		if snapshotStorage, ok := store.(interface {
			GetDailySnapshotByScope(exchange, marketType, symbol, accountScope string, date time.Time) (*storage.DailySnapshot, error)
		}); ok {
			if scope := scopeByExchange[strings.ToLower(strings.TrimSpace(r.Exchange))]; scope != "" {
				location := utils.GlobalLocation
				if location == nil {
					location = time.Local
				}
				date := endTime.In(location)
				snapshot, snapshotErr := snapshotStorage.GetDailySnapshotByScope(r.Exchange, r.MarketType, r.Symbol, scope, date)
				if snapshotErr != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "读取每日 PnL 快照失败"})
					return
				}
				if snapshot != nil && strings.EqualFold(strings.TrimSpace(snapshot.UnrealizedPnLAsset), strings.TrimSpace(r.PnLAsset)) {
					value := snapshot.UnrealizedPnL
					resp.UnrealizedPnL = &value
				}
			}
		}
		response[i] = resp
	}

	c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": response})
}

// ExchangePnLResponse 按交易所分组的盈亏响应
type ExchangePnLResponse struct {
	Exchange    string          `json:"exchange"`
	PnLAsset    string          `json:"pnl_asset"`
	TotalPnL    float64         `json:"total_pnl"`
	TotalTrades int             `json:"total_trades"`
	TotalVolume float64         `json:"total_volume"`
	WinRate     float64         `json:"win_rate"`
	Symbols     []SymbolPnLInfo `json:"symbols"`
}

// SymbolPnLInfo 币种盈亏信息
type SymbolPnLInfo struct {
	Symbol      string  `json:"symbol"`
	PnLAsset    string  `json:"pnl_asset"`
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

	asset, err := requestedPnLAsset(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_pnl_asset")
		return
	}
	results, _, err := queryScopedPnLStreams(storage, "", asset, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "无法按账户作用域和计价币核验 PnL: " + err.Error()})
		return
	}

	// 按交易所分组（直接使用 exchange 字段）
	exchangeMap := make(map[string]*ExchangePnLResponse)
	for _, r := range results {
		if err := validateStrategyPnLStream(r); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PnL 交易所汇总包含无效流水: " + err.Error()})
			return
		}
		exchange := strings.ToLower(r.Exchange)
		if exchange == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PnL 记录缺少交易所归属"})
			return
		}

		if _, exists := exchangeMap[exchange]; !exists {
			exchangeMap[exchange] = &ExchangePnLResponse{
				Exchange:    exchange,
				PnLAsset:    r.PnLAsset,
				TotalPnL:    0,
				TotalTrades: 0,
				TotalVolume: 0,
				WinRate:     0,
				Symbols:     []SymbolPnLInfo{},
			}
		}

		exData := exchangeMap[exchange]
		var aggregateOK bool
		if exData.TotalPnL, aggregateOK = addFiniteProfitValues(exData.TotalPnL, r.TotalPnL); aggregateOK {
			exData.TotalVolume, aggregateOK = addFiniteProfitValues(exData.TotalVolume, r.TotalVolume)
		}
		maxInt := int(^uint(0) >> 1)
		if aggregateOK && r.TotalTrades <= maxInt-exData.TotalTrades {
			exData.TotalTrades += r.TotalTrades
		} else {
			aggregateOK = false
		}
		if !aggregateOK {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PnL 交易所汇总溢出"})
			return
		}

		// 添加币种信息
		exData.Symbols = append(exData.Symbols, SymbolPnLInfo{
			Symbol:      r.Symbol,
			PnLAsset:    r.PnLAsset,
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
	exchange := strings.ToLower(strings.TrimSpace(c.Query("exchange")))
	marketType := strings.ToLower(strings.TrimSpace(c.Query("market_type")))
	if symbol == "" || exchange == "" || marketType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "symbol, exchange and market_type are required for scoped trade diagnostics"})
		return
	}
	asset, err := requestedPnLAsset(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_pnl_asset")
		return
	}
	endTime := time.Now()
	startTime := endTime.AddDate(0, 0, -30)
	if value := c.Query("start_time"); value != "" {
		startTime, err = time.Parse(time.RFC3339, value)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	}
	if value := c.Query("end_time"); value != "" {
		endTime, err = time.Parse(time.RFC3339, value)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	}
	if endTime.Before(startTime) {
		respondError(c, http.StatusBadRequest, "error.invalid_time_range")
		return
	}
	if _, _, err := queryScopedPnLStreams(st, exchange, asset, startTime, endTime); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "无法核验异常交易的账户作用域和计价币: " + err.Error()})
		return
	}
	scopes, err := resolveProfitAccountScopes(exchange)
	if err != nil || len(scopes) != 1 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "无法确定唯一交易凭据作用域"})
		return
	}
	reader, ok := st.(interface {
		QueryTradesByAccountScopeAndAsset(exchange, accountScope, marketType, asset string, startTime, endTime time.Time, limit int) ([]*storage.Trade, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储不支持按精确账户与币种查询诊断流水"})
		return
	}
	trades, err := reader.QueryTradesByAccountScopeAndAsset(exchange, scopes[0].scope, marketType, asset, startTime, endTime, 10000)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	var anomalousTrades []map[string]interface{}
	for _, trade := range trades {
		if !strings.EqualFold(trade.Symbol, symbol) {
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
				"exchange":      trade.Exchange,
				"market_type":   trade.MarketType,
				"pnl_asset":     trade.PnLAsset,
				"buy_price":     trade.BuyPrice,
				"sell_price":    trade.SellPrice,
				"quantity":      trade.Quantity,
				"pnl":           trade.PnL,
				"fee":           trade.Fee,
				"fee_asset":     trade.FeeAsset,
				"pnl_net":       trade.PnL - trade.Fee,
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

	exchangeID := strings.ToLower(strings.TrimSpace(c.Query("exchange")))
	symbolID := strings.TrimSpace(c.Query("symbol"))
	marketType := strings.ToLower(strings.TrimSpace(c.Query("market_type")))
	asset, err := requestedPnLAsset(c)
	if err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_pnl_asset")
		return
	}
	if exchangeID == "" || symbolID == "" || marketType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange, symbol and market_type are required for PnL diagnosis"})
		return
	}
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time

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

	if endTime.Before(startTime) {
		respondError(c, http.StatusBadRequest, "error.invalid_time_range")
		return
	}
	if _, _, err := queryScopedPnLStreams(st, exchangeID, asset, startTime, endTime); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "无法核验诊断账本的账户作用域和计价币: " + err.Error()})
		return
	}
	scopes, err := resolveProfitAccountScopes(exchangeID)
	if err != nil || len(scopes) != 1 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "无法确定唯一交易凭据作用域"})
		return
	}
	tradeReader, ok := st.(interface {
		QueryTradesByAccountScopeAndAsset(exchange, accountScope, marketType, asset string, startTime, endTime time.Time, limit int) ([]*storage.Trade, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储不支持按精确账户与币种查询诊断流水"})
		return
	}
	trades, err := tradeReader.QueryTradesByAccountScopeAndAsset(exchangeID, scopes[0].scope, marketType, asset, startTime, endTime, 100000)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if len(trades) >= 100000 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PnL 诊断结果达到完整性校验上限，拒绝返回可能不完整的汇总"})
		return
	}

	// 過滤指定交易所的交易
	totalPnL := 0.0
	totalTrades := 0
	totalVolume := 0.0
	winningTrades := 0
	losingTrades := 0
	maxProfit := 0.0
	maxLoss := 0.0

	// 按币种分组统计
	symbolStats := make(map[string]map[string]interface{})

	// 按日期分组统计
	dateStats := make(map[string]map[string]interface{})

	for _, trade := range trades {
		netPnL, validationErr := validatePnLDiagnosisTrade(trade, exchangeID, scopes[0].scope, marketType, asset)
		if validationErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "无法核验诊断流水: " + validationErr.Error()})
			return
		}
		if symbolID != "" && !strings.EqualFold(trade.Symbol, symbolID) {
			continue
		}

		var aggregateOK bool
		totalPnL, aggregateOK = addFiniteProfitValues(totalPnL, netPnL)
		if aggregateOK {
			totalVolume, aggregateOK = addFiniteProfitValues(totalVolume, trade.Quantity)
		}
		if !aggregateOK {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PnL 诊断汇总溢出"})
			return
		}
		totalTrades++
		if netPnL > maxProfit {
			maxProfit = netPnL
		}
		if netPnL < maxLoss {
			maxLoss = netPnL
		}

		if netPnL > 0 {
			winningTrades++
		} else if netPnL < 0 {
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
		if err := addPnLDiagnosticFloat(stats, "total_pnl", netPnL); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		stats["total_trades"] = stats["total_trades"].(int) + 1
		if err := addPnLDiagnosticFloat(stats, "total_volume", trade.Quantity); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		if netPnL > 0 {
			stats["winning_trades"] = stats["winning_trades"].(int) + 1
		} else if netPnL < 0 {
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
		if err := addPnLDiagnosticFloat(dateStat, "total_pnl", netPnL); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
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

	// 轉换為列表格式
	symbolList := make([]map[string]interface{}, 0, len(symbolStats))
	for symbol, stats := range symbolStats {
		symbolPnL, pnlOK := roundProfitToCents(stats["total_pnl"].(float64))
		symbolVolume, volumeOK := roundProfitToCents(stats["total_volume"].(float64))
		if !pnlOK || !volumeOK {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PnL 诊断分组金额无法安全舍入"})
			return
		}
		symbolList = append(symbolList, map[string]interface{}{
			"symbol":         symbol,
			"total_pnl":      symbolPnL,
			"total_trades":   stats["total_trades"],
			"total_volume":   symbolVolume,
			"winning_trades": stats["winning_trades"],
			"losing_trades":  stats["losing_trades"],
		})
	}

	// 按日期排序
	dateList := make([]map[string]interface{}, 0, len(dateStats))
	for date, stats := range dateStats {
		datePnL, ok := roundProfitToCents(stats["total_pnl"].(float64))
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PnL 诊断日期金额无法安全舍入"})
			return
		}
		dateList = append(dateList, map[string]interface{}{
			"date":         date,
			"total_pnl":    datePnL,
			"total_trades": stats["total_trades"],
		})
	}
	sort.Slice(dateList, func(i, j int) bool {
		return dateList[i]["date"].(string) < dateList[j]["date"].(string)
	})

	// Orders lack denomination and credential-scope evidence, so their realized
	// PnL must not be presented as a comparable figure.
	gridPnL, gridOK := roundProfitToCents(totalPnL)
	roundedVolume, volumeOK := roundProfitToCents(totalVolume)
	roundedAvgPnL, avgOK := roundProfitToCents(avgPnL)
	roundedMaxProfit, maxProfitOK := roundProfitToCents(maxProfit)
	roundedMaxLoss, maxLossOK := roundProfitToCents(maxLoss)
	if !gridOK || !volumeOK || !avgOK || !maxProfitOK || !maxLossOK {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "PnL 诊断金额无法安全舍入"})
		return
	}
	discrepancyExplanation := "交易所订单 realized_pnl 缺少计价币与凭据作用域证据，未与成交账本比较。"

	c.JSON(http.StatusOK, gin.H{
		"exchange":    exchangeID,
		"symbol":      symbolID,
		"market_type": marketType,
		"pnl_asset":   asset,
		"time_range": gin.H{
			"start": startTime.Format(time.RFC3339),
			"end":   endTime.Format(time.RFC3339),
		},
		"pnl_comparison": gin.H{
			"grid_pnl":                 gridPnL,
			"exchange_pnl":             nil,
			"discrepancy":              nil,
			"discrepancy_explanation":  discrepancyExplanation,
			"orders_with_realized_pnl": nil,
			"sell_orders_missing_pnl":  nil,
		},
		"summary": gin.H{
			"total_pnl":      gridPnL,
			"total_trades":   totalTrades,
			"total_volume":   roundedVolume,
			"winning_trades": winningTrades,
			"losing_trades":  losingTrades,
			"win_rate":       math.Round(winRate*10000) / 100,
			"avg_pnl":        roundedAvgPnL,
			"max_profit":     roundedMaxProfit,
			"max_loss":       roundedMaxLoss,
		},
		"by_symbol": symbolList,
		"by_date":   dateList,
		"note":      "仅展示指定凭据、交易所、市场与计价币下已扣除同币种手续费的配对成交盈亏。交易所订单 realized_pnl 尚无法证明计价币和账户归属，故不参与对比。",
	})
}
