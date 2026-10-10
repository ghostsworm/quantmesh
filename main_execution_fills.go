package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/logger"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/strategy"
	ordersync "quantmesh/sync"
)

const (
	ownedFillCaptureTimeout   = 10 * time.Second
	ownedFillCaptureWorkers   = 4
	ownedOrderUpdateQueueSize = 64
)

type runtimeOrderUpdateStages struct {
	capture    func(context.Context, position.OrderUpdate) error
	account    func(context.Context, position.OrderUpdate) error
	settle     func(context.Context, position.OrderUpdate) error
	settledQty func(position.OrderUpdate) (float64, bool)
}

type runtimeOrderUpdateWork struct {
	ctx      context.Context
	update   position.OrderUpdate
	stages   runtimeOrderUpdateStages
	ready    chan struct{}
	complete func()
	failure  func(error)
	skipped  func()
}

type runtimeOrderUpdateState struct {
	ctx             context.Context
	queue           chan runtimeOrderUpdateWork
	lastQty         float64
	lastStatus      string
	accountedQty    float64
	accountedStatus string
	accounted       bool
	quarantined     bool
	terminal        bool
	started         bool
}

// runtimeOrderUpdateCoordinator serializes economic processing per venue
// order while keeping slow fill-history I/O off the exchange's shared callback.
type runtimeOrderUpdateCoordinator struct {
	mu     sync.Mutex
	orders map[int64]*runtimeOrderUpdateState
}

func newRuntimeOrderUpdateCoordinator() *runtimeOrderUpdateCoordinator {
	return &runtimeOrderUpdateCoordinator{orders: make(map[int64]*runtimeOrderUpdateState)}
}

func (c *runtimeOrderUpdateCoordinator) Submit(ctx context.Context, update position.OrderUpdate,
	stages runtimeOrderUpdateStages, accepted func(), complete func(), failure func(error), skipped func()) error {
	if c == nil || ctx == nil || update.OrderID <= 0 || (stages.capture == nil && stages.account == nil && stages.settle == nil) {
		return fmt.Errorf("owned order update coordinator is unavailable")
	}
	if stages.settledQty != nil {
		settledQty, settled := stages.settledQty(update)
		if settled {
			if update.ExecutedQty > settledQty {
				err := fmt.Errorf("order %d reports quantity %.12g above settled quantity %.12g", update.OrderID, update.ExecutedQty, settledQty)
				if failure != nil {
					failure(err)
				}
				return err
			}
			// This update did not acquire a coordinator gate. In particular, do
			// not let an older duplicate clear a gate raised by a later fill that
			// exceeds the durably settled quantity.
			return nil
		}
	}
	c.mu.Lock()
	state := c.orders[update.OrderID]
	if state == nil {
		state = &runtimeOrderUpdateState{ctx: ctx, queue: make(chan runtimeOrderUpdateWork, ownedOrderUpdateQueueSize)}
		c.orders[update.OrderID] = state
	}
	if state.terminal {
		if update.ExecutedQty <= state.lastQty && terminalOrderUpdate(update.Status) {
			c.mu.Unlock()
			if skipped != nil {
				skipped()
			}
			return nil
		}
		c.mu.Unlock()
		err := fmt.Errorf("order %d received an update after terminal accounting", update.OrderID)
		if failure != nil {
			failure(err)
		}
		return err
	}
	if update.ExecutedQty < state.lastQty || (update.ExecutedQty == state.lastQty && strings.EqualFold(update.Status, state.lastStatus)) {
		c.mu.Unlock()
		if skipped != nil {
			skipped()
		}
		return nil
	}
	work := runtimeOrderUpdateWork{ctx: ctx, update: update, stages: stages, ready: make(chan struct{}), complete: complete, failure: failure, skipped: skipped}
	select {
	case state.queue <- work:
		if !state.started {
			state.started = true
			go c.run(update.OrderID, state)
		}
		c.mu.Unlock()
		// Enqueue first so queue-full work never creates an orphan gate. The
		// worker waits on ready while accepted() closes the admission gate.
		if accepted != nil {
			accepted()
		}
		close(work.ready)
		return nil
	default:
		c.mu.Unlock()
		err := fmt.Errorf("owned order update queue is full for order %d", update.OrderID)
		if failure != nil {
			failure(err)
		}
		return err
	}
}

