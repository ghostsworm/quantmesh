package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"quantmesh/utils"
)

// SaveStatistics 保存统计
func (s *SQLStorage) SaveStatistics(stats *Statistics) error {
	// 轉换為UTC時间存儲
	date := utils.ToUTC(stats.Date)
	createdAt := utils.ToUTC(stats.CreatedAt)
	if s.dbType == "mysql" {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM statistics WHERE date = ?`, date); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO statistics
			(date, total_trades, total_volume, total_pnl, win_rate, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
		`, date, stats.TotalTrades, stats.TotalVolume, stats.TotalPnL, stats.WinRate, createdAt); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO statistics
		(date, total_trades, total_volume, total_pnl, win_rate, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, date, stats.TotalTrades, stats.TotalVolume,
		stats.TotalPnL, stats.WinRate, createdAt)
	return err
}

// QueryStatistics 查詢统计數據
func (s *SQLStorage) QueryStatistics(startDate, endDate time.Time) ([]*Statistics, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxStats := 10000 // 最多返回1万条统计數據

	rows, err := s.db.Query(`
		SELECT date, total_trades, total_volume, total_pnl, win_rate, created_at
		FROM statistics
		WHERE date >= ? AND date <= ?
		ORDER BY date DESC
		LIMIT ?
	`, startDate, endDate, maxStats)
	if err != nil {
		return nil, fmt.Errorf("查詢统计數據失败: %w", err)
	}
	defer rows.Close()

	var stats []*Statistics
	for rows.Next() {
		stat := &Statistics{}
		err := rows.Scan(
			&stat.Date,
			&stat.TotalTrades,
			&stat.TotalVolume,
			&stat.TotalPnL,
			&stat.WinRate,
			&stat.CreatedAt,
		)
		if err != nil {
			continue
		}
		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍歷统计數據失败: %w", err)
	}

	return stats, nil
}

// GetStatisticsSummary 獲取统计彙總（從 trades 表實時计算）
func (s *SQLStorage) GetStatisticsSummary(account string) (*Statistics, error) {
	return s.GetStatisticsSummaryByExchange("", account)
}

