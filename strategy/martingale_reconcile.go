package strategy

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/utils"
)

type martingaleCloseOrderByClientID interface {
	GetOrderByClientOrderID(context.Context, string, string) (*exchange.Order, error)
}

const martingaleRuntimeReconcileInterval = 3 * time.Second

func (s *MartingaleStrategy) runEntryOrderReconciliation(ctx context.Context) {
	ticker := time.NewTicker(martingaleRuntimeReconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.hasPendingMartingaleEntryReconciliation() && !s.hasPendingMartingaleCloseReconciliation() {
				continue
			}
			reconcileCtx, cancel := context.WithTimeout(ctx, dcaFillEvidenceTimeout)
			var err error
			if s.hasPendingMartingaleEntryReconciliation() {
				err = s.reconcilePersistedEntryOrders(reconcileCtx)
			}
			if err == nil && s.hasPendingMartingaleCloseReconciliation() {
				err = s.reconcileCloseSubmission(reconcileCtx)
			}
			cancel()
			if err != nil && ctx.Err() == nil {
				logger.Warn("⚠️ [%s] 马丁格尔运行时订单对账仍未完成，将重试: %v", s.name, err)
			}
		}
	}
}

func (s *MartingaleStrategy) hasPendingMartingaleEntryReconciliation() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, entry := range s.entries {
		if entry != nil && (entry.Status == entryStatusPending || entry.Status == entryStatusPartiallyFilled || entry.Status == position.OrderStatusUnknown) {
			return true
		}
	}
	return false
}

func (s *MartingaleStrategy) hasPendingMartingaleCloseReconciliation() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.closeClientOrderID != ""
}

