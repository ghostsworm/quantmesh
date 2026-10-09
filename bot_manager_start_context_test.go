package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
)

func TestStartBotCancellationWhileLifecycleLockedSkipsValidation(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	var validations atomic.Int32
	cause := errors.New("fixture stops before exchange initialization")
	bm.SetStartConfigValidator(func(config.BotConfig) error {
		validations.Add(1)
		return cause
	})
	unlock := bm.lockBotLifecycle("owner")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := bm.StartBot(ctx, config.BotConfig{ID: "owner"})
		done <- err
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
	if !returned || validations.Load() != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled startup retained waiter or entered validation: returned=%v validations=%d deadline=%v", returned, validations.Load(), errors.Is(err, context.DeadlineExceeded))
	}
	if err := bm.WithBotConfigurationLock("owner", func() error { return nil }); err != nil {
		t.Fatal("cancelled startup retained lifecycle lock")
	}
}

func TestStartBotCancelledDuringValidationSkipsTransition(t *testing.T) {
	bus := event.NewEventBus(8)
	t.Cleanup(bus.Close)
	bm := NewBotManager(&config.Config{}, bus, nil, nil, "")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	bm.SetStartConfigValidator(func(config.BotConfig) error { cancel(); return nil })
	// Storage is unavailable: entering the transition would produce a disabled
	// Bot error, not the context cancellation. No exchange is initialized.
	_, err := bm.StartBot(ctx, config.BotConfig{ID: "owner"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled validator entered transition: %v", err)
	}
}

func TestStartBotReleasedLifecycleLockPreservesValidation(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	cause := errors.New("fixture stops before exchange initialization")
	var validations atomic.Int32
	bm.SetStartConfigValidator(func(config.BotConfig) error { validations.Add(1); return cause })
	unlock := bm.lockBotLifecycle("owner")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := bm.StartBot(ctx, config.BotConfig{ID: "owner"}); done <- err }()
	unlock()
	if err := <-done; !errors.Is(err, cause) || validations.Load() != 1 {
		t.Fatalf("valid startup skipped validator or lost cause: %v", err)
	}
}

func TestStartBotManagedDuplicateRemainsIdempotent(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	bm.AddRuntime(&BotRuntime{BotID: "owner"})
	bm.SetStartConfigValidator(func(config.BotConfig) error {
		t.Error("duplicate start entered validation")
		return errors.New("unexpected validation")
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rt, err := bm.StartBot(ctx, config.BotConfig{ID: "owner"})
	if err != nil || rt != nil {
		t.Fatal("managed duplicate ceased to be an idempotent no-op")
	}
}

func TestStartBotRejectsNilContext(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	if _, err := bm.StartBot(nil, config.BotConfig{ID: "owner"}); err == nil {
		t.Fatal("nil context admitted startup")
	}
}
