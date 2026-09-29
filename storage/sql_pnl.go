package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var ErrPnLScopeRequired = errors.New("PnL exchange and market_type are required when a symbol has trades in multiple scopes")

type PnLMarketScope struct {
	Exchange   string
	MarketType string
}

// ========== 配對成交 PnL 查詢 存儲 ==========

// GetPnLBySymbol 按币种對查詢盈亏數據（TotalPnL 為淨利潤，已扣手續費）
func (s *SQLStorage) GetPnLBySymbol(symbol, account string, startTime, endTime time.Time) (*PnLSummary, error) {
	scopes, err := s.listPnLMarketScopes(symbol, account, startTime, endTime)
	if err != nil {
		return nil, err
	}
	if len(scopes) > 1 {
		return nil, ErrPnLScopeRequired
	}
	scope := PnLMarketScope{}
	if len(scopes) == 1 {
		scope = scopes[0]
	}
	return s.getPnLBySymbolScope(symbol, account, scope.Exchange, scope.MarketType, startTime, endTime)
}

// GetPnLBySymbolScope keeps exchange and market ledgers separate. Empty legacy
// exchange/market values are addressed as "unknown" and are never guessed.
func (s *SQLStorage) GetPnLBySymbolScope(symbol, account, exchange, marketType string, startTime, endTime time.Time) (*PnLSummary, error) {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	marketType = strings.ToLower(strings.TrimSpace(marketType))
	if exchange == "" || marketType == "" {
		return nil, fmt.Errorf("exchange and market_type are required")
	}
	return s.getPnLBySymbolScope(symbol, account, exchange, marketType, startTime, endTime)
}