// GetStatisticsSummaryByExchange 獲取指定交易所的统计彙總
func (s *SQLStorage) GetStatisticsSummaryByExchange(exchange, account string) (*Statistics, error) {
	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_trades,
			COALESCE(SUM(quantity), 0) as total_volume,
			COALESCE(SUM(pnl), 0) as gross_pnl,
			COALESCE(SUM(COALESCE(fee, 0)), 0) as total_fee,
			COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) as net_pnl,
			CASE
				WHEN COUNT(*) > 0 THEN
					CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*)
				ELSE 0
			END as win_rate,
			COALESCE(SUM(COALESCE(buy_price_deviation, 0)), 0) as total_buy_deviation,
			COALESCE(SUM(COALESCE(sell_price_deviation, 0)), 0) as total_sell_deviation
		FROM %s
		WHERE 1=1
	`, s.tradesTbl())
	args := []interface{}{}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if account != "" {
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		// 这样可以确保即使舊數據的account字段為空，也能查詢到统计信息
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}

	row := s.db.QueryRow(query, args...)

	stat := &Statistics{}
	var totalTrades sql.NullInt64
	var totalVolume sql.NullFloat64
	var grossPnL sql.NullFloat64
	var totalFee sql.NullFloat64
	var netPnL sql.NullFloat64
	var winRate sql.NullFloat64
	var totalBuyDeviation sql.NullFloat64
	var totalSellDeviation sql.NullFloat64

	err := row.Scan(&totalTrades, &totalVolume, &grossPnL, &totalFee, &netPnL, &winRate, &totalBuyDeviation, &totalSellDeviation)
	if err != nil {
		if err == sql.ErrNoRows {
			return &Statistics{}, nil
		}
		return nil, fmt.Errorf("查詢统计彙總失败: %w", err)
	}

	if totalTrades.Valid {
		stat.TotalTrades = int(totalTrades.Int64)
	}
	if totalVolume.Valid {
		stat.TotalVolume = totalVolume.Float64
	}
	if grossPnL.Valid {
		stat.GrossPnL = grossPnL.Float64
	}
	if totalFee.Valid {
		stat.TotalFee = totalFee.Float64
	}
	if netPnL.Valid {
		stat.TotalPnL = netPnL.Float64
	}
	if winRate.Valid {
		stat.WinRate = winRate.Float64
	}
	if totalBuyDeviation.Valid {
		stat.TotalBuyDeviation = totalBuyDeviation.Float64
	}
	if totalSellDeviation.Valid {
		stat.TotalSellDeviation = totalSellDeviation.Float64
	}

	return stat, nil
}

// GetStatisticsSummaryByExchangeAndSymbol 獲取指定交易所、指定交易對的统计彙總
func (s *SQLStorage) GetStatisticsSummaryByExchangeAndSymbol(exchange, symbol, account, botID string) (*Statistics, error) {
	query := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_trades,
			COALESCE(SUM(quantity), 0) as total_volume,
			COALESCE(SUM(pnl), 0) as gross_pnl,
			COALESCE(SUM(COALESCE(fee, 0)), 0) as total_fee,
			COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) as net_pnl,
			CASE
				WHEN COUNT(*) > 0 THEN
					CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*)
				ELSE 0
			END as win_rate,
			COALESCE(SUM(COALESCE(buy_price_deviation, 0)), 0) as total_buy_deviation,
			COALESCE(SUM(COALESCE(sell_price_deviation, 0)), 0) as total_sell_deviation
		FROM %s
		WHERE 1=1
	`, s.tradesTbl())
	args := []interface{}{}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
	if bid := strings.TrimSpace(botID); bid != "" {
		query += " AND COALESCE(bot_id, '') = ?"
		args = append(args, bid)
	}

	row := s.db.QueryRow(query, args...)

	stat := &Statistics{}
	var totalTrades sql.NullInt64
	var totalVolume sql.NullFloat64
	var grossPnL sql.NullFloat64
	var totalFee sql.NullFloat64
	var netPnL sql.NullFloat64
	var winRate sql.NullFloat64
	var totalBuyDeviation sql.NullFloat64
	var totalSellDeviation sql.NullFloat64

	err := row.Scan(&totalTrades, &totalVolume, &grossPnL, &totalFee, &netPnL, &winRate, &totalBuyDeviation, &totalSellDeviation)
	if err != nil {
		if err == sql.ErrNoRows {
			return &Statistics{}, nil
		}
		return nil, fmt.Errorf("查詢统计彙總失败: %w", err)
	}

	if totalTrades.Valid {
		stat.TotalTrades = int(totalTrades.Int64)
	}
	if totalVolume.Valid {
		stat.TotalVolume = totalVolume.Float64
	}
	if grossPnL.Valid {
		stat.GrossPnL = grossPnL.Float64
	}
	if totalFee.Valid {
		stat.TotalFee = totalFee.Float64
	}
	if netPnL.Valid {
		stat.TotalPnL = netPnL.Float64
	}
	if winRate.Valid {
		stat.WinRate = winRate.Float64
	}
	if totalBuyDeviation.Valid {
		stat.TotalBuyDeviation = totalBuyDeviation.Float64
	}
	if totalSellDeviation.Valid {
		stat.TotalSellDeviation = totalSellDeviation.Float64
	}

	return stat, nil
}

// GetExchangePnLTotal 獲取交易所已實現盈虧的總計（從 orders 表的 realized_pnl 聚合）
func (s *SQLStorage) GetExchangePnLTotal(exchange, symbol, botID string) (float64, error) {
	query := `SELECT COALESCE(SUM(realized_pnl), 0) FROM orders WHERE realized_pnl IS NOT NULL AND status = 'FILLED'`
	args := []interface{}{}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}
	if bid := strings.TrimSpace(botID); bid != "" {
		query += " AND COALESCE(bot_id, '') = ?"
		args = append(args, bid)
	}
	var total float64
	err := s.db.QueryRow(query, args...).Scan(&total)
	return total, err
}

