package backtest

import (
	"context"
	"fmt"
	"plugin"
	"sync"

	"quantmesh/exchange"
	"quantmesh/logger"
)

// PluginStrategyAdapter 插件策略适配器（通用）
type PluginStrategyAdapter struct {
	pluginPath   string
	strategyName string
	plugin       *plugin.Plugin
	pluginObj    interface{}
	mu           sync.RWMutex
}

// NewPluginStrategyAdapter 创建插件策略适配器
func NewPluginStrategyAdapter(pluginPath string, strategyName string, config map[string]interface{}) (*PluginStrategyAdapter, error) {
	// 加载插件
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("加载插件失败: %w", err)
	}

	// 获取 NewPlugin 函数
	newPluginSymbol, err := p.Lookup("NewPlugin")
	if err != nil {
		return nil, fmt.Errorf("插件缺少 NewPlugin 函数: %w", err)
	}

	newPluginFunc, ok := newPluginSymbol.(func() interface{})
	if !ok {
		return nil, fmt.Errorf("NewPlugin 函数签名错误")
	}

	// 创建插件实例
	pluginObj := newPluginFunc()

	// 初始化插件
	if initializer, ok := pluginObj.(interface {
		Initialize(config map[string]interface{}) error
	}); ok {
		if err := initializer.Initialize(config); err != nil {
			return nil, fmt.Errorf("初始化插件失败: %w", err)
		}
	}

	adapter := &PluginStrategyAdapter{
		pluginPath:   pluginPath,
		strategyName: strategyName,
		plugin:       p,
		pluginObj:    pluginObj,
	}

	logger.Info("✅ 加载插件策略: %s (%s)", strategyName, pluginPath)

	return adapter, nil
}

// OnCandle 处理 K 线数据
func (a *PluginStrategyAdapter) OnCandle(candle *exchange.Candle) Signal {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 调用插件的 ExecuteStrategy 方法
	if executor, ok := a.pluginObj.(interface {
		ExecuteStrategy(ctx context.Context, strategyName string, params map[string]interface{}) (map[string]interface{}, error)
	}); ok {
		params := map[string]interface{}{
			"price": candle.Close,
		}

		result, err := executor.ExecuteStrategy(context.Background(), a.strategyName, params)
		if err != nil {
			logger.Warn("⚠️ 执行策略失败: %v", err)
			return Signal{Action: "hold", Price: candle.Close, Reason: "策略执行失败"}
		}

		// 解析结果
		signal := "hold"
		if s, ok := result["signal"].(string); ok {
			signal = s
		}

		reason := ""
		if r, ok := result["reason"].(string); ok {
			reason = r
		}

		return Signal{
			Action: signal,
			Price:  candle.Close,
			Reason: reason,
		}
	}

	return Signal{Action: "hold", Price: candle.Close, Reason: "插件不支持 ExecuteStrategy"}
}

// GetName 获取策略名称
func (a *PluginStrategyAdapter) GetName() string {
	return a.strategyName
}

// MomentumAdapter 动量策略适配器（直接实现，不依赖插件）
type MomentumAdapter struct {
	name         string
	priceHistory []float64
	rsiPeriod    int
	overbought   float64
	oversold     float64
	hasPosition  bool
	entryPrice   float64
}

// NewMomentumAdapter 创建动量策略适配器
func NewMomentumAdapter() *MomentumAdapter {
	return &MomentumAdapter{
		name:         "momentum",
		priceHistory: make([]float64, 0, 100),
		rsiPeriod:    14,
		overbought:   70,
		oversold:     30,
	}
}

// OnCandle 处理 K 线数据
func (a *MomentumAdapter) OnCandle(candle *exchange.Candle) Signal {
	a.priceHistory = append(a.priceHistory, candle.Close)

	// 保持历史记录
	maxHistory := a.rsiPeriod * 3
	if len(a.priceHistory) > maxHistory {
		a.priceHistory = a.priceHistory[len(a.priceHistory)-maxHistory:]
	}

	// 计算 RSI
	rsi := a.calculateRSI()
	if rsi == 50 {
		return Signal{Action: "hold", Price: candle.Close, Reason: "数据不足"}
	}

	// RSI < 30：超卖，买入信号
	if rsi < a.oversold && !a.hasPosition {
		a.hasPosition = true
		a.entryPrice = candle.Close
		return Signal{
			Action: "buy",
			Price:  candle.Close,
			Reason: fmt.Sprintf("RSI 超卖信号 (RSI=%.2f)", rsi),
		}
	}

	// RSI > 70：超买，卖出信号
	if rsi > a.overbought && a.hasPosition {
		a.hasPosition = false
		a.entryPrice = 0
		return Signal{
			Action: "sell",
			Price:  candle.Close,
			Reason: fmt.Sprintf("RSI 超买信号 (RSI=%.2f)", rsi),
		}
	}

	return Signal{Action: "hold", Price: candle.Close, Reason: "等待信号"}
}