// GetPnLBySymbolAccountScope queries exact credential scope, including legacy
// rows whose display/account column still contains an API-key prefix.
func (s *SQLStorage) GetPnLBySymbolAccountScope(symbol, accountScope, exchange, marketType string, startTime, endTime time.Time) (*PnLSummary, error) {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	marketType = strings.ToLower(strings.TrimSpace(marketType))
	if strings.TrimSpace(accountScope) == "" || exchange == "" || marketType == "" {
		return nil, fmt.Errorf("account_scope, exchange and market_type are required")
	}
	query := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0),
		COALESCE(SUM(quantity), 0), COALESCE(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN pnl < 0 THEN 1 ELSE 0 END), 0)
		FROM %s WHERE symbol = ? AND account_scope = ? AND LOWER(TRIM(exchange)) = ? AND LOWER(TRIM(market_type)) = ?
		AND created_at >= ? AND created_at <= ?`, s.tradesTbl())
	var summary PnLSummary
	summary.Symbol, summary.Exchange, summary.MarketType = symbol, exchange, marketType
	var totalTrades, winners, losers int
	if err := s.db.QueryRow(query, symbol, accountScope, exchange, marketType, startTime, endTime).
		Scan(&totalTrades, &summary.TotalPnL, &summary.TotalVolume, &winners, &losers); err != nil {
		return nil, fmt.Errorf("query PnL by account scope for %s: %w", symbol, err)
	}
	summary.TotalTrades, summary.WinningTrades, summary.LosingTrades = totalTrades, winners, losers
	if totalTrades > 0 {
		summary.WinRate = float64(winners) / float64(totalTrades)
	}
	return &summary, nil
}

// GetPnLBySymbolAccountScopeAndAsset returns one strategy's PnL only when its
// immutable account, market and denomination are all explicit.
func (s *SQLStorage) GetPnLBySymbolAccountScopeAndAsset(symbol, accountScope, exchange, marketType, asset string, startTime, endTime time.Time) (*PnLSummary, error) {
	symbol = strings.TrimSpace(symbol)
	accountScope = strings.TrimSpace(accountScope)
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	marketType = strings.ToLower(strings.TrimSpace(marketType))
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if symbol == "" || accountScope == "" || exchange == "" || marketType == "" || asset == "" || startTime.IsZero() || endTime.IsZero() || endTime.Before(startTime) {
		return nil, fmt.Errorf("symbol, account_scope, exchange, market_type, asset and a valid time range are required")
	}
	var unowned int64
	if err := s.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE symbol = ? AND created_at >= ? AND created_at <= ?
		AND (TRIM(COALESCE(exchange, '')) = '' OR (LOWER(TRIM(exchange)) = ? AND (account_scope IS NULL OR TRIM(account_scope) = '' OR market_type IS NULL OR TRIM(market_type) = '')))`, s.tradesTbl()), symbol, startTime, endTime, exchange).Scan(&unowned); err != nil {
		return nil, fmt.Errorf("check incomplete runtime PnL ownership: %w", err)
	}
	if unowned > 0 {
		return nil, fmt.Errorf("runtime PnL is incomplete: %d trades lack exchange, account scope or market type", unowned)
	}
	var unclassified int64
	if err := s.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE symbol = ? AND account_scope = ? AND LOWER(TRIM(exchange)) = ? AND LOWER(TRIM(market_type)) = ? AND created_at >= ? AND created_at <= ?
		AND (TRIM(COALESCE(pnl_asset, '')) = '' OR (COALESCE(fee, 0) <> 0 AND UPPER(TRIM(COALESCE(fee_asset, ''))) <> UPPER(TRIM(COALESCE(pnl_asset, '')))))`, s.tradesTbl()), symbol, accountScope, exchange, marketType, startTime, endTime).Scan(&unclassified); err != nil {
		return nil, fmt.Errorf("check runtime PnL denomination evidence: %w", err)
	}
	if unclassified > 0 {
		return nil, fmt.Errorf("runtime PnL is incomplete: %d trades have unknown or mismatched PnL/fee assets", unclassified)
	}
	query := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0),
		COALESCE(SUM(exchange_pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0), COALESCE(SUM(quantity), 0),
		COALESCE(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN pnl < 0 THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN exchange_pnl > 0 THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN exchange_pnl < 0 THEN 1 ELSE 0 END), 0)
		FROM %s WHERE symbol = ? AND account_scope = ? AND LOWER(TRIM(exchange)) = ? AND LOWER(TRIM(market_type)) = ? AND UPPER(TRIM(pnl_asset)) = ? AND created_at >= ? AND created_at <= ?`, s.tradesTbl())
	var summary PnLSummary
	var winners, losers, exchangeWinners, exchangeLosers int
	summary.Symbol, summary.Exchange, summary.MarketType, summary.PnLAsset = symbol, exchange, marketType, asset
	if err := s.db.QueryRow(query, symbol, accountScope, exchange, marketType, asset, startTime, endTime).Scan(&summary.TotalTrades, &summary.TotalPnL, &summary.ExchangePnL, &summary.TotalVolume, &winners, &losers, &exchangeWinners, &exchangeLosers); err != nil {
		return nil, fmt.Errorf("query runtime scoped PnL for %s %s: %w", symbol, asset, err)
	}
	summary.WinningTrades, summary.LosingTrades = winners, losers
	if summary.TotalTrades > 0 {
		summary.WinRate = float64(winners) / float64(summary.TotalTrades)
		summary.ExchangeWinRate = float64(exchangeWinners) / float64(summary.TotalTrades)
	}
	for _, value := range []float64{summary.TotalPnL, summary.ExchangePnL, summary.TotalVolume, summary.WinRate} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("runtime PnL contains non-finite values for %s %s", symbol, asset)
		}
	}
	return &summary, nil
}

func (s *SQLStorage) listPnLMarketScopes(symbol, account string, startTime, endTime time.Time) ([]PnLMarketScope, error) {
	query := fmt.Sprintf(`SELECT DISTINCT COALESCE(NULLIF(LOWER(TRIM(exchange)), ''), 'unknown'), COALESCE(NULLIF(LOWER(TRIM(market_type)), ''), 'unknown') FROM %s WHERE symbol = ? AND created_at >= ? AND created_at <= ?`, s.tradesTbl())
	args := []interface{}{symbol, startTime, endTime}
	if account != "" {
		query += " AND account = ?"
		args = append(args, account)
	}
	query += " ORDER BY 1, 2"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query PnL market scopes for %s: %w", symbol, err)
	}
	defer rows.Close()
	var scopes []PnLMarketScope
	for rows.Next() {
		var scope PnLMarketScope
		if err := rows.Scan(&scope.Exchange, &scope.MarketType); err != nil {
			return nil, fmt.Errorf("scan PnL market scope for %s: %w", symbol, err)
		}
		scopes = append(scopes, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate PnL market scopes for %s: %w", symbol, err)
	}
	return scopes, nil
}

