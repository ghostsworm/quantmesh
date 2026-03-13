package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// BotConfigFile Bot 独立配置文件结构
// 存储在 bots/{bot_id}/config.yaml
type BotConfigFile struct {
	// 基础信息
	BotID     string `yaml:"bot_id" json:"bot_id"`
	Name      string `yaml:"name" json:"name"`
	CreatedAt string `yaml:"created_at" json:"created_at"` // ISO 8601 格式
	UpdatedAt string `yaml:"updated_at" json:"updated_at"` // ISO 8601 格式

	// 交易配置
	Exchange   string `yaml:"exchange" json:"exchange"`
	Symbol     string `yaml:"symbol" json:"symbol"`
	MarketType string `yaml:"market_type" json:"market_type"` // spot/futures
	Testnet    bool   `yaml:"testnet" json:"testnet"`

	// 策略配置（支持多策略）
	StrategyMode string               `yaml:"strategy_mode" json:"strategy_mode"` // single/multi/hybrid
	Strategies   []BotStrategyConfig  `yaml:"strategies" json:"strategies"`

	// 混合策略配置（当 strategy_mode 为 hybrid 时使用）
	HybridStrategy *HybridStrategyConfig `yaml:"hybrid_strategy,omitempty" json:"hybrid_strategy,omitempty"`

	// 资金配置
	Capital CapitalConfig `yaml:"capital" json:"capital"`

	// 网格配置（适用于网格策略）
	Grid GridConfig `yaml:"grid,omitempty" json:"grid,omitempty"`

	// 风控配置
	RiskControl RiskControlConfig `yaml:"risk_control" json:"risk_control"`

	// 高级配置
	Advanced AdvancedConfig `yaml:"advanced,omitempty" json:"advanced,omitempty"`

	// 对冲配置（仅在对冲组中的 bot 使用）
	Hedge *HedgeBotConfig `yaml:"hedge,omitempty" json:"hedge,omitempty"`
}

// BotStrategyConfig 单个策略的完整配置（Bot 配置文件专用）
type BotStrategyConfig struct {
	Type     string                 `yaml:"type" json:"type"`           // grid, dca, momentum, trend_following, etc.
	Enabled  bool                   `yaml:"enabled" json:"enabled"`
	Weight   float64                `yaml:"weight" json:"weight"`       // 策略权重（多策略时分配资金）
	Params   map[string]interface{} `yaml:"params,omitempty" json:"params,omitempty"`   // 策略特定参数
	Settings map[string]interface{} `yaml:"settings,omitempty" json:"settings,omitempty"` // 策略设置
}

// CapitalConfig 资金配置
type CapitalConfig struct {
	TotalAllocated float64 `yaml:"total_allocated" json:"total_allocated"` // 总分配资金
	PerStrategy     bool    `yaml:"per_strategy" json:"per_strategy"`       // 是否按策略分配资金
	Withdrawal      WithdrawalPolicy `yaml:"withdrawal" json:"withdrawal"`
}

// GridConfig 网格策略配置
type GridConfig struct {
	PriceInterval  float64 `yaml:"price_interval" json:"price_interval"`
	ProfitSpread   float64 `yaml:"profit_spread,omitempty" json:"profit_spread,omitempty"`
	OrderQuantity  float64 `yaml:"order_quantity" json:"order_quantity"`
	MinOrderValue  float64 `yaml:"min_order_value" json:"min_order_value"`
	BuyWindowSize  int     `yaml:"buy_window_size" json:"buy_window_size"`
	SellWindowSize int     `yaml:"sell_window_size" json:"sell_window_size"`

	Direction    string  `yaml:"direction,omitempty" json:"direction,omitempty"`    // LONG/SHORT/BOTH
	PriceLow     float64 `yaml:"price_low,omitempty" json:"price_low,omitempty"`
	PriceHigh    float64 `yaml:"price_high,omitempty" json:"price_high,omitempty"`
	TriggerPrice float64 `yaml:"trigger_price,omitempty" json:"trigger_price,omitempty"`

	GridMode         string  `yaml:"grid_mode,omitempty" json:"grid_mode,omitempty"`           // arithmetic/geometric
	GridShiftEnabled bool    `yaml:"grid_shift_enabled,omitempty" json:"grid_shift_enabled,omitempty"`
	GridShiftStep    float64 `yaml:"grid_shift_step,omitempty" json:"grid_shift_step,omitempty"`

	// 三级火箭模式：小波动小网格、大波动大网格，根据持仓层数自动切换间距
	RocketTieredGrid *RocketTieredGridConfig `yaml:"rocket_tiered_grid,omitempty" json:"rocket_tiered_grid,omitempty"`

	// 网格自动重建配置（价格偏离时自动调整网格锚点）
	AutoRebuild GridAutoRebuildConfig `yaml:"auto_rebuild,omitempty" json:"auto_rebuild,omitempty"`
}

