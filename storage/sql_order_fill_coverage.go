package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
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

// OrderFillStream is an exact exchange/market/symbol stream present in the raw
// execution ledger. It is independent of paired PnL rows and current config.
type OrderFillStream struct {
	Exchange   string
	MarketType string
	Symbol     string
}

type ExecutionAccountScope struct {
	Exchange string
	Scope    string
}

// OrderFillLedgerIntegrity reports whether raw executions can be reconciled to
// paired trades without assuming a fee conversion currency.
type OrderFillLedgerIntegrity struct {
	UnpairedOrderCount          int64
	MissingFillOrderCount       int64
	InvalidTradeOrderCount      int64
	ExecutionMismatchOrderCount int64
	UnvaluedFeeCount            int64
	RawFeeTotal                 float64
	TradeFeeTotal               float64
	FeeTotalsMatch              bool
}

const maxVerifiedOrderFillStreams = 1000

// ListAccountExecutionScopesByExchange enumerates every durable trade/fill
// account scope so a missing retired-scope archive cannot silently hide history.
func (s *SQLStorage) ListAccountExecutionScopesByExchange(exchange string) ([]ExecutionAccountScope, int64, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("account execution scope discovery requires storage")
	}
	var invalid int64
	query := `SELECT COUNT(*) FROM (
		SELECT account_scope FROM ` + s.tradesTbl() + ` WHERE (? = '' OR LOWER(TRIM(exchange)) = LOWER(TRIM(?))) AND TRIM(COALESCE(account_scope, '')) = ''
		UNION ALL
		SELECT account_scope FROM order_fills WHERE (? = '' OR LOWER(TRIM(exchange)) = LOWER(TRIM(?))) AND TRIM(COALESCE(account_scope, '')) = ''
	) invalid_scopes`
	if err := s.db.QueryRow(query, exchange, exchange, exchange, exchange).Scan(&invalid); err != nil {
		return nil, 0, fmt.Errorf("count unattributed execution account scopes: %w", err)
	}
	query = `SELECT exchange, account_scope FROM (
		SELECT LOWER(TRIM(exchange)) AS exchange, account_scope FROM ` + s.tradesTbl() + ` WHERE (? = '' OR LOWER(TRIM(exchange)) = LOWER(TRIM(?))) AND TRIM(COALESCE(account_scope, '')) <> ''
		UNION
		SELECT LOWER(TRIM(exchange)) AS exchange, account_scope FROM order_fills WHERE (? = '' OR LOWER(TRIM(exchange)) = LOWER(TRIM(?))) AND TRIM(COALESCE(account_scope, '')) <> ''
	) account_scopes ORDER BY exchange, account_scope LIMIT ?`
	rows, err := s.db.Query(query, exchange, exchange, exchange, exchange, maxVerifiedOrderFillStreams+1)
	if err != nil {
		return nil, invalid, fmt.Errorf("list execution account scopes: %w", err)
	}
	defer rows.Close()
	scopes := make([]ExecutionAccountScope, 0)
	for rows.Next() {
		var scope ExecutionAccountScope
		if err := rows.Scan(&scope.Exchange, &scope.Scope); err != nil {
			return nil, invalid, fmt.Errorf("scan execution account scope: %w", err)
		}
		scope.Exchange = strings.ToLower(strings.TrimSpace(scope.Exchange))
		scope.Scope = strings.TrimSpace(scope.Scope)
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, invalid, fmt.Errorf("iterate execution account scopes: %w", err)
	}
	if len(scopes) > maxVerifiedOrderFillStreams {
		return nil, invalid, fmt.Errorf("execution account scope count exceeds verification limit %d", maxVerifiedOrderFillStreams)
	}
	return scopes, invalid, nil
}