func (c *runtimeOrderUpdateCoordinator) run(orderID int64, state *runtimeOrderUpdateState) {
	for {
		if err := state.ctx.Err(); err != nil {
			c.cancelState(orderID, state, err)
			return
		}
		select {
		case work := <-state.queue:
			select {
			case <-work.ready:
			case <-state.ctx.Done():
				c.failWork(work, state.ctx.Err())
				c.cancelState(orderID, state, state.ctx.Err())
				return
			}
			c.mu.Lock()
			stale := work.update.ExecutedQty < state.lastQty
			duplicate := work.update.ExecutedQty == state.lastQty && strings.EqualFold(work.update.Status, state.lastStatus)
			quarantined := state.quarantined
			accounted := state.accounted && work.update.ExecutedQty == state.accountedQty && strings.EqualFold(work.update.Status, state.accountedStatus)
			if state.terminal || stale || duplicate {
				c.mu.Unlock()
				c.skipWork(work)
				continue
			}
			c.mu.Unlock()
			if quarantined {
				c.failWork(work, fmt.Errorf("order %d is quarantined after uncertain strategy accounting", orderID))
				continue
			}
			ctx := work.ctx
			if err := ctx.Err(); err != nil {
				c.failWork(work, err)
				continue
			}
			if work.stages.capture != nil {
				if err := work.stages.capture(ctx, work.update); err != nil {
					c.failWork(work, err)
					continue
				}
			}
			if !accounted && work.stages.account != nil {
				if err := work.stages.account(ctx, work.update); err != nil {
					c.mu.Lock()
					state.quarantined = true
					c.mu.Unlock()
					c.failWork(work, err)
					continue
				}
				c.mu.Lock()
				state.accountedQty, state.accountedStatus, state.accounted = work.update.ExecutedQty, work.update.Status, true
				c.mu.Unlock()
			}
			if work.stages.settle != nil {
				if err := work.stages.settle(ctx, work.update); err != nil {
					c.failWork(work, err)
					continue
				}
			}

			c.mu.Lock()
			if work.update.ExecutedQty >= state.lastQty {
				state.lastQty = work.update.ExecutedQty
				state.lastStatus = work.update.Status
				state.terminal = terminalOrderUpdate(work.update.Status)
			}
			if state.terminal {
				var pending []runtimeOrderUpdateWork
				for {
					select {
					case queued := <-state.queue:
						pending = append(pending, queued)
					default:
						delete(c.orders, orderID)
						c.mu.Unlock()
						for _, queued := range pending {
							c.skipWork(queued)
						}
						if work.complete != nil {
							work.complete()
						}
						return
					}
				}
			}
			c.mu.Unlock()
			if work.complete != nil {
				work.complete()
			}
		case <-state.ctx.Done():
			c.cancelState(orderID, state, state.ctx.Err())
			return
		}
	}
}

func (c *runtimeOrderUpdateCoordinator) failWork(work runtimeOrderUpdateWork, err error) {
	if work.failure != nil {
		work.failure(err)
	}
}

func (c *runtimeOrderUpdateCoordinator) skipWork(work runtimeOrderUpdateWork) {
	if work.skipped != nil {
		work.skipped()
	}
}

func (c *runtimeOrderUpdateCoordinator) cancelState(orderID int64, state *runtimeOrderUpdateState, err error) {
	if err == nil {
		err = context.Canceled
	}
	c.mu.Lock()
	if c.orders[orderID] == state {
		delete(c.orders, orderID)
	}
	var pending []runtimeOrderUpdateWork
	for {
		select {
		case work := <-state.queue:
			pending = append(pending, work)
		default:
			c.mu.Unlock()
			for _, work := range pending {
				c.failWork(work, err)
			}
			return
		}
	}
}

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

func persistOrderFillsBeforeAccounting(ctx context.Context, provider exchange.IExchange, writer interface {
	SaveOrderFill(*storage.OrderFill) error
}, update position.OrderUpdate, exchangeName, marketType, accountScope, account, botID string) error {
	if ctx == nil || provider == nil || writer == nil || update.ExecutedQty <= 0 {
		return fmt.Errorf("positive cumulative order update lacks durable fill-capture dependencies")
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, ownedFillCaptureTimeout)
		lastErr = ordersync.PersistOwnedOrderFills(attemptCtx, provider, writer, update,
			exchangeName, marketType, accountScope, account, botID)
		cancel()
		if lastErr == nil {
			return nil
		}
		if attempt < 2 {
			timer := time.NewTimer(time.Duration(attempt+1) * 300 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return fmt.Errorf("capture order %d fills before accounting: %w", update.OrderID, ctx.Err())
			case <-timer.C:
			}
		}
	}
	return fmt.Errorf("capture order %d fills before accounting: %w", update.OrderID, lastErr)
}

