package storage

import (
	"database/sql"
	"fmt"
	"time"

	"quantmesh/logger"
)

// ========== Bot 启停状态 + MySQL 关联表迁移 ==========

// migrateBotStatesTable 遷移 Bot 啟停狀態表（SQLite）
func migrateBotStatesTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS bot_states (
			bot_id TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 1,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_by TEXT,
			reason TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_bot_states_enabled ON bot_states(enabled);
		CREATE INDEX IF NOT EXISTS idx_bot_states_updated_at ON bot_states(updated_at);
	`)
	return err
}

// migrateSystemMetricsTablesMySQL 創建系統監控表（SQLite 在 createTables 中建表；MySQL 路徑不跑 createTables，須單獨遷移）
func migrateSystemMetricsTablesMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS system_metrics (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  timestamp DATETIME(3) NOT NULL,
  cpu_percent DOUBLE NOT NULL,
  memory_mb DOUBLE NOT NULL,
  memory_percent DOUBLE NULL,
  process_id BIGINT NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_system_metrics_timestamp (timestamp)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS daily_system_metrics (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  date DATE NOT NULL,
  avg_cpu_percent DOUBLE NOT NULL,
  max_cpu_percent DOUBLE NOT NULL,
  min_cpu_percent DOUBLE NOT NULL,
  avg_memory_mb DOUBLE NOT NULL,
  max_memory_mb DOUBLE NOT NULL,
  min_memory_mb DOUBLE NOT NULL,
  sample_count INT NOT NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_daily_system_metrics_date (date),
  KEY idx_daily_system_metrics_date (date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL system_metrics / daily_system_metrics 表已就緒")
	return nil
}

// migrateBotRiskControlEventsMySQL 創建 Bot 開倉風控事件表（SQLite 在 createTables 中建表）
func migrateBotRiskControlEventsMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS bot_risk_control_events (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  bot_id VARCHAR(128) NOT NULL,
  event_type VARCHAR(32) NOT NULL,
  reason TEXT,
  source VARCHAR(64) DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  KEY idx_bot_rce_bot_time (bot_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL bot_risk_control_events 表已就緒")
	return nil
}

// migratePairedTradesTableMySQL 創建網格買賣配對成交表（與 GORM trades 分表，列名含 pnl）
func migratePairedTradesTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS ` + pairedTradesTableMySQL + ` (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  buy_order_id BIGINT,
  sell_order_id BIGINT,
  bot_id VARCHAR(128) DEFAULT '',
  exchange VARCHAR(64) DEFAULT 'binance',
  account VARCHAR(255) DEFAULT '',
  symbol VARCHAR(64),
  buy_price DECIMAL(20,8),
  sell_price DECIMAL(20,8),
  quantity DECIMAL(20,8),
  pnl DECIMAL(20,8) DEFAULT 0,
  exchange_pnl DECIMAL(20,8) DEFAULT 0,
  fee DECIMAL(20,8) DEFAULT 0,
  fee_asset VARCHAR(32) DEFAULT '',
  buy_price_deviation DECIMAL(20,8) DEFAULT 0,
  sell_price_deviation DECIMAL(20,8) DEFAULT 0,
  created_at TIMESTAMP(3) NULL,
  KEY idx_qm_pt_created_at (created_at),
  KEY idx_qm_pt_account_symbol (account(64), symbol(32)),
  KEY idx_qm_pt_exchange_symbol (exchange(32), symbol(32)),
  KEY idx_qm_pt_bot_ex_sym (bot_id(64), exchange(32), symbol(32))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL 網格配對成交表已就緒: %s", pairedTradesTableMySQL)
	return nil
}

// migratePairedTradesBotIDMySQL 為已有 MySQL 網格配對表補充 bot_id 列並回填
func migratePairedTradesBotIDMySQL(db *sql.DB) error {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = 'bot_id'
	`, pairedTradesTableMySQL).Scan(&n)
	if err != nil {
		return fmt.Errorf("检查 %s.bot_id 列: %w", pairedTradesTableMySQL, err)
	}
	if n == 0 {
		_, err := db.Exec(`ALTER TABLE ` + pairedTradesTableMySQL + ` ADD COLUMN bot_id VARCHAR(128) DEFAULT '' NOT NULL`)
		if err != nil {
			return fmt.Errorf("添加 %s.bot_id: %w", pairedTradesTableMySQL, err)
		}
		logger.Info("✅ MySQL %s 已添加 bot_id 列", pairedTradesTableMySQL)
	}
	_, _ = db.Exec(`CREATE INDEX idx_qm_pt_bot_ex_sym ON ` + pairedTradesTableMySQL + ` (bot_id(64), exchange(32), symbol(32))`)
	backfillTradesBotIDFromOrders(db, pairedTradesTableMySQL)
	return nil
}

