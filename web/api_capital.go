package web

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// CapitalOverview 资金概览
type CapitalOverview struct {
	TotalBalance       float64 `json:"totalBalance"`
	AllocatedCapital   float64 `json:"allocatedCapital"`
	UsedCapital        float64 `json:"usedCapital"`
	AvailableCapital   float64 `json:"availableCapital"`
	ReservedCapital    float64 `json:"reservedCapital"`
	UnrealizedPnL      float64 `json:"unrealizedPnL"`
	MarginRatio        float64 `json:"marginRatio"`
	LastUpdated        string  `json:"lastUpdated"`
}

// StrategyCapitalDetail 策略资金详情（扩展信息）
type StrategyCapitalDetail struct {
	StrategyID      string  `json:"strategyId"`
	StrategyName    string  `json:"strategyName"`
	StrategyType    string  `json:"strategyType"`
	Allocated       float64 `json:"allocated"`
	Used            float64 `json:"used"`
	Available       float64 `json:"available"`
	Weight          float64 `json:"weight"`
	MaxCapital      float64 `json:"maxCapital"`
	MaxPercentage   float64 `json:"maxPercentage"`
	ReserveRatio    float64 `json:"reserveRatio"`
	AutoRebalance   bool    `json:"autoRebalance"`
	Priority        int     `json:"priority"`
	UtilizationRate float64 `json:"utilizationRate"`
	Status          string  `json:"status"`
}

// CapitalAllocationConfig 资金分配配置
type CapitalAllocationConfig struct {
	StrategyID    string  `json:"strategyId"`
	MaxCapital    float64 `json:"maxCapital"`
	MaxPercentage float64 `json:"maxPercentage"`
	ReserveRatio  float64 `json:"reserveRatio"`
	AutoRebalance bool    `json:"autoRebalance"`
	Priority      int     `json:"priority"`
}

// RebalanceResult 再平衡结果
type RebalanceResult struct {
	Success         bool                    `json:"success"`
	Message         string                  `json:"message"`
	TotalMoved      float64                 `json:"totalMoved"`
	MovementDetails []CapitalMovement       `json:"movementDetails"`
	NewAllocations  []StrategyCapitalDetail `json:"newAllocations"`
	ExecutedAt      string                  `json:"executedAt"`
}

// CapitalMovement 资金移动详情
type CapitalMovement struct {
	FromStrategy string  `json:"fromStrategy"`
	ToStrategy   string  `json:"toStrategy"`
	Amount       float64 `json:"amount"`
	Reason       string  `json:"reason"`
}

// CapitalHistoryPoint 资金历史点
type CapitalHistoryPoint struct {
	Timestamp string  `json:"timestamp"`
	Total     float64 `json:"total"`
	Allocated float64 `json:"allocated"`
	Available float64 `json:"available"`
	PnL       float64 `json:"pnl"`
}

// 获取资金概览
func getCapitalOverviewHandler(c *gin.Context) {
	overview := CapitalOverview{
		TotalBalance:     50000.00,
		AllocatedCapital: 32000.00,
		UsedCapital:      28000.00,
		AvailableCapital: 15000.00,
		ReservedCapital:  3000.00,
		UnrealizedPnL:    823.45,
		MarginRatio:      0.56,
		LastUpdated:      time.Now().Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"overview": overview,
	})
}

// 获取资金分配配置
func getCapitalAllocationHandler(c *gin.Context) {
	strategies := []StrategyCapitalDetail{
		{
			StrategyID:      "grid",
			StrategyName:    "网格交易",
			StrategyType:    "grid",
			Allocated:       20000,
			Used:            18000,
			Available:       2000,
			Weight:          0.4,
			MaxCapital:      25000,
			MaxPercentage:   50,
			ReserveRatio:    0.1,
			AutoRebalance:   true,
			Priority:        1,
			UtilizationRate: 0.9,
			Status:          "active",
		},
		{
			StrategyID:      "dca",
			StrategyName:    "DCA 定投",
			StrategyType:    "dca",
			Allocated:       12000,
			Used:            10000,
			Available:       2000,
			Weight:          0.24,
			MaxCapital:      15000,
			MaxPercentage:   30,
			ReserveRatio:    0.1,
			AutoRebalance:   true,
			Priority:        2,
			UtilizationRate: 0.83,
			Status:          "active",
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"strategies": strategies,
	})
}

