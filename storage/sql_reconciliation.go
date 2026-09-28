package storage

import (
	"database/sql"
	"fmt"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// ========== 對账历史存儲 ==========

// SaveReconciliationHistory 保存對账历史
func (s *SQLStorage) SaveReconciliationHistory(history *ReconciliationHistory) error {
	// 轉换為UTC時间存儲
	reconcileTime := utils.ToUTC(history.ReconcileTime)
	createdAt := utils.ToUTC(history.CreatedAt)
	_, err := s.db.Exec(`
		INSERT INTO reconciliation_history
		(exchange, symbol, account, account_scope, market_type, bot_id, reconcile_time, local_position, exchange_position, position_diff,
		 active_buy_orders, active_sell_orders, pending_sell_qty,
		 total_buy_qty, total_sell_qty, estimated_profit, actual_profit, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, history.Exchange, history.Symbol, history.Account, history.AccountScope, history.MarketType, history.BotID, reconcileTime, history.LocalPosition, history.ExchangePosition,
		history.PositionDiff, history.ActiveBuyOrders, history.ActiveSellOrders,
		history.PendingSellQty, history.TotalBuyQty, history.TotalSellQty, history.EstimatedProfit, history.ActualProfit, createdAt)
	return err
}

// QueryReconciliationHistoryByScope returns only records with a fully verified runtime identity.
func (s *SQLStorage) QueryReconciliationHistoryByScope(exchange, symbol, account, marketType, accountScope, botID string, startTime, endTime time.Time, limit, offset int) ([]*ReconciliationHistory, error) {
	if exchange == "" || symbol == "" || marketType == "" || accountScope == "" || botID == "" {
		return nil, fmt.Errorf("scoped reconciliation history requires exchange, symbol, market type, account scope, and bot id")
	}
	if limit <= 0 || limit > 10000 {
		limit = 100
	}
	query := `SELECT id, exchange, symbol, account, account_scope, market_type, bot_id, reconcile_time, local_position, exchange_position, position_diff, active_buy_orders, active_sell_orders, pending_sell_qty, total_buy_qty, total_sell_qty, estimated_profit, actual_profit, created_at FROM reconciliation_history WHERE exchange = ? AND symbol = ? AND market_type = ? AND account_scope = ? AND bot_id = ? AND reconcile_time >= ? AND reconcile_time <= ?`
	args := []interface{}{exchange, symbol, marketType, accountScope, botID, startTime, endTime}
	if account != "" {
		query += ` AND account = ?`
		args = append(args, account)
	}
	query += ` ORDER BY reconcile_time DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	return s.queryReconciliationHistoryRows(query, args...)
}

func (s *SQLStorage) queryReconciliationHistoryRows(query string, args ...interface{}) ([]*ReconciliationHistory, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query reconciliation history: %w", err)
	}
	defer rows.Close()
	var histories []*ReconciliationHistory
	for rows.Next() {
		h := &ReconciliationHistory{}
		if err := rows.Scan(&h.ID, &h.Exchange, &h.Symbol, &h.Account, &h.AccountScope, &h.MarketType, &h.BotID, &h.ReconcileTime, &h.LocalPosition, &h.ExchangePosition, &h.PositionDiff, &h.ActiveBuyOrders, &h.ActiveSellOrders, &h.PendingSellQty, &h.TotalBuyQty, &h.TotalSellQty, &h.EstimatedProfit, &h.ActualProfit, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan reconciliation history: %w", err)
		}
		histories = append(histories, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reconciliation history: %w", err)
	}
	return histories, nil
}

func (s *SQLStorage) GetReconciliationCountByScope(exchange, symbol, account, marketType, accountScope, botID string) (int64, error) {
	if exchange == "" || symbol == "" || marketType == "" || accountScope == "" || botID == "" {
		return 0, fmt.Errorf("scoped reconciliation count requires exchange, symbol, market type, account scope, and bot id")
	}
	query := `SELECT COUNT(*) FROM reconciliation_history WHERE exchange = ? AND symbol = ? AND market_type = ? AND account_scope = ? AND bot_id = ?`
	args := []interface{}{exchange, symbol, marketType, accountScope, botID}
	if account != "" {
		query += ` AND account = ?`
		args = append(args, account)
	}
	var count int64
	if err := s.db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count scoped reconciliation history: %w", err)
	}
	return count, nil
}

func (s *SQLStorage) HasUnscopedReconciliationHistory(exchange, symbol, account string) (bool, error) {
	query := `SELECT COUNT(*) FROM reconciliation_history WHERE exchange = ? AND symbol = ? AND (market_type IS NULL OR market_type = '' OR account_scope IS NULL OR account_scope = '' OR bot_id IS NULL OR bot_id = '')`
	args := []interface{}{exchange, symbol}
	if account != "" {
		query += ` AND (account = ? OR account IS NULL OR account = '')`
		args = append(args, account)
	}
	var count int64
	if err := s.db.QueryRow(query, args...).Scan(&count); err != nil {
		return false, fmt.Errorf("check unscoped reconciliation history: %w", err)
	}
	return count > 0, nil
}

func (s *SQLStorage) GetLatestReconciliationHistoryByScope(exchange, symbol, account, marketType, accountScope, botID string) (*ReconciliationHistory, error) {
	rows, err := s.QueryReconciliationHistoryByScope(exchange, symbol, account, marketType, accountScope, botID, time.Unix(0, 0).UTC(), time.Now().UTC(), 1, 0)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// QueryReconciliationHistory 查詢對账历史
func (s *SQLStorage) QueryReconciliationHistory(exchange, symbol, account string, startTime, endTime time.Time, limit, offset int) ([]*ReconciliationHistory, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条對账記錄
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 對账历史查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	query := `
		SELECT id, exchange, symbol, account, reconcile_time, local_position, exchange_position, position_diff,
		       active_buy_orders, active_sell_orders, pending_sell_qty,
		       total_buy_qty, total_sell_qty, estimated_profit, actual_profit, created_at
		FROM reconciliation_history
		WHERE reconcile_time >= ? AND reconcile_time <= ?
	`
	args := []interface{}{startTime, endTime}

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
		// 这样可以确保即使舊數據的account字段為空，也能查詢到對账历史
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}

	query += " ORDER BY reconcile_time DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢對账历史失败: %w", err)
	}
	defer rows.Close()

	var histories []*ReconciliationHistory
	for rows.Next() {
		h := &ReconciliationHistory{}
		err := rows.Scan(
			&h.ID,
			&h.Exchange,
			&h.Symbol,
			&h.Account,
			&h.ReconcileTime,
			&h.LocalPosition,
			&h.ExchangePosition,
			&h.PositionDiff,
			&h.ActiveBuyOrders,
			&h.ActiveSellOrders,
			&h.PendingSellQty,
			&h.TotalBuyQty,
			&h.TotalSellQty,
			&h.EstimatedProfit,
			&h.ActualProfit,
			&h.CreatedAt,
		)
		if err != nil {
			continue
		}
		histories = append(histories, h)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍歷對賬歷史失败: %w", err)
	}

	return histories, nil
}

