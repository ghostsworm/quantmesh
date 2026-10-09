package main

import (
	"context"
	"fmt"
	"time"

	"quantmesh/storage"
)

// StopAll owns its admission for the entire batch, but takes only one lifecycle
// lock at a time. Recheck registration after waiting; never stop a stale List
// pointer that was already removed or replaced by another transition.
func (bm *BotManager) stopAllRuntimeWithLifecycle(br *BotRuntime) error {
	unlock := bm.lockBotLifecycle(br.BotID)
	defer unlock()
	bm.runtimesMu.RLock()
	current := bm.runtimes[br.BotID]
	bm.runtimesMu.RUnlock()
	if current != br {
		return nil
	}
	br.stopTransitionInProgress.Store(true)
	defer br.stopTransitionInProgress.Store(false)
	if br.stopIntent != nil {
		return bm.stopBotWithReason(br.BotID, br.stopIntent.UpdatedBy, br.stopIntent.Reason)
	}
	if br.stopJournalOperation != "" && br.stopJournalMode != botStopJournalModeShutdown {
		journal, err := bm.readStopJournal(br.BotID)
		if err != nil {
			return err
		}
		if journal == nil {
			return fmt.Errorf("existing Bot stop intent disappeared")
		}
		return bm.stopBotWithReason(br.BotID, journal.State.UpdatedBy, journal.State.Reason)
	}
	unlockJournal, err := bm.lockStopJournal(context.Background(), br.BotID)
	if err != nil {
		return err
	}
	defer unlockJournal()
	if br.stopJournalOperation == "" {
		state := &storage.BotState{BotID: br.BotID, Enabled: true, UpdatedAt: time.Now().UTC(), UpdatedBy: "process_shutdown", Reason: "Bot runtime shutdown"}
		if err := bm.beginDurableStopWithMode(br, state, botStopJournalModeShutdown); err != nil {
			blockRuntimeOpeningForUnverifiedStop(br)
			return fmt.Errorf("persist Bot shutdown intent before stop: %w", err)
		}
	}
	if br.Inner != nil && br.Inner.StopWithError != nil && !br.shutdownCallbackCompleted.Load() {
		if err := br.Inner.StopWithError(); err != nil {
			classifyRuntimeStopFailure(br, err)
			return fmt.Errorf("stop Bot %s safely: %w", br.BotID, err)
		}
	} else if br.Inner != nil && br.Inner.Stop != nil && !br.shutdownCallbackCompleted.Load() {
		br.Inner.Stop()
		if reason := br.Inner.shutdownCloseUnverifiedReason(); reason != "" {
			br.stopReconciliationPending.Store(true)
			return fmt.Errorf("legacy Bot stop remains unverified: %s", reason)
		}
	}
	br.shutdownCallbackCompleted.Store(true)
	if br.Inner != nil {
		br.Inner.controllerStopCompleted.Store(true)
	}
	// From here on the durable shutdown intent is already published; this flag
	// now represents only completion persistence, not the active stop callback.
	br.stopPersistencePending.Store(true)
	journal, err := bm.readStopJournal(br.BotID)
	if err != nil {
		return err
	}
	if journal == nil && br.shutdownCallbackCompleted.Load() {
		return bm.unregisterStoppedRuntime(br)
	}
	if journal == nil || journal.Operation != br.stopJournalOperation || journal.Mode != botStopJournalModeShutdown {
		return fmt.Errorf("durable Bot shutdown intent changed before retirement")
	}
	journal.Complete = true
	if err := bm.writeStopJournal(journal, false); err != nil {
		return fmt.Errorf("persist verified Bot shutdown intent: %w", err)
	}
	if err := bm.retireStopJournal(journal); err != nil {
		return fmt.Errorf("retire verified Bot shutdown intent: %w", err)
	}
	return bm.unregisterStoppedRuntime(br)
}

func (bm *BotManager) unregisterStoppedRuntime(br *BotRuntime) error {
	br.configMu.RLock()
	cfg := br.Config
	br.configMu.RUnlock()
	unregisterWebSymbolProvidersForRuntime(&cfg)
	bm.runtimesMu.Lock()
	if bm.runtimes[br.BotID] == br {
		delete(bm.runtimes, br.BotID)
	}
	bm.runtimesMu.Unlock()
	return nil
}

func blockRuntimeOpeningForUnverifiedStop(br *BotRuntime) {
	if br == nil || br.Inner == nil {
		return
	}
	if br.Inner.OpeningGate != nil {
		br.Inner.OpeningGate.Block("stop_intent_unverified")
	}
	if br.Inner.SuperPositionManager != nil {
		br.Inner.SuperPositionManager.OpeningGate().Block("stop_intent_unverified")
	}
}

func classifyRuntimeStopFailure(br *BotRuntime, err error) {
	if br == nil || br.Inner == nil || err == nil {
		return
	}
	if isRetryableRuntimeOwnershipRelease(err) {
		br.stopOwnershipPending.Store(true)
	} else if isRetryableRuntimeStopDrain(err) {
		br.stopDrainPending.Store(true)
	} else if isRetryableRuntimeStopVerification(err) {
		br.stopVerificationPending.Store(true)
	} else if isRetryableRuntimeStopCleanup(err) {
		br.stopCleanupPending.Store(true)
	} else {
		br.stopReconciliationPending.Store(true)
		br.Inner.retainShutdownCloseFailure(err.Error())
	}
	if br.Inner.OpeningGate != nil {
		br.Inner.OpeningGate.Block("strategy_stop_unverified")
	}
}
