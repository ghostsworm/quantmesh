package web

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"quantmesh/storage"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

// ProfitSummary 盈利彙總
type ProfitSummary struct {
	ExchangeID          string  `json:"exchangeId,omitempty"`
	TotalProfit         float64 `json:"totalProfit"` // 淨利潤（毛利 - 手續費 + 資金費淨額）
	GrossProfit         float64 `json:"grossProfit"` // 毛利（價差盈虧，未扣手續費）
	TotalFee            float64 `json:"totalFee"`    // 手續費合計
	FundingNet          float64 `json:"fundingNet"`  // 資金費淨額（正=淨收入，負=淨支出）
	TodayProfit         float64 `json:"todayProfit"`
	WeekProfit          float64 `json:"weekProfit"`
	MonthProfit         float64 `json:"monthProfit"`
	UnrealizedProfit    float64 `json:"unrealizedProfit"` // 未實現盈利（根據當前倉位和價格計算）
	ExchangeProfit      float64 `json:"exchangeProfit"`    // 交易所盈利（根據每筆訂單中交易所返回的 RealizedPnL 計算）
	WithdrawnProfit     float64 `json:"withdrawnProfit"`
	AvailableToWithdraw float64 `json:"availableToWithdraw"`
	PriceDeviationLoss  float64 `json:"priceDeviationLoss"` // 🔥 價格偏差導致的總損失（USDT）
	BuyPriceDeviation    float64 `json:"buyPriceDeviation"`  // 🔥 買入價格偏差總和（USDT）
	SellPriceDeviation   float64 `json:"sellPriceDeviation"` // 🔥 賣出價格偏差總和（USDT）
	LastUpdated         string  `json:"lastUpdated"`
}

// StrategyProfit 策略盈利
type StrategyProfit struct {
	ExchangeID          string  `json:"exchangeId"`
	StrategyID          string  `json:"strategyId"`
	StrategyName        string  `json:"strategyName"`
	StrategyType        string  `json:"strategyType"`
	TotalProfit         float64 `json:"totalProfit"`
	TodayProfit         float64 `json:"todayProfit"`
	UnrealizedProfit    float64 `json:"unrealizedProfit"`
	RealizedProfit      float64 `json:"realizedProfit"`
	WithdrawnProfit     float64 `json:"withdrawnProfit"`
	AvailableToWithdraw float64 `json:"availableToWithdraw"`
	TradeCount          int     `json:"tradeCount"`
	WinRate             float64 `json:"winRate"`
	AvgProfitPerTrade   float64 `json:"avgProfitPerTrade"`
	LastTradeAt         string  `json:"lastTradeAt,omitempty"`
}

