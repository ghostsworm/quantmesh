package storage

import (
	"database/sql"
	"fmt"
	"math"
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
  reason TEXT NOT NULL,
  evidence_note TEXT NOT NULL DEFAULT '',
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  created_at DATETIME(3) NOT NULL,
  resolved_at DATETIME(3) NULL,
  KEY idx_trade_fee_corrections_scope_status (exchange, market_type, symbol, account_scope, bot_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
	return err
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
		!finiteCorrection(canonical.BaseFeeQty) || canonical.BaseFeeQty < 0 || canonical.Reason == "" {
		return fmt.Errorf("trade fee correction is missing verified owner/economic fields")
	}
	_, insertErr := s.db.Exec(`
INSERT INTO trade_fee_corrections
(correction_id, bot_id, exchange, market_type, symbol, account_scope, account, order_id, client_order_id, leg, side, fee, fee_asset, base_fee_qty, reason, evidence_note, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', 'pending', ?)
`, canonical.CorrectionID, canonical.BotID, canonical.Exchange, canonical.MarketType, canonical.Symbol, canonical.AccountScope,
		canonical.Account, canonical.OrderID, canonical.ClientOrderID, canonical.Leg, canonical.Side, canonical.Fee, canonical.FeeAsset,
		canonical.BaseFeeQty, canonical.Reason, utils.ToUTC(canonical.CreatedAt))
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
		a.FeeAsset == b.FeeAsset && close(a.BaseFeeQty, b.BaseFeeQty) && a.Reason == b.Reason
}

func (s *SQLStorage) getTradeFeeCorrection(id string) (*TradeFeeCorrection, error) {
	row := s.db.QueryRow(`SELECT correction_id, bot_id, exchange, market_type, symbol, account_scope, account, order_id,
client_order_id, leg, side, fee, fee_asset, base_fee_qty, reason, evidence_note, status, created_at
FROM trade_fee_corrections WHERE correction_id = ?`, id)
	var correction TradeFeeCorrection
	if err := row.Scan(&correction.CorrectionID, &correction.BotID, &correction.Exchange, &correction.MarketType,
		&correction.Symbol, &correction.AccountScope, &correction.Account, &correction.OrderID, &correction.ClientOrderID,
		&correction.Leg, &correction.Side, &correction.Fee, &correction.FeeAsset, &correction.BaseFeeQty,
		&correction.Reason, &correction.Evidence, &correction.Status, &correction.CreatedAt); err != nil {
		return nil, err
	}
	return &correction, nil
}

// GetPendingTradeFeeCorrections returns only unresolved holds for the exact
// exchange, market, symbol, credential scope, and Bot owner.
func (s *SQLStorage) GetPendingTradeFeeCorrections(exchange, marketType, symbol, accountScope, botID string) ([]*TradeFeeCorrection, error) {
	rows, err := s.db.Query(`SELECT correction_id, bot_id, exchange, market_type, symbol, account_scope, account, order_id,
client_order_id, leg, side, fee, fee_asset, base_fee_qty, reason, evidence_note, status, created_at
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
			&correction.Reason, &correction.Evidence, &correction.Status, &correction.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pending trade fee correction: %w", err)
		}
		corrections = append(corrections, &correction)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending trade fee corrections: %w", err)
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
	query := fmt.Sprintf(`SELECT COUNT(*) FROM events e
WHERE e.event_type = ? AND %s = ? AND LOWER(%s) = LOWER(?) AND UPPER(%s) = UPPER(?)
AND (%s = '' OR NOT EXISTS (SELECT 1 FROM trade_fee_corrections c WHERE c.correction_id = %s))`,
		botExpr, exchangeExpr, symbolExpr, correctionIDExpr, correctionIDExpr)
	var count int
	if err := s.db.QueryRow(query, "trade_fee_correction", strings.TrimSpace(botID), strings.TrimSpace(exchange), strings.TrimSpace(symbol)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count legacy trade fee correction events: %w", err)
	}
	return count, nil
}

// ResolveTradeFeeCorrection records a scoped, audited external reconciliation.
// The caller must provide a non-empty evidence note; this does not apply a fee
// to trade rows and must only be used after those rows have been corrected.
func (s *SQLStorage) ResolveTradeFeeCorrection(correctionID, exchange, marketType, symbol, accountScope, botID, evidence string, resolvedAt time.Time) error {
	correctionID, evidence = strings.TrimSpace(correctionID), strings.TrimSpace(evidence)
	if correctionID == "" || evidence == "" || strings.TrimSpace(accountScope) == "" || strings.TrimSpace(botID) == "" {
		return fmt.Errorf("fee correction resolution requires identity, owner scope, and evidence")
	}
	result, err := s.db.Exec(`UPDATE trade_fee_corrections SET status = 'resolved', evidence_note = ?, resolved_at = ?
WHERE correction_id = ? AND exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND bot_id = ? AND status = 'pending'`,
		evidence, utils.ToUTC(resolvedAt), correctionID, strings.ToLower(strings.TrimSpace(exchange)),
		strings.ToLower(strings.TrimSpace(marketType)), strings.ToUpper(strings.TrimSpace(symbol)), strings.TrimSpace(accountScope), strings.TrimSpace(botID))
	if err != nil {
		return fmt.Errorf("resolve scoped trade fee correction: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify trade fee correction resolution: %w", err)
	}
	if changed != 1 {
		return fmt.Errorf("pending trade fee correction not found for the exact runtime owner")
	}
	return nil
}