// migrateOrdersTableMySQL 補齊歷史 MySQL orders 表缺失列。
// 部署庫中 orders 可能由 GORM 舊模型創建，缺少 storage 層查詢依賴的 account/bot_id 等列。
func migrateOrdersTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS orders (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  order_id BIGINT,
  bot_id VARCHAR(128) DEFAULT '',
  account VARCHAR(255) DEFAULT '',
  client_order_id VARCHAR(255),
  symbol VARCHAR(64),
  side VARCHAR(16),
  exchange VARCHAR(64) DEFAULT '',
  ` + "`type`" + ` VARCHAR(32) DEFAULT '',
  price DECIMAL(20,8),
  quantity DECIMAL(20,8),
  filled_qty DECIMAL(20,8) DEFAULT 0,
  status VARCHAR(64),
  realized_pnl DECIMAL(20,8),
  strategy_name VARCHAR(128) DEFAULT '',
  strategy_type VARCHAR(64) DEFAULT '',
  order_source VARCHAR(64) DEFAULT '',
  created_at TIMESTAMP(3) NULL,
  updated_at TIMESTAMP(3) NULL,
  KEY idx_orders_order_id (order_id),
  KEY idx_orders_created_at (created_at),
  KEY idx_orders_bot_id (bot_id),
  KEY idx_orders_account (account),
  KEY idx_orders_exchange_symbol (exchange, symbol)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}

	columns := []struct {
		name string
		def  string
	}{
		{"bot_id", "ALTER TABLE orders ADD COLUMN bot_id VARCHAR(128) DEFAULT ''"},
		{"account", "ALTER TABLE orders ADD COLUMN account VARCHAR(255) DEFAULT ''"},
		{"client_order_id", "ALTER TABLE orders ADD COLUMN client_order_id VARCHAR(255)"},
		{"exchange", "ALTER TABLE orders ADD COLUMN exchange VARCHAR(64) DEFAULT ''"},
		{"type", "ALTER TABLE orders ADD COLUMN `type` VARCHAR(32) DEFAULT ''"},
		{"filled_qty", "ALTER TABLE orders ADD COLUMN filled_qty DECIMAL(20,8) DEFAULT 0"},
		{"realized_pnl", "ALTER TABLE orders ADD COLUMN realized_pnl DECIMAL(20,8)"},
		{"strategy_name", "ALTER TABLE orders ADD COLUMN strategy_name VARCHAR(128) DEFAULT ''"},
		{"strategy_type", "ALTER TABLE orders ADD COLUMN strategy_type VARCHAR(64) DEFAULT ''"},
		{"order_source", "ALTER TABLE orders ADD COLUMN order_source VARCHAR(64) DEFAULT ''"},
		{"created_at", "ALTER TABLE orders ADD COLUMN created_at TIMESTAMP(3) NULL"},
		{"updated_at", "ALTER TABLE orders ADD COLUMN updated_at TIMESTAMP(3) NULL"},
	}
	for _, col := range columns {
		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'orders' AND COLUMN_NAME = ?
		`, col.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := db.Exec(col.def); err != nil {
				return err
			}
			logger.Info("🔄 MySQL orders 表成功添加列: %s", col.name)
		}
	}

	indexes := []struct {
		name string
		stmt string
	}{
		{"idx_orders_order_id", "CREATE INDEX idx_orders_order_id ON orders(order_id)"},
		{"idx_orders_created_at", "CREATE INDEX idx_orders_created_at ON orders(created_at)"},
		{"idx_orders_bot_id", "CREATE INDEX idx_orders_bot_id ON orders(bot_id)"},
		{"idx_orders_account", "CREATE INDEX idx_orders_account ON orders(account)"},
		{"idx_orders_exchange_symbol", "CREATE INDEX idx_orders_exchange_symbol ON orders(exchange, symbol)"},
	}
	for _, idx := range indexes {
		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'orders' AND INDEX_NAME = ?
		`, idx.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := db.Exec(idx.stmt); err != nil {
				return err
			}
		}
	}
	logger.Info("✅ MySQL orders 表已就緒")
	return nil
}

