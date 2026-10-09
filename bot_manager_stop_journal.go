package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"quantmesh/storage"
)

// Complete journals prove the stop callback finished, not that account positions
// are flat. Disable journals persist disabled state; shutdown journals preserve
// the prior enabled state. Incomplete records require reconciliation.
type botStopJournal struct {
	State         *storage.BotState         `json:"state"`
	Operation     string                    `json:"operation"`
	Mode          string                    `json:"mode,omitempty"`
	Complete      bool                      `json:"complete"`
	CapitalClaims []durableStopCapitalClaim `json:"capital_claims,omitempty"`
	Path          string                    `json:"-"`
}

const (
	botStopJournalModeDisable  = "disable"
	botStopJournalModeShutdown = "shutdown"
)

// Reservation tokens grant release authority for one generation. Keep them
// only in the private journal for a future reconciler; never expose them via APIs.
type durableStopCapitalClaim struct {
	WalletKey        string  `json:"wallet_key"`
	ReservationToken string  `json:"reservation_token"`
	Amount           float64 `json:"amount"`
	Exchange         string  `json:"exchange,omitempty"`
	Market           string  `json:"market,omitempty"`
	QuoteAsset       string  `json:"quote_asset,omitempty"`
	Symbol           string  `json:"symbol,omitempty"`
}

func durableStopClaims(claims []storage.AccountWalletCapitalClaim) []durableStopCapitalClaim {
	if len(claims) == 0 {
		return nil
	}
	durable := make([]durableStopCapitalClaim, 0, len(claims))
	for _, claim := range claims {
		durable = append(durable, durableStopCapitalClaim{
			WalletKey: claim.WalletKey, ReservationToken: claim.ReservationToken,
			Amount: claim.Amount, Exchange: claim.Exchange, Market: claim.Market,
			QuoteAsset: claim.QuoteAsset, Symbol: claim.Symbol,
		})
	}
	return durable
}

func validateDurableStopClaims(claims []durableStopCapitalClaim) error {
	seen := make(map[string]struct{}, len(claims))
	for _, claim := range claims {
		wallet, walletErr := hex.DecodeString(claim.WalletKey)
		token, err := hex.DecodeString(claim.ReservationToken)
		if walletErr != nil || len(wallet) != sha256.Size || err != nil || len(token) != sha256.Size ||
			math.IsNaN(claim.Amount) || math.IsInf(claim.Amount, 0) || claim.Amount <= 0 {
			return fmt.Errorf("invalid durable Bot stop capital claim")
		}
		if _, duplicate := seen[claim.WalletKey]; duplicate {
			return fmt.Errorf("duplicate durable Bot stop capital claim")
		}
		seen[claim.WalletKey] = struct{}{}
	}
	return nil
}

func (bm *BotManager) stopJournalPath(botID string) string {
	digest := sha256.Sum256([]byte(botID))
	return filepath.Join(bm.botStatesFilePath()+".stop-intents", hex.EncodeToString(digest[:])+".json")
}

func (bm *BotManager) readStopJournal(botID string) (*botStopJournal, error) {
	return readStopJournalAt(botID, bm.stopJournalPath(botID))
}

func readStopJournalAt(botID, path string) (*botStopJournal, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read durable Bot stop intent: %w", err)
	}
	var journal botStopJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("parse durable Bot stop intent: %w", err)
	}
	if journal.State == nil || journal.State.BotID != botID || journal.Operation == "" {
		return nil, fmt.Errorf("invalid durable Bot stop intent")
	}
	if journal.Mode == "" {
		journal.Mode = botStopJournalModeDisable // legacy journal format
	}
	validMode := journal.Mode == botStopJournalModeDisable && !journal.State.Enabled ||
		journal.Mode == botStopJournalModeShutdown && journal.State.Enabled
	if !validMode {
		return nil, fmt.Errorf("invalid durable Bot stop intent")
	}
	if err := validateDurableStopClaims(journal.CapitalClaims); err != nil {
		return nil, err
	}
	journal.Path = path
	return &journal, nil
}

