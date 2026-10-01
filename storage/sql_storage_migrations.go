package storage

import (
	"database/sql"
	"fmt"

	"quantmesh/logger"
)

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
			market_type TEXT NOT NULL DEFAULT '',
			account_scope TEXT NOT NULL DEFAULT '',
			income_type TEXT NOT NULL,
			income DECIMAL(20,8) NOT NULL,
			asset TEXT,
			info TEXT,
			transaction_id BIGINT,
			trade_time TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			identity_key TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_funding_payments_exchange_symbol ON funding_payments(exchange, symbol);
		CREATE INDEX IF NOT EXISTS idx_funding_payments_trade_time ON funding_payments(trade_time);
		CREATE INDEX IF NOT EXISTS idx_funding_payments_account ON funding_payments(account);
	`)
	if err != nil {
		return err
	}
	for _, column := range []struct{ name, ddl string }{
		{"market_type", `ALTER TABLE funding_payments ADD COLUMN market_type TEXT NOT NULL DEFAULT ''`},
		{"account_scope", `ALTER TABLE funding_payments ADD COLUMN account_scope TEXT NOT NULL DEFAULT ''`},
		{"identity_key", `ALTER TABLE funding_payments ADD COLUMN identity_key TEXT`},
	} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('funding_payments') WHERE name = ?`, column.name).Scan(&count); err != nil {
			return fmt.Errorf("检查 funding_payments.%s 字段失败: %w", column.name, err)
		}
		if count == 0 {
			if _, err := db.Exec(column.ddl); err != nil {
				return fmt.Errorf("添加 funding_payments.%s 字段失败: %w", column.name, err)
			}
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_funding_payments_scope_market_symbol_time ON funding_payments(account_scope, exchange, market_type, symbol, trade_time)`); err != nil {
		return fmt.Errorf("创建 funding_payments 作用域索引失败: %w", err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uk_funding_payments_identity ON funding_payments(identity_key) WHERE identity_key IS NOT NULL`); err != nil {
		return fmt.Errorf("创建 funding_payments 幂等索引失败: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS funding_income_sync_state (
			scope_key TEXT PRIMARY KEY,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			market_type TEXT NOT NULL,
			account_scope TEXT NOT NULL,
			covered_from TIMESTAMP NOT NULL,
			covered_through TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return fmt.Errorf("创建 funding_income_sync_state 表失败: %w", err)
	}
	return nil
}

func migrateMarginInterestTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS margin_interest_payments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange TEXT NOT NULL,
			account TEXT,
			account_scope TEXT NOT NULL,
			asset TEXT NOT NULL,
			raw_asset TEXT NOT NULL DEFAULT '',
			principal DECIMAL(30,12) NOT NULL,
			interest DECIMAL(30,12) NOT NULL,
			interest_rate DECIMAL(30,16) NOT NULL,
			interest_type TEXT NOT NULL,
			isolated_symbol TEXT NOT NULL DEFAULT '',
			transaction_id BIGINT NOT NULL,
			accrued_at TIMESTAMP NOT NULL,
			identity_key TEXT NOT NULL UNIQUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_margin_interest_scope_asset_time
			ON margin_interest_payments(account_scope, exchange, asset, accrued_at);
		CREATE TABLE IF NOT EXISTS margin_interest_sync_state (
			scope_key TEXT PRIMARY KEY,
			exchange TEXT NOT NULL,
			account_scope TEXT NOT NULL,
			asset TEXT NOT NULL,
			covered_from TIMESTAMP NOT NULL,
			covered_through TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS margin_interest_allocations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange TEXT NOT NULL,
			account_scope TEXT NOT NULL,
			transaction_id BIGINT NOT NULL,
			bot_id TEXT NOT NULL,
			asset TEXT NOT NULL,
			raw_asset TEXT NOT NULL,
			bot_principal DECIMAL(30,12) NOT NULL,
			account_principal DECIMAL(30,12) NOT NULL,
			interest DECIMAL(30,12) NOT NULL,
			accrued_at TIMESTAMP NOT NULL,
			identity_key TEXT NOT NULL UNIQUE,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_margin_interest_allocations_bot_time
			ON margin_interest_allocations(account_scope, exchange, bot_id, accrued_at);
		`)
	if err != nil {
		return err
	}
	var columnCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('margin_interest_payments') WHERE name = 'raw_asset'`).Scan(&columnCount); err != nil {
		return fmt.Errorf("check margin_interest_payments.raw_asset migration: %w", err)
	}
	if columnCount == 0 {
		if _, err := db.Exec(`ALTER TABLE margin_interest_payments ADD COLUMN raw_asset TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add margin_interest_payments.raw_asset: %w", err)
		}
	}
	return nil
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
			market_type TEXT NOT NULL DEFAULT '',
			account_scope TEXT NOT NULL DEFAULT '',
			symbol TEXT NOT NULL,
			account TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			equity REAL NOT NULL,
			unrealized_pnl REAL NOT NULL,
			unrealized_pnl_asset TEXT NOT NULL DEFAULT '',
			total_position_value REAL NOT NULL,
			market_price REAL NOT NULL DEFAULT 0,
			spot_position_qty REAL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_hourly_equity_exchange_symbol_account ON hourly_equity_records(exchange, symbol, account);
		CREATE INDEX IF NOT EXISTS idx_hourly_equity_timestamp ON hourly_equity_records(timestamp);
		CREATE TABLE IF NOT EXISTS daily_snapshots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange TEXT NOT NULL,
			market_type TEXT NOT NULL DEFAULT '',
			account_scope TEXT NOT NULL DEFAULT '',
			symbol TEXT NOT NULL,
			account TEXT NOT NULL,
			date DATE NOT NULL,
			unrealized_pnl REAL NOT NULL,
			unrealized_pnl_asset TEXT NOT NULL DEFAULT '',
			total_position_value REAL NOT NULL,
			spot_position_qty REAL,
			intraday_max_drawdown REAL NOT NULL,
			intraday_max_drawdown_pct REAL NOT NULL,
			intraday_peak_equity REAL NOT NULL,
			closing_price REAL NOT NULL,
			snapshot_time TIMESTAMP NOT NULL,
			account_equity REAL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(exchange, market_type, account_scope, symbol, date)
		);
		CREATE INDEX IF NOT EXISTS idx_daily_snapshots_exchange_symbol_account ON daily_snapshots(exchange, symbol, account);
		CREATE INDEX IF NOT EXISTS idx_daily_snapshots_date ON daily_snapshots(date);
		CREATE TABLE IF NOT EXISTS account_equity_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange TEXT NOT NULL,
			market_type TEXT NOT NULL DEFAULT '',
			account_scope TEXT NOT NULL DEFAULT '',
			account TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			account_equity REAL NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(exchange, market_type, account_scope, timestamp)
		);
	`)
	if err != nil {
		return err
	}
	if err := migrateAccountEquityMarketTypeSQLite(db); err != nil {
		return err
	}
	if err := migrateMarketTypeSnapshotColumnsSQLite(db); err != nil {
		return err
	}
	for _, column := range []struct{ table, name, definition string }{{"hourly_equity_records", "market_price", "market_price REAL NOT NULL DEFAULT 0"}, {"hourly_equity_records", "spot_position_qty", "spot_position_qty REAL"}, {"hourly_equity_records", "unrealized_pnl_asset", "unrealized_pnl_asset TEXT NOT NULL DEFAULT ''"}, {"daily_snapshots", "spot_position_qty", "spot_position_qty REAL"}, {"daily_snapshots", "unrealized_pnl_asset", "unrealized_pnl_asset TEXT NOT NULL DEFAULT ''"}} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('`+column.table+`') WHERE name = ?`, column.name).Scan(&count); err != nil {
			return fmt.Errorf("check %s.%s migration: %w", column.table, column.name, err)
		}
		if count == 0 {
			if _, err := db.Exec(`ALTER TABLE ` + column.table + ` ADD COLUMN ` + column.definition); err != nil {
				return fmt.Errorf("migrate %s.%s: %w", column.table, column.name, err)
			}
		}
	}
	return nil
}

func migrateAccountEquityMarketTypeSQLite(db *sql.DB) error {
	var hasMarketType, hasAccountScope int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('account_equity_records') WHERE name = 'market_type'`).Scan(&hasMarketType); err != nil {
		return fmt.Errorf("检查 account_equity_records.market_type 失败: %w", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('account_equity_records') WHERE name = 'account_scope'`).Scan(&hasAccountScope); err != nil {
		return fmt.Errorf("检查 account_equity_records.account_scope 失败: %w", err)
	}
	if hasMarketType > 0 && hasAccountScope > 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("开始账户权益维度迁移失败: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`ALTER TABLE account_equity_records RENAME TO account_equity_records_legacy`); err != nil {
		return fmt.Errorf("重命名旧账户权益表失败: %w", err)
	}
	marketTypeSelect := `''`
	if hasMarketType > 0 {
		marketTypeSelect = `market_type`
	}
	if _, err := tx.Exec(`CREATE TABLE account_equity_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT, exchange TEXT NOT NULL, market_type TEXT NOT NULL DEFAULT '', account_scope TEXT NOT NULL DEFAULT '',
		account TEXT NOT NULL, timestamp DATETIME NOT NULL, account_equity REAL NOT NULL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(exchange, market_type, account_scope, timestamp))`); err != nil {
		return fmt.Errorf("创建作用域隔离账户权益表失败: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO account_equity_records(id, exchange, market_type, account_scope, account, timestamp, account_equity, created_at)
		SELECT id, exchange, ` + marketTypeSelect + `, 'legacy:' || account, account, timestamp, account_equity, created_at FROM account_equity_records_legacy`); err != nil {
		return fmt.Errorf("复制旧账户权益样本失败: %w", err)
	}
	if _, err := tx.Exec(`DROP TABLE account_equity_records_legacy`); err != nil {
		return fmt.Errorf("删除旧账户权益表失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交账户权益维度迁移失败: %w", err)
	}
	return nil
}