func (s *MartingaleStrategy) resolveUnverifiedCommission(update *position.OrderUpdate) error {
	if update == nil || update.CommissionKnown || !finiteNumber(update.ExecutedQty) || update.ExecutedQty <= 0 {
		return nil
	}

	s.mu.RLock()
	var intent dcaOrderIntent
	found := false
	var attributedInventory float64
	exchangeName := ""
	if s.exchange != nil {
		exchangeName = strings.ToLower(s.exchange.GetName())
	}
	incomingCID := utils.RemoveBrokerPrefix(exchangeName, update.ClientOrderID)
	if s.isClosing && (update.OrderID == s.closeOrderID || (s.closeClientOrderID != "" && incomingCID == s.closeClientOrderID)) {
		side := exchange.SideSell
		if s.direction == "SHORT" {
			side = exchange.SideBuy
		}
		intent = dcaOrderIntent{orderID: s.closeOrderID, symbol: s.strategyCfg.Symbol, side: side,
			quantity: s.closeRequestedQty, progress: s.closeProgress, close: true}
		attributedInventory = s.totalQty
		found = true
	} else {
		for _, entry := range s.entries {
			if entry == nil || (entry.OrderID != update.OrderID && (entry.ClientOrderID == "" || entry.ClientOrderID != incomingCID)) {
				continue
			}
			side := exchange.SideBuy
			if s.direction == "SHORT" {
				side = exchange.SideSell
			}
			intent = dcaOrderIntent{orderID: entry.OrderID, symbol: s.strategyCfg.Symbol, side: side,
				quantity: entry.RequestedQuantity, progress: entry.FillProgress}
			found = true
			break
		}
	}
	s.mu.RUnlock()
	if !found || update.ExecutedQty <= intent.progress.Quantity+entryQtyEpsilon {
		return nil
	}
	if intent.close && update.ExecutedQty-intent.progress.Quantity > attributedInventory+entryQtyEpsilon {
		return nil // Let the close handler report the inventory contradiction.
	}
	if intent.orderID <= 0 {
		intent.orderID = update.OrderID
	}
	if !finiteNumber(update.AvgPrice) || update.AvgPrice <= 0 || !finiteNumber(intent.quantity) || intent.quantity <= 0 ||
		update.ExecutedQty > intent.quantity+entryQtyEpsilon {
		return nil // Let the strategy's intent validator report malformed execution evidence.
	}
	if s.exchange == nil || intent.orderID <= 0 {
		return fmt.Errorf("martingale order lacks exchange or stable order identity for fee verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	order := &exchange.Order{OrderID: intent.orderID, Symbol: intent.symbol, Side: intent.side, Quantity: intent.quantity,
		ExecutedQty: update.ExecutedQty, AvgPrice: update.AvgPrice, Status: exchange.OrderStatus(strings.ToUpper(strings.TrimSpace(update.Status)))}
	var fee, averagePrice float64
	var err error
	if intent.close {
		fee, averagePrice, err = s.reconcileCloseFills(ctx, intent.symbol, order, intent.progress)
	} else {
		fee, averagePrice, err = s.reconcileEntryFills(ctx, intent.symbol, order, intent.progress)
	}
	if err != nil {
		return fmt.Errorf("verify martingale order %d fill fees before accounting: %w", intent.orderID, err)
	}
	update.Commission, update.CommissionAsset, update.AvgPrice = fee, s.exchange.GetQuoteAsset(), averagePrice
	update.CommissionKnown = true
	return nil
}

// reconcileCloseSubmission resolves the durable pre-submit marker before the
// strategy can trade. Missing or unsupported exchange evidence is not treated
// as proof that the venue rejected the request.
func (s *MartingaleStrategy) reconcileCloseSubmission(ctx context.Context) error {
	s.mu.RLock()
	cid := s.closeClientOrderID
	if cid == "" {
		s.mu.RUnlock()
		return nil
	}
	symbol := s.strategyCfg.Symbol
	existingOrderID := s.closeOrderID
	requestedQty := s.closeRequestedQty
	progress := s.closeProgress
	direction := s.direction
	s.mu.RUnlock()

	if s.exchange == nil {
		return fmt.Errorf("exchange is unavailable for close-order reconciliation")
	}
	quantityDecimals := s.exchange.GetQuantityDecimals()
	if quantityDecimals < 0 || quantityDecimals > 18 {
		return fmt.Errorf("exchange quantity precision %d is invalid for close-order reconciliation", quantityDecimals)
	}
	order, queryErr := s.lookupCloseOrder(ctx, symbol, cid)
	if order == nil {
		return fmt.Errorf("close order %s was not verified (query error: %v); absence does not prove submission rejection", cid, queryErr)
	}

	wantSide := exchange.SideSell
	if direction == "SHORT" {
		wantSide = exchange.SideBuy
	}
	returnedCID := utils.RemoveBrokerPrefix(strings.ToLower(s.exchange.GetName()), order.ClientOrderID)
	quantityTolerance := math.Max(entryQtyEpsilon, math.Pow10(-s.exchange.GetQuantityDecimals())*1.01)
	if returnedCID != cid || order.OrderID <= 0 || !strings.EqualFold(order.Symbol, symbol) || order.Side != wantSide ||
		!finiteNumber(order.Quantity) || order.Quantity <= 0 || order.Quantity > requestedQty+quantityTolerance ||
		!finiteNumber(order.ExecutedQty) || order.ExecutedQty < 0 || order.ExecutedQty > order.Quantity+quantityTolerance || order.ExecutedQty > requestedQty+entryQtyEpsilon ||
		(existingOrderID > 0 && existingOrderID != order.OrderID) {
		return fmt.Errorf("close order %s identity or quantity does not match the durable intent", cid)
	}
	status := strings.ToUpper(strings.TrimSpace(string(order.Status)))
	switch status {
	case "NEW", "PARTIALLY_FILLED", "FILLED", "FULLY_FILLED", "CLOSED", "CANCELED", "CANCELLED", "REJECTED", "EXPIRED", "FAILED":
	default:
		return fmt.Errorf("close order %s has unrecognized exchange status %q", cid, order.Status)
	}
	if (signalOrderStatusFilled(status) && order.ExecutedQty <= 0) || status == "NEW" && order.ExecutedQty > entryQtyEpsilon ||
		status == "PARTIALLY_FILLED" && order.ExecutedQty <= 0 {
		return fmt.Errorf("close order %s has inconsistent status and cumulative fill", cid)
	}

	update := &position.OrderUpdate{OrderID: order.OrderID, ClientOrderID: order.ClientOrderID,
		Symbol: order.Symbol, Side: string(order.Side), Status: string(order.Status),
		ExecutedQty: order.ExecutedQty, AvgPrice: order.AvgPrice}
	if order.ExecutedQty > progress.Quantity {
		fee, averagePrice, err := s.reconcileCloseFills(ctx, symbol, order, progress)
		if err != nil {
			return err
		}
		update.Commission = fee
		update.CommissionAsset = s.exchange.GetQuoteAsset()
		update.AvgPrice = averagePrice
		update.CommissionKnown = true
	} else if progress.Quantity > 0 && order.ExecutedQty < progress.Quantity-entryQtyEpsilon {
		return fmt.Errorf("exchange close execution regressed below persisted progress")
	} else if order.ExecutedQty < progress.Quantity {
		update.ExecutedQty = progress.Quantity
	}

	s.mu.Lock()
	if s.closeClientOrderID != cid {
		s.mu.Unlock()
		return fmt.Errorf("close intent changed during reconciliation")
	}
	s.isClosing = true
	s.closeOrderID = order.OrderID
	s.pendingCloseReason = ""
	if s.closeRequestedQty == 0 {
		s.closeRequestedQty = order.Quantity
	}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("persist reconciled close order identity: %w", err)
	}
	s.mu.Unlock()

	if order.ExecutedQty > progress.Quantity || signalOrderStatusTerminal(string(order.Status)) {
		if err := s.OnOrderUpdate(update); err != nil {
			return fmt.Errorf("apply reconciled close order %d: %w", order.OrderID, err)
		}
	}
	return nil
}