func (s *SQLStorage) getPnLBySymbolScope(symbol, account, exchange, marketType string, startTime, endTime time.Time) (*PnLSummary, error) {
	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_trades,
			COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) as total_pnl,
			SUM(quantity) as total_volume,
			SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) as winning_trades,
			SUM(CASE WHEN pnl < 0 THEN 1 ELSE 0 END) as losing_trades
		FROM %s
		WHERE symbol = ? AND created_at >= ? AND created_at <= ?
		`, s.tradesTbl())
	args := []interface{}{symbol, startTime, endTime}
	if exchange != "" {
		query += " AND COALESCE(NULLIF(LOWER(TRIM(exchange)), ''), 'unknown') = ?"
		args = append(args, exchange)
	}
	if marketType != "" {
		query += " AND COALESCE(NULLIF(LOWER(TRIM(market_type)), ''), 'unknown') = ?"
		args = append(args, marketType)
	}
	if account != "" {
		query += " AND account = ?"
		args = append(args, account)
	}

	row := s.db.QueryRow(query, args...)

	summary := &PnLSummary{
		Symbol: symbol, Exchange: exchange, MarketType: marketType,
	}

	var totalTrades sql.NullInt64
	var totalPnL sql.NullFloat64
	var totalVolume sql.NullFloat64
	var winningTrades sql.NullInt64
	var losingTrades sql.NullInt64

	err := row.Scan(&totalTrades, &totalPnL, &totalVolume, &winningTrades, &losingTrades)
	if err != nil {
		if err == sql.ErrNoRows {
			return summary, nil
		}
		return nil, fmt.Errorf("查詢盈亏數據失败: %w", err)
	}

	if totalTrades.Valid {
		summary.TotalTrades = int(totalTrades.Int64)
	}
	if totalPnL.Valid {
		summary.TotalPnL = totalPnL.Float64
	}
	if totalVolume.Valid {
		summary.TotalVolume = totalVolume.Float64
	}
	if winningTrades.Valid {
		summary.WinningTrades = int(winningTrades.Int64)
	}
	if losingTrades.Valid {
		summary.LosingTrades = int(losingTrades.Int64)
	}

	if summary.TotalTrades > 0 {
		summary.WinRate = float64(summary.WinningTrades) / float64(summary.TotalTrades)
	}

	return summary, nil
}

// GetPnLByTimeRange 按時间区间查詢盈亏數據（按币种對分组）
func (s *SQLStorage) GetPnLByTimeRange(account string, startTime, endTime time.Time) ([]*PnLBySymbol, error) {
	// 限制最大返回數量，防止記憶體占用過大（分组后的結果通常不會太多，但还是要限制）
	maxLimit := 1000 // 最多返回1000個币种對
	query := fmt.Sprintf(`
		SELECT
			exchange,
			COALESCE(NULLIF(market_type, ''), 'unknown') AS market_type,
			symbol,
			COUNT(*) as total_trades,
			COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) as total_pnl,
			COALESCE(SUM(exchange_pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) as exchange_pnl,
			SUM(quantity) as total_volume,
			CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*) as win_rate,
			CAST(SUM(CASE WHEN exchange_pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*) as exchange_win_rate
		FROM %s
		WHERE created_at >= ? AND created_at <= ?
		`, s.tradesTbl())
	args := []interface{}{startTime, endTime}
	if account != "" {
		query += " AND account = ?"
		args = append(args, account)
	}
	query += " GROUP BY exchange, COALESCE(NULLIF(market_type, ''), 'unknown'), symbol ORDER BY total_pnl DESC LIMIT ?"
	args = append(args, maxLimit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢盈亏數據失败: %w", err)
	}
	defer rows.Close()

	var results []*PnLBySymbol
	for rows.Next() {
		r := &PnLBySymbol{}
		var totalTrades sql.NullInt64
		var totalPnL sql.NullFloat64
		var exchangePnL sql.NullFloat64
		var totalVolume sql.NullFloat64
		var winRate sql.NullFloat64
		var exchangeWinRate sql.NullFloat64

		err := rows.Scan(&r.Exchange, &r.MarketType, &r.Symbol, &totalTrades, &totalPnL, &exchangePnL, &totalVolume, &winRate, &exchangeWinRate)
		if err != nil {
			// 不能跳過：靜默丟行會讓盈亏報表少算，調用方還以為數據是完整的
			return nil, fmt.Errorf("解析盈亏數據失败: %w", err)
		}

		if totalTrades.Valid {
			r.TotalTrades = int(totalTrades.Int64)
		}
		if totalPnL.Valid {
			r.TotalPnL = totalPnL.Float64
		}
		if exchangePnL.Valid {
			r.ExchangePnL = exchangePnL.Float64
		}
		if totalVolume.Valid {
			r.TotalVolume = totalVolume.Float64
		}
		if winRate.Valid {
			r.WinRate = winRate.Float64
		}
		if exchangeWinRate.Valid {
			r.ExchangeWinRate = exchangeWinRate.Float64
		}

		results = append(results, r)
	}

	// 迭代中途的錯誤（連接中斷、超時）只會在這裡暴露，漏檢就等於返回殘缺數據
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍歷盈亏數據失败: %w", err)
	}

	return results, nil
}

// GetPnLByAccountScopeAndAsset returns exact-scope PnL grouped by symbol,
// market and denomination. Known non-requested assets are excluded, while
// missing ownership or fee-denomination evidence makes the result incomplete.
func (s *SQLStorage) GetPnLByAccountScopeAndAsset(exchange, accountScope, asset string, startTime, endTime time.Time) ([]*PnLBySymbol, error) {
	exchange = strings.ToLower(strings.TrimSpace(exchange))
	accountScope = strings.TrimSpace(accountScope)
	asset = strings.ToUpper(strings.TrimSpace(asset))
	if exchange == "" || accountScope == "" || asset == "" || startTime.IsZero() || endTime.IsZero() || endTime.Before(startTime) {
		return nil, fmt.Errorf("exchange, account_scope, asset and a valid time range are required")
	}
	var unscoped int64
	if err := s.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE (LOWER(TRIM(exchange)) = ? AND (account_scope IS NULL OR TRIM(account_scope) = '')) OR TRIM(COALESCE(exchange, '')) = ''`, s.tradesTbl()), exchange).Scan(&unscoped); err != nil {
		return nil, fmt.Errorf("check unscoped PnL rows: %w", err)
	}
	if unscoped > 0 {
		return nil, fmt.Errorf("PnL report is incomplete: exchange %s has %d trades without account scope", exchange, unscoped)
	}
	var unclassified int64
	if err := s.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE LOWER(TRIM(exchange)) = ? AND account_scope = ? AND (TRIM(COALESCE(pnl_asset, '')) = '' OR (COALESCE(fee, 0) <> 0 AND UPPER(TRIM(COALESCE(fee_asset, ''))) <> UPPER(TRIM(COALESCE(pnl_asset, '')))))`, s.tradesTbl()), exchange, accountScope).Scan(&unclassified); err != nil {
		return nil, fmt.Errorf("validate PnL/fee asset evidence: %w", err)
	}
	if unclassified > 0 {
		return nil, fmt.Errorf("PnL report is incomplete: scope %s has %d trades with unknown or mismatched PnL/fee assets", accountScope, unclassified)
	}
	query := fmt.Sprintf(`SELECT LOWER(TRIM(exchange)), COALESCE(NULLIF(LOWER(TRIM(market_type)), ''), 'unknown'), symbol, UPPER(TRIM(pnl_asset)), COUNT(*),
		COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0),
		COALESCE(SUM(exchange_pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0), COALESCE(SUM(quantity), 0),
		CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*),
		CAST(SUM(CASE WHEN exchange_pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*),
		COALESCE(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN pnl < 0 THEN 1 ELSE 0 END), 0)
		FROM %s WHERE LOWER(TRIM(exchange)) = ? AND account_scope = ? AND UPPER(TRIM(pnl_asset)) = ? AND created_at >= ? AND created_at <= ?
		GROUP BY LOWER(TRIM(exchange)), COALESCE(NULLIF(LOWER(TRIM(market_type)), ''), 'unknown'), symbol, UPPER(TRIM(pnl_asset)) ORDER BY symbol, market_type LIMIT 1000`, s.tradesTbl())
	rows, err := s.db.Query(query, exchange, accountScope, asset, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("query scoped PnL by asset: %w", err)
	}
	defer rows.Close()
	var results []*PnLBySymbol
	for rows.Next() {
		item := &PnLBySymbol{}
		if err := rows.Scan(&item.Exchange, &item.MarketType, &item.Symbol, &item.PnLAsset, &item.TotalTrades, &item.TotalPnL, &item.ExchangePnL, &item.TotalVolume, &item.WinRate, &item.ExchangeWinRate, &item.WinningTrades, &item.LosingTrades); err != nil {
			return nil, fmt.Errorf("scan scoped PnL by asset: %w", err)
		}
		if math.IsNaN(item.TotalPnL) || math.IsInf(item.TotalPnL, 0) || math.IsNaN(item.ExchangePnL) || math.IsInf(item.ExchangePnL, 0) || math.IsNaN(item.TotalVolume) || math.IsInf(item.TotalVolume, 0) {
			return nil, fmt.Errorf("scoped PnL contains non-finite values for %s %s %s", item.Exchange, item.MarketType, item.Symbol)
		}
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scoped PnL by asset: %w", err)
	}
	return results, nil
}

// GetRealizedPnLForWithdrawal returns only exactly attributable USDT-margined
// futures profit for one account and exchange. Legacy rows without an account
// or market identity are intentionally excluded from money-transfer decisions.
func (s *SQLStorage) GetRealizedPnLForWithdrawal(exchange, symbol, accountScope string, startTime, endTime time.Time) (float64, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(accountScope) == "" {
		return 0, fmt.Errorf("withdrawal PnL requires exact exchange and account scope")
	}
	if strings.TrimSpace(symbol) == "" || startTime.IsZero() || endTime.IsZero() || !startTime.Before(endTime) {
		return 0, fmt.Errorf("withdrawal PnL requires exact symbol and a non-empty time interval")
	}
	pendingCorrections, err := s.CountPendingTradeFeeCorrectionsForAccount(exchange, "futures", symbol, accountScope)
	if err != nil {
		return 0, fmt.Errorf("verify unresolved execution fee corrections: %w", err)
	}
	if pendingCorrections > 0 {
		return 0, fmt.Errorf("withdrawal interval has %d unresolved execution fee corrections; refusing transfer", pendingCorrections)
	}
	coverage, err := s.HasFundingIncomeCoverage(exchange, symbol, "futures", accountScope, startTime, endTime)
	if err != nil {
		return 0, fmt.Errorf("verify funding income coverage: %w", err)
	}
	if !coverage {
		return 0, fmt.Errorf("funding income history does not fully cover withdrawal interval exchange=%s symbol=%s", exchange, symbol)
	}
	fillCoverage, err := s.GetOrderFillCoverage(exchange, "futures", symbol, accountScope)
	if err != nil {
		return 0, fmt.Errorf("verify execution history coverage: %w", err)
	}
	if fillCoverage == nil || fillCoverage.CoveredFrom.After(startTime.UTC()) || fillCoverage.CoveredThrough.Before(endTime.UTC()) {
		return 0, fmt.Errorf("execution history does not fully cover withdrawal interval exchange=%s symbol=%s", exchange, symbol)
	}
	var total float64
	var unknownPnL, unvaluedPnLAsset, unvaluedFees int
	if err := s.db.QueryRow(`
		SELECT COALESCE(SUM(realized_pnl), 0) - COALESCE(SUM(commission), 0),
		       COALESCE(SUM(CASE WHEN realized_pnl IS NULL THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN realized_pnl IS NOT NULL AND UPPER(TRIM(COALESCE(realized_pnl_asset, ''))) <> 'USDT' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN COALESCE(commission, 0) <> 0 AND UPPER(COALESCE(commission_asset, '')) <> 'USDT' THEN 1 ELSE 0 END), 0)
		FROM order_fills
		WHERE exchange = ? AND account_scope = ? AND market_type = 'futures' AND symbol = ?
		  AND trade_time > ? AND trade_time <= ?`, exchange, accountScope, symbol, startTime.UTC(), endTime.UTC()).Scan(&total, &unknownPnL, &unvaluedPnLAsset, &unvaluedFees); err != nil {
		return 0, fmt.Errorf("query exchange execution PnL exchange=%s account_scope=%s symbol=%s: %w", exchange, accountScope, symbol, err)
	}
	if unknownPnL > 0 {
		return 0, fmt.Errorf("withdrawal interval includes %d executions without authoritative realized PnL; refusing transfer", unknownPnL)
	}
	if unvaluedPnLAsset > 0 {
		return 0, fmt.Errorf("withdrawal interval includes %d executions without verified USDT realized PnL denomination; refusing transfer", unvaluedPnLAsset)
	}
	if unvaluedFees > 0 {
		return 0, fmt.Errorf("withdrawal interval includes %d non-USDT or unclassified execution fees; refusing transfer", unvaluedFees)
	}
	fundingRows, err := s.db.Query(`
		SELECT UPPER(COALESCE(asset, '')), COALESCE(SUM(income), 0)
		FROM funding_payments
		WHERE exchange = ? AND account_scope = ? AND market_type = 'futures' AND symbol = ?
		  AND UPPER(income_type) = 'FUNDING_FEE' AND identity_key IS NOT NULL AND trade_time > ? AND trade_time <= ?
		GROUP BY UPPER(COALESCE(asset, ''))
	`, exchange, accountScope, symbol, startTime.UTC(), endTime.UTC())
	if err != nil {
		return 0, fmt.Errorf("query scoped withdrawal funding income exchange=%s symbol=%s: %w", exchange, symbol, err)
	}
	defer fundingRows.Close()
	for fundingRows.Next() {
		var asset string
		var amount float64
		if err := fundingRows.Scan(&asset, &amount); err != nil {
			return 0, fmt.Errorf("scan scoped withdrawal funding income: %w", err)
		}
		if asset != "USDT" {
			return 0, fmt.Errorf("withdrawal funding income denomination %q is not valued in USDT", asset)
		}
		total += amount
	}
	if err := fundingRows.Err(); err != nil {
		return 0, fmt.Errorf("iterate scoped withdrawal funding income: %w", err)
	}
	if math.IsNaN(total) || math.IsInf(total, 0) {
		return 0, fmt.Errorf("withdrawal PnL is non-finite exchange=%s account_scope=%s symbol=%s", exchange, accountScope, symbol)
	}
	return total, nil
}

// GetActualProfitBySymbol 计算指定币种在指定時间之前的累计實際盈利（淨利潤，已扣手續費）
// botID 非空時僅統計該 Bot 的 trades（單 Bot 對賬）；空則該 symbol 下全部（兼容舊行為）。
func (s *SQLStorage) GetActualProfitBySymbol(symbol, account string, beforeTime time.Time, botID string) (float64, error) {
	query := fmt.Sprintf(`
		SELECT COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) as total_pnl
		FROM %s
		WHERE symbol = ? AND created_at <= ?
		`, s.tradesTbl())
	args := []interface{}{symbol, beforeTime}
	if account != "" {
		query += " AND account = ?"
		args = append(args, account)
	}
	if bid := strings.TrimSpace(botID); bid != "" {
		query += " AND bot_id = ?"
		args = append(args, bid)
	}

	row := s.db.QueryRow(query, args...)

	var totalPnL sql.NullFloat64
	err := row.Scan(&totalPnL)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("查詢實際盈利失败: %w", err)
	}

	if totalPnL.Valid {
		return totalPnL.Float64, nil
	}

	return 0, nil
}

// GetActualProfitBySymbolMarketScope keeps reconciliation PnL isolated by exchange and market.
func (s *SQLStorage) GetActualProfitBySymbolMarketScope(exchange, marketType, symbol, account, accountScope string, beforeTime time.Time, botID string) (float64, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(marketType) == "" || strings.TrimSpace(symbol) == "" || strings.TrimSpace(accountScope) == "" {
		return 0, fmt.Errorf("exchange, market type, symbol, and account scope are required for scoped reconciliation PnL")
	}
	query := fmt.Sprintf(`SELECT COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) FROM %s WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND created_at <= ?`, s.tradesTbl())
	args := []interface{}{exchange, marketType, symbol, accountScope, beforeTime}
	if botID = strings.TrimSpace(botID); botID != "" {
		query += " AND bot_id = ?"
		args = append(args, botID)
	}
	var total sql.NullFloat64
	if err := s.db.QueryRow(query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("query market-scoped reconciliation PnL: %w", err)
	}
	if !total.Valid || math.IsNaN(total.Float64) || math.IsInf(total.Float64, 0) {
		return 0, fmt.Errorf("market-scoped reconciliation PnL is invalid")
	}
	return total.Float64, nil
}

// GetTotalBuySellQtyByMarketScope only reports totals from the exact account market.
func (s *SQLStorage) GetTotalBuySellQtyByMarketScope(exchange, marketType, symbol, account, accountScope, botID string) (float64, float64, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(marketType) == "" || strings.TrimSpace(symbol) == "" || strings.TrimSpace(accountScope) == "" {
		return 0, 0, fmt.Errorf("exchange, market type, symbol, and account scope are required for scoped reconciliation quantities")
	}
	query := fmt.Sprintf(`SELECT COALESCE(SUM(quantity), 0) FROM %s WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ?`, s.tradesTbl())
	args := []interface{}{exchange, marketType, symbol, accountScope}
	if botID = strings.TrimSpace(botID); botID != "" {
		query += " AND bot_id = ?"
		args = append(args, botID)
	}
	var total sql.NullFloat64
	if err := s.db.QueryRow(query, args...).Scan(&total); err != nil {
		return 0, 0, fmt.Errorf("query market-scoped reconciliation quantities: %w", err)
	}
	if !total.Valid || math.IsNaN(total.Float64) || math.IsInf(total.Float64, 0) || total.Float64 < 0 {
		return 0, 0, fmt.Errorf("market-scoped reconciliation quantity is invalid")
	}
	return total.Float64, total.Float64, nil
}

// HasUnclassifiedMarketTrades prevents a scoped display from implying completeness
// while legacy rows for this owner still lack market attribution.
func (s *SQLStorage) HasUnclassifiedMarketTrades(exchange, symbol, account, accountScope, botID string) (bool, error) {
	query := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE exchange = ? AND symbol = ? AND (market_type IS NULL OR TRIM(market_type) = '' OR account_scope IS NULL OR TRIM(account_scope) = '' OR bot_id IS NULL OR TRIM(bot_id) = '')`, s.tradesTbl())
	args := []interface{}{exchange, symbol}
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
	if accountScope != "" {
		query += " AND (account_scope = ? OR account_scope IS NULL OR account_scope = '')"
		args = append(args, accountScope)
	}
	if botID = strings.TrimSpace(botID); botID != "" {
		query += " AND (bot_id = ? OR bot_id IS NULL OR TRIM(bot_id) = '')"
		args = append(args, botID)
	}
	var count int64
	if err := s.db.QueryRow(query, args...).Scan(&count); err != nil {
		return false, fmt.Errorf("check legacy market attribution: %w", err)
	}
	return count > 0, nil
}

