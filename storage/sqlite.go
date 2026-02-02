package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteStorage SQLite 存儲實現
type SQLiteStorage struct {
	db     *sql.DB
	closed bool
}

// NewSQLiteStorage 創建 SQLite 存儲
func NewSQLiteStorage(path string) (*SQLiteStorage, error) {
	// 使用 WAL 模式提高並发性能
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("打开數據库失败: %w", err)
	}

	// 設置连接池
	db.SetMaxOpenConns(1) // SQLite 並发限制
	db.SetMaxIdleConns(1)

	// 創建表
	if err := createTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("創建表失败: %w", err)
	}

	// 迁移：添加 exchange 字段（如果不存在）
	if err := migrateTradesTable(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("迁移 trades 表失败: %w", err)
	}

	return &SQLiteStorage{db: db}, nil
}

// createTables 創建表
func createTables(db *sql.DB) error {
	// 订單表
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
	// 注意：account 列在舊版本中可能不存在，索引創建移到迁移后執行
	tradesSQL := `
	CREATE TABLE IF NOT EXISTS trades (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		buy_order_id BIGINT,
		sell_order_id BIGINT,
		exchange TEXT,
		account TEXT,
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

	// 對账历史表
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
	CREATE INDEX IF NOT EXISTS idx_reconciliation_history_account_exchange_symbol ON reconciliation_history(account, exchange, symbol);
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

	// 創建索引
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

	// 迁移：确保 news_analysis_history 表存在
	if err := migrateNewsAnalysisHistoryTable(db); err != nil {
		return fmt.Errorf("迁移 news_analysis_history 表失败: %w", err)
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
	if err := migrateInspectionReportsTable(db); err != nil {
		return fmt.Errorf("迁移 inspection_reports 表失败: %w", err)
	}
	if err := migrateFundingPaymentsTable(db); err != nil {
		return fmt.Errorf("迁移 funding_payments 表失败: %w", err)
	}

	return nil
}

// migrateInspectionReportsTable 遷移智子巡檢報告表
func migrateInspectionReportsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS inspection_reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			report_type TEXT NOT NULL,
			title TEXT NOT NULL,
			body TEXT NOT NULL,
			snapshot_json TEXT,
			analysis_json TEXT,
			event_type TEXT,
			event_data_json TEXT,
			generated_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_inspection_reports_generated_at ON inspection_reports(generated_at);
	`)
	return err
}

// migrateFundingPaymentsTable 遷移資金費用記錄表
func migrateFundingPaymentsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS funding_payments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			account TEXT,
			income_type TEXT NOT NULL,
			income DECIMAL(20,8) NOT NULL,
			asset TEXT,
			info TEXT,
			transaction_id BIGINT,
			trade_time TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_funding_payments_exchange_symbol ON funding_payments(exchange, symbol);
		CREATE INDEX IF NOT EXISTS idx_funding_payments_trade_time ON funding_payments(trade_time);
		CREATE INDEX IF NOT EXISTS idx_funding_payments_account ON funding_payments(account);
	`)
	return err
}

// migrateHourlyEquityAndDailySnapshotTables 遷移小時權益與每日快照表
func migrateHourlyEquityAndDailySnapshotTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS hourly_equity_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			account TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			equity REAL NOT NULL,
			unrealized_pnl REAL NOT NULL,
			total_position_value REAL NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_hourly_equity_exchange_symbol_account ON hourly_equity_records(exchange, symbol, account);
		CREATE INDEX IF NOT EXISTS idx_hourly_equity_timestamp ON hourly_equity_records(timestamp);
		CREATE TABLE IF NOT EXISTS daily_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			account TEXT NOT NULL,
			date DATE NOT NULL,
			unrealized_pnl REAL NOT NULL,
			total_position_value REAL NOT NULL,
			intraday_max_drawdown REAL NOT NULL,
			intraday_max_drawdown_pct REAL NOT NULL,
			intraday_peak_equity REAL NOT NULL,
			closing_price REAL NOT NULL,
			snapshot_time TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(exchange, symbol, account, date)
		);
		CREATE INDEX IF NOT EXISTS idx_daily_snapshots_exchange_symbol_account ON daily_snapshots(exchange, symbol, account);
		CREATE INDEX IF NOT EXISTS idx_daily_snapshots_date ON daily_snapshots(date);
	`)
	return err
}

// migrateBacktestTasksTable 迁移 backtest_tasks 表（回测任務）
func migrateBacktestTasksTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS backtest_tasks (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			strategy TEXT NOT NULL,
			symbol TEXT NOT NULL,
			interval TEXT NOT NULL,
			start_time INTEGER NOT NULL,
			end_time INTEGER NOT NULL,
			params TEXT NOT NULL,
			total_capital REAL NOT NULL,
			progress INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			started_at INTEGER,
			completed_at INTEGER,
			error TEXT,
			result_path TEXT,
			report_path TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_backtest_tasks_created_at ON backtest_tasks(created_at);
	`)
	return err
}

// migrateNewsAnalysisHistoryTable 迁移 news_analysis_history 表
func migrateNewsAnalysisHistoryTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS news_analysis_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			analysis_time TIMESTAMP NOT NULL,
			symbol TEXT NOT NULL,
			current_price REAL NOT NULL,
			assessment TEXT,
			recent_news_summary TEXT,
			gemini_prompt TEXT,
			gemini_response TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_news_analysis_history_analysis_time ON news_analysis_history(analysis_time);
		CREATE INDEX IF NOT EXISTS idx_news_analysis_history_symbol ON news_analysis_history(symbol);
	`)
	return err
}

// migratePriceHistoryTable 迁移 price_history 表
func migratePriceHistoryTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS price_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			asset_type TEXT NOT NULL,
			symbol TEXT NOT NULL,
			price REAL NOT NULL,
			source TEXT,
			recorded_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_price_history_lookup ON price_history(asset_type, symbol, recorded_at);
	`)
	return err
}

// migratePredictionVerificationTable 迁移 prediction_verification 表
func migratePredictionVerificationTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS prediction_verification (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			analysis_id INTEGER NOT NULL,
			asset_type TEXT NOT NULL,
			symbol TEXT NOT NULL,
			prediction_time TIMESTAMP NOT NULL,
			timeframe TEXT NOT NULL,
			predicted_direction TEXT NOT NULL,
			predicted_change_pct REAL,
			predicted_probability REAL,
			actual_price_at_prediction REAL,
			actual_price_at_verify REAL,
			actual_direction TEXT,
			actual_change_pct REAL,
			is_correct INTEGER,
			verified_at TIMESTAMP,
			status TEXT DEFAULT 'pending'
		);
		CREATE INDEX IF NOT EXISTS idx_pred_verif_status ON prediction_verification(status);
		CREATE INDEX IF NOT EXISTS idx_pred_verif_asset_symbol ON prediction_verification(asset_type, symbol);
	`)
	return err
}

// migrateProfitWithdrawRulesTable 迁移 profit_withdraw_rules 表（确保表存在）
func migrateProfitWithdrawRulesTable(db *sql.DB) error {
	// 如果表已存在，直接返回
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='profit_withdraw_rules'`).Scan(&name)
	if err == nil && name == "profit_withdraw_rules" {
		return nil
	}
	// err 可能是 sql.ErrNoRows；统一走創建逻辑即可
	_, err = db.Exec(`
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
		CREATE INDEX IF NOT EXISTS idx_profit_withdraw_rules_updated_at ON profit_withdraw_rules(updated_at);
	`)
	return err
}

// migrateProfitWithdrawRulesLastTriggered 為 profit_withdraw_rules 添加 last_triggered_at 列
func migrateProfitWithdrawRulesLastTriggered(db *sql.DB) error {
	row := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('profit_withdraw_rules') WHERE name='last_triggered_at'`)
	var count int
	if err := row.Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		_, err := db.Exec(`ALTER TABLE profit_withdraw_rules ADD COLUMN last_triggered_at TIMESTAMP`)
		return err
	}
	return nil
}

