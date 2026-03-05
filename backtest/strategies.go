package backtest

import (
	"fmt"
	"math"
)

// GridBacktestStrategy 网格回测策略
type GridBacktestStrategy struct {
	Name         string
	Symbol       string
	GridCount    int
	GridSpacing  float64
	GridLeverage int
	TotalCapital float64

	// 运行时状态
	Initialized  bool
	centerPrice  float64
	gridOrders   map[float64]TickOrder
	currentState string // RANGE, BREAKOUT_UP, BREAKOUT_DOWN
}

// NewGridBacktestStrategy 创建网格回测策略
func NewGridBacktestStrategy(name, symbol string, gridCount int, gridSpacing, gridLeverage, totalCapital float64) *GridBacktestStrategy {
	return &GridBacktestStrategy{
		Name:         name,
		Symbol:       symbol,
		GridCount:    gridCount,
		GridSpacing:  gridSpacing,
		GridLeverage: int(gridLeverage),
		TotalCapital: totalCapital,
		Initialized:  false,
		gridOrders:   make(map[float64]TickOrder),
		currentState: "RANGE",
	}
}

func (s *GridBacktestStrategy) OnInit(account *BacktestAccount, cfg interface{}) error {
	return nil
}

func (s *GridBacktestStrategy) OnKline(kline TickKline, timestamp int64) ([]TickOrder, error) {
	// 只在RANGE状态下运行网格
	if s.currentState == "BREAKOUT_UP" || s.currentState == "BREAKOUT_DOWN" {
		return []TickOrder{}, nil
	}

	// 初始化网格
	if !s.Initialized {
		s.initializeGrid(kline.Close)
	}

	// 检测市场状态
	s.detectMarketState(kline)

	// 返回网格订单
	var orders []TickOrder
	for _, order := range s.gridOrders {
		orders = append(orders, order)
	}

	return orders, nil
}

func (s *GridBacktestStrategy) OnTrade(trade TickTrade) {
	// 成交后更新网格（移除已成交订单）
	delete(s.gridOrders, trade.Price)
}

func (s *GridBacktestStrategy) GetName() string {
	return s.Name
}

func (s *GridBacktestStrategy) GetType() string {
	return "grid"
}

func (s *GridBacktestStrategy) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"grid_count":    s.GridCount,
		"grid_spacing":  s.GridSpacing,
		"grid_leverage": s.GridLeverage,
		"total_capital": s.TotalCapital,
	}
}

func (s *GridBacktestStrategy) initializeGrid(price float64) {
	s.centerPrice = price
	s.gridOrders = make(map[float64]TickOrder)

	// 计算每格资金
	capitalPerLevel := s.TotalCapital / float64(s.GridCount)

	// 生成网格订单
	for i := 0; i < s.GridCount; i++ {
		// 计算网格价格（等差数列）
		priceOffset := float64(i-s.GridCount/2) * s.centerPrice * s.GridSpacing
		gridPrice := s.centerPrice + priceOffset

		if gridPrice <= 0 {
			continue
		}

		// 计算订单数量
		orderSize := (capitalPerLevel * float64(s.GridLeverage)) / gridPrice

		var order TickOrder
		orderID := fmt.Sprintf("%s_%d", s.Name, i)

		if i < s.GridCount/2 {
			// 低于中心价：买单
			order = TickOrder{
				OrderID:   orderID,
				Side:      "buy",
				Price:     gridPrice,
				Size:      orderSize,
				Strategy:  s.Name,
				GridLevel: i,
				IsGrid:    true,
			}
		} else {
			// 高于中心价：卖单
			order = TickOrder{
				OrderID:   orderID,
				Side:      "sell",
				Price:     gridPrice,
				Size:      orderSize,
				Strategy:  s.Name,
				GridLevel: i,
				IsGrid:    true,
			}
		}

		s.gridOrders[gridPrice] = order
	}

	s.Initialized = true
}

