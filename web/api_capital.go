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
	TotalCapital       float64 `json:"totalCapital"`
	AvailableCapital   float64 `json:"availableCapital"`
	AllocatedCapital   float64 `json:"allocatedCapital"`
	ReservedCapital    float64 `json:"reservedCapital"`
	UsedMargin         float64 `json:"usedMargin"`
	UnrealizedPnL      float64 `json:"unrealizedPnL"`
	TodayPnL           float64 `json:"todayPnL"`
	AllocationRate     float64 `json:"allocationRate"`
	HealthScore        int     `json:"healthScore"`
	RiskLevel          string  `json:"riskLevel"`
	LastUpdated        string  `json:"lastUpdated"`
}

// StrategyCapitalDetail 策略资金详情（扩展信息）
type StrategyCapitalDetail struct {
	StrategyID       string  `json:"strategyId"`
	StrategyName     string  `json:"strategyName"`
	AllocatedAmount  float64 `json:"allocatedAmount"`
	UsedAmount       float64 `json:"usedAmount"`
	AvailableAmount  float64 `json:"availableAmount"`
	AllocationPct    float64 `json:"allocationPct"`
	MaxAllocationPct float64 `json:"maxAllocationPct"`
	Priority         int     `json:"priority"`
	IsLocked         bool    `json:"isLocked"`
	UnrealizedPnL    float64 `json:"unrealizedPnL"`
	TodayPnL         float64 `json:"todayPnL"`
	Status           string  `json:"status"`
}

// CapitalAllocationConfig 资金分配配置
type CapitalAllocationConfig struct {
	StrategyID       string  `json:"strategyId"`
	MaxAllocationPct float64 `json:"maxAllocationPct"`
	MinAllocationPct float64 `json:"minAllocationPct"`
	Priority         int     `json:"priority"`
	AutoRebalance    bool    `json:"autoRebalance"`
	RebalanceThreshold float64 `json:"rebalanceThreshold"`
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
		TotalCapital:     50000.00,
		AvailableCapital: 15000.00,
		AllocatedCapital: 32000.00,
		ReservedCapital:  3000.00,
		UsedMargin:       28000.00,
		UnrealizedPnL:    823.45,
		TodayPnL:         523.78,
		AllocationRate:   64.0,
		HealthScore:      85,
		RiskLevel:        "medium",
		LastUpdated:      time.Now().Format(time.RFC3339),
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"overview": overview,
	})
}

// 获取资金分配配置
func getCapitalAllocationHandler(c *gin.Context) {
	allocations := []StrategyCapitalDetail{
		{
			StrategyID:       "grid",
			StrategyName:     "网格交易策略",
			AllocatedAmount:  20000,
			UsedAmount:       18000,
			AvailableAmount:  2000,
			AllocationPct:    40,
			MaxAllocationPct: 50,
			Priority:         1,
			IsLocked:         false,
			UnrealizedPnL:    523.45,
			TodayPnL:         325.78,
			Status:           "running",
		},
		{
			StrategyID:       "dca",
			StrategyName:     "DCA 定投策略",
			AllocatedAmount:  12000,
			UsedAmount:       10000,
			AvailableAmount:  2000,
			AllocationPct:    24,
			MaxAllocationPct: 30,
			Priority:         2,
			IsLocked:         false,
			UnrealizedPnL:    300.00,
			TodayPnL:         198.00,
			Status:           "running",
		},
	}

	configs := []CapitalAllocationConfig{
		{
			StrategyID:         "grid",
			MaxAllocationPct:   50,
			MinAllocationPct:   10,
			Priority:           1,
			AutoRebalance:      true,
			RebalanceThreshold: 5,
		},
		{
			StrategyID:         "dca",
			MaxAllocationPct:   30,
			MinAllocationPct:   5,
			Priority:           2,
			AutoRebalance:      true,
			RebalanceThreshold: 5,
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"allocations": allocations,
		"configs":     configs,
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
		totalPct += alloc.MaxAllocationPct
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
		StrategyID:       strategyID,
		StrategyName:     getStrategyName(strategyID),
		AllocatedAmount:  20000,
		UsedAmount:       18000,
		AvailableAmount:  2000,
		AllocationPct:    40,
		MaxAllocationPct: 50,
		Priority:         1,
		IsLocked:         false,
		UnrealizedPnL:    523.45,
		TodayPnL:         325.78,
		Status:           "running",
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
				StrategyName:    "网格交易策略",
				AllocatedAmount: 19000,
				AllocationPct:   38,
			},
			{
				StrategyID:      "dca",
				StrategyName:    "DCA 定投策略",
				AllocatedAmount: 13000,
				AllocationPct:   26,
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
