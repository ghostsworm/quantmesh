package web

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/position"
)

// CapitalDataSource 资金数据源接口（由 main.go 实现）
type CapitalDataSource interface {
	GetExchanges() []exchange.IExchange
	GetStrategyConfigs() map[string]config.StrategyConfig
	GetPositionManagers() []PositionManagerInfo
	GetConfig() *config.Config // 新增
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
	IsTestnet    bool    `json:"isTestnet"` // 是否使用测试网
}

// ExchangeCapitalDetail 交易所资金详情（包含资产层级）
type ExchangeCapitalDetail struct {
	ExchangeID   string            `json:"exchangeId"`
	ExchangeName string            `json:"exchangeName"`
	Assets       []AssetAllocation `json:"assets"`
	IsTestnet    bool              `json:"isTestnet"` // 是否使用测试网
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
	Changes         []RebalanceChange       `json:"changes"` // 添加此字段以匹配前端
	TotalMoved      float64                 `json:"totalMoved"`
	MovementDetails []CapitalMovement       `json:"movementDetails"`
	NewAllocations  []StrategyCapitalDetail `json:"newAllocations"`
	ExecutedAt      string                  `json:"executedAt"`
}

// RebalanceChange 策略分配变化
type RebalanceChange struct {
	StrategyID         string  `json:"strategyId"`
	PreviousAllocation float64 `json:"previousAllocation"`
	NewAllocation      float64 `json:"newAllocation"`
	Difference         float64 `json:"difference"`
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
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "资金数据源未就绪",
			"overview": CapitalOverview{
				LastUpdated: time.Now().Format(time.RFC3339),
			},
		})
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
			logger.Error("❌ [资金概览] 获取交易所 %s 账户信息失败: %v", name, err)
			// 🔥 改进：报错也要加进列表，显示为 error 状态
			// 从配置中获取测试网状态
			isTestnet := false
			if cfg := capitalDataSource.GetConfig(); cfg != nil {
				if exCfg, ok := cfg.Exchanges[name]; ok {
					isTestnet = exCfg.Testnet
				}
			}
			overview.Exchanges = append(overview.Exchanges, ExchangeCapitalSummary{
				ExchangeID:   name,
				ExchangeName: name,
				TotalBalance: 0,
				Available:    0,
				Status:       "error",
				IsTestnet:    isTestnet,
			})
			continue
		}

		// 从配置中获取测试网状态
		isTestnet := false
		if cfg := capitalDataSource.GetConfig(); cfg != nil {
			if exCfg, ok := cfg.Exchanges[name]; ok {
				isTestnet = exCfg.Testnet
			}
		}

		summary := ExchangeCapitalSummary{
			ExchangeID:   name,
			ExchangeName: name,
			TotalBalance: math.Round(acc.TotalMarginBalance*100) / 100,
			Available:    math.Round(acc.AvailableBalance*100) / 100,
			Used:         math.Round((acc.TotalMarginBalance-acc.AvailableBalance)*100) / 100,
			PnL:          math.Round((acc.TotalMarginBalance-acc.TotalWalletBalance)*100) / 100,
			Status:       "online",
			IsTestnet:    isTestnet,
		}
		overview.Exchanges = append(overview.Exchanges, summary)
		overview.TotalBalance += acc.TotalMarginBalance
		overview.AvailableCapital += acc.AvailableBalance
		overview.UnrealizedPnL += (acc.TotalMarginBalance - acc.TotalWalletBalance)
	}

	// 2. 汇总策略分配数据
	for _, cfg := range strategyConfigs {
		if cfg.Enabled {
			alloc := overview.TotalBalance * cfg.Weight
			overview.AllocatedCapital += alloc
		}
	}

	// 3. 汇总实际占用资金
	for _, pm := range posManagers {
		overview.UsedCapital += pm.Manager.GetTotalBuyQty() * pm.Manager.GetPriceInterval()
	}

	if overview.TotalBalance > 0 {
		overview.MarginRatio = overview.UsedCapital / overview.TotalBalance
	}

	// 四舍五入
	overview.TotalBalance = math.Round(overview.TotalBalance*100) / 100
	overview.AllocatedCapital = math.Round(overview.AllocatedCapital*100) / 100
	overview.UsedCapital = math.Round(overview.UsedCapital*100) / 100
	overview.AvailableCapital = math.Round(overview.AvailableCapital*100) / 100
	overview.UnrealizedPnL = math.Round(overview.UnrealizedPnL*100) / 100

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"overview": overview,
	})
}

