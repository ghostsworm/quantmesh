package storage

import (
	"database/sql"
	"fmt"
	"strings"

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

// dateExprInConfiguredTimezone 返回按配置時區取日期的 SQL 表達式。
// SQLite 支援 datetime(col, '+N seconds')；MySQL/MariaDB 需使用 DATE_ADD。
func (s *SQLStorage) dateExprInConfiguredTimezone(column string) string {
	tzOffsetSeconds := utils.GetTimezoneOffsetSeconds()
	if s != nil && s.dbType == "mysql" {
		return fmt.Sprintf("DATE(DATE_ADD(%s, INTERVAL %d SECOND))", column, tzOffsetSeconds)
	}
	tzModifier := fmt.Sprintf("%+d seconds", tzOffsetSeconds)
	return fmt.Sprintf("date(datetime(%s, '%s'))", column, tzModifier)
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
		if err := migrateOrderFillsTable(db, false); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 order_fills 表失败: %w", err)
		}

		// orders.bot_id 是历史成交归属回填的数据源，必须先迁移 orders。
		if err := migrateOrdersTable(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 orders 表失败: %w", err)
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
		if err := migrateTradesMarketType(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 trades market_type 字段失败: %w", err)
		}
		if err := migrateTradesAccountScope(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 trades account_scope 字段失败: %w", err)
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
		if err := migrateStrategyRuntimeStateTableMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 strategy_runtime_states 表失败: %w", err)
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
		if err := migrateOrdersTableMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL orders 表失败: %w", err)
		}
		// orders 必须先就绪，再回填配对成交的 bot_id；否则旧顺序会静默漏数据。
		if err := backfillTradesBotIDFromOrders(db, pairedTradesTableMySQL); err != nil {
			db.Close()
			return nil, fmt.Errorf("回填 MySQL 网格配对成交 bot_id 失败: %w", err)
		}
		if err := migrateOrderFillsTable(db, true); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL order_fills 表失败: %w", err)
		}
		if err := migrateStatisticsTableMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL statistics 表失败: %w", err)
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
		if err := migrateKlineFilesTableMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL kline_files 表失败: %w", err)
		}
		if err := migrateProtectedKlineFilesTableMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL protected_kline_files 表失败: %w", err)
		}
		if err := migrateSystemSettingsTableMySQL(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("迁移 MySQL system_settings 表失败: %w", err)
		}
		// SQLite createTables / migrate*Table 已建但 MySQL 路徑歷史漏掉的 17 張表，
		// 按 SQLite 中出現的順序補齊；觸發任何相關功能（對賬/風控歷史/資金費/AI 提示/基差/
		// 利潤提取/巡檢/市場解讀/權益快照/回測/參數優化/新聞分析/價格快照/預測校驗）前必須就緒。
		mysqlExtraMigrations := []struct {
			name string
			fn   func(*sql.DB) error
		}{
			{"reconciliation_history", migrateReconciliationHistoryTableMySQL},
			{"risk_check_history", migrateRiskCheckHistoryTableMySQL},
			{"funding_rates", migrateFundingRatesTableMySQL},
			{"ai_prompts", migrateAIPromptsTableMySQL},
			{"basis_data", migrateBasisDataTableMySQL},
			{"profit_withdraw_rules", migrateProfitWithdrawRulesTableMySQL},
			{"profit_withdraw_records", migrateProfitWithdrawRecordsTableMySQL},
			{"inspection_reports", migrateInspectionReportsTableMySQL},
			{"funding_payments", migrateFundingPaymentsTableMySQL},
			{"market_interpret_tasks", migrateMarketInterpretTasksTableMySQL},
			{"hourly_equity_records", migrateHourlyEquityRecordsTableMySQL},
			{"daily_snapshots", migrateDailySnapshotsTableMySQL},
			{"account_equity_records", migrateAccountEquityRecordsTableMySQL},
			{"backtest_tasks", migrateBacktestTasksTableMySQL},
			{"optim_tasks", migrateOptimTasksTableMySQL},
			{"news_analysis_history", migrateNewsAnalysisHistoryTableMySQL},
			{"price_history", migratePriceHistoryTableMySQL},
			{"prediction_verification", migratePredictionVerificationTableMySQL},
			{"positions", migratePositionsTableMySQL},
			{"fix_session_states/fix_order_links", migrateFixTablesMySQL},
		}
		for _, m := range mysqlExtraMigrations {
			if err := m.fn(db); err != nil {
				db.Close()
				return nil, fmt.Errorf("迁移 MySQL %s 表失败: %w", m.name, err)
			}
		}
	}

	tradesTbl := "trades"
	if dbType == "mysql" {
		tradesTbl = pairedTradesTableMySQL
	}
	return &SQLStorage{db: db, dbType: dbType, tradesTable: tradesTbl}, nil
}

// Close 关闭數據库连接
func (s *SQLStorage) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}
