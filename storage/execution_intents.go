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
	"strings"

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

// HasLegacyExecutionHistory reports order rows that cannot be tied to the
// already-restored, owner-scoped intent journal. Callers must load and validate
// that journal first; its presence alone is not economic-settlement evidence.
func (s *SQLStorage) HasLegacyExecutionHistory(ctx context.Context, scope execution.IntentScope) (bool, error) {
	key, err := scope.Key()
	if err != nil {
		return false, fmt.Errorf("invalid legacy history scope: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT bot_id, client_order_id FROM orders WHERE exchange = ? AND symbol = ? AND (bot_id = ? OR bot_id = '' OR bot_id IS NULL)`, scope.Exchange, scope.Symbol, scope.Bot)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	var clientOrderIDs []string
	for rows.Next() {
		var botID, clientOrderID sql.NullString
		if err := rows.Scan(&botID, &clientOrderID); err != nil {
			return false, err
		}
		if !botID.Valid || botID.String != scope.Bot || !clientOrderID.Valid || clientOrderID.String == "" {
			return true, nil
		}
		clientOrderIDs = append(clientOrderIDs, clientOrderID.String)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(clientOrderIDs) == 0 {
		return false, nil
	}
	if err := rows.Close(); err != nil {
		return false, err
	}

	journalIDs := make(map[string]struct{})
	var after int64
	for {
		page, err := s.LoadExecutionIntents(ctx, key, after, 500)
		if err != nil {
			return false, fmt.Errorf("load owner execution intents for legacy history check: %w", err)
		}
		if len(page) > 500 {
			return false, fmt.Errorf("oversized execution intent page during legacy history check")
		}
		for _, record := range page {
			if record.ID <= after || record.Revision <= 0 || record.ClientOrderID == "" {
				return false, fmt.Errorf("invalid execution intent page during legacy history check")
			}
			after = record.ID
			journalIDs[record.ClientOrderID] = struct{}{}
		}
		if len(page) < 500 {
			break
		}
	}
	for _, clientOrderID := range clientOrderIDs {
		if _, found := journalIDs[clientOrderID]; !found {
			return true, nil
		}
	}
	return false, nil
}

// HasVerifiedExecutionOrderIDs proves that every restored position's unique
// entry order is present in the Bot's order ledger and in the already-validated
// owner-scoped intent journal. Repeated/ambiguous entry IDs are not ownership
// evidence and must be rejected by the caller.
func (s *SQLStorage) HasVerifiedExecutionOrderIDs(ctx context.Context, scope execution.IntentScope, positions []execution.ExposurePosition) (bool, error) {
	if len(positions) == 0 {
		return true, nil
	}
	key, err := scope.Key()
	if err != nil {
		return false, fmt.Errorf("invalid restored execution order scope: %w", err)
	}
	required := make(map[int64]execution.ExposurePosition, len(positions))
	for _, position := range positions {
		if !restorableExecutionGroup(position.Group) || position.EntryOrderID <= 0 ||
			(position.Group != "grid" && strings.TrimSpace(position.EntryClientOrderID) == "") || (position.Leg != "LONG" && position.Leg != "SHORT") ||
			position.Quantity <= 0 || math.IsNaN(position.Quantity) || math.IsInf(position.Quantity, 0) {
			return false, nil
		}
		if _, duplicate := required[position.EntryOrderID]; duplicate {
			return false, nil
		}
		required[position.EntryOrderID] = position
	}

	journalOrders := make(map[int64]string)
	journalFillQty := make(map[int64]float64)
	var after int64
	for {
		page, err := s.LoadExecutionIntents(ctx, key, after, 500)
		if err != nil {
			return false, fmt.Errorf("load owner execution intents for restored positions: %w", err)
		}
		if len(page) > 500 {
			return false, fmt.Errorf("oversized execution intent page for restored positions")
		}
		for _, record := range page {
			if record.ID <= after || record.Revision <= 0 || record.ClientOrderID == "" {
				return false, fmt.Errorf("invalid execution intent page for restored positions")
			}
			after = record.ID
			var evidence struct {
				Version int
				Scope   execution.IntentScope
				Request struct {
					ClientOrderID string
					Symbol        string
					Side          string
					PositionSide  string
					StrategyType  string
				}
				Opening bool
				Order   *struct {
					OrderID       int64
					ClientOrderID string
					Symbol        string
					Side          string
					ExecutedQty   float64
					Status        string
				}
				Unknown       bool
				LedgerPending bool
				Rejected      bool
				Settled       bool
			}
			if err := json.Unmarshal(record.Payload, &evidence); err != nil {
				return false, fmt.Errorf("decode restored execution order evidence: %w", err)
			}
			if evidence.Version != 1 || evidence.Scope != scope || evidence.Request.ClientOrderID != record.ClientOrderID ||
				evidence.Request.Symbol != scope.Symbol || !evidence.Settled || evidence.Unknown || evidence.LedgerPending || evidence.Rejected ||
				evidence.Order == nil || evidence.Order.OrderID <= 0 || evidence.Order.ClientOrderID != record.ClientOrderID ||
				evidence.Order.Symbol != scope.Symbol || evidence.Order.ExecutedQty <= 0 || math.IsNaN(evidence.Order.ExecutedQty) || math.IsInf(evidence.Order.ExecutedQty, 0) ||
				!verifiedTerminalFillStatus(evidence.Order.Status) {
				continue
			}
			position, needed := required[evidence.Order.OrderID]
			if !needed || !evidence.Opening || !exposurePositionEntrySide(position.Leg, evidence.Request.Side) ||
				!strings.EqualFold(evidence.Request.StrategyType, position.Group) ||
				(position.EntryClientOrderID != "" && position.EntryClientOrderID != evidence.Request.ClientOrderID) ||
				!strings.EqualFold(evidence.Order.Side, evidence.Request.Side) ||
				(evidence.Request.PositionSide != "" && !strings.EqualFold(evidence.Request.PositionSide, position.Leg)) ||
				position.Quantity > evidence.Order.ExecutedQty+restoredOrderQuantityTolerance(position.Quantity, evidence.Order.ExecutedQty) {
				continue
			}
			if _, duplicate := journalOrders[evidence.Order.OrderID]; duplicate {
				return false, nil
			}
			journalOrders[evidence.Order.OrderID] = record.ClientOrderID
			journalFillQty[evidence.Order.OrderID] = evidence.Order.ExecutedQty
		}
		if len(page) < 500 {
			break
		}
	}

	rows, err := s.db.QueryContext(ctx, `SELECT order_id, client_order_id, side, filled_qty FROM orders WHERE exchange = ? AND market_type = ? AND account_scope = ? AND symbol = ? AND bot_id = ?`, scope.Exchange, scope.Market, scope.Account, scope.Symbol, scope.Bot)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	verified := make(map[int64]struct{}, len(required))
	for rows.Next() {
		var orderID int64
		var clientOrderID, side sql.NullString
		var filledQty float64
		if err := rows.Scan(&orderID, &clientOrderID, &side, &filledQty); err != nil {
			return false, err
		}
		position, needed := required[orderID]
		if !needed || !clientOrderID.Valid || !side.Valid || filledQty <= 0 || math.IsNaN(filledQty) || math.IsInf(filledQty, 0) ||
			!exposurePositionEntrySide(position.Leg, side.String) {
			continue
		}
		journalFilled, exists := journalFillQty[orderID]
		if journalOrders[orderID] != clientOrderID.String || !exists ||
			math.Abs(filledQty-journalFilled) > restoredOrderQuantityTolerance(filledQty, journalFilled) {
			return false, nil
		}
		if _, duplicate := verified[orderID]; duplicate {
			return false, nil
		}
		if position.Quantity > filledQty+restoredOrderQuantityTolerance(position.Quantity, filledQty) {
			return false, nil
		}
		verified[orderID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return len(verified) == len(required), nil
}

func restorableExecutionGroup(group string) bool {
	switch group {
	case "grid", "trend", "mean_reversion", "momentum":
		return true
	default:
		return false
	}
}

func restoredOrderQuantityTolerance(a, b float64) float64 {
	return math.Max(1e-8, math.Max(math.Abs(a), math.Abs(b))*1e-8)
}

func exposurePositionEntrySide(leg, side string) bool {
	switch strings.ToUpper(strings.TrimSpace(leg)) {
	case "LONG":
		return strings.EqualFold(strings.TrimSpace(side), "BUY")
	case "SHORT":
		return strings.EqualFold(strings.TrimSpace(side), "SELL")
	default:
		return false
	}
}

func verifiedTerminalFillStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "FILLED", "CANCELED", "CANCELLED", "EXPIRED":
		return true
	default:
		return false
	}
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
