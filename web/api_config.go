package web

import (
	"context"
	"encoding/json"
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
	"quantmesh/storage"
)

var (
	// 全局配置管理器（需要從main.go注入）
	fileConfigManager *FileConfigManager
	configHotReloader *config.HotReloader
	// primaryStorageForAppConfig 主庫（SQLStorage，含 MySQL），用於持久化 app_config
	primaryStorageForAppConfig storage.Storage
	// newsMonitorRuntimeSync 主進程在保存完整 app_config 後同步新聞監控運行時（由 main 注入，可為 nil）
	newsMonitorRuntimeSync func(*config.Config)
)

// SetNewsMonitorRuntimeSync 註冊「完整主配置寫入後」的回調，用於 NewsMonitor / 價格記錄等與配置指針同步。
func SetNewsMonitorRuntimeSync(fn func(*config.Config)) {
	newsMonitorRuntimeSync = fn
}

func notifyNewsMonitorRuntimeSync(cfg *config.Config) {
	if cfg == nil || newsMonitorRuntimeSync == nil {
		return
	}
	newsMonitorRuntimeSync(cfg)
}

// SetPrimaryStorageForAppConfig 設置主庫存儲（啟動時注入），用於寫入 app_config。
func SetPrimaryStorageForAppConfig(st storage.Storage) {
	primaryStorageForAppConfig = st
}

// persistAppConfigToDB 將完整主配置寫入主庫 app_config（唯一持久化來源）。
func persistAppConfigToDB(cfg *config.Config, operator, source string) error {
	if cfg == nil {
		return fmt.Errorf("配置為空")
	}
	if primaryStorageForAppConfig == nil {
		return fmt.Errorf("主庫未初始化，無法持久化主配置（請檢查 storage.enabled）")
	}
	_, err := storage.SaveAppConfigSnapshot(context.Background(), primaryStorageForAppConfig, cfg, operator, source)
	return err
}

func loadConfigFromPrimaryDB() (*config.Config, error) {
	if primaryStorageForAppConfig == nil {
		return nil, fmt.Errorf("主庫未初始化")
	}
	ss, ok := primaryStorageForAppConfig.(*storage.SQLStorage)
	if !ok || ss == nil {
		return nil, fmt.Errorf("主庫類型異常")
	}
	doc, err := ss.GetAppConfigDocument(context.Background())
	if err != nil {
		return nil, err
	}
	if doc == nil || doc.Revision < 1 || strings.TrimSpace(doc.Content) == "" {
		return nil, nil
	}
	return config.LoadConfigFromJSON([]byte(doc.Content))
}

// FileConfigManager 運行時主配置持有者（權威數據來自內存與主庫 app_config，不再讀寫磁盤 YAML）。
type FileConfigManager struct {
	currentConfig *config.Config
	mu            sync.RWMutex
}

// NewFileConfigManager 創建配置管理器（保留函數名以兼容 main；不再使用磁盤路徑）。
func NewFileConfigManager(_ string) *FileConfigManager {
	return &FileConfigManager{}
}

// SetRuntimeConfig 將主進程已加載的配置寫入管理器（與磁盤一致或來自 app_config）。
// 避免首次 GET /api/config/json 僅從可能為「最小化」的 YAML 讀取，導致 exchanges 密鑰為空，
// 進而前端無法調用 exchange-symbols 拉取交易對。
func (fcm *FileConfigManager) SetRuntimeConfig(cfg *config.Config) {
	if fcm == nil || cfg == nil {
		return
	}
	fcm.mu.Lock()
	defer fcm.mu.Unlock()
	fcm.currentConfig = cfg
}

// SetFileConfigManager 設置配置文件管理器
func SetFileConfigManager(cm *FileConfigManager) {
	fileConfigManager = cm
}

