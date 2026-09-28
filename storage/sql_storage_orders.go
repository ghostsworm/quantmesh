package storage

import (
	"database/sql"
	"fmt"
	"strings"
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
			(order_id, bot_id, account, market_type, account_scope, client_order_id, symbol, side, exchange, type, price, quantity, filled_qty, status, realized_pnl, strategy_name, strategy_type, order_source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				bot_id          = COALESCE(NULLIF(VALUES(bot_id), ''), orders.bot_id),
				account         = COALESCE(NULLIF(VALUES(account), ''), orders.account),
				market_type     = COALESCE(NULLIF(VALUES(market_type), ''), orders.market_type),
				account_scope   = COALESCE(NULLIF(VALUES(account_scope), ''), orders.account_scope),
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
			order.OrderID, order.BotID, order.Account, order.MarketType, order.AccountScope, order.ClientOrderID, order.Symbol, order.Side,
			order.Exchange, order.Type, order.Price, order.Quantity, order.FilledQty,
			order.Status, realizedPnL, order.StrategyName, order.StrategyType, order.OrderSource, createdAt, updatedAt,
		}

	case "postgres":
		// PostgreSQL 使用 ON CONFLICT
		query = `
			INSERT INTO orders
			(order_id, bot_id, account, market_type, account_scope, client_order_id, symbol, side, exchange, type, price, quantity, filled_qty, status, realized_pnl, strategy_name, strategy_type, order_source, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
			ON CONFLICT(exchange, account, symbol, order_id) DO UPDATE SET
				bot_id          = COALESCE(NULLIF(EXCLUDED.bot_id, ''), orders.bot_id),
				account         = COALESCE(NULLIF(EXCLUDED.account, ''), orders.account),
				market_type     = COALESCE(NULLIF(EXCLUDED.market_type, ''), orders.market_type),
				account_scope   = COALESCE(NULLIF(EXCLUDED.account_scope, ''), orders.account_scope),
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
			order.OrderID, order.BotID, order.Account, order.MarketType, order.AccountScope, order.ClientOrderID, order.Symbol, order.Side,
			order.Exchange, order.Type, order.Price, order.Quantity, order.FilledQty,
			order.Status, realizedPnL, order.StrategyName, order.StrategyType, order.OrderSource, createdAt, updatedAt,
		}

	default: // sqlite
		// SQLite 使用 ON CONFLICT
		query = `
			INSERT INTO orders
			(order_id, bot_id, account, market_type, account_scope, client_order_id, symbol, side, exchange, type, price, quantity, filled_qty, status, realized_pnl, strategy_name, strategy_type, order_source, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(exchange, account, symbol, order_id) DO UPDATE SET
				bot_id          = COALESCE(NULLIF(excluded.bot_id, ''), orders.bot_id),
				account         = COALESCE(NULLIF(excluded.account, ''), orders.account),
				market_type     = COALESCE(NULLIF(excluded.market_type, ''), orders.market_type),
				account_scope   = COALESCE(NULLIF(excluded.account_scope, ''), orders.account_scope),
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
			order.OrderID, order.BotID, order.Account, order.MarketType, order.AccountScope, order.ClientOrderID, order.Symbol, order.Side,
			order.Exchange, order.Type, order.Price, order.Quantity, order.FilledQty,
			order.Status, realizedPnL, order.StrategyName, order.StrategyType, order.OrderSource, createdAt, updatedAt,
		}
	}

	_, err := s.db.Exec(query, args...)
	if s.dbType == "sqlite" && isOrdersOnConflictMismatch(err) {
		// 自愈：orders 表被外部重建（如同庫 GORM AutoMigrate）導致複合唯一索引丟失，修復後重試一次。
		logger.Warn("⚠️ orders 複合唯一索引缺失（ON CONFLICT 不匹配），嘗試現場修復後重試: order_id=%d symbol=%s", order.OrderID, order.Symbol)
		if repairErr := ensureOrdersCompositeUniqueConstraint(s.db); repairErr != nil {
			return fmt.Errorf("保存訂單 order_id=%d 失败且修復 orders 唯一索引失败: %v (原始錯誤: %w)", order.OrderID, repairErr, err)
		}
		_, err = s.db.Exec(query, args...)
	}
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

type DailyOrderCashflow struct {
	BuyOrders, SellOrders                int
	BuyQty, BuyValue, SellQty, SellValue float64
	StartBuyQty, StartSellQty            float64
	RealizedPnL                          float64
}