// migrateStatisticsTableMySQL 補齊歷史 MySQL statistics 表與 storage 層查詢字段的差異。
// 舊 GORM 表使用 trade_count/volume/total_pn_l；storage 層使用 total_trades/total_volume/total_pnl。
func migrateStatisticsTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS statistics (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  date DATETIME(3) NULL,
  total_trades BIGINT DEFAULT 0,
  total_volume DECIMAL(20,8) DEFAULT 0,
  total_pnl DECIMAL(20,8) DEFAULT 0,
  win_rate DECIMAL(10,4) DEFAULT 0,
  created_at TIMESTAMP(3) NULL,
  KEY idx_statistics_date (date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}

	columns := []struct {
		name string
		def  string
	}{
		{"date", "ALTER TABLE statistics ADD COLUMN date DATETIME(3) NULL"},
		{"total_trades", "ALTER TABLE statistics ADD COLUMN total_trades BIGINT DEFAULT 0"},
		{"total_volume", "ALTER TABLE statistics ADD COLUMN total_volume DECIMAL(20,8) DEFAULT 0"},
		{"total_pnl", "ALTER TABLE statistics ADD COLUMN total_pnl DECIMAL(20,8) DEFAULT 0"},
		{"win_rate", "ALTER TABLE statistics ADD COLUMN win_rate DECIMAL(10,4) DEFAULT 0"},
		{"created_at", "ALTER TABLE statistics ADD COLUMN created_at TIMESTAMP(3) NULL"},
	}
	for _, col := range columns {
		var count int
		if err := db.QueryRow(`
			SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'statistics' AND COLUMN_NAME = ?
		`, col.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := db.Exec(col.def); err != nil {
				return err
			}
			logger.Info("🔄 MySQL statistics 表成功添加列: %s", col.name)
		}
	}

	var tradeCountCol, volumeCol, totalPnLCol int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'statistics' AND COLUMN_NAME = 'trade_count'
	`).Scan(&tradeCountCol); err != nil {
		return err
	}
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'statistics' AND COLUMN_NAME = 'volume'
	`).Scan(&volumeCol); err != nil {
		return err
	}
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'statistics' AND COLUMN_NAME = 'total_pn_l'
	`).Scan(&totalPnLCol); err != nil {
		return err
	}
	if tradeCountCol > 0 {
		if _, err := db.Exec(`UPDATE statistics SET total_trades = IF((total_trades IS NULL OR total_trades = 0) AND trade_count IS NOT NULL, trade_count, total_trades)`); err != nil {
			return err
		}
	}
	if volumeCol > 0 {
		if _, err := db.Exec(`UPDATE statistics SET total_volume = IF((total_volume IS NULL OR total_volume = 0) AND volume IS NOT NULL, volume, total_volume)`); err != nil {
			return err
		}
	}
	if totalPnLCol > 0 {
		if _, err := db.Exec(`UPDATE statistics SET total_pnl = IF((total_pnl IS NULL OR total_pnl = 0) AND total_pn_l IS NOT NULL, total_pn_l, total_pnl)`); err != nil {
			return err
		}
	}

	var idxCount int
	if err := db.QueryRow(`
		SELECT COUNT(*) FROM INFORMATION_SCHEMA.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'statistics' AND INDEX_NAME = 'idx_statistics_date'
	`).Scan(&idxCount); err != nil {
		return err
	}
	if idxCount == 0 {
		if _, err := db.Exec(`CREATE INDEX idx_statistics_date ON statistics(date)`); err != nil {
			return err
		}
	}
	logger.Info("✅ MySQL statistics 表已就緒")
	return nil
}

// migrateBotStatesTableMySQL 遷移 Bot 啟停狀態表（MySQL）
func migrateBotStatesTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS bot_states (
			bot_id VARCHAR(255) PRIMARY KEY,
			enabled TINYINT NOT NULL DEFAULT 1,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_by VARCHAR(255),
			reason TEXT
		)
	`)
	if err != nil {
		return err
	}
	// MySQL 8.0+ 支持 IF NOT EXISTS，較舊版本可能需忽略重複索引錯誤
	_, _ = db.Exec("CREATE INDEX idx_bot_states_enabled ON bot_states(enabled)")
	_, _ = db.Exec("CREATE INDEX idx_bot_states_updated_at ON bot_states(updated_at)")
	return nil
}

