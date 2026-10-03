package strategy

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"quantmesh/exchange"
)

func TestFundingCarryCloseRetainsRemainingMarginAssets(t *testing.T) {
	s, margin, store := newFundingCarryRepayIntentFixture()
	margin.queryErr, margin.getOrderExecQty = nil, 0.4008
	s.spot.(*mockFCExchange).quantityDecimals = 4
	s.strategySpotKnown = true
	s.marginBorrowedAt = time.UnixMilli(1000).UTC()
	s.marginDebtEvents = []fundingCarryMarginDebtEvent{{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: s.marginBorrowedAt}}
	err := s.closeReverse(context.Background(), "remaining_margin_assets")
	if err == nil || !strings.Contains(err.Error(), "remaining") {
		t.Fatalf("remaining margin assets falsely declared flat: %v", err)
	}
	if margin.repayCalls != 1 || s.marginDebt != 0 || s.direction == DirectionNone || !s.intentInFlight || !s.unownedExposure {
		t.Fatal("confirmed repayment lost or remaining operation falsely completed")
	}
	var saved fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.MarginCoverOrders[0].Net != 0.4008 || saved.MarginCoverOrders[0].Consumed != 0.4 || !saved.IntentInFlight || !saved.ExposureUnknown {
		t.Fatal("durable remaining asset attribution lost")
	}
	for _, status := range []map[string]interface{}{s.GetFundingStatus(), s.GetVisualizationData()} {
		if status["margin_cover_remaining_qty"] != "0.0008" || status["margin_cover_remaining_known"] != true || status["margin_cover_remaining_basis"] != "historical_net_less_confirmed_repayment" {
			t.Fatal("remaining assets not visible with historical accounting basis")
		}
	}
	if err := s.beginRuntimeIntent(context.Background()); err == nil || margin.repayCalls != 1 {
		t.Fatal("unresolved close was overwritten or repayment repeated")
	}
}

func remainingCoverFixture(net float64) (*FundingCarryStrategy, *memoryRuntimeStateStore) {
	s, margin, store := newFundingCarryRepayIntentFixture()
	margin.positions = nil
	s.strategySpotKnown, s.direction, s.marginDebt, s.marginBorrowTransferID = true, DirectionNone, 0, 0
	s.marginDebtEvents = []fundingCarryMarginDebtEvent{
		{Action: "borrow", TransferID: 42, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: time.UnixMilli(1000).UTC()},
		{Action: "repay", TransferID: 1, Asset: "BTC", Amount: 0.4, Principal: 0.4, AccountScope: "scope-a", OccurredAt: time.UnixMilli(2000).UTC()},
	}
	s.marginCoverOrders = []fundingCarryCoverOrder{{OrderID: 7, Asset: "BTC", AccountScope: "scope-a", Requested: net, DebtToCover: 0.4, Gross: net, Net: net, Verified: true, RepayTransferID: 1, Consumed: 0.4,
		Fills: []*exchange.OrderFill{{OrderID: 7, TradeID: "cover-7", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 50000, Quantity: net, CommissionAsset: "BTC", TradeTime: 1500}}}}
	return s, store
}

func TestFundingCarryRemainingAssetsCannotPassFlatOrRestart(t *testing.T) {
	for _, net := range []float64{0.4008, math.Nextafter(0.4, 1)} {
		for _, mode := range []string{"memory", "durable", "both"} {
			t.Run(mode+"_"+fundingCarryCoverRemainingString(fundingCarryDecimalPrincipal(net)), func(t *testing.T) {
				s, store := remainingCoverFixture(net)
				state := s.runtimeStateSnapshotLocked()
				if mode == "memory" {
					state.MarginCoverOrders = nil
				}
				payload, err := json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(payload)); err != nil {
					t.Fatal(err)
				}
				if mode == "durable" {
					s.marginCoverOrders = nil
				}
				before := store.payload
				if err := s.VerifyFlat(context.Background()); err == nil || !strings.Contains(err.Error(), "remaining") {
					t.Fatalf("remaining assets accepted as flat: %v", err)
				}
				if mode != "memory" {
					restarted, margin, _ := newFundingCarryRepayIntentFixture()
					restarted.SetRuntimeStateStore(store)
					if err := restarted.Start(context.Background()); err == nil || margin.repayCalls != 0 || store.payload != before {
						t.Fatal("restart ignored remaining assets, spent funds or rewrote evidence")
					}
					if restarted.GetFundingStatus()["margin_cover_remaining_known"] != true || !restarted.intentInFlight || !restarted.unownedExposure {
						t.Fatal("remaining asset accounting not retained after guarded recovery")
					}
				}
			})
		}
	}
}

