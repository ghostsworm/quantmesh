package position

import (
	"context"
	"quantmesh/config"
	"quantmesh/execution"
	"testing"
)

type volatilityCancelExecutor struct {
	MockExecutor
	gate *execution.OpeningGate
	err  error
}

func (e *volatilityCancelExecutor) CancelOwnedOpeningOrders(context.Context) error {
	if e.err == nil {
		e.gate.Unblock(execution.UnverifiedCancellationBlock)
	}
	return e.err
}

func TestVolatilityRecoveryWaitsForOwnedCancellation(t *testing.T) {
	exec := &volatilityCancelExecutor{}
	spm := NewSuperPositionManager(&config.Config{}, exec, &MockExchange{}, 2, 4)
	exec.gate = spm.OpeningGate()
	spm.SetVolatilityRiskPause("high")
	spm.SetVolatilityRiskPause("")
	if !spm.IsOpeningPaused() {
		t.Fatal("recovery raced ahead of cancellation worker")
	}
	spm.CancelVolatilityOpeningOrders(context.Background())
	if spm.IsOpeningPaused() {
		t.Fatal("verified cancellation retained stale hold")
	}
	spm.SetVolatilityRiskPause("high")
	exec.err = context.DeadlineExceeded
	spm.CancelVolatilityOpeningOrders(context.Background())
	spm.SetVolatilityRiskPause("")
	spm.ResumeOpening()
	if !spm.IsOpeningPaused() {
		t.Fatal("failed cancellation was bypassed")
	}
}

func TestVolatilityPausePreservesInventoryProtection(t *testing.T) {
	spm, exec := newR5bSPM(t, "LONG", 4)
	fillSlot(spm, 100, 1, 100, "")
	spm.SetVolatilityRiskPause("high")
	orders := adjustAt(t, spm, exec, 100)
	if len(orders) == 0 {
		t.Fatal("volatility pause suppressed inventory protection")
	}
	for _, req := range orders {
		if !req.ReduceOnly || req.Side != "SELL" {
			t.Fatalf("opening escaped volatility gate: %+v", req)
		}
	}
}
