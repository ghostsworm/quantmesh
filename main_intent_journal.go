package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/logger"
	"quantmesh/order"
	"quantmesh/position"
)

const runtimeIntentBootstrapBlock = "execution_bootstrap_unverified"
const strategyIntentSettlementBlock = "strategy_execution_intent_unverified"

// Claim/persist venue evidence before any slot, strategy or capital consumer.
// Matching a symbol alone is not proof of Bot ownership on a shared account.
func observeOwnedRuntimeOrder(executor *order.ExchangeOrderExecutor, update *position.OrderUpdate) bool {
	if update == nil {
		return false
	}
	return executor.ObserveOrder(&exchange.Order{OrderID: update.OrderID, ClientOrderID: update.ClientOrderID,
		Symbol: update.Symbol, Side: exchange.Side(update.Side), Status: exchange.OrderStatus(update.Status),
		Price: update.Price, AvgPrice: update.AvgPrice, ExecutedQty: update.ExecutedQty})
}

// settleVerifiedStrategyIntent closes the normal positive-fill intent lifecycle
// only after the exact owning strategy has durably applied its terminal update.
// The executor performs a fresh exact-order venue query before persisting the
// settlement, so websocket state alone can never clear the durable intent.
func settleVerifiedStrategyIntent(ctx context.Context, executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate, strategyName string, update *position.OrderUpdate) error {
	if update == nil || !terminalOrderUpdate(update.Status) || update.ExecutedQty <= 0 || math.IsNaN(update.ExecutedQty) || math.IsInf(update.ExecutedQty, 0) {
		return nil
	}
	if executor == nil || gate == nil {
		err := fmt.Errorf("terminal strategy fill cannot be reconciled without its executor and opening gate")
		if gate != nil {
			gate.Block(strategyIntentSettlementBlock)
		}
		return err
	}
	var settleErr error
	defer func() {
		if settleErr != nil {
			gate.Block(strategyIntentSettlementBlock)
		}
	}()
	if strategyName == "" || update.OrderID <= 0 || update.ClientOrderID == "" {
		settleErr = fmt.Errorf("terminal strategy fill is missing its route or exact order identity")
		return settleErr
	}
	clientOrderID, owned := executor.OwnedIntentClientOrderID(update.ClientOrderID)
	if !owned {
		settleErr = fmt.Errorf("terminal strategy update has no matching durable execution intent")
		return settleErr
	}
	ownerStrategy, _, found := executor.IntentStrategyType(clientOrderID)
	if !found || ownerStrategy == "" || ownerStrategy != strategyName {
		settleErr = fmt.Errorf("terminal strategy update route %q does not match durable intent owner %q", strategyName, ownerStrategy)
		return settleErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(ctx, intentSettlementTimeout)
	defer cancel()
	settleErr = executor.SettleIntent(queryCtx, clientOrderID)
	return settleErr
}

// settleVerifiedGridIntent is called only after the grid slot snapshot and
// complete venue fill history have both been durably recorded.
func settleVerifiedGridIntent(ctx context.Context, executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate, update *position.OrderUpdate, accounted bool) error {
	if !accounted || update == nil || !terminalOrderUpdate(update.Status) || update.ExecutedQty <= 0 {
		return fmt.Errorf("grid intent settlement requires durable terminal grid accounting")
	}
	if executor == nil || gate == nil || update.OrderID <= 0 || strings.TrimSpace(update.ClientOrderID) == "" {
		return fmt.Errorf("grid intent settlement is missing its executor, gate, or exact order identity")
	}
	clientOrderID, owned := executor.OwnedIntentClientOrderID(update.ClientOrderID)
	if !owned {
		return fmt.Errorf("terminal grid update has no matching durable execution intent")
	}
	owner, strategyType, found := executor.IntentStrategyType(clientOrderID)
	if !found || owner == "" || strategyType != "grid" {
		return fmt.Errorf("terminal grid update does not match a durable grid intent owner")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	queryCtx, cancel := context.WithTimeout(ctx, intentSettlementTimeout)
	defer cancel()
	return executor.SettleReconciledIntent(queryCtx, clientOrderID, owner)
}

const intentSettlementTimeout = 10 * time.Second

// settleVerifiedGridZeroFill asynchronously settles only a grid intent whose
// strategy slot was durably updated and whose terminal update reports zero fill.
func settleVerifiedGridZeroFill(executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate, update *position.OrderUpdate, accounted bool) {
	if executor == nil || gate == nil || update == nil || !accounted || update.ExecutedQty != 0 ||
		!terminalOrderUpdate(update.Status) {
		return
	}
	canonicalCID, owned := executor.OwnedIntentClientOrderID(update.ClientOrderID)
	_, strategyType, strategyOwned := executor.IntentStrategyType(update.ClientOrderID)
	if !owned || !strategyOwned || strategyType != "grid" || update.OrderID <= 0 {
		return
	}
	block := "grid_zero_fill_settlement:" + canonicalCID
	gate.Block(block)
	copyUpdate := *update
	copyUpdate.ClientOrderID = canonicalCID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), intentSettlementTimeout)
		defer cancel()
		if err := executor.SettleZeroFillIntent(ctx, copyUpdate.ClientOrderID); err != nil {
			markErr := executor.MarkOrderReconciliationRequired(copyUpdate.OrderID, copyUpdate.ClientOrderID, err.Error())
			logger.Error("[%s] 网格零成交终态意图未能安全结算，保持开仓阻断: cid=%s settle_err=%v journal_err=%v",
				copyUpdate.Symbol, copyUpdate.ClientOrderID, err, markErr)
			return
		}
		gate.Unblock(block)
	}()
}