// GridAutoRebuildConfig 网格自动重建配置
// 当价格长期偏离网格区间时，自动撤销旧订单并按新价格重建网格
type GridAutoRebuildConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"` // 是否启用自动重建

	// 检查间隔
	CheckIntervalMinutes int `yaml:"check_interval_minutes" json:"check_interval_minutes"` // 检查间隔（分钟），默认 5

	// 触发条件（满足任一即可）
	PriceDeviationLayers int     `yaml:"price_deviation_layers" json:"price_deviation_layers"` // 价格偏离网格中心层数，默认 5
	OrderExpireMinutes   int     `yaml:"order_expire_minutes" json:"order_expire_minutes"`     // 订单过期时间（分钟），默认 30
	ExpiredOrderRatio    float64 `yaml:"expired_order_ratio" json:"expired_order_ratio"`        // 过期订单比例，默认 0.7

	// 重建模式
	RebuildMode string `yaml:"rebuild_mode" json:"rebuild_mode"` // smart/always，默认 smart

	// 保护机制（防止频繁重建）
	MaxRebuildsPerHour  int `yaml:"max_rebuilds_per_hour" json:"max_rebuilds_per_hour"` // 每小时最大重建次数，默认 2
	MinRebuildInterval  int `yaml:"min_rebuild_interval" json:"min_rebuild_interval"` // 两次重建最小间隔（分钟），默认 15
	RequireTrendConfirm bool `yaml:"require_trend_confirm" json:"require_trend_confirm"` // 是否需要趋势确认，默认 false
}

// GetDefaultAutoRebuildConfig 返回保守的默认自动重建配置
func GetDefaultAutoRebuildConfig() GridAutoRebuildConfig {
	return GridAutoRebuildConfig{
		Enabled:             false, // 默认关闭，需要用户主动开启
		CheckIntervalMinutes: 5,
		PriceDeviationLayers: 5,    // 偏离 5 层网格才触发（足够宽松）
		OrderExpireMinutes:   30,   // 订单挂单 30 分钟未成交算过期
		ExpiredOrderRatio:    0.7,  // 70% 订单过期才触发
		RebuildMode:          "smart",
		MaxRebuildsPerHour:   2,    // 每小时最多重建 2 次
		MinRebuildInterval:   15,   // 两次重建最少间隔 15 分钟
		RequireTrendConfirm:  false, // 默认不需要趋势确认（保守策略）
	}
}

// RiskControlConfig 风控配置
// OptionHedgeConfig 外部期权对冲配置（Put 保护 + 单向做多网格）
type OptionHedgeConfig struct {
	Enabled             bool    `yaml:"enabled" json:"enabled"`
	Exchange            string  `yaml:"exchange" json:"exchange"`                           // binance / deribit
	TargetCoverageRatio float64 `yaml:"target_coverage_ratio" json:"target_coverage_ratio"` // 目标覆盖率 0-1，默认 0.25
	MinCoverageRatio    float64 `yaml:"min_coverage_ratio" json:"min_coverage_ratio"`       // 最小覆盖率，低于则联动风控
	DTEWarningDays      int     `yaml:"dte_warning_days" json:"dte_warning_days"`           // DTE 低于此值告警
	RebalanceInterval   int     `yaml:"rebalance_interval" json:"rebalance_interval"`       // 同步间隔（秒）
}

type RiskControlConfig struct {
	GridRiskControl     GridRiskControl     `yaml:"grid_risk_control,omitempty" json:"grid_risk_control,omitempty"`
	OpenPositionControl OpenPositionControl `yaml:"open_position_control,omitempty" json:"open_position_control,omitempty"`
	OptionHedge         *OptionHedgeConfig  `yaml:"option_hedge,omitempty" json:"option_hedge,omitempty"`
	MaxDrawdownRatio    float64             `yaml:"max_drawdown_ratio,omitempty" json:"max_drawdown_ratio,omitempty"`
	StopLossRatio       float64             `yaml:"stop_loss_ratio,omitempty" json:"stop_loss_ratio,omitempty"`
	TakeProfitRatio     float64             `yaml:"take_profit_ratio,omitempty" json:"take_profit_ratio,omitempty"`
}