func (s *GridBacktestStrategy) detectMarketState(kline TickKline) {
	// 简化的市场状态检测
	// 实际应用中应该使用更复杂的指标
	change := (kline.Close - s.centerPrice) / s.centerPrice

	if change > 0.05 { // 上涨5%突破
		s.currentState = "BREAKOUT_UP"
	} else if change < -0.05 { // 下跌5%突破
		s.currentState = "BREAKOUT_DOWN"
	} else {
		s.currentState = "RANGE"
	}
}

// DCABacktestStrategy DCA回测策略
type DCABacktestStrategy struct {
	Name            string
	Symbol          string
	BaseOrderAmount float64
	MaxOrders       int
	TotalCapital    float64

	// 运行时状态
	orders []DCAOrder
}

type DCAOrder struct {
	Price     float64
	Size      float64
	Timestamp int64
}

// NewDCABacktestStrategy 创建DCA回测策略
func NewDCABacktestStrategy(name, symbol string, baseOrderAmount float64, maxOrders, totalCapital float64) *DCABacktestStrategy {
	return &DCABacktestStrategy{
		Name:            name,
		Symbol:          symbol,
		BaseOrderAmount: baseOrderAmount,
		MaxOrders:       int(maxOrders),
		TotalCapital:    totalCapital,
		orders:          make([]DCAOrder, 0),
	}
}

func (s *DCABacktestStrategy) OnInit(account *BacktestAccount, cfg interface{}) error {
	return nil
}

func (s *DCABacktestStrategy) OnKline(kline TickKline, timestamp int64) ([]TickOrder, error) {
	var orders []TickOrder

	// 检查是否需要下新订单
	if len(s.orders) < s.MaxOrders {
		// 简单的DCA策略：价格每下跌1%加仓
		if len(s.orders) > 0 {
			lastOrder := s.orders[len(s.orders)-1]
			change := (lastOrder.Price - kline.Close) / lastOrder.Price

			if change > 0.01 {
				// 价格下跌超过1%，加仓
				size := s.BaseOrderAmount / kline.Close
				order := TickOrder{
					OrderID:   fmt.Sprintf("%s_dca_%d", s.Name, timestamp),
					Side:      "buy",
					Price:     kline.Close,
					Size:      size,
					Strategy:  s.Name,
					IsGrid:    false,
				}

				orders = append(orders, order)
				s.orders = append(s.orders, DCAOrder{
					Price:     kline.Close,
					Size:      size,
					Timestamp: timestamp,
				})
			}
		} else {
			// 首单
			size := s.BaseOrderAmount / kline.Close
			order := TickOrder{
				OrderID:   fmt.Sprintf("%s_dca_%d", s.Name, timestamp),
				Side:      "buy",
				Price:     kline.Close,
				Size:      size,
				Strategy:  s.Name,
				IsGrid:    false,
			}

			orders = append(orders, order)
			s.orders = append(s.orders, DCAOrder{
				Price:     kline.Close,
				Size:      size,
				Timestamp: timestamp,
			})
		}
	}

	return orders, nil
}

func (s *DCABacktestStrategy) OnTrade(trade TickTrade) {
	// 更新订单状态
	for i, order := range s.orders {
		if order.Price == trade.Price && order.Timestamp == trade.Timestamp {
			s.orders = append(s.orders[:i], s.orders[i+1:]...)
			break
		}
	}
}

func (s *DCABacktestStrategy) GetName() string {
	return s.Name
}

func (s *DCABacktestStrategy) GetType() string {
	return "dca"
}

func (s *DCABacktestStrategy) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"base_order_amount": s.BaseOrderAmount,
		"max_orders":        s.MaxOrders,
		"total_capital":     s.TotalCapital,
	}
}

// MartingaleBacktestStrategy 马丁回测策略
type MartingaleBacktestStrategy struct {
	Name            string
	Symbol          string
	BaseOrderAmount float64
	Multiplier      float64
	MaxOrders       int
	TotalCapital    float64

	// 运行时状态
	orders []MartingaleOrder
}

type MartingaleOrder struct {
	Price     float64
	Size      float64
	Timestamp int64
	Level     int
}

