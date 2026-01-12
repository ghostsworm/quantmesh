package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"quantmesh/logger"
	"quantmesh/utils"
)

// SQLiteStorage SQLite 存储实现
type SQLiteStorage struct {
	db     *sql.DB
	closed bool
}

// NewSQLiteStorage 创建 SQLite 存储
func NewSQLiteStorage(path string) (*SQLiteStorage, error) {
	// 使用 WAL 模式提高并发性能
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 设置连接池
	db.SetMaxOpenConns(1) // SQLite 并发限制
	db.SetMaxIdleConns(1)

	// 创建表
	if err := createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("创建表失败: %w", err)
	}

	// 迁移：添加 exchange 字段（如果不存在）
	if err := migrateTradesTable(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("迁移 trades 表失败: %w", err)
	}

	return &SQLiteStorage{db: db}, nil
}

// createTables 创建表
func createTables(db *sql.DB) error {
	// 订单表
	ordersSQL := `
	CREATE TABLE IF NOT EXISTS orders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		order_id BIGINT UNIQUE,
		client_order_id TEXT,
		symbol TEXT,
		side TEXT,
		price DECIMAL(20,8),
		quantity DECIMAL(20,8),
		status TEXT,
		created_at TIMESTAMP,
		updated_at TIMESTAMP
	);`

	// 持仓表
	positionsSQL := `
	CREATE TABLE IF NOT EXISTS positions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		slot_price DECIMAL(20,8),
		symbol TEXT,
		size DECIMAL(20,8),
		entry_price DECIMAL(20,8),
		current_price DECIMAL(20,8),
		pnl DECIMAL(20,8),
		opened_at TIMESTAMP,
		closed_at TIMESTAMP
	);`

	// 交易表（买卖配对）
	tradesSQL := `
	CREATE TABLE IF NOT EXISTS trades (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		buy_order_id BIGINT,
		sell_order_id BIGINT,
		exchange TEXT,
		symbol TEXT,
		buy_price DECIMAL(20,8),
		sell_price DECIMAL(20,8),
		quantity DECIMAL(20,8),
		pnl DECIMAL(20,8),
		created_at TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_trades_exchange_symbol ON trades(exchange, symbol);
	CREATE INDEX IF NOT EXISTS idx_trades_created_at ON trades(created_at);`

	// 事件表
	eventsSQL := `
	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_type TEXT,
		data TEXT,
		created_at TIMESTAMP
	);`

	// 系统监控细粒度数据表
	systemMetricsSQL := `
	CREATE TABLE IF NOT EXISTS system_metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		cpu_percent REAL NOT NULL,
		memory_mb REAL NOT NULL,
		memory_percent REAL,
		process_id INTEGER,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_system_metrics_timestamp ON system_metrics(timestamp);`

	// 系统监控每日汇总数据表
	dailySystemMetricsSQL := `
	CREATE TABLE IF NOT EXISTS daily_system_metrics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		date DATE NOT NULL UNIQUE,
		avg_cpu_percent REAL NOT NULL,
		max_cpu_percent REAL NOT NULL,
		min_cpu_percent REAL NOT NULL,
		avg_memory_mb REAL NOT NULL,
		max_memory_mb REAL NOT NULL,
		min_memory_mb REAL NOT NULL,
		sample_count INTEGER NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_daily_system_metrics_date ON daily_system_metrics(date);`

	// 统计表
	statisticsSQL := `
	CREATE TABLE IF NOT EXISTS statistics (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		date DATE UNIQUE,
		total_trades INTEGER,
		total_volume DECIMAL(20,8),
		total_pnl DECIMAL(20,8),
		win_rate DECIMAL(5,2),
		created_at TIMESTAMP
	);`

	// 对账历史表
	reconciliationHistorySQL := `
	CREATE TABLE IF NOT EXISTS reconciliation_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT,
		reconcile_time TIMESTAMP,
		local_position DECIMAL(20,8),
		exchange_position DECIMAL(20,8),
		position_diff DECIMAL(20,8),
		active_buy_orders INTEGER,
		active_sell_orders INTEGER,
		pending_sell_qty DECIMAL(20,8),
		total_buy_qty DECIMAL(20,8),
		total_sell_qty DECIMAL(20,8),
		estimated_profit DECIMAL(20,8),
		actual_profit DECIMAL(20,8) DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_reconciliation_history_symbol ON reconciliation_history(symbol);
	CREATE INDEX IF NOT EXISTS idx_reconciliation_history_time ON reconciliation_history(reconcile_time);`

	// 风控检查历史表
	riskCheckHistorySQL := `
	CREATE TABLE IF NOT EXISTS risk_check_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		check_time TIMESTAMP NOT NULL,
		symbol TEXT NOT NULL,
		is_healthy INTEGER NOT NULL,
		price_deviation REAL,
		volume_ratio REAL,
		reason TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_risk_check_history_time ON risk_check_history(check_time);
	CREATE INDEX IF NOT EXISTS idx_risk_check_history_symbol ON risk_check_history(symbol);
	CREATE INDEX IF NOT EXISTS idx_risk_check_history_time_symbol ON risk_check_history(check_time, symbol);`

	// 资金费率表
	fundingRatesSQL := `
	CREATE TABLE IF NOT EXISTS funding_rates (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		exchange TEXT NOT NULL,
		rate REAL NOT NULL,
		timestamp TIMESTAMP NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_funding_rates_symbol ON funding_rates(symbol);
	CREATE INDEX IF NOT EXISTS idx_funding_rates_timestamp ON funding_rates(timestamp);
	CREATE INDEX IF NOT EXISTS idx_funding_rates_symbol_timestamp ON funding_rates(symbol, timestamp);`

	// AI提示词模板表
	aiPromptsSQL := `
	CREATE TABLE IF NOT EXISTS ai_prompts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		module TEXT UNIQUE NOT NULL,
		template TEXT NOT NULL,
		system_prompt TEXT,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_ai_prompts_module ON ai_prompts(module);`

	// 价差数据表
	basisDataSQL := `
	CREATE TABLE IF NOT EXISTS basis_data (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		exchange TEXT NOT NULL,
		spot_price REAL NOT NULL,
		futures_price REAL NOT NULL,
		basis REAL NOT NULL,
		basis_percent REAL NOT NULL,
		funding_rate REAL,
		timestamp DATETIME NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_basis_symbol_time ON basis_data(symbol, timestamp);
	CREATE INDEX IF NOT EXISTS idx_basis_exchange ON basis_data(exchange);`

	// 创建索引
	indexesSQL := `
	CREATE INDEX IF NOT EXISTS idx_orders_order_id ON orders(order_id);
	CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at);
	CREATE INDEX IF NOT EXISTS idx_positions_slot_price ON positions(slot_price);
	CREATE INDEX IF NOT EXISTS idx_trades_created_at ON trades(created_at);
	CREATE INDEX IF NOT EXISTS idx_trades_symbol ON trades(symbol);
	CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);
	`

	// 执行创建语句
	sqls := []string{
		ordersSQL,
		positionsSQL,
		tradesSQL,
		eventsSQL,
		systemMetricsSQL,
		dailySystemMetricsSQL,
		statisticsSQL,
		reconciliationHistorySQL,
		riskCheckHistorySQL,
		fundingRatesSQL,
		aiPromptsSQL,
		basisDataSQL,
		indexesSQL,
	}
	for _, sql := range sqls {
		if _, err := db.Exec(sql); err != nil {
			return fmt.Errorf("执行 SQL 失败: %w", err)
		}
	}

	// 迁移：为已存在的表添加 actual_profit 字段（如果不存在）
	if err := migrateReconciliationHistory(db); err != nil {
		return fmt.Errorf("迁移对账历史表失败: %w", err)
	}

	// 迁移：为 events 表添加 event_type 字段（如果不存在）
	if err := migrateEventsTable(db); err != nil {
		return fmt.Errorf("迁移事件表失败: %w", err)
	}

	return nil
}

