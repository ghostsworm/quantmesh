package storage

import (
	"database/sql"
	"fmt"
)

// createTables 創建表
func createTables(db *sql.DB) error {
	// 订單表
	ordersSQL := `
	CREATE TABLE IF NOT EXISTS orders (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		order_id BIGINT,
		bot_id TEXT DEFAULT '',
		account TEXT DEFAULT '',
		client_order_id TEXT,
		symbol TEXT,
		side TEXT,
		exchange TEXT DEFAULT '',
		type TEXT DEFAULT '',
		price DECIMAL(20,8),
		quantity DECIMAL(20,8),
		filled_qty DECIMAL(20,8) DEFAULT 0,
		status TEXT,
		realized_pnl DECIMAL(20,8),
		strategy_name TEXT DEFAULT '',
		strategy_type TEXT DEFAULT '',
		order_source TEXT DEFAULT '',
		created_at TIMESTAMP,
		updated_at TIMESTAMP
	);`

	// 持倉表
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

	// 交易表（買賣配對）
	// 注意：舊庫可能已有 trades 表但無 exchange/account 列，idx_trades_exchange_symbol 改在 migrateTradesTable 中創建
	tradesSQL := `
	CREATE TABLE IF NOT EXISTS trades (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		buy_order_id BIGINT,
		sell_order_id BIGINT,
		bot_id TEXT DEFAULT '',
		exchange TEXT,
		account TEXT,
		symbol TEXT,
		buy_price DECIMAL(20,8),
		sell_price DECIMAL(20,8),
		quantity DECIMAL(20,8),
		pnl DECIMAL(20,8),
		created_at TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_trades_created_at ON trades(created_at);`

	// 事件表
	eventsSQL := `
	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_type TEXT,
		data TEXT,
		created_at TIMESTAMP
	);`

	// 系统監控细粒度數據表
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

	// 系统監控每日彙總數據表
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

	// 對账历史表（不含 account+exchange+symbol 索引：舊庫可能尚無 exchange 列，該索引在 migrateReconciliationHistory 中創建）
	reconciliationHistorySQL := `
	CREATE TABLE IF NOT EXISTS reconciliation_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		exchange TEXT,
		symbol TEXT,
		account TEXT,
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
	// 注意：bot_id / exchange / market_type 相关索引不在此处创建，由 migrateRiskCheckHistoryTable 在添加列后创建，
	// 避免旧表（无 bot_id 列）时 CREATE INDEX ON (bot_id) 报错 "no such column: bot_id"
	riskCheckHistorySQL := `
	CREATE TABLE IF NOT EXISTS risk_check_history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		check_time TIMESTAMP NOT NULL,
		bot_id TEXT DEFAULT '',
		exchange TEXT DEFAULT '',
		market_type TEXT DEFAULT '',
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

	// Bot 開倉風控暫停/恢復事件（持久化，供詳情頁查詢與導出）
	botRiskControlEventsSQL := `
	CREATE TABLE IF NOT EXISTS bot_risk_control_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bot_id TEXT NOT NULL,
		event_type TEXT NOT NULL,
		reason TEXT DEFAULT '',
		source TEXT DEFAULT '',
		created_at TIMESTAMP NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_bot_rce_bot_time ON bot_risk_control_events(bot_id, created_at);`

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

	// 價差數據表
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

	// 盈利自动提取规则表
	withdrawRulesSQL := `
	CREATE TABLE IF NOT EXISTS profit_withdraw_rules (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL,
		exchange_id TEXT NOT NULL,
		strategy_id TEXT NOT NULL DEFAULT '',
		enabled INTEGER NOT NULL DEFAULT 1,
		trigger_amount REAL NOT NULL DEFAULT 0,
		withdraw_ratio REAL NOT NULL DEFAULT 0,
		frequency TEXT NOT NULL DEFAULT 'immediate',
		destination TEXT NOT NULL DEFAULT 'account',
		wallet_address TEXT,
		min_withdraw_amount REAL NOT NULL DEFAULT 0,
		max_withdraw_amount REAL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_profit_withdraw_rules_account ON profit_withdraw_rules(account_id);
	CREATE INDEX IF NOT EXISTS idx_profit_withdraw_rules_account_exchange ON profit_withdraw_rules(account_id, exchange_id);
	CREATE INDEX IF NOT EXISTS idx_profit_withdraw_rules_updated_at ON profit_withdraw_rules(updated_at);`

	// 盈利提取記錄表
	withdrawRecordsSQL := `
	CREATE TABLE IF NOT EXISTS profit_withdraw_records (
		id TEXT PRIMARY KEY,
		rule_id TEXT NOT NULL,
		account_id TEXT NOT NULL,
		exchange_id TEXT NOT NULL,
		strategy_id TEXT DEFAULT '',
		amount REAL NOT NULL,
		fee REAL NOT NULL DEFAULT 0,
		net_amount REAL NOT NULL,
		currency TEXT NOT NULL,
		type TEXT NOT NULL,
		status TEXT NOT NULL,
		destination TEXT NOT NULL,
		transfer_id TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		completed_at TIMESTAMP,
		failed_reason TEXT,
		note TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_withdraw_records_account ON profit_withdraw_records(account_id);
	CREATE INDEX IF NOT EXISTS idx_withdraw_records_created_at ON profit_withdraw_records(created_at);
	CREATE INDEX IF NOT EXISTS idx_withdraw_records_rule_id ON profit_withdraw_records(rule_id);`

	// orders 複合 UNIQUE 索引不可在此創建：舊庫或測試夾具可能尚無 account 等列；由 migrateOrdersTable → ensureOrdersCompositeUniqueConstraint 在列就緒後創建。
	indexesSQL := `
	CREATE INDEX IF NOT EXISTS idx_orders_order_id ON orders(order_id);
	CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at);
	CREATE INDEX IF NOT EXISTS idx_positions_slot_price ON positions(slot_price);
	CREATE INDEX IF NOT EXISTS idx_trades_created_at ON trades(created_at);
	CREATE INDEX IF NOT EXISTS idx_trades_symbol ON trades(symbol);
	CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);
	`

	// 執行創建语句
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
		botRiskControlEventsSQL,
		fundingRatesSQL,
		aiPromptsSQL,
		basisDataSQL,
		withdrawRulesSQL,
		withdrawRecordsSQL,
		indexesSQL,
	}
	for _, sql := range sqls {
		if _, err := db.Exec(sql); err != nil {
			return fmt.Errorf("執行 SQL 失败: %w", err)
		}
	}

	// 迁移：為已存在的表添加 actual_profit 和 account 字段（如果不存在）
	if err := migrateReconciliationHistory(db); err != nil {
		return fmt.Errorf("迁移對账历史表失败: %w", err)
	}

	// 迁移：為 events 表添加 event_type 字段（如果不存在）
	if err := migrateEventsTable(db); err != nil {
		return fmt.Errorf("迁移事件表失败: %w", err)
	}

	// 迁移：确保 profit_withdraw_rules 表存在（舊版本數據库升级）
	if err := migrateProfitWithdrawRulesTable(db); err != nil {
		return fmt.Errorf("迁移 profit_withdraw_rules 表失败: %w", err)
	}

	// 迁移：為 profit_withdraw_rules 添加 last_triggered_at 列
	if err := migrateProfitWithdrawRulesLastTriggered(db); err != nil {
		return fmt.Errorf("迁移 profit_withdraw_rules last_triggered_at 失败: %w", err)
	}

	// 迁移：确保 profit_withdraw_records 表存在
	if err := migrateProfitWithdrawRecordsTable(db); err != nil {
		return fmt.Errorf("迁移 profit_withdraw_records 表失败: %w", err)
	}

	// 迁移：确保 backtest_tasks 表存在
	if err := migrateBacktestTasksTable(db); err != nil {
		return fmt.Errorf("迁移 backtest_tasks 表失败: %w", err)
	}

	// 迁移：确保 optim_tasks 表存在
	if err := migrateOptimTasksTable(db); err != nil {
		return fmt.Errorf("迁移 optim_tasks 表失败: %w", err)
	}

	// 迁移：确保 news_analysis_history 表存在
	if err := migrateNewsAnalysisHistoryTable(db); err != nil {
		return fmt.Errorf("迁移 news_analysis_history 表失败: %w", err)
	}
	// 迁移：Gemini 用量持久化
	if err := migrateGeminiUsageTable(db); err != nil {
		return fmt.Errorf("迁移 gemini_usage 表失败: %w", err)
	}
	// 迁移：确保 price_history 和 prediction_verification 表存在
	if err := migratePriceHistoryTable(db); err != nil {
		return fmt.Errorf("迁移 price_history 表失败: %w", err)
	}
	if err := migratePredictionVerificationTable(db); err != nil {
		return fmt.Errorf("迁移 prediction_verification 表失败: %w", err)
	}
	if err := migrateHourlyEquityAndDailySnapshotTables(db); err != nil {
		return fmt.Errorf("迁移 hourly_equity / daily_snapshot 表失败: %w", err)
	}
	if err := migrateAccountEquitySnapshotColumns(db); err != nil {
		return fmt.Errorf("迁移 account_equity 列失败: %w", err)
	}
	if err := migrateInspectionReportsTable(db); err != nil {
		return fmt.Errorf("迁移 inspection_reports 表失败: %w", err)
	}
	if err := migrateFundingPaymentsTable(db); err != nil {
		return fmt.Errorf("迁移 funding_payments 表失败: %w", err)
	}
	if err := migrateProtectedKlineFilesTable(db); err != nil {
		return fmt.Errorf("迁移 protected_kline_files 表失败: %w", err)
	}
	if err := migrateKlineFilesTable(db); err != nil {
		return fmt.Errorf("迁移 kline_files 表失败: %w", err)
	}
	if err := migrateMarketInterpretTable(db); err != nil {
		return fmt.Errorf("迁移 market_interpret_tasks 表失败: %w", err)
	}
	if err := migrateBotStatesTable(db); err != nil {
		return fmt.Errorf("迁移 bot_states 表失败: %w", err)
	}
	if err := migrateFixTables(db); err != nil {
		return fmt.Errorf("迁移 fix tables 失败: %w", err)
	}

	return nil
}
