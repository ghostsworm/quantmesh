package web

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	"quantmesh/config"
)

var (
	// 全局配置管理器（需要从main.go注入）
	configManager     *ConfigManager
	configBackupMgr   *config.BackupManager
	configHotReloader *config.HotReloader
)

// ConfigManager 配置管理器
type ConfigManager struct {
	configPath    string
	currentConfig *config.Config
	mu            sync.RWMutex
}

// NewConfigManager 创建配置管理器
func NewConfigManager(configPath string) *ConfigManager {
	return &ConfigManager{
		configPath: configPath,
	}
}

// SetConfigManager 设置配置管理器
func SetConfigManager(cm *ConfigManager) {
	configManager = cm
}

// SetConfigBackupManager 设置备份管理器
func SetConfigBackupManager(bm *config.BackupManager) {
	configBackupMgr = bm
}

// SetConfigHotReloader 设置热更新器
func SetConfigHotReloader(hr *config.HotReloader) {
	configHotReloader = hr
}

// GetLatestConfig 获取最新配置（用于外部包获取最新配置）
func GetLatestConfig() (*config.Config, error) {
	if configManager == nil {
		return nil, fmt.Errorf("配置管理器未初始化")
	}
	return configManager.GetConfig()
}

// SetSymbolEnabled 设置指定交易所/交易对的 enabled 状态，并持久化到配置文件。
//
// 用途：
// - StopTrading 时写回 enabled=false，确保重启后不会自动再启动
// - StartTrading 时写回 enabled=true，确保重启后保持启动
func SetSymbolEnabled(exchange, symbol string, enabled bool) error {
	if configManager == nil {
		return fmt.Errorf("配置管理器未初始化")
	}
	if exchange == "" || symbol == "" {
		return fmt.Errorf("exchange 和 symbol 不能为空")
	}

	configManager.mu.Lock()
	defer configManager.mu.Unlock()

	// 确保有最新配置
	cfg := configManager.currentConfig
	if cfg == nil {
		loaded, err := config.LoadConfig(configManager.configPath)
		if err != nil {
			return err
		}
		cfg = loaded
	}

	found := false
	for i := range cfg.Trading.Symbols {
		if strings.EqualFold(cfg.Trading.Symbols[i].Exchange, exchange) &&
			strings.EqualFold(cfg.Trading.Symbols[i].Symbol, symbol) {
			cfg.Trading.Symbols[i].SetEnabled(enabled)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("未找到交易对配置: %s:%s", exchange, symbol)
	}

	// 保存到文件（含校验/normalize）
	if err := config.SaveConfig(cfg, configManager.configPath); err != nil {
		return err
	}

	// 更新内存中的配置
	configManager.currentConfig = cfg

	// 尝试热更新（失败不影响持久化）
	if configHotReloader != nil {
		_, _ = configHotReloader.UpdateConfig(cfg)
	}

	return nil
}

// GetCurrentAccountID 获取当前配置的账户标识
func GetCurrentAccountID() string {
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		return ""
	}

	exchange := cfg.App.CurrentExchange
	if exchange == "" {
		return ""
	}

	if exCfg, ok := cfg.Exchanges[exchange]; ok {
		apiKey := exCfg.APIKey
		if len(apiKey) > 8 {
			return apiKey[:8]
		}
		return apiKey
	}

	return ""
}

// GetConfig 获取当前配置
func (cm *ConfigManager) GetConfig() (*config.Config, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.currentConfig != nil {
		return cm.currentConfig, nil
	}

	// 如果内存中没有，从文件加载
	cfg, err := config.LoadConfig(cm.configPath)
	if err != nil {
		return nil, err
	}

	cm.currentConfig = cfg
	return cfg, nil
}

// UpdateConfig 更新配置
func (cm *ConfigManager) UpdateConfig(newConfig *config.Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 保存到文件
	if err := config.SaveConfig(newConfig, cm.configPath); err != nil {
		return err
	}

	// 更新内存中的配置
	cm.currentConfig = newConfig

	return nil
}

// GetConfigPath 获取配置文件路径
func (cm *ConfigManager) GetConfigPath() string {
	return cm.configPath
}

// getConfigHandler 获取当前配置（YAML格式）
// GET /api/config
func getConfigHandler(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 序列化为YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化配置失败: " + err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/x-yaml", data)
}

// getConfigJSONHandler 获取当前配置（JSON格式）
// GET /api/config/json
func getConfigJSONHandler(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 使用YAML序列化然后解析为map，以保留YAML标签的字段名（snake_case）
	// 这样前端就能正确读取字段名
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化配置失败: " + err.Error()})
		return
	}

	// 解析YAML为map，这样字段名就是YAML标签的值（snake_case）
	var configMap map[string]interface{}
	if err := yaml.Unmarshal(yamlData, &configMap); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "转换配置格式失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, configMap)
}