// GetTodayStatisticsByExchangeAndSymbol 獲取指定交易所、交易對的當日統計
func (s *SQLStorage) GetTodayStatisticsByExchangeAndSymbol(exchange, symbol, account, botID string) (*TodayStatistics, error) {
	// 獲取當日日期（UTC）
	now := time.Now().UTC()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	todayEnd := todayStart.Add(24 * time.Hour)

	// 查詢當日網格盈虧（網格配對成交表）
	gridQuery := fmt.Sprintf(`
		SELECT
			COUNT(*) as total_trades,
			COALESCE(SUM(pnl), 0) as grid_pnl
		FROM %s
		WHERE created_at >= ? AND created_at < ?
	`, s.tradesTbl())
	gridArgs := []interface{}{todayStart, todayEnd}
	if exchange != "" {
		gridQuery += " AND exchange = ?"
		gridArgs = append(gridArgs, exchange)
	}
	if symbol != "" {
		gridQuery += " AND symbol = ?"
		gridArgs = append(gridArgs, symbol)
	}
	if account != "" {
		gridQuery += " AND (account = ? OR account IS NULL OR account = '')"
		gridArgs = append(gridArgs, account)
	}
	if bid := strings.TrimSpace(botID); bid != "" {
		gridQuery += " AND COALESCE(bot_id, '') = ?"
		gridArgs = append(gridArgs, bid)
	}

	var gridTrades int
	var gridPnL float64
	err := s.db.QueryRow(gridQuery, gridArgs...).Scan(&gridTrades, &gridPnL)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("查詢當日網格統計失敗: %w", err)
	}

	// 查詢當日交易所盈虧（orders 表的 realized_pnl）
	exchangeQuery := `
		SELECT COALESCE(SUM(realized_pnl), 0)
		FROM orders
		WHERE realized_pnl IS NOT NULL
			AND status = 'FILLED'
			AND created_at >= ? AND created_at < ?
	`
	exchangeArgs := []interface{}{todayStart, todayEnd}
	if exchange != "" {
		exchangeQuery += " AND exchange = ?"
		exchangeArgs = append(exchangeArgs, exchange)
	}
	if symbol != "" {
		exchangeQuery += " AND symbol = ?"
		exchangeArgs = append(exchangeArgs, symbol)
	}
	if bid := strings.TrimSpace(botID); bid != "" {
		exchangeQuery += " AND COALESCE(bot_id, '') = ?"
		exchangeArgs = append(exchangeArgs, bid)
	}

	var exchangePnL float64
	err = s.db.QueryRow(exchangeQuery, exchangeArgs...).Scan(&exchangePnL)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("查詢當日交易所盈虧失敗: %w", err)
	}

	return &TodayStatistics{
		TotalTrades: gridTrades,
		GridPnL:     gridPnL,
		ExchangePnL: exchangePnL,
	}, nil
}

// GetExchangePnLOrderStats 獲取交易所盈虧相關的訂單統計（用於診斷差異）
// 返回：有 realized_pnl 的訂單數、無 realized_pnl 的 FILLED SELL 訂單數、有 realized_pnl 的訂單總和
func (s *SQLStorage) GetExchangePnLOrderStats(exchange, symbol string) (withPnLCount, missingPnLCount int, totalPnL float64, err error) {
	baseWhere := "status = 'FILLED'"
	args := []interface{}{}
	if exchange != "" {
		baseWhere += " AND exchange = ?"
		args = append(args, exchange)
	}
	if symbol != "" {
		baseWhere += " AND symbol = ?"
		args = append(args, symbol)
	}
	// 有 realized_pnl 的訂單數及總和
	var cnt int
	if err := s.db.QueryRow("SELECT COUNT(*), COALESCE(SUM(realized_pnl), 0) FROM orders WHERE realized_pnl IS NOT NULL AND "+baseWhere, args...).Scan(&cnt, &totalPnL); err != nil {
		return 0, 0, 0, err
	}
	withPnLCount = cnt
	// 無 realized_pnl 的 FILLED SELL 訂單數（可能漏記）
	var missing int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM orders WHERE "+baseWhere+" AND side = 'SELL' AND realized_pnl IS NULL", args...).Scan(&missing); err != nil {
		return withPnLCount, 0, totalPnL, err
	}
	missingPnLCount = missing
	return withPnLCount, missingPnLCount, totalPnL, nil
}

