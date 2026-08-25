package strategy

import (
	"context"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/utils"
)

// SpotLongStrategy 現貨做多對沖策略
// 訂閱 HedgeCoordinator 發送的 EventTypeHedgeSignal（target_spot_long），根據目標多倉買入或賣出現貨
// 用於做空網格的對沖：合約做空時，現貨持有多倉可對沖價格上漲風險
type SpotLongStrategy struct {
	name       string
	cfg        *config.Config
	executor   position.OrderExecutorInterface
	ex         position.IExchange
	groupID    string
	symbol     string
	baseAsset  string
	quoteAsset string

	eventBus        EventBus
	subscribableBus interface{ Subscribe() <-chan *event.Event }

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex

	positions []*Position
	orders    []*Order
	stats     *StrategyStatistics
}

// NewSpotLongStrategy 創建現貨做多對沖策略
func NewSpotLongStrategy(name string, cfg *config.Config, executor position.OrderExecutorInterface, ex position.IExchange, strategyCfg map[string]interface{}) *SpotLongStrategy {
	groupID := ""
	if g, ok := strategyCfg["group_id"].(string); ok {
		groupID = g
	}
	symbol := cfg.Trading.Symbol
	if s, ok := strategyCfg["symbol"].(string); ok && s != "" {
		symbol = s
	}
	baseAsset := "BTC"
	quoteAsset := "USDT"
	if ex != nil {
		baseAsset = ex.GetBaseAsset()
	}
	return &SpotLongStrategy{
		name:       name,
		cfg:        cfg,
		executor:   executor,
		ex:         ex,
		groupID:    groupID,
		symbol:     symbol,
		baseAsset:  baseAsset,
		quoteAsset: quoteAsset,
		positions:  []*Position{},
		orders:     []*Order{},
		stats:      &StrategyStatistics{},
	}
}

func (s *SpotLongStrategy) Name() string { return s.name }

func (s *SpotLongStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, ex position.IExchange) error {
	s.ex = ex
	return nil
}

func (s *SpotLongStrategy) SetEventBus(bus EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = bus
	if sub, ok := bus.(interface{ Subscribe() <-chan *event.Event }); ok {
		s.subscribableBus = sub
	}
}

func (s *SpotLongStrategy) OnPriceChange(price float64) error { return nil }

func (s *SpotLongStrategy) OnOrderUpdate(update *position.OrderUpdate) error { return nil }

func (s *SpotLongStrategy) GetPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*Position(nil), s.positions...)
}

func (s *SpotLongStrategy) GetOrders() []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*Order(nil), s.orders...)
}

func (s *SpotLongStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

func (s *SpotLongStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.subscribableBus == nil {
		s.mu.Unlock()
		logger.Warn("SpotLongStrategy: 無可訂閱的 EventBus，跳過啟動")
		return nil
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	subCh := s.subscribableBus.Subscribe()
	s.mu.Unlock()

	go func() {
		for {
			select {
			case <-s.ctx.Done():
				return
			case evt, ok := <-subCh:
				if !ok {
					return
				}
				if evt.Type == event.EventTypeHedgeSignal {
					s.onHedgeSignal(evt)
				}
			}
		}
	}()
	logger.Info("✅ SpotLongStrategy 已啟動 (group=%s)", s.groupID)
	return nil
}

func (s *SpotLongStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *SpotLongStrategy) GetVisualizationData() map[string]interface{} {
	return nil
}

func (s *SpotLongStrategy) onHedgeSignal(evt *event.Event) {
	evtGroupID := getString(evt.Data, "group_id")
	if evtGroupID != "" && evtGroupID != s.groupID {
		return
	}
	evtSymbol := getString(evt.Data, "symbol")
	if evtSymbol != "" && evtSymbol != s.symbol {
		return
	}
	targetLong := getFloat64(evt.Data, "target_spot_long")
	if targetLong < 0 {
		targetLong = 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	currentLong := s.getCurrentLongPosition(ctx)
	diff := targetLong - currentLong

	if math.Abs(diff) < 0.000001 {
		return
	}

	if diff > 0 {
		s.increaseLong(ctx, diff)
	} else {
		s.decreaseLong(ctx, -diff)
	}
}

func (s *SpotLongStrategy) getCurrentLongPosition(ctx context.Context) float64 {
	raw, err := s.ex.GetPositions(ctx, s.symbol)
	if err != nil || raw == nil {
		return 0
	}
	if infos, ok := raw.([]*position.PositionInfo); ok {
		for _, p := range infos {
			if p != nil && p.Symbol == s.symbol && p.Size > 0 {
				return p.Size
			}
		}
	}
	return 0
}

func (s *SpotLongStrategy) increaseLong(ctx context.Context, amount float64) {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		logger.Error("SpotLongStrategy 獲取價格失敗: %v", err)
		return
	}
	price = s.roundPrice(price)
	req := &position.OrderRequest{
		Symbol:        s.symbol,
		Side:          "BUY",
		Price:         price,
		Quantity:      amount,
		PriceDecimals: s.getPriceDecimals(),
		PostOnly:      true,
	}
	if _, err := s.executor.PlaceOrder(req); err != nil {
		logger.Error("SpotLongStrategy 買入失敗: %v", err)
		return
	}
	logger.Info("📥 SpotLongStrategy: 買入 %.6f %s 增加多倉", amount, s.baseAsset)
}

func (s *SpotLongStrategy) decreaseLong(ctx context.Context, amount float64) {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		logger.Error("SpotLongStrategy 獲取價格失敗: %v", err)
		return
	}
	price = s.roundPrice(price * 0.999)
	req := &position.OrderRequest{
		Symbol:        s.symbol,
		Side:          "SELL",
		Price:         price,
		Quantity:      amount,
		PriceDecimals: s.getPriceDecimals(),
		PostOnly:      true,
	}
	if _, err := s.executor.PlaceOrder(req); err != nil {
		logger.Error("SpotLongStrategy 賣出失敗: %v", err)
		return
	}
	logger.Info("📤 SpotLongStrategy: 賣出 %.6f %s 減少多倉", amount, s.baseAsset)
}

// roundQuantity 將數量向下取整到交易所精度。
// 現貨賣出沒有 ReduceOnly 兜底，向上取整會直接超出持有量被拒單。
func (s *SpotLongStrategy) roundQuantity(qty float64) float64 {
	decimals := s.ex.GetQuantityDecimals()
	if decimals <= 0 {
		decimals = 6
	}
	return utils.FloorToDecimals(qty, decimals)
}

func (s *SpotLongStrategy) roundPrice(price float64) float64 {
	decimals := s.ex.GetPriceDecimals()
	if decimals <= 0 {
		decimals = 2
	}
	return utils.RoundToDecimals(price, decimals)
}

func (s *SpotLongStrategy) getPriceDecimals() int {
	if s.ex != nil {
		return s.ex.GetPriceDecimals()
	}
	return 2
}
