package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

type borrowContextReadStore struct {
	*memoryRuntimeStateStore
	reads, waitAt int
}

func (s *borrowContextReadStore) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	s.reads++
	if s.reads == s.waitAt {
		<-ctx.Done()
		return 0, "", false, ctx.Err()
	}
	return s.LoadRuntimeState(name)
}

func TestFundingCarryBorrowReceiptReadCancellationPreservesCheckpoint(t *testing.T) {
	for _, waitAt := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("read_%d", waitAt), func(t *testing.T) {
			s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
			if err := s.SetMarginAccountScope("fixture-scope"); err != nil {
				t.Fatal(err)
			}
			state := fundingCarryRuntimeState{Strategy: "funding_carry", FuturesExchange: s.fut.GetName(), SpotExchange: s.spot.GetName(), Symbol: s.symbol,
				OwnershipReady: true, IntentInFlight: true, ExposureUnknown: true, MarginAccountScope: "fixture-scope", MarginBorrowTransferID: 42}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(payload)); err != nil {
				t.Fatal(err)
			}
			waiting := &borrowContextReadStore{memoryRuntimeStateStore: store, waitAt: waitAt}
			s.SetRuntimeStateStore(waiting)
			s.marginEx = &borrowReceiptFaultVenue{fundingCarryReturnedPrincipalExchange: parent}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			err = s.Start(ctx)
			if !errors.Is(err, context.DeadlineExceeded) || waiting.reads != waitAt {
				t.Fatalf("borrow read did not use caller cancellation: reads=%d err=%v", waiting.reads, err)
			}
			if store.payload != string(payload) || s.marginDebt != 0 || parent.borrowCalls != 0 || parent.repayCalls != 0 || len(parent.placedOrders) != 0 {
				t.Fatal("cancelled read mutated evidence or finances")
			}
			// The cancelled final read held s.mu; verify it was released.
			s.mu.Lock()
			s.mu.Unlock()
		})
	}
}
