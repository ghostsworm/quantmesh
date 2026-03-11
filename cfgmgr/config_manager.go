package cfgmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/storage"
)

// ConfigManager 配置管理器
// 负责合并配置文件和数据库配置，支持热更新
type ConfigManager struct {
	cfg      *config.Config
	storage  storage.ConfigStorage
	mu       sync.RWMutex
	watchers []ConfigWatcher
	ctx      context.Context
	cancel   context.CancelFunc
}

// ConfigWatcher 配置变更监听器
type ConfigWatcher interface {
	OnConfigChanged(scope storage.ConfigScope, scopeID, key string, oldValue, newValue interface{})
}

// NewConfigManager 创建配置管理器
func NewConfigManager(cfg *config.Config, store storage.ConfigStorage) *ConfigManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &ConfigManager{
		cfg:     cfg,
		storage: store,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start 启动配置管理器
func (cm *ConfigManager) Start() error {
	// 从配置文件初始化默认配置到数据库
	if err := cm.initDefaultConfigs(); err != nil {
		logger.Warn("初始化默认配置失败: %v", err)
	}

	// 生成简化版配置文件（如果不存在）
	if err := cm.generateMinimalConfig(); err != nil {
		logger.Warn("生成简化版配置文件失败: %v", err)
	}

	// 启动配置监控
	go cm.watchConfigChanges()

	return nil
}

// Stop 停止配置管理器
func (cm *ConfigManager) Stop() {
	cm.cancel()
}

// AddWatcher 添加配置变更监听器
func (cm *ConfigManager) AddWatcher(watcher ConfigWatcher) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.watchers = append(cm.watchers, watcher)
}

// Get 获取配置值（合并数据库配置和配置文件）
func (cm *ConfigManager) Get(scope storage.ConfigScope, scopeID, key string) (interface{}, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// 先从数据库获取
	entry, err := cm.storage.GetConfig(cm.ctx, scope, scopeID, key)
	if err != nil {
		return nil, fmt.Errorf("查询配置失败: %w", err)
	}

	// 如果数据库有配置，使用数据库的值
	if entry != nil {
		return entry.GetTypedValue()
	}

	// 否则从配置文件获取默认值
	return cm.getDefaultValue(scope, scopeID, key)
}

// GetWithDefault 获取配置值，如果不存在返回默认值
func (cm *ConfigManager) GetWithDefault(scope storage.ConfigScope, scopeID, key string, defaultValue interface{}) interface{} {
	val, err := cm.Get(scope, scopeID, key)
	if err != nil || val == nil {
		return defaultValue
	}
	return val
}

// GetString 获取字符串配置
func (cm *ConfigManager) GetString(scope storage.ConfigScope, scopeID, key string) string {
	val := cm.GetWithDefault(scope, scopeID, key, "")
	if str, ok := val.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", val)
}

