package web

import (
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"quantmesh/storage"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

// DailyPnLBreakdownSummary 日盈虧拆解摘要
type DailyPnLBreakdownSummary struct {
	TotalBuyOrders             int      `json:"total_buy_orders"`
	TotalBuyQty                float64  `json:"total_buy_qty"`
	TotalBuyValue              float64  `json:"total_buy_value"`
	TotalSellOrders            int      `json:"total_sell_orders"`
	TotalSellQty               float64  `json:"total_sell_qty"`
	TotalSellValue             float64  `json:"total_sell_value"`
	NetCashFlow                float64  `json:"net_cash_flow"`
	NetQtyChange               float64  `json:"net_qty_change"`
	StartPositionQty           float64  `json:"start_position_qty"`
	EndPositionQty             float64  `json:"end_position_qty"`
	StartPositionValue         float64  `json:"start_position_value"`
	EndPositionValue           float64  `json:"end_position_value"`
	PositionValueChange        float64  `json:"position_value_change"`
	NetTradingPnL              float64  `json:"net_trading_pnl"`
	PnLMethod                  string   `json:"pnl_method"`
	GridProfit                 float64  `json:"grid_profit"`
	GridTrades                 int      `json:"grid_trades"`
	TotalFee                   float64  `json:"total_fee"`
	PnLFeeDeduction            float64  `json:"pnl_fee_deduction"`
	FundingFee                 float64  `json:"funding_fee"`
	FundingFeeAsset            string   `json:"funding_fee_asset,omitempty"`
	ExchangePnL                float64  `json:"exchange_pnl"`
	UnrealizedPnLStart         *float64 `json:"unrealized_pnl_start"`
	UnrealizedPnLEnd           *float64 `json:"unrealized_pnl_end"`
	UnrealizedPnLStartVerified bool     `json:"unrealized_pnl_start_verified"`
	UnrealizedPnLEndVerified   bool     `json:"unrealized_pnl_end_verified"`
	OpenPrice                  float64  `json:"open_price"`
	ClosePrice                 float64  `json:"close_price"`
}

// DailyPnLBreakdownResponse 日盈虧拆解 API 響應
type DailyPnLBreakdownResponse struct {
	Date                 string                   `json:"date"`
	Summary              DailyPnLBreakdownSummary `json:"summary"`
	HourlyEquity         []HourlyEquityPoint      `json:"hourly_equity"`
	GridProfitTrades     []TopTradeItem           `json:"grid_profit_trades"`
	GridLossTrades       []TopTradeItem           `json:"grid_loss_trades"`
	ExchangeProfitOrders []ExchangeOrderItem      `json:"exchange_profit_orders"`
	ExchangeLossOrders   []ExchangeOrderItem      `json:"exchange_loss_orders"`
}

// HourlyEquityPoint 小時權益點
type HourlyEquityPoint struct {
	Timestamp int64   `json:"timestamp"`
	Equity    float64 `json:"equity"`
}

// TopTradeItem 單筆成交摘要（網格計算，盈利/虧損 Top）
type TopTradeItem struct {
	SellOrderID int64   `json:"sell_order_id"`
	BuyPrice    float64 `json:"buy_price"`
	SellPrice   float64 `json:"sell_price"`
	Quantity    float64 `json:"quantity"`
	PnL         float64 `json:"pnl"`
	Fee         float64 `json:"fee"`
}

// ExchangeOrderItem 交易所已實現盈虧單筆（交易所計算 Top）
type ExchangeOrderItem struct {
	OrderID     int64   `json:"order_id"`
	Side        string  `json:"side"`
	Price       float64 `json:"price"`
	FilledQty   float64 `json:"filled_qty"`
	RealizedPnL float64 `json:"realized_pnl"`
}

func dailyPnLInterval(date string, loc *time.Location) (time.Time, time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	parsed, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, loc)
	return start, start.AddDate(0, 0, 1), nil
}

func dailyFeeTotalByQuote(fees map[string]float64, quoteAsset string) (float64, error) {
	quoteAsset = strings.ToUpper(strings.TrimSpace(quoteAsset))
	if quoteAsset == "" {
		return 0, fmt.Errorf("quote asset is required")
	}
	var total float64
	for asset, amount := range fees {
		if math.IsNaN(amount) || math.IsInf(amount, 0) {
			return 0, fmt.Errorf("fee amount must be finite")
		}
		asset = strings.ToUpper(strings.TrimSpace(asset))
		if amount == 0 {
			continue
		}
		if asset == "" || asset != quoteAsset {
			return 0, fmt.Errorf("fee in %s requires historical conversion to %s", asset, quoteAsset)
		}
		total += amount
	}
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, fmt.Errorf("fee total is not finite")
	}
	return total, nil
}