// migrateEventsTable 迁移 events 表，添加 event_type 字段
func migrateEventsTable(db *sql.DB) error {
	row := db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('events') 
		WHERE name='event_type'
	`)
	var count int
	if err := row.Scan(&count); err != nil {
		return err
	}

	if count == 0 {
		logger.Info("🔄 [数据库] 为 events 表添加 event_type 列...")
		_, err := db.Exec(`ALTER TABLE events ADD COLUMN event_type TEXT`)
		if err != nil {
			return err
		}
	}
	return nil
}

// migrateReconciliationHistory 迁移对账历史表，添加 actual_profit 和 created_at 字段（如果不存在）
func migrateReconciliationHistory(db *sql.DB) error {
	// 检查 actual_profit 字段是否存在
	row := db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('reconciliation_history') 
		WHERE name='actual_profit'
	`)
	var count int
	if err := row.Scan(&count); err != nil {
		return err
	}

	// 如果字段不存在，添加它
	if count == 0 {
		_, err := db.Exec(`
			ALTER TABLE reconciliation_history 
			ADD COLUMN actual_profit DECIMAL(20,8) DEFAULT 0
		`)
		if err != nil {
			return err
		}
	}

	// 检查 created_at 字段是否存在
	row = db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('reconciliation_history') 
		WHERE name='created_at'
	`)
	if err := row.Scan(&count); err != nil {
		return err
	}

	// 如果字段不存在，添加它
	if count == 0 {
		_, err := db.Exec(`
			ALTER TABLE reconciliation_history 
			ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		`)
		if err != nil {
			return err
		}
	}

	return nil
}

// migrateTradesTable 迁移 trades 表，添加 exchange 字段
func migrateTradesTable(db *sql.DB) error {
	logger.Info("🔧 开始检查 trades 表结构...")
	
	// 检查 exchange 列是否存在
	rows, err := db.Query(`PRAGMA table_info(trades)`)
	if err != nil {
		return fmt.Errorf("检查表结构失败: %w", err)
	}
	defer rows.Close()

	hasExchangeColumn := false
	for rows.Next() {
		var cid int
		var name string
		var dataType string
		var notNull int
		var defaultValue interface{}
		var pk int

		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			continue
		}
		if name == "exchange" {
			hasExchangeColumn = true
			break
		}
	}

	if !hasExchangeColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 exchange 字段")

		// 添加 exchange 列
		logger.Info("🔧 添加 exchange 列...")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN exchange TEXT`)
		if err != nil {
			return fmt.Errorf("添加 exchange 列失败: %w", err)
		}
		logger.Info("✅ exchange 列添加成功")

		// 更新现有数据
		logger.Info("🔧 更新历史数据...")
		result, err := db.Exec(`UPDATE trades SET exchange = 'binance' WHERE exchange IS NULL`)
		if err != nil {
			return fmt.Errorf("更新现有数据失败: %w", err)
		}

		rowsAffected, _ := result.RowsAffected()
		logger.Info("✅ 迁移完成：已更新 %d 条历史交易记录的 exchange 字段为 binance", rowsAffected)

		// 创建索引
		logger.Info("🔧 创建索引...")
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_exchange_symbol ON trades(exchange, symbol)`)
		if err != nil {
			logger.Warn("⚠️ 创建索引失败: %v", err)
		} else {
			logger.Info("✅ 索引创建成功")
		}
	} else {
		logger.Info("✅ exchange 列已存在，跳过迁移")
	}

	logger.Info("✅ trades 表迁移检查完成")
	return nil
}

// SaveOrder 保存订单
func (s *SQLiteStorage) SaveOrder(order *Order) error {
	// 转换为UTC时间存储
	createdAt := utils.ToUTC(order.CreatedAt)
	updatedAt := utils.ToUTC(order.UpdatedAt)
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO orders 
		(order_id, client_order_id, symbol, side, price, quantity, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, order.OrderID, order.ClientOrderID, order.Symbol, order.Side,
		order.Price, order.Quantity, order.Status, createdAt, updatedAt)
	return err
}

// SavePosition 保存持仓
func (s *SQLiteStorage) SavePosition(position *Position) error {
	// 转换为UTC时间存储
	openedAt := utils.ToUTC(position.OpenedAt)
	var closedAt interface{}
	if position.ClosedAt != nil {
		closedAtUTC := utils.ToUTC(*position.ClosedAt)
		closedAt = closedAtUTC
	}

	_, err := s.db.Exec(`
		INSERT INTO positions 
		(slot_price, symbol, size, entry_price, current_price, pnl, opened_at, closed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, position.SlotPrice, position.Symbol, position.Size,
		position.EntryPrice, position.CurrentPrice, position.PnL,
		openedAt, closedAt)
	return err
}

