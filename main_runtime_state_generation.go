package main

import (
	"context"
	"fmt"
	"time"

	"quantmesh/execution"
	"quantmesh/storage"
)

const strategyRuntimeStateWriteTimeout = 15 * time.Second

func newOwnerFencedStrategyRuntimeStateAdapter(
	ctx context.Context,
	storageService *storage.StorageService,
	botID string,
	ownerScope execution.IntentScope,
) (*strategyRuntimeStateAdapter, error) {
	return newOwnerFencedStrategyRuntimeStateAdapterForScopes(ctx, storageService, botID, []execution.IntentScope{ownerScope})
}

func newOwnerFencedStrategyRuntimeStateAdapterForScopes(
	ctx context.Context,
	storageService *storage.StorageService,
	botID string,
	ownerScopes []execution.IntentScope,
) (*strategyRuntimeStateAdapter, error) {
	adapter := &strategyRuntimeStateAdapter{storageService: storageService, botID: botID}
	if storageService == nil || storageService.GetStorage() == nil {
		return adapter, nil
	}
	store, ok := storageService.GetStorage().(storage.FundingCarryRuntimeGenerationStore)
	if !ok {
		return nil, fmt.Errorf("storage backend does not support owner-fenced strategy runtime state")
	}
	scopeKeys := make([]string, 0, len(ownerScopes))
	for _, ownerScope := range ownerScopes {
		scopeKey, err := ownerScope.Key()
		if err != nil {
			return nil, fmt.Errorf("derive strategy runtime state owner scope: %w", err)
		}
		scopeKeys = append(scopeKeys, scopeKey)
	}
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, scopeKeys)
	if err != nil {
		return nil, fmt.Errorf("claim strategy runtime state owner generation: %w", err)
	}
	adapter.ownerGeneration = &generation
	return adapter, nil
}
