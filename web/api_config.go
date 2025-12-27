package web

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	"quantmesh/config"
)

var (
	// 全局配置管理器（需要从main.go注入）
	configManager      *ConfigManager
	configBackupMgr    *config.BackupManager
	configHotReloader  *config.HotReloader
)

// ConfigManager 配置管理器
type ConfigManager struct {
	configPath string
	currentConfig *config.Config
	mu         sync.RWMutex
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

// validateConfigHandler 验证配置（不保存）
// POST /api/config/validate
func validateConfigHandler(c *gin.Context) {
	var cfg config.Config
	if err := c.ShouldBindJSON(&cfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的配置格式: " + err.Error()})
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
		"valid": true,
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

	// 获取新配置
	var newConfig config.Config
	if err := c.ShouldBindJSON(&newConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的配置格式: " + err.Error()})
		return
	}

	// 获取当前配置
	oldConfig, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取当前配置失败: " + err.Error()})
		return
	}

	// 生成差异
	diff := config.DiffConfig(oldConfig, &newConfig)

	c.JSON(http.StatusOK, gin.H{
		"diff": diff,
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

	// 获取新配置
	var newConfig config.Config
	if err := c.ShouldBindJSON(&newConfig); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的配置格式: " + err.Error()})
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
	diff := config.DiffConfig(oldConfig, &newConfig)

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
	if err := configManager.UpdateConfig(&newConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败: " + err.Error()})
		return
	}

	// 尝试热更新
	if configHotReloader != nil {
		_, err := configHotReloader.UpdateConfig(&newConfig)
		if err != nil {
			// 热更新失败不影响配置保存，只记录警告
			// 注意：这里可能需要通过日志记录
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "配置更新成功",
		"backup_id": backupInfo.ID,
		"diff": diff,
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
		"count": len(backups),
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
		"message": "备份恢复成功",
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
		"message": "备份删除成功",
		"backup_id": backupID,
	})
}

