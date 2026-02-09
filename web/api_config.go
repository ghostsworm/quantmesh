package web

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/notify"
)

var (
	// 全局配置管理器（需要從main.go注入）
	configManager      *ConfigManager
	configBackupMgr    *config.BackupManager
	configHotReloader  *config.HotReloader
	configHistoryMgr   *config.HistoryManager
)

// ConfigManager 配置管理器
type ConfigManager struct {
	configPath    string
	currentConfig *config.Config
	mu            sync.RWMutex
}

// NewConfigManager 創建配置管理器
func NewConfigManager(configPath string) *ConfigManager {
	return &ConfigManager{
		configPath: configPath,
	}
}

// SetConfigManager 設置配置管理器
func SetConfigManager(cm *ConfigManager) {
	configManager = cm
}

// SetConfigBackupManager 設置备份管理器
func SetConfigBackupManager(bm *config.BackupManager) {
	configBackupMgr = bm
}

// SetConfigHotReloader 設置热更新器
func SetConfigHotReloader(hr *config.HotReloader) {
	configHotReloader = hr
}

// SetConfigHistoryManager 設置配置历史管理器
func SetConfigHistoryManager(hm *config.HistoryManager) {
	configHistoryMgr = hm
}

// GetLatestConfig 獲取最新配置（用於外部包獲取最新配置）
func GetLatestConfig() (*config.Config, error) {
	if configManager == nil {
		return nil, fmt.Errorf("配置管理器未初始化")
	}
	return configManager.GetConfig()
}

// SetSymbolEnabled 設置指定交易所/交易對的 enabled 状態，並持久化到配置文件。
//
// 用途：
// - StopTrading 時写回 enabled=false，确保重啟后不會自动再啟动
// - StartTrading 時写回 enabled=true，确保重啟后保持啟动
func SetSymbolEnabled(exchange, symbol string, enabled bool) error {
	if configManager == nil {
		return fmt.Errorf("配置管理器未初始化")
	}
	if exchange == "" || symbol == "" {
		return fmt.Errorf("exchange 和 symbol 不能為空")
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
		return fmt.Errorf("未找到交易對配置: %s:%s", exchange, symbol)
	}

	// 保存到文件（含校驗/normalize）
	if err := config.SaveConfig(cfg, configManager.configPath); err != nil {
		return err
	}

	// 保存到历史
	if configHistoryMgr != nil {
		currentContent, err := os.ReadFile(configManager.configPath)
		if err == nil {
			description := fmt.Sprintf("自动更新交易對狀態: %s:%s -> %v", exchange, symbol, enabled)
			_, _ = configHistoryMgr.SaveHistory(string(currentContent), description, "system")
		}
	}

	// 更新記憶體中的配置
	configManager.currentConfig = cfg

	// 尝試热更新（失败不影响持久化）
	if configHotReloader != nil {
		_, _ = configHotReloader.UpdateConfig(cfg)
	}

	return nil
}

// GetCurrentAccountID 獲取當前配置的账戶標识
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

// GetConfig 獲取當前配置
func (cm *ConfigManager) GetConfig() (*config.Config, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.currentConfig != nil {
		return cm.currentConfig, nil
	}

	// 如果記憶體中没有，從文件加載
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

	// 保存當前配置到历史
	if configHistoryMgr != nil {
		currentContent, err := os.ReadFile(cm.configPath)
		if err == nil {
			// 生成变更描述
			description := "通過 Web UI 更新配置"
			// 獲取舊配置用於對比（如果可能）
			if cm.currentConfig != nil {
				diff := config.DiffConfig(cm.currentConfig, newConfig)
				if len(diff.Changes) > 0 {
					description = fmt.Sprintf("通過 Web UI 修改了 %d 项配置", len(diff.Changes))
				}
			}
			_, _ = configHistoryMgr.SaveHistory(string(currentContent), description, "web")
		}
	}

	// 保存到文件
	if err := config.SaveConfig(newConfig, cm.configPath); err != nil {
		return err
	}

	// 更新記憶體中的配置
	cm.currentConfig = newConfig

	return nil
}

// GetConfigPath 獲取配置文件路径
func (cm *ConfigManager) GetConfigPath() string {
	return cm.configPath
}

// getConfigHandler 獲取當前配置（YAML格式）
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

	// 序列化為YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化配置失败: " + err.Error()})
		return
	}

	c.Data(http.StatusOK, "application/x-yaml", data)
}

