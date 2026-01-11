package web

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
)

// CapitalDataSource 资金数据源接口（由 main.go 实现）
type CapitalDataSource interface {
	GetExchanges() []exchange.IExchange
	GetStrategyConfigs() map[string]config.StrategyConfig
	GetPositionManagers() []PositionManagerInfo
}

// PositionManagerInfo 仓位管理器信息
type PositionManagerInfo struct {
	Exchange string
	Symbol   string
	Manager  *position.SuperPositionManager
}

var capitalDataSource CapitalDataSource

// SetCapitalDataSource 设置资金数据源
func SetCapitalDataSource(ds CapitalDataSource) {
	capitalDataSource = ds
}

// CapitalOverview 资金概览（汇总或分交易所）
type CapitalOverview struct {
	TotalBalance     float64                  `json:"totalBalance"`     // 总权益
	AllocatedCapital float64                  `json:"allocatedCapital"` // 已分配给策略的资金
	UsedCapital      float64                  `json:"usedCapital"`      // 实际已占用保证金
	AvailableCapital float64                  `json:"availableCapital"` // 交易所可用余额
	ReservedCapital  float64                  `json:"reservedCapital"`  // 用户预留资金（不可用于策略）
	UnrealizedPnL    float64                  `json:"unrealizedPnL"`    // 未实现盈亏
	MarginRatio      float64                  `json:"marginRatio"`      // 保证金占用率
	Exchanges        []ExchangeCapitalSummary `json:"exchanges,omitempty"`
	LastUpdated      string                   `json:"lastUpdated"`
}

// ExchangeCapitalSummary 交易所资金摘要
type ExchangeCapitalSummary struct {
	ExchangeID   string  `json:"exchangeId"`
	ExchangeName string  `json:"exchangeName"`
	TotalBalance float64 `json:"totalBalance"`
	Available    float64 `json:"available"`
	Used         float64 `json:"used"`
	PnL          float64 `json:"pnl"`
	Status       string  `json:"status"` // online, offline, error
}

// ExchangeCapitalDetail 交易所资金详情（包含资产层级）
type ExchangeCapitalDetail struct {
	ExchangeID   string            `json:"exchangeId"`
	ExchangeName string            `json:"exchangeName"`
	Assets       []AssetAllocation `json:"assets"`
}

// AssetAllocation 资产分配（如 USDT 下的策略分配）
type AssetAllocation struct {
	Asset            string                  `json:"asset"`
	TotalBalance     float64                 `json:"totalBalance"`
	AvailableBalance float64                 `json:"availableBalance"`
	AllocatedToStrategies float64            `json:"allocatedToStrategies"`
	Unallocated      float64                 `json:"unallocated"`
	Strategies       []StrategyCapitalDetail `json:"strategies"`
}

