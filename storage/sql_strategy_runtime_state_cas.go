package storage

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type StrategyRuntimeStateConditionalWriter interface {
	CompareAndSwapStrategyRuntimeState(context.Context, *StrategyRuntimeState, int, string) (bool, error)
}

// CompareAndSwapStrategyRuntimeState never inserts a missing checkpoint.
func (s *SQLStorage) CompareAndSwapStrategyRuntimeState(ctx context.Context, next *StrategyRuntimeState, expectedVersion int, expectedPayload string) (bool, error) {
	if ctx == nil || next == nil || strings.TrimSpace(next.BotID) == "" || strings.TrimSpace(next.StrategyName) == "" || next.SchemaVersion <= 0 || expectedVersion <= 0 || strings.TrimSpace(next.Payload) == "" {
		return false, fmt.Errorf("conditional runtime state write requires context, identity and valid snapshots")
	}
	query := `UPDATE strategy_runtime_states SET schema_version = ?, payload = ?, updated_at = ?
		WHERE bot_id = ? AND strategy_name = ? AND schema_version = ? AND payload = ?`
	if s.dbType == "mysql" {
		// The table collation is case-insensitive; provenance must be byte-exact.
		query = `UPDATE strategy_runtime_states SET schema_version = ?, payload = ?, updated_at = ?
			WHERE bot_id = ? AND strategy_name = ? AND schema_version = ? AND BINARY payload = BINARY ?`
	}
	result, err := s.db.ExecContext(ctx, query, next.SchemaVersion, next.Payload, time.Now().UTC(), next.BotID, next.StrategyName, expectedVersion, expectedPayload)
	if err != nil {
		return false, fmt.Errorf("conditional runtime state write: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("conditional runtime state affected rows: %w", err)
	}
	if rows == 0 && s.dbType == "mysql" && next.SchemaVersion == expectedVersion && next.Payload == expectedPayload {
		// MySQL reports changed rows, not matched rows. An identical target
		// and millisecond diagnostic time may therefore produce zero. Compare
		// the exact current source atomically at this SELECT snapshot; never
		// retry a mutation, insert a row, or accept a changed-target zero count.
		var matches bool
		err := s.db.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM strategy_runtime_states WHERE bot_id = ? AND strategy_name = ?
			AND schema_version = ? AND BINARY payload = BINARY ?)`, next.BotID, next.StrategyName, expectedVersion, expectedPayload).Scan(&matches)
		if err != nil {
			return false, fmt.Errorf("compare identical runtime state source: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("identical runtime state comparison canceled: %w", err)
		}
		return matches, nil
	}
	return rows == 1, nil
}
