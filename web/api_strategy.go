package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// StrategyInfo 策略信息
type StrategyInfo struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	Type               string   `json:"type"`
	RiskLevel          string   `json:"riskLevel"`
	IsPremium          bool     `json:"isPremium"`
	IsEnabled          bool     `json:"isEnabled"`
	IsLicensed         bool     `json:"isLicensed"`
	Features           []string `json:"features"`
	MinCapital         float64  `json:"minCapital"`
	RecommendedCapital float64  `json:"recommendedCapital"`
	Version            string   `json:"version"`
	Author             string   `json:"author"`
	Tags               []string `json:"tags"`
	RequiredVersion    string   `json:"requiredVersion"`
	CreatedAt          string   `json:"createdAt"`
	UpdatedAt          string   `json:"updatedAt"`
}

// StrategyDetailInfo 策略详情
type StrategyDetailInfo struct {
	StrategyInfo
	Parameters    []StrategyParameter `json:"parameters"`
	Documentation string              `json:"documentation"`
	Changelog     []ChangelogEntry    `json:"changelog"`
	Performance   StrategyPerformance `json:"performance"`
}

// StrategyParameter 策略参数
type StrategyParameter struct {
	Name         string      `json:"name"`
	Type         string      `json:"type"`
	Default      interface{} `json:"default"`
	Min          interface{} `json:"min,omitempty"`
	Max          interface{} `json:"max,omitempty"`
	Description  string      `json:"description"`
	Required     bool        `json:"required"`
	DisplayOrder int         `json:"displayOrder"`
}

// ChangelogEntry 变更日志条目
type ChangelogEntry struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Changes []string `json:"changes"`
}

// StrategyPerformance 策略性能
type StrategyPerformance struct {
	WinRate         float64 `json:"winRate"`
	AvgProfit       float64 `json:"avgProfit"`
	MaxDrawdown     float64 `json:"maxDrawdown"`
	SharpeRatio     float64 `json:"sharpeRatio"`
	TotalTrades     int     `json:"totalTrades"`
	BacktestPeriod  string  `json:"backtestPeriod"`
	LastUpdated     string  `json:"lastUpdated"`
}

// StrategyLicense 策略授权
type StrategyLicense struct {
	StrategyID   string `json:"strategyId"`
	Tier         string `json:"tier"`
	ValidFrom    string `json:"validFrom"`
	ValidUntil   string `json:"validUntil"`
	IsActive     bool   `json:"isActive"`
	MaxInstances int    `json:"maxInstances"`
}

// StrategyConfig 策略配置
type StrategyConfig struct {
	StrategyID    string                 `json:"strategyId"`
	Enabled       bool                   `json:"enabled"`
	Priority      int                    `json:"priority"`
	MaxAllocation float64                `json:"maxAllocation"`
	Parameters    map[string]interface{} `json:"parameters"`
}

