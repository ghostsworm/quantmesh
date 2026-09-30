package storage

import (
	"context"
	"embed"
	"fmt"
	"strings"
)

//go:embed migrations/202609300*_opening_pause_*.sql
var openingPauseMigrations embed.FS

type OpeningPauseHolder struct {
	OwnerID string
	Source  string
	Reason  string
}

// OpeningPauseStateStore is the durable owner registry used by the risk
// coordinator. A missing row means that owner has been explicitly released.
type OpeningPauseStateStore interface {
	LoadOpeningPauseHolders(context.Context) ([]OpeningPauseHolder, error)
	UpsertOpeningPauseHolder(context.Context, OpeningPauseHolder) error
	DeleteOpeningPauseHolder(context.Context, OpeningPauseHolder) error
}

// MigrateOpeningPauseHolders installs the additive, versioned pause-owner tables.
func (s *SQLStorage) MigrateOpeningPauseHolders(ctx context.Context) error {
	if s == nil || (s.dbType != "sqlite" && s.dbType != "mysql") {
		return fmt.Errorf("unsupported opening pause state database")
	}
	for _, migrationID := range []string{"2026093004", "2026093005"} {
		script, err := openingPauseMigrations.ReadFile("migrations/" + migrationID + "_opening_pause_holders_" + s.dbType + ".up.sql")
		if err != nil {
			return err
		}
		for _, statement := range strings.Split(string(script), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err := s.db.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("migrate opening pause holders (%s): %w", migrationID, err)
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	markerQuery := `INSERT OR IGNORE INTO opening_pause_state_migrations (migration_id) VALUES (?)`
	if s.dbType == "mysql" {
		markerQuery = `INSERT IGNORE INTO opening_pause_state_migrations (migration_id) VALUES (?)`
	}
	result, err := tx.ExecContext(ctx, markerQuery, "2026093004")
	if err != nil {
		return fmt.Errorf("record opening pause schema bootstrap: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if inserted > 0 {
		var sentinelQuery string
		if s.dbType == "mysql" {
			sentinelQuery = `INSERT IGNORE INTO opening_pause_holders (source, reason) VALUES (?, ?)`
		} else {
			sentinelQuery = `INSERT OR IGNORE INTO opening_pause_holders (source, reason) VALUES (?, ?)`
		}
		if _, err := tx.ExecContext(ctx, sentinelQuery, "opening_pause_legacy_state_unverified", "首次启用持久化暂停状态，旧版本的活动风险来源无法权威恢复；需人工核实后解除"); err != nil {
			return fmt.Errorf("install legacy opening pause safety hold: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, markerQuery, "2026093005"); err != nil {
		return fmt.Errorf("record opening pause instance-owner migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit opening pause schema bootstrap: %w", err)
	}
	return nil
}

func (s *SQLStorage) LoadOpeningPauseHolders(ctx context.Context) ([]OpeningPauseHolder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source, reason FROM opening_pause_holders ORDER BY source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var holders []OpeningPauseHolder
	for rows.Next() {
		var holder OpeningPauseHolder
		if err := rows.Scan(&holder.Source, &holder.Reason); err != nil {
			rows.Close()
			return nil, err
		}
		holders = append(holders, holder)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	ownerRows, err := s.db.QueryContext(ctx, `SELECT owner_id, source, reason FROM opening_pause_owners ORDER BY owner_id, source`)
	if err != nil {
		return nil, err
	}
	defer ownerRows.Close()
	for ownerRows.Next() {
		var holder OpeningPauseHolder
		if err := ownerRows.Scan(&holder.OwnerID, &holder.Source, &holder.Reason); err != nil {
			return nil, err
		}
		holders = append(holders, holder)
	}
	return holders, ownerRows.Err()
}

func (s *SQLStorage) UpsertOpeningPauseHolder(ctx context.Context, holder OpeningPauseHolder) error {
	holder.Source = strings.TrimSpace(holder.Source)
	holder.OwnerID = strings.TrimSpace(holder.OwnerID)
	maxSourceLength := 128
	if holder.OwnerID != "" {
		maxSourceLength = 96
	}
	if holder.Source == "" || len(holder.Source) > maxSourceLength || len(holder.OwnerID) > 64 {
		return fmt.Errorf("invalid opening pause source")
	}
	var query string
	if holder.OwnerID != "" {
		if s.dbType == "mysql" {
			query = `INSERT INTO opening_pause_owners (owner_id, source, reason) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE reason = VALUES(reason), updated_at = CURRENT_TIMESTAMP(3)`
		} else {
			query = `INSERT INTO opening_pause_owners (owner_id, source, reason) VALUES (?, ?, ?) ON CONFLICT(owner_id, source) DO UPDATE SET reason = excluded.reason, updated_at = CURRENT_TIMESTAMP`
		}
		_, err := s.db.ExecContext(ctx, query, holder.OwnerID, holder.Source, holder.Reason)
		return err
	}
	if s.dbType == "mysql" {
		query = `INSERT INTO opening_pause_holders (source, reason) VALUES (?, ?) ON DUPLICATE KEY UPDATE reason = VALUES(reason), updated_at = CURRENT_TIMESTAMP(3)`
	} else {
		query = `INSERT INTO opening_pause_holders (source, reason) VALUES (?, ?) ON CONFLICT(source) DO UPDATE SET reason = excluded.reason, updated_at = CURRENT_TIMESTAMP`
	}
	_, err := s.db.ExecContext(ctx, query, holder.Source, holder.Reason)
	return err
}

func (s *SQLStorage) DeleteOpeningPauseHolder(ctx context.Context, holder OpeningPauseHolder) error {
	holder.OwnerID = strings.TrimSpace(holder.OwnerID)
	holder.Source = strings.TrimSpace(holder.Source)
	maxSourceLength := 128
	if holder.OwnerID != "" {
		maxSourceLength = 96
	}
	if holder.Source == "" || len(holder.Source) > maxSourceLength || len(holder.OwnerID) > 64 {
		return fmt.Errorf("invalid opening pause source")
	}
	var err error
	if holder.OwnerID != "" {
		_, err = s.db.ExecContext(ctx, `DELETE FROM opening_pause_owners WHERE owner_id = ? AND source = ?`, holder.OwnerID, holder.Source)
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM opening_pause_holders WHERE source = ?`, holder.Source)
	}
	return err
}