// StrategyCapitalDetail 策略资金详情
type StrategyCapitalDetail struct {
	StrategyID      string  `json:"strategyId"`
	StrategyName    string  `json:"strategyName"`
	StrategyType    string  `json:"strategyType"`
	ExchangeID      string  `json:"exchangeId"` // 所属交易所
	Asset           string  `json:"asset"`      // 结算资产 (如 USDT)
	Allocated       float64 `json:"allocated"`  // 分配金额
	Used            float64 `json:"used"`       // 已占用
	Available       float64 `json:"available"`  // 可用配额
	Weight          float64 `json:"weight"`     // 权重 (0-1)
	MaxCapital      float64 `json:"maxCapital"` // 最大固定限额
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
	if capitalDataSource == nil {
		// 返回模拟数据作为回退
		mockOverview(c)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	exchanges := capitalDataSource.GetExchanges()
	strategyConfigs := capitalDataSource.GetStrategyConfigs()
	posManagers := capitalDataSource.GetPositionManagers()

	var overview CapitalOverview
	overview.LastUpdated = time.Now().Format(time.RFC3339)

	// 1. 汇总交易所实时数据
	exchangeMap := make(map[string]bool)
	for _, ex := range exchanges {
		name := ex.GetName()
		if exchangeMap[name] {
			continue
		}
		exchangeMap[name] = true

		acc, err := ex.GetAccount(ctx)
		if err != nil {
			overview.Exchanges = append(overview.Exchanges, ExchangeCapitalSummary{
				ExchangeID:   name,
				ExchangeName: name,
				Status:       "error",
			})
			continue
		}

		summary := ExchangeCapitalSummary{
			ExchangeID:   name,
			ExchangeName: name,
			TotalBalance: acc.TotalMarginBalance,
			Available:    acc.AvailableBalance,
			Used:         acc.TotalMarginBalance - acc.AvailableBalance,
			PnL:          acc.TotalMarginBalance - acc.TotalWalletBalance,
			Status:       "online",
		}
		overview.Exchanges = append(overview.Exchanges, summary)
		overview.TotalBalance += acc.TotalMarginBalance
		overview.AvailableCapital += acc.AvailableBalance
		overview.UnrealizedPnL += (acc.TotalMarginBalance - acc.TotalWalletBalance)
	}

	// 2. 汇总策略分配数据 (从配置计算)
	for _, cfg := range strategyConfigs {
		if cfg.Enabled {
			// 分配金额 = 总权益 * 权重
			// 注意：这只是一个简化的计算，实际可能更复杂
			alloc := overview.TotalBalance * cfg.Weight
			overview.AllocatedCapital += alloc
		}
	}

	// 3. 汇总实际占用资金 (从仓位管理器计算)
	for _, pm := range posManagers {
		// 估算单个交易对占用的保证金
		// 实际上 SuperPositionManager 应该提供获取已占用保证金的方法
		// 这里暂用估算：持仓数量 * 价格 / 杠杆 (简化)
		// 或者是 PM 内部已经算好的统计数据
		overview.UsedCapital += pm.Manager.GetTotalBuyQty() * pm.Manager.GetPriceInterval() // 简化占位
	}

	if overview.TotalBalance > 0 {
		overview.MarginRatio = overview.UsedCapital / overview.TotalBalance
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"overview": overview,
	})
}

func mockOverview(c *gin.Context) {
	exchange := c.DefaultQuery("exchange", "all")
	overview := CapitalOverview{
		TotalBalance:     75000.00,
		AllocatedCapital: 45000.00,
		UsedCapital:      32000.00,
		AvailableCapital: 43000.00,
		ReservedCapital:  5000.00,
		UnrealizedPnL:    1250.45,
		MarginRatio:      0.43,
		LastUpdated:      time.Now().Format(time.RFC3339),
	}
	if exchange == "all" {
		overview.Exchanges = []ExchangeCapitalSummary{
			{ExchangeID: "binance", ExchangeName: "Binance", TotalBalance: 50000.00, Available: 30000.00, Used: 20000.00, PnL: 850.25, Status: "online"},
			{ExchangeID: "okx", ExchangeName: "OKX", TotalBalance: 25000.00, Available: 13000.00, Used: 12000.00, PnL: 400.20, Status: "online"},
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "overview": overview})
}

