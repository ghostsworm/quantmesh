package storage

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	mysqlDriver "github.com/go-sql-driver/mysql"
	sqliteDriver "github.com/mattn/go-sqlite3"
)

//go:embed migrations/2026092401_risk_checkpoints_*.sql
var riskCheckpointMigrations embed.FS

var ErrRiskCheckpointConflict = errors.New("risk checkpoint revision conflict")

// MigrateRiskCheckpoints is an EXPLICIT schema change, never called by normal
// application startup. Production requires operator approval and a backup;
// the versioned up/down SQL is also available for the deployment migration step.
func (s *SQLStorage) MigrateRiskCheckpoints(ctx context.Context) error {
	return migrateRiskCheckpointsContext(ctx, s.db, s.dbType)
}

func migrateRiskCheckpoints(db *sql.DB, dbType string) error {
	return migrateRiskCheckpointsContext(context.Background(), db, dbType)
}

func migrateRiskCheckpointsContext(ctx context.Context, db *sql.DB, dbType string) error {
	if dbType != "sqlite" && dbType != "mysql" {
		return fmt.Errorf("unsupported risk checkpoint database")
	}
	data, err := riskCheckpointMigrations.ReadFile("migrations/2026092401_risk_checkpoints_" + dbType + ".up.sql")
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, string(data))
	return err
}

// LoadRiskCheckpoint distinguishes a confirmed absent checkpoint from errors.
func (s *SQLStorage) LoadRiskCheckpoint(ctx context.Context, key string) ([]byte, int64, error) {
	var payload []byte
	var revision int64
	err := s.db.QueryRowContext(ctx, `SELECT state_json, revision FROM risk_checkpoints WHERE checkpoint_key = ?`, key).Scan(&payload, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, nil
	}
	return payload, revision, err
}

// SaveRiskCheckpoint is an atomic compare-and-swap. A failed/uncertain write
// must be reloaded before retry; it must never overwrite a newer high water.
func (s *SQLStorage) SaveRiskCheckpoint(ctx context.Context, key string, expected int64, payload []byte) error {
	if key == "" || len(key) > 191 || expected < 0 || expected == math.MaxInt64 || !json.Valid(payload) {
		return fmt.Errorf("invalid risk checkpoint write")
	}
	var result sql.Result
	var err error
	if expected == 0 {
		query := `INSERT INTO risk_checkpoints (checkpoint_key, revision, state_json) VALUES (?, 1, ?)`
		result, err = s.db.ExecContext(ctx, query, key, string(payload))
	} else {
		result, err = s.db.ExecContext(ctx, `UPDATE risk_checkpoints SET state_json = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP WHERE checkpoint_key = ? AND revision = ?`, string(payload), key, expected)
	}
	if err != nil {
		var mysqlErr *mysqlDriver.MySQLError
		var sqliteErr sqliteDriver.Error
		if (errors.As(err, &mysqlErr) && mysqlErr.Number == 1062) || (errors.As(err, &sqliteErr) && sqliteErr.ExtendedCode == sqliteDriver.ErrConstraintUnique) {
			return ErrRiskCheckpointConflict
		}
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRiskCheckpointConflict
	}
	return nil
}
