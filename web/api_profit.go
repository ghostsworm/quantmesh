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
	IsEnabled       bool    `json:"enabled"` // 前端使用 enabled
	LastTriggeredAt string  `json:"lastTriggeredAt,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	// 前端额外需要的字段
	TriggerAmount     float64 `json:"triggerAmount,omitempty"` // 触发金额（对应 Threshold）
	WithdrawRatio     float64 `json:"withdrawRatio,omitempty"` // 提取比例 0-1（对应 Percentage/100）
	Frequency         string  `json:"frequency,omitempty"`     // immediate, daily, weekly
	Destination       string  `json:"destination,omitempty"`   // account, wallet
	MinWithdrawAmount float64 `json:"minWithdrawAmount,omitempty"`
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

	// 获取当前账户标识
	accountID := GetCurrentAccountID()

	// 1. 获取累计盈利
	summaryStats, err := st.GetStatisticsSummaryByExchange(exchangeID, accountID)
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

	dailyStats, err := st.QueryDailyStatisticsByExchange(exchangeID, accountID, monthStart, now)
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

	// 获取当前账户标识
	accountID := GetCurrentAccountID()

	// 查询所有时间的盈亏（按币种和交易所分组）
	// 使用一个很早的时间作为起点
	startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Now()

	pnlList, err := st.GetPnLByTimeRange(accountID, startTime, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询策略盈亏失败: " + err.Error()})
		return
	}

	// 获取今日盈亏用于计算 TodayProfit
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayPnlList, _ := st.GetPnLByTimeRange(accountID, todayStart, now)
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
			WinRate:             math.Round(p.WinRate*100) / 100, // 保持小数形式（0-1），前端会转换为百分比
			AvgProfitPerTrade:   0,                               // 可计算
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
	accountID := GetCurrentAccountID()
	summary, err := st.GetPnLBySymbol(strategyID, accountID, startTime, now)
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
		WinRate:             math.Round(summary.WinRate*100) / 100, // 保持小数形式（0-1），前端会转换为百分比
		TradeCount:          summary.TotalTrades,
		AvgProfitPerTrade:   0,
		LastTradeAt:         now.Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"profit":  profit,
	})
}

// 获取提取规则（从数据库读取，空库时返回空数组）
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
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询提取规则失败: " + err.Error()})
		return
	}

	// 转换为 API 响应格式
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

// 更新提取规则（全量替换：先删除账户下所有规则，再插入新规则）
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

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存储服务未就绪（storageProv=nil）"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存储接口未就绪（GetStorage()=nil），请检查 storage.enabled 配置和数据库初始化日志"})
		return
	}

	accountID := GetCurrentAccountID()

	// 构建新规则列表
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
		// 如果 ID 为空或以 temp- 开头，生成新 ID
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

// 创建或更新单个提取规则
func upsertWithdrawRuleHandler(c *gin.Context) {
	var req ProfitWithdrawRule
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的请求数据: " + err.Error(),
		})
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存储服务未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存储接口未就绪"})
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

	// 如果没有 ID，生成一个新的
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

// 删除提取规则
func deleteWithdrawRuleHandler(c *gin.Context) {
	ruleID := c.Param("id")

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存储服务未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存储接口未就绪"})
		return
	}

	accountID := GetCurrentAccountID()
	if err := st.DeleteProfitWithdrawRule(accountID, ruleID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "删除规则失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取规则已删除",
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
			"message": "无效的请求数据: " + err.Error(),
		})
		return
	}
	if req.ExchangeID == "" || req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请提供交易所 ID 和有效提取金额",
		})
		return
	}
	currency := req.Currency
	if currency == "" {
		currency = "USDT"
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存储服务未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存储接口未就绪"})
		return
	}

	if exchangeGetterFunc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "交易所获取器未配置"})
		return
	}
	ex := exchangeGetterFunc(req.ExchangeID)
	if ex == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "未找到该交易所实例"})
		return
	}

	// 内部转账通常无手续费
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
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存提取记录失败: " + err.Error()})
		return
	}

	ctx := c.Request.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	transferID, err := ex.InternalTransfer(ctx, "UMFUTURE", "SPOT", currency, req.Amount)
	if err != nil {
		_ = st.UpdateWithdrawRecordStatus(recordID, "failed", "", err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "内部转账失败: " + err.Error()})
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

// 获取提取历史
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
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询提取历史失败: " + err.Error()})
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
	accountID := GetCurrentAccountID()

	dailyStats, err := st.QueryDailyStatisticsByExchange(exchangeID, accountID, startDate, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查询每日统计失败: " + err.Error()})
		return
	}

	// 获取起始之前的累计盈利作为 base
	allStatsBefore, _ := st.QueryDailyStatisticsByExchange(exchangeID, accountID, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), startDate.AddDate(0, 0, -1))
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
