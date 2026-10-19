package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	sqliteDriver "github.com/mattn/go-sqlite3"
	"quantmesh/accounting"
)

//go:embed migrations/20261012*_funding_recovery_*.sql
var fundingRecoveryMigrations embed.FS

const fundingRecoveryMySQLLockName = "quantmesh:funding_recovery_schema_v1"

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

type FundingRecoveryStore interface {
	CreateRecoveryEpisode(context.Context, FundingCarryRuntimeGeneration, accounting.RecoveryEpisode) (accounting.RecoveryEpisode, error)
	CreateRecoveryEpisodeForIntent(context.Context, FundingCarryRuntimeGeneration, accounting.RecoveryOriginIntent, accounting.RecoveryEpisode) (accounting.RecoveryEpisode, bool, error)
	GetRecoveryEpisode(context.Context, string) (accounting.RecoveryEpisode, bool, error)
	ApplyRecoveryOperation(context.Context, FundingCarryRuntimeGeneration, accounting.RecoveryOperation) (accounting.RecoveryReceipt, bool, error)
	GetRecoveryReceipt(context.Context, string, string) (accounting.RecoveryReceipt, bool, error)
	GetRecoveryOutbox(context.Context, string, string) (accounting.RecoveryOutbox, bool, error)
	ListPendingRecoveryOutbox(context.Context) ([]accounting.RecoveryOutbox, error)
	ListRecoveryOutboxes(context.Context, string) ([]accounting.RecoveryOutbox, error)
	ListRecoveryChildren(context.Context, string) ([]accounting.RecoveryChild, error)
	ListRecoveryEvents(context.Context, string) ([]accounting.RecoveryEvent, error)
	GetRecoverySourceCursor(context.Context, accounting.RecoverySourceIdentity) (accounting.RecoverySourceCursor, bool, error)
	RecordRecoverySourceAnomaly(context.Context, FundingCarryRuntimeGeneration, accounting.RecoverySourceAnomaly) (accounting.RecoverySourceAnomaly, bool, error)
	GetRecoverySourceAnomaly(context.Context, string, string) (accounting.RecoverySourceAnomaly, bool, error)
	ListRecoverySourceAnomalies(context.Context, string) ([]accounting.RecoverySourceAnomaly, error)
	MarkRecoveryProjected(context.Context, FundingCarryRuntimeGeneration, accounting.RecoveryOutbox) error
	ListActiveRecoveryHolds(context.Context) ([]accounting.RecoveryHold, error)
	GetRecoveryHold(context.Context, string, uint64) (accounting.RecoveryHold, bool, error)
	GetRecoverySubmissionAttempt(context.Context, string, string) (accounting.RecoverySubmissionAttempt, bool, error)
	ListRecoverySubmissionAttempts(context.Context, string) ([]accounting.RecoverySubmissionAttempt, error)
	ReadRecoverySubmissionAdjudication(context.Context, accounting.RecoverySubmissionAdjudication) (accounting.RecoverySubmissionAttempt, bool, error)
	AcknowledgeRecoverySubmission(context.Context, FundingCarryRuntimeGeneration, accounting.OneShotPermit, string) error
	AdjudicateRecoverySubmission(context.Context, FundingCarryRuntimeGeneration, accounting.RecoverySubmissionAdjudication) (accounting.RecoverySubmissionAttempt, bool, error)
	TakeOverRecoveryEpisode(context.Context, FundingCarryRuntimeGeneration, accounting.RecoveryOwnerTakeoverAuthorization) (accounting.RecoveryEpisode, bool, error)
	CreateRecoveryPermit(context.Context, FundingCarryRuntimeGeneration, accounting.OneShotPermit) (accounting.OneShotPermit, error)
	// ConsumeRecoveryPermit returns maySubmit=true only to the caller that
	// committed the one-row authorized->consumed CAS. Readback of an already
	// consumed permit is never permission to issue a physical RPC.
	ConsumeRecoveryPermit(context.Context, FundingCarryRuntimeGeneration, accounting.OneShotPermit) (permit accounting.OneShotPermit, maySubmit bool, err error)
	RevokeRecoveryPermit(context.Context, FundingCarryRuntimeGeneration, string, string) error
	PrepareRecoveryRelease(context.Context, FundingCarryRuntimeGeneration, accounting.RecoveryReleaseAuthorization) (accounting.RecoveryEpisode, error)
	ReleaseRecoveryHold(context.Context, FundingCarryRuntimeGeneration, accounting.RecoveryReleaseAuthorization) (accounting.RecoveryHold, error)
	ReopenRecoveryEpisode(context.Context, FundingCarryRuntimeGeneration, string, uint64, accounting.RecoveryEvent, string) (accounting.RecoveryHold, error)
}

// RecoverySourceWindowVerifier validates the trusted source service's
// completion attestation and binds it to the exact rows passed to storage.
// Implementations must not perform exchange or other network calls. A missing
// verifier keeps debt/funding cursor advancement fail-closed.
type RecoverySourceWindowVerifier interface {
	VerifyRecoverySourceWindow(context.Context, accounting.RecoverySourceWindow, []accounting.RecoveryEvent) error
}

var ErrFundingRecoveryUnavailable = errors.New("funding recovery storage unavailable")
var ErrFundingRecoveryNotFound = errors.New("funding recovery record not found")
var ErrFundingRecoveryGenerationScopeMismatch = errors.New("funding recovery owner scopes do not match runtime generation")
var ErrRecoverySourceWindowVerifierUnavailable = errors.New("trusted recovery source window verifier unavailable")

// SetRecoverySourceWindowVerifier installs the trusted source completion
// verifier once during composition. Replacing it at runtime is rejected.
func (s *SQLStorage) SetRecoverySourceWindowVerifier(verifier RecoverySourceWindowVerifier) error {
	if s == nil || verifier == nil {
		return ErrRecoverySourceWindowVerifierUnavailable
	}
	s.recoverySourceVerifierMu.Lock()
	defer s.recoverySourceVerifierMu.Unlock()
	if s.recoverySourceWindowVerifier != nil {
		return errors.New("recovery source window verifier already configured")
	}
	s.recoverySourceWindowVerifier = verifier
	return nil
}

func (s *SQLStorage) recoverySourceVerifier() RecoverySourceWindowVerifier {
	if s == nil {
		return nil
	}
	s.recoverySourceVerifierMu.RLock()
	defer s.recoverySourceVerifierMu.RUnlock()
	return s.recoverySourceWindowVerifier
}

func (s *SQLStorage) lockFundingRecoveryGenerationForScopes(ctx context.Context, tx *sql.Tx, generation FundingCarryRuntimeGeneration, scopes []string) error {
	if err := s.lockFundingCarryRuntimeGeneration(ctx, tx, generation); err != nil {
		return err
	}
	keys, err := normalizeFundingCarryGenerationScopes(scopes)
	if err != nil {
		return fmt.Errorf("validate funding recovery account scopes: %w", err)
	}
	if len(keys) != len(generation.scopeKeys) {
		return ErrFundingRecoveryGenerationScopeMismatch
	}
	for i := range keys {
		if keys[i] != generation.scopeKeys[i] {
			return ErrFundingRecoveryGenerationScopeMismatch
		}
	}
	return nil
}

func (s *SQLStorage) lockFundingRecoveryGenerationForEpisode(ctx context.Context, tx *sql.Tx, generation FundingCarryRuntimeGeneration, episodeID string) (accounting.RecoveryEpisode, error) {
	if err := s.lockFundingCarryRuntimeGeneration(ctx, tx, generation); err != nil {
		return accounting.RecoveryEpisode{}, err
	}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=?`, episodeID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryEpisode{}, accounting.ErrRecoveryConflict
	}
	if err != nil {
		return accounting.RecoveryEpisode{}, fmt.Errorf("read funding recovery episode while fencing owner: %w", err)
	}
	var episode accounting.RecoveryEpisode
	if err := json.Unmarshal(raw, &episode); err != nil {
		return episode, err
	}
	keys, err := normalizeFundingCarryGenerationScopes(episode.AccountScopes)
	if err != nil {
		return episode, fmt.Errorf("validate stored funding recovery account scopes: %w", err)
	}
	if len(keys) != len(generation.scopeKeys) {
		return episode, ErrFundingRecoveryGenerationScopeMismatch
	}
	for i := range keys {
		if keys[i] != generation.scopeKeys[i] {
			return episode, ErrFundingRecoveryGenerationScopeMismatch
		}
	}
	if episode.OwnerGeneration != generation.ID() {
		return episode, accounting.ErrRecoveryConflict
	}
	return episode, nil
}

func (s *SQLStorage) MigrateFundingRecovery(ctx context.Context) error {
	if s == nil || s.db == nil || (s.dbType != "sqlite" && s.dbType != "mysql") {
		return errors.New("unsupported funding recovery database")
	}
	var migrationBodies [][]byte
	for _, version := range []string{"2026101201", "2026101202", "2026101203", "2026101204", "2026101205"} {
		path := "migrations/" + version + "_funding_recovery_" + s.dbType + ".up.sql"
		body, err := fundingRecoveryMigrations.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read funding recovery migration %s: %w", version, err)
		}
		migrationBodies = append(migrationBodies, body)
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open funding recovery migration connection: %w", err)
	}
	defer conn.Close()
	if s.dbType == "mysql" {
		var acquired sql.NullInt64
		if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, ?)`, fundingRecoveryMySQLLockName, orderReconciliationLockTimeout).Scan(&acquired); err != nil {
			return fmt.Errorf("lock funding recovery migration: %w", err)
		}
		if !acquired.Valid || acquired.Int64 != 1 {
			return errors.New("lock funding recovery migration: timeout")
		}
		var migrationErr error
		for i, body := range migrationBodies {
			if i == 3 {
				exists, existsErr := fundingRecoveryCursorSymbolColumnExists(ctx, conn, s.dbType)
				if existsErr != nil {
					migrationErr = fmt.Errorf("inspect funding recovery cursor schema: %w", existsErr)
					break
				}
				if exists {
					continue
				}
			}
			for _, statement := range strings.Split(string(body), ";") {
				statement = strings.TrimSpace(statement)
				if statement == "" {
					continue
				}
				if _, migrationErr = conn.ExecContext(ctx, statement); migrationErr != nil {
					migrationErr = fmt.Errorf("apply funding recovery migration %d: %w", i+1201, migrationErr)
					break
				}
			}
			if migrationErr != nil {
				break
			}
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var released sql.NullInt64
		releaseErr := conn.QueryRowContext(releaseCtx, `SELECT RELEASE_LOCK(?)`, fundingRecoveryMySQLLockName).Scan(&released)
		if releaseErr != nil || !released.Valid || released.Int64 != 1 {
			if releaseErr == nil {
				releaseErr = errors.New("named lock ownership lost")
			}
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			if migrationErr != nil {
				return errors.Join(migrationErr, fmt.Errorf("release migration lock: %w", releaseErr))
			}
			return fmt.Errorf("release funding recovery migration lock: %w", releaseErr)
		}
		return migrationErr
	}
	if _, err := conn.ExecContext(ctx, `PRAGMA busy_timeout=100`); err != nil {
		return err
	}
	lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		if _, err := conn.ExecContext(lockCtx, `BEGIN EXCLUSIVE`); err == nil {
			break
		} else if !isSQLiteReconciliationLockBusy(err) {
			return fmt.Errorf("lock SQLite funding recovery migration: %w", err)
		} else if lockCtx.Err() != nil {
			return fmt.Errorf("lock SQLite funding recovery migration: %w", lockCtx.Err())
		}
		t := time.NewTimer(25 * time.Millisecond)
		select {
		case <-lockCtx.Done():
			t.Stop()
			return lockCtx.Err()
		case <-t.C:
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	for i, body := range migrationBodies {
		if i == 3 {
			exists, existsErr := fundingRecoveryCursorSymbolColumnExists(ctx, conn, s.dbType)
			if existsErr != nil {
				return fmt.Errorf("inspect funding recovery cursor schema: %w", existsErr)
			}
			if exists {
				continue
			}
		}
		for _, statement := range strings.Split(string(body), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err := conn.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply funding recovery migration %d: %w", i+1201, err)
			}
		}
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit funding recovery migration: %w", err)
	}
	committed = true
	return nil
}

