package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/position"
	"quantmesh/utils"
)

const futuresHedgeRuntimeStateVersion = 1

type futuresHedgePendingOrder struct {
	ClientOrderID string  `json:"client_order_id"`
	OrderID       int64   `json:"order_id,omitempty"`
	Side          string  `json:"side"`
	Quantity      float64 `json:"quantity"`
	ExecutedQty   float64 `json:"executed_qty"`
}

type futuresHedgeRuntimeState struct {
	BotID    string                    `json:"bot_id"`
	Strategy string                    `json:"strategy"`
	GroupID  string                    `json:"group_id"`
	Symbol   string                    `json:"symbol"`
	Pending  *futuresHedgePendingOrder `json:"pending,omitempty"`
}

type futuresHedgeOrderTracker struct {
	mu           sync.Mutex
	store        RuntimeStateStore
	botID        string
	strategy     string
	groupID      string
	symbol       string
	exchangeName string
	pending      *futuresHedgePendingOrder
}

func newFuturesHedgeOrderTracker(cfg *config.Config, strategyName, groupID, symbol, exchangeName string) *futuresHedgeOrderTracker {
	botID := ""
	if cfg != nil {
		botID = cfg.Trading.BotID
	}
	return &futuresHedgeOrderTracker{botID: botID, strategy: strategyName, groupID: groupID, symbol: symbol,
		exchangeName: strings.ToLower(strings.TrimSpace(exchangeName))}
}

func (t *futuresHedgeOrderTracker) SetStore(store RuntimeStateStore) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.store = store
}

func (t *futuresHedgeOrderTracker) Begin(side string, quantity float64) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending != nil {
		return "", fmt.Errorf("%s has unresolved hedge order %s; refusing another order", t.strategy, t.pending.ClientOrderID)
	}
	if (side != "BUY" && side != "SELL") || !finiteNumber(quantity) || quantity <= 0 {
		return "", fmt.Errorf("%s hedge order has invalid side or quantity", t.strategy)
	}
	if t.store == nil {
		return "", fmt.Errorf("%s runtime state store is unavailable", t.strategy)
	}
	pending := &futuresHedgePendingOrder{ClientOrderID: utils.NewCompactOrderID(), Side: side, Quantity: quantity}
	t.pending = pending
	if err := t.persistLocked(); err != nil {
		t.pending = nil
		return "", fmt.Errorf("persist %s hedge order intent before submission: %w", t.strategy, err)
	}
	return pending.ClientOrderID, nil
}

func (t *futuresHedgeOrderTracker) Bind(clientOrderID string, order *position.Order) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil {
		return nil // A terminal order update may precede the REST acknowledgement.
	}
	if t.pending.ClientOrderID != clientOrderID || order == nil || order.OrderID <= 0 ||
		(t.pending.OrderID > 0 && t.pending.OrderID != order.OrderID) ||
		(order.ClientOrderID != "" && t.normalizeClientOrderID(order.ClientOrderID) != clientOrderID) ||
		(order.Symbol != "" && !strings.EqualFold(order.Symbol, t.symbol)) ||
		(order.Side != "" && !strings.EqualFold(order.Side, t.pending.Side)) ||
		!finiteNumber(order.Quantity) || order.Quantity <= 0 ||
		math.Abs(order.Quantity-t.pending.Quantity) > math.Max(1e-10, t.pending.Quantity*1e-8) {
		return fmt.Errorf("%s hedge order acknowledgement conflicts with durable intent", t.strategy)
	}
	previous := *t.pending
	t.pending.OrderID = order.OrderID
	if err := t.persistLocked(); err != nil {
		*t.pending = previous
		return fmt.Errorf("persist %s hedge order acknowledgement: %w", t.strategy, err)
	}
	return nil
}

