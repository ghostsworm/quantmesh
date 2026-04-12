package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/storage"
)

var botConfigManager *config.BotConfigManager

// InitBotConfigManager 初始化 Bot 配置文件管理器（僅作無主庫時的後備；權威持久化為 bot_configs 表）
func InitBotConfigManager(baseDir string) error {
	var err error
	botConfigManager, err = config.NewBotConfigManager(baseDir)
	if err != nil {
		return err
	}
	logger.Info("✅ Bot 配置管理器已初始化，目录: %s", botConfigManager.GetBotDataPath(""))
	return nil
}

// sqlStorageForBotConfig 主庫 *SQLStorage（與 app_config 同庫），用於 bot_configs 文檔表
func sqlStorageForBotConfig() *storage.SQLStorage {
	if primaryStorageForAppConfig == nil {
		return nil
	}
	ss, ok := primaryStorageForAppConfig.(*storage.SQLStorage)
	if !ok || ss == nil {
		return nil
	}
	return ss
}

func botConfigSnapshotExists(botID string) (bool, error) {
	ss := sqlStorageForBotConfig()
	if ss != nil {
		if err := ss.EnsureAppConfigDocumentTables(); err != nil {
			return false, err
		}
		doc, err := ss.GetBotConfigDocument(context.Background(), botID)
		if err != nil {
			return false, err
		}
		if doc != nil && strings.TrimSpace(doc.Content) != "" {
			return true, nil
		}
	}
	if botConfigManager != nil && botConfigManager.BotConfigExists(botID) {
		return true, nil
	}
	return false, nil
}

// loadBotConfigUnified 優先從主庫 bot_configs 讀取，否則嘗試本地 bots/*/config.yaml
func loadBotConfigUnified(botID string) (*config.BotConfigFile, error) {
	ss := sqlStorageForBotConfig()
	if ss != nil {
		if err := ss.EnsureAppConfigDocumentTables(); err != nil {
			return nil, err
		}
		doc, err := ss.GetBotConfigDocument(context.Background(), botID)
		if err != nil {
			return nil, err
		}
		if doc != nil && strings.TrimSpace(doc.Content) != "" {
			var bf config.BotConfigFile
			if err := json.Unmarshal([]byte(doc.Content), &bf); err != nil {
				return nil, fmt.Errorf("解析 bot_configs JSON: %w", err)
			}
			return &bf, nil
		}
	}
	if botConfigManager != nil && botConfigManager.BotConfigExists(botID) {
		return botConfigManager.LoadBotConfig(botID)
	}
	return nil, nil
}

func saveBotConfigUnified(bf *config.BotConfigFile, operator, source string) error {
	if bf == nil {
		return fmt.Errorf("配置為空")
	}
	if ss := sqlStorageForBotConfig(); ss != nil {
		if err := ss.EnsureAppConfigDocumentTables(); err != nil {
			return err
		}
		_, err := storage.SaveBotConfigSnapshot(context.Background(), primaryStorageForAppConfig, bf, operator, source)
		return err
	}
	if botConfigManager != nil {
		return botConfigManager.SaveBotConfig(bf)
	}
	return fmt.Errorf("無可用 Bot 配置存儲（主庫未初始化且無本地管理器）")
}

func deleteBotConfigUnified(botID string) error {
	var deleted bool
	if ss := sqlStorageForBotConfig(); ss != nil {
		if err := ss.EnsureAppConfigDocumentTables(); err != nil {
			return err
		}
		doc, err := ss.GetBotConfigDocument(context.Background(), botID)
		if err != nil {
			return err
		}
		if doc != nil && strings.TrimSpace(doc.Content) != "" {
			if err := storage.DeleteBotConfigSnapshot(context.Background(), primaryStorageForAppConfig, botID); err != nil {
				return err
			}
			deleted = true
		}
	}
	if botConfigManager != nil && botConfigManager.BotConfigExists(botID) {
		if err := botConfigManager.DeleteBotConfig(botID); err != nil {
			return err
		}
		deleted = true
	}
	if !deleted {
		return fmt.Errorf("not found")
	}
	return nil
}

func botConfigStorageReady() bool {
	return sqlStorageForBotConfig() != nil || botConfigManager != nil
}