// GetDailyExchangePnL 獲取每日交易所已實現盈虧（從 orders 表按日期聚合 realized_pnl）
func (s *SQLStorage) GetDailyExchangePnL(exchange, symbol string, startDate, endDate time.Time, botID string) (map[string]float64, error) {
	dateExpr := s.dateExprInConfiguredTimezone("created_at")
	query := fmt.Sprintf(`
		SELECT %s as dt, COALESCE(SUM(realized_pnl), 0) as total
		FROM orders
		WHERE realized_pnl IS NOT NULL AND status = 'FILLED'
			AND %s >= ? AND %s <= ?
	`, dateExpr, dateExpr, dateExpr)
	args := []interface{}{startDate.Format("2006-01-02"), endDate.Format("2006-01-02")}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}
	if bid := strings.TrimSpace(botID); bid != "" {
		query += " AND COALESCE(bot_id, '') = ?"
		args = append(args, bid)
	}
	query += " GROUP BY " + dateExpr

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢每日交易所盈虧失败: %w", err)
	}
	defer rows.Close()

	result := make(map[string]float64)
	for rows.Next() {
		var dt string
		var total float64
		if err := rows.Scan(&dt, &total); err != nil {
			// 金額聚合不能跳行：少一天就是日盈亏曲线出現缺口
			return nil, fmt.Errorf("解析每日盈亏失败: %w", err)
		}
		result[dt] = total
	}
	return result, rows.Err()
}

// GetDailyTradesSummary 獲取指定日（配置時區）的成交筆數、毛利、手續費
func (s *SQLStorage) GetDailyTradesSummary(exchange, account, dateStr, botID string) (count int, grossPnl, totalFee float64, err error) {
	dateExpr := s.dateExprInConfiguredTimezone("created_at")
	query := fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(SUM(pnl), 0), COALESCE(SUM(COALESCE(fee, 0)), 0)
		FROM %s
		WHERE %s = ?
	`, s.tradesTbl(), dateExpr)
	args := []interface{}{dateStr}
	if exchange != "" {
		query += " AND (exchange = ? OR exchange = '')"
		args = append(args, exchange)
	}
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
	if bid := strings.TrimSpace(botID); bid != "" {
		query += " AND COALESCE(bot_id, '') = ?"
		args = append(args, bid)
	}
	var cnt sql.NullInt64
	var pnl, fee sql.NullFloat64
	err = s.db.QueryRow(query, args...).Scan(&cnt, &pnl, &fee)
	if err != nil {
		return 0, 0, 0, err
	}
	if cnt.Valid {
		count = int(cnt.Int64)
	}
	if pnl.Valid {
		grossPnl = pnl.Float64
	}
	if fee.Valid {
		totalFee = fee.Float64
	}
	return count, grossPnl, totalFee, nil
}

// QueryDailyPnLTrades returns a dimension-filtered summary and the exact top
// winning/losing paired trades. Unlike the legacy date-only summary, every
// supplied owner and market dimension is an exact predicate; unclassified
// legacy rows are never attributed to a classified report.
func (s *SQLStorage) QueryDailyPnLTrades(exchange, marketType, symbol, account, accountScope, botID string, start, end time.Time) (count int, grossPnL, totalFee float64, winners, losers []*Trade, err error) {
	if !start.Before(end) {
		return 0, 0, 0, nil, nil, fmt.Errorf("invalid daily PnL interval")
	}
	where, args := dailyPnLTradeFilter(exchange, marketType, symbol, account, accountScope, botID, start, end)
	query := fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(pnl), 0), COALESCE(SUM(COALESCE(fee, 0)), 0) FROM %s WHERE %s`, s.tradesTbl(), where)
	if err = s.db.QueryRow(query, args...).Scan(&count, &grossPnL, &totalFee); err != nil {
		return 0, 0, 0, nil, nil, fmt.Errorf("query filtered daily trade summary: %w", err)
	}
	winners, err = s.queryDailyPnLTradeExtremes(where, args, true)
	if err != nil {
		return 0, 0, 0, nil, nil, err
	}
	losers, err = s.queryDailyPnLTradeExtremes(where, args, false)
	if err != nil {
		return 0, 0, 0, nil, nil, err
	}
	return count, grossPnL, totalFee, winners, losers, nil
}

