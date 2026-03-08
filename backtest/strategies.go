package backtest

import (
	"fmt"
	"math"
)

// GridRiskControl 网格风控配置
type GridRiskControl struct {
	Enabled                   bool    // 是否启用风控
	StopLossRatio             float64 // 止损比例（如 0.2 = 20%）
	TakeProfitTriggerRatio    float64 // 止盈触发比例（如 0.08 = 8%）
	TrailingTakeProfitRatio   float64 // 移动止盈比例
	MaxGridLayers             int     // 最大网格层数
	MaxOpenOrdersAtCap        int     // 达到层数限制时最大挂单数
	TrendFilterEnabled        bool    // 是否启用趋势检测
}

// GridBacktestStrategy 网格回测策略
type GridBacktestStrategy struct {
	Name         string
	Symbol       string
	GridCount    int
	GridSpacing  float64
	GridLeverage int
	TotalCapital float64
	Direction    string // "LONG", "SHORT", "BOTH"
	RiskControl  *GridRiskControl

	// 運行時狀態
	Initialized     bool
	centerPrice     float64
	gridOrders      map[float64]TickOrder
	currentState    string // RANGE, BREAKOUT_UP, BREAKOUT_DOWN
	account         *BacktestAccount
	entryPrice      float64     // 入场价格（用于风控）
	entryCapital    float64     // 入场资金（用于风控）
	maxCapital      float64     // 最高资金（用于移动止盈）
	priceHistory    []float64   // 价格历史（用于趋势检测）
	currentPosition float64     // 当前持仓量
	currentLayers   int         // 当前网格层数
}

// NewGridBacktestStrategy 創建网格回测策略
func NewGridBacktestStrategy(name, symbol string, gridCount int, gridSpacing, gridLeverage, totalCapital float64) *GridBacktestStrategy {
	return &GridBacktestStrategy{
		Name:         name,
		Symbol:       symbol,
		GridCount:    gridCount,
		GridSpacing:  gridSpacing,
		GridLeverage: int(gridLeverage),
		TotalCapital: totalCapital,
		Direction:    "LONG", // 默认做多
		RiskControl:  nil,
		Initialized:  false,
		gridOrders:   make(map[float64]TickOrder),
		currentState: "RANGE",
		priceHistory: make([]float64, 0, 100),
	}
}

// SetDirection 设置交易方向
func (s *GridBacktestStrategy) SetDirection(direction string) {
	switch direction {
	case "LONG", "SHORT", "BOTH":
		s.Direction = direction
	default:
		s.Direction = "LONG"
	}
}

// SetRiskControl 设置风控配置
func (s *GridBacktestStrategy) SetRiskControl(riskControl *GridRiskControl) {
	s.RiskControl = riskControl
}

func (s *GridBacktestStrategy) OnInit(account *BacktestAccount, cfg interface{}) error {
	s.account = account
	// 初始化风控状态
	if s.RiskControl != nil && s.RiskControl.Enabled {
		s.entryPrice = account.GetLastPrice()
		s.entryCapital = account.Cash + account.PositionSize*s.entryPrice
		s.maxCapital = s.entryCapital
	}
	return nil
}

func (s *GridBacktestStrategy) OnKline(kline TickKline, timestamp int64) ([]TickOrder, error) {
	// 更新价格历史（用于趋势检测）
	s.priceHistory = append(s.priceHistory, kline.Close)
	if len(s.priceHistory) > 100 {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-100:]
	}

	// 风控检查
	if s.RiskControl != nil && s.RiskControl.Enabled {
		if stopOrders, shouldStop := s.checkRiskControl(kline, timestamp); shouldStop {
			return stopOrders, nil
		}
	}

	// 只在RANGE狀態下運行网格
	if s.currentState == "BREAKOUT_UP" || s.currentState == "BREAKOUT_DOWN" {
		return []TickOrder{}, nil
	}

	// 初始化网格
	if !s.Initialized {
		s.initializeGrid(kline.Close)
	}

	// 检测市场狀態
	s.detectMarketState(kline)

	// 返回网格訂單，根据方向过滤
	var orders []TickOrder
	for _, order := range s.gridOrders {
		// 根据交易方向过滤订单
		if !s.shouldGenerateOrder(order.Side) {
			continue
		}
		orders = append(orders, order)
	}

	return orders, nil
}