// getConfigJSONHandler 獲取當前配置（JSON格式）
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

	// 使用YAML序列化然后解析為map，以保留YAML標签的字段名（snake_case）
	// 这样前端就能正确读取字段名
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化配置失败: " + err.Error()})
		return
	}

	// 解析YAML為map，这样字段名就是YAML標签的值（snake_case）
	var configMap map[string]interface{}
	if err := yaml.Unmarshal(yamlData, &configMap); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "轉换配置格式失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, configMap)
}

// bindConfigFromJSONMap 绑定前端 snake_case JSON 為 Config
//
// 前端通過 /api/config/json 獲取的配置是 snake_case 字段名（来自 YAML tag）。
// 為了让 POST /validate /preview /update 也接受同样結構，这里先把 JSON 解析為 map，
// 再通過 yaml marshal/unmarshal 使用 yaml tag 写回到 config.Config。
func bindConfigFromJSONMap(c *gin.Context) (*config.Config, error) {
	var configMap map[string]interface{}
	if err := c.ShouldBindJSON(&configMap); err != nil {
		return nil, fmt.Errorf("無效的配置格式: %w", err)
	}

	yamlData, err := yaml.Marshal(configMap)
	if err != nil {
		return nil, fmt.Errorf("序列化配置失败: %w", err)
	}

	var cfg config.Config
	if err := yaml.Unmarshal(yamlData, &cfg); err != nil {
		return nil, fmt.Errorf("轉换配置格式失败: %w", err)
	}

	return &cfg, nil
}

// validateConfigHandler 驗证配置（不保存）
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
		"message": "配置驗证通過",
	})
}

// previewConfigHandler 預览配置变更
// POST /api/config/preview
func previewConfigHandler(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	// 獲取新配置（snake_case）
	newConfig, err := bindConfigFromJSONMap(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 獲取當前配置
	oldConfig, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取當前配置失败: " + err.Error()})
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

	// 獲取新配置（snake_case）
	newConfig, err := bindConfigFromJSONMap(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 驗证配置
	if err := newConfig.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "配置驗证失败: " + err.Error()})
		return
	}

	// 獲取當前配置
	oldConfig, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取當前配置失败: " + err.Error()})
		return
	}

	// 生成差异
	diff := config.DiffConfig(oldConfig, newConfig)

	// 創建备份
	var backupInfo *config.BackupInfo
	if configBackupMgr != nil {
		backupInfo, err = configBackupMgr.CreateBackup(configManager.GetConfigPath(), "通過Web界面更新配置")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "創建备份失败: " + err.Error()})
			return
		}
	}

	// 保存配置
	if err := configManager.UpdateConfig(newConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败: " + err.Error()})
		return
	}

	// 尝試热更新
	if configHotReloader != nil {
		_, err := configHotReloader.UpdateConfig(newConfig)
		if err != nil {
			// 热更新失败不影响配置保存，只記錄警告
			// 注意：这里可能需要通過日志記錄
		}
	}

	// 🔥 推送交易参數变更到运行中的 SymbolRuntime（解决内存中参數不同步的问题）
	var updatedSymbols []string
	if symbolManagerProvider != nil {
		if updater, ok := symbolManagerProvider.(TradingParamsUpdater); ok {
			updatedSymbols = updater.UpdateTradingParams(newConfig)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "配置更新成功",
		"backup_id":        backupInfo.ID,
		"diff":             diff,
		"requires_restart": diff.RequiresRestart,
		"hot_updated":      updatedSymbols,
	})
}

// getBackupsHandler 獲取备份列表
// GET /api/config/backups
func getBackupsHandler(c *gin.Context) {
	if configBackupMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "备份管理器未初始化"})
		return
	}

	backups, err := configBackupMgr.ListBackups()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取备份列表失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"backups": backups,
		"count":   len(backups),
	})
}

// restoreBackupHandler 恢複备份
// POST /api/config/restore/:backup_id
func restoreBackupHandler(c *gin.Context) {
	if configManager == nil || configBackupMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器或备份管理器未初始化"})
		return
	}

	backupID := c.Param("backup_id")
	if backupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "备份ID不能為空"})
		return
	}

	// 恢複备份
	if err := configBackupMgr.RestoreBackup(backupID, configManager.GetConfigPath()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "恢複备份失败: " + err.Error()})
		return
	}

	// 重新加載配置
	cfg, err := config.LoadConfig(configManager.GetConfigPath())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重新加載配置失败: " + err.Error()})
		return
	}

	// 更新記憶體中的配置
	if err := configManager.UpdateConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新配置失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "备份恢複成功",
		"backup_id": backupID,
	})
}

