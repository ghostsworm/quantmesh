package backtest

import (
	"fmt"
	"math"
)

// TickMatcher 准tick级撮合引擎
// 参考自 paxg 项目，实现基于价格路径穿越检测的订单撮合
type TickMatcher struct {
	// 配置参数
	BuySlippage           float64 // 买入滑点系数 (默认 1.0001)
	SellSlippage          float64 // 卖出滑点系数 (默认 0.9999)
	MaxVolumeRatio        float64 // 每分钟最大成交量占比 (默认 0.2)
	MaxGridTradesPerMinute int     // 每分钟最大网格成交数 (默认 5)

	// 运行时状态
	minuteVolumeUsed  float64
	minuteGridTrades  int
	currentMinuteVol  float64
	tradeIDCounter    int
	onTradeCallback   func(*TickTrade)
}

// TickPricePath 价格路径点 (价格, 时间权重)
type TickPricePath struct {
	Price float64
	Ratio float64 // 0.0 - 1.0，表示在分钟内的时间位置
}

// TickKline 分钟K线数据
type TickKline struct {
	Timestamp int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

// TickOrder 订单
type TickOrder struct {
	OrderID    string
	Side       string // "buy" or "sell"
	Price      float64
	Size       float64
	Strategy   string
	GridLevel  int    // 网格层级（仅网格策略使用）
	IsGrid     bool   // 是否为网格订单
}

// TickTrade 成交记录
type TickTrade struct {
	TradeID    string
	OrderID    string
	Side       string
	Price      float64
	Size       float64
	Strategy   string
	Timestamp  int64
	GridLevel  int
	Slippage   float64 // 滑点成本
}

// MatcherConfig 撮合引擎配置
type MatcherConfig struct {
	BuySlippage           float64
	SellSlippage          float64
	MaxVolumeRatio        float64
	MaxGridTradesPerMinute int
}

// DefaultMatcherConfig 返回默认配置
func DefaultMatcherConfig() MatcherConfig {
	return MatcherConfig{
		BuySlippage:           1.0001,
		SellSlippage:          0.9999,
		MaxVolumeRatio:        0.2,
		MaxGridTradesPerMinute: 5,
	}
}

// NewTickMatcher 创建新的tick撮合引擎
func NewTickMatcher(config MatcherConfig) *TickMatcher {
	if config.BuySlippage == 0 {
		config.BuySlippage = 1.0001
	}
	if config.SellSlippage == 0 {
		config.SellSlippage = 0.9999
	}
	if config.MaxVolumeRatio == 0 {
		config.MaxVolumeRatio = 0.2
	}
	if config.MaxGridTradesPerMinute == 0 {
		config.MaxGridTradesPerMinute = 5
	}

	return &TickMatcher{
		BuySlippage:           config.BuySlippage,
		SellSlippage:          config.SellSlippage,
		MaxVolumeRatio:        config.MaxVolumeRatio,
		MaxGridTradesPerMinute: config.MaxGridTradesPerMinute,
	}
}

// SetTradeCallback 设置成交回调函数
func (m *TickMatcher) SetTradeCallback(callback func(*TickTrade)) {
	m.onTradeCallback = callback
}

// GetPricePath 生成分钟内价格路径
// 参考 paxg 实现，根据开盘价和收盘价关系生成不同的价格路径
func (k *TickKline) GetPricePath() []TickPricePath {
	if k.Close >= k.Open {
		// 阳线：开盘 -> 最低 -> 最高 -> 收盘
		return []TickPricePath{
			{Price: k.Open, Ratio: 0.0},
			{Price: k.Low, Ratio: 1.0 / 3.0},
			{Price: k.High, Ratio: 2.0 / 3.0},
			{Price: k.Close, Ratio: 1.0},
		}
	}
	// 阴线：开盘 -> 最高 -> 最低 -> 收盘
	return []TickPricePath{
		{Price: k.Open, Ratio: 0.0},
		{Price: k.High, Ratio: 1.0 / 3.0},
		{Price: k.Low, Ratio: 2.0 / 3.0},
		{Price: k.Close, Ratio: 1.0},
	}
}

// crossesDown 检测价格是否从上方穿越到下方
func crossesDown(priceA, priceB, orderPrice float64) bool {
	return priceA > orderPrice && priceB <= orderPrice
}

// crossesUp 检测价格是否从下方穿越到上方
func crossesUp(priceA, priceB, orderPrice float64) bool {
	return priceA < orderPrice && priceB >= orderPrice
}

// ResetMinute 每分钟开始时重置状态
func (m *TickMatcher) ResetMinute(minuteVolume float64) {
	m.minuteVolumeUsed = 0
	m.minuteGridTrades = 0
	m.currentMinuteVol = minuteVolume
}

// canTrade 检查是否可以进行交易
func (m *TickMatcher) canTrade(size float64, isGrid bool, currentPosition float64, side string) bool {
	// 检查成交量限制
	maxVol := m.currentMinuteVol * m.MaxVolumeRatio
	if m.minuteVolumeUsed+size > maxVol {
		return false
	}

	// 检查网格交易频率限制
	if isGrid && m.minuteGridTrades >= m.MaxGridTradesPerMinute {
		return false
	}

	return true
}

// ProcessPath 处理价格路径，返回成交列表
// positionSize: 分钟初的净仓位（正=多，负=空）
func (m *TickMatcher) ProcessPath(kline *TickKline, orders []TickOrder, timestamp int64, positionSize float64) []TickTrade {
	m.ResetMinute(kline.Volume)
	path := kline.GetPricePath()

	var trades []TickTrade
	filledOrderIDs := make(map[string]bool)
	currentPosition := positionSize

	// 遍历价格路径
	for i := 0; i < len(path)-1; i++ {
		priceA := path[i].Price
		priceB := path[i+1].Price

		for _, order := range orders {
			// 跳过已成交订单
			if filledOrderIDs[order.OrderID] {
				continue
			}

			// 检测触发条件
			triggered := false
			if order.Side == "buy" {
				triggered = crossesDown(priceA, priceB, order.Price)
			} else {
				// 卖单：向上或向下穿越都触发
				triggered = crossesUp(priceA, priceB, order.Price) || crossesDown(priceA, priceB, order.Price)
			}

			if !triggered {
				continue
			}

			// 检查是否可以交易
			if !m.canTrade(order.Size, order.IsGrid, currentPosition, order.Side) {
				continue
			}

			// 计算成交价格（含滑点）
			fillPrice := order.Price
			slippage := 0.0
			if order.Side == "buy" {
				fillPrice = order.Price * m.BuySlippage
				slippage = fillPrice - order.Price
			} else {
				fillPrice = order.Price * m.SellSlippage
				slippage = order.Price - fillPrice
			}

			// 创建成交记录
			m.tradeIDCounter++
			trade := &TickTrade{
				TradeID:   fmt.Sprintf("T%d", m.tradeIDCounter),
				OrderID:   order.OrderID,
				Side:      order.Side,
				Price:     fillPrice,
				Size:      order.Size,
				Strategy:  order.Strategy,
				Timestamp: timestamp,
				GridLevel: order.GridLevel,
				Slippage:  slippage * order.Size,
			}

			trades = append(trades, *trade)
			filledOrderIDs[order.OrderID] = true

			// 更新状态
			m.minuteVolumeUsed += order.Size
			if order.IsGrid {
				m.minuteGridTrades++
			}

			// 更新仓位
			if order.Side == "buy" {
				currentPosition += order.Size
			} else {
				currentPosition -= order.Size
			}

			// 调用回调
			if m.onTradeCallback != nil {
				m.onTradeCallback(trade)
			}
		}
	}

	return trades
}

// ProcessPathWithLimit 处理价格路径（带仓位限制）
// maxLongSize: 最大多头仓位（资产数量）
// maxShortSize: 最大空头仓位（资产数量）
func (m *TickMatcher) ProcessPathWithLimit(
	kline *TickKline,
	orders []TickOrder,
	timestamp int64,
	positionSize float64,
	maxLongSize, maxShortSize float64,
) []TickTrade {
	m.ResetMinute(kline.Volume)
	path := kline.GetPricePath()

	var trades []TickTrade
	filledOrderIDs := make(map[string]bool)
	currentPosition := positionSize

	for i := 0; i < len(path)-1; i++ {
		priceA := path[i].Price
		priceB := path[i+1].Price

		for _, order := range orders {
			if filledOrderIDs[order.OrderID] {
				continue
			}

			triggered := false
			if order.Side == "buy" {
				triggered = crossesDown(priceA, priceB, order.Price)
			} else {
				triggered = crossesUp(priceA, priceB, order.Price) || crossesDown(priceA, priceB, order.Price)
			}

			if !triggered {
				continue
			}

			// 检查成交量限制
			maxVol := m.currentMinuteVol * m.MaxVolumeRatio
			if m.minuteVolumeUsed+order.Size > maxVol {
				continue
			}

			// 检查网格交易频率限制
			if order.IsGrid && m.minuteGridTrades >= m.MaxGridTradesPerMinute {
				continue
			}

			// 检查仓位限制
			if order.Side == "buy" && maxLongSize > 0 {
				if currentPosition+order.Size > maxLongSize {
					continue
				}
			} else if order.Side == "sell" && maxShortSize > 0 {
				if currentPosition-order.Size < -maxShortSize {
					continue
				}
			}

			// 计算成交价格
			fillPrice := order.Price
			slippage := 0.0
			if order.Side == "buy" {
				fillPrice = order.Price * m.BuySlippage
				slippage = fillPrice - order.Price
			} else {
				fillPrice = order.Price * m.SellSlippage
				slippage = order.Price - fillPrice
			}

			m.tradeIDCounter++
			trade := &TickTrade{
				TradeID:   fmt.Sprintf("T%d", m.tradeIDCounter),
				OrderID:   order.OrderID,
				Side:      order.Side,
				Price:     fillPrice,
				Size:      order.Size,
				Strategy:  order.Strategy,
				Timestamp: timestamp,
				GridLevel: order.GridLevel,
				Slippage:  slippage * order.Size,
			}

			trades = append(trades, *trade)
			filledOrderIDs[order.OrderID] = true
			m.minuteVolumeUsed += order.Size
			if order.IsGrid {
				m.minuteGridTrades++
			}

			if order.Side == "buy" {
				currentPosition += order.Size
			} else {
				currentPosition -= order.Size
			}

			if m.onTradeCallback != nil {
				m.onTradeCallback(trade)
			}
		}
	}

	return trades
}

// EstimateSlippage 估算滑点损失
func (m *TickMatcher) EstimateSlippage(trades []TickTrade) float64 {
	totalSlippage := 0.0
	for _, trade := range trades {
		totalSlippage += trade.Slippage
	}
	return totalSlippage
}

// CalculateEffectivePrice 计算有效成交价（扣除滑点后）
func (m *TickMatcher) CalculateEffectivePrice(originalPrice float64, side string, size float64) float64 {
	if side == "buy" {
		fillPrice := originalPrice * m.BuySlippage
		return fillPrice
	}
	fillPrice := originalPrice * m.SellSlippage
	return fillPrice
}

// ValidateOrder 验证订单是否有效
func (m *TickMatcher) ValidateOrder(order TickOrder) error {
	if order.OrderID == "" {
		return fmt.Errorf("order ID cannot be empty")
	}
	if order.Price <= 0 {
		return fmt.Errorf("price must be positive")
	}
	if order.Size <= 0 {
		return fmt.Errorf("size must be positive")
	}
	if order.Side != "buy" && order.Side != "sell" {
		return fmt.Errorf("side must be 'buy' or 'sell'")
	}
	return nil
}

// GetStatistics 获取撮合统计信息
type MatcherStatistics struct {
	TotalVolumeUsed  float64
	TotalGridTrades  int
	TotalTrades      int
	AverageSlippage  float64
}

// GetStats 获取当前统计信息
func (m *TickMatcher) GetStats() MatcherStatistics {
	return MatcherStatistics{
		TotalVolumeUsed: m.minuteVolumeUsed,
		TotalGridTrades: m.minuteGridTrades,
		TotalTrades:     m.tradeIDCounter,
		AverageSlippage: 0, // 需要在运行时累积计算
	}
}

// Reset 重置撮合引擎状态
func (m *TickMatcher) Reset() {
	m.minuteVolumeUsed = 0
	m.minuteGridTrades = 0
	m.currentMinuteVol = 0
	m.tradeIDCounter = 0
}

// Clone 克隆撮合引擎（用于并行回测）
func (m *TickMatcher) Clone() *TickMatcher {
	return &TickMatcher{
		BuySlippage:           m.BuySlippage,
		SellSlippage:          m.SellSlippage,
		MaxVolumeRatio:        m.MaxVolumeRatio,
		MaxGridTradesPerMinute: m.MaxGridTradesPerMinute,
		tradeIDCounter:        0,
	}
}

// Helper function to calculate price impact
func CalculatePriceImpact(size, volume, marketDepth float64) float64 {
	if volume == 0 {
		return 0
	}
	volumeRatio := size / volume
	if volumeRatio < 0.01 {
		return 0 // 小于1%成交量，无价格影响
	}
	// 简单线性模型：越大单滑点越大
	return math.Min(volumeRatio*0.001, 0.005) // 最多0.5%额外滑点
}

// CalculateEffectiveSpread 计算有效买卖价差
func CalculateEffectiveSpread(buyPrice, sellPrice float64) float64 {
	if sellPrice == 0 || buyPrice == 0 {
		return 0
	}
	return (sellPrice - buyPrice) / sellPrice
}
