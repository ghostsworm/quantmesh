package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/web"
)

func TestBotConfigMutationSerializesAgainstStartValidation(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	entered, allowPersist := make(chan struct{}), make(chan struct{})
	validationEntered := make(chan struct{})
	mutationResult, startResult := make(chan error, 1), make(chan error, 1)
	var persisted atomic.Bool
	startCause := errors.New("fixture stops before exchange initialization")
	bm.SetStartConfigValidator(func(config.BotConfig) error {
		close(validationEntered)
		if !persisted.Load() {
			t.Error("start observed config before mutation completed")
		}
		return startCause
	})
	go func() {
		mutationResult <- bm.WithBotConfigurationLock("config-bot", func() error {
			close(entered)
			<-allowPersist
			persisted.Store(true)
			return nil
		})
	}()
	<-entered
	startAttempted := make(chan struct{})
	go func() {
		close(startAttempted)
		_, err := bm.StartBot(context.Background(), config.BotConfig{ID: "config-bot"})
		startResult <- err
	}()
	<-startAttempted
	select {
	case <-validationEntered:
		t.Error("start validation ran inside configuration mutation")
	case <-time.After(25 * time.Millisecond):
	}
	close(allowPersist)
	if err := <-mutationResult; err != nil {
		t.Fatal(err)
	}
	if err := <-startResult; !errors.Is(err, startCause) {
		t.Fatalf("start cause lost: %v", err)
	}
}

func TestBotConfigMutationRefusesManagedRuntimeWithoutStopping(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	stopCalls := 0
	bm.AddRuntime(&BotRuntime{BotID: "managed", Inner: &SymbolRuntime{StopWithError: func() error {
		stopCalls++
		return nil
	}}})
	persisted := false
	err := bm.WithBotConfigurationLock("managed", func() error { persisted = true; return nil })
	if !errors.Is(err, web.ErrBotConfigRuntimeManaged) || persisted || stopCalls != 0 {
		t.Fatalf("managed recovery config modified or runtime stopped: %v", err)
	}
}

func TestBotConfigMutationReleasesLockAfterFailure(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	cause := errors.New("fixture persistence failure")
	if err := bm.WithBotConfigurationLock("config-bot", func() error { return cause }); !errors.Is(err, cause) {
		t.Fatalf("mutation error lost: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- bm.WithBotConfigurationLock("config-bot", func() error { return nil }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("failed mutation leaked lifecycle lock")
	}
}

func TestBotConfigMutationRefusesProcessShutdown(t *testing.T) {
	bm := NewBotManager(&config.Config{}, nil, nil, nil, "")
	bm.runtimeAdmissions.Block("shutdown")
	persisted := false
	if err := bm.WithBotConfigurationLock("config-bot", func() error { persisted = true; return nil }); err == nil || persisted {
		t.Fatal("shutdown admitted recovery configuration mutation")
	}
}
