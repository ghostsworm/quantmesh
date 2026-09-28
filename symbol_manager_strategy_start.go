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
	// Strategy Start may launch asynchronous decision loops before a later
	// strategy fails to restore. Hold admission closed for the full startup.
	gate.Block(strategyStartupFailureBlock)
	if err := start(); err != nil {
		return err
	}
	gate.Unblock(strategyStartupFailureBlock)
	return nil
}