// SaveTrade 保存交易
func (s *SQLiteStorage) SaveTrade(trade *Trade) error {
	// 转换为UTC时间存储
	createdAt := utils.ToUTC(trade.CreatedAt)
	// 确保 exchange 不为空，默认为 binance（兼容旧数据）
	exchange := trade.Exchange
	if exchange == "" {
		exchange = "binance"
	}
	_, err := s.db.Exec(`
		INSERT INTO trades 
		(buy_order_id, sell_order_id, exchange, symbol, buy_price, sell_price, quantity, pnl, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, trade.BuyOrderID, trade.SellOrderID, exchange, trade.Symbol,
		trade.BuyPrice, trade.SellPrice, trade.Quantity, trade.PnL, createdAt)
	return err
}

// SaveSystemMetrics 保存系统监控细粒度数据
func (s *SQLiteStorage) SaveSystemMetrics(metrics *SystemMetrics) error {
	// 转换为UTC时间存储
	timestamp := utils.ToUTC(metrics.Timestamp)
	var memoryPercent interface{}
	if metrics.MemoryPercent > 0 {
		memoryPercent = metrics.MemoryPercent
	}

	_, err := s.db.Exec(`
		INSERT INTO system_metrics 
		(timestamp, cpu_percent, memory_mb, memory_percent, process_id)
		VALUES (?, ?, ?, ?, ?)
	`, timestamp, metrics.CPUPercent, metrics.MemoryMB, memoryPercent, metrics.ProcessID)
	return err
}

// SaveDailySystemMetrics 保存系统监控每日汇总数据
func (s *SQLiteStorage) SaveDailySystemMetrics(metrics *DailySystemMetrics) error {
	// 转换为UTC时间存储
	date := utils.ToUTC(metrics.Date)
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO daily_system_metrics 
		(date, avg_cpu_percent, max_cpu_percent, min_cpu_percent, 
		 avg_memory_mb, max_memory_mb, min_memory_mb, sample_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, date, metrics.AvgCPUPercent, metrics.MaxCPUPercent, metrics.MinCPUPercent,
		metrics.AvgMemoryMB, metrics.MaxMemoryMB, metrics.MinMemoryMB, metrics.SampleCount)
	return err
}

// SaveEvent 保存事件
func (s *SQLiteStorage) SaveEvent(eventType string, data map[string]interface{}) error {
	// 检查是否是系统监控事件
	if eventType == "system_metrics" {
		return s.saveSystemMetricsFromMap(data)
	}

	// 将 data 序列化为 JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化事件数据失败: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO events (event_type, data, created_at)
		VALUES (?, ?, ?)
	`, eventType, string(jsonData), utils.NowUTC())
	return err
}

