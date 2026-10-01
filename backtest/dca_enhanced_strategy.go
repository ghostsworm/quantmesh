package backtest

import (
	"fmt"
	"math"
	"sort"

	"quantmesh/indicators"
)

type dcaEnhancedBacktestLayer struct {
	index      int
	price      float64
	quantity   float64
	cost       float64
	openingFee float64
}

type dcaEnhancedBacktestOrder struct {
	order     TickOrder
	layer     *dcaEnhancedBacktestLayer
	closeLast int
	emitted   bool
}

// DCAEnhancedBacktestStrategy reproduces the live enhanced DCA layer and exit
// rules in the candle backtest engine. Signals are queued for the next candle
// to avoid filling an order inside the candle whose close created that signal.
type DCAEnhancedBacktestStrategy struct {
	Name, Symbol                                                                 string
	TotalCapital                                                                 float64
	FeeRate                                                                      float64
	BaseOrderAmount, SafetyOrderAmount                                           float64
	MaxSafetyOrders, ATRPeriod, TrendPeriod                                      int
	ATRMultiplier, MinPriceStep, MaxPriceStep, SafetyOrderScale, SafetyOrderStep float64
	FirstOrderTakeProfit, LastOrderTakeProfit, TotalTakeProfit                   float64
	TrailingTakeProfit, TrailingActivation, StopLoss                             float64
	CascadeProtection                                                            bool
	CascadeDropThreshold                                                         float64
	CascadePauseDuration                                                         int
	TrendFilterEnabled                                                           bool
	TrendMethod                                                                  string
	SellSlippage                                                                 float64

	account             *BacktestAccount
	candles             []indicators.Candle
	prices              []float64
	layers              []*dcaEnhancedBacktestLayer
	working             map[string]*dcaEnhancedBacktestOrder
	orderSequence       uint64
	dynamicInterval     float64
	highestProfit       float64
	takeProfitTriggered bool
	paused              bool
	pauseUntil          int64
}