// 获取所有可用策略
func getStrategiesHandler(c *gin.Context) {
	// 策略列表（包含内置策略和插件策略）
	strategies := []StrategyInfo{
		{
			ID:                 "grid",
			Name:               "网格交易策略",
			Description:        "经典网格交易策略，在价格区间内自动挂单，赚取波动差价",
			Type:               "grid",
			RiskLevel:          "low",
			IsPremium:          false,
			IsEnabled:          true,
			IsLicensed:         true,
			Features:           getStrategyFeatures("grid"),
			MinCapital:         100,
			RecommendedCapital: 500,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"网格", "震荡市", "自动化"},
			RequiredVersion:    "3.0.0",
			CreatedAt:          "2024-01-01T00:00:00Z",
			UpdatedAt:          "2024-06-15T00:00:00Z",
		},
		{
			ID:                 "dca",
			Name:               "DCA 定投策略",
			Description:        "定期定额买入策略，分散入场成本，降低投资风险",
			Type:               "dca",
			RiskLevel:          "low",
			IsPremium:          false,
			IsEnabled:          true,
			IsLicensed:         true,
			Features:           getStrategyFeatures("dca"),
			MinCapital:         100,
			RecommendedCapital: 500,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"定投", "长期", "低风险"},
			RequiredVersion:    "3.0.0",
			CreatedAt:          "2024-01-15T00:00:00Z",
			UpdatedAt:          "2024-07-01T00:00:00Z",
		},
		{
			ID:                 "dca_enhanced",
			Name:               "增强型 DCA 策略",
			Description:        "基于 ATR 动态间距、三级止盈、50层仓位管理、瀑布保护和趋势过滤的增强型 DCA",
			Type:               "dca",
			RiskLevel:          "medium",
			IsPremium:          true,
			IsEnabled:          false,
			IsLicensed:         false,
			Features:           getStrategyFeatures("dca_enhanced"),
			MinCapital:         200,
			RecommendedCapital: 1000,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"DCA", "ATR", "多层止盈", "风险管理"},
			RequiredVersion:    "3.4.0",
			CreatedAt:          "2025-12-01T00:00:00Z",
			UpdatedAt:          "2026-01-10T00:00:00Z",
		},
		{
			ID:                 "martingale",
			Name:               "马丁格尔策略",
			Description:        "亏损加倍补仓策略，支持正向/反向马丁、风险削减和多空双向",
			Type:               "martingale",
			RiskLevel:          "high",
			IsPremium:          true,
			IsEnabled:          false,
			IsLicensed:         false,
			Features:           getStrategyFeatures("martingale"),
			MinCapital:         500,
			RecommendedCapital: 2000,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"马丁格尔", "补仓", "高风险高收益"},
			RequiredVersion:    "3.4.0",
			CreatedAt:          "2025-12-01T00:00:00Z",
			UpdatedAt:          "2026-01-10T00:00:00Z",
		},
		{
			ID:                 "combo",
			Name:               "组合策略模块",
			Description:        "多策略组合管理，支持动态权重调整和市场自适应切换",
			Type:               "combo",
			RiskLevel:          "high",
			IsPremium:          true,
			IsEnabled:          false,
			IsLicensed:         false,
			Features:           getStrategyFeatures("combo"),
			MinCapital:         1000,
			RecommendedCapital: 5000,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"组合", "多策略", "自适应"},
			RequiredVersion:    "3.4.0",
			CreatedAt:          "2025-12-01T00:00:00Z",
			UpdatedAt:          "2026-01-10T00:00:00Z",
		},
		{
			ID:                 "trend_following",
			Name:               "趋势跟踪策略",
			Description:        "基于技术指标的趋势跟踪策略，在趋势形成时入场，趋势反转时离场",
			Type:               "trend",
			RiskLevel:          "medium",
			IsPremium:          true,
			IsEnabled:          false,
			IsLicensed:         false,
			Features:           getStrategyFeatures("trend_following"),
			MinCapital:         300,
			RecommendedCapital: 1000,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"趋势", "技术指标", "顺势交易"},
			RequiredVersion:    "3.4.0",
			CreatedAt:          "2025-06-01T00:00:00Z",
			UpdatedAt:          "2026-01-10T00:00:00Z",
		},
		{
			ID:                 "mean_reversion",
			Name:               "均值回归策略",
			Description:        "利用价格偏离均值后回归的特性进行交易",
			Type:               "mean_reversion",
			RiskLevel:          "medium",
			IsPremium:          true,
			IsEnabled:          false,
			IsLicensed:         false,
			Features:           getStrategyFeatures("mean_reversion"),
			MinCapital:         200,
			RecommendedCapital: 800,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"均值回归", "统计套利", "震荡市"},
			RequiredVersion:    "3.4.0",
			CreatedAt:          "2025-06-01T00:00:00Z",
			UpdatedAt:          "2026-01-10T00:00:00Z",
		},
		{
			ID:                 "breakout",
			Name:               "突破策略",
			Description:        "价格突破关键支撑/阻力位时入场，捕捉大幅波动",
			Type:               "breakout",
			RiskLevel:          "medium",
			IsPremium:          false,
			IsEnabled:          false,
			IsLicensed:         true,
			Features:           getStrategyFeatures("breakout"),
			MinCapital:         200,
			RecommendedCapital: 1000,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"突破", "阻力位", "动量"},
			RequiredVersion:    "3.4.0",
			CreatedAt:          "2025-08-01T00:00:00Z",
			UpdatedAt:          "2026-01-10T00:00:00Z",
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"strategies": strategies,
		"total":      len(strategies),
	})
}

