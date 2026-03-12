package web

import (
	"net/http"
	"strings"
	"time"

	"quantmesh/config"

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

// StrategyParameter 策略参數
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
	WinRate        float64 `json:"winRate"`
	AvgProfit      float64 `json:"avgProfit"`
	MaxDrawdown    float64 `json:"maxDrawdown"`
	SharpeRatio    float64 `json:"sharpeRatio"`
	TotalTrades    int     `json:"totalTrades"`
	BacktestPeriod string  `json:"backtestPeriod"`
	LastUpdated    string  `json:"lastUpdated"`
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

// StrategyTemplate 策略組合模板
type StrategyTemplate struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"` // combo, hedge
	Strategies  []string `json:"strategies"`
	Weights     []float64 `json:"weights,omitempty"`
	Tags        []string `json:"tags"`
}

// getStrategyTemplatesHandler 獲取預設組合模板
// GET /api/strategies/templates
func getStrategyTemplatesHandler(c *gin.Context) {
	templates := []StrategyTemplate{
		{
			ID:          "grid_trend",
			Name:        "網格+趨勢",
			Description: "震盪時網格，突破時趨勢",
			Type:        "combo",
			Strategies:  []string{"grid", "trend_following"},
			Weights:     []float64{0.6, 0.4},
			Tags:        []string{"网格", "趋势", "震荡市", "突破"},
		},
		{
			ID:          "dca_martingale",
			Name:        "DCA+馬丁",
			Description: "穩健定投+極端行情補倉",
			Type:        "combo",
			Strategies:  []string{"dca", "martingale"},
			Weights:     []float64{0.7, 0.3},
			Tags:        []string{"DCA", "马丁", "定投", "补仓"},
		},
		{
			ID:          "futures_spot_hedge",
			Name:        "合約+現貨對沖",
			Description: "合約打底+現貨對沖",
			Type:        "hedge",
			Strategies:  []string{"grid", "grid"},
			Tags:        []string{"对冲", "合约", "现货", "跨市场"},
		},
		{
			ID:          "grid_spot_short_hedge",
			Name:        "網格+現貨做空對沖",
			Description: "合約網格做多 + 現貨借幣做空，對沖下跌風險",
			Type:        "hedge",
			Strategies:  []string{"grid", "spot_short"},
			Tags:        []string{"对冲", "合约", "现货做空", "借币"},
		},
		{
			ID:          "grid_spot_long_hedge",
			Name:        "網格+現貨做多對沖",
			Description: "合約網格做空 + 現貨買入持倉，對沖上漲風險",
			Type:        "hedge",
			Strategies:  []string{"grid", "spot_long"},
			Tags:        []string{"对冲", "合约", "现货做多", "做空网格"},
		},
		{
			ID:          "spot_grid_futures_hedge",
			Name:        "現貨網格+合約對沖",
			Description: "現貨網格做多 + 合約做空對沖，降低下跌風險",
			Type:        "hedge",
			Strategies:  []string{"grid", "futures_short"},
			Tags:        []string{"对冲", "现货", "合约", "现货主腿"},
		},
	}
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"templates": templates,
		"total":     len(templates),
	})
}