// ProfitWithdrawRule 提取规则
type ProfitWithdrawRule struct {
	ID              string  `json:"id"`
	ExchangeID      string  `json:"exchangeId"`
	StrategyID      string  `json:"strategyId"`
	Type            string  `json:"type"`        // percentage, fixed, threshold
	TriggerType     string  `json:"triggerType"` // auto, manual, scheduled
	Threshold       float64 `json:"threshold"`
	Amount          float64 `json:"amount"`
	Percentage      float64 `json:"percentage"`
	TargetAddress   string  `json:"targetAddress"`
	Currency        string  `json:"currency"`
	IsEnabled       bool    `json:"enabled"` // 前端使用 enabled
	LastTriggeredAt string  `json:"lastTriggeredAt,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	// 前端額外需要的字段
	TriggerAmount     float64 `json:"triggerAmount,omitempty"` // 触发金額（對应 Threshold）
	WithdrawRatio     float64 `json:"withdrawRatio,omitempty"` // 提取比例 0-1（對应 Percentage/100）
	Frequency         string  `json:"frequency,omitempty"`     // immediate, daily, weekly
	Destination       string  `json:"destination,omitempty"`   // account, wallet
	MinWithdrawAmount float64 `json:"minWithdrawAmount,omitempty"`
}

// WithdrawRecord 提取記錄
type WithdrawRecord struct {
	ID            string  `json:"id"`
	ExchangeID    string  `json:"exchangeId"`
	StrategyID    string  `json:"strategyId"`
	StrategyName  string  `json:"strategyName"`
	Amount        float64 `json:"amount"`
	Fee           float64 `json:"fee"`
	NetAmount     float64 `json:"netAmount"`
	Currency      string  `json:"currency"`
	Type          string  `json:"type"`        // auto, manual
	Status        string  `json:"status"`      // pending, processing, completed, failed, cancelled
	Destination   string  `json:"destination"` // account, wallet
	WalletAddress string  `json:"walletAddress,omitempty"`
	TargetAddress string  `json:"targetAddress"` // 兼容舊版
	TxHash        string  `json:"txHash,omitempty"`
	CreatedAt     string  `json:"createdAt"`
	CompletedAt   string  `json:"completedAt,omitempty"`
	FailedReason  string  `json:"failedReason,omitempty"`
	Note          string  `json:"note,omitempty"`
}

// ProfitTrendPoint 盈利趋势点
type ProfitTrendPoint struct {
	Timestamp string  `json:"timestamp"`
	Profit    float64 `json:"profit"`
	CumProfit float64 `json:"cumProfit"`
}

// FundingPaymentItem 資金費用記錄（API 返回）
type FundingPaymentItem struct {
	ID            int64   `json:"id"`
	Exchange      string  `json:"exchange"`
	Symbol        string  `json:"symbol"`
	IncomeType    string  `json:"incomeType"`
	Income        float64 `json:"income"` // 正=收入，負=支出
	Asset         string  `json:"asset"`
	TransactionID int64   `json:"transactionId"`
	TradeTime     string  `json:"tradeTime"`
	CreatedAt     string  `json:"createdAt"`
}

// 獲取盈利彙總
func getProfitSummaryHandler(c *gin.Context) {
	exchangeID := c.Query("exchange_id")

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()

	// 1. 獲取累计盈利
	summaryStats, err := st.GetStatisticsSummaryByExchange(exchangeID, accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢统计彙總失败: " + err.Error()})
		return
	}

	// 2. 獲取今日/本周/本月盈利（按配置時區）
	now := utils.NowConfiguredTimezone()
	// 今日开始
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, utils.GlobalLocation)
	// 本周开始（周一）
	offset := int(now.Weekday()) - 1
	if offset < 0 {
		offset = 6
	}
	weekStart := todayStart.AddDate(0, 0, -offset)
	// 本月开始
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, utils.GlobalLocation)

	dailyStats, err := st.QueryDailyStatisticsByExchange(exchangeID, accountID, monthStart, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢每日统计失败: " + err.Error()})
		return
	}

	todayProfit := 0.0
	weekProfit := 0.0
	monthProfit := 0.0

	for _, s := range dailyStats {
		monthProfit += s.TotalPnL
		if s.Date.After(todayStart) || s.Date.Equal(todayStart) {
			todayProfit += s.TotalPnL
		}
		if s.Date.After(weekStart) || s.Date.Equal(weekStart) {
			weekProfit += s.TotalPnL
		}
	}

	// 3. 獲取未實現盈利 (Unrealized Profit)
	unrealizedProfit := 0.0
	pmProvider := PickPositionProvider(c)
	priceProv := PickPriceProvider(c)

	if pmProvider != nil {
		slots := pmProvider.GetAllSlots()
		currentPrice := 0.0
		if priceProv != nil {
			currentPrice = priceProv.GetLastPrice()
		}

		for _, slot := range slots {
			// 如果指定了交易所且槽位不属於該交易所，跳過
			if exchangeID != "" && slot.Exchange != exchangeID {
				continue
			}

			if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
				price := slot.Price
				if currentPrice > 0 {
					unrealizedProfit += (currentPrice - price) * slot.PositionQty
				}
			}
		}
	}

	// 資金費用淨額（正=淨收入，負=淨支出）
	fundingSum := 0.0
	todayFunding := 0.0
	weekFunding := 0.0
	monthFunding := 0.0
	if stWithFunding, ok := st.(interface {
		GetFundingPaymentsSum(account, exchange string, startTime, endTime time.Time) (float64, error)
	}); ok {
		startAll := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		fundingSum, _ = stWithFunding.GetFundingPaymentsSum(accountID, exchangeID, startAll, now)
		todayFunding, _ = stWithFunding.GetFundingPaymentsSum(accountID, exchangeID, todayStart, now)
		weekFunding, _ = stWithFunding.GetFundingPaymentsSum(accountID, exchangeID, weekStart, now)
		monthFunding, _ = stWithFunding.GetFundingPaymentsSum(accountID, exchangeID, monthStart, now)
	}
	netWithFunding := summaryStats.TotalPnL + fundingSum
	todayProfitWithFunding := todayProfit + todayFunding
	weekProfitWithFunding := weekProfit + weekFunding
	monthProfitWithFunding := monthProfit + monthFunding

	// 🔥 计算价格偏差导致的损失
	// 买入价格偏差：如果实际买入价格高于委托价格，会导致成本增加（负值表示损失）
	// 卖出价格偏差：如果实际卖出价格低于委托价格，会导致收益减少（负值表示损失）
	// 总偏差损失 = 买入偏差（通常为负）+ 卖出偏差（通常为负）
	priceDeviationLoss := summaryStats.TotalBuyDeviation + summaryStats.TotalSellDeviation

	// 4. 計算交易所盈利（根據每筆訂單中交易所返回的 RealizedPnL）
	exchangeProfit := 0.0
	// 查詢所有已成交的訂單，累加 RealizedPnL
	allOrders, err := st.QueryOrdersWithTimeRange(10000, 0, "FILLED", nil, nil) // 查詢最多1萬筆已成交訂單
	if err == nil {
		for _, order := range allOrders {
			// 如果指定了交易所且訂單不屬於該交易所，跳過
			if exchangeID != "" && order.Exchange != exchangeID {
				continue
			}
			// 累加交易所返回的已實現盈虧
			if order.RealizedPnL != nil {
				exchangeProfit += *order.RealizedPnL
			}
		}
	}

	summary := ProfitSummary{
		ExchangeID:          exchangeID,
		TotalProfit:         math.Round(netWithFunding*100) / 100,
		GrossProfit:         math.Round(summaryStats.GrossPnL*100) / 100,
		TotalFee:            math.Round(summaryStats.TotalFee*100) / 100,
		FundingNet:          math.Round(fundingSum*100) / 100,
		TodayProfit:         math.Round(todayProfitWithFunding*100) / 100,
		WeekProfit:          math.Round(weekProfitWithFunding*100) / 100,
		MonthProfit:         math.Round(monthProfitWithFunding*100) / 100,
		UnrealizedProfit:    math.Round(unrealizedProfit*100) / 100,
		ExchangeProfit:      math.Round(exchangeProfit*100) / 100,
		WithdrawnProfit:     0, // TODO: 從提現記錄统计
		AvailableToWithdraw: math.Round(netWithFunding*100) / 100,
		PriceDeviationLoss:  math.Round(priceDeviationLoss*100) / 100,
		BuyPriceDeviation:   math.Round(summaryStats.TotalBuyDeviation*100) / 100,
		SellPriceDeviation:  math.Round(summaryStats.TotalSellDeviation*100) / 100,
		LastUpdated:         time.Now().Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"summary": summary,
	})
}

// 獲取資金費用明細
func getFundingHistoryHandler(c *gin.Context) {
	exchangeID := c.Query("exchange_id")
	startStr := c.Query("start_time")
	endStr := c.Query("end_time")

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "存儲服務未就绪", "records": []FundingPaymentItem{}})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "存儲接口未就绪", "records": []FundingPaymentItem{}})
		return
	}

	stWithFunding, ok := st.(interface {
		GetFundingPayments(account, exchange string, startTime, endTime time.Time) ([]*storage.FundingPayment, error)
	})
	if !ok {
		c.JSON(http.StatusOK, gin.H{"success": true, "records": []FundingPaymentItem{}})
		return
	}

	accountID := GetCurrentAccountID()
	now := utils.NowConfiguredTimezone()
	// 默認最近 30 天
	endTime := now
	startTime := now.AddDate(0, 0, -30)
	if endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			endTime = t
		}
	}
	if startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			startTime = t
		}
	}
	if startTime.After(endTime) {
		startTime, endTime = endTime, startTime
	}

	list, err := stWithFunding.GetFundingPayments(accountID, exchangeID, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢資金費用失敗: " + err.Error(), "records": []FundingPaymentItem{}})
		return
	}

	records := make([]FundingPaymentItem, 0, len(list))
	for _, p := range list {
		records = append(records, FundingPaymentItem{
			ID:            p.ID,
			Exchange:      p.Exchange,
			Symbol:        p.Symbol,
			IncomeType:    p.IncomeType,
			Income:        math.Round(p.Income*1e8) / 1e8,
			Asset:         p.Asset,
			TransactionID: p.TransactionID,
			TradeTime:     p.TradeTime.Format(time.RFC3339),
			CreatedAt:     p.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "records": records})
}

// 按策略獲取盈利
func getStrategyProfitsHandler(c *gin.Context) {
	exchangeID := c.Query("exchange_id")

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "profits": []StrategyProfit{}})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "profits": []StrategyProfit{}})
		return
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()

	// 查詢所有時间的盈亏（按币种和交易所分组）
	// 使用一個很早的時间作為起点
	startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := utils.NowConfiguredTimezone()

	pnlList, err := st.GetPnLByTimeRange(accountID, startTime, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢策略盈亏失败: " + err.Error()})
		return
	}

	// 獲取今日盈亏用於计算 TodayProfit（按配置時區）
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, utils.GlobalLocation)
	todayPnlList, _ := st.GetPnLByTimeRange(accountID, todayStart, now)
	todayPnlMap := make(map[string]float64)
	for _, p := range todayPnlList {
		key := p.Exchange + ":" + p.Symbol
		todayPnlMap[key] = p.TotalPnL
	}

	// 獲取未實現盈亏
	unrealizedPnlMap := make(map[string]float64)
	pmProvider := PickPositionProvider(c)
	priceProv := PickPriceProvider(c)
	if pmProvider != nil {
		slots := pmProvider.GetAllSlots()
		currentPrice := 0.0
		if priceProv != nil {
			currentPrice = priceProv.GetLastPrice()
		}

		for _, slot := range slots {
			if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
				key := slot.Exchange + ":" + slot.Symbol
				price := slot.Price
				if currentPrice > 0 {
					unrealizedPnlMap[key] += (currentPrice - price) * slot.PositionQty
				}
			}
		}
	}

	profits := make([]StrategyProfit, 0)
	for _, p := range pnlList {
		// 如果指定了交易所且不匹配，跳過
		if exchangeID != "" && p.Exchange != exchangeID {
			continue
		}

		key := p.Exchange + ":" + p.Symbol

		// 暂時將 symbol 作為 strategyId
		strategyID := strings.ToLower(p.Symbol)
		if strings.Contains(strategyID, "usdt") {
			strategyID = strings.ReplaceAll(strategyID, "usdt", "")
		}

		profits = append(profits, StrategyProfit{
			ExchangeID:          p.Exchange,
			StrategyID:          p.Symbol, // 使用 Symbol 作為唯一標识
			StrategyName:        p.Symbol + " 策略",
			StrategyType:        "grid", // 默认為网格，實際应從配置獲取
			TotalProfit:         math.Round(p.TotalPnL*100) / 100,
			TodayProfit:         math.Round(todayPnlMap[key]*100) / 100,
			UnrealizedProfit:    math.Round(unrealizedPnlMap[key]*100) / 100,
			RealizedProfit:      math.Round(p.TotalPnL*100) / 100,
			WithdrawnProfit:     0,
			AvailableToWithdraw: math.Round(p.TotalPnL*100) / 100,
			TradeCount:          p.TotalTrades,
			WinRate:             math.Round(p.WinRate*100) / 100, // 保持小數形式（0-1），前端會轉换為百分比
			AvgProfitPerTrade:   0,                               // 可计算
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"profits": profits,
	})
}

// 獲取單個策略盈利详情
func getStrategyProfitDetailHandler(c *gin.Context) {
	strategyID := c.Param("id") // 實際上这里傳的是 Symbol

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	// 查詢該幣種的所有盈亏
	startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Now()
	accountID := GetCurrentAccountID()
	summary, err := st.GetPnLBySymbol(strategyID, accountID, startTime, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢策略盈亏详情失败: " + err.Error()})
		return
	}

	// 獲取未實現盈亏
	unrealizedPnL := 0.0
	pmProvider := PickPositionProvider(c)
	priceProv := PickPriceProvider(c)
	if pmProvider != nil {
		slots := pmProvider.GetAllSlots()
		currentPrice := 0.0
		if priceProv != nil {
			currentPrice = priceProv.GetLastPrice()
		}

		for _, slot := range slots {
			if slot.Symbol == strategyID && slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
				if currentPrice > 0 {
					unrealizedPnL += (currentPrice - slot.Price) * slot.PositionQty
				}
			}
		}
	}

	profit := StrategyProfit{
		StrategyID:          strategyID,
		StrategyName:        strategyID + " 策略",
		StrategyType:        "grid",
		TotalProfit:         math.Round(summary.TotalPnL*100) / 100,
		TodayProfit:         0, // 需要額外查詢
		UnrealizedProfit:    math.Round(unrealizedPnL*100) / 100,
		RealizedProfit:      math.Round(summary.TotalPnL*100) / 100,
		WithdrawnProfit:     0,
		AvailableToWithdraw: math.Round(summary.TotalPnL*100) / 100,
		WinRate:             math.Round(summary.WinRate*100) / 100, // 保持小數形式（0-1），前端會轉换為百分比
		TradeCount:          summary.TotalTrades,
		AvgProfitPerTrade:   0,
		LastTradeAt:         now.Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"profit":  profit,
	})
}

// 獲取提取规则（從數據库读取，空库時返回空數组）
func getWithdrawRulesHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "rules": []ProfitWithdrawRule{}})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "rules": []ProfitWithdrawRule{}})
		return
	}

	accountID := GetCurrentAccountID()
	dbRules, err := st.ListProfitWithdrawRules(accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢提取规则失败: " + err.Error()})
		return
	}

	// 轉换為 API 响应格式
	rules := make([]ProfitWithdrawRule, 0, len(dbRules))
	for _, r := range dbRules {
		rules = append(rules, ProfitWithdrawRule{
			ID:                r.ID,
			ExchangeID:        r.ExchangeID,
			StrategyID:        r.StrategyID,
			IsEnabled:         r.Enabled,
			TriggerAmount:     r.TriggerAmount,
			WithdrawRatio:     r.WithdrawRatio,
			Frequency:         r.Frequency,
			Destination:       r.Destination,
			MinWithdrawAmount: r.MinWithdrawAmount,
			TargetAddress:     r.WalletAddress,
			CreatedAt:         r.CreatedAt.Format(time.RFC3339),
			UpdatedAt:         r.UpdatedAt.Format(time.RFC3339),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"rules":   rules,
	})
}

// 更新提取规则（全量替换：先刪除账戶下所有规则，再插入新规则）
func updateWithdrawRulesHandler(c *gin.Context) {
	var req struct {
		Rules []ProfitWithdrawRule `json:"rules"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲服務未就绪（storageProv=nil）"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲接口未就绪（GetStorage()=nil），请检查 storage.enabled 配置和數據库初始化日志"})
		return
	}

	accountID := GetCurrentAccountID()

	// 構建新规则列表
	now := time.Now()
	newRules := make([]*storage.ProfitWithdrawRule, 0, len(req.Rules))
	for _, r := range req.Rules {
		rule := &storage.ProfitWithdrawRule{
			ID:                r.ID,
			AccountID:         accountID,
			ExchangeID:        r.ExchangeID,
			StrategyID:        r.StrategyID,
			Enabled:           r.IsEnabled,
			TriggerAmount:     r.TriggerAmount,
			WithdrawRatio:     r.WithdrawRatio,
			Frequency:         r.Frequency,
			Destination:       r.Destination,
			WalletAddress:     r.TargetAddress,
			MinWithdrawAmount: r.MinWithdrawAmount,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		// 如果 ID 為空或以 temp- 开头，生成新 ID
		if rule.ID == "" || strings.HasPrefix(rule.ID, "temp-") {
			rule.ID = fmt.Sprintf("rule_%s_%d", accountID, now.UnixNano())
			now = now.Add(time.Nanosecond) // 确保唯一
		}
		newRules = append(newRules, rule)
	}

	// 全量替换规则
	if err := st.ReplaceProfitWithdrawRules(accountID, newRules); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存规则失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取规则已更新",
		"rules":   req.Rules,
	})
}

