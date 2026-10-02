package strategy

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
)

func TestFundingCarryReverseCloseCannotDiscardPrincipal(t *testing.T) {
	for _, mode := range []string{"partial_principal", "tiny_debt", "tiny_residual", "residual_component", "principal_mismatch"} {
		t.Run(mode, func(t *testing.T) {
			debt, interest := 0.4, 0.0005
			if mode == "tiny_debt" {
				debt, interest = 1e-11, 0
			}
			spot := &mockFCExchange{baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 3, priceDecimals: 2}
			futures := &mockFCExchange{quantityDecimals: 3}
			margin := &mockFCExchange{
				baseAsset: "BTC", quantityDecimals: 3,
				positions:      []*exchange.Position{{Symbol: "BTCUSDT", Size: -(debt + interest), MarginBorrowed: debt, MarginInterest: interest, MarginDebtKnown: true}},
				getOrderStatus: exchange.OrderStatusFilled, getOrderExecQty: 0.401,
				clearDebtOnRepay: true, repayPrincipal: debt,
			}
			if mode == "partial_principal" {
				margin.repayPrincipal = debt - 1e-11
			}
			if mode == "tiny_residual" {
				margin.clearDebtOnRepay = false
			}
			if mode == "principal_mismatch" {
				margin.positions[0].MarginBorrowed += 1e-11
				margin.positions[0].Size -= 1e-11
			}
			venue := &fundingCarryResidualCloseExchange{mockFCExchange: margin, tinyResidual: mode == "tiny_residual", residualComponent: mode == "residual_component"}
			s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, venue, nil)
			store := &memoryRuntimeStateStore{}
			s.SetRuntimeStateStore(store)
			s.direction, s.marginDebt, s.marginBorrowTransferID = DirectionReverse, debt, 42
			s.marginAccountScope = "scope-a"
			s.marginBorrowedAt = time.Now().UTC()
			s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: debt, Principal: debt, OccurredAt: s.marginBorrowedAt, AccountScope: "scope-a"}}
			if err := s.closeReverse(context.Background(), mode); err == nil {
				t.Fatal("unresolved principal or liability declared closed")
			}
			var saved fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
				t.Fatal(err)
			}
			if s.direction != DirectionReverse || s.marginBorrowTransferID != 42 || !s.unownedExposure || saved.Direction != DirectionReverse || !saved.ExposureUnknown {
				t.Fatal("unresolved close discarded recovery identity")
			}
			if mode != "tiny_residual" && mode != "residual_component" && (s.marginDebt <= 0 || saved.MarginDebt <= 0) {
				t.Fatal("unpaid principal cleared")
			}
		})
	}
}

type fundingCarryResidualCloseExchange struct {
	*mockFCExchange
	tinyResidual       bool
	residualComponent  bool
	afterRepaySnapshot func()
}

func (e *fundingCarryResidualCloseExchange) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	if e.repayCalls > 0 && e.afterRepaySnapshot != nil {
		e.afterRepaySnapshot()
	}
	if e.tinyResidual && e.repayCalls > 0 {
		return []*exchange.Position{{Symbol: symbol, Size: -1e-11, MarginBorrowed: 1e-11, MarginDebtKnown: true}}, nil
	}
	if e.residualComponent && e.repayCalls > 0 {
		return []*exchange.Position{{Symbol: symbol, Size: 0, MarginBorrowed: 1e-11, MarginDebtKnown: true}}, nil
	}
	return e.mockFCExchange.GetPositions(ctx, symbol)
}

func TestFundingCarryReverseCloseFinalCommitChecksOwner(t *testing.T) {
	for _, lostOwner := range []bool{false, true} {
		s, parent, store := newFundingCarryReturnedPrincipalFixture(0)
		s.direction, s.marginDebt, s.marginBorrowTransferID = DirectionReverse, 0.4, 42
		s.marginAccountScope, s.marginBorrowedAt = "scope-a", time.Now().UTC()
		s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, OccurredAt: s.marginBorrowedAt, AccountScope: "scope-a"}}
		parent.placeOrderErr = nil
		parent.positions = []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.4, MarginBorrowed: 0.4, MarginDebtKnown: true}}
		parent.getOrderStatus, parent.getOrderExecQty, parent.clearDebtOnRepay = exchange.OrderStatusFilled, 0.401, true
		gate := &execution.OpeningGate{}
		s.SetOpeningGate(gate)
		venue := &fundingCarryResidualCloseExchange{mockFCExchange: parent.mockFCExchange}
		var beforeLoss string
		if lostOwner {
			venue.afterRepaySnapshot = func() { beforeLoss = store.payload; gate.Block(strategyWalletRuntimeOwnershipBlock) }
		}
		s.marginEx = venue
		err := s.closeReverse(context.Background(), "verified_final_commit")
		if lostOwner {
			if err == nil || store.payload != beforeLoss || s.direction != DirectionReverse || s.marginBorrowTransferID != 42 || !s.unownedExposure {
				t.Fatal("lost owner committed flat state or discarded recovery evidence")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		state, err := decodeFundingCarryRuntimeState(store.version, store.payload, s.fut.GetName(), s.spot.GetName(), s.symbol)
		if err != nil || state.MarginDebt != 0 || state.Direction != DirectionNone || len(state.MarginDebtEvents) != 2 {
			t.Fatalf("normal close did not retain a recoverable complete ledger: state=%+v err=%v", state, err)
		}
	}
}
