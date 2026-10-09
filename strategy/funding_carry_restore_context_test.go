package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fundingCarryRestoreContextStore struct {
	*memoryRuntimeStateStore
	reads  int
	cancel context.CancelFunc
}

func (s *fundingCarryRestoreContextStore) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	s.reads++
	if s.reads == 6 {
		if s.cancel == nil {
			<-ctx.Done()
			return 0, "", false, ctx.Err()
		}
		// A reader can return data concurrently with caller cancellation.
		s.cancel()
	}
	return s.LoadRuntimeState(name)
}

func TestFundingCarryOrdinaryRestoreUsesStartupCancellation(t *testing.T) {
	for _, mode := range []string{"waiting_read", "cancel_before_import"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			s.direction, s.marginDebt, s.marginBorrowTransferID = DirectionNone, 0, 0
			state := fundingCarryRuntimeState{Strategy: "funding_carry", FuturesExchange: s.fut.GetName(), SpotExchange: s.spot.GetName(), Symbol: s.symbol, MarginAccountScope: "scope-a", OwnershipReady: true}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(payload)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			wrapped := &fundingCarryRestoreContextStore{memoryRuntimeStateStore: store}
			if mode == "cancel_before_import" {
				wrapped.cancel = cancel
			}
			s.SetRuntimeStateStore(wrapped)
			err = s.Start(ctx)
			want := context.DeadlineExceeded
			if mode == "cancel_before_import" {
				want = context.Canceled
			}
			if !errors.Is(err, want) || wrapped.reads != 6 {
				t.Fatalf("ordinary restore bypassed cancellation: reads=%d err=%v", wrapped.reads, err)
			}
			if s.strategySpotKnown || s.started || store.payload != string(payload) || margin.repayCalls != 0 || len(margin.placedOrders) != 0 {
				t.Fatal("cancelled ordinary restore adopted ownership or changed financial evidence")
			}
			s.mu.Lock()
			s.mu.Unlock()
		})
	}
}
