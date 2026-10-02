package strategy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/utils"
)

// FundingPerpSpreadStrategy 雙永续跨所資金費差：高費率所做空、低費率所做多，名義對齊
type FundingPerpSpreadStrategy struct {
	name   string
	cfg    *config.Config
	symCfg config.SymbolConfig
	fp     *config.FundingPerpSpreadConfig
	legA   exchange.IExchange
	legB   exchange.IExchange
	symA   string
	symB   string

	minSpread  float64
	exitSpread float64
	maxBasis   float64
	tickInt    time.Duration

	mu                        sync.RWMutex
	ctx                       context.Context
	cancel                    context.CancelFunc
	runDone                   chan struct{}
	started                   bool
	stopMu                    sync.Mutex
	stopped                   bool
	stopErr                   error
	ownershipReady            bool
	exposureUnknown           bool
	ownedA                    float64
	ownedB                    float64
	intentInFlight            bool
	emergencyCloseRequired    bool
	executionLedgerUnverified bool
	pendingExecutions         []fundingPerpSpreadPendingExecutionState
	pendingOrder              *fundingPerpSpreadOrderIntent
	runtimeStateStore         RuntimeStateStore
	coordinationLock          lock.DistributedLock
	coordinationTTL           time.Duration
	eventBus                  EventBus
	openingGate               *execution.OpeningGate
	executionRecorder         FundingPerpSpreadExecutionRecorder

	consecutiveErrors int
}

// FundingPerpSpreadExecutionRecorder synchronously verifies and records the
// fills for one exact order after its physical position change is reconciled.
type FundingPerpSpreadExecutionRecorder func(context.Context, exchange.IExchange, *exchange.OrderRequest, *exchange.Order) (bool, error)

type fundingPerpSpreadExecutionLedgerError struct{ cause error }

func (e *fundingPerpSpreadExecutionLedgerError) Error() string { return e.cause.Error() }
func (e *fundingPerpSpreadExecutionLedgerError) Unwrap() error { return e.cause }

type fundingPerpSpreadPendingExecution struct {
	client  exchange.IExchange
	request *exchange.OrderRequest
	order   *exchange.Order
}

// SetExecutionRecorder installs the runtime-owned durable fill ledger hook.
func (s *FundingPerpSpreadStrategy) SetExecutionRecorder(recorder FundingPerpSpreadExecutionRecorder) {
	s.mu.Lock()
	s.executionRecorder = recorder
	s.mu.Unlock()
}

func (s *FundingPerpSpreadStrategy) recordOrderExecution(ctx context.Context, client exchange.IExchange, request *exchange.OrderRequest, order *exchange.Order) (bool, error) {
	s.mu.RLock()
	recorder := s.executionRecorder
	s.mu.RUnlock()
	if recorder == nil {
		s.retainExecutionLedgerFailure(client, request, order)
		return false, &fundingPerpSpreadExecutionLedgerError{cause: errors.New("funding_perp_spread durable execution ledger is unavailable")}
	}
	orderResolved, err := recorder(ctx, client, request, order)
	if err != nil || !orderResolved {
		if err == nil {
			err = errors.New("order or fill identity could not be verified")
		}
		s.retainExecutionLedgerFailure(client, request, order)
		return orderResolved, &fundingPerpSpreadExecutionLedgerError{cause: fmt.Errorf("verify and persist executions: %w", err)}
	}
	if _, err := s.resolvePendingExecution(client, request, order); err != nil {
		s.retainExecutionLedgerFailure(client, request, order)
		return true, &fundingPerpSpreadExecutionLedgerError{cause: fmt.Errorf("persist execution-ledger reconciliation: %w", err)}
	}
	return orderResolved, nil
}

// NewFundingPerpSpreadStrategy 建立策略
func NewFundingPerpSpreadStrategy(
	name string,
	cfg *config.Config,
	symCfg config.SymbolConfig,
	legA, legB exchange.IExchange,
	fp *config.FundingPerpSpreadConfig,
	stratCfg map[string]interface{},
) *FundingPerpSpreadStrategy {
	minS := 0.0001
	exitS := 0.00005
	maxB := 1.0
	intervalSec := 45
	if fp != nil {
		if fp.MinFundingSpread > 0 {
			minS = fp.MinFundingSpread
		}
		if fp.ExitFundingSpread > 0 {
			exitS = fp.ExitFundingSpread
		}
		if fp.MaxBasisPct > 0 {
			maxB = fp.MaxBasisPct
		}
	}
	if stratCfg != nil {
		if v, ok := stratCfg["min_funding_spread"].(float64); ok && v > 0 {
			minS = v
		}
		if v, ok := stratCfg["exit_funding_spread"].(float64); ok && v > 0 {
			exitS = v
		}
		if v, ok := stratCfg["max_basis_pct"].(float64); ok && v > 0 {
			maxB = v
		}
		if v, ok := stratCfg["rebalance_interval_sec"].(float64); ok && v >= 10 {
			intervalSec = int(v)
		}
	}

	return &FundingPerpSpreadStrategy{
		name:        name,
		cfg:         cfg,
		symCfg:      symCfg,
		fp:          fp,
		legA:        legA,
		legB:        legB,
		symA:        fp.LegA.Symbol,
		symB:        fp.LegB.Symbol,
		minSpread:   minS,
		exitSpread:  exitS,
		maxBasis:    maxB,
		openingGate: &execution.OpeningGate{},
		tickInt:     time.Duration(intervalSec) * time.Second,
	}
}

func (s *FundingPerpSpreadStrategy) SetOpeningGate(gate *execution.OpeningGate) {
	if gate == nil {
		gate = &execution.OpeningGate{}
	}
	s.mu.Lock()
	s.openingGate = gate
	s.mu.Unlock()
}

// MarkOwnershipUnverified freezes strategy actions after its process-level
// position lease is lost. Recovery requires a fresh, explicit reconciliation.
func (s *FundingPerpSpreadStrategy) MarkOwnershipUnverified() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exposureUnknown = true
	if err := s.persistRuntimeStateLocked(); err != nil {
		return fmt.Errorf("persist funding_perp_spread ownership loss: %w", err)
	}
	return nil
}

func (s *FundingPerpSpreadStrategy) Name() string { return s.name }

func (s *FundingPerpSpreadStrategy) Initialize(*config.Config, position.OrderExecutorInterface, position.IExchange) error {
	return nil
}

func (s *FundingPerpSpreadStrategy) SetEventBus(bus EventBus) { s.eventBus = bus }

func (s *FundingPerpSpreadStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	s.mu.Lock()
	s.runtimeStateStore = store
	s.mu.Unlock()
}

func (s *FundingPerpSpreadStrategy) SetCoordinationLock(distributedLock lock.DistributedLock) {
	s.mu.Lock()
	s.coordinationLock = distributedLock
	s.mu.Unlock()
}