// deleteBackupHandler 刪除备份
// DELETE /api/config/backup/:backup_id
func deleteBackupHandler(c *gin.Context) {
	if configBackupMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "备份管理器未初始化"})
		return
	}

	backupID := c.Param("backup_id")
	if backupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "备份ID不能為空"})
		return
	}

	if err := configBackupMgr.DeleteBackup(backupID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "刪除备份失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "备份刪除成功",
		"backup_id": backupID,
	})
}

// ========== 配置历史 API ==========

// getConfigHistoryListHandler 獲取配置历史列表
// GET /api/config/history
func getConfigHistoryListHandler(c *gin.Context) {
	if configHistoryMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置历史管理器未初始化"})
		return
	}

	// 解析分页参數
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	histories, total, err := configHistoryMgr.ListHistory(limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取歷史列表失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"histories": histories,
		"total":     total,
		"limit":     limit,
		"offset":    offset,
	})
}

// getConfigHistoryHandler 獲取指定版本的配置历史
// GET /api/config/history/:version
func getConfigHistoryHandler(c *gin.Context) {
	if configHistoryMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置历史管理器未初始化"})
		return
	}

	versionStr := c.Param("version")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的版本号"})
		return
	}

	history, err := configHistoryMgr.GetHistory(version)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, history)
}

// restoreConfigHistoryHandler 恢複到指定历史版本
// POST /api/config/history/:version/restore
func restoreConfigHistoryHandler(c *gin.Context) {
	if configHistoryMgr == nil || configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	versionStr := c.Param("version")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的版本号"})
		return
	}

	// 先保存當前配置到历史（作為恢複前的备份）
	currentContent, err := os.ReadFile(configManager.GetConfigPath())
	if err == nil {
		_, _ = configHistoryMgr.SaveHistory(string(currentContent), fmt.Sprintf("恢複到版本 %d 前的自动备份", version), "system")
	}

	// 恢複到指定版本
	if err := configHistoryMgr.RestoreHistory(version); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "恢複失败: " + err.Error()})
		return
	}

	// 重新加載配置到記憶體
	cfg, err := config.LoadConfig(configManager.GetConfigPath())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重新加載配置失败: " + err.Error()})
		return
	}

	// 更新記憶體中的配置
	configManager.mu.Lock()
	configManager.currentConfig = cfg
	configManager.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("已恢複到版本 %d", version),
		"version": version,
	})
}

// diffConfigHistoryHandler 對比两個版本的配置
// POST /api/config/history/diff
func diffConfigHistoryHandler(c *gin.Context) {
	if configHistoryMgr == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置历史管理器未初始化"})
		return
	}

	var req config.HistoryDiffRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的请求参數: " + err.Error()})
		return
	}

	response, err := configHistoryMgr.DiffVersions(req.SourceVersion, req.TargetVersion)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

// validateConfigYAMLHandler 驗證 YAML 配置（直接接收 YAML 文本）
// POST /api/config/validate-yaml
func validateConfigYAMLHandler(c *gin.Context) {
	// 读取请求体中的 YAML 内容
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败: " + err.Error()})
		return
	}

	// 解析 YAML
	cfg, err := config.LoadConfigFromBytes(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"valid": false,
			"error": fmt.Sprintf("YAML 解析失败: %v", err),
		})
		return
	}

	// 驗证配置
	if err := cfg.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"valid": false,
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":   true,
		"message": "配置驗证通過",
	})
}

// updateConfigYAMLHandler 更新配置（直接接收 YAML 文本）
// POST /api/config/update-yaml
func updateConfigYAMLHandler(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	// 读取请求体中的 YAML 内容
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败: " + err.Error()})
		return
	}

	yamlContent := string(body)

	// 解析 YAML
	newConfig, err := config.LoadConfigFromBytes(body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("YAML 解析失败: %v", err)})
		return
	}

	// 驗证配置
	if err := newConfig.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "配置驗证失败: " + err.Error()})
		return
	}

	// 獲取當前配置用於對比
	oldConfig, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取當前配置失败: " + err.Error()})
		return
	}

	// 生成差异
	diff := config.DiffConfig(oldConfig, newConfig)

	// 保存當前配置到历史
	if configHistoryMgr != nil {
		currentContent, err := os.ReadFile(configManager.GetConfigPath())
		if err == nil {
			// 生成变更描述
			description := "通過 YAML 编辑器更新配置"
			if len(diff.Changes) > 0 {
				description = fmt.Sprintf("修改了 %d 项配置", len(diff.Changes))
			}
			_, _ = configHistoryMgr.SaveHistory(string(currentContent), description, "web")
		}
	}

	// 創建磁盘备份
	var backupID string
	if configBackupMgr != nil {
		backupInfo, err := configBackupMgr.CreateBackup(configManager.GetConfigPath(), "通過YAML编辑器更新配置")
		if err == nil && backupInfo != nil {
			backupID = backupInfo.ID
		}
	}

	// 直接写入 YAML 文件（保持原始格式）
	if err := os.WriteFile(configManager.GetConfigPath(), []byte(yamlContent), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败: " + err.Error()})
		return
	}

	// 更新記憶體中的配置
	configManager.mu.Lock()
	configManager.currentConfig = newConfig
	configManager.mu.Unlock()

	// 尝試热更新
	if configHotReloader != nil {
		_, _ = configHotReloader.UpdateConfig(newConfig)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "配置更新成功",
		"backup_id":        backupID,
		"changes_count":    len(diff.Changes),
		"requires_restart": diff.RequiresRestart,
	})
}