func fundingRecoveryCursorSymbolColumnExists(ctx context.Context, conn *sql.Conn, dialect string) (bool, error) {
	if dialect == "mysql" {
		var count int
		err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='funding_recovery_source_cursors' AND column_name='symbol'`).Scan(&count)
		return count == 1, err
	}
	rows, err := conn.QueryContext(ctx, `PRAGMA table_info(funding_recovery_source_cursors)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if name == "symbol" {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *SQLStorage) CreateRecoveryEpisode(ctx context.Context, generation FundingCarryRuntimeGeneration, episode accounting.RecoveryEpisode) (accounting.RecoveryEpisode, error) {
	created, _, err := s.createRecoveryEpisode(ctx, generation, nil, episode)
	return created, err
}

// CreateRecoveryEpisodeForIntent atomically binds one immutable owner-scoped
// execution intent to one accounting-only held episode. A generation change
// cannot create a second business episode; it must use explicit takeover.
func (s *SQLStorage) CreateRecoveryEpisodeForIntent(ctx context.Context, generation FundingCarryRuntimeGeneration, origin accounting.RecoveryOriginIntent, episode accounting.RecoveryEpisode) (accounting.RecoveryEpisode, bool, error) {
	key, err := origin.Key()
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	if !sameStrings(origin.OwnerScopes, generation.ScopeKeys()) || !sameStrings(episode.AccountScopes, origin.OwnerScopes) {
		return accounting.RecoveryEpisode{}, false, ErrFundingRecoveryGenerationScopeMismatch
	}
	if episode.Bot != origin.Bot || episode.Strategy != origin.Strategy ||
		episode.State != accounting.RecoveryBlocked || episode.RecoveryGeneration != 1 || len(episode.ActionPlan) != 0 {
		return accounting.RecoveryEpisode{}, false, errors.New("origin intent episode must be exact-scope, blocked, generation one, and have no replay actions")
	}
	created, didCreate, err := s.createRecoveryEpisode(ctx, generation, &fundingRecoveryOriginBinding{key: key, identity: origin}, episode)
	return created, didCreate, err
}

type fundingRecoveryOriginBinding struct {
	key      string
	identity accounting.RecoveryOriginIntent
}

func (s *SQLStorage) createRecoveryEpisode(ctx context.Context, generation FundingCarryRuntimeGeneration, origin *fundingRecoveryOriginBinding, episode accounting.RecoveryEpisode) (accounting.RecoveryEpisode, bool, error) {
	if err := episode.Validate(); err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	if episode.OwnerGeneration != generation.ID() {
		return accounting.RecoveryEpisode{}, false, ErrFundingCarryRuntimeGenerationLost
	}
	if episode.State != accounting.RecoveryPrepared && episode.State != accounting.RecoveryReopened && episode.State != accounting.RecoveryProjected {
		if origin == nil || episode.State != accounting.RecoveryBlocked {
			return accounting.RecoveryEpisode{}, false, errors.New("episode must start held")
		}
	}
	now := time.Now().UTC()
	episode.CreatedAt = now
	episode.UpdatedAt = now
	payload, err := json.Marshal(episode)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	hold := recoveryHoldForEpisode(episode, "create")
	holdJSON, err := json.Marshal(hold)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	defer tx.Rollback()
	if err = s.lockFundingRecoveryGenerationForScopes(ctx, tx, generation, episode.AccountScopes); err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	if origin != nil {
		var existingEpisodeID string
		var existingOriginPayload []byte
		originErr := tx.QueryRowContext(ctx, `SELECT episode_id,payload_json FROM funding_recovery_origins WHERE origin_key=?`, origin.key).Scan(&existingEpisodeID, &existingOriginPayload)
		if originErr == nil {
			matches, compareErr := recoveryOriginPayloadMatches(existingOriginPayload, origin.identity)
			if compareErr != nil {
				return accounting.RecoveryEpisode{}, false, fmt.Errorf("decode recovery origin intent binding: %w", compareErr)
			}
			if !matches {
				return accounting.RecoveryEpisode{}, false, accounting.ErrRecoveryConflict
			}
			var existing accounting.RecoveryEpisode
			var existingRaw []byte
			if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=?`, existingEpisodeID).Scan(&existingRaw); err != nil {
				return accounting.RecoveryEpisode{}, false, fmt.Errorf("read episode bound to recovery origin intent: %w", err)
			}
			if err = json.Unmarshal(existingRaw, &existing); err != nil {
				return accounting.RecoveryEpisode{}, false, err
			}
			if existing.OwnerGeneration != generation.ID() || existing.Bot != origin.identity.Bot || existing.Strategy != origin.identity.Strategy {
				return accounting.RecoveryEpisode{}, false, accounting.ErrRecoveryConflict
			}
			expectedChild := recoveryOriginChild(origin, existing)
			var childRaw []byte
			if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_children WHERE episode_id=? AND child_id=?`, existing.ID, expectedChild.ID).Scan(&childRaw); err != nil {
				return accounting.RecoveryEpisode{}, false, fmt.Errorf("read child bound to recovery origin intent: %w", err)
			}
			var child accounting.RecoveryChild
			if err = json.Unmarshal(childRaw, &child); err != nil {
				return accounting.RecoveryEpisode{}, false, err
			}
			if err = child.Validate(); err != nil || !sameRecoveryChildOwner(child, expectedChild) || child.Side != expectedChild.Side || child.OrderIntentHash != expectedChild.OrderIntentHash || child.SourceIntentHash != expectedChild.SourceIntentHash {
				return accounting.RecoveryEpisode{}, false, accounting.ErrRecoveryConflict
			}
			return existing, false, nil
		}
		if !errors.Is(originErr, sql.ErrNoRows) {
			return accounting.RecoveryEpisode{}, false, fmt.Errorf("read recovery origin intent binding: %w", originErr)
		}
	}
	var existingRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=?`, episode.ID).Scan(&existingRaw)
	if err == nil {
		var existing accounting.RecoveryEpisode
		if err = json.Unmarshal(existingRaw, &existing); err != nil {
			return accounting.RecoveryEpisode{}, false, err
		}
		requested := episode
		requested.CreatedAt, requested.UpdatedAt = existing.CreatedAt, existing.UpdatedAt
		left, _ := json.Marshal(existing)
		right, _ := json.Marshal(requested)
		if string(left) == string(right) {
			return existing, false, nil
		}
		return accounting.RecoveryEpisode{}, false, accounting.ErrRecoveryConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryEpisode{}, false, err
	}
	if origin != nil {
		originPayload, marshalErr := json.Marshal(origin.identity)
		if marshalErr != nil {
			return accounting.RecoveryEpisode{}, false, marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_origins(origin_key,episode_id,payload_json) VALUES(?,?,?)`, origin.key, episode.ID, originPayload); err != nil {
			return accounting.RecoveryEpisode{}, false, recoveryConflict(err)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_episodes(episode_id,revision,recovery_generation,state,bot_id,strategy,payload_json) VALUES(?,?,?,?,?,?,?)`, episode.ID, episode.Revision, episode.RecoveryGeneration, episode.State, episode.Bot, episode.Strategy, payload)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, recoveryConflict(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_holds(hold_id,episode_id,recovery_generation,hold_revision,state,payload_json) VALUES(?,?,?,?,?,?)`, hold.ID, hold.EpisodeID, hold.Generation, hold.Revision, hold.State, holdJSON)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, recoveryConflict(err)
	}
	if origin != nil {
		child := recoveryOriginChild(origin, episode)
		childJSON, marshalErr := json.Marshal(child)
		if marshalErr != nil {
			return accounting.RecoveryEpisode{}, false, marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_children(episode_id,child_id,revision,payload_json) VALUES(?,?,?,?)`, child.EpisodeID, child.ID, child.Revision, childJSON); err != nil {
			return accounting.RecoveryEpisode{}, false, recoveryConflict(err)
		}
	}
	for _, action := range episode.ActionPlan {
		b, _ := json.Marshal(action)
		if _, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_actions(episode_id,action_id,ordinal,revision,payload_json) VALUES(?,?,?,1,?)`, episode.ID, action.ID, action.Ordinal, b); err != nil {
			return accounting.RecoveryEpisode{}, false, recoveryConflict(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return accounting.RecoveryEpisode{}, false, fmt.Errorf("commit recovery episode and initial hold: %w", err)
	}
	return episode, true, nil
}

func recoveryOriginPayloadMatches(payload []byte, expected accounting.RecoveryOriginIntent) (bool, error) {
	var stored accounting.RecoveryOriginIntent
	if err := json.Unmarshal(payload, &stored); err != nil {
		return false, err
	}
	return reflect.DeepEqual(stored, expected), nil
}

func recoveryOriginChild(origin *fundingRecoveryOriginBinding, episode accounting.RecoveryEpisode) accounting.RecoveryChild {
	return accounting.RecoveryChild{
		ID: "origin-" + origin.key[:40], EpisodeID: episode.ID, Role: "execution_origin",
		Exchange: origin.identity.Exchange, Market: origin.identity.Market, AccountScope: origin.identity.AccountScope,
		Bot: origin.identity.Bot, Symbol: origin.identity.Symbol, StrategyName: origin.identity.Strategy,
		StrategyType: "funding_carry", ClientOrderID: origin.identity.ClientOrderID, Side: origin.identity.Side,
		SourceIntentHash: origin.identity.IntentHash, Revision: 1, State: accounting.ChildUnknown, UpdatedAt: episode.CreatedAt,
	}
}

func recoveryHoldForEpisode(e accounting.RecoveryEpisode, operation string) accounting.RecoveryHold {
	return accounting.RecoveryHold{ID: fmt.Sprintf("%s:%d", e.ID, e.RecoveryGeneration), EpisodeID: e.ID, Bot: e.Bot, Strategy: e.Strategy, AccountScopes: append([]string(nil), e.AccountScopes...), Generation: e.RecoveryGeneration, Revision: 1, Source: fmt.Sprintf("recovery_episode:%s:%d", e.ID, e.RecoveryGeneration), State: "held", CreatedByOperation: operation, UpdatedAt: time.Now().UTC()}
}

func (s *SQLStorage) GetRecoveryEpisode(ctx context.Context, id string) (accounting.RecoveryEpisode, bool, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryEpisode{}, false, nil
	}
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	var out accounting.RecoveryEpisode
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, false, fmt.Errorf("decode recovery episode: %w", err)
	}
	return out, true, nil
}

