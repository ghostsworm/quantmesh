package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// The daily equity schema is additive and independent of the existing fill,
// funding and interest ledgers. It stores only historical evidence versions,
// immutable report versions, the current pointer and persisted conflicts.
//
//go:embed migrations/2026110501_daily_equity_*.sql
var dailyEquityMigrations embed.FS

const (
	dailyEquityMySQLLockName  = "quantmesh:daily_equity_schema_v1"
	dailyEquityLockTimeout    = 30
	dailyEquitySQLiteBusyMS   = 100
	dailyEquitySQLiteRetry    = 25 * time.Millisecond
	dailyEquitySQLiteLockWait = 30 * time.Second
)

// dailyEquityTableSchemas lists the columns every daily equity table must have.
// The migration uses CREATE TABLE IF NOT EXISTS, so an incompatible table that
// predates this feature would otherwise be accepted silently. Probing the
// columns makes both startup and the status check fail closed instead.
var dailyEquityTableSchemas = []struct {
	table   string
	columns []string
}{
	{
		table: "daily_equity_evidence_versions",
		columns: []string{"id", "exchange", "account_scope", "local_date", "revision", "scope_revision",
			"timezone", "method", "evidence_hash", "payload_json", "created_at", "updated_at"},
	},
	{
		table: "daily_equity_report_versions",
		columns: []string{"id", "exchange", "account_scope", "local_date", "revision", "evidence_revision",
			"input_digest", "method", "equity_reconciliation_complete", "report_json", "created_at", "updated_at"},
	},
	{
		table: "daily_equity_current",
		columns: []string{"exchange", "account_scope", "local_date", "evidence_revision", "evidence_hash",
			"report_revision", "report_input_digest", "conflicted", "created_at", "updated_at"},
	},
	{
		table: "daily_equity_conflicts",
		columns: []string{"id", "exchange", "account_scope", "local_date", "source", "source_id", "wallet",
			"reason", "existing_fingerprint", "incoming_fingerprint", "created_at", "updated_at"},
	},
}

func (s *SQLStorage) dailyEquityValidateBackend() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("daily equity storage is unavailable")
	}
	switch s.dbType {
	case "sqlite", "mysql":
		return nil
	default:
		return fmt.Errorf("daily equity storage does not support database type %q", s.dbType)
	}
}

func dailyEquityMigrationFile(dbType, direction string) (string, error) {
	if direction != "up" && direction != "down" {
		return "", fmt.Errorf("daily equity migration direction must be up or down")
	}
	switch dbType {
	case "sqlite", "mysql":
		return fmt.Sprintf("migrations/2026110501_daily_equity_%s.%s.sql", dbType, direction), nil
	default:
		return "", fmt.Errorf("daily equity migration does not support database type %q", dbType)
	}
}

// MigrateDailyEquity installs the additive schema with the same single-connection
// lock discipline as the existing reconciliation and recovery migrations. It is
// idempotent and is called from NewStorageService; a failure must stop startup.
func (s *SQLStorage) MigrateDailyEquity(ctx context.Context) error {
	if err := s.dailyEquityValidateBackend(); err != nil {
		return err
	}
	if err := s.runDailyEquityMigration(ctx, "up"); err != nil {
		return err
	}
	return s.validateDailyEquitySchema(ctx)
}

// RollbackDailyEquityMigration drops only the daily equity schema. It is never
// invoked by startup; callers must explicitly opt in on a disposable database.
func (s *SQLStorage) RollbackDailyEquityMigration(ctx context.Context) error {
	if err := s.dailyEquityValidateBackend(); err != nil {
		return err
	}
	return s.runDailyEquityMigration(ctx, "down")
}

// DailyEquityMigrationStatus reports whether every daily equity table exists
// with the required columns. A partial or incompatible schema is reported as
// false, not as ready.
func (s *SQLStorage) DailyEquityMigrationStatus(ctx context.Context) (bool, error) {
	if err := s.dailyEquityValidateBackend(); err != nil {
		return false, err
	}
	tables := make([]string, 0, len(dailyEquityTableSchemas))
	for _, schema := range dailyEquityTableSchemas {
		tables = append(tables, schema.table)
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tables)), ",")
	args := make([]any, 0, len(tables))
	for _, table := range tables {
		args = append(args, table)
	}
	var count int
	var err error
	if s.dbType == "mysql" {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name IN (`+placeholders+`)`, args...).Scan(&count)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN (`+placeholders+`)`, args...).Scan(&count)
	}
	if err != nil {
		return false, fmt.Errorf("read daily equity migration status: %w", err)
	}
	if count != len(tables) {
		return false, nil
	}
	if err := s.validateDailyEquitySchema(ctx); err != nil {
		return false, nil
	}
	return true, nil
}

// validateDailyEquitySchema probes every required column. Table names and
// columns come from the fixed list above, never from caller input.
func (s *SQLStorage) validateDailyEquitySchema(ctx context.Context) error {
	for _, schema := range dailyEquityTableSchemas {
		query := "SELECT " + strings.Join(schema.columns, ", ") + " FROM " + schema.table + " WHERE 1 = 0"
		rows, err := s.db.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("daily equity schema %s is missing or incompatible: %w", schema.table, err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close daily equity schema probe for %s: %w", schema.table, err)
		}
	}
	return nil
}