func dailyRealizedPnLByQuote(summary storage.DailyOrderFillSummary, quoteAsset string) (float64, error) {
	quoteAsset = strings.ToUpper(strings.TrimSpace(quoteAsset))
	if quoteAsset == "" {
		return 0, fmt.Errorf("quote asset is required")
	}
	if summary.RealizedPnLUnknownAssetCount != 0 {
		return 0, fmt.Errorf("realized pnl has no denomination for %d fills", summary.RealizedPnLUnknownAssetCount)
	}
	var total float64
	for asset, amount := range summary.RealizedPnLByAsset {
		if math.IsNaN(amount) || math.IsInf(amount, 0) {
			return 0, fmt.Errorf("realized pnl amount must be finite")
		}
		if !strings.EqualFold(strings.TrimSpace(asset), quoteAsset) {
			return 0, fmt.Errorf("realized pnl in %s requires historical conversion to %s", asset, quoteAsset)
		}
		total += amount
	}
	if len(summary.RealizedPnLByAsset) == 0 && summary.RealizedPnL != 0 {
		return 0, fmt.Errorf("realized pnl aggregate has no denomination evidence")
	}
	if math.IsNaN(total) || math.IsInf(total, 0) || math.IsNaN(summary.RealizedPnL) || math.IsInf(summary.RealizedPnL, 0) {
		return 0, fmt.Errorf("realized pnl total must be finite")
	}
	if math.Abs(total-summary.RealizedPnL) > math.Max(1e-10, math.Abs(summary.RealizedPnL)*1e-10) {
		return 0, fmt.Errorf("realized pnl asset totals do not reconcile to the daily aggregate")
	}
	return total, nil
}

func dailyFeeTotalByQuoteAndBase(fees, quoteValues, historicalValues map[string]float64, quoteAsset, baseAsset string, allowBaseConversion bool, unknownCounts ...map[string]int) (float64, error) {
	quoteAsset = strings.ToUpper(strings.TrimSpace(quoteAsset))
	baseAsset = strings.ToUpper(strings.TrimSpace(baseAsset))
	if quoteAsset == "" {
		return 0, fmt.Errorf("quote asset is required")
	}
	var total float64
	for asset, amount := range fees {
		asset = strings.ToUpper(strings.TrimSpace(asset))
		if math.IsNaN(amount) || math.IsInf(amount, 0) {
			return 0, fmt.Errorf("fee amount must be finite")
		}
		switch {
		case asset == quoteAsset:
			total += amount
		case allowBaseConversion && baseAsset != "" && asset == baseAsset:
			if amount == 0 {
				continue
			}
			converted, ok := quoteValues[asset]
			if !ok || math.IsNaN(converted) || math.IsInf(converted, 0) {
				return 0, fmt.Errorf("base-asset fee has no finite execution-price conversion")
			}
			total += converted
		default:
			if len(unknownCounts) > 0 && unknownCounts[0][asset] > 0 {
				return 0, fmt.Errorf("fee in %s has %d fills without verified historical conversion to %s", asset, unknownCounts[0][asset], quoteAsset)
			}
			if amount == 0 {
				continue
			}
			converted, ok := historicalValues[asset]
			if !ok || math.IsNaN(converted) || math.IsInf(converted, 0) {
				return 0, fmt.Errorf("fee in %s requires historical conversion to %s", asset, quoteAsset)
			}
			total += converted
		}
	}
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, fmt.Errorf("fee total is not finite")
	}
	return total, nil
}

