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
	fileConfigManager    *FileConfigManager
	configBackupMgr    *config.BackupManager
	configHotReloader  *config.HotReloader
	configHistoryMgr   *config.HistoryManager
)

// FileConfigManager 配置文件管理器
type FileConfigManager struct {
	configPath    string
	currentConfig *config.Config
	mu            sync.RWMutex
}

// NewFileConfigManager 創建配置文件管理器
func NewFileConfigManager(configPath string) *FileConfigManager {
	return &FileConfigManager{
		configPath: configPath,
	}
}

// SetFileConfigManager 設置配置文件管理器
func SetFileConfigManager(cm *FileConfigManager) {
	fileConfigManager = cm
}

// SetConfigBackupManager 設置备份管理器
func SetConfigBackupManager(bm *config.BackupManager) {
	configBackupMgr = bm
}

// SetConfigHotReloader 設置熱重載器
func SetConfigHotReloader(hr *config.HotReloader) {
	configHotReloader = hr
}

// SetConfigHistoryManager 設置歷史管理器
func SetConfigHistoryManager(hm *config.HistoryManager) {
	configHistoryMgr = hm
}

// GetConfig 獲取當前配置
func GetConfig() *config.Config {
	if fileConfigManager == nil {
		return nil
	}
	return fileConfigManager.getCurrentConfig()
}

// getCurrentConfig 獲取當前配置（內部方法）
func (fcm *FileConfigManager) getCurrentConfig() *config.Config {
	fcm.mu.RLock()
	defer fcm.mu.RUnlock()
	return fcm.currentConfig
}

// GetLatestConfig 獲取最新配置（用於外部包獲取最新配置）
func GetLatestConfig() (*config.Config, error) {
	if fileConfigManager == nil {
		return nil, fmt.Errorf("配置管理器未初始化")
	}
	return fileConfigManager.GetConfig()
}

