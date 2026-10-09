package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrFundingCarryRuntimeGenerationLost               = errors.New("funding_carry runtime generation is no longer current")
	ErrFundingCarryRuntimeStateCommitOutcomeUnknown    = errors.New("funding_carry runtime state commit outcome is unknown")
	ErrFundingCarryRuntimeStateCommitConfirmedCanceled = errors.New("funding_carry runtime state commit confirmed after caller cancellation")
)

const fundingCarryRuntimeCommitProbeTimeout = 5 * time.Second

type FundingCarryRuntimeGeneration struct {
	scopeKeys  []string
	ownerToken string
}

type FundingCarryRuntimeGenerationStore interface {
	ClaimFundingCarryRuntimeGeneration(context.Context, []string) (FundingCarryRuntimeGeneration, error)
	SetFundingCarryRuntimeState(context.Context, FundingCarryRuntimeGeneration, *StrategyRuntimeState) error
	CompareAndSwapFundingCarryRuntimeState(context.Context, FundingCarryRuntimeGeneration, *StrategyRuntimeState, int, string) (bool, error)
}

func migrateFundingCarryRuntimeGeneration(db *sql.DB, dialect string) error {
	if dialect != "sqlite" && dialect != "mysql" {
		return fmt.Errorf("unsupported funding_carry generation database")
	}
	for _, migration := range []string{
		"migrations/2026100701_funding_carry_runtime_generation_" + dialect + ".up.sql",
		"migrations/2026100702_funding_carry_runtime_state_receipts_" + dialect + ".up.sql",
	} {
		data, err := fundingCarryRuntimeGenerationMigrations.ReadFile(migration)
		if err != nil {
			return fmt.Errorf("read funding_carry runtime migration %s: %w", migration, err)
		}
		if _, err := db.Exec(string(data)); err != nil {
			return fmt.Errorf("apply funding_carry runtime migration %s: %w", migration, err)
		}
	}
	return nil
}

//go:embed migrations/2026100701_funding_carry_runtime_generation_*.sql migrations/2026100702_funding_carry_runtime_state_receipts_*.sql
var fundingCarryRuntimeGenerationMigrations embed.FS

func normalizeFundingCarryGenerationScopes(scopeKeys []string) ([]string, error) {
	if len(scopeKeys) == 0 {
		return nil, fmt.Errorf("funding_carry runtime generation requires ownership scopes")
	}
	keys := append([]string(nil), scopeKeys...)
	for _, key := range keys {
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 32 || strings.ToLower(key) != key {
			return nil, fmt.Errorf("funding_carry runtime generation has invalid ownership scope")
		}
	}
	sort.Strings(keys)
	for i := 1; i < len(keys); i++ {
		if keys[i] == keys[i-1] {
			return nil, fmt.Errorf("funding_carry runtime generation has duplicate ownership scope")
		}
	}
	return keys, nil
}

