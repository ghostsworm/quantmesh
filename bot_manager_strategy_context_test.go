package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
)

type strategyContextCoordinator interface {
	WithBotStrategyConfigurationContext(context.Context, string, func(bool) error) error
}

func TestActualStrategyCoordinatorCancellationWhileLifecycleLocked(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	adapter := &botManagerProviderAdapter{manager: &SymbolManager{botManager: bm}}
	unlock := bm.lockBotLifecycle("owner")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	called := false
	done := make(chan error, 1)
	go func() {
		persist := func(bool) error { called = true; return nil }
		if provider, ok := any(adapter).(strategyContextCoordinator); ok {
			done <- provider.WithBotStrategyConfigurationContext(ctx, "owner", persist)
		} else {
			done <- adapter.WithBotStrategyConfigurationLock("owner", persist)
		}
	}()
	returned := false
	var err error
	select {
	case err = <-done:
		returned = true
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	if !returned {
		err = <-done
	}
	if !returned || called || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("strategy save ignored request cancellation: returned=%v callback=%v err=%v", returned, called, err)
	}
	if err := bm.WithBotConfigurationLock("owner", func() error { return nil }); err != nil {
		t.Fatal("cancelled strategy save retained lifecycle lock")
	}
}
