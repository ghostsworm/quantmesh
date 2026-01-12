package web

import (
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ProfitSummary 盈利汇总
type ProfitSummary struct {
	ExchangeID          string  `json:"exchangeId,omitempty"`
	TotalProfit         float64 `json:"totalProfit"`
	TodayProfit         float64 `json:"todayProfit"`
	WeekProfit          float64 `json:"weekProfit"`
	MonthProfit         float64 `json:"monthProfit"`
	UnrealizedProfit    float64 `json:"unrealizedProfit"`
	WithdrawnProfit     float64 `json:"withdrawnProfit"`
	AvailableToWithdraw float64 `json:"availableToWithdraw"`
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
	IsEnabled       bool    `json:"isEnabled"`
	LastTriggeredAt string  `json:"lastTriggeredAt,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
}

// WithdrawRecord 提取记录
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
	TargetAddress string  `json:"targetAddress"` // 兼容旧版
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

// 获取盈利汇总
func getProfitSummaryHandler(c *gin.Context) {
	exchangeID := c.Query("exchange_id")

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "存储服务未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "存储接口未就绪"})
		return
	}

	// 1. 获取累计盈利
	summaryStats, err := st.GetStatisticsSummaryByExchange(exchangeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询统计汇总失败: " + err.Error()})
		return
	}

	// 2. 获取今日/本周/本月盈利
	now := time.Now()
	// 今日开始
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// 本周开始（周一）
	offset := int(now.Weekday()) - 1
	if offset < 0 {
		offset = 6
	}
	weekStart := todayStart.AddDate(0, 0, -offset)
	// 本月开始
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	dailyStats, err := st.QueryDailyStatisticsByExchange(exchangeID, monthStart, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询每日统计失败: " + err.Error()})
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

	// 3. 获取未实现盈利 (Unrealized Profit)
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
			// 如果指定了交易所且槽位不属于该交易所，跳过
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

	summary := ProfitSummary{
		ExchangeID:          exchangeID,
		TotalProfit:         math.Round(summaryStats.TotalPnL*100) / 100,
		TodayProfit:         math.Round(todayProfit*100) / 100,
		WeekProfit:          math.Round(weekProfit*100) / 100,
		MonthProfit:         math.Round(monthProfit*100) / 100,
		UnrealizedProfit:    math.Round(unrealizedProfit*100) / 100,
		WithdrawnProfit:     0, // TODO: 从提现记录统计
		AvailableToWithdraw: math.Round(summaryStats.TotalPnL*100) / 100,
		LastUpdated:         time.Now().Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"summary": summary,
	})
}

// 按策略获取盈利
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

	// 查询所有时间的盈亏（按币种和交易所分组）
	// 使用一个很早的时间作为起点
	startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Now()

	pnlList, err := st.GetPnLByTimeRange(startTime, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询策略盈亏失败: " + err.Error()})
		return
	}

	// 获取今日盈亏用于计算 TodayProfit
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayPnlList, _ := st.GetPnLByTimeRange(todayStart, now)
	todayPnlMap := make(map[string]float64)
	for _, p := range todayPnlList {
		key := p.Exchange + ":" + p.Symbol
		todayPnlMap[key] = p.TotalPnL
	}

	// 获取未实现盈亏
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
		// 如果指定了交易所且不匹配，跳过
		if exchangeID != "" && p.Exchange != exchangeID {
			continue
		}

		key := p.Exchange + ":" + p.Symbol
		
		// 暂时将 symbol 作为 strategyId
		strategyID := strings.ToLower(p.Symbol)
		if strings.Contains(strategyID, "usdt") {
			strategyID = strings.ReplaceAll(strategyID, "usdt", "")
		}

		profits = append(profits, StrategyProfit{
			ExchangeID:          p.Exchange,
			StrategyID:          p.Symbol, // 使用 Symbol 作为唯一标识
			StrategyName:        p.Symbol + " 策略",
			StrategyType:        "grid", // 默认为网格，实际应从配置获取
			TotalProfit:         math.Round(p.TotalPnL*100) / 100,
			TodayProfit:         math.Round(todayPnlMap[key]*100) / 100,
			UnrealizedProfit:    math.Round(unrealizedPnlMap[key]*100) / 100,
			RealizedProfit:      math.Round(p.TotalPnL*100) / 100,
			WithdrawnProfit:     0,
			AvailableToWithdraw: math.Round(p.TotalPnL*100) / 100,
			TradeCount:          p.TotalTrades,
			WinRate:             math.Round(p.WinRate*10000) / 100, // 转换为百分比
			AvgProfitPerTrade:   0,                                 // 可计算
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"profits": profits,
	})
}

// 获取单个策略盈利详情
func getStrategyProfitDetailHandler(c *gin.Context) {
	strategyID := c.Param("id") // 实际上这里传的是 Symbol

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "存储服务未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "存储接口未就绪"})
		return
	}

	// 查询该币种的所有盈亏
	startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Now()
	summary, err := st.GetPnLBySymbol(strategyID, startTime, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询策略盈亏详情失败: " + err.Error()})
		return
	}

	// 获取未实现盈亏
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
		TodayProfit:         0, // 需要额外查询
		UnrealizedProfit:    math.Round(unrealizedPnL*100) / 100,
		RealizedProfit:      math.Round(summary.TotalPnL*100) / 100,
		WithdrawnProfit:     0,
		AvailableToWithdraw: math.Round(summary.TotalPnL*100) / 100,
		WinRate:             math.Round(summary.WinRate*10000) / 100,
		TradeCount:          summary.TotalTrades,
		AvgProfitPerTrade:   0,
		LastTradeAt:         now.Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"profit":  profit,
	})
}

// 获取提取规则
func getWithdrawRulesHandler(c *gin.Context) {
	rules := []ProfitWithdrawRule{
		{
			ID:              "rule_001",
			StrategyID:      "grid",
			Type:            "percentage",
			TriggerType:     "auto",
			Threshold:       1000,
			Percentage:      50,
			TargetAddress:   "0x1234...5678",
			Currency:        "USDT",
			IsEnabled:       true,
			LastTriggeredAt: time.Now().AddDate(0, 0, -3).Format(time.RFC3339),
			CreatedAt:       time.Now().AddDate(0, -1, 0).Format(time.RFC3339),
			UpdatedAt:       time.Now().Format(time.RFC3339),
		},
		{
			ID:              "rule_002",
			StrategyID:      "",
			Type:            "threshold",
			TriggerType:     "scheduled",
			Threshold:       5000,
			Amount:          2000,
			TargetAddress:   "0x1234...5678",
			Currency:        "USDT",
			IsEnabled:       true,
			CreatedAt:       time.Now().AddDate(0, -2, 0).Format(time.RFC3339),
			UpdatedAt:       time.Now().Format(time.RFC3339),
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"rules":   rules,
	})
}

// 更新提取规则
func updateWithdrawRulesHandler(c *gin.Context) {
	var req struct {
		Rules []ProfitWithdrawRule `json:"rules"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的请求数据: " + err.Error(),
		})
		return
	}

	// TODO: 保存规则到数据库

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取规则已更新",
		"rules":   req.Rules,
	})
}