// 获取策略详情
func getStrategyDetailHandler(c *gin.Context) {
	strategyID := c.Param("id")

	// 模拟策略详情
	detail := StrategyDetailInfo{
		StrategyInfo: StrategyInfo{
			ID:                 strategyID,
			Name:               getStrategyName(strategyID),
			Description:        getStrategyDescription(strategyID),
			Type:               getStrategyType(strategyID),
			RiskLevel:          "medium", // 简化
			IsPremium:          isStrategyPremium(strategyID),
			IsEnabled:          false,
			IsLicensed:         !isStrategyPremium(strategyID),
			Features:           getStrategyFeatures(strategyID),
			MinCapital:         200,
			RecommendedCapital: 1000,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               getStrategyTags(strategyID),
			RequiredVersion:    "3.4.0",
			CreatedAt:          "2024-01-01T00:00:00Z",
			UpdatedAt:          time.Now().Format(time.RFC3339),
		},
		Parameters:    getStrategyParameters(strategyID),
		Documentation: "详细文档请参考 https://docs.quantmesh.io/strategies/" + strategyID,
		Changelog: []ChangelogEntry{
			{
				Version: "1.0.0",
				Date:    "2024-01-01",
				Changes: []string{"初始版本发布"},
			},
		},
		Performance: StrategyPerformance{
			WinRate:        65.5,
			AvgProfit:      2.3,
			MaxDrawdown:    12.5,
			SharpeRatio:    1.85,
			TotalTrades:    1523,
			BacktestPeriod: "2023-01-01 至 2024-12-31",
			LastUpdated:    time.Now().Format(time.RFC3339),
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"strategy": detail,
	})
}

// 启用策略
func enableStrategyHandler(c *gin.Context) {
	strategyID := c.Param("id")

	// TODO: 实际实现启用策略逻辑

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"message":   "策略已启用",
		"isEnabled": true,
		"strategy": gin.H{
			"id":        strategyID,
			"isEnabled": true,
		},
	})
}

// 禁用策略
func disableStrategyHandler(c *gin.Context) {
	strategyID := c.Param("id")

	// TODO: 实际实现禁用策略逻辑

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"message":   "策略已禁用",
		"isEnabled": false,
		"strategy": gin.H{
			"id":        strategyID,
			"isEnabled": false,
		},
	})
}

// 获取策略授权信息
func getStrategyLicenseHandler(c *gin.Context) {
	strategyID := c.Param("id")

	// 非付费策略默认已授权
	if !isStrategyPremium(strategyID) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"license": StrategyLicense{
				StrategyID:   strategyID,
				Tier:         "free",
				ValidFrom:    "2024-01-01T00:00:00Z",
				ValidUntil:   "2099-12-31T23:59:59Z",
				IsActive:     true,
				MaxInstances: 999,
			},
		})
		return
	}

	// 付费策略检查授权
	// TODO: 从数据库查询实际授权信息
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"license": StrategyLicense{
			StrategyID:   strategyID,
			Tier:         "",
			ValidFrom:    "",
			ValidUntil:   "",
			IsActive:     false,
			MaxInstances: 0,
		},
		"message": "该策略需要购买授权",
	})
}