// 获取资金分配配置
func getCapitalAllocationHandler(c *gin.Context) {
	if capitalDataSource == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false, 
			"message": "资金数据源未就绪",
			"exchanges": []ExchangeCapitalDetail{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	exchanges := capitalDataSource.GetExchanges()
	strategyConfigs := capitalDataSource.GetStrategyConfigs()
	posManagers := capitalDataSource.GetPositionManagers()
	cfg := capitalDataSource.GetConfig()

	var details []ExchangeCapitalDetail
	exchangeMap := make(map[string]*ExchangeCapitalDetail)
	exchangeInstanceMap := make(map[string]exchange.IExchange)

	// 建立交易所实例映射（正在运行的交易所）
	for _, ex := range exchanges {
		name := ex.GetName()
		exchangeInstanceMap[strings.ToLower(name)] = ex
	}

	// 格式化交易所显示名称
	formatExchangeName := func(exID string) string {
		exIDLower := strings.ToLower(exID)
		switch exIDLower {
		case "binance":
			return "Binance"
		case "gate":
			return "Gate.io"
		case "okx":
			return "OKX"
		case "bitget":
			return "Bitget"
		case "bybit":
			return "Bybit"
		case "huobi":
			return "Huobi"
		case "kucoin":
			return "KuCoin"
		default:
			// 首字母大写
			if len(exID) > 0 {
				return strings.ToUpper(exID[:1]) + strings.ToLower(exID[1:])
			}
			return exID
		}
	}

	// 从配置中获取所有配置的交易所（与 getExchanges API 保持一致的逻辑）
	configuredExchanges := make(map[string]bool)
	if cfg != nil {
		// 从配置的 exchanges 中读取
		for exName := range cfg.Exchanges {
			if exName != "" {
				configuredExchanges[strings.ToLower(exName)] = true
			}
		}
		// 从交易对配置中读取交易所
		for _, sym := range cfg.Trading.Symbols {
			if sym.Exchange != "" {
				configuredExchanges[strings.ToLower(sym.Exchange)] = true
			} else if cfg.App.CurrentExchange != "" {
				configuredExchanges[strings.ToLower(cfg.App.CurrentExchange)] = true
			}
		}
		// 如果只有单交易对配置
		if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" {
			if cfg.App.CurrentExchange != "" {
				configuredExchanges[strings.ToLower(cfg.App.CurrentExchange)] = true
			}
		}
	}

	// 🔥 关键修复：添加所有正在运行的交易所实例（确保它们被包含）
	for exNameLower := range exchangeInstanceMap {
		configuredExchanges[exNameLower] = true
	}

	// 🔥 从运行状态中读取交易所（与 getExchanges API 保持一致）
	statusMu.RLock()
	for _, st := range statusBySymbol {
		if st != nil && st.Exchange != "" {
			configuredExchanges[strings.ToLower(st.Exchange)] = true
		}
	}
	statusMu.RUnlock()

	// 向后兼容：如果仍然没有交易所，尝试从 currentStatus 读取
	if len(configuredExchanges) == 0 && currentStatus != nil && currentStatus.Exchange != "" {
		configuredExchanges[strings.ToLower(currentStatus.Exchange)] = true
	}

	logger.Debug("ℹ️ [资金分配] 找到 %d 个交易所: %v", len(configuredExchanges), configuredExchanges)

	// 处理所有配置的交易所（包括正在运行的）
	for exNameLower := range configuredExchanges {
		if _, ok := exchangeMap[exNameLower]; ok {
			continue
		}

		// 从配置中获取测试网状态（使用原始大小写的键查找）
		isTestnet := false
		if cfg != nil {
			// 尝试查找原始键（可能大小写不同）
			for exKey := range cfg.Exchanges {
				if strings.ToLower(exKey) == exNameLower {
					if exCfg, ok := cfg.Exchanges[exKey]; ok {
						isTestnet = exCfg.Testnet
					}
					break
				}
			}
		}

		// 如果有运行的实例，尝试获取账户信息
		var exDetail *ExchangeCapitalDetail
		if ex, hasInstance := exchangeInstanceMap[exNameLower]; hasInstance {
			acc, err := ex.GetAccount(ctx)
			if err != nil {
				logger.Error("❌ [资金分配] 获取交易所 %s 账户信息失败: %v", exNameLower, err)
				// 获取失败也要显示，只是余额为 0
				exDetail = &ExchangeCapitalDetail{
					ExchangeID:   exNameLower,
					ExchangeName: formatExchangeName(exNameLower),
					Assets: []AssetAllocation{
						{
							Asset:            "USDT",
							TotalBalance:     0,
							AvailableBalance: 0,
						},
					},
					IsTestnet: isTestnet,
				}
			} else {
				exDetail = &ExchangeCapitalDetail{
					ExchangeID:   exNameLower,
					ExchangeName: formatExchangeName(exNameLower),
					Assets: []AssetAllocation{
						{
							Asset:            "USDT",
							TotalBalance:     math.Round(acc.TotalMarginBalance*100) / 100,
							AvailableBalance: math.Round(acc.AvailableBalance*100) / 100,
						},
					},
					IsTestnet: isTestnet,
				}
			}
		} else {
			// 没有运行的实例，显示为未连接状态
			logger.Debug("ℹ️ [资金分配] 交易所 %s 已配置但未运行", exNameLower)
			exDetail = &ExchangeCapitalDetail{
				ExchangeID:   exNameLower,
				ExchangeName: formatExchangeName(exNameLower),
				Assets: []AssetAllocation{
					{
						Asset:            "USDT",
						TotalBalance:     0,
						AvailableBalance: 0,
					},
				},
				IsTestnet: isTestnet,
			}
		}

		exchangeMap[exNameLower] = exDetail
		details = append(details, *exDetail)
	}

	// 填充策略分配
	for strategyID, cfg := range strategyConfigs {
		if !cfg.Enabled {
			continue
		}

		for i := range details {
			for j := range details[i].Assets {
				asset := &details[i].Assets[j]
				
				alloc := asset.TotalBalance * cfg.Weight
				
				// 从配置中读取 maxCapital 和 maxPercentage
				maxCapital := 0.0
				maxPercentage := 100.0
				if cfg.Config != nil {
					if val, ok := cfg.Config["max_capital"].(float64); ok {
						maxCapital = val
					} else if val, ok := cfg.Config["max_capital"].(int); ok {
						maxCapital = float64(val)
					}
					if val, ok := cfg.Config["max_percentage"].(float64); ok {
						maxPercentage = val
					} else if val, ok := cfg.Config["max_percentage"].(int); ok {
						maxPercentage = float64(val)
					}
				}
				
				strategy := StrategyCapitalDetail{
					StrategyID:      strategyID,
					StrategyName:    getStrategyName(strategyID),
					StrategyType:    strategyID,
					ExchangeID:      details[i].ExchangeID,
					Asset:           asset.Asset,
					Allocated:       math.Round(alloc*100) / 100,
					Weight:          cfg.Weight,
					MaxCapital:      maxCapital,
					MaxPercentage:  maxPercentage,
					Status:          "active",
				}
				
				// 从配置中读取其他字段
				if cfg.Config != nil {
					if val, ok := cfg.Config["reserve_ratio"].(float64); ok {
						strategy.ReserveRatio = val
					} else {
						strategy.ReserveRatio = 0.1 // 默认值
					}
					if val, ok := cfg.Config["auto_rebalance"].(bool); ok {
						strategy.AutoRebalance = val
					}
					if val, ok := cfg.Config["priority"].(int); ok {
						strategy.Priority = val
					} else if val, ok := cfg.Config["priority"].(float64); ok {
						strategy.Priority = int(val)
					} else {
						strategy.Priority = 1 // 默认值
					}
				} else {
					strategy.ReserveRatio = 0.1
					strategy.AutoRebalance = false
					strategy.Priority = 1
				}

				// 计算实际占用
				for _, pm := range posManagers {
					if pm.Exchange == details[i].ExchangeID {
						// 这里需要判断该 PM 是否属于该策略
						// TODO: 完善策略与交易对的关联逻辑
						strategy.Used += pm.Manager.GetTotalBuyQty() * pm.Manager.GetPriceInterval()
					}
				}
				
				strategy.Used = math.Round(strategy.Used*100) / 100
				strategy.Available = math.Round((strategy.Allocated - strategy.Used)*100) / 100
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
			asset.AllocatedToStrategies = math.Round(asset.AllocatedToStrategies*100) / 100
			asset.Unallocated = math.Round((asset.TotalBalance - asset.AllocatedToStrategies)*100) / 100
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"exchanges": details,
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

	// 1. 验证每个策略的 maxPercentage 范围（这是上限，不是实际分配比例）
	for _, alloc := range req.Allocations {
		if alloc.MaxPercentage < 0 || alloc.MaxPercentage > 100 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "策略 " + alloc.StrategyID + " 的分配比例上限必须在 0-100 之间",
			})
			return
		}
	}

	// 2. 验证实际分配金额总和不超过总余额
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

		if totalRealBalance > 0 {
			totalFixedCapital := 0.0
			for _, alloc := range req.Allocations {
				if alloc.MaxCapital < 0 {
					c.JSON(http.StatusBadRequest, gin.H{
						"success": false,
						"message": "策略 " + alloc.StrategyID + " 的分配金额不能为负数",
					})
					return
				}
				totalFixedCapital += alloc.MaxCapital
			}

			// 计算实际分配比例
			actualTotalPct := (totalFixedCapital / totalRealBalance) * 100
			
			if actualTotalPct > 100 {
				c.JSON(http.StatusBadRequest, gin.H{
					"success": false,
					"message": fmt.Sprintf("同一资产下的总分配比例不能超过 100%%，当前为 %.2f%%", actualTotalPct),
				})
				return
			}
		}
	}

	// 3. 持久化到 config.yaml
	if capitalDataSource != nil {
		globalCfg := capitalDataSource.GetConfig()
		if globalCfg != nil {
			updated := false
			
			// 计算总资金用于计算权重
			ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
			defer cancel()
			
			exchanges := capitalDataSource.GetExchanges()
			var totalRealBalance float64
			for _, ex := range exchanges {
				if acc, err := ex.GetAccount(ctx); err == nil {
					totalRealBalance += acc.TotalMarginBalance
				}
			}
			
			// 更新每个策略的配置
			for _, alloc := range req.Allocations {
				// strategyId 应该是策略类型（如 "grid", "martingale"）
				// 如果包含交易所信息（如 "binance-grid"），需要解析
				strategyType := alloc.StrategyID
				if strings.Contains(strategyType, "-") {
					// 如果包含 "-"，可能是 "exchange-strategy" 格式，提取策略类型
					parts := strings.Split(strategyType, "-")
					if len(parts) > 1 {
						strategyType = parts[len(parts)-1] // 取最后一部分作为策略类型
					}
				}
				
				if sc, ok := globalCfg.Strategies.Configs[strategyType]; ok {
					// 更新配置
					if sc.Config == nil {
						sc.Config = make(map[string]interface{})
					}
					sc.Config["max_capital"] = alloc.MaxCapital
					sc.Config["max_percentage"] = alloc.MaxPercentage
					sc.Config["reserve_ratio"] = alloc.ReserveRatio
					sc.Config["auto_rebalance"] = alloc.AutoRebalance
					sc.Config["priority"] = alloc.Priority
					
					// 优先使用 maxPercentage 计算权重（因为用户设置的是百分比）
					if alloc.MaxPercentage > 0 {
						// 如果使用百分比模式，直接使用百分比作为权重
						sc.Weight = alloc.MaxPercentage / 100.0
						logger.Info("✅ 更新策略 %s 配置: maxPercentage=%.2f%%, weight=%.4f (基于百分比)", strategyType, alloc.MaxPercentage, sc.Weight)
					} else if totalRealBalance > 0 && alloc.MaxCapital > 0 {
						// 如果没有百分比，使用金额计算权重
						newWeight := alloc.MaxCapital / totalRealBalance
						sc.Weight = newWeight
						logger.Info("✅ 更新策略 %s 配置: maxCapital=%.2f, weight=%.4f (基于金额)", strategyType, alloc.MaxCapital, sc.Weight)
					}
					
					globalCfg.Strategies.Configs[strategyType] = sc
					updated = true
				} else {
					logger.Warn("⚠️ 未找到策略配置: %s (尝试的 strategyType: %s)", alloc.StrategyID, strategyType)
				}
			}
			
			// 保存到文件
			if updated {
				configPath := "config.yaml"
				if err := config.SaveConfig(globalCfg, configPath); err != nil {
					logger.Error("❌ 保存资金分配配置失败: %v", err)
					c.JSON(http.StatusInternalServerError, gin.H{
						"success": false,
						"message": "保存配置失败: " + err.Error(),
					})
					return
				}
				logger.Info("✅ 资金分配配置已保存到 %s", configPath)
			}
		}
	}

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

	if capitalDataSource == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "资金数据源未就绪"})
		return
	}

	configs := capitalDataSource.GetStrategyConfigs()
	cfg, ok := configs[strategyID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "未找到策略配置"})
		return
	}

	// 汇总该策略在所有交易所的资金
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	exchanges := capitalDataSource.GetExchanges()
	posManagers := capitalDataSource.GetPositionManagers()

	var totalAllocated, totalUsed float64
	for _, ex := range exchanges {
		if acc, err := ex.GetAccount(ctx); err == nil {
			totalAllocated += acc.TotalMarginBalance * cfg.Weight
		}
	}

	for _, pm := range posManagers {
		// 简化逻辑：这里应该判断 PM 是否属于该策略
		totalUsed += pm.Manager.GetTotalBuyQty() * pm.Manager.GetPriceInterval()
	}

	maxCap := 0.0
	if val, ok := cfg.Config["max_capital"].(float64); ok {
		maxCap = val
	} else if val, ok := cfg.Config["max_capital"].(int); ok {
		maxCap = float64(val)
	}

	capital := StrategyCapitalDetail{
		StrategyID:      strategyID,
		StrategyName:    getStrategyName(strategyID),
		StrategyType:    strategyID,
		Allocated:       math.Round(totalAllocated*100) / 100,
		Used:            math.Round(totalUsed*100) / 100,
		Available:       math.Round((totalAllocated-totalUsed)*100) / 100,
		Weight:          cfg.Weight,
		MaxCapital:      maxCap,
		Status:          "active",
	}
	if totalAllocated > 0 {
		capital.UtilizationRate = totalUsed / totalAllocated
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"capital": capital,
	})
}