// 創建或更新單個提取规则
func upsertWithdrawRuleHandler(c *gin.Context) {
	var req ProfitWithdrawRule
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	accountID := GetCurrentAccountID()
	now := time.Now()

	rule := &storage.ProfitWithdrawRule{
		ID:                req.ID,
		AccountID:         accountID,
		ExchangeID:        req.ExchangeID,
		StrategyID:        req.StrategyID,
		Enabled:           req.IsEnabled,
		TriggerAmount:     req.TriggerAmount,
		WithdrawRatio:     req.WithdrawRatio,
		Frequency:         req.Frequency,
		Destination:       req.Destination,
		WalletAddress:     req.TargetAddress,
		MinWithdrawAmount: req.MinWithdrawAmount,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// 如果没有 ID，生成一個新的
	if rule.ID == "" || strings.HasPrefix(rule.ID, "temp-") {
		rule.ID = fmt.Sprintf("rule_%s_%d", accountID, now.UnixNano())
	}

	if err := st.UpsertProfitWithdrawRule(accountID, rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存规则失败: " + err.Error()})
		return
	}

	req.ID = rule.ID
	req.CreatedAt = now.Format(time.RFC3339)
	req.UpdatedAt = now.Format(time.RFC3339)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取规则已保存",
		"rule":    req,
	})
}