func (s *SQLStorage) ClaimFundingCarryRuntimeGeneration(ctx context.Context, scopeKeys []string) (FundingCarryRuntimeGeneration, error) {
	keys, err := normalizeFundingCarryGenerationScopes(scopeKeys)
	if err != nil {
		return FundingCarryRuntimeGeneration{}, fmt.Errorf("claim funding_carry runtime generation: %w", err)
	}
	if ctx == nil || s == nil || s.db == nil {
		return FundingCarryRuntimeGeneration{}, fmt.Errorf("claim funding_carry runtime generation: storage and context are required")
	}
	token, err := newFundingCarryRuntimeID()
	if err != nil {
		return FundingCarryRuntimeGeneration{}, fmt.Errorf("create funding_carry runtime generation token: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return FundingCarryRuntimeGeneration{}, fmt.Errorf("begin funding_carry runtime generation claim: %w", err)
	}
	defer tx.Rollback()
	for _, key := range keys {
		if s.dbType == "mysql" {
			_, err = tx.ExecContext(ctx, `INSERT INTO funding_carry_runtime_generations (scope_key, generation, owner_token, updated_at) VALUES (?, 0, '', ?) ON DUPLICATE KEY UPDATE scope_key=VALUES(scope_key)`, key, time.Now().UTC())
		} else {
			_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO funding_carry_runtime_generations (scope_key, generation, owner_token) VALUES (?, 0, '')`, key)
		}
		if err != nil {
			return FundingCarryRuntimeGeneration{}, fmt.Errorf("initialize funding_carry runtime generation scope: %w", err)
		}
	}
	for _, key := range keys {
		if _, err = tx.ExecContext(ctx, `UPDATE funding_carry_runtime_generations SET generation = generation + 1, owner_token = ?, updated_at = ? WHERE scope_key = ?`, token, time.Now().UTC(), key); err != nil {
			return FundingCarryRuntimeGeneration{}, fmt.Errorf("advance funding_carry runtime generation: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return FundingCarryRuntimeGeneration{}, fmt.Errorf("commit funding_carry runtime generation claim: %w", err)
	}
	return FundingCarryRuntimeGeneration{scopeKeys: keys, ownerToken: token}, nil
}

func newFundingCarryRuntimeID() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (s *SQLStorage) lockFundingCarryRuntimeGeneration(ctx context.Context, tx *sql.Tx, generation FundingCarryRuntimeGeneration) error {
	keys, err := normalizeFundingCarryGenerationScopes(generation.scopeKeys)
	if err != nil || generation.ownerToken == "" || len(generation.ownerToken) != 64 {
		return fmt.Errorf("invalid funding_carry runtime generation")
	}
	if s.dbType != "mysql" && s.dbType != "sqlite" {
		return fmt.Errorf("unsupported funding_carry runtime generation database")
	}
	for _, key := range keys {
		// The UPDATE is the first transactional statement: it obtains SQLite's
		// single-writer reservation and locks the InnoDB row before validation.
		if _, err := tx.ExecContext(ctx, `UPDATE funding_carry_runtime_generations SET owner_token = owner_token WHERE scope_key = ?`, key); err != nil {
			return fmt.Errorf("lock funding_carry runtime generation scope: %w", err)
		}
		var currentToken string
		var currentGeneration int64
		query := `SELECT owner_token, generation FROM funding_carry_runtime_generations WHERE scope_key = ?`
		if s.dbType == "mysql" {
			query += ` FOR UPDATE`
		}
		if err := tx.QueryRowContext(ctx, query, key).Scan(&currentToken, &currentGeneration); err != nil {
			return fmt.Errorf("read funding_carry runtime generation scope: %w", err)
		}
		if currentToken != generation.ownerToken || currentGeneration <= 0 {
			return ErrFundingCarryRuntimeGenerationLost
		}
	}
	return nil
}

func (s *SQLStorage) SetFundingCarryRuntimeState(ctx context.Context, generation FundingCarryRuntimeGeneration, state *StrategyRuntimeState) error {
	if s == nil || s.db == nil || ctx == nil || state == nil || strings.TrimSpace(state.BotID) == "" || strings.TrimSpace(state.StrategyName) == "" || state.SchemaVersion <= 0 || strings.TrimSpace(state.Payload) == "" {
		return fmt.Errorf("funding_carry runtime state write requires context, identity and valid snapshot")
	}
	writeID, err := newFundingCarryRuntimeID()
	if err != nil {
		return fmt.Errorf("create funding_carry runtime state write ID: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin fenced funding_carry runtime state write: %w", err)
	}
	defer tx.Rollback()
	if err := s.lockFundingCarryRuntimeGeneration(ctx, tx, generation); err != nil {
		return err
	}
	if s.dbType == "mysql" {
		_, err = tx.ExecContext(ctx, `INSERT INTO strategy_runtime_states (bot_id, strategy_name, schema_version, payload, updated_at) VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE schema_version=VALUES(schema_version), payload=VALUES(payload), updated_at=VALUES(updated_at)`, state.BotID, state.StrategyName, state.SchemaVersion, state.Payload, time.Now().UTC())
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO strategy_runtime_states (bot_id, strategy_name, schema_version, payload, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(bot_id, strategy_name) DO UPDATE SET schema_version=excluded.schema_version, payload=excluded.payload, updated_at=excluded.updated_at`, state.BotID, state.StrategyName, state.SchemaVersion, state.Payload, time.Now().UTC())
	}
	if err != nil {
		return fmt.Errorf("write fenced funding_carry runtime state: %w", err)
	}
	if err := s.writeFundingCarryRuntimeStateReceipt(ctx, tx, generation, state, writeID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		confirmed, probeErr := s.confirmFundingCarryRuntimeStateCommit(generation, state, writeID)
		if confirmed {
			if contextErr := ctx.Err(); contextErr != nil {
				return errors.Join(ErrFundingCarryRuntimeStateCommitConfirmedCanceled, contextErr)
			}
			return nil
		}
		unknownErr := fmt.Errorf("%w: commit was not positively confirmed", ErrFundingCarryRuntimeStateCommitOutcomeUnknown)
		commitErr := fmt.Errorf("commit fenced funding_carry runtime state: %w", err)
		if probeErr != nil {
			return errors.Join(commitErr, unknownErr, probeErr)
		}
		return errors.Join(commitErr, unknownErr)
	}
	if _, err := fundingCarryRuntimeCommitResult(ctx, true); err != nil {
		return err
	}
	return nil
}

func (s *SQLStorage) CompareAndSwapFundingCarryRuntimeState(ctx context.Context, generation FundingCarryRuntimeGeneration, next *StrategyRuntimeState, expectedVersion int, expectedPayload string) (bool, error) {
	if s == nil || s.db == nil || ctx == nil || next == nil || strings.TrimSpace(next.BotID) == "" || strings.TrimSpace(next.StrategyName) == "" || next.SchemaVersion <= 0 || expectedVersion <= 0 || strings.TrimSpace(next.Payload) == "" {
		return false, fmt.Errorf("fenced conditional runtime state write requires context, identity and valid snapshots")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return false, fmt.Errorf("begin fenced conditional runtime state write: %w", err)
	}
	defer tx.Rollback()
	if err := s.lockFundingCarryRuntimeGeneration(ctx, tx, generation); err != nil {
		return false, err
	}
	query := `UPDATE strategy_runtime_states SET schema_version = ?, payload = ?, updated_at = ? WHERE bot_id = ? AND strategy_name = ? AND schema_version = ? AND payload = ?`
	if s.dbType == "mysql" {
		query = `UPDATE strategy_runtime_states SET schema_version = ?, payload = ?, updated_at = ? WHERE bot_id = ? AND strategy_name = ? AND schema_version = ? AND BINARY payload = BINARY ?`
	}
	result, err := tx.ExecContext(ctx, query, next.SchemaVersion, next.Payload, time.Now().UTC(), next.BotID, next.StrategyName, expectedVersion, expectedPayload)
	if err != nil {
		return false, fmt.Errorf("conditional fenced runtime state write: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("conditional fenced runtime state affected rows: %w", err)
	}
	saved := rows == 1
	if rows == 0 && s.dbType == "mysql" && next.SchemaVersion == expectedVersion && next.Payload == expectedPayload {
		var matches bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM strategy_runtime_states WHERE bot_id = ? AND strategy_name = ? AND schema_version = ? AND BINARY payload = BINARY ?)`, next.BotID, next.StrategyName, expectedVersion, expectedPayload).Scan(&matches)
		if err != nil {
			return false, fmt.Errorf("compare identical fenced runtime state source: %w", err)
		}
		saved = matches
	}
	if !saved {
		return false, nil
	}
	writeID, err := newFundingCarryRuntimeID()
	if err != nil {
		return false, fmt.Errorf("create funding_carry runtime state write ID: %w", err)
	}
	if err := s.writeFundingCarryRuntimeStateReceipt(ctx, tx, generation, next, writeID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		confirmed, probeErr := s.confirmFundingCarryRuntimeStateCommit(generation, next, writeID)
		if !confirmed {
			unknownErr := fmt.Errorf("%w: commit was not positively confirmed", ErrFundingCarryRuntimeStateCommitOutcomeUnknown)
			commitErr := fmt.Errorf("commit fenced conditional runtime state write: %w", err)
			if probeErr != nil {
				return false, errors.Join(commitErr, unknownErr, probeErr)
			}
			return false, errors.Join(commitErr, unknownErr)
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return false, errors.Join(ErrFundingCarryRuntimeStateCommitConfirmedCanceled, contextErr)
		}
		return true, nil
	}
	return fundingCarryRuntimeCommitResult(ctx, saved)
}

// A nil Commit result confirms the transaction, but callers may have been
// canceled while the driver was committing. Preserve that boundary so a
// pre-submit strategy intent can recover only this positively committed write.
func fundingCarryRuntimeCommitResult(ctx context.Context, saved bool) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("funding_carry runtime commit result requires context")
	}
	if contextErr := ctx.Err(); contextErr != nil {
		if saved {
			return false, errors.Join(ErrFundingCarryRuntimeStateCommitConfirmedCanceled, contextErr)
		}
		return false, contextErr
	}
	return saved, nil
}

func (s *SQLStorage) writeFundingCarryRuntimeStateReceipt(ctx context.Context, tx *sql.Tx, generation FundingCarryRuntimeGeneration, state *StrategyRuntimeState, writeID string) error {
	query := `INSERT INTO funding_carry_runtime_state_write_receipts (bot_id, strategy_name, write_id, owner_token) VALUES (?, ?, ?, ?)
		ON CONFLICT(bot_id, strategy_name) DO UPDATE SET write_id = excluded.write_id, owner_token = excluded.owner_token`
	if s.dbType == "mysql" {
		query = `INSERT INTO funding_carry_runtime_state_write_receipts (bot_id, strategy_name, write_id, owner_token) VALUES (?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE write_id = VALUES(write_id), owner_token = VALUES(owner_token)`
	}
	if _, err := tx.ExecContext(ctx, query, state.BotID, state.StrategyName, writeID, generation.ownerToken); err != nil {
		return fmt.Errorf("write fenced funding_carry runtime state receipt: %w", err)
	}
	return nil
}

// confirmFundingCarryRuntimeStateCommit confirms only this exact write ID, the
// same generation owner for every claimed scope, and the requested snapshot.
// One SELECT gives MySQL and SQLite a single consistent read snapshot.
func (s *SQLStorage) confirmFundingCarryRuntimeStateCommit(generation FundingCarryRuntimeGeneration, state *StrategyRuntimeState, writeID string) (bool, error) {
	probeCtx, cancel := context.WithTimeout(context.Background(), fundingCarryRuntimeCommitProbeTimeout)
	defer cancel()
	if s == nil || s.commitProbe == nil {
		return false, fmt.Errorf("independent funding_carry runtime commit confirmation pool is unavailable")
	}
	keys, err := normalizeFundingCarryGenerationScopes(generation.scopeKeys)
	if err != nil || generation.ownerToken == "" || state == nil {
		return false, fmt.Errorf("invalid funding_carry runtime state commit confirmation")
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
	query := `SELECT g.scope_key, g.owner_token, g.generation, state.schema_version, state.payload,
		receipt.write_id, receipt.owner_token
		FROM funding_carry_runtime_generations AS g
		LEFT JOIN strategy_runtime_states AS state ON state.bot_id = ? AND state.strategy_name = ?
		LEFT JOIN funding_carry_runtime_state_write_receipts AS receipt ON receipt.bot_id = state.bot_id AND receipt.strategy_name = state.strategy_name
		WHERE g.scope_key IN (` + placeholders + `)`
	args := make([]any, 0, len(keys)+2)
	args = append(args, state.BotID, state.StrategyName)
	for _, key := range keys {
		args = append(args, key)
	}
	rows, err := s.commitProbe.QueryContext(probeCtx, query, args...)
	if err != nil {
		return false, fmt.Errorf("read fenced funding_carry runtime commit evidence: %w", err)
	}
	defer rows.Close()
	seen := make(map[string]struct{}, len(keys))
	for rows.Next() {
		var scopeKey, ownerToken string
		var currentGeneration int64
		var schemaVersion sql.NullInt64
		var payload, receiptID, receiptOwner sql.NullString
		if err := rows.Scan(&scopeKey, &ownerToken, &currentGeneration, &schemaVersion, &payload, &receiptID, &receiptOwner); err != nil {
			return false, fmt.Errorf("scan fenced funding_carry runtime commit evidence: %w", err)
		}
		if _, duplicate := seen[scopeKey]; duplicate {
			return false, fmt.Errorf("duplicate ownership scope in funding_carry runtime commit evidence")
		}
		seen[scopeKey] = struct{}{}
		if ownerToken != generation.ownerToken || currentGeneration <= 0 || !schemaVersion.Valid ||
			schemaVersion.Int64 != int64(state.SchemaVersion) || !payload.Valid || payload.String != state.Payload ||
			!receiptID.Valid || receiptID.String != writeID || !receiptOwner.Valid || receiptOwner.String != generation.ownerToken {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate fenced funding_carry runtime commit evidence: %w", err)
	}
	return len(seen) == len(keys), nil
}