// testNotificationHandler 测试通知发送
// POST /api/config/test-notification
func testNotificationHandler(c *gin.Context) {
	// 绑定前端传来的临时配置
	cfg, err := bindConfigFromJSONMap(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取要测试的渠道类型
	channel := c.Query("channel")
	if channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未指定测试渠道"})
		return
	}

	// 创建通知服务（使用传入的配置）
	ns := notify.NewNotificationService(cfg)

	// 创建测试事件
	testEvent := &event.Event{
		Type:      event.EventTypeError, // 使用 Error 类型以确保触发通知
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"message": fmt.Sprintf("这是一条来自 QuantMesh 的测试通知 [%s]", channel),
		},
	}

	// 查找对应的通知器
	// 注意：某些通知器的 Name() 包含额外信息，如 "Email (smtp)"
	// 前端传来的 channel 是简短名称如 "email"，需要支持前缀匹配
	var targetNotifier notify.Notifier
	for _, n := range ns.GetNotifiers() {
		name := n.Name()
		if strings.EqualFold(name, channel) || strings.HasPrefix(strings.ToLower(name), strings.ToLower(channel)) {
			targetNotifier = n
			break
		}
	}

	if targetNotifier == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("渠道 %s 未启用或配置不完整", channel)})
		return
	}

	// 同步发送通知以便获取结果
	if err := targetNotifier.Send(testEvent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"error":   fmt.Sprintf("发送测试通知失败: %v", err),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("测试通知已发送到 %s", channel),
	})
}

// getPriceRangeHandler 获取运行中交易对的实时价格范围
// GET /api/config/price-range?exchange=binance&symbol=BTCUSDT
func getPriceRangeHandler(c *gin.Context) {
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchangeName == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange 和 symbol 参数不能为空"})
		return
	}

	// 先尝试从运行时获取实时数据
	if symbolManagerProvider != nil {
		rtInterface, exists := symbolManagerProvider.Get(exchangeName, symbol)
		if exists {
			// 使用反射获取 SuperPositionManager
			rtVal := reflect.ValueOf(rtInterface)
			if rtVal.Kind() == reflect.Ptr {
				rtVal = rtVal.Elem()
			}
			spmField := rtVal.FieldByName("SuperPositionManager")
			if spmField.IsValid() && !spmField.IsNil() {
				// 调用 GetTradingParamsSummary
				method := spmField.MethodByName("GetTradingParamsSummary")
				if method.IsValid() {
					results := method.Call(nil)
					if len(results) > 0 {
						summary := results[0].Interface().(map[string]interface{})
						c.JSON(http.StatusOK, gin.H{
							"success": true,
							"source":  "runtime",
							"data":    summary,
						})
						return
					}
				}
			}
		}
	}

	// 如果没有运行时，从配置计算（静态模式）
	cfg, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取配置失败"})
		return
	}

	// 找到对应的 SymbolConfig
	for _, symCfg := range cfg.Trading.Symbols {
		if strings.EqualFold(symCfg.Exchange, exchangeName) && strings.EqualFold(symCfg.Symbol, symbol) {
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"source":  "config",
				"data": map[string]interface{}{
					"price_interval":   symCfg.PriceInterval,
					"order_quantity":   symCfg.OrderQuantity,
					"buy_window_size":  symCfg.BuyWindowSize,
					"sell_window_size": symCfg.SellWindowSize,
					"direction":        symCfg.GetDirection(),
					"current_price":    0,
					"message":          "交易对未运行，无法获取实时价格范围",
				},
			})
			return
		}
	}

	c.JSON(http.StatusNotFound, gin.H{"error": "未找到交易对配置"})
}