// shouldGenerateOrder 根据交易方向判断是否应该生成订单
func (s *GridBacktestStrategy) shouldGenerateOrder(orderSide string) bool {
	switch s.Direction {
	case "LONG":
		// 单向做多：只生成买单（当无持仓时），不生成卖单
		// 实际上网格策略需要同时挂买卖单来成交
		// 这里我们简化处理：LONG 表示倾向于持有做多仓位
		return true
	case "SHORT":
		// 单向做空
		return true
	case "BOTH":
		// 双向交易
		return true
	default:
		return true
	}
}

// checkRiskControl 检查风控条件
func (s *GridBacktestStrategy) checkRiskControl(kline TickKline, timestamp int64) ([]TickOrder, bool) {
	if s.account == nil {
		return nil, false
	}

	currentCapital := s.account.Cash + s.account.PositionSize*kline.Close

	// 更新最高资金
	if currentCapital > s.maxCapital {
		s.maxCapital = currentCapital
	}

	// 检查止损
	if s.RiskControl.StopLossRatio > 0 {
		lossRatio := (s.entryCapital - currentCapital) / s.entryCapital
		if lossRatio >= s.RiskControl.StopLossRatio {
			// 触发止损，平仓
			if s.account.PositionSize > 0 {
				closeOrder := TickOrder{
					OrderID:   fmt.Sprintf("%s_stoploss_%d", s.Name, timestamp),
					Side:      "sell",
					Price:     kline.Close,
					Size:      s.account.PositionSize,
					Strategy:  s.Name,
					IsGrid:    false,
					IsRiskCtrl: true,
				}
				return []TickOrder{closeOrder}, true
			}
		}
	}

	// 检查移动止盈
	if s.RiskControl.TakeProfitTriggerRatio > 0 && s.RiskControl.TrailingTakeProfitRatio > 0 {
		profitRatio := (currentCapital - s.entryCapital) / s.entryCapital
		if profitRatio >= s.RiskControl.TakeProfitTriggerRatio {
			// 已达到止盈触发比例，检查回撤
			drawdownRatio := (s.maxCapital - currentCapital) / s.maxCapital
			if drawdownRatio >= s.RiskControl.TrailingTakeProfitRatio {
				// 触发移动止盈，平仓
				if s.account.PositionSize > 0 {
					closeOrder := TickOrder{
						OrderID:   fmt.Sprintf("%s_trailingtp_%d", s.Name, timestamp),
						Side:      "sell",
						Price:     kline.Close,
						Size:      s.account.PositionSize,
						Strategy:  s.Name,
						IsGrid:    false,
						IsRiskCtrl: true,
					}
					return []TickOrder{closeOrder}, true
				}
			}
		}
	}

	// 检查趋势检测
	if s.RiskControl.TrendFilterEnabled && len(s.priceHistory) >= 20 {
		if s.isDownTrend() {
			// 下跌趋势中，暂停开仓（已挂的订单仍然有效）
			// 这里可以返回空订单列表来暂停新开仓
		}
	}

	// 检查网格层数限制
	if s.RiskControl.MaxGridLayers > 0 {
		if s.currentLayers >= s.RiskControl.MaxGridLayers {
			// 达到层数限制，限制挂单数量
			maxOrders := s.RiskControl.MaxOpenOrdersAtCap
			if maxOrders > 0 && len(s.gridOrders) > maxOrders {
				// 只保留最近的 maxOrders 个订单
				// 这里简化处理，返回限制后的订单
			}
		}
	}

	return nil, false
}

// isDownTrend 检测是否为下跌趋势
func (s *GridBacktestStrategy) isDownTrend() bool {
	if len(s.priceHistory) < 20 {
		return false
	}

	// 简单的趋势检测：比较最近20根K线的MA
	sum := 0.0
	for _, p := range s.priceHistory {
		sum += p
	}
	ma := sum / float64(len(s.priceHistory))

	// 如果当前价格低于MA，认为是下跌趋势
	return s.priceHistory[len(s.priceHistory)-1] < ma
}