// migrateMarketTypeSnapshotColumnsSQLite keeps legacy snapshots explicitly unclassified,
// while rebuilding the daily key so one account/symbol/date can store each market separately.
func migrateMarketTypeSnapshotColumnsSQLite(db *sql.DB) error {
	for _, column := range []string{"market_type", "account_scope", "unrealized_pnl_asset"} {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('hourly_equity_records') WHERE name = ?`, column).Scan(&exists); err != nil {
			return fmt.Errorf("检查 hourly_equity_records.%s 失败: %w", column, err)
		}
		if exists == 0 {
			if _, err := db.Exec(`ALTER TABLE hourly_equity_records ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("添加 hourly_equity_records.%s 失败: %w", column, err)
			}
		}
	}
	var marketTypeCount, accountScopeCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('daily_snapshots') WHERE name = 'market_type'`).Scan(&marketTypeCount); err != nil {
		return fmt.Errorf("检查 daily_snapshots.market_type 失败: %w", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('daily_snapshots') WHERE name = 'account_scope'`).Scan(&accountScopeCount); err != nil {
		return fmt.Errorf("检查 daily_snapshots.account_scope 失败: %w", err)
	}
	if marketTypeCount > 0 && accountScopeCount > 0 {
		var assetCount int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('daily_snapshots') WHERE name = 'unrealized_pnl_asset'`).Scan(&assetCount); err != nil {
			return fmt.Errorf("检查 daily_snapshots.unrealized_pnl_asset 失败: %w", err)
		}
		if assetCount == 0 {
			if _, err := db.Exec(`ALTER TABLE daily_snapshots ADD COLUMN unrealized_pnl_asset TEXT NOT NULL DEFAULT ''`); err != nil {
				return fmt.Errorf("添加 daily_snapshots.unrealized_pnl_asset 失败: %w", err)
			}
		}
		return nil
	}
	var accountEquityCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('daily_snapshots') WHERE name = 'account_equity'`).Scan(&accountEquityCount); err != nil {
		return fmt.Errorf("检查 daily_snapshots.account_equity 失败: %w", err)
	}
	accountEquitySelect := "NULL"
	if accountEquityCount > 0 {
		accountEquitySelect = "account_equity"
	}
	marketTypeSelect := `''`
	if marketTypeCount > 0 {
		marketTypeSelect = `market_type`
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("開始快照維度迁移失败: %w", err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`ALTER TABLE daily_snapshots RENAME TO daily_snapshots_market_legacy`); err != nil {
		return fmt.Errorf("保留旧日快照表失败: %w", err)
	}
	if _, err = tx.Exec(`CREATE TABLE daily_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT, exchange TEXT NOT NULL, market_type TEXT NOT NULL DEFAULT '', account_scope TEXT NOT NULL DEFAULT '',
		symbol TEXT NOT NULL, account TEXT NOT NULL, date DATE NOT NULL, unrealized_pnl REAL NOT NULL,
		unrealized_pnl_asset TEXT NOT NULL DEFAULT '', total_position_value REAL NOT NULL, intraday_max_drawdown REAL NOT NULL,
		intraday_max_drawdown_pct REAL NOT NULL, intraday_peak_equity REAL NOT NULL,
		closing_price REAL NOT NULL, snapshot_time TIMESTAMP NOT NULL, account_equity REAL, spot_position_qty REAL,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(exchange, market_type, account_scope, symbol, date))`); err != nil {
		return fmt.Errorf("創建帶市場維度的日快照表失败: %w", err)
	}
	copySQL := `INSERT INTO daily_snapshots (id, exchange, market_type, account_scope, symbol, account, date, unrealized_pnl, total_position_value,
		intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, account_equity, created_at)
		SELECT id, exchange, ` + marketTypeSelect + `, 'legacy:' || account, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown,
		intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, ` + accountEquitySelect + `, created_at
		FROM daily_snapshots_market_legacy`
	if _, err = tx.Exec(copySQL); err != nil {
		return fmt.Errorf("複製舊日快照失败: %w", err)
	}
	if _, err = tx.Exec(`DROP TABLE daily_snapshots_market_legacy`); err != nil {
		return fmt.Errorf("清理舊日快照表失败: %w", err)
	}
	if _, err = tx.Exec(`CREATE INDEX IF NOT EXISTS idx_daily_snapshots_exchange_symbol_account ON daily_snapshots(exchange, symbol, account);
		CREATE INDEX IF NOT EXISTS idx_daily_snapshots_date ON daily_snapshots(date)`); err != nil {
		return fmt.Errorf("重建日快照索引失败: %w", err)
	}
	return tx.Commit()
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
			account_scope TEXT NOT NULL DEFAULT '',
			claim_id TEXT NOT NULL DEFAULT '',
			claim_started_at TIMESTAMP,
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

func migrateProfitWithdrawRulesAccountScope(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('profit_withdraw_rules') WHERE name='account_scope'`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		_, err := db.Exec(`ALTER TABLE profit_withdraw_rules ADD COLUMN account_scope TEXT NOT NULL DEFAULT ''`)
		return err
	}
	return nil
}

func migrateProfitWithdrawRulesClaimID(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('profit_withdraw_rules') WHERE name='claim_id'`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		_, err := db.Exec(`ALTER TABLE profit_withdraw_rules ADD COLUMN claim_id TEXT NOT NULL DEFAULT ''`)
		return err
	}
	return nil
}

