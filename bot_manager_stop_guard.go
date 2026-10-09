package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"quantmesh/logger"
)

const stopGuardWaitLimit = 30 * time.Second
const stopGuardPollInterval = 10 * time.Millisecond

// Lock files are persistent: unlinking one would let peers lock different
// inodes. This serializes controllers sharing a data path, not remote wallets.
func (bm *BotManager) lockStopJournal(ctx context.Context, botID string) (func(), error) {
	path := bm.stopJournalPath(botID)
	if br, ok := bm.Get(botID); ok && br.stopJournalFile != "" {
		path = br.stopJournalFile
	}
	return lockBotStatePath(ctx, path)
}

// All writers of a shared state document must use its path, not a Bot ID.
// The order is lifecycle -> Bot journal guard -> shared state document guard.
func lockBotStatePath(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("prepare Bot transition guard: %w", err)
	}
	file, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open Bot transition guard: %w", err)
	}
	closeGuard := func() {
		if err := file.Close(); err != nil {
			logger.Warn("close Bot transition guard: %v", err)
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, stopGuardWaitLimit)
	defer cancel()
	for {
		if err := waitCtx.Err(); err != nil {
			closeGuard()
			return nil, fmt.Errorf("wait Bot transition guard: %w", err)
		}
		locked, err := tryStopGuardLock(file)
		if err != nil {
			closeGuard()
			return nil, fmt.Errorf("lock Bot transition guard: %w", err)
		}
		if locked {
			if err := waitCtx.Err(); err != nil {
				closeGuard()
				return nil, fmt.Errorf("wait Bot transition guard: %w", err)
			}
			return closeGuard, nil
		}
		timer := time.NewTimer(stopGuardPollInterval)
		select {
		case <-waitCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
