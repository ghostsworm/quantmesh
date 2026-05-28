package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/mattn/go-sqlite3"
)

// pairedTradesTableMySQL 網格買賣配對成交表。須與 database 包 GORM 的 trades（逐筆成交、列名 pn_l 等）分離，
// 否則與 storage 預期的 buy_order_id/sell_order_id/pnl 結構衝突，導致查詢 Unknown column 'pnl'。
const pairedTradesTableMySQL = "qm_paired_trades"

// SQLStorage 基於 database/sql 的存儲實現（SQLite 與 MySQL 共用本類型）。
type SQLStorage struct {
	db     *sql.DB
	dbType string // sqlite, mysql, postgres
	closed bool
	// tradesTable 網格配對成交表名：SQLite 為 trades；MySQL 為 qm_paired_trades（避免與 GORM trades 同庫衝突）
	tradesTable string
}

// tradesTbl 返回網格配對成交表名（永遠為內部常量，非用戶輸入）
func (s *SQLStorage) tradesTbl() string {
	if s != nil && s.tradesTable != "" {
		return s.tradesTable
	}
	return "trades"
}

// mysqlQuoteIdent MySQL 保留字（如 key、interval）作列名時需反引號；SQLite 保持原名。
func (s *SQLStorage) mysqlQuoteIdent(name string) string {
	if s == nil || s.dbType != "mysql" {
		return name
	}
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// NewSQLStorage 打開 SQLite 路徑並初始化存儲（含表遷移）
func NewSQLStorage(path string) (*SQLStorage, error) {
	return NewStorage("sqlite", path+"?_journal_mode=WAL&_synchronous=NORMAL")
}

// NewMySQLStorage 創建 MySQL 存儲
func NewMySQLStorage(dsn string) (*SQLStorage, error) {
	return NewStorage("mysql", dsn)
}

// NewStorage 創建通用存儲（支援 sqlite、mysql，PostgreSQL 暂不支持）
func NewStorage(dbType, dsn string) (*SQLStorage, error) {
	var driverName string
	switch dbType {
	case "sqlite":
		driverName = "sqlite3"
	case "mysql":
		driverName = "mysql"
	case "postgres", "postgresql":
		return nil, fmt.Errorf("PostgreSQL 暂不支持，请使用 sqlite 或 mysql")
	default:
		return nil, fmt.Errorf("不支援的數據库類型: %s", dbType)
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("打开數據库失败: %w", err)
	}

	// 設置连接池
	if dbType == "sqlite" {
		db.SetMaxOpenConns(1) // SQLite 並发限制
		db.SetMaxIdleConns(1)
	} else {
		db.SetMaxOpenConns(100)
		db.SetMaxIdleConns(10)
	}

	// 創建表和索引（僅 SQLite 需要，MySQL/PostgreSQL 使用 GORM AutoMigrate）
	if dbType == "sqlite" {
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

		// 迁移：trades 表添加交易所方式盈亏字段
		if err := migrateTradesExchangePnL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 trades exchange_pnl 字段失败: %w", err)
		}

		// 迁移：orders 表增加 filled_qty / exchange / type / realized_pnl 列
		if err := migrateOrdersTable(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 orders 表失败: %w", err)
		}
		// 迁移：risk_check_history 表增加 bot_id / exchange / market_type 列
		if err := migrateRiskCheckHistoryTable(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 risk_check_history 表失败: %w", err)
		}

		// 迁移：创建系统设置表
		if err := migrateSystemSettingsTable(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 system_settings 表失败: %w", err)
		}

		// 迁移：创建配置管理表
		if err := migrateConfigTables(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 config_tables 表失败: %w", err)
		}
		// 迁移：主配置 / Bot 文檔表（app_config、bot_configs 及歷史表）
		if err := migrateAppConfigDocumentTables(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 app_config 文檔表失败: %w", err)
		}
	} else if dbType == "mysql" {
		// MySQL 需單獨遷移 bot_states 表（SQLite 在 createTables 中已包含）
		if err := migrateBotStatesTableMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 bot_states 表失败: %w", err)
		}
		if err := migrateAppConfigDocumentTablesMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 app_config 文檔表失败: %w", err)
		}
		if err := migratePairedTradesTableMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL 網格配對成交表失败: %w", err)
		}
		if err := migratePairedTradesBotIDMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL 網格配對成交表 bot_id 失败: %w", err)
		}
		if err := migrateSystemMetricsTablesMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL system_metrics 表失败: %w", err)
		}
		if err := migrateBotRiskControlEventsMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL bot_risk_control_events 表失败: %w", err)
		}
		if err := migrateGeminiUsageTableMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL gemini_usage 表失败: %w", err)
		}
	}

	tradesTbl := "trades"
	if dbType == "mysql" {
		tradesTbl = pairedTradesTableMySQL
	}
	return &SQLStorage{db: db, dbType: dbType, tradesTable: tradesTbl}, nil
}

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

