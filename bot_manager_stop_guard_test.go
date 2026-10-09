package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
)

func TestStopJournalGuardCancellationAndRelease(t *testing.T) {
	first := NewBotManager(nil, nil, nil, nil, "")
	second := NewBotManager(nil, nil, nil, nil, "")
	first.botStatesFileOverride = t.TempDir() + "/states.json"
	second.botStatesFileOverride = first.botStatesFileOverride
	release, err := first.lockStopJournal(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			release()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	unlock, err := second.lockStopJournal(ctx, "owner")
	if unlock != nil {
		unlock()
		t.Fatal("peer acquired held guard")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected cancellable wait, got %v", err)
	}
	other, err := second.lockStopJournal(context.Background(), "other")
	if err != nil {
		t.Fatal(err)
	}
	other()
	release()
	released = true
	next, err := second.lockStopJournal(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	next()
}

func TestStartBotCancelledBehindPeerJournalGuardSkipsValidator(t *testing.T) {
	first := NewBotManager(&config.Config{}, nil, nil, nil, "")
	second := NewBotManager(&config.Config{}, nil, nil, nil, "")
	first.botStatesFileOverride = t.TempDir() + "/states.json"
	second.botStatesFileOverride = first.botStatesFileOverride
	release, err := first.lockStopJournal(context.Background(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var validations atomic.Int32
	second.SetStartConfigValidator(func(config.BotConfig) error { validations.Add(1); return errors.New("unexpected validation") })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = second.StartBot(ctx, config.BotConfig{ID: "owner"})
	if !errors.Is(err, context.DeadlineExceeded) || validations.Load() != 0 {
		t.Fatalf("cancelled actual startup entered validator: %v, calls=%d", err, validations.Load())
	}
	if err := second.WithBotConfigurationLock("owner", func() error { return nil }); err != nil {
		t.Fatal("cancelled start retained lifecycle")
	}
}
