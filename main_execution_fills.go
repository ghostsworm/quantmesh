package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
	ordersync "quantmesh/sync"
)

const (
	ownedFillCaptureTimeout = 10 * time.Second
	ownedFillCaptureWorkers = 4
)

type runtimeFillCapture struct {
	mu       sync.Mutex
	target   map[int64]float64
	captured map[int64]float64
	running  map[int64]bool
	workers  chan struct{}
}

func newRuntimeFillCapture() *runtimeFillCapture {
	return &runtimeFillCapture{
		target: make(map[int64]float64), captured: make(map[int64]float64), running: make(map[int64]bool),
		workers: make(chan struct{}, ownedFillCaptureWorkers),
	}
}

func terminalOrderUpdate(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "FILLED", "CANCELED", "CANCELLED", "EXPIRED", "REJECTED", "PARTIALLY_FILLED_CANCELED", "PARTIALLY_FILLED_CANCELLED":
		return true
	default:
		return false
	}
}

func (c *runtimeFillCapture) Observe(ctx context.Context, provider exchange.IExchange, writer interface {
	SaveOrderFill(*storage.OrderFill) error
}, update position.OrderUpdate, exchangeName, marketType, accountScope, account, botID string,
	onFailure func(error), onSuccess func()) {
	if !terminalOrderUpdate(update.Status) || update.OrderID <= 0 || update.ExecutedQty <= 0 {
		return
	}
	if c == nil || ctx == nil || provider == nil || writer == nil || exchangeName == "" || marketType == "" || accountScope == "" {
		if onFailure != nil {
			onFailure(fmt.Errorf("automatic execution ledger is unavailable for terminal order %d", update.OrderID))
		}
		return
	}
	c.mu.Lock()
	if update.ExecutedQty > c.target[update.OrderID] {
		c.target[update.OrderID] = update.ExecutedQty
	}
	if c.captured[update.OrderID] >= c.target[update.OrderID] {
		c.mu.Unlock()
		if onSuccess != nil {
			onSuccess()
		}
		return
	}
	if c.running[update.OrderID] {
		c.mu.Unlock()
		return
	}
	c.running[update.OrderID] = true
	c.mu.Unlock()

	go func() {
		select {
		case c.workers <- struct{}{}:
			defer func() { <-c.workers }()
		case <-ctx.Done():
			if onFailure != nil {
				onFailure(fmt.Errorf("automatic execution capture canceled for order %d: %w", update.OrderID, ctx.Err()))
			}
			c.finish(update.OrderID)
			return
		}
		for {
			c.mu.Lock()
			target := c.target[update.OrderID]
			captured := c.captured[update.OrderID]
			c.mu.Unlock()
			if captured >= target {
				c.finish(update.OrderID)
				return
			}
			attempts := 3
			var captureErr error
			for attempt := 0; attempt < attempts; attempt++ {
				attemptCtx, cancel := context.WithTimeout(ctx, ownedFillCaptureTimeout)
				captureUpdate := update
				captureUpdate.ExecutedQty = target
				captureErr = ordersync.PersistOwnedOrderFills(attemptCtx, provider, writer, captureUpdate,
					exchangeName, marketType, accountScope, account, botID)
				cancel()
				if captureErr == nil {
					break
				}
				if attempt+1 < attempts {
					timer := time.NewTimer(time.Duration(attempt+1) * 300 * time.Millisecond)
					select {
					case <-ctx.Done():
						timer.Stop()
						captureErr = ctx.Err()
						attempt = attempts
					case <-timer.C:
					}
				}
			}
			if captureErr != nil {
				if onFailure != nil {
					onFailure(fmt.Errorf("persist complete execution history for order %d: %w", update.OrderID, captureErr))
				}
				c.finish(update.OrderID)
				return
			}
			c.mu.Lock()
			if target > c.captured[update.OrderID] {
				c.captured[update.OrderID] = target
			}
			if c.target[update.OrderID] > c.captured[update.OrderID] {
				c.mu.Unlock()
				continue
			}
			delete(c.running, update.OrderID)
			c.mu.Unlock()
			if onSuccess != nil {
				onSuccess()
			}
			return
		}
	}()
}

func captureTerminalOrderAndSettleOwnedIntent(ctx context.Context, capture *runtimeFillCapture, provider exchange.IExchange, writer interface {
	SaveOrderFill(*storage.OrderFill) error
}, update position.OrderUpdate, exchangeName, marketType, accountScope, account, botID string,
	executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate, strategyName string,
	strategyAccountingVerified, gridAccountingVerified bool,
	onCaptureFailure func(error), onSettlementFailure func(error), onIntentSettled func()) {
	if capture == nil || gate == nil || executor == nil {
		if onCaptureFailure != nil {
			onCaptureFailure(fmt.Errorf("terminal fill reconciliation dependencies are unavailable"))
		}
		return
	}
	blockReason := fmt.Sprintf("execution_ledger_unverified:%d", update.OrderID)
	gate.Block(blockReason)
	capture.Observe(ctx, provider, writer, update, exchangeName, marketType, accountScope, account, botID,
		func(err error) {
			gate.Block(blockReason)
			if onCaptureFailure != nil {
				onCaptureFailure(err)
			}
		}, func() {
			_, strategyType, owned := executor.IntentStrategyType(update.ClientOrderID)
			var settlementErr error
			if strategyType == "grid" || strategyName == "grid" {
				settlementErr = settleVerifiedGridIntent(ctx, executor, gate, &update, gridAccountingVerified)
			} else if strategyName != "" && strategyAccountingVerified {
				settlementErr = settleVerifiedStrategyIntent(ctx, executor, gate, strategyName, &update)
			} else if owned && strategyType != "" && strategyAccountingVerified {
				settlementErr = fmt.Errorf("terminal execution intent has no matching routed strategy")
			}
			if settlementErr != nil {
				gate.Block(strategyIntentSettlementBlock)
				gate.Unblock(blockReason)
				if onSettlementFailure != nil {
					onSettlementFailure(settlementErr)
				}
				return
			}
			gate.Unblock(blockReason)
			if onIntentSettled != nil && (strategyType == "grid" || (strategyName != "" && strategyAccountingVerified)) {
				onIntentSettled()
			}
		})
}

func (c *runtimeFillCapture) finish(orderID int64) {
	c.mu.Lock()
	delete(c.running, orderID)
	c.mu.Unlock()
}
