package main

import (
	"errors"
	"testing"

	"quantmesh/execution"
)

func TestStrategyStartupFailureBlocksBotWideOpening(t *testing.T) {
	gate := &execution.OpeningGate{}
	startupErr := errors.New("persisted strategy state is invalid")
	err := startStrategiesWithFailClosedGate(func() error { return startupErr }, gate)
	if !errors.Is(err, startupErr) {
		t.Fatalf("startup error should be propagated, got %v", err)
	}
	if !gate.HasBlock(strategyStartupFailureBlock) {
		t.Fatal("strategy restore failure must block all bot opening paths")
	}
	if _, err := gate.Begin(); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("opening should remain blocked, got %v", err)
	}
}

func TestSuccessfulStrategyStartupDoesNotAddFailureBlock(t *testing.T) {
	gate := &execution.OpeningGate{}
	if err := startStrategiesWithFailClosedGate(func() error { return nil }, gate); err != nil {
		t.Fatalf("startup: %v", err)
	}
	if gate.HasBlock(strategyStartupFailureBlock) {
		t.Fatal("successful strategy startup must not leave a failure block")
	}
	release, err := gate.Begin()
	if err != nil {
		t.Fatalf("opening should be admitted after successful startup: %v", err)
	}
	release()
}

func TestStrategyStartupRequiresGateAndCallback(t *testing.T) {
	if err := startStrategiesWithFailClosedGate(nil, &execution.OpeningGate{}); err == nil {
		t.Fatal("nil startup callback must fail")
	}
	if err := startStrategiesWithFailClosedGate(func() error { return nil }, nil); err == nil {
		t.Fatal("nil opening gate must fail")
	}
}
