package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"quantmesh/logger"
)

// MigrateBotConfigs 迁移现有的 Bot 配置到独立的配置文件
// 从主配置文件中的 Bot 配置迁移到 bots/{bot_id}/config.yaml
func MigrateBotConfigs(cfg *Config, baseDir string) (*MigrationResult, error) {
	if len(cfg.Bots) == 0 {
		return nil, fmt.Errorf("没有 Bot 配置需要迁移")
	}

	// 创建 Bot 配置管理器
	manager, err := NewBotConfigManager(baseDir)
	if err != nil {
		return nil, fmt.Errorf("创建 Bot 配置管理器失败: %w", err)
	}

	result := &MigrationResult{
		StartTime:   time.Now(),
		BaseDir:     baseDir,
		BotsDir:     manager.GetBotDataPath(""),
		TotalBots:   len(cfg.Bots),
		Migrated:    make([]string, 0),
		Failed:      make([]MigrationFailure, 0),
		Skipped:     make([]string, 0),
	}

	logger.Info("🔄 开始迁移 Bot 配置到独立文件...")
	logger.Info("📂 Bot 配置目录: %s", manager.GetBotDataPath(""))

	for _, bot := range cfg.Bots {
		botID := bot.ID
		if botID == "" {
			botID = GenerateBotID(bot.Exchange, bot.Symbol, bot.GetMarketType())
		}

		// 检查配置文件是否已存在
		if manager.BotConfigExists(botID) {
			logger.Info("⏭️  Bot %s 配置文件已存在，跳过", botID)
			result.Skipped = append(result.Skipped, botID)
			continue
		}

		// 转换为新的配置文件格式
		botConfigFile := ConvertFromBotConfig(bot)
		botConfigFile.CreatedAt = time.Now().Format(time.RFC3339)
		botConfigFile.UpdatedAt = time.Now().Format(time.RFC3339)

		// 保存配置文件
		if err := manager.CreateBotConfig(botConfigFile); err != nil {
			logger.Error("❌ 迁移 Bot %s 配置失败: %v", botID, err)
			result.Failed = append(result.Failed, MigrationFailure{
				BotID: botID,
				Error: err.Error(),
			})
			continue
		}

		logger.Info("✅ Bot %s (%s) 配置已迁移到 %s",
			botID, bot.Name, manager.GetBotConfigPath(botID))
		result.Migrated = append(result.Migrated, botID)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	// 打印迁移结果
	logger.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Info("📊 Bot 配置迁移完成")
	logger.Info("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	logger.Info("总计: %d 个 Bot", result.TotalBots)
	logger.Info("成功: %d 个", len(result.Migrated))
	logger.Info("跳过: %d 个", len(result.Skipped))
	logger.Info("失败: %d 个", len(result.Failed))
	logger.Info("耗时: %v", result.Duration)

	if len(result.Failed) > 0 {
		logger.Warn("⚠️  以下 Bot 迁移失败:")
		for _, failure := range result.Failed {
			logger.Warn("  - %s: %s", failure.BotID, failure.Error)
		}
	}

	return result, nil
}

// MigrationResult 迁移结果
type MigrationResult struct {
	StartTime time.Time            `json:"start_time"`
	EndTime   time.Time            `json:"end_time"`
	Duration  time.Duration        `json:"duration"`
	BaseDir   string               `json:"base_dir"`
	BotsDir   string               `json:"bots_dir"`
	TotalBots int                  `json:"total_bots"`
	Migrated  []string             `json:"migrated"`
	Skipped   []string             `json:"skipped"`
	Failed    []MigrationFailure   `json:"failed"`
}

// MigrationFailure 迁移失败记录
type MigrationFailure struct {
	BotID string `json:"bot_id"`
	Error string `json:"error"`
}

// RollbackMigration 回滚迁移（删除已创建的配置文件）
func RollbackMigration(result *MigrationResult) error {
	if len(result.Migrated) == 0 {
		return nil
	}

	logger.Warn("⚠️  开始回滚 Bot 配置迁移...")

	manager, err := NewBotConfigManager(result.BaseDir)
	if err != nil {
		return fmt.Errorf("创建 Bot 配置管理器失败: %w", err)
	}

	var rollbackErrors []string
	for _, botID := range result.Migrated {
		if err := manager.DeleteBotConfig(botID); err != nil {
			logger.Error("❌ 删除 Bot %s 配置失败: %v", botID, err)
			rollbackErrors = append(rollbackErrors, fmt.Sprintf("%s: %v", botID, err))
		} else {
			logger.Info("✅ 已删除 Bot %s 的配置文件", botID)
		}
	}

	if len(rollbackErrors) > 0 {
		return fmt.Errorf("部分回滚失败: %v", rollbackErrors)
	}

	logger.Info("✅ 迁移回滚完成")
	return nil
}

// SyncBotConfigsToMain 将独立配置文件同步回主配置文件
// 用于在修改独立配置文件后更新主配置
func SyncBotConfigsToMain(baseDir string, mainConfig *Config) error {
	manager, err := NewBotConfigManager(baseDir)
	if err != nil {
		return fmt.Errorf("创建 Bot 配置管理器失败: %w", err)
	}

	// 获取所有 Bot 配置
	botIDs, err := manager.ListBotConfigs()
	if err != nil {
		return fmt.Errorf("列出 Bot 配置失败: %w", err)
	}

	logger.Info("🔄 开始同步 Bot 配置到主配置文件...")

	var updatedBots []BotConfig
	for _, botID := range botIDs {
		botConfigFile, err := manager.LoadBotConfig(botID)
		if err != nil {
			logger.Warn("⚠️  加载 Bot %s 配置失败，跳过: %v", botID, err)
			continue
		}

		// 转换为主配置格式
		bot := ConvertToBotConfig(botConfigFile)
		updatedBots = append(updatedBots, bot)
		logger.Info("✅ Bot %s 配置已同步", botID)
	}

	// 更新主配置
	mainConfig.Bots = updatedBots

	logger.Info("✅ 已同步 %d 个 Bot 配置到主配置文件", len(updatedBots))
	return nil
}

// ValidateBotConfigs 验证所有 Bot 配置文件
func ValidateBotConfigs(baseDir string) (*ValidationResult, error) {
	manager, err := NewBotConfigManager(baseDir)
	if err != nil {
		return nil, fmt.Errorf("创建 Bot 配置管理器失败: %w", err)
	}

	botIDs, err := manager.ListBotConfigs()
	if err != nil {
		return nil, fmt.Errorf("列出 Bot 配置失败: %w", err)
	}

	result := &ValidationResult{
		Total:   len(botIDs),
		Valid:   make([]string, 0),
		Invalid: make([]ValidationError, 0),
	}

	for _, botID := range botIDs {
		config, err := manager.LoadBotConfig(botID)
		if err != nil {
			result.Invalid = append(result.Invalid, ValidationError{
				BotID:  botID,
				Field:  "file",
				Error:  err.Error(),
			})
			continue
		}

		// 验证必需字段
		if config.BotID == "" {
			result.Invalid = append(result.Invalid, ValidationError{
				BotID:  botID,
				Field:  "bot_id",
				Error:  "bot_id is empty",
			})
			continue
		}

		if config.Exchange == "" {
			result.Invalid = append(result.Invalid, ValidationError{
				BotID:  botID,
				Field:  "exchange",
				Error:  "exchange is empty",
			})
			continue
		}

		if config.Symbol == "" {
			result.Invalid = append(result.Invalid, ValidationError{
				BotID:  botID,
				Field:  "symbol",
				Error:  "symbol is empty",
			})
			continue
		}

		if len(config.Strategies) == 0 {
			result.Invalid = append(result.Invalid, ValidationError{
				BotID:  botID,
				Field:  "strategies",
				Error:  "no strategies defined",
			})
			continue
		}

		// 验证策略配置
		for i, strategy := range config.Strategies {
			if strategy.Type == "" {
				result.Invalid = append(result.Invalid, ValidationError{
					BotID:  botID,
					Field:  fmt.Sprintf("strategies[%d].type", i),
					Error:  "strategy type is empty",
				})
			}
		}

		result.Valid = append(result.Valid, botID)
	}

	return result, nil
}

// ValidationResult 验证结果
type ValidationResult struct {
	Total   int                 `json:"total"`
	Valid   []string            `json:"valid"`
	Invalid []ValidationError   `json:"invalid"`
}

// ValidationError 验证错误
type ValidationError struct {
	BotID  string `json:"bot_id"`
	Field  string `json:"field"`
	Error  string `json:"error"`
}

// ExportBotConfig 导出单个 Bot 配置为 JSON
func ExportBotConfig(baseDir, botID string) (string, error) {
	manager, err := NewBotConfigManager(baseDir)
	if err != nil {
		return "", fmt.Errorf("创建 Bot 配置管理器失败: %w", err)
	}

	config, err := manager.LoadBotConfig(botID)
	if err != nil {
		return "", fmt.Errorf("加载 Bot 配置失败: %w", err)
	}

	jsonData, err := config.ExportToJSON()
	if err != nil {
		return "", fmt.Errorf("导出配置失败: %w", err)
	}

	// 保存到文件
	exportPath := filepath.Join(manager.GetBotDataPath(botID), "config.json")
	if err := os.WriteFile(exportPath, jsonData, 0644); err != nil {
		return "", fmt.Errorf("写入导出文件失败: %w", err)
	}

	return exportPath, nil
}

// ImportBotConfig 从 JSON 导入 Bot 配置
func ImportBotConfig(baseDir, botID, jsonPath string) error {
	manager, err := NewBotConfigManager(baseDir)
	if err != nil {
		return fmt.Errorf("创建 Bot 配置管理器失败: %w", err)
	}

	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("读取 JSON 文件失败: %w", err)
	}

	config, err := ImportFromJSON(jsonData)
	if err != nil {
		return fmt.Errorf("解析 JSON 失败: %w", err)
	}

	// 验证 Bot ID
	if config.BotID != botID {
		return fmt.Errorf("JSON 中的 Bot ID (%s) 与指定的 Bot ID (%s) 不匹配", config.BotID, botID)
	}

	if err := manager.SaveBotConfig(config); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	logger.Info("✅ Bot %s 配置已从 %s 导入", botID, jsonPath)
	return nil
}