func (s *FundingPerpSpreadStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("funding_perp_spread strategy already started")
	}
	s.started = true
	store := s.runtimeStateStore
	s.mu.Unlock()
	if store == nil {
		s.resetStartAfterFailure()
		return errors.New("funding_perp_spread requires durable runtime state storage")
	}
	checkCtx, checkCancel := context.WithTimeout(ctx, 15*time.Second)
	defer checkCancel()
	version, payload, found, err := store.LoadRuntimeState("funding_perp_spread")
	if err != nil {
		s.resetStartAfterFailure()
		return fmt.Errorf("load funding_perp_spread runtime state: %w", err)
	}
	var restored fundingPerpSpreadRuntimeState
	if found {
		restored, err = decodeFundingPerpSpreadRuntimeState(version, payload,
			s.legA.GetName(), s.symA, s.legB.GetName(), s.symB)
		if err != nil {
			s.resetStartAfterFailure()
			return err
		}
	}
	var posA, posB float64
	err = s.withLegCoordination(checkCtx, func(coordCtx context.Context) error {
		if found && restored.IntentInFlight {
			if err := s.reconcileUnfilledOrderIntent(coordCtx, &restored); err != nil {
				return fmt.Errorf("reconcile persisted funding_perp_spread order intent: %w", err)
			}
		}
		var snapshotErr error
		posA, snapshotErr = s.readLegSnapshot(coordCtx, s.legA, s.symA)
		if snapshotErr != nil {
			return fmt.Errorf("legA position/order state cannot be safely reconciled: %w", snapshotErr)
		}
		posB, snapshotErr = s.readLegSnapshot(coordCtx, s.legB, s.symB)
		if snapshotErr != nil {
			return fmt.Errorf("legB position/order state cannot be safely reconciled: %w", snapshotErr)
		}
		if found {
			if math.Abs(posA-restored.OwnedA) > s.legTolerance(s.legA) || math.Abs(posB-restored.OwnedB) > s.legTolerance(s.legB) {
				return fmt.Errorf("runtime state ownership does not match exchange positions (A %.8f/%.8f, B %.8f/%.8f)", posA, restored.OwnedA, posB, restored.OwnedB)
			}
			if !restored.EmergencyCloseRequired {
				if err := validateFundingPerpSpreadHedgeShape(posA, posB, s.legTolerance(s.legA), s.legTolerance(s.legB)); err != nil {
					return fmt.Errorf("persisted funding_perp_spread exposure is not a valid two-leg hedge: %w", err)
				}
			}
			if !restored.EmergencyCloseRequired && math.Abs(posA) > s.legTolerance(s.legA) && math.Abs(posB) > s.legTolerance(s.legB) {
				priceA, priceErr := s.legA.GetLatestPrice(coordCtx, s.symA)
				if priceErr != nil {
					return fmt.Errorf("read legA price to verify restored hedge notional: %w", priceErr)
				}
				priceB, priceErr := s.legB.GetLatestPrice(coordCtx, s.symB)
				if priceErr != nil {
					return fmt.Errorf("read legB price to verify restored hedge notional: %w", priceErr)
				}
				if err := validateFundingPerpSpreadNotional(posA, posB, priceA, priceB, s.maxBasis); err != nil {
					return fmt.Errorf("persisted funding_perp_spread notional is not balanced: %w", err)
				}
			}
		} else if posA != 0 || posB != 0 {
			return fmt.Errorf("unowned positions exist without a runtime state (A %.8f, B %.8f)", posA, posB)
		}
		s.mu.Lock()
		s.ctx, s.cancel = context.WithCancel(ctx)
		s.runDone = make(chan struct{})
		s.ownershipReady = true
		s.exposureUnknown = false
		s.ownedA, s.ownedB = 0, 0
		s.executionLedgerUnverified = false
		s.pendingExecutions = nil
		if found {
			s.ownedA, s.ownedB = restored.OwnedA, restored.OwnedB
			s.executionLedgerUnverified = restored.ExecutionLedgerUnverified
			s.emergencyCloseRequired = restored.EmergencyCloseRequired
			s.pendingExecutions = cloneFundingPerpSpreadPendingExecutions(restored.PendingExecutions)
		}
		persistErr := s.persistRuntimeStateLocked()
		if persistErr != nil {
			s.cancel()
			s.ctx, s.cancel, s.runDone = nil, nil, nil
			s.ownershipReady = false
		}
		s.mu.Unlock()
		if persistErr != nil {
			return fmt.Errorf("persist initial funding_perp_spread runtime state: %w", persistErr)
		}
		return nil
	})
	if err != nil {
		s.resetStartAfterFailure()
		return err
	}
	s.mu.RLock()
	ledgerUnverified := s.executionLedgerUnverified
	gate := s.openingGate
	s.mu.RUnlock()
	if ledgerUnverified && gate != nil {
		gate.Block("execution_ledger_unverified")
	}
	if ledgerUnverified && len(s.pendingExecutions) > 0 {
		if reconcileErr := s.reconcilePendingExecutionLedger(checkCtx); reconcileErr != nil {
			logger.Warn("funding_perp_spread pending execution ledger remains blocked after startup reconciliation: %v", reconcileErr)
		}
	}
	s.mu.RLock()
	emergencyCloseRequired := s.emergencyCloseRequired
	gate = s.openingGate
	s.mu.RUnlock()
	if emergencyCloseRequired {
		if gate != nil {
			gate.Block("interrupted_exposure_recovery")
		}
		closeCtx, closeCancel := context.WithTimeout(ctx, 30*time.Second)
		closeErr := s.closeAll(closeCtx, "interrupted_order_recovery")
		flatErr := s.VerifyFlat(closeCtx)
		closeCancel()
		if flatErr != nil {
			s.resetStartAfterFailure()
			if closeErr != nil {
				return errors.Join(fmt.Errorf("recover interrupted funding_perp_spread exposure: %w", closeErr), flatErr)
			}
			return fmt.Errorf("verify interrupted funding_perp_spread recovery flatness: %w", flatErr)
		}
		if err := s.clearEmergencyCloseRequired(); err != nil {
			s.resetStartAfterFailure()
			return errors.Join(closeErr, fmt.Errorf("persist completed interrupted-exposure recovery: %w", err))
		}
		if closeErr != nil {
			logger.Warn("funding_perp_spread exposure was flattened during startup recovery; trading remains gated by unresolved execution ledger: %v", closeErr)
		}
		if gate != nil {
			gate.Unblock("interrupted_exposure_recovery")
		}
	}
	s.mu.Lock()
	go s.runLoop()
	s.mu.Unlock()
	return nil
}

func validateFundingPerpSpreadHedgeShape(posA, posB, toleranceA, toleranceB float64) error {
	if math.IsNaN(posA) || math.IsInf(posA, 0) || math.IsNaN(posB) || math.IsInf(posB, 0) {
		return errors.New("position snapshot contains a non-finite quantity")
	}
	openA := math.Abs(posA) > toleranceA
	openB := math.Abs(posB) > toleranceB
	if openA != openB {
		return fmt.Errorf("only one hedge leg is open (A %.8f, B %.8f)", posA, posB)
	}
	if openA && (posA > 0) == (posB > 0) {
		return fmt.Errorf("both hedge legs have the same direction (A %.8f, B %.8f)", posA, posB)
	}
	return nil
}

