package strategy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestFundingCarryRepaymentReadsUseStartupCancellation(t *testing.T) {
	// Borrow, cover-intent and cover-fill probes precede repayment reads.
	for _, waitAt := range []int{4, 5} {
		t.Run(fmt.Sprintf("read_%d", waitAt), func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			s.strategySpotKnown, s.intentInFlight, s.unownedExposure = true, true, true
			s.marginBorrowedAt = time.UnixMilli(1000).UTC()
			s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
			s.marginRepayIntent = &fundingCarryRepayIntent{Asset: "BTC", AccountScope: "scope-a", Amount: 0.4, BorrowTransferID: 42, TransferID: 7}
			if err := s.persistRuntimeStateLocked(); err != nil {
				t.Fatal(err)
			}
			before := store.payload
			s.marginDebt, s.marginBorrowTransferID = 0, 0
			s.marginDebtEvents, s.marginRepayIntent = nil, nil
			s.intentInFlight, s.unownedExposure, s.strategySpotKnown = false, false, false
			waiting := &borrowContextReadStore{memoryRuntimeStateStore: store, waitAt: waitAt}
			s.SetRuntimeStateStore(waiting)
			venue := &fundingCarryRecoveryExchange{fundingCarryRepayIntentExchange: margin}
			s.marginEx = venue
			margin.queryErr, margin.repayAmount = nil, 0.4
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			err := s.Start(ctx)
			if !errors.Is(err, context.DeadlineExceeded) || waiting.reads != waitAt {
				t.Fatalf("repayment read bypassed cancellation: reads=%d err=%v", waiting.reads, err)
			}
			if store.payload != before || s.marginDebt != 0 || venue.queries != 0 || margin.repayCalls != 0 {
				t.Fatal("cancelled read imported debt, queried receipt or replayed repayment")
			}
			s.mu.Lock()
			s.mu.Unlock()
			if err := s.acquireOperation(t.Context()); err != nil {
				t.Fatal(err)
			}
			s.releaseOperation()
		})
	}
}
