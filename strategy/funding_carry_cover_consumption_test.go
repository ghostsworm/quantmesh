package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestFundingCarryRepaymentConsumesExactCoverEvidence(t *testing.T) {
	s, margin, store := newFundingCarryRepayIntentFixture()
	s.strategySpotKnown = true
	s.marginBorrowedAt = time.UnixMilli(1000).UTC()
	s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
	if err := s.closeReverse(context.Background(), "lookup_failure"); err == nil {
		t.Fatal("query failure ignored")
	}
	if s.marginRepayIntent.CoverOrderID != 1 || s.marginCoverOrders[0].Consumed != 0 {
		t.Fatal("cover source lost or unconfirmed repayment consumed assets")
	}
	margin.queryErr = nil
	if err := s.repayMarginPrincipal(context.Background(), "BTC", 0.4, 0); err != nil {
		t.Fatal(err)
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
		t.Fatal(err)
	}
	if state.MarginCoverOrders[0].RepayTransferID != 1 || state.MarginCoverOrders[0].Consumed != 0.4 || len(state.MarginDebtEvents) != 2 || state.MarginDebt != 0 || margin.repayCalls != 1 {
		t.Fatal("cover consumption not committed with confirmed debt")
	}
	if _, err := decodeFundingCarryRuntimeStateForRecovery(store.version, store.payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true); err != nil {
		t.Fatal(err)
	}
	state.MarginCoverOrders[0].Consumed = 0.39
	if err := validateFundingCarryCoverOrders(state, true); err == nil {
		t.Fatal("consumption not bound to financial event")
	}
}

func TestFundingCarryCoverConsumptionRollbackAndReplay(t *testing.T) {
	s, margin, store := newFundingCarryRepayIntentFixture()
	_ = s.closeReverse(context.Background(), "lookup_failure")
	margin.queryErr = nil
	store.err = errors.New("injected debt+consumption save failure")
	if err := s.repayMarginPrincipal(context.Background(), "BTC", 0.4, 0); err == nil {
		t.Fatal("save failure ignored")
	}
	if s.marginDebt != 0.4 || len(s.marginDebtEvents) != 0 || s.marginCoverOrders[0].Consumed != 0 || s.marginRepayIntent == nil {
		t.Fatal("failed save partially committed asset consumption")
	}
	store.err = nil
	if err := s.repayMarginPrincipal(context.Background(), "BTC", 0.4, 0); err != nil {
		t.Fatal(err)
	}
	if margin.repayCalls != 1 || s.marginCoverOrders[0].Consumed != 0.4 || len(s.marginDebtEvents) != 1 {
		t.Fatal("retry repeated repayment or consumption")
	}
}

func TestFundingCarryCoverSourceCannotBeReusedOrInvented(t *testing.T) {
	for _, mode := range []string{"missing", "wrong_scope", "wrong_asset", "already_consumed", "shortfall"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, _ := newFundingCarryRepayIntentFixture()
			s.intentInFlight = true
			s.marginCoverOrders = []fundingCarryCoverOrder{{OrderID: 7, Asset: "BTC", AccountScope: "scope-a", Verified: true, Net: 0.4, DebtToCover: 0.4}}
			switch mode {
			case "missing":
				s.marginCoverOrders = nil
			case "wrong_scope":
				s.marginCoverOrders[0].AccountScope = "scope-b"
			case "wrong_asset":
				s.marginCoverOrders[0].Asset = "ETH"
			case "already_consumed":
				s.marginCoverOrders[0].RepayTransferID, s.marginCoverOrders[0].Consumed = 9, 0.4
			case "shortfall":
				s.marginCoverOrders[0].Net = 0.39
			}
			if err := s.repayMarginPrincipalWithCover(context.Background(), "BTC", 0.4, 0, 7); err == nil || margin.repayCalls != 0 {
				t.Fatal("invalid cover source reached repayment RPC")
			}
		})
	}
}

func TestFundingCarryStartupConsumesBoundCoverAcknowledgement(t *testing.T) {
	s, margin, store := newFundingCarryRepayIntentFixture()
	s.strategySpotKnown = true
	s.marginBorrowedAt = time.UnixMilli(1000).UTC()
	s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
	if err := s.closeReverse(context.Background(), "lookup_failure"); err == nil {
		t.Fatal("lookup failure ignored")
	}
	margin.queryErr = nil
	restarted, _, _ := newFundingCarryRepayIntentFixture()
	restarted.marginDebt, restarted.marginBorrowTransferID = 0, 0
	restarted.marginEx = &fundingCarryRecoveryExchange{fundingCarryRepayIntentExchange: margin}
	restarted.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	if err := restarted.Start(context.Background()); err == nil {
		t.Fatal("whole operation falsely resumed")
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
		t.Fatal(err)
	}
	if margin.repayCalls != 1 || state.MarginDebt != 0 || state.MarginCoverOrders[0].Consumed != 0.4 || state.MarginCoverOrders[0].RepayTransferID != 1 || !state.IntentInFlight || !state.ExposureUnknown {
		t.Fatal("restart lost consumption attribution or repeated repayment")
	}
}
