package storage

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/utils"
)

// migrateOrderFillsTable 创建按交易所成交 ID 幂等的原始成交账本。
func migrateOrderFillsTable(db *sql.DB, mysql bool) error {
	if mysql {
		_, err := db.Exec(`CREATE TABLE IF NOT EXISTS order_fills (
			id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
			exchange VARCHAR(64) NOT NULL, market_type VARCHAR(32) NOT NULL,
			account_scope VARCHAR(512) NOT NULL, account VARCHAR(255) NOT NULL DEFAULT '',
			bot_id VARCHAR(128) NOT NULL DEFAULT '', symbol VARCHAR(64) NOT NULL,
			trade_id VARCHAR(128) NOT NULL, order_id BIGINT NOT NULL, side VARCHAR(16) NOT NULL,
			price DECIMAL(38,18) NOT NULL, quantity DECIMAL(38,18) NOT NULL, quote_quantity DECIMAL(38,18) NOT NULL DEFAULT 0,
			commission DECIMAL(38,18) NOT NULL DEFAULT 0, commission_asset VARCHAR(32) NOT NULL DEFAULT '',
			commission_quote DECIMAL(38,18) NOT NULL DEFAULT 0, commission_quote_rate DECIMAL(38,18) NOT NULL DEFAULT 0, commission_quote_known BOOLEAN NOT NULL DEFAULT FALSE,
			realized_pnl DECIMAL(38,18), realized_pnl_asset VARCHAR(32) NOT NULL DEFAULT '', trade_time TIMESTAMP(3) NOT NULL,
			created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			UNIQUE KEY uk_order_fills_identity (exchange, market_type, account_scope(128), symbol, trade_id),
			KEY idx_order_fills_scope_time (account_scope(128), exchange, market_type, symbol, trade_time)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
		if err != nil {
			return err
		}
		_, err = db.Exec(`CREATE TABLE IF NOT EXISTS order_fill_sync_coverage (
			scope_hash CHAR(64) NOT NULL, exchange_name VARCHAR(64) NOT NULL,
			market_type VARCHAR(32) NOT NULL, symbol VARCHAR(64) NOT NULL,
			account_scope VARCHAR(512) NOT NULL, covered_from TIMESTAMP(3) NOT NULL,
			covered_through TIMESTAMP(3) NOT NULL, updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (scope_hash, exchange_name, market_type, symbol)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)
		if err != nil {
			return err
		}
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'order_fills' AND column_name = 'quote_quantity'`).Scan(&count); err != nil {
			return fmt.Errorf("check order_fills.quote_quantity migration: %w", err)
		}
		if count == 0 {
			_, err = db.Exec(`ALTER TABLE order_fills ADD COLUMN quote_quantity DECIMAL(38,18) NOT NULL DEFAULT 0 AFTER quantity`)
		}
		if err != nil {
			return fmt.Errorf("migrate order_fills.quote_quantity: %w", err)
		}
		for _, col := range []struct{ name, ddl string }{
			{"realized_pnl_asset", `ALTER TABLE order_fills ADD COLUMN realized_pnl_asset VARCHAR(32) NOT NULL DEFAULT ''`},
			{"commission_quote", `ALTER TABLE order_fills ADD COLUMN commission_quote DECIMAL(38,18) NOT NULL DEFAULT 0`},
			{"commission_quote_rate", `ALTER TABLE order_fills ADD COLUMN commission_quote_rate DECIMAL(38,18) NOT NULL DEFAULT 0`},
			{"commission_quote_known", `ALTER TABLE order_fills ADD COLUMN commission_quote_known BOOLEAN NOT NULL DEFAULT FALSE`},
		} {
			if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'order_fills' AND column_name = ?`, col.name).Scan(&count); err != nil {
				return fmt.Errorf("check order_fills.%s migration: %w", col.name, err)
			}
			if count == 0 {
				if _, err := db.Exec(col.ddl); err != nil {
					return fmt.Errorf("migrate order_fills.%s: %w", col.name, err)
				}
			}
		}
		return nil
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS order_fills (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exchange TEXT NOT NULL, market_type TEXT NOT NULL, account_scope TEXT NOT NULL,
		account TEXT NOT NULL DEFAULT '', bot_id TEXT NOT NULL DEFAULT '', symbol TEXT NOT NULL,
		trade_id TEXT NOT NULL, order_id BIGINT NOT NULL, side TEXT NOT NULL,
		price DECIMAL(38,18) NOT NULL, quantity DECIMAL(38,18) NOT NULL, quote_quantity DECIMAL(38,18) NOT NULL DEFAULT 0,
		commission DECIMAL(38,18) NOT NULL DEFAULT 0, commission_asset TEXT NOT NULL DEFAULT '',
		commission_quote DECIMAL(38,18) NOT NULL DEFAULT 0, commission_quote_rate DECIMAL(38,18) NOT NULL DEFAULT 0, commission_quote_known INTEGER NOT NULL DEFAULT 0,
		realized_pnl DECIMAL(38,18), realized_pnl_asset TEXT NOT NULL DEFAULT '', trade_time TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(exchange, market_type, account_scope, symbol, trade_id)
	);
	CREATE INDEX IF NOT EXISTS idx_order_fills_scope_time ON order_fills(account_scope, exchange, market_type, symbol, trade_time)`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS order_fill_sync_coverage (
		scope_hash TEXT NOT NULL, exchange_name TEXT NOT NULL, market_type TEXT NOT NULL,
		symbol TEXT NOT NULL, account_scope TEXT NOT NULL, covered_from TIMESTAMP NOT NULL,
		covered_through TIMESTAMP NOT NULL, updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (scope_hash, exchange_name, market_type, symbol)
	)`)
	if err != nil {
		return err
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('order_fills') WHERE name = 'quote_quantity'`).Scan(&count); err != nil {
		return fmt.Errorf("check order_fills.quote_quantity migration: %w", err)
	}
	if count == 0 {
		_, err = db.Exec(`ALTER TABLE order_fills ADD COLUMN quote_quantity DECIMAL(38,18) NOT NULL DEFAULT 0`)
	}
	if err != nil {
		return err
	}
	for _, col := range []struct{ name, ddl string }{
		{"realized_pnl_asset", `ALTER TABLE order_fills ADD COLUMN realized_pnl_asset TEXT NOT NULL DEFAULT ''`},
		{"commission_quote", `ALTER TABLE order_fills ADD COLUMN commission_quote DECIMAL(38,18) NOT NULL DEFAULT 0`},
		{"commission_quote_rate", `ALTER TABLE order_fills ADD COLUMN commission_quote_rate DECIMAL(38,18) NOT NULL DEFAULT 0`},
		{"commission_quote_known", `ALTER TABLE order_fills ADD COLUMN commission_quote_known INTEGER NOT NULL DEFAULT 0`},
	} {
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('order_fills') WHERE name = ?`, col.name).Scan(&count); err != nil {
			return fmt.Errorf("check order_fills.%s migration: %w", col.name, err)
		}
		if count == 0 {
			if _, err := db.Exec(col.ddl); err != nil {
				return fmt.Errorf("migrate order_fills.%s: %w", col.name, err)
			}
		}
	}
	return nil
}