// AdvancedConfig 高级配置
type AdvancedConfig struct {
	ReconcileInterval     int                `yaml:"reconcile_interval,omitempty" json:"reconcile_interval,omitempty"`
	OrderCleanupThreshold int                `yaml:"order_cleanup_threshold,omitempty" json:"order_cleanup_threshold,omitempty"`
	CleanupBatchSize      int                `yaml:"cleanup_batch_size,omitempty" json:"cleanup_batch_size,omitempty"`
	MarginLockDurationSec int                `yaml:"margin_lock_duration_sec,omitempty" json:"margin_lock_duration_sec,omitempty"`
	PositionSafetyCheck   int                `yaml:"position_safety_check,omitempty" json:"position_safety_check,omitempty"`
	CloseOnStop           bool               `yaml:"close_on_stop,omitempty" json:"close_on_stop,omitempty"`
	CloseOnStopConfig     ClosePositionConfig `yaml:"close_on_stop_config,omitempty" json:"close_on_stop_config,omitempty"`
	SlotFilter            SlotFilterConfig   `yaml:"slot_filter,omitempty" json:"slot_filter,omitempty"`
	SmartOrder            SmartOrderConfig   `yaml:"smart_order,omitempty" json:"smart_order,omitempty"`

	// 配置档案
	Profiles    map[string]ProfileConfig `yaml:"profiles,omitempty" json:"profiles,omitempty"`
	SwitchRules SwitchRules              `yaml:"switch_rules,omitempty" json:"switch_rules,omitempty"`
}

// HedgeBotConfig 对冲 Bot 配置（仅在对冲组中的 bot 使用）
type HedgeBotConfig struct {
	GroupID      string  `yaml:"group_id" json:"group_id"`           // 所属对冲组 ID
	GroupName    string  `yaml:"group_name" json:"group_name"`       // 对冲组名称
	Role         string  `yaml:"role" json:"role"`                   // futures/spot
	HedgeRatio   float64 `yaml:"hedge_ratio" json:"hedge_ratio"`     // 对冲比例
	Rebalance    bool    `yaml:"rebalance" json:"rebalance"`         // 是否自动再平衡
	SyncPosition bool    `yaml:"sync_position" json:"sync_position"` // 是否同步仓位
}

// BotConfigManager Bot 配置文件管理器
type BotConfigManager struct {
	botsDir string
	mu      sync.RWMutex
}

// NewBotConfigManager 创建 Bot 配置管理器
func NewBotConfigManager(baseDir string) (*BotConfigManager, error) {
	botsDir := filepath.Join(baseDir, "bots")
	if err := os.MkdirAll(botsDir, 0755); err != nil {
		return nil, fmt.Errorf("创建 bots 目录失败: %w", err)
	}
	return &BotConfigManager{
		botsDir: botsDir,
	}, nil
}

// GetBotConfigPath 获取 Bot 配置文件路径
func (m *BotConfigManager) GetBotConfigPath(botID string) string {
	return filepath.Join(m.botsDir, botID, "config.yaml")
}

// GetBotDataPath 获取 Bot 数据目录路径
func (m *BotConfigManager) GetBotDataPath(botID string) string {
	return filepath.Join(m.botsDir, botID)
}

// LoadBotConfig 加载 Bot 配置文件
func (m *BotConfigManager) LoadBotConfig(botID string) (*BotConfigFile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	configPath := m.GetBotConfigPath(botID)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取 Bot 配置文件失败: %w", err)
	}

	var config BotConfigFile
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析 Bot 配置文件失败: %w", err)
	}

	return &config, nil
}

// SaveBotConfig 保存 Bot 配置文件
func (m *BotConfigManager) SaveBotConfig(config *BotConfigFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	botDir := m.GetBotDataPath(config.BotID)
	if err := os.MkdirAll(botDir, 0755); err != nil {
		return fmt.Errorf("创建 Bot 目录失败: %w", err)
	}

	// 清理策略配置中的数值类型，避免 YAML 序列化为字符串
	cleanedConfig := m.cleanConfigValues(config)

	configPath := m.GetBotConfigPath(config.BotID)
	data, err := yaml.Marshal(cleanedConfig)
	if err != nil {
		return fmt.Errorf("序列化 Bot 配置失败: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("写入 Bot 配置文件失败: %w", err)
	}

	return nil
}

// cleanConfigValues 清理配置中的数值类型，确保数值不会被序列化为字符串
func (m *BotConfigManager) cleanConfigValues(config *BotConfigFile) *BotConfigFile {
	cleaned := *config

	// 清理每个策略的 Params 和 Settings
	for i := range cleaned.Strategies {
		cleaned.Strategies[i].Params = cleanMapValues(cleaned.Strategies[i].Params)
		cleaned.Strategies[i].Settings = cleanMapValues(cleaned.Strategies[i].Settings)
	}

	return &cleaned
}

// cleanMapValues 清理 map[string]interface{} 中的数值类型
func cleanMapValues(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}

	cleaned := make(map[string]interface{})
	for k, v := range m {
		cleaned[k] = cleanValue(v)
	}

	return cleaned
}