// reconcileUnfilledOrderIntent clears an order intent only after exact terminal
// identity and position evidence; filled intents transition to emergency close.
func (s *FundingPerpSpreadStrategy) reconcileUnfilledOrderIntent(ctx context.Context, state *fundingPerpSpreadRuntimeState) error {
	intent := state.PendingOrder
	if intent == nil {
		return errors.New("pending order lacks a recoverable durable identity")
	}
	ex, ok := s.exchangeForPendingOrder(intent)
	if !ok {
		return errors.New("pending order exchange is outside the configured legs")
	}
	query, ok := ex.(exchange.OrderByClientIDQuerier)
	if !ok {
		return fmt.Errorf("exchange %s cannot query historical orders by ClientOrderID", ex.GetName())
	}
	order, err := query.GetOrderByClientOrderID(ctx, intent.Symbol, intent.ClientOrderID)
	if err != nil {
		return fmt.Errorf("query order %s: %w", intent.ClientOrderID, err)
	}
	if order == nil {
		return fmt.Errorf("order %s is not found; absence does not prove rejection", intent.ClientOrderID)
	}
	returnedClientOrderID := utils.RemoveBrokerPrefix(strings.ToLower(ex.GetName()), order.ClientOrderID)
	if returnedClientOrderID != intent.ClientOrderID || !strings.EqualFold(order.Symbol, intent.Symbol) ||
		string(order.Side) != intent.Side || math.IsNaN(order.Quantity) || math.IsInf(order.Quantity, 0) ||
		math.Abs(order.Quantity-intent.Quantity) > math.Max(1e-12, intent.Quantity*1e-9) ||
		math.IsNaN(order.ExecutedQty) || math.IsInf(order.ExecutedQty, 0) || order.ExecutedQty < 0 || order.ExecutedQty > order.Quantity+math.Max(1e-12, order.Quantity*1e-9) {
		return fmt.Errorf("order %s identity or fill quantity does not match the durable intent", intent.ClientOrderID)
	}
	order, err = resolveFundingPerpSpreadIntentOrder(ctx, ex, intent, order)
	if err != nil {
		return err
	}
	if order.ExecutedQty > 0 {
		if order.OrderID <= 0 {
			return fmt.Errorf("filled order %s has no exchange order ID for exact fill-history reconciliation", intent.ClientOrderID)
		}
		s.mu.RLock()
		recorder := s.executionRecorder
		s.mu.RUnlock()
		if recorder == nil {
			return fmt.Errorf("filled order %s cannot be economically reconciled because the durable execution recorder is unavailable", intent.ClientOrderID)
		}
		request := &exchange.OrderRequest{
			Symbol: intent.Symbol, Side: exchange.Side(intent.Side), Type: exchange.OrderTypeMarket,
			Quantity: intent.Quantity, StrategyType: "funding_perp_spread", ClientOrderID: intent.ClientOrderID,
		}
		resolved, recordErr := recorder(ctx, ex, request, order)
		if recordErr != nil {
			return fmt.Errorf("persist fills for recovered order %s: %w", intent.ClientOrderID, recordErr)
		}
		if !resolved {
			return fmt.Errorf("fills for recovered order %s remain unverified", intent.ClientOrderID)
		}
		actual, snapshotErr := s.readLegSnapshot(ctx, ex, intent.Symbol)
		if snapshotErr != nil {
			return fmt.Errorf("verify exposure for recovered order %s: %w", intent.ClientOrderID, snapshotErr)
		}
		delta := order.ExecutedQty
		if order.Side == exchange.SideSell {
			delta = -delta
		}
		expected := intent.PositionBefore + delta
		if math.Abs(actual-expected) > s.legTolerance(ex) {
			return fmt.Errorf("position change for recovered order %s does not match its fills (before %.8f, expected %.8f, actual %.8f)", intent.ClientOrderID, intent.PositionBefore, expected, actual)
		}
		if strings.EqualFold(ex.GetName(), s.legA.GetName()) && strings.EqualFold(intent.Symbol, s.symA) {
			state.OwnedA = actual
		} else if strings.EqualFold(ex.GetName(), s.legB.GetName()) && strings.EqualFold(intent.Symbol, s.symB) {
			state.OwnedB = actual
		} else {
			return errors.New("recovered order does not match a configured funding_perp_spread leg")
		}
		state.IntentInFlight = false
		state.PendingOrder = nil
		state.ExposureUnknown = false
		state.EmergencyCloseRequired = true
		return nil
	}
	switch order.Status {
	case exchange.OrderStatusCanceled, exchange.OrderStatusRejected, exchange.OrderStatusExpired:
	default:
		return fmt.Errorf("order %s is not in a no-fill terminal state: %s", intent.ClientOrderID, order.Status)
	}
	actual, err := s.readLegSnapshot(ctx, ex, intent.Symbol)
	if err != nil {
		return fmt.Errorf("verify unchanged %s position: %w", intent.Symbol, err)
	}
	if math.Abs(actual-intent.PositionBefore) > s.legTolerance(ex) {
		return fmt.Errorf("position changed while order %s was pending (before %.8f, now %.8f)", intent.ClientOrderID, intent.PositionBefore, actual)
	}
	state.IntentInFlight = false
	state.PendingOrder = nil
	state.ExposureUnknown = false
	return nil
}

func (s *FundingPerpSpreadStrategy) exchangeForPendingOrder(intent *fundingPerpSpreadOrderIntent) (exchange.IExchange, bool) {
	if strings.EqualFold(intent.LegExchange, s.legA.GetName()) && strings.EqualFold(intent.Symbol, s.symA) {
		return s.legA, true
	}
	if strings.EqualFold(intent.LegExchange, s.legB.GetName()) && strings.EqualFold(intent.Symbol, s.symB) {
		return s.legB, true
	}
	return nil, false
}

func (s *FundingPerpSpreadStrategy) resetStartAfterFailure() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.ctx, s.cancel, s.runDone = nil, nil, nil
	s.started = false
	s.ownershipReady = false
	s.mu.Unlock()
}

func (s *FundingPerpSpreadStrategy) readLegSnapshot(ctx context.Context, ex exchange.IExchange, symbol string) (float64, error) {
	size, err := netFutSize(ctx, ex, symbol)
	if err != nil {
		return 0, fmt.Errorf("read positions: %w", err)
	}
	orders, err := ex.GetOpenOrders(ctx, symbol)
	if err != nil {
		return 0, fmt.Errorf("read open orders: %w", err)
	}
	if orders == nil {
		return 0, fmt.Errorf("open-order snapshot for %s is nil; order state is unverified", symbol)
	}
	if len(orders) != 0 {
		return 0, fmt.Errorf("%d open order(s) exist", len(orders))
	}
	return size, nil
}

func (s *FundingPerpSpreadStrategy) Stop() error {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	s.mu.RLock()
	cancel, runDone := s.cancel, s.runDone
	s.mu.RUnlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if runDone != nil {
		select {
		case <-runDone:
		case <-time.After(20 * time.Second):
			return errors.New("funding_perp_spread run loop did not stop; positions left unchanged for safety")
		}
	}
	if s.stopped {
		return nil
	}
	ctx, stopClose := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopClose()
	s.stopErr = s.closeAll(ctx, "strategy_stop")
	if s.stopErr == nil {
		s.stopped = true
	}
	return s.stopErr
}

// PrepareShutdown stops new strategy decisions and waits for any in-flight
// two-leg operation to finish. It deliberately leaves positions unchanged.
func (s *FundingPerpSpreadStrategy) PrepareShutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("funding_perp_spread shutdown requires a context")
	}
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	return s.prepareShutdownLocked(ctx)
}

// CloseForShutdown closes and verifies both exchange legs after the run loop
// has stopped. A successful primary-leg result is never treated as success for
// the secondary leg.
func (s *FundingPerpSpreadStrategy) CloseForShutdown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("funding_perp_spread close requires a context")
	}
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	s.mu.RLock()
	stopped := s.stopped
	s.mu.RUnlock()
	if stopped {
		return nil
	}
	if err := s.prepareShutdownLocked(ctx); err != nil {
		return err
	}
	if err := s.closeAll(ctx, "process_shutdown"); err != nil {
		s.mu.Lock()
		s.stopErr = err
		s.mu.Unlock()
		return err
	}
	if err := s.VerifyFlat(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.stopped = true
	s.stopErr = nil
	s.mu.Unlock()
	return nil
}

// VerifyFlat proves both legs are flat under the same coordination locks used
// for strategy actions, preventing a concurrent strategy operation from
// invalidating the final shutdown snapshot.
func (s *FundingPerpSpreadStrategy) VerifyFlat(ctx context.Context) error {
	if ctx == nil {
		return errors.New("funding_perp_spread flatness verification requires a context")
	}
	return s.withLegCoordination(ctx, func(coordCtx context.Context) error {
		for _, leg := range []struct {
			ex     exchange.IExchange
			symbol string
		}{{s.legA, s.symA}, {s.legB, s.symB}} {
			actual, err := s.readLegSnapshot(coordCtx, leg.ex, leg.symbol)
			if err != nil {
				return fmt.Errorf("verify shutdown leg %s: %w", leg.symbol, err)
			}
			if math.Abs(actual) > s.legTolerance(leg.ex) {
				return fmt.Errorf("shutdown leg %s remains exposed: %.8f", leg.symbol, actual)
			}
		}
		return nil
	})
}

func (s *FundingPerpSpreadStrategy) prepareShutdownLocked(ctx context.Context) error {
	s.mu.RLock()
	cancel, runDone := s.cancel, s.runDone
	s.mu.RUnlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if runDone == nil {
		return errors.New("funding_perp_spread run-loop completion is unavailable")
	}
	select {
	case <-runDone:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("funding_perp_spread run loop did not stop; positions left unchanged: %w", ctx.Err())
	}
}

