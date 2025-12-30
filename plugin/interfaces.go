package plugin

import "context"

// Plugin 插件基础接口
type Plugin interface {
	// Name 返回插件名称
	Name() string
	
	// Version 返回插件版本
	Version() string
	
	// Initialize 初始化插件
	Initialize(config map[string]interface{}) error
	
	// Close 关闭插件
	Close() error
}

// AIStrategyPlugin AI 策略插件接口
type AIStrategyPlugin interface {
	Plugin
	
	// AnalyzeMarket 分析市场
	AnalyzeMarket(ctx context.Context, symbol string, timeframe string) (map[string]interface{}, error)
	
	// OptimizeParameters 优化参数
	OptimizeParameters(ctx context.Context, currentParams map[string]interface{}) (map[string]interface{}, error)
	
	// AnalyzeRisk 分析风险
	AnalyzeRisk(ctx context.Context, position float64, marketData map[string]interface{}) (map[string]interface{}, error)
	
	// MakeDecision 做出交易决策
	MakeDecision(ctx context.Context, marketCondition map[string]interface{}) (string, error)
}

// StrategyPlugin 策略插件接口
type StrategyPlugin interface {
	Plugin
	
	// GetStrategy 获取指定策略
	GetStrategy(name string) (interface{}, error)
	
	// ListStrategies 列出所有策略
	ListStrategies() []string
	
	// ExecuteStrategy 执行指定策略
	ExecuteStrategy(ctx context.Context, strategyName string, params map[string]interface{}) (map[string]interface{}, error)
}

// RiskPlugin 风控插件接口
type RiskPlugin interface {
	Plugin
	
	// PredictRisk 预测风险
	PredictRisk(ctx context.Context, marketData map[string]interface{}) (float64, error)
	
	// OptimizePortfolio 优化投资组合
	OptimizePortfolio(ctx context.Context, positions map[string]float64) (map[string]float64, error)
}

