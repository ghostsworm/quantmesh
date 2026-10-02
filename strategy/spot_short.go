package strategy

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
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

	ctx                      context.Context
	cancel                   context.CancelFunc
	mu                       sync.RWMutex
	tradeMu                  sync.Mutex
	pendingRepay             map[int64]spotShortPendingRepay
	pendingBorrow            map[string]spotShortPendingBorrow
	pendingBuy               map[string]spotShortPendingBuy
	runtimeStateStore        RuntimeStateStore
	runtimeStateErrorHandler func(error)
	unresolvedDebtHandler    func(error)

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
	if !isNilSpotRawExchange(rawEx) {
		smEx, _ = rawEx.(exchange.ISpotMarginExchange)
	}
	return &SpotShortStrategy{
		name:          name,
		cfg:           cfg,
		executor:      executor,
		ex:            ex,
		rawEx:         rawEx,
		smEx:          smEx,
		groupID:       groupID,
		symbol:        symbol,
		baseAsset:     baseAsset,
		quoteAsset:    quoteAsset,
		pendingRepay:  make(map[int64]spotShortPendingRepay),
		pendingBorrow: make(map[string]spotShortPendingBorrow),
		pendingBuy:    make(map[string]spotShortPendingBuy),
		positions:     []*Position{},
		orders:        []*Order{},
		stats:         &StrategyStatistics{},
	}
}