// GetInt 获取整数配置
func (cm *ConfigManager) GetInt(scope storage.ConfigScope, scopeID, key string) int {
	val := cm.GetWithDefault(scope, scopeID, key, 0)
	switch v := val.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// GetFloat64 获取浮点数配置
func (cm *ConfigManager) GetFloat64(scope storage.ConfigScope, scopeID, key string) float64 {
	val := cm.GetWithDefault(scope, scopeID, key, 0.0)
	switch v := val.(type) {
	case float32:
		return float64(v)
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0.0
}

// GetBool 获取布尔配置
func (cm *ConfigManager) GetBool(scope storage.ConfigScope, scopeID, key string) bool {
	val := cm.GetWithDefault(scope, scopeID, key, false)
	if b, ok := val.(bool); ok {
		return b
	}
	return false
}

// Set 设置配置值（保存到数据库）
func (cm *ConfigManager) Set(scope storage.ConfigScope, scopeID, key string, value interface{}, updatedBy string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 获取旧值
	oldValue, _ := cm.Get(scope, scopeID, key)

	// 创建配置条目
	entry, err := storage.NewConfigEntry(scope, scopeID, key, value, "", "", "")
	if err != nil {
		return fmt.Errorf("创建配置条目失败: %w", err)
	}

	// 保存到数据库
	if err := cm.storage.SetConfig(cm.ctx, entry, updatedBy); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	// 通知监听器
	cm.notifyWatchers(scope, scopeID, key, oldValue, value)

	logger.Info("配置已更新: %s.%s.%s = %v (操作人: %s)", scope, scopeID, key, value, updatedBy)
	return nil
}

// Delete 删除配置（恢复默认值）
func (cm *ConfigManager) Delete(scope storage.ConfigScope, scopeID, key string, updatedBy string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 获取旧值
	oldValue, _ := cm.Get(scope, scopeID, key)

	// 从数据库删除
	if err := cm.storage.DeleteConfig(cm.ctx, scope, scopeID, key); err != nil {
		return fmt.Errorf("删除配置失败: %w", err)
	}

	// 获取默认值
	defaultValue, _ := cm.getDefaultValue(scope, scopeID, key)

	// 通知监听器
	cm.notifyWatchers(scope, scopeID, key, oldValue, defaultValue)

	logger.Info("配置已删除，恢复默认值: %s.%s.%s = %v (操作人: %s)", scope, scopeID, key, defaultValue, updatedBy)
	return nil
}

// GetConfigsByScope 按作用域获取所有配置
func (cm *ConfigManager) GetConfigsByScope(scope storage.ConfigScope, scopeID string) (map[string]interface{}, error) {
	entries, err := cm.storage.GetConfigsByScope(cm.ctx, scope, scopeID)
	if err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	for _, entry := range entries {
		val, err := entry.GetTypedValue()
		if err == nil {
			result[entry.Key] = val
		}
	}

	return result, nil
}

// GetConfigHistory 获取配置变更历史
func (cm *ConfigManager) GetConfigHistory(scope storage.ConfigScope, scopeID, key string, limit int) ([]*storage.ConfigHistory, error) {
	return cm.storage.GetConfigHistoryByKey(cm.ctx, scope, scopeID, key, limit)
}

// initDefaultConfigs 从配置文件初始化默认配置
func (cm *ConfigManager) initDefaultConfigs() error {
	var entries []*storage.ConfigEntry

	// 通知配置
	entries = append(entries, cm.initNotificationConfigs()...)

	// 交易配置
	entries = append(entries, cm.initTradingConfigs()...)

	// 系统配置
	entries = append(entries, cm.initSystemConfigs()...)

	// Bot 配置
	entries = append(entries, cm.initBotConfigs()...)

	// 初始化到数据库
	return cm.storage.InitializeConfigs(cm.ctx, entries)
}

// initNotificationConfigs 初始化通知配置
func (cm *ConfigManager) initNotificationConfigs() []*storage.ConfigEntry {
	return []*storage.ConfigEntry{
		// 通知开关
		{
			Key:      "enabled",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeBoolean,
			BoolValue: cm.cfg.Notifications.Enabled,
			Value:     fmt.Sprintf("%v", cm.cfg.Notifications.Enabled),
			Category:  "notifications",
			DisplayName: "启用通知",
			Description: "是否启用通知功能",
			Editable:   true,
		},
		// 通知规则
		{
			Key:      "rules.order_placed",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeBoolean,
			BoolValue: cm.cfg.Notifications.Rules.OrderPlaced,
			Value:     fmt.Sprintf("%v", cm.cfg.Notifications.Rules.OrderPlaced),
			Category:  "notifications",
			DisplayName: "下单时通知",
			Description: "下单成功时发送通知",
			Editable:   true,
		},
		{
			Key:      "rules.order_filled",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeBoolean,
			BoolValue: cm.cfg.Notifications.Rules.OrderFilled,
			Value:     fmt.Sprintf("%v", cm.cfg.Notifications.Rules.OrderFilled),
			Category:  "notifications",
			DisplayName: "成交时通知",
			Description: "订单成交时发送通知",
			Editable:   true,
		},
		{
			Key:      "rules.error",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeBoolean,
			BoolValue: cm.cfg.Notifications.Rules.Error,
			Value:     fmt.Sprintf("%v", cm.cfg.Notifications.Rules.Error),
			Category:  "notifications",
			DisplayName: "错误时通知",
			Description: "发生错误时发送通知",
			Editable:   true,
		},
		{
			Key:      "rules.margin_insufficient",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeBoolean,
			BoolValue: cm.cfg.Notifications.Rules.MarginInsufficient,
			Value:     fmt.Sprintf("%v", cm.cfg.Notifications.Rules.MarginInsufficient),
			Category:  "notifications",
			DisplayName: "保证金不足通知",
			Description: "保证金不足时发送通知",
			Editable:   true,
		},
		{
			Key:      "rules.risk_triggered",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeBoolean,
			BoolValue: cm.cfg.Notifications.Rules.RiskTriggered,
			Value:     fmt.Sprintf("%v", cm.cfg.Notifications.Rules.RiskTriggered),
			Category:  "notifications",
			DisplayName: "风控触发通知",
			Description: "风控被触发时发送通知",
			Editable:   true,
		},
		{
			Key:      "rules.stop_loss",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeBoolean,
			BoolValue: cm.cfg.Notifications.Rules.StopLoss,
			Value:     fmt.Sprintf("%v", cm.cfg.Notifications.Rules.StopLoss),
			Category:  "notifications",
			DisplayName: "止损通知",
			Description: "触发止损时发送通知",
			Editable:   true,
		},
		{
			Key:      "rules.allocation_exceeded",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeBoolean,
			BoolValue: cm.cfg.Notifications.Rules.AllocationExceeded,
			Value:     fmt.Sprintf("%v", cm.cfg.Notifications.Rules.AllocationExceeded),
			Category:  "notifications",
			DisplayName: "分配超限通知",
			Description: "超出资金分配限制时发送通知",
			Editable:   true,
		},
		{
			Key:      "rules.inspector_report",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeBoolean,
			BoolValue: cm.cfg.Notifications.Rules.InspectorReport,
			Value:     fmt.Sprintf("%v", cm.cfg.Notifications.Rules.InspectorReport),
			Category:  "notifications",
			DisplayName: "巡检报告通知",
			Description: "智能子巡检报告通知",
			Editable:   true,
		},
	}
}

// initTradingConfigs 初始化交易配置
func (cm *ConfigManager) initTradingConfigs() []*storage.ConfigEntry {
	return []*storage.ConfigEntry{
		{
			Key:      "position_safety_check",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeNumber,
			NumberValue: float64(cm.cfg.Trading.PositionSafetyCheck),
			Value:     fmt.Sprintf("%d", cm.cfg.Trading.PositionSafetyCheck),
			Category:  "trading",
			DisplayName: "持仓安全检查",
			Description: "最少能向下持有多少仓",
			Editable:   true,
		},
	}
}

// initSystemConfigs 初始化系统配置
func (cm *ConfigManager) initSystemConfigs() []*storage.ConfigEntry {
	return []*storage.ConfigEntry{
		{
			Key:      "log_level",
			Scope:    storage.ScopeGlobal,
			ScopeID:  "",
			Type:     storage.TypeString,
			Value:     cm.cfg.System.LogLevel,
			Category:  "system",
			DisplayName: "日志级别",
			Description: "系统日志级别 (debug, info, warn, error)",
			Editable:   true,
		},
	}
}

// initBotConfigs 初始化 Bot 配置
func (cm *ConfigManager) initBotConfigs() []*storage.ConfigEntry {
	var entries []*storage.ConfigEntry

	for _, bot := range cm.cfg.Bots {
		botID := bot.ID
		if botID == "" {
			botID = config.GenerateBotID(bot.Exchange, bot.Symbol, bot.MarketType)
		}

		// Bot 基本配置
		entries = append(entries, &storage.ConfigEntry{
			Key:         "enabled",
			Scope:       storage.ScopeBot,
			ScopeID:     botID,
			Type:        storage.TypeBoolean,
			BoolValue:   bot.Enabled != nil && *bot.Enabled,
			Value:       fmt.Sprintf("%v", bot.Enabled != nil && *bot.Enabled),
			Category:    "bot",
			DisplayName: "启用 Bot",
			Description: fmt.Sprintf("是否启用 Bot %s", bot.Name),
			Editable:    true,
		})

		// 交易参数配置
		entries = append(entries, &storage.ConfigEntry{
			Key:         "price_interval",
			Scope:       storage.ScopeBot,
			ScopeID:     botID,
			Type:        storage.TypeNumber,
			NumberValue: bot.PriceInterval,
			Value:       fmt.Sprintf("%.2f", bot.PriceInterval),
			Category:    "trading",
			DisplayName: "价格间隔",
			Description: "网格价格间隔",
			Editable:    true,
		})

		entries = append(entries, &storage.ConfigEntry{
			Key:         "profit_spread",
			Scope:       storage.ScopeBot,
			ScopeID:     botID,
			Type:        storage.TypeNumber,
			NumberValue: bot.ProfitSpread,
			Value:       fmt.Sprintf("%.2f", bot.ProfitSpread),
			Category:    "trading",
			DisplayName: "利润间距",
			Description: "平仓价差",
			Editable:    true,
		})

		entries = append(entries, &storage.ConfigEntry{
			Key:         "order_quantity",
			Scope:       storage.ScopeBot,
			ScopeID:     botID,
			Type:        storage.TypeNumber,
			NumberValue: bot.OrderQuantity,
			Value:       fmt.Sprintf("%.2f", bot.OrderQuantity),
			Category:    "trading",
			DisplayName: "订单金额",
			Description: "每单金额（USDT）",
			Editable:    true,
		})

		entries = append(entries, &storage.ConfigEntry{
			Key:         "min_order_value",
			Scope:       storage.ScopeBot,
			ScopeID:     botID,
			Type:        storage.TypeNumber,
			NumberValue: bot.MinOrderValue,
			Value:       fmt.Sprintf("%.2f", bot.MinOrderValue),
			Category:    "trading",
			DisplayName: "最小订单金额",
			Description: "最小订单金额（USDT）",
			Editable:    true,
		})

		entries = append(entries, &storage.ConfigEntry{
			Key:         "buy_window_size",
			Scope:       storage.ScopeBot,
			ScopeID:     botID,
			Type:        storage.TypeNumber,
			NumberValue: float64(bot.BuyWindowSize),
			Value:       fmt.Sprintf("%d", bot.BuyWindowSize),
			Category:    "trading",
			DisplayName: "买单窗口",
			Description: "买单窗口大小",
			Editable:    true,
		})

		entries = append(entries, &storage.ConfigEntry{
			Key:         "sell_window_size",
			Scope:       storage.ScopeBot,
			ScopeID:     botID,
			Type:        storage.TypeNumber,
			NumberValue: float64(bot.SellWindowSize),
			Value:       fmt.Sprintf("%d", bot.SellWindowSize),
			Category:    "trading",
			DisplayName: "卖单窗口",
			Description: "卖单窗口大小",
			Editable:    true,
		})

		entries = append(entries, &storage.ConfigEntry{
			Key:         "position_safety_check",
			Scope:       storage.ScopeBot,
			ScopeID:     botID,
			Type:        storage.TypeNumber,
			NumberValue: float64(bot.PositionSafetyCheck),
			Value:       fmt.Sprintf("%d", bot.PositionSafetyCheck),
			Category:    "trading",
			DisplayName: "持仓安全检查",
			Description: "最少能向下持有多少仓",
			Editable:    true,
		})

		// 三级火箭网格（JSON 存储）
		if bot.RocketTieredGrid != nil {
			jsonBytes, _ := json.Marshal(bot.RocketTieredGrid)
			entries = append(entries, &storage.ConfigEntry{
				Key:         "rocket_tiered_grid",
				Scope:       storage.ScopeBot,
				ScopeID:     botID,
				Type:        storage.TypeJSON,
				JSONValue:   string(jsonBytes),
				Value:       string(jsonBytes),
				Category:    "trading",
				DisplayName: "三级火箭网格",
				Description: "小波动小网格、大波动大网格，根据持仓层数自动切换间距",
				Editable:    true,
			})
		}

		// 风控配置
		entries = append(entries, &storage.ConfigEntry{
			Key:         "grid_risk_control.enabled",
			Scope:       storage.ScopeBot,
			ScopeID:     botID,
			Type:        storage.TypeBoolean,
			BoolValue:   bot.GridRiskControl.Enabled,
			Value:       fmt.Sprintf("%v", bot.GridRiskControl.Enabled),
			Category:    "risk_control",
			DisplayName: "启用网格风控",
			Description: "是否启用网格策略风控",
			Editable:    true,
		})

		entries = append(entries, &storage.ConfigEntry{
			Key:         "grid_risk_control.stop_loss_ratio",
			Scope:       storage.ScopeBot,
			ScopeID:     botID,
			Type:        storage.TypeNumber,
			NumberValue: bot.GridRiskControl.StopLossRatio,
			Value:       fmt.Sprintf("%.4f", bot.GridRiskControl.StopLossRatio),
			Category:    "risk_control",
			DisplayName: "止损比例",
			Description: "单币种最大浮亏比例",
			Editable:    true,
		})
	}

	return entries
}

// getDefaultValue 从配置文件获取默认值
func (cm *ConfigManager) getDefaultValue(scope storage.ConfigScope, scopeID, key string) (interface{}, error) {
	switch scope {
	case storage.ScopeGlobal:
		return cm.getGlobalDefault(key)
	case storage.ScopeBot:
		return cm.getBotDefault(scopeID, key)
	default:
		return nil, fmt.Errorf("未知的配置作用域: %s", scope)
	}
}

// getGlobalDefault 获取全局配置默认值
func (cm *ConfigManager) getGlobalDefault(key string) (interface{}, error) {
	// 通知配置
	if key == "notifications.enabled" {
		return cm.cfg.Notifications.Enabled, nil
	}
	if key == "notifications.rules.order_placed" {
		return cm.cfg.Notifications.Rules.OrderPlaced, nil
	}
	if key == "notifications.rules.order_filled" {
		return cm.cfg.Notifications.Rules.OrderFilled, nil
	}
	if key == "notifications.rules.error" {
		return cm.cfg.Notifications.Rules.Error, nil
	}
	if key == "notifications.rules.margin_insufficient" {
		return cm.cfg.Notifications.Rules.MarginInsufficient, nil
	}
	if key == "notifications.rules.risk_triggered" {
		return cm.cfg.Notifications.Rules.RiskTriggered, nil
	}
	if key == "notifications.rules.stop_loss" {
		return cm.cfg.Notifications.Rules.StopLoss, nil
	}
	if key == "notifications.rules.allocation_exceeded" {
		return cm.cfg.Notifications.Rules.AllocationExceeded, nil
	}
	if key == "notifications.rules.inspector_report" {
		return cm.cfg.Notifications.Rules.InspectorReport, nil
	}

	// 交易配置
	if key == "trading.position_safety_check" {
		return cm.cfg.Trading.PositionSafetyCheck, nil
	}

	// 系统配置
	if key == "system.log_level" {
		return cm.cfg.System.LogLevel, nil
	}

	return nil, fmt.Errorf("未找到配置: %s", key)
}

// getBotDefault 获取机器人配置默认值
func (cm *ConfigManager) getBotDefault(botID, key string) (interface{}, error) {
	// 从配置文件中查找对应机器人的配置
	for _, bot := range cm.cfg.Bots {
		if bot.ID == botID || config.GenerateBotID(bot.Exchange, bot.Symbol, bot.GetMarketType()) == botID {
			// 根据key返回对应的值
			switch key {
			case "enabled":
				return bot.Enabled, nil
			case "position_safety_check":
				return bot.PositionSafetyCheck, nil
			case "price_interval":
				return bot.PriceInterval, nil
			case "profit_spread":
				return bot.ProfitSpread, nil
			case "rocket_tiered_grid":
				return bot.RocketTieredGrid, nil
			// 可以添加更多配置项
			}
		}
	}
	return nil, fmt.Errorf("未找到机器人配置: %s.%s", botID, key)
}

// notifyWatchers 通知所有监听器配置已变更
func (cm *ConfigManager) notifyWatchers(scope storage.ConfigScope, scopeID, key string, oldValue, newValue interface{}) {
	for _, watcher := range cm.watchers {
		watcher.OnConfigChanged(scope, scopeID, key, oldValue, newValue)
	}
}

// watchConfigChanges 监控配置变更（定期检查数据库）
func (cm *ConfigManager) watchConfigChanges() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	lastVersion := make(map[string]int64)

	for {
		select {
		case <-cm.ctx.Done():
			return
		case <-ticker.C:
			// 检查配置变更
			entries, err := cm.storage.GetAllConfigs(cm.ctx)
			if err != nil {
				logger.Warn("检查配置变更失败: %v", err)
				continue
			}

			for _, entry := range entries {
				key := fmt.Sprintf("%s.%s.%s", entry.Scope, entry.ScopeID, entry.Key)
				if lastVer, ok := lastVersion[key]; !ok || entry.Version > lastVer {
					lastVersion[key] = entry.Version
					// 通知监听器
					for _, watcher := range cm.watchers {
						val, _ := entry.GetTypedValue()
						watcher.OnConfigChanged(entry.Scope, entry.ScopeID, entry.Key, nil, val)
					}
				}
			}
		}
	}
}

// generateMinimalConfig 生成简化版配置文件
// 只有在简化版配置文件不存在时才生成，避免覆盖用户自定义的配置
func (cm *ConfigManager) generateMinimalConfig() error {
	minimalConfigPath := "config.minimal.yaml"

	// 检查简化版配置文件是否已存在
	if _, err := config.LoadConfig(minimalConfigPath); err == nil {
		// 文件已存在且可加载，不需要重新生成
		logger.Debug("简化版配置文件已存在: %s", minimalConfigPath)
		return nil
	}

	logger.Info("📝 正在生成简化版配置文件: %s", minimalConfigPath)

	// 创建简化版配置
	minimalCfg := &config.Config{}

	// === 必须保留在配置文件中的配置 ===

	// 数据库配置（必须）
	minimalCfg.Database.Type = cm.cfg.Database.Type
	if minimalCfg.Database.Type == "" {
		minimalCfg.Database.Type = "sqlite"
	}
	minimalCfg.Database.DSN = cm.cfg.Database.DSN

	// 存储配置（必须）
	minimalCfg.Storage.Enabled = cm.cfg.Storage.Enabled
	minimalCfg.Storage.Type = cm.cfg.Storage.Type
	minimalCfg.Storage.Path = cm.cfg.Storage.Path
	minimalCfg.Storage.BufferSize = cm.cfg.Storage.BufferSize
	minimalCfg.Storage.BatchSize = cm.cfg.Storage.BatchSize
	minimalCfg.Storage.FlushInterval = cm.cfg.Storage.FlushInterval

	// Web 服务配置（必须）
	minimalCfg.Web.Enabled = cm.cfg.Web.Enabled
	minimalCfg.Web.Host = cm.cfg.Web.Host
	minimalCfg.Web.Port = cm.cfg.Web.Port
	minimalCfg.Web.APIKey = cm.cfg.Web.APIKey

	// 系统配置（必须）
	minimalCfg.System.LogLevel = cm.cfg.System.LogLevel
	minimalCfg.System.Timezone = cm.cfg.System.Timezone
	minimalCfg.System.CancelOnExit = cm.cfg.System.CancelOnExit
	minimalCfg.System.ClosePositionsOnExit = cm.cfg.System.ClosePositionsOnExit

	// 交易所配置（必须，至少需要 API 密钥）
	minimalCfg.Exchanges = make(map[string]config.ExchangeConfig)
	for name, exchange := range cm.cfg.Exchanges {
		// 只保存必要的交易所配置
		minimalCfg.Exchanges[name] = config.ExchangeConfig{
			APIKey:     exchange.APIKey,
			SecretKey:  exchange.SecretKey,
			Passphrase: exchange.Passphrase,
		}
	}

	// Bot 配置（如果有的话，保留基本结构）
	if len(cm.cfg.Bots) > 0 {
		minimalCfg.Bots = make([]config.BotConfig, 0, len(cm.cfg.Bots))
		for _, bot := range cm.cfg.Bots {
			// 只保留最基础的 Bot 配置
			minimalBot := config.BotConfig{
				ID:         bot.ID,
				Name:       bot.Name,
				Exchange:   bot.Exchange,
				Symbol:     bot.Symbol,
				MarketType: bot.MarketType,
				Enabled:    bot.Enabled,
			}
			minimalCfg.Bots = append(minimalCfg.Bots, minimalBot)
		}
	}

	// 日志清理配置
	minimalCfg.System.LogRetentionDays = cm.cfg.System.LogRetentionDays
	minimalCfg.System.LogCleanup.Enabled = cm.cfg.System.LogCleanup.Enabled
	minimalCfg.System.LogCleanup.Schedule = cm.cfg.System.LogCleanup.Schedule
	minimalCfg.System.LogCleanup.RetentionDays = cm.cfg.System.LogCleanup.RetentionDays
	minimalCfg.System.LogCleanup.LevelsToClean = cm.cfg.System.LogCleanup.LevelsToClean

	// 保存简化版配置文件
	if err := config.SaveConfigWithoutValidation(minimalCfg, minimalConfigPath); err != nil {
		return fmt.Errorf("保存简化版配置文件失败: %w", err)
	}

	logger.Info("✅ 简化版配置文件已生成: %s", minimalConfigPath)
	logger.Info("💡 提示: 以下配置已从配置文件移到数据库，可通过 Web UI 修改:")
	logger.Info("   - 通知配置（开关、规则等）")
	logger.Info("   - 交易配置（持仓安全检查等）")
	logger.Info("   - 系统日志级别")
	logger.Info("💡 如需使用简化版配置，请执行: mv config.minimal.yaml config.yaml")

	return nil
}