type DailyOrderFillSummary struct {
	BuyOrders, SellOrders                int
	BuyQty, BuyValue, SellQty, SellValue float64
	RealizedPnL                          float64
	FillCount                            int
	FeesByAsset                          map[string]float64
	FeeQuoteValueByAsset                 map[string]float64
	HistoricalFeeQuoteValueByAsset       map[string]float64
	FeeQuoteUnknownCountByAsset          map[string]int
}

// QueryDailyOrderFillsByScope derives daily execution economics from the
// exchange fill ledger, never from submitted order quantity or limit price.
func (s *SQLStorage) QueryDailyOrderFillsByScope(account, exchange, marketType, symbol, accountScope string, start, end time.Time) (DailyOrderFillSummary, error) {
	var summary DailyOrderFillSummary
	if exchange == "" || marketType == "" || symbol == "" || accountScope == "" || !start.Before(end) {
		return summary, fmt.Errorf("daily execution query requires a complete account and market scope")
	}
	query := `SELECT
		COUNT(DISTINCT CASE WHEN UPPER(side) = 'BUY' THEN order_id END),
		COUNT(DISTINCT CASE WHEN UPPER(side) = 'SELL' THEN order_id END),
		COALESCE(SUM(CASE WHEN UPPER(side) = 'BUY' THEN quantity ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN UPPER(side) = 'BUY' THEN CASE WHEN quote_quantity > 0 THEN quote_quantity ELSE price * quantity END ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN UPPER(side) = 'SELL' THEN quantity ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN UPPER(side) = 'SELL' THEN CASE WHEN quote_quantity > 0 THEN quote_quantity ELSE price * quantity END ELSE 0 END), 0),
		COALESCE(SUM(COALESCE(realized_pnl, 0)), 0), COUNT(*)
		FROM order_fills WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND trade_time >= ? AND trade_time < ?`
	args := []interface{}{exchange, marketType, symbol, accountScope, start, end}
	if account != "" {
		query += ` AND account = ?`
		args = append(args, account)
	}
	if err := s.db.QueryRow(query, args...).Scan(&summary.BuyOrders, &summary.SellOrders, &summary.BuyQty, &summary.BuyValue,
		&summary.SellQty, &summary.SellValue, &summary.RealizedPnL, &summary.FillCount); err != nil {
		return DailyOrderFillSummary{}, fmt.Errorf("query daily execution aggregate: %w", err)
	}
	feeQuery := `SELECT commission_asset, SUM(commission), SUM(commission * price), SUM(CASE WHEN commission_quote_known = 1 THEN commission_quote ELSE 0 END), SUM(CASE WHEN commission <> 0 AND commission_quote_known = 0 THEN 1 ELSE 0 END) FROM order_fills WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND trade_time >= ? AND trade_time < ?`
	if account != "" {
		feeQuery += ` AND account = ?`
	}
	feeQuery += ` GROUP BY commission_asset`
	feeRows, err := s.db.Query(feeQuery, args...)
	if err != nil {
		return DailyOrderFillSummary{}, fmt.Errorf("query daily execution fees by asset: %w", err)
	}
	defer feeRows.Close()
	summary.FeesByAsset = make(map[string]float64)
	summary.FeeQuoteValueByAsset = make(map[string]float64)
	summary.HistoricalFeeQuoteValueByAsset = make(map[string]float64)
	summary.FeeQuoteUnknownCountByAsset = make(map[string]int)
	for feeRows.Next() {
		var asset string
		var amount, quoteValue, convertedValue float64
		var unknownCount int
		if err := feeRows.Scan(&asset, &amount, &quoteValue, &convertedValue, &unknownCount); err != nil {
			return DailyOrderFillSummary{}, fmt.Errorf("scan daily execution fees: %w", err)
		}
		asset = strings.ToUpper(strings.TrimSpace(asset))
		if asset == "" && amount > 0 {
			asset = "UNKNOWN"
		}
		summary.FeesByAsset[asset] += amount
		summary.FeeQuoteValueByAsset[asset] += quoteValue
		summary.HistoricalFeeQuoteValueByAsset[asset] += convertedValue
		summary.FeeQuoteUnknownCountByAsset[asset] += unknownCount
	}
	if err := feeRows.Err(); err != nil {
		return DailyOrderFillSummary{}, fmt.Errorf("iterate daily execution fees: %w", err)
	}
	return summary, nil
}

