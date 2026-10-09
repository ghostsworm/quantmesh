package strategy

import (
	"context"
	"fmt"
	"math"
	"strings"
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
	tradeMu                  sync.Mutex
	pendingOrders            map[int64]spotLongPendingOrder
	pendingIntents           map[string]spotLongPendingIntent
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
		name:           name,
		cfg:            cfg,
		executor:       executor,
		ex:             ex,
		groupID:        groupID,
		symbol:         symbol,
		baseAsset:      baseAsset,
		quoteAsset:     quoteAsset,
		positions:      []*Position{},
		orders:         []*Order{},
		stats:          &StrategyStatistics{},
		pendingOrders:  make(map[int64]spotLongPendingOrder),
		pendingIntents: make(map[string]spotLongPendingIntent),
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
	if !ok && update.ClientOrderID != "" && update.OrderID > 0 {
		if intent, found := s.pendingIntents[update.ClientOrderID]; found {
			if (update.Side != "" && update.Side != intent.Side) || (update.Symbol != "" && update.Symbol != s.symbol) ||
				!finiteNumber(update.ExecutedQty) || update.ExecutedQty < 0 || update.ExecutedQty > intent.Quantity {
				s.mu.Unlock()
				return fmt.Errorf("spot long order update identity/quantity mismatch for client order %s", update.ClientOrderID)
			}
			delete(s.pendingIntents, update.ClientOrderID)
			pending = spotLongPendingOrder{ClientOrderID: update.ClientOrderID, Side: intent.Side, Quantity: intent.Quantity}
			s.pendingOrders[update.OrderID] = pending
			if err := s.persistRuntimeStateLocked(); err != nil {
				delete(s.pendingOrders, update.OrderID)
				s.pendingIntents[update.ClientOrderID] = intent
				s.mu.Unlock()
				return err
			}
			ok = true
		}
	}
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if (update.Side != "" && update.Side != pending.Side) || (update.Symbol != "" && update.Symbol != s.symbol) ||
		(pending.ClientOrderID != "" && update.ClientOrderID != "" && pending.ClientOrderID != update.ClientOrderID) {
		s.mu.Unlock()
		return fmt.Errorf("spot long order update identity mismatch for order %d", update.OrderID)
	}
	if update.ExecutedQty < pending.ExecutedQty || update.ExecutedQty > pending.Quantity || math.IsNaN(update.ExecutedQty) || math.IsInf(update.ExecutedQty, 0) {
		s.mu.Unlock()
		return fmt.Errorf("invalid cumulative fill for spot long order %d: %.12g (previous %.12g, requested %.12g)", update.OrderID, update.ExecutedQty, pending.ExecutedQty, pending.Quantity)
	}
	if strings.EqualFold(update.Status, "FILLED") && update.ExecutedQty <= 0 {
		s.mu.Unlock()
		return fmt.Errorf("spot long order %d reports FILLED without positive cumulative execution", update.OrderID)
	}
	if strings.EqualFold(update.Status, "FILLED") && !spotLongFillMatchesRequested(update.ExecutedQty, pending.Quantity) {
		s.mu.Unlock()
		return fmt.Errorf("spot long order %d reports FILLED below its requested quantity", update.OrderID)
	}
	pending.ExecutedQty = update.ExecutedQty
	if strings.EqualFold(update.Status, "FILLED") || strings.EqualFold(update.Status, "CANCELED") ||
		strings.EqualFold(update.Status, "CANCELLED") || strings.EqualFold(update.Status, "EXPIRED") || strings.EqualFold(update.Status, "REJECTED") {
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

func spotLongFillMatchesRequested(executedQty, requestedQty float64) bool {
	if !finiteNumber(executedQty) || !finiteNumber(requestedQty) || executedQty < 0 || requestedQty <= 0 {
		return false
	}
	return executedQty+math.Max(1e-10, math.Abs(requestedQty)*1e-8) >= requestedQty
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
	if err := s.reconcilePendingIntents(ctx); err != nil {
		return err
	}
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
		s.mu.RLock()
		pending := s.pendingOrders[id]
		s.mu.RUnlock()
		if err := validateSpotLongOrderSnapshot(result, id, pending, s.symbol); err != nil {
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

func validateSpotLongOrderSnapshot(raw interface{}, orderID int64, pending spotLongPendingOrder, symbol string) error {
	var returnedID int64
	var clientOrderID, returnedSymbol, side string
	var quantity float64
	switch order := raw.(type) {
	case *exchange.Order:
		if order == nil {
			return fmt.Errorf("exchange returned nil order")
		}
		returnedID, clientOrderID, returnedSymbol, side, quantity = order.OrderID, order.ClientOrderID, order.Symbol, string(order.Side), order.Quantity
	case *position.Order:
		if order == nil {
			return fmt.Errorf("exchange returned nil order")
		}
		returnedID, clientOrderID, returnedSymbol, side, quantity = order.OrderID, order.ClientOrderID, order.Symbol, order.Side, order.Quantity
	default:
		return fmt.Errorf("unsupported exchange order response %T", raw)
	}
	tolerance := math.Max(1e-10, pending.Quantity*1e-8)
	if returnedID != orderID || side != pending.Side || (returnedSymbol != "" && returnedSymbol != symbol) ||
		(pending.ClientOrderID != "" && clientOrderID != pending.ClientOrderID) || !finiteNumber(quantity) || math.Abs(quantity-pending.Quantity) > tolerance {
		return fmt.Errorf("exchange order identity or quantity does not match persisted spot long order")
	}
	return nil
}

func (s *SpotLongStrategy) reconcilePendingIntents(ctx context.Context) error {
	s.mu.RLock()
	intents := make(map[string]spotLongPendingIntent, len(s.pendingIntents))
	for clientOrderID, intent := range s.pendingIntents {
		intents[clientOrderID] = intent
	}
	s.mu.RUnlock()
	if len(intents) == 0 {
		return nil
	}
	query, ok := s.ex.(exchange.OrderByClientIDQuerier)
	if !ok {
		return fmt.Errorf("spot long has %d unresolved order intent(s), but exchange cannot query exact client order ids", len(intents))
	}
	for clientOrderID, intent := range intents {
		order, err := query.GetOrderByClientOrderID(ctx, s.symbol, clientOrderID)
		if err != nil {
			return fmt.Errorf("query spot long order by client id %s: %w", clientOrderID, err)
		}
		if order == nil {
			return fmt.Errorf("spot long order %s is not verifiable; refusing startup", clientOrderID)
		}
		tolerance := math.Max(1e-10, intent.Quantity*1e-8)
		if order.OrderID <= 0 || order.ClientOrderID != clientOrderID || order.Symbol != s.symbol || string(order.Side) != intent.Side ||
			!finiteNumber(order.Quantity) || math.Abs(order.Quantity-intent.Quantity) > tolerance {
			return fmt.Errorf("exchange order identity/quantity mismatch while reconciling spot long client order %s", clientOrderID)
		}
		s.mu.Lock()
		if current, found := s.pendingIntents[clientOrderID]; found {
			delete(s.pendingIntents, clientOrderID)
			s.pendingOrders[order.OrderID] = spotLongPendingOrder{ClientOrderID: clientOrderID, Side: current.Side, Quantity: current.Quantity}
			if err := s.persistRuntimeStateLocked(); err != nil {
				delete(s.pendingOrders, order.OrderID)
				s.pendingIntents[clientOrderID] = current
				s.mu.Unlock()
				return fmt.Errorf("persist reconciled spot long order %d: %w", order.OrderID, err)
			}
		}
		s.mu.Unlock()
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
	hasPendingOrders := len(s.pendingOrders) > 0 || len(s.pendingIntents) > 0
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
	s.tradeMu.Lock()
	defer s.tradeMu.Unlock()
	return s.placeSpotLongOrder(ctx, "BUY", amount)
}

func (s *SpotLongStrategy) decreaseLong(ctx context.Context, amount float64) error {
	s.tradeMu.Lock()
	defer s.tradeMu.Unlock()
	return s.placeSpotLongOrder(ctx, "SELL", amount)
}

func (s *SpotLongStrategy) placeSpotLongOrder(ctx context.Context, side string, amount float64) error {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return nil
	}
	s.mu.Lock()
	if len(s.pendingOrders) > 0 || len(s.pendingIntents) > 0 {
		s.mu.Unlock()
		return fmt.Errorf("spot long has an unresolved order; refusing another order")
	}
	s.mu.Unlock()
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		logger.Error("SpotLongStrategy 獲取價格失敗: %v", err)
		return fmt.Errorf("get price for spot long %s %s: %w", side, s.symbol, err)
	}
	if side == "BUY" {
		price = s.roundPrice(price)
	} else {
		price = s.roundPrice(price * 0.999)
	}
	clientOrderID := utils.GenerateOrderID(price, side, s.getPriceDecimals())
	intent := spotLongPendingIntent{Side: side, Quantity: amount, CreatedAtUnixMilli: time.Now().UTC().UnixMilli()}
	s.mu.Lock()
	if len(s.pendingOrders) > 0 || len(s.pendingIntents) > 0 {
		s.mu.Unlock()
		return fmt.Errorf("spot long has an unresolved order; refusing another order")
	}
	s.pendingIntents[clientOrderID] = intent
	if err := s.persistRuntimeStateLocked(); err != nil {
		delete(s.pendingIntents, clientOrderID)
		s.mu.Unlock()
		return fmt.Errorf("persist spot long %s intent before submission: %w", side, err)
	}
	s.mu.Unlock()
	req := &position.OrderRequest{
		Symbol:        s.symbol,
		Side:          side,
		Price:         price,
		Quantity:      amount,
		PriceDecimals: s.getPriceDecimals(),
		PostOnly:      true,
		ClientOrderID: clientOrderID,
		StrategyName:  s.name,
		StrategyType:  "spot_long",
	}
	ord, err := s.executor.PlaceOrder(req)
	if err != nil {
		return fmt.Errorf("place spot long %s %s outcome unresolved (client_order_id=%s): %w", side, s.symbol, clientOrderID, err)
	}
	if ord == nil || ord.OrderID <= 0 || (ord.ClientOrderID != "" && ord.ClientOrderID != clientOrderID) ||
		(ord.Symbol != "" && ord.Symbol != s.symbol) || (ord.Side != "" && ord.Side != side) ||
		(ord.Quantity > 0 && math.Abs(ord.Quantity-amount) > math.Max(1e-10, amount*1e-8)) {
		return fmt.Errorf("spot long %s %s returned invalid order identity; outcome unresolved (client_order_id=%s)", side, s.symbol, clientOrderID)
	}
	s.mu.Lock()
	if _, unresolved := s.pendingIntents[clientOrderID]; unresolved {
		delete(s.pendingIntents, clientOrderID)
		s.pendingOrders[ord.OrderID] = spotLongPendingOrder{ClientOrderID: clientOrderID, Side: side, Quantity: amount}
		err = s.persistRuntimeStateLocked()
		if err != nil {
			delete(s.pendingOrders, ord.OrderID)
			s.pendingIntents[clientOrderID] = intent
		}
	}
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("persist spot long order %d intent: %w", ord.OrderID, err)
	}
	logger.Info("SpotLongStrategy: %s %.6f %s (order=%d)", side, amount, s.baseAsset, ord.OrderID)
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