// migrateMarketInterpretTable 遷移市場 AI 解讀任務表
func migrateMarketInterpretTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS market_interpret_tasks (
			task_id TEXT PRIMARY KEY,
			page_type TEXT NOT NULL,
			symbol TEXT NOT NULL,
			status TEXT NOT NULL,
			progress INTEGER NOT NULL DEFAULT 0,
			result TEXT,
			error TEXT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_market_interpret_page_created ON market_interpret_tasks(page_type, created_at);
	`)
	return err
}

// migrateProtectedKlineFilesTable 遷移K線文件保護表
func migrateProtectedKlineFilesTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS protected_kline_files (
			filename TEXT PRIMARY KEY,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_protected_kline_files_filename ON protected_kline_files(filename);
	`)
	return err
}

// migrateKlineFilesTable 迁移 K 线文件统一管理表
func migrateKlineFilesTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS kline_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			filename TEXT UNIQUE NOT NULL,           -- 文件名（不含路径）
			exchange TEXT NOT NULL,                  -- 交易所 (binance, bitget)
			symbol TEXT NOT NULL,                    -- 交易对 (BTCUSDT)
			interval TEXT NOT NULL,                  -- K线周期 (tick, 1m, 1h, 1d)
			start_time TIMESTAMP NOT NULL,           -- 数据开始时间
			end_time TIMESTAMP,                      -- 数据结束时间（采集中为 NULL）
			status TEXT NOT NULL DEFAULT 'collecting', -- collecting | completed | error
			has_depth INTEGER NOT NULL DEFAULT 0,    -- 是否带深度数据 (0/1)
			candle_count INTEGER DEFAULT 0,          -- K线条数
			file_size INTEGER DEFAULT 0,             -- 文件大小（字节）
			source TEXT NOT NULL,                    -- 数据来源: collector | backtest_cache | manual
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_kline_files_symbol ON kline_files(symbol);
		CREATE INDEX IF NOT EXISTS idx_kline_files_status ON kline_files(status);
		CREATE INDEX IF NOT EXISTS idx_kline_files_exchange_symbol_interval ON kline_files(exchange, symbol, interval);
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

// migrateAccountEquitySnapshotColumns 為快照表增加交易所帳戶權益列（用於淨值曲線；SQLite pragma 檢測）
func migrateAccountEquitySnapshotColumns(db *sql.DB) error {
	checks := []struct {
		pragmaQuery string
		alterSQL    string
	}{
		{
			"SELECT COUNT(*) FROM pragma_table_info('hourly_equity_records') WHERE name = ?",
			"ALTER TABLE hourly_equity_records ADD COLUMN account_equity REAL",
		},
		{
			"SELECT COUNT(*) FROM pragma_table_info('daily_snapshots') WHERE name = ?",
			"ALTER TABLE daily_snapshots ADD COLUMN account_equity REAL",
		},
	}
	for _, c := range checks {
		var count int
		if err := db.QueryRow(c.pragmaQuery, "account_equity").Scan(&count); err != nil {
			continue
		}
		if count == 0 {
			if _, err := db.Exec(c.alterSQL); err != nil {
				// 列已存在等情況忽略
			}
		}
	}
	return nil
}

