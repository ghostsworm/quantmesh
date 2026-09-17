package storage

import (
	"database/sql"
	"fmt"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// SaveOrder 保存订單（使用 UPSERT 保留已有非零值，支援 SQLite/MySQL/PostgreSQL）
func (s *SQLStorage) SaveOrder(order *Order) error {
	// 轉换為UTC時间存儲
	createdAt := utils.ToUTC(order.CreatedAt)
	updatedAt := utils.ToUTC(order.UpdatedAt)

	// realized_pnl 可能為 nil（表示無數據），需要傳 NULL
	var realizedPnL interface{}
	if order.RealizedPnL != nil {
		realizedPnL = *order.RealizedPnL
	}
	if order.Account == "" {
		order.Account = order.BotID
	}

	var query string
	var args []interface{}

	// 根據數據库類型使用不同的 UPSERT 語法
	switch s.dbType {
	case "mysql":
		// MySQL 使用 ON DUPLICATE KEY UPDATE
		query = `
			INSERT INTO orders
			(order_id, bot_id, account, client_order_id, symbol, side, exchange, type, price, quantity, filled_qty, status, realized_pnl, strategy_name, strategy_type, order_source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				bot_id          = COALESCE(NULLIF(VALUES(bot_id), ''), orders.bot_id),
				account         = COALESCE(NULLIF(VALUES(account), ''), orders.account),
				client_order_id = COALESCE(NULLIF(VALUES(client_order_id), ''), orders.client_order_id),
				symbol          = COALESCE(NULLIF(VALUES(symbol), ''), orders.symbol),
				side            = COALESCE(NULLIF(VALUES(side), ''), orders.side),
				exchange        = COALESCE(NULLIF(VALUES(exchange), ''), orders.exchange),
				type            = COALESCE(NULLIF(VALUES(type), ''), orders.type),
				price           = CASE WHEN VALUES(price) > 0 THEN VALUES(price) ELSE orders.price END,
				quantity        = CASE WHEN VALUES(quantity) > 0 THEN VALUES(quantity) ELSE orders.quantity END,
				filled_qty      = CASE WHEN VALUES(filled_qty) > 0 THEN VALUES(filled_qty) ELSE orders.filled_qty END,
				status          = COALESCE(NULLIF(VALUES(status), ''), orders.status),
				realized_pnl    = COALESCE(VALUES(realized_pnl), orders.realized_pnl),
				strategy_name   = COALESCE(NULLIF(VALUES(strategy_name), ''), orders.strategy_name),
				strategy_type   = COALESCE(NULLIF(VALUES(strategy_type), ''), orders.strategy_type),
				order_source    = COALESCE(NULLIF(VALUES(order_source), ''), orders.order_source),
				updated_at      = VALUES(updated_at)
		`
		args = []interface{}{
			order.OrderID, order.BotID, order.Account, order.ClientOrderID, order.Symbol, order.Side,
			order.Exchange, order.Type, order.Price, order.Quantity, order.FilledQty,
			order.Status, realizedPnL, order.StrategyName, order.StrategyType, order.OrderSource, createdAt, updatedAt,
		}

	case "postgres":
		// PostgreSQL 使用 ON CONFLICT
		query = `
			INSERT INTO orders
			(order_id, bot_id, account, client_order_id, symbol, side, exchange, type, price, quantity, filled_qty, status, realized_pnl, strategy_name, strategy_type, order_source, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
			ON CONFLICT(exchange, account, symbol, order_id) DO UPDATE SET
				bot_id          = COALESCE(NULLIF(EXCLUDED.bot_id, ''), orders.bot_id),
				account         = COALESCE(NULLIF(EXCLUDED.account, ''), orders.account),
				client_order_id = COALESCE(NULLIF(EXCLUDED.client_order_id, ''), orders.client_order_id),
				symbol          = COALESCE(NULLIF(EXCLUDED.symbol, ''), orders.symbol),
				side            = COALESCE(NULLIF(EXCLUDED.side, ''), orders.side),
				exchange        = COALESCE(NULLIF(EXCLUDED.exchange, ''), orders.exchange),
				type            = COALESCE(NULLIF(EXCLUDED.type, ''), orders.type),
				price           = CASE WHEN EXCLUDED.price > 0 THEN EXCLUDED.price ELSE orders.price END,
				quantity        = CASE WHEN EXCLUDED.quantity > 0 THEN EXCLUDED.quantity ELSE orders.quantity END,
				filled_qty      = CASE WHEN EXCLUDED.filled_qty > 0 THEN EXCLUDED.filled_qty ELSE orders.filled_qty END,
				status          = COALESCE(NULLIF(EXCLUDED.status, ''), orders.status),
				realized_pnl    = COALESCE(EXCLUDED.realized_pnl, orders.realized_pnl),
				strategy_name   = COALESCE(NULLIF(EXCLUDED.strategy_name, ''), orders.strategy_name),
				strategy_type   = COALESCE(NULLIF(EXCLUDED.strategy_type, ''), orders.strategy_type),
				order_source    = COALESCE(NULLIF(EXCLUDED.order_source, ''), orders.order_source),
				updated_at      = EXCLUDED.updated_at
		`
		args = []interface{}{
			order.OrderID, order.BotID, order.Account, order.ClientOrderID, order.Symbol, order.Side,
			order.Exchange, order.Type, order.Price, order.Quantity, order.FilledQty,
			order.Status, realizedPnL, order.StrategyName, order.StrategyType, order.OrderSource, createdAt, updatedAt,
		}

	default: // sqlite
		// SQLite 使用 ON CONFLICT
		query = `
			INSERT INTO orders
			(order_id, bot_id, account, client_order_id, symbol, side, exchange, type, price, quantity, filled_qty, status, realized_pnl, strategy_name, strategy_type, order_source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(exchange, account, symbol, order_id) DO UPDATE SET
				bot_id          = COALESCE(NULLIF(excluded.bot_id, ''), orders.bot_id),
				account         = COALESCE(NULLIF(excluded.account, ''), orders.account),
				client_order_id = COALESCE(NULLIF(excluded.client_order_id, ''), orders.client_order_id),
				symbol          = COALESCE(NULLIF(excluded.symbol, ''), orders.symbol),
				side            = COALESCE(NULLIF(excluded.side, ''), orders.side),
				exchange        = COALESCE(NULLIF(excluded.exchange, ''), orders.exchange),
				type            = COALESCE(NULLIF(excluded.type, ''), orders.type),
				price           = CASE WHEN excluded.price > 0 THEN excluded.price ELSE orders.price END,
				quantity        = CASE WHEN excluded.quantity > 0 THEN excluded.quantity ELSE orders.quantity END,
				filled_qty      = CASE WHEN excluded.filled_qty > 0 THEN excluded.filled_qty ELSE orders.filled_qty END,
				status          = COALESCE(NULLIF(excluded.status, ''), orders.status),
				realized_pnl    = COALESCE(excluded.realized_pnl, orders.realized_pnl),
				strategy_name   = COALESCE(NULLIF(excluded.strategy_name, ''), orders.strategy_name),
				strategy_type   = COALESCE(NULLIF(excluded.strategy_type, ''), orders.strategy_type),
				order_source    = COALESCE(NULLIF(excluded.order_source, ''), orders.order_source),
				updated_at      = excluded.updated_at
		`
		args = []interface{}{
			order.OrderID, order.BotID, order.Account, order.ClientOrderID, order.Symbol, order.Side,
			order.Exchange, order.Type, order.Price, order.Quantity, order.FilledQty,
			order.Status, realizedPnL, order.StrategyName, order.StrategyType, order.OrderSource, createdAt, updatedAt,
		}
	}

	_, err := s.db.Exec(query, args...)
	return err
}

// QueryOrders 查詢訂單
func (s *SQLStorage) QueryOrders(limit, offset int, status string) ([]*Order, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条订單
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 订單查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	query := `
		SELECT order_id, COALESCE(bot_id, '') as bot_id, COALESCE(account, COALESCE(bot_id, '')) as account, client_order_id, symbol, side,
			COALESCE(exchange, '') as exchange, COALESCE(type, '') as type,
			price, quantity, COALESCE(filled_qty, 0) as filled_qty,
			status, realized_pnl,
			COALESCE(strategy_name, '') as strategy_name, COALESCE(strategy_type, '') as strategy_type,
			COALESCE(order_source, '') as order_source,
			created_at, updated_at
		FROM orders
		WHERE 1=1
	`
	args := []interface{}{}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢訂單失败: %w", err)
	}
	defer rows.Close()

	var orders []*Order
	for rows.Next() {
		order := &Order{}
		var realizedPnL sql.NullFloat64
		err := rows.Scan(
			&order.OrderID,
			&order.BotID,
			&order.Account,
			&order.ClientOrderID,
			&order.Symbol,
			&order.Side,
			&order.Exchange,
			&order.Type,
			&order.Price,
			&order.Quantity,
			&order.FilledQty,
			&order.Status,
			&realizedPnL,
			&order.StrategyName,
			&order.StrategyType,
			&order.OrderSource,
			&order.CreatedAt,
			&order.UpdatedAt,
		)
		if err != nil {
			continue
		}
		if realizedPnL.Valid {
			v := realizedPnL.Float64
			order.RealizedPnL = &v
		}
		orders = append(orders, order)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍歷訂單失败: %w", err)
	}

	return orders, nil
}