func isNilSpotRawExchange(rawEx exchange.IExchange) bool {
	if rawEx == nil {
		return true
	}
	value := reflect.ValueOf(rawEx)
	return value.Kind() == reflect.Pointer && value.IsNil()
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

// SetRuntimeStateStore wires durable recovery before Start is called.
func (s *SpotShortStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeStateStore = store
}

func (s *SpotShortStrategy) SetRuntimeStateErrorHandler(handler func(error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeStateErrorHandler = handler
}

func (s *SpotShortStrategy) SetUnresolvedDebtHandler(handler func(error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.unresolvedDebtHandler = handler
}

func (s *SpotShortStrategy) reportUnresolvedDebt(err error) {
	s.mu.RLock()
	handler := s.unresolvedDebtHandler
	s.mu.RUnlock()
	if handler != nil {
		handler(err)
	}
}

func (s *SpotShortStrategy) OnPriceChange(price float64) error { return nil }

func (s *SpotShortStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	if update == nil || update.Side != "BUY" {
		return nil
	}
	if update.OrderID > 0 && finiteNumber(update.ExecutedQty) && update.ExecutedQty > 0 {
		s.mu.RLock()
		pending, found := s.pendingRepay[update.OrderID]
		if !found && update.ClientOrderID != "" {
			if intent, ok := s.pendingBuy[update.ClientOrderID]; ok {
				pending = spotShortPendingRepay{ClientOrderID: update.ClientOrderID, OrderQuantity: intent.Quantity}
				found = true
			}
		}
		s.mu.RUnlock()
		if found && !pending.RepayUncertain && update.ExecutedQty > pending.ExecutedQty {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err := s.verifySpotShortFillBaseFee(ctx, update.OrderID, pending, update)
			cancel()
			if err != nil {
				wrapped := fmt.Errorf("verify spot short buy order %d net base receipt before repayment: %w", update.OrderID, err)
				s.reportUnresolvedDebt(wrapped)
				return wrapped
			}
		}
	}
	s.mu.Lock()
	pending, ok := s.pendingRepay[update.OrderID]
	if !ok && update.ClientOrderID != "" && update.OrderID > 0 {
		if intent, found := s.pendingBuy[update.ClientOrderID]; found {
			if (update.Symbol != "" && update.Symbol != s.symbol) || (update.Side != "" && update.Side != "BUY") ||
				math.IsNaN(update.ExecutedQty) || math.IsInf(update.ExecutedQty, 0) || update.ExecutedQty < 0 || update.ExecutedQty > intent.Quantity {
				s.mu.Unlock()
				return fmt.Errorf("spot short buy update identity/quantity mismatch for client order %s", update.ClientOrderID)
			}
			if s.pendingRepay == nil {
				s.pendingRepay = make(map[int64]spotShortPendingRepay)
			}
			previousBuy := intent
			delete(s.pendingBuy, update.ClientOrderID)
			s.pendingRepay[update.OrderID] = spotShortPendingRepay{ClientOrderID: update.ClientOrderID, OrderQuantity: intent.Quantity}
			if err := s.persistRuntimeStateLocked(); err != nil {
				delete(s.pendingRepay, update.OrderID)
				s.pendingBuy[update.ClientOrderID] = previousBuy
				s.mu.Unlock()
				return err
			}
			pending = s.pendingRepay[update.OrderID]
			ok = true
		}
	}
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if pending.RepayUncertain {
		s.mu.Unlock()
		return fmt.Errorf("spot short repayment outcome for order %d requires exchange reconciliation", update.OrderID)
	}
	if (update.Symbol != "" && update.Symbol != s.symbol) ||
		(pending.ClientOrderID != "" && update.ClientOrderID != "" && pending.ClientOrderID != update.ClientOrderID) {
		s.mu.Unlock()
		return fmt.Errorf("spot short buy update identity mismatch for order %d", update.OrderID)
	}
	if update.ExecutedQty < pending.ExecutedQty || update.ExecutedQty > pending.OrderQuantity || math.IsNaN(update.ExecutedQty) || math.IsInf(update.ExecutedQty, 0) {
		s.mu.Unlock()
		return fmt.Errorf("invalid cumulative filled quantity %.12g for spot short order %d (previous %.12g, requested %.12g)", update.ExecutedQty, update.OrderID, pending.ExecutedQty, pending.OrderQuantity)
	}
	if update.Status == "FILLED" && update.ExecutedQty <= 0 {
		s.mu.Unlock()
		return fmt.Errorf("spot short buy order %d reports FILLED without positive cumulative execution", update.OrderID)
	}
	delta := update.ExecutedQty - pending.ExecutedQty
	baseFeeDelta := update.BaseFeeQty
	if delta == 0 {
		baseFeeDelta = 0 // Duplicate cumulative order updates must not reapply per-fill fees.
	}
	repayAmount := delta - baseFeeDelta
	if baseFeeDelta < 0 || baseFeeDelta > delta || math.IsNaN(baseFeeDelta) || math.IsInf(baseFeeDelta, 0) {
		s.mu.Unlock()
		return fmt.Errorf("invalid base-asset fee %.12g for spot short order %d fill delta %.12g", baseFeeDelta, update.OrderID, delta)
	}
	if repayAmount > 0 {
		if s.smEx == nil {
			s.mu.Unlock()
			return fmt.Errorf("spot short repay executor unavailable for filled buy order %d", update.OrderID)
		}
		pending.RepayUncertain = true
		s.pendingRepay[update.OrderID] = pending
		if err := s.persistRuntimeStateLocked(); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.mu.Unlock()
	if repayAmount > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, err := s.smEx.Repay(ctx, s.baseAsset, repayAmount)
		cancel()
		if err != nil {
			logger.Error("SpotShortStrategy 還幣失敗 (order=%d): %v", update.OrderID, err)
			return fmt.Errorf("repay borrowed %s after filled buy order %d; outcome requires reconciliation: %w", s.baseAsset, update.OrderID, err)
		}
	}
	s.mu.Lock()
	pending, ok = s.pendingRepay[update.OrderID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("spot short order %d disappeared during repayment", update.OrderID)
	}
	pending.ExecutedQty = update.ExecutedQty
	pending.BaseFeeQty += baseFeeDelta
	pending.RepayUncertain = false
	if update.Status == "FILLED" || update.Status == "CANCELED" || update.Status == "CANCELLED" || update.Status == "EXPIRED" || update.Status == "REJECTED" {
		delete(s.pendingRepay, update.OrderID)
	} else {
		s.pendingRepay[update.OrderID] = pending
	}
	err := s.persistRuntimeStateLocked()
	if err != nil {
		s.pendingRepay[update.OrderID] = pending
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if repayAmount > 0 {
		logger.Info("📤 SpotShortStrategy: 買回成交後已還幣 %.6f %s", repayAmount, s.baseAsset)
	}
	return nil
}

// verifySpotShortFillBaseFee reconciles the full cumulative fill history and
// the already-persisted fee cursor before any newly received base asset is repaid.
func (s *SpotShortStrategy) verifySpotShortFillBaseFee(ctx context.Context, orderID int64, pending spotShortPendingRepay,
	update *position.OrderUpdate) error {
	if s.ex == nil || orderID <= 0 {
		return fmt.Errorf("exchange or stable order identity is unavailable")
	}
	if !finiteNumber(update.ExecutedQty) || update.ExecutedQty <= pending.ExecutedQty ||
		update.ExecutedQty > pending.OrderQuantity || update.ExecutedQty <= 0 {
		return fmt.Errorf("cumulative execution is invalid for the persisted buy intent")
	}
	if (update.Symbol != "" && update.Symbol != s.symbol) || update.Side != "BUY" ||
		(pending.ClientOrderID != "" && update.ClientOrderID != "" && pending.ClientOrderID != update.ClientOrderID) {
		return fmt.Errorf("order identity does not match the persisted buy intent")
	}
	raw, err := s.ex.GetOrderFills(ctx, s.symbol, orderID)
	if err != nil {
		return fmt.Errorf("query order fills: %w", err)
	}
	var fills []*exchange.OrderFill
	switch rows := raw.(type) {
	case []*exchange.OrderFill:
		fills = rows
	case []exchange.OrderFill:
		fills = make([]*exchange.OrderFill, len(rows))
		for i := range rows {
			fill := rows[i]
			fills[i] = &fill
		}
	default:
		return fmt.Errorf("exchange returned unsupported fill evidence %T", raw)
	}
	if len(fills) == 0 {
		return fmt.Errorf("exchange returned no fill evidence")
	}
	sort.Slice(fills, func(i, j int) bool {
		if fills[i] == nil {
			return false
		}
		if fills[j] == nil {
			return true
		}
		if fills[i].TradeTime != fills[j].TradeTime {
			return fills[i].TradeTime < fills[j].TradeTime
		}
		return fills[i].TradeID < fills[j].TradeID
	})
	seen := make(map[string]struct{}, len(fills))
	var totalQty, totalBaseFee, totalNotional float64
	var prefixQty, prefixBaseFee float64
	for _, fill := range fills {
		if fill == nil || strings.TrimSpace(fill.TradeID) == "" || fill.OrderID != 0 && fill.OrderID != orderID ||
			fill.Symbol != "" && !strings.EqualFold(fill.Symbol, s.symbol) || fill.Side != "" && fill.Side != exchange.SideBuy ||
			!finiteNumber(fill.Price) || fill.Price <= 0 || !finiteNumber(fill.Quantity) || fill.Quantity <= 0 ||
			!finiteNumber(fill.Commission) || !finiteNumber(fill.BaseFeeQty) || fill.BaseFeeQty < 0 || fill.BaseFeeQty > fill.Quantity {
			return fmt.Errorf("exchange returned invalid fill evidence")
		}
		if _, exists := seen[fill.TradeID]; exists {
			return fmt.Errorf("exchange returned duplicate trade ID %q", fill.TradeID)
		}
		seen[fill.TradeID] = struct{}{}
		if fill.CommissionQuoteKnown && !finiteNumber(fill.CommissionQuote) ||
			!fill.CommissionQuoteKnown && fill.Commission == 0 && strings.TrimSpace(fill.CommissionAsset) == "" {
			return fmt.Errorf("fill %s has no verifiable fee evidence", fill.TradeID)
		}
		if strings.EqualFold(fill.CommissionAsset, s.baseAsset) && fill.Commission > 0 && fill.BaseFeeQty == 0 {
			return fmt.Errorf("fill %s reports a base-asset fee without its base fee quantity", fill.TradeID)
		}
		if prefixQty < pending.ExecutedQty-entryQtyEpsilon {
			if prefixQty+fill.Quantity > pending.ExecutedQty+entryQtyEpsilon {
				return fmt.Errorf("persisted repayment cursor splits an exchange fill")
			}
			prefixQty += fill.Quantity
			prefixBaseFee += fill.BaseFeeQty
		} else {
			totalBaseFee += fill.BaseFeeQty
		}
		totalQty += fill.Quantity
		totalNotional += fill.Price * fill.Quantity
		if !finiteNumber(totalQty) || !finiteNumber(totalBaseFee) || !finiteNumber(prefixQty) || !finiteNumber(prefixBaseFee) || !finiteNumber(totalNotional) {
			return fmt.Errorf("exchange fill totals exceed the supported numeric range")
		}
	}
	tolerance := math.Max(1e-10, update.ExecutedQty*1e-8)
	if math.Abs(totalQty-update.ExecutedQty) > tolerance || math.Abs(prefixQty-pending.ExecutedQty) > tolerance ||
		math.Abs(prefixBaseFee-pending.BaseFeeQty) > tolerance || !finiteNumber(totalNotional) || !finiteNumber(totalBaseFee) {
		return fmt.Errorf("fill history does not reconcile with cumulative execution and persisted fee cursor")
	}
	averagePrice := totalNotional / totalQty
	if !finiteNumber(averagePrice) || !finiteNumber(update.AvgPrice) || update.AvgPrice < 0 ||
		(update.AvgPrice > 0 && math.Abs(averagePrice-update.AvgPrice) > math.Max(1e-8, update.AvgPrice*1e-8)) {
		return fmt.Errorf("fill notional does not match the order average price")
	}
	update.BaseFeeQty = totalBaseFee
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
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if err := s.restoreRuntimeStateLocked(); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	if err := s.reconcilePendingBorrowIntents(ctx); err != nil {
		return err
	}
	if err := s.reconcilePendingRepayOrders(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	if s.subscribableBus == nil {
		s.mu.Unlock()
		logger.Warn("SpotShortStrategy: 無可訂閱的 EventBus，跳過啟動")
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.ctx, s.cancel = runCtx, cancel
	subCh := s.subscribableBus.Subscribe()
	s.mu.Unlock()

	go s.runRuntimeReconciliation(runCtx)
	go func() {
		for {
			select {
			case <-runCtx.Done():
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

const spotShortRuntimeReconcileInterval = 3 * time.Second
const spotShortRuntimeReconcileTimeout = 30 * time.Second

func (s *SpotShortStrategy) runRuntimeReconciliation(ctx context.Context) {
	ticker := time.NewTicker(spotShortRuntimeReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.hasPendingRuntimeReconciliation() {
				continue
			}
			reconcileCtx, cancel := context.WithTimeout(ctx, spotShortRuntimeReconcileTimeout)
			err := s.reconcileRuntimeState(reconcileCtx)
			cancel()
			if err != nil && ctx.Err() == nil {
				logger.Warn("⚠️ SpotShortStrategy 运行时负债/订单对账仍未完成，将重试: %v", err)
			}
		}
	}
}

func (s *SpotShortStrategy) hasPendingRuntimeReconciliation() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pendingBorrow) > 0 || len(s.pendingBuy) > 0 || len(s.pendingRepay) > 0
}

func (s *SpotShortStrategy) reconcileRuntimeState(ctx context.Context) error {
	s.tradeMu.Lock()
	defer s.tradeMu.Unlock()
	if err := s.reconcilePendingBorrowIntents(ctx); err != nil {
		return err
	}
	return s.reconcilePendingRepayOrders(ctx)
}

func (s *SpotShortStrategy) reconcilePendingBorrowIntents(ctx context.Context) error {
	s.mu.RLock()
	intents := make(map[string]spotShortPendingBorrow, len(s.pendingBorrow))
	for cid, intent := range s.pendingBorrow {
		intents[cid] = intent
	}
	s.mu.RUnlock()
	if len(intents) == 0 {
		return nil
	}
	query, ok := s.ex.(exchange.OrderByClientIDQuerier)
	if !ok {
		return fmt.Errorf("spot short has %d pending borrow/sell intents, but margin exchange cannot query exact client order IDs", len(intents))
	}
	for cid, intent := range intents {
		if intent.Phase == "prepared" {
			confirmed, err := s.reconcilePendingBorrowTransfer(ctx, cid, intent)
			if err != nil {
				return err
			}
			intent = confirmed
			intents[cid] = confirmed
		}
		order, err := query.GetOrderByClientOrderID(ctx, s.symbol, cid)
		if err != nil {
			return fmt.Errorf("query spot short margin sell by client ID %s: %w", cid, err)
		}
		if order == nil {
			return fmt.Errorf("spot short margin sell %s is not yet verifiable; borrowed debt remains unresolved", cid)
		}
		tolerance := math.Max(1e-10, intent.Amount*1e-8)
		if order.OrderID <= 0 || order.ClientOrderID != cid || order.Symbol != s.symbol || order.Side != exchange.SideSell ||
			!finiteNumber(order.Quantity) || order.Quantity <= 0 || math.Abs(order.Quantity-intent.Amount) > tolerance {
			return fmt.Errorf("spot short margin sell identity/quantity mismatch for client ID %s", cid)
		}
		s.mu.Lock()
		current, exists := s.pendingBorrow[cid]
		if !exists || current != intent {
			s.mu.Unlock()
			return fmt.Errorf("spot short borrow intent %s changed during reconciliation", cid)
		}
		delete(s.pendingBorrow, cid)
		if err := s.persistRuntimeStateLocked(); err != nil {
			s.pendingBorrow[cid] = current
			s.mu.Unlock()
			return fmt.Errorf("persist reconciled margin sell %d: %w", order.OrderID, err)
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *SpotShortStrategy) reconcilePendingBorrowTransfer(ctx context.Context, clientOrderID string, intent spotShortPendingBorrow) (spotShortPendingBorrow, error) {
	history, ok := s.ex.(exchange.MarginBorrowHistoryQuerier)
	if !ok {
		return intent, fmt.Errorf("spot short borrow %s is unresolved; exchange cannot query margin borrow history", clientOrderID)
	}
	queryStart := intent.CreatedAtUnixMilli
	queryEnd := time.Now().UTC().UnixMilli()
	const maxHistoryWindow = 30 * 24 * time.Hour
	if queryEnd < intent.CreatedAtUnixMilli || time.Duration(queryEnd-intent.CreatedAtUnixMilli)*time.Millisecond > maxHistoryWindow {
		return intent, fmt.Errorf("spot short borrow %s is outside the exchange's supported recovery history window", clientOrderID)
	}
	const pageSize = 100
	var candidates []exchange.MarginBorrowRecord
	for page := 1; ; page++ {
		records, total, err := history.GetMarginBorrowHistory(ctx, s.baseAsset, queryStart, queryEnd, page, pageSize)
		if err != nil {
			return intent, fmt.Errorf("query spot short borrow history for %s: %w", clientOrderID, err)
		}
		for _, record := range records {
			if record.Asset == s.baseAsset && record.TransferID > 0 && record.Timestamp >= intent.CreatedAtUnixMilli && record.Timestamp <= queryEnd &&
				finiteNumber(record.Amount) && math.Abs(record.Amount-intent.Amount) <= math.Max(1e-10, intent.Amount*1e-8) {
				candidates = append(candidates, record)
			}
		}
		if int64(page*pageSize) >= total || len(records) == 0 {
			break
		}
	}
	if len(candidates) != 1 || !strings.EqualFold(candidates[0].Status, "CONFIRMED") {
		return intent, fmt.Errorf("spot short borrow %s has %d matching history records; exact confirmed transfer cannot be proven", clientOrderID, len(candidates))
	}
	intent.Phase = "borrowed"
	intent.BorrowTransferID = candidates[0].TransferID
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.pendingBorrow[clientOrderID]
	if !exists || current != (spotShortPendingBorrow{Amount: intent.Amount, Phase: "prepared", CreatedAtUnixMilli: intent.CreatedAtUnixMilli}) {
		return intent, fmt.Errorf("spot short borrow intent %s changed during transfer reconciliation", clientOrderID)
	}
	s.pendingBorrow[clientOrderID] = intent
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.pendingBorrow[clientOrderID] = current
		return current, fmt.Errorf("persist recovered borrow transfer %d: %w", intent.BorrowTransferID, err)
	}
	return intent, nil
}

func (s *SpotShortStrategy) reconcilePendingRepayOrders(ctx context.Context) error {
	if err := s.reconcilePendingBuyIntents(ctx); err != nil {
		return err
	}
	s.mu.RLock()
	pendingByID := make(map[int64]spotShortPendingRepay, len(s.pendingRepay))
	for id, pending := range s.pendingRepay {
		pendingByID[id] = pending
	}
	s.mu.RUnlock()
	for id, pending := range pendingByID {
		if s.ex == nil {
			return fmt.Errorf("spot short exchange unavailable while reconciling buy order %d", id)
		}
		raw, err := s.ex.GetOrder(ctx, s.symbol, id)
		if err != nil {
			return fmt.Errorf("query spot short buy order %d: %w", id, err)
		}
		update, err := strategyOrderUpdateFromExchange(raw)
		if err != nil {
			return fmt.Errorf("query spot short buy order %d: %w", id, err)
		}
		if update.OrderID != 0 && update.OrderID != id {
			return fmt.Errorf("exchange returned order %d while reconciling spot short order %d", update.OrderID, id)
		}
		if update.Side != "BUY" || (update.Symbol != "" && update.Symbol != s.symbol) ||
			(pending.ClientOrderID != "" && update.ClientOrderID != "" && pending.ClientOrderID != update.ClientOrderID) {
			return fmt.Errorf("exchange order identity mismatch while reconciling spot short order %d", id)
		}
		if update.ExecutedQty < pending.ExecutedQty {
			return fmt.Errorf("exchange cumulative fill regressed for spot short order %d: %.12g < %.12g", id, update.ExecutedQty, pending.ExecutedQty)
		}
		if update.ExecutedQty > pending.ExecutedQty {
			if err := s.verifySpotShortFillBaseFee(ctx, id, pending, update); err != nil {
				return fmt.Errorf("reconcile spot short order %d fills: %w", id, err)
			}
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stop spot short reconciliation before applying order %d: %w", id, err)
		}
		update.OrderID = id
		update.Symbol = s.symbol
		if err := s.OnOrderUpdate(update); err != nil {
			return fmt.Errorf("apply reconciled spot short order %d: %w", id, err)
		}
	}
	return nil
}

func (s *SpotShortStrategy) reconcilePendingBuyIntents(ctx context.Context) error {
	s.mu.RLock()
	intents := make(map[string]spotShortPendingBuy, len(s.pendingBuy))
	for clientOrderID, intent := range s.pendingBuy {
		intents[clientOrderID] = intent
	}
	s.mu.RUnlock()
	if len(intents) == 0 {
		return nil
	}
	query, ok := s.ex.(exchange.OrderByClientIDQuerier)
	if !ok {
		return fmt.Errorf("spot short has %d unresolved buy intent(s), but exchange cannot query exact client order ids", len(intents))
	}
	for clientOrderID, intent := range intents {
		order, err := query.GetOrderByClientOrderID(ctx, s.symbol, clientOrderID)
		if err != nil {
			return fmt.Errorf("query spot short buy order by client id %s: %w", clientOrderID, err)
		}
		if order == nil {
			return fmt.Errorf("spot short buy order %s is not yet verifiable; refusing startup", clientOrderID)
		}
		if order.OrderID <= 0 || order.ClientOrderID != clientOrderID || order.Symbol != s.symbol || order.Side != exchange.SideBuy ||
			math.IsNaN(order.Quantity) || math.IsInf(order.Quantity, 0) || math.Abs(order.Quantity-intent.Quantity) > math.Max(1e-10, intent.Quantity*1e-8) {
			return fmt.Errorf("exchange order identity/quantity mismatch while reconciling spot short client order %s", clientOrderID)
		}
		s.mu.Lock()
		if current, found := s.pendingBuy[clientOrderID]; found {
			delete(s.pendingBuy, clientOrderID)
			s.pendingRepay[order.OrderID] = spotShortPendingRepay{ClientOrderID: clientOrderID, OrderQuantity: current.Quantity}
			if err := s.persistRuntimeStateLocked(); err != nil {
				delete(s.pendingRepay, order.OrderID)
				s.pendingBuy[clientOrderID] = current
				s.mu.Unlock()
				return fmt.Errorf("persist reconciled spot short buy order %d: %w", order.OrderID, err)
			}
		}
		s.mu.Unlock()
	}
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

	currentShort, err := s.getCurrentShortPosition(ctx)
	if err != nil {
		logger.Error("SpotShortStrategy 無法核實當前空倉，拒絕執行 hedge signal: %v", err)
		return
	}
	diff := targetShort - currentShort

	if math.Abs(diff) < 0.000001 {
		return
	}
	s.mu.RLock()
	hasPendingBuy := len(s.pendingRepay) > 0 || len(s.pendingBuy) > 0
	s.mu.RUnlock()
	if hasPendingBuy {
		return
	}

	if diff > 0 {
		if err := s.increaseShort(ctx, diff); err != nil {
			logger.Error("SpotShortStrategy 增加空倉失敗 (target=%.8f current=%.8f): %v", targetShort, currentShort, err)
		}
	} else {
		if err := s.decreaseShort(ctx, -diff); err != nil {
			logger.Error("SpotShortStrategy 買回/持久化待還狀態失敗: %v", err)
		}
	}
}

func (s *SpotShortStrategy) getCurrentShortPosition(ctx context.Context) (float64, error) {
	raw, err := s.ex.GetPositions(ctx, s.symbol)
	if err != nil {
		return 0, fmt.Errorf("query current position for %s: %w", s.symbol, err)
	}
	if raw == nil {
		return 0, fmt.Errorf("exchange returned nil position data for %s", s.symbol)
	}
	// positionExchangeAdapter 返回 []*position.PositionInfo
	if infos, ok := raw.([]*position.PositionInfo); ok {
		current := 0.0
		found := false
		for _, p := range infos {
			if p == nil {
				return 0, fmt.Errorf("exchange returned a nil position entry for %s", s.symbol)
			}
			if p.Symbol != s.symbol {
				continue
			}
			if found || math.IsNaN(p.Size) || math.IsInf(p.Size, 0) || p.Size > 0 {
				return 0, fmt.Errorf("exchange returned ambiguous or invalid spot short position for %s", s.symbol)
			}
			found = true
			current = -p.Size
		}
		return current, nil
	}
	return 0, fmt.Errorf("exchange returned unsupported position data %T for %s", raw, s.symbol)
}

func (s *SpotShortStrategy) increaseShort(ctx context.Context, amount float64) error {
	s.tradeMu.Lock()
	defer s.tradeMu.Unlock()
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return nil
	}
	s.mu.RLock()
	if len(s.pendingBuy) > 0 || len(s.pendingRepay) > 0 {
		s.mu.RUnlock()
		return fmt.Errorf("spot short has an unresolved buy/repayment order; refusing another borrow")
	}
	var unresolvedClientOrderID string
	for clientOrderID := range s.pendingBorrow {
		unresolvedClientOrderID = clientOrderID
		break
	}
	s.mu.RUnlock()
	if unresolvedClientOrderID != "" {
		err := fmt.Errorf("spot short has unresolved borrow intent %s; refusing another borrow until exchange reconciliation", unresolvedClientOrderID)
		s.reportUnresolvedDebt(err)
		return err
	}
	if s.smEx == nil || s.ex == nil || s.executor == nil {
		return fmt.Errorf("spot short borrow dependencies are unavailable")
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err == nil && price <= 0 {
		err = fmt.Errorf("invalid price %.8f", price)
	}
	if err != nil {
		return fmt.Errorf("fetch %s price before borrowing: %w", s.symbol, err)
	}
	price = s.roundPrice(price)
	clientOrderID := utils.GenerateOrderID(price, "SELL", s.getPriceDecimals())
	intent := spotShortPendingBorrow{Amount: amount, Phase: "prepared", CreatedAtUnixMilli: time.Now().UTC().UnixMilli()}
	s.mu.Lock()
	if s.pendingBorrow == nil {
		s.pendingBorrow = make(map[string]spotShortPendingBorrow)
	}
	s.pendingBorrow[clientOrderID] = intent
	if err := s.persistRuntimeStateLocked(); err != nil {
		delete(s.pendingBorrow, clientOrderID)
		s.mu.Unlock()
		return fmt.Errorf("persist borrow intent before borrowing: %w", err)
	}
	s.mu.Unlock()
	transferID, err := s.smEx.Borrow(ctx, s.baseAsset, amount)
	if err != nil {
		wrapped := fmt.Errorf("借幣 %.8f %s 結果未核實: %w", amount, s.baseAsset, err)
		s.reportUnresolvedDebt(wrapped)
		return wrapped
	}
	if transferID <= 0 {
		wrapped := fmt.Errorf("借币请求返回无效流水号，借币结果未核实 (transfer_id=%d)", transferID)
		s.reportUnresolvedDebt(wrapped)
		return wrapped
	}
	s.mu.Lock()
	intent.Phase = "borrowed"
	intent.BorrowTransferID = transferID
	s.pendingBorrow[clientOrderID] = intent
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.mu.Unlock()
		wrapped := fmt.Errorf("borrow succeeded but its transfer id could not be durably recorded: %w", err)
		s.reportUnresolvedDebt(wrapped)
		return wrapped
	}
	s.mu.Unlock()
	req := &position.OrderRequest{
		Symbol:        s.symbol,
		Side:          "SELL",
		Price:         price,
		Quantity:      amount,
		PriceDecimals: s.getPriceDecimals(),
		PostOnly:      true,
		ClientOrderID: clientOrderID,
		StrategyName:  s.name,
		StrategyType:  "spot_short",
	}
	order, err := s.executor.PlaceOrder(req)
	if err != nil {
		wrapped := fmt.Errorf("借币成功但卖出订单结果未核实 (client_order_id=%s): %w", clientOrderID, err)
		s.reportUnresolvedDebt(wrapped)
		return wrapped
	}
	if order == nil || order.OrderID <= 0 || (order.ClientOrderID != "" && order.ClientOrderID != clientOrderID) {
		wrapped := fmt.Errorf("借币成功但卖出订单回执无效，结果未核实 (client_order_id=%s)", clientOrderID)
		s.reportUnresolvedDebt(wrapped)
		return wrapped
	}
	s.mu.Lock()
	delete(s.pendingBorrow, clientOrderID)
	err = s.persistRuntimeStateLocked()
	if err != nil {
		s.pendingBorrow[clientOrderID] = intent
	}
	s.mu.Unlock()
	if err != nil {
		wrapped := fmt.Errorf("卖出订单已受理但借币意图清理未持久化，结果未核实 (client_order_id=%s): %w", clientOrderID, err)
		s.reportUnresolvedDebt(wrapped)
		return wrapped
	}
	logger.Info("📤 SpotShortStrategy: 借幣 %.6f %s 並賣出", amount, s.baseAsset)
	return nil
}

func (s *SpotShortStrategy) decreaseShort(ctx context.Context, amount float64) error {
	s.tradeMu.Lock()
	defer s.tradeMu.Unlock()
	amount = s.roundQuantity(amount)
	if amount <= 0 {
		return nil
	}
	price, err := s.ex.GetLatestPrice(ctx, s.symbol)
	if err != nil || price <= 0 {
		logger.Error("SpotShortStrategy 獲取價格失敗: %v", err)
		return err
	}
	// 限價買單略高於市價以提高成交率
	price = s.roundPrice(price * 1.001)
	clientOrderID := utils.GenerateOrderID(price, "BUY", s.getPriceDecimals())
	intent := spotShortPendingBuy{Quantity: amount, CreatedAtUnixMilli: time.Now().UTC().UnixMilli()}
	s.mu.Lock()
	if len(s.pendingBuy) > 0 || len(s.pendingRepay) > 0 {
		s.mu.Unlock()
		return fmt.Errorf("spot short has an unresolved buy/repayment order; refusing another buy")
	}
	if s.pendingBuy == nil {
		s.pendingBuy = make(map[string]spotShortPendingBuy)
	}
	s.pendingBuy[clientOrderID] = intent
	if err := s.persistRuntimeStateLocked(); err != nil {
		delete(s.pendingBuy, clientOrderID)
		s.mu.Unlock()
		return fmt.Errorf("persist spot short buy intent before submission: %w", err)
	}
	s.mu.Unlock()
	req := &position.OrderRequest{
		Symbol:        s.symbol,
		Side:          "BUY",
		Price:         price,
		Quantity:      amount,
		PriceDecimals: s.getPriceDecimals(),
		PostOnly:      true,
		ClientOrderID: clientOrderID,
		StrategyName:  s.name,
		StrategyType:  "spot_short",
	}
	ord, err := s.executor.PlaceOrder(req)
	if err != nil {
		wrapped := fmt.Errorf("spot short buy submission outcome is unresolved (client_order_id=%s): %w", clientOrderID, err)
		s.reportUnresolvedDebt(wrapped)
		return wrapped
	}
	if ord == nil || ord.OrderID <= 0 || (ord.ClientOrderID != "" && ord.ClientOrderID != clientOrderID) ||
		(ord.Symbol != "" && ord.Symbol != s.symbol) || (ord.Side != "" && ord.Side != "BUY") ||
		(ord.Quantity > 0 && math.Abs(ord.Quantity-amount) > math.Max(1e-10, amount*1e-8)) {
		wrapped := fmt.Errorf("spot short buy acknowledgement is invalid; outcome unresolved (client_order_id=%s)", clientOrderID)
		s.reportUnresolvedDebt(wrapped)
		return wrapped
	}
	s.mu.Lock()
	if s.pendingRepay == nil {
		s.pendingRepay = make(map[int64]spotShortPendingRepay)
	}
	if _, unresolved := s.pendingBuy[clientOrderID]; unresolved {
		delete(s.pendingBuy, clientOrderID)
		s.pendingRepay[ord.OrderID] = spotShortPendingRepay{ClientOrderID: clientOrderID, OrderQuantity: amount}
		err = s.persistRuntimeStateLocked()
		if err != nil {
			delete(s.pendingRepay, ord.OrderID)
			s.pendingBuy[clientOrderID] = intent
		}
	}
	s.mu.Unlock()
	if err != nil {
		wrapped := fmt.Errorf("persist spot short pending repay for order %d: %w", ord.OrderID, err)
		s.reportUnresolvedDebt(wrapped)
		return wrapped
	}
	logger.Info("📥 SpotShortStrategy: 已下買回單 %.6f %s (order=%d)，成交後還幣", amount, s.baseAsset, ord.OrderID)
	return nil
}

// roundQuantity 將數量向下取整到交易所精度。
// 現貨賣出沒有 ReduceOnly 兜底，向上取整會直接超出持有量被拒單。
func (s *SpotShortStrategy) roundQuantity(qty float64) float64 {
	decimals := s.ex.GetQuantityDecimals()
	if decimals <= 0 {
		decimals = 6
	}
	return utils.FloorToDecimals(qty, decimals)
}

func (s *SpotShortStrategy) roundPrice(price float64) float64 {
	decimals := s.ex.GetPriceDecimals()
	if decimals <= 0 {
		decimals = 2
	}
	return utils.RoundToDecimals(price, decimals)
}

func (s *SpotShortStrategy) getPriceDecimals() int {
	if s.ex != nil {
		return s.ex.GetPriceDecimals()
	}
	return 2
}
