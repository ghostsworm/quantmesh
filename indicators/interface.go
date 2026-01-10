// Package indicators 技术指标库
// 提供 50+ 常用技术指标，支持策略开发和回测
package indicators

// Candle K线数据
type Candle struct {
	Time   int64   // 时间戳
	Open   float64 // 开盘价
	High   float64 // 最高价
	Low    float64 // 最低价
	Close  float64 // 收盘价
	Volume float64 // 成交量
}

// Indicator 指标接口
type Indicator interface {
	// Name 指标名称
	Name() string
	// Calculate 计算指标值
	Calculate(candles []Candle) []float64
	// Period 计算所需的最小周期数
	Period() int
}

// MultiValueIndicator 多值指标接口（如 MACD、布林带等）
type MultiValueIndicator interface {
	Indicator
	// CalculateMulti 计算多个值
	CalculateMulti(candles []Candle) map[string][]float64
}

// SignalIndicator 信号指标接口
type SignalIndicator interface {
	Indicator
	// Signal 返回交易信号：1=买入，-1=卖出，0=观望
	Signal(candles []Candle) int
}

// IndicatorResult 指标计算结果
type IndicatorResult struct {
	Name   string             // 指标名称
	Values map[string]float64 // 当前值（支持多值指标）
	Signal int                // 信号：1=买入，-1=卖出，0=观望
}

// IndicatorConfig 指标配置
type IndicatorConfig struct {
	Name       string                 `json:"name" yaml:"name"`
	Enabled    bool                   `json:"enabled" yaml:"enabled"`
	Parameters map[string]interface{} `json:"parameters" yaml:"parameters"`
	Weight     float64                `json:"weight" yaml:"weight"` // 权重（用于组合信号）
}

// IndicatorRegistry 指标注册表
type IndicatorRegistry struct {
	indicators map[string]func(params map[string]interface{}) Indicator
}

// NewIndicatorRegistry 创建指标注册表
func NewIndicatorRegistry() *IndicatorRegistry {
	return &IndicatorRegistry{
		indicators: make(map[string]func(params map[string]interface{}) Indicator),
	}
}

// Register 注册指标
func (r *IndicatorRegistry) Register(name string, factory func(params map[string]interface{}) Indicator) {
	r.indicators[name] = factory
}

// Get 获取指标
func (r *IndicatorRegistry) Get(name string, params map[string]interface{}) Indicator {
	if factory, ok := r.indicators[name]; ok {
		return factory(params)
	}
	return nil
}

// List 列出所有注册的指标
func (r *IndicatorRegistry) List() []string {
	names := make([]string, 0, len(r.indicators))
	for name := range r.indicators {
		names = append(names, name)
	}
	return names
}

// DefaultRegistry 默认指标注册表
var DefaultRegistry = NewIndicatorRegistry()

// RegisterIndicator 注册指标到默认注册表
func RegisterIndicator(name string, factory func(params map[string]interface{}) Indicator) {
	DefaultRegistry.Register(name, factory)
}

// GetIndicator 从默认注册表获取指标
func GetIndicator(name string, params map[string]interface{}) Indicator {
	return DefaultRegistry.Get(name, params)
}

// ListIndicators 列出默认注册表中的所有指标
func ListIndicators() []string {
	return DefaultRegistry.List()
}