// calculateRSI 计算 RSI（简化版）
func (a *MomentumAdapter) calculateRSI() float64 {
	if len(a.priceHistory) < a.rsiPeriod+1 {
		return 50
	}

	gains := 0.0
	losses := 0.0

	for i := len(a.priceHistory) - a.rsiPeriod; i < len(a.priceHistory); i++ {
		change := a.priceHistory[i] - a.priceHistory[i-1]
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(a.rsiPeriod)
	avgLoss := losses / float64(a.rsiPeriod)

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// GetName 获取策略名称
func (a *MomentumAdapter) GetName() string {
	return a.name
}

// MeanReversionAdapter 均值回归策略适配器
type MeanReversionAdapter struct {
	name         string
	priceHistory []float64
	period       int
	threshold    float64
	hasPosition  bool
	entryPrice   float64
}

// NewMeanReversionAdapter 创建均值回归策略适配器
func NewMeanReversionAdapter() *MeanReversionAdapter {
	return &MeanReversionAdapter{
		name:         "mean_reversion",
		priceHistory: make([]float64, 0, 100),
		period:       20,
		threshold:    2.0, // 2 倍标准差
	}
}

// OnCandle 处理 K 线数据
func (a *MeanReversionAdapter) OnCandle(candle *exchange.Candle) Signal {
	a.priceHistory = append(a.priceHistory, candle.Close)

	// 保持历史记录
	maxHistory := a.period * 3
	if len(a.priceHistory) > maxHistory {
		a.priceHistory = a.priceHistory[len(a.priceHistory)-maxHistory:]
	}

	if len(a.priceHistory) < a.period {
		return Signal{Action: "hold", Price: candle.Close, Reason: "数据不足"}
	}

	// 计算均值和标准差
	mean := 0.0
	for i := len(a.priceHistory) - a.period; i < len(a.priceHistory); i++ {
		mean += a.priceHistory[i]
	}
	mean /= float64(a.period)

	variance := 0.0
	for i := len(a.priceHistory) - a.period; i < len(a.priceHistory); i++ {
		diff := a.priceHistory[i] - mean
		variance += diff * diff
	}
	variance /= float64(a.period)
	stdDev := 0.0
	if variance > 0 {
		stdDev = 1.0
		for i := 0; i < 10; i++ {
			stdDev = (stdDev + variance/stdDev) / 2
		}
	}

	// 计算布林带
	upperBand := mean + a.threshold*stdDev
	lowerBand := mean - a.threshold*stdDev

	currentPrice := candle.Close

	// 价格低于下轨：买入信号
	if currentPrice < lowerBand && !a.hasPosition {
		a.hasPosition = true
		a.entryPrice = currentPrice
		return Signal{
			Action: "buy",
			Price:  currentPrice,
			Reason: fmt.Sprintf("价格低于下轨 (%.2f < %.2f)", currentPrice, lowerBand),
		}
	}

	// 价格高于上轨：卖出信号
	if currentPrice > upperBand && a.hasPosition {
		a.hasPosition = false
		a.entryPrice = 0
		return Signal{
			Action: "sell",
			Price:  currentPrice,
			Reason: fmt.Sprintf("价格高于上轨 (%.2f > %.2f)", currentPrice, upperBand),
		}
	}

	// 价格回归均值：卖出信号
	if a.hasPosition && currentPrice >= mean {
		a.hasPosition = false
		a.entryPrice = 0
		return Signal{
			Action: "sell",
			Price:  currentPrice,
			Reason: fmt.Sprintf("价格回归均值 (%.2f >= %.2f)", currentPrice, mean),
		}
	}

	return Signal{Action: "hold", Price: currentPrice, Reason: "等待信号"}
}

// GetName 获取策略名称
func (a *MeanReversionAdapter) GetName() string {
	return a.name
}

// TrendFollowingAdapter 趋势跟踪策略适配器
type TrendFollowingAdapter struct {
	name         string
	priceHistory []float64
	fastPeriod   int
	slowPeriod   int
	hasPosition  bool
	entryPrice   float64
}

// NewTrendFollowingAdapter 创建趋势跟踪策略适配器
func NewTrendFollowingAdapter() *TrendFollowingAdapter {
	return &TrendFollowingAdapter{
		name:         "trend_following",
		priceHistory: make([]float64, 0, 100),
		fastPeriod:   10,
		slowPeriod:   30,
	}
}

// OnCandle 处理 K 线数据
func (a *TrendFollowingAdapter) OnCandle(candle *exchange.Candle) Signal {
	a.priceHistory = append(a.priceHistory, candle.Close)

	// 保持历史记录
	maxHistory := a.slowPeriod * 3
	if len(a.priceHistory) > maxHistory {
		a.priceHistory = a.priceHistory[len(a.priceHistory)-maxHistory:]
	}

	if len(a.priceHistory) < a.slowPeriod {
		return Signal{Action: "hold", Price: candle.Close, Reason: "数据不足"}
	}

	// 计算快速均线
	fastMA := 0.0
	for i := len(a.priceHistory) - a.fastPeriod; i < len(a.priceHistory); i++ {
		fastMA += a.priceHistory[i]
	}
	fastMA /= float64(a.fastPeriod)

	// 计算慢速均线
	slowMA := 0.0
	for i := len(a.priceHistory) - a.slowPeriod; i < len(a.priceHistory); i++ {
		slowMA += a.priceHistory[i]
	}
	slowMA /= float64(a.slowPeriod)

	// 金叉：快线上穿慢线，买入信号
	if fastMA > slowMA && !a.hasPosition {
		a.hasPosition = true
		a.entryPrice = candle.Close
		return Signal{
			Action: "buy",
			Price:  candle.Close,
			Reason: fmt.Sprintf("金叉信号 (快线=%.2f > 慢线=%.2f)", fastMA, slowMA),
		}
	}

	// 死叉：快线下穿慢线，卖出信号
	if fastMA < slowMA && a.hasPosition {
		a.hasPosition = false
		a.entryPrice = 0
		return Signal{
			Action: "sell",
			Price:  candle.Close,
			Reason: fmt.Sprintf("死叉信号 (快线=%.2f < 慢线=%.2f)", fastMA, slowMA),
		}
	}

	return Signal{Action: "hold", Price: candle.Close, Reason: "等待信号"}
}

// GetName 获取策略名称
func (a *TrendFollowingAdapter) GetName() string {
	return a.name
}
