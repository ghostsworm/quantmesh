package strategy

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/position"
)

const dcaFillEvidenceTimeout = 15 * time.Second

type dcaOrderIntent struct {
	orderID       int64
	clientOrderID string
	symbol        string
	side          exchange.Side
	quantity      float64
	progress      position.FillProgress
	baseFeeQty    float64
	close         bool
}

// resolveUnverifiedCommission replaces order-stream fee placeholders with
// complete per-fill evidence before DCA mutates its inventory or ledger.
func (s *DCAEnhancedStrategy) resolveUnverifiedCommission(update *position.OrderUpdate) error {
	if update.CommissionKnown || !finiteNumber(update.ExecutedQty) || update.ExecutedQty <= 0 {
		return nil
	}

	s.mu.RLock()
	var intent dcaOrderIntent
	found := false
	for _, layer := range s.layers {
		clientMatch := layer != nil && layer.ClientOrderID != "" && update.ClientOrderID != "" &&
			s.normalizeClientOrderID(update.ClientOrderID) == layer.ClientOrderID
		if layer != nil && (layer.OrderID > 0 && layer.OrderID == update.OrderID || clientMatch) {
			intent = dcaOrderIntent{orderID: layer.OrderID, clientOrderID: layer.ClientOrderID, symbol: s.strategyCfg.Symbol, side: exchange.SideBuy,
				quantity: layer.RequestedQuantity, progress: layer.FillProgress, baseFeeQty: layer.EntryBaseFeeQty}
			found = true
			break
		}
	}
	closeClientMatch := s.closeClientOrderID != "" && update.ClientOrderID != "" &&
		s.normalizeClientOrderID(update.ClientOrderID) == s.closeClientOrderID
	if s.isClosing && (s.closeOrderID > 0 && s.closeOrderID == update.OrderID || closeClientMatch) {
		intent = dcaOrderIntent{orderID: s.closeOrderID, clientOrderID: s.closeClientOrderID, symbol: s.strategyCfg.Symbol, side: exchange.SideSell,
			quantity: s.closeRequestedQty, progress: s.closeProgress, baseFeeQty: s.closeBaseFeeQty, close: true}
		found = true
	}
	s.mu.RUnlock()
	if !found || update.ExecutedQty <= intent.progress.Quantity {
		return nil
	}
	if intent.orderID <= 0 {
		intent.orderID = update.OrderID
	}
	// Let the normal intent validator handle malformed/non-valued updates first;
	// requesting fills for an impossible cumulative execution only masks the real
	// contradiction and cannot make it safe to account.
	if !finiteNumber(update.ExecutedQty) || !finiteNumber(update.AvgPrice) || update.AvgPrice <= 0 || update.ExecutedQty > intent.quantity+entryQtyEpsilon {
		return nil
	}
	if s.exchange == nil || intent.orderID <= 0 || intent.quantity <= 0 {
		return fmt.Errorf("DCA cannot verify commission without exchange and persisted order intent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), dcaFillEvidenceTimeout)
	defer cancel()
	order := &exchange.Order{OrderID: intent.orderID, Symbol: intent.symbol, Side: intent.side,
		Quantity: intent.quantity, ExecutedQty: update.ExecutedQty, AvgPrice: update.AvgPrice,
		Status: exchange.OrderStatus(strings.ToUpper(strings.TrimSpace(update.Status)))}
	feeQuote, baseFeeQty, averagePrice, err := s.reconcilePersistedOrderFills(ctx, intent, order)
	if err != nil {
		s.requireDCAOrderReconciliation(update, "DCA commission evidence is incomplete: "+err.Error())
		return fmt.Errorf("verify DCA order %d fill fees before accounting: %w", update.OrderID, err)
	}
	update.Commission = feeQuote
	update.CommissionAsset = s.exchange.GetQuoteAsset()
	update.BaseFeeQty = baseFeeQty
	update.AvgPrice = averagePrice
	update.CommissionKnown = true
	return nil
}

// reconcilePersistedOrders settles orders whose callbacks may have been missed
// while the process was offline. Any missing or contradictory exchange evidence
// aborts startup so the strategy cannot trade against stale inventory.
func (s *DCAEnhancedStrategy) reconcilePersistedOrders(ctx context.Context) error {
	s.mu.RLock()
	intents := make([]dcaOrderIntent, 0, len(s.layers)+1)
	for _, layer := range s.layers {
		if layer != nil && (layer.Status == entryStatusPending || layer.Status == entryStatusPartiallyFilled || layer.Status == position.OrderStatusUnknown) {
			intents = append(intents, dcaOrderIntent{orderID: layer.OrderID, clientOrderID: layer.ClientOrderID, symbol: s.strategyCfg.Symbol,
				side: exchange.SideBuy, quantity: layer.RequestedQuantity, progress: layer.FillProgress, baseFeeQty: layer.EntryBaseFeeQty})
		}
	}
	if s.isClosing {
		intents = append(intents, dcaOrderIntent{orderID: s.closeOrderID, clientOrderID: s.closeClientOrderID, symbol: s.strategyCfg.Symbol,
			side: exchange.SideSell, quantity: s.closeRequestedQty, progress: s.closeProgress, baseFeeQty: s.closeBaseFeeQty, close: true})
	}
	s.mu.RUnlock()
	if len(intents) == 0 {
		return nil
	}
	if s.exchange == nil {
		return fmt.Errorf("DCA exchange is unavailable for persisted order reconciliation")
	}
	for _, intent := range intents {
		if (intent.orderID <= 0 && intent.clientOrderID == "") || intent.quantity <= 0 {
			return fmt.Errorf("DCA persisted order has invalid identity or requested quantity")
		}
		if err := s.reconcilePersistedOrder(ctx, intent); err != nil {
			return fmt.Errorf("reconcile DCA order %d: %w", intent.orderID, err)
		}
	}
	return nil
}

func (s *DCAEnhancedStrategy) reconcilePersistedOrder(ctx context.Context, intent dcaOrderIntent) error {
	var raw interface{}
	var err error
	if intent.orderID > 0 {
		raw, err = s.exchange.GetOrder(ctx, intent.symbol, intent.orderID)
	} else if query, ok := s.exchange.(exchange.OrderByClientIDQuerier); ok {
		raw, err = query.GetOrderByClientOrderID(ctx, intent.symbol, intent.clientOrderID)
	} else {
		return fmt.Errorf("exchange does not support recovery by persisted client order ID")
	}
	if err != nil {
		return fmt.Errorf("query order: %w", err)
	}
	order, ok := dcaExchangeOrder(raw)
	if !ok || order == nil {
		return fmt.Errorf("exchange returned unsupported or missing order evidence (%T)", raw)
	}
	decimals := s.exchange.GetQuantityDecimals()
	if decimals < 0 || decimals > 18 {
		return fmt.Errorf("invalid exchange quantity precision %d", decimals)
	}
	tolerance := math.Max(entryQtyEpsilon, math.Pow10(-decimals)*1.01)
	if order.OrderID <= 0 || intent.orderID > 0 && order.OrderID != intent.orderID || !strings.EqualFold(order.Symbol, intent.symbol) || order.Side != intent.side ||
		order.ClientOrderID != "" && intent.clientOrderID != "" && s.normalizeClientOrderID(order.ClientOrderID) != intent.clientOrderID ||
		!finiteNumber(order.Quantity) || order.Quantity <= 0 || math.Abs(order.Quantity-intent.quantity) > tolerance ||
		!finiteNumber(order.ExecutedQty) || order.ExecutedQty < 0 || order.ExecutedQty > order.Quantity+tolerance ||
		order.ExecutedQty > intent.quantity+tolerance || order.ExecutedQty+tolerance < intent.progress.Quantity {
		return fmt.Errorf("exchange order identity or quantities conflict with persisted intent")
	}
	status := strings.ToUpper(strings.TrimSpace(string(order.Status)))
	switch status {
	case "NEW", "PARTIALLY_FILLED", "FILLED", "FULLY_FILLED", "CANCELED", "CANCELLED", "REJECTED", "EXPIRED", "FAILED":
	default:
		return fmt.Errorf("unrecognized exchange order status %q", order.Status)
	}
	if status == "NEW" && order.ExecutedQty > tolerance || status == "PARTIALLY_FILLED" && order.ExecutedQty <= 0 ||
		(status == "FILLED" || status == "FULLY_FILLED") && order.ExecutedQty <= 0 {
		return fmt.Errorf("order status conflicts with cumulative execution")
	}
	update := &position.OrderUpdate{OrderID: order.OrderID, ClientOrderID: intent.clientOrderID, Symbol: order.Symbol, Side: string(order.Side), Status: status,
		ExecutedQty: order.ExecutedQty, AvgPrice: order.AvgPrice, CommissionKnown: true}
	if order.ExecutedQty > intent.progress.Quantity {
		fee, baseFeeQty, averagePrice, err := s.reconcilePersistedOrderFills(ctx, intent, order)
		if err != nil {
			return err
		}
		update.Commission = fee
		update.CommissionAsset = s.exchange.GetQuoteAsset()
		update.BaseFeeQty = baseFeeQty
		update.AvgPrice = averagePrice
	}
	if status == "NEW" || status == "PARTIALLY_FILLED" || isDCAOrderTerminal(status) || order.ExecutedQty > intent.progress.Quantity {
		if err := s.OnOrderUpdate(update); err != nil {
			return fmt.Errorf("apply recovered order state: %w", err)
		}
	}
	return nil
}

func dcaExchangeOrder(raw interface{}) (*exchange.Order, bool) {
	switch order := raw.(type) {
	case *exchange.Order:
		return order, true
	case exchange.Order:
		return &order, true
	default:
		return nil, false
	}
}

func isDCAOrderTerminal(status string) bool {
	switch status {
	case "FILLED", "FULLY_FILLED", "CANCELED", "CANCELLED", "REJECTED", "EXPIRED", "FAILED":
		return true
	default:
		return false
	}
}

func (s *DCAEnhancedStrategy) reconcilePersistedOrderFills(ctx context.Context, intent dcaOrderIntent, order *exchange.Order) (float64, float64, float64, error) {
	raw, err := s.exchange.GetOrderFills(ctx, intent.symbol, order.OrderID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("query order fills: %w", err)
	}
	fills, ok := dcaExchangeOrderFills(raw)
	if !ok || len(fills) == 0 {
		return 0, 0, 0, fmt.Errorf("new cumulative execution has no supported fill evidence (%T)", raw)
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
	var quantity, notional, prefixQty, prefixNotional, prefixBaseFee, feeQuote, addedBaseFee float64
	var sameTimeGroupStart float64
	var sameTimeGroupTime int64
	hasSameTimeGroup := false
	for _, fill := range fills {
		if fill == nil || strings.TrimSpace(fill.TradeID) == "" || fill.OrderID != 0 && fill.OrderID != order.OrderID ||
			fill.Symbol != "" && !strings.EqualFold(fill.Symbol, intent.symbol) || fill.Side != "" && fill.Side != intent.side ||
			!finiteNumber(fill.Price) || fill.Price <= 0 || !finiteNumber(fill.Quantity) || fill.Quantity <= 0 ||
			!finiteNumber(fill.Commission) || !finiteNumber(fill.BaseFeeQty) || fill.BaseFeeQty < 0 || fill.BaseFeeQty > fill.Quantity+entryQtyEpsilon {
			return 0, 0, 0, fmt.Errorf("order returned invalid fill evidence")
		}
		if intent.progress.Quantity > 0 && fill.TradeTime <= 0 {
			return 0, 0, 0, fmt.Errorf("fill %s has no usable timestamp for persisted-prefix reconciliation", fill.TradeID)
		}
		if intent.progress.Quantity > 0 {
			if !hasSameTimeGroup {
				sameTimeGroupStart = quantity
				sameTimeGroupTime = fill.TradeTime
				hasSameTimeGroup = true
			} else if fill.TradeTime != sameTimeGroupTime {
				if intent.progress.Quantity > sameTimeGroupStart+entryQtyEpsilon && intent.progress.Quantity < quantity-entryQtyEpsilon {
					return 0, 0, 0, fmt.Errorf("persisted fill cursor splits trades with identical timestamps")
				}
				sameTimeGroupStart = quantity
				sameTimeGroupTime = fill.TradeTime
			}
		}
		if _, duplicate := seen[fill.TradeID]; duplicate {
			return 0, 0, 0, fmt.Errorf("order returned duplicate trade ID %q", fill.TradeID)
		}
		if fill.BaseFeeQty > 0 && (!s.supportsSpotBaseFee() || intent.close && intent.side != exchange.SideSell || !intent.close && intent.side != exchange.SideBuy) {
			return 0, 0, 0, fmt.Errorf("base-asset fee is unsupported for this DCA order recovery")
		}
		if s.hasUnmappedBaseFee(fill.Commission, fill.CommissionAsset, fill.BaseFeeQty) {
			return 0, 0, 0, fmt.Errorf("fill %s reports a base-asset commission without its inventory fee quantity", fill.TradeID)
		}
		if fill.BaseFeeQty > 0 && fill.Commission <= 0 {
			return 0, 0, 0, fmt.Errorf("fill %s reports base-fee inventory without positive commission evidence", fill.TradeID)
		}
		if s.hasMismatchedBaseFeeAsset(fill.CommissionAsset, fill.BaseFeeQty) {
			return 0, 0, 0, fmt.Errorf("fill %s base-asset commission denomination conflicts with its inventory fee quantity", fill.TradeID)
		}
		seen[fill.TradeID] = struct{}{}
		quantity += fill.Quantity
		notional += fill.Price * fill.Quantity
		if prefixQty < intent.progress.Quantity-entryQtyEpsilon {
			if prefixQty+fill.Quantity > intent.progress.Quantity+entryQtyEpsilon {
				return 0, 0, 0, fmt.Errorf("persisted fill cursor splits an exchange trade")
			}
			prefixQty += fill.Quantity
			prefixNotional += fill.Price * fill.Quantity
			prefixBaseFee += fill.BaseFeeQty
			continue
		}
		converted, known := 0.0, false
		if fill.CommissionQuoteKnown {
			converted, known = fill.CommissionQuote, finiteNumber(fill.CommissionQuote)
		} else if fill.Commission == 0 && strings.TrimSpace(fill.CommissionAsset) == "" {
			return 0, 0, 0, fmt.Errorf("fill %s has no verifiable fee denomination", fill.TradeID)
		} else {
			converted, known = commissionInQuote(s.exchange, fill.Commission, fill.CommissionAsset, fill.Price)
		}
		if !known {
			return 0, 0, 0, fmt.Errorf("fill %s commission cannot be valued in quote asset", fill.TradeID)
		}
		feeQuote += converted
		addedBaseFee += fill.BaseFeeQty
	}
	if hasSameTimeGroup && intent.progress.Quantity > sameTimeGroupStart+entryQtyEpsilon && intent.progress.Quantity < quantity-entryQtyEpsilon {
		return 0, 0, 0, fmt.Errorf("persisted fill cursor splits trades with identical timestamps")
	}
	tolerance := math.Max(entryQtyEpsilon, order.ExecutedQty*1e-8)
	if math.Abs(quantity-order.ExecutedQty) > tolerance || math.Abs(prefixQty-intent.progress.Quantity) > tolerance ||
		math.Abs(prefixBaseFee-intent.baseFeeQty) > tolerance ||
		intent.progress.Quantity > 0 && math.Abs(prefixNotional-intent.progress.Notional) > math.Max(1e-8, intent.progress.Notional*1e-8) {
		return 0, 0, 0, fmt.Errorf("fill history does not reconcile to the order and persisted cursor")
	}
	average := notional / quantity
	if !finiteNumber(feeQuote) || !finiteNumber(notional) || !finiteNumber(average) || average <= 0 || !finiteNumber(order.AvgPrice) ||
		order.AvgPrice > 0 && math.Abs(average-order.AvgPrice) > math.Max(1e-8, order.AvgPrice*1e-8) {
		return 0, 0, 0, fmt.Errorf("fill notional does not reconcile to exchange order average")
	}
	return feeQuote, addedBaseFee, average, nil
}

func dcaExchangeOrderFills(raw interface{}) ([]*exchange.OrderFill, bool) {
	switch fills := raw.(type) {
	case []*exchange.OrderFill:
		return fills, true
	case []exchange.OrderFill:
		result := make([]*exchange.OrderFill, len(fills))
		for i := range fills {
			result[i] = &fills[i]
		}
		return result, true
	default:
		return nil, false
	}
}