// QueryOrdersWithTimeRange 查詢訂單（带时间范围）
func (s *SQLStorage) QueryOrdersWithTimeRange(limit, offset int, status string, startTime, endTime *time.Time) ([]*Order, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条订單
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 订單查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	query := `
		SELECT order_id, COALESCE(bot_id, '') as bot_id, COALESCE(account, COALESCE(bot_id, '')) as account, client_order_id, symbol, side,
			COALESCE(exchange, '') as exchange, COALESCE(type, '') as type,
			price, quantity, COALESCE(filled_qty, 0) as filled_qty,
			status, realized_pnl,
			COALESCE(strategy_name, '') as strategy_name, COALESCE(strategy_type, '') as strategy_type,
			COALESCE(order_source, '') as order_source,
			created_at, updated_at
		FROM orders
		WHERE 1=1
	`
	args := []interface{}{}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	// 添加时间范围条件
	if startTime != nil {
		query += " AND created_at >= ?"
		args = append(args, *startTime)
	}
	if endTime != nil {
		query += " AND created_at <= ?"
		args = append(args, *endTime)
	}

	query += " ORDER BY created_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢訂單失败: %w", err)
	}
	defer rows.Close()

	var orders []*Order
	for rows.Next() {
		order := &Order{}
		var realizedPnL sql.NullFloat64
		err := rows.Scan(
			&order.OrderID,
			&order.BotID,
			&order.Account,
			&order.ClientOrderID,
			&order.Symbol,
			&order.Side,
			&order.Exchange,
			&order.Type,
			&order.Price,
			&order.Quantity,
			&order.FilledQty,
			&order.Status,
			&realizedPnL,
			&order.StrategyName,
			&order.StrategyType,
			&order.OrderSource,
			&order.CreatedAt,
			&order.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("掃描訂單失败: %w", err)
		}
		if realizedPnL.Valid {
			order.RealizedPnL = &realizedPnL.Float64
		}
		orders = append(orders, order)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("查詢訂單迭代失败: %w", err)
	}

	return orders, nil
}