func (s *FundingPerpSpreadStrategy) OnPriceChange(float64) error               { return nil }
func (s *FundingPerpSpreadStrategy) OnOrderUpdate(*position.OrderUpdate) error { return nil }
func (s *FundingPerpSpreadStrategy) GetPositions() []*Position                 { return nil }
func (s *FundingPerpSpreadStrategy) GetOrders() []*Order                       { return nil }
func (s *FundingPerpSpreadStrategy) GetStatistics() *StrategyStatistics        { return nil }

func fundingSpreadExchangeName(ex exchange.IExchange) string {
	if ex == nil {
		return ""
	}
	return ex.GetName()
}

func (s *FundingPerpSpreadStrategy) GetVisualizationData() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ownershipVerified := s.ownershipReady && !s.exposureUnknown && !s.intentInFlight
	return map[string]interface{}{
		"type":                 "funding_perp_spread",
		"ownership_verified":   ownershipVerified,
		"leg_a_exchange":       fundingSpreadExchangeName(s.legA),
		"leg_a_symbol":         s.symA,
		"leg_a_owned_quantity": s.ownedA,
		"leg_b_exchange":       fundingSpreadExchangeName(s.legB),
		"leg_b_symbol":         s.symB,
		"leg_b_owned_quantity": s.ownedB,
	}
}

func (s *FundingPerpSpreadStrategy) clearEmergencyCloseRequired() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if math.Abs(s.ownedA) > s.legTolerance(s.legA) || math.Abs(s.ownedB) > s.legTolerance(s.legB) || s.exposureUnknown || s.intentInFlight {
		return errors.New("cannot clear interrupted-exposure recovery before verified flat ownership")
	}
	s.emergencyCloseRequired = false
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.emergencyCloseRequired = true
		return err
	}
	return nil
}

func (s *FundingPerpSpreadStrategy) runLoop() {
	defer close(s.runDone)
	ticker := time.NewTicker(s.tickInt)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.tick(); err != nil {
				s.mu.Lock()
				s.consecutiveErrors++
				n := s.consecutiveErrors
				s.mu.Unlock()
				logger.Warn("⚠️ [funding_perp_spread] tick error (%d): %v", n, err)
			} else {
				s.mu.Lock()
				s.consecutiveErrors = 0
				s.mu.Unlock()
			}
		}
	}
}

func (s *FundingPerpSpreadStrategy) tick() error {
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()

	infoA, err := s.legA.GetFundingInfo(ctx, s.symA)
	if err != nil {
		return fmt.Errorf("legA GetFundingInfo: %w", err)
	}
	infoB, err := s.legB.GetFundingInfo(ctx, s.symB)
	if err != nil {
		return fmt.Errorf("legB GetFundingInfo: %w", err)
	}
	rA, err := normalizeFundingRateToEightHours(infoA, s.symA)
	if err != nil {
		return fmt.Errorf("legA funding rate: %w", err)
	}
	rB, err := normalizeFundingRateToEightHours(infoB, s.symB)
	if err != nil {
		return fmt.Errorf("legB funding rate: %w", err)
	}
	spread := math.Abs(rA - rB)
	if rA == rB {
		spread = 0
	}

	pxA, err := s.legA.GetLatestPrice(ctx, s.symA)
	if err != nil {
		return err
	}
	pxB, err := s.legB.GetLatestPrice(ctx, s.symB)
	if err != nil {
		return err
	}
	basisPct := math.Abs(pxA-pxB) / math.Max(1e-12, (pxA+pxB)/2) * 100

	posA, err := netFutSize(ctx, s.legA, s.symA)
	if err != nil {
		return err
	}
	posB, err := netFutSize(ctx, s.legB, s.symB)
	if err != nil {
		return err
	}
	if err := s.verifyOwnedExposure(posA, posB); err != nil {
		return err
	}
	hasPos := posA != 0 || posB != 0
	if hasPos && posA != 0 && posB != 0 {
		if err := validateFundingPerpSpreadNotional(posA, posB, pxA, pxB, s.maxBasis); err != nil {
			logger.Warn("⚠️ [funding_perp_spread] 兩腿名義敞口失衡，執行受管平倉: %v", err)
			return s.closeAll(ctx, "hedge_notional_imbalance")
		}
	}

	if hasPos && posA != 0 && posB != 0 && posA*posB > 0 {
		logger.Warn("⚠️ [funding_perp_spread] 兩腿同向 posA=%.8f posB=%.8f", posA, posB)
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"message": "雙永续兩腿同向，請手動檢查", "pos_a": posA, "pos_b": posB,
		})
	}
	if hasPos && !fundingPerpSpreadCarryDirectionFavorable(posA, posB, rA, rB) {
		logger.Info("📉 [funding_perp_spread] 費率排序已不支持當前持倉方向，執行受管平倉")
		return s.closeAll(ctx, "funding_direction_reversed")
	}

	if hasPos && spread < s.exitSpread {
		logger.Info("📉 [funding_perp_spread] 價差 %.6f < 退出 %.6f，平倉", spread, s.exitSpread)
		return s.closeAll(ctx, "exit_spread")
	}

	if hasPos {
		return nil
	}

	if spread < s.minSpread {
		return nil
	}
	if basisPct > s.maxBasis {
		logger.Warn("⚠️ [funding_perp_spread] 基差 %.4f%% > 上限 %.4f%%，暫不開倉", basisPct, s.maxBasis)
		return nil
	}

	// 高費率所做空，低費率所做多
	var shortEx exchange.IExchange
	var shortSym string
	var longEx exchange.IExchange
	var longSym string
	if rA >= rB {
		shortEx, shortSym, longEx, longSym = s.legA, s.symA, s.legB, s.symB
	} else {
		shortEx, shortSym, longEx, longSym = s.legB, s.symB, s.legA, s.symA
	}

	return s.openSpread(ctx, shortEx, shortSym, longEx, longSym, pxA, pxB, rA, rB)
}

func normalizeFundingRateToEightHours(info *exchange.FundingInfo, symbol string) (float64, error) {
	if info == nil {
		return 0, fmt.Errorf("funding info is nil for %s", symbol)
	}
	if !strings.EqualFold(strings.TrimSpace(info.Symbol), strings.TrimSpace(symbol)) {
		return 0, fmt.Errorf("funding info symbol mismatch: got %q, want %q", info.Symbol, symbol)
	}
	if info.FundingInterval <= 0 || info.FundingInterval > 24*time.Hour {
		return 0, fmt.Errorf("unsupported or unknown funding interval for %s: %s", symbol, info.FundingInterval)
	}
	if math.IsNaN(info.Rate) || math.IsInf(info.Rate, 0) {
		return 0, fmt.Errorf("non-finite funding rate for %s", symbol)
	}
	rate := info.Rate * float64(8*time.Hour) / float64(info.FundingInterval)
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, fmt.Errorf("normalized funding rate is non-finite for %s", symbol)
	}
	return rate, nil
}

func validateFundingPerpSpreadNotional(posA, posB, priceA, priceB, maxImbalancePct float64) error {
	if math.IsNaN(priceA) || math.IsInf(priceA, 0) || priceA <= 0 ||
		math.IsNaN(priceB) || math.IsInf(priceB, 0) || priceB <= 0 ||
		math.IsNaN(maxImbalancePct) || math.IsInf(maxImbalancePct, 0) || maxImbalancePct < 0 {
		return fmt.Errorf("invalid hedge price or imbalance threshold")
	}
	notionalA := math.Abs(posA) * priceA
	notionalB := math.Abs(posB) * priceB
	if math.IsNaN(notionalA) || math.IsInf(notionalA, 0) || math.IsNaN(notionalB) || math.IsInf(notionalB, 0) {
		return fmt.Errorf("non-finite hedge notional")
	}
	denominator := math.Max(notionalA, notionalB)
	if denominator == 0 {
		return nil
	}
	imbalancePct := math.Abs(notionalA-notionalB) / denominator * 100
	if imbalancePct > maxImbalancePct {
		return fmt.Errorf("notional imbalance %.6f%% exceeds allowed %.6f%% (A %.8f, B %.8f)", imbalancePct, maxImbalancePct, notionalA, notionalB)
	}
	return nil
}