// 獲取所有可用策略
func getStrategiesHandler(c *gin.Context) {
	// 獲取當前配置以确定策略是否啟用
	var enabledMap = make(map[string]bool)
	if globalConfig != nil {
		cfg := globalConfig
		if cfg != nil {
			for id, sc := range cfg.Strategies.Configs {
				enabledMap[id] = sc.Enabled
			}
		}
	}

	// 策略列表（包含内置策略和插件策略）
	strategies := []StrategyInfo{
		{
			ID:                 "grid",
			Name:               "网格交易策略",
			Description:        "經典网格交易策略，在價格区间内自动挂單，赚取波动差價",
			Type:               "grid",
			RiskLevel:          "low",
			IsPremium:          false,
			IsEnabled:          enabledMap["grid"],
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
			Description:        "定期定額買入策略，分散入场成本，降低投资风險",
			Type:               "dca",
			RiskLevel:          "low",
			IsPremium:          false,
			IsEnabled:          enabledMap["dca"],
			IsLicensed:         true,
			Features:           getStrategyFeatures("dca"),
			MinCapital:         100,
			RecommendedCapital: 500,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"定投", "长期", "低风險"},
			RequiredVersion:    "3.0.0",
			CreatedAt:          "2024-01-15T00:00:00Z",
			UpdatedAt:          "2024-07-01T00:00:00Z",
		},
		{
			ID:                 "dca_enhanced",
			Name:               "增强型 DCA 策略",
			Description:        "基於 ATR 动態间距、三级止盈、50层倉位管理、瀑布保护和趨勢過濾的增强型 DCA",
			Type:               "dca",
			RiskLevel:          "medium",
			IsPremium:          false,
			IsEnabled:          enabledMap["dca_enhanced"],
			IsLicensed:         true,
			Features:           getStrategyFeatures("dca_enhanced"),
			MinCapital:         200,
			RecommendedCapital: 1000,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"DCA", "ATR", "多层止盈", "风險管理"},
			RequiredVersion:    "3.4.0",
			CreatedAt:          "2025-12-01T00:00:00Z",
			UpdatedAt:          "2026-01-10T00:00:00Z",
		},
		{
			ID:                 "martingale",
			Name:               "马丁格尔策略",
			Description:        "亏损加倍补倉策略，支援正向/反向马丁、风險削减 and 多空双向",
			Type:               "martingale",
			RiskLevel:          "high",
			IsPremium:          false,
			IsEnabled:          enabledMap["martingale"],
			IsLicensed:         true,
			Features:           getStrategyFeatures("martingale"),
			MinCapital:         500,
			RecommendedCapital: 2000,
			Version:            "1.0.0",
			Author:             "QuantMesh",
			Tags:               []string{"马丁格尔", "补倉", "高风險高收益"},
			RequiredVersion:    "3.4.0",
			CreatedAt:          "2025-12-01T00:00:00Z",
			UpdatedAt:          "2026-01-10T00:00:00Z",
		},
		{
			ID:                 "combo",
			Name:               "组合策略模塊",
			Description:        "多策略组合管理，支援动態权重調整和市场自适应切换",
			Type:               "combo",
			RiskLevel:          "high",
			IsPremium:          false,
			IsEnabled:          enabledMap["combo"],
			IsLicensed:         true,
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
			Description:        "基於技术指標的趋势跟踪策略，在趋势形成時入场，趋势反轉時离场",
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
			Tags:               []string{"趋势", "技术指標", "顺势交易"},
			RequiredVersion:    "3.4.0",
			CreatedAt:          "2025-06-01T00:00:00Z",
			UpdatedAt:          "2026-01-10T00:00:00Z",
		},
		{
			ID:                 "mean_reversion",
			Name:               "均值回归策略",
			Description:        "利用價格偏离均值后回归的特性進行交易",
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
			Description:        "價格突破关键支撑/阻力位時入场，捕捉大幅波动",
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

// 獲取策略详情
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
		Documentation: "详细文檔请参考 https://docs.quantmesh.io/strategies/" + strategyID,
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

// 啟用策略
func enableStrategyHandler(c *gin.Context) {
	strategyID := c.Param("id")

	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	if globalConfig == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置未初始化"})
		return
	}

	cfg := globalConfig
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取配置失败"})
		return
	}

	// 确保 configs 不為 nil
	if cfg.Strategies.Configs == nil {
		cfg.Strategies.Configs = make(map[string]config.StrategyConfig)
	}

	// 更新或創建配置
	sc := cfg.Strategies.Configs[strategyID]
	sc.Enabled = true
	if sc.Type == "" {
		sc.Type = getStrategyType(strategyID)
	}
	if sc.Weight == 0 {
		sc.Weight = 0.1 // 默认权重
	}
	cfg.Strategies.Configs[strategyID] = sc

	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"message":   "策略已啟用",
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

	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	if globalConfig == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置未初始化"})
		return
	}

	cfg := globalConfig
	if cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取配置失败"})
		return
	}

	if cfg.Strategies.Configs != nil {
		if sc, ok := cfg.Strategies.Configs[strategyID]; ok {
			sc.Enabled = false
			cfg.Strategies.Configs[strategyID] = sc

			if err := fileConfigManager.UpdateConfig(cfg); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败: " + err.Error()})
				return
			}
		}
	}

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

// 獲取策略授权信息
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
	// TODO: 從數據库查詢實際授权信息
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
		"message": "該策略需要购買授权",
	})
}

