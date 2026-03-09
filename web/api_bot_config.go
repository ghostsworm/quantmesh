package web

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/logger"
)

var botConfigManager *config.BotConfigManager

// InitBotConfigManager 初始化 Bot 配置管理器
func InitBotConfigManager(baseDir string) error {
	var err error
	botConfigManager, err = config.NewBotConfigManager(baseDir)
	if err != nil {
		return err
	}
	logger.Info("✅ Bot 配置管理器已初始化，目录: %s", botConfigManager.GetBotDataPath(""))
	return nil
}

// BotConfigFileResponse Bot 配置文件响应
type BotConfigFileResponse struct {
	BotID      string              `json:"bot_id"`
	Name       string              `json:"name"`
	Exchange   string              `json:"exchange"`
	Symbol     string              `json:"symbol"`
	MarketType string              `json:"market_type"`
	Config     *config.BotConfigFile `json:"config"`
	Exists     bool                `json:"exists"`
}

// getBotConfigFile 获取 Bot 配置文件
// GET /api/bots/:id/config-file
func getBotConfigFile(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	// 检查配置文件是否存在
	exists := botConfigManager.BotConfigExists(botID)

	var botConfig *config.BotConfigFile
	if exists {
		var err error
		botConfig, err = botConfigManager.LoadBotConfig(botID)
		if err != nil {
			logger.Error("加载 Bot 配置文件失败: %v", err)
			respondError(c, http.StatusInternalServerError, "error.config_load_failed", err)
			return
		}
	} else {
		// 如果配置文件不存在，尝试从主配置中获取并创建
		cfg, err := GetLatestConfig()
		if err != nil || cfg == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Bot config file not found and cannot be created from main config",
			})
			return
		}

		// 查找 Bot
		for _, b := range cfg.Bots {
			id := b.ID
			if id == "" {
				id = config.GenerateBotID(b.Exchange, b.Symbol, b.GetMarketType())
			}
			if id == botID {
				// 转换为新的配置文件格式
				botConfig = config.ConvertFromBotConfig(b)
				botConfig.CreatedAt = time.Now().Format(time.RFC3339)
				botConfig.UpdatedAt = time.Now().Format(time.RFC3339)

				// 保存配置文件
				if err := botConfigManager.CreateBotConfig(botConfig); err != nil {
					logger.Error("创建 Bot 配置文件失败: %v", err)
					// 不返回错误，继续返回内存中的配置
				} else {
					logger.Info("✅ 为 Bot %s 创建了独立配置文件", botID)
					exists = true
				}
				break
			}
		}

		if botConfig == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
			return
		}
	}

	c.JSON(http.StatusOK, BotConfigFileResponse{
		BotID:      botID,
		Name:       botConfig.Name,
		Exchange:   botConfig.Exchange,
		Symbol:     botConfig.Symbol,
		MarketType: botConfig.MarketType,
		Config:     botConfig,
		Exists:     exists,
	})
}