// 获取策略配置列表
func getStrategyConfigsHandler(c *gin.Context) {
	configs := []StrategyConfig{
		{
			StrategyID:    "grid",
			Enabled:       true,
			Priority:      1,
			MaxAllocation: 30.0,
			Parameters: map[string]interface{}{
				"gridCount":  10,
				"gridSpread": 1.0,
			},
		},
		{
			StrategyID:    "dca",
			Enabled:       true,
			Priority:      2,
			MaxAllocation: 20.0,
			Parameters: map[string]interface{}{
				"interval": "4h",
				"amount":   100,
			},
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"configs": configs,
	})
}

// 更新策略配置
func updateStrategyConfigHandler(c *gin.Context) {
	strategyID := c.Param("id")

	var config StrategyConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的配置数据: " + err.Error(),
		})
		return
	}

	config.StrategyID = strategyID

	// TODO: 保存配置到数据库

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "策略配置已更新",
	})
}

// 获取策略类型列表
func getStrategyTypesHandler(c *gin.Context) {
	types := []string{
		"grid",
		"dca",
		"martingale",
		"trend",
		"mean_reversion",
		"breakout",
		"combo",
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"types":   types,
	})
}

// 购买策略
func purchaseStrategyHandler(c *gin.Context) {
	strategyID := c.Param("id")

	var req struct {
		Tier string `json:"tier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的请求数据",
		})
		return
	}

	// TODO: 实际实现购买逻辑

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "策略购买成功",
		"license": StrategyLicense{
			StrategyID:   strategyID,
			Tier:         req.Tier,
			ValidFrom:    time.Now().Format(time.RFC3339),
			ValidUntil:   time.Now().AddDate(1, 0, 0).Format(time.RFC3339),
			IsActive:     true,
			MaxInstances: getTierInstances(req.Tier),
		},
	})
}

// 获取已启用的策略
func getEnabledStrategiesHandler(c *gin.Context) {
	strategies := []StrategyInfo{
		{
			ID:          "grid",
			Name:        "网格交易策略",
			Description: "经典网格交易策略",
			Type:        "grid",
			IsPremium:   false,
			IsEnabled:   true,
			IsLicensed:  true,
			Version:     "1.0.0",
			Author:      "QuantMesh",
			Tags:        []string{"网格", "震荡市"},
		},
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"strategies": strategies,
	})
}

// 批量更新策略
func batchUpdateStrategiesHandler(c *gin.Context) {
	var req struct {
		Updates []struct {
			StrategyID string `json:"strategyId"`
			Enabled    bool   `json:"enabled"`
		} `json:"updates"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "无效的请求数据",
		})
		return
	}

	// TODO: 实际实现批量更新逻辑

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "批量更新成功",
	})
}

// 辅助函数
func getStrategyName(id string) string {
	names := map[string]string{
		"grid":            "网格交易策略",
		"dca":             "DCA 定投策略",
		"dca_enhanced":    "增强型 DCA 策略",
		"martingale":      "马丁格尔策略",
		"combo":           "组合策略模块",
		"trend_following": "趋势跟踪策略",
		"mean_reversion":  "均值回归策略",
		"breakout":        "突破策略",
	}
	if name, ok := names[id]; ok {
		return name
	}
	return id
}

func getStrategyDescription(id string) string {
	descs := map[string]string{
		"grid":            "经典网格交易策略，在价格区间内自动挂单",
		"dca":             "定期定额买入策略",
		"dca_enhanced":    "增强型 DCA 策略，支持 ATR 动态间距",
		"martingale":      "马丁格尔加仓策略",
		"combo":           "多策略组合管理",
		"trend_following": "趋势跟踪策略",
		"mean_reversion":  "均值回归策略",
		"breakout":        "突破策略",
	}
	if desc, ok := descs[id]; ok {
		return desc
	}
	return "策略描述"
}