// GetBotState 獲取 Bot 啟停狀態
func (s *SQLStorage) GetBotState(botID string) (*BotState, error) {
	var state BotState
	var enabled int
	var updatedAt, updatedBy, reason sql.NullString

	err := s.db.QueryRow(`
		SELECT bot_id, enabled, updated_at, updated_by, reason
		FROM bot_states
		WHERE bot_id = ?`, botID).Scan(
		&state.BotID, &enabled, &updatedAt, &updatedBy, &reason)

	if err == sql.ErrNoRows {
		// 數據庫中沒有記錄，返回 nil（表示使用配置文件的值）
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查詢 bot_state 失败: %w", err)
	}

	state.Enabled = enabled == 1
	if updatedAt.Valid {
		if t, e := time.Parse(time.RFC3339, updatedAt.String); e == nil {
			state.UpdatedAt = t
		}
	}
	if updatedBy.Valid {
		state.UpdatedBy = updatedBy.String
	}
	if reason.Valid {
		state.Reason = reason.String
	}

	return &state, nil
}

// SetBotState 設置 Bot 啟停狀態
func (s *SQLStorage) SetBotState(state *BotState) error {
	enabled := 0
	if state.Enabled {
		enabled = 1
	}

	updatedAt := state.UpdatedAt.Format(time.RFC3339)
	var err error
	switch s.dbType {
	case "mysql":
		_, err = s.db.Exec(`
			INSERT INTO bot_states (bot_id, enabled, updated_at, updated_by, reason)
			VALUES (?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				enabled = VALUES(enabled),
				updated_at = VALUES(updated_at),
				updated_by = VALUES(updated_by),
				reason = VALUES(reason)`,
			state.BotID, enabled, updatedAt, state.UpdatedBy, state.Reason)
	default:
		_, err = s.db.Exec(`
			INSERT INTO bot_states (bot_id, enabled, updated_at, updated_by, reason)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(bot_id) DO UPDATE SET
				enabled = excluded.enabled,
				updated_at = excluded.updated_at,
				updated_by = excluded.updated_by,
				reason = excluded.reason`,
			state.BotID, enabled, updatedAt, state.UpdatedBy, state.Reason)
	}

	if err != nil {
		return fmt.Errorf("設置 bot_state 失败: %w", err)
	}
	return nil
}