// putBotConfigFile 更新 Bot 配置文件
// PUT /api/bots/:id/config-file
func putBotConfigFile(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	var req config.BotConfigFile
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 验证 Bot ID
	req.BotID = botID
	req.UpdatedAt = time.Now().Format(time.RFC3339)

	// 检查 Bot 是否正在运行
	if botManagerProvider != nil {
		if bot, ok := botManagerProvider.GetBot(botID); ok && bot.Running {
			// 运行中的 Bot 只允许修改部分参数
			c.JSON(http.StatusConflict, gin.H{
				"error":     "bot_running",
				"error_key": "error.bot_running_cannot_update_full_config",
				"message":   "Cannot update full config while bot is running. Use /api/bots/:id/strategy for strategy updates.",
			})
			return
		}
	}

	// 保存配置文件
	existed := botConfigManager.BotConfigExists(botID)
	if err := botConfigManager.SaveBotConfig(&req); err != nil {
		logger.Error("保存 Bot 配置文件失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	// 同步更新主配置文件
	if configManager != nil {
		cfg, err := GetLatestConfig()
		if err == nil && cfg != nil {
			// 更新主配置中的 Bot
			found := false
			for i := range cfg.Bots {
				id := cfg.Bots[i].ID
				if id == "" {
					id = config.GenerateBotID(cfg.Bots[i].Exchange, cfg.Bots[i].Symbol, cfg.Bots[i].GetMarketType())
				}
				if id == botID {
					// 转换并更新
					updatedBot := config.ConvertToBotConfig(&req)
					cfg.Bots[i] = updatedBot
					found = true
					break
				}
			}

			// 如果是新 Bot，添加到主配置
			if !found && !existed {
				newBot := config.ConvertToBotConfig(&req)
				cfg.Bots = append(cfg.Bots, newBot)
			}

			// 保存主配置
			if err := fileConfigManager.UpdateConfig(cfg); err != nil {
				logger.Error("同步主配置失败: %v", err)
			}
		}
	}

	logger.Info("✅ Bot %s 配置文件已更新", botID)
	action := "created"
	if existed {
		action = "updated"
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"bot_id": botID,
		"action": action,
	})
}

// deleteBotConfigFile 删除 Bot 配置文件
// DELETE /api/bots/:id/config-file
func deleteBotConfigFile(c *gin.Context) {
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
	if botManagerProvider != nil {
		if bot, ok := botManagerProvider.GetBot(botID); ok && bot.Running {
			c.JSON(http.StatusConflict, gin.H{
				"error":     "bot_running",
				"error_key": "error.bot_running_cannot_delete_config",
				"message":   "Cannot delete config while bot is running. Stop the bot first.",
			})
			return
		}
	}

	// 检查 Bot 是否属于对冲组
	if configManager != nil {
		cfg, err := GetLatestConfig()
		if err == nil && cfg != nil {
			if groupName := findGroupNameByBotID(cfg, botID); groupName != "" {
				c.JSON(http.StatusForbidden, gin.H{
					"error":      "bot_in_hedge_group",
					"error_key":  "error.bot_in_hedge_group_cannot_delete_config",
					"group_name": groupName,
				})
				return
			}
		}
	}

	if !botConfigManager.BotConfigExists(botID) {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot config file not found"})
		return
	}

	if err := botConfigManager.DeleteBotConfig(botID); err != nil {
		logger.Error("删除 Bot 配置文件失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_delete_failed", err)
		return
	}

	logger.Info("✅ Bot %s 配置文件已删除", botID)
	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"bot_id": botID,
	})
}

// StrategyConfigUpdateRequest 策略配置更新请求
type StrategyConfigUpdateRequest struct {
	StrategyIndex int                          `json:"strategy_index"` // 策略索引（多策略时）
	Strategy      config.BotStrategyConfig    `json:"strategy"`       // 策略配置
}

// putBotStrategyConfig 更新 Bot 的单个策略配置
// PUT /api/bots/:id/strategy-config
func putBotStrategyConfig(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	var req StrategyConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 加载配置文件
	botConfig, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", err)
		return
	}

	// 验证策略索引
	if req.StrategyIndex < 0 || req.StrategyIndex >= len(botConfig.Strategies) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "invalid_strategy_index",
			"error_key": "error.invalid_strategy_index",
			"message":   "Strategy index out of range",
		})
		return
	}

	// 检查 Bot 是否正在运行（允许运行时修改参数，但不允许修改策略类型）
	if botManagerProvider != nil {
		if bot, ok := botManagerProvider.GetBot(botID); ok && bot.Running {
			oldType := botConfig.Strategies[req.StrategyIndex].Type
			newType := req.Strategy.Type
			if oldType != newType {
				c.JSON(http.StatusConflict, gin.H{
					"error":     "bot_running",
					"error_key": "error.bot_running_cannot_change_strategy_type",
					"message":   "Cannot change strategy type while bot is running.",
				})
				return
			}
		}
	}

	// 更新策略配置
	botConfig.Strategies[req.StrategyIndex] = req.Strategy
	botConfig.UpdatedAt = time.Now().Format(time.RFC3339)

	// 保存配置文件
	if err := botConfigManager.SaveBotConfig(botConfig); err != nil {
		logger.Error("保存 Bot 配置文件失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	// 同步更新主配置
	syncBotConfigToMain(botID, botConfig)

	logger.Info("✅ Bot %s 策略 %d 配置已更新", botID, req.StrategyIndex)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"bot_id":        botID,
		"strategy_index": req.StrategyIndex,
	})
}