func (s *SQLStorage) GetExistingOrderIDsByScope(exchange, marketType, symbol, accountScope string) (map[int64]bool, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(marketType) == "" || strings.TrimSpace(symbol) == "" || strings.TrimSpace(accountScope) == "" {
		return nil, fmt.Errorf("existing order lookup requires complete account and market scope")
	}
	rows, err := s.db.Query(`SELECT order_id FROM orders WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ?`, exchange, marketType, symbol, accountScope)
	if err != nil {
		return nil, fmt.Errorf("query scoped existing order IDs: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]bool)
	for rows.Next() {
		var orderID int64
		if err := rows.Scan(&orderID); err != nil {
			return nil, fmt.Errorf("scan scoped existing order ID: %w", err)
		}
		result[orderID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scoped existing order IDs: %w", err)
	}
	return result, nil
}

// GetExistingOrderIDsForScope 查询本批成交触及的订单 ID，避免把账户全部历史 ID 装入内存。
func (s *SQLStorage) GetExistingOrderIDsForScope(exchange, marketType, symbol, accountScope string, orderIDs []int64) (map[int64]bool, error) {
	if strings.TrimSpace(exchange) == "" || strings.TrimSpace(marketType) == "" || strings.TrimSpace(symbol) == "" || strings.TrimSpace(accountScope) == "" {
		return nil, fmt.Errorf("existing order lookup requires complete account and market scope")
	}
	result := make(map[int64]bool, len(orderIDs))
	if len(orderIDs) == 0 {
		return result, nil
	}
	query := `SELECT order_id FROM orders WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND order_id IN (`
	args := []interface{}{exchange, marketType, symbol, accountScope}
	for i, orderID := range orderIDs {
		if orderID <= 0 {
			return nil, fmt.Errorf("invalid order ID in scoped lookup")
		}
		if i > 0 {
			query += ","
		}
		query += "?"
		args = append(args, orderID)
	}
	query += ")"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query scoped existing order IDs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var orderID int64
		if err := rows.Scan(&orderID); err != nil {
			return nil, fmt.Errorf("scan scoped existing order ID: %w", err)
		}
		result[orderID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scoped existing order IDs: %w", err)
	}
	return result, nil
}

// QueryDailyOrderCashflowByScope computes the day's cash flow and beginning
// inventory inside one immutable account/market/symbol scope without paging
// order rows into application memory.
func (s *SQLStorage) QueryDailyOrderCashflowByScope(account, exchange, marketType, symbol, accountScope, botID string, start, end time.Time) (DailyOrderCashflow, error) {
	var result DailyOrderCashflow
	if accountScope == "" || marketType == "" || exchange == "" || symbol == "" || !start.Before(end) {
		return result, fmt.Errorf("daily order query requires complete account and market scope")
	}
	query := `SELECT
		COALESCE(SUM(CASE WHEN created_at >= ? AND side = 'BUY' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at >= ? AND side = 'SELL' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at >= ? AND side = 'BUY' THEN COALESCE(NULLIF(filled_qty, 0), quantity) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at >= ? AND side = 'BUY' THEN price * COALESCE(NULLIF(filled_qty, 0), quantity) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at >= ? AND side = 'SELL' THEN COALESCE(NULLIF(filled_qty, 0), quantity) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at >= ? AND side = 'SELL' THEN price * COALESCE(NULLIF(filled_qty, 0), quantity) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at < ? AND side = 'BUY' THEN COALESCE(NULLIF(filled_qty, 0), quantity) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at < ? AND side = 'SELL' THEN COALESCE(NULLIF(filled_qty, 0), quantity) ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN created_at >= ? THEN COALESCE(realized_pnl, 0) ELSE 0 END), 0)
		FROM orders WHERE status = 'FILLED' AND created_at < ? AND exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ?`
	args := []interface{}{start, start, start, start, start, start, start, start, start, end, exchange, marketType, symbol, accountScope}
	if account != "" {
		query += ` AND account = ?`
		args = append(args, account)
	}
	if botID = strings.TrimSpace(botID); botID != "" {
		query += ` AND COALESCE(bot_id, '') = ?`
		args = append(args, botID)
	}
	err := s.db.QueryRow(query, args...).Scan(&result.BuyOrders, &result.SellOrders, &result.BuyQty, &result.BuyValue, &result.SellQty, &result.SellValue, &result.StartBuyQty, &result.StartSellQty, &result.RealizedPnL)
	if err != nil {
		return DailyOrderCashflow{}, fmt.Errorf("query scoped daily order cashflow: %w", err)
	}
	return result, nil
}

func (s *SQLStorage) QueryTopDailyOrdersByScope(account, exchange, marketType, symbol, accountScope, botID string, start, end time.Time) (winners, losers []*Order, err error) {
	if accountScope == "" || marketType == "" || exchange == "" || symbol == "" || !start.Before(end) {
		return nil, nil, fmt.Errorf("daily order ranking requires complete account and market scope")
	}
	where := `status = 'FILLED' AND created_at >= ? AND created_at < ? AND exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND realized_pnl IS NOT NULL`
	args := []interface{}{start, end, exchange, marketType, symbol, accountScope}
	if account != "" {
		where += ` AND account = ?`
		args = append(args, account)
	}
	if botID = strings.TrimSpace(botID); botID != "" {
		where += ` AND COALESCE(bot_id, '') = ?`
		args = append(args, botID)
	}
	queryTop := func(comparison, direction string) ([]*Order, error) {
		query := `SELECT order_id, side, price, COALESCE(NULLIF(filled_qty, 0), quantity), realized_pnl FROM orders WHERE ` + where + ` AND realized_pnl ` + comparison + ` 0 ORDER BY realized_pnl ` + direction + `, order_id DESC LIMIT 20`
		rows, err := s.db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("query daily realized pnl ranking: %w", err)
		}
		defer rows.Close()
		var result []*Order
		for rows.Next() {
			order := &Order{RealizedPnL: new(float64)}
			if err := rows.Scan(&order.OrderID, &order.Side, &order.Price, &order.FilledQty, order.RealizedPnL); err != nil {
				return nil, fmt.Errorf("scan daily realized pnl ranking: %w", err)
			}
			result = append(result, order)
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate daily realized pnl ranking: %w", err)
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
