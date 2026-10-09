package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/execution"
)

type fundingCarryFlatContextStore struct {
	*memoryRuntimeStateStore
	reads     int
	wait      bool
	afterRead func()
}

func (r *fundingCarryFlatContextStore) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	r.reads++
	if r.wait {
		<-ctx.Done()
		return 0, "", false, ctx.Err()
	}
	version, payload, found, err := r.LoadRuntimeState(name)
	if r.afterRead != nil {
		r.afterRead()
	}
	return version, payload, found, err
}

func TestFundingCarryFlatProofRechecksReadContextAndOwner(t *testing.T) {
	for _, mode := range []string{"waiting_read", "cancel_after_read", "lost_owner", "legacy_store"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			margin.positions = nil
			s.direction, s.marginDebt, s.marginBorrowTransferID, s.strategySpotKnown = DirectionNone, 0, 0, true
			if err := s.persistRuntimeStateLocked(); err != nil {
				t.Fatal(err)
			}
			before := store.payload
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			wrapped := &fundingCarryFlatContextStore{memoryRuntimeStateStore: store, wait: mode == "waiting_read"}
			if mode == "cancel_after_read" {
				wrapped.afterRead = cancel
			}
			if mode == "lost_owner" {
				wrapped.afterRead = func() { gate.Block(strategyWalletRuntimeOwnershipBlock) }
			}
			if mode == "legacy_store" {
				s.SetRuntimeStateStore(store)
			} else {
				s.SetRuntimeStateStore(wrapped)
			}
			err := s.VerifyFlat(ctx)
			if err == nil {
				t.Fatalf("flat proof accepted %s after unverified durable read", mode)
			}
			if mode == "waiting_read" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if mode == "cancel_after_read" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if mode != "legacy_store" && wrapped.reads != 1 {
				t.Fatalf("proof bypassed context reader: %d", wrapped.reads)
			}
			if store.payload != before || margin.repayCalls != 0 || len(margin.placedOrders) != 0 {
				t.Fatal("flat proof mutated financial evidence")
			}
			s.mu.Lock()
			s.mu.Unlock()
			probeCtx, probeCancel := context.WithTimeout(t.Context(), time.Second)
			defer probeCancel()
			if err := s.acquireOperation(probeCtx); err != nil {
				t.Fatal("proof retained operation token:", err)
			}
			s.releaseOperation()
		})
	}
}