func (s *SQLStorage) GetRecoveryReceipt(ctx context.Context, episodeID, operationID string) (accounting.RecoveryReceipt, bool, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=?`, episodeID, operationID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryReceipt{}, false, nil
	}
	if err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	var out accounting.RecoveryReceipt
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, false, err
	}
	return out, true, nil
}

func (s *SQLStorage) ApplyRecoveryOperation(ctx context.Context, generation FundingCarryRuntimeGeneration, op accounting.RecoveryOperation) (accounting.RecoveryReceipt, bool, error) {
	hash, err := op.CanonicalHash()
	if err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	if op.RequestHash != "" && op.RequestHash != hash {
		return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryOperationConflict
	}
	op.RequestHash = hash
	for _, child := range op.Children {
		if err := child.Validate(); err != nil {
			return accounting.RecoveryReceipt{}, false, err
		}
		if child.EpisodeID != op.EpisodeID {
			return accounting.RecoveryReceipt{}, false, errors.New("child episode mismatch")
		}
	}
	for _, event := range op.Events {
		if err := event.Validate(); err != nil {
			return accounting.RecoveryReceipt{}, false, err
		}
		if event.EpisodeID != op.EpisodeID {
			return accounting.RecoveryReceipt{}, false, errors.New("event episode mismatch")
		}
		if event.Kind == accounting.RecoveryOpeningBalanceEvent &&
			(event.OpeningBalance.Actor != op.Actor || event.OpeningBalance.AuditRef != op.AuditRef ||
				event.OpeningBalance.SnapshotHash != event.SourceHash || op.EvidenceHash != event.SourceHash) {
			return accounting.RecoveryReceipt{}, false, errors.New("opening balance operator, audit reference, or snapshot digest differs from operation")
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	defer tx.Rollback()
	if _, err = s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, op.EpisodeID); err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	var oldHash string
	var oldReceipt []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=?`, op.EpisodeID, op.OperationID).Scan(&oldHash, &oldReceipt)
	if err == nil {
		if oldHash != hash {
			return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryOperationConflict
		}
		var receipt accounting.RecoveryReceipt
		if err = json.Unmarshal(oldReceipt, &receipt); err != nil {
			return receipt, false, err
		}
		return receipt, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryReceipt{}, false, err
	}
	var episodeRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=? AND revision=?`, op.EpisodeID, op.ExpectedEpisodeRevision).Scan(&episodeRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
	}
	if err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	var episode accounting.RecoveryEpisode
	if err = json.Unmarshal(episodeRaw, &episode); err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	// A projected operation is a completed cursor checkpoint, not a terminal
	// episode. Permit the next CAS operation while preserving the prior receipt.
	if episode.OwnerGeneration != generation.ID() {
		return accounting.RecoveryReceipt{}, false, ErrFundingCarryRuntimeGenerationLost
	}
	stateAllowsAccounting := episode.State == accounting.RecoveryPrepared || episode.State == accounting.RecoveryReopened || episode.State == accounting.RecoveryProjected
	stateAllowsTakeoverAdjudication := episode.State == accounting.RecoveryBlocked && episode.HasCurrentOwnerTakeover()
	if !stateAllowsAccounting && !stateAllowsTakeoverAdjudication {
		return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
	}
	// Applying the ledger and later committing its runtime projection each
	// advance the episode revision. Reserve room for both increments before any
	// event or cursor write, so an accepted operation can always be projected.
	if op.ExpectedEpisodeRevision >= accounting.MaxDurableRecoveryRevision-1 {
		return accounting.RecoveryReceipt{}, false, errors.New("recovery episode revision exhausted before ledger commit")
	}
	debtEventCount := 0
	for _, event := range op.Events {
		if isRecoverySourceCursorEvent(event) {
			debtEventCount++
		}
	}
	if debtEventCount > 0 && op.SourceWindow == nil {
		return accounting.RecoveryReceipt{}, false, errors.New("debt source events require a complete source window")
	}
	var cursorKey string
	var oldCursor accounting.RecoverySourceCursor
	if op.SourceWindow != nil {
		window := *op.SourceWindow
		if err := window.Validate(); err != nil {
			return accounting.RecoveryReceipt{}, false, err
		}
		if !containsString(episode.AccountScopes, window.AccountScope) {
			return accounting.RecoveryReceipt{}, false, errors.New("source cursor scope is outside episode")
		}
		if uint64(debtEventCount) != window.Rows {
			return accounting.RecoveryReceipt{}, false, errors.New("complete source window row count differs from accepted debt events")
		}
		windowEvents := make([]accounting.RecoveryEvent, 0, debtEventCount)
		for _, event := range op.Events {
			if !isRecoverySourceCursorEvent(event) {
				continue
			}
			if event.Source != window.Source || event.Exchange != window.Exchange || event.Market != window.Market || event.AccountScope != window.AccountScope || event.Symbol != window.Symbol || event.Kind != window.Kind || event.Asset != window.Asset ||
				event.ObservedAt.Before(window.From) || !event.ObservedAt.Before(window.Through) {
				return accounting.RecoveryReceipt{}, false, errors.New("debt event identity or observed time differs from complete source window")
			}
			windowEvents = append(windowEvents, event)
		}
		verifier := s.recoverySourceVerifier()
		if verifier == nil {
			return accounting.RecoveryReceipt{}, false, ErrRecoverySourceWindowVerifierUnavailable
		}
		if err = verifier.VerifyRecoverySourceWindow(ctx, window, windowEvents); err != nil {
			return accounting.RecoveryReceipt{}, false, fmt.Errorf("verify trusted recovery source window: %w", err)
		}
		cursorKey = recoverySourceCursorKey(window.Identity())
		var oldRaw []byte
		readErr := tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_source_cursors WHERE cursor_key=?`, cursorKey).Scan(&oldRaw)
		if readErr == nil {
			if err = json.Unmarshal(oldRaw, &oldCursor); err != nil {
				return accounting.RecoveryReceipt{}, false, err
			}
			if oldCursor.EpisodeID != op.EpisodeID || oldCursor.Revision != window.ExpectedRevision || !oldCursor.Through.Equal(window.From) {
				return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
			}
		} else if !errors.Is(readErr, sql.ErrNoRows) {
			return accounting.RecoveryReceipt{}, false, readErr
		} else if window.ExpectedRevision != 0 {
			return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
		}
		if (window.ExpectedRevision == 0 && oldCursor.Revision != 0) || (window.ExpectedRevision > 0 && oldCursor.Revision == 0) {
			return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
		}
	}
	if len(op.Events) == 0 && len(op.Children) == 0 && op.SourceWindow == nil {
		return accounting.RecoveryReceipt{}, false, errors.New("empty recovery operation")
	}
	childByID := make(map[string]accounting.RecoveryChild, len(op.Children))
	for _, child := range op.Children {
		if _, duplicate := childByID[child.ID]; duplicate {
			return accounting.RecoveryReceipt{}, false, errors.New("recovery operation contains duplicate child identity")
		}
		childByID[child.ID] = child
	}
	fillEvents := make(map[string][]accounting.RecoveryEvent)
	for _, event := range op.Events {
		if !containsString(episode.AccountScopes, event.AccountScope) {
			return accounting.RecoveryReceipt{}, false, errors.New("event account scope is outside episode")
		}
		if event.Kind == accounting.RecoveryFillEvent {
			child, included := childByID[event.ChildID]
			if !included {
				return accounting.RecoveryReceipt{}, false, errors.New("fill event child must be included for cursor CAS")
			}
			if child.State != accounting.ChildBound || strings.TrimSpace(child.VenueOrderID) == "" {
				return accounting.RecoveryReceipt{}, false, errors.New("fill event requires a venue-bound child")
			}
			evidence := event.FillEvidence
			if evidence == nil || child.AccountScope != event.AccountScope || child.Exchange != event.Exchange || child.Market != event.Market ||
				child.Symbol != evidence.Symbol || child.ClientOrderID != evidence.ClientOrderID || child.VenueOrderID != evidence.VenueOrderID ||
				child.Side != evidence.Side || child.OrderIntentHash != evidence.OrderIntentHash {
				return accounting.RecoveryReceipt{}, false, errors.New("fill event identity differs from bound child")
			}
			fillEvents[event.ChildID] = append(fillEvents[event.ChildID], event)
		} else if event.ChildID != "" {
			child, included := childByID[event.ChildID]
			if !included {
				var raw []byte
				if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_children WHERE episode_id=? AND child_id=?`, op.EpisodeID, event.ChildID).Scan(&raw); err != nil {
					return accounting.RecoveryReceipt{}, false, errors.New("event child is not registered")
				}
				if err = json.Unmarshal(raw, &child); err != nil {
					return accounting.RecoveryReceipt{}, false, err
				}
			}
			if child.AccountScope != event.AccountScope || child.Exchange != event.Exchange || child.Market != event.Market {
				return accounting.RecoveryReceipt{}, false, errors.New("event owner differs from child")
			}
		}
	}
	for childID, child := range childByID {
		expected, ok := op.ExpectedChildRevisions[childID]
		if !ok {
			return accounting.RecoveryReceipt{}, false, errors.New("child expected revision is required")
		}
		var old accounting.RecoveryChild
		if expected > 0 {
			var raw []byte
			if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_children WHERE episode_id=? AND child_id=? AND revision=?`, op.EpisodeID, childID, expected).Scan(&raw); err != nil {
				return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
			}
			if err = json.Unmarshal(raw, &old); err != nil {
				return accounting.RecoveryReceipt{}, false, err
			}
			if !sameRecoveryChildOwner(old, child) {
				return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
			}
			if old.VenueOrderID != "" && old.VenueOrderID != child.VenueOrderID {
				return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
			}
		}
		fillList := fillEvents[childID]
		sort.Slice(fillList, func(i, j int) bool { return fillList[i].CursorSequence < fillList[j].CursorSequence })
		cursor := old.FillCursor
		for _, event := range fillList {
			if event.CursorSequence != cursor.Sequence+1 {
				return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
			}
			cursor = accounting.Cursor{Sequence: event.CursorSequence, TradeID: event.ExternalID}
		}
		if child.FillCursor != cursor {
			return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
		}
	}
	for _, child := range op.Children {
		if !containsString(episode.AccountScopes, child.AccountScope) || child.Bot != episode.Bot || child.StrategyName != episode.Strategy {
			return accounting.RecoveryReceipt{}, false, errors.New("child owner is outside episode")
		}
		expected := op.ExpectedChildRevisions[child.ID]
		child.Revision = expected + 1
		child.UpdatedAt = time.Now().UTC()
		payload, _ := json.Marshal(child)
		if expected == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_children(episode_id,child_id,revision,payload_json) VALUES(?,?,?,?)`, op.EpisodeID, child.ID, child.Revision, payload)
		} else {
			var result sql.Result
			result, err = tx.ExecContext(ctx, `UPDATE funding_recovery_children SET revision=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND child_id=? AND revision=?`, child.Revision, payload, op.EpisodeID, child.ID, expected)
			if err == nil {
				var n int64
				n, err = result.RowsAffected()
				if err == nil && n != 1 {
					err = accounting.ErrRecoveryConflict
				}
			}
		}
		if err != nil {
			return accounting.RecoveryReceipt{}, false, recoveryConflict(err)
		}
	}
	for _, event := range op.Events {
		eventKey := recoveryExternalEventKey(event)
		payload, _ := json.Marshal(event)
		_, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_events(event_key,episode_id,event_id,event_revision,payload_json) VALUES(?,?,?,1,?)`, eventKey, event.EpisodeID, event.ID, payload)
		if err != nil {
			return accounting.RecoveryReceipt{}, false, recoveryConflict(err)
		}
	}
	var updatedCursor *accounting.RecoverySourceCursor
	if op.SourceWindow != nil {
		window := *op.SourceWindow
		cursor := accounting.RecoverySourceCursor{RecoverySourceWindow: window, EpisodeID: op.EpisodeID, Revision: window.ExpectedRevision + 1, UpdatedAt: time.Now().UTC()}
		cursorJSON, marshalErr := json.Marshal(cursor)
		if marshalErr != nil {
			return accounting.RecoveryReceipt{}, false, marshalErr
		}
		if window.ExpectedRevision == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_source_cursors(cursor_key,episode_id,revision,source,exchange_name,market,account_scope,symbol,event_type,asset,window_from,window_through,complete_rows,total_rows,completion_marker,evidence_hash,payload_json) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
				cursorKey, op.EpisodeID, cursor.Revision, window.Source, window.Exchange, window.Market, window.AccountScope, window.Symbol, string(window.Kind), window.Asset, window.From.UTC(), window.Through.UTC(), window.Rows, window.TotalRows, window.CompletionMarker, window.EvidenceHash, cursorJSON)
		} else {
			result, updateErr := tx.ExecContext(ctx, `UPDATE funding_recovery_source_cursors SET revision=?,window_from=?,window_through=?,complete_rows=?,total_rows=?,completion_marker=?,evidence_hash=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE cursor_key=? AND episode_id=? AND revision=?`,
				cursor.Revision, window.From.UTC(), window.Through.UTC(), window.Rows, window.TotalRows, window.CompletionMarker, window.EvidenceHash, cursorJSON, cursorKey, op.EpisodeID, window.ExpectedRevision)
			err = updateErr
			if err == nil {
				var affected int64
				affected, err = result.RowsAffected()
				if err == nil && affected != 1 {
					err = accounting.ErrRecoveryConflict
				}
			}
		}
		if err != nil {
			return accounting.RecoveryReceipt{}, false, recoveryConflict(err)
		}
		updatedCursor = &cursor
	}
	next := episode
	next.Revision++
	next.State = accounting.RecoveryLedgerCommitted
	next.UpdatedAt = time.Now().UTC()
	nextJSON, _ := json.Marshal(next)
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_episodes SET revision=?,state=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND revision=?`, next.Revision, next.State, nextJSON, next.ID, op.ExpectedEpisodeRevision)
	if err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return accounting.RecoveryReceipt{}, false, accounting.ErrRecoveryConflict
	}
	projectionBytes, err := json.Marshal(fundingRecoveryProjection{Episode: next, Children: op.Children, Events: op.Events,
		SourceCursor: updatedCursor, ExpectedRuntimeRevision: op.ExpectedRuntimeRevision})
	if err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	projectionHash, err := fundingRecoveryProjectionHash(projectionBytes)
	if err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	receipt := accounting.RecoveryReceipt{EpisodeID: op.EpisodeID, OperationID: op.OperationID, RequestHash: hash, EpisodeRevision: next.Revision, State: accounting.RecoveryLedgerCommitted, ProjectionHash: projectionHash, Actor: op.Actor, AuditRef: op.AuditRef, EvidenceHash: op.EvidenceHash, OwnerGeneration: episode.OwnerGeneration, CreatedAt: time.Now().UTC()}
	receiptJSON, _ := json.Marshal(receipt)
	_, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_operations(episode_id,operation_id,request_hash,phase,receipt_json) VALUES(?,?,?,?,?)`, op.EpisodeID, op.OperationID, hash, string(accounting.RecoveryLedgerCommitted), receiptJSON)
	if err != nil {
		return accounting.RecoveryReceipt{}, false, recoveryConflict(err)
	}
	outbox := accounting.RecoveryOutbox{EpisodeID: op.EpisodeID, OperationID: op.OperationID, ProjectionPayload: projectionBytes, ExpectedRuntimeRevision: op.ExpectedRuntimeRevision, TargetEpisodeRevision: next.Revision, ProjectionHash: projectionHash, State: accounting.RecoveryProjectionPending, UpdatedAt: time.Now().UTC()}
	outboxJSON, _ := json.Marshal(outbox)
	_, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_outbox(episode_id,operation_id,state,target_episode_revision,projection_hash,payload_json) VALUES(?,?,?,?,?,?)`, op.EpisodeID, op.OperationID, outbox.State, outbox.TargetEpisodeRevision, outbox.ProjectionHash, outboxJSON)
	if err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	if err = updateRecoverySourceAnomaliesTx(ctx, tx, op, next); err != nil {
		return accounting.RecoveryReceipt{}, false, err
	}
	if err = tx.Commit(); err != nil {
		var raw []byte
		readErr := s.commitProbe.QueryRowContext(context.Background(), `SELECT receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=?`, op.EpisodeID, op.OperationID).Scan(&raw)
		var read accounting.RecoveryReceipt
		if readErr == nil {
			readErr = json.Unmarshal(raw, &read)
		}
		found := readErr == nil
		if readErr == nil && found && read.RequestHash == hash {
			return read, true, nil
		}
		return accounting.RecoveryReceipt{}, false, errors.Join(err, readErr)
	}
	receipt.State = accounting.RecoveryProjectionPending
	return receipt, false, nil
}

func (s *SQLStorage) GetRecoveryOutbox(ctx context.Context, episodeID, operationID string) (accounting.RecoveryOutbox, bool, error) {
	var raw []byte
	var rowEpisodeID, rowOperationID, rowState, rowHash string
	var rowRevision uint64
	err := s.db.QueryRowContext(ctx, `SELECT episode_id,operation_id,state,target_episode_revision,projection_hash,payload_json FROM funding_recovery_outbox WHERE episode_id=? AND operation_id=?`, episodeID, operationID).
		Scan(&rowEpisodeID, &rowOperationID, &rowState, &rowRevision, &rowHash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryOutbox{}, false, nil
	}
	if err != nil {
		return accounting.RecoveryOutbox{}, false, err
	}
	out, err := decodeFundingRecoveryOutboxRecord(rowEpisodeID, rowOperationID, rowState, rowRevision, rowHash, raw)
	if err != nil {
		return accounting.RecoveryOutbox{}, false, err
	}
	return out, true, nil
}

func decodeFundingRecoveryOutboxRecord(rowEpisodeID, rowOperationID, rowState string, rowRevision uint64, rowHash string, raw []byte) (accounting.RecoveryOutbox, error) {
	var out accounting.RecoveryOutbox
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	if out.EpisodeID != rowEpisodeID || out.OperationID != rowOperationID || string(out.State) != rowState || out.TargetEpisodeRevision != rowRevision || out.ProjectionHash != rowHash {
		return out, errors.New("durable recovery outbox index/payload identity mismatch")
	}
	return out, nil
}

func (s *SQLStorage) ListPendingRecoveryOutbox(ctx context.Context) ([]accounting.RecoveryOutbox, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT episode_id,operation_id,state,target_episode_revision,projection_hash,payload_json FROM funding_recovery_outbox WHERE state=? ORDER BY updated_at,episode_id,operation_id`, string(accounting.RecoveryProjectionPending))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]accounting.RecoveryOutbox, 0)
	for rows.Next() {
		var rowEpisodeID, rowOperationID, rowState, rowHash string
		var rowRevision uint64
		var raw []byte
		if err = rows.Scan(&rowEpisodeID, &rowOperationID, &rowState, &rowRevision, &rowHash, &raw); err != nil {
			return nil, err
		}
		item, decodeErr := decodeFundingRecoveryOutboxRecord(rowEpisodeID, rowOperationID, rowState, rowRevision, rowHash, raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLStorage) ListRecoveryOutboxes(ctx context.Context, episodeID string) ([]accounting.RecoveryOutbox, error) {
	if ctx == nil || strings.TrimSpace(episodeID) == "" {
		return nil, ErrFundingRecoveryUnavailable
	}
	rows, err := s.db.QueryContext(ctx, `SELECT episode_id,operation_id,state,target_episode_revision,projection_hash,payload_json FROM funding_recovery_outbox WHERE episode_id=? ORDER BY target_episode_revision,operation_id`, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]accounting.RecoveryOutbox, 0)
	for rows.Next() {
		var rowEpisodeID, rowOperationID, rowState, rowHash string
		var rowRevision uint64
		var raw []byte
		if err = rows.Scan(&rowEpisodeID, &rowOperationID, &rowState, &rowRevision, &rowHash, &raw); err != nil {
			return nil, err
		}
		item, decodeErr := decodeFundingRecoveryOutboxRecord(rowEpisodeID, rowOperationID, rowState, rowRevision, rowHash, raw)
		if decodeErr != nil {
			return nil, decodeErr
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLStorage) ListRecoveryChildren(ctx context.Context, episodeID string) ([]accounting.RecoveryChild, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM funding_recovery_children WHERE episode_id=? ORDER BY child_id`, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]accounting.RecoveryChild, 0)
	for rows.Next() {
		var raw []byte
		var item accounting.RecoveryChild
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLStorage) ListRecoveryEvents(ctx context.Context, episodeID string) ([]accounting.RecoveryEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM funding_recovery_events WHERE episode_id=? ORDER BY created_at,event_id`, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]accounting.RecoveryEvent, 0)
	for rows.Next() {
		var raw []byte
		var item accounting.RecoveryEvent
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLStorage) MarkRecoveryProjected(ctx context.Context, generation FundingCarryRuntimeGeneration, outbox accounting.RecoveryOutbox) error {
	if outbox.EpisodeID == "" || outbox.OperationID == "" || outbox.AppliedOperationID != outbox.OperationID || outbox.AppliedRevision != outbox.TargetEpisodeRevision || outbox.AppliedHash != outbox.ProjectionHash || outbox.State != accounting.RecoveryProjected {
		return errors.New("projection readback marker mismatch")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	currentEpisode, err := s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, outbox.EpisodeID)
	if err != nil {
		return err
	}
	var rowEpisodeID, rowOperationID, rowState, rowHash string
	var rowRevision uint64
	var currentRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT episode_id,operation_id,state,target_episode_revision,projection_hash,payload_json FROM funding_recovery_outbox WHERE episode_id=? AND operation_id=? AND state=?`, outbox.EpisodeID, outbox.OperationID, string(accounting.RecoveryProjectionPending)).
		Scan(&rowEpisodeID, &rowOperationID, &rowState, &rowRevision, &rowHash, &currentRaw)
	if err != nil {
		return errors.New("recovery outbox unavailable")
	}
	current, err := decodeFundingRecoveryOutboxRecord(rowEpisodeID, rowOperationID, rowState, rowRevision, rowHash, currentRaw)
	if err != nil {
		return err
	}
	if current.ProjectionHash != outbox.ProjectionHash || current.TargetEpisodeRevision != outbox.TargetEpisodeRevision ||
		current.ExpectedRuntimeRevision != outbox.ExpectedRuntimeRevision {
		return accounting.ErrRecoveryConflict
	}
	currentPayload, err := canonicalRecoveryJSON(current.ProjectionPayload)
	if err != nil {
		return err
	}
	providedPayload, err := canonicalRecoveryJSON(outbox.ProjectionPayload)
	if err != nil {
		return err
	}
	if !bytes.Equal(currentPayload, providedPayload) {
		return errors.New("recovery outbox projection differs from durable payload")
	}
	if err = VerifyFundingRecoveryProjectionHash(current.ProjectionPayload, current.ProjectionHash); err != nil {
		return errors.New("durable recovery outbox payload hash mismatch")
	}
	if err = VerifyFundingRecoveryProjectionHash(outbox.ProjectionPayload, outbox.ProjectionHash); err != nil {
		return errors.New("recovery outbox payload hash mismatch")
	}
	var projection fundingRecoveryProjection
	if err = json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil {
		return err
	}
	if err = VerifyFundingRecoveryOutboxRuntimeRevision(outbox, outbox.ProjectionPayload); err != nil {
		return err
	}
	priorEpisode := projection.Episode
	normalProjection := currentEpisode.Revision == outbox.TargetEpisodeRevision && currentEpisode.State == accounting.RecoveryLedgerCommitted &&
		currentEpisode.OwnerGeneration == generation.ID() && priorEpisode.OwnerGeneration == generation.ID() && priorEpisode.Revision == outbox.TargetEpisodeRevision
	takenOverProjection := false
	if currentEpisode.Revision == outbox.TargetEpisodeRevision+1 && currentEpisode.State == accounting.RecoveryBlocked && currentEpisode.HasCurrentOwnerTakeover() {
		takeover := currentEpisode.OwnerTakeovers[len(currentEpisode.OwnerTakeovers)-1]
		if takeover.PreviousOwnerGeneration == priorEpisode.OwnerGeneration && takeover.OwnerGeneration == generation.ID() && takeover.EpisodeRevision == currentEpisode.Revision && priorEpisode.Revision == outbox.TargetEpisodeRevision && priorEpisode.State == accounting.RecoveryLedgerCommitted {
			comparable := currentEpisode
			comparable.OwnerGeneration = priorEpisode.OwnerGeneration
			comparable.Revision = priorEpisode.Revision
			comparable.State = priorEpisode.State
			comparable.UpdatedAt = priorEpisode.UpdatedAt
			comparable.OwnerTakeovers = append([]accounting.RecoveryOwnerTakeoverRecord(nil), currentEpisode.OwnerTakeovers[:len(currentEpisode.OwnerTakeovers)-1]...)
			takenOverProjection = reflect.DeepEqual(comparable, priorEpisode)
		}
	}
	if priorEpisode.ID != currentEpisode.ID || priorEpisode.Bot != currentEpisode.Bot || priorEpisode.Strategy != currentEpisode.Strategy ||
		!reflect.DeepEqual(priorEpisode.AccountScopes, currentEpisode.AccountScopes) || (!normalProjection && !takenOverProjection) {
		return accounting.ErrRecoveryConflict
	}
	var receiptRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=?`, outbox.EpisodeID, outbox.OperationID).Scan(&receiptRaw)
	if err != nil {
		return err
	}
	var receipt accounting.RecoveryReceipt
	if err = json.Unmarshal(receiptRaw, &receipt); err != nil {
		return err
	}
	if receipt.EpisodeID != outbox.EpisodeID || receipt.OperationID != outbox.OperationID ||
		receipt.EpisodeRevision != outbox.TargetEpisodeRevision || receipt.ProjectionHash != outbox.ProjectionHash ||
		receipt.State != accounting.RecoveryLedgerCommitted {
		return errors.New("recovery outbox does not match durable operation receipt")
	}
	if takenOverProjection {
		for _, event := range projection.Events {
			if event.Kind == accounting.RecoveryFillEvent || event.Kind == accounting.RecoveryFeeEvent || event.FillEvidence != nil {
				return errors.New("takeover projection cannot write fill or fee evidence")
			}
		}
	}
	if currentEpisode.Revision >= accounting.MaxDurableRecoveryRevision {
		return errors.New("recovery episode revision exhausted before projection commit")
	}
	raw, _ := json.Marshal(outbox)
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_outbox SET state=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND operation_id=? AND state=?`, outbox.State, raw, outbox.EpisodeID, outbox.OperationID, string(accounting.RecoveryProjectionPending))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return accounting.ErrRecoveryConflict
	}
	episode := currentEpisode
	episode.Revision++
	episode.State = accounting.RecoveryProjected
	episode.UpdatedAt = time.Now().UTC()
	episodeRaw, marshalErr := json.Marshal(episode)
	if marshalErr != nil {
		return marshalErr
	}
	result, err = tx.ExecContext(ctx, `UPDATE funding_recovery_episodes SET state=?,payload_json=?,revision=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND revision=?`, accounting.RecoveryProjected, episodeRaw, episode.Revision, outbox.EpisodeID, currentEpisode.Revision)
	if err != nil {
		return err
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return accounting.ErrRecoveryConflict
	}
	receipt.State, receipt.EpisodeRevision = accounting.RecoveryProjected, episode.Revision
	receiptRaw, _ = json.Marshal(receipt)
	if _, err = tx.ExecContext(ctx, `UPDATE funding_recovery_operations SET phase=?,receipt_json=? WHERE episode_id=? AND operation_id=?`, receipt.State, receiptRaw, outbox.EpisodeID, outbox.OperationID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStorage) ListActiveRecoveryHolds(ctx context.Context) ([]accounting.RecoveryHold, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM funding_recovery_holds WHERE state='held' ORDER BY episode_id,recovery_generation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]accounting.RecoveryHold, 0)
	for rows.Next() {
		var raw []byte
		var item accounting.RecoveryHold
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *SQLStorage) GetRecoveryHold(ctx context.Context, episodeID string, generation uint64) (accounting.RecoveryHold, bool, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_holds WHERE episode_id=? AND recovery_generation=?`, episodeID, generation).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryHold{}, false, nil
	}
	if err != nil {
		return accounting.RecoveryHold{}, false, err
	}
	var hold accounting.RecoveryHold
	if err = json.Unmarshal(raw, &hold); err != nil {
		return hold, false, err
	}
	return hold, true, nil
}

