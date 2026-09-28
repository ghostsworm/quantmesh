package storage

import (
	"context"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	mysqlDriver "github.com/go-sql-driver/mysql"
	sqliteDriver "github.com/mattn/go-sqlite3"
	"quantmesh/execution"
)

//go:embed migrations/2026092402_execution_intents_*.sql
var executionIntentMigrations embed.FS

// MigrateExecutionIntents is explicit and is NOT called by application startup.
func (s *SQLStorage) MigrateExecutionIntents(ctx context.Context) error {
	if s.dbType != "sqlite" && s.dbType != "mysql" {
		return fmt.Errorf("unsupported intent journal database")
	}
	data, err := executionIntentMigrations.ReadFile("migrations/2026092402_execution_intents_" + s.dbType + ".up.sql")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, string(data))
	return err
}

func validIntentScopeKey(key string) bool {
	decoded, err := hex.DecodeString(key)
	return err == nil && len(decoded) == 32
}

func (s *SQLStorage) SaveExecutionIntent(ctx context.Context, key, cid string, expected int64, payload []byte) error {
	if !validIntentScopeKey(key) || cid == "" || len(cid) > 191 || expected < 0 || expected == math.MaxInt64 || !json.Valid(payload) {
		return fmt.Errorf("invalid execution intent write")
	}
	var result sql.Result
	var err error
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if s.dbType == "sqlite" {
		// A write-ahead intent must survive a host crash, not just a process
		// restart. Do not inherit the legacy NORMAL synchronous setting.
		if _, err := conn.ExecContext(ctx, `PRAGMA synchronous = FULL`); err != nil {
			return err
		}
	}
	if expected == 0 {
		result, err = conn.ExecContext(ctx, `INSERT INTO execution_intents (scope_key, client_order_id, revision, state_json) VALUES (?, ?, 1, ?)`, key, cid, string(payload))
	} else {
		result, err = conn.ExecContext(ctx, `UPDATE execution_intents SET state_json = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP WHERE scope_key = ? AND client_order_id = ? AND revision = ?`, string(payload), key, cid, expected)
	}
	if err != nil {
		var my *mysqlDriver.MySQLError
		var sq sqliteDriver.Error
		if (errors.As(err, &my) && my.Number == 1062) || (errors.As(err, &sq) && sq.ExtendedCode == sqliteDriver.ErrConstraintUnique) {
			return execution.ErrIntentJournalConflict
		}
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return execution.ErrIntentJournalConflict
	}
	return nil
}

// Legacy rows lack a complete owner/cumulative settlement checkpoint. Their
// existence requires an explicit migration/reconciliation, never route guessing.
func (s *SQLStorage) HasLegacyExecutionHistory(ctx context.Context, bot, exchange, symbol string) (bool, error) {
	if bot == "" || exchange == "" || symbol == "" {
		return false, fmt.Errorf("incomplete legacy history scope")
	}
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM orders WHERE exchange = ? AND symbol = ? AND (bot_id = ? OR bot_id = '' OR bot_id IS NULL) LIMIT 1`, exchange, symbol, bot).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return found == 1, err
}

func (s *SQLStorage) LoadExecutionIntents(ctx context.Context, key string, after int64, limit int) ([]execution.IntentJournalRecord, error) {
	if !validIntentScopeKey(key) || after < 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("invalid intent journal page")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, client_order_id, revision, state_json FROM execution_intents WHERE scope_key = ? AND id > ? ORDER BY id ASC LIMIT ?`, key, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []execution.IntentJournalRecord
	for rows.Next() {
		var r execution.IntentJournalRecord
		if err := rows.Scan(&r.ID, &r.ClientOrderID, &r.Revision, &r.Payload); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
