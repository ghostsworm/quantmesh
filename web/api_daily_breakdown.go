package web

import (
	"net/http"
	"sort"
	"time"

	"quantmesh/storage"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

// DailyPnLBreakdownSummary 日盈虧拆解摘要
type DailyPnLBreakdownSummary struct {
	TotalBuyOrders    int     `json:"total_buy_orders"`
	TotalBuyQty       float64 `json:"total_buy_qty"`
	TotalBuyValue     float64 `json:"total_buy_value"`
	TotalSellOrders   int     `json:"total_sell_orders"`
	TotalSellQty      float64 `json:"total_sell_qty"`
	TotalSellValue    float64 `json:"total_sell_value"`
	NetCashFlow       float64 `json:"net_cash_flow"`
	NetQtyChange      float64 `json:"net_qty_change"`
	StartPositionQty  float64 `json:"start_position_qty"`
	EndPositionQty    float64 `json:"end_position_qty"`
	StartPositionValue float64 `json:"start_position_value"`
	EndPositionValue  float64 `json:"end_position_value"`
	PositionValueChange float64 `json:"position_value_change"`
	NetTradingPnL    float64 `json:"net_trading_pnl"`
	GridProfit       float64 `json:"grid_profit"`
	GridTrades       int     `json:"grid_trades"`
	TotalFee         float64 `json:"total_fee"`
	FundingFee       float64 `json:"funding_fee"`
	ExchangePnL      float64 `json:"exchange_pnl"`
	UnrealizedPnLStart float64 `json:"unrealized_pnl_start"`
	UnrealizedPnLEnd float64 `json:"unrealized_pnl_end"`
	OpenPrice        float64 `json:"open_price"`
	ClosePrice       float64 `json:"close_price"`
}

// DailyPnLBreakdownResponse 日盈虧拆解 API 響應
type DailyPnLBreakdownResponse struct {
	Date          string                   `json:"date"`
	Summary       DailyPnLBreakdownSummary `json:"summary"`
	HourlyEquity  []HourlyEquityPoint     `json:"hourly_equity"`
	TopTrades     []TopTradeItem          `json:"top_trades"`
}

// HourlyEquityPoint 小時權益點
type HourlyEquityPoint struct {
	Timestamp int64   `json:"timestamp"`
	Equity    float64 `json:"equity"`
}

// TopTradeItem 單筆成交摘要（用於 top_trades）
type TopTradeItem struct {
	SellOrderID int64   `json:"sell_order_id"`
	BuyPrice    float64 `json:"buy_price"`
	SellPrice   float64 `json:"sell_price"`
	Quantity    float64 `json:"quantity"`
	PnL         float64 `json:"pnl"`
}

// getDailyPnLBreakdown 獲取指定日的盈虧拆解
// GET /api/statistics/daily/breakdown?date=YYYY-MM-DD&exchange=X&symbol=Y
func getDailyPnLBreakdown(c *gin.Context) {
	dateStr := c.Query("date")
	if dateStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "date is required (YYYY-MM-DD)"})
		return
	}
	loc := utils.GlobalLocation
	if loc == nil {
		loc = time.UTC
	}
	t, err := time.ParseInLocation("2006-01-02", dateStr, loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYY-MM-DD"})
		return
	}
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)
	dayStartUTC := dayStart.UTC()
	dayEndUTC := dayEnd.UTC()

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil {
		c.JSON(http.StatusOK, emptyDailyBreakdown(dateStr))
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, emptyDailyBreakdown(dateStr))
		return
	}

	accountID := GetCurrentAccountID()
	status := pickStatus(c)
	exchangeID := c.DefaultQuery("exchange", "")
	symbolID := c.DefaultQuery("symbol", "")
	if status != nil {
		if exchangeID == "" {
			exchangeID = status.Exchange
		}
		if symbolID == "" {
			symbolID = status.Symbol
		}
	}

	summary := DailyPnLBreakdownSummary{}

	// 1. Trades 當日成交摘要（筆數、毛利、手續費）
	gridTrades, gridProfit, totalFee, _ := st.GetDailyTradesSummary(exchangeID, accountID, dateStr)
	summary.GridTrades = gridTrades
	summary.GridProfit = gridProfit
	summary.TotalFee = totalFee

	// 2. Orders 當日訂單聚合（買/賣筆數、數量、金額）
	orders, errOrders := st.QueryOrdersWithTimeRange(10000, 0, "FILLED", &dayStartUTC, &dayEndUTC)
	if errOrders == nil {
		var totalBuyQty, totalBuyValue, totalSellQty, totalSellValue float64
		var buyCount, sellCount int
		for _, o := range orders {
			if exchangeID != "" && o.Exchange != exchangeID {
				continue
			}
			if symbolID != "" && o.Symbol != symbolID {
				continue
			}
			qty := o.FilledQty
			if qty <= 0 {
				qty = o.Quantity
			}
			value := o.Price * qty
			if o.Side == "BUY" {
				buyCount++
				totalBuyQty += qty
				totalBuyValue += value
			} else {
				sellCount++
				totalSellQty += qty
				totalSellValue += value
			}
		}
		summary.TotalBuyOrders = buyCount
		summary.TotalBuyQty = totalBuyQty
		summary.TotalBuyValue = totalBuyValue
		summary.TotalSellOrders = sellCount
		summary.TotalSellQty = totalSellQty
		summary.TotalSellValue = totalSellValue
		summary.NetCashFlow = totalSellValue - totalBuyValue
		summary.NetQtyChange = totalBuyQty - totalSellQty
	}

	// 3. 日初持倉數量（用於 end_position_qty）
	startBuyQty, startSellQty, _ := st.GetFilledOrderQtySumBeforeTime(exchangeID, symbolID, dayStartUTC)
	summary.StartPositionQty = startBuyQty - startSellQty
	summary.EndPositionQty = summary.StartPositionQty + summary.NetQtyChange

	// 4. 資金費用
	fundingMap, _ := st.GetDailyFundingPayments(accountID, exchangeID, dayStartUTC, dayEndUTC)
	if v, ok := fundingMap[dateStr]; ok {
		summary.FundingFee = v
	}

	// 5. 交易所已實現盈虧（當日）
	startDate := dayStart
	endDate := dayStart.Add(24*time.Hour - time.Nanosecond)
	epMap, _ := st.GetDailyExchangePnL(exchangeID, symbolID, startDate, endDate)
	if v, ok := epMap[dateStr]; ok {
		summary.ExchangePnL = v
	}

	// 6. 小時權益（當日）並取首條作為 start_position_value
	var hourlyEquity []HourlyEquityPoint
	if exchangeID != "" && symbolID != "" {
		records, _ := st.QueryHourlyEquityRecords(exchangeID, symbolID, accountID, dayStartUTC, dayEndUTC)
		for i, r := range records {
			hourlyEquity = append(hourlyEquity, HourlyEquityPoint{
				Timestamp: r.Timestamp.Unix(),
				Equity:    r.Equity,
			})
			if i == 0 {
				summary.StartPositionValue = r.TotalPositionValue
			}
		}
	}

	// 7. 每日快照（收盤未實現盈虧、收盤價、當日開盤/收盤價值）
	var snap *storage.DailySnapshot
	dateForSnapshot, _ := time.Parse("2006-01-02", dateStr)
	if exchangeID != "" && symbolID != "" {
		snap, _ = st.GetDailySnapshot(exchangeID, symbolID, accountID, dateForSnapshot)
	}
	if snap != nil {
		summary.UnrealizedPnLEnd = snap.UnrealizedPnL
		summary.ClosePrice = snap.ClosingPrice
		summary.EndPositionValue = snap.TotalPositionValue
	}
	summary.PositionValueChange = summary.EndPositionValue - summary.StartPositionValue
	// 開盤價：若無單獨存儲則用收盤價（前端可選顯示 N/A）
	if summary.OpenPrice == 0 && snap != nil {
		summary.OpenPrice = summary.ClosePrice
	}

	// 8. 未實現盈虧（日初）：前一日收盤快照的 unrealized_pnl，或 0
	if exchangeID != "" && symbolID != "" {
		prevDay := dayStart.AddDate(0, 0, -1)
		prevSnap, _ := st.GetDailySnapshot(exchangeID, symbolID, accountID, prevDay)
		if prevSnap != nil {
			summary.UnrealizedPnLStart = prevSnap.UnrealizedPnL
		}
	}

	// 淨交易盈虧：現金流 + 持倉價值變化（與手續費、資金費、交易所 PnL 的關係見下方）
	summary.NetTradingPnL = summary.NetCashFlow + summary.PositionValueChange

	// 9. Top trades（當日成交按 PnL 排序取前若干筆）
	var topTrades []TopTradeItem
	trades, errTrades := st.QueryTrades(dayStartUTC, dayEndUTC, 500, 0)
	if errTrades == nil {
		for _, tr := range trades {
			if exchangeID != "" && tr.Exchange != exchangeID {
				continue
			}
			if symbolID != "" && tr.Symbol != symbolID {
				continue
			}
			topTrades = append(topTrades, TopTradeItem{
				SellOrderID: tr.SellOrderID,
				BuyPrice:    tr.BuyPrice,
				SellPrice:   tr.SellPrice,
				Quantity:    tr.Quantity,
				PnL:         tr.PnL,
			})
		}
		sort.Slice(topTrades, func(i, j int) bool { return topTrades[i].PnL > topTrades[j].PnL })
		if len(topTrades) > 20 {
			topTrades = topTrades[:20]
		}
	}

	c.JSON(http.StatusOK, DailyPnLBreakdownResponse{
		Date:         dateStr,
		Summary:      summary,
		HourlyEquity: hourlyEquity,
		TopTrades:    topTrades,
	})
}

func emptyDailyBreakdown(dateStr string) gin.H {
	return gin.H{
		"date":          dateStr,
		"summary":       DailyPnLBreakdownSummary{},
		"hourly_equity": []HourlyEquityPoint{},
		"top_trades":    []TopTradeItem{},
	}
}