// cleanValue 清理单个值的类型
func cleanValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}

	// 处理字符串类型，尝试转换为数值
	switch val := v.(type) {
	case string:
		// 尝试解析为浮点数
		var f float64
		if _, err := fmt.Sscanf(val, "%f", &f); err == nil {
			// 成功解析为数字，返回浮点数
			return f
		}
		// 尝试解析为整数
		var i int64
		if _, err := fmt.Sscanf(val, "%d", &i); err == nil {
			return i
		}
		// 尝试解析为布尔值
		if val == "true" {
			return true
		}
		if val == "false" {
			return false
		}
		// 保持字符串
		return val
	case map[string]interface{}:
		// 递归处理嵌套 map
		return cleanMapValues(val)
	case []interface{}:
		// 处理数组
		arr := make([]interface{}, len(val))
		for i, item := range val {
			arr[i] = cleanValue(item)
		}
		return arr
	default:
		// 其他类型保持不变
		return v
	}
}

// CreateBotConfig 创建新的 Bot 配置文件
func (m *BotConfigManager) CreateBotConfig(config *BotConfigFile) error {
	// 检查是否已存在
	configPath := m.GetBotConfigPath(config.BotID)
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("Bot 配置文件已存在: %s", config.BotID)
	}

	return m.SaveBotConfig(config)
}

// DeleteBotConfig 删除 Bot 配置文件
func (m *BotConfigManager) DeleteBotConfig(botID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	botDir := m.GetBotDataPath(botID)
	if err := os.RemoveAll(botDir); err != nil {
		return fmt.Errorf("删除 Bot 配置目录失败: %w", err)
	}

	return nil
}

// ListBotConfigs 列出所有 Bot 配置
func (m *BotConfigManager) ListBotConfigs() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, err := os.ReadDir(m.botsDir)
	if err != nil {
		return nil, fmt.Errorf("读取 bots 目录失败: %w", err)
	}

	var botIDs []string
	for _, entry := range entries {
		if entry.IsDir() {
			botIDs = append(botIDs, entry.Name())
		}
	}

	return botIDs, nil
}

// BotConfigExists 检查 Bot 配置是否存在
func (m *BotConfigManager) BotConfigExists(botID string) bool {
	configPath := m.GetBotConfigPath(botID)
	_, err := os.Stat(configPath)
	return err == nil
}

// ConvertFromBotConfig 从旧的 BotConfig 转换到新的 BotConfigFile
func ConvertFromBotConfig(bc BotConfig) *BotConfigFile {
	config := &BotConfigFile{
		BotID:       bc.ID,
		Name:        bc.Name,
		Exchange:    bc.Exchange,
		Symbol:      bc.Symbol,
		MarketType:  bc.MarketType,
		Testnet:     bc.Testnet,
		StrategyMode: "single",
		Capital: CapitalConfig{
			TotalAllocated: bc.TotalAllocatedCapital,
			Withdrawal:     bc.WithdrawalPolicy,
		},
		Grid: GridConfig{
			PriceInterval:     bc.PriceInterval,
			ProfitSpread:      bc.ProfitSpread,
			OrderQuantity:     bc.OrderQuantity,
			MinOrderValue:     bc.MinOrderValue,
			BuyWindowSize:     bc.BuyWindowSize,
			SellWindowSize:    bc.SellWindowSize,
			Direction:         bc.Direction,
			PriceLow:          bc.PriceLow,
			PriceHigh:         bc.PriceHigh,
			TriggerPrice:      bc.TriggerPrice,
			GridMode:          bc.GridMode,
			GridShiftEnabled:  bc.GridShiftEnabled,
			GridShiftStep:     bc.GridShiftStep,
			RocketTieredGrid:  bc.RocketTieredGrid,
		},
		RiskControl: RiskControlConfig{
			GridRiskControl:     bc.GridRiskControl,
			OpenPositionControl: bc.OpenPositionControl,
		},
		Advanced: AdvancedConfig{
			ReconcileInterval:     bc.ReconcileInterval,
			OrderCleanupThreshold: bc.OrderCleanupThreshold,
			CleanupBatchSize:      bc.CleanupBatchSize,
			MarginLockDurationSec: bc.MarginLockDurationSec,
			PositionSafetyCheck:   bc.PositionSafetyCheck,
			CloseOnStop:           bc.CloseOnStop,
			CloseOnStopConfig:     bc.CloseOnStopConfig,
			SlotFilter:            bc.SlotFilter,
			SmartOrder:            bc.SmartOrder,
			Profiles:              bc.Profiles,
			SwitchRules:           bc.SwitchRules,
		},
	}

	// 转换策略配置
	if len(bc.Strategies) > 0 {
		if len(bc.Strategies) > 1 {
			config.StrategyMode = "multi"
		}

		config.Strategies = make([]BotStrategyConfig, len(bc.Strategies))
		for i, s := range bc.Strategies {
			config.Strategies[i] = BotStrategyConfig{
				Type:     s.Type,
				Enabled:  true,
				Weight:   s.Weight,
				Params:   s.Config,
				Settings: make(map[string]interface{}),
			}
		}
	}

	return config
}