func fundingPerpSpreadCarryDirectionFavorable(posA, posB, rateA, rateB float64) bool {
	switch {
	case posA < 0 && posB > 0:
		return rateA >= rateB
	case posA > 0 && posB < 0:
		return rateB > rateA
	default:
		return false
	}
}

func netFutSize(ctx context.Context, ex exchange.IExchange, sym string) (float64, error) {
	pos, err := readScopedPositionSnapshot(ctx, ex, sym)
	if err != nil {
		return 0, err
	}
	if pos == nil {
		return 0, fmt.Errorf("position snapshot for %s is nil; exposure is unverified", sym)
	}
	var sum float64
	var positive, negative bool
	for _, p := range pos {
		if p == nil {
			return 0, fmt.Errorf("position snapshot for %s contains a null entry", sym)
		}
		if math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
			return 0, fmt.Errorf("invalid position size for %s: %v", sym, p.Size)
		}
		positive = positive || p.Size > 0
		negative = negative || p.Size < 0
		sum += p.Size
	}
	if positive && negative {
		return 0, fmt.Errorf("both long and short positions exist for %s; net exposure cannot prove ownership", sym)
	}
	return sum, nil
}

func (s *FundingPerpSpreadStrategy) legTolerance(ex exchange.IExchange) float64 {
	decimals := ex.GetQuantityDecimals()
	if decimals < 0 {
		decimals = 0
	}
	return math.Pow10(-decimals) / 2
}

func (s *FundingPerpSpreadStrategy) exposureSnapshot(ctx context.Context) (float64, float64, error) {
	posA, err := s.readLegSnapshot(ctx, s.legA, s.symA)
	if err != nil {
		return 0, 0, fmt.Errorf("read legA exposure: %w", err)
	}
	posB, err := s.readLegSnapshot(ctx, s.legB, s.symB)
	if err != nil {
		return 0, 0, fmt.Errorf("read legB exposure: %w", err)
	}
	return posA, posB, nil
}

func (s *FundingPerpSpreadStrategy) verifyOwnedExposure(posA, posB float64) error {
	s.mu.Lock()
	if !s.ownershipReady || s.exposureUnknown || s.intentInFlight {
		s.mu.Unlock()
		return errors.New("funding_perp_spread ownership is not verified; trading is blocked")
	}
	validA := math.Abs(posA-s.ownedA) <= s.legTolerance(s.legA)
	validB := math.Abs(posB-s.ownedB) <= s.legTolerance(s.legB)
	if !validA || !validB {
		s.exposureUnknown = true
		ownedA, ownedB := s.ownedA, s.ownedB
		persistErr := s.persistRuntimeStateLocked()
		s.mu.Unlock()
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"message":    "交易所實際敞口與本策略記賬不一致，已鎖定自動交易和平倉，需人工核對",
			"position_a": posA, "owned_a": ownedA,
			"position_b": posB, "owned_b": ownedB,
		})
		return errors.Join(fmt.Errorf("actual exposure differs from strategy ownership (legA %.8f/%.8f, legB %.8f/%.8f)", posA, ownedA, posB, ownedB), persistErr)
	}
	s.mu.Unlock()
	return nil
}

func (s *FundingPerpSpreadStrategy) recordOpenedLeg(ex exchange.IExchange, symbol string, actual, requested float64, side exchange.Side) error {
	if math.IsNaN(actual) || math.IsInf(actual, 0) || math.Abs(actual) > requested+s.legTolerance(ex) {
		s.mu.Lock()
		s.exposureUnknown = true
		s.mu.Unlock()
		return fmt.Errorf("opened exposure does not match requested leg size: actual=%.8f requested=%.8f", actual, requested)
	}
	if actual == 0 || (side == exchange.SideSell && actual > 0) || (side == exchange.SideBuy && actual < 0) {
		s.mu.Lock()
		s.exposureUnknown = true
		s.mu.Unlock()
		return fmt.Errorf("opened leg has no confirmed exposure in the requested direction: %.8f", actual)
	}
	s.mu.Lock()
	switch {
	case strings.EqualFold(ex.GetName(), s.legA.GetName()) && strings.EqualFold(symbol, s.symA):
		s.ownedA = actual
	case strings.EqualFold(ex.GetName(), s.legB.GetName()) && strings.EqualFold(symbol, s.symB):
		s.ownedB = actual
	default:
		s.exposureUnknown = true
		s.mu.Unlock()
		return errors.New("opened order does not match either configured leg")
	}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.exposureUnknown = true
		s.mu.Unlock()
		return fmt.Errorf("persist opened leg ownership: %w", err)
	}
	s.mu.Unlock()
	return nil
}

func (s *FundingPerpSpreadStrategy) beginOrderIntent(intent fundingPerpSpreadOrderIntent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ownershipReady || s.exposureUnknown || s.intentInFlight {
		return errors.New("funding_perp_spread execution state is unresolved; new order blocked")
	}
	if strings.TrimSpace(intent.ClientOrderID) == "" || strings.TrimSpace(intent.LegExchange) == "" ||
		strings.TrimSpace(intent.Symbol) == "" || (intent.Side != "BUY" && intent.Side != "SELL") ||
		math.IsNaN(intent.Quantity) || math.IsInf(intent.Quantity, 0) || intent.Quantity <= 0 ||
		math.IsNaN(intent.PositionBefore) || math.IsInf(intent.PositionBefore, 0) {
		return errors.New("funding_perp_spread order intent identity is invalid")
	}
	s.intentInFlight = true
	s.pendingOrder = cloneFundingPerpSpreadOrderIntent(&intent)
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.exposureUnknown = true
		return fmt.Errorf("persist order intent before exchange submission: %w", err)
	}
	return nil
}

func (s *FundingPerpSpreadStrategy) finishOrderIntent() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intentInFlight = false
	pending := s.pendingOrder
	s.pendingOrder = nil
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.intentInFlight = true
		s.pendingOrder = pending
		s.exposureUnknown = true
		return fmt.Errorf("persist reconciled order result: %w", err)
	}
	return nil
}

func (s *FundingPerpSpreadStrategy) markExposureUnknown() {
	s.mu.Lock()
	s.exposureUnknown = true
	_ = s.persistRuntimeStateLocked()
	s.mu.Unlock()
}

func (s *FundingPerpSpreadStrategy) retainExecutionLedgerFailure(client exchange.IExchange, request *exchange.OrderRequest, order *exchange.Order) {
	if err := s.persistPendingExecution(client, request, order); err != nil {
		logger.Error("funding_perp_spread execution-ledger block could not be persisted; exposure remains unknown: %v", err)
	}
	gate := s.openingGate
	if gate != nil {
		gate.Block("execution_ledger_unverified")
	}
}

func (s *FundingPerpSpreadStrategy) persistPendingExecution(client exchange.IExchange, request *exchange.OrderRequest, order *exchange.Order) error {
	s.mu.Lock()
	s.executionLedgerUnverified = true
	if client != nil && request != nil && request.ClientOrderID != "" && request.Symbol != "" &&
		(request.Side == exchange.SideBuy || request.Side == exchange.SideSell) && request.Quantity > 0 &&
		!math.IsNaN(request.Quantity) && !math.IsInf(request.Quantity, 0) {
		pending := fundingPerpSpreadPendingExecutionState{
			Exchange: client.GetName(), Symbol: request.Symbol, ClientOrderID: request.ClientOrderID,
			Side: string(request.Side), Quantity: request.Quantity,
		}
		if order != nil && order.OrderID > 0 {
			pending.OrderID = order.OrderID
		}
		updated := false
		for index := range s.pendingExecutions {
			current := &s.pendingExecutions[index]
			if strings.EqualFold(current.Exchange, pending.Exchange) && strings.EqualFold(current.Symbol, pending.Symbol) && current.ClientOrderID == pending.ClientOrderID {
				if pending.OrderID > 0 {
					current.OrderID = pending.OrderID
				}
				current.Side, current.Quantity = pending.Side, pending.Quantity
				updated = true
				break
			}
		}
		if !updated {
			s.pendingExecutions = append(s.pendingExecutions, pending)
		}
	}
	persistErr := s.persistRuntimeStateLocked()
	if persistErr != nil {
		s.exposureUnknown = true
	}
	gate := s.openingGate
	s.mu.Unlock()
	if gate != nil {
		gate.Block("execution_ledger_unverified")
	}
	return persistErr
}