func TestFundingCarryRemainingAssetsExactZeroAndInvalidEvidence(t *testing.T) {
	for _, mode := range []string{"exact_zero", "unverified", "wrong_scope", "wrong_asset", "duplicate", "overdraw", "missing_event", "duplicate_event", "invalid_event", "nan", "new_owner"} {
		t.Run(mode, func(t *testing.T) {
			s, store := remainingCoverFixture(0.4)
			r := &s.marginCoverOrders[0]
			switch mode {
			case "unverified":
				r.Verified = false
			case "wrong_scope":
				r.AccountScope = "scope-other"
			case "wrong_asset":
				r.Asset = "ETH"
			case "duplicate":
				s.marginCoverOrders = append(s.marginCoverOrders, *r)
			case "overdraw":
				r.Consumed = math.Nextafter(r.Net, 1)
			case "missing_event":
				s.marginDebtEvents = nil
			case "duplicate_event":
				s.marginDebtEvents = append(s.marginDebtEvents, s.marginDebtEvents[1])
			case "invalid_event":
				s.marginDebtEvents[1].OccurredAt = time.Time{}
			case "nan":
				r.Net = math.NaN()
			case "new_owner":
				s.strategySpotKnown = false
			}
			status := s.GetFundingStatus()
			if mode != "exact_zero" {
				if status["margin_cover_remaining_qty"] != nil || status["margin_cover_remaining_known"] != false {
					t.Fatal("invalid evidence invented known zero")
				}
				return
			}
			if status["margin_cover_remaining_qty"] != "0" || status["margin_cover_remaining_known"] != true {
				t.Fatal("fully consumed journal not zero")
			}
			if err := s.persistRuntimeStateLocked(); err != nil {
				t.Fatal(err)
			}
			if err := s.VerifyFlat(context.Background()); err != nil {
				t.Fatal("fully consumed journal blocked flat:", err)
			}
			restarted, _, _ := newFundingCarryRepayIntentFixture()
			restarted.SetRuntimeStateStore(store)
			if err := restarted.restoreRuntimeState(); err != nil {
				t.Fatal("fully consumed journal blocked restore:", err)
			}
		})
	}
}

func TestFundingCarryRemainingAssetsSumDistinctCycles(t *testing.T) {
	s, _ := remainingCoverFixture(0.4008)
	second := cloneFundingCarryCoverOrders(s.marginCoverOrders)[0]
	second.OrderID, second.RepayTransferID = 8, 2
	second.Fills[0].OrderID, second.Fills[0].TradeID = 8, "cover-8"
	s.marginCoverOrders = append(s.marginCoverOrders, second)
	repay := s.marginDebtEvents[1]
	repay.TransferID = 2
	borrow := s.marginDebtEvents[0]
	borrow.TransferID = 43
	borrow.OccurredAt, repay.OccurredAt = time.UnixMilli(3000).UTC(), time.UnixMilli(4000).UTC()
	s.marginDebtEvents = append(s.marginDebtEvents, borrow, repay)
	if got := s.GetFundingStatus()["margin_cover_remaining_qty"]; got != "0.0016" {
		t.Fatalf("distinct source remainders not summed exactly: %v", got)
	}
}