// GetTotalBuySellQty 獲取累计買入和累计賣出數量（從trades表计算）
// botID 非空時僅統計該 Bot 的配對成交；空則該 symbol 下全部（兼容舊行為）。
func (s *SQLStorage) GetTotalBuySellQty(symbol, account, botID string) (totalBuyQty, totalSellQty float64, err error) {
	query := fmt.Sprintf(`
		SELECT
			COALESCE(SUM(quantity), 0) as total_qty
		FROM %s
		WHERE symbol = ?
	`, s.tradesTbl())
	args := []interface{}{symbol}
	if account != "" {
		query += " AND account = ?"
		args = append(args, account)
	}
	if bid := strings.TrimSpace(botID); bid != "" {
		query += " AND bot_id = ?"
		args = append(args, bid)
	}

	var totalQty sql.NullFloat64
	err = s.db.QueryRow(query, args...).Scan(&totalQty)
	if err != nil {
		if err == sql.ErrNoRows {
			// 如果没有匹配的記錄，返回0而不是錯误
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("查詢累计買賣數量失败: %w", err)
	}

	if totalQty.Valid {
		// trades表中的quantity是配對交易的quantity，每笔交易都有買入和賣出
		// 所以累计買入 = 累计賣出 = SUM(quantity)
		return totalQty.Float64, totalQty.Float64, nil
	}

	return 0, 0, nil
}