// NewMartingaleBacktestStrategy 创建马丁回测策略
func NewMartingaleBacktestStrategy(name, symbol string, baseOrderAmount, multiplier float64, maxOrders, totalCapital float64) *MartingaleBacktestStrategy {
	return &MartingaleBacktestStrategy{
		Name:            name,
		Symbol:          symbol,
		BaseOrderAmount: baseOrderAmount,
		Multiplier:      multiplier,
		MaxOrders:       int(maxOrders),
		TotalCapital:    totalCapital,
		orders:          make([]MartingaleOrder, 0),
	}
}

func (s *MartingaleBacktestStrategy) OnInit(account *BacktestAccount, cfg interface{}) error {
	return nil
}

func (s *MartingaleBacktestStrategy) OnKline(kline TickKline, timestamp int64) ([]TickOrder, error) {
	var orders []TickOrder

	// 检查是否需要下新订单
	if len(s.orders) < s.MaxOrders {
		if len(s.orders) > 0 {
			lastOrder := s.orders[len(s.orders)-1]
			change := (lastOrder.Price - kline.Close) / lastOrder.Price

			// 价格下跌超过设定比例，加仓
			if change > 0.01 {
				level := lastOrder.Level + 1
				size := s.BaseOrderAmount * math.Pow(s.Multiplier, float64(level)) / kline.Close

				order := TickOrder{
					OrderID:   fmt.Sprintf("%s_martingale_%d_%d", s.Name, timestamp, level),
					Side:      "buy",
					Price:     kline.Close,
					Size:      size,
					Strategy:  s.Name,
					IsGrid:    false,
				}

				orders = append(orders, order)
				s.orders = append(s.orders, MartingaleOrder{
					Price:     kline.Close,
					Size:      size,
					Timestamp: timestamp,
					Level:     level,
				})
			}
		} else {
			// 首单
			size := s.BaseOrderAmount / kline.Close
			order := TickOrder{
				OrderID:   fmt.Sprintf("%s_martingale_%d_0", s.Name, timestamp),
				Side:      "buy",
				Price:     kline.Close,
				Size:      size,
				Strategy:  s.Name,
				IsGrid:    false,
			}

			orders = append(orders, order)
			s.orders = append(s.orders, MartingaleOrder{
				Price:     kline.Close,
				Size:      size,
				Timestamp: timestamp,
				Level:     0,
			})
		}
	}

	return orders, nil
}

func (s *MartingaleBacktestStrategy) OnTrade(trade TickTrade) {
	// 更新订单状态
	for i, order := range s.orders {
		if order.Price == trade.Price && order.Timestamp == trade.Timestamp {
			s.orders = append(s.orders[:i], s.orders[i+1:]...)
			break
		}
	}
}

func (s *MartingaleBacktestStrategy) GetName() string {
	return s.Name
}

func (s *MartingaleBacktestStrategy) GetType() string {
	return "martingale"
}

func (s *MartingaleBacktestStrategy) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"base_order_amount": s.BaseOrderAmount,
		"multiplier":        s.Multiplier,
		"max_orders":        s.MaxOrders,
		"total_capital":     s.TotalCapital,
	}
}

// TrendBacktestStrategy 趋势回测策略
type TrendBacktestStrategy struct {
	Name         string
	Symbol       string
	TotalCapital float64
	Lookback     int

	// 运行时状态
	positionSize float64
	entryPrice   float64
	priceHistory []float64
}

// NewTrendBacktestStrategy 创建趋势回测策略
func NewTrendBacktestStrategy(name, symbol string, totalCapital float64, lookback int) *TrendBacktestStrategy {
	return &TrendBacktestStrategy{
		Name:         name,
		Symbol:       symbol,
		TotalCapital: totalCapital,
		Lookback:     lookback,
		positionSize: 0,
		entryPrice:   0,
		priceHistory: make([]float64, 0),
	}
}

func (s *TrendBacktestStrategy) OnInit(account *BacktestAccount, cfg interface{}) error {
	return nil
}