// bindConfigFromJSONMap 绑定前端 snake_case JSON 为 Config
//
// 前端通过 /api/config/json 获取的配置是 snake_case 字段名（来自 YAML tag）。
// 为了让 POST /validate /preview /update 也接受同样结构，这里先把 JSON 解析为 map，
// 再通过 yaml marshal/unmarshal 使用 yaml tag 写回到 config.Config。
func bindConfigFromJSONMap(c *gin.Context) (*config.Config, error) {
	var configMap map[string]interface{}
	if err := c.ShouldBindJSON(&configMap); err != nil {
		return nil, fmt.Errorf("无效的配置格式: %w", err)
	}

	yamlData, err := yaml.Marshal(configMap)
	if err != nil {
		return nil, fmt.Errorf("序列化配置失败: %w", err)
	}

	var cfg config.Config
	if err := yaml.Unmarshal(yamlData, &cfg); err != nil {
		return nil, fmt.Errorf("转换配置格式失败: %w", err)
	}

	return &cfg, nil
}

// validateConfigHandler 验证配置（不保存）
// POST /api/config/validate
func validateConfigHandler(c *gin.Context) {
	cfg, err := bindConfigFromJSONMap(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := cfg.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":   true,
		"message": "配置验证通过",
	})
}

// previewConfigHandler 预览配置变更
// POST /api/config/preview
func previewConfigHandler(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	// 获取新配置（snake_case）
	newConfig, err := bindConfigFromJSONMap(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取当前配置
	oldConfig, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取当前配置失败: " + err.Error()})
		return
	}

	// 生成差异
	diff := config.DiffConfig(oldConfig, newConfig)

	c.JSON(http.StatusOK, gin.H{
		"diff":             diff,
		"requires_restart": diff.RequiresRestart,
	})
}

// updateConfigHandler 更新配置
// POST /api/config/update
func updateConfigHandler(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	// 获取新配置（snake_case）
	newConfig, err := bindConfigFromJSONMap(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 验证配置
	if err := newConfig.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "配置验证失败: " + err.Error()})
		return
	}

	// 获取当前配置
	oldConfig, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取当前配置失败: " + err.Error()})
		return
	}

	// 生成差异
	diff := config.DiffConfig(oldConfig, newConfig)

	// 创建备份
	var backupInfo *config.BackupInfo
	if configBackupMgr != nil {
		backupInfo, err = configBackupMgr.CreateBackup(configManager.GetConfigPath(), "通过Web界面更新配置")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "创建备份失败: " + err.Error()})
			return
		}
	}

	// 保存配置
	if err := configManager.UpdateConfig(newConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败: " + err.Error()})
		return
	}

	// 尝试热更新
	if configHotReloader != nil {
		_, err := configHotReloader.UpdateConfig(newConfig)
		if err != nil {
			// 热更新失败不影响配置保存，只记录警告
			// 注意：这里可能需要通过日志记录
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "配置更新成功",
		"backup_id":        backupInfo.ID,
		"diff":             diff,
		"requires_restart": diff.RequiresRestart,
	})
}

// getBackupsHandler 获取备份列表
// GET /api/config/backups
func getBackupsHandler(c *gin.Context) {
	if configBackupMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "备份管理器未初始化"})
		return
	}

	backups, err := configBackupMgr.ListBackups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取备份列表失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"backups": backups,
		"count":   len(backups),
	})
}

// restoreBackupHandler 恢复备份
// POST /api/config/restore/:backup_id
func restoreBackupHandler(c *gin.Context) {
	if configManager == nil || configBackupMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器或备份管理器未初始化"})
		return
	}

	backupID := c.Param("backup_id")
	if backupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "备份ID不能为空"})
		return
	}

	// 恢复备份
	if err := configBackupMgr.RestoreBackup(backupID, configManager.GetConfigPath()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "恢复备份失败: " + err.Error()})
		return
	}

	// 重新加载配置
	cfg, err := config.LoadConfig(configManager.GetConfigPath())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重新加载配置失败: " + err.Error()})
		return
	}

	// 更新内存中的配置
	if err := configManager.UpdateConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "备份恢复成功",
		"backup_id": backupID,
	})
}

// deleteBackupHandler 删除备份
// DELETE /api/config/backup/:backup_id
func deleteBackupHandler(c *gin.Context) {
	if configBackupMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "备份管理器未初始化"})
		return
	}

	backupID := c.Param("backup_id")
	if backupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "备份ID不能为空"})
		return
	}

	if err := configBackupMgr.DeleteBackup(backupID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除备份失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "备份删除成功",
		"backup_id": backupID,
	})
}
