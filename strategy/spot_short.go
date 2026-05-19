package strategy

import (
	"context"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/position"
)

// SpotShortStrategy 現貨借幣做空策略
// 訂閱 HedgeCoordinator 發送的 EventTypeHedgeSignal，根據目標空倉執行借幣/賣出或買回/還幣
type SpotShortStrategy struct {
	name       string
	cfg        *config.Config
	executor   position.OrderExecutorInterface
	ex         position.IExchange
	rawEx      exchange.IExchange           // 用於 Borrow/Repay 類型斷言
	smEx       exchange.ISpotMarginExchange // 現貨槓桿交易所（借還）
	groupID    string
	symbol     string
	baseAsset  string
	quoteAsset string

	eventBus        EventBus
	subscribableBus interface{ Subscribe() <-chan *event.Event }

	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
	pendingRepay map[int64]float64 // orderID -> 待還數量

	positions []*Position
	orders    []*Order
	stats     *StrategyStatistics
}

// NewSpotShortStrategy 創建現貨做空策略
// ex 為 position.IExchange（適配器），rawEx 為原始 exchange.IExchange（用於 Borrow/Repay，可為 nil）
func NewSpotShortStrategy(name string, cfg *config.Config, executor position.OrderExecutorInterface, ex position.IExchange, rawEx exchange.IExchange, strategyCfg map[string]interface{}) *SpotShortStrategy {
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
		quoteAsset = ex.GetQuoteAsset()
		if quoteAsset == "" {
			quoteAsset = "USDT"
		}
	}
	var smEx exchange.ISpotMarginExchange
	if rawEx != nil {
		smEx, _ = rawEx.(exchange.ISpotMarginExchange)
	}
	return &SpotShortStrategy{
		name:       name,
		cfg:        cfg,
		executor:   executor,
		ex:         ex,
		rawEx:      rawEx,
		smEx:       smEx,
		groupID:    groupID,
		symbol:     symbol,
		baseAsset:  baseAsset,
		quoteAsset: quoteAsset,
		positions:  []*Position{},
		orders:     []*Order{},
		stats:      &StrategyStatistics{},
	}
}

func (s *SpotShortStrategy) Name() string { return s.name }

func (s *SpotShortStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, ex position.IExchange) error {
	s.ex = ex
	return nil
}

func (s *SpotShortStrategy) SetEventBus(bus EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = bus
	if sub, ok := bus.(interface{ Subscribe() <-chan *event.Event }); ok {
		s.subscribableBus = sub
	}
}

func (s *SpotShortStrategy) OnPriceChange(price float64) error { return nil }

func (s *SpotShortStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	if update == nil || update.Status != "FILLED" || update.Side != "BUY" {
		return nil
	}
	s.mu.Lock()
	amount, ok := s.pendingRepay[update.OrderID]
	s.mu.Unlock()
	if !ok || amount <= 0 {
		return nil
	}
	if s.smEx == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := s.smEx.Repay(ctx, s.baseAsset, amount); err != nil {
		logger.Error("SpotShortStrategy 還幣失敗 (order=%d): %v", update.OrderID, err)
		return nil
	}
	s.mu.Lock()
	delete(s.pendingRepay, update.OrderID)
	s.mu.Unlock()
	logger.Info("📤 SpotShortStrategy: 買回成交後已還幣 %.6f %s", amount, s.baseAsset)
	return nil
}

func (s *SpotShortStrategy) GetPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*Position(nil), s.positions...)
}

func (s *SpotShortStrategy) GetOrders() []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]*Order(nil), s.orders...)
}

func (s *SpotShortStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

func (s *SpotShortStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.subscribableBus == nil {
		s.mu.Unlock()
		logger.Warn("SpotShortStrategy: 無可訂閱的 EventBus，跳過啟動")
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
	logger.Info("✅ SpotShortStrategy 已啟動 (group=%s)", s.groupID)
	return nil
}

func (s *SpotShortStrategy) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *SpotShortStrategy) GetVisualizationData() map[string]interface{} {
	return nil
}

func (s *SpotShortStrategy) onHedgeSignal(evt *event.Event) {
	evtGroupID := getString(evt.Data, "group_id")
	if evtGroupID != "" && evtGroupID != s.groupID {
		return
	}
	evtSymbol := getString(evt.Data, "symbol")
	if evtSymbol != "" && evtSymbol != s.symbol {
		return
	}
	targetShort := getFloat64(evt.Data, "target_spot_short")
	if targetShort < 0 {
		targetShort = 0
	}
	if s.smEx == nil {
		logger.Warn("SpotShortStrategy: 交易所不支援借幣做空，跳過")
		return
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

func (s *SpotShortStrategy) getCurrentShortPosition(ctx context.Context) float64 {
	raw, err := s.ex.GetPositions(ctx, s.symbol)
	if err != nil || raw == nil {
		return 0
	}
	// positionExchangeAdapter 返回 []*position.PositionInfo
	if infos, ok := raw.([]*position.PositionInfo); ok {
		for _, p := range infos {
			if p != nil && p.Symbol == s.symbol && p.Size < 0 {
				return -p.Size
			}
		}
	}
	return 0
}

func (s *SpotShortStrategy) increaseShort(ctx context.Context, amount float64) {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return
	}
	if _, err := s.smEx.Borrow(ctx, s.baseAsset, amount); err != nil {
		logger.Error("SpotShortStrategy 借幣失敗: %v", err)
		return
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		logger.Error("SpotShortStrategy 獲取價格失敗: %v", err)
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
	}
	if _, err := s.executor.PlaceOrder(req); err != nil {
		logger.Error("SpotShortStrategy 賣出失敗: %v", err)
		return
	}
	logger.Info("📤 SpotShortStrategy: 借幣 %.6f %s 並賣出", amount, s.baseAsset)
}

func (s *SpotShortStrategy) decreaseShort(ctx context.Context, amount float64) {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		logger.Error("SpotShortStrategy 獲取價格失敗: %v", err)
		return
	}
	// 限價買單略高於市價以提高成交率
	price = s.roundPrice(price * 1.001)
	req := &position.OrderRequest{
		Symbol:        s.symbol,
		Side:          "BUY",
		Price:         price,
		Quantity:      amount,
		PriceDecimals: s.getPriceDecimals(),
		PostOnly:      true,
	}
	ord, err := s.executor.PlaceOrder(req)
	if err != nil {
		logger.Error("SpotShortStrategy 買回失敗: %v", err)
		return
	}
	s.mu.Lock()
	if s.pendingRepay == nil {
		s.pendingRepay = make(map[int64]float64)
	}
	s.pendingRepay[ord.OrderID] = amount
	s.mu.Unlock()
	logger.Info("📥 SpotShortStrategy: 已下買回單 %.6f %s (order=%d)，成交後還幣", amount, s.baseAsset, ord.OrderID)
}

func (s *SpotShortStrategy) roundQuantity(qty float64) float64 {
	decimals := s.ex.GetQuantityDecimals()
	if decimals <= 0 {
		decimals = 6
	}
	factor := math.Pow10(decimals)
	return math.Floor(qty*factor+0.5) / factor
}

func (s *SpotShortStrategy) roundPrice(price float64) float64 {
	decimals := s.ex.GetPriceDecimals()
	if decimals <= 0 {
		decimals = 2
	}
	factor := math.Pow10(decimals)
	return math.Floor(price*factor+0.5) / factor
}

func (s *SpotShortStrategy) getPriceDecimals() int {
	if s.ex != nil {
		return s.ex.GetPriceDecimals()
	}
	return 2
}