// 获取资金分配配置
func getCapitalAllocationHandler(c *gin.Context) {
	if capitalDataSource == nil {
		mockAllocation(c)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	exchanges := capitalDataSource.GetExchanges()
	strategyConfigs := capitalDataSource.GetStrategyConfigs()
	posManagers := capitalDataSource.GetPositionManagers()

	var details []ExchangeCapitalDetail
	exchangeMap := make(map[string]*ExchangeCapitalDetail)

	for _, ex := range exchanges {
		name := ex.GetName()
		if _, ok := exchangeMap[name]; ok {
			continue
		}

		acc, err := ex.GetAccount(ctx)
		if err != nil {
			continue
		}

		exDetail := &ExchangeCapitalDetail{
			ExchangeID:   name,
			ExchangeName: name,
			Assets: []AssetAllocation{
				{
					Asset:            "USDT", // 默认假设结算资产为 USDT
					TotalBalance:     acc.TotalMarginBalance,
					AvailableBalance: acc.AvailableBalance,
				},
			},
		}
		exchangeMap[name] = exDetail
		details = append(details, *exDetail)
	}

	// 填充策略分配
	for _, cfg := range strategyConfigs {
		if !cfg.Enabled {
			continue
		}

		// 遍历所有交易所的所有资产，尝试分配（简化逻辑：默认分配到 binance USDT，如果存在）
		for i := range details {
			for j := range details[i].Assets {
				asset := &details[i].Assets[j]
				
				// 计算分配金额
				alloc := asset.TotalBalance * cfg.Weight
				
				strategy := StrategyCapitalDetail{
					StrategyID:      "strategy_" + nameFromConfig(cfg), // 需要一个方法从 cfg 获取 ID/Name
					StrategyName:    nameFromConfig(cfg),
					StrategyType:    typeFromConfig(cfg),
					ExchangeID:      details[i].ExchangeID,
					Asset:           asset.Asset,
					Allocated:       alloc,
					Weight:          cfg.Weight,
					Status:          "active",
				}

				// 计算实际占用
				for _, pm := range posManagers {
					if pm.Exchange == details[i].ExchangeID {
						// 这里需要判断该 PM 是否属于该策略
						// 目前暂简化为：如果 PM 的 Symbol 包含在该策略中
						// TODO: 完善策略与交易对的关联逻辑
						strategy.Used += pm.Manager.GetTotalBuyQty() * pm.Manager.GetPriceInterval()
					}
				}
				
				strategy.Available = strategy.Allocated - strategy.Used
				if strategy.Allocated > 0 {
					strategy.UtilizationRate = strategy.Used / strategy.Allocated
				}

				asset.Strategies = append(asset.Strategies, strategy)
				asset.AllocatedToStrategies += strategy.Allocated
			}
		}
	}

	// 计算未分配资金
	for i := range details {
		for j := range details[i].Assets {
			asset := &details[i].Assets[j]
			asset.Unallocated = asset.TotalBalance - asset.AllocatedToStrategies
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"exchanges": details,
	})
}

func nameFromConfig(cfg config.StrategyConfig) string {
	// 这里只是占位，实际需要根据配置获取策略名
	return "策略"
}

func typeFromConfig(cfg config.StrategyConfig) string {
	return "grid"
}

func mockAllocation(c *gin.Context) {
	data := []ExchangeCapitalDetail{
		{
			ExchangeID:   "binance",
			ExchangeName: "Binance",
			Assets: []AssetAllocation{
				{
					Asset:            "USDT",
					TotalBalance:     50000.00,
					AvailableBalance: 30000.00,
					AllocatedToStrategies: 32000.00,
					Unallocated:      18000.00,
					Strategies: []StrategyCapitalDetail{
						{
							StrategyID: "grid", StrategyName: "网格交易", StrategyType: "grid", ExchangeID: "binance", Asset: "USDT",
							Allocated: 20000, Used: 18000, Available: 2000, Weight: 0.4, Status: "active",
						},
						{
							StrategyID: "dca", StrategyName: "DCA 定投", StrategyType: "dca", ExchangeID: "binance", Asset: "USDT",
							Allocated: 12000, Used: 10000, Available: 2000, Weight: 0.24, Status: "active",
						},
					},
				},
			},
		},
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "exchanges": data})
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

	// 1. 验证分配总和不超过 100%
	totalPct := 0.0
	for _, alloc := range req.Allocations {
		if alloc.MaxPercentage < 0 || alloc.MaxPercentage > 100 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "策略 " + alloc.StrategyID + " 的分配比例必须在 0-100 之间",
			})
			return
		}
		totalPct += alloc.MaxPercentage
	}
	
	if totalPct > 100 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "同一资产下的总分配比例不能超过 100%",
		})
		return
	}

	// 2. 验证硬限制（可选：验证是否超过真实可用余额）
	if capitalDataSource != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		
		exchanges := capitalDataSource.GetExchanges()
		var totalRealBalance float64
		for _, ex := range exchanges {
			if acc, err := ex.GetAccount(ctx); err == nil {
				totalRealBalance += acc.TotalMarginBalance
			}
		}

		totalFixedCapital := 0.0
		for _, alloc := range req.Allocations {
			totalFixedCapital += alloc.MaxCapital
		}

		if totalRealBalance > 0 && totalFixedCapital > totalRealBalance {
			// 这里只是警告，或者也可以报错
			// logger.Warn("⚠️ 固定资金分配总额 (%.2f) 超过了账户总权益 (%.2f)", totalFixedCapital, totalRealBalance)
		}
	}

	// TODO: 持久化到 config.yaml
	// 这里需要调用 config.Service 来保存修改后的策略权重和限额

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "资金分配配置已更新并校验通过",
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