// migrateBacktestTasksTable 迁移 backtest_tasks 表（回测任務）
func migrateBacktestTasksTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS backtest_tasks (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			mode TEXT,
			bot_id TEXT,
			group_id TEXT,
			strategy TEXT NOT NULL,
			strategies_json TEXT,
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
			report_path TEXT,
			data_source TEXT,
			kline_file TEXT,
			cache_name TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_backtest_tasks_created_at ON backtest_tasks(created_at);
	`)
	if err != nil {
		return err
	}

	// 检查并添加新字段（为现有表添加列）
	return migrateBacktestTasksColumns(db)
}

// migrateBacktestTasksColumns 为 backtest_tasks 表添加新列（数据源相关字段）
func migrateBacktestTasksColumns(db *sql.DB) error {
	// 检查表是否存在新字段，不存在则添加
	columns := []struct {
		name string
		def  string
	}{
		{"mode", "ALTER TABLE backtest_tasks ADD COLUMN mode TEXT;"},
		{"bot_id", "ALTER TABLE backtest_tasks ADD COLUMN bot_id TEXT;"},
		{"group_id", "ALTER TABLE backtest_tasks ADD COLUMN group_id TEXT;"},
		{"strategies_json", "ALTER TABLE backtest_tasks ADD COLUMN strategies_json TEXT;"},
		{"data_source", "ALTER TABLE backtest_tasks ADD COLUMN data_source TEXT;"},
		{"kline_file", "ALTER TABLE backtest_tasks ADD COLUMN kline_file TEXT;"},
		{"cache_name", "ALTER TABLE backtest_tasks ADD COLUMN cache_name TEXT;"},
	}

	for _, col := range columns {
		// 先检查列是否存在
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('backtest_tasks') WHERE name = ?", col.name).Scan(&count)
		if err != nil {
			continue // 忽略错误，尝试下一列
		}

		// 列不存在则添加
		if count == 0 {
			if _, err := db.Exec(col.def); err != nil {
				// ALTER TABLE 失败不应该阻止程序启动，仅记录日志
				// logger.Warn("添加列 %s 失败: %v", col.name, err)
			}
		}
	}

	return nil
}

// migrateOptimTasksTable 迁移 optim_tasks 表（参数优化任务）
func migrateOptimTasksTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS optim_tasks (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			strategy TEXT NOT NULL,
			symbol TEXT NOT NULL,
			interval TEXT NOT NULL,
			start_time INTEGER NOT NULL,
			end_time INTEGER NOT NULL,
			total_capital REAL NOT NULL,
			search_space TEXT NOT NULL,
			progress INTEGER DEFAULT 0,
			total_combos INTEGER DEFAULT 0,
			completed_combos INTEGER DEFAULT 0,
			created_at INTEGER NOT NULL,
			started_at INTEGER,
			completed_at INTEGER,
			result_path TEXT,
			error TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_optim_tasks_created_at ON optim_tasks(created_at);
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
	hasBuyPriceDeviationColumn := false
	hasSellPriceDeviationColumn := false
	hasBotIDColumn := false
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
		if name == "buy_price_deviation" {
			hasBuyPriceDeviationColumn = true
		}
		if name == "sell_price_deviation" {
			hasSellPriceDeviationColumn = true
		}
		if name == "bot_id" {
			hasBotIDColumn = true
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

	// 🔥 检查并添加价格偏差字段
	if !hasBuyPriceDeviationColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 buy_price_deviation 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN buy_price_deviation DECIMAL(20,8) DEFAULT 0`)
		if err != nil {
			return fmt.Errorf("添加 buy_price_deviation 列失败: %w", err)
		}
		logger.Info("✅ buy_price_deviation 列添加成功")
	}
	if !hasSellPriceDeviationColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 sell_price_deviation 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN sell_price_deviation DECIMAL(20,8) DEFAULT 0`)
		if err != nil {
			return fmt.Errorf("添加 sell_price_deviation 列失败: %w", err)
		}
		logger.Info("✅ sell_price_deviation 列添加成功")
	}

	if !hasBotIDColumn {
		logger.Info("🔄 开始迁移 trades 表：添加 bot_id 字段")
		_, err := db.Exec(`ALTER TABLE trades ADD COLUMN bot_id TEXT DEFAULT ''`)
		if err != nil {
			return fmt.Errorf("添加 bot_id 列失败: %w", err)
		}
		logger.Info("✅ bot_id 列添加成功")
		backfillTradesBotIDFromOrders(db, "trades")
	}

	// 無論是否是新增列，都确保索引存在（老库可能已有列但缺索引）
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_account_symbol ON trades(account, symbol)`)
	if err != nil {
		logger.Warn("⚠️ 确保 trades account 索引失败: %v", err)
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_exchange_symbol ON trades(exchange, symbol)`)
	if err != nil {
		logger.Warn("⚠️ 确保 trades exchange 索引失败: %v", err)
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_trades_bot_exchange_symbol ON trades(bot_id, exchange, symbol)`)
	if err != nil {
		logger.Warn("⚠️ 确保 trades bot_id 索引失败: %v", err)
	}

	logger.Info("✅ trades 表迁移检查完成")
	return nil
}