func (s *MartingaleStrategy) lookupCloseOrder(ctx context.Context, symbol, cid string) (*exchange.Order, error) {
	if query, ok := s.exchange.(martingaleCloseOrderByClientID); ok {
		order, err := query.GetOrderByClientOrderID(ctx, symbol, cid)
		if err == nil && order != nil {
			return order, nil
		}
		if err != nil {
			// An unavailable historical lookup can still be complemented by the
			// independently queried open-order snapshot below.
		}
	}
	openOrdersRaw, err := s.exchange.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("query open close orders: %w", err)
	}
	var openOrders []*exchange.Order
	switch orders := openOrdersRaw.(type) {
	case []*exchange.Order:
		openOrders = orders
	case []exchange.Order:
		openOrders = make([]*exchange.Order, len(orders))
		for i := range orders {
			openOrders[i] = &orders[i]
		}
	default:
		return nil, fmt.Errorf("unsupported open-order response %T", openOrdersRaw)
	}
	var matched *exchange.Order
	for _, order := range openOrders {
		if order == nil || utils.RemoveBrokerPrefix(strings.ToLower(s.exchange.GetName()), order.ClientOrderID) != cid {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("multiple open orders share close client ID %s", cid)
		}
		matched = order
	}
	return matched, nil
}

func (s *MartingaleStrategy) reconcileCloseFills(ctx context.Context, symbol string, order *exchange.Order, progress position.FillProgress) (float64, float64, error) {
	raw, err := s.exchange.GetOrderFills(ctx, symbol, order.OrderID)
	if err != nil {
		return 0, 0, fmt.Errorf("query close order %d fills: %w", order.OrderID, err)
	}
	fills, ok := raw.([]*exchange.OrderFill)
	if !ok || len(fills) == 0 {
		return 0, 0, fmt.Errorf("close order %d has unaccounted fills but exchange returned no supported fill details", order.OrderID)
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
	var qty, notional, addedFee float64
	prefixQty, prefixNotional := 0.0, 0.0
	quoteAsset := s.exchange.GetQuoteAsset()
	for _, fill := range fills {
		if fill == nil || fill.TradeID == "" || fill.OrderID != 0 && fill.OrderID != order.OrderID ||
			fill.Symbol != "" && !strings.EqualFold(fill.Symbol, symbol) || fill.Side != "" && fill.Side != order.Side ||
			!finiteNumber(fill.Price) || fill.Price <= 0 || !finiteNumber(fill.Quantity) || fill.Quantity <= 0 ||
			!finiteNumber(fill.Commission) {
			return 0, 0, fmt.Errorf("close order %d returned invalid fill evidence", order.OrderID)
		}
		if _, exists := seen[fill.TradeID]; exists {
			return 0, 0, fmt.Errorf("close order %d returned duplicate trade id %q", order.OrderID, fill.TradeID)
		}
		seen[fill.TradeID] = struct{}{}
		if fill.BaseFeeQty != 0 {
			return 0, 0, fmt.Errorf("close order %d contains base-asset fees that martingale inventory cannot safely reconcile", order.OrderID)
		}
		qty += fill.Quantity
		notional += fill.Price * fill.Quantity
		if prefixQty < progress.Quantity-entryQtyEpsilon {
			if prefixQty+fill.Quantity > progress.Quantity+entryQtyEpsilon {
				return 0, 0, fmt.Errorf("persisted close progress splits an exchange fill")
			}
			prefixQty += fill.Quantity
			prefixNotional += fill.Price * fill.Quantity
			continue
		}
		converted, known := 0.0, false
		if fill.CommissionQuoteKnown {
			converted, known = fill.CommissionQuote, finiteNumber(fill.CommissionQuote)
		} else if fill.Commission == 0 && strings.TrimSpace(fill.CommissionAsset) == "" {
			return 0, 0, fmt.Errorf("close order %d fill %s has no verifiable commission evidence", order.OrderID, fill.TradeID)
		} else {
			converted, known = commissionInQuote(s.exchange, fill.Commission, fill.CommissionAsset, fill.Price)
		}
		if !known {
			return 0, 0, fmt.Errorf("close order %d fill %s commission cannot be valued in %s", order.OrderID, fill.TradeID, quoteAsset)
		}
		addedFee += converted
	}
	tolerance := math.Max(entryQtyEpsilon, order.ExecutedQty*1e-8)
	if math.Abs(qty-order.ExecutedQty) > tolerance || math.Abs(prefixQty-progress.Quantity) > tolerance ||
		progress.Quantity > 0 && math.Abs(prefixNotional-progress.Notional) > math.Max(1e-8, progress.Notional*1e-8) {
		return 0, 0, fmt.Errorf("close order %d fill history does not reconcile with cumulative execution", order.OrderID)
	}
	averagePrice := notional / qty
	if !finiteNumber(averagePrice) || averagePrice <= 0 || !finiteNumber(order.AvgPrice) || order.AvgPrice > 0 && math.Abs(averagePrice-order.AvgPrice) > math.Max(1e-8, order.AvgPrice*1e-8) {
		return 0, 0, fmt.Errorf("close order %d fill notional does not match exchange order", order.OrderID)
	}
	return addedFee, averagePrice, nil
}

// reconcilePersistedEntryOrders repairs fills missed while the process was
// offline. Every active/unknown entry must be proven against exchange order
// state before Start can enable price decisions.
func (s *MartingaleStrategy) reconcilePersistedEntryOrders(ctx context.Context) error {
	s.mu.RLock()
	entries := make([]MartingaleEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		if entry != nil && (entry.Status == entryStatusPending || entry.Status == entryStatusPartiallyFilled || entry.Status == position.OrderStatusUnknown) {
			entries = append(entries, *entry)
		}
	}
	s.mu.RUnlock()
	if len(entries) == 0 {
		return nil
	}
	if s.exchange == nil {
		return fmt.Errorf("exchange is unavailable for entry-order reconciliation")
	}
	for _, entry := range entries {
		if (entry.OrderID <= 0 && entry.ClientOrderID == "") || entry.RequestedQuantity <= 0 {
			return fmt.Errorf("entry level %d has invalid persisted order identity or requested quantity", entry.Level)
		}
		if err := s.reconcilePersistedEntryOrder(ctx, entry); err != nil {
			return fmt.Errorf("reconcile entry order %d at level %d: %w", entry.OrderID, entry.Level, err)
		}
	}
	return nil
}

func (s *MartingaleStrategy) reconcilePersistedEntryOrder(ctx context.Context, entry MartingaleEntry) error {
	var order *exchange.Order
	if entry.ClientOrderID != "" {
		var err error
		order, err = s.lookupEntryOrder(ctx, entry)
		if err != nil {
			return fmt.Errorf("query order by persisted client ID: %w", err)
		}
	} else {
		raw, err := s.exchange.GetOrder(ctx, s.strategyCfg.Symbol, entry.OrderID)
		if err != nil {
			return fmt.Errorf("query order: %w", err)
		}
		var ok bool
		order, ok = dcaExchangeOrder(raw)
		if !ok || order == nil {
			return fmt.Errorf("exchange returned unsupported or missing order evidence (%T)", raw)
		}
	}
	if order == nil {
		return fmt.Errorf("exchange did not find the persisted order; absence does not prove submission rejection")
	}
	if entry.ClientOrderID != "" {
		returnedCID := utils.RemoveBrokerPrefix(strings.ToLower(s.exchange.GetName()), order.ClientOrderID)
		if returnedCID != entry.ClientOrderID {
			return fmt.Errorf("exchange order client ID does not match persisted entry intent")
		}
	}
	if entry.OrderID == 0 {
		if order.OrderID <= 0 {
			return fmt.Errorf("client-ID query returned an invalid exchange order ID")
		}
		s.mu.Lock()
		matched := false
		for _, persisted := range s.entries {
			if persisted != nil && persisted.ClientOrderID == entry.ClientOrderID {
				if persisted.OrderID != 0 && persisted.OrderID != order.OrderID {
					s.mu.Unlock()
					return fmt.Errorf("persisted entry order ID changed during reconciliation")
				}
				persisted.OrderID = order.OrderID
				matched = true
				break
			}
		}
		if !matched {
			s.mu.Unlock()
			return fmt.Errorf("persisted entry intent changed during reconciliation")
		}
		if err := s.persistRuntimeStateLocked(); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("persist reconciled entry order ID: %w", err)
		}
		s.mu.Unlock()
		entry.OrderID = order.OrderID
	}
	wantSide := exchange.SideBuy
	if s.direction == "SHORT" {
		wantSide = exchange.SideSell
	}
	decimals := s.exchange.GetQuantityDecimals()
	if decimals < 0 || decimals > 18 {
		return fmt.Errorf("invalid exchange quantity precision %d", decimals)
	}
	tolerance := math.Max(entryQtyEpsilon, math.Pow10(-decimals)*1.01)
	if order.OrderID != entry.OrderID || !strings.EqualFold(order.Symbol, s.strategyCfg.Symbol) || order.Side != wantSide ||
		!finiteNumber(order.Quantity) || order.Quantity <= 0 || math.Abs(order.Quantity-entry.RequestedQuantity) > tolerance ||
		!finiteNumber(order.ExecutedQty) || order.ExecutedQty < 0 || order.ExecutedQty > order.Quantity+tolerance ||
		order.ExecutedQty > entry.RequestedQuantity+tolerance || order.ExecutedQty+tolerance < entry.FillProgress.Quantity {
		return fmt.Errorf("exchange order identity or quantities conflict with persisted intent")
	}
	status := strings.ToUpper(strings.TrimSpace(string(order.Status)))
	switch status {
	case "NEW", "PARTIALLY_FILLED", "FILLED", "FULLY_FILLED", "CLOSED", "CANCELED", "CANCELLED", "REJECTED", "EXPIRED", "FAILED":
	default:
		return fmt.Errorf("unrecognized exchange order status %q", order.Status)
	}
	if status == "NEW" && order.ExecutedQty > tolerance || status == "PARTIALLY_FILLED" && order.ExecutedQty <= 0 ||
		(status == "FILLED" || status == "FULLY_FILLED" || status == "CLOSED") && order.ExecutedQty <= 0 {
		return fmt.Errorf("order status conflicts with cumulative execution")
	}
	update := &position.OrderUpdate{OrderID: order.OrderID, Symbol: order.Symbol, Side: string(order.Side), Status: status,
		ExecutedQty: order.ExecutedQty, AvgPrice: order.AvgPrice}
	if order.ExecutedQty > entry.FillProgress.Quantity {
		fee, averagePrice, err := s.reconcileEntryFills(ctx, s.strategyCfg.Symbol, order, entry.FillProgress)
		if err != nil {
			return err
		}
		update.Commission = fee
		update.CommissionAsset = s.exchange.GetQuoteAsset()
		update.AvgPrice = averagePrice
		update.CommissionKnown = true
	}
	if status == "NEW" || status == "PARTIALLY_FILLED" || signalOrderStatusTerminal(status) || order.ExecutedQty > entry.FillProgress.Quantity {
		if err := s.OnOrderUpdate(update); err != nil {
			return fmt.Errorf("apply recovered order state: %w", err)
		}
	}
	return nil
}

