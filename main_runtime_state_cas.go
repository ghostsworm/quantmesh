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
	return writer.CompareAndSwapStrategyRuntimeState(ctx, &storage.StrategyRuntimeState{BotID: a.botID, StrategyName: name, SchemaVersion: nextVersion, Payload: nextPayload}, expectedVersion, expectedPayload)
}