// QueryTopDailyRealizedFills ranks complete per-order realized PnL using the
// execution ledger, not order-level averages that can omit partial fills.
func (s *SQLStorage) QueryTopDailyRealizedFills(account, exchange, marketType, symbol, accountScope string, start, end time.Time) (winners, losers []*Order, err error) {
	if exchange == "" || marketType == "" || symbol == "" || accountScope == "" || !start.Before(end) {
		return nil, nil, fmt.Errorf("daily realized execution ranking requires a complete account and market scope")
	}
	queryTop := func(operator, direction string) ([]*Order, error) {
		query := `SELECT order_id, MAX(side), SUM(CASE WHEN quote_quantity > 0 THEN quote_quantity ELSE price * quantity END) / NULLIF(SUM(quantity), 0), SUM(quantity), SUM(COALESCE(realized_pnl, 0))
			FROM order_fills WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND trade_time >= ? AND trade_time < ?`
		args := []interface{}{exchange, marketType, symbol, accountScope, start, end}
		if account != "" {
			query += ` AND account = ?`
			args = append(args, account)
		}
		query += ` GROUP BY order_id HAVING SUM(COALESCE(realized_pnl, 0)) ` + operator + ` 0 ORDER BY SUM(COALESCE(realized_pnl, 0)) ` + direction + `, order_id DESC LIMIT 20`
		rows, err := s.db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("query daily realized execution ranking: %w", err)
		}
		defer rows.Close()
		var result []*Order
		for rows.Next() {
			order := &Order{RealizedPnL: new(float64), Status: "FILLED"}
			if err := rows.Scan(&order.OrderID, &order.Side, &order.Price, &order.FilledQty, order.RealizedPnL); err != nil {
				return nil, fmt.Errorf("scan daily realized execution ranking: %w", err)
			}
			result = append(result, order)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate daily realized execution ranking: %w", err)
		}
		return result, nil
	}
	winners, err = queryTop(">", "DESC")
	if err != nil {
		return nil, nil, err
	}
	losers, err = queryTop("<", "ASC")
	if err != nil {
		return nil, nil, err
	}
	return winners, losers, nil
}