// SetSymbolEnabled 設置指定交易所/交易對的 enabled 状態，並持久化到配置文件。
//
// 用途：
// - StopTrading 時写回 enabled=false，确保重啟后不會自动再啟动
// - StartTrading 時写回 enabled=true，确保重啟后保持啟动
func SetSymbolEnabled(exchange, symbol string, enabled bool, marketType ...string) error {
	if fileConfigManager == nil {
		return fmt.Errorf("配置管理器未初始化")
	}
	if exchange == "" || symbol == "" {
		return fmt.Errorf("exchange 和 symbol 不能為空")
	}

	fileConfigManager.mu.Lock()
	defer fileConfigManager.mu.Unlock()

	// 确保有最新配置
	cfg := fileConfigManager.currentConfig
	if cfg == nil {
		loaded, err := config.LoadConfig(fileConfigManager.configPath)
		if err != nil {
			return err
		}
		cfg = loaded
	}

	mt := ""
	if len(marketType) > 0 {
		mt = marketType[0]
	}

	found := false
	for i := range cfg.Trading.Symbols {
		if strings.EqualFold(cfg.Trading.Symbols[i].Exchange, exchange) &&
			strings.EqualFold(cfg.Trading.Symbols[i].Symbol, symbol) {
			// 如果指定了 market_type，必須精確匹配
			if mt != "" && !strings.EqualFold(cfg.Trading.Symbols[i].GetMarketType(), mt) {
				continue
			}
			cfg.Trading.Symbols[i].SetEnabled(enabled)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("未找到交易對配置: %s:%s (market_type=%s)", exchange, symbol, mt)
	}

	// 保存到文件（含校驗/normalize）
	if err := config.SaveConfig(cfg, fileConfigManager.configPath); err != nil {
		return err
	}

	// 保存到历史
	if configHistoryMgr != nil {
		currentContent, err := os.ReadFile(fileConfigManager.configPath)
		if err == nil {
			description := fmt.Sprintf("自动更新交易對狀態: %s:%s -> %v", exchange, symbol, enabled)
			_, _ = configHistoryMgr.SaveHistory(string(currentContent), description, "system")
		}
	}

	// 更新記憶體中的配置
	fileConfigManager.currentConfig = cfg

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
func (fcm *FileConfigManager) GetConfig() (*config.Config, error) {
	fcm.mu.RLock()
	defer fcm.mu.RUnlock()

	if fcm.currentConfig != nil {
		return fcm.currentConfig, nil
	}

	// 如果記憶體中没有，從文件加載
	cfg, err := config.LoadConfig(fcm.configPath)
	if err != nil {
		return nil, err
	}

	fcm.currentConfig = cfg
	return cfg, nil
}

// GetConfigPath 獲取配置文件路径
func (fcm *FileConfigManager) GetConfigPath() string {
	return fcm.configPath
}

// UpdateConfig 更新配置
func (fcm *FileConfigManager) UpdateConfig(newConfig *config.Config) error {
	fcm.mu.Lock()
	defer fcm.mu.Unlock()

	// 保存當前配置到历史
	if configHistoryMgr != nil {
		currentContent, err := os.ReadFile(fcm.configPath)
		if err == nil {
			// 生成变更描述
			description := "通過 Web UI 更新配置"
			// 獲取舊配置用於對比（如果可能）
			if fcm.currentConfig != nil {
				diff := config.DiffConfig(fcm.currentConfig, newConfig)
				if len(diff.Changes) > 0 {
					description = fmt.Sprintf("通過 Web UI 修改了 %d 项配置", len(diff.Changes))
				}
			}
			_, _ = configHistoryMgr.SaveHistory(string(currentContent), description, "web")
		}
	}

	// 保存到文件
	if err := config.SaveConfig(newConfig, fcm.configPath); err != nil {
		return err
	}

	// 更新記憶體中的配置
	fcm.currentConfig = newConfig

	return nil
}

// getConfigHandler 獲取當前配置（YAML格式）
// GET /api/config
func getConfigHandler(c *gin.Context) {
	if fileConfigManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	cfg, err := fileConfigManager.GetConfig()
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
	if fileConfigManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	cfg, err := fileConfigManager.GetConfig()
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

// normalizeNumericStrings 递归遍历 map，将可解析为数字的字符串转为 float64/int。
// 前端 DecimalNumberInput 可能把 "70.000000" 这样的值作为字符串传入，
// 导致 YAML unmarshal 到 float64 字段时失败。
func normalizeNumericStrings(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, item := range val {
			val[k] = normalizeNumericStrings(item)
		}
		return val
	case []interface{}:
		for i, item := range val {
			val[i] = normalizeNumericStrings(item)
		}
		return val
	case string:
		if val == "" {
			return val
		}
		if n, err := strconv.ParseFloat(val, 64); err == nil {
			if intVal, intErr := strconv.ParseInt(val, 10, 64); intErr == nil {
				return intVal
			}
			return n
		}
		return val
	default:
		return val
	}
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

	normalizeNumericStrings(configMap)

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
	oldConfig, err := fileConfigManager.GetConfig()
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
	oldConfig, err := fileConfigManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取當前配置失败: " + err.Error()})
		return
	}

	// 生成差异
	diff := config.DiffConfig(oldConfig, newConfig)

	// 創建备份
	var backupInfo *config.BackupInfo
	if configBackupMgr != nil {
		backupInfo, err = configBackupMgr.CreateBackup(fileConfigManager.GetConfigPath(), "通過Web界面更新配置")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "創建备份失败: " + err.Error()})
			return
		}
	}

	// 保存配置
	if err := fileConfigManager.UpdateConfig(newConfig); err != nil {
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
	if err := configBackupMgr.RestoreBackup(backupID, fileConfigManager.GetConfigPath()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "恢複备份失败: " + err.Error()})
		return
	}

	// 重新加載配置
	cfg, err := config.LoadConfig(fileConfigManager.GetConfigPath())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重新加載配置失败: " + err.Error()})
		return
	}

	// 更新記憶體中的配置
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
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
	currentContent, err := os.ReadFile(fileConfigManager.GetConfigPath())
	if err == nil {
		_, _ = configHistoryMgr.SaveHistory(string(currentContent), fmt.Sprintf("恢複到版本 %d 前的自动备份", version), "system")
	}

	// 恢複到指定版本
	if err := configHistoryMgr.RestoreHistory(version); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "恢複失败: " + err.Error()})
		return
	}

	// 重新加載配置到記憶體
	cfg, err := config.LoadConfig(fileConfigManager.GetConfigPath())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "重新加載配置失败: " + err.Error()})
		return
	}

	// 更新記憶體中的配置
	fileConfigManager.mu.Lock()
	fileConfigManager.currentConfig = cfg
	fileConfigManager.mu.Unlock()

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
	oldConfig, err := fileConfigManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取當前配置失败: " + err.Error()})
		return
	}

	// 生成差异
	diff := config.DiffConfig(oldConfig, newConfig)

	// 保存當前配置到历史
	if configHistoryMgr != nil {
		currentContent, err := os.ReadFile(fileConfigManager.GetConfigPath())
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
		backupInfo, err := configBackupMgr.CreateBackup(fileConfigManager.GetConfigPath(), "通過YAML编辑器更新配置")
		if err == nil && backupInfo != nil {
			backupID = backupInfo.ID
		}
	}

	// 直接写入 YAML 文件（保持原始格式）
	if err := os.WriteFile(fileConfigManager.GetConfigPath(), []byte(yamlContent), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败: " + err.Error()})
		return
	}

	// 更新記憶體中的配置
	fileConfigManager.mu.Lock()
	fileConfigManager.currentConfig = newConfig
	fileConfigManager.mu.Unlock()

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

