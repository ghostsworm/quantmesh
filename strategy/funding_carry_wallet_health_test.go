package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"quantmesh/execution"
)

type fundingCarryExpiryStateStore struct {
	*memoryRuntimeStateStore
	healthy        *atomic.Bool
	writes         int
	failCompletion bool
	initialPayload string
}

func (s *fundingCarryExpiryStateStore) SaveRuntimeState(name string, version int, payload string) error {
	s.writes++
	if s.failCompletion && s.writes == 2 {
		return errors.New("injected unsubmitted intent completion failure")
	}
	err := s.memoryRuntimeStateStore.SaveRuntimeState(name, version, payload)
	if s.writes == 1 {
		s.initialPayload = s.payload
		s.healthy.Store(false)
	}
	return err
}

func TestFundingCarryWalletOpeningHealthAfterIntentPersistence(t *testing.T) {
	for _, mode := range []string{"borrow", "collateral_transfer", "borrow_completion_failure", "collateral_transfer_completion_failure"} {
		t.Run(mode, func(t *testing.T) {
			var healthy atomic.Bool
			healthy.Store(true)
			var gate execution.OpeningGate
			gate.SetAdmissionCheck(healthy.Load)
			var s *FundingCarryStrategy
			var operation func() error
			var mutations func() int
			store := &fundingCarryExpiryStateStore{memoryRuntimeStateStore: &memoryRuntimeStateStore{}, healthy: &healthy}
			store.failCompletion = strings.HasSuffix(mode, "completion_failure")
			if strings.HasPrefix(mode, "borrow") {
				strategy, margin, _ := newFundingCarryReturnedPrincipalFixture(0)
				s = strategy
				operation = func() error { return s.openReverseHedge(context.Background(), 50000, 50000, -0.01) }
				mutations = func() int { return margin.borrowCalls + margin.repayCalls + len(margin.placedOrders) }
			} else {
				strategy, _, spot := newFundingCarryBudgetStrategy(true, 0, 0, 500)
				s = strategy
				s.strategySpotKnown = true
				operation = func() error { return s.ensureFuturesMargin(context.Background(), 100, 0) }
				mutations = func() int { return spot.transferCalls }
			}
			s.SetOpeningGate(&gate)
			s.SetRuntimeStateStore(store)
			err := operation()
			if healthy.Load() {
				t.Fatal("intent persistence did not invalidate health")
			}
			if !errors.Is(err, execution.ErrOpeningPaused) || mutations() != 0 {
				t.Fatalf("expired wallet admission mutated finances: mutations=%d error=%v", mutations(), err)
			}
			var saved fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &saved); err != nil {
				t.Fatal(err)
			}
			if store.failCompletion {
				if !s.intentInFlight || !s.unownedExposure || !saved.IntentInFlight || store.payload != store.initialPayload {
					t.Fatal("failed completion erased durable pending intent")
				}
				return
			}
			if s.intentInFlight || s.unownedExposure || saved.IntentInFlight || saved.ExposureUnknown || saved.MarginBorrowTransferID != 0 || saved.MarginDebt != 0 {
				t.Fatal("provably unsubmitted wallet operation became unresolved")
			}
		})
	}
}
