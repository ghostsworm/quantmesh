package main

import (
	"fmt"

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
	if bm == nil || botID == "" || persist == nil {
		return fmt.Errorf("Bot configuration mutation requires manager, identity and persistence callback")
	}
	unlock := bm.lockBotLifecycle(botID)
	defer unlock()
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