// backfillTradesBotIDFromOrders 用 orders.bot_id 回填 trades（best-effort）
func backfillTradesBotIDFromOrders(db *sql.DB, tableName string) {
	q := fmt.Sprintf(`
		UPDATE %s SET bot_id = COALESCE(
			(SELECT NULLIF(TRIM(o.bot_id), '') FROM orders o WHERE o.order_id = %s.sell_order_id LIMIT 1),
			(SELECT NULLIF(TRIM(o.bot_id), '') FROM orders o WHERE o.order_id = %s.buy_order_id LIMIT 1),
			''
		)
		WHERE (bot_id IS NULL OR bot_id = '')
			AND (
				EXISTS (SELECT 1 FROM orders o WHERE o.order_id = %s.sell_order_id AND NULLIF(TRIM(o.bot_id), '') IS NOT NULL)
				OR EXISTS (SELECT 1 FROM orders o WHERE o.order_id = %s.buy_order_id AND NULLIF(TRIM(o.bot_id), '') IS NOT NULL)
			)
	`, tableName, tableName, tableName, tableName, tableName)
	if _, err := db.Exec(q); err != nil {
		logger.Warn("⚠️ 回填 trades.bot_id 失败: %v", err)
	} else {
		logger.Info("✅ 已嘗試從 orders 回填 %s.bot_id", tableName)
	}
}

// migrateOrdersTable 迁移 orders 表，添加 filled_qty / exchange / type / realized_pnl / strategy_name / strategy_type 列
func migrateTradesExchangePnL(db *sql.DB) error {
	// 检查表是否存在新字段，不存在则添加
	columns := []struct {
		name string
		def  string
	}{
		{"exchange_pnl", "ALTER TABLE trades ADD COLUMN exchange_pnl DECIMAL(20,8) DEFAULT 0"},
	}

	for _, col := range columns {
		// 先检查列是否存在
		var count int
		err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('trades') WHERE name = ?", col.name).Scan(&count)
		if err != nil {
			continue // 忽略错误，尝试下一列
		}
		if count == 0 {
			if _, err := db.Exec(col.def); err != nil {
				logger.Warn("⚠️ trades 表添加列 %s 失败: %v", col.name, err)
			} else {
				logger.Info("🔄 trades 表成功添加列: %s", col.name)
			}
		}
	}
	return nil
}

