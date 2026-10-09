package strategy

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
)

func TestFundingCarryConfirmationRejectsLooseAmountTolerance(t *testing.T) {
	for _, name := range []string{"tiny_wrong_total", "tiny_wrong_components", "relative_wrong_total", "borrow_wrong_principal"} {
		t.Run(name, func(t *testing.T) {
			requested, action := 1e-11, "repay"
			row := exchange.MarginBorrowRecord{TransferID: 81, Asset: "BTC", Amount: requested, Principal: requested, Status: "CONFIRMED", Timestamp: time.Now().UnixMilli()}
			switch name {
			case "tiny_wrong_total":
				row.Amount, row.Principal = 2e-11, 2e-11
			case "tiny_wrong_components":
				row.Principal = 2e-11
			case "relative_wrong_total":
				requested, row.Amount, row.Principal = 1, 1+1e-9, 1+1e-9
			case "borrow_wrong_principal":
				action, row.Principal = "borrow", 2e-11
			}
			venue := &fundingCarryStableDebtExchange{row: row}
			s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, &venue.mockFCExchange, &venue.mockFCExchange, venue, nil)
			store := &memoryRuntimeStateStore{payload: "unchanged"}
			s.SetRuntimeStateStore(store)
			if _, err := s.confirmMarginDebtTransaction(context.Background(), action, 81, "BTC", requested); err == nil {
				t.Fatal("inconsistent financial evidence accepted")
			}
			if err := s.recordMarginDebtEvent(context.Background(), action, 81, "BTC", requested); err == nil {
				t.Fatal("invalid financial evidence committed")
			}
			if len(s.marginDebtEvents) != 0 || store.payload != "unchanged" {
				t.Fatal("invalid evidence changed ledger")
			}
		})
	}
}

func TestFundingCarryEventPrecisionPreservesValidComponents(t *testing.T) {
	for _, amount := range []float64{1e-11, 0.3, 1e9} {
		e := fundingCarryMarginDebtEvent{Action: "repay", TransferID: 81, Asset: "BTC", Amount: amount, Principal: amount, OccurredAt: time.Now()}
		if err := validateFundingCarryDebtEventIntegrity(e, ""); err != nil {
			t.Fatal(err)
		}
		e.Principal, e.InterestPaid = 0, amount
		if err := validateFundingCarryDebtEventIntegrity(e, ""); err != nil {
			t.Fatal(err)
		}
	}
	e := fundingCarryMarginDebtEvent{Action: "repay", TransferID: 81, Asset: "BTC", Amount: math.Nextafter(0.3, math.Inf(1)), Principal: 0.1, InterestPaid: 0.2, OccurredAt: time.Now()}
	if err := validateFundingCarryDebtEventIntegrity(e, ""); err != nil {
		t.Fatal(err)
	}
}

func TestFundingCarryConfirmationPreservesOriginalBorrowPrincipal(t *testing.T) {
	amount := 0.3
	principal := math.Nextafter(amount, math.Inf(1))
	venue := &fundingCarryStableDebtExchange{row: exchange.MarginBorrowRecord{TransferID: 81, Asset: "BTC", Amount: amount, Principal: principal, Status: "CONFIRMED", Timestamp: time.Now().UnixMilli()}}
	s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, &venue.mockFCExchange, &venue.mockFCExchange, venue, nil)
	event, err := s.confirmMarginDebtTransaction(context.Background(), "borrow", 81, "BTC", amount)
	if err != nil {
		t.Fatal(err)
	}
	if event.Principal != principal {
		t.Fatal("raw borrow principal was overwritten")
	}
}

func TestFundingCarryRestoreRejectsTinyComponentMismatch(t *testing.T) {
	when := time.Now().Add(-time.Second)
	state := fundingCarryRuntimeState{Strategy: "funding_carry", Symbol: "BTCUSDT", OwnershipReady: true, MarginDebtEvents: []fundingCarryMarginDebtEvent{
		{Action: "borrow", TransferID: 80, Asset: "BTC", Amount: 2e-11, Principal: 2e-11, OccurredAt: when},
		{Action: "repay", TransferID: 81, Asset: "BTC", Amount: 1e-11, Principal: 2e-11, OccurredAt: when.Add(time.Millisecond)},
	}}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	venue := &mockFCExchange{baseAsset: "BTC"}
	s := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, venue, venue, venue, nil)
	store := &memoryRuntimeStateStore{version: fundingCarryRuntimeStateVersion, found: true, payload: string(payload)}
	s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	s.unownedExposure, s.intentInFlight = true, true
	if err := s.restoreRuntimeState(); err == nil {
		t.Fatal("invalid components became verified")
	}
	if s.strategySpotKnown || !s.unownedExposure || !s.intentInFlight || len(s.marginDebtEvents) != 0 || store.payload != string(payload) {
		t.Fatal("rejected evidence changed ownership")
	}
}