func NewDCAEnhancedBacktestStrategy(name, symbol string, params map[string]interface{}, totalCapital float64) (*DCAEnhancedBacktestStrategy, error) {
	getFloat := func(key string, fallback float64) float64 { return getFloatParam(params, key, fallback) }
	getInt := func(key string, fallback int) int { return getIntParam(params, key, fallback) }
	getBool := func(key string, fallback bool) bool { return getBoolParam(params, key, fallback) }
	getString := func(key, fallback string) string {
		if value := getStringParam(params, key); value != "" {
			return value
		}
		return fallback
	}
	s := &DCAEnhancedBacktestStrategy{
		Name: name, Symbol: symbol, TotalCapital: totalCapital,
		FeeRate:           getFloat("fee_rate", 0.0004),
		BaseOrderAmount:   getFloat("base_order_amount", 100),
		SafetyOrderAmount: getFloat("safety_order_amount", 200),
		MaxSafetyOrders:   getInt("max_safety_orders", 50),
		ATRPeriod:         getInt("atr_period", 14), ATRMultiplier: getFloat("atr_multiplier", 1.5),
		MinPriceStep: getFloat("min_price_step", 1), MaxPriceStep: getFloat("max_price_step", 5),
		SafetyOrderScale: getFloat("safety_order_scale", 1.05), SafetyOrderStep: getFloat("safety_order_step", 1),
		FirstOrderTakeProfit: getFloat("first_order_take_profit", 1), LastOrderTakeProfit: getFloat("last_order_take_profit", 0.5),
		TotalTakeProfit: getFloat("total_take_profit", 2), TrailingTakeProfit: getFloat("trailing_take_profit", 0.5),
		TrailingActivation: getFloat("trailing_activation", 1), StopLoss: getFloat("stop_loss", 10),
		CascadeProtection: getBool("cascade_protection", true), CascadeDropThreshold: getFloat("cascade_drop_threshold", 5),
		CascadePauseDuration: getInt("cascade_pause_duration", 300),
		TrendFilterEnabled:   getBool("trend_filter_enabled", true), TrendMethod: getString("trend_method", "ema"),
		SellSlippage: getFloat("sell_slippage", 0.9999),
		TrendPeriod:  getInt("trend_period", 20), dynamicInterval: getFloat("min_price_step", 1),
		working: make(map[string]*dcaEnhancedBacktestOrder),
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *DCAEnhancedBacktestStrategy) validate() error {
	if !isFinite(s.TotalCapital) || s.TotalCapital <= 0 || !isFinite(s.FeeRate) || s.FeeRate < 0 || s.FeeRate > 1 {
		return fmt.Errorf("DCA enhanced backtest capital and fee rate are invalid")
	}
	if !isFinite(s.BaseOrderAmount) || s.BaseOrderAmount <= 0 || !isFinite(s.SafetyOrderAmount) || s.SafetyOrderAmount <= 0 ||
		s.MaxSafetyOrders < 0 || s.MaxSafetyOrders > 50 || s.ATRPeriod < 2 || s.ATRPeriod > 1000 || s.TrendPeriod < 2 || s.TrendPeriod > 1000 || s.CascadePauseDuration < 0 || s.CascadePauseDuration > 86400 {
		return fmt.Errorf("DCA enhanced backtest order sizes or periods are invalid")
	}
	for name, value := range map[string]float64{
		"atr_multiplier": s.ATRMultiplier, "min_price_step": s.MinPriceStep, "max_price_step": s.MaxPriceStep,
		"safety_order_scale": s.SafetyOrderScale, "safety_order_step": s.SafetyOrderStep,
		"first_order_take_profit": s.FirstOrderTakeProfit, "last_order_take_profit": s.LastOrderTakeProfit,
		"total_take_profit": s.TotalTakeProfit, "trailing_take_profit": s.TrailingTakeProfit,
		"trailing_activation": s.TrailingActivation, "stop_loss": s.StopLoss, "cascade_drop_threshold": s.CascadeDropThreshold,
		"sell_slippage": s.SellSlippage,
	} {
		if !isFinite(value) || value < 0 {
			return fmt.Errorf("DCA enhanced backtest %s is invalid", name)
		}
	}
	if s.ATRMultiplier <= 0 || s.MinPriceStep <= 0 || s.MaxPriceStep < s.MinPriceStep ||
		s.SafetyOrderScale <= 0 || s.SafetyOrderStep <= 0 {
		return fmt.Errorf("DCA enhanced backtest spacing and scale parameters are inconsistent")
	}
	if s.SellSlippage <= 0 || s.SellSlippage > 1 {
		return fmt.Errorf("DCA enhanced backtest sell slippage must be in (0, 1]")
	}
	if s.MaxSafetyOrders > 0 {
		largestAmount := s.SafetyOrderAmount * math.Pow(s.SafetyOrderScale, float64(s.MaxSafetyOrders-1))
		largestStep := s.MaxPriceStep * math.Pow(s.SafetyOrderStep, float64(s.MaxSafetyOrders-1))
		if !isFinite(largestAmount) || largestAmount <= 0 || !isFinite(largestStep) {
			return fmt.Errorf("DCA enhanced backtest safety order progression overflows")
		}
	}
	return nil
}

func (s *DCAEnhancedBacktestStrategy) OnInit(account *BacktestAccount, _ interface{}) error {
	if account == nil {
		return fmt.Errorf("DCA enhanced backtest account is required")
	}
	s.account = account
	s.candles = make([]indicators.Candle, 0, 200)
	s.prices = make([]float64, 0, 200)
	s.layers = nil
	s.working = make(map[string]*dcaEnhancedBacktestOrder)
	s.highestProfit, s.takeProfitTriggered, s.paused = 0, false, false
	return nil
}

func (s *DCAEnhancedBacktestStrategy) OnKline(kline TickKline, timestamp int64) ([]TickOrder, error) {
	if !isFinite(kline.Open) || kline.Open <= 0 || !isFinite(kline.Close) || kline.Close <= 0 ||
		!isFinite(kline.High) || kline.High <= 0 || !isFinite(kline.Low) || kline.Low <= 0 ||
		!isFinite(kline.Volume) || kline.Volume < 0 || kline.High < kline.Low ||
		kline.High < kline.Open || kline.High < kline.Close || kline.Low > kline.Open || kline.Low > kline.Close {
		return nil, fmt.Errorf("DCA enhanced backtest received invalid candle at %d", timestamp)
	}
	if len(s.prices) > 0 {
		previousClose := s.prices[len(s.prices)-1]
		for _, intent := range s.working {
			if intent.layer == nil && intent.emitted {
				intent.order.Price = previousClose
			}
		}
	}
	existingOrderIDs := make([]string, 0, len(s.working))
	for id := range s.working {
		existingOrderIDs = append(existingOrderIDs, id)
	}
	sort.Strings(existingOrderIDs)
	s.appendCandle(kline, timestamp)
	s.updateDynamicInterval(kline.Close)
	s.updateCascadePause(kline.Close)
	s.checkExit(kline.Close)
	s.checkEntry(kline.Close)
	orders := make([]TickOrder, 0, len(existingOrderIDs))
	for _, id := range existingOrderIDs {
		if intent := s.working[id]; intent != nil {
			orders = append(orders, intent.order)
			intent.emitted = true
		}
	}
	return orders, nil
}

func (s *DCAEnhancedBacktestStrategy) appendCandle(k TickKline, timestamp int64) {
	s.candles = append(s.candles, indicators.Candle{Time: timestamp / 1000, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
	if len(s.candles) > 200 {
		s.candles = append([]indicators.Candle(nil), s.candles[len(s.candles)-200:]...)
	}
	s.prices = append(s.prices, k.Close)
	if len(s.prices) > 200 {
		s.prices = append([]float64(nil), s.prices[len(s.prices)-200:]...)
	}
}

func (s *DCAEnhancedBacktestStrategy) updateDynamicInterval(price float64) {
	if len(s.candles) < s.ATRPeriod+1 {
		s.dynamicInterval = s.MinPriceStep
		return
	}
	atr := indicators.NewATR(s.ATRPeriod).CurrentATR(s.candles)
	step := s.MinPriceStep
	if atr > 0 {
		step = math.Max(s.MinPriceStep, math.Min((atr/price)*100*s.ATRMultiplier, s.MaxPriceStep))
	}
	s.dynamicInterval = step
}

func (s *DCAEnhancedBacktestStrategy) updateCascadePause(price float64) {
	now := timestampNow(s.candles)
	if s.paused && now < s.pauseUntil {
		return
	}
	s.paused = false
	if !s.CascadeProtection || len(s.prices) < 10 {
		return
	}
	recent := s.prices[len(s.prices)-10:]
	peak := recent[0]
	for _, value := range recent {
		if value > peak {
			peak = value
		}
	}
	if peak > 0 && (peak-price)/peak*100 >= s.CascadeDropThreshold {
		s.paused = true
		s.pauseUntil = now + int64(s.CascadePauseDuration)*1000
	}
}

func timestampNow(candles []indicators.Candle) int64 {
	if len(candles) == 0 {
		return 0
	}
	return candles[len(candles)-1].Time * 1000
}

func (s *DCAEnhancedBacktestStrategy) checkEntry(price float64) {
	if s.paused || s.hasWorkingEntry() || s.hasWorkingExit() {
		return
	}
	if len(s.layers) == 0 {
		if s.TrendFilterEnabled && !s.trendUp() {
			return
		}
		s.submitEntry(0, s.BaseOrderAmount, price)
		return
	}
	if len(s.layers) >= s.MaxSafetyOrders+1 {
		return
	}
	last := s.layers[len(s.layers)-1]
	index := len(s.layers)
	requiredDrop := s.dynamicInterval * math.Pow(s.SafetyOrderStep, float64(index-1))
	if (last.price-price)/last.price*100 < requiredDrop {
		return
	}
	amount := s.SafetyOrderAmount * math.Pow(s.SafetyOrderScale, float64(index-1))
	s.submitEntry(index, amount, price)
}

func (s *DCAEnhancedBacktestStrategy) trendUp() bool {
	if len(s.prices) < s.TrendPeriod*2 {
		return true
	}
	values := s.prices[len(s.prices)-s.TrendPeriod*2:]
	var short, long float64
	if s.TrendMethod == "ema" {
		shortEMA, longEMA := indicators.EMA(values, s.TrendPeriod), indicators.EMA(values, s.TrendPeriod*2)
		if len(shortEMA) == 0 || len(longEMA) == 0 {
			return true
		}
		short, long = shortEMA[len(shortEMA)-1], longEMA[len(longEMA)-1]
	} else {
		shortSMA, longSMA := indicators.SMA(values, s.TrendPeriod), indicators.SMA(values, s.TrendPeriod*2)
		if len(shortSMA) == 0 || len(longSMA) == 0 {
			return true
		}
		short, long = shortSMA[len(shortSMA)-1], longSMA[len(longSMA)-1]
	}
	return short >= long
}

func (s *DCAEnhancedBacktestStrategy) submitEntry(index int, amount, price float64) {
	quantity := amount / price
	if !isFinite(quantity) || quantity <= 0 {
		return
	}
	s.orderSequence++
	id := fmt.Sprintf("%s_entry_%d", s.Name, s.orderSequence)
	layer := &dcaEnhancedBacktestLayer{index: index, price: price}
	s.working[id] = &dcaEnhancedBacktestOrder{order: TickOrder{OrderID: id, Side: "buy", Price: price, Size: quantity, Strategy: s.Name}, layer: layer, closeLast: -1}
}

func (s *DCAEnhancedBacktestStrategy) checkExit(price float64) {
	if len(s.layers) == 0 || s.hasWorkingExit() {
		return
	}
	quantity, cost, openingFee := s.inventory()
	if quantity <= 0 || cost <= 0 {
		return
	}
	pnl := s.netPnLPercent(quantity, cost, openingFee, price)
	if len(s.layers) == 1 && pnl >= s.FirstOrderTakeProfit {
		s.submitClose(price, -1)
		return
	}
	if len(s.layers) > 1 {
		last := s.layers[len(s.layers)-1]
		if s.netPnLPercent(last.quantity, last.cost, last.openingFee, price) >= s.LastOrderTakeProfit {
			s.submitClose(price, last.index)
			return
		}
	}
	if pnl >= s.TotalTakeProfit {
		s.submitClose(price, -1)
		return
	}
	if pnl > s.highestProfit {
		s.highestProfit = pnl
	}
	if !s.takeProfitTriggered && pnl >= s.TrailingActivation {
		s.takeProfitTriggered = true
	}
	if s.takeProfitTriggered && s.highestProfit-pnl >= s.TrailingTakeProfit {
		s.submitClose(price, -1)
		return
	}
	if pnl <= -s.StopLoss {
		s.submitClose(price, -1)
	}
}

func (s *DCAEnhancedBacktestStrategy) submitClose(price float64, lastLayerIndex int) {
	for id, intent := range s.working {
		if intent.layer != nil {
			delete(s.working, id)
		}
	}
	quantity := 0.0
	if lastLayerIndex >= 0 {
		for _, layer := range s.layers {
			if layer.index == lastLayerIndex {
				quantity = layer.quantity
				break
			}
		}
	} else {
		quantity, _, _ = s.inventory()
	}
	if quantity <= 0 {
		return
	}
	s.orderSequence++
	id := fmt.Sprintf("%s_close_%d", s.Name, s.orderSequence)
	s.working[id] = &dcaEnhancedBacktestOrder{order: TickOrder{OrderID: id, Side: "sell", Price: price, Size: quantity, Strategy: s.Name}, closeLast: lastLayerIndex}
}

func (s *DCAEnhancedBacktestStrategy) netPnLPercent(quantity, cost, openingFee, price float64) float64 {
	proceeds := quantity * price * s.SellSlippage
	return (proceeds - cost - openingFee - proceeds*s.FeeRate) / cost * 100
}

func (s *DCAEnhancedBacktestStrategy) inventory() (quantity, cost, fee float64) {
	for _, layer := range s.layers {
		quantity += layer.quantity
		cost += layer.cost
		fee += layer.openingFee
	}
	return
}

func (s *DCAEnhancedBacktestStrategy) hasWorkingEntry() bool {
	for _, order := range s.working {
		if order.layer != nil {
			return true
		}
	}
	return false
}

func (s *DCAEnhancedBacktestStrategy) hasWorkingExit() bool {
	for _, order := range s.working {
		if order.layer == nil {
			return true
		}
	}
	return false
}

func (s *DCAEnhancedBacktestStrategy) OnTrade(trade TickTrade) {
	intent := s.working[trade.OrderID]
	if intent == nil {
		return
	}
	if intent.layer != nil && trade.Side == "buy" {
		delete(s.working, trade.OrderID)
		layer := intent.layer
		layer.quantity += trade.Size
		layer.cost += trade.Size * trade.Price
		layer.openingFee += trade.Size * trade.Price * s.FeeRate
		layer.price = layer.cost / layer.quantity
		s.layers = append(s.layers, layer)
		return
	}
	if trade.Side != "sell" {
		return
	}
	if trade.Size >= intent.order.Size-1e-10 {
		delete(s.working, trade.OrderID)
	} else {
		intent.order.Size -= trade.Size
	}
	if intent.closeLast >= 0 {
		for i, layer := range s.layers {
			if layer.index == intent.closeLast {
				s.reduceLayer(i, trade.Size)
				break
			}
		}
	} else {
		quantity, _, _ := s.inventory()
		if quantity > 0 {
			remaining := trade.Size
			for i := len(s.layers) - 1; i >= 0 && remaining > 0; i-- {
				closed := math.Min(s.layers[i].quantity, remaining)
				s.reduceLayer(i, closed)
				remaining -= closed
			}
		}
	}
	if len(s.layers) == 0 {
		s.highestProfit, s.takeProfitTriggered = 0, false
	}
}

func (s *DCAEnhancedBacktestStrategy) reduceLayer(index int, quantity float64) {
	if index < 0 || index >= len(s.layers) || quantity <= 0 {
		return
	}
	layer := s.layers[index]
	if quantity >= layer.quantity-1e-10 {
		s.layers = append(s.layers[:index], s.layers[index+1:]...)
		return
	}
	remainingRatio := (layer.quantity - quantity) / layer.quantity
	layer.quantity -= quantity
	layer.cost *= remainingRatio
	layer.openingFee *= remainingRatio
}

func (s *DCAEnhancedBacktestStrategy) GetName() string { return s.Name }
func (s *DCAEnhancedBacktestStrategy) GetType() string { return "dca_enhanced" }
func (s *DCAEnhancedBacktestStrategy) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"base_order_amount": s.BaseOrderAmount, "safety_order_amount": s.SafetyOrderAmount,
		"max_safety_orders": s.MaxSafetyOrders, "atr_period": s.ATRPeriod, "atr_multiplier": s.ATRMultiplier,
		"min_price_step": s.MinPriceStep, "max_price_step": s.MaxPriceStep,
		"safety_order_scale": s.SafetyOrderScale, "safety_order_step": s.SafetyOrderStep,
		"first_order_take_profit": s.FirstOrderTakeProfit, "last_order_take_profit": s.LastOrderTakeProfit,
		"total_take_profit": s.TotalTakeProfit, "trailing_take_profit": s.TrailingTakeProfit,
		"trailing_activation": s.TrailingActivation, "stop_loss": s.StopLoss,
		"cascade_protection": s.CascadeProtection, "cascade_drop_threshold": s.CascadeDropThreshold,
		"cascade_pause_duration": s.CascadePauseDuration, "trend_filter_enabled": s.TrendFilterEnabled,
		"trend_method": s.TrendMethod, "trend_period": s.TrendPeriod,
		"fee_rate": s.FeeRate, "sell_slippage": s.SellSlippage, "total_capital": s.TotalCapital,
	}
}
