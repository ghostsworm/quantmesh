package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"quantmesh/exchange"
)

func TestFundingCarryCoverConsumptionTracksConfirmedInterestNotOldTarget(t *testing.T) {
	for _, tc := range []struct {
		name        string
		amount      float64
		saveFailure bool
	}{
		{name: "accrued_interest", amount: 0.4002},
		{name: "interest_shortfall", amount: 0.402},
		{name: "save_failure_retry", amount: 0.4002, saveFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			amount := tc.amount
			s, margin, store := newFundingCarryRepayIntentFixture()
			s.strategySpotKnown, s.intentInFlight, s.unownedExposure = true, true, true
			s.marginBorrowedAt = time.UnixMilli(1000).UTC()
			s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
			fill := &exchange.OrderFill{OrderID: 7, TradeID: "cover-7", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 50000, Quantity: 0.401, CommissionAsset: "BTC", TradeTime: 1500}
			s.marginCoverOrders = []fundingCarryCoverOrder{{OrderID: 7, Asset: "BTC", AccountScope: "scope-a", Requested: 0.401, DebtToCover: 0.4, Gross: 0.401, Net: 0.401, Verified: true, Fills: []*exchange.OrderFill{fill}}}
			legacy, err := json.Marshal(s.runtimeStateSnapshotLocked())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeFundingCarryRuntimeStateForRecovery(5, string(legacy), s.fut.GetName(), s.spot.GetName(), s.symbol, true); err != nil {
				t.Fatal("legacy schema5 rejected:", err)
			}
			err = s.repayMarginPrincipalWithCover(context.Background(), "BTC", amount, 0, 7)
			if amount > 0.401 {
				if err == nil || margin.repayCalls != 0 || s.marginRepayIntent != nil {
					t.Fatal("interest shortfall reached repayment RPC")
				}
				return
			}
			if err == nil || margin.repayCalls != 1 || s.marginRepayIntent == nil || s.marginRepayIntent.TransferID != 1 {
				t.Fatalf("historical debt target blocked accepted interest repayment: %v", err)
			}
			margin.queryErr = nil
			restarted, _, _ := newFundingCarryRepayIntentFixture()
			venue := &fundingCarryRecoveryExchange{fundingCarryRepayIntentExchange: margin}
			restarted.marginEx = venue
			restarted.SetRuntimeStateStore(&borrowReceiptContextStore{store})
			before := store.payload
			if tc.saveFailure {
				venue.afterQuery = func() { store.err = errors.New("injected interest consumption save failure") }
			}
			if err := restarted.Start(context.Background()); err == nil {
				t.Fatal("whole recovery falsely completed")
			}
			if tc.saveFailure {
				if store.payload != before || restarted.marginDebt != 0.4 || restarted.marginCoverOrders[0].Consumed != 0 {
					t.Fatal("failed save partially consumed interest cover")
				}
				store.err, venue.afterQuery = nil, nil
				if err := restarted.Start(context.Background()); err == nil {
					t.Fatal("whole recovery falsely completed after retry")
				}
			}
			var state fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
				t.Fatal(err)
			}
			if state.MarginDebt != 0 || state.MarginRepayIntent != nil || state.MarginCoverOrders[0].Consumed != amount || state.MarginCoverOrders[0].DebtToCover != 0.4 || margin.repayCalls != 1 || len(state.MarginDebtEvents) != 2 {
				t.Fatal("confirmed interest consumption not recovered atomically")
			}
			if _, err := decodeFundingCarryRuntimeStateForRecovery(store.version, store.payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true); err != nil {
				t.Fatal(err)
			}
			if store.version != fundingCarryRuntimeStateVersion {
				t.Fatal("new consumption semantics did not bump schema")
			}
			state.MarginCoverOrders[0].Consumed = 0.4001
			if err := validateFundingCarryCoverOrders(state, true); err == nil {
				t.Fatal("consumption detached from actual financial event")
			}
		})
	}
}
