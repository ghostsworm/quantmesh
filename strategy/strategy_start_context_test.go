package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
)

type startupContextStrategy struct {
	routingTestStrategy
	start         func(context.Context) error
	starts, stops int
	received      context.Context
}

func (s *startupContextStrategy) Start(ctx context.Context) error {
	s.starts++
	s.received = ctx
	if s.start != nil {
		return s.start(ctx)
	}
	return nil
}
func (s *startupContextStrategy) Stop() error { s.stops++; return nil }

func startupContextManager(names ...string) *StrategyManager {
	cfg := &config.Config{}
	cfg.Strategies.Configs = make(map[string]config.StrategyConfig)
	for _, name := range names {
		cfg.Strategies.Configs[name] = config.StrategyConfig{Enabled: true, Type: name, Weight: 1}
	}
	return NewStrategyManager(cfg, 100)
}

func TestStrategyManagerStartupContextPreventsDetachedCancellation(t *testing.T) {
	for _, mode := range []string{"nil", "caller_cancelled", "manager_stopped", "cancel_during_start", "cancel_later_start"} {
		t.Run(mode, func(t *testing.T) {
			manager := startupContextManager("a", "b", "c")
			first, second := &startupContextStrategy{}, &startupContextStrategy{}
			last := &startupContextStrategy{}
			manager.RegisterStrategy("a", first, 1, 0)
			manager.RegisterStrategy("b", second, 1, 0)
			manager.RegisterStrategy("c", last, 1, 0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "nil" {
				ctx = nil
			}
			if mode == "caller_cancelled" {
				cancel()
			}
			if mode == "manager_stopped" {
				manager.cancel()
			}
			if mode == "cancel_during_start" {
				first.start = func(context.Context) error { cancel(); return nil }
			}
			if mode == "cancel_later_start" {
				second.start = func(context.Context) error { cancel(); return nil }
			}
			err := manager.StartAllContext(ctx)
			if err == nil || last.starts != 0 || last.stops != 0 || (mode != "cancel_later_start" && second.starts != 0) {
				t.Fatal("cancelled lifecycle advanced startup")
			}
			if mode != "nil" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation cause: %v", err)
			}
			if mode == "cancel_during_start" {
				if first.starts != 1 || first.stops != 1 || first.received.Err() != context.Canceled {
					t.Fatal("started strategy not cancelled and rolled back")
				}
			} else if mode == "cancel_later_start" {
				if first.starts != 1 || first.stops != 1 || second.starts != 1 || second.stops != 1 {
					t.Fatal("later cancellation did not roll back every started strategy")
				}
			} else if first.starts != 0 || first.stops != 0 {
				t.Fatal("unstarted strategy was invoked")
			}
		})
	}
}

func TestStrategyManagerStartupContextRetainsBothLifecycleOwners(t *testing.T) {
	for _, mode := range []string{"caller", "manager", "legacy"} {
		t.Run(mode, func(t *testing.T) {
			manager := startupContextManager("a")
			target := &startupContextStrategy{}
			manager.RegisterStrategy("a", target, 1, 0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var err error
			if mode == "legacy" {
				err = manager.StartAll()
			} else {
				err = manager.StartAllContext(ctx)
			}
			if err != nil || target.received.Err() != nil {
				t.Fatalf("startup failed: %v", err)
			}
			if mode == "caller" {
				cancel()
			} else {
				manager.cancel()
			}
			select {
			case <-target.received.Done():
			case <-time.After(time.Second):
				t.Fatal("running strategy detached from lifecycle owner")
			}
		})
	}
}

func TestFundingCarryManagerContextCancellationEndsRealLoop(t *testing.T) {
	s, margin, _ := newFundingCarryRepayIntentFixture()
	margin.positions = nil
	s.direction, s.marginDebt, s.marginBorrowTransferID, s.strategySpotKnown = DirectionNone, 0, 0, true
	if err := s.persistRuntimeStateLocked(); err != nil {
		t.Fatal(err)
	}
	manager := startupContextManager("funding_carry")
	manager.RegisterStrategy("funding_carry", s, 1, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := manager.StartAllContext(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.IsRunning() {
		t.Fatal("real loop did not start")
	}
	cancel()
	select {
	case <-s.runDone:
	case <-time.After(time.Second):
		t.Fatal("real loop survived caller cancellation")
	}
	if s.IsRunning() {
		t.Fatal("cancelled loop still reported running")
	}
	if err := manager.StopAllWithError(); err != nil {
		t.Fatal(err)
	}
}