// ListOrderFillStreamsByAccountScope enumerates raw ledger streams, including
// fills that have no paired PnL row and streams removed from current config.
func (s *SQLStorage) ListOrderFillStreamsByAccountScope(exchange, accountScope string) ([]OrderFillStream, int64, error) {
	if s == nil || s.db == nil || strings.TrimSpace(exchange) == "" || strings.TrimSpace(accountScope) == "" {
		return nil, 0, fmt.Errorf("raw order-fill stream discovery requires storage, exchange and account scope")
	}
	var invalid int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM order_fills WHERE account_scope = ? AND
		(LOWER(TRIM(COALESCE(exchange, ''))) <> LOWER(TRIM(?)) OR TRIM(COALESCE(market_type, '')) = '' OR TRIM(COALESCE(symbol, '')) = '')`, accountScope, exchange).Scan(&invalid); err != nil {
		return nil, 0, fmt.Errorf("count invalid raw order-fill identities: %w", err)
	}
	rows, err := s.db.Query(`SELECT exchange, market_type, symbol FROM order_fills WHERE account_scope = ? AND LOWER(TRIM(exchange)) = LOWER(TRIM(?)) GROUP BY exchange, market_type, symbol ORDER BY exchange, market_type, symbol LIMIT ?`, accountScope, exchange, maxVerifiedOrderFillStreams+1)
	if err != nil {
		return nil, 0, fmt.Errorf("list raw order-fill streams: %w", err)
	}
	defer rows.Close()
	streams := make([]OrderFillStream, 0)
	for rows.Next() {
		var stream OrderFillStream
		if err := rows.Scan(&stream.Exchange, &stream.MarketType, &stream.Symbol); err != nil {
			return nil, 0, fmt.Errorf("scan raw order-fill stream: %w", err)
		}
		stream.Exchange = strings.ToLower(strings.TrimSpace(stream.Exchange))
		stream.MarketType = strings.ToLower(strings.TrimSpace(stream.MarketType))
		stream.Symbol = strings.ToUpper(strings.TrimSpace(stream.Symbol))
		if stream.Exchange == "" || stream.MarketType == "" || stream.Symbol == "" {
			continue
		}
		streams = append(streams, stream)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate raw order-fill streams: %w", err)
	}
	if len(streams) > maxVerifiedOrderFillStreams {
		return nil, invalid, fmt.Errorf("raw order-fill stream count exceeds verification limit %d", maxVerifiedOrderFillStreams)
	}
	return streams, invalid, nil
}

// GetOrderFillLedgerIntegrity rejects unpaired orders, unvalued fee currencies,
// and disagreement between raw fill fees and paired trade fees. Non-quote fee
// conversions remain unprovable because order_fills stores no quote-asset ID.
func (s *SQLStorage) GetOrderFillLedgerIntegrity(exchange, marketType, symbol, accountScope, pnlAsset string) (OrderFillLedgerIntegrity, error) {
	var result OrderFillLedgerIntegrity
	if s == nil || s.db == nil || strings.TrimSpace(exchange) == "" || strings.TrimSpace(marketType) == "" || strings.TrimSpace(symbol) == "" || strings.TrimSpace(accountScope) == "" || strings.TrimSpace(pnlAsset) == "" {
		return result, fmt.Errorf("order-fill integrity requires complete exchange, market, symbol, account and asset scope")
	}
	if err := s.db.QueryRow(`SELECT COUNT(DISTINCT f.order_id) FROM order_fills f
		WHERE LOWER(TRIM(f.exchange)) = LOWER(TRIM(?)) AND LOWER(TRIM(f.market_type)) = LOWER(TRIM(?))
		  AND UPPER(TRIM(f.symbol)) = UPPER(TRIM(?)) AND f.account_scope = ?
		  AND NOT EXISTS (SELECT 1 FROM `+s.tradesTbl()+` t WHERE LOWER(TRIM(t.exchange)) = LOWER(TRIM(f.exchange))
		    AND LOWER(TRIM(t.market_type)) = LOWER(TRIM(f.market_type)) AND UPPER(TRIM(t.symbol)) = UPPER(TRIM(f.symbol))
		    AND t.account_scope = f.account_scope AND (t.buy_order_id = f.order_id OR t.sell_order_id = f.order_id))`,
		exchange, marketType, symbol, accountScope).Scan(&result.UnpairedOrderCount); err != nil {
		return result, fmt.Errorf("count unpaired raw execution orders: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM (
		SELECT DISTINCT order_id FROM (
			SELECT t.buy_order_id AS order_id FROM `+s.tradesTbl()+` t WHERE LOWER(TRIM(t.exchange)) = LOWER(TRIM(?))
			  AND LOWER(TRIM(t.market_type)) = LOWER(TRIM(?)) AND UPPER(TRIM(t.symbol)) = UPPER(TRIM(?)) AND t.account_scope = ?
			  AND t.buy_order_id IS NOT NULL AND t.buy_order_id <> 0 AND NOT EXISTS (
				SELECT 1 FROM order_fills f WHERE LOWER(TRIM(f.exchange)) = LOWER(TRIM(t.exchange))
				  AND LOWER(TRIM(f.market_type)) = LOWER(TRIM(t.market_type)) AND UPPER(TRIM(f.symbol)) = UPPER(TRIM(t.symbol))
				  AND f.account_scope = t.account_scope AND f.order_id = t.buy_order_id)
			UNION ALL
			SELECT t.sell_order_id AS order_id FROM `+s.tradesTbl()+` t WHERE LOWER(TRIM(t.exchange)) = LOWER(TRIM(?))
			  AND LOWER(TRIM(t.market_type)) = LOWER(TRIM(?)) AND UPPER(TRIM(t.symbol)) = UPPER(TRIM(?)) AND t.account_scope = ?
			  AND t.sell_order_id IS NOT NULL AND t.sell_order_id <> 0 AND NOT EXISTS (
				SELECT 1 FROM order_fills f WHERE LOWER(TRIM(f.exchange)) = LOWER(TRIM(t.exchange))
				  AND LOWER(TRIM(f.market_type)) = LOWER(TRIM(t.market_type)) AND UPPER(TRIM(f.symbol)) = UPPER(TRIM(t.symbol))
				  AND f.account_scope = t.account_scope AND f.order_id = t.sell_order_id)
		) missing_orders
	) missing_order_ids`, exchange, marketType, symbol, accountScope, exchange, marketType, symbol, accountScope).Scan(&result.MissingFillOrderCount); err != nil {
		return result, fmt.Errorf("count paired trade orders missing raw fills: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM `+s.tradesTbl()+` WHERE LOWER(TRIM(exchange)) = LOWER(TRIM(?))
		AND LOWER(TRIM(market_type)) = LOWER(TRIM(?)) AND UPPER(TRIM(symbol)) = UPPER(TRIM(?)) AND account_scope = ?
		AND (buy_order_id IS NULL OR buy_order_id = 0 OR sell_order_id IS NULL OR sell_order_id = 0 OR buy_order_id = sell_order_id)`,
		exchange, marketType, symbol, accountScope).Scan(&result.InvalidTradeOrderCount); err != nil {
		return result, fmt.Errorf("count trades with invalid order identities: %w", err)
	}
	mismatchCount, err := s.countExecutionQuantityMismatches(exchange, marketType, symbol, accountScope)
	if err != nil {
		return result, err
	}
	result.ExecutionMismatchOrderCount = mismatchCount
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM order_fills WHERE LOWER(TRIM(exchange)) = LOWER(TRIM(?))
		AND LOWER(TRIM(market_type)) = LOWER(TRIM(?)) AND UPPER(TRIM(symbol)) = UPPER(TRIM(?)) AND account_scope = ?
		AND commission <> 0 AND UPPER(TRIM(COALESCE(commission_asset, ''))) <> UPPER(TRIM(?))`,
		exchange, marketType, symbol, accountScope, pnlAsset).Scan(&result.UnvaluedFeeCount); err != nil {
		return result, fmt.Errorf("count execution fees without proven PnL-asset denomination: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(commission), 0) FROM order_fills WHERE LOWER(TRIM(exchange)) = LOWER(TRIM(?))
		AND LOWER(TRIM(market_type)) = LOWER(TRIM(?)) AND UPPER(TRIM(symbol)) = UPPER(TRIM(?)) AND account_scope = ?
		AND UPPER(TRIM(COALESCE(commission_asset, ''))) = UPPER(TRIM(?))`,
		exchange, marketType, symbol, accountScope, pnlAsset).Scan(&result.RawFeeTotal); err != nil {
		return result, fmt.Errorf("sum raw execution fees: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(fee), 0) FROM `+s.tradesTbl()+` WHERE LOWER(TRIM(exchange)) = LOWER(TRIM(?))
		AND LOWER(TRIM(market_type)) = LOWER(TRIM(?)) AND UPPER(TRIM(symbol)) = UPPER(TRIM(?)) AND account_scope = ?
		AND UPPER(TRIM(COALESCE(fee_asset, ''))) = UPPER(TRIM(?))`,
		exchange, marketType, symbol, accountScope, pnlAsset).Scan(&result.TradeFeeTotal); err != nil {
		return result, fmt.Errorf("sum paired trade fees: %w", err)
	}
	feeDelta := result.RawFeeTotal - result.TradeFeeTotal
	result.FeeTotalsMatch = math.Abs(feeDelta) <= 1e-8*math.Max(1, math.Max(math.Abs(result.RawFeeTotal), math.Abs(result.TradeFeeTotal)))
	return result, nil
}

type executionOrderAggregate struct {
	side     string
	quantity float64
}

func (s *SQLStorage) countExecutionQuantityMismatches(exchange, marketType, symbol, accountScope string) (int64, error) {
	tradeRows, err := s.db.Query(`SELECT buy_order_id, sell_order_id, quantity FROM `+s.tradesTbl()+`
		WHERE LOWER(TRIM(exchange)) = LOWER(TRIM(?)) AND LOWER(TRIM(market_type)) = LOWER(TRIM(?))
		AND UPPER(TRIM(symbol)) = UPPER(TRIM(?)) AND account_scope = ?`, exchange, marketType, symbol, accountScope)
	if err != nil {
		return 0, fmt.Errorf("read trade-leg execution quantities: %w", err)
	}
	expected := make(map[int64]executionOrderAggregate)
	for tradeRows.Next() {
		var buyOrderID, sellOrderID sql.NullInt64
		var quantity float64
		if err := tradeRows.Scan(&buyOrderID, &sellOrderID, &quantity); err != nil {
			tradeRows.Close()
			return 0, fmt.Errorf("scan trade-leg execution quantities: %w", err)
		}
		if !buyOrderID.Valid || !sellOrderID.Valid || buyOrderID.Int64 <= 0 || sellOrderID.Int64 <= 0 || buyOrderID.Int64 == sellOrderID.Int64 || !finiteOrderFillQuantity(quantity) || quantity <= 0 {
			continue
		}
		for _, leg := range []struct {
			id   int64
			side string
		}{{buyOrderID.Int64, "BUY"}, {sellOrderID.Int64, "SELL"}} {
			current := expected[leg.id]
			if current.side != "" && current.side != leg.side {
				current.side = "CONFLICT"
			}
			current.side = current.sideOr(leg.side)
			current.quantity += quantity
			expected[leg.id] = current
		}
	}
	if err := tradeRows.Err(); err != nil {
		tradeRows.Close()
		return 0, fmt.Errorf("iterate trade-leg execution quantities: %w", err)
	}
	if err := tradeRows.Close(); err != nil {
		return 0, err
	}
	rawRows, err := s.db.Query(`SELECT order_id, UPPER(TRIM(side)), SUM(quantity) FROM order_fills
		WHERE LOWER(TRIM(exchange)) = LOWER(TRIM(?)) AND LOWER(TRIM(market_type)) = LOWER(TRIM(?))
		AND UPPER(TRIM(symbol)) = UPPER(TRIM(?)) AND account_scope = ? GROUP BY order_id, UPPER(TRIM(side))`, exchange, marketType, symbol, accountScope)
	if err != nil {
		return 0, fmt.Errorf("read raw fill order quantities: %w", err)
	}
	actual := make(map[int64]executionOrderAggregate)
	for rawRows.Next() {
		var orderID int64
		var side string
		var quantity float64
		if err := rawRows.Scan(&orderID, &side, &quantity); err != nil {
			rawRows.Close()
			return 0, fmt.Errorf("scan raw fill order quantities: %w", err)
		}
		current := actual[orderID]
		if current.side != "" && current.side != side {
			current.side = "CONFLICT"
		} else if current.side == "" {
			current.side = side
		}
		current.quantity += quantity
		actual[orderID] = current
	}
	if err := rawRows.Err(); err != nil {
		rawRows.Close()
		return 0, fmt.Errorf("iterate raw fill order quantities: %w", err)
	}
	if err := rawRows.Close(); err != nil {
		return 0, err
	}
	mismatched := make(map[int64]struct{})
	for orderID, want := range expected {
		got, found := actual[orderID]
		if !found || got.side != want.side || want.side == "CONFLICT" || got.side == "CONFLICT" || !orderFillQuantitiesEqual(got.quantity, want.quantity) {
			mismatched[orderID] = struct{}{}
		}
	}
	for orderID := range actual {
		if _, found := expected[orderID]; !found {
			mismatched[orderID] = struct{}{}
		}
	}
	return int64(len(mismatched)), nil
}

func (a executionOrderAggregate) sideOr(side string) string {
	if a.side == "CONFLICT" {
		return "CONFLICT"
	}
	if a.side == "" {
		return side
	}
	return a.side
}

func finiteOrderFillQuantity(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func orderFillQuantitiesEqual(a, b float64) bool {
	return finiteOrderFillQuantity(a) && finiteOrderFillQuantity(b) && math.Abs(a-b) <= 1e-8*math.Max(1, math.Max(math.Abs(a), math.Abs(b)))
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
