package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// StrategyRuntimeStateStore is intentionally separate from Storage so optional
// state persistence does not break external Storage implementations.
type StrategyRuntimeStateStore interface {
	GetStrategyRuntimeState(botID, strategyName string) (*StrategyRuntimeState, error)
	SetStrategyRuntimeState(state *StrategyRuntimeState) error
}

func migrateStrategyRuntimeStateTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS strategy_runtime_states (
		bot_id TEXT NOT NULL,
		strategy_name TEXT NOT NULL,
		schema_version INTEGER NOT NULL,
		payload TEXT NOT NULL,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (bot_id, strategy_name)
	)`)
	return err
}

func migrateStrategyRuntimeStateTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS strategy_runtime_states (
		bot_id VARCHAR(255) NOT NULL,
		strategy_name VARCHAR(128) NOT NULL,
		schema_version INT NOT NULL,
		payload LONGTEXT NOT NULL,
		updated_at DATETIME(3) NOT NULL,
		PRIMARY KEY (bot_id, strategy_name)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	return err
}

func (s *SQLStorage) GetStrategyRuntimeState(botID, strategyName string) (*StrategyRuntimeState, error) {
	botID = strings.TrimSpace(botID)
	strategyName = strings.TrimSpace(strategyName)
	if botID == "" || strategyName == "" {
		return nil, fmt.Errorf("bot_id and strategy_name are required")
	}
	var state StrategyRuntimeState
	var updatedAt time.Time
	err := s.db.QueryRow(`SELECT bot_id, strategy_name, schema_version, payload, updated_at
		FROM strategy_runtime_states WHERE bot_id = ? AND strategy_name = ?`, botID, strategyName).
		Scan(&state.BotID, &state.StrategyName, &state.SchemaVersion, &state.Payload, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load strategy runtime state %s/%s: %w", botID, strategyName, err)
	}
	state.UpdatedAt = updatedAt
	return &state, nil
}

func (s *SQLStorage) SetStrategyRuntimeState(state *StrategyRuntimeState) error {
	if state == nil {
		return fmt.Errorf("strategy runtime state is required")
	}
	state.BotID = strings.TrimSpace(state.BotID)
	state.StrategyName = strings.TrimSpace(state.StrategyName)
	if state.BotID == "" || state.StrategyName == "" || state.SchemaVersion <= 0 || strings.TrimSpace(state.Payload) == "" {
		return fmt.Errorf("bot_id, strategy_name, positive schema_version, and payload are required")
	}
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	query := `INSERT INTO strategy_runtime_states (bot_id, strategy_name, schema_version, payload, updated_at)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT(bot_id, strategy_name) DO UPDATE SET
		schema_version=excluded.schema_version, payload=excluded.payload, updated_at=excluded.updated_at`
	if s.dbType == "mysql" {
		query = `INSERT INTO strategy_runtime_states (bot_id, strategy_name, schema_version, payload, updated_at)
			VALUES (?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE
			schema_version=VALUES(schema_version), payload=VALUES(payload), updated_at=VALUES(updated_at)`
	}
	if _, err := s.db.Exec(query, state.BotID, state.StrategyName, state.SchemaVersion, state.Payload, state.UpdatedAt.UTC()); err != nil {
		return fmt.Errorf("save strategy runtime state %s/%s: %w", state.BotID, state.StrategyName, err)
	}
	return nil
}
