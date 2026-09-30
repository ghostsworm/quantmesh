package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/order"
	"quantmesh/position"
)

const (
	runtimeExposureMarkAge        = 2 * time.Minute
	runtimeExposureBootstrapBlock = "exposure_bootstrap_unverified"
)

// Install before bootstrap checks. The book stays unavailable until either an
// empty account or an exactly matched restored owner inventory is seeded.
func configureRuntimeExposure(executor *order.ExchangeOrderExecutor, quote func() (float64, time.Time)) (*execution.ExposureBook, error) {
	book, err := execution.NewExposureBook(execution.ExposureLimits{}, runtimeExposureMarkAge)
	if err != nil {
		return nil, err
	}
	executor.SetExposureBook(book)
	executor.RequireExposureBook()
	executor.SetExposureMarkProvider(quote)
	return book, nil
}

func bootstrapRuntimeExposure(ctx context.Context, executor *order.ExchangeOrderExecutor, gate *execution.OpeningGate, ex exchange.IExchange, backend runtimeIntentBackend, scope execution.IntentScope, book *execution.ExposureBook, spm *position.SuperPositionManager) error {
	gate.Block(runtimeExposureBootstrapBlock)
	snapshotCtx, releaseSnapshot, err := executor.BeginPositionSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("freeze order submissions before startup exposure verification: %w", err)
	}
	defer releaseSnapshot()
	if err := configureRuntimeIntentJournalWithGridRecovery(snapshotCtx, executor, gate, ex, backend, scope, true); err != nil {
		return err
	}
	positions, err := ex.GetPositions(snapshotCtx, scope.Symbol)
	if err != nil {
		return fmt.Errorf("verify startup exposure positions for %s: %w", scope.Symbol, err)
	}
	if positions == nil {
		return fmt.Errorf("verify startup exposure positions for %s: response is nil, not an authoritative empty snapshot", scope.Symbol)
	}
	for _, venuePosition := range positions {
		if venuePosition == nil {
			return fmt.Errorf("startup exposure position response contains an unverifiable nil entry for %s", scope.Symbol)
		}
		if !strings.EqualFold(venuePosition.Symbol, scope.Symbol) || math.IsNaN(venuePosition.Size) || math.IsInf(venuePosition.Size, 0) {
			return fmt.Errorf("startup exposure position response contains invalid or out-of-scope row for %s", scope.Symbol)
		}
	}
	inventory, restored, err := spm.RestoredGridExposureInventory()
	if err != nil {
		return fmt.Errorf("verify restored grid inventory: %w", err)
	}
	if !restored {
		inventory = nil
	} else if len(inventory) != 0 {
		verified, err := backend.HasVerifiedExecutionOrderIDs(snapshotCtx, scope, inventory)
		if err != nil {
			return fmt.Errorf("verify restored grid entry-order ownership: %w", err)
		}
		if !verified {
			return fmt.Errorf("restored grid inventory entry orders lack exact owner intent and order-ledger evidence")
		}
	}
	if strings.EqualFold(scope.Market, "spot") {
		for _, venuePosition := range positions {
			if venuePosition.Size != 0 {
				return fmt.Errorf("spot startup position snapshot is non-empty; owner inventory recovery is unsupported")
			}
		}
		if len(inventory) != 0 {
			return fmt.Errorf("spot grid inventory cannot be restored from derivative position snapshots")
		}
	} else {
		direction := ""
		if spm != nil {
			direction = spm.GetDirection()
		}
		if err := verifyRestoredFuturesInventory(positions, inventory, restored, direction); err != nil {
			return err
		}
	}
	openOrders, err := ex.GetOpenOrders(snapshotCtx, scope.Symbol)
	if err != nil {
		return fmt.Errorf("verify startup exposure orders for %s: %w", scope.Symbol, err)
	}
	if openOrders == nil {
		return fmt.Errorf("verify startup exposure orders for %s: response is nil, not an authoritative empty snapshot", scope.Symbol)
	}
	for _, openOrder := range openOrders {
		if openOrder == nil || !strings.EqualFold(openOrder.Symbol, scope.Symbol) {
			return fmt.Errorf("startup exposure order response contains invalid or out-of-scope row for %s", scope.Symbol)
		}
	}
	if len(openOrders) != 0 {
		return fmt.Errorf("startup exposure has %d open orders requiring reconciliation", len(openOrders))
	}
	if err := snapshotCtx.Err(); err != nil {
		return fmt.Errorf("startup exposure snapshot coordination was lost before seeding: %w", err)
	}
	if err := book.Seed(inventory); err != nil {
		return err
	}
	gate.Unblock(runtimeExposureBootstrapBlock)
	return nil
}

func verifyRestoredFuturesInventory(venue []*exchange.Position, inventory []execution.ExposurePosition, restored bool, direction string) error {
	var venueLong, venueShort float64
	for _, p := range venue {
		quantity := math.Abs(p.Size)
		if quantity == 0 {
			continue
		}
		leg := ""
		switch strings.ToUpper(strings.TrimSpace(p.PositionSide)) {
		case "LONG":
			leg = "LONG"
		case "SHORT":
			leg = "SHORT"
		case "", "BOTH", "NET":
			if strings.EqualFold(strings.TrimSpace(direction), "BOTH") {
				return fmt.Errorf("net position snapshot cannot verify both restored hedge legs")
			}
			if p.Size > 0 {
				leg = "LONG"
			} else {
				leg = "SHORT"
			}
		default:
			return fmt.Errorf("position snapshot contains unsupported position side %q", p.PositionSide)
		}
		if leg == "LONG" {
			venueLong += quantity
		} else {
			venueShort += quantity
		}
		if math.IsInf(venueLong, 0) || math.IsInf(venueShort, 0) {
			return fmt.Errorf("position snapshot gross quantity overflow")
		}
	}
	var ownerLong, ownerShort float64
	for _, p := range inventory {
		if p.Group != "grid" || p.Quantity <= 0 || math.IsNaN(p.Quantity) || math.IsInf(p.Quantity, 0) {
			return fmt.Errorf("restored grid inventory contains invalid owner quantity")
		}
		switch p.Leg {
		case "LONG":
			ownerLong += p.Quantity
		case "SHORT":
			ownerShort += p.Quantity
		default:
			return fmt.Errorf("restored grid inventory contains invalid position leg")
		}
		if math.IsInf(ownerLong, 0) || math.IsInf(ownerShort, 0) {
			return fmt.Errorf("restored grid inventory gross quantity overflow")
		}
	}
	if !restored && (venueLong != 0 || venueShort != 0) {
		return fmt.Errorf("non-empty derivatives position has no restored owner ledger")
	}
	if !restoredQuantitiesMatch(venueLong, ownerLong) || !restoredQuantitiesMatch(venueShort, ownerShort) {
		return fmt.Errorf("venue gross positions do not exactly reconcile to restored grid owner inventory")
	}
	return nil
}

func restoredQuantitiesMatch(venue, owner float64) bool {
	tolerance := math.Max(1e-8, math.Max(math.Abs(venue), math.Abs(owner))*1e-8)
	return math.Abs(venue-owner) <= tolerance
}

func (a *exchangeExecutorAdapter) SetExposureLimits(limits execution.ExposureLimits) error {
	return a.executor.SetExposureLimits(limits)
}

func (a *exchangeExecutorAdapter) ReconcileExposurePositions(positions []execution.ExposurePosition) error {
	if err := a.executor.ReconcileExposureGroup("grid", positions); err != nil {
		return err
	}
	a.executor.RefreshExposureRisk()
	return nil
}