// addBotStrategy 添加策略（多策略模式）
// POST /api/bots/:id/strategies
func addBotStrategy(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	var strategy config.BotStrategyConfig
	if err := c.ShouldBindJSON(&strategy); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 加载配置文件
	botConfig, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", err)
		return
	}

	// 检查 Bot 是否正在运行
	if botManagerProvider != nil {
		if bot, ok := botManagerProvider.GetBot(botID); ok && bot.Running {
			c.JSON(http.StatusConflict, gin.H{
				"error":     "bot_running",
				"error_key": "error.bot_running_cannot_add_strategy",
				"message":   "Cannot add strategy while bot is running. Stop the bot first.",
			})
			return
		}
	}

	// 添加策略
	botConfig.Strategies = append(botConfig.Strategies, strategy)
	if len(botConfig.Strategies) > 1 {
		botConfig.StrategyMode = "multi"
	}
	botConfig.UpdatedAt = time.Now().Format(time.RFC3339)

	// 保存配置文件
	if err := botConfigManager.SaveBotConfig(botConfig); err != nil {
		logger.Error("保存 Bot 配置文件失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	// 同步更新主配置
	syncBotConfigToMain(botID, botConfig)

	logger.Info("✅ Bot %s 已添加策略 %s", botID, strategy.Type)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"bot_id":        botID,
		"strategy_type": strategy.Type,
		"strategy_count": len(botConfig.Strategies),
	})
}

// removeBotStrategy 移除策略
// DELETE /api/bots/:id/strategies/:index
func removeBotStrategy(c *gin.Context) {
	botID := c.Param("id")
	strategyIndex := c.Param("index")

	if botID == "" || strategyIndex == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_params")
		return
	}

	if botConfigManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	// 解析策略索引
	var index int
	if _, err := fmt.Sscanf(strategyIndex, "%d", &index); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_strategy_index")
		return
	}

	// 加载配置文件
	botConfig, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", err)
		return
	}

	// 检查 Bot 是否正在运行
	if botManagerProvider != nil {
		if bot, ok := botManagerProvider.GetBot(botID); ok && bot.Running {
			c.JSON(http.StatusConflict, gin.H{
				"error":     "bot_running",
				"error_key": "error.bot_running_cannot_remove_strategy",
				"message":   "Cannot remove strategy while bot is running. Stop the bot first.",
			})
			return
		}
	}

	// 验证索引
	if index < 0 || index >= len(botConfig.Strategies) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "invalid_strategy_index",
			"error_key": "error.invalid_strategy_index",
		})
		return
	}

	// 至少保留一个策略
	if len(botConfig.Strategies) <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "cannot_remove_last_strategy",
			"error_key": "error.cannot_remove_last_strategy",
		})
		return
	}

	// 移除策略
	removedType := botConfig.Strategies[index].Type
	botConfig.Strategies = append(botConfig.Strategies[:index], botConfig.Strategies[index+1:]...)
	if len(botConfig.Strategies) == 1 {
		botConfig.StrategyMode = "single"
	}
	botConfig.UpdatedAt = time.Now().Format(time.RFC3339)

	// 保存配置文件
	if err := botConfigManager.SaveBotConfig(botConfig); err != nil {
		logger.Error("保存 Bot 配置文件失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	// 同步更新主配置
	syncBotConfigToMain(botID, botConfig)

	logger.Info("✅ Bot %s 已移除策略 %s (索引 %d)", botID, removedType, index)
	c.JSON(http.StatusOK, gin.H{
		"ok":            true,
		"bot_id":        botID,
		"strategy_type": removedType,
		"strategy_count": len(botConfig.Strategies),
	})
}

// syncBotConfigToMain 将 Bot 配置同步到主配置文件
func syncBotConfigToMain(botID string, botConfig *config.BotConfigFile) {
	if configManager == nil {
		return
	}

	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		return
	}

	// 更新主配置中的 Bot
	found := false
	for i := range cfg.Bots {
		id := cfg.Bots[i].ID
		if id == "" {
			id = config.GenerateBotID(cfg.Bots[i].Exchange, cfg.Bots[i].Symbol, cfg.Bots[i].GetMarketType())
		}
		if id == botID {
			updatedBot := config.ConvertToBotConfig(botConfig)
			cfg.Bots[i] = updatedBot
			found = true
			break
		}
	}

	if !found {
		// 添加新 Bot
		newBot := config.ConvertToBotConfig(botConfig)
		cfg.Bots = append(cfg.Bots, newBot)
	}

	// 保存主配置
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		logger.Error("同步主配置失败: %v", err)
	}
}
