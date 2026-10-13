package main

import (
	"context"
	"fmt"
	"time"

	"quantmesh/web"
)

// WithBotConfigurationLock excludes starts/stops while a recovery configuration
// is checked and persisted. It does not stop a managed runtime on the user's behalf.
func (bm *BotManager) WithBotConfigurationLock(botID string, persist func() error) error {
	if persist == nil {
		return fmt.Errorf("Bot configuration persistence callback is required")
	}
	return bm.WithBotStrategyConfigurationLock(botID, func(managed bool) error {
		if managed {
			return web.ErrBotConfigRuntimeManaged
		}
		return persist()
	})
}

// Managed runtimes may retain existing hot risk controls. The callback must
// reject recovery-contract changes for managed runtimes; this method only
// supplies lifecycle serialization and the authoritative registration state.
func (bm *BotManager) WithBotStrategyConfigurationLock(botID string, persist func(bool) error) error {
	return bm.WithBotStrategyConfigurationContext(context.Background(), botID, persist)
}

const botLifecycleLockRetryInterval = 10 * time.Millisecond
const botRuntimeOperationTimeout = 20 * time.Second

// Context-aware callers wait on the same lifecycle mutex without spawning a
// waiter goroutine that could outlive cancellation and retain a future lock.
func (bm *BotManager) WithBotStrategyConfigurationContext(ctx context.Context, botID string, persist func(bool) error) error {
	if bm == nil || ctx == nil || botID == "" || persist == nil {
		return fmt.Errorf("Bot configuration mutation requires manager, identity and persistence callback")
	}
	mu := bm.botLifecycleMutex(botID)
	if ctx.Done() == nil {
		mu.Lock()
	} else {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			if mu.TryLock() {
				break
			}
			timer := time.NewTimer(botLifecycleLockRetryInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	defer mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	finish, err := bm.runtimeAdmissions.Begin()
	if err != nil {
		return fmt.Errorf("process shutdown rejects Bot configuration mutation: %w", err)
	}
	defer finish()
	bm.runtimesMu.RLock()
	_, managed := bm.runtimes[botID]
	bm.runtimesMu.RUnlock()
	return persist(managed)
}

// WithBotRuntimeOperationContext serializes a bounded recovery operation with
// Bot start/stop and rejects a runtime whose stop state is already uncertain.
// The runtime map lock is released before operation runs; callers must honor ctx.
func (bm *BotManager) WithBotRuntimeOperationContext(ctx context.Context, botID string, operation func(context.Context, *BotRuntime) error) error {
	if ctx == nil || operation == nil {
		return fmt.Errorf("Bot runtime operation requires context and callback")
	}
	operationCtx, cancel := context.WithTimeout(ctx, botRuntimeOperationTimeout)
	defer cancel()
	return bm.WithBotStrategyConfigurationContext(operationCtx, botID, func(managed bool) error {
		if !managed {
			return fmt.Errorf("Bot runtime is unavailable")
		}
		runtime, found := bm.Get(botID)
		if !found || runtime == nil || runtime.Inner == nil || runtime.stopTransitionPending() {
			return fmt.Errorf("Bot runtime is stopping or unavailable")
		}
		return operation(operationCtx, runtime)
	})
}