func (s *TrendBacktestStrategy) OnKline(kline TickKline, timestamp int64) ([]TickOrder, error) {
	// 更新价格历史
	s.priceHistory = append(s.priceHistory, kline.Close)
	if len(s.priceHistory) > s.Lookback {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-s.Lookback:]
	}

	var orders []TickOrder

	// 计算移动平均线
	if len(s.priceHistory) < s.Lookback {
		return orders, nil
	}

	sum := 0.0
	for _, p := range s.priceHistory {
		sum += p
	}
	ma := sum / float64(len(s.priceHistory))

	// 趋势突破策略
	if s.positionSize == 0 {
		if kline.Close > ma && kline.Close > s.priceHistory[len(s.priceHistory)-2] {
			// 突破上涨，做多
			size := (s.TotalCapital * 0.3) / kline.Close // 使用30%资金
			order := TickOrder{
				OrderID:   fmt.Sprintf("%s_trend_long_%d", s.Name, timestamp),
				Side:      "buy",
				Price:     kline.Close,
				Size:      size,
				Strategy:  s.Name,
				IsGrid:    false,
			}

			orders = append(orders, order)
		}
	} else {
		// 持仓中，检查止损
		change := (kline.Close - s.entryPrice) / s.entryPrice

		// 止损3%或趋势反转
		if change < -0.03 || kline.Close < ma {
			// 平仓
			order := TickOrder{
				OrderID:   fmt.Sprintf("%s_trend_close_%d", s.Name, timestamp),
				Side:      "sell",
				Price:     kline.Close,
				Size:      s.positionSize,
				Strategy:  s.Name,
				IsGrid:    false,
			}

			orders = append(orders, order)
		}
	}

	return orders, nil
}

func (s *TrendBacktestStrategy) OnTrade(trade TickTrade) {
	if trade.Side == "buy" {
		s.positionSize = trade.Size
		s.entryPrice = trade.Price
	} else if trade.Side == "sell" {
		s.positionSize = 0
		s.entryPrice = 0
	}
}

func (s *TrendBacktestStrategy) GetName() string {
	return s.Name
}

func (s *TrendBacktestStrategy) GetType() string {
	return "trend"
}

func (s *TrendBacktestStrategy) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"total_capital": s.TotalCapital,
		"lookback":      s.Lookback,
	}
}

// ComboBacktestStrategy 组合回测策略
type ComboBacktestStrategy struct {
	Name          string
	Symbol        string
	TotalCapital  float64
	SubStrategies []BacktestStrategy
	Weights       []float64
	MarketState   string
}

// NewComboBacktestStrategy 创建组合回测策略
func NewComboBacktestStrategy(name, symbol string, totalCapital float64, subStrategies []BacktestStrategy, weights []float64) *ComboBacktestStrategy {
	return &ComboBacktestStrategy{
		Name:          name,
		Symbol:        symbol,
		TotalCapital:  totalCapital,
		SubStrategies: subStrategies,
		Weights:       weights,
		MarketState:   "unknown",
	}
}

func (s *ComboBacktestStrategy) OnInit(account *BacktestAccount, cfg interface{}) error {
	return nil
}

func (s *ComboBacktestStrategy) OnKline(kline TickKline, timestamp int64) ([]TickOrder, error) {
	var allOrders []TickOrder

	// 收集所有子策略的订单
	for _, strategy := range s.SubStrategies {
		orders, err := strategy.OnKline(kline, timestamp)
		if err != nil {
			continue
		}
		allOrders = append(allOrders, orders...)
	}

	return allOrders, nil
}

func (s *ComboBacktestStrategy) OnTrade(trade TickTrade) {
	// 通知所有子策略
	for _, strategy := range s.SubStrategies {
		strategy.OnTrade(trade)
	}
}

func (s *ComboBacktestStrategy) GetName() string {
	return s.Name
}

func (s *ComboBacktestStrategy) GetType() string {
	return "combo"
}

func (s *ComboBacktestStrategy) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"total_capital": s.TotalCapital,
		"sub_strategies": len(s.SubStrategies),
	}
}