func (s *SQLStorage) CreateRecoveryPermit(ctx context.Context, generation FundingCarryRuntimeGeneration, p accounting.OneShotPermit) (accounting.OneShotPermit, error) {
	p = p.Clone()
	if err := p.Validate(); err != nil {
		return accounting.OneShotPermit{}, err
	}
	if p.State != accounting.PermitAuthorized {
		return accounting.OneShotPermit{}, errors.New("new permit must be authorized")
	}
	raw, _ := json.Marshal(p)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	lockedEpisode, lockErr := s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, p.EpisodeID)
	if lockErr != nil {
		return p, lockErr
	}
	var priorRaw []byte
	lookupErr := tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_permits WHERE permit_id=?`, p.ID).Scan(&priorRaw)
	if lookupErr == nil {
		var prior accounting.OneShotPermit
		if err = json.Unmarshal(priorRaw, &prior); err != nil {
			return p, err
		}
		if prior.EpisodeID != p.EpisodeID || prior.Generation != p.Generation || prior.ActionID != p.ActionID || prior.ActionOrdinal != p.ActionOrdinal || prior.Kind != p.Kind || prior.AccountScope != p.AccountScope || prior.OwnerGeneration != p.OwnerGeneration || prior.LeaseID != p.LeaseID || prior.ExpectedRevision != p.ExpectedRevision || prior.OrderIntentHash != p.OrderIntentHash || !sameRecoveryOrderIntent(prior.OrderIntent, p.OrderIntent) || !sameStrings(prior.GateSourceExceptions, p.GateSourceExceptions) {
			return p, accounting.ErrRecoveryOperationConflict
		}
		if prior.State != accounting.PermitAuthorized || !recoveryEpisodeAllowsPermit(lockedEpisode) || lockedEpisode.RecoveryGeneration != p.Generation || lockedEpisode.OwnerGeneration != p.OwnerGeneration || lockedEpisode.Revision != p.ExpectedRevision || lockedEpisode.NextActionOrdinal != p.ActionOrdinal {
			return p, accounting.ErrRecoveryConflict
		}
		return prior, nil
	}
	if !errors.Is(lookupErr, sql.ErrNoRows) {
		return p, lookupErr
	}
	var episodeRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=? AND revision=?`, p.EpisodeID, p.ExpectedRevision).Scan(&episodeRaw); err != nil {
		return p, accounting.ErrRecoveryConflict
	}
	var episode accounting.RecoveryEpisode
	if err = json.Unmarshal(episodeRaw, &episode); err != nil {
		return p, err
	}
	if !recoveryEpisodeAllowsPermit(episode) || episode.RecoveryGeneration != p.Generation || episode.OwnerGeneration != p.OwnerGeneration || episode.NextActionOrdinal != p.ActionOrdinal {
		return p, accounting.ErrRecoveryConflict
	}
	if int(p.ActionOrdinal) > len(episode.ActionPlan) {
		return p, accounting.ErrRecoveryConflict
	}
	action := episode.ActionPlan[p.ActionOrdinal-1]
	if action.ID != p.ActionID || action.Ordinal != p.ActionOrdinal || action.Kind != p.Kind || action.AccountScope != p.AccountScope || action.Status != "authorized" || action.OrderIntentHash != p.OrderIntentHash || !sameStrings(action.GateSourceExceptions, p.GateSourceExceptions) {
		return p, accounting.ErrRecoveryConflict
	}
	if p.Kind == accounting.RecoveryActionOrder && (p.OrderIntent == nil || p.OrderIntent.Market != action.Market || p.OrderIntent.Symbol != action.Symbol) {
		return p, accounting.ErrRecoveryConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_permits(permit_id,episode_id,recovery_generation,action_id,lease_id,state,payload_json) VALUES(?,?,?,?,?,?,?)`, p.ID, p.EpisodeID, p.Generation, p.ActionID, p.LeaseID, p.State, raw)
	if err != nil {
		return p, recoveryConflict(err)
	}
	if err = tx.Commit(); err != nil {
		return p, err
	}
	return p, nil
}

func recoveryEpisodeAllowsPermit(episode accounting.RecoveryEpisode) bool {
	return !episode.HasCurrentOwnerTakeover() && (episode.State == accounting.RecoveryPrepared || episode.State == accounting.RecoveryReopened || episode.State == accounting.RecoveryProjected)
}

func (s *SQLStorage) ConsumeRecoveryPermit(ctx context.Context, generation FundingCarryRuntimeGeneration, p accounting.OneShotPermit) (accounting.OneShotPermit, bool, error) {
	p = p.Clone()
	if err := p.Validate(); err != nil {
		return p, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, false, err
	}
	defer tx.Rollback()
	episode, err := s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, p.EpisodeID)
	if err != nil {
		return p, false, err
	}
	if !recoveryEpisodeAllowsPermit(episode) || episode.RecoveryGeneration != p.Generation || episode.OwnerGeneration != p.OwnerGeneration || episode.Revision != p.ExpectedRevision || episode.NextActionOrdinal != p.ActionOrdinal || int(p.ActionOrdinal) > len(episode.ActionPlan) {
		return p, false, accounting.ErrRecoveryConflict
	}
	action := episode.ActionPlan[p.ActionOrdinal-1]
	if action.ID != p.ActionID || action.Ordinal != p.ActionOrdinal || action.Kind != p.Kind || action.AccountScope != p.AccountScope || action.Status != "authorized" || action.OrderIntentHash != p.OrderIntentHash || !sameStrings(action.GateSourceExceptions, p.GateSourceExceptions) {
		return p, false, accounting.ErrRecoveryConflict
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_permits WHERE permit_id=? AND episode_id=? AND recovery_generation=?`, p.ID, p.EpisodeID, p.Generation).Scan(&raw)
	if err != nil {
		return p, false, accounting.ErrRecoveryConflict
	}
	var current accounting.OneShotPermit
	if err = json.Unmarshal(raw, &current); err != nil {
		return p, false, err
	}
	if current.EpisodeID != p.EpisodeID || current.Generation != p.Generation || current.ActionID != p.ActionID || current.ActionOrdinal != p.ActionOrdinal || current.Kind != p.Kind || current.AccountScope != p.AccountScope || current.OwnerGeneration != p.OwnerGeneration || current.LeaseID != p.LeaseID || current.ExpectedRevision != p.ExpectedRevision || current.OrderIntentHash != p.OrderIntentHash || !sameStrings(current.GateSourceExceptions, p.GateSourceExceptions) {
		return p, false, accounting.ErrRecoveryConflict
	}
	if !sameRecoveryOrderIntent(current.OrderIntent, p.OrderIntent) {
		return p, false, accounting.ErrRecoveryConflict
	}
	if current.State == accounting.PermitConsumed {
		return current, false, nil
	}
	if current.State != accounting.PermitAuthorized {
		return p, false, accounting.ErrRecoveryConflict
	}
	current.State = accounting.PermitConsumed
	current.ConsumedAt = time.Now().UTC()
	updated, _ := json.Marshal(current)
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_permits SET state=?,payload_json=?,consumed_at=CURRENT_TIMESTAMP WHERE permit_id=? AND state=?`, current.State, updated, p.ID, string(accounting.PermitAuthorized))
	if err != nil {
		return p, false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return p, false, accounting.ErrRecoveryConflict
	}
	if current.Kind == accounting.RecoveryActionOrder {
		if current.OrderIntent == nil {
			return p, false, accounting.ErrRecoveryConflict
		}
		childID := recoveryAttemptChildID(current.ID)
		child := accounting.RecoveryChild{
			ID: childID, EpisodeID: current.EpisodeID, Role: action.Role, Exchange: current.OrderIntent.Exchange,
			Market: current.OrderIntent.Market, AccountScope: current.AccountScope, Bot: episode.Bot,
			Symbol: current.OrderIntent.Symbol, StrategyName: episode.Strategy, StrategyType: episode.Strategy,
			ClientOrderID: current.OrderIntent.ClientOrderID, OrderIntentHash: current.OrderIntentHash,
			Side: current.OrderIntent.Side, Revision: 1, State: accounting.ChildProvisional,
		}
		if err = child.Validate(); err != nil {
			return p, false, err
		}
		childJSON, marshalErr := json.Marshal(child)
		if marshalErr != nil {
			return p, false, marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_children(episode_id,child_id,revision,payload_json) VALUES(?,?,?,?)`, child.EpisodeID, child.ID, child.Revision, childJSON); err != nil {
			return p, false, recoveryConflict(err)
		}
		attempt := accounting.RecoverySubmissionAttempt{
			EpisodeID: current.EpisodeID, Generation: current.Generation, ActionID: current.ActionID,
			PermitID: current.ID, AccountScope: current.AccountScope, OwnerGeneration: current.OwnerGeneration,
			LeaseID: current.LeaseID, IntentHash: current.OrderIntentHash, Intent: *current.OrderIntent, ChildID: childID,
			State: accounting.RecoverySubmissionUnknown, Revision: 1, CreatedAt: current.ConsumedAt, UpdatedAt: current.ConsumedAt,
		}
		if err = attempt.Validate(); err != nil {
			return p, false, err
		}
		attemptJSON, marshalErr := json.Marshal(attempt)
		if marshalErr != nil {
			return p, false, marshalErr
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_submission_attempts(permit_id,episode_id,recovery_generation,action_id,revision,state,client_order_id,intent_hash,payload_json) VALUES(?,?,?,?,?,?,?,?,?)`,
			attempt.PermitID, attempt.EpisodeID, attempt.Generation, attempt.ActionID, attempt.Revision, attempt.State, attempt.Intent.ClientOrderID, attempt.IntentHash, attemptJSON)
		if err != nil {
			return p, false, recoveryConflict(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return current, false, err
	}
	return current, true, nil
}

func recoveryAttemptChildID(permitID string) string { return "recovery-attempt:" + permitID }

func sameRecoveryOrderIntent(left, right *accounting.RecoveryOrderIntent) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (s *SQLStorage) GetRecoverySubmissionAttempt(ctx context.Context, episodeID, permitID string) (accounting.RecoverySubmissionAttempt, bool, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_submission_attempts WHERE episode_id=? AND permit_id=?`, episodeID, permitID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoverySubmissionAttempt{}, false, nil
	}
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	var attempt accounting.RecoverySubmissionAttempt
	if err = json.Unmarshal(raw, &attempt); err != nil {
		return attempt, false, err
	}
	if err = attempt.Validate(); err != nil {
		return attempt, false, err
	}
	return attempt, true, nil
}

