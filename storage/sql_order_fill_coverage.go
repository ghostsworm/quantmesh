package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"quantmesh/utils"
)

type OrderFillCoverage struct {
	Exchange       string
	MarketType     string
	Symbol         string
	AccountScope   string
	CoveredFrom    time.Time
	CoveredThrough time.Time
}

func orderFillScopeHash(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(sum[:])
}

func (s *SQLStorage) GetOrderFillCoverage(exchange, marketType, symbol, accountScope string) (*OrderFillCoverage, error) {
	if exchange == "" || marketType == "" || symbol == "" || accountScope == "" {
		return nil, fmt.Errorf("order fill coverage requires a complete account and market scope")
	}
	var coverage OrderFillCoverage
	err := s.db.QueryRow(`SELECT exchange_name, market_type, symbol, account_scope, covered_from, covered_through
		FROM order_fill_sync_coverage WHERE scope_hash = ? AND exchange_name = ? AND market_type = ? AND symbol = ?`,
		orderFillScopeHash(accountScope), exchange, marketType, symbol).Scan(
		&coverage.Exchange, &coverage.MarketType, &coverage.Symbol, &coverage.AccountScope, &coverage.CoveredFrom, &coverage.CoveredThrough)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read order fill synchronization coverage: %w", err)
	}
	if coverage.AccountScope != accountScope || coverage.CoveredFrom.IsZero() || coverage.CoveredThrough.Before(coverage.CoveredFrom) {
		return nil, fmt.Errorf("stored order fill coverage identity or interval is invalid")
	}
	coverage.CoveredFrom = utils.ToUTC(coverage.CoveredFrom)
	coverage.CoveredThrough = utils.ToUTC(coverage.CoveredThrough)
	return &coverage, nil
}

// AdvanceOrderFillCoverage merges a completely fetched interval. Gaps are
// rejected; callers must not claim coverage across an unqueried time range.
func (s *SQLStorage) AdvanceOrderFillCoverage(exchange, marketType, symbol, accountScope string, from, through time.Time) error {
	if exchange == "" || marketType == "" || symbol == "" || accountScope == "" || !from.Before(through) {
		return fmt.Errorf("order fill coverage requires a complete scope and non-empty interval")
	}
	from, through = utils.ToUTC(from), utils.ToUTC(through)
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin order fill coverage update: %w", err)
	}
	defer tx.Rollback()
	query := `SELECT account_scope, covered_from, covered_through FROM order_fill_sync_coverage WHERE scope_hash = ? AND exchange_name = ? AND market_type = ? AND symbol = ?`
	if s.dbType == "mysql" {
		query += " FOR UPDATE"
	}
	var oldScope string
	var oldFrom, oldThrough time.Time
	readErr := tx.QueryRow(query, orderFillScopeHash(accountScope), exchange, marketType, symbol).Scan(&oldScope, &oldFrom, &oldThrough)
	if readErr != nil && readErr != sql.ErrNoRows {
		return fmt.Errorf("read existing order fill coverage: %w", readErr)
	}
	if readErr == nil {
		if oldScope != accountScope {
			return fmt.Errorf("order fill scope hash collision")
		}
		oldFrom, oldThrough = utils.ToUTC(oldFrom), utils.ToUTC(oldThrough)
		if from.After(oldThrough) || through.Before(oldFrom) {
			return fmt.Errorf("refusing to merge order fill coverage across a gap")
		}
		if oldFrom.Before(from) {
			from = oldFrom
		}
		if oldThrough.After(through) {
			through = oldThrough
		}
		_, err = tx.Exec(`UPDATE order_fill_sync_coverage SET covered_from = ?, covered_through = ?, updated_at = CURRENT_TIMESTAMP WHERE scope_hash = ? AND exchange_name = ? AND market_type = ? AND symbol = ?`,
			from, through, orderFillScopeHash(accountScope), exchange, marketType, symbol)
	} else {
		_, err = tx.Exec(`INSERT INTO order_fill_sync_coverage (scope_hash, exchange_name, market_type, symbol, account_scope, covered_from, covered_through) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			orderFillScopeHash(accountScope), exchange, marketType, symbol, accountScope, from, through)
	}
	if err != nil {
		return fmt.Errorf("persist order fill synchronization coverage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit order fill synchronization coverage: %w", err)
	}
	return nil
}