// 创建或更新单个提取规则
func upsertWithdrawRuleHandler(c *gin.Context) {
	var rule ProfitWithdrawRule
	if err := c.ShouldBindJSON(&rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的请求数据: " + err.Error(),
		})
		return
	}

	// 如果没有 ID，生成一个新的
	if rule.ID == "" {
		rule.ID = "rule_" + time.Now().Format("20060102150405")
	}
	rule.CreatedAt = time.Now().Format(time.RFC3339)
	rule.UpdatedAt = time.Now().Format(time.RFC3339)

	// TODO: 保存到数据库

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取规则已保存",
		"rule":    rule,
	})
}

// 删除提取规则
func deleteWithdrawRuleHandler(c *gin.Context) {
	ruleID := c.Param("id")

	// TODO: 从数据库删除

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取规则已删除",
		"ruleId":  ruleID,
	})
}

// 手动提取
func withdrawProfitHandler(c *gin.Context) {
	var req struct {
		StrategyID    string  `json:"strategyId"`
		Amount        float64 `json:"amount"`
		TargetAddress string  `json:"targetAddress"`
		Currency      string  `json:"currency"`
		Note          string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的请求数据: " + err.Error(),
		})
		return
	}

	// 计算手续费（示例：0.1%）
	fee := req.Amount * 0.001
	netAmount := req.Amount - fee

	record := WithdrawRecord{
		ID:            "wd_" + time.Now().Format("20060102150405"),
		StrategyID:    req.StrategyID,
		StrategyName:  getStrategyName(req.StrategyID),
		Amount:        req.Amount,
		Fee:           fee,
		NetAmount:     netAmount,
		Currency:      req.Currency,
		Type:          "manual",
		Status:        "pending",
		TargetAddress: req.TargetAddress,
		CreatedAt:     time.Now().Format(time.RFC3339),
		Note:          req.Note,
	}

	// TODO: 保存到数据库并触发提取流程

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取请求已提交",
		"record":  record,
	})
}

// 获取提取历史
func getWithdrawHistoryHandler(c *gin.Context) {
	// 目前系统中还没有持久化存储提取历史的表
	// 为了移除假数据，我们暂时返回空列表，并预留 TODO
	records := make([]WithdrawRecord, 0)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"records": records,
		"total":   0,
	})
}

// 获取盈利趋势
func getProfitTrendHandler(c *gin.Context) {
	period := c.DefaultQuery("period", "30d")
	exchangeID := c.Query("exchange_id")
	// strategyID := c.Query("strategy_id") // 暂时不支持按策略过滤

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

	// 根据周期计算天数
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

	now := time.Now()
	startDate := now.AddDate(0, 0, -days)

	dailyStats, err := st.QueryDailyStatisticsByExchange(exchangeID, startDate, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询每日统计失败: " + err.Error()})
		return
	}

	// 获取起始之前的累计盈利作为 base
	allStatsBefore, _ := st.QueryDailyStatisticsByExchange(exchangeID, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), startDate.AddDate(0, 0, -1))
	baseProfit := 0.0
	for _, s := range allStatsBefore {
		baseProfit += s.TotalPnL
	}

	// 将结果按日期填充，缺失的日期补0
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

	// TODO: 实际取消逻辑

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取已取消",
		"id":      withdrawID,
	})
}

// 获取提取详情
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
			"message": "无效的请求数据: " + err.Error(),
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

	// 预计到账时间
	estimatedArrival := time.Now().Add(30 * time.Minute).Format(time.RFC3339)

	c.JSON(http.StatusOK, gin.H{
		"success":          true,
		"fee":              math.Round(fee*100) / 100,
		"netAmount":        math.Round(netAmount*100) / 100,
		"estimatedArrival": estimatedArrival,
	})
}