// CountOrders 统计订單数量（不受 limit 限制，返回真实总数）
func (s *SQLStorage) CountOrders(status string) (int64, error) {
	query := `SELECT COUNT(*) FROM orders WHERE 1=1`
	args := []interface{}{}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	var count int64
	err := s.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计订單数量失败: %w", err)
	}
	return count, nil
}

// CountOrdersWithFilter 带筛选条件的订单计数（支持 exchange、symbol 筛选）
func (s *SQLStorage) CountOrdersWithFilter(status, exchange, symbol string, startTime, endTime *time.Time) (int64, error) {
	query := `SELECT COUNT(*) FROM orders WHERE 1=1`
	args := []interface{}{}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	if exchange != "" {
		query += " AND (LOWER(COALESCE(exchange, '')) = LOWER(?) OR COALESCE(exchange, '') = '')"
		args = append(args, exchange)
	}
	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}
	if startTime != nil {
		query += " AND updated_at >= ?"
		args = append(args, *startTime)
	}
	if endTime != nil {
		query += " AND updated_at <= ?"
		args = append(args, *endTime)
	}

	var count int64
	err := s.db.QueryRow(query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("统计订單数量失败: %w", err)
	}
	return count, nil
}

// QueryOrdersWithFilter 带完整筛选条件的订单查询（支持 exchange、symbol 筛选）
func (s *SQLStorage) QueryOrdersWithFilter(limit, offset int, status, exchange, symbol string, startTime, endTime *time.Time) ([]*Order, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条订單
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 订單查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	query := `
		SELECT order_id, COALESCE(bot_id, '') as bot_id, COALESCE(account, COALESCE(bot_id, '')) as account, client_order_id, symbol, side,
			COALESCE(exchange, '') as exchange, COALESCE(type, '') as type,
			price, quantity, COALESCE(filled_qty, 0) as filled_qty,
			status, realized_pnl,
			COALESCE(strategy_name, '') as strategy_name, COALESCE(strategy_type, '') as strategy_type,
			COALESCE(order_source, '') as order_source,
			created_at, updated_at
		FROM orders
		WHERE 1=1
	`
	args := []interface{}{}

	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}

	// 添加 exchange 筛选条件（大小写不敏感）
	// 兼容历史订单：早期 SaveOrder 未写入 exchange，导致 exchange 为空；筛选时也包含空 exchange 的订单
	if exchange != "" {
		query += " AND (LOWER(COALESCE(exchange, '')) = LOWER(?) OR COALESCE(exchange, '') = '')"
		args = append(args, exchange)
	}

	// 添加 symbol 筛选条件
	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}

	// 添加时间范围条件（按 updated_at：挂单早、近日才成交的订单可被「最近24小时」命中）
	if startTime != nil {
		query += " AND updated_at >= ?"
		args = append(args, *startTime)
	}
	if endTime != nil {
		query += " AND updated_at <= ?"
		args = append(args, *endTime)
	}

	query += " ORDER BY updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢訂單失败: %w", err)
	}
	defer rows.Close()

	var orders []*Order
	for rows.Next() {
		order := &Order{}
		var realizedPnL sql.NullFloat64
		err := rows.Scan(
			&order.OrderID,
			&order.BotID,
			&order.Account,
			&order.ClientOrderID,
			&order.Symbol,
			&order.Side,
			&order.Exchange,
			&order.Type,
			&order.Price,
			&order.Quantity,
			&order.FilledQty,
			&order.Status,
			&realizedPnL,
			&order.StrategyName,
			&order.StrategyType,
			&order.OrderSource,
			&order.CreatedAt,
			&order.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("掃描订單失败: %w", err)
		}
		if realizedPnL.Valid {
			order.RealizedPnL = &realizedPnL.Float64
		}
		orders = append(orders, order)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("查詢訂單迭代失败: %w", err)
	}

	return orders, nil
}

// GetFilledOrderQtySumBeforeTime 獲取指定時間前已成交訂單的買/賣數量合計（用於日初持倉）
func (s *SQLStorage) GetFilledOrderQtySumBeforeTime(exchange, symbol string, before time.Time) (buyQty, sellQty float64, err error) {
	query := `
		SELECT
			COALESCE(SUM(CASE WHEN side = 'BUY' THEN COALESCE(filled_qty, quantity) ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN side = 'SELL' THEN COALESCE(filled_qty, quantity) ELSE 0 END), 0)
		FROM orders
		WHERE status = 'FILLED' AND created_at < ?
	`
	args := []interface{}{before}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}
	var bq, sq sql.NullFloat64
	err = s.db.QueryRow(query, args...).Scan(&bq, &sq)
	if err != nil {
		return 0, 0, err
	}
	if bq.Valid {
		buyQty = bq.Float64
	}
	if sq.Valid {
		sellQty = sq.Float64
	}
	return buyQty, sellQty, nil
}
