package main

import (
	"errors"
	"testing"

	"quantmesh/execution"
)

func TestStrategyStartupFailureBlocksBotWideOpening(t *testing.T) {
	gate := &execution.OpeningGate{}
	startupErr := errors.New("persisted strategy state is invalid")
	var blockedDuringStartup bool
	err := startStrategiesWithFailClosedGate(func() error {
		blockedDuringStartup = gate.HasBlock(strategyStartupFailureBlock)
		return startupErr
	}, gate)
	if !errors.Is(err, startupErr) {
		t.Fatalf("startup error should be propagated, got %v", err)
	}
	if !blockedDuringStartup {
		t.Fatal("all strategy restore/start operations must run behind the opening gate")
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
	var blockedDuringStartup bool
	if err := startStrategiesWithFailClosedGate(func() error {
		blockedDuringStartup = gate.HasBlock(strategyStartupFailureBlock)
		return nil
	}, gate); err != nil {
		t.Fatalf("startup: %v", err)
	}
	if !blockedDuringStartup {
		t.Fatal("opening gate must remain blocked until all strategies start successfully")
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

func TestSuccessfulStrategyStartupPreservesIndependentOpeningBlocks(t *testing.T) {
	gate := &execution.OpeningGate{}
	gate.Block("operator_pause")
	if err := startStrategiesWithFailClosedGate(func() error { return nil }, gate); err != nil {
		t.Fatalf("startup: %v", err)
	}
	if gate.HasBlock(strategyStartupFailureBlock) {
		t.Fatal("successful startup retained its own admission block")
	}
	if !gate.HasBlock("operator_pause") {
		t.Fatal("successful startup cleared an unrelated opening block")
	}
}

func TestStrategyStartupRequiresGateAndCallback(t *testing.T) {
	if err := startStrategiesWithFailClosedGate(nil, &execution.OpeningGate{}); err == nil {
		t.Fatal("nil startup callback must fail")
	}
	if err := startStrategiesWithFailClosedGate(func() error { return nil }, nil); err == nil {
		t.Fatal("nil opening gate must fail")
	}
}
