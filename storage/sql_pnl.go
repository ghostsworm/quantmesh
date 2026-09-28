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

func (s *SQLStorage) listPnLMarketScopes(symbol, account string, startTime, endTime time.Time) ([]PnLMarketScope, error) {
	query := fmt.Sprintf(`SELECT DISTINCT COALESCE(NULLIF(LOWER(TRIM(exchange)), ''), 'unknown'), COALESCE(NULLIF(LOWER(TRIM(market_type)), ''), 'unknown') FROM %s WHERE symbol = ? AND created_at >= ? AND created_at <= ?`, s.tradesTbl())
	args := []interface{}{symbol, startTime, endTime}
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
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
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		query += " AND (account = ? OR account IS NULL OR account = '')"
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
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		query += " AND (account = ? OR account IS NULL OR account = '')"
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

// GetRealizedPnLForWithdrawal returns only exactly attributable USDT-margined
// futures profit for one account and exchange. Legacy rows without an account
// or market identity are intentionally excluded from money-transfer decisions.
func (s *SQLStorage) GetRealizedPnLForWithdrawal(exchange, symbol, accountScope string, startTime, endTime time.Time) (float64, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(accountScope) == "" {
		return 0, fmt.Errorf("withdrawal PnL requires exact exchange and account scope")
	}
	query := fmt.Sprintf(`
		SELECT COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0)
		FROM %s
		WHERE exchange = ? AND account_scope = ? AND market_type = 'futures'
		  AND created_at > ? AND created_at <= ?`, s.tradesTbl())
	args := []interface{}{exchange, accountScope, startTime, endTime}
	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}
	var total float64
	if err := s.db.QueryRow(query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("query attributable withdrawal PnL exchange=%s account_scope=%s symbol=%s: %w", exchange, accountScope, symbol, err)
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
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		query += " AND (account = ? OR account IS NULL OR account = '')"
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
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
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
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
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
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		// 这样可以确保即使舊數據的account字段為空，也能查詢到累计買賣數量
		query += " AND (account = ? OR account IS NULL OR account = '')"
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