func dailyPnLTradeFilter(exchange, marketType, symbol, account, accountScope, botID string, start, end time.Time) (string, []interface{}) {
	conditions := []string{"created_at >= ?", "created_at < ?"}
	args := []interface{}{start, end}
	for _, filter := range []struct{ column, value string }{
		{"exchange", strings.TrimSpace(exchange)},
		{"market_type", strings.ToLower(strings.TrimSpace(marketType))},
		{"symbol", strings.TrimSpace(symbol)},
		{"account", strings.TrimSpace(account)},
		{"account_scope", strings.TrimSpace(accountScope)},
		{"bot_id", strings.TrimSpace(botID)},
	} {
		if filter.value == "" {
			continue
		}
		conditions = append(conditions, "COALESCE("+filter.column+", '') = ?")
		args = append(args, filter.value)
	}
	return strings.Join(conditions, " AND "), args
}

func (s *SQLStorage) queryDailyPnLTradeExtremes(where string, args []interface{}, winners bool) ([]*Trade, error) {
	comparison, direction := "<", "ASC"
	if winners {
		comparison, direction = ">", "DESC"
	}
	query := fmt.Sprintf(`SELECT id, buy_order_id, sell_order_id, COALESCE(bot_id, ''), COALESCE(exchange, ''), COALESCE(market_type, ''), COALESCE(account, ''), COALESCE(symbol, ''), buy_price, sell_price, quantity, pnl, COALESCE(fee, 0), created_at FROM %s WHERE %s AND pnl %s 0 ORDER BY pnl %s, id DESC LIMIT 20`, s.tradesTbl(), where, comparison, direction)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query top daily trade PnL: %w", err)
	}
	defer rows.Close()
	var trades []*Trade
	for rows.Next() {
		trade := &Trade{}
		if err := rows.Scan(&trade.ID, &trade.BuyOrderID, &trade.SellOrderID, &trade.BotID, &trade.Exchange, &trade.MarketType, &trade.Account, &trade.Symbol,
			&trade.BuyPrice, &trade.SellPrice, &trade.Quantity, &trade.PnL, &trade.Fee, &trade.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan top daily trade PnL: %w", err)
		}
		trades = append(trades, trade)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top daily trade PnL: %w", err)
	}
	return trades, nil
}

// QueryDailyStatisticsFromTrades 從 trades 表查詢每日统计
func (s *SQLStorage) QueryDailyStatisticsFromTrades(account string, startDate, endDate time.Time, botID string) ([]*DailyStatisticsWithTradeCount, error) {
	return s.QueryDailyStatisticsByExchange("", "", account, startDate, endDate, botID)
}

