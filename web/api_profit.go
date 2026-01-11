package web

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// ProfitSummary 盈利汇总
type ProfitSummary struct {
	TotalProfit       float64 `json:"totalProfit"`
	TodayProfit       float64 `json:"todayProfit"`
	WeekProfit        float64 `json:"weekProfit"`
	MonthProfit       float64 `json:"monthProfit"`
	AvailableWithdraw float64 `json:"availableWithdraw"`
	PendingWithdraw   float64 `json:"pendingWithdraw"`
	TotalWithdrawn    float64 `json:"totalWithdrawn"`
	UnrealizedProfit  float64 `json:"unrealizedProfit"`
	RealizedProfit    float64 `json:"realizedProfit"`
}

// StrategyProfit 策略盈利
type StrategyProfit struct {
	StrategyID       string  `json:"strategyId"`
	StrategyName     string  `json:"strategyName"`
	TotalProfit      float64 `json:"totalProfit"`
	TodayProfit      float64 `json:"todayProfit"`
	WeekProfit       float64 `json:"weekProfit"`
	MonthProfit      float64 `json:"monthProfit"`
	WinRate          float64 `json:"winRate"`
	TotalTrades      int     `json:"totalTrades"`
	AvgProfitPerTrade float64 `json:"avgProfitPerTrade"`
	MaxDrawdown      float64 `json:"maxDrawdown"`
}

// ProfitWithdrawRule 提取规则
type ProfitWithdrawRule struct {
	ID              string  `json:"id"`
	StrategyID      string  `json:"strategyId"`
	Type            string  `json:"type"` // percentage, fixed, threshold
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
	StrategyID    string  `json:"strategyId"`
	StrategyName  string  `json:"strategyName"`
	Amount        float64 `json:"amount"`
	Fee           float64 `json:"fee"`
	NetAmount     float64 `json:"netAmount"`
	Currency      string  `json:"currency"`
	Type          string  `json:"type"` // auto, manual
	Status        string  `json:"status"` // pending, processing, completed, failed
	TargetAddress string  `json:"targetAddress"`
	TxHash        string  `json:"txHash,omitempty"`
	CreatedAt     string  `json:"createdAt"`
	CompletedAt   string  `json:"completedAt,omitempty"`
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
	summary := ProfitSummary{
		TotalProfit:       15823.45,
		TodayProfit:       523.78,
		WeekProfit:        2156.32,
		MonthProfit:       6890.21,
		AvailableWithdraw: 12500.00,
		PendingWithdraw:   500.00,
		TotalWithdrawn:    3500.00,
		UnrealizedProfit:  823.45,
		RealizedProfit:    15000.00,
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"summary": summary,
	})
}

// 按策略获取盈利
func getStrategyProfitsHandler(c *gin.Context) {
	profits := []StrategyProfit{
		{
			StrategyID:        "grid",
			StrategyName:      "网格交易策略",
			TotalProfit:       8523.45,
			TodayProfit:       325.78,
			WeekProfit:        1256.32,
			MonthProfit:       3890.21,
			WinRate:           72.5,
			TotalTrades:       1523,
			AvgProfitPerTrade: 5.6,
			MaxDrawdown:       8.5,
		},
		{
			StrategyID:        "dca",
			StrategyName:      "DCA 定投策略",
			TotalProfit:       7300.00,
			TodayProfit:       198.00,
			WeekProfit:        900.00,
			MonthProfit:       3000.00,
			WinRate:           85.2,
			TotalTrades:       245,
			AvgProfitPerTrade: 29.8,
			MaxDrawdown:       5.2,
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"profits": profits,
	})
}

// 获取单个策略盈利详情
func getStrategyProfitDetailHandler(c *gin.Context) {
	strategyID := c.Param("id")

	profit := StrategyProfit{
		StrategyID:        strategyID,
		StrategyName:      getStrategyName(strategyID),
		TotalProfit:       8523.45,
		TodayProfit:       325.78,
		WeekProfit:        1256.32,
		MonthProfit:       3890.21,
		WinRate:           72.5,
		TotalTrades:       1523,
		AvgProfitPerTrade: 5.6,
		MaxDrawdown:       8.5,
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
	limitStr := c.DefaultQuery("limit", "10")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	// 模拟历史记录
	records := []WithdrawRecord{
		{
			ID:            "wd_20260110120000",
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
		},
		{
			ID:            "wd_20260105150000",
			StrategyID:    "dca",
			StrategyName:  "DCA 定投策略",
			Amount:        500,
			Fee:           0.5,
			NetAmount:     499.5,
			Currency:      "USDT",
			Type:          "auto",
			Status:        "completed",
			TargetAddress: "0x1234...5678",
			TxHash:        "0x5678...9abc",
			CreatedAt:     time.Now().AddDate(0, 0, -6).Format(time.RFC3339),
			CompletedAt:   time.Now().AddDate(0, 0, -6).Add(45 * time.Minute).Format(time.RFC3339),
		},
		{
			ID:            "wd_20260101100000",
			StrategyID:    "",
			StrategyName:  "全部策略",
			Amount:        2000,
			Fee:           2.0,
			NetAmount:     1998,
			Currency:      "USDT",
			Type:          "manual",
			Status:        "completed",
			TargetAddress: "0x1234...5678",
			TxHash:        "0xdef0...1234",
			CreatedAt:     time.Now().AddDate(0, 0, -10).Format(time.RFC3339),
			CompletedAt:   time.Now().AddDate(0, 0, -10).Add(1 * time.Hour).Format(time.RFC3339),
		},
	}

	// 限制返回数量
	if len(records) > limit {
		records = records[:limit]
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"records": records,
		"total":   len(records),
		"limit":   limit,
	})
}

// 获取盈利趋势
func getProfitTrendHandler(c *gin.Context) {
	period := c.DefaultQuery("period", "30d")
	strategyID := c.Query("strategy_id")

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

	// 生成模拟趋势数据
	trend := make([]ProfitTrendPoint, days)
	cumProfit := 0.0
	baseProfit := 15000.0

	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -days+i+1)
		// 模拟每日盈利（有波动）
		dailyProfit := 100 + 50*math.Sin(float64(i)*0.3) + float64(i%7)*20
		if strategyID != "" {
			dailyProfit *= 0.5 // 单策略盈利较少
		}
		cumProfit += dailyProfit

		trend[i] = ProfitTrendPoint{
			Timestamp: date.Format("2006-01-02"),
			Profit:    math.Round(dailyProfit*100) / 100,
			CumProfit: math.Round((baseProfit+cumProfit)*100) / 100,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"trend":      trend,
		"period":     period,
		"strategyId": strategyID,
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