func (s *FundingPerpSpreadStrategy) resolvePendingExecution(client exchange.IExchange, request *exchange.OrderRequest, order *exchange.Order) (bool, error) {
	if client == nil || request == nil {
		return false, errors.New("cannot resolve a pending execution without its exact exchange request")
	}
	s.mu.Lock()
	oldPending := cloneFundingPerpSpreadPendingExecutions(s.pendingExecutions)
	filtered := make([]fundingPerpSpreadPendingExecutionState, 0, len(oldPending))
	removed := false
	orderID := int64(0)
	if order != nil {
		orderID = order.OrderID
	}
	for _, pending := range oldPending {
		matches := strings.EqualFold(pending.Exchange, client.GetName()) && strings.EqualFold(pending.Symbol, request.Symbol) && pending.ClientOrderID == request.ClientOrderID
		if matches && (pending.OrderID == 0 || pending.OrderID == orderID) {
			removed = true
			continue
		}
		filtered = append(filtered, pending)
	}
	if !removed {
		s.mu.Unlock()
		return false, nil
	}
	oldBlocked := s.executionLedgerUnverified
	s.pendingExecutions = filtered
	if len(filtered) == 0 {
		s.executionLedgerUnverified = false
	}
	persistErr := s.persistRuntimeStateLocked()
	if persistErr != nil {
		s.pendingExecutions = oldPending
		s.executionLedgerUnverified = true
		s.exposureUnknown = true
	}
	stillBlocked := s.executionLedgerUnverified
	gate := s.openingGate
	s.mu.Unlock()
	if persistErr != nil {
		return false, fmt.Errorf("persist resolved pending execution: %w", persistErr)
	}
	if oldBlocked && !stillBlocked && gate != nil {
		gate.Unblock("execution_ledger_unverified")
	}
	return true, nil
}

func (s *FundingPerpSpreadStrategy) reconcilePendingExecutionLedger(ctx context.Context) error {
	s.mu.RLock()
	pendingExecutions := cloneFundingPerpSpreadPendingExecutions(s.pendingExecutions)
	recorder := s.executionRecorder
	s.mu.RUnlock()
	if len(pendingExecutions) == 0 {
		return nil
	}
	if recorder == nil {
		return errors.New("execution recorder is unavailable during startup reconciliation")
	}
	var reconcileErr error
	for _, pending := range pendingExecutions {
		client := s.executionExchange(pending.Exchange, pending.Symbol)
		if client == nil {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("no configured leg matches pending execution %s:%s", pending.Exchange, pending.Symbol))
			continue
		}
		request := &exchange.OrderRequest{Symbol: pending.Symbol, Side: exchange.Side(pending.Side), Type: exchange.OrderTypeMarket,
			Quantity: pending.Quantity, StrategyType: "funding_perp_spread", ClientOrderID: pending.ClientOrderID}
		var initialOrder *exchange.Order
		if pending.OrderID > 0 {
			initialOrder = &exchange.Order{OrderID: pending.OrderID, ClientOrderID: pending.ClientOrderID, Symbol: pending.Symbol,
				Side: exchange.Side(pending.Side), Quantity: pending.Quantity, Status: exchange.OrderStatusNew}
		}
		resolved, err := recorder(ctx, client, request, initialOrder)
		if err != nil || !resolved {
			if err == nil {
				err = errors.New("order identity remains unverified")
			}
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("reconcile pending order %s on %s: %w", pending.ClientOrderID, pending.Exchange, err))
			continue
		}
		cleared, err := s.resolvePendingExecution(client, request, initialOrder)
		if err != nil {
			reconcileErr = errors.Join(reconcileErr, err)
		} else if !cleared {
			reconcileErr = errors.Join(reconcileErr, fmt.Errorf("pending order %s was verified but did not match its persisted ledger marker", pending.ClientOrderID))
		}
	}
	return reconcileErr
}

func resolveFundingPerpSpreadIntentOrder(ctx context.Context, client exchange.IExchange, intent *fundingPerpSpreadOrderIntent, initial *exchange.Order) (*exchange.Order, error) {
	if ctx == nil || client == nil || intent == nil || initial == nil {
		return nil, errors.New("pending order lacks an exact exchange order identity")
	}
	if fundingPerpSpreadTerminalOrderStatus(initial.Status) {
		return initial, nil
	}
	if initial.OrderID <= 0 {
		return nil, errors.New("nonterminal pending order has no exchange order ID for status reconciliation")
	}
	resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	order := initial
	for !fundingPerpSpreadTerminalOrderStatus(order.Status) {
		refreshed, err := client.GetOrder(resolveCtx, intent.Symbol, order.OrderID)
		if err != nil {
			return nil, fmt.Errorf("query terminal state for pending order %s: %w", intent.ClientOrderID, err)
		}
		if refreshed == nil {
			return nil, fmt.Errorf("query terminal state for pending order %s returned nil", intent.ClientOrderID)
		}
		canonicalCID := utils.RemoveBrokerPrefix(strings.ToLower(client.GetName()), refreshed.ClientOrderID)
		if refreshed.OrderID != order.OrderID || canonicalCID != intent.ClientOrderID || !strings.EqualFold(refreshed.Symbol, intent.Symbol) ||
			string(refreshed.Side) != intent.Side || math.IsNaN(refreshed.Quantity) || math.IsInf(refreshed.Quantity, 0) ||
			math.Abs(refreshed.Quantity-intent.Quantity) > math.Max(1e-12, intent.Quantity*1e-9) ||
			math.IsNaN(refreshed.ExecutedQty) || math.IsInf(refreshed.ExecutedQty, 0) || refreshed.ExecutedQty < order.ExecutedQty ||
			refreshed.ExecutedQty > intent.Quantity+math.Max(1e-12, intent.Quantity*1e-9) {
			return nil, fmt.Errorf("terminal query changed or contradicted pending order %s identity/fill evidence", intent.ClientOrderID)
		}
		order = refreshed
		if fundingPerpSpreadTerminalOrderStatus(order.Status) {
			return order, nil
		}
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-resolveCtx.Done():
			timer.Stop()
			return nil, fmt.Errorf("pending order %s did not reach a terminal state: %w", intent.ClientOrderID, resolveCtx.Err())
		case <-timer.C:
		}
	}
	return order, nil
}

func fundingPerpSpreadTerminalOrderStatus(status exchange.OrderStatus) bool {
	switch strings.ToUpper(strings.TrimSpace(string(status))) {
	case "FILLED", "CANCELED", "CANCELLED", "EXPIRED", "REJECTED", "PARTIALLY_FILLED_CANCELED", "PARTIALLY_FILLED_CANCELLED":
		return true
	default:
		return false
	}
}

func (s *FundingPerpSpreadStrategy) executionExchange(exchangeName, symbol string) exchange.IExchange {
	if strings.EqualFold(exchangeName, s.legA.GetName()) && strings.EqualFold(symbol, s.symA) {
		return s.legA
	}
	if strings.EqualFold(exchangeName, s.legB.GetName()) && strings.EqualFold(symbol, s.symB) {
		return s.legB
	}
	return nil
}

func (s *FundingPerpSpreadStrategy) capitalUSDT() float64 {
	c := s.symCfg.TotalAllocatedCapital
	if c <= 0 {
		c = s.symCfg.OrderQuantity
	}
	return c
}