// BotConfigFileResponse Bot 配置文件响应
type BotConfigFileResponse struct {
	BotID      string                `json:"bot_id"`
	Name       string                `json:"name"`
	Exchange   string                `json:"exchange"`
	Symbol     string                `json:"symbol"`
	MarketType string                `json:"market_type"`
	Config     *config.BotConfigFile `json:"config"`
	Exists     bool                  `json:"exists"`
}

// getBotConfigFile 获取 Bot 配置文件
// GET /api/bots/:id/config-file
func getBotConfigFile(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if !botConfigStorageReady() {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	botConfig, err := loadBotConfigUnified(botID)
	if err != nil {
		logger.Error("加载 Bot 配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_load_failed", err)
		return
	}

	exists := false
	if botConfig != nil {
		exists = true
	} else {
		cfg, err := GetLatestConfig()
		if err != nil || cfg == nil {
			c.JSON(http.StatusNotFound, gin.H{
				"error": "Bot config file not found and cannot be created from main config",
			})
			return
		}

		for _, b := range cfg.Bots {
			id := b.ID
			if id == "" {
				id = config.GenerateBotID(b.Exchange, b.Symbol, b.GetMarketType())
			}
			if id == botID {
				botConfig = config.ConvertFromBotConfig(b)
				botConfig.CreatedAt = time.Now().Format(time.RFC3339)
				botConfig.UpdatedAt = time.Now().Format(time.RFC3339)

				if err := saveBotConfigUnified(botConfig, "web", "get_bot_config_lazy_create"); err != nil {
					logger.Warn("首次持久化 Bot 配置失敗（仍返回內存對象）: %v", err)
				} else {
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

	if !botConfigStorageReady() {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	var req config.BotConfigFile
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	req.BotID = botID
	req.UpdatedAt = time.Now().Format(time.RFC3339)

	if botManagerProvider != nil {
		if bot, ok := botManagerProvider.GetBot(botID); ok && bot.Running {
			c.JSON(http.StatusConflict, gin.H{
				"error":     "bot_running",
				"error_key": "error.bot_running_cannot_update_full_config",
				"message":   "Cannot update full config while bot is running. Use /api/bots/:id/strategy for strategy updates.",
			})
			return
		}
	}

	existed, err := botConfigSnapshotExists(botID)
	if err != nil {
		logger.Error("檢查 Bot 配置是否存在失敗: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	if err := saveBotConfigUnified(&req, "web", "put_bot_config"); err != nil {
		logger.Error("保存 Bot 配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	if configManager != nil {
		cfg, err := GetLatestConfig()
		if err == nil && cfg != nil {
			found := false
			for i := range cfg.Bots {
				id := cfg.Bots[i].ID
				if id == "" {
					id = config.GenerateBotID(cfg.Bots[i].Exchange, cfg.Bots[i].Symbol, cfg.Bots[i].GetMarketType())
				}
				if id == botID {
					updatedBot := config.ConvertToBotConfig(&req)
					cfg.Bots[i] = updatedBot
					found = true
					break
				}
			}

			if !found && !existed {
				newBot := config.ConvertToBotConfig(&req)
				cfg.Bots = append(cfg.Bots, newBot)
			}

			if err := fileConfigManager.UpdateConfig(cfg); err != nil {
				logger.Error("同步主配置失败: %v", err)
			}
		}
	}

	logger.Info("✅ Bot %s 配置已更新（主庫 bot_configs 或本地後備）", botID)
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

	if !botConfigStorageReady() {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

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

	if configManager != nil {
		cfg, err := GetLatestConfig()
		if err == nil && cfg != nil {
			if groupName := FindGroupNameByBotID(cfg, botID); groupName != "" {
				c.JSON(http.StatusForbidden, gin.H{
					"error":      "bot_in_hedge_group",
					"error_key":  "error.bot_in_hedge_group_cannot_delete_config",
					"group_name": groupName,
				})
				return
			}
		}
	}

	ok, err := botConfigSnapshotExists(botID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_delete_failed", err)
		return
	}
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot config file not found"})
		return
	}

	if err := deleteBotConfigUnified(botID); err != nil {
		logger.Error("删除 Bot 配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_delete_failed", err)
		return
	}

	logger.Info("✅ Bot %s 配置已删除", botID)
	c.JSON(http.StatusOK, gin.H{
		"ok":     true,
		"bot_id": botID,
	})
}

// StrategyConfigUpdateRequest 策略配置更新请求
type StrategyConfigUpdateRequest struct {
	StrategyIndex int                    `json:"strategy_index"` // 策略索引（多策略时）
	Strategy      config.BotStrategyConfig `json:"strategy"`       // 策略配置
}

// putBotStrategyConfig 更新 Bot 的单个策略配置
// PUT /api/bots/:id/strategy-config
func putBotStrategyConfig(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}

	if !botConfigStorageReady() {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	var req StrategyConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	botConfig, err := loadBotConfigUnified(botID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed", err)
		return
	}
	if botConfig == nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", fmt.Errorf("not found"))
		return
	}

	if req.StrategyIndex < 0 || req.StrategyIndex >= len(botConfig.Strategies) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "invalid_strategy_index",
			"error_key": "error.invalid_strategy_index",
			"message":   "Strategy index out of range",
		})
		return
	}

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

	botConfig.Strategies[req.StrategyIndex] = req.Strategy
	botConfig.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := saveBotConfigUnified(botConfig, "web", "put_bot_strategy_config"); err != nil {
		logger.Error("保存 Bot 配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	syncBotConfigToMain(botID, botConfig)

	logger.Info("✅ Bot %s 策略 %d 配置已更新", botID, req.StrategyIndex)
	c.JSON(http.StatusOK, gin.H{
		"ok":             true,
		"bot_id":         botID,
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

	if !botConfigStorageReady() {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	var strategy config.BotStrategyConfig
	if err := c.ShouldBindJSON(&strategy); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	botConfig, err := loadBotConfigUnified(botID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed", err)
		return
	}
	if botConfig == nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", fmt.Errorf("not found"))
		return
	}

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

	botConfig.Strategies = append(botConfig.Strategies, strategy)
	if len(botConfig.Strategies) > 1 {
		botConfig.StrategyMode = "multi"
	}
	botConfig.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := saveBotConfigUnified(botConfig, "web", "add_bot_strategy"); err != nil {
		logger.Error("保存 Bot 配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	syncBotConfigToMain(botID, botConfig)

	logger.Info("✅ Bot %s 已添加策略 %s", botID, strategy.Type)
	c.JSON(http.StatusOK, gin.H{
		"ok":             true,
		"bot_id":         botID,
		"strategy_type":  strategy.Type,
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

	if !botConfigStorageReady() {
		respondError(c, http.StatusServiceUnavailable, "error.bot_config_manager_unavailable")
		return
	}

	var index int
	if _, err := fmt.Sscanf(strategyIndex, "%d", &index); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_strategy_index")
		return
	}

	botConfig, err := loadBotConfigUnified(botID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed", err)
		return
	}
	if botConfig == nil {
		respondError(c, http.StatusNotFound, "error.bot_config_not_found", fmt.Errorf("not found"))
		return
	}

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

	if index < 0 || index >= len(botConfig.Strategies) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "invalid_strategy_index",
			"error_key": "error.invalid_strategy_index",
		})
		return
	}

	if len(botConfig.Strategies) <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":     "cannot_remove_last_strategy",
			"error_key": "error.cannot_remove_last_strategy",
		})
		return
	}

	removedType := botConfig.Strategies[index].Type
	botConfig.Strategies = append(botConfig.Strategies[:index], botConfig.Strategies[index+1:]...)
	if len(botConfig.Strategies) == 1 {
		botConfig.StrategyMode = "single"
	}
	botConfig.UpdatedAt = time.Now().Format(time.RFC3339)

	if err := saveBotConfigUnified(botConfig, "web", "remove_bot_strategy"); err != nil {
		logger.Error("保存 Bot 配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	syncBotConfigToMain(botID, botConfig)

	logger.Info("✅ Bot %s 已移除策略 %s (索引 %d)", botID, removedType, index)
	c.JSON(http.StatusOK, gin.H{
		"ok":             true,
		"bot_id":         botID,
		"strategy_type":  removedType,
		"strategy_count": len(botConfig.Strategies),
	})
}

// syncBotConfigToMain 将 Bot 配置同步到主配置（app_config 快照）
func syncBotConfigToMain(botID string, botConfig *config.BotConfigFile) {
	if configManager == nil {
		return
	}

	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		return
	}

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
		newBot := config.ConvertToBotConfig(botConfig)
		cfg.Bots = append(cfg.Bots, newBot)
	}

	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		logger.Error("同步主配置失败: %v", err)
	}
}