// GetLatestReconciliationHistory 獲取指定币种的最新對账記錄
func (s *SQLStorage) GetLatestReconciliationHistory(exchange, symbol, account string) (*ReconciliationHistory, error) {
	query := `
		SELECT id, exchange, symbol, account, reconcile_time, local_position, exchange_position, position_diff,
		       active_buy_orders, active_sell_orders, pending_sell_qty,
		       total_buy_qty, total_sell_qty, estimated_profit, actual_profit, created_at
		FROM reconciliation_history
		WHERE symbol = ?
	`
	args := []interface{}{symbol}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if account != "" {
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
	query += " ORDER BY reconcile_time DESC LIMIT 1"

	row := s.db.QueryRow(query, args...)
	h := &ReconciliationHistory{}

	err := row.Scan(
		&h.ID,
		&h.Exchange,
		&h.Symbol,
		&h.Account,
		&h.ReconcileTime,
		&h.LocalPosition,
		&h.ExchangePosition,
		&h.PositionDiff,
		&h.ActiveBuyOrders,
		&h.ActiveSellOrders,
		&h.PendingSellQty,
		&h.TotalBuyQty,
		&h.TotalSellQty,
		&h.EstimatedProfit,
		&h.ActualProfit,
		&h.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // 没有記錄，回傳 nil 而不是錯误
		}
		return nil, fmt.Errorf("查詢最新對账記錄失败: %w", err)
	}

	return h, nil
}

// GetReconciliationCount 獲取指定币种的對账次數（统计历史記錄數量）
func (s *SQLStorage) GetReconciliationCount(exchange, symbol, account string) (int64, error) {
	query := `SELECT COUNT(*) FROM reconciliation_history WHERE symbol = ?`
	args := []interface{}{symbol}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if account != "" {
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}

	var count int64
	err := s.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil // 没有記錄，回傳 0
		}
		return 0, fmt.Errorf("统计對账次數失败: %w", err)
	}

	return count, nil
}