// 刪除提取规则
func deleteWithdrawRuleHandler(c *gin.Context) {
	ruleID := c.Param("id")

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	accountID := GetCurrentAccountID()
	if err := st.DeleteProfitWithdrawRule(accountID, ruleID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "刪除规则失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取规则已刪除",
		"ruleId":  ruleID,
	})
}

// 手动提取
func withdrawProfitHandler(c *gin.Context) {
	var req struct {
		ExchangeID    string  `json:"exchangeId"`
		StrategyID    string  `json:"strategyId"`
		Amount        float64 `json:"amount"`
		TargetAddress string  `json:"targetAddress"`
		Currency      string  `json:"currency"`
		Note          string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}
	if req.ExchangeID == "" || req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请提供交易所 ID 和有效提取金額",
		})
		return
	}
	currency := req.Currency
	if currency == "" {
		currency = "USDT"
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	if exchangeGetterFunc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "交易所獲取器未配置"})
		return
	}
	ex := exchangeGetterFunc(req.ExchangeID)
	if ex == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "未找到該交易所實例"})
		return
	}

	// 內部轉帳通常無手续费
	fee := 0.0
	netAmount := req.Amount
	recordID := "wd_" + time.Now().Format("20060102150405")
	accountID := GetCurrentAccountID()

	record := &storage.ProfitWithdrawRecord{
		ID:          recordID,
		RuleID:      "",
		AccountID:   accountID,
		ExchangeID:  req.ExchangeID,
		StrategyID:  req.StrategyID,
		Amount:      req.Amount,
		Fee:         fee,
		NetAmount:   netAmount,
		Currency:    currency,
		Type:        "manual",
		Status:      "pending",
		Destination: "account",
		CreatedAt:   time.Now(),
		Note:        req.Note,
	}
	if err := st.SaveWithdrawRecord(record); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存提取記錄失败: " + err.Error()})
		return
	}

	ctx := c.Request.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	transferID, err := ex.InternalTransfer(ctx, "UMFUTURE", "SPOT", currency, req.Amount)
	if err != nil {
		_ = st.UpdateWithdrawRecordStatus(recordID, "failed", "", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "內部轉帳失败: " + err.Error()})
		return
	}

	_ = st.UpdateWithdrawRecordStatus(recordID, "completed", transferID, "")
	completedAt := time.Now()

	respRecord := WithdrawRecord{
		ID:            recordID,
		ExchangeID:    req.ExchangeID,
		StrategyID:    req.StrategyID,
		StrategyName:  getStrategyName(req.StrategyID),
		Amount:        req.Amount,
		Fee:           fee,
		NetAmount:     netAmount,
		Currency:      currency,
		Type:          "manual",
		Status:        "completed",
		Destination:   "account",
		TargetAddress: req.TargetAddress,
		CreatedAt:     record.CreatedAt.Format(time.RFC3339),
		CompletedAt:   completedAt.Format(time.RFC3339),
		Note:          req.Note,
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取已完成",
		"record":  respRecord,
	})
}

