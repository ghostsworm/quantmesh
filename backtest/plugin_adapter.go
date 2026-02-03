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

// NewPluginStrategyAdapter 創建插件策略适配器
func NewPluginStrategyAdapter(pluginPath string, strategyName string, config map[string]interface{}) (*PluginStrategyAdapter, error) {
	// 加載插件
	p, err := plugin.Open(pluginPath)
	if err != nil {
		return nil, fmt.Errorf("加載插件失败: %w", err)
	}

	// 獲取 NewPlugin 函數
	newPluginSymbol, err := p.Lookup("NewPlugin")
	if err != nil {
		return nil, fmt.Errorf("插件缺少 NewPlugin 函數: %w", err)
	}

	newPluginFunc, ok := newPluginSymbol.(func() interface{})
	if !ok {
		return nil, fmt.Errorf("NewPlugin 函數签名錯误")
	}

	// 創建插件實例
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

	logger.Info("✅ 加載插件策略: %s (%s)", strategyName, pluginPath)

	return adapter, nil
}

// OnCandle 处理 K 線數據
func (a *PluginStrategyAdapter) OnCandle(candle *exchange.Candle) Signal {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 調用插件的 ExecuteStrategy 方法
	if executor, ok := a.pluginObj.(interface {
		ExecuteStrategy(ctx context.Context, strategyName string, params map[string]interface{}) (map[string]interface{}, error)
	}); ok {
		params := map[string]interface{}{
			"price": candle.Close,
		}

		result, err := executor.ExecuteStrategy(context.Background(), a.strategyName, params)
		if err != nil {
			logger.Warn("⚠️ 執行策略失败: %v", err)
			return Signal{Action: "hold", Price: candle.Close, Reason: "策略執行失败"}
		}

		// 解析結果
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

	return Signal{Action: "hold", Price: candle.Close, Reason: "插件不支援 ExecuteStrategy"}
}

// GetName 獲取策略名称
func (a *PluginStrategyAdapter) GetName() string {
	return a.strategyName
}

// MomentumAdapter 动量策略适配器（直接實現，不依赖插件）
type MomentumAdapter struct {
	name         string
	priceHistory []float64
	rsiPeriod    int
	overbought   float64
	oversold     float64
	hasPosition  bool
	entryPrice   float64
}

// NewMomentumAdapter 創建动量策略适配器
func NewMomentumAdapter() *MomentumAdapter {
	return NewMomentumAdapterWithParams(nil)
}

// NewMomentumAdapterWithParams 創建帶參數的动量策略适配器
func NewMomentumAdapterWithParams(params map[string]interface{}) *MomentumAdapter {
	rsiPeriod := 14
	overbought := 70.0
	oversold := 30.0
	if params != nil {
		if v, ok := params["rsi_period"]; ok {
			switch t := v.(type) {
			case float64:
				rsiPeriod = int(t)
			case int:
				rsiPeriod = t
			case int64:
				rsiPeriod = int(t)
			}
		}
		if v, ok := params["overbought"]; ok {
			switch t := v.(type) {
			case float64:
				overbought = t
			case int:
				overbought = float64(t)
			}
		}
		if v, ok := params["oversold"]; ok {
			switch t := v.(type) {
			case float64:
				oversold = t
			case int:
				oversold = float64(t)
			}
		}
	}
	if rsiPeriod < 1 {
		rsiPeriod = 14
	}
	return &MomentumAdapter{
		name:         "momentum",
		priceHistory: make([]float64, 0, 100),
		rsiPeriod:    rsiPeriod,
		overbought:   overbought,
		oversold:     oversold,
	}
}

// OnCandle 处理 K 線數據
func (a *MomentumAdapter) OnCandle(candle *exchange.Candle) Signal {
	a.priceHistory = append(a.priceHistory, candle.Close)

	// 保持历史記錄
	maxHistory := a.rsiPeriod * 3
	if len(a.priceHistory) > maxHistory {
		a.priceHistory = a.priceHistory[len(a.priceHistory)-maxHistory:]
	}

	// 计算 RSI
	rsi := a.calculateRSI()
	if rsi == 50 {
		return Signal{Action: "hold", Price: candle.Close, Reason: "數據不足"}
	}

	// RSI < 30：超賣，買入信号
	if rsi < a.oversold && !a.hasPosition {
		a.hasPosition = true
		a.entryPrice = candle.Close
		return Signal{
			Action: "buy",
			Price:  candle.Close,
			Reason: fmt.Sprintf("RSI 超賣信号 (RSI=%.2f)", rsi),
		}
	}

	// RSI > 70：超買，賣出信号
	if rsi > a.overbought && a.hasPosition {
		a.hasPosition = false
		a.entryPrice = 0
		return Signal{
			Action: "sell",
			Price:  candle.Close,
			Reason: fmt.Sprintf("RSI 超買信号 (RSI=%.2f)", rsi),
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

// GetName 獲取策略名称
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

// NewMeanReversionAdapter 創建均值回归策略适配器
func NewMeanReversionAdapter() *MeanReversionAdapter {
	return NewMeanReversionAdapterWithParams(nil)
}

// NewMeanReversionAdapterWithParams 創建帶參數的均值回归策略适配器
func NewMeanReversionAdapterWithParams(params map[string]interface{}) *MeanReversionAdapter {
	period := 20
	threshold := 2.0
	if params != nil {
		if v, ok := params["period"]; ok {
			switch t := v.(type) {
			case float64:
				period = int(t)
			case int:
				period = t
			case int64:
				period = int(t)
			}
		}
		if v, ok := params["std_multiplier"]; ok {
			switch t := v.(type) {
			case float64:
				threshold = t
			case int:
				threshold = float64(t)
			}
		}
	}
	if period < 1 {
		period = 20
	}
	return &MeanReversionAdapter{
		name:         "mean_reversion",
		priceHistory: make([]float64, 0, 100),
		period:       period,
		threshold:    threshold,
	}
}

// OnCandle 处理 K 線數據
func (a *MeanReversionAdapter) OnCandle(candle *exchange.Candle) Signal {
	a.priceHistory = append(a.priceHistory, candle.Close)

	// 保持历史記錄
	maxHistory := a.period * 3
	if len(a.priceHistory) > maxHistory {
		a.priceHistory = a.priceHistory[len(a.priceHistory)-maxHistory:]
	}

	if len(a.priceHistory) < a.period {
		return Signal{Action: "hold", Price: candle.Close, Reason: "數據不足"}
	}

	// 计算均值和標准差
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

	// 價格低於下轨：買入信号
	if currentPrice < lowerBand && !a.hasPosition {
		a.hasPosition = true
		a.entryPrice = currentPrice
		return Signal{
			Action: "buy",
			Price:  currentPrice,
			Reason: fmt.Sprintf("價格低於下轨 (%.2f < %.2f)", currentPrice, lowerBand),
		}
	}

	// 價格高於上轨：賣出信号
	if currentPrice > upperBand && a.hasPosition {
		a.hasPosition = false
		a.entryPrice = 0
		return Signal{
			Action: "sell",
			Price:  currentPrice,
			Reason: fmt.Sprintf("價格高於上轨 (%.2f > %.2f)", currentPrice, upperBand),
		}
	}

	// 價格回归均值：賣出信号
	if a.hasPosition && currentPrice >= mean {
		a.hasPosition = false
		a.entryPrice = 0
		return Signal{
			Action: "sell",
			Price:  currentPrice,
			Reason: fmt.Sprintf("價格回归均值 (%.2f >= %.2f)", currentPrice, mean),
		}
	}

	return Signal{Action: "hold", Price: currentPrice, Reason: "等待信号"}
}

// GetName 獲取策略名称
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

// NewTrendFollowingAdapter 創建趋势跟踪策略适配器
func NewTrendFollowingAdapter() *TrendFollowingAdapter {
	return NewTrendFollowingAdapterWithParams(nil)
}

// NewTrendFollowingAdapterWithParams 創建帶參數的趋势跟踪策略适配器
func NewTrendFollowingAdapterWithParams(params map[string]interface{}) *TrendFollowingAdapter {
	fastPeriod := 10
	slowPeriod := 30
	if params != nil {
		if v, ok := params["fast_period"]; ok {
			switch t := v.(type) {
			case float64:
				fastPeriod = int(t)
			case int:
				fastPeriod = t
			case int64:
				fastPeriod = int(t)
			}
		}
		if v, ok := params["slow_period"]; ok {
			switch t := v.(type) {
			case float64:
				slowPeriod = int(t)
			case int:
				slowPeriod = t
			case int64:
				slowPeriod = int(t)
			}
		}
	}
	if fastPeriod < 1 {
		fastPeriod = 10
	}
	if slowPeriod < fastPeriod {
		slowPeriod = fastPeriod + 10
	}
	return &TrendFollowingAdapter{
		name:         "trend_following",
		priceHistory: make([]float64, 0, 100),
		fastPeriod:   fastPeriod,
		slowPeriod:   slowPeriod,
	}
}

// OnCandle 处理 K 線數據
func (a *TrendFollowingAdapter) OnCandle(candle *exchange.Candle) Signal {
	a.priceHistory = append(a.priceHistory, candle.Close)

	// 保持历史記錄
	maxHistory := a.slowPeriod * 3
	if len(a.priceHistory) > maxHistory {
		a.priceHistory = a.priceHistory[len(a.priceHistory)-maxHistory:]
	}

	if len(a.priceHistory) < a.slowPeriod {
		return Signal{Action: "hold", Price: candle.Close, Reason: "數據不足"}
	}

	// 计算快速均線
	fastMA := 0.0
	for i := len(a.priceHistory) - a.fastPeriod; i < len(a.priceHistory); i++ {
		fastMA += a.priceHistory[i]
	}
	fastMA /= float64(a.fastPeriod)

	// 计算慢速均線
	slowMA := 0.0
	for i := len(a.priceHistory) - a.slowPeriod; i < len(a.priceHistory); i++ {
		slowMA += a.priceHistory[i]
	}
	slowMA /= float64(a.slowPeriod)

	// 金叉：快線上穿慢線，買入信号
	if fastMA > slowMA && !a.hasPosition {
		a.hasPosition = true
		a.entryPrice = candle.Close
		return Signal{
			Action: "buy",
			Price:  candle.Close,
			Reason: fmt.Sprintf("金叉信号 (快線=%.2f > 慢線=%.2f)", fastMA, slowMA),
		}
	}

	// 死叉：快線下穿慢線，賣出信号
	if fastMA < slowMA && a.hasPosition {
		a.hasPosition = false
		a.entryPrice = 0
		return Signal{
			Action: "sell",
			Price:  candle.Close,
			Reason: fmt.Sprintf("死叉信号 (快線=%.2f < 慢線=%.2f)", fastMA, slowMA),
		}
	}

	return Signal{Action: "hold", Price: candle.Close, Reason: "等待信号"}
}

// GetName 獲取策略名称
func (a *TrendFollowingAdapter) GetName() string {
	return a.name
}
