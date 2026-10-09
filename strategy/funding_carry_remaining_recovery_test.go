package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"quantmesh/execution"
)

func TestFundingCarryStartupRetainsRemainingAssetAccounting(t *testing.T) {
	donor, store := remainingCoverFixture(0.4008)
	if err := donor.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	s, margin, _ := newFundingCarryRepayIntentFixture()
	s.direction, s.marginDebt, s.marginBorrowTransferID = DirectionNone, 0, 0
	margin.positionsErr = errors.New("live inventory deliberately unavailable")
	s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	before := store.payload
	for attempt := 0; attempt < 2; attempt++ {
		err := s.Start(context.Background())
		if err == nil || s.started {
			t.Fatal("remaining asset recovery started trading")
		}
		if !strings.Contains(err.Error(), "0.0008 BTC") || !strings.Contains(err.Error(), "current inventory and disposal remain unverified") {
			t.Fatalf("startup diagnostic omitted historical amount or current-inventory boundary: %v", err)
		}
		status := s.GetFundingStatus()
		if status["margin_cover_remaining_qty"] != "0.0008" || status["margin_cover_remaining_known"] != true {
			t.Fatalf("startup lost durable remaining asset accounting: %+v", status)
		}
		if !s.unownedExposure || !s.intentInFlight || len(s.marginDebtEvents) != 2 || len(s.marginCoverOrders) != 1 || s.marginDebt != 0 {
			t.Fatal("remaining accounting import lost financial evidence or reconciliation flags")
		}
		if store.payload != before || margin.repayCalls != 0 || len(margin.placedOrders) != 0 {
			t.Fatal("historical recovery rewrote state or performed a financial RPC")
		}
	}
}

type remainingRecoveryStore struct {
	*memoryRuntimeStateStore
	coordinator *walletCoordinationTestLock
	underLease  func()
}

func (r *remainingRecoveryStore) LoadRuntimeState(kind string) (int, string, bool, error) {
	r.coordinator.mu.Lock()
	locked := r.coordinator.active != 0
	r.coordinator.mu.Unlock()
	if locked && r.underLease != nil {
		r.underLease()
	}
	return r.memoryRuntimeStateStore.LoadRuntimeState(kind)
}

func (r *remainingRecoveryStore) LoadRuntimeStateContext(ctx context.Context, kind string) (int, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return 0, "", false, err
	}
	version, payload, found, err := r.LoadRuntimeState(kind)
	if contextErr := ctx.Err(); contextErr != nil {
		return 0, "", false, contextErr
	}
	return version, payload, found, err
}

func TestFundingCarryRemainingRecoveryRechecksSnapshotAndOwner(t *testing.T) {
	for _, mode := range []string{"changed_snapshot", "disappeared", "load_error", "invalid_ledger", "wrong_asset", "wrong_scope", "empty_scope", "owner_lost", "cancelled", "local_repay", "local_cover", "local_save_failure"} {
		t.Run(mode, func(t *testing.T) {
			donor, store := remainingCoverFixture(0.4008)
			if mode == "invalid_ledger" {
				donor.marginDebtEvents = donor.marginDebtEvents[1:]
			}
			if mode == "wrong_asset" {
				donor.marginCoverOrders[0].Asset = "ETH"
			}
			if err := donor.persistRuntimeStateLocked(); err != nil {
				t.Fatal(err)
			}
			s, margin, _ := newFundingCarryRepayIntentFixture()
			s.direction, s.marginDebt, s.marginBorrowTransferID = DirectionNone, 0, 0
			coordinator := &walletCoordinationTestLock{}
			if err := s.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
				t.Fatal(err)
			}
			wrapped := &remainingRecoveryStore{memoryRuntimeStateStore: store, coordinator: coordinator}
			s.SetRuntimeStateStore(wrapped)
			gate := &execution.OpeningGate{}
			s.SetOpeningGate(gate)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "wrong_scope" {
				s.marginAccountScope = "scope-b"
			}
			if mode == "empty_scope" {
				s.marginAccountScope = ""
			}
			if mode == "local_repay" {
				s.marginRepayIntent = &fundingCarryRepayIntent{TransferID: 99}
			}
			if mode == "local_cover" {
				s.marginCoverIntent = &fundingCarryCoverIntent{ClientOrderID: "local-unpersisted-cid"}
			}
			if mode == "local_save_failure" {
				s.runtimeStateErr = errors.New("injected unresolved save failure")
			}
			before := store.payload
			wrapped.underLease = func() {
				switch mode {
				case "changed_snapshot":
					r := &donor.marginCoverOrders[0]
					r.Net, r.Gross, r.Requested, r.Fills[0].Quantity = 0.4011, 0.4011, 0.4011, 0.4011
					if err := donor.persistRuntimeStateLocked(); err != nil {
						t.Fatal(err)
					}
				case "disappeared":
					store.found = false
				case "load_error":
					store.err = errors.New("injected locked load failure")
				case "owner_lost":
					gate.Block(strategyWalletRuntimeOwnershipBlock)
				case "cancelled":
					cancel()
				}
			}
			if err := s.Start(ctx); err == nil || s.started || margin.repayCalls != 0 || len(margin.placedOrders) != 0 {
				t.Fatal("remaining recovery resumed trading or spent funds")
			}
			if mode == "changed_snapshot" {
				if got := s.GetFundingStatus()["margin_cover_remaining_qty"]; got != "0.0011" {
					t.Fatalf("pre-lock stale snapshot adopted: %v", got)
				}
			} else if s.strategySpotKnown || len(s.marginCoverOrders) != 0 || store.payload != before {
				t.Fatal("invalid, cancelled or stale-owner accounting mutated memory/durable evidence")
			}
			if mode == "local_repay" && s.marginRepayIntent.TransferID != 99 || mode == "local_cover" && s.marginCoverIntent.ClientOrderID != "local-unpersisted-cid" {
				t.Fatal("unpersisted local financial identity lost")
			}
			coordinator.mu.Lock()
			active := coordinator.active
			coordinator.mu.Unlock()
			if active != 0 {
				t.Fatal("recovery leaked wallet lease")
			}
			if err := s.acquireOperation(context.Background()); err != nil {
				t.Fatal("recovery leaked operation token:", err)
			}
			s.releaseOperation()
		})
	}
}

func TestFundingCarryRemainingRecoveryPreservesOtherLegAccounting(t *testing.T) {
	donor, store := remainingCoverFixture(0.4008)
	donor.direction, donor.futQty = DirectionReverse, 0.2
	donor.marginBorrowTransferID, donor.marginBorrowedAt = 42, donor.marginDebtEvents[0].OccurredAt
	donor.unownedExposure, donor.intentInFlight = true, true
	if err := donor.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	s, _, _ := newFundingCarryRepayIntentFixture()
	s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	if err := s.Start(context.Background()); err == nil || s.futQty != 0.2 || s.direction != DirectionReverse {
		t.Fatal("remaining recovery discarded other leg exposure")
	}
	// Exercise the import helper with an input that remains owned by the caller.
	var saved fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	err := s.restoreRemainingMarginAccountingLocked(context.Background(), saved)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	saved.MarginCoverOrders[0].Fills[0].Quantity = 9
	if s.marginCoverOrders[0].Fills[0].Quantity != 0.4008 {
		t.Fatal("remaining recovery aliased fill evidence")
	}
}
