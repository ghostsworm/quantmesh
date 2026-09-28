package main

import (
	"context"
	"fmt"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/order"
)

const (
	runtimeExposureMarkAge        = 2 * time.Minute
	runtimeExposureBootstrapBlock = "exposure_bootstrap_unverified"
)

// Install before bootstrap checks. Only a verified empty owner may Seed(nil).
func configureRuntimeExposure(executor *order.ExchangeOrderExecutor, quote func() (float64, time.Time)) (*execution.ExposureBook, error) {
	book, err := execution.NewExposureBook(execution.ExposureLimits{}, runtimeExposureMarkAge)
	if err != nil {
		return nil, err
	}
	executor.SetExposureBook(book)
	executor.SetExposureMarkProvider(quote)
	return book, nil
}

func bootstrapRuntimeExposure(ctx context.Context, executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate, ex exchange.IExchange, backend runtimeIntentBackend, scope execution.IntentScope, book *execution.ExposureBook) error {
	gate.Block(runtimeExposureBootstrapBlock)
	if err := configureRuntimeIntentJournal(ctx, executor, gate, ex, backend, scope); err != nil {
		return err
	}
	positions, err := ex.GetPositions(ctx, scope.Symbol)
	if err != nil {
		return fmt.Errorf("verify startup exposure positions for %s: %w", scope.Symbol, err)
	}
	for _, position := range positions {
		if position == nil {
			return fmt.Errorf("startup exposure position response contains an unverifiable nil entry for %s", scope.Symbol)
		}
		if position.Symbol == scope.Symbol && position.Size != 0 {
			return fmt.Errorf("startup exposure is not empty for %s: position requires reconciliation", scope.Symbol)
		}
	}
	openOrders, err := ex.GetOpenOrders(ctx, scope.Symbol)
	if err != nil {
		return fmt.Errorf("verify startup exposure orders for %s: %w", scope.Symbol, err)
	}
	if len(openOrders) != 0 {
		return fmt.Errorf("startup exposure is not empty for %s: %d open orders require reconciliation", scope.Symbol, len(openOrders))
	}
	if err := book.Seed(nil); err != nil {
		return err
	}
	gate.Unblock(runtimeExposureBootstrapBlock)
	return nil
}

func (a *exchangeExecutorAdapter) SetExposureLimits(limits execution.ExposureLimits) error {
	return a.executor.SetExposureLimits(limits)
}