type runtimeIntentBackend interface {
	execution.IntentJournal
	HasLegacyExecutionHistory(context.Context, execution.IntentScope) (bool, error)
	HasVerifiedExecutionOrderIDs(context.Context, execution.IntentScope, []execution.ExposurePosition) (bool, error)
}

// The ordinary entry point permits only an empty venue account. Grid startup
// may skip the futures-position emptiness check only when a separate bootstrap
// reconciles the restored owner inventory exactly before seeding exposure.
// Neither an empty journal nor a terminal order implies settled strategy state.
func configureRuntimeIntentJournal(ctx context.Context, executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate, ex exchange.IExchange, backend runtimeIntentBackend, scope execution.IntentScope) error {
	return configureRuntimeIntentJournalWithGridRecovery(ctx, executor, gate, ex, backend, scope, false)
}

func configureRuntimeIntentJournalWithGridRecovery(ctx context.Context, executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate, ex exchange.IExchange, backend runtimeIntentBackend, scope execution.IntentScope, allowRestoredFuturesPosition bool) error {
	gate.Block(runtimeIntentBootstrapBlock)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := executor.ConfigureIntentJournal(ctx, backend, scope); err != nil {
		return err
	}
	legacy, err := backend.HasLegacyExecutionHistory(ctx, scope)
	if err != nil {
		return fmt.Errorf("verify legacy execution history: %w", err)
	}
	if legacy {
		return fmt.Errorf("legacy trading history requires migration and reconciliation")
	}
	if strings.EqualFold(scope.Market, "spot") {
		inventoryReader, ok := ex.(exchange.SpotInventoryReader)
		if !ok {
			return fmt.Errorf("spot exchange does not expose authoritative total base-asset inventory")
		}
		balance, err := inventoryReader.SpotInventoryQty(ctx)
		if err != nil {
			return fmt.Errorf("verify initial total spot inventory: %w", err)
		}
		if math.IsNaN(balance) || math.IsInf(balance, 0) || balance < 0 || balance != 0 {
			return fmt.Errorf("spot inventory requires owned lot reconciliation")
		}
	} else if !allowRestoredFuturesPosition {
		positions, err := queryShutdownPositions(ctx, ex, scope.Symbol)
		if err != nil {
			return err
		}
		if len(positions) > 0 {
			return fmt.Errorf("existing positions require owned lot reconciliation")
		}
	}
	orders, err := ex.GetOpenOrders(ctx, scope.Symbol)
	if err != nil {
		return fmt.Errorf("verify initial open orders: %w", err)
	}
	if orders == nil {
		return fmt.Errorf("verify initial open orders: exchange returned a nil snapshot; order state is unverified")
	}
	for _, o := range orders {
		if o == nil {
			return fmt.Errorf("verify initial open orders: snapshot contains an unverifiable nil order")
		}
		if o.Symbol == "" || !strings.EqualFold(o.Symbol, scope.Symbol) {
			return fmt.Errorf("verify initial open orders: snapshot contains an out-of-scope order for %q", scope.Symbol)
		}
		return fmt.Errorf("existing open orders for %q require reconciliation", scope.Symbol)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	gate.Unblock(runtimeIntentBootstrapBlock)
	return nil
}