func (t *futuresHedgeOrderTracker) SubmissionFailed(clientOrderID string, submitErr error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil || t.pending.ClientOrderID != clientOrderID {
		return nil
	}
	if errors.Is(submitErr, execution.ErrOrderUnknown) {
		return nil // Keep both the CID and any reservation until exact reconciliation.
	}
	previous := t.pending
	t.pending = nil
	if err := t.persistLocked(); err != nil {
		t.pending = previous
		return fmt.Errorf("persist rejected %s hedge order rollback: %w", t.strategy, err)
	}
	return nil
}

func (t *futuresHedgeOrderTracker) OnOrderUpdate(update *position.OrderUpdate) error {
	if update == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	pending := t.pending
	if pending == nil || (update.OrderID != pending.OrderID && t.normalizeClientOrderID(update.ClientOrderID) != pending.ClientOrderID) {
		return nil
	}
	if update.OrderID <= 0 || (pending.OrderID > 0 && update.OrderID != pending.OrderID) ||
		(update.ClientOrderID != "" && t.normalizeClientOrderID(update.ClientOrderID) != pending.ClientOrderID) ||
		(update.Symbol != "" && !strings.EqualFold(update.Symbol, t.symbol)) ||
		(update.Side != "" && !strings.EqualFold(update.Side, pending.Side)) ||
		!finiteNumber(update.ExecutedQty) || update.ExecutedQty < pending.ExecutedQty || update.ExecutedQty > pending.Quantity {
		return fmt.Errorf("%s hedge order update conflicts with durable intent", t.strategy)
	}
	status := strings.ToUpper(strings.TrimSpace(update.Status))
	switch status {
	case "NEW", "PARTIALLY_FILLED", "FILLED", "CANCELED", "CANCELLED", "REJECTED", "EXPIRED", "FAILED":
	default:
		return fmt.Errorf("%s hedge order has unrecognized status %q", t.strategy, update.Status)
	}
	if status == "FILLED" && update.ExecutedQty <= 0 {
		return fmt.Errorf("%s hedge order %d reports FILLED without positive execution", t.strategy, update.OrderID)
	}
	previous := *pending
	pending.OrderID = update.OrderID
	pending.ExecutedQty = update.ExecutedQty
	if isTerminalHedgeOrderStatus(status) {
		t.pending = nil
	}
	if err := t.persistLocked(); err != nil {
		t.pending = pending
		*pending = previous
		return fmt.Errorf("persist %s hedge order update: %w", t.strategy, err)
	}
	return nil
}

