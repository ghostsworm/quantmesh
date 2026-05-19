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
)

// FuturesShortStrategy 合約做空對沖策略
// 訂閱 HedgeCoordinator 發送的 EventTypeHedgeSignal（target_futures_short），根據目標空倉開倉或平倉
// 用於現貨網格對沖：現貨網格做多時，合約持空倉可對沖下跌風險
type FuturesShortStrategy struct {
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

// NewFuturesShortStrategy 創建合約做空對沖策略
func NewFuturesShortStrategy(name string, cfg *config.Config, executor position.OrderExecutorInterface, ex position.IExchange, strategyCfg map[string]interface{}) *FuturesShortStrategy {
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
	return &FuturesShortStrategy{
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

func (s *FuturesShortStrategy) Name() string { return s.name }

func (s *FuturesShortStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, ex position.IExchange) error {
	s.ex = ex
	return nil
}

func (s *FuturesShortStrategy) SetEventBus(bus EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = bus
	if sub, ok := bus.(interface{ Subscribe() <-chan *event.Event }); ok {
		s.subscribableBus = sub
	}
}

func (s *FuturesShortStrategy) OnPriceChange(price float64) error { return nil }

func (s *FuturesShortStrategy) OnOrderUpdate(update *position.OrderUpdate) error { return nil }

func (s *FuturesShortStrategy) GetPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*Position(nil), s.positions...)
}

func (s *FuturesShortStrategy) GetOrders() []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*Order(nil), s.orders...)
}

func (s *FuturesShortStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

func (s *FuturesShortStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.subscribableBus == nil {
		s.mu.Unlock()
		logger.Warn("FuturesShortStrategy: 無可訂閱的 EventBus，跳過啟動")
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
	logger.Info("✅ FuturesShortStrategy 已啟動 (group=%s)", s.groupID)
	return nil
}

func (s *FuturesShortStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *FuturesShortStrategy) GetVisualizationData() map[string]interface{} {
	return nil
}

func (s *FuturesShortStrategy) onHedgeSignal(evt *event.Event) {
	evtGroupID := getString(evt.Data, "group_id")
	if evtGroupID != "" && evtGroupID != s.groupID {
		return
	}
	evtSymbol := getString(evt.Data, "symbol")
	if evtSymbol != "" && evtSymbol != s.symbol {
		return
	}
	targetShort := getFloat64(evt.Data, "target_futures_short")
	if targetShort < 0 {
		targetShort = 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	currentShort := s.getCurrentShortPosition(ctx)
	diff := targetShort - currentShort

	if math.Abs(diff) < 0.000001 {
		return
	}

	if diff > 0 {
		s.increaseShort(ctx, diff)
	} else {
		s.decreaseShort(ctx, -diff)
	}
}

func (s *FuturesShortStrategy) getCurrentShortPosition(ctx context.Context) float64 {
	raw, err := s.ex.GetPositions(ctx, s.symbol)
	if err != nil || raw == nil {
		return 0
	}
	if infos, ok := raw.([]*position.PositionInfo); ok {
		for _, p := range infos {
			if p != nil && p.Symbol == s.symbol && p.Size < 0 {
				return -p.Size
			}
		}
	}
	return 0
}

func (s *FuturesShortStrategy) increaseShort(ctx context.Context, amount float64) {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		logger.Error("FuturesShortStrategy 獲取價格失敗: %v", err)
		return
	}
	price = s.roundPrice(price)
	req := &position.OrderRequest{
		Symbol:        s.symbol,
		Side:          "SELL",
		Price:         price,
		Quantity:      amount,
		PriceDecimals: s.getPriceDecimals(),
		PostOnly:      true,
		ReduceOnly:    false,
	}
	if _, err := s.executor.PlaceOrder(req); err != nil {
		logger.Error("FuturesShortStrategy 開空失敗: %v", err)
		return
	}
	logger.Info("📤 FuturesShortStrategy: 開空 %.6f %s", amount, s.baseAsset)
}

func (s *FuturesShortStrategy) decreaseShort(ctx context.Context, amount float64) {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		logger.Error("FuturesShortStrategy 獲取價格失敗: %v", err)
		return
	}
	price = s.roundPrice(price * 1.001)
	req := &position.OrderRequest{
		Symbol:        s.symbol,
		Side:          "BUY",
		Price:         price,
		Quantity:      amount,
		PriceDecimals: s.getPriceDecimals(),
		PostOnly:      true,
		ReduceOnly:    true,
	}
	if _, err := s.executor.PlaceOrder(req); err != nil {
		logger.Error("FuturesShortStrategy 平空失敗: %v", err)
		return
	}
	logger.Info("📥 FuturesShortStrategy: 平空 %.6f %s", amount, s.baseAsset)
}

func (s *FuturesShortStrategy) roundQuantity(qty float64) float64 {
	decimals := s.ex.GetQuantityDecimals()
	if decimals <= 0 {
		decimals = 6
	}
	factor := math.Pow10(decimals)
	return math.Floor(qty*factor+0.5) / factor
}

func (s *FuturesShortStrategy) roundPrice(price float64) float64 {
	decimals := s.ex.GetPriceDecimals()
	if decimals <= 0 {
		decimals = 2
	}
	factor := math.Pow10(decimals)
	return math.Floor(price*factor+0.5) / factor
}

func (s *FuturesShortStrategy) getPriceDecimals() int {
	if s.ex != nil {
		return s.ex.GetPriceDecimals()
	}
	return 2
}
