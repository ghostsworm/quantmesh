package strategy

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"quantmesh/exchange"
	"quantmesh/position"
	"quantmesh/utils"
)

type martingaleCloseOrderByClientID interface {
	GetOrderByClientOrderID(context.Context, string, string) (*exchange.Order, error)
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