func migrateOrdersTable(db *sql.DB) error {
	columns := []struct {
		name string
		def  string
	}{
		{"filled_qty", "ALTER TABLE orders ADD COLUMN filled_qty DECIMAL(20,8) DEFAULT 0"},
		{"bot_id", "ALTER TABLE orders ADD COLUMN bot_id TEXT DEFAULT ''"},
		{"account", "ALTER TABLE orders ADD COLUMN account TEXT DEFAULT ''"},
		{"exchange", "ALTER TABLE orders ADD COLUMN exchange TEXT DEFAULT ''"},
		{"type", "ALTER TABLE orders ADD COLUMN type TEXT DEFAULT ''"},
		{"realized_pnl", "ALTER TABLE orders ADD COLUMN realized_pnl DECIMAL(20,8)"},
		{"strategy_name", "ALTER TABLE orders ADD COLUMN strategy_name TEXT DEFAULT ''"},
		{"strategy_type", "ALTER TABLE orders ADD COLUMN strategy_type TEXT DEFAULT ''"},
		{"order_source", "ALTER TABLE orders ADD COLUMN order_source TEXT DEFAULT ''"},
	}

	for _, col := range columns {
		row := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('orders') WHERE name=?`, col.name)
		var count int
		if err := row.Scan(&count); err != nil {
			continue
		}
		if count == 0 {
			if _, err := db.Exec(col.def); err != nil {
				logger.Warn("⚠️ orders 表添加列 %s 失败: %v", col.name, err)
			} else {
				logger.Info("🔄 orders 表成功添加列: %s", col.name)
			}
		}
	}

	if err := ensureOrdersCompositeUniqueConstraint(db); err != nil {
		return err
	}
	return nil
}

func migrateRiskCheckHistoryTable(db *sql.DB) error {
	columns := []struct {
		name string
		def  string
	}{
		{"bot_id", "ALTER TABLE risk_check_history ADD COLUMN bot_id TEXT DEFAULT ''"},
		{"exchange", "ALTER TABLE risk_check_history ADD COLUMN exchange TEXT DEFAULT ''"},
		{"market_type", "ALTER TABLE risk_check_history ADD COLUMN market_type TEXT DEFAULT ''"},
	}
	for _, col := range columns {
		row := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('risk_check_history') WHERE name=?`, col.name)
		var count int
		if err := row.Scan(&count); err != nil {
			continue
		}
		if count == 0 {
			if _, err := db.Exec(col.def); err != nil {
				logger.Warn("⚠️ risk_check_history 表添加列 %s 失败: %v", col.name, err)
			} else {
				logger.Info("🔄 risk_check_history 表成功添加列: %s", col.name)
			}
		}
	}

	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_risk_check_history_bot_id ON risk_check_history(bot_id)`); err != nil {
		logger.Warn("⚠️ 创建 risk_check_history bot_id 索引失败: %v", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_risk_check_history_bot_time ON risk_check_history(bot_id, check_time)`); err != nil {
		logger.Warn("⚠️ 创建 risk_check_history bot_id+check_time 索引失败: %v", err)
	}
	return nil
}

func hasLegacyOrderIDUniqueConstraint(db *sql.DB) (bool, error) {
	rows, err := db.Query(`PRAGMA index_list('orders')`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	type idxInfo struct {
		name    string
		unique  int
		partial int
	}
	var indexes []idxInfo
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return false, err
		}
		indexes = append(indexes, idxInfo{name: name, unique: unique, partial: partial})
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	for _, idx := range indexes {
		if idx.unique == 0 || idx.partial != 0 {
			continue
		}
		infoRows, err := db.Query(fmt.Sprintf(`PRAGMA index_info('%s')`, idx.name))
		if err != nil {
			return false, err
		}
		var cols []string
		for infoRows.Next() {
			var seqno, cid int
			var colName string
			if err := infoRows.Scan(&seqno, &cid, &colName); err != nil {
				infoRows.Close()
				return false, err
			}
			cols = append(cols, colName)
		}
		infoRows.Close()
		if len(cols) == 1 && cols[0] == "order_id" {
			return true, nil
		}
		if len(cols) == 2 && cols[0] == "exchange" && cols[1] == "order_id" {
			return true, nil
		}
	}
	return false, nil
}

// ordersCompositeUniqueIndexName 與 SaveOrder 中 SQLite ON CONFLICT 目標列一致。
const ordersCompositeUniqueIndexName = "idx_orders_exchange_account_symbol_order_id"

// ordersCompositeUniqueIndexMatches 檢查是否存在與 upsert 一致的複合 UNIQUE 索引（列名與順序須完全一致）。
func ordersCompositeUniqueIndexMatches(db *sql.DB) (bool, error) {
	rows, err := db.Query(`PRAGMA index_list('orders')`)
	if err != nil {
		return false, err
	}
	found := false
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			rows.Close()
			return false, err
		}
		if name != ordersCompositeUniqueIndexName {
			continue
		}
		if unique == 0 || partial != 0 {
			rows.Close()
			return false, nil
		}
		found = true
		break
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close() // SQLite 單連接：必須關閉結果集後再發起 PRAGMA index_info，否則死鎖

	if !found {
		return false, nil
	}
	infoRows, err := db.Query(fmt.Sprintf(`PRAGMA index_info('%s')`, ordersCompositeUniqueIndexName))
	if err != nil {
		return false, err
	}
	defer infoRows.Close()
	want := []string{"exchange", "account", "symbol", "order_id"}
	var cols []string
	for infoRows.Next() {
		var seqno, cid int
		var colName string
		if err := infoRows.Scan(&seqno, &cid, &colName); err != nil {
			return false, err
		}
		cols = append(cols, colName)
	}
	if err := infoRows.Err(); err != nil {
		return false, err
	}
	if len(cols) != len(want) {
		return false, nil
	}
	for i := range want {
		if cols[i] != want[i] {
			return false, nil
		}
	}
	return true, nil
}