// dailyPnLFeeDeduction excludes spot base-asset fees already reflected in the
// observed inventory delta. Other fees remain explicit PnL deductions.
func dailyPnLFeeDeduction(totalFees float64, fees, quoteValues map[string]float64, quoteAsset, baseAsset, marketType string) (float64, error) {
	if math.IsNaN(totalFees) || math.IsInf(totalFees, 0) {
		return 0, fmt.Errorf("total fees must be finite")
	}
	if marketType != "spot" || strings.EqualFold(strings.TrimSpace(quoteAsset), strings.TrimSpace(baseAsset)) {
		return totalFees, nil
	}
	baseAsset = strings.ToUpper(strings.TrimSpace(baseAsset))
	if baseAsset == "" {
		return totalFees, nil
	}
	baseFee := fees[baseAsset]
	if baseFee == 0 {
		return totalFees, nil
	}
	baseFeeValue, ok := quoteValues[baseAsset]
	if !ok || math.IsNaN(baseFeeValue) || math.IsInf(baseFeeValue, 0) {
		return 0, fmt.Errorf("base-asset fee has no finite execution-price conversion")
	}
	deduction := totalFees - baseFeeValue
	if math.IsNaN(deduction) || math.IsInf(deduction, 0) {
		return 0, fmt.Errorf("PnL fee deduction is not finite")
	}
	return deduction, nil
}

func spotDailyPositionValues(dayStart, dayEnd time.Time, previous, current *storage.DailySnapshot) (float64, float64, error) {
	const maxSnapshotAge = 2 * time.Hour
	if previous == nil || current == nil || !dayStart.Before(dayEnd) || previous.SnapshotTime.After(dayStart) ||
		dayStart.Sub(previous.SnapshotTime) > maxSnapshotAge || current.SnapshotTime.Before(dayStart) || current.SnapshotTime.After(dayEnd) ||
		dayEnd.Sub(current.SnapshotTime) > maxSnapshotAge || previous.SpotPositionQty == nil || current.SpotPositionQty == nil ||
		previous.ClosingPrice <= 0 || current.ClosingPrice <= 0 {
		return 0, 0, fmt.Errorf("spot PnL requires fresh opening and closing account-inventory snapshots")
	}
	if math.IsNaN(*previous.SpotPositionQty) || math.IsInf(*previous.SpotPositionQty, 0) || *previous.SpotPositionQty < 0 ||
		math.IsNaN(*current.SpotPositionQty) || math.IsInf(*current.SpotPositionQty, 0) || *current.SpotPositionQty < 0 ||
		math.IsNaN(previous.ClosingPrice) || math.IsInf(previous.ClosingPrice, 0) || math.IsNaN(current.ClosingPrice) || math.IsInf(current.ClosingPrice, 0) {
		return 0, 0, fmt.Errorf("spot account inventory snapshot is not finite")
	}
	openingValue := *previous.SpotPositionQty * previous.ClosingPrice
	closingValue := *current.SpotPositionQty * current.ClosingPrice
	if math.IsNaN(openingValue) || math.IsInf(openingValue, 0) || math.IsNaN(closingValue) || math.IsInf(closingValue, 0) {
		return 0, 0, fmt.Errorf("spot account inventory value is not finite")
	}
	return openingValue, closingValue, nil
}

func spotInventoryDeltaMatchesFills(startQty, endQty, buyQty, sellQty, baseAssetFees float64) error {
	values := []float64{startQty, endQty, buyQty, sellQty, baseAssetFees}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("spot inventory reconciliation values must be finite")
		}
	}
	if startQty < 0 || endQty < 0 || buyQty < 0 || sellQty < 0 {
		return fmt.Errorf("spot inventory and fill quantities cannot be negative")
	}
	ledgerDelta := buyQty - sellQty - baseAssetFees
	actualDelta := endQty - startQty
	tolerance := math.Max(1e-8, math.Max(math.Abs(ledgerDelta), math.Abs(actualDelta))*1e-6)
	if math.Abs(actualDelta-ledgerDelta) > tolerance {
		return fmt.Errorf("account inventory change differs from the complete fill ledger")
	}
	return nil
}

