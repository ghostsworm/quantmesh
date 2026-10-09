package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/risk"
)

const retiredEquityAccountsCheckpointKey = "retired-equity-accounts-v1"
const retiredEquityAccountPendingVerification = "pending_verification"
const retiredEquityAccountReadyForReset = "ready_for_explicit_reset"
const retiredEquityFlatEvidenceRequired = 2
const retiredEquityFlatEvidenceInterval = time.Minute
const retiredEquityAccountPauseSource = "retired_equity_account_unverified"
const retiredEquityVerificationInterval = time.Minute
const retiredEquityVerificationTimeout = 30 * time.Second

type retiredEquityAccountsState struct {
	Version      int                          `json:"version"`
	Revision     int64                        `json:"revision"`
	Accounts     []retiredEquityAccountRecord `json:"accounts"`
	PendingReset *retiredEquityResetRecord    `json:"pending_reset,omitempty"`
	ResetHistory []retiredEquityResetRecord   `json:"reset_history,omitempty"`
}

type retiredEquityResetRecord struct {
	ID                string    `json:"id"`
	Actor             string    `json:"actor"`
	TargetScope       string    `json:"target_scope"`
	StartedAt         time.Time `json:"started_at"`
	CompletedAt       time.Time `json:"completed_at,omitempty"`
	BaselineRevision  int64     `json:"baseline_revision,omitempty"`
	RetiredAccountIDs []string  `json:"retired_account_ids"`
}

type equityBaselineResetter interface {
	ResetEquityBaseline(context.Context, string, string) (risk.EquityCheckpoint, error)
}

type retiredEquityAccountRecord struct {
	ID                    string    `json:"id"`
	Exchange              string    `json:"exchange"`
	MarketType            string    `json:"market_type"`
	Scope                 string    `json:"scope"`
	CredentialVersion     string    `json:"credential_version"`
	CredentialsCiphertext string    `json:"credentials_ciphertext"`
	Status                string    `json:"status"`
	RetiredAt             time.Time `json:"retired_at"`
	LastObservedAt        time.Time `json:"last_observed_at,omitempty"`
	LastFlatAt            time.Time `json:"last_flat_at,omitempty"`
	FlatEvidenceCount     int       `json:"flat_evidence_count"`
	LastEvidenceResult    string    `json:"last_evidence_result,omitempty"`
	credentials           equityAccountEvidenceConfig
}

type retiredEquityAccountsStore struct {
	mu      sync.Mutex
	backend riskCheckpointBackend
	key     []byte
	keyErr  error
}

type RetiredEquityAccountStatus struct {
	ID                 string    `json:"id"`
	Exchange           string    `json:"exchange"`
	MarketType         string    `json:"market_type"`
	AccountScope       string    `json:"account_scope"`
	Status             string    `json:"status"`
	RetiredAt          time.Time `json:"retired_at"`
	LastObservedAt     time.Time `json:"last_observed_at,omitempty"`
	LastFlatAt         time.Time `json:"last_flat_at,omitempty"`
	FlatEvidenceCount  int       `json:"flat_evidence_count"`
	LastEvidenceResult string    `json:"last_evidence_result,omitempty"`
}

func newRetiredEquityAccountsStore(backend riskCheckpointBackend) *retiredEquityAccountsStore {
	key, err := config.LoadMasterKey("")
	return &retiredEquityAccountsStore{backend: backend, key: key, keyErr: err}
}