// AcknowledgeRecoverySubmission durably records the venue's positive order
// acknowledgement. It only advances the exact attempt created by the consumed
// permit; it never makes the permit reusable or releases the episode hold.
func (s *SQLStorage) AcknowledgeRecoverySubmission(ctx context.Context, generation FundingCarryRuntimeGeneration, permit accounting.OneShotPermit, venueOrderID string) error {
	permit = permit.Clone()
	if err := permit.Validate(); err != nil || permit.Kind != accounting.RecoveryActionOrder || venueOrderID == "" {
		return accounting.ErrRecoveryConflict
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	episode, err := s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, permit.EpisodeID)
	if err != nil {
		return err
	}
	if episode.RecoveryGeneration != permit.Generation || episode.OwnerGeneration != permit.OwnerGeneration {
		return accounting.ErrRecoveryConflict
	}
	var permitRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_permits WHERE permit_id=? AND episode_id=? AND recovery_generation=?`, permit.ID, permit.EpisodeID, permit.Generation).Scan(&permitRaw); err != nil {
		return recoveryConflict(err)
	}
	var persistedPermit accounting.OneShotPermit
	if err = json.Unmarshal(permitRaw, &persistedPermit); err != nil {
		return err
	}
	if persistedPermit.State != accounting.PermitConsumed || !sameRecoveryPermitIdentity(persistedPermit, permit) {
		return accounting.ErrRecoveryConflict
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_submission_attempts WHERE episode_id=? AND permit_id=?`, permit.EpisodeID, permit.ID).Scan(&raw); err != nil {
		return recoveryConflict(err)
	}
	var attempt accounting.RecoverySubmissionAttempt
	if err = json.Unmarshal(raw, &attempt); err != nil {
		return err
	}
	if err = attempt.Validate(); err != nil {
		return err
	}
	if attempt.Generation != permit.Generation || attempt.ActionID != permit.ActionID || attempt.AccountScope != permit.AccountScope || attempt.OwnerGeneration != permit.OwnerGeneration || attempt.LeaseID != permit.LeaseID || attempt.IntentHash != permit.OrderIntentHash || attempt.Intent != *permit.OrderIntent {
		return accounting.ErrRecoveryConflict
	}
	if attempt.State == accounting.RecoverySubmissionAcknowledged {
		if attempt.VenueOrderID == venueOrderID {
			var childRaw []byte
			if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_children WHERE episode_id=? AND child_id=?`, attempt.EpisodeID, attempt.ChildID).Scan(&childRaw); err != nil {
				return accounting.ErrRecoveryConflict
			}
			var child accounting.RecoveryChild
			if err = json.Unmarshal(childRaw, &child); err != nil || child.State != accounting.ChildBound || child.VenueOrderID != venueOrderID {
				return accounting.ErrRecoveryConflict
			}
			return tx.Commit()
		}
		return accounting.ErrRecoveryConflict
	}
	if attempt.State != accounting.RecoverySubmissionUnknown || attempt.Revision != 1 {
		return accounting.ErrRecoveryConflict
	}
	var childRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_children WHERE episode_id=? AND child_id=?`, attempt.EpisodeID, attempt.ChildID).Scan(&childRaw); err != nil {
		return accounting.ErrRecoveryConflict
	}
	var child accounting.RecoveryChild
	if err = json.Unmarshal(childRaw, &child); err != nil {
		return err
	}
	if err = child.Validate(); err != nil || child.State != accounting.ChildProvisional || child.ClientOrderID != attempt.Intent.ClientOrderID || child.OrderIntentHash != attempt.IntentHash || child.Side != attempt.Intent.Side || child.AccountScope != attempt.AccountScope || child.Exchange != attempt.Intent.Exchange || child.Market != attempt.Intent.Market || child.Symbol != attempt.Intent.Symbol || child.Bot == "" {
		return accounting.ErrRecoveryConflict
	}
	child.State = accounting.ChildBound
	child.VenueOrderID = venueOrderID
	child.Revision++
	child.UpdatedAt = time.Now().UTC()
	childJSON, err := json.Marshal(child)
	if err != nil {
		return err
	}
	childResult, err := tx.ExecContext(ctx, `UPDATE funding_recovery_children SET revision=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND child_id=? AND revision=?`, child.Revision, childJSON, child.EpisodeID, child.ID, 1)
	if err != nil {
		return err
	}
	childRows, err := childResult.RowsAffected()
	if err != nil || childRows != 1 {
		return accounting.ErrRecoveryConflict
	}
	attempt.State = accounting.RecoverySubmissionAcknowledged
	attempt.VenueOrderID = venueOrderID
	attempt.Revision++
	attempt.UpdatedAt = time.Now().UTC()
	updated, err := json.Marshal(attempt)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_submission_attempts SET revision=?,state=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND permit_id=? AND revision=? AND state=?`, attempt.Revision, attempt.State, updated, attempt.EpisodeID, attempt.PermitID, 1, accounting.RecoverySubmissionUnknown)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return accounting.ErrRecoveryConflict
	}
	return tx.Commit()
}

// AdjudicateRecoverySubmission binds a server-verified positive venue order to
// the original provisional child. It never releases the hold or authorizes a
// new permit; empty/error venue lookups must not call this method.
func (s *SQLStorage) AdjudicateRecoverySubmission(ctx context.Context, generation FundingCarryRuntimeGeneration, adjudication accounting.RecoverySubmissionAdjudication) (accounting.RecoverySubmissionAttempt, bool, error) {
	if err := adjudication.Validate(); err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	requestHash, err := recoverySubmissionAdjudicationRequestHash(adjudication)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	defer tx.Rollback()
	episode, err := s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, adjudication.EpisodeID)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	var priorHash string
	var priorAttemptRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,payload_json FROM funding_recovery_adjudications WHERE episode_id=? AND operation_id=?`, adjudication.EpisodeID, adjudication.OperationID).Scan(&priorHash, &priorAttemptRaw)
	if err == nil {
		if priorHash != requestHash {
			return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryOperationConflict
		}
		var prior accounting.RecoverySubmissionAttempt
		if err = json.Unmarshal(priorAttemptRaw, &prior); err != nil {
			return prior, false, err
		}
		if err = prior.Validate(); err != nil {
			return prior, false, err
		}
		if err = tx.Commit(); err != nil {
			return prior, false, err
		}
		return prior, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	if episode.State != accounting.RecoveryBlocked || episode.Revision != adjudication.ExpectedEpisodeRevision {
		return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryConflict
	}
	hold, err := readHeldRecoveryRecordTx(ctx, tx, episode.ID, episode.RecoveryGeneration)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	if hold.Bot != episode.Bot || hold.Strategy != episode.Strategy || hold.Generation != episode.RecoveryGeneration {
		return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryConflict
	}
	holdScopes, err := normalizeFundingCarryGenerationScopes(hold.AccountScopes)
	if err != nil || !sameStrings(holdScopes, generation.scopeKeys) {
		return accounting.RecoverySubmissionAttempt{}, false, ErrFundingRecoveryGenerationScopeMismatch
	}
	var attemptRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_submission_attempts WHERE episode_id=? AND permit_id=?`, adjudication.EpisodeID, adjudication.PermitID).Scan(&attemptRaw); err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, recoveryConflict(err)
	}
	var attempt accounting.RecoverySubmissionAttempt
	if err = json.Unmarshal(attemptRaw, &attempt); err != nil {
		return attempt, false, err
	}
	if err = attempt.Validate(); err != nil {
		return attempt, false, err
	}
	if attempt.Revision != adjudication.ExpectedAttemptRevision ||
		(attempt.State != accounting.RecoverySubmissionUnknown && attempt.State != accounting.RecoverySubmissionAcknowledged) ||
		attempt.EpisodeID != episode.ID || attempt.Intent.ClientOrderID == "" ||
		attempt.AccountScope == "" || !containsRecoveryScope(episode.AccountScopes, attempt.AccountScope) {
		return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryConflict
	}
	var childRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_children WHERE episode_id=? AND child_id=?`, attempt.EpisodeID, attempt.ChildID).Scan(&childRaw); err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, recoveryConflict(err)
	}
	var child accounting.RecoveryChild
	if err = json.Unmarshal(childRaw, &child); err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	if err = child.Validate(); err != nil ||
		(child.State != accounting.ChildProvisional && !(attempt.State == accounting.RecoverySubmissionAcknowledged && child.State == accounting.ChildBound && child.VenueOrderID == attempt.VenueOrderID)) ||
		child.ClientOrderID != attempt.Intent.ClientOrderID || child.OrderIntentHash != attempt.IntentHash || child.Side != attempt.Intent.Side || child.AccountScope != attempt.AccountScope ||
		child.Exchange != attempt.Intent.Exchange || child.Market != attempt.Intent.Market || child.Symbol != attempt.Intent.Symbol || child.Bot != episode.Bot || child.StrategyName != episode.Strategy || child.StrategyType != episode.Strategy {
		return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryConflict
	}
	if attempt.State == accounting.RecoverySubmissionUnknown {
		child.State = accounting.ChildBound
		child.VenueOrderID = adjudication.VenueOrderID
		child.Revision++
		child.UpdatedAt = time.Now().UTC()
		childJSON, marshalErr := json.Marshal(child)
		if marshalErr != nil {
			return accounting.RecoverySubmissionAttempt{}, false, marshalErr
		}
		result, updateErr := tx.ExecContext(ctx, `UPDATE funding_recovery_children SET revision=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND child_id=? AND revision=?`, child.Revision, childJSON, child.EpisodeID, child.ID, child.Revision-1)
		if updateErr != nil {
			return accounting.RecoverySubmissionAttempt{}, false, updateErr
		}
		if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
			return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryConflict
		}
	} else if child.VenueOrderID != adjudication.VenueOrderID {
		return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryConflict
	}
	previousState := attempt.State
	attempt.State = accounting.RecoverySubmissionFound
	attempt.VenueOrderID = adjudication.VenueOrderID
	attempt.EvidenceHash = adjudication.EvidenceHash
	attempt.Actor = adjudication.Actor
	attempt.AuditRef = adjudication.AuditRef
	attempt.Revision++
	attempt.UpdatedAt = time.Now().UTC()
	if err = attempt.Validate(); err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	updatedAttempt, err := json.Marshal(attempt)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_submission_attempts SET revision=?,state=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND permit_id=? AND revision=? AND state=?`, attempt.Revision, attempt.State, updatedAttempt, attempt.EpisodeID, attempt.PermitID, adjudication.ExpectedAttemptRevision, previousState)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	if rows, rowsErr := result.RowsAffected(); rowsErr != nil || rows != 1 {
		return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_adjudications(episode_id,operation_id,request_hash,permit_id,payload_json) VALUES(?,?,?,?,?)`, attempt.EpisodeID, adjudication.OperationID, requestHash, attempt.PermitID, updatedAttempt); err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, recoveryConflict(err)
	}
	if err = tx.Commit(); err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	return attempt, false, nil
}

// ReadRecoverySubmissionAdjudication makes response-loss retries independent
// of mutable venue status/fill snapshots. It compares only the operator request
// identity; the original server evidence remains immutable in the stored result.
func (s *SQLStorage) ReadRecoverySubmissionAdjudication(ctx context.Context, adjudication accounting.RecoverySubmissionAdjudication) (accounting.RecoverySubmissionAttempt, bool, error) {
	if ctx == nil || s == nil || s.db == nil || adjudication.EpisodeID == "" || adjudication.OperationID == "" || adjudication.PermitID == "" ||
		adjudication.ExpectedAttemptRevision == 0 || adjudication.ExpectedEpisodeRevision == 0 || adjudication.Actor == "" || adjudication.AuditRef == "" ||
		adjudication.Decision != accounting.RecoverySubmissionFound || !adjudication.Confirmed {
		return accounting.RecoverySubmissionAttempt{}, false, errors.New("incomplete funding recovery adjudication request identity")
	}
	requestHash, err := recoverySubmissionAdjudicationRequestHash(adjudication)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	var storedHash string
	var raw []byte
	err = s.db.QueryRowContext(ctx, `SELECT request_hash,payload_json FROM funding_recovery_adjudications WHERE episode_id=? AND operation_id=?`, adjudication.EpisodeID, adjudication.OperationID).Scan(&storedHash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoverySubmissionAttempt{}, false, nil
	}
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	if storedHash != requestHash {
		return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryOperationConflict
	}
	var attempt accounting.RecoverySubmissionAttempt
	if err = json.Unmarshal(raw, &attempt); err != nil {
		return attempt, false, err
	}
	if err = attempt.Validate(); err != nil {
		return attempt, false, err
	}
	return attempt, true, nil
}

func recoverySubmissionAdjudicationRequestHash(adjudication accounting.RecoverySubmissionAdjudication) (string, error) {
	request := adjudication
	request.VenueOrderID = ""
	request.EvidenceHash = ""
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	requestSum := sha256.Sum256(requestJSON)
	return hex.EncodeToString(requestSum[:]), nil
}

func containsRecoveryScope(scopes []string, scope string) bool {
	for _, candidate := range scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func sameRecoveryPermitIdentity(left, right accounting.OneShotPermit) bool {
	return left.ID == right.ID && left.EpisodeID == right.EpisodeID && left.Generation == right.Generation &&
		left.ActionID == right.ActionID && left.ActionOrdinal == right.ActionOrdinal && left.Kind == right.Kind &&
		left.AccountScope == right.AccountScope && left.OwnerGeneration == right.OwnerGeneration && left.LeaseID == right.LeaseID &&
		left.OrderIntentHash == right.OrderIntentHash && left.ExpectedRevision == right.ExpectedRevision &&
		sameStrings(left.GateSourceExceptions, right.GateSourceExceptions) && sameRecoveryOrderIntent(left.OrderIntent, right.OrderIntent)
}

func (s *SQLStorage) ListRecoverySubmissionAttempts(ctx context.Context, episodeID string) ([]accounting.RecoverySubmissionAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM funding_recovery_submission_attempts WHERE episode_id=? ORDER BY created_at,permit_id`, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attempts := make([]accounting.RecoverySubmissionAttempt, 0)
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var attempt accounting.RecoverySubmissionAttempt
		if err = json.Unmarshal(raw, &attempt); err != nil {
			return nil, err
		}
		if err = attempt.Validate(); err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return attempts, nil
}

func (s *SQLStorage) RevokeRecoveryPermit(ctx context.Context, generation FundingCarryRuntimeGeneration, episodeID, permitID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, episodeID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_permits SET state=?,payload_json=json_set(payload_json,'$.state',?) WHERE episode_id=? AND permit_id=? AND state=?`, accounting.PermitRevoked, accounting.PermitRevoked, episodeID, permitID, string(accounting.PermitAuthorized))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return accounting.ErrRecoveryConflict
	}
	return tx.Commit()
}

func (s *SQLStorage) TakeOverRecoveryEpisode(ctx context.Context, generation FundingCarryRuntimeGeneration, auth accounting.RecoveryOwnerTakeoverAuthorization) (accounting.RecoveryEpisode, bool, error) {
	if err := auth.Validate(); err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	if ctx == nil || s == nil || s.db == nil || generation.ID() == "" {
		return accounting.RecoveryEpisode{}, false, ErrFundingRecoveryUnavailable
	}
	requestHash, err := recoveryOwnerTakeoverRequestHash(generation, auth)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	defer tx.Rollback()
	if err = s.lockFundingRecoveryGenerationForScopes(ctx, tx, generation, generation.ScopeKeys()); err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	episode, err := readFundingRecoveryEpisodeTx(ctx, tx, auth.EpisodeID)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	var oldHash, oldReceipt []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=?`, auth.EpisodeID, auth.OperationID).Scan(&oldHash, &oldReceipt)
	if err == nil {
		if string(oldHash) != requestHash {
			return accounting.RecoveryEpisode{}, false, accounting.ErrRecoveryOperationConflict
		}
		var prior accounting.RecoveryReceipt
		if err = json.Unmarshal(oldReceipt, &prior); err != nil {
			return accounting.RecoveryEpisode{}, false, err
		}
		if prior.OperationID != auth.OperationID || prior.EpisodeID != auth.EpisodeID || prior.RequestHash != requestHash || prior.OwnerGeneration != generation.ID() || prior.PreviousOwnerGeneration != auth.ExpectedOwnerGeneration || prior.State != accounting.RecoveryBlocked {
			return accounting.RecoveryEpisode{}, false, accounting.ErrRecoveryConflict
		}
		return episode, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryEpisode{}, false, err
	}
	if err = validateRecoveryOwnerTakeoverEpisode(generation, auth, episode); err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	hold, err := readHeldRecoveryRecordTx(ctx, tx, episode.ID, episode.RecoveryGeneration)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	holdScopes, scopeErr := normalizeFundingCarryGenerationScopes(hold.AccountScopes)
	if scopeErr != nil || !sameStrings(holdScopes, generation.scopeKeys) || hold.Bot != episode.Bot || hold.Strategy != episode.Strategy {
		return accounting.RecoveryEpisode{}, false, ErrFundingRecoveryGenerationScopeMismatch
	}
	evidenceHash := recoveryOwnerTakeoverEvidenceHash(episode, hold, generation)
	if err = revokeAuthorizedRecoveryPermitsTx(ctx, tx, episode.ID); err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	updated, receipt, err := applyRecoveryOwnerTakeover(episode, hold, generation, auth, evidenceHash, requestHash)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	updatedJSON, err := json.Marshal(updated)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_episodes SET revision=?,state=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND revision=?`, updated.Revision, updated.State, updatedJSON, episode.ID, episode.Revision)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return accounting.RecoveryEpisode{}, false, accounting.ErrRecoveryConflict
	}
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_operations(episode_id,operation_id,request_hash,phase,receipt_json) VALUES(?,?,?,?,?)`, episode.ID, auth.OperationID, requestHash, string(accounting.RecoveryBlocked), receiptJSON); err != nil {
		return accounting.RecoveryEpisode{}, false, recoveryConflict(err)
	}
	if err = tx.Commit(); err != nil {
		return accounting.RecoveryEpisode{}, false, err
	}
	return updated, false, nil
}

func recoveryOwnerTakeoverRequestHash(generation FundingCarryRuntimeGeneration, auth accounting.RecoveryOwnerTakeoverAuthorization) (string, error) {
	return ExpectedFundingRecoveryOwnerTakeoverRequestHash(generation.ID(), generation.ScopeKeys(), auth)
}

func recoveryOwnerTakeoverRequestHashForOwner(ownerGeneration string, scopes []string, auth accounting.RecoveryOwnerTakeoverAuthorization) (string, error) {
	requestBytes, err := json.Marshal(struct {
		Authorization accounting.RecoveryOwnerTakeoverAuthorization `json:"authorization"`
		Owner         string                                        `json:"new_owner_generation"`
		Scopes        []string                                      `json:"account_scopes"`
	}{auth, ownerGeneration, scopes})
	if err != nil {
		return "", err
	}
	return canonicalHash(requestBytes), nil
}

// ExpectedFundingRecoveryOwnerTakeoverRequestHash returns the idempotency hash
// for a takeover authorization using its persisted non-secret owner identity
// and normalized account scopes.
func ExpectedFundingRecoveryOwnerTakeoverRequestHash(ownerGeneration string, scopes []string,
	auth accounting.RecoveryOwnerTakeoverAuthorization) (string, error) {
	if strings.TrimSpace(ownerGeneration) == "" {
		return "", errors.New("funding recovery takeover owner generation is required")
	}
	normalized, err := normalizeFundingCarryGenerationScopes(scopes)
	if err != nil {
		return "", err
	}
	return recoveryOwnerTakeoverRequestHashForOwner(ownerGeneration, normalized, auth)
}

func readFundingRecoveryEpisodeTx(ctx context.Context, tx *sql.Tx, episodeID string) (accounting.RecoveryEpisode, error) {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=?`, episodeID).Scan(&raw); err != nil {
		return accounting.RecoveryEpisode{}, recoveryConflict(err)
	}
	var episode accounting.RecoveryEpisode
	if err := json.Unmarshal(raw, &episode); err != nil {
		return episode, err
	}
	if episode.ID != episodeID || episode.OwnerGeneration == "" || episode.Revision == 0 || episode.RecoveryGeneration == 0 {
		return episode, errors.New("stored recovery episode has incomplete owner identity")
	}
	return episode, nil
}

