package position

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/utils"
)

func (cpm *ClosePositionManager) observeClose(op *closeOperation, ord *ExchangeOrder) error {
	op.mu.Lock()
	defer op.mu.Unlock()
	r := &op.record
	if ord == nil || ord.OrderID <= 0 || (r.OrderID > 0 && ord.OrderID != r.OrderID) ||
		ord.Symbol != r.Symbol || ord.Side != r.Side || (ord.ClientOrderID != r.ClientOrderID && ord.ClientOrderID != utils.AddBrokerPrefix(strings.ToLower(cpm.exchange.GetName()), r.ClientOrderID)) {
		return fmt.Errorf("close order identity not verified: %w", execution.ErrOrderUnknown)
	}
	q := op.request.Quantity
	if ord.Quantity != q || math.IsNaN(ord.ExecutedQty) || math.IsInf(ord.ExecutedQty, 0) || ord.ExecutedQty < op.progress || ord.ExecutedQty > q || ord.ExecutedQty < 0 ||
		(ord.ExecutedQty > 0 && !positiveFinite(ord.AvgPrice)) || (ord.Status == "FILLED" && ord.ExecutedQty != q) {
		return fmt.Errorf("close cumulative fill invalid: %w", execution.ErrOrderUnknown)
	}
	switch ord.Status {
	case "NEW", "PARTIALLY_FILLED", "FILLED", "CANCELED", "CANCELLED", "EXPIRED", "REJECTED":
	default:
		return fmt.Errorf("unknown close order status: %w", execution.ErrOrderUnknown)
	}
	terminal := isLiquidationTerminalStatus(ord.Status) || ord.Status == "CANCELLED"
	if op.terminal && (!terminal || ord.ExecutedQty != op.progress) {
		return fmt.Errorf("close changed after terminal evidence: %w", execution.ErrOrderUnknown)
	}
	if recorder, ok := cpm.exchange.(interface{ ConfirmCloseOrder(*ExchangeOrder) error }); ok {
		if err := recorder.ConfirmCloseOrder(ord); err != nil {
			return err
		}
	}
	op.terminal, op.progress = terminal, ord.ExecutedQty
	r.OrderID, r.FilledQty, r.UpdatedAt = ord.OrderID, op.baseFilled+op.progress, time.Now()
	if r.FilledQty > r.TargetQty {
		return fmt.Errorf("close exceeds target: %w", execution.ErrOrderUnknown)
	}
	if terminal && r.FilledQty == r.TargetQty {
		r.Status = CloseStatusFilled
	}
	return nil
}

func (cpm *ClosePositionManager) queryClose(ctx context.Context, op *closeOperation) error {
	r := op.snapshot()
	qctx, cancel := context.WithTimeout(ctx, closeRequestTimeout)
	defer cancel()
	ord, err := cpm.exchange.GetOrder(qctx, cpm.symbol, r.OrderID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return cpm.observeClose(op, ord)
}

func (cpm *ClosePositionManager) watchClose(ctx context.Context, op *closeOperation, cfg config.ClosePositionConfig) {
	ticker := time.NewTicker(cpm.pollInterval)
	defer ticker.Stop()
	for {
		r := op.snapshot()
		if r.Status == CloseStatusFilled {
			return
		}
		if err := ctx.Err(); err != nil {
			op.fail(CloseStatusUnknown, err)
			return
		}
		if err := cpm.queryClose(ctx, op); err != nil {
			op.fail(CloseStatusUnknown, err)
			return
		}
		r = op.snapshot()
		if r.Status == CloseStatusFilled {
			return
		}
		op.mu.Lock()
		terminal := op.terminal
		op.mu.Unlock()
		timedOut := !r.TimeoutAt.IsZero() && !time.Now().Before(r.TimeoutAt)
		if terminal || timedOut {
			if !terminal {
				if err := cpm.cancelClose(ctx, op); err != nil {
					op.fail(CloseStatusUnknown, err)
					return
				}
			}
			if op.snapshot().Status == CloseStatusFilled {
				return
			}
			if timedOut && cfg.AutoRetry && r.Method == CloseMethodLimit && r.RetryCount < cfg.MaxRetries {
				if err := cpm.retryCloseMarket(ctx, op); err != nil {
					return
				}
				continue
			}
			status := CloseStatusCanceled
			if timedOut {
				status = CloseStatusTimeout
			}
			op.fail(status, fmt.Errorf("order terminated with unclosed target quantity"))
			return
		}
		select {
		case <-ctx.Done():
			op.fail(CloseStatusUnknown, ctx.Err())
			return
		case <-ticker.C:
		}
	}
}

func (cpm *ClosePositionManager) cancelClose(ctx context.Context, op *closeOperation) error {
	r := op.snapshot()
	cctx, cancel := context.WithTimeout(ctx, closeRequestTimeout)
	cancelErr := cpm.exchange.CancelOrder(cctx, cpm.symbol, r.OrderID)
	cancel()
	// A cancellation error may race a fill; only a verified terminal query can
	// resolve that ambiguity. ACK alone and one absence never free inventory.
	if err := cpm.queryClose(ctx, op); err != nil {
		return errors.Join(cancelErr, err)
	}
	op.mu.Lock()
	terminal := op.terminal
	op.mu.Unlock()
	if !terminal {
		return fmt.Errorf("cancel not terminal: %w", execution.ErrOrderUnknown)
	}
	return nil
}

func (cpm *ClosePositionManager) retryCloseMarket(ctx context.Context, op *closeOperation) error {
	if err := ctx.Err(); err != nil {
		op.fail(CloseStatusUnknown, err)
		return err
	}
	op.mu.Lock()
	if !op.terminal {
		op.mu.Unlock()
		return fmt.Errorf("cannot replace a live order")
	}
	r := &op.record
	remaining := r.TargetQty - r.FilledQty
	op.baseFilled, op.progress, op.terminal = r.FilledQty, 0, false
	op.request = ExchangeOrderRequest{Symbol: r.Symbol, Side: r.Side, Type: "MARKET", Quantity: remaining, ReduceOnly: true, ClientOrderID: utils.NewCompactOrderID()}
	r.OrderID, r.ClientOrderID, r.Method, r.RetryCount, r.TimeoutAt = 0, op.request.ClientOrderID, CloseMethodMarket, r.RetryCount+1, time.Time{}
	op.mu.Unlock()
	cctx, cancel := context.WithTimeout(ctx, closeRequestTimeout)
	defer cancel()
	return cpm.submitClose(cctx, op)
}
