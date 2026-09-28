package main

import (
	"fmt"

	"quantmesh/execution"
)

const strategyStartupFailureBlock = "strategy_startup_unverified"

func startStrategiesWithFailClosedGate(start func() error, gate *execution.OpeningGate) error {
	if start == nil || gate == nil {
		return fmt.Errorf("strategy startup and opening gate are required")
	}
	if err := start(); err != nil {
		gate.Block(strategyStartupFailureBlock)
		return err
	}
	return nil
}
