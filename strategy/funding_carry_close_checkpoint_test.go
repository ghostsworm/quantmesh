package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
)

type fundingCarryCloseCheckpointExchange struct {
	*mockFCExchange
	reads              int
	afterCloseSnapshot func()
}

func (e *fundingCarryCloseCheckpointExchange) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	rows, err := e.mockFCExchange.GetPositions(ctx, symbol)
	e.reads++
	if e.reads == 2 && e.afterCloseSnapshot != nil {
		e.afterCloseSnapshot()
	}
	return rows, err
}

func TestFundingCarryReverseCloseCheckpointsFuturesBeforeDebtCover(t *testing.T) {
	for _, mode := range []string{"buy_failure", "save_failure", "owner_lost", "tiny_remaining"} {
		t.Run(mode, func(t *testing.T) {
			spot := &mockFCExchange{baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 3, priceDecimals: 2}
			futures := &fundingCarryCloseCheckpointExchange{mockFCExchange: &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.4}}}}
			margin := &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.4, MarginBorrowed: 0.4, MarginDebtKnown: true}}, placeOrderErr: errors.New("injected buy failure")}
			s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, margin, nil)
			store := &memoryRuntimeStateStore{}
			s.SetRuntimeStateStore(store)
			s.direction, s.futQty, s.marginDebt = DirectionReverse, 0.4, 0.4
			s.strategySpotKnown = true
			s.marginAccountScope, s.marginBorrowTransferID, s.marginBorrowedAt = "scope-a", 42, time.Now().UTC()
			s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, OccurredAt: s.marginBorrowedAt, AccountScope: "scope-a"}}
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			var before string
			futures.afterCloseSnapshot = func() {
				before = store.payload
				if mode == "save_failure" {
					store.err = errors.New("injected close checkpoint save failure")
				}
				if mode == "owner_lost" {
					gate.Block(strategyWalletRuntimeOwnershipBlock)
				}
				if mode == "tiny_remaining" {
					futures.positions[0].Size = 1e-11
				}
			}
			if err := s.closeReverse(context.Background(), mode); err == nil {
				t.Fatal("injected close failure ignored")
			}
			if !s.unownedExposure || s.marginDebt != 0.4 || s.marginBorrowTransferID != 42 {
				t.Fatal("incomplete close lost debt identity")
			}
			if mode != "buy_failure" {
				if (mode != "tiny_remaining" && store.payload != before) || s.futQty != 0.4 || len(margin.placedOrders) != 0 || margin.repayCalls != 0 {
					t.Fatal("failed or stale checkpoint continued wallet mutation")
				}
				return
			}
			var saved fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
				t.Fatal(err)
			}
			if s.futQty != 0 || saved.OwnedFutures != 0 || len(futures.placedOrders) != 1 {
				t.Fatal("verified futures close lost before debt cover failure")
			}
		})
	}
}

func TestFundingCarryReverseCloseCheckpointNormalHedgeStillCloses(t *testing.T) {
	spot := &mockFCExchange{baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 3, priceDecimals: 2}
	futures := &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.4}}}
	margin := &mockFCExchange{quantityDecimals: 3, positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: -0.4, MarginBorrowed: 0.4, MarginDebtKnown: true}}, clearDebtOnRepay: true, getOrderStatus: exchange.OrderStatusFilled, getOrderExecQty: 0.4, fillEvidence: true}
	s := NewFundingCarryStrategy("fc", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, margin, nil)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	s.direction, s.futQty, s.marginDebt = DirectionReverse, 0.4, 0.4
	s.strategySpotKnown = true
	s.marginAccountScope, s.marginBorrowTransferID, s.marginBorrowedAt = "scope-a", 42, time.Now().UTC()
	s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, OccurredAt: s.marginBorrowedAt, AccountScope: "scope-a"}}
	if err := s.closeReverse(context.Background(), "normal_checkpoint"); err != nil {
		t.Fatal(err)
	}
	state, err := decodeFundingCarryRuntimeState(store.version, store.payload, s.fut.GetName(), s.spot.GetName(), s.symbol)
	if err != nil || state.OwnedFutures != 0 || state.MarginDebt != 0 || state.Direction != DirectionNone || len(futures.placedOrders) != 1 || margin.repayCalls != 1 || state.IntentInFlight {
		t.Fatalf("normal checkpoint did not complete verified close: state=%+v err=%v", state, err)
	}
}