// migrateProfitWithdrawRecordsTable 确保 profit_withdraw_records 表存在
func migrateProfitWithdrawRecordsTable(db *sql.DB) error {
	_, err := db.Exec(`
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
		CREATE INDEX IF NOT EXISTS idx_withdraw_records_rule_id ON profit_withdraw_records(rule_id);
	`)
	return err
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
		logger.Info("🔄 [數據库] 為 events 表添加 event_type 列...")
		_, err := db.Exec(`ALTER TABLE events ADD COLUMN event_type TEXT`)
		if err != nil {
			return err
		}
	}
	return nil
}

// migrateReconciliationHistory 迁移對账历史表，添加 actual_profit、account 和 created_at 字段（如果不存在）
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

	// 检查 account 字段是否存在
	row = db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('reconciliation_history') 
		WHERE name='account'
	`)
	if err := row.Scan(&count); err != nil {
		return err
	}

	// 如果字段不存在，添加它
	if count == 0 {
		_, err := db.Exec(`
			ALTER TABLE reconciliation_history 
			ADD COLUMN account TEXT
		`)
		if err != nil {
			return err
		}
		// 為現有數據創建索引
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_reconciliation_history_account_symbol ON reconciliation_history(account, symbol)`)
		if err != nil {
			logger.Warn("⚠️ 創建 reconciliation_history account 索引失败: %v", err)
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

	// 检查 exchange 字段是否存在
	row = db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('reconciliation_history') 
		WHERE name='exchange'
	`)
	if err := row.Scan(&count); err != nil {
		return err
	}

	// 如果字段不存在，添加它
	if count == 0 {
		_, err := db.Exec(`
			ALTER TABLE reconciliation_history 
			ADD COLUMN exchange TEXT
		`)
		if err != nil {
			return err
		}
	}

	// 無論是否是新增列，都确保索引存在（老库可能已有列但缺索引）
	// 舊索引：兼容历史版本
	_, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_reconciliation_history_account_symbol ON reconciliation_history(account, symbol)`)
	if err != nil {
		logger.Warn("⚠️ 确保 reconciliation_history account+symbol 索引失败: %v", err)
	}
	// 新索引：支援按 exchange 维度過滤
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_reconciliation_history_account_exchange_symbol ON reconciliation_history(account, exchange, symbol)`)
	if err != nil {
		logger.Warn("⚠️ 确保 reconciliation_history account+exchange+symbol 索引失败: %v", err)
	}

	return nil
}

// migrateTradesTable 迁移 trades 表，添加 exchange 和 account 字段
func migrateTradesTable(db *sql.DB) error {
	logger.Info("🔧 开始检查 trades 表結構...")

	// 检查 exchange 列是否存在
	rows, err := db.Query(`PRAGMA table_info(trades)`)
	if err != nil {
		return fmt.Errorf("检查表結構失败: %w", err)
	}
	defer rows.Close()

	hasExchangeColumn := false
	hasAccountColumn := false
	hasFeeColumn := false
	hasFeeAssetColumn := false
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
		}
		if name == "account" {
			hasAccountColumn = true
		}
		if name == "fee" {
			hasFeeColumn = true
		}
		if name == "fee_asset" {
			hasFeeAssetColumn = true
		}
	}

	if !hasExchangeColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 exchange 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN exchange TEXT`)
		if err != nil {
			return fmt.Errorf("添加 exchange 列失败: %w", err)
		}
		logger.Info("✅ exchange 列添加成功")

		// 更新現有數據
		_, err = db.Exec(`UPDATE trades SET exchange = 'binance' WHERE exchange IS NULL`)
		if err != nil {
			logger.Warn("⚠️ 更新現有 exchange 數據失败: %v", err)
		}
	}

	if !hasAccountColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 account 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN account TEXT`)
		if err != nil {
			return fmt.Errorf("添加 account 列失败: %w", err)
		}
		logger.Info("✅ account 列添加成功")

		// 為現有數據創建索引
		_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_account_symbol ON trades(account, symbol)`)
		if err != nil {
			logger.Warn("⚠️ 創建 account 索引失败: %v", err)
		}
	}

	// 检查 fee 列是否存在（手續費支持）
	if !hasFeeColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 fee 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN fee DECIMAL(20,8) DEFAULT 0`)
		if err != nil {
			return fmt.Errorf("添加 fee 列失败: %w", err)
		}
		logger.Info("✅ fee 列添加成功")
	}
	if !hasFeeAssetColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 fee_asset 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN fee_asset TEXT DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("添加 fee_asset 列失败: %w", err)
		}
		logger.Info("✅ fee_asset 列添加成功")
	}

	// 無論是否是新增列，都确保索引存在（老库可能已有列但缺索引）
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_account_symbol ON trades(account, symbol)`)
	if err != nil {
		logger.Warn("⚠️ 确保 trades account 索引失败: %v", err)
	}

	logger.Info("✅ trades 表迁移检查完成")
	return nil
}

