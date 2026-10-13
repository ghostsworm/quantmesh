package main

import (
	"context"
	"fmt"
	"time"

	"quantmesh/storage"
)

// fundingCarryRuntimeStateAdapter keeps the generic strategy interface intact
// while fencing every FundingCarry write with its claimed shared generation.
type fundingCarryRuntimeStateAdapter struct {
	*strategyRuntimeStateAdapter
	generation         storage.FundingCarryRuntimeGeneration
	fundingIncomeScope string
}

func (a *fundingCarryRuntimeStateAdapter) FundingCarryFundingIncomeScope() string {
	if a == nil {
		return ""
	}
	return a.fundingIncomeScope
}

func (a *fundingCarryRuntimeStateAdapter) SaveRuntimeState(name string, schemaVersion int, payload string) error {
	if a == nil || a.strategyRuntimeStateAdapter == nil || a.storageService == nil || a.storageService.GetStorage() == nil {
		return fmt.Errorf("funding_carry runtime state storage is unavailable")
	}
	writer, ok := a.storageService.GetStorage().(storage.FundingCarryRuntimeGenerationStore)
	if !ok {
		return fmt.Errorf("storage does not support fenced funding_carry runtime state writes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return writer.SetFundingCarryRuntimeState(ctx, a.generation, &storage.StrategyRuntimeState{
		BotID: a.botID, StrategyName: name, SchemaVersion: schemaVersion, Payload: payload,
	})
}

// FundingCarryRecoveryBinding exposes the durable recovery repository together
// with the exact owner generation already claimed for this runtime.
func (a *fundingCarryRuntimeStateAdapter) FundingCarryRecoveryBinding() (storage.FundingRecoveryStore, storage.FundingCarryRuntimeGeneration, string, []string, error) {
	if a == nil || a.strategyRuntimeStateAdapter == nil || a.storageService == nil || a.storageService.GetStorage() == nil || a.generation.ID() == "" {
		return nil, storage.FundingCarryRuntimeGeneration{}, "", nil, fmt.Errorf("funding_carry recovery binding is unavailable")
	}
	store, ok := a.storageService.GetStorage().(storage.FundingRecoveryStore)
	if !ok {
		return nil, storage.FundingCarryRuntimeGeneration{}, "", nil, fmt.Errorf("storage does not support durable funding_carry recovery")
	}
	return store, a.generation, a.botID, a.generation.ScopeKeys(), nil
}

func (a *fundingCarryRuntimeStateAdapter) SaveRuntimeStateContext(ctx context.Context, name string, schemaVersion int, payload string) error {
	if ctx == nil || a == nil || a.strategyRuntimeStateAdapter == nil || a.storageService == nil || a.storageService.GetStorage() == nil {
		return fmt.Errorf("context-aware funding_carry runtime state storage is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	writer, ok := a.storageService.GetStorage().(storage.FundingCarryRuntimeGenerationStore)
	if !ok {
		return fmt.Errorf("storage does not support fenced funding_carry runtime state writes")
	}
	return writer.SetFundingCarryRuntimeState(ctx, a.generation, &storage.StrategyRuntimeState{
		BotID: a.botID, StrategyName: name, SchemaVersion: schemaVersion, Payload: payload,
	})
}

func (a *fundingCarryRuntimeStateAdapter) CompareAndSwapRuntimeState(ctx context.Context, name string, expectedVersion int, expectedPayload string, nextVersion int, nextPayload string) (bool, error) {
	if a == nil || a.strategyRuntimeStateAdapter == nil || a.storageService == nil || a.storageService.GetStorage() == nil {
		return false, fmt.Errorf("fenced funding_carry conditional runtime state storage unavailable")
	}
	writer, ok := a.storageService.GetStorage().(storage.FundingCarryRuntimeGenerationStore)
	if !ok {
		return false, fmt.Errorf("storage does not support fenced funding_carry conditional runtime state writes")
	}
	return writer.CompareAndSwapFundingCarryRuntimeState(ctx, a.generation,
		&storage.StrategyRuntimeState{BotID: a.botID, StrategyName: name, SchemaVersion: nextVersion, Payload: nextPayload},
		expectedVersion, expectedPayload)
}