// 獲取提取历史
func getWithdrawHistoryHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "records": []WithdrawRecord{}, "total": 0})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "records": []WithdrawRecord{}, "total": 0})
		return
	}

	accountID := GetCurrentAccountID()
	limit := 100
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	dbRecords, err := st.GetWithdrawRecords(accountID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢提取历史失败: " + err.Error()})
		return
	}

	records := make([]WithdrawRecord, 0, len(dbRecords))
	for _, r := range dbRecords {
		rec := WithdrawRecord{
			ID:            r.ID,
			ExchangeID:    r.ExchangeID,
			StrategyID:    r.StrategyID,
			StrategyName:  getStrategyName(r.StrategyID),
			Amount:        r.Amount,
			Fee:           r.Fee,
			NetAmount:     r.NetAmount,
			Currency:      r.Currency,
			Type:          r.Type,
			Status:        r.Status,
			Destination:   r.Destination,
			TargetAddress: "",
			TxHash:        r.TransferID,
			CreatedAt:     r.CreatedAt.Format(time.RFC3339),
			FailedReason:  r.FailedReason,
			Note:          r.Note,
		}
		if r.CompletedAt != nil {
			rec.CompletedAt = r.CompletedAt.Format(time.RFC3339)
		}
		records = append(records, rec)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"records": records,
		"total":   len(records),
	})
}