func (s *retiredEquityAccountsStore) archiveRemoved(ctx context.Context, previous, next equityScopeSnapshot, now time.Time) error {
	if s == nil || s.backend == nil {
		return fmt.Errorf("retired account archive storage is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := removedEquityAccounts(previous, next)
	if len(removed) == 0 {
		return nil
	}
	if s.keyErr != nil {
		return fmt.Errorf("load retired account archive key: %w", s.keyErr)
	}
	if len(s.key) < 32 {
		return fmt.Errorf("retired account archive requires the configured master key")
	}
	if now.IsZero() {
		return fmt.Errorf("retired account archive timestamp is required")
	}

	payload, revision, err := s.backend.LoadRiskCheckpoint(ctx, retiredEquityAccountsCheckpointKey)
	if err != nil {
		return fmt.Errorf("load retired account archive: %w", err)
	}
	state := retiredEquityAccountsState{Version: 1, Revision: revision, Accounts: []retiredEquityAccountRecord{}}
	if payload != nil {
		if err := json.Unmarshal(payload, &state); err != nil {
			return fmt.Errorf("decode retired account archive: %w", err)
		}
		if err := validateRetiredEquityAccountsState(state, revision); err != nil {
			return err
		}
	}
	known := make(map[string]struct{}, len(state.Accounts))
	for _, account := range state.Accounts {
		if err := validateRetiredEquityAccount(account); err != nil {
			return fmt.Errorf("retired account archive contains an invalid record")
		}
		if _, duplicate := known[account.ID]; duplicate {
			return fmt.Errorf("retired account archive contains duplicate records")
		}
		known[account.ID] = struct{}{}
	}
	changed := false
	for _, account := range removed {
		id := retiredEquityAccountID(account)
		if _, ok := known[id]; ok {
			continue
		}
		plaintext, err := json.Marshal(account)
		if err != nil {
			return fmt.Errorf("encode retired account credentials: %w", err)
		}
		ciphertext, err := config.EncryptAPIKey(string(plaintext), s.key)
		if err != nil {
			return fmt.Errorf("encrypt retired account credentials: %w", err)
		}
		record := retiredEquityAccountRecord{ID: id, Exchange: account.Exchange, MarketType: account.MarketType,
			Scope: account.Scope, CredentialVersion: account.credentialVersion(), CredentialsCiphertext: ciphertext,
			Status: retiredEquityAccountPendingVerification, RetiredAt: now.UTC()}
		state.Accounts = append(state.Accounts, record)
		known[id] = struct{}{}
		changed = true
	}
	if !changed {
		return nil
	}
	sort.Slice(state.Accounts, func(i, j int) bool { return state.Accounts[i].ID < state.Accounts[j].ID })
	state.Revision = revision + 1
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode retired account archive state: %w", err)
	}
	if err := s.backend.SaveRiskCheckpoint(ctx, retiredEquityAccountsCheckpointKey, revision, encoded); err != nil {
		return fmt.Errorf("persist retired account archive: %w", err)
	}
	return nil
}

func (s *retiredEquityAccountsStore) load(ctx context.Context) ([]retiredEquityAccountRecord, int64, error) {
	if s == nil || s.backend == nil {
		return nil, 0, fmt.Errorf("retired account archive storage is unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, revision, err := s.backend.LoadRiskCheckpoint(ctx, retiredEquityAccountsCheckpointKey)
	if err != nil {
		return nil, 0, fmt.Errorf("load retired account archive: %w", err)
	}
	if payload == nil && revision == 0 {
		return []retiredEquityAccountRecord{}, 0, nil
	}
	if s.keyErr != nil {
		return nil, 0, fmt.Errorf("load retired account archive key: %w", s.keyErr)
	}
	if len(s.key) < 32 {
		return nil, 0, fmt.Errorf("retired account archive requires the configured master key")
	}
	var state retiredEquityAccountsState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, 0, fmt.Errorf("decode retired account archive: %w", err)
	}
	if err := validateRetiredEquityAccountsState(state, revision); err != nil {
		return nil, 0, err
	}
	for i := range state.Accounts {
		record := &state.Accounts[i]
		plaintext, err := config.DecryptAPIKey(record.CredentialsCiphertext, s.key)
		if err != nil {
			return nil, 0, fmt.Errorf("decrypt retired account credentials")
		}
		if err := json.Unmarshal([]byte(plaintext), &record.credentials); err != nil {
			return nil, 0, fmt.Errorf("decode retired account credentials")
		}
		if record.credentials.Exchange != record.Exchange || record.credentials.MarketType != record.MarketType ||
			record.credentials.Scope != record.Scope || record.credentials.credentialVersion() != record.CredentialVersion ||
			retiredEquityAccountID(record.credentials) != record.ID {
			return nil, 0, fmt.Errorf("retired account credentials do not match their archive identity")
		}
	}
	return state.Accounts, revision, nil
}

func (bm *BotManager) RetiredEquityAccountStatuses(ctx context.Context) ([]RetiredEquityAccountStatus, error) {
	if bm == nil || ctx == nil || bm.retiredEquityAccounts == nil {
		return nil, fmt.Errorf("retired account status storage is unavailable")
	}
	accounts, _, err := bm.retiredEquityAccounts.load(ctx)
	if err != nil {
		return nil, err
	}
	statuses := make([]RetiredEquityAccountStatus, 0, len(accounts))
	for _, account := range accounts {
		statuses = append(statuses, RetiredEquityAccountStatus{ID: account.ID, Exchange: account.Exchange,
			MarketType: account.MarketType, AccountScope: account.Scope, Status: account.Status, RetiredAt: account.RetiredAt,
			LastObservedAt: account.LastObservedAt, LastFlatAt: account.LastFlatAt, FlatEvidenceCount: account.FlatEvidenceCount,
			LastEvidenceResult: account.LastEvidenceResult})
	}
	return statuses, nil
}

func (bm *BotManager) RetiredEquityAccountResetHistory(ctx context.Context) ([]retiredEquityResetRecord, error) {
	if bm == nil || ctx == nil || bm.retiredEquityAccounts == nil {
		return nil, fmt.Errorf("retired account reset history is unavailable")
	}
	if _, _, err := bm.retiredEquityAccounts.load(ctx); err != nil {
		return nil, err
	}
	store := bm.retiredEquityAccounts
	store.mu.Lock()
	defer store.mu.Unlock()
	payload, revision, err := store.backend.LoadRiskCheckpoint(ctx, retiredEquityAccountsCheckpointKey)
	if err != nil {
		return nil, fmt.Errorf("load retired-account reset history: %w", err)
	}
	if payload == nil {
		return []retiredEquityResetRecord{}, nil
	}
	var state retiredEquityAccountsState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, fmt.Errorf("decode retired-account reset history: %w", err)
	}
	if err := validateRetiredEquityAccountsState(state, revision); err != nil {
		return nil, err
	}
	return append([]retiredEquityResetRecord(nil), state.ResetHistory...), nil
}

// ResetRetiredEquityAccounts performs an explicit, audited account-scope
// transition. It keeps the opening hold until fresh retired-account evidence,
// the new-scope baseline CAS, and the durable audit/archive update all succeed.
func (bm *BotManager) ResetRetiredEquityAccounts(ctx context.Context, actor string, newObserver retiredEquityFuturesObserverFactory) (retiredEquityResetRecord, error) {
	if bm == nil || ctx == nil || strings.TrimSpace(actor) == "" || bm.retiredEquityAccounts == nil {
		return retiredEquityResetRecord{}, fmt.Errorf("retired-account reset is unavailable")
	}
	bm.equityConfigRefreshMu.Lock()
	defer bm.equityConfigRefreshMu.Unlock()
	scope := bm.equityScopeSnapshot()
	if !scope.configured || scope.err != "" || strings.TrimSpace(scope.scope) == "" {
		return retiredEquityResetRecord{}, fmt.Errorf("current equity account scope is unavailable or unsupported")
	}
	bm.openingPauseCoordinatorMu.RLock()
	coordinator := bm.openingPauseCoordinator
	bm.openingPauseCoordinatorMu.RUnlock()
	if coordinator == nil {
		return retiredEquityResetRecord{}, fmt.Errorf("opening pause coordinator is unavailable")
	}
	accounts, _, err := bm.retiredEquityAccounts.load(ctx)
	if err != nil {
		return retiredEquityResetRecord{}, err
	}
	if len(accounts) > 0 {
		if err := coordinator.Pause(retiredEquityAccountPauseSource, "舊帳戶重置及新權益基線核驗進行中", nil); err != nil {
			return retiredEquityResetRecord{}, fmt.Errorf("cannot verify the retired-account opening hold: %w", err)
		}
	}
	operation, completed, err := bm.retiredEquityAccounts.beginReset(ctx, actor, scope.scope, time.Now())
	if err != nil {
		return retiredEquityResetRecord{}, err
	}
	if completed {
		if _, err := coordinator.ReleaseChecked(retiredEquityAccountPauseSource, nil); err != nil {
			return operation, fmt.Errorf("reset was durably completed but opening-hold release is unverified: %w", err)
		}
		return operation, nil
	}
	if err := bm.verifyRetiredEquityAccountsOnce(ctx, newObserver); err != nil {
		return operation, fmt.Errorf("fresh retired-account verification is incomplete: %w", err)
	}
	accounts, _, err = bm.retiredEquityAccounts.load(ctx)
	if err != nil {
		return operation, fmt.Errorf("reload retired-account evidence after verification: %w", err)
	}
	readyIDs := make([]string, 0, len(accounts))
	for _, account := range accounts {
		if account.Status != retiredEquityAccountReadyForReset || account.FlatEvidenceCount != retiredEquityFlatEvidenceRequired {
			return operation, fmt.Errorf("retired account %s is not verified flat", account.ID)
		}
		readyIDs = append(readyIDs, account.ID)
	}
	sort.Strings(readyIDs)
	if strings.Join(readyIDs, ",") != strings.Join(operation.RetiredAccountIDs, ",") {
		return operation, fmt.Errorf("retired-account membership changed during reset")
	}
	bm.equityResetterMu.RLock()
	resetter := bm.equityBaselineResetter
	bm.equityResetterMu.RUnlock()
	if resetter == nil {
		return operation, fmt.Errorf("durable equity baseline resetter is unavailable")
	}
	baseline, err := resetter.ResetEquityBaseline(ctx, operation.ID, scope.scope)
	if err != nil {
		return operation, fmt.Errorf("new-scope equity baseline was not verified: %w", err)
	}
	if baseline.Scope != scope.scope || baseline.ResetOperationID != operation.ID || baseline.ResetOperationRevision < 1 ||
		baseline.ResetOperationRevision > baseline.Revision {
		return operation, fmt.Errorf("baseline resetter returned mismatched durable evidence")
	}
	completedOperation, err := bm.retiredEquityAccounts.completeReset(ctx, operation.ID, baseline.ResetOperationRevision, time.Now())
	if err != nil {
		return operation, fmt.Errorf("baseline persisted but reset audit/archive completion failed; retry the same admin operation: %w", err)
	}
	if _, err := coordinator.ReleaseChecked(retiredEquityAccountPauseSource, nil); err != nil {
		return completedOperation, fmt.Errorf("reset audit is durable but opening-hold release is unverified: %w", err)
	}
	return completedOperation, nil
}

func (s *retiredEquityAccountsStore) recordEvidence(ctx context.Context, id string, complete, flat bool, observedAt time.Time) error {
	if s == nil || s.backend == nil || strings.TrimSpace(id) == "" || observedAt.IsZero() || observedAt.After(time.Now().Add(5*time.Second)) {
		return fmt.Errorf("retired account evidence identity or timestamp is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keyErr != nil || len(s.key) < 32 {
		return fmt.Errorf("retired account evidence requires the configured master key")
	}
	payload, revision, err := s.backend.LoadRiskCheckpoint(ctx, retiredEquityAccountsCheckpointKey)
	if err != nil {
		return fmt.Errorf("load retired account evidence state: %w", err)
	}
	if payload == nil || revision < 1 {
		return fmt.Errorf("retired account evidence archive is absent")
	}
	var state retiredEquityAccountsState
	if err := json.Unmarshal(payload, &state); err != nil {
		return fmt.Errorf("retired account evidence archive is invalid")
	}
	if err := validateRetiredEquityAccountsState(state, revision); err != nil {
		return fmt.Errorf("retired account evidence archive is invalid: %w", err)
	}
	index := -1
	for i := range state.Accounts {
		if state.Accounts[i].ID == id {
			index = i
		}
	}
	if index < 0 {
		return fmt.Errorf("retired account evidence record was not found")
	}
	record := &state.Accounts[index]
	if err := validateRetiredEquityAccount(*record); err != nil {
		return fmt.Errorf("retired account evidence record is invalid")
	}
	observedAt = observedAt.UTC()
	if !record.LastObservedAt.IsZero() && observedAt.Before(record.LastObservedAt) {
		return fmt.Errorf("retired account evidence timestamp regressed")
	}
	record.LastObservedAt = observedAt
	switch {
	case !complete:
		record.LastEvidenceResult = "incomplete"
		record.FlatEvidenceCount = 0
		record.LastFlatAt = time.Time{}
		record.Status = retiredEquityAccountPendingVerification
	case !flat:
		record.LastEvidenceResult = "open_exposure"
		record.FlatEvidenceCount = 0
		record.LastFlatAt = time.Time{}
		record.Status = retiredEquityAccountPendingVerification
	default:
		record.LastEvidenceResult = "flat"
		if record.FlatEvidenceCount == 0 {
			record.FlatEvidenceCount = 1
			record.LastFlatAt = observedAt
		} else if record.FlatEvidenceCount < retiredEquityFlatEvidenceRequired && observedAt.Sub(record.LastFlatAt) >= retiredEquityFlatEvidenceInterval {
			record.FlatEvidenceCount++
			record.LastFlatAt = observedAt
		} else if record.FlatEvidenceCount >= retiredEquityFlatEvidenceRequired {
			// Keep the saturating readiness count valid while refreshing the
			// latest flat-evidence time for continuously verified accounts.
			record.LastFlatAt = observedAt
		}
		if record.FlatEvidenceCount >= retiredEquityFlatEvidenceRequired {
			record.Status = retiredEquityAccountReadyForReset
		} else {
			record.Status = retiredEquityAccountPendingVerification
		}
	}
	if err := validateRetiredEquityAccount(*record); err != nil {
		return fmt.Errorf("refuse to persist invalid retired-account evidence state: %w", err)
	}
	state.Revision = revision + 1
	encoded, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode retired account evidence state: %w", err)
	}
	if err := s.backend.SaveRiskCheckpoint(ctx, retiredEquityAccountsCheckpointKey, revision, encoded); err != nil {
		return fmt.Errorf("persist retired account evidence state: %w", err)
	}
	return nil
}

func (s *retiredEquityAccountsStore) beginReset(ctx context.Context, actor, targetScope string, now time.Time) (retiredEquityResetRecord, bool, error) {
	actor, targetScope = strings.TrimSpace(actor), strings.TrimSpace(targetScope)
	if s == nil || s.backend == nil || ctx == nil || actor == "" || targetScope == "" || now.IsZero() {
		return retiredEquityResetRecord{}, false, fmt.Errorf("retired-account reset requires storage, actor, target scope, and timestamp")
	}
	if _, _, err := s.load(ctx); err != nil {
		return retiredEquityResetRecord{}, false, fmt.Errorf("validate retired-account archive before reset: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, revision, err := s.backend.LoadRiskCheckpoint(ctx, retiredEquityAccountsCheckpointKey)
	if err != nil {
		return retiredEquityResetRecord{}, false, fmt.Errorf("load retired-account reset state: %w", err)
	}
	if payload == nil || revision < 1 {
		return retiredEquityResetRecord{}, false, fmt.Errorf("retired-account archive is absent")
	}
	var state retiredEquityAccountsState
	if err := json.Unmarshal(payload, &state); err != nil {
		return retiredEquityResetRecord{}, false, fmt.Errorf("decode retired-account reset state: %w", err)
	}
	if err := validateRetiredEquityAccountsState(state, revision); err != nil {
		return retiredEquityResetRecord{}, false, err
	}
	if state.PendingReset != nil {
		pending := *state.PendingReset
		if pending.Actor != actor || pending.TargetScope != targetScope {
			return retiredEquityResetRecord{}, false, fmt.Errorf("another retired-account reset operation is pending")
		}
		return pending, false, nil
	}
	if len(state.Accounts) == 0 {
		if len(state.ResetHistory) > 0 {
			last := state.ResetHistory[len(state.ResetHistory)-1]
			if last.Actor == actor && last.TargetScope == targetScope {
				return last, true, nil
			}
		}
		return retiredEquityResetRecord{}, false, fmt.Errorf("no retired accounts are awaiting explicit reset")
	}
	ids := make([]string, 0, len(state.Accounts))
	for _, account := range state.Accounts {
		if account.Status != retiredEquityAccountReadyForReset || account.FlatEvidenceCount != retiredEquityFlatEvidenceRequired {
			return retiredEquityResetRecord{}, false, fmt.Errorf("retired account %s has not met the complete flat-evidence threshold", account.ID)
		}
		ids = append(ids, account.ID)
	}
	sort.Strings(ids)
	var rawID [16]byte
	if _, err := rand.Read(rawID[:]); err != nil {
		return retiredEquityResetRecord{}, false, fmt.Errorf("create retired-account reset identity: %w", err)
	}
	operation := retiredEquityResetRecord{ID: hex.EncodeToString(rawID[:]), Actor: actor, TargetScope: targetScope,
		StartedAt: now.UTC(), RetiredAccountIDs: ids}
	state.PendingReset = &operation
	state.Revision = revision + 1
	encoded, err := json.Marshal(state)
	if err != nil {
		return retiredEquityResetRecord{}, false, fmt.Errorf("encode retired-account reset intent: %w", err)
	}
	if err := s.backend.SaveRiskCheckpoint(ctx, retiredEquityAccountsCheckpointKey, revision, encoded); err != nil {
		return retiredEquityResetRecord{}, false, fmt.Errorf("persist retired-account reset intent: %w", err)
	}
	return operation, false, nil
}

func (s *retiredEquityAccountsStore) completeReset(ctx context.Context, operationID string, baselineRevision int64, now time.Time) (retiredEquityResetRecord, error) {
	if s == nil || s.backend == nil || ctx == nil || strings.TrimSpace(operationID) == "" || baselineRevision < 1 || now.IsZero() {
		return retiredEquityResetRecord{}, fmt.Errorf("invalid retired-account reset completion")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, revision, err := s.backend.LoadRiskCheckpoint(ctx, retiredEquityAccountsCheckpointKey)
	if err != nil {
		return retiredEquityResetRecord{}, fmt.Errorf("load retired-account reset completion state: %w", err)
	}
	if payload == nil || revision < 1 {
		return retiredEquityResetRecord{}, fmt.Errorf("retired-account reset intent is absent")
	}
	var state retiredEquityAccountsState
	if err := json.Unmarshal(payload, &state); err != nil {
		return retiredEquityResetRecord{}, fmt.Errorf("decode retired-account reset completion state: %w", err)
	}
	if err := validateRetiredEquityAccountsState(state, revision); err != nil {
		return retiredEquityResetRecord{}, err
	}
	if state.PendingReset == nil || state.PendingReset.ID != operationID {
		return retiredEquityResetRecord{}, fmt.Errorf("retired-account reset intent does not match completion")
	}
	readyIDs := make([]string, 0, len(state.Accounts))
	for _, account := range state.Accounts {
		if account.Status != retiredEquityAccountReadyForReset || account.FlatEvidenceCount != retiredEquityFlatEvidenceRequired {
			return retiredEquityResetRecord{}, fmt.Errorf("retired account %s lost its complete flat evidence before reset completion", account.ID)
		}
		readyIDs = append(readyIDs, account.ID)
	}
	sort.Strings(readyIDs)
	if strings.Join(readyIDs, ",") != strings.Join(state.PendingReset.RetiredAccountIDs, ",") {
		return retiredEquityResetRecord{}, fmt.Errorf("retired account membership changed during reset")
	}
	completed := *state.PendingReset
	completed.CompletedAt = now.UTC()
	completed.BaselineRevision = baselineRevision
	state.ResetHistory = append(state.ResetHistory, completed)
	state.PendingReset = nil
	state.Accounts = []retiredEquityAccountRecord{}
	state.Revision = revision + 1
	encoded, err := json.Marshal(state)
	if err != nil {
		return retiredEquityResetRecord{}, fmt.Errorf("encode retired-account reset audit: %w", err)
	}
	if err := s.backend.SaveRiskCheckpoint(ctx, retiredEquityAccountsCheckpointKey, revision, encoded); err != nil {
		return retiredEquityResetRecord{}, fmt.Errorf("persist retired-account reset audit: %w", err)
	}
	return completed, nil
}

func validateRetiredEquityAccountsState(state retiredEquityAccountsState, revision int64) error {
	if state.Version != 1 || state.Revision != revision || revision < 1 || state.Accounts == nil {
		return fmt.Errorf("retired account archive identity or revision is invalid")
	}
	seen := make(map[string]struct{}, len(state.Accounts))
	for _, account := range state.Accounts {
		if err := validateRetiredEquityAccount(account); err != nil {
			return fmt.Errorf("retired account archive contains an invalid record")
		}
		if _, duplicate := seen[account.ID]; duplicate {
			return fmt.Errorf("retired account archive contains duplicate records")
		}
		seen[account.ID] = struct{}{}
	}
	if state.PendingReset != nil {
		if err := validateRetiredEquityResetRecord(*state.PendingReset, false); err != nil {
			return err
		}
	}
	resetIDs := make(map[string]struct{}, len(state.ResetHistory))
	for _, completed := range state.ResetHistory {
		if err := validateRetiredEquityResetRecord(completed, true); err != nil {
			return err
		}
		if _, duplicate := resetIDs[completed.ID]; duplicate {
			return fmt.Errorf("retired-account reset history contains duplicate operations")
		}
		resetIDs[completed.ID] = struct{}{}
	}
	if state.PendingReset != nil {
		if _, duplicate := resetIDs[state.PendingReset.ID]; duplicate {
			return fmt.Errorf("pending retired-account reset duplicates completed audit")
		}
	}
	return nil
}

func validateRetiredEquityResetRecord(record retiredEquityResetRecord, completed bool) error {
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.Actor) == "" || strings.TrimSpace(record.TargetScope) == "" ||
		record.StartedAt.IsZero() || len(record.RetiredAccountIDs) == 0 {
		return fmt.Errorf("retired-account reset audit identity is invalid")
	}
	hasCompletedAt, hasBaselineRevision := !record.CompletedAt.IsZero(), record.BaselineRevision > 0
	if hasCompletedAt != hasBaselineRevision || completed != hasCompletedAt || (completed && record.CompletedAt.Before(record.StartedAt)) {
		return fmt.Errorf("retired-account reset audit completion is inconsistent")
	}
	seen := make(map[string]struct{}, len(record.RetiredAccountIDs))
	for _, id := range record.RetiredAccountIDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("retired-account reset audit contains an empty account identity")
		}
		if _, duplicate := seen[id]; duplicate {
			return fmt.Errorf("retired-account reset audit contains duplicate account identities")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateRetiredEquityAccount(record retiredEquityAccountRecord) error {
	if record.ID == "" || record.CredentialsCiphertext == "" || record.RetiredAt.IsZero() ||
		(record.Status != retiredEquityAccountPendingVerification && record.Status != retiredEquityAccountReadyForReset) ||
		record.FlatEvidenceCount < 0 || record.FlatEvidenceCount > retiredEquityFlatEvidenceRequired {
		return fmt.Errorf("invalid retired account identity or evidence state")
	}
	if record.FlatEvidenceCount == 0 && !record.LastFlatAt.IsZero() {
		return fmt.Errorf("zero-count retired account has a flat evidence timestamp")
	}
	if record.FlatEvidenceCount > 0 && record.LastFlatAt.IsZero() {
		return fmt.Errorf("retired account flat evidence is missing its timestamp")
	}
	if !record.LastObservedAt.IsZero() && record.LastObservedAt.Before(record.RetiredAt) {
		return fmt.Errorf("retired account evidence predates retirement")
	}
	if !record.LastFlatAt.IsZero() && record.LastObservedAt.Before(record.LastFlatAt) {
		return fmt.Errorf("retired account flat evidence is newer than its last observation")
	}
	if record.FlatEvidenceCount > 0 && (record.LastEvidenceResult != "flat" || record.LastObservedAt.IsZero()) {
		return fmt.Errorf("retired account flat evidence is inconsistent")
	}
	if record.LastEvidenceResult != "" && record.LastEvidenceResult != "flat" &&
		record.LastEvidenceResult != "incomplete" && record.LastEvidenceResult != "open_exposure" {
		return fmt.Errorf("retired account evidence result is invalid")
	}
	if record.Status == retiredEquityAccountReadyForReset && record.FlatEvidenceCount != retiredEquityFlatEvidenceRequired {
		return fmt.Errorf("retired account readiness lacks the required flat evidence")
	}
	if record.Status == retiredEquityAccountPendingVerification && record.FlatEvidenceCount >= retiredEquityFlatEvidenceRequired {
		return fmt.Errorf("retired account pending state conflicts with its flat evidence")
	}
	return nil
}

func removedEquityAccounts(previous, next equityScopeSnapshot) []equityAccountEvidenceConfig {
	current := make(map[string]struct{}, len(next.accounts))
	if next.err == "" {
		for _, account := range next.accounts {
			current[retiredEquityAccountID(account)] = struct{}{}
		}
	}
	removed := make([]equityAccountEvidenceConfig, 0)
	for _, account := range previous.accounts {
		if _, ok := current[retiredEquityAccountID(account)]; !ok {
			removed = append(removed, account)
		}
	}
	sort.Slice(removed, func(i, j int) bool { return retiredEquityAccountID(removed[i]) < retiredEquityAccountID(removed[j]) })
	return removed
}

func retiredEquityAccountID(account equityAccountEvidenceConfig) string {
	identity, _ := json.Marshal([]string{account.Exchange, account.MarketType, account.Scope, account.credentialVersion()})
	digest := sha256.Sum256(identity)
	return fmt.Sprintf("%x", digest[:])
}

type retiredEquityFuturesObserverFactory func(string, config.ExchangeConfig) (exchange.AccountFuturesFlatnessObserver, error)

// verifyRetiredEquityAccountsOnce uses read-only exchange adapters and records
// unavailable/unsupported account scopes as incomplete rather than flat.
func (bm *BotManager) verifyRetiredEquityAccountsOnce(ctx context.Context, newObserver retiredEquityFuturesObserverFactory) error {
	if bm == nil || ctx == nil || bm.retiredEquityAccounts == nil {
		return fmt.Errorf("retired account verification is unavailable")
	}
	accounts, _, err := bm.retiredEquityAccounts.load(ctx)
	if err != nil {
		return fmt.Errorf("load retired accounts for read-only verification: %w", err)
	}
	var failedScopes []string
	for _, account := range accounts {
		if err := ctx.Err(); err != nil {
			return err
		}
		observedAt := time.Now().UTC()
		complete, flat := false, false
		if strings.EqualFold(account.MarketType, "futures") && newObserver != nil {
			credentials := config.ExchangeConfig{APIKey: account.credentials.APIKey, SecretKey: account.credentials.SecretKey,
				Passphrase: account.credentials.Passphrase, Testnet: account.credentials.Testnet}
			observer, createErr := newObserver(account.Exchange, credentials)
			if createErr == nil && observer != nil {
				queryCtx, cancel := context.WithTimeout(ctx, retiredEquityVerificationTimeout)
				complete, flat, observedAt, err = observer.ObserveAccountFuturesFlatness(queryCtx)
				cancel()
			} else if createErr != nil {
				err = createErr
			} else {
				err = fmt.Errorf("Futures observer factory returned nil")
			}
		} else {
			err = fmt.Errorf("read-only zero-exposure verifier is unsupported for %s/%s", account.Exchange, account.MarketType)
		}
		if err != nil {
			complete, flat = false, false
			failedScopes = append(failedScopes, account.Exchange+"/"+account.MarketType)
		}
		if observedAt.IsZero() {
			observedAt = time.Now().UTC()
		}
		if persistErr := bm.retiredEquityAccounts.recordEvidence(ctx, account.ID, complete, flat, observedAt); persistErr != nil {
			return fmt.Errorf("persist retired-account verification evidence: %w", persistErr)
		}
	}
	if len(failedScopes) > 0 {
		return fmt.Errorf("retired-account read-only verification incomplete for %s", strings.Join(failedScopes, ","))
	}
	return nil
}

func (bm *BotManager) StartRetiredEquityAccountVerificationLoop(ctx context.Context, newObserver retiredEquityFuturesObserverFactory) {
	if bm == nil || ctx == nil || bm.retiredEquityAccounts == nil {
		return
	}
	go func() {
		verify := func() {
			err := bm.verifyRetiredEquityAccountsOnce(ctx, newObserver)
			if err != nil && ctx.Err() == nil {
				logger.Warn("retired-account read-only verification remains unresolved: %v", err)
			}
		}
		verify()
		ticker := time.NewTicker(retiredEquityVerificationInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				verify()
			}
		}
	}()
}
