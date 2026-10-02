package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"quantmesh/strategy"
	"quantmesh/web"
)

type capitalReleaseCollectionStrategy struct {
	capitalReleaseRuntimeStrategy
	entered chan struct{}
	unblock chan struct{}
}

func (s *capitalReleaseCollectionStrategy) SetEventBus(strategy.EventBus) {
	close(s.entered)
	<-s.unblock
}

func TestRuntimeStrategyCapitalReleaseCollectionDeadlineAndRetry(t *testing.T) {
	for _, path := range []string{"provider", "inventory"} {
		t.Run(path, func(t *testing.T) {
			rt, venue, _ := capitalReleaseRuntimeFixture(t)
			current := &capitalReleaseCollectionStrategy{entered: make(chan struct{}), unblock: make(chan struct{})}
			rt.StrategyManager.RegisterStrategy("dca", current, 1, 0)
			allocator := rt.StrategyManager.GetCapitalAllocator()
			allocator.Allocate()
			if !allocator.Reserve("dca", 200) {
				t.Fatal("fixture reserve failed")
			}
			var once sync.Once
			unblock := func() { once.Do(func() { close(current.unblock) }) }
			defer unblock()
			holder := make(chan struct{})
			go func() { rt.StrategyManager.SetEventBus(nil); close(holder) }()
			select {
			case <-current.entered:
			case <-time.After(time.Second):
				t.Fatal("fixture did not hold real manager write lock")
			}
			provider := newRuntimeStrategyCapitalProvider(nil, func() []*SymbolRuntime { return []*SymbolRuntime{rt} }).(web.VerifiedStrategyCapitalProvider)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if path == "inventory" {
					done <- verifyStandardSpotBotInventoryFlatContext(ctx, rt.SuperPositionManager, rt.StrategyManager, rt.capitalReleaseScope.Symbol)
					return
				}
				_, err := provider.ReleaseVerifiedCapital(ctx, "dca")
				done <- err
			}()
			var err error
			select {
			case err = <-done:
			case <-time.After(time.Second):
				unblock()
				err = <-done
				t.Error("production collection wait outlived proof deadline")
			}
			if !errors.Is(err, context.DeadlineExceeded) || allocator.GetUsed("dca") != 200 || venue.lastReadSymbol != "" {
				t.Errorf("cancelled collection proof: err=%v used=%v venueSymbol=%s", err, allocator.GetUsed("dca"), venue.lastReadSymbol)
			}
			unblock()
			<-holder
			amount, err := provider.ReleaseVerifiedCapital(t.Context(), "dca")
			if err != nil || amount != 200 || allocator.GetUsed("dca") != 0 {
				t.Fatalf("retry failed: amount=%v err=%v", amount, err)
			}
		})
	}
}