// ConvertToBotConfig 从新的 BotConfigFile 转换到旧的 BotConfig
func ConvertToBotConfig(bcf *BotConfigFile) BotConfig {
	bc := BotConfig{
		ID:                    bcf.BotID,
		Name:                  bcf.Name,
		Exchange:              bcf.Exchange,
		Symbol:                bcf.Symbol,
		MarketType:            bcf.MarketType,
		Testnet:               bcf.Testnet,
		TotalAllocatedCapital: bcf.Capital.TotalAllocated,
		WithdrawalPolicy:      bcf.Capital.Withdrawal,
		PriceInterval:         bcf.Grid.PriceInterval,
		ProfitSpread:          bcf.Grid.ProfitSpread,
		OrderQuantity:         bcf.Grid.OrderQuantity,
		MinOrderValue:         bcf.Grid.MinOrderValue,
		BuyWindowSize:         bcf.Grid.BuyWindowSize,
		SellWindowSize:        bcf.Grid.SellWindowSize,
		Direction:             bcf.Grid.Direction,
		PriceLow:              bcf.Grid.PriceLow,
		PriceHigh:             bcf.Grid.PriceHigh,
		TriggerPrice:          bcf.Grid.TriggerPrice,
		GridMode:              bcf.Grid.GridMode,
		GridShiftEnabled:      bcf.Grid.GridShiftEnabled,
		GridShiftStep:         bcf.Grid.GridShiftStep,
		RocketTieredGrid:      bcf.Grid.RocketTieredGrid,
		GridRiskControl:       bcf.RiskControl.GridRiskControl,
		OpenPositionControl:   bcf.RiskControl.OpenPositionControl,
		ReconcileInterval:     bcf.Advanced.ReconcileInterval,
		OrderCleanupThreshold: bcf.Advanced.OrderCleanupThreshold,
		CleanupBatchSize:      bcf.Advanced.CleanupBatchSize,
		MarginLockDurationSec: bcf.Advanced.MarginLockDurationSec,
		PositionSafetyCheck:   bcf.Advanced.PositionSafetyCheck,
		CloseOnStop:           bcf.Advanced.CloseOnStop,
		CloseOnStopConfig:     bcf.Advanced.CloseOnStopConfig,
		SlotFilter:            bcf.Advanced.SlotFilter,
		SmartOrder:            bcf.Advanced.SmartOrder,
		Profiles:              bcf.Advanced.Profiles,
		SwitchRules:           bcf.Advanced.SwitchRules,
	}

	// 转换策略配置
	if len(bcf.Strategies) > 0 {
		bc.Strategies = make([]StrategyInstance, len(bcf.Strategies))
		for i, s := range bcf.Strategies {
			bc.Strategies[i] = StrategyInstance{
				Type:   s.Type,
				Weight: s.Weight,
				Config: s.Params,
			}
		}
	}

	return bc
}

// ExportToJSON 导出为 JSON（用于 API 响应）
func (bcf *BotConfigFile) ExportToJSON() ([]byte, error) {
	return json.MarshalIndent(bcf, "", "  ")
}

// ImportFromJSON 从 JSON 导入
func ImportFromJSON(data []byte) (*BotConfigFile, error) {
	var config BotConfigFile
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return &config, nil
}