func (s *MartingaleStrategy) lookupEntryOrder(ctx context.Context, entry MartingaleEntry) (*exchange.Order, error) {
	if entry.OrderID > 0 {
		raw, err := s.exchange.GetOrder(ctx, s.strategyCfg.Symbol, entry.OrderID)
		if err == nil {
			if order, ok := dcaExchangeOrder(raw); ok && order != nil {
				return order, nil
			}
		}
	}
	if query, ok := s.exchange.(martingaleCloseOrderByClientID); ok {
		order, err := query.GetOrderByClientOrderID(ctx, s.strategyCfg.Symbol, entry.ClientOrderID)
		if err == nil && order != nil {
			if entry.OrderID > 0 && order.OrderID != entry.OrderID {
				return nil, fmt.Errorf("client-ID lookup returned a conflicting order ID")
			}
			return order, nil
		}
	}
	openOrdersRaw, err := s.exchange.GetOpenOrders(ctx, s.strategyCfg.Symbol)
	if err != nil {
		return nil, fmt.Errorf("query open orders: %w", err)
	}
	var openOrders []*exchange.Order
	switch orders := openOrdersRaw.(type) {
	case []*exchange.Order:
		openOrders = orders
	case []exchange.Order:
		openOrders = make([]*exchange.Order, len(orders))
		for i := range orders {
			openOrders[i] = &orders[i]
		}
	default:
		return nil, fmt.Errorf("unsupported open-order response %T", openOrdersRaw)
	}
	var matched *exchange.Order
	for _, candidate := range openOrders {
		if candidate == nil || utils.RemoveBrokerPrefix(strings.ToLower(s.exchange.GetName()), candidate.ClientOrderID) != entry.ClientOrderID {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("multiple open orders share entry client ID %s", entry.ClientOrderID)
		}
		matched = candidate
	}
	if matched != nil && entry.OrderID > 0 && matched.OrderID != entry.OrderID {
		return nil, fmt.Errorf("open-order lookup returned a conflicting order ID")
	}
	return matched, nil
}