func (s *FundingPerpSpreadStrategy) openSpread(ctx context.Context, shortEx exchange.IExchange, shortSym string, longEx exchange.IExchange, longSym string, pxA, pxB, rA, rB float64) error {
	return s.withLegCoordination(ctx, func(coordCtx context.Context) error {
		return s.openSpreadCoordinated(coordCtx, shortEx, shortSym, longEx, longSym, pxA, pxB, rA, rB)
	})
}

func (s *FundingPerpSpreadStrategy) openSpreadCoordinated(ctx context.Context, shortEx exchange.IExchange, shortSym string, longEx exchange.IExchange, longSym string, pxA, pxB, rA, rB float64) error {
	cap := s.capitalUSDT()
	if cap < 200 {
		return fmt.Errorf("分配資金 %.2f USDT 過小，建議 ≥200", cap)
	}
	legNotional := cap / 2
	if legNotional < 50 {
		return fmt.Errorf("單腿名義 %.2f USDT 過小", legNotional)
	}
	qtyShort, err := fundingPerpSpreadOrderQuantity(legNotional, pxA, pxB, shortEx.GetQuantityDecimals())
	if err != nil {
		return fmt.Errorf("short leg quantity: %w", err)
	}
	qtyLong, err := fundingPerpSpreadOrderQuantity(legNotional, pxA, pxB, longEx.GetQuantityDecimals())
	if err != nil {
		return fmt.Errorf("long leg quantity: %w", err)
	}
	if qtyShort <= 0 || qtyLong <= 0 {
		return fmt.Errorf("數量精度截斷為 0")
	}
	currentA, err := s.readLegSnapshot(ctx, s.legA, s.symA)
	if err != nil {
		return fmt.Errorf("verify legA before opening: %w", err)
	}
	currentB, err := s.readLegSnapshot(ctx, s.legB, s.symB)
	if err != nil {
		return fmt.Errorf("verify legB before opening: %w", err)
	}
	if err := s.verifyOwnedExposure(currentA, currentB); err != nil {
		return fmt.Errorf("refuse new spread: %w", err)
	}
	if math.Abs(currentA) > s.legTolerance(s.legA) || math.Abs(currentB) > s.legTolerance(s.legB) {
		return errors.New("refuse new spread: an existing owned leg must be flat before opening another allocation")
	}
	s.mu.RLock()
	gate := s.openingGate
	s.mu.RUnlock()
	if gate == nil {
		return fmt.Errorf("funding_perp_spread opening gate is unavailable: %w", execution.ErrExposureUnverified)
	}
	releaseOpening, err := gate.Begin()
	if err != nil {
		return err
	}
	defer releaseOpening()
	shortClientOrderID := utils.NewCompactOrderID()
	shortPositionBefore := currentA
	if strings.EqualFold(shortEx.GetName(), s.legB.GetName()) && strings.EqualFold(shortSym, s.symB) {
		shortPositionBefore = currentB
	}
	if err := s.beginOrderIntent(fundingPerpSpreadOrderIntent{
		ClientOrderID: shortClientOrderID, LegExchange: shortEx.GetName(), Symbol: shortSym, Side: "SELL", Quantity: qtyShort, PositionBefore: shortPositionBefore,
	}); err != nil {
		return err
	}

	shortRequest := &exchange.OrderRequest{
		Symbol: shortSym, Side: exchange.SideSell, Type: exchange.OrderTypeMarket,
		Quantity: qtyShort, Price: 0, PriceDecimals: shortEx.GetPriceDecimals(),
		StrategyType: "funding_perp_spread", ClientOrderID: shortClientOrderID,
	}
	shortOrder, err := shortEx.PlaceOrder(ctx, shortRequest)
	shortActual, readErr := s.readLegSnapshot(ctx, shortEx, shortSym)
	if readErr != nil {
		s.markExposureUnknown()
		return fmt.Errorf("open-short result cannot be reconciled: %w", readErr)
	}
	if shortActual != 0 {
		if captureErr := s.recordOpenedLeg(shortEx, shortSym, shortActual, qtyShort, exchange.SideSell); captureErr != nil {
			return fmt.Errorf("open short ownership unresolved: %w", captureErr)
		}
	}
	if err != nil {
		s.markExposureUnknown()
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{"leg": "short", "error": err.Error()})
		_, captureErr := s.recordOrderExecution(ctx, shortEx, shortRequest, shortOrder)
		return errors.Join(fmt.Errorf("開空: %w", err), captureErr)
	}
	if shortActual == 0 {
		s.markExposureUnknown()
		return errors.New("short order returned success but no position change was confirmed")
	}
	if err := s.persistPendingExecution(shortEx, shortRequest, shortOrder); err != nil {
		return fmt.Errorf("short leg is open but pending execution identity could not be persisted: %w", err)
	}
	if err := s.finishOrderIntent(); err != nil {
		return err
	}
	longClientOrderID := utils.NewCompactOrderID()
	longPositionBefore := currentA
	if strings.EqualFold(longEx.GetName(), s.legB.GetName()) && strings.EqualFold(longSym, s.symB) {
		longPositionBefore = currentB
	}
	if err := s.beginOrderIntent(fundingPerpSpreadOrderIntent{
		ClientOrderID: longClientOrderID, LegExchange: longEx.GetName(), Symbol: longSym, Side: "BUY", Quantity: qtyLong, PositionBefore: longPositionBefore,
	}); err != nil {
		return fmt.Errorf("short leg is open but long-leg intent could not be persisted: %w", err)
	}
	longRequest := &exchange.OrderRequest{
		Symbol: longSym, Side: exchange.SideBuy, Type: exchange.OrderTypeMarket,
		Quantity: qtyLong, Price: 0, PriceDecimals: longEx.GetPriceDecimals(),
		StrategyType: "funding_perp_spread", ClientOrderID: longClientOrderID,
	}
	longOrder, err := longEx.PlaceOrder(ctx, longRequest)
	longActual, readErr := s.readLegSnapshot(ctx, longEx, longSym)
	if readErr != nil {
		s.markExposureUnknown()
		return fmt.Errorf("open-long result cannot be reconciled; short leg may remain open: %w", readErr)
	}
	if longActual != 0 {
		if captureErr := s.recordOpenedLeg(longEx, longSym, longActual, qtyLong, exchange.SideBuy); captureErr != nil {
			return fmt.Errorf("open long ownership unresolved; short leg may remain open: %w", captureErr)
		}
	}
	if err != nil {
		s.markExposureUnknown()
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"message": "開多失敗，空頭已成交，請手動處理", "error": err.Error(),
		})
		_, shortCaptureErr := s.recordOrderExecution(ctx, shortEx, shortRequest, shortOrder)
		_, longCaptureErr := s.recordOrderExecution(ctx, longEx, longRequest, longOrder)
		return errors.Join(fmt.Errorf("開多失敗（空頭已下單）: %w", err), shortCaptureErr, longCaptureErr)
	}
	if longActual == 0 {
		s.markExposureUnknown()
		return errors.New("long order returned success but no position change was confirmed; short leg may remain open")
	}
	if err := s.persistPendingExecution(longEx, longRequest, longOrder); err != nil {
		return fmt.Errorf("both leg positions are open but long pending execution identity could not be persisted: %w", err)
	}
	if err := s.finishOrderIntent(); err != nil {
		return fmt.Errorf("both leg positions are open but result-state persistence failed: %w", err)
	}
	logger.Info("✅ [funding_perp_spread] 已建倉 short=%s qty=%.8f long=%s qty=%.8f rA=%.6f rB=%.6f",
		shortSym, qtyShort, longSym, qtyLong, rA, rB)
	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{
		"short_symbol": shortSym, "long_symbol": longSym,
		"r_a": rA, "r_b": rB, "spread": math.Abs(rA - rB),
	})
	_, shortCaptureErr := s.recordOrderExecution(ctx, shortEx, shortRequest, shortOrder)
	_, longCaptureErr := s.recordOrderExecution(ctx, longEx, longRequest, longOrder)
	return errors.Join(shortCaptureErr, longCaptureErr)
}