func (t *futuresHedgeOrderTracker) RestoreAndReconcile(ctx context.Context, venue position.IExchange) error {
	t.mu.Lock()
	if t.store == nil {
		t.mu.Unlock()
		return fmt.Errorf("%s runtime state store is unavailable; refusing to start without hedge-order recovery", t.strategy)
	}
	version, payload, found, err := t.store.LoadRuntimeState(t.strategy)
	if err != nil {
		t.mu.Unlock()
		return fmt.Errorf("load %s hedge runtime state: %w", t.strategy, err)
	}
	if !found {
		t.mu.Unlock()
		return nil
	}
	if version != futuresHedgeRuntimeStateVersion {
		t.mu.Unlock()
		return fmt.Errorf("unsupported %s hedge runtime state version %d", t.strategy, version)
	}
	var state futuresHedgeRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		t.mu.Unlock()
		return fmt.Errorf("decode %s hedge runtime state: %w", t.strategy, err)
	}
	if state.BotID != t.botID || state.Strategy != t.strategy || state.GroupID != t.groupID || state.Symbol != t.symbol ||
		state.Pending != nil && !t.validPending(state.Pending) {
		t.mu.Unlock()
		return fmt.Errorf("%s hedge runtime state identity or order is invalid", t.strategy)
	}
	t.pending = state.Pending
	var pending *futuresHedgePendingOrder
	if t.pending != nil {
		copyPending := *t.pending
		pending = &copyPending
	}
	t.mu.Unlock()
	if pending == nil {
		return nil
	}
	query, ok := venue.(interface {
		GetOrderByClientOrderID(context.Context, string, string) (*exchange.Order, error)
	})
	if !ok {
		return fmt.Errorf("%s exchange cannot reconcile pending hedge order by client ID", t.strategy)
	}
	order, err := query.GetOrderByClientOrderID(ctx, t.symbol, pending.ClientOrderID)
	if err != nil {
		return fmt.Errorf("query persisted %s hedge order %s: %w", t.strategy, pending.ClientOrderID, err)
	}
	if order == nil {
		return fmt.Errorf("persisted %s hedge order %s has no authoritative exchange evidence", t.strategy, pending.ClientOrderID)
	}
	if order.OrderID <= 0 || t.normalizeClientOrderID(order.ClientOrderID) != pending.ClientOrderID || !strings.EqualFold(order.Symbol, t.symbol) ||
		!strings.EqualFold(string(order.Side), pending.Side) || !finiteNumber(order.Quantity) ||
		math.Abs(order.Quantity-pending.Quantity) > math.Max(1e-10, pending.Quantity*1e-8) ||
		!finiteNumber(order.ExecutedQty) || order.ExecutedQty < pending.ExecutedQty || order.ExecutedQty > pending.Quantity {
		return fmt.Errorf("exchange hedge order conflicts with persisted %s intent", t.strategy)
	}
	status := strings.ToUpper(strings.TrimSpace(string(order.Status)))
	if status == "FILLED" && order.ExecutedQty <= 0 {
		return fmt.Errorf("exchange hedge order reports FILLED without positive execution")
	}
	if status != "NEW" && status != "PARTIALLY_FILLED" && !isTerminalHedgeOrderStatus(status) {
		return fmt.Errorf("exchange returned unrecognized %s hedge order status %q", t.strategy, order.Status)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pending == nil || t.pending.ClientOrderID != pending.ClientOrderID {
		return nil
	}
	if order.ExecutedQty < t.pending.ExecutedQty {
		return fmt.Errorf("exchange %s hedge order snapshot is older than its latest update", t.strategy)
	}
	previous := *t.pending
	if isTerminalHedgeOrderStatus(status) {
		t.pending = nil
	} else {
		t.pending.OrderID = order.OrderID
		t.pending.ExecutedQty = order.ExecutedQty
	}
	if err := t.persistLocked(); err != nil {
		t.pending = &previous
		return fmt.Errorf("persist reconciled %s hedge order: %w", t.strategy, err)
	}
	return nil
}

func (t *futuresHedgeOrderTracker) validPending(pending *futuresHedgePendingOrder) bool {
	return pending != nil && pending.ClientOrderID != "" && pending.Side != "" &&
		(pending.Side == "BUY" || pending.Side == "SELL") && finiteNumber(pending.Quantity) && pending.Quantity > 0 &&
		finiteNumber(pending.ExecutedQty) && pending.ExecutedQty >= 0 && pending.ExecutedQty <= pending.Quantity && pending.OrderID >= 0
}

func (t *futuresHedgeOrderTracker) persistLocked() error {
	if t.store == nil {
		return fmt.Errorf("runtime state store is unavailable")
	}
	state := futuresHedgeRuntimeState{BotID: t.botID, Strategy: t.strategy, GroupID: t.groupID, Symbol: t.symbol}
	if t.pending != nil {
		copyPending := *t.pending
		state.Pending = &copyPending
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return t.store.SaveRuntimeState(t.strategy, futuresHedgeRuntimeStateVersion, string(payload))
}

func (t *futuresHedgeOrderTracker) normalizeClientOrderID(clientOrderID string) string {
	return utils.RemoveBrokerPrefix(t.exchangeName, clientOrderID)
}

func futuresHedgeExchangeName(venue position.IExchange) string {
	if venue == nil {
		return ""
	}
	return venue.GetName()
}

func isTerminalHedgeOrderStatus(status string) bool {
	switch status {
	case "FILLED", "CANCELED", "CANCELLED", "REJECTED", "EXPIRED", "FAILED":
		return true
	default:
		return false
	}
}