func (s *GridBacktestStrategy) OnTrade(trade TickTrade) {
	// 成交后更新网格（移除已成交訂單）
	delete(s.gridOrders, trade.Price)

	// 更新当前持仓
	if trade.Side == "buy" {
		s.currentPosition += trade.Size
	} else if trade.Side == "sell" {
		s.currentPosition -= trade.Size
	}

	// 更新网格层数（简化计算）
	if len(s.gridOrders) > 0 {
		s.currentLayers = s.GridCount - len(s.gridOrders)
	}
}

func (s *GridBacktestStrategy) GetName() string {
	return s.Name
}

func (s *GridBacktestStrategy) GetType() string {
	return "grid"
}

func (s *GridBacktestStrategy) GetConfig() map[string]interface{} {
	config := map[string]interface{}{
		"grid_count":    s.GridCount,
		"grid_spacing":  s.GridSpacing,
		"grid_leverage": s.GridLeverage,
		"total_capital": s.TotalCapital,
		"direction":     s.Direction,
	}

	if s.RiskControl != nil {
		config["risk_control_enabled"] = s.RiskControl.Enabled
		config["stop_loss_ratio"] = s.RiskControl.StopLossRatio
		config["take_profit_trigger_ratio"] = s.RiskControl.TakeProfitTriggerRatio
		config["trailing_take_profit_ratio"] = s.RiskControl.TrailingTakeProfitRatio
		config["max_grid_layers"] = s.RiskControl.MaxGridLayers
		config["max_open_orders_at_cap"] = s.RiskControl.MaxOpenOrdersAtCap
		config["trend_filter_enabled"] = s.RiskControl.TrendFilterEnabled
	}

	return config
}

func (s *GridBacktestStrategy) initializeGrid(price float64) {
	s.centerPrice = price
	s.gridOrders = make(map[float64]TickOrder)

	// 計算每格資金
	capitalPerLevel := s.TotalCapital / float64(s.GridCount)

	// 生成网格訂單
	for i := 0; i < s.GridCount; i++ {
		// 計算网格價格（等差数列）
		priceOffset := float64(i-s.GridCount/2) * s.centerPrice * s.GridSpacing
		gridPrice := s.centerPrice + priceOffset

		if gridPrice <= 0 {
			continue
		}

		// 計算訂單數量
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
	// 简化的市场狀態检测
	// 实际应用中应该使用更复杂的指標
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

	// 運行时狀態
	orders []DCAOrder
}

type DCAOrder struct {
	Price     float64
	Size      float64
	Timestamp int64
}

// NewDCABacktestStrategy 創建DCA回测策略
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

	// 檢查是否需要下新訂單
	if len(s.orders) < s.MaxOrders {
		// 简单的DCA策略：價格每下跌1%加仓
		if len(s.orders) > 0 {
			lastOrder := s.orders[len(s.orders)-1]
			change := (lastOrder.Price - kline.Close) / lastOrder.Price

			if change > 0.01 {
				// 價格下跌超过1%，加仓
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
	// 更新訂單狀態
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

	// 運行时狀態
	orders []MartingaleOrder
}

type MartingaleOrder struct {
	Price     float64
	Size      float64
	Timestamp int64
	Level     int
}

// NewMartingaleBacktestStrategy 創建马丁回测策略
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

	// 檢查是否需要下新訂單
	if len(s.orders) < s.MaxOrders {
		if len(s.orders) > 0 {
			lastOrder := s.orders[len(s.orders)-1]
			change := (lastOrder.Price - kline.Close) / lastOrder.Price

			// 價格下跌超过设定比例，加仓
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
	// 更新訂單狀態
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

	// 運行时狀態
	positionSize float64
	entryPrice   float64
	priceHistory []float64
}

// NewTrendBacktestStrategy 創建趋势回测策略
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
	// 更新價格歷史
	s.priceHistory = append(s.priceHistory, kline.Close)
	if len(s.priceHistory) > s.Lookback {
		s.priceHistory = s.priceHistory[len(s.priceHistory)-s.Lookback:]
	}

	var orders []TickOrder

	// 計算移动平均线
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
			size := (s.TotalCapital * 0.3) / kline.Close // 使用30%資金
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
		// 持仓中，檢查止损
		change := (kline.Close - s.entryPrice) / s.entryPrice

		// 止损3%或趋势反转
		if change < -0.03 || kline.Close < ma {
			// 平倉
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

// NewComboBacktestStrategy 創建组合回测策略
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

	// 收集所有子策略的訂單
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