// 獲取策略配置列表
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

	var reqConfig StrategyConfig
	if err := c.ShouldBindJSON(&reqConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的配置數據: " + err.Error(),
		})
		return
	}

	reqConfig.StrategyID = strategyID

	// TODO: 保存配置到數據库
	if globalConfig != nil {
		cfg := globalConfig
		if cfg != nil {
			if cfg.Strategies.Configs == nil {
				cfg.Strategies.Configs = make(map[string]config.StrategyConfig)
			}
			sc := cfg.Strategies.Configs[strategyID]
			sc.Enabled = reqConfig.Enabled
			sc.Weight = reqConfig.MaxAllocation / 100.0 // 假设 maxAllocation 是百分比
			sc.Config = reqConfig.Parameters
			cfg.Strategies.Configs[strategyID] = sc

			if err := fileConfigManager.UpdateConfig(cfg); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败: " + err.Error()})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "策略配置已更新",
	})
}

// 獲取策略類型列表
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

// 购買策略
func purchaseStrategyHandler(c *gin.Context) {
	strategyID := c.Param("id")

	var req struct {
		Tier string `json:"tier"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據",
		})
		return
	}

	// TODO: 實際實現购買逻辑

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "策略购買成功",
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

// 獲取已啟用的策略
func getEnabledStrategiesHandler(c *gin.Context) {
	strategies := []StrategyInfo{
		{
			ID:          "grid",
			Name:        "网格交易策略",
			Description: "經典网格交易策略",
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
			"message": "無效的请求數據",
		})
		return
	}

	// TODO: 實際實現批量更新逻辑

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "批量更新成功",
	})
}

// 辅助函數
func getStrategyName(id string) string {
	names := map[string]string{
		"grid":            "网格交易策略",
		"dca":             "DCA 定投策略",
		"dca_enhanced":    "增强型 DCA 策略",
		"martingale":      "马丁格尔策略",
		"combo":           "组合策略模塊",
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
		"grid":            "經典网格交易策略，在價格区间内自动挂單",
		"dca":             "定期定額買入策略",
		"dca_enhanced":    "增强型 DCA 策略，支援 ATR 动態间距",
		"martingale":      "马丁格尔加倉策略",
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
		"trend_following": true,
		"mean_reversion":  true,
	}
	return premium[id]
}

func getStrategyTags(id string) []string {
	tags := map[string][]string{
		"grid":            {"网格", "震荡市", "自动化"},
		"dca":             {"定投", "长期", "低风險"},
		"dca_enhanced":    {"DCA", "ATR", "多层止盈"},
		"martingale":      {"马丁格尔", "补倉", "高风險"},
		"combo":           {"组合", "多策略", "自适应"},
		"trend_following": {"趋势", "顺势", "技术指標"},
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
			"自动挂單買賣",
			"支援自定义网格數量",
			"支援动態网格间距",
		},
		"dca": {
			"定時定額買入",
			"分散入场成本",
			"自动複投收益",
		},
		"dca_enhanced": {
			"ATR 动態间距調整",
			"三级阶梯止盈",
			"50 层精细倉位管理",
			"瀑布保护机制",
			"趨勢過濾器",
		},
		"martingale": {
			"亏损加倍补倉",
			"支援反向马丁",
			"风險削减模式",
			"多空双向支援",
		},
		"combo": {
			"多策略组合运行",
			"动態权重分配",
			"市场自适应切换",
			"风險對冲能力",
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
			{Name: "gridCount", Type: "number", Default: 10, Min: 3, Max: 100, Description: "网格數量", Required: true, DisplayOrder: 1},
			{Name: "upperPrice", Type: "number", Default: 0, Description: "网格上限價格", Required: true, DisplayOrder: 2},
			{Name: "lowerPrice", Type: "number", Default: 0, Description: "网格下限價格", Required: true, DisplayOrder: 3},
			{Name: "totalAmount", Type: "number", Default: 1000, Description: "總投资金額", Required: true, DisplayOrder: 4},
		},
		"dca": {
			{Name: "interval", Type: "select", Default: "4h", Description: "定投间隔", Required: true, DisplayOrder: 1},
			{Name: "amount", Type: "number", Default: 100, Description: "每次投资金額", Required: true, DisplayOrder: 2},
		},
		"dca_enhanced": {
			{Name: "base_order_amount", Type: "number", Default: 100.0, Min: 10.0, Description: "基础订單金額 (USDT)", Required: true, DisplayOrder: 1},
			{Name: "safety_order_amount", Type: "number", Default: 200.0, Min: 10.0, Description: "安全订單金額 (USDT)", Required: true, DisplayOrder: 2},
			{Name: "max_safety_orders", Type: "number", Default: 50, Min: 1, Max: 50, Description: "最大安全订單數", Required: true, DisplayOrder: 3},
			{Name: "atr_period", Type: "number", Default: 14, Min: 5, Max: 50, Description: "ATR 周期", Required: true, DisplayOrder: 4},
			{Name: "atr_multiplier", Type: "number", Default: 1.5, Min: 0.5, Max: 5.0, Description: "ATR 乘數", Required: true, DisplayOrder: 5},
			{Name: "total_take_profit", Type: "number", Default: 2.0, Min: 0.1, Description: "全倉止盈比例 (%)", Required: true, DisplayOrder: 6},
			{Name: "stop_loss", Type: "number", Default: 10.0, Min: 0.1, Description: "止损比例 (%)", Required: true, DisplayOrder: 7},
		},
		"martingale": {
			{Name: "initial_amount", Type: "number", Default: 100.0, Min: 10.0, Description: "初始金額 (USDT)", Required: true, DisplayOrder: 1},
			{Name: "multiplier", Type: "number", Default: 2.0, Min: 1.1, Max: 5.0, Description: "加倉倍數", Required: true, DisplayOrder: 2},
			{Name: "max_levels", Type: "number", Default: 6, Min: 1, Max: 20, Description: "最大层數", Required: true, DisplayOrder: 3},
			{Name: "price_step", Type: "number", Default: 2.0, Min: 0.1, Max: 50.0, Description: "加倉间距 (%)", Required: true, DisplayOrder: 4},
			{Name: "take_profit", Type: "number", Default: 3.0, Min: 0.1, Description: "止盈比例 (%)", Required: true, DisplayOrder: 5},
			{Name: "direction", Type: "select", Default: "LONG", Description: "方向 (LONG/SHORT)", Required: true, DisplayOrder: 6},
		},
		"combo": {
			{Name: "total_capital", Type: "number", Default: 10000.0, Min: 100.0, Description: "總资金 (USDT)", Required: true, DisplayOrder: 1},
			{Name: "market_detection", Type: "boolean", Default: true, Description: "啟用市况检测", Required: true, DisplayOrder: 2},
			{Name: "hedge_enabled", Type: "boolean", Default: true, Description: "啟用對冲", Required: true, DisplayOrder: 3},
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

// ========== 策略運行狀態 API ==========

// StrategyRuntimeStatusResponse 策略運行狀態響應
type StrategyRuntimeStatusResponse struct {
	Name              string                 `json:"name"`
	Type              string                 `json:"type"`
	IsEnabled         bool                   `json:"isEnabled"`
	IsRunning         bool                   `json:"isRunning"`
	Weight            float64                `json:"weight"`
	AllocatedFunds    float64                `json:"allocatedFunds"`
	UsedFunds         float64                `json:"usedFunds"`
	AvailableFunds    float64                `json:"availableFunds"`
	PositionCount     int                    `json:"positionCount"`
	OrderCount        int                    `json:"orderCount"`
	Statistics        *StrategyStatsResponse `json:"statistics"`
	Positions         []StrategyPositionResp `json:"positions,omitempty"`
	Orders            []StrategyOrderResp    `json:"orders,omitempty"`
	VisualizationData map[string]interface{} `json:"visualizationData,omitempty"` // 新增：策略可视化數據
}

// StrategyStatsResponse 策略統計響應
type StrategyStatsResponse struct {
	TotalTrades int     `json:"totalTrades"`
	WinRate     float64 `json:"winRate"`
	TotalPnL    float64 `json:"totalPnL"`
	TotalVolume float64 `json:"totalVolume"`
}

// StrategyPositionResp 策略持倉響應
type StrategyPositionResp struct {
	Symbol       string  `json:"symbol"`
	Size         float64 `json:"size"`
	EntryPrice   float64 `json:"entryPrice"`
	CurrentPrice float64 `json:"currentPrice"`
	PnL          float64 `json:"pnl"`
}

// StrategyOrderResp 策略訂單響應
type StrategyOrderResp struct {
	OrderID  int64   `json:"orderId"`
	Symbol   string  `json:"symbol"`
	Side     string  `json:"side"`
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
	Status   string  `json:"status"`
}

// SymbolStrategyRuntimeItem 單個幣種下的策略運行狀態聚合（用於 GET /api/strategies/runtime/all）
type SymbolStrategyRuntimeItem struct {
	Exchange    string                         `json:"exchange"`
	Symbol      string                         `json:"symbol"`
	MarketType  string                         `json:"marketType"`
	Strategies  []StrategyRuntimeStatusResponse `json:"strategies"`
}

// StrategyRuntimeProvider 策略運行時提供者接口
type StrategyRuntimeProvider interface {
	GetAllStrategyStatus(exchange, symbol string) ([]StrategyRuntimeStatusResponse, error)
	GetAllStrategyStatusAll() ([]SymbolStrategyRuntimeItem, error)
	GetStrategyStatus(exchange, symbol, strategyName string) (*StrategyRuntimeStatusResponse, error)
}

var strategyRuntimeProvider StrategyRuntimeProvider

// RegisterStrategyRuntimeProvider 注册策略運行時提供者
func RegisterStrategyRuntimeProvider(provider StrategyRuntimeProvider) {
	strategyRuntimeProvider = provider
}

func isStrategyRuntimeNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// 运行态不存在属于可预期业务状态，不应返回 500。
	return strings.Contains(msg, "未找到") || strings.Contains(msg, "not found")
}

// getStrategyRuntimeStatusHandler 獲取所有策略的運行狀態
func getStrategyRuntimeStatusHandler(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchange == "" || symbol == "" {
		// 嘗試從配置獲取默認值
		if globalConfig != nil {
			cfg := globalConfig
			if cfg != nil && len(cfg.Trading.Symbols) > 0 {
				exchange = cfg.Trading.Symbols[0].Exchange
				symbol = cfg.Trading.Symbols[0].Symbol
			}
		}
	}

	if strategyRuntimeProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"strategies": []StrategyRuntimeStatusResponse{},
			"message":    "策略運行時提供者未初始化",
		})
		return
	}

	statuses, err := strategyRuntimeProvider.GetAllStrategyStatus(exchange, symbol)
	if err != nil {
		if isStrategyRuntimeNotFoundError(err) {
			c.JSON(http.StatusOK, gin.H{
				"success":    true,
				"strategies": []StrategyRuntimeStatusResponse{},
				"exchange":   exchange,
				"symbol":     symbol,
				"message":    "策略運行時未找到",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "獲取策略狀態失敗: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"strategies": statuses,
		"exchange":   exchange,
		"symbol":     symbol,
	})
}

// getStrategyRuntimeStatusByIDHandler 獲取單個策略的運行狀態
func getStrategyRuntimeStatusByIDHandler(c *gin.Context) {
	strategyID := c.Param("id")
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchange == "" || symbol == "" {
		// 嘗試從配置獲取默認值
		if globalConfig != nil {
			cfg := globalConfig
			if cfg != nil && len(cfg.Trading.Symbols) > 0 {
				exchange = cfg.Trading.Symbols[0].Exchange
				symbol = cfg.Trading.Symbols[0].Symbol
			}
		}
	}

	if strategyRuntimeProvider == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "策略運行時提供者未初始化",
		})
		return
	}

	status, err := strategyRuntimeProvider.GetStrategyStatus(exchange, symbol, strategyID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "獲取策略狀態失敗: " + err.Error(),
		})
		return
	}

	if status == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": "策略未找到",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"strategy": status,
	})
}

// getStrategyRuntimeAllHandler 獲取所有幣種下所有策略的運行狀態（聚合）
func getStrategyRuntimeAllHandler(c *gin.Context) {
	if strategyRuntimeProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    []SymbolStrategyRuntimeItem{},
			"message": "策略運行時提供者未初始化",
		})
		return
	}

	data, err := strategyRuntimeProvider.GetAllStrategyStatusAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "獲取策略狀態失敗: " + err.Error(),
		})
		return
	}

	if data == nil {
		data = []SymbolStrategyRuntimeItem{}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    data,
	})
}