// 獲取盈利趋势
func getProfitTrendHandler(c *gin.Context) {
	period := c.DefaultQuery("period", "30d")
	exchangeID := c.Query("exchange_id")
	// strategyID := c.Query("strategy_id") // 暂時不支援按策略過滤

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "trend": []ProfitTrendPoint{}})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "trend": []ProfitTrendPoint{}})
		return
	}

	// 根據周期计算天數
	days := 30
	switch period {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	case "1y":
		days = 365
	}

	now := utils.NowConfiguredTimezone()
	startDate := now.AddDate(0, 0, -days)
	accountID := GetCurrentAccountID()

	dailyStats, err := st.QueryDailyStatisticsByExchange(exchangeID, accountID, startDate, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢每日统计失败: " + err.Error()})
		return
	}

	// 獲取起始之前的累计盈利作為 base
	allStatsBefore, _ := st.QueryDailyStatisticsByExchange(exchangeID, accountID, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), startDate.AddDate(0, 0, -1))
	baseProfit := 0.0
	for _, s := range allStatsBefore {
		baseProfit += s.TotalPnL
	}

	// 將結果按日期填充，缺失的日期补0
	trendMap := make(map[string]float64)
	for _, s := range dailyStats {
		trendMap[s.Date.Format("2006-01-02")] = s.TotalPnL
	}

	trend := make([]ProfitTrendPoint, days+1)
	cumProfit := baseProfit
	for i := 0; i <= days; i++ {
		date := startDate.AddDate(0, 0, i)
		dateStr := date.Format("2006-01-02")
		dailyProfit := trendMap[dateStr]
		cumProfit += dailyProfit

		trend[i] = ProfitTrendPoint{
			Timestamp: dateStr,
			Profit:    math.Round(dailyProfit*100) / 100,
			CumProfit: math.Round(cumProfit*100) / 100,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"trend":   trend,
		"period":  period,
	})
}

