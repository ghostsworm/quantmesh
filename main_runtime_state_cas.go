package main

import (
	"context"
	"fmt"

	"quantmesh/storage"
)

func (a *strategyRuntimeStateAdapter) CompareAndSwapRuntimeState(ctx context.Context, name string, expectedVersion int, expectedPayload string, nextVersion int, nextPayload string) (bool, error) {
	if a == nil || a.storageService == nil || a.storageService.GetStorage() == nil {
		return false, fmt.Errorf("conditional runtime state storage unavailable")
	}
	writer, ok := a.storageService.GetStorage().(storage.StrategyRuntimeStateConditionalWriter)
	if !ok {
		return false, fmt.Errorf("storage does not support atomic conditional runtime state writes")
	}
	next := &storage.StrategyRuntimeState{BotID: a.botID, StrategyName: name, SchemaVersion: nextVersion, Payload: nextPayload}
	if a.ownerGeneration != nil {
		fencedWriter, ok := a.storageService.GetStorage().(storage.FundingCarryRuntimeGenerationStore)
		if !ok {
			return false, fmt.Errorf("storage backend does not support owner-fenced strategy runtime state")
		}
		return fencedWriter.CompareAndSwapFundingCarryRuntimeState(ctx, *a.ownerGeneration, next, expectedVersion, expectedPayload)
	}
	return writer.CompareAndSwapStrategyRuntimeState(ctx, next, expectedVersion, expectedPayload)
}