func dailyNetTradingPnL(marketType string, cashFlow, positionChange, realizedPnL float64) (float64, string, error) {
	if math.IsNaN(cashFlow) || math.IsInf(cashFlow, 0) || math.IsNaN(positionChange) || math.IsInf(positionChange, 0) || math.IsNaN(realizedPnL) || math.IsInf(realizedPnL, 0) {
		return 0, "", fmt.Errorf("daily PnL inputs must be finite")
	}
	switch marketType {
	case "spot":
		result := cashFlow + positionChange
		if math.IsNaN(result) || math.IsInf(result, 0) {
			return 0, "", fmt.Errorf("daily spot PnL is not finite")
		}
		return result, "cashflow_position_change", nil
	case "futures":
		return realizedPnL, "exchange_realized", nil
	default:
		return 0, "", fmt.Errorf("daily PnL calculation is not defined for this market type")
	}
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
	dayStart, dayEnd, err := dailyPnLInterval(dateStr, loc)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date format, use YYYY-MM-DD"})
		return
	}
	dayStartUTC := dayStart.UTC()
	dayEndUTC := dayEnd.UTC()

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "daily PnL storage is unavailable"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "daily PnL storage is unavailable"})
		return
	}

	accountID := GetCurrentAccountID()
	status := pickStatus(c)
	accountScope := ""
	botID := strings.TrimSpace(c.Query("bot_id"))
	marketType := strings.ToLower(strings.TrimSpace(c.Query("market_type")))
	exchangeID := c.DefaultQuery("exchange", "")
	symbolID := c.DefaultQuery("symbol", "")
	if status != nil {
		if exchangeID == "" || exchangeID == status.Exchange {
			accountScope = status.AccountScope
		}
		if exchangeID == "" {
			exchangeID = status.Exchange
		}
		if symbolID == "" {
			symbolID = status.Symbol
		}
		if marketType == "" {
			marketType = status.MarketType
		}
	}
	if strings.TrimSpace(accountScope) == "" || strings.TrimSpace(marketType) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "daily PnL account scope or market type is unavailable"})
		return
	}
	if botID != "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "complete per-bot execution history is not yet verifiable; account-level fills may contain unattributed activity"})
		return
	}
	coverageReader, ok := st.(interface {
		GetOrderFillCoverage(exchange, marketType, symbol, accountScope string) (*storage.OrderFillCoverage, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "persistent execution-history coverage is unavailable"})
		return
	}
	coverage, err := coverageReader.GetOrderFillCoverage(exchangeID, marketType, symbolID, accountScope)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify execution-history coverage"})
		return
	}
	if dayEndUTC.After(time.Now().UTC()) || coverage == nil || coverage.CoveredFrom.After(dayStartUTC) || coverage.CoveredThrough.Before(dayEndUTC) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution history does not yet cover the complete requested day; no complete daily PnL is available"})
		return
	}
	if marketType == "futures" {
		fundingCoverageReader, ok := st.(interface {
			GetFundingIncomeCoverage(exchange, symbol, marketType, accountScope string) (time.Time, time.Time, error)
		})
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "persistent funding-income coverage is unavailable"})
			return
		}
		fundingFrom, fundingThrough, err := fundingCoverageReader.GetFundingIncomeCoverage(exchangeID, symbolID, "futures", accountScope)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify funding-income coverage"})
			return
		}
		if fundingFrom.IsZero() || fundingThrough.IsZero() || fundingFrom.After(dayStartUTC) || fundingThrough.Before(dayEndUTC) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "funding income does not yet cover the complete requested day; no complete daily PnL is available"})
			return
		}
	}
	summary := DailyPnLBreakdownSummary{}
	quoteAsset := ""
	if status != nil {
		quoteAsset = strings.ToUpper(strings.TrimSpace(status.QuoteAsset))
	}
	if quoteAsset == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "quote asset is unavailable; daily amounts cannot be safely combined"})
		return
	}
	fillReader, ok := st.(interface {
		QueryDailyOrderFillsByScope(account, exchange, marketType, symbol, accountScope string, start, end time.Time) (storage.DailyOrderFillSummary, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "daily execution-ledger aggregation is unavailable"})
		return
	}
	fillSummary, err := fillReader.QueryDailyOrderFillsByScope(accountID, exchangeID, marketType, symbolID, accountScope, dayStartUTC, dayEndUTC)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to aggregate daily execution ledger"})
		return
	}
	verifiedRealizedPnL, err := dailyRealizedPnLByQuote(fillSummary, quoteAsset)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "daily realized PnL denomination cannot be verified for the selected quote asset"})
		return
	}
	baseAsset := ""
	if status != nil {
		baseAsset = strings.ToUpper(strings.TrimSpace(status.BaseAsset))
	}
	feeTotal, err := dailyFeeTotalByQuoteAndBase(fillSummary.FeesByAsset, fillSummary.FeeQuoteValueByAsset, fillSummary.HistoricalFeeQuoteValueByAsset, quoteAsset, baseAsset, marketType == "spot", fillSummary.FeeQuoteUnknownCountByAsset)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "daily fees contain non-quote assets; historical fee conversion evidence is required before net PnL can be reported"})
		return
	}
	summary.TotalFee = feeTotal
	summary.PnLFeeDeduction, err = dailyPnLFeeDeduction(feeTotal, fillSummary.FeesByAsset, fillSummary.FeeQuoteValueByAsset, quoteAsset, baseAsset, marketType)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "daily fee treatment cannot be reconciled with the selected market accounting"})
		return
	}

	// 1. One strict scope drives the paired-trade aggregate and both top lists.
	tradeReader, ok := st.(interface {
		QueryDailyPnLTrades(exchange, marketType, symbol, account, accountScope, botID string, start, end time.Time) (int, float64, float64, []*storage.Trade, []*storage.Trade, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "dimension-filtered daily trade query is unavailable"})
		return
	}
	gridTrades, gridProfit, _, winningTrades, losingTrades, err := tradeReader.QueryDailyPnLTrades(exchangeID, marketType, symbolID, accountID, accountScope, botID, dayStartUTC, dayEndUTC)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load scoped daily trade summary"})
		return
	}
	summary.GridTrades, summary.GridProfit = gridTrades, gridProfit

	// 2. 成交现金流、日初库存与交易所已实现盈亏由同一严格作用域 SQL 聚合，避免分页截断。
	orderReader, ok := st.(interface {
		QueryDailyOrderCashflowByScope(account, exchange, marketType, symbol, accountScope, botID string, start, end time.Time) (storage.DailyOrderCashflow, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scoped daily order aggregation is unavailable"})
		return
	}
	orderSummary, err := orderReader.QueryDailyOrderCashflowByScope(accountID, exchangeID, marketType, symbolID, accountScope, botID, dayStartUTC, dayEndUTC)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load scoped daily order summary"})
		return
	}
	summary.TotalBuyOrders = fillSummary.BuyOrders
	summary.TotalBuyQty = fillSummary.BuyQty
	summary.TotalBuyValue = fillSummary.BuyValue
	summary.TotalSellOrders = fillSummary.SellOrders
	summary.TotalSellQty = fillSummary.SellQty
	summary.TotalSellValue = fillSummary.SellValue
	summary.NetCashFlow = fillSummary.SellValue - fillSummary.BuyValue
	summary.NetQtyChange = fillSummary.BuyQty - fillSummary.SellQty
	summary.StartPositionQty = orderSummary.StartBuyQty - orderSummary.StartSellQty
	summary.EndPositionQty = summary.StartPositionQty + summary.NetQtyChange
	fillRankingReader, ok := st.(interface {
		QueryTopDailyRealizedFills(account, exchange, marketType, symbol, accountScope string, start, end time.Time) ([]*storage.Order, []*storage.Order, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "execution-ledger realized PnL ranking is unavailable"})
		return
	}
	exchangeWinners, exchangeLosers, err := fillRankingReader.QueryTopDailyRealizedFills(accountID, exchangeID, marketType, symbolID, accountScope, dayStartUTC, dayEndUTC)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load scoped daily realized pnl rankings"})
		return
	}

	// 4. 資金費用
	fundingReader, ok := st.(interface {
		GetDailyFundingPaymentsByScope(account, exchange, marketType, symbol, accountScope string, startTime, endTime time.Time) (map[string]float64, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scoped daily funding query is unavailable"})
		return
	}
	fundingMap, err := fundingReader.GetDailyFundingPaymentsByScope(accountID, exchangeID, marketType, symbolID, accountScope, dayStartUTC, dayEndUTC)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load daily funding payments"})
		return
	}
	for asset, amount := range fundingMap {
		if strings.ToUpper(strings.TrimSpace(asset)) != quoteAsset {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "daily funding asset differs from quote asset; currency normalization is required"})
			return
		}
		summary.FundingFeeAsset = asset
		summary.FundingFee = amount
	}

	// 5. 交易所已實現盈虧（當日）
	summary.ExchangePnL = verifiedRealizedPnL

	// 6. 小時權益（當日）並取首條作為 start_position_value
	var hourlyEquity []HourlyEquityPoint
	if exchangeID != "" && symbolID != "" {
		var records []*storage.HourlyEquityRecord
		if accountScope != "" {
			if scopeStorage, ok := st.(interface {
				QueryHourlyEquityRecordsByScope(exchange, marketType, symbol, accountScope string, startTime, endTime time.Time) ([]*storage.HourlyEquityRecord, error)
			}); ok {
				records, err = scopeStorage.QueryHourlyEquityRecordsByScope(exchangeID, marketType, symbolID, accountScope, dayStartUTC, dayEndUTC)
			} else {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scoped hourly equity query is unavailable"})
				return
			}
		} else if marketType != "" {
			marketStorage, ok := st.(interface {
				QueryHourlyEquityRecordsByMarketType(exchange, marketType, symbol, account string, startTime, endTime time.Time) ([]*storage.HourlyEquityRecord, error)
			})
			if !ok {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "market-scoped hourly equity query is unavailable"})
				return
			}
			records, err = marketStorage.QueryHourlyEquityRecordsByMarketType(exchangeID, marketType, symbolID, accountID, dayStartUTC, dayEndUTC)
		} else {
			records, err = st.QueryHourlyEquityRecords(exchangeID, symbolID, accountID, dayStartUTC, dayEndUTC)
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load hourly equity"})
			return
		}
		for _, r := range records {
			hourlyEquity = append(hourlyEquity, HourlyEquityPoint{
				Timestamp: r.Timestamp.Unix(),
				Equity:    r.Equity,
			})
		}
	}

	// 7. 每日快照（收盤未實現盈虧、收盤價、當日開盤/收盤價值）
	var snap *storage.DailySnapshot
	dateForSnapshot := dayStart
	if exchangeID != "" && symbolID != "" {
		if accountScope != "" {
			if scopeStorage, ok := st.(interface {
				GetDailySnapshotByScope(exchange, marketType, symbol, accountScope string, date time.Time) (*storage.DailySnapshot, error)
			}); ok {
				snap, err = scopeStorage.GetDailySnapshotByScope(exchangeID, marketType, symbolID, accountScope, dateForSnapshot)
			} else {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scoped daily snapshot query is unavailable"})
				return
			}
		} else if marketType != "" {
			marketStorage, ok := st.(interface {
				GetDailySnapshotByMarketType(exchange, marketType, symbol, account string, date time.Time) (*storage.DailySnapshot, error)
			})
			if !ok {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "market-scoped daily snapshot query is unavailable"})
				return
			}
			snap, err = marketStorage.GetDailySnapshotByMarketType(exchangeID, marketType, symbolID, accountID, dateForSnapshot)
		} else {
			snap, err = st.GetDailySnapshot(exchangeID, symbolID, accountID, dateForSnapshot)
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load daily snapshot"})
			return
		}
	}
	if snap != nil {
		summary.ClosePrice = snap.ClosingPrice
		summary.EndPositionValue = snap.TotalPositionValue
		if strings.EqualFold(strings.TrimSpace(snap.UnrealizedPnLAsset), quoteAsset) {
			value := snap.UnrealizedPnL
			summary.UnrealizedPnLEnd = &value
			summary.UnrealizedPnLEndVerified = true
		}
	}
	summary.PositionValueChange = summary.EndPositionValue - summary.StartPositionValue
	// 開盤價：若無單獨存儲則用收盤價（前端可選顯示 N/A）
	if summary.OpenPrice == 0 && snap != nil {
		summary.OpenPrice = summary.ClosePrice
	}

	// 8. 未實現盈虧（日初）：前一日收盤快照的 unrealized_pnl，或 0
	if exchangeID != "" && symbolID != "" {
		prevDay := dayStart.AddDate(0, 0, -1)
		var prevSnap *storage.DailySnapshot
		if accountScope != "" {
			if scopeStorage, ok := st.(interface {
				GetDailySnapshotByScope(exchange, marketType, symbol, accountScope string, date time.Time) (*storage.DailySnapshot, error)
			}); ok {
				prevSnap, err = scopeStorage.GetDailySnapshotByScope(exchangeID, marketType, symbolID, accountScope, prevDay)
			} else {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "scoped prior daily snapshot query is unavailable"})
				return
			}
		} else if marketType != "" {
			marketStorage, ok := st.(interface {
				GetDailySnapshotByMarketType(exchange, marketType, symbol, account string, date time.Time) (*storage.DailySnapshot, error)
			})
			if !ok {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "market-scoped prior daily snapshot query is unavailable"})
				return
			}
			prevSnap, err = marketStorage.GetDailySnapshotByMarketType(exchangeID, marketType, symbolID, accountID, prevDay)
		} else {
			prevSnap, err = st.GetDailySnapshot(exchangeID, symbolID, accountID, prevDay)
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load prior daily snapshot"})
			return
		}
		if prevSnap != nil && strings.EqualFold(strings.TrimSpace(prevSnap.UnrealizedPnLAsset), quoteAsset) {
			value := prevSnap.UnrealizedPnL
			summary.UnrealizedPnLStart = &value
			summary.UnrealizedPnLStartVerified = true
		}
		if marketType == "spot" {
			openingValue, closingValue, snapshotErr := spotDailyPositionValues(dayStartUTC, dayEndUTC, prevSnap, snap)
			if snapshotErr != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "spot PnL requires fresh opening and closing position-value snapshots"})
				return
			}
			summary.StartPositionValue = openingValue
			summary.EndPositionValue = closingValue
			summary.StartPositionQty = *prevSnap.SpotPositionQty
			summary.EndPositionQty = *snap.SpotPositionQty
			baseAsset := ""
			if status != nil {
				baseAsset = strings.ToUpper(strings.TrimSpace(status.BaseAsset))
			}
			if baseAsset == "" {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "base asset is unavailable; account inventory cannot be reconciled"})
				return
			}
			if reconcileErr := spotInventoryDeltaMatchesFills(summary.StartPositionQty, summary.EndPositionQty, fillSummary.BuyQty, fillSummary.SellQty, fillSummary.FeesByAsset[baseAsset]); reconcileErr != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "spot account inventory does not reconcile to the execution ledger; external balance changes or missing fills prevent reliable daily PnL"})
				return
			}
		}
	}

	// 现货用实际成交现金流和持仓估值变化；合约用逐笔交易所已实现盈亏，
	// 不把合约名义成交额误当成账户现金流。
	summary.NetTradingPnL, summary.PnLMethod, err = dailyNetTradingPnL(marketType, summary.NetCashFlow, summary.PositionValueChange, summary.ExchangePnL)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "daily PnL calculation is not defined for this market type"})
		return
	}

	// 9. Grid Top trades：与上方摘要使用完全相同的 SQL 过滤集。
	mapTradeItems := func(trades []*storage.Trade) []TopTradeItem {
		items := make([]TopTradeItem, 0, len(trades))
		for _, tr := range trades {
			item := TopTradeItem{
				SellOrderID: tr.SellOrderID,
				BuyPrice:    tr.BuyPrice,
				SellPrice:   tr.SellPrice,
				Quantity:    tr.Quantity,
				PnL:         tr.PnL,
				Fee:         tr.Fee,
			}
			items = append(items, item)
		}
		return items
	}
	gridProfitTrades, gridLossTrades := mapTradeItems(winningTrades), mapTradeItems(losingTrades)

	// 10. Exchange Top orders：當日 FILLED 且 realized_pnl 不為空，盈利 Top / 虧損 Top
	mapExchangeItems := func(orders []*storage.Order) []ExchangeOrderItem {
		items := make([]ExchangeOrderItem, 0, len(orders))
		for _, o := range orders {
			if o.RealizedPnL == nil {
				continue
			}
			qty := o.FilledQty
			if qty <= 0 {
				qty = o.Quantity
			}
			items = append(items, ExchangeOrderItem{OrderID: o.OrderID, Side: o.Side, Price: o.Price, FilledQty: qty, RealizedPnL: *o.RealizedPnL})
		}
		return items
	}
	exchangeProfitOrders, exchangeLossOrders := mapExchangeItems(exchangeWinners), mapExchangeItems(exchangeLosers)

	c.JSON(http.StatusOK, DailyPnLBreakdownResponse{
		Date:                 dateStr,
		Summary:              summary,
		HourlyEquity:         hourlyEquity,
		GridProfitTrades:     gridProfitTrades,
		GridLossTrades:       gridLossTrades,
		ExchangeProfitOrders: exchangeProfitOrders,
		ExchangeLossOrders:   exchangeLossOrders,
	})
}

func emptyDailyBreakdown(dateStr string) gin.H {
	return gin.H{
		"date":                   dateStr,
		"summary":                DailyPnLBreakdownSummary{},
		"hourly_equity":          []HourlyEquityPoint{},
		"grid_profit_trades":     []TopTradeItem{},
		"grid_loss_trades":       []TopTradeItem{},
		"exchange_profit_orders": []ExchangeOrderItem{},
		"exchange_loss_orders":   []ExchangeOrderItem{},
	}
}
