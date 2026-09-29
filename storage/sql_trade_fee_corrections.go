package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"quantmesh/utils"
)

// migrateTradeFeeCorrectionsTableMySQL creates the durable owner-scoped hold
// table used when a late execution-fee correction cannot be applied safely.
func migrateTradeFeeCorrectionsTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS trade_fee_corrections (
  correction_id CHAR(64) NOT NULL PRIMARY KEY,
  bot_id VARCHAR(128) NOT NULL,
  exchange VARCHAR(64) NOT NULL,
  market_type VARCHAR(32) NOT NULL,
  symbol VARCHAR(64) NOT NULL,
  account_scope VARCHAR(256) NOT NULL,
  account VARCHAR(128) NOT NULL DEFAULT '',
  order_id BIGINT NOT NULL,
  client_order_id VARCHAR(128) NOT NULL DEFAULT '',
  leg VARCHAR(16) NOT NULL,
  side VARCHAR(16) NOT NULL,
  fee DOUBLE NOT NULL,
  fee_asset VARCHAR(32) NOT NULL DEFAULT '',
  base_fee_qty DOUBLE NOT NULL DEFAULT 0,
  executed_qty DOUBLE NOT NULL DEFAULT 0,
  reason TEXT NOT NULL,
  evidence_note TEXT NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  created_at DATETIME(3) NOT NULL,
  resolved_at DATETIME(3) NULL,
  KEY idx_trade_fee_corrections_scope_status (exchange, market_type, symbol, account_scope, bot_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	if err != nil {
		return err
	}
	var columnCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'trade_fee_corrections' AND COLUMN_NAME = 'executed_qty'`).Scan(&columnCount); err != nil {
		return fmt.Errorf("inspect trade fee correction executed_qty migration: %w", err)
	}
	if columnCount == 0 {
		if _, err := db.Exec(`ALTER TABLE trade_fee_corrections ADD COLUMN executed_qty DOUBLE NOT NULL DEFAULT 0 AFTER base_fee_qty`); err != nil {
			return fmt.Errorf("add trade fee correction executed_qty: %w", err)
		}
	}
	return nil
}

func migrateTradeFeeCorrectionsTableSQLite(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(trade_fee_corrections)`)
	if err != nil {
		return fmt.Errorf("inspect trade_fee_corrections columns: %w", err)
	}
	found := false
	for rows.Next() {
		var columnID int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&columnID, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("scan trade_fee_corrections columns: %w", err)
		}
		if name == "executed_qty" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate trade_fee_corrections columns: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close trade_fee_corrections column cursor: %w", err)
	}
	if !found {
		if _, err := db.Exec(`ALTER TABLE trade_fee_corrections ADD COLUMN executed_qty REAL NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add trade fee correction executed_qty: %w", err)
		}
	}
	return nil
}

// SaveTradeFeeCorrection is idempotent by CorrectionID and rejects any attempt
// to reuse that identity for different economic evidence.
func (s *SQLStorage) SaveTradeFeeCorrection(correction *TradeFeeCorrection) error {
	if correction == nil {
		return fmt.Errorf("trade fee correction is nil")
	}
	canonical := *correction
	canonical.CorrectionID = strings.TrimSpace(canonical.CorrectionID)
	canonical.BotID = strings.TrimSpace(canonical.BotID)
	canonical.Exchange = strings.ToLower(strings.TrimSpace(canonical.Exchange))
	canonical.MarketType = strings.ToLower(strings.TrimSpace(canonical.MarketType))
	canonical.Symbol = strings.ToUpper(strings.TrimSpace(canonical.Symbol))
	canonical.AccountScope = strings.TrimSpace(canonical.AccountScope)
	canonical.Account = strings.TrimSpace(canonical.Account)
	canonical.Leg = strings.ToLower(strings.TrimSpace(canonical.Leg))
	canonical.Side = strings.ToUpper(strings.TrimSpace(canonical.Side))
	canonical.FeeAsset = strings.ToUpper(strings.TrimSpace(canonical.FeeAsset))
	canonical.Status = "pending"
	if canonical.CreatedAt.IsZero() {
		canonical.CreatedAt = utils.NowUTC()
	}
	if canonical.CorrectionID == "" || canonical.BotID == "" || canonical.Exchange == "" || canonical.MarketType == "" ||
		canonical.Symbol == "" || canonical.AccountScope == "" || canonical.OrderID <= 0 ||
		(canonical.Leg != "open" && canonical.Leg != "close") || !finiteCorrection(canonical.Fee) || canonical.Fee < 0 ||
		!finiteCorrection(canonical.BaseFeeQty) || canonical.BaseFeeQty < 0 || !finiteCorrection(canonical.ExecutedQty) ||
		canonical.ExecutedQty <= 0 || canonical.Reason == "" {
		return fmt.Errorf("trade fee correction is missing verified owner/economic fields")
	}
	_, insertErr := s.db.Exec(`
INSERT INTO trade_fee_corrections
(correction_id, bot_id, exchange, market_type, symbol, account_scope, account, order_id, client_order_id, leg, side, fee, fee_asset, base_fee_qty, executed_qty, reason, evidence_note, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', 'pending', ?)
`, canonical.CorrectionID, canonical.BotID, canonical.Exchange, canonical.MarketType, canonical.Symbol, canonical.AccountScope,
		canonical.Account, canonical.OrderID, canonical.ClientOrderID, canonical.Leg, canonical.Side, canonical.Fee, canonical.FeeAsset,
		canonical.BaseFeeQty, canonical.ExecutedQty, canonical.Reason, utils.ToUTC(canonical.CreatedAt))
	if insertErr == nil {
		return nil
	}
	existing, readErr := s.getTradeFeeCorrection(canonical.CorrectionID)
	if readErr != nil {
		return insertErr
	}
	if sameTradeFeeCorrection(*existing, canonical) {
		if existing.Status != "pending" {
			return fmt.Errorf("resolved trade fee correction id %q was replayed; renewed reconciliation is required", canonical.CorrectionID)
		}
		return nil
	}
	return fmt.Errorf("trade fee correction id %q already contains different evidence: %w", canonical.CorrectionID, insertErr)
}

func finiteCorrection(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func sameTradeFeeCorrection(a, b TradeFeeCorrection) bool {
	close := func(x, y float64) bool { return math.Abs(x-y) <= 1e-10 }
	return a.CorrectionID == b.CorrectionID && a.BotID == b.BotID && a.Exchange == b.Exchange && a.MarketType == b.MarketType &&
		a.Symbol == b.Symbol && a.AccountScope == b.AccountScope && a.Account == b.Account && a.OrderID == b.OrderID &&
		a.ClientOrderID == b.ClientOrderID && a.Leg == b.Leg && a.Side == b.Side && close(a.Fee, b.Fee) &&
		a.FeeAsset == b.FeeAsset && close(a.BaseFeeQty, b.BaseFeeQty) && close(a.ExecutedQty, b.ExecutedQty) && a.Reason == b.Reason
}

func (s *SQLStorage) getTradeFeeCorrection(id string) (*TradeFeeCorrection, error) {
	row := s.db.QueryRow(`SELECT correction_id, bot_id, exchange, market_type, symbol, account_scope, account, order_id,
client_order_id, leg, side, fee, fee_asset, base_fee_qty, executed_qty, reason, evidence_note, status, created_at
FROM trade_fee_corrections WHERE correction_id = ?`, id)
	var correction TradeFeeCorrection
	if err := row.Scan(&correction.CorrectionID, &correction.BotID, &correction.Exchange, &correction.MarketType,
		&correction.Symbol, &correction.AccountScope, &correction.Account, &correction.OrderID, &correction.ClientOrderID,
		&correction.Leg, &correction.Side, &correction.Fee, &correction.FeeAsset, &correction.BaseFeeQty,
		&correction.ExecutedQty, &correction.Reason, &correction.Evidence, &correction.Status, &correction.CreatedAt); err != nil {
		return nil, err
	}
	return &correction, nil
}

// GetPendingTradeFeeCorrections returns only unresolved holds for the exact
// exchange, market, symbol, credential scope, and Bot owner.
func (s *SQLStorage) GetPendingTradeFeeCorrections(exchange, marketType, symbol, accountScope, botID string) ([]*TradeFeeCorrection, error) {
	rows, err := s.db.Query(`SELECT correction_id, bot_id, exchange, market_type, symbol, account_scope, account, order_id,
client_order_id, leg, side, fee, fee_asset, base_fee_qty, executed_qty, reason, evidence_note, status, created_at
FROM trade_fee_corrections
WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND bot_id = ? AND status = 'pending'
ORDER BY created_at, correction_id`, strings.ToLower(strings.TrimSpace(exchange)), strings.ToLower(strings.TrimSpace(marketType)),
		strings.ToUpper(strings.TrimSpace(symbol)), strings.TrimSpace(accountScope), strings.TrimSpace(botID))
	if err != nil {
		return nil, fmt.Errorf("query pending trade fee corrections: %w", err)
	}
	defer rows.Close()
	corrections := make([]*TradeFeeCorrection, 0)
	for rows.Next() {
		var correction TradeFeeCorrection
		if err := rows.Scan(&correction.CorrectionID, &correction.BotID, &correction.Exchange, &correction.MarketType,
			&correction.Symbol, &correction.AccountScope, &correction.Account, &correction.OrderID, &correction.ClientOrderID,
			&correction.Leg, &correction.Side, &correction.Fee, &correction.FeeAsset, &correction.BaseFeeQty,
			&correction.ExecutedQty, &correction.Reason, &correction.Evidence, &correction.Status, &correction.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pending trade fee correction: %w", err)
		}
		corrections = append(corrections, &correction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending trade fee corrections: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close pending trade fee corrections: %w", err)
	}
	legacy, err := s.getLegacyTradeFeeCorrections(exchange, symbol, botID)
	if err != nil {
		return nil, err
	}
	corrections = append(corrections, legacy...)
	sort.Slice(corrections, func(i, j int) bool {
		if corrections[i].CreatedAt.Equal(corrections[j].CreatedAt) {
			return corrections[i].CorrectionID < corrections[j].CorrectionID
		}
		return corrections[i].CreatedAt.Before(corrections[j].CreatedAt)
	})
	return corrections, nil
}

func (s *SQLStorage) getLegacyTradeFeeCorrections(exchange, symbol, botID string) ([]*TradeFeeCorrection, error) {
	jsonValue := func(column, path string) string {
		if s.dbType == "mysql" {
			return fmt.Sprintf("CASE WHEN JSON_VALID(%s) THEN JSON_UNQUOTE(JSON_EXTRACT(%s, '%s')) ELSE '' END", column, column, path)
		}
		return fmt.Sprintf("CASE WHEN json_valid(%s) THEN json_extract(%s, '%s') ELSE '' END", column, column, path)
	}
	botExpr, exchangeExpr := jsonValue("e.data", "$.bot_id"), jsonValue("e.data", "$.exchange")
	symbolExpr, correctionIDExpr := jsonValue("e.data", "$.symbol"), jsonValue("e.data", "$.correction_id")
	query := fmt.Sprintf(`SELECT e.id, e.data, e.created_at FROM events e
WHERE e.event_type = ? AND %s = ? AND LOWER(%s) = LOWER(?) AND UPPER(%s) = UPPER(?)
AND (%s = '' OR NOT EXISTS (SELECT 1 FROM trade_fee_corrections c WHERE c.correction_id = %s))
ORDER BY e.created_at, e.id`, botExpr, exchangeExpr, symbolExpr, correctionIDExpr, correctionIDExpr)
	rows, err := s.db.Query(query, "trade_fee_correction", strings.TrimSpace(botID), strings.TrimSpace(exchange), strings.TrimSpace(symbol))
	if err != nil {
		return nil, fmt.Errorf("query legacy trade fee correction events: %w", err)
	}
	defer rows.Close()
	corrections := make([]*TradeFeeCorrection, 0)
	for rows.Next() {
		var eventID int64
		var payload string
		var createdAt time.Time
		if err := rows.Scan(&eventID, &payload, &createdAt); err != nil {
			return nil, fmt.Errorf("scan legacy trade fee correction event: %w", err)
		}
		var evidence struct {
			BotID         string  `json:"bot_id"`
			Exchange      string  `json:"exchange"`
			MarketType    string  `json:"market_type"`
			Symbol        string  `json:"symbol"`
			OrderID       int64   `json:"order_id"`
			ClientOrderID string  `json:"client_order_id"`
			Leg           string  `json:"leg"`
			Side          string  `json:"side"`
			Fee           float64 `json:"fee"`
			FeeAsset      string  `json:"fee_asset"`
			BaseFeeQty    float64 `json:"base_fee_qty"`
			ExecutedQty   float64 `json:"executed_qty"`
			Reason        string  `json:"reason"`
			CorrectionID  string  `json:"correction_id"`
		}
		if err := json.Unmarshal([]byte(payload), &evidence); err != nil {
			return nil, fmt.Errorf("decode legacy trade fee correction event %d: %w", eventID, err)
		}
		corrections = append(corrections, &TradeFeeCorrection{
			CorrectionID: fmt.Sprintf("legacy-event-%d", eventID), BotID: evidence.BotID, Exchange: strings.ToLower(evidence.Exchange),
			MarketType: strings.ToLower(evidence.MarketType), Symbol: strings.ToUpper(evidence.Symbol), OrderID: evidence.OrderID,
			ClientOrderID: evidence.ClientOrderID, Leg: strings.ToLower(evidence.Leg), Side: strings.ToUpper(evidence.Side),
			Fee: evidence.Fee, FeeAsset: strings.ToUpper(evidence.FeeAsset), BaseFeeQty: evidence.BaseFeeQty,
			ExecutedQty: evidence.ExecutedQty, Reason: evidence.Reason, Status: "pending", LegacyEvent: true, CreatedAt: createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate legacy trade fee correction events: %w", err)
	}
	return corrections, nil
}

func (s *SQLStorage) CountPendingTradeFeeCorrections(exchange, marketType, symbol, accountScope, botID string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM trade_fee_corrections
WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND bot_id = ? AND status = 'pending'`,
		strings.ToLower(strings.TrimSpace(exchange)), strings.ToLower(strings.TrimSpace(marketType)),
		strings.ToUpper(strings.TrimSpace(symbol)), strings.TrimSpace(accountScope), strings.TrimSpace(botID)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count pending scoped trade fee corrections: %w", err)
	}
	legacyCount, err := s.countLegacyTradeFeeCorrectionEvents(exchange, symbol, botID)
	if err != nil {
		return 0, err
	}
	count += legacyCount
	return count, nil
}

// CountPendingTradeFeeCorrectionsForAccount blocks money movement whenever any
// Bot in the exact account/symbol stream has an unresolved fee correction.
// Legacy events lack credential scope, so they conservatively block every
// account with the same exchange and symbol.
func (s *SQLStorage) CountPendingTradeFeeCorrectionsForAccount(exchange, marketType, symbol, accountScope string) (int, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(marketType) == "" ||
		strings.TrimSpace(symbol) == "" || strings.TrimSpace(accountScope) == "" {
		return 0, fmt.Errorf("pending fee correction check requires exact account and market scope")
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM trade_fee_corrections
WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND status = 'pending'`,
		strings.ToLower(strings.TrimSpace(exchange)), strings.ToLower(strings.TrimSpace(marketType)),
		strings.ToUpper(strings.TrimSpace(symbol)), strings.TrimSpace(accountScope)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count account-scoped pending fee corrections: %w", err)
	}
	legacyCount, err := s.countLegacyTradeFeeCorrectionEvents(exchange, symbol, "")
	if err != nil {
		return 0, err
	}
	return count + legacyCount, nil
}

func (s *SQLStorage) countLegacyTradeFeeCorrectionEvents(exchange, symbol, botID string) (int, error) {
	jsonValue := func(column, path string) string {
		if s.dbType == "mysql" {
			return fmt.Sprintf("CASE WHEN JSON_VALID(%s) THEN JSON_UNQUOTE(JSON_EXTRACT(%s, '%s')) ELSE '' END", column, column, path)
		}
		return fmt.Sprintf("CASE WHEN json_valid(%s) THEN json_extract(%s, '%s') ELSE '' END", column, column, path)
	}
	botExpr := jsonValue("e.data", "$.bot_id")
	exchangeExpr := jsonValue("e.data", "$.exchange")
	symbolExpr := jsonValue("e.data", "$.symbol")
	correctionIDExpr := jsonValue("e.data", "$.correction_id")
	botFilter := ""
	args := []interface{}{"trade_fee_correction"}
	if strings.TrimSpace(botID) != "" {
		botFilter = " AND " + botExpr + " = ?"
		args = append(args, strings.TrimSpace(botID))
	}
	query := fmt.Sprintf(`SELECT COUNT(*) FROM events e
WHERE e.event_type = ?%s AND LOWER(%s) = LOWER(?) AND UPPER(%s) = UPPER(?)
AND (%s = '' OR NOT EXISTS (SELECT 1 FROM trade_fee_corrections c WHERE c.correction_id = %s))`,
		botFilter, exchangeExpr, symbolExpr, correctionIDExpr, correctionIDExpr)
	var count int
	args = append(args, strings.TrimSpace(exchange), strings.TrimSpace(symbol))
	if err := s.db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count legacy trade fee correction events: %w", err)
	}
	return count, nil
}

// ResolveTradeFeeCorrection atomically apportions a verified quote-denominated
// correction over every paired trade row owned by the exact execution order.
// It rejects incomplete quantity coverage and base-asset fees, whose inventory
// impact requires a separate position reconciliation.
func (s *SQLStorage) ResolveTradeFeeCorrection(correctionID, exchange, marketType, symbol, accountScope, botID, evidence string, resolvedAt time.Time) error {
	correctionID, evidence = strings.TrimSpace(correctionID), strings.TrimSpace(evidence)
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	marketType = strings.ToLower(strings.TrimSpace(marketType))
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	accountScope, botID = strings.TrimSpace(accountScope), strings.TrimSpace(botID)
	if correctionID == "" || len(evidence) < 12 || len(evidence) > 1000 || accountScope == "" || botID == "" || exchange == "" || marketType == "" || symbol == "" {
		return fmt.Errorf("fee correction resolution requires identity, owner scope, and evidence")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin scoped trade fee correction: %w", err)
	}
	defer tx.Rollback()
	lockSuffix := ""
	if s.dbType == "mysql" {
		lockSuffix = " FOR UPDATE"
	}
	var correction TradeFeeCorrection
	if err := tx.QueryRow(`SELECT order_id, account, leg, fee, fee_asset, base_fee_qty, executed_qty, status, evidence_note
FROM trade_fee_corrections WHERE correction_id = ? AND exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND bot_id = ?`+lockSuffix,
		correctionID, exchange, marketType, symbol, accountScope, botID).Scan(&correction.OrderID, &correction.Account, &correction.Leg,
		&correction.Fee, &correction.FeeAsset, &correction.BaseFeeQty, &correction.ExecutedQty, &correction.Status, &correction.Evidence); err != nil {
		return fmt.Errorf("load pending fee correction for exact owner: %w", err)
	}
	if correction.Status == "resolved" {
		if correction.Evidence == evidence {
			return tx.Commit()
		}
		return fmt.Errorf("fee correction was already resolved with different evidence")
	}
	if correction.Status != "pending" || correction.OrderID <= 0 || !finiteCorrection(correction.ExecutedQty) || correction.ExecutedQty <= 0 ||
		!finiteCorrection(correction.Fee) || correction.Fee <= 0 || correction.FeeAsset == "" ||
		!finiteCorrection(correction.BaseFeeQty) || correction.BaseFeeQty != 0 {
		return fmt.Errorf("fee correction is not eligible for paired-ledger application")
	}
	orderColumn := "buy_order_id"
	if correction.Leg == "close" {
		orderColumn = "sell_order_id"
	} else if correction.Leg != "open" {
		return fmt.Errorf("fee correction has an invalid execution leg")
	}
	query := fmt.Sprintf(`SELECT id, quantity, COALESCE(fee, 0), pnl_asset, fee_asset FROM %s
WHERE %s = ? AND bot_id = ? AND LOWER(exchange) = ? AND LOWER(market_type) = ? AND UPPER(symbol) = ?
AND account_scope = ? AND COALESCE(account, '') = ? ORDER BY id`, s.tradesTbl(), orderColumn)
	if s.dbType == "mysql" {
		query += " FOR UPDATE"
	}
	rows, err := tx.Query(query, correction.OrderID, botID, exchange, marketType, symbol, accountScope, strings.TrimSpace(correction.Account))
	if err != nil {
		return fmt.Errorf("query paired trades for fee correction: %w", err)
	}
	type tradeFeeTarget struct {
		id       int64
		quantity float64
		fee      float64
	}
	targets := make([]tradeFeeTarget, 0)
	totalQuantity := 0.0
	for rows.Next() {
		var target tradeFeeTarget
		var pnlAsset, feeAsset string
		if err := rows.Scan(&target.id, &target.quantity, &target.fee, &pnlAsset, &feeAsset); err != nil {
			rows.Close()
			return fmt.Errorf("scan paired trade for fee correction: %w", err)
		}
		if target.quantity <= 0 || !finiteCorrection(target.quantity) || !finiteCorrection(target.fee) ||
			!strings.EqualFold(pnlAsset, correction.FeeAsset) || !strings.EqualFold(feeAsset, pnlAsset) {
			rows.Close()
			return fmt.Errorf("paired trade quantity or fee denomination is not verified")
		}
		targets = append(targets, target)
		totalQuantity += target.quantity
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil {
		return fmt.Errorf("iterate paired trades for fee correction: %w", rowsErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close paired trade fee correction rows: %w", closeErr)
	}
	tolerance := math.Max(1e-8, math.Abs(correction.ExecutedQty)*1e-8)
	if len(targets) == 0 || !finiteCorrection(totalQuantity) || math.Abs(totalQuantity-correction.ExecutedQty) > tolerance {
		return fmt.Errorf("paired trade quantity coverage %.12g does not match execution quantity %.12g", totalQuantity, correction.ExecutedQty)
	}
	remainingFee := correction.Fee
	for i, target := range targets {
		allocation := remainingFee
		if i < len(targets)-1 {
			allocation = correction.Fee * (target.quantity / totalQuantity)
			remainingFee -= allocation
		}
		if !finiteCorrection(allocation) || !finiteCorrection(target.fee+allocation) {
			return fmt.Errorf("fee allocation overflows paired trade %d", target.id)
		}
		result, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET fee = fee + ? WHERE id = ?`, s.tradesTbl()), allocation, target.id)
		if err != nil {
			return fmt.Errorf("apply fee to paired trade %d: %w", target.id, err)
		}
		changed, err := result.RowsAffected()
		if err != nil || changed != 1 {
			return fmt.Errorf("verify paired trade fee update %d: rows=%d err=%v", target.id, changed, err)
		}
	}
	if resolvedAt.IsZero() {
		resolvedAt = utils.NowUTC()
	}
	result, err := tx.Exec(`UPDATE trade_fee_corrections SET status = 'resolved', evidence_note = ?, resolved_at = ?
WHERE correction_id = ? AND exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND bot_id = ? AND status = 'pending'`,
		evidence, utils.ToUTC(resolvedAt), correctionID, exchange, marketType, symbol, accountScope, botID)
	if err != nil {
		return fmt.Errorf("mark applied fee correction resolved: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		return fmt.Errorf("verify fee correction resolution update: rows=%d err=%v", changed, err)
	}
	return tx.Commit()
}