func syncStopJournalDirectory(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func (bm *BotManager) syncStopJournalDirectory(path string) error {
	if bm.stopJournalDirectorySync != nil {
		return bm.stopJournalDirectorySync(path)
	}
	return syncStopJournalDirectory(path)
}

func (bm *BotManager) writeStopJournal(journal *botStopJournal, initial bool) error {
	path := bm.stopJournalPath(journal.State.BotID)
	if journal.Path != "" {
		path = journal.Path
	}
	if !initial {
		current, err := readStopJournalAt(journal.State.BotID, path)
		if err != nil {
			return err
		}
		if current == nil && journal.Complete {
			initial = true // retry after a prior unlink/directory-sync failure
		} else if current == nil || current.Operation != journal.Operation {
			return fmt.Errorf("Bot stop intent ownership changed")
		}
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	dirInfo, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !dirInfo.IsDir() {
		return fmt.Errorf("durable Bot stop intent directory is not a directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("secure durable Bot stop intent directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".stop-intent-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if initial {
		// Atomically publish without replacing another manager's stop intent.
		err = os.Link(name, path)
	} else {
		err = os.Rename(name, path)
	}
	if err != nil {
		return err
	}
	return bm.syncStopJournalDirectory(path)
}

func (bm *BotManager) retireStopJournal(journal *botStopJournal) error {
	path := bm.stopJournalPath(journal.State.BotID)
	if journal.Path != "" {
		path = journal.Path
	}
	current, err := readStopJournalAt(journal.State.BotID, path)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if current.Operation != journal.Operation {
		return fmt.Errorf("Bot stop intent ownership changed")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return bm.syncStopJournalDirectory(path)
}

func (bm *BotManager) beginDurableStop(br *BotRuntime, state *storage.BotState) error {
	return bm.beginDurableStopWithMode(br, state, botStopJournalModeDisable)
}

func (bm *BotManager) beginDurableStopWithMode(br *BotRuntime, state *storage.BotState, mode string) error {
	if mode != botStopJournalModeDisable && mode != botStopJournalModeShutdown {
		return fmt.Errorf("invalid durable Bot stop mode")
	}
	var claims []storage.AccountWalletCapitalClaim
	if br.Inner != nil {
		claims = append(claims, br.Inner.capitalReservationClaims...)
	}
	durableClaims := durableStopClaims(claims)
	if err := validateDurableStopClaims(durableClaims); err != nil {
		return err
	}
	if br.stopJournalOperation != "" {
		journalPath := br.stopJournalFile
		if journalPath == "" {
			journalPath = bm.stopJournalPath(br.BotID)
		}
		current, err := readStopJournalAt(br.BotID, journalPath)
		if err != nil {
			return err
		}
		if current == nil {
			// A prior write may have published the hard link but failed its
			// directory fsync. Re-publish the same operation, never a new one.
			journal := &botStopJournal{
				State: state, Operation: br.stopJournalOperation, Mode: mode,
				CapitalClaims: durableClaims, Path: journalPath,
			}
			if err := bm.writeStopJournal(journal, true); err != nil {
				return err
			}
			br.stopJournalMode = mode
			return nil
		}
		if current.Operation != br.stopJournalOperation {
			return fmt.Errorf("existing Bot stop intent ownership changed")
		}
		if current.Mode == mode && br.stopJournalMode == mode {
			if err := bm.syncStopJournalDirectory(current.Path); err != nil {
				return err
			}
			return nil
		}
		current.State = state
		current.Mode = mode
		current.CapitalClaims = durableClaims
		if err := bm.writeStopJournal(current, false); err != nil {
			return err
		}
		br.stopJournalMode = mode
		return nil
	}
	if current, err := bm.readStopJournal(br.BotID); err != nil || current != nil {
		if err != nil {
			return err
		}
		return fmt.Errorf("existing Bot stop intent requires reconciliation")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	operation := hex.EncodeToString(nonce[:])
	br.stopJournalOperation = operation
	br.stopJournalFile = bm.stopJournalPath(br.BotID)
	br.stopJournalMode = mode
	journal := &botStopJournal{State: state, Operation: operation, Mode: mode, CapitalClaims: durableClaims}
	if err := bm.writeStopJournal(journal, true); err != nil {
		return err
	}
	return nil
}

// StopBot may finish only a complete stop without replaying financial callbacks.
// An interrupted stop is never guessed complete.
func (bm *BotManager) recoverDurableStop(botID, updatedBy, reason string) error {
	journal, err := bm.readStopJournal(botID)
	if err != nil || journal == nil {
		return err
	}
	if !journal.Complete {
		if bm.stopRecovery == nil {
			return fmt.Errorf("interrupted Bot stop requires financial reconciliation")
		}
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := bm.stopRecovery(recoveryCtx, journal); err != nil {
			return fmt.Errorf("interrupted Bot stop financial reconciliation: %w", err)
		}
	}
	if updatedBy == "" {
		updatedBy = journal.State.UpdatedBy
	}
	if reason == "" {
		reason = journal.State.Reason
	}
	if err := bm.saveBotStateToDB(botID, false, updatedBy, reason); err != nil {
		return err
	}
	return bm.retireStopJournal(journal)
}

// A complete process-shutdown journal proves the stop callback returned
// successfully. Retire it under the Bot journal lock before an enabled Bot is
// started again; incomplete shutdowns remain blocked for financial review.
func (bm *BotManager) retireCompletedShutdownIntent(botID string) error {
	journal, err := bm.readStopJournal(botID)
	if err != nil || journal == nil {
		return err
	}
	if journal.Mode != botStopJournalModeShutdown || !journal.Complete {
		return fmt.Errorf("durable Bot stop intent requires reconciliation")
	}
	return bm.retireStopJournal(journal)
}
