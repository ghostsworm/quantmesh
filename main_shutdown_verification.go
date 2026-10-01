package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/utils"
)

var errShutdownCloseUnverified = errors.New("shutdown close requires reconciliation")
var errShutdownOrderEvidence = errors.New("shutdown order evidence is inconsistent")

func shutdownRuntimeScopeKey(rt *SymbolRuntime) string {
	// AccountID is a short API-key prefix in legacy runtimes, not proof of
	// account identity. Missing full identity must not dedupe another runtime.
	if rt.AccountScope == "" {
		return fmt.Sprintf("runtime:%p", rt)
	}
	return strings.ToLower(rt.Config.Exchange) + "|" + rt.AccountScope + "|" + rt.AccountMarketType + "|" + strings.ToUpper(rt.Config.Symbol)
}

func (rt *SymbolRuntime) markShutdownCloseUnverified(reason string) {
	rt.shutdownCloseUnverified.Store(&reason)
	if rt.SuperPositionManager != nil {
		rt.SuperPositionManager.OpeningGate().Block("shutdown_close_unverified")
	}
}

func (rt *SymbolRuntime) shutdownCloseUnverifiedReason() string {
	if rt == nil {
		return ""
	}
	if reason := rt.shutdownCloseUnverified.Load(); reason != nil {
		return *reason
	}
	return ""
}

// Query failures and malformed quantities are not proof of flatness. Preserve
// gross legs; opposite positions cannot cancel each other into a false zero.
func queryShutdownPositions(ctx context.Context, ex exchange.IExchange, symbol string) ([]*exchange.Position, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	positions, err := ex.GetPositions(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("query shutdown positions: %w", err)
	}
	if positions == nil {
		return nil, fmt.Errorf("query shutdown positions: exchange returned a nil snapshot; flatness is unverified")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, p := range positions {
		if p == nil {
			return nil, fmt.Errorf("shutdown positions contain nil entry")
		}
		if !strings.EqualFold(p.Symbol, symbol) {
			return nil, fmt.Errorf("shutdown position response contains unexpected symbol %q while verifying %s", p.Symbol, symbol)
		}
		if math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
			return nil, fmt.Errorf("shutdown position quantity is not finite")
		}
		switch strings.ToUpper(strings.TrimSpace(p.PositionSide)) {
		case "", "BOTH", "NET":
		case "LONG":
			if p.Size < 0 {
				return nil, fmt.Errorf("shutdown LONG position has negative signed quantity")
			}
		case "SHORT":
			if p.Size > 0 {
				return nil, fmt.Errorf("shutdown SHORT position has positive signed quantity")
			}
		default:
			return nil, fmt.Errorf("shutdown position side %q is unsupported", p.PositionSide)
		}
	}
	return nonZeroPositions(positions, symbol), nil
}

// Do not chase newly appearing/increasing exposure from another actor.
func validateShutdownResidual(before, after []*exchange.Position) error {
	var original, remaining [2]float64
	for n, positions := range [][]*exchange.Position{before, after} {
		for _, p := range positions {
			leg := 0
			if p.Size < 0 {
				leg = 1
			}
			if n == 0 {
				original[leg] += math.Abs(p.Size)
			} else {
				remaining[leg] += math.Abs(p.Size)
			}
		}
	}
	for leg := range original {
		if math.IsInf(original[leg], 0) || math.IsInf(remaining[leg], 0) || remaining[leg] > original[leg] {
			return fmt.Errorf("shutdown exposure increased or changed direction; reconciliation required")
		}
	}
	return nil
}

func waitShutdownPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// An explicit error accompanies every unverified result, including when no
// query ever succeeded. Empty last-known state alone never means success.
func waitPositionsFlat(ctx context.Context, ex exchange.IExchange, symbol string, opts shutdownCloseOptions) ([]*exchange.Position, error) {
	qctx, cancel := context.WithTimeout(ctx, opts.fillWait)
	defer cancel()
	var last []*exchange.Position
	var lastErr error
	for {
		positions, err := queryShutdownPositions(qctx, ex, symbol)
		if err == nil {
			last = positions
			if len(last) == 0 {
				return nil, nil
			}
			lastErr = fmt.Errorf("%d position entries remain", len(last))
		} else {
			lastErr = err
		}
		if err := waitShutdownPoll(qctx, opts.pollInterval); err != nil {
			return last, fmt.Errorf("shutdown flatness not verified: %w", errors.Join(lastErr, err))
		}
	}
}

type shutdownOwnedOrder struct {
	request  exchange.OrderRequest
	id       int64
	filled   float64
	quantity float64
}