// 取消提取
func cancelWithdrawHandler(c *gin.Context) {
	withdrawID := c.Param("id")

	// TODO: 實際取消逻辑

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取已取消",
		"id":      withdrawID,
	})
}

// 獲取提取详情
func getWithdrawDetailHandler(c *gin.Context) {
	withdrawID := c.Param("id")

	record := WithdrawRecord{
		ID:            withdrawID,
		StrategyID:    "grid",
		StrategyName:  "网格交易策略",
		Amount:        1000,
		Fee:           1.0,
		NetAmount:     999,
		Currency:      "USDT",
		Type:          "manual",
		Status:        "completed",
		TargetAddress: "0x1234...5678",
		TxHash:        "0xabcd...ef12",
		CreatedAt:     time.Now().AddDate(0, 0, -1).Format(time.RFC3339),
		CompletedAt:   time.Now().AddDate(0, 0, -1).Add(30 * time.Minute).Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"record":  record,
	})
}

// 估算提取费用
func estimateWithdrawFeeHandler(c *gin.Context) {
	var req struct {
		StrategyID    string  `json:"strategyId"`
		Amount        float64 `json:"amount"`
		TargetAddress string  `json:"targetAddress"`
		Currency      string  `json:"currency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}

	// 计算手续费（示例逻辑）
	var fee float64
	switch req.Currency {
	case "USDT":
		fee = 1.0 // TRC20 USDT 固定 1 USDT
	case "ETH":
		fee = req.Amount * 0.005 // 0.5% 手续费
	case "BTC":
		fee = req.Amount * 0.001 // 0.1% 手续费
	default:
		fee = req.Amount * 0.001
	}

	netAmount := req.Amount - fee

	// 預计到账時间
	estimatedArrival := time.Now().Add(30 * time.Minute).Format(time.RFC3339)

	c.JSON(http.StatusOK, gin.H{
		"success":          true,
		"fee":              math.Round(fee*100) / 100,
		"netAmount":        math.Round(netAmount*100) / 100,
		"estimatedArrival": estimatedArrival,
	})
}
