// Package indicators 技术指標库
// 提供 50+ 常用技术指標，支援策略开发和回测
package indicators

// Candle K線數據
type Candle struct {
	Time   int64   // 時间戳
	Open   float64 // 开盘價
	High   float64 // 最高價
	Low    float64 // 最低價
	Close  float64 // 收盘價
	Volume float64 // 成交量
}

// Indicator 指標接口
type Indicator interface {
	// Name 指標名称
	Name() string
	// Calculate 计算指標值
	Calculate(candles []Candle) []float64
	// Period 计算所需的最小周期數
	Period() int
}

// MultiValueIndicator 多值指標介面（如 MACD、布林带等）
type MultiValueIndicator interface {
	Indicator
	// CalculateMulti 计算多個值
	CalculateMulti(candles []Candle) map[string][]float64
}

// SignalIndicator 信号指標接口
type SignalIndicator interface {
	Indicator
	// Signal 返回交易信号：1=買入，-1=賣出，0=观望
	Signal(candles []Candle) int
}

// IndicatorResult 指標计算結果
type IndicatorResult struct {
	Name   string             // 指標名称
	Values map[string]float64 // 當前值（支援多值指標）
	Signal int                // 信号：1=買入，-1=賣出，0=观望
}

// IndicatorConfig 指標配置
type IndicatorConfig struct {
	Name       string                 `json:"name" yaml:"name"`
	Enabled    bool                   `json:"enabled" yaml:"enabled"`
	Parameters map[string]interface{} `json:"parameters" yaml:"parameters"`
	Weight     float64                `json:"weight" yaml:"weight"` // 权重（用於组合信号）
}

// IndicatorRegistry 指標注册表
type IndicatorRegistry struct {
	indicators map[string]func(params map[string]interface{}) Indicator
}

// NewIndicatorRegistry 創建指標注册表
func NewIndicatorRegistry() *IndicatorRegistry {
	return &IndicatorRegistry{
		indicators: make(map[string]func(params map[string]interface{}) Indicator),
	}
}

// Register 注册指標
func (r *IndicatorRegistry) Register(name string, factory func(params map[string]interface{}) Indicator) {
	r.indicators[name] = factory
}

// Get 獲取指標
func (r *IndicatorRegistry) Get(name string, params map[string]interface{}) Indicator {
	if factory, ok := r.indicators[name]; ok {
		return factory(params)
	}
	return nil
}

// List 列出所有注册的指標
func (r *IndicatorRegistry) List() []string {
	names := make([]string, 0, len(r.indicators))
	for name := range r.indicators {
		names = append(names, name)
	}
	return names
}

// DefaultRegistry 默认指標注册表
var DefaultRegistry = NewIndicatorRegistry()

// RegisterIndicator 注册指標到默认注册表
func RegisterIndicator(name string, factory func(params map[string]interface{}) Indicator) {
	DefaultRegistry.Register(name, factory)
}

// GetIndicator 從默认注册表獲取指標
func GetIndicator(name string, params map[string]interface{}) Indicator {
	return DefaultRegistry.Get(name, params)
}

// ListIndicators 列出默认注册表中的所有指標
func ListIndicators() []string {
	return DefaultRegistry.List()
}