func validateRecoveryOwnerTakeoverEpisode(generation FundingCarryRuntimeGeneration, auth accounting.RecoveryOwnerTakeoverAuthorization, episode accounting.RecoveryEpisode) error {
	storedScopes, err := normalizeFundingCarryGenerationScopes(episode.AccountScopes)
	if err != nil || !sameStrings(storedScopes, generation.scopeKeys) {
		return ErrFundingRecoveryGenerationScopeMismatch
	}
	if episode.OwnerGeneration != auth.ExpectedOwnerGeneration || episode.OwnerGeneration == generation.ID() || episode.Revision != auth.ExpectedEpisodeRevision || episode.State == accounting.RecoveryReleased || episode.State == accounting.RecoveryReleasePending {
		return accounting.ErrRecoveryConflict
	}
	return nil
}

func readHeldRecoveryRecordTx(ctx context.Context, tx *sql.Tx, episodeID string, recoveryGeneration uint64) (accounting.RecoveryHold, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_holds WHERE episode_id=? AND recovery_generation=? AND state=?`, episodeID, recoveryGeneration, "held").Scan(&raw)
	if err != nil {
		return accounting.RecoveryHold{}, recoveryConflict(err)
	}
	var hold accounting.RecoveryHold
	if err = json.Unmarshal(raw, &hold); err != nil {
		return hold, err
	}
	if hold.State != "held" || hold.EpisodeID != episodeID || hold.Generation != recoveryGeneration {
		return accounting.RecoveryHold{}, accounting.ErrRecoveryConflict
	}
	return hold, nil
}

func revokeAuthorizedRecoveryPermitsTx(ctx context.Context, tx *sql.Tx, episodeID string) error {
	rows, err := tx.QueryContext(ctx, `SELECT permit_id,payload_json FROM funding_recovery_permits WHERE episode_id=? AND state=?`, episodeID, string(accounting.PermitAuthorized))
	if err != nil {
		return err
	}
	type permitRecord struct {
		id  string
		raw []byte
	}
	var pending []permitRecord
	for rows.Next() {
		var record permitRecord
		if err = rows.Scan(&record.id, &record.raw); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, record)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, record := range pending {
		var permit accounting.OneShotPermit
		if err = json.Unmarshal(record.raw, &permit); err != nil {
			return err
		}
		if permit.State != accounting.PermitAuthorized || permit.ID != record.id || permit.EpisodeID != episodeID {
			return accounting.ErrRecoveryConflict
		}
		permit.State = accounting.PermitRevoked
		updated, marshalErr := json.Marshal(permit)
		if marshalErr != nil {
			return marshalErr
		}
		result, updateErr := tx.ExecContext(ctx, `UPDATE funding_recovery_permits SET state=?,payload_json=? WHERE episode_id=? AND permit_id=? AND state=?`, string(accounting.PermitRevoked), updated, episodeID, record.id, string(accounting.PermitAuthorized))
		if updateErr != nil {
			return updateErr
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return accounting.ErrRecoveryConflict
		}
	}
	return nil
}

func recoveryOwnerTakeoverEvidenceHash(episode accounting.RecoveryEpisode, hold accounting.RecoveryHold, generation FundingCarryRuntimeGeneration) string {
	evidence, _ := json.Marshal(struct {
		EpisodeID     string   `json:"episode_id"`
		Revision      uint64   `json:"episode_revision"`
		RecoveryGen   uint64   `json:"recovery_generation"`
		PreviousOwner string   `json:"previous_owner_generation"`
		CurrentOwner  string   `json:"current_owner_generation"`
		Scopes        []string `json:"account_scopes"`
		HoldID        string   `json:"hold_id"`
		HoldRevision  uint64   `json:"hold_revision"`
	}{episode.ID, episode.Revision, episode.RecoveryGeneration, episode.OwnerGeneration, generation.ID(), generation.ScopeKeys(), hold.ID, hold.Revision})
	return canonicalHash(evidence)
}

func applyRecoveryOwnerTakeover(episode accounting.RecoveryEpisode, hold accounting.RecoveryHold, generation FundingCarryRuntimeGeneration, auth accounting.RecoveryOwnerTakeoverAuthorization, evidenceHash, requestHash string) (accounting.RecoveryEpisode, accounting.RecoveryReceipt, error) {
	previousOwner := episode.OwnerGeneration
	updated := episode
	updated.OwnerTakeovers = append(append([]accounting.RecoveryOwnerTakeoverRecord(nil), episode.OwnerTakeovers...), accounting.RecoveryOwnerTakeoverRecord{
		OperationID: auth.OperationID, Actor: auth.Actor, AuditRef: auth.AuditRef, EvidenceHash: evidenceHash,
		PreviousOwnerGeneration: previousOwner, OwnerGeneration: generation.ID(), EpisodeRevision: episode.Revision + 1, CreatedAt: time.Now().UTC(),
	})
	updated.OwnerGeneration = generation.ID()
	updated.Revision++
	updated.State = accounting.RecoveryBlocked
	updated.UpdatedAt = time.Now().UTC()
	if err := updated.Validate(); err != nil {
		return accounting.RecoveryEpisode{}, accounting.RecoveryReceipt{}, err
	}
	return updated, accounting.RecoveryReceipt{
		EpisodeID: episode.ID, OperationID: auth.OperationID, RequestHash: requestHash, EpisodeRevision: updated.Revision,
		RecoveryGeneration: episode.RecoveryGeneration, State: updated.State, ProjectionHash: evidenceHash,
		Actor: auth.Actor, AuditRef: auth.AuditRef, EvidenceHash: evidenceHash,
		PreviousOwnerGeneration: previousOwner, OwnerGeneration: generation.ID(), CreatedAt: time.Now().UTC(),
	}, nil
}

func validateRecoveryReleaseSubmissionEvidence(ctx context.Context, tx *sql.Tx, episodeID string) error {
	attemptRows, err := tx.QueryContext(ctx, `SELECT payload_json FROM funding_recovery_submission_attempts WHERE episode_id=?`, episodeID)
	if err != nil {
		return fmt.Errorf("read recovery submission attempts before release: %w", err)
	}
	attempts := make([]accounting.RecoverySubmissionAttempt, 0)
	for attemptRows.Next() {
		var raw []byte
		var attempt accounting.RecoverySubmissionAttempt
		if err := attemptRows.Scan(&raw); err != nil {
			attemptRows.Close()
			return err
		}
		if err := json.Unmarshal(raw, &attempt); err != nil {
			attemptRows.Close()
			return err
		}
		if err := attempt.Validate(); err != nil || attempt.EpisodeID != episodeID || attempt.State != accounting.RecoverySubmissionFound {
			attemptRows.Close()
			return accounting.ErrRecoveryConflict
		}
		attempts = append(attempts, attempt)
	}
	if err := attemptRows.Err(); err != nil {
		attemptRows.Close()
		return err
	}
	if err := attemptRows.Close(); err != nil {
		return err
	}
	if len(attempts) == 0 {
		return nil
	}
	childRows, err := tx.QueryContext(ctx, `SELECT payload_json FROM funding_recovery_children WHERE episode_id=?`, episodeID)
	if err != nil {
		return fmt.Errorf("read recovery children before release: %w", err)
	}
	children := make(map[string]accounting.RecoveryChild)
	for childRows.Next() {
		var raw []byte
		var child accounting.RecoveryChild
		if err := childRows.Scan(&raw); err != nil {
			childRows.Close()
			return err
		}
		if err := json.Unmarshal(raw, &child); err != nil {
			childRows.Close()
			return err
		}
		children[child.ID] = child
	}
	if err := childRows.Err(); err != nil {
		childRows.Close()
		return err
	}
	if err := childRows.Close(); err != nil {
		return err
	}
	eventRows, err := tx.QueryContext(ctx, `SELECT payload_json FROM funding_recovery_events WHERE episode_id=?`, episodeID)
	if err != nil {
		return fmt.Errorf("read recovery events before release: %w", err)
	}
	events := make([]accounting.RecoveryEvent, 0)
	for eventRows.Next() {
		var raw []byte
		var event accounting.RecoveryEvent
		if err := eventRows.Scan(&raw); err != nil {
			eventRows.Close()
			return err
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			eventRows.Close()
			return err
		}
		events = append(events, event)
	}
	if err := eventRows.Err(); err != nil {
		eventRows.Close()
		return err
	}
	if err := eventRows.Close(); err != nil {
		return err
	}
	accountedChildren := make(map[string]struct{}, len(attempts))
	for _, attempt := range attempts {
		accountedChildren[attempt.ChildID] = struct{}{}
		child, found := children[attempt.ChildID]
		if !found || child.EpisodeID != episodeID || child.State == accounting.ChildProvisional || child.State == accounting.ChildUnknown ||
			child.VenueOrderID != attempt.VenueOrderID || child.OrderIntentHash != attempt.IntentHash || child.Symbol != attempt.Intent.Symbol ||
			child.Side != attempt.Intent.Side || child.Exchange != attempt.Intent.Exchange || child.Market != attempt.Intent.Market || child.AccountScope != attempt.AccountScope {
			return accounting.ErrRecoveryConflict
		}
		zeroTerminalMarkers := 0
		fillEventCount := 0
		batchMarkerCount := 0
		fillIDs := make(map[string]struct{})
		feeAdjustedFillIDs := make(map[string]struct{})
		for _, event := range events {
			if event.Validate() != nil || event.EpisodeID != episodeID {
				return accounting.ErrRecoveryConflict
			}
			if event.ChildID != attempt.ChildID {
				continue
			}
			if event.Exchange != child.Exchange || event.Market != child.Market || event.AccountScope != child.AccountScope {
				return accounting.ErrRecoveryConflict
			}
			if event.Kind == accounting.RecoveryFillBatchCompleteEvent && event.CompletenessMarker == string(accounting.RecoveryFillBatchCompleteEvent) {
				if event.Source != "venue_order_fill_batch" {
					return accounting.ErrRecoveryConflict
				}
				batchMarkerCount++
			} else if event.Kind == accounting.RecoveryTerminalEvent && strings.HasPrefix(event.ExternalID, "terminal_zero:") && event.CompletenessMarker == "zero_fill_terminal" {
				zeroTerminalMarkers++
			}
			if event.Kind == accounting.RecoveryFillEvent {
				if event.Source != "venue_order_fill_batch" || event.FillEvidence == nil || event.FillEvidence.VenueOrderID != attempt.VenueOrderID ||
					event.FillEvidence.ClientOrderID != attempt.Intent.ClientOrderID || event.FillEvidence.OrderIntentHash != attempt.IntentHash ||
					event.FillEvidence.Symbol != attempt.Intent.Symbol || event.FillEvidence.Side != attempt.Intent.Side {
					return accounting.ErrRecoveryConflict
				}
				fillEventCount++
				fillIDs[event.ExternalID] = struct{}{}
			}
			if event.Kind == accounting.RecoveryFeeEvent && event.Source == accounting.RecoveryManualFeeAdjustmentSource {
				feeAdjustedFillIDs[event.RelatedExternalID] = struct{}{}
			}
		}
		if fillEventCount == 0 {
			if zeroTerminalMarkers != 1 || batchMarkerCount != 0 {
				return accounting.ErrRecoveryConflict
			}
		} else if zeroTerminalMarkers != 0 || batchMarkerCount == 0 || accounting.ValidateRecoveryFillBatchMarkers(events, attempt.ChildID) != nil {
			return accounting.ErrRecoveryConflict
		}
		for _, event := range events {
			if event.ChildID == attempt.ChildID && event.Kind == accounting.RecoveryFillEvent && event.FillEvidence != nil && !event.FillEvidence.CommissionKnown {
				if _, adjusted := feeAdjustedFillIDs[event.ExternalID]; !adjusted {
					return accounting.ErrRecoveryConflict
				}
			}
		}
		for tradeID := range feeAdjustedFillIDs {
			if _, exists := fillIDs[tradeID]; !exists {
				return accounting.ErrRecoveryConflict
			}
		}
	}
	for _, child := range children {
		if child.State == accounting.ChildBound {
			if _, found := accountedChildren[child.ID]; !found {
				return accounting.ErrRecoveryConflict
			}
		}
	}
	return nil
}

func (s *SQLStorage) PrepareRecoveryRelease(ctx context.Context, generation FundingCarryRuntimeGeneration, auth accounting.RecoveryReleaseAuthorization) (accounting.RecoveryEpisode, error) {
	if err := auth.Validate(); err != nil {
		return accounting.RecoveryEpisode{}, err
	}
	requestBytes, _ := json.Marshal(auth)
	requestHash := canonicalHash(requestBytes)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return accounting.RecoveryEpisode{}, err
	}
	defer tx.Rollback()
	if _, err = s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, auth.EpisodeID); err != nil {
		return accounting.RecoveryEpisode{}, err
	}
	if err = requireRecoverySourceAnomaliesVerified(ctx, tx, auth.EpisodeID); err != nil {
		return accounting.RecoveryEpisode{}, err
	}
	if err = validateRecoveryReleaseSubmissionEvidence(ctx, tx, auth.EpisodeID); err != nil {
		return accounting.RecoveryEpisode{}, err
	}
	childRows, err := tx.QueryContext(ctx, `SELECT payload_json FROM funding_recovery_children WHERE episode_id=?`, auth.EpisodeID)
	if err != nil {
		return accounting.RecoveryEpisode{}, fmt.Errorf("read recovery children before hold release: %w", err)
	}
	for childRows.Next() {
		var childPayload []byte
		if err = childRows.Scan(&childPayload); err != nil {
			childRows.Close()
			return accounting.RecoveryEpisode{}, err
		}
		var child accounting.RecoveryChild
		if err = json.Unmarshal(childPayload, &child); err != nil {
			childRows.Close()
			return accounting.RecoveryEpisode{}, err
		}
		if child.State == accounting.ChildProvisional || child.State == accounting.ChildUnknown {
			childRows.Close()
			return accounting.RecoveryEpisode{}, accounting.ErrRecoveryConflict
		}
	}
	if err = childRows.Err(); err != nil {
		childRows.Close()
		return accounting.RecoveryEpisode{}, fmt.Errorf("iterate recovery children before hold release: %w", err)
	}
	if err = childRows.Close(); err != nil {
		return accounting.RecoveryEpisode{}, err
	}
	var existingHash string
	var existingRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=?`, auth.EpisodeID, auth.OperationID).Scan(&existingHash, &existingRaw)
	if err == nil {
		if existingHash != requestHash {
			return accounting.RecoveryEpisode{}, accounting.ErrRecoveryOperationConflict
		}
		var receipt accounting.RecoveryReceipt
		if err = json.Unmarshal(existingRaw, &receipt); err != nil {
			return accounting.RecoveryEpisode{}, err
		}
		if receipt.State != accounting.RecoveryReleasePending && receipt.State != accounting.RecoveryReleased {
			return accounting.RecoveryEpisode{}, accounting.ErrRecoveryConflict
		}
		var currentRaw []byte
		if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=?`, auth.EpisodeID).Scan(&currentRaw); err != nil {
			return accounting.RecoveryEpisode{}, err
		}
		var current accounting.RecoveryEpisode
		if err = json.Unmarshal(currentRaw, &current); err != nil {
			return current, err
		}
		return current, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryEpisode{}, err
	}
	var episodeRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=? AND revision=?`, auth.EpisodeID, auth.ExpectedEpisodeRevision).Scan(&episodeRaw)
	if err != nil {
		return accounting.RecoveryEpisode{}, accounting.ErrRecoveryConflict
	}
	var episode accounting.RecoveryEpisode
	if err = json.Unmarshal(episodeRaw, &episode); err != nil {
		return episode, err
	}
	if episode.State != accounting.RecoveryProjected || episode.RecoveryGeneration != auth.RecoveryGeneration || episode.OwnerGeneration != auth.OwnerGeneration {
		return episode, accounting.ErrRecoveryConflict
	}
	var holdRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_holds WHERE episode_id=? AND recovery_generation=? AND state='held' AND hold_revision=?`, auth.EpisodeID, auth.RecoveryGeneration, auth.ExpectedHoldRevision).Scan(&holdRaw); err != nil {
		return episode, accounting.ErrRecoveryConflict
	}
	var hold accounting.RecoveryHold
	if err = json.Unmarshal(holdRaw, &hold); err != nil {
		return episode, err
	}
	var pending int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_outbox WHERE episode_id=? AND state<>?`, auth.EpisodeID, string(accounting.RecoveryProjected)).Scan(&pending); err != nil || pending != 0 {
		return episode, accounting.ErrRecoveryConflict
	}
	var projectionHash string
	var outboxRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT projection_hash,payload_json FROM funding_recovery_outbox WHERE episode_id=? ORDER BY target_episode_revision DESC LIMIT 1`, auth.EpisodeID).Scan(&projectionHash, &outboxRaw)
	if err != nil || projectionHash != auth.ProjectionHash {
		return episode, accounting.ErrRecoveryConflict
	}
	var outbox accounting.RecoveryOutbox
	if err = json.Unmarshal(outboxRaw, &outbox); err != nil {
		return episode, err
	}
	if outbox.State != accounting.RecoveryProjected || outbox.AppliedOperationID != outbox.OperationID || outbox.AppliedHash != auth.ProjectionHash || outbox.ExpectedRuntimeRevision != auth.RuntimeRevision {
		return episode, accounting.ErrRecoveryConflict
	}
	if err = revokeAuthorizedRecoveryPermitsTx(ctx, tx, auth.EpisodeID); err != nil {
		return episode, fmt.Errorf("revoke unused recovery permits before hold release: %w", err)
	}
	episode.Revision++
	episode.State = accounting.RecoveryReleasePending
	episode.UpdatedAt = time.Now().UTC()
	episodeRaw, _ = json.Marshal(episode)
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_episodes SET revision=?,state=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND revision=? AND state=?`, episode.Revision, episode.State, episodeRaw, auth.EpisodeID, auth.ExpectedEpisodeRevision, string(accounting.RecoveryProjected))
	if err != nil {
		return episode, err
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return episode, accounting.ErrRecoveryConflict
	}
	receipt := accounting.RecoveryReceipt{EpisodeID: auth.EpisodeID, OperationID: auth.OperationID, RequestHash: requestHash, EpisodeRevision: episode.Revision, RecoveryGeneration: auth.RecoveryGeneration, State: accounting.RecoveryReleasePending, ProjectionHash: auth.ProjectionHash, Actor: auth.Actor, AuditRef: auth.AuditRef, EvidenceHash: auth.EvidenceHash, ExposureClearanceHash: auth.ExposureClearanceHash, OwnerGeneration: auth.OwnerGeneration, CreatedAt: time.Now().UTC()}
	receiptJSON, _ := json.Marshal(receipt)
	if _, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_operations(episode_id,operation_id,request_hash,phase,receipt_json) VALUES(?,?,?,?,?)`, auth.EpisodeID, auth.OperationID, requestHash, string(accounting.RecoveryReleasePending), receiptJSON); err != nil {
		return episode, recoveryConflict(err)
	}
	if err = tx.Commit(); err != nil {
		return episode, err
	}
	return episode, nil
}

func (s *SQLStorage) ReleaseRecoveryHold(ctx context.Context, generation FundingCarryRuntimeGeneration, auth accounting.RecoveryReleaseAuthorization) (accounting.RecoveryHold, error) {
	if err := auth.Validate(); err != nil {
		return accounting.RecoveryHold{}, err
	}
	if auth.ExpectedEpisodeRevision == ^uint64(0) {
		return accounting.RecoveryHold{}, accounting.ErrRecoveryConflict
	}
	requestBytes, _ := json.Marshal(auth)
	requestHash := canonicalHash(requestBytes)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return accounting.RecoveryHold{}, err
	}
	defer tx.Rollback()
	if _, err = s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, auth.EpisodeID); err != nil {
		return accounting.RecoveryHold{}, err
	}
	if err = requireRecoverySourceAnomaliesVerified(ctx, tx, auth.EpisodeID); err != nil {
		return accounting.RecoveryHold{}, err
	}
	var oldHash string
	var oldReceipt []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=? AND phase=?`, auth.EpisodeID, auth.OperationID, string(accounting.RecoveryReleased)).Scan(&oldHash, &oldReceipt)
	if err == nil {
		if oldHash != requestHash {
			return accounting.RecoveryHold{}, accounting.ErrRecoveryOperationConflict
		}
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_holds WHERE episode_id=? AND recovery_generation=?`, auth.EpisodeID, auth.RecoveryGeneration).Scan(&raw); err != nil {
			return accounting.RecoveryHold{}, err
		}
		var hold accounting.RecoveryHold
		if err = json.Unmarshal(raw, &hold); err != nil {
			return hold, err
		}
		return hold, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryHold{}, err
	}
	var episodeRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=? AND revision=? AND state=?`, auth.EpisodeID, auth.ExpectedEpisodeRevision+1, string(accounting.RecoveryReleasePending)).Scan(&episodeRaw)
	if err != nil {
		return accounting.RecoveryHold{}, accounting.ErrRecoveryConflict
	}
	var episode accounting.RecoveryEpisode
	if err = json.Unmarshal(episodeRaw, &episode); err != nil {
		return accounting.RecoveryHold{}, err
	}
	if episode.RecoveryGeneration != auth.RecoveryGeneration || episode.OwnerGeneration != auth.OwnerGeneration {
		return accounting.RecoveryHold{}, accounting.ErrRecoveryConflict
	}
	var storedHash string
	var receiptRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=? AND phase=?`, auth.EpisodeID, auth.OperationID, string(accounting.RecoveryReleasePending)).Scan(&storedHash, &receiptRaw)
	if err != nil || storedHash != requestHash {
		return accounting.RecoveryHold{}, accounting.ErrRecoveryConflict
	}
	var holdRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_holds WHERE episode_id=? AND recovery_generation=? AND hold_revision=? AND state='held'`, auth.EpisodeID, auth.RecoveryGeneration, auth.ExpectedHoldRevision).Scan(&holdRaw)
	if err != nil {
		return accounting.RecoveryHold{}, accounting.ErrRecoveryConflict
	}
	var hold accounting.RecoveryHold
	if err = json.Unmarshal(holdRaw, &hold); err != nil {
		return hold, err
	}
	hold.State = "released"
	hold.Revision++
	hold.ReleasedByOperation = auth.OperationID
	hold.UpdatedAt = time.Now().UTC()
	holdRaw, _ = json.Marshal(hold)
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_holds SET state=?,hold_revision=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND recovery_generation=? AND hold_revision=? AND state='held'`, hold.State, hold.Revision, holdRaw, auth.EpisodeID, auth.RecoveryGeneration, auth.ExpectedHoldRevision)
	if err != nil {
		return hold, err
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return hold, accounting.ErrRecoveryConflict
	}
	episode.Revision++
	episode.State = accounting.RecoveryReleased
	episode.UpdatedAt = time.Now().UTC()
	episodeRaw, _ = json.Marshal(episode)
	result, err = tx.ExecContext(ctx, `UPDATE funding_recovery_episodes SET revision=?,state=?,payload_json=?,updated_at=CURRENT_TIMESTAMP WHERE episode_id=? AND revision=? AND state=?`, episode.Revision, episode.State, episodeRaw, auth.EpisodeID, auth.ExpectedEpisodeRevision+1, string(accounting.RecoveryReleasePending))
	if err != nil {
		return hold, err
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return hold, accounting.ErrRecoveryConflict
	}
	var receipt accounting.RecoveryReceipt
	if err = json.Unmarshal(receiptRaw, &receipt); err != nil {
		return hold, err
	}
	if receipt.Actor != auth.Actor || receipt.AuditRef != auth.AuditRef || receipt.EvidenceHash != auth.EvidenceHash || receipt.ExposureClearanceHash != auth.ExposureClearanceHash ||
		receipt.ProjectionHash != auth.ProjectionHash || receipt.OwnerGeneration != auth.OwnerGeneration {
		return accounting.RecoveryHold{}, accounting.ErrRecoveryOperationConflict
	}
	receipt.State = accounting.RecoveryReleased
	receipt.EpisodeRevision = episode.Revision
	receiptJSON, _ := json.Marshal(receipt)
	if _, err = tx.ExecContext(ctx, `UPDATE funding_recovery_operations SET phase=?,receipt_json=? WHERE episode_id=? AND operation_id=? AND request_hash=?`, string(accounting.RecoveryReleased), receiptJSON, auth.EpisodeID, auth.OperationID, requestHash); err != nil {
		return hold, err
	}
	if err = tx.Commit(); err != nil {
		return hold, err
	}
	return hold, nil
}

func (s *SQLStorage) ReopenRecoveryEpisode(ctx context.Context, generation FundingCarryRuntimeGeneration, episodeID string, expectedRevision uint64, event accounting.RecoveryEvent, operationID string) (accounting.RecoveryHold, error) {
	if err := event.Validate(); err != nil {
		return accounting.RecoveryHold{}, err
	}
	if isRecoverySourceCursorEvent(event) {
		return accounting.RecoveryHold{}, errors.New("debt and funding events require a complete source window operation")
	}
	if event.EpisodeID != episodeID || operationID == "" {
		return accounting.RecoveryHold{}, errors.New("invalid late event reopen request")
	}
	requestBytes, err := json.Marshal(struct {
		EpisodeID string                   `json:"episode_id"`
		Revision  uint64                   `json:"expected_revision"`
		Event     accounting.RecoveryEvent `json:"event"`
	}{episodeID, expectedRevision, event})
	if err != nil {
		return accounting.RecoveryHold{}, err
	}
	requestHash := canonicalHash(requestBytes)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return accounting.RecoveryHold{}, err
	}
	defer tx.Rollback()
	episode, err := s.lockFundingRecoveryGenerationForEpisode(ctx, tx, generation, episodeID)
	if err != nil {
		return accounting.RecoveryHold{}, err
	}
	if event.EpisodeID != episodeID || !containsString(episode.AccountScopes, event.AccountScope) {
		return accounting.RecoveryHold{}, ErrFundingRecoveryGenerationScopeMismatch
	}
	var oldHash string
	var oldReceipt []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=?`, episodeID, operationID).Scan(&oldHash, &oldReceipt)
	if err == nil {
		if oldHash != requestHash {
			return accounting.RecoveryHold{}, accounting.ErrRecoveryOperationConflict
		}
		var receipt accounting.RecoveryReceipt
		if err = json.Unmarshal(oldReceipt, &receipt); err != nil {
			return accounting.RecoveryHold{}, err
		}
		var holdRaw []byte
		if err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_holds WHERE episode_id=? AND recovery_generation=?`, episodeID, receipt.RecoveryGeneration).Scan(&holdRaw); err != nil {
			return accounting.RecoveryHold{}, err
		}
		var hold accounting.RecoveryHold
		if err = json.Unmarshal(holdRaw, &hold); err != nil {
			return hold, err
		}
		return hold, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoveryHold{}, err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_episodes WHERE episode_id=? AND revision=? AND state=?`, episodeID, expectedRevision, string(accounting.RecoveryReleased)).Scan(&raw)
	if err != nil {
		return accounting.RecoveryHold{}, accounting.ErrRecoveryConflict
	}
	if err = json.Unmarshal(raw, &episode); err != nil {
		return accounting.RecoveryHold{}, err
	}
	episode.Revision++
	episode.RecoveryGeneration++
	episode.State = accounting.RecoveryReopened
	episode.UpdatedAt = time.Now().UTC()
	eventKey := recoveryExternalEventKey(event)
	eventJSON, _ := json.Marshal(event)
	if _, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_events(event_key,episode_id,event_id,event_revision,payload_json) VALUES(?,?,?,1,?)`, eventKey, episodeID, event.ID, eventJSON); err != nil {
		return accounting.RecoveryHold{}, recoveryConflict(err)
	}
	hold := recoveryHoldForEpisode(episode, operationID)
	holdJSON, _ := json.Marshal(hold)
	if _, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_holds(hold_id,episode_id,recovery_generation,hold_revision,state,payload_json) VALUES(?,?,?,?,?,?)`, hold.ID, episodeID, hold.Generation, hold.Revision, hold.State, holdJSON); err != nil {
		return accounting.RecoveryHold{}, recoveryConflict(err)
	}
	episodeJSON, _ := json.Marshal(episode)
	result, err := tx.ExecContext(ctx, `UPDATE funding_recovery_episodes SET revision=?,recovery_generation=?,state=?,payload_json=? WHERE episode_id=? AND revision=? AND state=?`, episode.Revision, episode.RecoveryGeneration, episode.State, episodeJSON, episodeID, expectedRevision, string(accounting.RecoveryReleased))
	if err != nil {
		return accounting.RecoveryHold{}, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return accounting.RecoveryHold{}, accounting.ErrRecoveryConflict
	}
	receipt := accounting.RecoveryReceipt{EpisodeID: episodeID, OperationID: operationID, RequestHash: requestHash, EpisodeRevision: episode.Revision, RecoveryGeneration: episode.RecoveryGeneration, State: episode.State, ProjectionHash: canonicalHash(eventJSON), CreatedAt: time.Now().UTC()}
	receiptJSON, _ := json.Marshal(receipt)
	if _, err = tx.ExecContext(ctx, `INSERT INTO funding_recovery_operations(episode_id,operation_id,request_hash,phase,receipt_json) VALUES(?,?,?,?,?)`, episodeID, operationID, requestHash, string(episode.State), receiptJSON); err != nil {
		return accounting.RecoveryHold{}, recoveryConflict(err)
	}
	if err = tx.Commit(); err != nil {
		return accounting.RecoveryHold{}, err
	}
	return hold, nil
}