func migrateProfitWithdrawRulesClaimStartedAt(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('profit_withdraw_rules') WHERE name='claim_started_at'`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		_, err := db.Exec(`ALTER TABLE profit_withdraw_rules ADD COLUMN claim_started_at TIMESTAMP`)
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
			account_scope TEXT NOT NULL DEFAULT '',
			claim_id TEXT NOT NULL DEFAULT '',
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
	if err != nil {
		return err
	}
	for _, column := range []string{"account_scope", "claim_id"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('profit_withdraw_records') WHERE name = ?`, column).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := db.Exec(`ALTER TABLE profit_withdraw_records ADD COLUMN ` + column + ` TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
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
	for _, column := range []struct{ name, ddl string }{
		{"account_scope", "TEXT NOT NULL DEFAULT ''"},
		{"market_type", "TEXT NOT NULL DEFAULT ''"},
		{"bot_id", "TEXT NOT NULL DEFAULT ''"},
	} {
		var exists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('reconciliation_history') WHERE name = ?`, column.name).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			if _, err := db.Exec(`ALTER TABLE reconciliation_history ADD COLUMN ` + column.name + ` ` + column.ddl); err != nil {
				return fmt.Errorf("add reconciliation_history.%s: %w", column.name, err)
			}
		}
	}
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
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_reconciliation_history_scope ON reconciliation_history(exchange, market_type, symbol, account_scope, bot_id, reconcile_time)`)
	if err != nil {
		return fmt.Errorf("create reconciliation history scoped index: %w", err)
	}

	return nil
}