// saveSystemMetricsFromMap 从 map 保存系统监控数据
func (s *SQLiteStorage) saveSystemMetricsFromMap(data map[string]interface{}) error {
	metrics := &SystemMetrics{}

	if timestamp, ok := data["timestamp"].(time.Time); ok {
		metrics.Timestamp = utils.ToUTC(timestamp)
	} else if timestampStr, ok := data["timestamp"].(string); ok {
		var err error
		parsedTime, err := time.Parse(time.RFC3339, timestampStr)
		if err != nil {
			metrics.Timestamp = utils.NowUTC()
		} else {
			metrics.Timestamp = utils.ToUTC(parsedTime)
		}
	} else {
		metrics.Timestamp = utils.NowUTC()
	}

	if cpuPercent, ok := data["cpu_percent"].(float64); ok {
		metrics.CPUPercent = cpuPercent
	}
	if memoryMB, ok := data["memory_mb"].(float64); ok {
		metrics.MemoryMB = memoryMB
	}
	if memoryPercent, ok := data["memory_percent"].(float64); ok {
		metrics.MemoryPercent = memoryPercent
	}
	if processID, ok := data["process_id"].(int); ok {
		metrics.ProcessID = processID
	} else if processID, ok := data["process_id"].(float64); ok {
		metrics.ProcessID = int(processID)
	}

	return s.SaveSystemMetrics(metrics)
}

// SaveStatistics 保存统计
func (s *SQLiteStorage) SaveStatistics(stats *Statistics) error {
	// 转换为UTC时间存储
	date := utils.ToUTC(stats.Date)
	createdAt := utils.ToUTC(stats.CreatedAt)
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO statistics 
		(date, total_trades, total_volume, total_pnl, win_rate, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, date, stats.TotalTrades, stats.TotalVolume,
		stats.TotalPnL, stats.WinRate, createdAt)
	return err
}

// QueryOrders 查询订单
func (s *SQLiteStorage) QueryOrders(limit, offset int, status string) ([]*Order, error) {
	query := `
		SELECT order_id, client_order_id, symbol, side, price, quantity, status, created_at, updated_at
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
		return nil, fmt.Errorf("查询订单失败: %w", err)
	}
	defer rows.Close()

	var orders []*Order
	for rows.Next() {
		order := &Order{}
		err := rows.Scan(
			&order.OrderID,
			&order.ClientOrderID,
			&order.Symbol,
			&order.Side,
			&order.Price,
			&order.Quantity,
			&order.Status,
			&order.CreatedAt,
			&order.UpdatedAt,
		)
		if err != nil {
			continue
		}
		orders = append(orders, order)
	}

	return orders, nil
}

// QueryTrades 查询交易
func (s *SQLiteStorage) QueryTrades(startTime, endTime time.Time, limit, offset int) ([]*Trade, error) {
	rows, err := s.db.Query(`
		SELECT buy_order_id, sell_order_id, exchange, symbol, buy_price, sell_price, quantity, pnl, created_at
		FROM trades
		WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, startTime, endTime, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("查询交易失败: %w", err)
	}
	defer rows.Close()

	var trades []*Trade
	for rows.Next() {
		trade := &Trade{}
		err := rows.Scan(
			&trade.BuyOrderID,
			&trade.SellOrderID,
			&trade.Exchange,
			&trade.Symbol,
			&trade.BuyPrice,
			&trade.SellPrice,
			&trade.Quantity,
			&trade.PnL,
			&trade.CreatedAt,
		)
		if err != nil {
			continue
		}
		// 兼容旧数据：如果 exchange 为空，默认为 binance
		if trade.Exchange == "" {
			trade.Exchange = "binance"
		}
		trades = append(trades, trade)
	}

	return trades, nil
}