func recoveryExternalEventKey(e accounting.RecoveryEvent) string {
	identity := []string{e.Source, e.Exchange, e.Market, e.AccountScope, string(e.Kind), e.ExternalID}
	if e.Symbol != "" {
		identity = append(identity, e.Symbol)
	}
	b, _ := json.Marshal(identity)
	return canonicalHash(b)
}

func recoverySourceCursorKey(identity accounting.RecoverySourceIdentity) string {
	encoded, _ := json.Marshal(identity)
	return canonicalHash(encoded)
}

func (s *SQLStorage) GetRecoverySourceCursor(ctx context.Context, identity accounting.RecoverySourceIdentity) (accounting.RecoverySourceCursor, bool, error) {
	if err := identity.Validate(); err != nil {
		return accounting.RecoverySourceCursor{}, false, err
	}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_source_cursors WHERE cursor_key=?`, recoverySourceCursorKey(identity)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return accounting.RecoverySourceCursor{}, false, nil
	}
	if err != nil {
		return accounting.RecoverySourceCursor{}, false, err
	}
	var cursor accounting.RecoverySourceCursor
	if err = json.Unmarshal(raw, &cursor); err != nil {
		return accounting.RecoverySourceCursor{}, false, err
	}
	return cursor, true, nil
}

func sameRecoveryChildOwner(a, b accounting.RecoveryChild) bool {
	return a.ID == b.ID && a.EpisodeID == b.EpisodeID && a.Role == b.Role && a.Exchange == b.Exchange && a.Market == b.Market && a.AccountScope == b.AccountScope && a.Bot == b.Bot && a.Symbol == b.Symbol && a.StrategyName == b.StrategyName && a.StrategyType == b.StrategyType && a.ClientOrderID == b.ClientOrderID && a.OrderIntentHash == b.OrderIntentHash && a.SourceIntentHash == b.SourceIntentHash && a.Side == b.Side
}
func canonicalHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// fundingRecoveryProjection is the durable, complete business projection
// protected by a recovery outbox hash. Keep its field order and tags stable:
// pending records written before semantic hashing used json.Marshal on this
// exact shape, and must remain recoverable after a database JSON round-trip.
type fundingRecoveryProjection struct {
	Episode                 accounting.RecoveryEpisode       `json:"episode"`
	Children                []accounting.RecoveryChild       `json:"children"`
	Events                  []accounting.RecoveryEvent       `json:"events"`
	SourceCursor            *accounting.RecoverySourceCursor `json:"source_cursor,omitempty"`
	ExpectedRuntimeRevision string                           `json:"expected_runtime_revision,omitempty"`
}

// VerifyFundingRecoveryReceiptOutboxPair checks that a durable receipt and its
// outbox describe the same operation and projection state. It is intentionally
// read-only so callers can run it before touching runtime state.
func VerifyFundingRecoveryReceiptOutboxPair(receipt accounting.RecoveryReceipt, outbox accounting.RecoveryOutbox) error {
	return verifyFundingRecoveryReceiptOutboxPair(receipt, outbox, 1)
}

// VerifyFundingRecoveryOwnerTakeoverReceipt binds the receipt to the exact
// audited takeover record and recomputes the original idempotency request hash.
func VerifyFundingRecoveryOwnerTakeoverReceipt(episode accounting.RecoveryEpisode, takeover accounting.RecoveryOwnerTakeoverRecord,
	receipt accounting.RecoveryReceipt) error {
	if episode.ID == "" || episode.RecoveryGeneration == 0 || takeover.EpisodeRevision == 0 || takeover.CreatedAt.IsZero() ||
		takeover.OperationID == "" || takeover.Actor == "" || takeover.AuditRef == "" || takeover.EvidenceHash == "" ||
		takeover.PreviousOwnerGeneration == "" || takeover.OwnerGeneration == "" || takeover.PreviousOwnerGeneration == takeover.OwnerGeneration ||
		receipt.EpisodeID != episode.ID || receipt.OperationID != takeover.OperationID || receipt.RequestHash == "" || receipt.CreatedAt.IsZero() ||
		receipt.Actor != takeover.Actor || receipt.AuditRef != takeover.AuditRef || receipt.EvidenceHash != takeover.EvidenceHash ||
		receipt.ProjectionHash != takeover.EvidenceHash || receipt.RecoveryGeneration != episode.RecoveryGeneration ||
		receipt.State != accounting.RecoveryBlocked || receipt.OwnerGeneration != takeover.OwnerGeneration ||
		receipt.PreviousOwnerGeneration != takeover.PreviousOwnerGeneration || receipt.EpisodeRevision != takeover.EpisodeRevision {
		return accounting.ErrRecoveryConflict
	}
	if takeover.EpisodeRevision > accounting.MaxDurableRecoveryRevision {
		return accounting.ErrRecoveryConflict
	}
	scopes, err := normalizeFundingCarryGenerationScopes(episode.AccountScopes)
	if err != nil {
		return err
	}
	authorization := accounting.RecoveryOwnerTakeoverAuthorization{EpisodeID: episode.ID, OperationID: takeover.OperationID,
		Actor: takeover.Actor, AuditRef: takeover.AuditRef, ExpectedOwnerGeneration: takeover.PreviousOwnerGeneration,
		ExpectedEpisodeRevision: takeover.EpisodeRevision - 1, Confirmed: true}
	expectedHash, err := recoveryOwnerTakeoverRequestHashForOwner(takeover.OwnerGeneration, scopes, authorization)
	if err != nil || expectedHash != receipt.RequestHash {
		return errors.Join(errors.New("owner takeover receipt request hash mismatch"), err)
	}
	return nil
}

// VerifyFundingRecoveryReceiptOutboxPairAfterVerifiedTakeover is for a caller
// that has already validated the exact owner-takeover record against the
// projected episode. It permits only the single additional episode revision
// created by that takeover; ordinary callers must use the strict helper above.
func VerifyFundingRecoveryReceiptOutboxPairAfterVerifiedTakeover(receipt, takeoverReceipt accounting.RecoveryReceipt,
	outbox accounting.RecoveryOutbox, episode accounting.RecoveryEpisode) error {
	if err := verifyFundingRecoveryReceiptOutboxPair(receipt, outbox, 2); err != nil {
		return err
	}
	if episode.ID != outbox.EpisodeID || episode.Revision < receipt.EpisodeRevision ||
		!episode.HasCurrentOwnerTakeover() {
		return errors.New("projected recovery receipt revision lacks a current owner takeover")
	}
	var projection fundingRecoveryProjection
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil {
		return err
	}
	takeover := episode.OwnerTakeovers[len(episode.OwnerTakeovers)-1]
	if outbox.TargetEpisodeRevision >= accounting.MaxDurableRecoveryRevision || takeover.EpisodeRevision != outbox.TargetEpisodeRevision+1 ||
		takeover.OwnerGeneration != episode.OwnerGeneration || takeover.OperationID == "" || takeover.Actor == "" || takeover.AuditRef == "" || takeover.EvidenceHash == "" ||
		takeover.PreviousOwnerGeneration == "" || takeover.PreviousOwnerGeneration == takeover.OwnerGeneration ||
		projection.Episode.ID != episode.ID || projection.Episode.Revision != outbox.TargetEpisodeRevision ||
		projection.Episode.OwnerGeneration != takeover.PreviousOwnerGeneration || projection.Episode.State != accounting.RecoveryLedgerCommitted ||
		receipt.OwnerGeneration != projection.Episode.OwnerGeneration {
		return errors.New("projected recovery receipt does not match the audited takeover record")
	}
	if err := VerifyFundingRecoveryOwnerTakeoverReceipt(episode, takeover, takeoverReceipt); err != nil {
		return fmt.Errorf("owner takeover receipt does not match the audited takeover record: %w", err)
	}
	prior := projection.Episode
	comparable := episode
	comparable.OwnerGeneration = prior.OwnerGeneration
	comparable.Revision = prior.Revision
	comparable.State = prior.State
	comparable.UpdatedAt = prior.UpdatedAt
	comparable.OwnerTakeovers = append([]accounting.RecoveryOwnerTakeoverRecord(nil), episode.OwnerTakeovers[:len(episode.OwnerTakeovers)-1]...)
	if !reflect.DeepEqual(comparable, prior) {
		return errors.New("projected recovery receipt takeover changed unrelated episode fields")
	}
	return nil
}

func verifyFundingRecoveryReceiptOutboxPair(receipt accounting.RecoveryReceipt, outbox accounting.RecoveryOutbox, projectedRevisionDelta uint64) error {
	if receipt.EpisodeID == "" || receipt.OperationID == "" || receipt.EpisodeID != outbox.EpisodeID ||
		receipt.OperationID != outbox.OperationID || receipt.ProjectionHash == "" ||
		receipt.ProjectionHash != outbox.ProjectionHash || outbox.TargetEpisodeRevision == 0 {
		return errors.New("recovery receipt/outbox identity or projection hash mismatch")
	}
	if err := VerifyFundingRecoveryProjectionHash(outbox.ProjectionPayload, outbox.ProjectionHash); err != nil {
		return fmt.Errorf("verify recovery receipt/outbox payload: %w", err)
	}
	if err := VerifyFundingRecoveryOutboxRuntimeRevision(outbox, outbox.ProjectionPayload); err != nil {
		return err
	}
	switch outbox.State {
	case accounting.RecoveryProjectionPending:
		if receipt.State != accounting.RecoveryLedgerCommitted || receipt.EpisodeRevision != outbox.TargetEpisodeRevision ||
			outbox.AppliedOperationID != "" || outbox.AppliedRevision != 0 || outbox.AppliedHash != "" {
			return errors.New("pending recovery outbox does not match ledger-committed receipt")
		}
	case accounting.RecoveryProjected:
		if receipt.State != accounting.RecoveryProjected || receipt.EpisodeRevision <= outbox.TargetEpisodeRevision ||
			receipt.EpisodeRevision-outbox.TargetEpisodeRevision != projectedRevisionDelta || outbox.AppliedOperationID != outbox.OperationID ||
			outbox.AppliedRevision != outbox.TargetEpisodeRevision || outbox.AppliedHash != outbox.ProjectionHash {
			return errors.New("projected recovery outbox does not match projected receipt")
		}
	default:
		return errors.New("recovery outbox has unsupported projection state")
	}
	return nil
}

func VerifyFundingRecoveryOutboxRuntimeRevision(outbox accounting.RecoveryOutbox, payload []byte) error {
	var projection fundingRecoveryProjection
	if err := json.Unmarshal(payload, &projection); err != nil {
		return err
	}
	if projection.ExpectedRuntimeRevision == "" {
		return errors.New("legacy recovery outbox runtime revision is not hash-bound; manual reconciliation is required")
	}
	if projection.ExpectedRuntimeRevision != outbox.ExpectedRuntimeRevision {
		return errors.New("recovery outbox runtime revision differs from its hashed projection")
	}
	return nil
}

// fundingRecoveryProjectionHash hashes canonical JSON semantics instead of
// serializer-specific bytes. MySQL's native JSON column may reorder object
// keys and rewrite numeric spelling while preserving the represented value.
func fundingRecoveryProjectionHash(payload []byte) (string, error) {
	canonical, err := canonicalRecoveryJSON(payload)
	if err != nil {
		return "", fmt.Errorf("canonicalize recovery projection: %w", err)
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// FundingRecoveryProjectionHash returns the semantic hash used by durable
// recovery outboxes. Callers creating replay fixtures or verifying exported
// evidence should use this rather than hashing serializer-specific bytes.
func FundingRecoveryProjectionHash(payload []byte) (string, error) {
	return fundingRecoveryProjectionHash(payload)
}

// VerifyFundingRecoveryProjectionHash accepts the current semantic hash and
// the legacy json.Marshal hash only when the stored payload is semantically
// identical to the complete legacy projection shape. The second check keeps
// already-persisted pending outboxes recoverable without trusting a hash
// detached from its payload; unknown or changed JSON fields are not discarded.
func VerifyFundingRecoveryProjectionHash(payload []byte, expected string) error {
	semanticHash, err := fundingRecoveryProjectionHash(payload)
	if err != nil {
		return err
	}
	// Native MySQL JSON readback is compact; surrounding whitespace here is a
	// caller-side byte change, not the database's object-key/number rewrite.
	if !bytes.Equal(bytes.TrimSpace(payload), payload) {
		return errors.New("legacy recovery projection byte hash mismatch")
	}

	var projection fundingRecoveryProjection
	if err = json.Unmarshal(payload, &projection); err != nil {
		return err
	}
	legacyPayload, err := json.Marshal(projection)
	if err != nil {
		return err
	}
	originalCanonical, err := canonicalRecoveryJSON(payload)
	if err != nil {
		return err
	}
	legacyCanonical, err := canonicalRecoveryJSON(legacyPayload)
	if err != nil {
		return err
	}
	legacyDigest := sha256.Sum256(legacyPayload)
	if bytes.Equal(originalCanonical, legacyCanonical) {
		if semanticHash == expected || hex.EncodeToString(legacyDigest[:]) == expected || canonicalHash(payload) == expected {
			return nil
		}
		return errors.New("legacy recovery projection hash mismatch")
	}

	// Older replay records may have been hashed directly from an equivalent
	// projection that omitted a nil children field. Accept that exact-byte
	// digest only when decoding and re-encoding the known schema (with that one
	// nil field omitted) proves there are no unrecognized or changed fields.
	legacyWithoutNilChildren, err := json.Marshal(struct {
		Episode      accounting.RecoveryEpisode       `json:"episode"`
		Children     []accounting.RecoveryChild       `json:"children,omitempty"`
		Events       []accounting.RecoveryEvent       `json:"events"`
		SourceCursor *accounting.RecoverySourceCursor `json:"source_cursor,omitempty"`
	}{projection.Episode, projection.Children, projection.Events, projection.SourceCursor})
	if err != nil {
		return err
	}
	legacyWithoutNilChildrenCanonical, err := canonicalRecoveryJSON(legacyWithoutNilChildren)
	if err != nil {
		return err
	}
	if projection.Children == nil && bytes.Equal(originalCanonical, legacyWithoutNilChildrenCanonical) && canonicalHash(legacyWithoutNilChildren) == expected {
		return nil
	}
	return errors.New("recovery projection contains fields outside the legacy hash shape")
}

// canonicalRecoveryJSON parses JSON without float64 conversion, rejects
// duplicate keys, sorts object members, preserves array order, and normalizes
// equivalent numeric spellings. This makes the digest stable across native
// JSON serializers without omitting any semantic payload content.
func canonicalRecoveryJSON(payload []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	value, err := readCanonicalRecoveryJSONValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("multiple JSON values")
		}
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func readCanonicalRecoveryJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				keyToken, keyErr := decoder.Token()
				if keyErr != nil {
					return nil, keyErr
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("recovery JSON object key is not a string")
				}
				if _, duplicate := object[key]; duplicate {
					return nil, fmt.Errorf("duplicate recovery JSON object key %q", key)
				}
				item, itemErr := readCanonicalRecoveryJSONValue(decoder)
				if itemErr != nil {
					return nil, itemErr
				}
				object[key] = item
			}
			if _, err = decoder.Token(); err != nil {
				return nil, err
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for decoder.More() {
				item, itemErr := readCanonicalRecoveryJSONValue(decoder)
				if itemErr != nil {
					return nil, itemErr
				}
				array = append(array, item)
			}
			if _, err = decoder.Token(); err != nil {
				return nil, err
			}
			return array, nil
		default:
			return nil, fmt.Errorf("unexpected recovery JSON delimiter %q", value)
		}
	case json.Number:
		number, numberErr := canonicalRecoveryJSONNumber(value.String())
		if numberErr != nil {
			return nil, numberErr
		}
		return json.Number(number), nil
	case string, bool, nil:
		return value, nil
	default:
		return nil, fmt.Errorf("unexpected recovery JSON token type %T", token)
	}
}

// canonicalRecoveryJSONNumber expresses a JSON number as an integer
// coefficient times a power of ten. It avoids floating point and therefore
// keeps large revisions and decimal evidence exact.
func canonicalRecoveryJSONNumber(number string) (string, error) {
	negative := strings.HasPrefix(number, "-")
	if negative {
		number = number[1:]
	}
	exponent := new(big.Int)
	if index := strings.IndexAny(number, "eE"); index >= 0 {
		if _, ok := exponent.SetString(number[index+1:], 10); !ok {
			return "", errors.New("invalid recovery JSON number exponent")
		}
		number = number[:index]
	}
	if point := strings.IndexByte(number, '.'); point >= 0 {
		fractionLength := big.NewInt(int64(len(number) - point - 1))
		number = number[:point] + number[point+1:]
		exponent.Sub(exponent, fractionLength)
	}
	digits := strings.TrimLeft(number, "0")
	if digits == "" {
		return "0e0", nil
	}
	for len(digits) > 1 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
		exponent.Add(exponent, big.NewInt(1))
	}
	prefix := ""
	if negative {
		prefix = "-"
	}
	return prefix + digits + "e" + exponent.String(), nil
}
func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
func containsChild(items []accounting.RecoveryChild, want string) bool {
	for _, item := range items {
		if item.ID == want {
			return true
		}
	}
	return false
}
func recoveryConflict(err error) error {
	if err == nil {
		return nil
	}
	var mysqlErr *mysqlDriver.MySQLError
	var sqliteErr sqliteDriver.Error
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return accounting.ErrRecoveryConflict
	}
	if errors.As(err, &sqliteErr) && (sqliteErr.ExtendedCode == sqliteDriver.ErrConstraintUnique || sqliteErr.ExtendedCode == sqliteDriver.ErrConstraintPrimaryKey) {
		return accounting.ErrRecoveryConflict
	}
	return err
}