// 更新资金分配
func updateCapitalAllocationHandler(c *gin.Context) {
	var req struct {
		Allocations []CapitalAllocationConfig `json:"allocations"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的请求数据: " + err.Error(),
		})
		return
	}

	// 验证分配总和不超过 100%
	totalPct := 0.0
	for _, alloc := range req.Allocations {
		totalPct += alloc.MaxPercentage
	}
	if totalPct > 100 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "总分配比例不能超过 100%",
		})
		return
	}

	// TODO: 保存到配置

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "资金分配配置已更新",
	})
}

// 更新单个策略的资金配置
func updateStrategyCapitalHandler(c *gin.Context) {
	strategyID := c.Param("id")

	var config CapitalAllocationConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的请求数据: " + err.Error(),
		})
		return
	}

	config.StrategyID = strategyID

	// TODO: 保存到配置

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "策略资金配置已更新",
	})
}

// 获取单个策略的资金详情
func getStrategyCapitalDetailHandler(c *gin.Context) {
	strategyID := c.Param("id")

	capital := StrategyCapitalDetail{
		StrategyID:      strategyID,
		StrategyName:    getStrategyName(strategyID),
		StrategyType:    getStrategyType(strategyID),
		Allocated:       20000,
		Used:            18000,
		Available:       2000,
		Weight:          0.4,
		MaxCapital:      25000,
		MaxPercentage:   50,
		ReserveRatio:    0.1,
		AutoRebalance:   true,
		Priority:        1,
		UtilizationRate: 0.9,
		Status:          "active",
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"capital": capital,
	})
}

// 触发资金再平衡
func rebalanceCapitalHandler(c *gin.Context) {
	var req struct {
		Mode      string `json:"mode"` // auto, manual, proportional
		Force     bool   `json:"force"`
		DryRun    bool   `json:"dryRun"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		// 使用默认值
		req.Mode = "auto"
	}

	// 模拟再平衡结果
	result := RebalanceResult{
		Success:    true,
		Message:    "资金再平衡完成",
		TotalMoved: 2000,
		MovementDetails: []CapitalMovement{
			{
				FromStrategy: "grid",
				ToStrategy:   "dca",
				Amount:       1000,
				Reason:       "平衡分配比例",
			},
			{
				FromStrategy: "reserve",
				ToStrategy:   "grid",
				Amount:       1000,
				Reason:       "补充保证金",
			},
		},
		NewAllocations: []StrategyCapitalDetail{
			{
				StrategyID:      "grid",
				StrategyName:    "网格交易",
				StrategyType:    "grid",
				Allocated:       19000,
				Used:            17000,
				Available:       2000,
				Weight:          0.38,
				MaxCapital:      25000,
				MaxPercentage:   50,
				ReserveRatio:    0.1,
				AutoRebalance:   true,
				Priority:        1,
				UtilizationRate: 0.89,
				Status:          "active",
			},
			{
				StrategyID:      "dca",
				StrategyName:    "DCA 定投",
				StrategyType:    "dca",
				Allocated:       13000,
				Used:            11000,
				Available:       2000,
				Weight:          0.26,
				MaxCapital:      15000,
				MaxPercentage:   30,
				ReserveRatio:    0.1,
				AutoRebalance:   true,
				Priority:        2,
				UtilizationRate: 0.85,
				Status:          "active",
			},
		},
		ExecutedAt: time.Now().Format(time.RFC3339),
	}

	if req.DryRun {
		result.Message = "模拟再平衡完成（未实际执行）"
	}

	c.JSON(http.StatusOK, result)
}

// 获取资金历史记录
func getCapitalHistoryHandler(c *gin.Context) {
	daysStr := c.DefaultQuery("days", "30")
	days, _ := strconv.Atoi(daysStr)
	if days <= 0 {
		days = 30
	}
	if days > 365 {
		days = 365
	}

	// 生成模拟历史数据
	history := make([]CapitalHistoryPoint, days)
	baseTotal := 45000.0

	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -days+i+1)
		// 模拟资金变化
		growth := float64(i) * 50 + math.Sin(float64(i)*0.2)*500
		total := baseTotal + growth
		allocated := total * 0.65
		available := total - allocated

		dailyPnL := 100 + 50*math.Sin(float64(i)*0.3) + float64(i%7)*20

		history[i] = CapitalHistoryPoint{
			Timestamp: date.Format("2006-01-02"),
			Total:     math.Round(total*100) / 100,
			Allocated: math.Round(allocated*100) / 100,
			Available: math.Round(available*100) / 100,
			PnL:       math.Round(dailyPnL*100) / 100,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"history": history,
		"days":    days,
	})
}

// 设置预留保证金
func setReserveCapitalHandler(c *gin.Context) {
	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的请求数据: " + err.Error(),
		})
		return
	}

	if req.Amount < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "预留保证金不能为负数",
		})
		return
	}

	// TODO: 保存到配置

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "预留保证金已设置为 " + strconv.FormatFloat(req.Amount, 'f', 2, 64),
	})
}

// 锁定/解锁策略资金
func lockStrategyCapitalHandler(c *gin.Context) {
	strategyID := c.Param("id")

	var req struct {
		Locked bool `json:"locked"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的请求数据: " + err.Error(),
		})
		return
	}

	action := "已锁定"
	if !req.Locked {
		action = "已解锁"
	}

	// TODO: 保存到配置

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"message":    "策略资金" + action,
		"strategyId": strategyID,
		"locked":     req.Locked,
	})
}
