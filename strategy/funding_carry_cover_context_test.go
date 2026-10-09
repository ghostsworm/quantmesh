package strategy

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"quantmesh/exchange"
)

func TestFundingCarryCoverReadsUseStartupCancellation(t *testing.T) {
	for _, phase := range []string{"cid", "fills"} {
		for _, stage := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s_read_%d", phase, stage), func(t *testing.T) {
				s, margin, store := newFundingCarryRepayIntentFixture()
				s.strategySpotKnown, s.intentInFlight, s.unownedExposure = true, true, true
				s.marginBorrowedAt = time.UnixMilli(1000).UTC()
				s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
				req := &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Quantity: 0.401, Price: 50000}
				if err := s.prepareMarginCoverIntent(t.Context(), req, 0.4); err != nil {
					t.Fatal(err)
				}
				order := &exchange.Order{OrderID: 7, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side, Type: req.Type, Quantity: req.Quantity, Price: req.Price, CreatedAt: time.Now().UTC(), Status: exchange.OrderStatusFilled, ExecutedQty: req.Quantity}
				waitAt := stage + 1 // preceding contextual borrow probe
				if phase == "fills" {
					if err := s.checkpointMarginCoverOrder(t.Context(), order, req.Quantity, 0.4); err != nil {
						t.Fatal(err)
					}
					waitAt++ // preceding cover-intent probe
				}
				before := store.payload
				s.marginDebt, s.marginBorrowTransferID = 0, 0
				s.marginCoverIntent, s.marginCoverOrders, s.marginDebtEvents = nil, nil, nil
				s.intentInFlight, s.unownedExposure, s.strategySpotKnown = false, false, false
				waiting := &borrowContextReadStore{memoryRuntimeStateStore: store, waitAt: waitAt}
				s.SetRuntimeStateStore(waiting)
				venue := &fundingCarryCoverFillVenue{fundingCarryCoverQueryVenue: &fundingCarryCoverQueryVenue{fundingCarryRepayIntentExchange: margin, order: order}}
				s.marginEx = venue
				ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
				defer cancel()
				err := s.Start(ctx)
				if !errors.Is(err, context.DeadlineExceeded) || waiting.reads != waitAt {
					t.Fatalf("cover read bypassed cancellation: reads=%d want=%d err=%v", waiting.reads, waitAt, err)
				}
				if store.payload != before || s.marginDebt != 0 || venue.queries != 0 || venue.fillQueries != 0 || margin.repayCalls != 0 || len(margin.placedOrders) != 0 {
					t.Fatal("cancelled checkpoint read queried venue, imported debt or changed financial evidence")
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
}