func (o *shutdownOwnedOrder) observe(exName string, ord *exchange.Order) (bool, error) {
	if ord == nil || ord.OrderID <= 0 || (o.id > 0 && ord.OrderID != o.id) ||
		(ord.Symbol != "" && ord.Symbol != o.request.Symbol) || (ord.Side != "" && ord.Side != o.request.Side) ||
		(ord.ClientOrderID != "" && ord.ClientOrderID != o.request.ClientOrderID && ord.ClientOrderID != utils.AddBrokerPrefix(strings.ToLower(exName), o.request.ClientOrderID)) {
		return false, fmt.Errorf("shutdown order identity mismatch: %w", errShutdownOrderEvidence)
	}
	if math.IsNaN(ord.ExecutedQty) || math.IsInf(ord.ExecutedQty, 0) || ord.ExecutedQty < o.filled ||
		math.IsNaN(ord.Quantity) || math.IsInf(ord.Quantity, 0) || ord.Quantity < 0 || ord.Quantity > o.request.Quantity {
		return false, fmt.Errorf("shutdown order has invalid or regressing quantities: %w", errShutdownOrderEvidence)
	}
	expected := o.request.Quantity
	if o.quantity > 0 {
		expected = o.quantity
	}
	if ord.Quantity > 0 {
		if o.quantity > 0 && ord.Quantity != o.quantity {
			return false, fmt.Errorf("shutdown order quantity changed: %w", errShutdownOrderEvidence)
		}
		expected = ord.Quantity
	}
	if ord.ExecutedQty > expected || (ord.Status == exchange.OrderStatusFilled && (ord.ExecutedQty <= 0 || ord.ExecutedQty != expected)) {
		return false, fmt.Errorf("shutdown terminal fill does not match original quantity: %w", errShutdownOrderEvidence)
	}
	switch ord.Status {
	case exchange.OrderStatusNew, exchange.OrderStatusPartiallyFilled,
		exchange.OrderStatusFilled, exchange.OrderStatusCanceled, exchange.OrderStatusExpired, exchange.OrderStatusRejected:
	default:
		return false, fmt.Errorf("shutdown order status is unrecognized: %w", errShutdownOrderEvidence)
	}
	o.id, o.filled = ord.OrderID, ord.ExecutedQty
	if ord.Quantity > 0 {
		o.quantity = ord.Quantity
	}
	return isTerminalOrderStatus(ord.Status), nil
}

func awaitShutdownOrder(ctx context.Context, ex exchange.IExchange, owned *shutdownOwnedOrder, opts shutdownCloseOptions) error {
	qctx, cancel := context.WithTimeout(ctx, opts.fillWait)
	defer cancel()
	var lastErr error
	for {
		if err := qctx.Err(); err != nil {
			return errors.Join(lastErr, err)
		}
		ord, err := ex.GetOrder(qctx, owned.request.Symbol, owned.id)
		if err == nil {
			if err := qctx.Err(); err != nil {
				return err
			}
			terminal, err := owned.observe(ex.GetName(), ord)
			if err != nil {
				return err
			}
			if terminal {
				return nil
			}
			lastErr = fmt.Errorf("shutdown order %d remains nonterminal", owned.id)
		} else {
			lastErr = err
		}
		if err := waitShutdownPoll(qctx, opts.pollInterval); err != nil {
			return errors.Join(lastErr, err)
		}
	}
}

// Each submitted intent has a stable CID. An ambiguous submission is never
// replaced. Only a verified terminal order permits the next phase, and only
// this operation's exact order ID can be cancelled (never CancelAllOrders).
func submitAndVerifyShutdownOrder(ctx context.Context, ex exchange.IExchange, req exchange.OrderRequest, opts shutdownCloseOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	req.ClientOrderID = utils.NewCompactOrderID()
	owned := shutdownOwnedOrder{request: req}
	ord, err := ex.PlaceOrder(ctx, &req)
	if err != nil {
		return fmt.Errorf("shutdown submission %s unverified; no replacement: %w", req.ClientOrderID, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := owned.observe(ex.GetName(), ord); err != nil {
		return err
	}
	if err := awaitShutdownOrder(ctx, ex, &owned, opts); err == nil {
		return nil
	} else if errors.Is(err, errShutdownOrderEvidence) {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cancelErr := ex.CancelOrder(ctx, req.Symbol, owned.id)
	if err := awaitShutdownOrder(ctx, ex, &owned, opts); err != nil {
		return fmt.Errorf("shutdown order %d termination not verified: %w", owned.id, errors.Join(cancelErr, err))
	}
	return nil
}