// SaveOrder 保存订單
func (s *SQLiteStorage) SaveOrder(order *Order) error {
	// 轉换為UTC時间存儲
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

// SavePosition 保存持倉
func (s *SQLiteStorage) SavePosition(position *Position) error {
	// 轉换為UTC時间存儲
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
	// 轉换為UTC時间存儲
	createdAt := utils.ToUTC(trade.CreatedAt)
	// 确保 exchange 不為空，默认為 binance（兼容舊數據）
	exchange := trade.Exchange
	if exchange == "" {
		exchange = "binance"
	}
	_, err := s.db.Exec(`
		INSERT INTO trades 
		(buy_order_id, sell_order_id, exchange, account, symbol, buy_price, sell_price, quantity, pnl, fee, fee_asset, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, trade.BuyOrderID, trade.SellOrderID, exchange, trade.Account, trade.Symbol,
		trade.BuyPrice, trade.SellPrice, trade.Quantity, trade.PnL, trade.Fee, trade.FeeAsset, createdAt)
	if err != nil {
		return err
	}
	// 合规审计：記錄成交事件
	if globalAuditLogger != nil {
		globalAuditLogger.LogTrade(trade)
	}
	return nil
}

// SaveSystemMetrics 保存系统監控细粒度數據
func (s *SQLiteStorage) SaveSystemMetrics(metrics *SystemMetrics) error {
	// 轉换為UTC時间存儲
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

// SaveDailySystemMetrics 保存系统監控每日彙總數據
func (s *SQLiteStorage) SaveDailySystemMetrics(metrics *DailySystemMetrics) error {
	// 轉换為UTC時间存儲
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
	// 检查是否是系统監控事件
	if eventType == "system_metrics" {
		return s.saveSystemMetricsFromMap(data)
	}

	// 將 data 序列化為 JSON
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化事件數據失败: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO events (event_type, data, created_at)
		VALUES (?, ?, ?)
	`, eventType, string(jsonData), utils.NowUTC())
	return err
}

// saveSystemMetricsFromMap 從 map 保存系统監控數據
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
	// 轉换為UTC時间存儲
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

// QueryOrders 查詢訂單
func (s *SQLiteStorage) QueryOrders(limit, offset int, status string) ([]*Order, error) {
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
		return nil, fmt.Errorf("查詢訂單失败: %w", err)
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

// QueryPositions 查詢持倉历史
func (s *SQLiteStorage) QueryPositions(limit, offset int) ([]*Position, error) {
	maxLimit := 10000
	if limit <= 0 {
		limit = 100
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 持倉查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	rows, err := s.db.Query(`
		SELECT slot_price, symbol, size, entry_price, current_price, pnl, opened_at, closed_at
		FROM positions
		ORDER BY opened_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("查詢持倉失败: %w", err)
	}
	defer rows.Close()

	var positions []*Position
	for rows.Next() {
		p := &Position{}
		var closedAt interface{}
		err := rows.Scan(
			&p.SlotPrice,
			&p.Symbol,
			&p.Size,
			&p.EntryPrice,
			&p.CurrentPrice,
			&p.PnL,
			&p.OpenedAt,
			&closedAt,
		)
		if err != nil {
			continue
		}
		if closedAt != nil {
			if t, ok := closedAt.(time.Time); ok {
				p.ClosedAt = &t
			}
		}
		positions = append(positions, p)
	}

	return positions, rows.Err()
}

// QueryTrades 查詢交易
func (s *SQLiteStorage) QueryTrades(startTime, endTime time.Time, limit, offset int) ([]*Trade, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条交易
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 交易查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	rows, err := s.db.Query(`
		SELECT buy_order_id, sell_order_id, exchange, account, symbol, buy_price, sell_price, quantity, pnl, created_at
		FROM trades
		WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, startTime, endTime, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("查詢交易失败: %w", err)
	}
	defer rows.Close()

	var trades []*Trade
	for rows.Next() {
		trade := &Trade{}
		err := rows.Scan(
			&trade.BuyOrderID,
			&trade.SellOrderID,
			&trade.Exchange,
			&trade.Account,
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
		// 兼容舊數據：如果 exchange 為空，默认為 binance
		if trade.Exchange == "" {
			trade.Exchange = "binance"
		}
		trades = append(trades, trade)
	}

	return trades, nil
}

// QueryStatistics 查詢统计數據
func (s *SQLiteStorage) QueryStatistics(startDate, endDate time.Time) ([]*Statistics, error) {
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

	return stats, nil
}

// GetStatisticsSummary 獲取统计彙總（從 trades 表實時计算）
func (s *SQLiteStorage) GetStatisticsSummary(account string) (*Statistics, error) {
	return s.GetStatisticsSummaryByExchange("", account)
}

// GetStatisticsSummaryByExchange 獲取指定交易所的统计彙總
func (s *SQLiteStorage) GetStatisticsSummaryByExchange(exchange, account string) (*Statistics, error) {
	query := `
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
			END as win_rate
		FROM trades
		WHERE 1=1
	`
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

	err := row.Scan(&totalTrades, &totalVolume, &grossPnL, &totalFee, &netPnL, &winRate)
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

	return stat, nil
}

// QueryDailyStatisticsFromTrades 從 trades 表查詢每日统计
func (s *SQLiteStorage) QueryDailyStatisticsFromTrades(account string, startDate, endDate time.Time) ([]*DailyStatisticsWithTradeCount, error) {
	return s.QueryDailyStatisticsByExchange("", account, startDate, endDate)
}

// QueryDailyStatisticsByExchange 從 trades 表查詢指定交易所的每日统计
func (s *SQLiteStorage) QueryDailyStatisticsByExchange(exchange, account string, startDate, endDate time.Time) ([]*DailyStatisticsWithTradeCount, error) {
	// 限制最大返回數量，防止記憶體占用過大（分组后的結果通常不會太多，但还是要限制）
	maxLimit := 3650 // 最多返回10年的每日统计（3650天）

	// 轉换為日期字符串（YYYY-MM-DD格式）
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")

	query := `
		SELECT 
			date(created_at) as date,
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
		FROM trades
		WHERE date(created_at) >= ? AND date(created_at) <= ?
	`
	args := []interface{}{startDateStr, endDateStr}
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
	query += " GROUP BY date(created_at) ORDER BY date DESC LIMIT ?"
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

	return stats, nil
}

// SaveReconciliationHistory 保存對账历史
func (s *SQLiteStorage) SaveReconciliationHistory(history *ReconciliationHistory) error {
	// 轉换為UTC時间存儲
	reconcileTime := utils.ToUTC(history.ReconcileTime)
	createdAt := utils.ToUTC(history.CreatedAt)
	_, err := s.db.Exec(`
		INSERT INTO reconciliation_history 
		(exchange, symbol, account, reconcile_time, local_position, exchange_position, position_diff,
		 active_buy_orders, active_sell_orders, pending_sell_qty,
		 total_buy_qty, total_sell_qty, estimated_profit, actual_profit, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, history.Exchange, history.Symbol, history.Account, reconcileTime, history.LocalPosition, history.ExchangePosition,
		history.PositionDiff, history.ActiveBuyOrders, history.ActiveSellOrders,
		history.PendingSellQty, history.TotalBuyQty, history.TotalSellQty, history.EstimatedProfit, history.ActualProfit, createdAt)
	return err
}

// QueryReconciliationHistory 查詢對账历史
func (s *SQLiteStorage) QueryReconciliationHistory(exchange, symbol, account string, startTime, endTime time.Time, limit, offset int) ([]*ReconciliationHistory, error) {
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

	return histories, nil
}

// GetLatestReconciliationHistory 獲取指定币种的最新對账記錄
func (s *SQLiteStorage) GetLatestReconciliationHistory(exchange, symbol, account string) (*ReconciliationHistory, error) {
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
func (s *SQLiteStorage) GetReconciliationCount(exchange, symbol, account string) (int64, error) {
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

// GetPnLBySymbol 按币种對查詢盈亏數據（TotalPnL 為淨利潤，已扣手續費）
func (s *SQLiteStorage) GetPnLBySymbol(symbol, account string, startTime, endTime time.Time) (*PnLSummary, error) {
	query := `
		SELECT 
			COUNT(*) as total_trades,
			COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) as total_pnl,
			SUM(quantity) as total_volume,
			SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) as winning_trades,
			SUM(CASE WHEN pnl < 0 THEN 1 ELSE 0 END) as losing_trades
		FROM trades
		WHERE symbol = ? AND created_at >= ? AND created_at <= ?
		`
	args := []interface{}{symbol, startTime, endTime}
	if account != "" {
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}

	row := s.db.QueryRow(query, args...)

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
func (s *SQLiteStorage) GetPnLByTimeRange(account string, startTime, endTime time.Time) ([]*PnLBySymbol, error) {
	// 限制最大返回數量，防止記憶體占用過大（分组后的結果通常不會太多，但还是要限制）
	maxLimit := 1000 // 最多返回1000個币种對
	query := `
		SELECT 
			exchange,
			symbol,
			COUNT(*) as total_trades,
			COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) as total_pnl,
			SUM(quantity) as total_volume,
			CAST(SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END) AS FLOAT) / COUNT(*) as win_rate
		FROM trades
		WHERE created_at >= ? AND created_at <= ?
		`
	args := []interface{}{startTime, endTime}
	if account != "" {
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
	query += " GROUP BY exchange, symbol ORDER BY total_pnl DESC LIMIT ?"
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

// GetActualProfitBySymbol 计算指定币种在指定時间之前的累计實際盈利（淨利潤，已扣手續費）
func (s *SQLiteStorage) GetActualProfitBySymbol(symbol, account string, beforeTime time.Time) (float64, error) {
	query := `
		SELECT COALESCE(SUM(pnl), 0) - COALESCE(SUM(COALESCE(fee, 0)), 0) as total_pnl
		FROM trades
		WHERE symbol = ? AND created_at <= ?
		`
	args := []interface{}{symbol, beforeTime}
	if account != "" {
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
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

// GetTotalBuySellQty 獲取累计買入和累计賣出數量（從trades表计算）
func (s *SQLiteStorage) GetTotalBuySellQty(symbol, account string) (totalBuyQty, totalSellQty float64, err error) {
	query := `
		SELECT 
			COALESCE(SUM(quantity), 0) as total_qty
		FROM trades
		WHERE symbol = ?
	`
	args := []interface{}{symbol}
	if account != "" {
		// 兼容舊數據：如果account不為空，同時匹配account字段為NULL或空字符串的記錄
		// 这样可以确保即使舊數據的account字段為空，也能查詢到累计買賣數量
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
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

// SaveRiskCheck 保存风控检查記錄
func (s *SQLiteStorage) SaveRiskCheck(record *RiskCheckRecord) error {
	// 轉换為UTC時间存儲
	checkTime := utils.ToUTC(record.CheckTime)
	_, err := s.db.Exec(`
		INSERT INTO risk_check_history 
		(check_time, symbol, is_healthy, price_deviation, volume_ratio, reason)
		VALUES (?, ?, ?, ?, ?, ?)
	`, checkTime, record.Symbol, record.IsHealthy, record.PriceDeviation, record.VolumeRatio, record.Reason)
	return err
}

// QueryRiskCheckHistory 查詢风控检查历史
func (s *SQLiteStorage) QueryRiskCheckHistory(startTime, endTime time.Time, limit int) ([]*RiskCheckHistory, error) {
	// 如果 limit <= 0，默认限制為 200 条，防止前端渲染數據過大導致卡顿
	if limit <= 0 {
		limit = 200
	}
	// 上限限制，避免一次性拉取過多數據占用記憶體/CPU
	if limit > 500 {
		limit = 500
	}

	// 根據時间範圍决定聚合粒度
	timeRange := endTime.Sub(startTime)
	var truncateDuration time.Duration
	if timeRange > 30*24*time.Hour {
		// 超過30天，按小時聚合
		truncateDuration = time.Hour
	} else if timeRange > 7*24*time.Hour {
		// 超過7天，按30分钟聚合
		truncateDuration = 30 * time.Minute
	} else if timeRange > 24*time.Hour {
		// 超過1天，按10分钟聚合
		truncateDuration = 10 * time.Minute
	} else {
		// 1天内，按分钟聚合
		truncateDuration = time.Minute
	}

	// 查詢數據，按時间倒序，限制數量
	rows, err := s.db.Query(`
		SELECT check_time, symbol, is_healthy, price_deviation, volume_ratio, reason
		FROM risk_check_history
		WHERE check_time >= ? AND check_time <= ?
		ORDER BY check_time DESC
		LIMIT ?
	`, startTime, endTime, limit*4) // 多查詢一些，因為后面會聚合，但限制在 4 倍以内防止過大
	if err != nil {
		return nil, fmt.Errorf("查詢风控检查历史失败: %w", err)
	}
	defer rows.Close()

	// 按检查時间分组
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

		// 根據時间範圍聚合時间戳
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

	// 轉换為切片並排序
	result := make([]*RiskCheckHistory, 0, len(historyMap))
	for _, history := range historyMap {
		result = append(result, history)
	}

	// 按時间排序（升序），使用 sort.Slice 替代 O(n^2) 嵌套循环
	sort.Slice(result, func(i, j int) bool {
		return result[i].CheckTime.Before(result[j].CheckTime)
	})

	// 限制返回數量（取最新的 limit 条）
	if len(result) > limit {
		result = result[len(result)-limit:]
	}

	return result, nil
}

// CleanupRiskCheckHistory 清理指定時间之前的风控检查历史
func (s *SQLiteStorage) CleanupRiskCheckHistory(beforeTime time.Time) error {
	_, err := s.db.Exec(`
		DELETE FROM risk_check_history 
		WHERE check_time < ?
	`, beforeTime)
	return err
}

// SaveFundingRate 保存资金费率（僅在变动時存儲）
func (s *SQLiteStorage) SaveFundingRate(symbol, exchange string, rate float64, timestamp time.Time) error {
	// 獲取該交易對的最新资金费率
	latestRate, err := s.GetLatestFundingRate(symbol, exchange)
	if err == nil {
		// 比较新舊费率（考虑浮点精度误差）
		const epsilon = 0.0000001
		if abs(latestRate-rate) < epsilon {
			// 费率未变化，不存儲
			return nil
		}
	}

	// 费率有变化，插入新記錄
	_, err = s.db.Exec(`
		INSERT INTO funding_rates (symbol, exchange, rate, timestamp, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, symbol, exchange, rate, timestamp, time.Now())
	return err
}

// GetLatestFundingRate 獲取最新的资金费率
func (s *SQLiteStorage) GetLatestFundingRate(symbol, exchange string) (float64, error) {
	var rate float64
	err := s.db.QueryRow(`
		SELECT rate FROM funding_rates
		WHERE symbol = ? AND exchange = ?
		ORDER BY timestamp DESC
		LIMIT 1
	`, symbol, exchange).Scan(&rate)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("未找到资金费率記錄")
	}
	return rate, err
}