// QueryDailyStatisticsByExchange 從 trades 表查詢指定交易所的每日统计
func (s *SQLStorage) QueryDailyStatisticsByExchange(exchange, symbol, account string, startDate, endDate time.Time, botID string) ([]*DailyStatisticsWithTradeCount, error) {
	// 限制最大返回數量，防止記憶體占用過大（分组后的結果通常不會太多，但还是要限制）
	maxLimit := 3650 // 最多返回10年的每日统计（3650天）

	// 轉换為日期字符串（YYYY-MM-DD格式）
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")

	// 獲取配置時区的日期表達式（SQLite / MySQL 語法不同）
	dateExpr := s.dateExprInConfiguredTimezone("created_at")

	query := fmt.Sprintf(`
		SELECT
			%s as date,
			COUNT(*) as total_trades,
			COALESCE(SUM(quantity), 0) as total_volume,
			COALESCE(SUM(pnl), 0) as gross_pnl,
			COALESCE(SUM(COALESCE(fee, 0)), 0) as total_fee,
			COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) as net_pnl,
			CASE
				WHEN COUNT(*) > 0 THEN
					CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*)
				ELSE 0
			END as win_rate,
			SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) as winning_trades,
			SUM(CASE WHEN pnl < 0 THEN 1 ELSE 0 END) as losing_trades,
			COALESCE(SUM(CASE WHEN pnl > 0 THEN quantity ELSE 0 END), 0) as volume_profit,
			COALESCE(SUM(CASE WHEN pnl <= 0 THEN quantity ELSE 0 END), 0) as volume_stop_loss
		FROM %s
		WHERE %s >= ? AND %s <= ?
	`, dateExpr, s.tradesTbl(), dateExpr, dateExpr)
	args := []interface{}{startDateStr, endDateStr}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}
	if account != "" {
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		// 这样可以确保即使舊數據的account字段為空，也能查詢到统计信息
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
	if bid := strings.TrimSpace(botID); bid != "" {
		query += " AND COALESCE(bot_id, '') = ?"
		args = append(args, bid)
	}
	query += " GROUP BY " + dateExpr + " ORDER BY date DESC LIMIT ?"
	args = append(args, maxLimit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢每日统计失败: %w", err)
	}
	defer rows.Close()

	var stats []*DailyStatisticsWithTradeCount
	for rows.Next() {
		stat := &DailyStatisticsWithTradeCount{}
		var dateStr string
		var totalTrades sql.NullInt64
		var totalVolume sql.NullFloat64
		var grossPnL sql.NullFloat64
		var totalFee sql.NullFloat64
		var netPnL sql.NullFloat64
		var winRate sql.NullFloat64
		var winningTrades sql.NullInt64
		var losingTrades sql.NullInt64
		var volumeProfit sql.NullFloat64
		var volumeStopLoss sql.NullFloat64

		err := rows.Scan(&dateStr, &totalTrades, &totalVolume, &grossPnL, &totalFee, &netPnL, &winRate, &winningTrades, &losingTrades, &volumeProfit, &volumeStopLoss)
		if err != nil {
			// 統計報表不能跳行：少一天就是報表數字直接算錯
			return nil, fmt.Errorf("解析每日统计失败: %w", err)
		}

		// 解析日期
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("解析统计日期失败 (%s): %w", dateStr, err)
		}
		stat.Date = date

		if totalTrades.Valid {
			stat.TotalTrades = int(totalTrades.Int64)
		}
		if totalVolume.Valid {
			stat.TotalVolume = totalVolume.Float64
		}
		if grossPnL.Valid {
			stat.GrossPnL = grossPnL.Float64
		}
		if totalFee.Valid {
			stat.TotalFee = totalFee.Float64
		}
		if netPnL.Valid {
			stat.TotalPnL = netPnL.Float64
		}
		if winRate.Valid {
			stat.WinRate = winRate.Float64
		}
		if winningTrades.Valid {
			stat.WinningTrades = int(winningTrades.Int64)
		}
		if losingTrades.Valid {
			stat.LosingTrades = int(losingTrades.Int64)
		}
		if volumeProfit.Valid {
			stat.VolumeProfit = volumeProfit.Float64
		}
		if volumeStopLoss.Valid {
			stat.VolumeStopLoss = volumeStopLoss.Float64
		}

		stats = append(stats, stat)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍歷每日统计失败: %w", err)
	}

	return stats, nil
}
