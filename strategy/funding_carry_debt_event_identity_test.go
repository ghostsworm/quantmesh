package strategy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
)

type fundingCarryStableDebtExchange struct {
	mockFCExchange
	row exchange.MarginBorrowRecord
}

type fundingCarryReusedBorrowExchange struct {
	*fundingCarryMarginBalanceExchange
	occurredAt time.Time
}

func (e *fundingCarryReusedBorrowExchange) GetMarginTransactionByID(context.Context, string, string, int64) (exchange.MarginBorrowRecord, error) {
	return exchange.MarginBorrowRecord{TransferID: 1, Asset: "BTC", Amount: e.borrowAmount, Principal: e.borrowAmount, Status: "CONFIRMED", Timestamp: e.occurredAt.UnixMilli()}, nil
}

func TestFundingCarryReverseOpeningRejectsReusedBorrowBeforeSell(t *testing.T) {
	s, futures, spot := newFundingCarryBudgetStrategy(false, 0, 300, 300)
	s.cfg = &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {FeeRate: 0.0002}}}
	s.symbol, s.symCfg.Symbol, s.symCfg.TotalAllocatedCapital = "BTCUSDT", "BTCUSDT", 500
	futures.baseAsset, futures.latestPrice, futures.fundingRate = "BTC", 50000, -0.01
	spot.baseAsset, spot.latestPrice = "BTC", 50000
	futures.quantityDecimals, spot.quantityDecimals = 8, 8
	futures.priceDecimals, spot.priceDecimals = 2, 2
	when := time.Now().Add(-time.Second).UTC()
	margin := &fundingCarryReusedBorrowExchange{fundingCarryMarginBalanceExchange: &fundingCarryMarginBalanceExchange{mockFCExchange: &mockFCExchange{baseAsset: "BTC", latestPrice: 50000, quantityDecimals: 8, priceDecimals: 2}, balance: 300}, occurredAt: when}
	s.marginEx = margin
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	s.strategySpotKnown = true
	s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 1, Asset: "BTC", Amount: 0.005, Principal: 0.005, OccurredAt: when}}
	err := s.openReverseHedge(context.Background(), 50000, 50000, -0.01)
	if err == nil || !strings.Contains(err.Error(), "reused or conflicting transaction") {
		t.Fatalf("unexpected opening outcome: %v", err)
	}
	if margin.borrowCalls != 1 || len(margin.placedOrders) != 0 || len(futures.placedOrders) != 0 || len(s.marginDebtEvents) != 1 || !s.unownedExposure {
		t.Fatal("reused borrow identity reached trading or rewrote ledger")
	}
}

func (e *fundingCarryStableDebtExchange) GetMarginTransactionByID(context.Context, string, string, int64) (exchange.MarginBorrowRecord, error) {
	return e.row, nil
}

func TestFundingCarryDebtEventReplayIsIdempotentAcrossRestart(t *testing.T) {
	venue := &fundingCarryStableDebtExchange{row: exchange.MarginBorrowRecord{TransferID: 81, Asset: "BTC", Amount: 0.5, Principal: 0.49, Interest: 0.01, Status: "CONFIRMED", Timestamp: time.Now().Add(-time.Second).UnixMilli()}}
	s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, &venue.mockFCExchange, &venue.mockFCExchange, venue, nil)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(store)
	s.strategySpotKnown = true
	if err := s.recordMarginDebtEvent(context.Background(), "repay", 81, "BTC", 0.5); err != nil {
		t.Fatal(err)
	}
	original := store.payload
	errors := make(chan error, 20)
	for range 20 {
		go func() { errors <- s.recordMarginDebtEvent(context.Background(), "repay", 81, "BTC", 0.5) }()
	}
	for range 20 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	if len(s.marginDebtEvents) != 1 || store.payload != original {
		t.Fatal("confirmed repayment was recorded twice")
	}
	restored := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, &venue.mockFCExchange, &venue.mockFCExchange, venue, nil)
	restored.SetRuntimeStateStore(store)
	if err := restored.restoreRuntimeState(); err != nil {
		t.Fatal(err)
	}
	if err := restored.recordMarginDebtEvent(context.Background(), "repay", 81, "BTC", 0.5); err != nil {
		t.Fatal(err)
	}
	if len(restored.marginDebtEvents) != 1 || store.payload != original {
		t.Fatal("restart lost debt event identity")
	}
	venue.row.Principal, venue.row.Interest = 0.48, 0.02
	if err := restored.recordMarginDebtEvent(context.Background(), "repay", 81, "BTC", 0.5); err == nil {
		t.Fatal("same transfer with conflicting principal/interest was accepted")
	}
	if len(restored.marginDebtEvents) != 1 || store.payload != original {
		t.Fatal("conflicting evidence rewrote ledger")
	}
}

func TestFundingCarryDebtIdentitySeparatesTransactionKinds(t *testing.T) {
	event := fundingCarryMarginDebtEvent{Action: "borrow", TransferID: 81, Asset: "BTC", Amount: 0.5, Principal: 0.5, OccurredAt: time.Now().Add(-time.Second)}
	state := fundingCarryRuntimeState{Strategy: "funding_carry", Symbol: "BTCUSDT", OwnershipReady: true, MarginDebtEvents: []fundingCarryMarginDebtEvent{event, event}}
	state.MarginDebtEvents[1].Action = "repay"
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeFundingCarryRuntimeState(fundingCarryRuntimeStateVersion, string(payload), "", "", "BTCUSDT"); err != nil {
		t.Fatalf("borrow and repay namespaces were conflated: %v", err)
	}
}

func TestFundingCarryDebtRestoreRejectsRepeatedTransactionIdentity(t *testing.T) {
	e := fundingCarryMarginDebtEvent{Action: "repay", TransferID: 81, Asset: "BTC", Amount: 0.5, Principal: 0.49, InterestPaid: 0.01, OccurredAt: time.Now().Add(-time.Second)}
	for _, conflicting := range []bool{false, true} {
		state := fundingCarryRuntimeState{Strategy: "funding_carry", Symbol: "BTCUSDT", OwnershipReady: true, MarginDebtEvents: []fundingCarryMarginDebtEvent{e, e}}
		if conflicting {
			state.MarginDebtEvents[1].InterestPaid = 0.02
		}
		payload, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeFundingCarryRuntimeState(fundingCarryRuntimeStateVersion, string(payload), "", "", "BTCUSDT"); err == nil {
			t.Fatal("duplicate durable transaction identity was restored as verified")
		}
	}
}
