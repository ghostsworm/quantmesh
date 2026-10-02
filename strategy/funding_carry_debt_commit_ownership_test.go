package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
)

type fundingCarryInterruptedDebtExchange struct {
	fundingCarryStableDebtExchange
	afterQuery func()
}

func (e *fundingCarryInterruptedDebtExchange) GetMarginTransactionByID(context.Context, string, string, int64) (exchange.MarginBorrowRecord, error) {
	if e.afterQuery != nil {
		e.afterQuery()
	}
	return e.row, nil
}

func TestFundingCarryDebtCommitRejectsInterruptedConfirmation(t *testing.T) {
	for _, principal := range []bool{false, true} {
		for _, lostOwner := range []bool{false, true} {
			name := "event/canceled"
			if principal {
				name = "principal/canceled"
			}
			if lostOwner {
				name += "/owner-lost"
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				gate := &execution.OpeningGate{}
				venue := &fundingCarryInterruptedDebtExchange{fundingCarryStableDebtExchange: fundingCarryStableDebtExchange{row: exchange.MarginBorrowRecord{TransferID: 81, Asset: "BTC", Amount: 0.002, Principal: 0.002, Status: "CONFIRMED", Timestamp: time.Now().UnixMilli()}}}
				venue.afterQuery = cancel
				if lostOwner {
					venue.afterQuery = func() { gate.Block(strategyWalletRuntimeOwnershipBlock) }
				}
				s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, &venue.mockFCExchange, &venue.mockFCExchange, venue, nil)
				store := &memoryRuntimeStateStore{}
				s.SetRuntimeStateStore(store)
				s.SetOpeningGate(gate)
				s.marginDebt, s.direction, s.strategySpotKnown = 0.005, DirectionReverse, true
				s.mu.Lock()
				err := s.persistRuntimeStateLocked()
				s.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
				original := store.payload
				commit := func(ctx context.Context) error {
					if principal {
						return s.returnBorrowedPrincipal(ctx, 81, "BTC", 0.002, 0.003)
					}
					return s.recordMarginDebtEvent(ctx, "repay", 81, "BTC", 0.002)
				}
				err = commit(ctx)
				if err == nil || (!lostOwner && !errors.Is(err, context.Canceled)) {
					t.Fatalf("interrupted confirmation accepted: %v", err)
				}
				if s.marginDebt != 0.005 || len(s.marginDebtEvents) != 0 || store.payload != original {
					t.Fatal("interrupted confirmation advanced financial state")
				}
				venue.afterQuery = nil
				gate.Unblock(strategyWalletRuntimeOwnershipBlock)
				gate.Block("manual")
				if err := commit(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := commit(context.Background()); err != nil {
					t.Fatal(err)
				}
				if len(s.marginDebtEvents) != 1 || !gate.HasBlock("manual") {
					t.Fatal("protective retry lost idempotency or manual pause")
				}
				committed := store.payload
				gate.Block(strategyWalletRuntimeOwnershipBlock)
				if err := commit(context.Background()); err == nil {
					t.Fatal("lost owner reported successful ledger replay")
				}
				if store.payload != committed || len(s.marginDebtEvents) != 1 {
					t.Fatal("rejected replay changed durable evidence")
				}
			})
		}
	}
}