// QueryStatistics 查询统计数据
func (s *SQLiteStorage) QueryStatistics(startDate, endDate time.Time) ([]*Statistics, error) {
	rows, err := s.db.Query(`
		SELECT date, total_trades, total_volume, total_pnl, win_rate, created_at
		FROM statistics
		WHERE date >= ? AND date <= ?
		ORDER BY date DESC
	`, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("查询统计数据失败: %w", err)
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

	return stats, nil
}

// GetStatisticsSummary 获取统计汇总（从 trades 表实时计算）
func (s *SQLiteStorage) GetStatisticsSummary() (*Statistics, error) {
	return s.GetStatisticsSummaryByExchange("")
}

// GetStatisticsSummaryByExchange 获取指定交易所的统计汇总
func (s *SQLiteStorage) GetStatisticsSummaryByExchange(exchange string) (*Statistics, error) {
	query := `
		SELECT 
			COUNT(*) as total_trades,
			COALESCE(SUM(quantity), 0) as total_volume,
			COALESCE(SUM(pnl), 0) as total_pnl,
			CASE 
				WHEN COUNT(*) > 0 THEN 
					CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*)
				ELSE 0
			END as win_rate
		FROM trades
	`
	var row *sql.Row
	if exchange != "" {
		query += " WHERE exchange = ?"
		row = s.db.QueryRow(query, exchange)
	} else {
		row = s.db.QueryRow(query)
	}

	stat := &Statistics{}
	var totalTrades sql.NullInt64
	var totalVolume sql.NullFloat64
	var totalPnL sql.NullFloat64
	var winRate sql.NullFloat64

	err := row.Scan(&totalTrades, &totalVolume, &totalPnL, &winRate)
	if err != nil {
		if err == sql.ErrNoRows {
			return &Statistics{}, nil
		}
		return nil, fmt.Errorf("查询统计汇总失败: %w", err)
	}

	if totalTrades.Valid {
		stat.TotalTrades = int(totalTrades.Int64)
	}
	if totalVolume.Valid {
		stat.TotalVolume = totalVolume.Float64
	}
	if totalPnL.Valid {
		stat.TotalPnL = totalPnL.Float64
	}
	if winRate.Valid {
		stat.WinRate = winRate.Float64
	}

	return stat, nil
}

// QueryDailyStatisticsFromTrades 从 trades 表查询每日统计
func (s *SQLiteStorage) QueryDailyStatisticsFromTrades(startDate, endDate time.Time) ([]*DailyStatisticsWithTradeCount, error) {
	return s.QueryDailyStatisticsByExchange("", startDate, endDate)
}

// QueryDailyStatisticsByExchange 从 trades 表查询指定交易所的每日统计
func (s *SQLiteStorage) QueryDailyStatisticsByExchange(exchange string, startDate, endDate time.Time) ([]*DailyStatisticsWithTradeCount, error) {
	// 转换为日期字符串（YYYY-MM-DD格式）
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")

	query := `
		SELECT 
			date(created_at) as date,
			COUNT(*) as total_trades,
			COALESCE(SUM(quantity), 0) as total_volume,
			COALESCE(SUM(pnl), 0) as total_pnl,
			CASE 
				WHEN COUNT(*) > 0 THEN 
					CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*)
				ELSE 0
			END as win_rate,
			SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) as winning_trades,
			SUM(CASE WHEN pnl < 0 THEN 1 ELSE 0 END) as losing_trades
		FROM trades
		WHERE date(created_at) >= ? AND date(created_at) <= ?
	`
	args := []interface{}{startDateStr, endDateStr}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	query += " GROUP BY date(created_at) ORDER BY date DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询每日统计失败: %w", err)
	}
	defer rows.Close()

	var stats []*DailyStatisticsWithTradeCount
	for rows.Next() {
		stat := &DailyStatisticsWithTradeCount{}
		var dateStr string
		var totalTrades sql.NullInt64
		var totalVolume sql.NullFloat64
		var totalPnL sql.NullFloat64
		var winRate sql.NullFloat64
		var winningTrades sql.NullInt64
		var losingTrades sql.NullInt64

		err := rows.Scan(&dateStr, &totalTrades, &totalVolume, &totalPnL, &winRate, &winningTrades, &losingTrades)
		if err != nil {
			continue
		}

		// 解析日期
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		stat.Date = date

		if totalTrades.Valid {
			stat.TotalTrades = int(totalTrades.Int64)
		}
		if totalVolume.Valid {
			stat.TotalVolume = totalVolume.Float64
		}
		if totalPnL.Valid {
			stat.TotalPnL = totalPnL.Float64
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

		stats = append(stats, stat)
	}

	return stats, nil
}

