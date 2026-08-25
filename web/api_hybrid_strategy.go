package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/logger"
)

// ============================================================================
// 混合策略 API
// ============================================================================

// getBuiltInRuleTemplates 获取内置规则模板
// GET /api/hybrid/rules/templates
func getBuiltInRuleTemplates(c *gin.Context) {
	templates := config.GetBuiltInRuleTemplates()
	c.JSON(http.StatusOK, gin.H{"templates": templates})
}

// GetHybridStrategyConfigRequest 获取混合策略配置请求
type GetHybridStrategyConfigRequest struct {
	BotID string `uri:"bot_id" binding:"required"`
}

// getHybridStrategyConfig 获取混合策略配置
// GET /api/bots/:id/hybrid-config
func getHybridStrategyConfig(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	// 加载 Bot 配置
	botConfig, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", err)
		return
	}

	// 检查是否是混合模式
	if botConfig.StrategyMode != "hybrid" || botConfig.HybridStrategy == nil {
		c.JSON(http.StatusOK, gin.H{
			"hybrid_mode": false,
			"message":    "Bot is not in hybrid mode",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"hybrid_mode": true,
		"config":      botConfig.HybridStrategy,
	})
}

// UpdateHybridStrategyRequest 更新混合策略请求
type UpdateHybridStrategyRequest struct {
	HybridStrategy *config.HybridStrategyConfig `json:"hybrid_strategy" binding:"required"`
}

