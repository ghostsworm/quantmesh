package main

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/storage"
)

func TestAllocateCrossMarginInterestOnlyWhenBotPrincipalMatchesExchange(t *testing.T) {
	borrowedAt := time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	repaidAt := borrowedAt.Add(2 * time.Hour)
	states := []*storage.StrategyRuntimeState{
		fundingCarryTestDebtState(t, "bot-a", "scope-a", []fundingCarryMarginDebtEventSnapshot{
			{Action: "borrow", TransferID: 11, Asset: "BTC", Amount: 0.7, Principal: 0.7, OccurredAt: borrowedAt, AccountScope: "scope-a"},
			{Action: "repay", TransferID: 12, Asset: "BTC", Amount: 0.2, Principal: 0.2, OccurredAt: repaidAt, AccountScope: "scope-a"},
		}),
		fundingCarryTestDebtState(t, "bot-b", "scope-a", []fundingCarryMarginDebtEventSnapshot{
			{Action: "borrow", TransferID: 21, Asset: "BTC", Amount: 0.3, Principal: 0.3, OccurredAt: borrowedAt.Add(time.Minute), AccountScope: "scope-a"},
		}),
	}
	payment := exchange.MarginInterestRecord{
		TransactionID: 91, AccruedAt: borrowedAt.Add(time.Hour).UnixMilli(), Asset: "BTC", RawAsset: "BTC",
		Principal: 1, Interest: 0.0001, Type: "PERIODIC",
	}
	allocations, err := allocateCrossMarginInterest(states, "scope-a", payment)
	if err != nil {
		t.Fatal("allocate reconciled interest:", err)
	}
	if math.Abs(allocations["bot-a"].Interest-0.00007) > 1e-12 || math.Abs(allocations["bot-b"].Interest-0.00003) > 1e-12 ||
		allocations["bot-a"].BotPrincipal != 0.7 || allocations["bot-b"].BotPrincipal != 0.3 {
		t.Fatalf("unexpected Bot interest split: %+v", allocations)
	}
	payment.AccruedAt = repaidAt.Add(time.Minute).UnixMilli()
	payment.Principal = 0.8
	allocations, err = allocateCrossMarginInterest(states, "scope-a", payment)
	if err != nil || math.Abs(allocations["bot-a"].Interest-0.0000625) > 1e-12 || math.Abs(allocations["bot-b"].Interest-0.0000375) > 1e-12 || math.Abs(allocations["bot-a"].BotPrincipal-0.5) > 1e-12 {
		t.Fatalf("post-repayment interest split=%+v err=%v", allocations, err)
	}
}

