//go:build tools

package examples

import (
	"context"
	"fmt"

	"quantmesh/config"
	"quantmesh/plugin"
	"quantmesh/position"
	"quantmesh/strategy"
)

// ExampleStrategyPlugin 示例策略插件
// 这是一個开源的示例，展示如何創建策略插件
type ExampleStrategyPlugin struct {
	metadata *plugin.PluginMetadata
	strategy strategy.Strategy
}

// NewExampleStrategyPlugin 創建示例策略插件
func NewExampleStrategyPlugin() *ExampleStrategyPlugin {
	return &ExampleStrategyPlugin{
		metadata: &plugin.PluginMetadata{
			Name:        "example_strategy",
			Version:     "1.0.0",
			Author:      "QuantMesh Team",
			Description: "示例策略插件 - 展示如何創建自定义策略",
			Type:        plugin.PluginTypeStrategy,
			License:     "free",
			RequiresKey: false, // 免费插件不需要許可证
		},
	}
}

// GetMetadata 獲取插件元數據
func (p *ExampleStrategyPlugin) GetMetadata() *plugin.PluginMetadata {
	return p.metadata
}

// Initialize 初始化插件
func (p *ExampleStrategyPlugin) Initialize(cfg *config.Config, params map[string]interface{}) error {
	// 創建策略實例
	p.strategy = &ExampleStrategy{
		name: p.metadata.Name,
	}
	return nil
}

// Validate 驗证許可证 (免费插件直接返回成功)
func (p *ExampleStrategyPlugin) Validate(licenseKey string) error {
	return nil
}

// GetStrategy 獲取策略實例
func (p *ExampleStrategyPlugin) GetStrategy() strategy.Strategy {
	return p.strategy
}

// Close 关闭插件
func (p *ExampleStrategyPlugin) Close() error {
	if p.strategy != nil {
		return p.strategy.Stop()
	}
	return nil
}

// ExampleStrategy 示例策略實現
type ExampleStrategy struct {
	name     string
	cfg      *config.Config
	executor position.OrderExecutorInterface
	exchange position.IExchange
	bus      strategy.EventBus
}

func (s *ExampleStrategy) Name() string {
	return s.name
}

func (s *ExampleStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	s.cfg = cfg
	s.executor = executor
	s.exchange = exchange
	return nil
}

func (s *ExampleStrategy) OnPriceChange(price float64) error {
	// 實現你的策略逻辑
	return nil
}

func (s *ExampleStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// 处理订單更新
	return nil
}

func (s *ExampleStrategy) GetPositions() []*strategy.Position {
	return nil
}

func (s *ExampleStrategy) GetOrders() []*strategy.Order {
	return nil
}

func (s *ExampleStrategy) GetStatistics() *strategy.StrategyStatistics {
	return &strategy.StrategyStatistics{}
}

func (s *ExampleStrategy) Start(ctx context.Context) error {
	return nil
}

func (s *ExampleStrategy) Stop() error {
	return nil
}

func (s *ExampleStrategy) SetEventBus(bus strategy.EventBus) {
	s.bus = bus
}

// ===== 闭源插件示例 (僅展示結構，實際代碼不公开) =====

// PremiumAIStrategyPlugin 高级AI策略插件 (闭源)
// 這個插件需要商业許可证才能使用
type PremiumAIStrategyPlugin struct {
	metadata  *plugin.PluginMetadata
	strategy  strategy.Strategy
	validator *plugin.LicenseValidator
}

// NewPremiumAIStrategyPlugin 創建高级AI策略插件
func NewPremiumAIStrategyPlugin() *PremiumAIStrategyPlugin {
	return &PremiumAIStrategyPlugin{
		metadata: &plugin.PluginMetadata{
			Name:        "premium_ai_strategy",
			Version:     "2.0.0",
			Author:      "QuantMesh Pro Team",
			Description: "高级AI驅动策略 - 使用机器学习优化交易决策",
			Type:        plugin.PluginTypeStrategy,
			License:     "commercial",
			RequiresKey: true, // 需要商业許可证
		},
		validator: plugin.NewLicenseValidator(),
	}
}

func (p *PremiumAIStrategyPlugin) GetMetadata() *plugin.PluginMetadata {
	return p.metadata
}

func (p *PremiumAIStrategyPlugin) Initialize(cfg *config.Config, params map[string]interface{}) error {
	// 这里是闭源的初始化逻辑
	// 實際代碼不會在开源倉库中
	return fmt.Errorf("此插件需要商业許可证，请联系 commercial@quantmesh.com")
}

func (p *PremiumAIStrategyPlugin) Validate(licenseKey string) error {
	// 驗证商业許可证
	return p.validator.ValidatePlugin(p.metadata.Name, licenseKey)
}

func (p *PremiumAIStrategyPlugin) GetStrategy() strategy.Strategy {
	return p.strategy
}

func (p *PremiumAIStrategyPlugin) Close() error {
	if p.strategy != nil {
		return p.strategy.Stop()
	}
	return nil
}