// mergeNotificationChannelConfig 当请求配置中某渠道的必填项为空时，从已保存配置补充
func mergeNotificationChannelConfig(cfg, saved *config.Config, channel string) {
	switch channel {
	case "email":
		if cfg.Notifications.Email.From == "" && saved.Notifications.Email.From != "" {
			cfg.Notifications.Email.From = saved.Notifications.Email.From
		}
		if cfg.Notifications.Email.To == "" && saved.Notifications.Email.To != "" {
			cfg.Notifications.Email.To = saved.Notifications.Email.To
		}
		if cfg.Notifications.Email.Provider == "" && saved.Notifications.Email.Provider != "" {
			cfg.Notifications.Email.Provider = saved.Notifications.Email.Provider
		}
		if cfg.Notifications.Email.Resend.APIKey == "" && saved.Notifications.Email.Resend.APIKey != "" {
			cfg.Notifications.Email.Resend.APIKey = saved.Notifications.Email.Resend.APIKey
		}
		if cfg.Notifications.Email.SMTP.Host == "" && saved.Notifications.Email.SMTP.Host != "" {
			cfg.Notifications.Email.SMTP = saved.Notifications.Email.SMTP
		}
	case "feishu":
		if cfg.Notifications.Feishu.Webhook == "" && saved.Notifications.Feishu.Webhook != "" {
			cfg.Notifications.Feishu.Webhook = saved.Notifications.Feishu.Webhook
		}
	case "dingtalk":
		if cfg.Notifications.DingTalk.Webhook == "" && saved.Notifications.DingTalk.Webhook != "" {
			cfg.Notifications.DingTalk.Webhook = saved.Notifications.DingTalk.Webhook
		}
		if cfg.Notifications.DingTalk.Secret == "" && saved.Notifications.DingTalk.Secret != "" {
			cfg.Notifications.DingTalk.Secret = saved.Notifications.DingTalk.Secret
		}
	case "wechat_work":
		if cfg.Notifications.WeChatWork.Webhook == "" && saved.Notifications.WeChatWork.Webhook != "" {
			cfg.Notifications.WeChatWork.Webhook = saved.Notifications.WeChatWork.Webhook
		}
	case "slack":
		if cfg.Notifications.Slack.Webhook == "" && saved.Notifications.Slack.Webhook != "" {
			cfg.Notifications.Slack.Webhook = saved.Notifications.Slack.Webhook
		}
	case "webhook":
		if cfg.Notifications.Webhook.URL == "" && saved.Notifications.Webhook.URL != "" {
			cfg.Notifications.Webhook.URL = saved.Notifications.Webhook.URL
		}
	case "telegram":
		if cfg.Notifications.Telegram.BotToken == "" && saved.Notifications.Telegram.BotToken != "" {
			cfg.Notifications.Telegram.BotToken = saved.Notifications.Telegram.BotToken
		}
		if cfg.Notifications.Telegram.ChatID == "" && saved.Notifications.Telegram.ChatID != "" {
			cfg.Notifications.Telegram.ChatID = saved.Notifications.Telegram.ChatID
		}
	}
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

	// 若请求中的渠道配置不完整，尝试从已保存配置补充（前端可能发送了稀疏 config）
	if fileConfigManager != nil {
		if saved, err := fileConfigManager.GetConfig(); err == nil && saved != nil {
			mergeNotificationChannelConfig(cfg, saved, strings.ToLower(channel))
		}
	}

	// 测试时临时启用被测试渠道：前端可能未勾选 notifications.enabled 或各渠道的 enabled，
	// 用户填好参数后直接点「测试连接」，应能测试成功
	cfg.Notifications.Enabled = true
	switch strings.ToLower(channel) {
	case "email":
		cfg.Notifications.Email.Enabled = true
		if cfg.Notifications.Email.Provider == "" {
			cfg.Notifications.Email.Provider = "smtp"
		}
	case "telegram":
		cfg.Notifications.Telegram.Enabled = true
	case "webhook":
		cfg.Notifications.Webhook.Enabled = true
	case "feishu":
		cfg.Notifications.Feishu.Enabled = true
	case "dingtalk":
		cfg.Notifications.DingTalk.Enabled = true
	case "wechat_work":
		cfg.Notifications.WeChatWork.Enabled = true
	case "slack":
		cfg.Notifications.Slack.Enabled = true
	}

	// 创建通知服务（使用传入的配置，configManager可以为nil因为这是测试函数）
	ns := notify.NewNotificationService(cfg, nil)

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
		// 返回更具体的错误提示，便于用户排查
		var hint string
		switch strings.ToLower(channel) {
		case "email":
			if cfg.Notifications.Email.From == "" || cfg.Notifications.Email.To == "" {
				hint = "请填写发件人和收件人"
			} else if cfg.Notifications.Email.Provider == "resend" && cfg.Notifications.Email.Resend.APIKey == "" {
				hint = "请填写 Resend API Key"
			} else if cfg.Notifications.Email.Provider == "smtp" && cfg.Notifications.Email.SMTP.Host == "" {
				hint = "请填写 SMTP 配置"
			}
		case "feishu":
			if cfg.Notifications.Feishu.Webhook == "" {
				hint = "请填写飞书 Webhook URL"
			}
		case "dingtalk":
			if cfg.Notifications.DingTalk.Webhook == "" {
				hint = "请填写钉钉 Webhook URL"
			}
		case "wechat_work":
			if cfg.Notifications.WeChatWork.Webhook == "" {
				hint = "请填写企业微信 Webhook URL"
			}
		case "slack":
			if cfg.Notifications.Slack.Webhook == "" {
				hint = "请填写 Slack Webhook URL"
			}
		case "webhook":
			if cfg.Notifications.Webhook.URL == "" {
				hint = "请填写 Webhook URL"
			}
		case "telegram":
			if cfg.Notifications.Telegram.BotToken == "" || cfg.Notifications.Telegram.ChatID == "" {
				hint = "请填写 Telegram Bot Token 和 Chat ID"
			}
		}
		if hint != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("渠道 %s 配置不完整: %s", channel, hint)})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("渠道 %s 未启用或配置不完整", channel)})
		}
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
	cfg, err := fileConfigManager.GetConfig()
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