func ownedPositiveOrderStages(provider exchange.IExchange, writer interface {
	SaveOrderFill(*storage.OrderFill) error
}, exchangeName, marketType, accountScope, account, botID, symbol string,
	executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate,
	strategyManager *strategy.StrategyManager, multiExecutor *strategy.MultiStrategyExecutor,
	spm *position.SuperPositionManager, eventBus *event.EventBus,
	retryBootstrap func() (bool, error)) runtimeOrderUpdateStages {
	return runtimeOrderUpdateStages{
		settledQty: func(update position.OrderUpdate) (float64, bool) {
			return executor.SettledIntentExecutedQty(update.ClientOrderID)
		},
		capture: func(ctx context.Context, update position.OrderUpdate) error {
			err := persistOrderFillsBeforeAccounting(ctx, provider, writer, update, exchangeName, marketType, accountScope, account, botID)
			if err != nil {
				_ = executor.MarkTradeLedgerReconciliationRequired(update.OrderID, update.ClientOrderID, err.Error())
			}
			return err
		},
		account: func(_ context.Context, update position.OrderUpdate) error {
			ownerStrategy, ownerType, owned := executor.IntentStrategyType(update.ClientOrderID)
			if !owned || ownerStrategy == "" || ownerType == "" {
				return fmt.Errorf("owned order %d has no durable strategy route", update.OrderID)
			}
			if multiExecutor != nil {
				route := multiExecutor.GetStrategyByOrderID(update.OrderID)
				if route == "" {
					route = multiExecutor.GetStrategyByClientOrderID(update.ClientOrderID)
				}
				if route != "" && route != ownerStrategy {
					return fmt.Errorf("order %d strategy route conflicts with durable owner", update.OrderID)
				}
			}
			gridAccounted, _ := spm.OnOrderUpdateWithAccounting(update)
			if ownerType == "grid" && !gridAccounted {
				return fmt.Errorf("grid accounting for order %d is not durable", update.OrderID)
			}
			if strategyManager != nil {
				accounted, err := strategyManager.ApplyOrderUpdateForStrategyWithAccounting(ownerStrategy, &update)
				if err != nil || !accounted {
					if err == nil {
						err = fmt.Errorf("strategy %s did not confirm durable order accounting", ownerStrategy)
					}
					gate.Block("strategy_accounting_unverified")
					return err
				}
			} else if ownerType != "grid" {
				return fmt.Errorf("strategy manager is unavailable for owner strategy %s", ownerStrategy)
			}
			if multiExecutor != nil {
				multiExecutor.OnOrderUpdate(&update)
			}
			return nil
		},
		settle: func(ctx context.Context, update position.OrderUpdate) error {
			if !terminalOrderUpdate(update.Status) {
				return nil
			}
			ownerStrategy, ownerType, owned := executor.IntentStrategyType(update.ClientOrderID)
			if !owned || ownerStrategy == "" || ownerType == "" {
				return fmt.Errorf("terminal order %d has no durable strategy owner", update.OrderID)
			}
			// Grid accounting must already have a durable terminal cursor before
			// the owner-scoped intent can settle. The check is idempotent and
			// deliberately avoids the legacy global settlement gate: this
			// coordinator owns a per-order gate which a verified retry can clear.
			if ownerType == "grid" {
				accounted, _ := spm.OnOrderUpdateWithAccounting(update)
				if !accounted {
					return fmt.Errorf("grid terminal accounting for order %d is not durable", update.OrderID)
				}
			}
			canonicalCID, owned := executor.OwnedIntentClientOrderID(update.ClientOrderID)
			if !owned {
				return fmt.Errorf("terminal order %d lost its durable intent owner", update.OrderID)
			}
			var err error
			if ownerType == "grid" {
				err = executor.SettleReconciledIntent(ctx, canonicalCID, ownerStrategy)
			} else {
				err = executor.SettleIntent(ctx, canonicalCID)
			}
			if err != nil {
				return fmt.Errorf("settle terminal owner intent for order %d: %w", update.OrderID, err)
			}
			if retryBootstrap != nil {
				if bootstrapped, retryErr := retryBootstrap(); retryErr != nil {
					if eventBus != nil {
						eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
							"bot_id": botID, "symbol": symbol, "exchange": exchangeName,
							"reason": runtimeExposureBootstrapBlock, "order_id": update.OrderID,
							"requires_reconciliation": true,
						}})
					}
				} else if bootstrapped {
					logger.InfoCtx(ctx, "[%s] terminal owner-scoped exposure bootstrap completed", botID)
				}
			}
			return nil
		},
	}
}