func (s *SQLStorage) runDailyEquityMigration(ctx context.Context, direction string) error {
	connCtx := ctx
	var cancel context.CancelFunc
	if s.dbType == "sqlite" {
		connCtx, cancel = context.WithTimeout(ctx, dailyEquitySQLiteLockWait)
		defer cancel()
	}
	conn, err := acquireDailyEquityConn(connCtx, s.db, s.dbType)
	if err != nil {
		return fmt.Errorf("connect for daily equity migration: %w", err)
	}
	defer conn.Close()
	if s.dbType == "mysql" {
		return s.runDailyEquityMigrationMySQL(ctx, conn, direction)
	}
	return s.runDailyEquityMigrationSQLite(ctx, conn, direction)
}

func acquireDailyEquityConn(ctx context.Context, db *sql.DB, dbType string) (*sql.Conn, error) {
	for {
		conn, err := db.Conn(ctx)
		if err == nil {
			return conn, nil
		}
		if dbType != "sqlite" || !isSQLiteDailyEquityLockBusy(err) {
			return nil, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("SQLite connection acquisition interrupted: %w; last lock error: %w", ctxErr, err)
		}
		timer := time.NewTimer(dailyEquitySQLiteRetry)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil, fmt.Errorf("SQLite connection acquisition interrupted: %w; last lock error: %w", ctx.Err(), err)
		case <-timer.C:
		}
	}
}

func isSQLiteDailyEquityLockBusy(err error) bool {
	var sqliteErr sqlite3.Error
	return errors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked)
}

func (s *SQLStorage) runDailyEquityMigrationSQLite(ctx context.Context, conn *sql.Conn, direction string) error {
	if _, err := conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA busy_timeout=%d`, dailyEquitySQLiteBusyMS)); err != nil {
		return fmt.Errorf("set SQLite daily equity migration busy timeout: %w", err)
	}
	lockCtx, cancel := context.WithTimeout(ctx, dailyEquitySQLiteLockWait)
	defer cancel()
	for {
		if _, err := conn.ExecContext(lockCtx, `BEGIN EXCLUSIVE`); err == nil {
			break
		} else if !isSQLiteDailyEquityLockBusy(err) {
			return fmt.Errorf("acquire SQLite daily equity migration lock: %w", err)
		} else if lockErr := lockCtx.Err(); lockErr != nil {
			return fmt.Errorf("acquire SQLite daily equity migration lock: %w", lockErr)
		}
		timer := time.NewTimer(dailyEquitySQLiteRetry)
		select {
		case <-lockCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return fmt.Errorf("acquire SQLite daily equity migration lock: %w", lockCtx.Err())
		case <-timer.C:
		}
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	if err := s.applyDailyEquityMigration(ctx, conn, s.dbType, direction); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return fmt.Errorf("commit SQLite daily equity migration: %w", err)
	}
	committed = true
	return nil
}

func (s *SQLStorage) runDailyEquityMigrationMySQL(ctx context.Context, conn *sql.Conn, direction string) error {
	var acquired sql.NullInt64
	if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, ?)`, dailyEquityMySQLLockName, dailyEquityLockTimeout).Scan(&acquired); err != nil {
		return fmt.Errorf("acquire MySQL daily equity migration lock: %w", err)
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return fmt.Errorf("acquire MySQL daily equity migration lock: timed out or unavailable")
	}
	migrationErr := s.applyDailyEquityMigration(ctx, conn, s.dbType, direction)
	releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var released sql.NullInt64
	releaseErr := conn.QueryRowContext(releaseCtx, `SELECT RELEASE_LOCK(?)`, dailyEquityMySQLLockName).Scan(&released)
	if releaseErr != nil || !released.Valid || released.Int64 != 1 {
		if releaseErr == nil {
			releaseErr = fmt.Errorf("MySQL daily equity migration lock was not owned at release")
		}
		// A named lock survives returning a live session to database/sql's pool.
		// ErrBadConn makes database/sql discard this physical connection instead.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		if migrationErr != nil {
			return fmt.Errorf("%v; release MySQL daily equity migration lock: %w", migrationErr, releaseErr)
		}
		return fmt.Errorf("release MySQL daily equity migration lock: %w", releaseErr)
	}
	return migrationErr
}

type dailyEquityMigrationExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *SQLStorage) applyDailyEquityMigration(ctx context.Context, exec dailyEquityMigrationExecutor, dbType, direction string) error {
	path, err := dailyEquityMigrationFile(dbType, direction)
	if err != nil {
		return err
	}
	body, err := dailyEquityMigrations.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read daily equity migration %s: %w", path, err)
	}
	for _, statement := range strings.Split(string(body), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := exec.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply daily equity migration %s: %w", path, err)
		}
	}
	return nil
}