func getStrategyType(id string) string {
	if strings.Contains(id, "dca") {
		return "dca"
	}
	if strings.Contains(id, "grid") {
		return "grid"
	}
	if strings.Contains(id, "martingale") {
		return "martingale"
	}
	return id
}

func isStrategyPremium(id string) bool {
	premium := map[string]bool{
		"dca_enhanced":    true,
		"martingale":      true,
		"combo":           true,
		"trend_following": true,
		"mean_reversion":  true,
	}
	return premium[id]
}

func getStrategyTags(id string) []string {
	tags := map[string][]string{
		"grid":            {"网格", "震荡市", "自动化"},
		"dca":             {"定投", "长期", "低风险"},
		"dca_enhanced":    {"DCA", "ATR", "多层止盈"},
		"martingale":      {"马丁格尔", "补仓", "高风险"},
		"combo":           {"组合", "多策略", "自适应"},
		"trend_following": {"趋势", "顺势", "技术指标"},
		"mean_reversion":  {"均值回归", "统计套利"},
		"breakout":        {"突破", "动量"},
	}
	if t, ok := tags[id]; ok {
		return t
	}
	return []string{}
}

func getStrategyFeatures(id string) []string {
	features := map[string][]string{
		"grid": {
			"自动挂单买卖",
			"支持自定义网格数量",
			"支持动态网格间距",
		},
		"dca": {
			"定时定额买入",
			"分散入场成本",
			"自动复投收益",
		},
		"dca_enhanced": {
			"ATR 动态间距调整",
			"三级阶梯止盈",
			"50 层精细仓位管理",
			"瀑布保护机制",
			"趋势过滤器",
		},
		"martingale": {
			"亏损加倍补仓",
			"支持反向马丁",
			"风险削减模式",
			"多空双向支持",
		},
		"combo": {
			"多策略组合运行",
			"动态权重分配",
			"市场自适应切换",
			"风险对冲能力",
		},
	}
	if f, ok := features[id]; ok {
		return f
	}
	return []string{"基础功能"}
}

func getStrategyParameters(id string) []StrategyParameter {
	params := map[string][]StrategyParameter{
		"grid": {
			{Name: "gridCount", Type: "number", Default: 10, Min: 3, Max: 100, Description: "网格数量", Required: true, DisplayOrder: 1},
			{Name: "upperPrice", Type: "number", Default: 0, Description: "网格上限价格", Required: true, DisplayOrder: 2},
			{Name: "lowerPrice", Type: "number", Default: 0, Description: "网格下限价格", Required: true, DisplayOrder: 3},
			{Name: "totalAmount", Type: "number", Default: 1000, Description: "总投资金额", Required: true, DisplayOrder: 4},
		},
		"dca": {
			{Name: "interval", Type: "select", Default: "4h", Description: "定投间隔", Required: true, DisplayOrder: 1},
			{Name: "amount", Type: "number", Default: 100, Description: "每次投资金额", Required: true, DisplayOrder: 2},
		},
		"dca_enhanced": {
			{Name: "atrPeriod", Type: "number", Default: 14, Min: 5, Max: 50, Description: "ATR 计算周期", Required: true, DisplayOrder: 1},
			{Name: "atrMultiplier", Type: "number", Default: 1.5, Min: 0.5, Max: 5, Description: "ATR 乘数", Required: true, DisplayOrder: 2},
			{Name: "maxLayers", Type: "number", Default: 50, Min: 10, Max: 100, Description: "最大层数", Required: true, DisplayOrder: 3},
			{Name: "takeProfitLevels", Type: "array", Default: []float64{1.5, 3.0, 5.0}, Description: "止盈层级 (%)", Required: true, DisplayOrder: 4},
		},
	}
	if p, ok := params[id]; ok {
		return p
	}
	return []StrategyParameter{}
}

func getTierInstances(tier string) int {
	switch tier {
	case "basic":
		return 1
	case "pro":
		return 5
	case "enterprise":
		return 999
	default:
		return 1
	}
}