func processOwnedZeroFillTerminal(ctx context.Context, update position.OrderUpdate, provider exchange.IExchange,
	executor *order.ExchangeOrderExecutor, strategyManager *strategy.StrategyManager, multiExecutor *strategy.MultiStrategyExecutor,
	spm *position.SuperPositionManager, retryBootstrap func() (bool, error)) error {
	if !zeroFillTerminalRuntimeStatus(update.Status) || update.ExecutedQty != 0 || provider == nil {
		return fmt.Errorf("order %d is not a zero-fill terminal update", update.OrderID)
	}
	canonicalCID, owned := executor.OwnedIntentClientOrderID(update.ClientOrderID)
	ownerStrategy, ownerType, routed := executor.IntentStrategyType(update.ClientOrderID)
	if !owned || !routed || ownerStrategy == "" || ownerType == "" {
		return fmt.Errorf("zero-fill order %d has no durable strategy route", update.OrderID)
	}
	// Verify exact venue identity and zero-fill terminal status before economic
	// accounting. Settlement performs a second fresh query after durable writes.
	queryCtx, cancel := context.WithTimeout(ctx, intentSettlementTimeout)
	venueOrder, err := provider.GetOrder(queryCtx, update.Symbol, update.OrderID)
	cancel()
	if err != nil {
		return fmt.Errorf("re-query zero-fill terminal order %d: %w", update.OrderID, err)
	}
	if venueOrder == nil || venueOrder.OrderID != update.OrderID || venueOrder.Symbol != update.Symbol ||
		(venueOrder.ClientOrderID != "" && venueOrder.ClientOrderID != update.ClientOrderID && venueOrder.ClientOrderID != canonicalCID) ||
		!zeroFillTerminalRuntimeStatus(string(venueOrder.Status)) || venueOrder.ExecutedQty != 0 {
		return fmt.Errorf("zero-fill terminal order %d failed exact venue re-query", update.OrderID)
	}
	update.ClientOrderID = canonicalCID
	gridAccounted, zeroGridAccounted := spm.OnOrderUpdateWithAccounting(update)
	if ownerType == "grid" {
		if !gridAccounted && !zeroGridAccounted {
			return fmt.Errorf("grid zero-fill accounting for order %d is not durable", update.OrderID)
		}
	} else {
		if strategyManager == nil {
			return fmt.Errorf("strategy manager is unavailable for zero-fill owner %s", ownerStrategy)
		}
		accounted, accountingErr := strategyManager.ApplyOrderUpdateForStrategyWithAccounting(ownerStrategy, &update)
		if accountingErr != nil || !accounted {
			if accountingErr == nil {
				accountingErr = fmt.Errorf("strategy %s did not confirm durable zero-fill accounting", ownerStrategy)
			}
			return accountingErr
		}
	}
	if multiExecutor != nil {
		route := multiExecutor.GetStrategyByOrderID(update.OrderID)
		if route == "" {
			route = multiExecutor.GetStrategyByClientOrderID(update.ClientOrderID)
		}
		if route != "" && route != ownerStrategy {
			return fmt.Errorf("zero-fill order %d strategy route conflicts with durable owner", update.OrderID)
		}
		multiExecutor.OnOrderUpdate(&update)
	}
	settleCtx, settleCancel := context.WithTimeout(ctx, intentSettlementTimeout)
	defer settleCancel()
	if err := executor.SettleZeroFillIntent(settleCtx, canonicalCID); err != nil {
		_ = executor.MarkOrderReconciliationRequired(update.OrderID, canonicalCID, err.Error())
		return fmt.Errorf("settle verified zero-fill order %d: %w", update.OrderID, err)
	}
	if retryBootstrap != nil {
		_, _ = retryBootstrap()
	}
	return nil
}

func zeroFillTerminalRuntimeStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "CANCELED", "CANCELLED", "EXPIRED", "REJECTED":
		return true
	default:
		return false
	}
}

func zeroFillOrderStages(provider exchange.IExchange, executor *order.ExchangeOrderExecutor,
	strategyManager *strategy.StrategyManager, multiExecutor *strategy.MultiStrategyExecutor,
	spm *position.SuperPositionManager, retryBootstrap func() (bool, error)) runtimeOrderUpdateStages {
	return runtimeOrderUpdateStages{
		settledQty: func(update position.OrderUpdate) (float64, bool) {
			return executor.SettledIntentExecutedQty(update.ClientOrderID)
		},
		capture: func(ctx context.Context, update position.OrderUpdate) error {
			if provider == nil || update.OrderID <= 0 || update.ExecutedQty != 0 || !zeroFillTerminalRuntimeStatus(update.Status) {
				return fmt.Errorf("invalid zero-fill terminal update for order %d", update.OrderID)
			}
			cid, owned := executor.OwnedIntentClientOrderID(update.ClientOrderID)
			if !owned {
				return fmt.Errorf("zero-fill order %d has no durable owner", update.OrderID)
			}
			queryCtx, cancel := context.WithTimeout(ctx, intentSettlementTimeout)
			venueOrder, err := provider.GetOrder(queryCtx, update.Symbol, update.OrderID)
			cancel()
			if err != nil {
				return fmt.Errorf("re-query zero-fill terminal order %d: %w", update.OrderID, err)
			}
			if venueOrder == nil || venueOrder.OrderID != update.OrderID || venueOrder.Symbol != update.Symbol ||
				(venueOrder.ClientOrderID != "" && venueOrder.ClientOrderID != update.ClientOrderID && venueOrder.ClientOrderID != cid) ||
				!zeroFillTerminalRuntimeStatus(string(venueOrder.Status)) || venueOrder.ExecutedQty != 0 {
				return fmt.Errorf("zero-fill terminal order %d failed exact venue re-query", update.OrderID)
			}
			return nil
		},
		account: func(_ context.Context, update position.OrderUpdate) error {
			owner, ownerType, found := executor.IntentStrategyType(update.ClientOrderID)
			if !found || owner == "" || ownerType == "" {
				return fmt.Errorf("zero-fill order %d has no durable strategy route", update.OrderID)
			}
			if multiExecutor != nil {
				route := multiExecutor.GetStrategyByOrderID(update.OrderID)
				if route == "" {
					route = multiExecutor.GetStrategyByClientOrderID(update.ClientOrderID)
				}
				if route != "" && route != owner {
					return fmt.Errorf("zero-fill order %d strategy route conflicts with durable owner", update.OrderID)
				}
			}
			gridAccounted, zeroGridAccounted := spm.OnOrderUpdateWithAccounting(update)
			if ownerType == "grid" {
				if !gridAccounted && !zeroGridAccounted {
					return fmt.Errorf("grid zero-fill accounting for order %d is not durable", update.OrderID)
				}
			} else {
				if strategyManager == nil {
					return fmt.Errorf("strategy manager is unavailable for zero-fill owner %s", owner)
				}
				accounted, err := strategyManager.ApplyOrderUpdateForStrategyWithAccounting(owner, &update)
				if err != nil || !accounted {
					if err == nil {
						err = fmt.Errorf("strategy %s did not confirm durable zero-fill accounting", owner)
					}
					return err
				}
			}
			if multiExecutor != nil {
				multiExecutor.OnOrderUpdate(&update)
			}
			return nil
		},
		settle: func(ctx context.Context, update position.OrderUpdate) error {
			cid, owned := executor.OwnedIntentClientOrderID(update.ClientOrderID)
			if !owned {
				return fmt.Errorf("zero-fill order %d lost its durable owner", update.OrderID)
			}
			settleCtx, cancel := context.WithTimeout(ctx, intentSettlementTimeout)
			defer cancel()
			if err := executor.SettleZeroFillIntent(settleCtx, cid); err != nil {
				_ = executor.MarkOrderReconciliationRequired(update.OrderID, cid, err.Error())
				return fmt.Errorf("settle zero-fill terminal order %d: %w", update.OrderID, err)
			}
			if retryBootstrap != nil {
				_, _ = retryBootstrap()
			}
			return nil
		},
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