// SetConfigHotReloader 設置熱重載器
func SetConfigHotReloader(hr *config.HotReloader) {
	configHotReloader = hr
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
		loaded, err := loadConfigFromPrimaryDB()
		if err != nil || loaded == nil {
			if err != nil {
				return err
			}
			return fmt.Errorf("無法加載配置（內存為空且主庫無快照）")
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

	if err := persistAppConfigToDB(cfg, "system", "symbol_enabled"); err != nil {
		return err
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
	if fcm.currentConfig != nil {
		c := fcm.currentConfig
		fcm.mu.RUnlock()
		return c, nil
	}
	fcm.mu.RUnlock()

	fcm.mu.Lock()
	defer fcm.mu.Unlock()
	if fcm.currentConfig != nil {
		return fcm.currentConfig, nil
	}
	cfg, err := loadConfigFromPrimaryDB()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("無可用配置（請先完成引導或確保主庫 app_config 有快照）")
	}
	fcm.currentConfig = cfg
	return cfg, nil
}

// GetConfigPath 已廢棄：主配置不再使用磁盤 YAML，保留方法返回空字串避免舊代碼崩潰。
func (fcm *FileConfigManager) GetConfigPath() string {
	return ""
}

// UpdateConfig 更新配置
func (fcm *FileConfigManager) UpdateConfig(newConfig *config.Config) error {
	fcm.mu.Lock()
	defer fcm.mu.Unlock()

	if err := newConfig.Validate(); err != nil {
		return err
	}
	if err := persistAppConfigToDB(newConfig, "web", "file_config_update"); err != nil {
		return err
	}
	fcm.currentConfig = newConfig
	notifyNewsMonitorRuntimeSync(newConfig)
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
	if fileConfigManager == nil {
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
	if fileConfigManager == nil {
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
		"diff":             diff,
		"requires_restart": diff.RequiresRestart,
		"hot_updated":      updatedSymbols,
	})
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
	if fileConfigManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}

	// 读取请求体中的 YAML 内容
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败: " + err.Error()})
		return
	}

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

	if err := fileConfigManager.UpdateConfig(newConfig); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存配置失败: " + err.Error()})
		return
	}

	// 尝試热更新
	if configHotReloader != nil {
		_, _ = configHotReloader.UpdateConfig(newConfig)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":          "配置更新成功",
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

// parseSecurityFromAppConfigRaw 從主庫 app_config JSON 讀取 security 段（該段不在 config.Config 結構體中）。
func parseSecurityFromAppConfigRaw() (encryptionEnabled bool, masterKeyPath string) {
	masterKeyPath = config.DefaultMasterKeyPath
	if primaryStorageForAppConfig == nil {
		return false, masterKeyPath
	}
	ss, ok := primaryStorageForAppConfig.(*storage.SQLStorage)
	if !ok || ss == nil {
		return false, masterKeyPath
	}
	doc, err := ss.GetAppConfigDocument(context.Background())
	if err != nil || doc == nil || strings.TrimSpace(doc.Content) == "" {
		return false, masterKeyPath
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(doc.Content), &raw); err != nil {
		return false, masterKeyPath
	}
	sec, ok := raw["security"].(map[string]interface{})
	if !ok {
		return false, masterKeyPath
	}
	if v, ok := sec["encryption_enabled"].(bool); ok {
		encryptionEnabled = v
	}
	if p, ok := sec["master_key_path"].(string); ok {
		if s := strings.TrimSpace(p); s != "" {
			masterKeyPath = s
		}
	}
	return encryptionEnabled, masterKeyPath
}

func masterKeyFileOrEnvPresent(masterKeyPath string) bool {
	if os.Getenv(config.MasterKeyEnvVar) != "" {
		return true
	}
	_, err := os.Stat(masterKeyPath)
	return err == nil
}

// getConfigSecurityStatusHandler 獲取 API 密鑰加密相關狀態
// GET /api/config/security/status
func getConfigSecurityStatusHandler(c *gin.Context) {
	if fileConfigManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}
	encryptionEnabled, masterKeyPath := parseSecurityFromAppConfigRaw()
	c.JSON(http.StatusOK, gin.H{
		"encryption_enabled": encryptionEnabled,
		"master_key_path":    masterKeyPath,
		"master_key_exists":  masterKeyFileOrEnvPresent(masterKeyPath),
	})
}

// postConfigSecurityGenerateKeyHandler 在磁盤上生成主密鑰文件（環境變量優先時不寫文件）
// POST /api/config/security/generate-key
func postConfigSecurityGenerateKeyHandler(c *gin.Context) {
	if fileConfigManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "配置管理器未初始化"})
		return
	}
	if os.Getenv(config.MasterKeyEnvVar) != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "已通過環境變量設置主密鑰，無需生成密鑰文件"})
		return
	}
	_, masterKeyPath := parseSecurityFromAppConfigRaw()
	if masterKeyFileOrEnvPresent(masterKeyPath) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "主密鑰已存在（環境變量或密鑰文件）"})
		return
	}
	if _, err := config.LoadOrGenerateMasterKey(masterKeyPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"master_key_path": masterKeyPath})
}