// GetFundingRateHistory 獲取资金费率历史
func (s *SQLiteStorage) GetFundingRateHistory(symbol, exchange string, limit int) ([]*FundingRate, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条资金费率記錄
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 资金费率历史查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

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

// SaveFundingPayment 保存資金費用記錄
func (s *SQLiteStorage) SaveFundingPayment(payment *FundingPayment) error {
	tradeTime := utils.ToUTC(payment.TradeTime)
	_, err := s.db.Exec(`
		INSERT INTO funding_payments (exchange, symbol, account, income_type, income, asset, info, transaction_id, trade_time, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, payment.Exchange, payment.Symbol, payment.Account, payment.IncomeType, payment.Income, payment.Asset, payment.Info, payment.TransactionID, tradeTime, time.Now().UTC())
	return err
}

// GetFundingPayments 獲取資金費用記錄（按時間區間）
func (s *SQLiteStorage) GetFundingPayments(account, exchange string, startTime, endTime time.Time) ([]*FundingPayment, error) {
	startUTC := utils.ToUTC(startTime)
	endUTC := utils.ToUTC(endTime)
	query := `
		SELECT id, exchange, symbol, account, income_type, income, asset, info, transaction_id, trade_time, created_at
		FROM funding_payments
		WHERE trade_time >= ? AND trade_time <= ?
	`
	args := []interface{}{startUTC, endUTC}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}
	query += " ORDER BY trade_time DESC LIMIT 10000"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*FundingPayment
	for rows.Next() {
		var p FundingPayment
		var tradeTime, createdAt time.Time
		err := rows.Scan(&p.ID, &p.Exchange, &p.Symbol, &p.Account, &p.IncomeType, &p.Income, &p.Asset, &p.Info, &p.TransactionID, &tradeTime, &createdAt)
		if err != nil {
			return nil, err
		}
		p.TradeTime = tradeTime
		p.CreatedAt = createdAt
		list = append(list, &p)
	}
	return list, rows.Err()
}

// GetFundingPaymentsSum 獲取資金費用淨額（收入 - 支出，正數表示淨收入）
func (s *SQLiteStorage) GetFundingPaymentsSum(account, exchange string, startTime, endTime time.Time) (float64, error) {
	startUTC := utils.ToUTC(startTime)
	endUTC := utils.ToUTC(endTime)
	query := `
		SELECT COALESCE(SUM(income), 0) FROM funding_payments
		WHERE trade_time >= ? AND trade_time <= ?
	`
	args := []interface{}{startUTC, endUTC}
	if exchange != "" {
		query += " AND exchange = ?"
		args = append(args, exchange)
	}
	if account != "" {
		query += " AND (account = ? OR account IS NULL OR account = '')"
		args = append(args, account)
	}

	var sum sql.NullFloat64
	err := s.db.QueryRow(query, args...).Scan(&sum)
	if err != nil {
		return 0, err
	}
	if sum.Valid {
		return sum.Float64, nil
	}
	return 0, nil
}

// abs 计算绝對值（用於浮点數比较）
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// GetAIPromptTemplate 獲取AI提示词模板
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

// SetAIPromptTemplate 設置AI提示词模板
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

// GetAllAIPromptTemplates 獲取所有AI提示词模板
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

// SaveBasisData 保存價差數據
func (s *SQLiteStorage) SaveBasisData(data *BasisData) error {
	_, err := s.db.Exec(`
		INSERT INTO basis_data (symbol, exchange, spot_price, futures_price, basis, basis_percent, funding_rate, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, data.Symbol, data.Exchange, data.SpotPrice, data.FuturesPrice, data.Basis, data.BasisPercent, data.FundingRate, data.Timestamp)
	return err
}

// GetLatestBasis 獲取最新價差數據
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

// GetBasisHistory 獲取價差历史數據
func (s *SQLiteStorage) GetBasisHistory(symbol, exchange string, limit int) ([]*BasisData, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条價差記錄
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 價差历史查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
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

// GetBasisStatistics 獲取價差统计數據
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
		return nil, fmt.Errorf("没有找到數據")
	}

	// 计算统计數據
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

	// 计算標准差
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

// SaveNewsAnalysisHistory 保存新聞分析历史（成功后填充 history.ID）
func (s *SQLiteStorage) SaveNewsAnalysisHistory(history *NewsAnalysisHistory) error {
	result, err := s.db.Exec(`
		INSERT INTO news_analysis_history (analysis_time, symbol, current_price, assessment, recent_news_summary, gemini_prompt, gemini_response, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, history.AnalysisTime, history.Symbol, history.CurrentPrice, history.Assessment, history.RecentNewsSummary, history.GeminiPrompt, history.GeminiResponse, history.CreatedAt)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err == nil {
		history.ID = id
	}
	return nil
}

// SaveInspectionReport 保存智子巡檢報告
func (s *SQLiteStorage) SaveInspectionReport(report *InspectionReport) error {
	if report == nil {
		return nil
	}
	if report.GeneratedAt.IsZero() {
		report.GeneratedAt = time.Now()
	}
	result, err := s.db.Exec(`
		INSERT INTO inspection_reports (report_type, title, body, snapshot_json, analysis_json, event_type, event_data_json, generated_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, report.ReportType, report.Title, report.Body, report.SnapshotJSON, report.AnalysisJSON, report.EventType, report.EventDataJSON, report.GeneratedAt, report.CreatedAt)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err == nil {
		report.ID = id
	}
	return nil
}

// QueryNewsAnalysisHistory 查詢新聞分析历史（分页，回傳 total 總數）
func (s *SQLiteStorage) QueryNewsAnalysisHistory(symbol string, startTime, endTime time.Time, limit, offset int) ([]*NewsAnalysisHistory, int64, error) {
	args := []interface{}{startTime, endTime}
	whereSymbol := ""
	if symbol != "" {
		whereSymbol = " AND symbol = ?"
		args = append(args, symbol)
	}

	// 查總數
	var total int64
	countSQL := "SELECT COUNT(*) FROM news_analysis_history WHERE analysis_time >= ? AND analysis_time <= ?" + whereSymbol
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// 分页查詢
	args = append(args, limit, offset)
	query := `
		SELECT id, analysis_time, symbol, current_price, assessment, recent_news_summary, gemini_prompt, gemini_response, created_at
		FROM news_analysis_history
		WHERE analysis_time >= ? AND analysis_time <= ?` + whereSymbol + `
		ORDER BY analysis_time DESC
		LIMIT ? OFFSET ?`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []*NewsAnalysisHistory
	for rows.Next() {
		var h NewsAnalysisHistory
		var prompt, response sql.NullString
		if err := rows.Scan(&h.ID, &h.AnalysisTime, &h.Symbol, &h.CurrentPrice, &h.Assessment, &h.RecentNewsSummary, &prompt, &response, &h.CreatedAt); err != nil {
			return nil, 0, err
		}
		if prompt.Valid {
			h.GeminiPrompt = prompt.String
		}
		if response.Valid {
			h.GeminiResponse = response.String
		}
		list = append(list, &h)
	}
	return list, total, rows.Err()
}

// GetLatestNewsAnalysisHistory 獲取指定币种最新分析記錄（symbol 為空時返回任意币种最新）
func (s *SQLiteStorage) GetLatestNewsAnalysisHistory(symbol string) (*NewsAnalysisHistory, error) {
	var query string
	var args []interface{}
	if symbol != "" {
		query = `
			SELECT id, analysis_time, symbol, current_price, assessment, recent_news_summary, gemini_prompt, gemini_response, created_at
			FROM news_analysis_history
			WHERE symbol = ?
			ORDER BY analysis_time DESC
			LIMIT 1`
		args = []interface{}{symbol}
	} else {
		query = `
			SELECT id, analysis_time, symbol, current_price, assessment, recent_news_summary, gemini_prompt, gemini_response, created_at
			FROM news_analysis_history
			ORDER BY analysis_time DESC
			LIMIT 1`
	}
	var h NewsAnalysisHistory
	var prompt, response sql.NullString
	var err error
	if len(args) > 0 {
		err = s.db.QueryRow(query, args...).Scan(&h.ID, &h.AnalysisTime, &h.Symbol, &h.CurrentPrice, &h.Assessment, &h.RecentNewsSummary, &prompt, &response, &h.CreatedAt)
	} else {
		err = s.db.QueryRow(query).Scan(&h.ID, &h.AnalysisTime, &h.Symbol, &h.CurrentPrice, &h.Assessment, &h.RecentNewsSummary, &prompt, &response, &h.CreatedAt)
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if prompt.Valid {
		h.GeminiPrompt = prompt.String
	}
	if response.Valid {
		h.GeminiResponse = response.String
	}
	return &h, nil
}

// GetNewsAnalysisHistoryByID 按 ID 獲取分析記錄
func (s *SQLiteStorage) GetNewsAnalysisHistoryByID(id int64) (*NewsAnalysisHistory, error) {
	var h NewsAnalysisHistory
	var prompt, response sql.NullString
	err := s.db.QueryRow(`
		SELECT id, analysis_time, symbol, current_price, assessment, recent_news_summary, gemini_prompt, gemini_response, created_at
		FROM news_analysis_history WHERE id = ?
	`, id).Scan(&h.ID, &h.AnalysisTime, &h.Symbol, &h.CurrentPrice, &h.Assessment, &h.RecentNewsSummary, &prompt, &response, &h.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if prompt.Valid {
		h.GeminiPrompt = prompt.String
	}
	if response.Valid {
		h.GeminiResponse = response.String
	}
	return &h, nil
}

// CleanupNewsAnalysisHistory 清理指定時间之前的記錄
func (s *SQLiteStorage) CleanupNewsAnalysisHistory(beforeTime time.Time) error {
	_, err := s.db.Exec(`DELETE FROM news_analysis_history WHERE analysis_time < ?`, beforeTime)
	return err
}

// SavePriceHistory 保存價格历史
func (s *SQLiteStorage) SavePriceHistory(h *PriceHistory) error {
	_, err := s.db.Exec(`
		INSERT INTO price_history (asset_type, symbol, price, source, recorded_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, h.AssetType, h.Symbol, h.Price, h.Source, h.RecordedAt, h.CreatedAt)
	return err
}

// GetPriceAtTime 獲取指定時间附近的價格（在 tolerance 範圍内取最近的一条）
func (s *SQLiteStorage) GetPriceAtTime(assetType, symbol string, t time.Time, tolerance time.Duration) (*PriceHistory, error) {
	start := t.Add(-tolerance)
	end := t.Add(tolerance)
	var h PriceHistory
	err := s.db.QueryRow(`
		SELECT id, asset_type, symbol, price, source, recorded_at, created_at
		FROM price_history
		WHERE asset_type = ? AND symbol = ? AND recorded_at >= ? AND recorded_at <= ?
		ORDER BY ABS(strftime('%s', recorded_at) - strftime('%s', ?)) LIMIT 1
	`, assetType, symbol, start, end, t).Scan(&h.ID, &h.AssetType, &h.Symbol, &h.Price, &h.Source, &h.RecordedAt, &h.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &h, nil
}

// GetPriceHistory 查詢價格历史
func (s *SQLiteStorage) GetPriceHistory(assetType, symbol string, startTime, endTime time.Time, limit int) ([]*PriceHistory, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.Query(`
		SELECT id, asset_type, symbol, price, source, recorded_at, created_at
		FROM price_history
		WHERE asset_type = ? AND symbol = ? AND recorded_at >= ? AND recorded_at <= ?
		ORDER BY recorded_at ASC LIMIT ?
	`, assetType, symbol, startTime, endTime, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*PriceHistory
	for rows.Next() {
		var h PriceHistory
		var source sql.NullString
		if err := rows.Scan(&h.ID, &h.AssetType, &h.Symbol, &h.Price, &source, &h.RecordedAt, &h.CreatedAt); err != nil {
			return nil, err
		}
		if source.Valid {
			h.Source = source.String
		}
		list = append(list, &h)
	}
	return list, rows.Err()
}

// SavePredictionVerification 保存預测驗证記錄
func (s *SQLiteStorage) SavePredictionVerification(v *PredictionVerification) error {
	isCorrect := 0
	if v.IsCorrect {
		isCorrect = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO prediction_verification (
			analysis_id, asset_type, symbol, prediction_time, timeframe,
			predicted_direction, predicted_change_pct, predicted_probability,
			actual_price_at_prediction, actual_price_at_verify, actual_direction,
			actual_change_pct, is_correct, verified_at, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, v.AnalysisID, v.AssetType, v.Symbol, v.PredictionTime, v.Timeframe,
		v.PredictedDirection, v.PredictedChangePct, v.PredictedProbability,
		v.ActualPriceAtPred, v.ActualPriceAtVerify, v.ActualDirection,
		v.ActualChangePct, isCorrect, v.VerifiedAt, v.Status)
	return err
}

// QueryPredictionVerifications 查詢預测驗证記錄
func (s *SQLiteStorage) QueryPredictionVerifications(assetType, symbol string, startTime, endTime time.Time, limit, offset int) ([]*PredictionVerification, int64, error) {
	args := []interface{}{startTime, endTime}
	where := "WHERE prediction_time >= ? AND prediction_time <= ?"
	if assetType != "" {
		where += " AND asset_type = ?"
		args = append(args, assetType)
	}
	if symbol != "" {
		where += " AND symbol = ?"
		args = append(args, symbol)
	}

	var total int64
	countArgs := args
	if err := s.db.QueryRow("SELECT COUNT(*) FROM prediction_verification "+where, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	rows, err := s.db.Query(`
		SELECT id, analysis_id, asset_type, symbol, prediction_time, timeframe,
			predicted_direction, predicted_change_pct, predicted_probability,
			actual_price_at_prediction, actual_price_at_verify, actual_direction,
			actual_change_pct, is_correct, verified_at, status
		FROM prediction_verification `+where+` ORDER BY prediction_time DESC LIMIT ? OFFSET ?
	`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var list []*PredictionVerification
	for rows.Next() {
		var v PredictionVerification
		var isCorrect int
		var verifiedAt sql.NullTime
		if err := rows.Scan(&v.ID, &v.AnalysisID, &v.AssetType, &v.Symbol, &v.PredictionTime, &v.Timeframe,
			&v.PredictedDirection, &v.PredictedChangePct, &v.PredictedProbability,
			&v.ActualPriceAtPred, &v.ActualPriceAtVerify, &v.ActualDirection,
			&v.ActualChangePct, &isCorrect, &verifiedAt, &v.Status); err != nil {
			return nil, 0, err
		}
		v.IsCorrect = isCorrect == 1
		if verifiedAt.Valid {
			v.VerifiedAt = verifiedAt.Time
		}
		list = append(list, &v)
	}
	return list, total, rows.Err()
}

// GetPredictionVerificationsByStatus 按状態查詢
func (s *SQLiteStorage) GetPredictionVerificationsByStatus(status string, limit int) ([]*PredictionVerification, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT id, analysis_id, asset_type, symbol, prediction_time, timeframe,
			predicted_direction, predicted_change_pct, predicted_probability,
			actual_price_at_prediction, actual_price_at_verify, actual_direction,
			actual_change_pct, is_correct, verified_at, status
		FROM prediction_verification WHERE status = ? ORDER BY prediction_time ASC LIMIT ?
	`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*PredictionVerification
	for rows.Next() {
		var v PredictionVerification
		var isCorrect int
		var verifiedAt sql.NullTime
		if err := rows.Scan(&v.ID, &v.AnalysisID, &v.AssetType, &v.Symbol, &v.PredictionTime, &v.Timeframe,
			&v.PredictedDirection, &v.PredictedChangePct, &v.PredictedProbability,
			&v.ActualPriceAtPred, &v.ActualPriceAtVerify, &v.ActualDirection,
			&v.ActualChangePct, &isCorrect, &verifiedAt, &v.Status); err != nil {
			return nil, err
		}
		v.IsCorrect = isCorrect == 1
		if verifiedAt.Valid {
			v.VerifiedAt = verifiedAt.Time
		}
		list = append(list, &v)
	}
	return list, rows.Err()
}

// UpdatePredictionVerification 更新預测驗证記錄
func (s *SQLiteStorage) UpdatePredictionVerification(v *PredictionVerification) error {
	isCorrect := 0
	if v.IsCorrect {
		isCorrect = 1
	}
	_, err := s.db.Exec(`
		UPDATE prediction_verification SET
			actual_price_at_verify = ?, actual_direction = ?, actual_change_pct = ?,
			is_correct = ?, verified_at = ?, status = ?
		WHERE id = ?
	`, v.ActualPriceAtVerify, v.ActualDirection, v.ActualChangePct, isCorrect, v.VerifiedAt, v.Status, v.ID)
	return err
}

// GetPredictionAccuracyStats 獲取預测准确率统计
func (s *SQLiteStorage) GetPredictionAccuracyStats(assetType string, since time.Time) (total int, correct int, err error) {
	query := "SELECT COUNT(*), SUM(CASE WHEN is_correct = 1 THEN 1 ELSE 0 END) FROM prediction_verification WHERE status = 'verified' AND verified_at >= ?"
	args := []interface{}{since}
	if assetType != "" {
		query += " AND asset_type = ?"
		args = append(args, assetType)
	}
	var sumCorrect sql.NullInt64
	err = s.db.QueryRow(query, args...).Scan(&total, &sumCorrect)
	if err != nil {
		return 0, 0, err
	}
	if sumCorrect.Valid {
		correct = int(sumCorrect.Int64)
	}
	return total, correct, nil
}

// Close 关闭數據库连接
func (s *SQLiteStorage) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

// ListAccountIDsWithProfitRules 返回有提取规则的所有 account_id（用於定時任務）
func (s *SQLiteStorage) ListAccountIDsWithProfitRules() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT account_id FROM profit_withdraw_rules WHERE enabled = 1`)
	if err != nil {
		return nil, fmt.Errorf("查詢 account_id 失败: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListProfitWithdrawRules 查詢指定账戶的自动提取规则（返回全交易所）
func (s *SQLiteStorage) ListProfitWithdrawRules(accountID string) ([]*ProfitWithdrawRule, error) {
	if accountID == "" {
		accountID = "default"
	}

	rows, err := s.db.Query(`
		SELECT id, account_id, exchange_id, strategy_id, enabled, trigger_amount, withdraw_ratio,
		       frequency, destination, wallet_address, min_withdraw_amount, max_withdraw_amount,
		       created_at, updated_at,
		       last_triggered_at
		FROM profit_withdraw_rules
		WHERE account_id = ?
		ORDER BY updated_at DESC
	`, accountID)
	if err != nil {
		return nil, fmt.Errorf("查詢 profit_withdraw_rules 失败: %w", err)
	}
	defer rows.Close()

	var out []*ProfitWithdrawRule
	for rows.Next() {
		r := &ProfitWithdrawRule{}
		var enabledInt int
		var walletAddr sql.NullString
		var maxAmt sql.NullFloat64
		var createdAt, updatedAt time.Time
		var lastTriggered sql.NullTime
		if err := rows.Scan(
			&r.ID,
			&r.AccountID,
			&r.ExchangeID,
			&r.StrategyID,
			&enabledInt,
			&r.TriggerAmount,
			&r.WithdrawRatio,
			&r.Frequency,
			&r.Destination,
			&walletAddr,
			&r.MinWithdrawAmount,
			&maxAmt,
			&createdAt,
			&updatedAt,
			&lastTriggered,
		); err != nil {
			continue
		}
		r.Enabled = enabledInt != 0
		if walletAddr.Valid {
			r.WalletAddress = walletAddr.String
		}
		if maxAmt.Valid {
			v := maxAmt.Float64
			r.MaxWithdrawAmount = &v
		}
		if lastTriggered.Valid {
			t := lastTriggered.Time
			r.LastTriggeredAt = &t
		}
		r.CreatedAt = createdAt
		r.UpdatedAt = updatedAt
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReplaceProfitWithdrawRules 用一组规则替换指定账戶的全部规则（事務保证原子性）
func (s *SQLiteStorage) ReplaceProfitWithdrawRules(accountID string, rules []*ProfitWithdrawRule) error {
	if accountID == "" {
		accountID = "default"
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("开啟事務失败: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.Exec(`DELETE FROM profit_withdraw_rules WHERE account_id = ?`, accountID); err != nil {
		return fmt.Errorf("清空舊规则失败: %w", err)
	}

	now := utils.NowUTC()
	stmt, err := tx.Prepare(`
		INSERT INTO profit_withdraw_rules
		(id, account_id, exchange_id, strategy_id, enabled, trigger_amount, withdraw_ratio,
		 frequency, destination, wallet_address, min_withdraw_amount, max_withdraw_amount,
		 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("准备插入语句失败: %w", err)
	}
	defer stmt.Close()

	for _, r := range rules {
		if r == nil {
			continue
		}
		if r.ID == "" {
			r.ID = fmt.Sprintf("rule_%d", time.Now().UnixNano())
		}
		r.AccountID = accountID
		if r.CreatedAt.IsZero() {
			r.CreatedAt = now
		}
		r.UpdatedAt = now
		if r.ExchangeID == "" {
			return fmt.Errorf("exchange_id 不能為空")
		}

		var wallet interface{}
		if r.WalletAddress != "" {
			wallet = r.WalletAddress
		}
		var maxAmt interface{}
		if r.MaxWithdrawAmount != nil {
			maxAmt = *r.MaxWithdrawAmount
		}

		enabledInt := 0
		if r.Enabled {
			enabledInt = 1
		}

		if _, err := stmt.Exec(
			r.ID,
			r.AccountID,
			r.ExchangeID,
			r.StrategyID,
			enabledInt,
			r.TriggerAmount,
			r.WithdrawRatio,
			r.Frequency,
			r.Destination,
			wallet,
			r.MinWithdrawAmount,
			maxAmt,
			utils.ToUTC(r.CreatedAt),
			utils.ToUTC(r.UpdatedAt),
		); err != nil {
			return fmt.Errorf("插入规则失败: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事務失败: %w", err)
	}
	return nil
}

// UpsertProfitWithdrawRule 創建或更新單条规则
func (s *SQLiteStorage) UpsertProfitWithdrawRule(accountID string, rule *ProfitWithdrawRule) error {
	if rule == nil {
		return fmt.Errorf("rule 不能為空")
	}
	if accountID == "" {
		accountID = "default"
	}
	if rule.ExchangeID == "" {
		return fmt.Errorf("exchange_id 不能為空")
	}
	if rule.ID == "" {
		rule.ID = fmt.Sprintf("rule_%d", time.Now().UnixNano())
	}

	now := utils.NowUTC()
	if rule.CreatedAt.IsZero() {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	rule.AccountID = accountID

	enabledInt := 0
	if rule.Enabled {
		enabledInt = 1
	}
	var wallet interface{}
	if rule.WalletAddress != "" {
		wallet = rule.WalletAddress
	}
	var maxAmt interface{}
	if rule.MaxWithdrawAmount != nil {
		maxAmt = *rule.MaxWithdrawAmount
	}

	_, err := s.db.Exec(`
		INSERT INTO profit_withdraw_rules
		(id, account_id, exchange_id, strategy_id, enabled, trigger_amount, withdraw_ratio,
		 frequency, destination, wallet_address, min_withdraw_amount, max_withdraw_amount,
		 created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  exchange_id=excluded.exchange_id,
		  strategy_id=excluded.strategy_id,
		  enabled=excluded.enabled,
		  trigger_amount=excluded.trigger_amount,
		  withdraw_ratio=excluded.withdraw_ratio,
		  frequency=excluded.frequency,
		  destination=excluded.destination,
		  wallet_address=excluded.wallet_address,
		  min_withdraw_amount=excluded.min_withdraw_amount,
		  max_withdraw_amount=excluded.max_withdraw_amount,
		  updated_at=excluded.updated_at
	`, rule.ID, rule.AccountID, rule.ExchangeID, rule.StrategyID, enabledInt,
		rule.TriggerAmount, rule.WithdrawRatio, rule.Frequency, rule.Destination, wallet,
		rule.MinWithdrawAmount, maxAmt, utils.ToUTC(rule.CreatedAt), utils.ToUTC(rule.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("upsert profit_withdraw_rules 失败: %w", err)
	}
	return nil
}

// DeleteProfitWithdrawRule 刪除單条规则（按账戶隔离）
func (s *SQLiteStorage) DeleteProfitWithdrawRule(accountID string, ruleID string) error {
	if accountID == "" {
		accountID = "default"
	}
	if ruleID == "" {
		return fmt.Errorf("ruleID 不能為空")
	}
	_, err := s.db.Exec(`DELETE FROM profit_withdraw_rules WHERE account_id = ? AND id = ?`, accountID, ruleID)
	if err != nil {
		return fmt.Errorf("刪除 profit_withdraw_rules 失败: %w", err)
	}
	return nil
}

// UpdateRuleLastTriggeredAt 更新规则最后執行時间
func (s *SQLiteStorage) UpdateRuleLastTriggeredAt(ruleID string, triggeredAt time.Time) error {
	_, err := s.db.Exec(`UPDATE profit_withdraw_rules SET last_triggered_at = ?, updated_at = ? WHERE id = ?`,
		triggeredAt, time.Now(), ruleID)
	if err != nil {
		return fmt.Errorf("更新规则 last_triggered_at 失败: %w", err)
	}
	return nil
}

// SaveWithdrawRecord 保存提取記錄
func (s *SQLiteStorage) SaveWithdrawRecord(record *ProfitWithdrawRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO profit_withdraw_records (id, rule_id, account_id, exchange_id, strategy_id, amount, fee, net_amount, currency, type, status, destination, transfer_id, created_at, completed_at, failed_reason, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.RuleID, record.AccountID, record.ExchangeID, record.StrategyID,
		record.Amount, record.Fee, record.NetAmount, record.Currency, record.Type, record.Status, record.Destination,
		record.TransferID, record.CreatedAt, nil, record.FailedReason, record.Note)
	if err != nil {
		return fmt.Errorf("保存 profit_withdraw_records 失败: %w", err)
	}
	return nil
}

// UpdateWithdrawRecordStatus 更新提取記錄状態
func (s *SQLiteStorage) UpdateWithdrawRecordStatus(id, status, transferID, failedReason string) error {
	var completedAt interface{}
	if status == "completed" || status == "failed" {
		completedAt = time.Now()
	} else {
		completedAt = nil
	}
	_, err := s.db.Exec(`
		UPDATE profit_withdraw_records SET status = ?, transfer_id = ?, failed_reason = ?, completed_at = ? WHERE id = ?`,
		status, transferID, failedReason, completedAt, id)
	if err != nil {
		return fmt.Errorf("更新 profit_withdraw_records 状態失败: %w", err)
	}
	return nil
}

// GetWithdrawRecords 查詢提取記錄（按創建時间倒序）
func (s *SQLiteStorage) GetWithdrawRecords(accountID string, limit int) ([]*ProfitWithdrawRecord, error) {
	if accountID == "" {
		accountID = "default"
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT id, rule_id, account_id, exchange_id, strategy_id, amount, fee, net_amount, currency, type, status, destination, transfer_id, created_at, completed_at, failed_reason, note
		FROM profit_withdraw_records WHERE account_id = ? ORDER BY created_at DESC LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, fmt.Errorf("查詢 profit_withdraw_records 失败: %w", err)
	}
	defer rows.Close()

	var out []*ProfitWithdrawRecord
	for rows.Next() {
		r := &ProfitWithdrawRecord{}
		var completedAt sql.NullTime
		var transferID, failedReason, note sql.NullString
		if err := rows.Scan(
			&r.ID, &r.RuleID, &r.AccountID, &r.ExchangeID, &r.StrategyID,
			&r.Amount, &r.Fee, &r.NetAmount, &r.Currency, &r.Type, &r.Status, &r.Destination,
			&transferID, &r.CreatedAt, &completedAt, &failedReason, &note,
		); err != nil {
			continue
		}
		if transferID.Valid {
			r.TransferID = transferID.String
		}
		if completedAt.Valid {
			t := completedAt.Time
			r.CompletedAt = &t
		}
		if failedReason.Valid {
			r.FailedReason = failedReason.String
		}
		if note.Valid {
			r.Note = note.String
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveHourlyEquityRecord 保存小時權益記錄
func (s *SQLiteStorage) SaveHourlyEquityRecord(record *HourlyEquityRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO hourly_equity_records (exchange, symbol, account, timestamp, equity, unrealized_pnl, total_position_value, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Exchange, record.Symbol, record.Account, record.Timestamp,
		record.Equity, record.UnrealizedPnL, record.TotalPositionValue, time.Now())
	if err != nil {
		return fmt.Errorf("保存 hourly_equity_record 失败: %w", err)
	}
	return nil
}

// SaveDailySnapshot 保存每日快照（upsert）
func (s *SQLiteStorage) SaveDailySnapshot(snapshot *DailySnapshot) error {
	_, err := s.db.Exec(`
		INSERT INTO daily_snapshots (exchange, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(exchange, symbol, account, date) DO UPDATE SET
			unrealized_pnl = excluded.unrealized_pnl,
			total_position_value = excluded.total_position_value,
			intraday_max_drawdown = excluded.intraday_max_drawdown,
			intraday_max_drawdown_pct = excluded.intraday_max_drawdown_pct,
			intraday_peak_equity = excluded.intraday_peak_equity,
			closing_price = excluded.closing_price,
			snapshot_time = excluded.snapshot_time`,
		snapshot.Exchange, snapshot.Symbol, snapshot.Account, snapshot.Date.Format("2006-01-02"),
		snapshot.UnrealizedPnL, snapshot.TotalPositionValue, snapshot.IntradayMaxDrawdown, snapshot.IntradayMaxDrawdownPct,
		snapshot.IntradayPeakEquity, snapshot.ClosingPrice, snapshot.SnapshotTime, time.Now())
	if err != nil {
		return fmt.Errorf("保存 daily_snapshot 失败: %w", err)
	}
	return nil
}

// QueryDailySnapshots 查詢日期範圍內的每日快照
func (s *SQLiteStorage) QueryDailySnapshots(exchange, symbol, account string, startDate, endDate time.Time) ([]*DailySnapshot, error) {
	rows, err := s.db.Query(`
		SELECT id, exchange, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at
		FROM daily_snapshots
		WHERE exchange = ? AND symbol = ? AND account = ? AND date >= ? AND date <= ?
		ORDER BY date ASC`,
		exchange, symbol, account, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("查詢 daily_snapshots 失败: %w", err)
	}
	defer rows.Close()

	var out []*DailySnapshot
	for rows.Next() {
		snap := &DailySnapshot{}
		var dateStr string
		var snapshotTime, createdAt time.Time
		if err := rows.Scan(
			&snap.ID, &snap.Exchange, &snap.Symbol, &snap.Account, &dateStr,
			&snap.UnrealizedPnL, &snap.TotalPositionValue, &snap.IntradayMaxDrawdown, &snap.IntradayMaxDrawdownPct,
			&snap.IntradayPeakEquity, &snap.ClosingPrice, &snapshotTime, &createdAt,
		); err != nil {
			continue
		}
		if t, e := time.Parse("2006-01-02", dateStr); e == nil {
			snap.Date = t
		}
		snap.SnapshotTime = snapshotTime
		snap.CreatedAt = createdAt
		out = append(out, snap)
	}
	return out, rows.Err()
}

// GetDailySnapshot 查詢單日快照
func (s *SQLiteStorage) GetDailySnapshot(exchange, symbol, account string, date time.Time) (*DailySnapshot, error) {
	dateStr := date.Format("2006-01-02")
	row := s.db.QueryRow(`
		SELECT id, exchange, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at
		FROM daily_snapshots
		WHERE exchange = ? AND symbol = ? AND account = ? AND date = ?`,
		exchange, symbol, account, dateStr)
	snap := &DailySnapshot{}
	var snapshotTime, createdAt time.Time
	var dStr string
	err := row.Scan(
		&snap.ID, &snap.Exchange, &snap.Symbol, &snap.Account, &dStr,
		&snap.UnrealizedPnL, &snap.TotalPositionValue, &snap.IntradayMaxDrawdown, &snap.IntradayMaxDrawdownPct,
		&snap.IntradayPeakEquity, &snap.ClosingPrice, &snapshotTime, &createdAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查詢 daily_snapshot 失败: %w", err)
	}
	if t, e := time.Parse("2006-01-02", dStr); e == nil {
		snap.Date = t
	}
	snap.SnapshotTime = snapshotTime
	snap.CreatedAt = createdAt
	return snap, nil
}

// QueryHourlyEquityRecords 查詢時間範圍內的小時權益記錄（用於計算日內最大回撤）
func (s *SQLiteStorage) QueryHourlyEquityRecords(exchange, symbol, account string, startTime, endTime time.Time) ([]*HourlyEquityRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, exchange, symbol, account, timestamp, equity, unrealized_pnl, total_position_value, created_at
		FROM hourly_equity_records
		WHERE exchange = ? AND symbol = ? AND account = ? AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC`,
		exchange, symbol, account, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("查詢 hourly_equity_records 失败: %w", err)
	}
	defer rows.Close()

	var out []*HourlyEquityRecord
	for rows.Next() {
		r := &HourlyEquityRecord{}
		var ts, createdAt time.Time
		if err := rows.Scan(&r.ID, &r.Exchange, &r.Symbol, &r.Account, &ts, &r.Equity, &r.UnrealizedPnL, &r.TotalPositionValue, &createdAt); err != nil {
			continue
		}
		r.Timestamp = ts
		r.CreatedAt = createdAt
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteHourlyEquityRecordsBefore 刪除指定時間之前的小時級數據（用於 90 天清理）
func (s *SQLiteStorage) DeleteHourlyEquityRecordsBefore(cutoff time.Time) error {
	result, err := s.db.Exec(`DELETE FROM hourly_equity_records WHERE timestamp < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("刪除過期 hourly_equity_records 失败: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected > 0 {
		logger.Info("🧹 已清理 %d 条過期小時級數據", affected)
	}
	return nil
}
