package plugin

import (
	"context"
	"fmt"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/strategy"
)

// PluginType 插件类型
type PluginType string

const (
	PluginTypeStrategy PluginType = "strategy" // 策略插件
	PluginTypeAI       PluginType = "ai"       // AI插件
	PluginTypeRisk     PluginType = "risk"     // 风控插件
	PluginTypeSignal   PluginType = "signal"   // 信号源插件
)

// PluginMetadata 插件元数据
type PluginMetadata struct {
	Name        string     // 插件名称
	Version     string     // 版本号
	Author      string     // 作者
	Description string     // 描述
	Type        PluginType // 插件类型
	License     string     // 许可证类型 (free/commercial)
	RequiresKey bool       // 是否需要激活密钥
}

// Plugin 插件接口
type Plugin interface {
	// GetMetadata 获取插件元数据
	GetMetadata() *PluginMetadata

	// Initialize 初始化插件
	Initialize(cfg *config.Config, params map[string]interface{}) error

	// Validate 验证插件许可证
	Validate(licenseKey string) error

	// Close 关闭插件
	Close() error
}

// StrategyPlugin 策略插件接口
type StrategyPlugin interface {
	Plugin
	// GetStrategy 获取策略实例
	GetStrategy() strategy.Strategy
}

// AIPlugin AI插件接口
type AIPlugin interface {
	Plugin
	// Analyze 分析市场
	Analyze(ctx context.Context, data interface{}) (interface{}, error)
}

// SignalPlugin 信号源插件接口
type SignalPlugin interface {
	Plugin
	// GetSignal 获取交易信号
	GetSignal(ctx context.Context) (*TradingSignal, error)
}

// TradingSignal 交易信号
type TradingSignal struct {
	Symbol    string  // 交易对
	Action    string  // BUY/SELL/HOLD
	Strength  float64 // 信号强度 0-1
	Price     float64 // 建议价格
	Reason    string  // 信号原因
	Timestamp int64   // 时间戳
}

// PluginRegistry 插件注册表
type PluginRegistry struct {
	plugins map[string]Plugin
	mu      sync.RWMutex
}

var (
	globalRegistry *PluginRegistry
	once           sync.Once
)

// GetRegistry 获取全局插件注册表
func GetRegistry() *PluginRegistry {
	once.Do(func() {
		globalRegistry = &PluginRegistry{
			plugins: make(map[string]Plugin),
		}
	})
	return globalRegistry
}

// Register 注册插件
func (r *PluginRegistry) Register(plugin Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	metadata := plugin.GetMetadata()
	if metadata == nil {
		return fmt.Errorf("插件元数据为空")
	}

	if _, exists := r.plugins[metadata.Name]; exists {
		return fmt.Errorf("插件 %s 已注册", metadata.Name)
	}

	r.plugins[metadata.Name] = plugin
	logger.Info("✅ 插件已注册: %s v%s (%s)", metadata.Name, metadata.Version, metadata.Type)
	return nil
}

// Get 获取插件
func (r *PluginRegistry) Get(name string) (Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	plugin, exists := r.plugins[name]
	if !exists {
		return nil, fmt.Errorf("插件 %s 未找到", name)
	}
	return plugin, nil
}

// GetByType 按类型获取插件
func (r *PluginRegistry) GetByType(pluginType PluginType) []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Plugin
	for _, plugin := range r.plugins {
		if plugin.GetMetadata().Type == pluginType {
			result = append(result, plugin)
		}
	}
	return result
}

// List 列出所有插件
func (r *PluginRegistry) List() []*PluginMetadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*PluginMetadata
	for _, plugin := range r.plugins {
		result = append(result, plugin.GetMetadata())
	}
	return result
}

// Unregister 注销插件
func (r *PluginRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	plugin, exists := r.plugins[name]
	if !exists {
		return fmt.Errorf("插件 %s 未找到", name)
	}

	if err := plugin.Close(); err != nil {
		logger.Warn("⚠️ 关闭插件 %s 时出错: %v", name, err)
	}

	delete(r.plugins, name)
	logger.Info("✅ 插件已注销: %s", name)
	return nil
}

// CloseAll 关闭所有插件
func (r *PluginRegistry) CloseAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for name, plugin := range r.plugins {
		if err := plugin.Close(); err != nil {
			logger.Warn("⚠️ 关闭插件 %s 时出错: %v", name, err)
		}
	}
	r.plugins = make(map[string]Plugin)
	logger.Info("✅ 所有插件已关闭")
}

// PluginLoader 插件加载器
type PluginLoader struct {
	registry     *PluginRegistry
	cfg          *config.Config
	licenseStore *LicenseStore
}

// NewPluginLoader 创建插件加载器
func NewPluginLoader(cfg *config.Config) *PluginLoader {
	return &PluginLoader{
		registry:     GetRegistry(),
		cfg:          cfg,
		licenseStore: NewLicenseStore(),
	}
}

// LoadPlugin 加载插件
func (l *PluginLoader) LoadPlugin(plugin Plugin, licenseKey string, params map[string]interface{}) error {
	metadata := plugin.GetMetadata()

	// 1. 验证许可证
	if metadata.RequiresKey {
		if err := plugin.Validate(licenseKey); err != nil {
			return fmt.Errorf("插件 %s 许可证验证失败: %v", metadata.Name, err)
		}
		logger.Info("✅ 插件 %s 许可证验证通过", metadata.Name)
	}

	// 2. 初始化插件
	if err := plugin.Initialize(l.cfg, params); err != nil {
		return fmt.Errorf("插件 %s 初始化失败: %v", metadata.Name, err)
	}

	// 3. 注册插件
	if err := l.registry.Register(plugin); err != nil {
		return fmt.Errorf("插件 %s 注册失败: %v", metadata.Name, err)
	}

	// 4. 保存许可证信息
	if metadata.RequiresKey {
		l.licenseStore.Store(metadata.Name, licenseKey)
	}

	logger.Info("✅ 插件 %s 加载成功", metadata.Name)
	return nil
}

// LoadStrategyPlugin 加载策略插件
func (l *PluginLoader) LoadStrategyPlugin(
	plugin StrategyPlugin,
	licenseKey string,
	params map[string]interface{},
	strategyManager *strategy.StrategyManager,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
) error {
	// 1. 加载插件
	if err := l.LoadPlugin(plugin, licenseKey, params); err != nil {
		return err
	}

	// 2. 获取策略实例
	strat := plugin.GetStrategy()
	if strat == nil {
		return fmt.Errorf("插件 %s 未提供策略实例", plugin.GetMetadata().Name)
	}

	// 3. 初始化策略
	if err := strat.Initialize(l.cfg, executor, exchange); err != nil {
		return fmt.Errorf("策略初始化失败: %v", err)
	}

	// 4. 注册到策略管理器
	weight := 1.0
	if w, ok := params["weight"].(float64); ok {
		weight = w
	}
	fixedPool := 0.0
	if fp, ok := params["fixed_pool"].(float64); ok {
		fixedPool = fp
	}

	strategyManager.RegisterStrategy(strat.Name(), strat, weight, fixedPool)
	logger.Info("✅ 策略插件 %s 已注册到策略管理器", strat.Name())

	return nil
}

// GetRegistry 获取注册表
func (l *PluginLoader) GetRegistry() *PluginRegistry {
	return l.registry
}

// GetLicenseStore 获取许可证存储
func (l *PluginLoader) GetLicenseStore() *LicenseStore {
	return l.licenseStore
}

