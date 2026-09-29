package strategy

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
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

	ctx                      context.Context
	cancel                   context.CancelFunc
	mu                       sync.RWMutex
	pendingOrders            map[int64]spotLongPendingOrder
	runtimeStateStore        RuntimeStateStore
	runtimeStateErrorHandler func(error)

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
		name:          name,
		cfg:           cfg,
		executor:      executor,
		ex:            ex,
		groupID:       groupID,
		symbol:        symbol,
		baseAsset:     baseAsset,
		quoteAsset:    quoteAsset,
		positions:     []*Position{},
		orders:        []*Order{},
		stats:         &StrategyStatistics{},
		pendingOrders: make(map[int64]spotLongPendingOrder),
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

func (s *SpotLongStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeStateStore = store
}

func (s *SpotLongStrategy) SetRuntimeStateErrorHandler(handler func(error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeStateErrorHandler = handler
}

func (s *SpotLongStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	if update == nil {
		return nil
	}
	s.mu.Lock()
	pending, ok := s.pendingOrders[update.OrderID]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if (update.Side != "" && update.Side != pending.Side) || (update.Symbol != "" && update.Symbol != s.symbol) {
		s.mu.Unlock()
		return fmt.Errorf("spot long order update identity mismatch for order %d", update.OrderID)
	}
	if update.ExecutedQty < pending.ExecutedQty || update.ExecutedQty > pending.Quantity || math.IsNaN(update.ExecutedQty) || math.IsInf(update.ExecutedQty, 0) {
		s.mu.Unlock()
		return fmt.Errorf("invalid cumulative fill for spot long order %d: %.12g (previous %.12g, requested %.12g)", update.OrderID, update.ExecutedQty, pending.ExecutedQty, pending.Quantity)
	}
	pending.ExecutedQty = update.ExecutedQty
	if update.Status == "FILLED" || update.Status == "CANCELED" || update.Status == "CANCELLED" || update.Status == "EXPIRED" || update.Status == "REJECTED" {
		delete(s.pendingOrders, update.OrderID)
	} else {
		s.pendingOrders[update.OrderID] = pending
	}
	err := s.persistRuntimeStateLocked()
	if err != nil {
		s.pendingOrders[update.OrderID] = pending
	}
	s.mu.Unlock()
	return err
}

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
	if err := s.restoreRuntimeStateLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	if err := s.reconcilePendingOrders(ctx); err != nil {
		return err
	}
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

func (s *SpotLongStrategy) reconcilePendingOrders(ctx context.Context) error {
	s.mu.RLock()
	ids := make([]int64, 0, len(s.pendingOrders))
	for id := range s.pendingOrders {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	if len(ids) == 0 {
		return nil
	}
	if s.ex == nil {
		return fmt.Errorf("spot long exchange unavailable while reconciling %d persisted orders", len(ids))
	}
	for _, id := range ids {
		result, err := s.ex.GetOrder(ctx, s.symbol, id)
		if err != nil {
			return fmt.Errorf("reconcile spot long order %d: %w", id, err)
		}
		update, err := strategyOrderUpdateFromExchange(result)
		if err != nil {
			return fmt.Errorf("reconcile spot long order %d: %w", id, err)
		}
		if update.OrderID == 0 {
			update.OrderID = id
		}
		if update.Symbol == "" {
			update.Symbol = s.symbol
		}
		if err := s.OnOrderUpdate(update); err != nil {
			return fmt.Errorf("apply reconciled spot long order %d: %w", id, err)
		}
	}
	return nil
}

func strategyOrderUpdateFromExchange(raw interface{}) (*position.OrderUpdate, error) {
	switch order := raw.(type) {
	case *exchange.Order:
		if order == nil {
			return nil, fmt.Errorf("exchange returned nil order")
		}
		return &position.OrderUpdate{OrderID: order.OrderID, ClientOrderID: order.ClientOrderID, Symbol: order.Symbol,
			Status: string(order.Status), ExecutedQty: order.ExecutedQty, AvgPrice: order.AvgPrice, Side: string(order.Side), UpdateTime: order.UpdateTime}, nil
	case *position.Order:
		if order == nil {
			return nil, fmt.Errorf("exchange returned nil order")
		}
		return &position.OrderUpdate{OrderID: order.OrderID, ClientOrderID: order.ClientOrderID, Symbol: order.Symbol,
			Status: order.Status, ExecutedQty: order.ExecutedQty, AvgPrice: order.AvgPrice, Side: order.Side}, nil
	default:
		return nil, fmt.Errorf("unsupported exchange order response %T", raw)
	}
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

	s.mu.RLock()
	hasPendingOrders := len(s.pendingOrders) > 0
	s.mu.RUnlock()
	if hasPendingOrders {
		return
	}
	currentLong, err := s.getCurrentLongPosition(ctx)
	if err != nil {
		logger.Error("SpotLongStrategy 读取现货持仓失败，已跳过本次对冲决策: %v", err)
		return
	}
	diff := targetLong - currentLong

	if math.Abs(diff) < 0.000001 {
		return
	}

	if diff > 0 {
		if err := s.increaseLong(ctx, diff); err != nil {
			logger.Error("SpotLongStrategy 買入/持久化失敗: %v", err)
		}
	} else {
		if err := s.decreaseLong(ctx, -diff); err != nil {
			logger.Error("SpotLongStrategy 賣出/持久化失敗: %v", err)
		}
	}
}

func (s *SpotLongStrategy) getCurrentLongPosition(ctx context.Context) (float64, error) {
	if s.ex == nil {
		return 0, fmt.Errorf("spot long exchange is unavailable")
	}
	raw, err := s.ex.GetPositions(ctx, s.symbol)
	if err != nil {
		return 0, fmt.Errorf("get positions for %s: %w", s.symbol, err)
	}
	if raw == nil {
		return 0, fmt.Errorf("get positions for %s returned no data", s.symbol)
	}
	if infos, ok := raw.([]*position.PositionInfo); ok {
		var current float64
		found := false
		for _, p := range infos {
			if p == nil {
				return 0, fmt.Errorf("get positions for %s returned a nil position entry", s.symbol)
			}
			if p.Symbol != s.symbol {
				continue
			}
			if found {
				return 0, fmt.Errorf("get positions for %s returned duplicate symbol entries", s.symbol)
			}
			if !finiteNumber(p.Size) || p.Size < 0 {
				return 0, fmt.Errorf("get positions for %s returned invalid long inventory %v", s.symbol, p.Size)
			}
			found = true
			current = p.Size
		}
		return current, nil
	}
	return 0, fmt.Errorf("unsupported position response type %T for %s", raw, s.symbol)
}

func (s *SpotLongStrategy) increaseLong(ctx context.Context, amount float64) error {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return nil
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		logger.Error("SpotLongStrategy 獲取價格失敗: %v", err)
		return fmt.Errorf("get price for spot long buy %s: %w", s.symbol, err)
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
	ord, err := s.executor.PlaceOrder(req)
	if err != nil {
		return fmt.Errorf("place spot long buy %s: %w", s.symbol, err)
	}
	if ord == nil || ord.OrderID <= 0 {
		return fmt.Errorf("spot long buy %s returned invalid order identity", s.symbol)
	}
	s.mu.Lock()
	s.pendingOrders[ord.OrderID] = spotLongPendingOrder{Side: "BUY", Quantity: amount}
	err = s.persistRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	logger.Info("📥 SpotLongStrategy: 買入 %.6f %s 增加多倉", amount, s.baseAsset)
	return nil
}

func (s *SpotLongStrategy) decreaseLong(ctx context.Context, amount float64) error {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return nil
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		logger.Error("SpotLongStrategy 獲取價格失敗: %v", err)
		return fmt.Errorf("get price for spot long sell %s: %w", s.symbol, err)
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
	ord, err := s.executor.PlaceOrder(req)
	if err != nil {
		return fmt.Errorf("place spot long sell %s: %w", s.symbol, err)
	}
	if ord == nil || ord.OrderID <= 0 {
		return fmt.Errorf("spot long sell %s returned invalid order identity", s.symbol)
	}
	s.mu.Lock()
	s.pendingOrders[ord.OrderID] = spotLongPendingOrder{Side: "SELL", Quantity: amount}
	err = s.persistRuntimeStateLocked()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	logger.Info("📤 SpotLongStrategy: 賣出 %.6f %s 減少多倉", amount, s.baseAsset)
	return nil
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