func (s *MartingaleStrategy) reconcileEntryFills(ctx context.Context, symbol string, order *exchange.Order, progress position.FillProgress) (float64, float64, error) {
	raw, err := s.exchange.GetOrderFills(ctx, symbol, order.OrderID)
	if err != nil {
		return 0, 0, fmt.Errorf("query entry order fills: %w", err)
	}
	fills, ok := dcaExchangeOrderFills(raw)
	if !ok || len(fills) == 0 {
		return 0, 0, fmt.Errorf("new cumulative execution has no supported fill evidence (%T)", raw)
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
	var qty, notional, prefixQty, prefixNotional, addedFee float64
	for _, fill := range fills {
		if fill == nil || strings.TrimSpace(fill.TradeID) == "" || fill.OrderID != 0 && fill.OrderID != order.OrderID ||
			fill.Symbol != "" && !strings.EqualFold(fill.Symbol, symbol) || fill.Side != "" && fill.Side != order.Side ||
			!finiteNumber(fill.Price) || fill.Price <= 0 || !finiteNumber(fill.Quantity) || fill.Quantity <= 0 || !finiteNumber(fill.Commission) || fill.BaseFeeQty != 0 {
			return 0, 0, fmt.Errorf("entry order returned invalid or unsupported fill evidence")
		}
		if _, exists := seen[fill.TradeID]; exists {
			return 0, 0, fmt.Errorf("entry order returned duplicate trade ID %q", fill.TradeID)
		}
		seen[fill.TradeID] = struct{}{}
		qty += fill.Quantity
		notional += fill.Price * fill.Quantity
		if prefixQty < progress.Quantity-entryQtyEpsilon {
			if prefixQty+fill.Quantity > progress.Quantity+entryQtyEpsilon {
				return 0, 0, fmt.Errorf("persisted entry progress splits an exchange fill")
			}
			prefixQty += fill.Quantity
			prefixNotional += fill.Price * fill.Quantity
			continue
		}
		converted, known := 0.0, false
		if fill.CommissionQuoteKnown {
			converted, known = fill.CommissionQuote, finiteNumber(fill.CommissionQuote) && fill.CommissionQuote >= 0
		} else if fill.Commission == 0 && strings.TrimSpace(fill.CommissionAsset) == "" {
			return 0, 0, fmt.Errorf("entry fill %s has no verifiable commission evidence", fill.TradeID)
		} else {
			converted, known = commissionInQuote(s.exchange, fill.Commission, fill.CommissionAsset, fill.Price)
		}
		if !known {
			return 0, 0, fmt.Errorf("entry fill %s commission cannot be valued in %s", fill.TradeID, s.exchange.GetQuoteAsset())
		}
		addedFee += converted
	}
	tolerance := math.Max(entryQtyEpsilon, order.ExecutedQty*1e-8)
	if math.Abs(qty-order.ExecutedQty) > tolerance || math.Abs(prefixQty-progress.Quantity) > tolerance ||
		progress.Quantity > 0 && math.Abs(prefixNotional-progress.Notional) > math.Max(1e-8, progress.Notional*1e-8) {
		return 0, 0, fmt.Errorf("entry fill history does not reconcile with cumulative execution")
	}
	averagePrice := notional / qty
	if !finiteNumber(averagePrice) || averagePrice <= 0 || !finiteNumber(order.AvgPrice) || order.AvgPrice > 0 && math.Abs(averagePrice-order.AvgPrice) > math.Max(1e-8, order.AvgPrice*1e-8) {
		return 0, 0, fmt.Errorf("entry fill notional does not match exchange order")
	}
	return addedFee, averagePrice, nil
}