// SaveOrderFill 插入逐笔成交；重复身份只有在经济字段一致时视为安全重放。
func (s *SQLStorage) SaveOrderFill(fill *OrderFill) error {
	if fill == nil || fill.Exchange == "" || fill.MarketType == "" || fill.AccountScope == "" || fill.Symbol == "" || fill.TradeID == "" || fill.OrderID == 0 || (fill.Side != "BUY" && fill.Side != "SELL") || fill.TradeTime.IsZero() || !finitePositive(fill.Price) || !finitePositive(fill.Quantity) || math.IsNaN(fill.Commission) || math.IsInf(fill.Commission, 0) {
		return fmt.Errorf("order fill requires valid scoped identity, time, price, quantity, and commission")
	}
	tradeTime := utils.ToUTC(fill.TradeTime)
	var realized interface{}
	if fill.RealizedPnL != nil {
		if math.IsNaN(*fill.RealizedPnL) || math.IsInf(*fill.RealizedPnL, 0) {
			return fmt.Errorf("order fill realized pnl must be finite")
		}
		realized = *fill.RealizedPnL
	}
	if !finiteNonNegative(fill.QuoteQuantity) {
		return fmt.Errorf("order fill quote quantity must be finite and non-negative")
	}
	if !finiteNonNegative(fill.CommissionQuoteRate) || math.IsNaN(fill.CommissionQuote) || math.IsInf(fill.CommissionQuote, 0) || (fill.CommissionQuoteKnown && fill.Commission != 0 && !finitePositive(fill.CommissionQuoteRate)) {
		return fmt.Errorf("order fill commission conversion must be finite with a positive rate")
	}
	if fill.RealizedPnL == nil && strings.TrimSpace(fill.RealizedPnLAsset) != "" {
		return fmt.Errorf("order fill cannot claim a realized PnL asset without realized PnL")
	}
	args := []interface{}{fill.Exchange, fill.MarketType, fill.AccountScope, fill.Account, fill.BotID, fill.Symbol, fill.TradeID, fill.OrderID, fill.Side, fill.Price, fill.Quantity, fill.QuoteQuantity, fill.Commission, fill.CommissionAsset, fill.CommissionQuote, fill.CommissionQuoteRate, fill.CommissionQuoteKnown, realized, fill.RealizedPnLAsset, tradeTime}
	_, err := s.db.Exec(`INSERT INTO order_fills (exchange, market_type, account_scope, account, bot_id, symbol, trade_id, order_id, side, price, quantity, quote_quantity, commission, commission_asset, commission_quote, commission_quote_rate, commission_quote_known, realized_pnl, realized_pnl_asset, trade_time) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, args...)
	if err == nil {
		return nil
	}
	var old OrderFill
	var oldCommissionQuoteKnown bool
	var oldQuoteQuantity float64
	var oldPnL sql.NullFloat64
	var oldTime time.Time
	readErr := s.db.QueryRow(`SELECT exchange, market_type, account_scope, account, bot_id, symbol, trade_id, order_id, side, price, quantity, quote_quantity, commission, commission_asset, commission_quote, commission_quote_rate, commission_quote_known, realized_pnl, realized_pnl_asset, trade_time FROM order_fills WHERE exchange = ? AND market_type = ? AND account_scope = ? AND symbol = ? AND trade_id = ?`, fill.Exchange, fill.MarketType, fill.AccountScope, fill.Symbol, fill.TradeID).Scan(&old.Exchange, &old.MarketType, &old.AccountScope, &old.Account, &old.BotID, &old.Symbol, &old.TradeID, &old.OrderID, &old.Side, &old.Price, &old.Quantity, &oldQuoteQuantity, &old.Commission, &old.CommissionAsset, &old.CommissionQuote, &old.CommissionQuoteRate, &oldCommissionQuoteKnown, &oldPnL, &old.RealizedPnLAsset, &oldTime)
	old.CommissionQuoteKnown = oldCommissionQuoteKnown
	if readErr != nil {
		return fmt.Errorf("insert order fill: %w (identity lookup: %v)", err, readErr)
	}
	samePnL := !oldPnL.Valid || fill.RealizedPnL == nil || *fill.RealizedPnL == oldPnL.Float64
	samePnLAsset := old.RealizedPnLAsset == "" || fill.RealizedPnLAsset == "" || strings.EqualFold(old.RealizedPnLAsset, fill.RealizedPnLAsset)
	sameAccount := old.Account == fill.Account || old.Account == "" || fill.Account == ""
	sameBot := old.BotID == fill.BotID || old.BotID == "" || fill.BotID == ""
	sameQuoteQuantity := oldQuoteQuantity == fill.QuoteQuantity || oldQuoteQuantity == 0 || fill.QuoteQuantity == 0
	sameFeeConversion := !old.CommissionQuoteKnown || !fill.CommissionQuoteKnown || (old.CommissionQuote == fill.CommissionQuote && old.CommissionQuoteRate == fill.CommissionQuoteRate)
	if old.OrderID != fill.OrderID || old.Side != fill.Side || old.Price != fill.Price || old.Quantity != fill.Quantity || !sameQuoteQuantity || old.Commission != fill.Commission || old.CommissionAsset != fill.CommissionAsset || !samePnL || !samePnLAsset || !sameFeeConversion || !oldTime.Equal(tradeTime) || !sameAccount || !sameBot {
		return fmt.Errorf("order fill identity collision has conflicting economic fields: %s/%s", fill.Exchange, fill.TradeID)
	}
	if (!oldPnL.Valid && fill.RealizedPnL != nil) || (old.RealizedPnLAsset == "" && fill.RealizedPnLAsset != "") || (old.Account == "" && fill.Account != "") || (old.BotID == "" && fill.BotID != "") || (oldQuoteQuantity == 0 && fill.QuoteQuantity > 0) || (!old.CommissionQuoteKnown && fill.CommissionQuoteKnown) {
		_, err := s.db.Exec(`UPDATE order_fills SET account = CASE WHEN account = '' THEN ? ELSE account END, bot_id = CASE WHEN bot_id = '' THEN ? ELSE bot_id END, realized_pnl = COALESCE(realized_pnl, ?), realized_pnl_asset = CASE WHEN realized_pnl_asset = '' THEN ? ELSE realized_pnl_asset END, quote_quantity = CASE WHEN quote_quantity = 0 THEN ? ELSE quote_quantity END, commission_quote = CASE WHEN commission_quote_known = 0 THEN ? ELSE commission_quote END, commission_quote_rate = CASE WHEN commission_quote_known = 0 THEN ? ELSE commission_quote_rate END, commission_quote_known = CASE WHEN commission_quote_known = 0 THEN ? ELSE commission_quote_known END WHERE exchange = ? AND market_type = ? AND account_scope = ? AND symbol = ? AND trade_id = ?`, fill.Account, fill.BotID, realized, fill.RealizedPnLAsset, fill.QuoteQuantity, fill.CommissionQuote, fill.CommissionQuoteRate, fill.CommissionQuoteKnown, fill.Exchange, fill.MarketType, fill.AccountScope, fill.Symbol, fill.TradeID)
		if err != nil {
			return fmt.Errorf("enrich existing execution attribution: %w", err)
		}
	}
	return nil
}

func finitePositive(v float64) bool    { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
func finiteNonNegative(v float64) bool { return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