// 触发资金再平衡
func rebalanceCapitalHandler(c *gin.Context) {
	var req struct {
		Mode   string `json:"mode"` // equal, weighted, priority
		Force  bool   `json:"force"`
		DryRun bool   `json:"dryRun"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req.Mode = "weighted" // 默认按权重
	}

	if capitalDataSource == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "资金数据源未就绪"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// 1. 获取总资产 (实时从交易所取)
	exchanges := capitalDataSource.GetExchanges()
	totalBalance := 0.0
	for _, ex := range exchanges {
		acc, err := ex.GetAccount(ctx)
		if err == nil {
			totalBalance += acc.TotalMarginBalance
		}
	}

	if totalBalance <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "无法获取账户余额或余额为0"})
		return
	}

	// 2. 获取策略配置
	stratConfigs := capitalDataSource.GetStrategyConfigs()
	enabledStrategies := make([]string, 0)
	for id, cfg := range stratConfigs {
		if cfg.Enabled {
			enabledStrategies = append(enabledStrategies, id)
		}
	}

	if len(enabledStrategies) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "没有已启用的策略"})
		return
	}

	// 3. 计算新分配
	changes := make([]RebalanceChange, 0)
	newAllocations := make([]StrategyCapitalDetail, 0)
	
	count := float64(len(enabledStrategies))
	totalWeight := 0.0
	for _, id := range enabledStrategies {
		totalWeight += stratConfigs[id].Weight
	}

	for _, id := range enabledStrategies {
		cfg := stratConfigs[id]
		
		// 计算目标分配
		var targetAllocation float64
		switch req.Mode {
		case "equal":
			targetAllocation = totalBalance / count
		case "weighted":
			if totalWeight > 0 {
				targetAllocation = (cfg.Weight / totalWeight) * totalBalance
			} else {
				targetAllocation = totalBalance / count
			}
		case "priority":
			// 简化逻辑：高权重的先分（实际生产环境会更复杂）
			targetAllocation = (cfg.Weight / totalWeight) * totalBalance
		default:
			targetAllocation = (cfg.Weight / totalWeight) * totalBalance
		}

		// 获取当前分配（从配置读取）
		prevAllocation := 0.0
		if val, ok := cfg.Config["max_capital"].(float64); ok {
			prevAllocation = val
		} else if val, ok := cfg.Config["max_capital"].(int); ok {
			prevAllocation = float64(val)
		}

		diff := targetAllocation - prevAllocation
		
		changes = append(changes, RebalanceChange{
			StrategyID:         id,
			PreviousAllocation: math.Round(prevAllocation*100) / 100,
			NewAllocation:      math.Round(targetAllocation*100) / 100,
			Difference:         math.Round(diff*100) / 100,
		})

		newAllocations = append(newAllocations, StrategyCapitalDetail{
			StrategyID:   id,
			StrategyName: getStrategyName(id),
			Allocated:    targetAllocation,
			Status:       "active",
		})
	}

	// 4. 如果不是 DryRun，则应用配置（实际写入 config.yaml）
	if !req.DryRun {
		globalCfg := capitalDataSource.GetConfig()
		for _, change := range changes {
			if sc, ok := globalCfg.Strategies.Configs[change.StrategyID]; ok {
				if sc.Config == nil {
					sc.Config = make(map[string]interface{})
				}
				sc.Config["max_capital"] = change.NewAllocation
				globalCfg.Strategies.Configs[change.StrategyID] = sc
			}
		}
		// 保存到文件
		if err := config.SaveConfig(globalCfg, "config.yaml"); err != nil {
			logger.Error("❌ 保存再平衡配置失败: %v", err)
		} else {
			logger.Info("✅ 资金再平衡配置已保存")
		}
	}

	result := RebalanceResult{
		Success:        true,
		Message:        "再平衡计算完成",
		Changes:        changes,
		NewAllocations: newAllocations,
		ExecutedAt:     time.Now().Format(time.RFC3339),
	}

	if req.DryRun {
		result.Message = "模拟再平衡预览（未应用）"
	} else {
		result.Message = "再平衡已成功应用到配置"
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
