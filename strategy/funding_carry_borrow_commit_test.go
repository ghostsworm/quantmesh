package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"quantmesh/exchange"
	"quantmesh/execution"
)

type fundingCarryInterruptedBorrowExchange struct {
	*fundingCarryReturnedPrincipalExchange
	afterBorrowQuery func()
	afterBorrow      func()
	queryErr         error
	borrowQueries    int
}

func (e *fundingCarryInterruptedBorrowExchange) Borrow(ctx context.Context, asset string, amount float64) (int64, error) {
	id, err := e.fundingCarryReturnedPrincipalExchange.Borrow(ctx, asset, amount)
	if e.afterBorrow != nil {
		e.afterBorrow()
	}
	return id, err
}

func (e *fundingCarryInterruptedBorrowExchange) GetMarginTransactionByID(ctx context.Context, asset, kind string, id int64) (exchange.MarginBorrowRecord, error) {
	row, err := e.fundingCarryReturnedPrincipalExchange.GetMarginTransactionByID(ctx, asset, kind, id)
	if kind == "BORROW" {
		e.borrowQueries++
		if e.afterBorrowQuery != nil {
			e.afterBorrowQuery()
		}
		if e.queryErr != nil {
			return exchange.MarginBorrowRecord{}, e.queryErr
		}
	}
	return row, err
}

func TestFundingCarryBorrowAcknowledgementPrecedesQueryFailure(t *testing.T) {
	s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
	venue := &fundingCarryInterruptedBorrowExchange{fundingCarryReturnedPrincipalExchange: parent, queryErr: errors.New("injected borrow query failure")}
	var ack fundingCarryRuntimeState
	venue.afterBorrowQuery = func() {
		if err := json.Unmarshal([]byte(store.payload), &ack); err != nil {
			t.Fatal(err)
		}
	}
	s.marginEx = venue
	if err := s.openReverseHedge(context.Background(), 50000, 50000, -0.01); err == nil {
		t.Fatal("query failure ignored")
	}
	if ack.MarginBorrowTransferID != 1 || !ack.IntentInFlight || ack.MarginDebt != 0 || len(ack.MarginDebtEvents) != 0 {
		t.Fatal("ACK not saved before querying or treated as verified debt")
	}
	var saved fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.MarginBorrowTransferID != 1 || !saved.ExposureUnknown || venue.borrowCalls != 1 || venue.repayCalls != 0 || s.marginDebt != 0 || len(s.marginDebtEvents) != 0 {
		t.Fatal("unverified borrow identity lost or wallet mutated")
	}
}

func TestFundingCarryBorrowAckSaveFailureKeepsIdentityWithoutFurtherRPC(t *testing.T) {
	s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
	venue := &fundingCarryInterruptedBorrowExchange{fundingCarryReturnedPrincipalExchange: parent}
	var original string
	venue.afterBorrow = func() { original = store.payload; store.err = errors.New("injected ACK save failure") }
	s.marginEx = venue
	if err := s.openReverseHedge(context.Background(), 50000, 50000, -0.01); err == nil {
		t.Fatal("ACK save failure ignored")
	}
	if s.marginBorrowTransferID != 1 || !s.unownedExposure || !s.intentInFlight || store.payload != original || venue.borrowCalls != 1 || venue.borrowQueries != 0 || venue.repayCalls != 0 {
		t.Fatal("failed ACK save lost identity or continued execution")
	}
}

func TestFundingCarryVerifiedBorrowContinuesAfterAckCheckpoint(t *testing.T) {
	s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
	parent.placeOrderErr = nil
	parent.getOrderStatus, parent.getOrderExecQty = exchange.OrderStatusFilled, 0.003
	futures := s.fut.(*fundingCarryBudgetExchange)
	futures.getOrderStatus, futures.getOrderExecQty = exchange.OrderStatusFilled, 0.003
	venue := &fundingCarryInterruptedBorrowExchange{fundingCarryReturnedPrincipalExchange: parent}
	checkpointed := false
	venue.afterBorrowQuery = func() {
		var ack fundingCarryRuntimeState
		if err := json.Unmarshal([]byte(store.payload), &ack); err != nil {
			t.Fatal(err)
		}
		checkpointed = ack.IntentInFlight && ack.MarginBorrowTransferID == 1 && len(ack.MarginDebtEvents) == 0
	}
	s.marginEx = venue
	if err := s.openReverseHedge(context.Background(), 50000, 50000, -0.01); err != nil {
		t.Fatal(err)
	}
	if !checkpointed || s.intentInFlight || s.unownedExposure || s.marginDebt != 0.003 || s.marginBorrowTransferID != 1 || len(futures.placedOrders) != 1 || venue.borrowCalls != 1 {
		t.Fatal("normal verified hedge failed after ACK checkpoint")
	}
}

func TestFundingCarryBorrowConfirmationCannotCommitAfterInterruption(t *testing.T) {
	for _, lostOwner := range []bool{false, true} {
		name := "canceled"
		if lostOwner {
			name = "owner_lost"
		}
		t.Run(name, func(t *testing.T) {
			s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(context.Background())
			venue := &fundingCarryInterruptedBorrowExchange{fundingCarryReturnedPrincipalExchange: parent}
			var beforeLoss string
			venue.afterBorrowQuery = func() {
				beforeLoss = store.payload
				if lostOwner {
					gate.Block(strategyWalletRuntimeOwnershipBlock)
				} else {
					cancel()
				}
			}
			s.marginEx = venue
			err := s.openReverseHedge(ctx, 50000, 50000, -0.01)
			cancel()
			if err == nil {
				t.Fatal("interrupted borrow confirmation succeeded")
			}
			if s.marginDebt != 0 || len(s.marginDebtEvents) != 0 || venue.repayCalls != 0 || !s.unownedExposure {
				t.Fatal("interrupted query advanced debt or changed shared wallet")
			}
			if lostOwner && store.payload != beforeLoss {
				t.Fatal("old owner overwrote durable state")
			}
			var state fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
				t.Fatal(err)
			}
			if !state.IntentInFlight || state.MarginBorrowTransferID != 1 || s.marginBorrowTransferID != 1 {
				t.Fatal("interrupted borrow lost recovery intent")
			}
		})
	}
}