// ListBotStates 列出所有 Bot 狀態
func (s *SQLStorage) ListBotStates() ([]*BotState, error) {
	rows, err := s.db.Query(`
		SELECT bot_id, enabled, updated_at, updated_by, reason
		FROM bot_states
		ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("查詢 bot_states 列表失败: %w", err)
	}
	defer rows.Close()

	var states []*BotState
	for rows.Next() {
		var state BotState
		var enabled int
		var updatedAt, updatedBy, reason sql.NullString

		if err := rows.Scan(&state.BotID, &enabled, &updatedAt, &updatedBy, &reason); err != nil {
			// 漏掉一個 Bot 狀態會讓調用方以為該 Bot 不存在，進而誤判啟停
			return nil, fmt.Errorf("解析 Bot 狀態失败: %w", err)
		}

		state.Enabled = enabled == 1
		if updatedAt.Valid {
			if t, e := time.Parse(time.RFC3339, updatedAt.String); e == nil {
				state.UpdatedAt = t
			}
		}
		if updatedBy.Valid {
			state.UpdatedBy = updatedBy.String
		}
		if reason.Valid {
			state.Reason = reason.String
		}

		states = append(states, &state)
	}
	return states, rows.Err()
}

// migrateKlineFilesTableMySQL 創建 K 線文件統一管理表（MySQL）。
// SQLite 在 migrateKlineFilesTable 中建表；MySQL 路徑不跑那些遷移，須單獨補。
// interval 為 MySQL 保留字，建表 DDL 必須用反引號包裹，否則 Error 1064。
func migrateKlineFilesTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS kline_files (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  filename VARCHAR(255) NOT NULL,
  exchange VARCHAR(64) NOT NULL,
  symbol VARCHAR(64) NOT NULL,
  ` + "`interval`" + ` VARCHAR(32) NOT NULL,
  start_time BIGINT NOT NULL,
  end_time BIGINT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'collecting',
  has_depth TINYINT NOT NULL DEFAULT 0,
  candle_count BIGINT NOT NULL DEFAULT 0,
  file_size BIGINT NOT NULL DEFAULT 0,
  source VARCHAR(64) NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_kline_files_filename (filename)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	_, _ = db.Exec("CREATE INDEX idx_kline_files_symbol ON kline_files(symbol)")
	_, _ = db.Exec("CREATE INDEX idx_kline_files_status ON kline_files(status)")
	_, _ = db.Exec("CREATE INDEX idx_kline_files_exchange_symbol_interval ON kline_files(exchange, symbol, `interval`)")
	logger.Info("✅ MySQL kline_files 表已就緒")
	return nil
}

// migrateSystemSettingsTableMySQL 創建系統設置表（MySQL）。
// SQLite 在 migrateSystemSettingsTable 中建表；MySQL 路徑不跑那些遷移，須單獨補。
// key/value/type 為 MySQL 保留字，建表與索引 DDL 必須用反引號包裹。
func migrateSystemSettingsTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS system_settings (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  ` + "`key`" + ` VARCHAR(255) NOT NULL,
  ` + "`value`" + ` TEXT,
  ` + "`type`" + ` VARCHAR(32) NOT NULL DEFAULT 'string',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_system_settings_key (` + "`key`" + `)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	_, _ = db.Exec("CREATE INDEX idx_system_settings_key ON system_settings(`key`)")
	logger.Info("✅ MySQL system_settings 表已就緒")
	return nil
}

// migrateProtectedKlineFilesTableMySQL 創建 K 線文件保護表（MySQL）。
func migrateProtectedKlineFilesTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS protected_kline_files (
  filename VARCHAR(255) NOT NULL PRIMARY KEY,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL protected_kline_files 表已就緒")
	return nil
}

// 以下 17 個 MySQL 遷移函數補齊 SQLite createTables 已建但 MySQL 路徑未覆蓋的表。
// 規則：ENGINE=InnoDB CHARSET=utf8mb4_unicode_ci；REAL→DOUBLE；INTEGER PK→BIGINT
// AUTO_INCREMENT；bool→TINYINT；TEXT PK→VARCHAR(255)；保留字 key/value/type/interval
// 用反引號；UNIQUE/索引保留；IF NOT EXISTS，索引單獨發並用 `_, _ =` 容錯。
// 若 SQLite 在 createTables 之後另有 ALTER 補列（如 hourly_equity_records.account_equity、
// daily_snapshots.account_equity、profit_withdraw_rules.last_triggered_at），在 MySQL
// 建表時直接合入，一次到位。

func migrateReconciliationHistoryTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS reconciliation_history (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  exchange VARCHAR(64),
  symbol VARCHAR(64),
  account VARCHAR(255),
  reconcile_time TIMESTAMP(3) NULL,
  local_position DECIMAL(20,8),
  exchange_position DECIMAL(20,8),
  position_diff DECIMAL(20,8),
  active_buy_orders INT DEFAULT 0,
  active_sell_orders INT DEFAULT 0,
  pending_sell_qty DECIMAL(20,8),
  total_buy_qty DECIMAL(20,8),
  total_sell_qty DECIMAL(20,8),
  estimated_profit DECIMAL(20,8),
  actual_profit DECIMAL(20,8) DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_reconciliation_history_symbol (symbol),
  KEY idx_reconciliation_history_time (reconcile_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL reconciliation_history 表已就緒")
	return nil
}

func migrateRiskCheckHistoryTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS risk_check_history (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  check_time TIMESTAMP(3) NOT NULL,
  bot_id VARCHAR(128) NOT NULL DEFAULT '',
  exchange VARCHAR(64) NOT NULL DEFAULT '',
  market_type VARCHAR(64) NOT NULL DEFAULT '',
  symbol VARCHAR(64) NOT NULL,
  is_healthy TINYINT NOT NULL,
  price_deviation DOUBLE,
  volume_ratio DOUBLE,
  reason TEXT,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_risk_check_history_time (check_time),
  KEY idx_risk_check_history_symbol (symbol),
  KEY idx_risk_check_history_time_symbol (check_time, symbol),
  KEY idx_risk_check_history_bot_id (bot_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL risk_check_history 表已就緒")
	return nil
}

func migrateFundingRatesTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS funding_rates (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  symbol VARCHAR(64) NOT NULL,
  exchange VARCHAR(64) NOT NULL,
  rate DOUBLE NOT NULL,
  timestamp TIMESTAMP(3) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_funding_rates_symbol (symbol),
  KEY idx_funding_rates_timestamp (timestamp),
  KEY idx_funding_rates_symbol_timestamp (symbol, timestamp)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL funding_rates 表已就緒")
	return nil
}

func migrateAIPromptsTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS ai_prompts (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  module VARCHAR(128) NOT NULL,
  template MEDIUMTEXT NOT NULL,
  system_prompt MEDIUMTEXT,
  updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_ai_prompts_module (module)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL ai_prompts 表已就緒")
	return nil
}

func migrateBasisDataTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS basis_data (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  symbol VARCHAR(64) NOT NULL,
  exchange VARCHAR(64) NOT NULL,
  spot_price DOUBLE NOT NULL,
  futures_price DOUBLE NOT NULL,
  basis DOUBLE NOT NULL,
  basis_percent DOUBLE NOT NULL,
  funding_rate DOUBLE,
  timestamp DATETIME(3) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_basis_symbol_time (symbol, timestamp),
  KEY idx_basis_exchange (exchange)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL basis_data 表已就緒")
	return nil
}

func migrateProfitWithdrawRulesTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS profit_withdraw_rules (
  id VARCHAR(128) NOT NULL PRIMARY KEY,
  account_id VARCHAR(255) NOT NULL,
  exchange_id VARCHAR(64) NOT NULL,
  strategy_id VARCHAR(128) NOT NULL DEFAULT '',
  enabled TINYINT NOT NULL DEFAULT 1,
  trigger_amount DOUBLE NOT NULL DEFAULT 0,
  withdraw_ratio DOUBLE NOT NULL DEFAULT 0,
  frequency VARCHAR(32) NOT NULL DEFAULT 'immediate',
  destination VARCHAR(32) NOT NULL DEFAULT 'account',
  wallet_address VARCHAR(255),
  min_withdraw_amount DOUBLE NOT NULL DEFAULT 0,
  max_withdraw_amount DOUBLE,
  last_triggered_at TIMESTAMP(3) NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_profit_withdraw_rules_account (account_id),
  KEY idx_profit_withdraw_rules_account_exchange (account_id, exchange_id),
  KEY idx_profit_withdraw_rules_updated_at (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL profit_withdraw_rules 表已就緒")
	return nil
}

func migrateProfitWithdrawRecordsTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS profit_withdraw_records (
  id VARCHAR(128) NOT NULL PRIMARY KEY,
  rule_id VARCHAR(128) NOT NULL,
  account_id VARCHAR(255) NOT NULL,
  exchange_id VARCHAR(64) NOT NULL,
  strategy_id VARCHAR(128) DEFAULT '',
  amount DOUBLE NOT NULL,
  fee DOUBLE NOT NULL DEFAULT 0,
  net_amount DOUBLE NOT NULL,
  currency VARCHAR(32) NOT NULL,
  ` + "`type`" + ` VARCHAR(32) NOT NULL,
  status VARCHAR(32) NOT NULL,
  destination VARCHAR(32) NOT NULL,
  transfer_id VARCHAR(255),
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  completed_at TIMESTAMP(3) NULL,
  failed_reason TEXT,
  note TEXT,
  KEY idx_withdraw_records_account (account_id),
  KEY idx_withdraw_records_created_at (created_at),
  KEY idx_withdraw_records_rule_id (rule_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL profit_withdraw_records 表已就緒")
	return nil
}

func migrateInspectionReportsTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS inspection_reports (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  report_type VARCHAR(64) NOT NULL,
  title VARCHAR(255) NOT NULL,
  body LONGTEXT NOT NULL,
  snapshot_json LONGTEXT,
  analysis_json LONGTEXT,
  event_type VARCHAR(64),
  event_data_json LONGTEXT,
  generated_at TIMESTAMP(3) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_inspection_reports_generated_at (generated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL inspection_reports 表已就緒")
	return nil
}

func migrateFundingPaymentsTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS funding_payments (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  exchange VARCHAR(64) NOT NULL,
  symbol VARCHAR(64) NOT NULL,
  account VARCHAR(255),
  income_type VARCHAR(64) NOT NULL,
  income DECIMAL(20,8) NOT NULL,
  asset VARCHAR(32),
  info TEXT,
  transaction_id BIGINT,
  trade_time TIMESTAMP(3) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_funding_payments_exchange_symbol (exchange, symbol),
  KEY idx_funding_payments_trade_time (trade_time),
  KEY idx_funding_payments_account (account)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL funding_payments 表已就緒")
	return nil
}

func migrateMarketInterpretTasksTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS market_interpret_tasks (
  task_id VARCHAR(128) NOT NULL PRIMARY KEY,
  page_type VARCHAR(64) NOT NULL,
  symbol VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL,
  progress INT NOT NULL DEFAULT 0,
  result LONGTEXT,
  error TEXT,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL,
  KEY idx_market_interpret_page_created (page_type, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL market_interpret_tasks 表已就緒")
	return nil
}

func migrateHourlyEquityRecordsTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS hourly_equity_records (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  exchange VARCHAR(64) NOT NULL,
  symbol VARCHAR(64) NOT NULL,
  account VARCHAR(255) NOT NULL,
  timestamp DATETIME(3) NOT NULL,
  equity DOUBLE NOT NULL,
  unrealized_pnl DOUBLE NOT NULL,
  total_position_value DOUBLE NOT NULL,
  account_equity DOUBLE,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_hourly_equity_exchange_symbol_account (exchange, symbol, account),
  KEY idx_hourly_equity_timestamp (timestamp)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL hourly_equity_records 表已就緒")
	return nil
}

func migrateDailySnapshotsTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS daily_snapshots (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  exchange VARCHAR(64) NOT NULL,
  symbol VARCHAR(64) NOT NULL,
  account VARCHAR(255) NOT NULL,
  date DATE NOT NULL,
  unrealized_pnl DOUBLE NOT NULL,
  total_position_value DOUBLE NOT NULL,
  intraday_max_drawdown DOUBLE NOT NULL,
  intraday_max_drawdown_pct DOUBLE NOT NULL,
  intraday_peak_equity DOUBLE NOT NULL,
  closing_price DOUBLE NOT NULL,
  snapshot_time TIMESTAMP(3) NOT NULL,
  account_equity DOUBLE,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_daily_snapshots_dim (exchange, symbol, account, date),
  KEY idx_daily_snapshots_exchange_symbol_account (exchange, symbol, account),
  KEY idx_daily_snapshots_date (date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL daily_snapshots 表已就緒")
	return nil
}

func migrateBacktestTasksTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS backtest_tasks (
  id VARCHAR(128) NOT NULL PRIMARY KEY,
  status VARCHAR(32) NOT NULL,
  mode VARCHAR(32),
  bot_id VARCHAR(128),
  group_id VARCHAR(128),
  strategy VARCHAR(64) NOT NULL,
  strategies_json LONGTEXT,
  symbol VARCHAR(64) NOT NULL,
  ` + "`interval`" + ` VARCHAR(32) NOT NULL,
  start_time BIGINT NOT NULL,
  end_time BIGINT NOT NULL,
  params LONGTEXT NOT NULL,
  total_capital DOUBLE NOT NULL,
  progress INT DEFAULT 0,
  created_at BIGINT NOT NULL,
  started_at BIGINT,
  completed_at BIGINT,
  error TEXT,
  result_path VARCHAR(512),
  report_path VARCHAR(512),
  data_source VARCHAR(64),
  kline_file VARCHAR(255),
  cache_name VARCHAR(255),
  KEY idx_backtest_tasks_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL backtest_tasks 表已就緒")
	return nil
}

func migrateOptimTasksTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS optim_tasks (
  id VARCHAR(128) NOT NULL PRIMARY KEY,
  status VARCHAR(32) NOT NULL,
  strategy VARCHAR(64) NOT NULL,
  symbol VARCHAR(64) NOT NULL,
  ` + "`interval`" + ` VARCHAR(32) NOT NULL,
  start_time BIGINT NOT NULL,
  end_time BIGINT NOT NULL,
  total_capital DOUBLE NOT NULL,
  search_space LONGTEXT NOT NULL,
  progress INT DEFAULT 0,
  total_combos INT DEFAULT 0,
  completed_combos INT DEFAULT 0,
  created_at BIGINT NOT NULL,
  started_at BIGINT,
  completed_at BIGINT,
  result_path VARCHAR(512),
  error TEXT,
  KEY idx_optim_tasks_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL optim_tasks 表已就緒")
	return nil
}

func migrateNewsAnalysisHistoryTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS news_analysis_history (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  analysis_time TIMESTAMP(3) NOT NULL,
  symbol VARCHAR(64) NOT NULL,
  current_price DOUBLE NOT NULL,
  assessment TEXT,
  recent_news_summary MEDIUMTEXT,
  gemini_prompt MEDIUMTEXT,
  gemini_response LONGTEXT,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_news_analysis_history_analysis_time (analysis_time),
  KEY idx_news_analysis_history_symbol (symbol)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL news_analysis_history 表已就緒")
	return nil
}

func migratePriceHistoryTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS price_history (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  asset_type VARCHAR(64) NOT NULL,
  symbol VARCHAR(64) NOT NULL,
  price DOUBLE NOT NULL,
  source VARCHAR(64),
  recorded_at TIMESTAMP(3) NOT NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_price_history_lookup (asset_type, symbol, recorded_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL price_history 表已就緒")
	return nil
}

func migratePositionsTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS positions (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  slot_price DECIMAL(20,8),
  symbol VARCHAR(64),
  size DECIMAL(20,8),
  entry_price DECIMAL(20,8),
  current_price DECIMAL(20,8),
  pnl DECIMAL(20,8),
  opened_at DATETIME(3) NULL,
  closed_at DATETIME(3) NULL,
  KEY idx_positions_slot_price (slot_price)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL positions 表已就緒")
	return nil
}

func migratePredictionVerificationTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS prediction_verification (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  analysis_id BIGINT NOT NULL,
  asset_type VARCHAR(64) NOT NULL,
  symbol VARCHAR(64) NOT NULL,
  prediction_time TIMESTAMP(3) NOT NULL,
  timeframe VARCHAR(32) NOT NULL,
  predicted_direction VARCHAR(32) NOT NULL,
  predicted_change_pct DOUBLE,
  predicted_probability DOUBLE,
  actual_price_at_prediction DOUBLE,
  actual_price_at_verify DOUBLE,
  actual_direction VARCHAR(32),
  actual_change_pct DOUBLE,
  is_correct TINYINT,
  verified_at TIMESTAMP(3) NULL,
  status VARCHAR(32) DEFAULT 'pending',
  KEY idx_pred_verif_status (status),
  KEY idx_pred_verif_asset_symbol (asset_type, symbol)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL prediction_verification 表已就緒")
	return nil
}