func (s *FundingPerpSpreadStrategy) closeAll(ctx context.Context, reason string) error {
	return s.withLegCoordination(ctx, func(coordCtx context.Context) error {
		return s.closeAllCoordinated(coordCtx, reason)
	})
}

func (s *FundingPerpSpreadStrategy) closeAllCoordinated(ctx context.Context, reason string) error {
	posA, posB, err := s.exposureSnapshot(ctx)
	if err != nil {
		return err
	}
	if err := s.verifyOwnedExposure(posA, posB); err != nil {
		return err
	}
	s.mu.RLock()
	ownedA, ownedB := s.ownedA, s.ownedB
	s.mu.RUnlock()
	captures := make([]fundingPerpSpreadPendingExecution, 0, 2)
	for _, leg := range []struct {
		ex  exchange.IExchange
		sym string
		qty float64
	}{{s.legA, s.symA, ownedA}, {s.legB, s.symB, ownedB}} {
		capture, err := s.closeLegWithoutExecutionCapture(ctx, leg.ex, leg.sym, leg.qty)
		if capture != nil {
			captures = append(captures, *capture)
		}
		if err != nil {
			return errors.Join(err, s.recordPendingExecutions(ctx, captures))
		}
	}
	ledgerErr := s.recordPendingExecutions(ctx, captures)
	logger.Info("✅ [funding_perp_spread] 平倉完成 reason=%s", reason)
	s.publishEvent(event.EventTypePositionClosed, map[string]interface{}{"reason": reason})
	return ledgerErr
}

func (s *FundingPerpSpreadStrategy) closeLeg(ctx context.Context, ex exchange.IExchange, sym string, owned float64) error {
	capture, err := s.closeLegWithoutExecutionCapture(ctx, ex, sym, owned)
	if capture == nil {
		return err
	}
	return errors.Join(err, s.recordPendingExecutions(ctx, []fundingPerpSpreadPendingExecution{*capture}))
}

func (s *FundingPerpSpreadStrategy) closeLegWithoutExecutionCapture(ctx context.Context, ex exchange.IExchange, sym string, owned float64) (*fundingPerpSpreadPendingExecution, error) {
	actual, err := s.readLegSnapshot(ctx, ex, sym)
	if err != nil {
		return nil, fmt.Errorf("read %s position before close: %w", sym, err)
	}
	if math.Abs(actual-owned) > s.legTolerance(ex) {
		return nil, fmt.Errorf("refusing to close %s: exchange exposure %.8f differs from strategy-owned %.8f", sym, actual, owned)
	}
	if owned == 0 {
		return nil, nil
	}
	side := exchange.SideBuy
	if owned > 0 {
		side = exchange.SideSell
	}
	clientOrderID := utils.NewCompactOrderID()
	if err := s.beginOrderIntent(fundingPerpSpreadOrderIntent{
		ClientOrderID: clientOrderID, LegExchange: ex.GetName(), Symbol: sym, Side: string(side), Quantity: math.Abs(owned), PositionBefore: actual,
	}); err != nil {
		return nil, err
	}
	request := &exchange.OrderRequest{
		Symbol: sym, Side: side, Type: exchange.OrderTypeMarket,
		Quantity: math.Abs(owned), ReduceOnly: true, PriceDecimals: ex.GetPriceDecimals(),
		StrategyType: "funding_perp_spread", ClientOrderID: clientOrderID,
	}
	order, orderErr := ex.PlaceOrder(ctx, request)
	capture := &fundingPerpSpreadPendingExecution{client: ex, request: request, order: order}
	after, readErr := netFutSize(ctx, ex, sym)
	if readErr != nil {
		s.markExposureUnknown()
		return capture, fmt.Errorf("close %s result cannot be reconciled: %w", sym, readErr)
	}
	orders, ordersErr := ex.GetOpenOrders(ctx, sym)
	if ordersErr != nil || orders == nil || len(orders) > 0 {
		s.markExposureUnknown()
		return capture, fmt.Errorf("close %s remains unverified because open-order evidence is incomplete or orders may remain (count=%d, nil_snapshot=%t, error=%v)", sym, len(orders), orders == nil, ordersErr)
	}
	if math.Abs(after) <= s.legTolerance(ex) {
		if err := s.setOwnedLeg(ex, sym, 0); err != nil {
			s.markExposureUnknown()
			return capture, err
		}
		if err := s.persistPendingExecution(ex, request, order); err != nil {
			return capture, fmt.Errorf("position closed but pending execution identity could not be persisted: %w", err)
		}
		if err := s.finishOrderIntent(); err != nil {
			return capture, err
		}
		return capture, nil
	}
	if err := s.setOwnedLeg(ex, sym, after); err != nil {
		s.markExposureUnknown()
		return capture, err
	}
	if err := s.persistPendingExecution(ex, request, order); err != nil {
		return capture, fmt.Errorf("position changed but pending execution identity could not be persisted: %w", err)
	}
	if err := s.finishOrderIntent(); err != nil {
		return capture, err
	}
	if orderErr != nil {
		s.markExposureUnknown()
		return capture, fmt.Errorf("close %s result remains uncertain at exposure %.8f (order error: %v)", sym, after, orderErr)
	}
	if math.Abs(after) > s.legTolerance(ex) {
		return capture, fmt.Errorf("close %s left residual exposure %.8f", sym, after)
	}
	return capture, fmt.Errorf("close %s was acknowledged but exposure remains %.8f", sym, after)
}

func (s *FundingPerpSpreadStrategy) recordPendingExecutions(ctx context.Context, executions []fundingPerpSpreadPendingExecution) error {
	var captureErr error
	for _, execution := range executions {
		_, err := s.recordOrderExecution(ctx, execution.client, execution.request, execution.order)
		captureErr = errors.Join(captureErr, err)
	}
	return captureErr
}

func (s *FundingPerpSpreadStrategy) setOwnedLeg(ex exchange.IExchange, symbol string, size float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.EqualFold(ex.GetName(), s.legA.GetName()) && strings.EqualFold(symbol, s.symA) {
		s.ownedA = size
	} else if strings.EqualFold(ex.GetName(), s.legB.GetName()) && strings.EqualFold(symbol, s.symB) {
		s.ownedB = size
	} else {
		s.exposureUnknown = true
		return errors.New("cannot assign closed position to a configured strategy leg")
	}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.exposureUnknown = true
		return fmt.Errorf("persist updated owned position: %w", err)
	}
	return nil
}

func (s *FundingPerpSpreadStrategy) publishEvent(typ event.EventType, data map[string]interface{}) {
	if s.eventBus == nil {
		return
	}
	s.eventBus.Publish(&event.Event{Type: typ, Data: data})
}

func roundPerpQty(q float64, decimals int) float64 {
	if decimals <= 0 {
		return math.Round(q)
	}
	p := math.Pow10(decimals)
	return math.Round(q*p) / p
}

func fundingPerpSpreadOrderQuantity(legNotional, priceA, priceB float64, decimals int) (float64, error) {
	if math.IsNaN(legNotional) || math.IsInf(legNotional, 0) || legNotional <= 0 ||
		math.IsNaN(priceA) || math.IsInf(priceA, 0) || priceA <= 0 ||
		math.IsNaN(priceB) || math.IsInf(priceB, 0) || priceB <= 0 || decimals < 0 {
		return 0, fmt.Errorf("invalid notional, prices, or quantity precision")
	}
	quantity := legNotional / math.Max(priceA, priceB)
	if math.IsNaN(quantity) || math.IsInf(quantity, 0) || quantity <= 0 {
		return 0, fmt.Errorf("invalid target quantity")
	}
	precision := math.Pow10(decimals)
	if math.IsInf(precision, 0) || precision <= 0 {
		return 0, fmt.Errorf("unsupported quantity precision %d", decimals)
	}
	quantity = math.Floor(quantity*precision) / precision
	if math.IsNaN(quantity) || math.IsInf(quantity, 0) {
		return 0, fmt.Errorf("rounded quantity is invalid")
	}
	return quantity, nil
}