func TestAllocateCrossMarginInterestFailsClosedForUnmatchedOrUnverifiableDebt(t *testing.T) {
	at := time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	state := fundingCarryTestDebtState(t, "bot-a", "scope-a", []fundingCarryMarginDebtEventSnapshot{
		{Action: "borrow", TransferID: 11, Asset: "BTC", Amount: 0.7, Principal: 0.7, OccurredAt: at, AccountScope: "scope-a"},
	})
	payment := exchange.MarginInterestRecord{
		TransactionID: 91, AccruedAt: at.Add(time.Hour).UnixMilli(), Asset: "BTC", RawAsset: "BTC",
		Principal: 1, Interest: 0.0001, Type: "PERIODIC",
	}
	if _, err := allocateCrossMarginInterest([]*storage.StrategyRuntimeState{state}, "scope-a", payment); err == nil {
		t.Fatal("untracked external principal must prevent Bot allocation")
	}
	var payload fundingCarryMarginDebtStateSnapshot
	if err := json.Unmarshal([]byte(state.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	payload.MarginDebtEvents[0].AccountScope = ""
	encoded, _ := json.Marshal(payload)
	state.Payload = string(encoded)
	if _, err := allocateCrossMarginInterest([]*storage.StrategyRuntimeState{state}, "scope-a", payment); err == nil {
		t.Fatal("event without exact account scope must prevent Bot allocation")
	}
	payload.MarginDebtEvents[0].AccountScope = "scope-a"
	payload.ExposureUnknown = true
	encoded, _ = json.Marshal(payload)
	state.Payload = string(encoded)
	if _, err := allocateCrossMarginInterest([]*storage.StrategyRuntimeState{state}, "scope-a", payment); err == nil {
		t.Fatal("unresolved Bot debt ownership must prevent Bot allocation")
	}
}

func TestAllocateCrossMarginInterestRejectsConvertedAndLegacyDebtEvents(t *testing.T) {
	at := time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	state := fundingCarryTestDebtState(t, "bot-a", "scope-a", []fundingCarryMarginDebtEventSnapshot{
		{Action: "borrow", TransferID: 11, Asset: "BTC", Amount: 0.7, Principal: 0.7, OccurredAt: at, AccountScope: "scope-a"},
	})
	payment := exchange.MarginInterestRecord{
		TransactionID: 91, AccruedAt: at.Add(time.Hour).UnixMilli(), Asset: "BNB", RawAsset: "BTC",
		Principal: 0.7, Interest: 0.0001, Type: "PERIODIC_CONVERTED",
	}
	if _, err := allocateCrossMarginInterest([]*storage.StrategyRuntimeState{state}, "scope-a", payment); err == nil {
		t.Fatal("converted interest must not be allocated without historical asset valuation")
	}
	payment.Asset, payment.RawAsset, payment.Type = "BTC", "BTC", "PERIODIC"
	var payload fundingCarryMarginDebtStateSnapshot
	if err := json.Unmarshal([]byte(state.Payload), &payload); err != nil {
		t.Fatal(err)
	}
	payload.MarginDebtEvents[0].Principal = 0
	encoded, _ := json.Marshal(payload)
	state.Payload = string(encoded)
	if _, err := allocateCrossMarginInterest([]*storage.StrategyRuntimeState{state}, "scope-a", payment); err == nil {
		t.Fatal("legacy borrow event without separately verified principal must not be allocated")
	}
}

func TestAllocateCrossMarginInterestRejectsOnBorrowCharges(t *testing.T) {
	at := time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	state := fundingCarryTestDebtState(t, "bot-a", "scope-a", []fundingCarryMarginDebtEventSnapshot{
		{Action: "borrow", TransferID: 11, Asset: "BTC", Amount: 0.7, Principal: 0.7, OccurredAt: at, AccountScope: "scope-a"},
	})
	payment := exchange.MarginInterestRecord{
		TransactionID: 91, AccruedAt: at.Add(time.Minute).UnixMilli(), Asset: "BTC", RawAsset: "BTC",
		Principal: 0.7, Interest: 0.0001, Type: "ON_BORROW",
	}
	if _, err := allocateCrossMarginInterest([]*storage.StrategyRuntimeState{state}, "scope-a", payment); err == nil {
		t.Fatal("on-borrow charges must remain unallocated until principal semantics are verified")
	}
}

func TestAllocateCrossMarginInterestConservesDatabaseScaleForRepeatingRatios(t *testing.T) {
	at := time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	states := make([]*storage.StrategyRuntimeState, 0, 3)
	for index, botID := range []string{"bot-a", "bot-b", "bot-c"} {
		states = append(states, fundingCarryTestDebtState(t, botID, "scope-a", []fundingCarryMarginDebtEventSnapshot{
			{Action: "borrow", TransferID: int64(11 + index), Asset: "BTC", Amount: 1, Principal: 1, OccurredAt: at, AccountScope: "scope-a"},
		}))
	}
	payment := exchange.MarginInterestRecord{
		TransactionID: 91, AccruedAt: at.Add(time.Hour).UnixMilli(), Asset: "BTC", RawAsset: "BTC",
		Principal: 3, Interest: 0.0001, Type: "PERIODIC",
	}
	allocations, err := allocateCrossMarginInterest(states, "scope-a", payment)
	if err != nil {
		t.Fatal("allocate interest with repeating ratio:", err)
	}
	total := 0.0
	for _, botID := range []string{"bot-a", "bot-b", "bot-c"} {
		share := allocations[botID]
		if share.Interest*marginInterestStorageScale != math.Round(share.Interest*marginInterestStorageScale) {
			t.Errorf("Bot %s share exceeds DECIMAL(30,12) scale: %.16f", botID, share.Interest)
		}
		total += share.Interest
	}
	if math.Abs(total-payment.Interest) > 1e-15 {
		t.Fatalf("database-scale allocations sum=%0.16f; exchange charge=%0.16f", total, payment.Interest)
	}
}

func fundingCarryTestDebtState(t *testing.T, botID, scope string, events []fundingCarryMarginDebtEventSnapshot) *storage.StrategyRuntimeState {
	t.Helper()
	marginDebt := 0.0
	for _, event := range events {
		switch event.Action {
		case "borrow":
			marginDebt += event.Principal
		case "repay":
			marginDebt -= event.Principal
		}
	}
	direction := 0
	if marginDebt > 0 {
		direction = 2
	}
	state := fundingCarryMarginDebtStateSnapshot{
		Strategy: "funding_carry", FuturesExchange: "binance", SpotExchange: "binance", MarginAccountScope: scope,
		OwnershipReady: true, Direction: direction, MarginDebt: marginDebt, MarginDebtEvents: events,
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return &storage.StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: 1, Payload: string(payload)}
}
