package strategy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestFundingCarryRemainingReadsUseStartupCancellation(t *testing.T) {
	// Borrow/cover/fill/repayment probes precede remaining accounting.
	for _, waitAt := range []int{5, 6} {
		t.Run(fmt.Sprintf("read_%d", waitAt), func(t *testing.T) {
			donor, store := remainingCoverFixture(0.4008)
			if err := donor.persistRuntimeStateLocked(); err != nil {
				t.Fatal(err)
			}
			before := store.payload
			s, margin, _ := newFundingCarryRepayIntentFixture()
			s.direction, s.marginDebt, s.marginBorrowTransferID = DirectionNone, 0, 0
			waiting := &borrowContextReadStore{memoryRuntimeStateStore: store, waitAt: waitAt}
			s.SetRuntimeStateStore(waiting)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			err := s.Start(ctx)
			var pending *FundingCarryReconciliationRequiredError
			if !errors.Is(err, context.DeadlineExceeded) || errors.As(err, &pending) || waiting.reads != waitAt {
				t.Fatalf("remaining read bypassed cancellation or admitted recovery: reads=%d err=%v", waiting.reads, err)
			}
			if store.payload != before || s.strategySpotKnown || len(s.marginCoverOrders) != 0 || margin.repayCalls != 0 || len(margin.placedOrders) != 0 {
				t.Fatal("cancelled remaining read imported historical assets or changed financial evidence")
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
