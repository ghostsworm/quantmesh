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
			continue
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
