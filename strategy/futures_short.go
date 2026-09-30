package strategy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/execution"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/utils"
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

	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
	orderTracker *futuresHedgeOrderTracker

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
		name:         name,
		cfg:          cfg,
		executor:     executor,
		ex:           ex,
		groupID:      groupID,
		symbol:       symbol,
		baseAsset:    baseAsset,
		quoteAsset:   quoteAsset,
		positions:    []*Position{},
		orders:       []*Order{},
		stats:        &StrategyStatistics{},
		orderTracker: newFuturesHedgeOrderTracker(cfg, name, groupID, symbol, futuresHedgeExchangeName(ex)),
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

func (s *FuturesShortStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	return s.orderTracker.OnOrderUpdate(update)
}

func (s *FuturesShortStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	s.orderTracker.SetStore(store)
}

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
	if err := s.orderTracker.RestoreAndReconcile(ctx, s.ex); err != nil {
		return err
	}
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
	if evt == nil {
		return
	}
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

	currentShort, err := s.getCurrentShortPosition(ctx)
	if err != nil {
		logger.Error("FuturesShortStrategy 無法核實當前空倉，拒絕調整對沖倉位: %v", err)
		return
	}
	diff := targetShort - currentShort

	if math.Abs(diff) < 0.000001 {
		return
	}

	if diff > 0 {
		if err := s.increaseShort(ctx, diff); err != nil {
			logger.Error("FuturesShortStrategy 開空失敗: %v", err)
		}
	} else {
		if err := s.decreaseShort(ctx, -diff); err != nil {
			logger.Error("FuturesShortStrategy 平空失敗: %v", err)
		}
	}
}

func (s *FuturesShortStrategy) getCurrentShortPosition(ctx context.Context) (float64, error) {
	if s.ex == nil {
		return 0, fmt.Errorf("query futures short position %s: exchange unavailable", s.symbol)
	}
	raw, err := s.ex.GetPositions(ctx, s.symbol)
	if err != nil {
		return 0, fmt.Errorf("query futures short position %s: %w", s.symbol, err)
	}
	infos, ok := raw.([]*position.PositionInfo)
	if !ok || infos == nil {
		return 0, fmt.Errorf("query futures short position %s returned unverifiable data", s.symbol)
	}
	var total float64
	for _, p := range infos {
		if p == nil || p.Symbol != s.symbol {
			continue
		}
		if math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
			return 0, fmt.Errorf("query futures short position %s returned non-finite size", s.symbol)
		}
		if p.Size < 0 {
			total -= p.Size
			if math.IsInf(total, 0) {
				return 0, fmt.Errorf("query futures short position %s returned overflowing size", s.symbol)
			}
		}
	}
	return total, nil
}

func (s *FuturesShortStrategy) increaseShort(ctx context.Context, amount float64) error {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return nil
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		return fmt.Errorf("get futures short price for %s: price=%g err=%v", s.symbol, price, err)
	}
	price = s.roundPrice(price)
	return s.submitHedgeOrder("SELL", price, amount, false)
}

func (s *FuturesShortStrategy) decreaseShort(ctx context.Context, amount float64) error {
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return nil
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		return fmt.Errorf("get futures short price for %s: price=%g err=%v", s.symbol, price, err)
	}
	price = s.roundPrice(price * 1.001)
	return s.submitHedgeOrder("BUY", price, amount, true)
}

func (s *FuturesShortStrategy) submitHedgeOrder(side string, price, amount float64, reduceOnly bool) error {
	cid, err := s.orderTracker.Begin(side, amount)
	if err != nil {
		return err
	}
	req := &position.OrderRequest{
		Symbol:        s.symbol,
		Side:          side,
		Price:         price,
		Quantity:      amount,
		PriceDecimals: s.getPriceDecimals(),
		PostOnly:      true,
		ReduceOnly:    reduceOnly,
		ClientOrderID: cid,
		StrategyName:  s.name,
		StrategyType:  "futures_short",
	}
	order, err := s.executor.PlaceOrder(req)
	if err != nil {
		if rollbackErr := s.orderTracker.SubmissionFailed(cid, err); rollbackErr != nil {
			return errors.Join(err, rollbackErr)
		}
		return err
	}
	if order == nil {
		return fmt.Errorf("futures short order acknowledgement is missing: %w", execution.ErrOrderUnknown)
	}
	if err := s.orderTracker.Bind(cid, order); err != nil {
		return fmt.Errorf("futures short order acknowledgement requires reconciliation: %w: %v", execution.ErrOrderUnknown, err)
	}
	logger.Info("📤 FuturesShortStrategy: %s %.6f %s (cid=%s)", side, amount, s.baseAsset, cid)
	return nil
}

// roundQuantity 將數量向下取整到交易所精度。
// 必須向下：向上取整會讓平倉量超出持倉、開倉量超出餘額，直接被交易所拒單。
func (s *FuturesShortStrategy) roundQuantity(qty float64) float64 {
	decimals := s.ex.GetQuantityDecimals()
	if decimals <= 0 {
		decimals = 6
	}
	return utils.FloorToDecimals(qty, decimals)
}

func (s *FuturesShortStrategy) roundPrice(price float64) float64 {
	decimals := s.ex.GetPriceDecimals()
	if decimals <= 0 {
		decimals = 2
	}
	return utils.RoundToDecimals(price, decimals)
}

func (s *FuturesShortStrategy) getPriceDecimals() int {
	if s.ex != nil {
		return s.ex.GetPriceDecimals()
	}
	return 2
}
