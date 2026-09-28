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

const gridZeroFillSettlementTimeout = 10 * time.Second

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
		ctx, cancel := context.WithTimeout(context.Background(), gridZeroFillSettlementTimeout)
		defer cancel()
		if err := executor.SettleIntent(ctx, copyUpdate.ClientOrderID); err != nil {
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
	HasLegacyExecutionHistory(context.Context, string, string, string) (bool, error)
}

// Only a verified fresh owner can start without a full economic checkpoint.
// Existing inventory/orders/history stay blocked for the recovery workflow;
// neither an empty journal nor a terminal order implies settled strategy state.
func configureRuntimeIntentJournal(ctx context.Context, executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate, ex exchange.IExchange, backend runtimeIntentBackend, scope execution.IntentScope) error {
	gate.Block(runtimeIntentBootstrapBlock)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := executor.ConfigureIntentJournal(ctx, backend, scope); err != nil {
		return err
	}
	legacy, err := backend.HasLegacyExecutionHistory(ctx, scope.Bot, scope.Exchange, scope.Symbol)
	if err != nil {
		return fmt.Errorf("verify legacy execution history: %w", err)
	}
	if legacy {
		return fmt.Errorf("legacy trading history requires migration and reconciliation")
	}
	if strings.EqualFold(scope.Market, "spot") {
		asset := ex.GetBaseAsset()
		if asset == "" {
			return fmt.Errorf("spot base asset unavailable")
		}
		balance, err := ex.GetBalance(ctx, asset)
		if err != nil {
			return fmt.Errorf("verify initial spot inventory: %w", err)
		}
		if math.IsNaN(balance) || math.IsInf(balance, 0) || balance != 0 {
			return fmt.Errorf("spot inventory requires owned lot reconciliation")
		}
	} else {
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
	for _, o := range orders {
		if o == nil || o.Symbol == "" || strings.EqualFold(o.Symbol, scope.Symbol) {
			return fmt.Errorf("existing or malformed open orders require reconciliation")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	gate.Unblock(runtimeIntentBootstrapBlock)
	return nil
}