// SaveReconciliationHistory 保存对账历史
func (s *SQLiteStorage) SaveReconciliationHistory(history *ReconciliationHistory) error {
	// 转换为UTC时间存储
	reconcileTime := utils.ToUTC(history.ReconcileTime)
	createdAt := utils.ToUTC(history.CreatedAt)
	_, err := s.db.Exec(`
		INSERT INTO reconciliation_history 
		(symbol, reconcile_time, local_position, exchange_position, position_diff,
		 active_buy_orders, active_sell_orders, pending_sell_qty,
		 total_buy_qty, total_sell_qty, estimated_profit, actual_profit, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, history.Symbol, reconcileTime, history.LocalPosition, history.ExchangePosition,
		history.PositionDiff, history.ActiveBuyOrders, history.ActiveSellOrders,
		history.PendingSellQty, history.TotalBuyQty, history.TotalSellQty, history.EstimatedProfit, history.ActualProfit, createdAt)
	return err
}

// QueryReconciliationHistory 查询对账历史
func (s *SQLiteStorage) QueryReconciliationHistory(symbol string, startTime, endTime time.Time, limit, offset int) ([]*ReconciliationHistory, error) {
	query := `
		SELECT id, symbol, reconcile_time, local_position, exchange_position, position_diff,
		       active_buy_orders, active_sell_orders, pending_sell_qty,
		       total_buy_qty, total_sell_qty, estimated_profit, actual_profit, created_at
		FROM reconciliation_history
		WHERE reconcile_time >= ? AND reconcile_time <= ?
	`
	args := []interface{}{startTime, endTime}

	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}

	query += " ORDER BY reconcile_time DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询对账历史失败: %w", err)
	}
	defer rows.Close()

	var histories []*ReconciliationHistory
	for rows.Next() {
		h := &ReconciliationHistory{}
		err := rows.Scan(
			&h.ID,
			&h.Symbol,
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

	return histories, nil
}

// GetLatestReconciliationHistory 获取指定币种的最新对账记录
func (s *SQLiteStorage) GetLatestReconciliationHistory(symbol string) (*ReconciliationHistory, error) {
	query := `
		SELECT id, symbol, reconcile_time, local_position, exchange_position, position_diff,
		       active_buy_orders, active_sell_orders, pending_sell_qty,
		       total_buy_qty, total_sell_qty, estimated_profit, actual_profit, created_at
		FROM reconciliation_history
		WHERE symbol = ?
		ORDER BY reconcile_time DESC
		LIMIT 1
	`

	row := s.db.QueryRow(query, symbol)
	h := &ReconciliationHistory{}

	err := row.Scan(
		&h.ID,
		&h.Symbol,
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
			return nil, nil // 没有记录，返回 nil 而不是错误
		}
		return nil, fmt.Errorf("查询最新对账记录失败: %w", err)
	}

	return h, nil
}

// GetReconciliationCount 获取指定币种的对账次数（统计历史记录数量）
func (s *SQLiteStorage) GetReconciliationCount(symbol string) (int64, error) {
	query := `SELECT COUNT(*) FROM reconciliation_history WHERE symbol = ?`

	var count int64
	err := s.db.QueryRow(query, symbol).Scan(&count)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil // 没有记录，返回 0
		}
		return 0, fmt.Errorf("统计对账次数失败: %w", err)
	}

	return count, nil
}

// GetPnLBySymbol 按币种对查询盈亏数据
func (s *SQLiteStorage) GetPnLBySymbol(symbol string, startTime, endTime time.Time) (*PnLSummary, error) {
	row := s.db.QueryRow(`
		SELECT 
			COUNT(*) as total_trades,
			SUM(pnl) as total_pnl,
			SUM(quantity) as total_volume,
			SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) as winning_trades,
			SUM(CASE WHEN pnl < 0 THEN 1 ELSE 0 END) as losing_trades
		FROM trades
		WHERE symbol = ? AND created_at >= ? AND created_at <= ?
	`, symbol, startTime, endTime)

	summary := &PnLSummary{
		Symbol: symbol,
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
		return nil, fmt.Errorf("查询盈亏数据失败: %w", err)
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

// GetPnLByTimeRange 按时间区间查询盈亏数据（按币种对分组）
func (s *SQLiteStorage) GetPnLByTimeRange(startTime, endTime time.Time) ([]*PnLBySymbol, error) {
	rows, err := s.db.Query(`
		SELECT 
			exchange,
			symbol,
			COUNT(*) as total_trades,
			SUM(pnl) as total_pnl,
			SUM(quantity) as total_volume,
			CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*) as win_rate
		FROM trades
		WHERE created_at >= ? AND created_at <= ?
		GROUP BY exchange, symbol
		ORDER BY total_pnl DESC
	`, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("查询盈亏数据失败: %w", err)
	}
	defer rows.Close()

	var results []*PnLBySymbol
	for rows.Next() {
		r := &PnLBySymbol{}
		var totalTrades sql.NullInt64
		var totalPnL sql.NullFloat64
		var totalVolume sql.NullFloat64
		var winRate sql.NullFloat64

		err := rows.Scan(&r.Exchange, &r.Symbol, &totalTrades, &totalPnL, &totalVolume, &winRate)
		if err != nil {
			continue
		}

		if totalTrades.Valid {
			r.TotalTrades = int(totalTrades.Int64)
		}
		if totalPnL.Valid {
			r.TotalPnL = totalPnL.Float64
		}
		if totalVolume.Valid {
			r.TotalVolume = totalVolume.Float64
		}
		if winRate.Valid {
			r.WinRate = winRate.Float64
		}

		results = append(results, r)
	}

	return results, nil
}

// GetActualProfitBySymbol 计算指定币种在指定时间之前的累计实际盈利
func (s *SQLiteStorage) GetActualProfitBySymbol(symbol string, beforeTime time.Time) (float64, error) {
	row := s.db.QueryRow(`
		SELECT COALESCE(SUM(pnl), 0) as total_pnl
		FROM trades
		WHERE symbol = ? AND created_at <= ?
	`, symbol, beforeTime)

	var totalPnL sql.NullFloat64
	err := row.Scan(&totalPnL)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("查询实际盈利失败: %w", err)
	}

	if totalPnL.Valid {
		return totalPnL.Float64, nil
	}

	return 0, nil
}

// SaveRiskCheck 保存风控检查记录
func (s *SQLiteStorage) SaveRiskCheck(record *RiskCheckRecord) error {
	// 转换为UTC时间存储
	checkTime := utils.ToUTC(record.CheckTime)
	_, err := s.db.Exec(`
		INSERT INTO risk_check_history 
		(check_time, symbol, is_healthy, price_deviation, volume_ratio, reason)
		VALUES (?, ?, ?, ?, ?, ?)
	`, checkTime, record.Symbol, record.IsHealthy, record.PriceDeviation, record.VolumeRatio, record.Reason)
	return err
}

// QueryRiskCheckHistory 查询风控检查历史
func (s *SQLiteStorage) QueryRiskCheckHistory(startTime, endTime time.Time, limit int) ([]*RiskCheckHistory, error) {
	// 如果 limit <= 0，默认限制为 200 条，防止前端渲染数据过大导致卡顿
	if limit <= 0 {
		limit = 200
	}
	// 上限限制，避免一次性拉取过多数据占用内存/CPU
	if limit > 500 {
		limit = 500
	}

	// 根据时间范围决定聚合粒度
	timeRange := endTime.Sub(startTime)
	var truncateDuration time.Duration
	if timeRange > 30*24*time.Hour {
		// 超过30天，按小时聚合
		truncateDuration = time.Hour
	} else if timeRange > 7*24*time.Hour {
		// 超过7天，按30分钟聚合
		truncateDuration = 30 * time.Minute
	} else if timeRange > 24*time.Hour {
		// 超过1天，按10分钟聚合
		truncateDuration = 10 * time.Minute
	} else {
		// 1天内，按分钟聚合
		truncateDuration = time.Minute
	}

	// 查询数据，按时间倒序，限制数量
	rows, err := s.db.Query(`
		SELECT check_time, symbol, is_healthy, price_deviation, volume_ratio, reason
		FROM risk_check_history
		WHERE check_time >= ? AND check_time <= ?
		ORDER BY check_time DESC
		LIMIT ?
	`, startTime, endTime, limit*4) // 多查询一些，因为后面会聚合，但限制在 4 倍以内防止过大
	if err != nil {
		return nil, fmt.Errorf("查询风控检查历史失败: %w", err)
	}
	defer rows.Close()

	// 按检查时间分组
	historyMap := make(map[time.Time]*RiskCheckHistory)

	for rows.Next() {
		var checkTime time.Time
		var symbol string
		var isHealthy int
		var priceDeviation sql.NullFloat64
		var volumeRatio sql.NullFloat64
		var reason sql.NullString

		err := rows.Scan(&checkTime, &symbol, &isHealthy, &priceDeviation, &volumeRatio, &reason)
		if err != nil {
			continue
		}

		// 根据时间范围聚合时间戳
		checkTimeRounded := checkTime.Truncate(truncateDuration)

		history, exists := historyMap[checkTimeRounded]
		if !exists {
			history = &RiskCheckHistory{
				CheckTime: checkTimeRounded,
				Symbols:   []*RiskCheckSymbol{},
			}
			historyMap[checkTimeRounded] = history
		}

		symbolData := &RiskCheckSymbol{
			Symbol:    symbol,
			IsHealthy: isHealthy == 1,
		}
		if priceDeviation.Valid {
			symbolData.PriceDeviation = priceDeviation.Float64
		}
		if volumeRatio.Valid {
			symbolData.VolumeRatio = volumeRatio.Float64
		}
		if reason.Valid {
			symbolData.Reason = reason.String
		}

		history.Symbols = append(history.Symbols, symbolData)
		if symbolData.IsHealthy {
			history.HealthyCount++
		}
		history.TotalCount++
	}

	// 转换为切片并排序
	result := make([]*RiskCheckHistory, 0, len(historyMap))
	for _, history := range historyMap {
		result = append(result, history)
	}

	// 按时间排序（升序），使用 sort.Slice 替代 O(n^2) 嵌套循环
	sort.Slice(result, func(i, j int) bool {
		return result[i].CheckTime.Before(result[j].CheckTime)
	})

	// 限制返回数量（取最新的 limit 条）
	if len(result) > limit {
		result = result[len(result)-limit:]
	}

	return result, nil
}

// CleanupRiskCheckHistory 清理指定时间之前的风控检查历史
func (s *SQLiteStorage) CleanupRiskCheckHistory(beforeTime time.Time) error {
	_, err := s.db.Exec(`
		DELETE FROM risk_check_history 
		WHERE check_time < ?
	`, beforeTime)
	return err
}

// SaveFundingRate 保存资金费率（仅在变动时存储）
func (s *SQLiteStorage) SaveFundingRate(symbol, exchange string, rate float64, timestamp time.Time) error {
	// 获取该交易对的最新资金费率
	latestRate, err := s.GetLatestFundingRate(symbol, exchange)
	if err == nil {
		// 比较新旧费率（考虑浮点精度误差）
		const epsilon = 0.0000001
		if abs(latestRate-rate) < epsilon {
			// 费率未变化，不存储
			return nil
		}
	}

	// 费率有变化，插入新记录
	_, err = s.db.Exec(`
		INSERT INTO funding_rates (symbol, exchange, rate, timestamp, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, symbol, exchange, rate, timestamp, time.Now())
	return err
}