func rebuildOrdersTableForCompositeUnique(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS orders_v2 (
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
		);
	`); err != nil {
		return err
	}

	if _, err = tx.Exec(`
		INSERT INTO orders_v2 (
			id, order_id, bot_id, account, client_order_id, symbol, side, exchange, type, price, quantity, filled_qty,
			status, realized_pnl, strategy_name, strategy_type, order_source, created_at, updated_at
		)
		SELECT
			id, order_id, COALESCE(bot_id, ''), COALESCE(account, COALESCE(bot_id, '')), client_order_id, symbol, side, COALESCE(exchange, ''), COALESCE(type, ''),
			price, quantity, COALESCE(filled_qty, 0), status, realized_pnl, COALESCE(strategy_name, ''),
			COALESCE(strategy_type, ''), COALESCE(order_source, ''), created_at, updated_at
		FROM orders;
	`); err != nil {
		return err
	}

	if _, err = tx.Exec(`DROP TABLE orders`); err != nil {
		return err
	}
	if _, err = tx.Exec(`ALTER TABLE orders_v2 RENAME TO orders`); err != nil {
		return err
	}
	if _, err = tx.Exec(`DROP INDEX IF EXISTS idx_orders_exchange_order_id`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_exchange_account_symbol_order_id ON orders(exchange, account, symbol, order_id)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_order_id ON orders(order_id)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_bot_id ON orders(bot_id)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_account ON orders(account)`); err != nil {
		return err
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at)`); err != nil {
		return err
	}
	return tx.Commit()
}

func ensureOrdersCompositeUniqueConstraint(db *sql.DB) error {
	legacyUnique, err := hasLegacyOrderIDUniqueConstraint(db)
	if err != nil {
		return err
	}
	if legacyUnique {
		logger.Info("🔄 檢測到舊版 orders 唯一約束，開始遷移為 (exchange, account, symbol, order_id)")
		if err := rebuildOrdersTableForCompositeUnique(db); err != nil {
			return fmt.Errorf("重建 orders 表失败: %w", err)
		}
	}
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_orders_exchange_order_id`); err != nil {
		return err
	}
	ok, err := ordersCompositeUniqueIndexMatches(db)
	if err != nil {
		return err
	}
	if !ok {
		logger.Info("🔄 orders 複合唯一索引缺失或與 ON CONFLICT 列不一致，正在重建 %s", ordersCompositeUniqueIndexName)
		if _, err := db.Exec(fmt.Sprintf(`DROP INDEX IF EXISTS %s`, ordersCompositeUniqueIndexName)); err != nil {
			return err
		}
		if _, err := db.Exec(fmt.Sprintf(
			`CREATE UNIQUE INDEX %s ON orders(exchange, account, symbol, order_id)`,
			ordersCompositeUniqueIndexName,
		)); err != nil {
			return fmt.Errorf("創建 orders 複合唯一索引失败（若表中存在重複的 exchange/account/symbol/order_id 組合，請先清理重複行）: %w", err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_order_id ON orders(order_id)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_bot_id ON orders(bot_id)`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_account ON orders(account)`); err != nil {
		return err
	}
	return nil
}

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

// SavePosition 保存持倉
func (s *SQLStorage) SavePosition(position *Position) error {
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
func (s *SQLStorage) SaveTrade(trade *Trade) error {
	// 轉换為UTC時间存儲
	createdAt := utils.ToUTC(trade.CreatedAt)
	// 确保 exchange 不為空，默认為 binance（兼容舊數據）
	exchange := trade.Exchange
	if exchange == "" {
		exchange = "binance"
	}
	botID := strings.TrimSpace(trade.BotID)
	_, err := s.db.Exec(fmt.Sprintf(`
		INSERT INTO %s
		(buy_order_id, sell_order_id, bot_id, exchange, account, symbol, buy_price, sell_price, quantity, pnl, exchange_pnl, fee, fee_asset, buy_price_deviation, sell_price_deviation, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.tradesTbl()), trade.BuyOrderID, trade.SellOrderID, botID, exchange, trade.Account, trade.Symbol,
		trade.BuyPrice, trade.SellPrice, trade.Quantity, trade.PnL, trade.ExchangePnL, trade.Fee, trade.FeeAsset,
		trade.BuyPriceDeviation, trade.SellPriceDeviation, createdAt)
	if err != nil {
		return err
	}
	// 合规审计：記錄成交事件
	if globalAuditLogger != nil {
		globalAuditLogger.LogTrade(trade)
	}
	return nil
}

// SaveTradeWithDeviation 保存交易記錄（包含價格偏差）
func (s *SQLStorage) SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	trade := &Trade{
		BuyOrderID:         buyOrderID,
		SellOrderID:        sellOrderID,
		BotID:              strings.TrimSpace(botID),
		Exchange:           exchange,
		Symbol:             symbol,
		BuyPrice:           buyPrice,
		SellPrice:          sellPrice,
		Quantity:           quantity,
		PnL:                pnl,
		Fee:                fee,
		FeeAsset:           feeAsset,
		BuyPriceDeviation:  buyPriceDeviation,
		SellPriceDeviation: sellPriceDeviation,
		CreatedAt:          createdAt,
	}
	return s.SaveTrade(trade)
}

// SaveTradeWithExchangePnL 保存交易記錄（包含交易所盈虧和價格偏差）
func (s *SQLStorage) SaveTradeWithExchangePnL(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	trade := &Trade{
		BuyOrderID:         buyOrderID,
		SellOrderID:        sellOrderID,
		BotID:              strings.TrimSpace(botID),
		Exchange:           exchange,
		Symbol:             symbol,
		BuyPrice:           buyPrice,
		SellPrice:          sellPrice,
		Quantity:           quantity,
		PnL:                pnl,
		ExchangePnL:        exchangePnL,
		Fee:                fee,
		FeeAsset:           feeAsset,
		BuyPriceDeviation:  buyPriceDeviation,
		SellPriceDeviation: sellPriceDeviation,
		CreatedAt:          createdAt,
	}
	return s.SaveTrade(trade)
}

// SaveSystemMetrics 保存系统監控细粒度數據
func (s *SQLStorage) SaveSystemMetrics(metrics *SystemMetrics) error {
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
func (s *SQLStorage) SaveDailySystemMetrics(metrics *DailySystemMetrics) error {
	// 轉换為UTC時间存儲
	date := utils.ToUTC(metrics.Date)
	if s.dbType == "mysql" {
		_, err := s.db.Exec(`
			INSERT INTO daily_system_metrics
			(date, avg_cpu_percent, max_cpu_percent, min_cpu_percent,
			 avg_memory_mb, max_memory_mb, min_memory_mb, sample_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				avg_cpu_percent = VALUES(avg_cpu_percent),
				max_cpu_percent = VALUES(max_cpu_percent),
				min_cpu_percent = VALUES(min_cpu_percent),
				avg_memory_mb = VALUES(avg_memory_mb),
				max_memory_mb = VALUES(max_memory_mb),
				min_memory_mb = VALUES(min_memory_mb),
				sample_count = VALUES(sample_count)
		`, date, metrics.AvgCPUPercent, metrics.MaxCPUPercent, metrics.MinCPUPercent,
			metrics.AvgMemoryMB, metrics.MaxMemoryMB, metrics.MinMemoryMB, metrics.SampleCount)
		return err
	}
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
func (s *SQLStorage) SaveEvent(eventType string, data map[string]interface{}) error {
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
func (s *SQLStorage) saveSystemMetricsFromMap(data map[string]interface{}) error {
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
func (s *SQLStorage) SaveStatistics(stats *Statistics) error {
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

// QueryPositions 查詢持倉历史
func (s *SQLStorage) QueryPositions(limit, offset int) ([]*Position, error) {
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
func (s *SQLStorage) QueryTrades(startTime, endTime time.Time, limit, offset int) ([]*Trade, error) {
	// 限制最大返回數量，防止記憶體占用過大
	maxLimit := 10000 // 最多返回1万条交易
	if limit <= 0 {
		limit = 100 // 預設 100条
	}
	if limit > maxLimit {
		limit = maxLimit
		logger.Warn("⚠️ 交易查詢 limit 超過限制 (%d)，已限制為 %d", limit, maxLimit)
	}

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT buy_order_id, sell_order_id, exchange, account, symbol, buy_price, sell_price, quantity, pnl, COALESCE(fee, 0) as fee, created_at
		FROM %s
		WHERE created_at >= ? AND created_at <= ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, s.tradesTbl()), startTime, endTime, limit, offset)
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
			&trade.Fee,
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

// GetTradesBySellOrderIDs 根據賣單 ID 查詢對應的成交盈虧，返回 sell_order_id -> pnl 的映射
func (s *SQLStorage) GetTradesBySellOrderIDs(sellOrderIDs []int64) (map[int64]float64, error) {
	result := make(map[int64]float64)
	if len(sellOrderIDs) == 0 {
		return result, nil
	}
	// 構建 IN 子句的佔位符
	placeholders := ""
	args := make([]interface{}, 0, len(sellOrderIDs))
	for i, id := range sellOrderIDs {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, id)
	}
	query := fmt.Sprintf(`
		SELECT sell_order_id, pnl FROM %s WHERE sell_order_id IN (%s)
	`, s.tradesTbl(), placeholders)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢賣單盈虧失败: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sellOrderID int64
		var pnl float64
		if err := rows.Scan(&sellOrderID, &pnl); err != nil {
			continue
		}
		result[sellOrderID] = pnl
	}
	return result, rows.Err()
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
	tzOffsetSeconds := utils.GetTimezoneOffsetSeconds()
	tzModifier := fmt.Sprintf("%+d seconds", tzOffsetSeconds)
	query := fmt.Sprintf(`
		SELECT date(datetime(created_at, '%s')) as dt, COALESCE(SUM(realized_pnl), 0) as total
		FROM orders
		WHERE realized_pnl IS NOT NULL AND status = 'FILLED'
			AND date(datetime(created_at, '%s')) >= ? AND date(datetime(created_at, '%s')) <= ?
	`, tzModifier, tzModifier, tzModifier)
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
	query += fmt.Sprintf(" GROUP BY date(datetime(created_at, '%s'))", tzModifier)

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
			continue
		}
		result[dt] = total
	}
	return result, rows.Err()
}

// GetDailyTradesSummary 獲取指定日（配置時區）的成交筆數、毛利、手續費
func (s *SQLStorage) GetDailyTradesSummary(exchange, account, dateStr, botID string) (count int, grossPnl, totalFee float64, err error) {
	tzOffsetSeconds := utils.GetTimezoneOffsetSeconds()
	tzModifier := fmt.Sprintf("%+d seconds", tzOffsetSeconds)
	query := fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(SUM(pnl), 0), COALESCE(SUM(COALESCE(fee, 0)), 0)
		FROM %s
		WHERE date(datetime(created_at, '%s')) = ?
	`, s.tradesTbl(), tzModifier)
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

	// 獲取配置時区的偏移秒數（用於將 UTC 時間轉換為本地時間後再按日期分組）
	// 例如：Asia/Shanghai 為 +28800 秒（8小時）
	// SQLite 的 datetime(created_at, '+N seconds') 可將 UTC 時間轉換為本地時間
	tzOffsetSeconds := utils.GetTimezoneOffsetSeconds()
	tzModifier := fmt.Sprintf("%+d seconds", tzOffsetSeconds)

	query := fmt.Sprintf(`
		SELECT 
			date(datetime(created_at, '%s')) as date,
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
		WHERE date(datetime(created_at, '%s')) >= ? AND date(datetime(created_at, '%s')) <= ?
	`, tzModifier, s.tradesTbl(), tzModifier, tzModifier)
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
	query += fmt.Sprintf(" GROUP BY date(datetime(created_at, '%s')) ORDER BY date DESC LIMIT ?", tzModifier)
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


// Close 关闭數據库连接
func (s *SQLStorage) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}





