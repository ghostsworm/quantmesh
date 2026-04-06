package web

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

// ValidationResult 配置验证结果
type ValidationResult struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// getBotConfigValidation 验证 Bot 配置
// GET /api/bots/:id/validate
func getBotConfigValidation(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	// 加载配置文件
	botConfig, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		c.JSON(http.StatusOK, ValidationResult{
			Valid: false,
			Errors: []string{"配置文件加载失败: " + err.Error()},
		})
		return
	}

	// 验证配置
	result := validateBotConfig(botConfig)

	c.JSON(http.StatusOK, result)
}

// validateBotConfig 验证 Bot 配置的完整性
func validateBotConfig(cfg *config.BotConfigFile) ValidationResult {
	result := ValidationResult{
		Valid:    true,
		Errors:   make([]string, 0),
		Warnings: make([]string, 0),
	}

	// 验证基础字段
	if cfg.BotID == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "Bot ID 不能为空")
	}

	if cfg.Name == "" {
		result.Warnings = append(result.Warnings, "Bot 名称为空")
	}

	if cfg.Exchange == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "交易所不能为空")
	}

	if cfg.Symbol == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "交易对不能为空")
	}

	// 验证市场类型
	if !config.ValidMarketType(cfg.MarketType) {
		result.Valid = false
		result.Errors = append(result.Errors, "市场类型必须是 spot、futures 或 funding_carry")
	}

	// 验证策略配置
	if len(cfg.Strategies) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "至少需要一个策略")
	}

	totalWeight := 0.0
	for i, strategy := range cfg.Strategies {
		if strategy.Type == "" {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("策略 %d 类型不能为空", i))
		}

		if strategy.Weight <= 0 {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("策略 %d 权重必须大于 0", i))
		}

		if strategy.Enabled {
			totalWeight += strategy.Weight
		}
	}

	// 验证权重总和
	if len(cfg.Strategies) > 1 && totalWeight <= 0 {
		result.Warnings = append(result.Warnings, "所有策略都被禁用")
	}

	// 验证资金配置
	if cfg.Capital.TotalAllocated <= 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "总分配资金必须大于 0")
	}

	// 验证网格配置（如果是网格策略）
	hasGridStrategy := false
	for _, s := range cfg.Strategies {
		if s.Type == "grid" && s.Enabled {
			hasGridStrategy = true
			break
		}
	}

	if hasGridStrategy {
		if cfg.Grid.PriceInterval <= 0 {
			result.Valid = false
			result.Errors = append(result.Errors, "价格间隔必须大于 0")
		}

		if cfg.Grid.OrderQuantity <= 0 {
			result.Valid = false
			result.Errors = append(result.Errors, "订单金额必须大于 0")
		}

		if cfg.Grid.BuyWindowSize <= 0 {
			result.Valid = false
			result.Errors = append(result.Errors, "买入窗口必须大于 0")
		}

		if cfg.Grid.SellWindowSize <= 0 {
			result.Valid = false
			result.Errors = append(result.Errors, "卖出窗口必须大于 0")
		}
	}

	// 验证风控配置
	if cfg.RiskControl.MaxDrawdownRatio < 0 || cfg.RiskControl.MaxDrawdownRatio > 1 {
		result.Warnings = append(result.Warnings, "最大回撤比例建议在 0-1 之间")
	}

	if cfg.RiskControl.StopLossRatio < 0 || cfg.RiskControl.StopLossRatio > 1 {
		result.Warnings = append(result.Warnings, "止损比例建议在 0-1 之间")
	}

	// 验证对冲配置
	if cfg.Hedge != nil {
		if cfg.Hedge.GroupID == "" {
			result.Warnings = append(result.Warnings, "对冲组 ID 为空")
		}

		if cfg.Hedge.HedgeRatio <= 0 || cfg.Hedge.HedgeRatio > 1 {
			result.Valid = false
			result.Errors = append(result.Errors, "对冲比例必须在 0-1 之间")
		}

		if cfg.Hedge.Role != "futures" && cfg.Hedge.Role != "spot" {
			result.Valid = false
			result.Errors = append(result.Errors, "对冲角色必须是 futures 或 spot")
		}
	}

	return result
}