// GetLatestFundingRate 获取最新的资金费率
func (s *SQLiteStorage) GetLatestFundingRate(symbol, exchange string) (float64, error) {
	var rate float64
	err := s.db.QueryRow(`
		SELECT rate FROM funding_rates
		WHERE symbol = ? AND exchange = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`, symbol, exchange).Scan(&rate)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("未找到资金费率记录")
	}
	return rate, err
}

// GetFundingRateHistory 获取资金费率历史
func (s *SQLiteStorage) GetFundingRateHistory(symbol, exchange string, limit int) ([]*FundingRate, error) {
	query := `
		SELECT id, symbol, exchange, rate, timestamp, created_at
		FROM funding_rates
		WHERE 1=1
	`
	args := []interface{}{}

	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}

	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rates []*FundingRate
	for rows.Next() {
		var fr FundingRate
		err := rows.Scan(&fr.ID, &fr.Symbol, &fr.Exchange, &fr.Rate, &fr.Timestamp, &fr.CreatedAt)
		if err != nil {
			return nil, err
		}
		rates = append(rates, &fr)
	}

	return rates, rows.Err()
}

// abs 计算绝对值（用于浮点数比较）
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// GetAIPromptTemplate 获取AI提示词模板
func (s *SQLiteStorage) GetAIPromptTemplate(module string) (*AIPromptTemplate, error) {
	var template AIPromptTemplate
	err := s.db.QueryRow(
		"SELECT id, module, template, system_prompt, updated_at FROM ai_prompts WHERE module = ?",
		module,
	).Scan(&template.ID, &template.Module, &template.Template, &template.SystemPrompt, &template.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil // 不存在，返回nil
	}
	if err != nil {
		return nil, err
	}
	return &template, nil
}

