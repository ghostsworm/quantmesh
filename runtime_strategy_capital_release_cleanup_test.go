package main

import (
	"context"
	"errors"
	"testing"

	"quantmesh/lock"
	"quantmesh/strategy"
	"quantmesh/web"
)

var errCapitalPhysicalUnlock = errors.New("physical lease unlock acknowledgement unavailable")

type capitalPhysicalUnlockFailure struct {
	lock.DistributedLock
	err error
}

func (l *capitalPhysicalUnlockFailure) Unlock(context.Context, string) error {
	return l.err
}

func TestRuntimeStrategyCapitalReleaseReportsPhysicalUnlockFailure(t *testing.T) {
	for _, phase := range []string{"before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			venue := &capitalReleaseRuntimeVenue{runtimeJournalVenue: &runtimeJournalVenue{}}
			coordinator := &capitalPhysicalUnlockFailure{DistributedLock: lock.NewNopLock(), err: errCapitalPhysicalUnlock}
			rt, _ := capitalReleaseRuntimeFixtureWithLock(t, venue, coordinator)
			current := rt.StrategyManager.GetStrategy("dca").(*capitalReleaseRuntimeStrategy)
			wantAmount, wantUsed := 200.0, 0.0
			if phase == "before_commit" {
				current.positions = []*strategy.Position{{Symbol: "BTCUSDT", Size: 1}}
				wantAmount, wantUsed = 0, 200
			}
			provider := newRuntimeStrategyCapitalProvider(nil, func() []*SymbolRuntime { return []*SymbolRuntime{rt} }).(web.VerifiedStrategyCapitalProvider)
			amounts, err := provider.ReleaseAllVerifiedCapital(t.Context())
			if !errors.Is(err, errCapitalPhysicalUnlock) || amounts["dca"] != wantAmount || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != wantUsed {
				t.Fatalf("physical cleanup failure or committed amount concealed: %v %v", amounts, err)
			}
			coordinator.err = nil
			current.positions = nil
			amounts, err = provider.ReleaseAllVerifiedCapital(t.Context())
			if err != nil || amounts["dca"] != wantUsed || rt.StrategyManager.GetCapitalAllocator().GetUsed("dca") != 0 {
				t.Fatalf("cleanup retry erased or duplicated amount: %v %v", amounts, err)
			}
		})
	}
}