// updateHybridStrategy 更新混合策略配置
// PUT /api/bots/:id/hybrid-config
func updateHybridStrategy(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	// 检查 Bot 是否正在运行
	if botManagerProvider() != nil {
		if bot, ok := botManagerProvider().GetBot(botID); ok && bot.Running {
			c.JSON(http.StatusConflict, gin.H{
				"error":     "bot_running",
				"error_key": "error.bot_running_cannot_update_hybrid",
				"message":   "Cannot update hybrid strategy configuration while bot is running",
			})
			return
		}
	}

	var req UpdateHybridStrategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 加载 Bot 配置
	botConfig, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", err)
		return
	}

	// 更新混合策略配置
	botConfig.HybridStrategy = req.HybridStrategy
	botConfig.StrategyMode = "hybrid"

	// 保存配置
	if err := botConfigManager.SaveBotConfig(botConfig); err != nil {
		logger.Error("保存混合策略配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	// 同步到主配置
	syncBotConfigToMain(botID, botConfig)

	logger.Info("✅ Bot %s 混合策略配置已更新", botID)
	c.JSON(http.StatusOK, gin.H{
		"ok":    true,
		"bot_id": botID,
	})
}

// EnableHybridModeRequest 启用混合模式请求
type EnableHybridModeRequest struct {
	Name             string                        `json:"name" binding:"required"`
	Description      string                        `json:"description,omitempty"`
	SubStrategies    []config.SubStrategyConfig    `json:"sub_strategies" binding:"required"`
	CollaborationRules []config.CollaborationRule `json:"collaboration_rules"`
}

// enableHybridMode 启用混合模式
// POST /api/bots/:id/enable-hybrid
func enableHybridMode(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	// 检查 Bot 是否正在运行
	if botManagerProvider() != nil {
		if bot, ok := botManagerProvider().GetBot(botID); ok && bot.Running {
			c.JSON(http.StatusConflict, gin.H{
				"error":     "bot_running",
				"error_key": "error.bot_running_cannot_enable_hybrid",
				"message":   "Cannot enable hybrid mode while bot is running",
			})
			return
		}
	}

	var req EnableHybridModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 加载 Bot 配置
	botConfig, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", err)
		return
	}

	// 创建混合策略配置
	hybridConfig := &config.HybridStrategyConfig{
		Name:               req.Name,
		Description:        req.Description,
		SubStrategies:      req.SubStrategies,
		CollaborationRules: req.CollaborationRules,
		GlobalSettings:     make(map[string]interface{}),
	}

	// 更新 Bot 配置
	botConfig.HybridStrategy = hybridConfig
	botConfig.StrategyMode = "hybrid"

	// 保存配置
	if err := botConfigManager.SaveBotConfig(botConfig); err != nil {
		logger.Error("保存混合策略配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	// 同步到主配置
	syncBotConfigToMain(botID, botConfig)

	logger.Info("✅ Bot %s 已启用混合模式", botID)
	c.JSON(http.StatusOK, gin.H{
		"ok":    true,
		"bot_id": botID,
		"mode":  "hybrid",
	})
}

// disableHybridMode 禁用混合模式
// POST /api/bots/:id/disable-hybrid
func disableHybridMode(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	// 检查 Bot 是否正在运行
	if botManagerProvider() != nil {
		if bot, ok := botManagerProvider().GetBot(botID); ok && bot.Running {
			c.JSON(http.StatusConflict, gin.H{
				"error":     "bot_running",
				"error_key": "error.bot_running_cannot_disable_hybrid",
				"message":   "Cannot disable hybrid mode while bot is running",
			})
			return
		}
	}

	// 加载 Bot 配置
	botConfig, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", err)
		return
	}

	// 检查是否是混合模式
	if botConfig.StrategyMode != "hybrid" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "not_in_hybrid_mode",
			"message": "Bot is not in hybrid mode",
		})
		return
	}

	// 转换为多策略模式
	if len(botConfig.HybridStrategy.SubStrategies) > 0 {
		// 保留主策略
		newStrategies := make([]config.BotStrategyConfig, 0)
		for _, subStrategy := range botConfig.HybridStrategy.SubStrategies {
			if subStrategy.Role == config.RolePrimary && subStrategy.Enabled {
				newStrategies = append(newStrategies, config.BotStrategyConfig{
					Type:     subStrategy.Type,
					Enabled:  subStrategy.Enabled,
					Weight:   subStrategy.Weight,
					Params:   subStrategy.Config,
					Settings: subStrategy.Metadata,
				})
			}
		}

		botConfig.Strategies = newStrategies
		if len(newStrategies) > 1 {
			botConfig.StrategyMode = "multi"
		} else {
			botConfig.StrategyMode = "single"
		}
	} else {
		botConfig.StrategyMode = "single"
	}

	botConfig.HybridStrategy = nil

	// 保存配置
	if err := botConfigManager.SaveBotConfig(botConfig); err != nil {
		logger.Error("保存配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	// 同步到主配置
	syncBotConfigToMain(botID, botConfig)

	logger.Info("✅ Bot %s 已禁用混合模式", botID)
	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"bot_id": botID,
		"mode":   botConfig.StrategyMode,
	})
}

// getHybridStrategyStatus 获取混合策略状态
// GET /api/bots/:id/hybrid-status
func getHybridStrategyStatus(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	// 加载 Bot 配置
	botConfig, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", err)
		return
	}

	isHybrid := botConfig.StrategyMode == "hybrid" && botConfig.HybridStrategy != nil

	status := gin.H{
		"bot_id":      botID,
		"hybrid_mode": isHybrid,
	}

	if isHybrid && botConfig.HybridStrategy != nil {
		status["sub_strategies_count"] = len(botConfig.HybridStrategy.SubStrategies)
		status["rules_count"] = len(botConfig.HybridStrategy.CollaborationRules)

		// 统计启用的子策略
		enabledCount := 0
		for _, sub := range botConfig.HybridStrategy.SubStrategies {
			if sub.Enabled {
				enabledCount++
			}
		}
		status["enabled_sub_strategies"] = enabledCount

		// 统计启用的规则
		enabledRules := 0
		for _, rule := range botConfig.HybridStrategy.CollaborationRules {
			if rule.Enabled {
				enabledRules++
			}
		}
		status["enabled_rules"] = enabledRules
	}

	c.JSON(http.StatusOK, status)
}