// SetAIPromptTemplate 设置AI提示词模板
func (s *SQLiteStorage) SetAIPromptTemplate(template *AIPromptTemplate) error {
	_, err := s.db.Exec(
		`INSERT INTO ai_prompts (module, template, system_prompt, updated_at) 
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(module) DO UPDATE SET 
		 template = excluded.template,
		 system_prompt = excluded.system_prompt,
		 updated_at = excluded.updated_at`,
		template.Module, template.Template, template.SystemPrompt, time.Now(),
	)
	return err
}

// GetAllAIPromptTemplates 获取所有AI提示词模板
func (s *SQLiteStorage) GetAllAIPromptTemplates() ([]*AIPromptTemplate, error) {
	rows, err := s.db.Query(
		"SELECT id, module, template, system_prompt, updated_at FROM ai_prompts ORDER BY module",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var templates []*AIPromptTemplate
	for rows.Next() {
		var template AIPromptTemplate
		err := rows.Scan(&template.ID, &template.Module, &template.Template, &template.SystemPrompt, &template.UpdatedAt)
		if err != nil {
			return nil, err
		}
		templates = append(templates, &template)
	}

	return templates, rows.Err()
}

// SaveBasisData 保存价差数据
func (s *SQLiteStorage) SaveBasisData(data *BasisData) error {
	_, err := s.db.Exec(`
		INSERT INTO basis_data (symbol, exchange, spot_price, futures_price, basis, basis_percent, funding_rate, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, data.Symbol, data.Exchange, data.SpotPrice, data.FuturesPrice, data.Basis, data.BasisPercent, data.FundingRate, data.Timestamp)
	return err
}

// GetLatestBasis 获取最新价差数据
func (s *SQLiteStorage) GetLatestBasis(symbol, exchange string) (*BasisData, error) {
	var data BasisData
	err := s.db.QueryRow(`
		SELECT symbol, exchange, spot_price, futures_price, basis, basis_percent, funding_rate, timestamp
		FROM basis_data
		WHERE symbol = ? AND exchange = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`, symbol, exchange).Scan(
		&data.Symbol, &data.Exchange, &data.SpotPrice, &data.FuturesPrice,
		&data.Basis, &data.BasisPercent, &data.FundingRate, &data.Timestamp,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &data, nil
}

// GetBasisHistory 获取价差历史数据
func (s *SQLiteStorage) GetBasisHistory(symbol, exchange string, limit int) ([]*BasisData, error) {
	if limit <= 0 {
		limit = 100
	}

	rows, err := s.db.Query(`
		SELECT symbol, exchange, spot_price, futures_price, basis, basis_percent, funding_rate, timestamp
		FROM basis_data
		WHERE symbol = ? AND exchange = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`, symbol, exchange, limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*BasisData
	for rows.Next() {
		var data BasisData
		err := rows.Scan(
			&data.Symbol, &data.Exchange, &data.SpotPrice, &data.FuturesPrice,
			&data.Basis, &data.BasisPercent, &data.FundingRate, &data.Timestamp,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, &data)
	}

	return result, rows.Err()
}

// GetBasisStatistics 获取价差统计数据
func (s *SQLiteStorage) GetBasisStatistics(symbol, exchange string, hours int) (*BasisStats, error) {
	if hours <= 0 {
		hours = 24
	}

	cutoffTime := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)

	rows, err := s.db.Query(`
		SELECT basis_percent
		FROM basis_data
		WHERE symbol = ? AND exchange = ? AND timestamp >= ?
		ORDER BY timestamp DESC
	`, symbol, exchange, cutoffTime)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var values []float64
	for rows.Next() {
		var value float64
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("没有找到数据")
	}

	// 计算统计数据
	var sum, max, min float64
	max = values[0]
	min = values[0]

	for _, v := range values {
		sum += v
		if v > max {
			max = v
		}
		if v < min {
			min = v
		}
	}

	avg := sum / float64(len(values))

	// 计算标准差
	var variance float64
	for _, v := range values {
		diff := v - avg
		variance += diff * diff
	}
	variance /= float64(len(values))
	stdDev := 0.0
	if variance > 0 {
		// 简化的平方根计算
		stdDev = variance
		for i := 0; i < 10; i++ {
			stdDev = (stdDev + variance/stdDev) / 2
		}
	}

	return &BasisStats{
		Symbol:     symbol,
		Exchange:   exchange,
		AvgBasis:   avg,
		MaxBasis:   max,
		MinBasis:   min,
		StdDev:     stdDev,
		DataPoints: len(values),
		Hours:      hours,
	}, nil
}

// Close 关闭数据库连接
func (s *SQLiteStorage) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}
