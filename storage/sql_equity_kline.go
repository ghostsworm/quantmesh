package storage

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"

	"quantmesh/logger"
)

// ========== 小時權益 / 每日快照 / K线文件保護 存儲 ==========

// SaveHourlyEquityRecord 保存小時權益記錄
func (s *SQLStorage) SaveHourlyEquityRecord(record *HourlyEquityRecord) error {
	var acct interface{}
	if record.AccountEquity != nil {
		acct = *record.AccountEquity
	}
	_, err := s.db.Exec(`
		INSERT INTO hourly_equity_records (exchange, market_type, account_scope, symbol, account, timestamp, equity, unrealized_pnl, total_position_value, market_price, spot_position_qty, account_equity, unrealized_pnl_asset, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Exchange, record.MarketType, record.AccountScope, record.Symbol, record.Account, record.Timestamp,
		record.Equity, record.UnrealizedPnL, record.TotalPositionValue, record.MarketPrice, record.SpotPositionQty, acct, record.UnrealizedPnLAsset, time.Now())
	if err != nil {
		return fmt.Errorf("保存 hourly_equity_record 失败: %w", err)
	}
	return nil
}

// SaveDailySnapshot 保存每日快照（upsert）
func (s *SQLStorage) SaveDailySnapshot(snapshot *DailySnapshot) error {
	var acct interface{}
	if snapshot.AccountEquity != nil {
		acct = *snapshot.AccountEquity
	}
	query := `
		INSERT INTO daily_snapshots (exchange, market_type, account_scope, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at, account_equity, spot_position_qty, unrealized_pnl_asset)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(exchange, market_type, account_scope, symbol, date) DO UPDATE SET
			unrealized_pnl = excluded.unrealized_pnl,
			total_position_value = excluded.total_position_value,
			intraday_max_drawdown = excluded.intraday_max_drawdown,
			intraday_max_drawdown_pct = excluded.intraday_max_drawdown_pct,
			intraday_peak_equity = excluded.intraday_peak_equity,
			closing_price = excluded.closing_price,
			snapshot_time = excluded.snapshot_time,
			account_equity = COALESCE(excluded.account_equity, account_equity),
			spot_position_qty = COALESCE(excluded.spot_position_qty, spot_position_qty),
			unrealized_pnl_asset = excluded.unrealized_pnl_asset`
	if s.dbType == "mysql" {
		query = `INSERT INTO daily_snapshots (exchange, market_type, account_scope, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at, account_equity, spot_position_qty, unrealized_pnl_asset)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE unrealized_pnl = VALUES(unrealized_pnl), total_position_value = VALUES(total_position_value),
			intraday_max_drawdown = VALUES(intraday_max_drawdown), intraday_max_drawdown_pct = VALUES(intraday_max_drawdown_pct),
			intraday_peak_equity = VALUES(intraday_peak_equity), closing_price = VALUES(closing_price), snapshot_time = VALUES(snapshot_time),
			account_equity = COALESCE(VALUES(account_equity), account_equity), spot_position_qty = COALESCE(VALUES(spot_position_qty), spot_position_qty),
			unrealized_pnl_asset = VALUES(unrealized_pnl_asset)`
	}
	_, err := s.db.Exec(query,
		snapshot.Exchange, snapshot.MarketType, snapshot.AccountScope, snapshot.Symbol, snapshot.Account, snapshot.Date.Format("2006-01-02"),
		snapshot.UnrealizedPnL, snapshot.TotalPositionValue, snapshot.IntradayMaxDrawdown, snapshot.IntradayMaxDrawdownPct,
		snapshot.IntradayPeakEquity, snapshot.ClosingPrice, snapshot.SnapshotTime, time.Now(), acct, snapshot.SpotPositionQty, snapshot.UnrealizedPnLAsset)
	if err != nil {
		return fmt.Errorf("保存 daily_snapshot 失败: %w", err)
	}
	return nil
}

// QueryDailySnapshots 查詢日期範圍內的每日快照
func (s *SQLStorage) QueryDailySnapshots(exchange, symbol, account string, startDate, endDate time.Time) ([]*DailySnapshot, error) {
	return s.queryDailySnapshots(exchange, "", symbol, account, startDate, endDate)
}

// QueryDailySnapshotsByMarketType returns only rows for one market; unknown maps to legacy blank rows.
func (s *SQLStorage) QueryDailySnapshotsByMarketType(exchange, marketType, symbol, account string, startDate, endDate time.Time) ([]*DailySnapshot, error) {
	if marketType == "unknown" {
		marketType = ""
	}
	return s.queryDailySnapshots(exchange, marketType, symbol, account, startDate, endDate)
}

func (s *SQLStorage) QueryDailySnapshotsByScope(exchange, marketType, symbol, accountScope string, startDate, endDate time.Time) ([]*DailySnapshot, error) {
	if marketType == "unknown" {
		marketType = ""
	}
	return s.queryDailySnapshotsByScope(exchange, marketType, symbol, accountScope, startDate, endDate)
}

func (s *SQLStorage) queryDailySnapshots(exchange, marketType, symbol, account string, startDate, endDate time.Time) ([]*DailySnapshot, error) {
	legacyScope, hashedLegacyScope := legacyAccountScopes(account)
	rows, err := s.db.Query(`
		SELECT id, exchange, market_type, account_scope, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at, account_equity, spot_position_qty, unrealized_pnl_asset
		FROM daily_snapshots
		WHERE exchange = ? AND market_type = ? AND (account_scope = '' OR account_scope IN (?, ?)) AND symbol = ? AND account = ? AND date >= ? AND date <= ?
		ORDER BY date ASC`,
		exchange, marketType, legacyScope, hashedLegacyScope, symbol, account, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("查詢 daily_snapshots 失败: %w", err)
	}
	defer rows.Close()

	var out []*DailySnapshot
	for rows.Next() {
		snap := &DailySnapshot{}
		var dateStr string
		var snapshotTime, createdAt time.Time
		var acct sql.NullFloat64
		var spotQty sql.NullFloat64
		var pnlAsset string
		if err := rows.Scan(
			&snap.ID, &snap.Exchange, &snap.MarketType, &snap.AccountScope, &snap.Symbol, &snap.Account, &dateStr,
			&snap.UnrealizedPnL, &snap.TotalPositionValue, &snap.IntradayMaxDrawdown, &snap.IntradayMaxDrawdownPct,
			&snap.IntradayPeakEquity, &snap.ClosingPrice, &snapshotTime, &createdAt, &acct, &spotQty, &pnlAsset,
		); err != nil {
			continue
		}
		if acct.Valid {
			v := acct.Float64
			snap.AccountEquity = &v
		}
		if spotQty.Valid {
			value := spotQty.Float64
			snap.SpotPositionQty = &value
		}
		if t, e := time.Parse("2006-01-02", dateStr); e == nil {
			snap.Date = t
		}
		snap.SnapshotTime = snapshotTime
		snap.UnrealizedPnLAsset = pnlAsset
		snap.CreatedAt = createdAt
		out = append(out, snap)
	}
	return out, rows.Err()
}

func (s *SQLStorage) queryDailySnapshotsByScope(exchange, marketType, symbol, accountScope string, startDate, endDate time.Time) ([]*DailySnapshot, error) {
	rows, err := s.db.Query(`SELECT id, exchange, market_type, account_scope, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at, account_equity, spot_position_qty, unrealized_pnl_asset
		FROM daily_snapshots WHERE exchange = ? AND market_type = ? AND symbol = ? AND account_scope = ? AND date >= ? AND date <= ? ORDER BY date ASC`,
		exchange, marketType, symbol, accountScope, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("按作用域查詢 daily_snapshots 失败: %w", err)
	}
	defer rows.Close()
	var out []*DailySnapshot
	for rows.Next() {
		snap := &DailySnapshot{}
		var dateStr string
		var snapshotTime, createdAt time.Time
		var acct sql.NullFloat64
		var spotQty sql.NullFloat64
		var pnlAsset string
		if err := rows.Scan(&snap.ID, &snap.Exchange, &snap.MarketType, &snap.AccountScope, &snap.Symbol, &snap.Account, &dateStr,
			&snap.UnrealizedPnL, &snap.TotalPositionValue, &snap.IntradayMaxDrawdown, &snap.IntradayMaxDrawdownPct,
			&snap.IntradayPeakEquity, &snap.ClosingPrice, &snapshotTime, &createdAt, &acct, &spotQty, &pnlAsset); err != nil {
			return nil, fmt.Errorf("解析作用域 daily_snapshot 失败: %w", err)
		}
		if acct.Valid {
			value := acct.Float64
			snap.AccountEquity = &value
		}
		if spotQty.Valid {
			value := spotQty.Float64
			snap.SpotPositionQty = &value
		}
		snap.Date, _ = time.Parse("2006-01-02", dateStr)
		snap.SnapshotTime, snap.CreatedAt = snapshotTime, createdAt
		snap.UnrealizedPnLAsset = pnlAsset
		out = append(out, snap)
	}
	return out, rows.Err()
}

// GetDailySnapshot 查詢單日快照
func (s *SQLStorage) GetDailySnapshot(exchange, symbol, account string, date time.Time) (*DailySnapshot, error) {
	dateStr := date.Format("2006-01-02")
	legacyScope, hashedLegacyScope := legacyAccountScopes(account)
	row := s.db.QueryRow(`
		SELECT id, exchange, market_type, account_scope, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at, account_equity, spot_position_qty, unrealized_pnl_asset
		FROM daily_snapshots
		WHERE exchange = ? AND market_type = '' AND (account_scope = '' OR account_scope IN (?, ?)) AND symbol = ? AND account = ? AND date = ?`,
		exchange, legacyScope, hashedLegacyScope, symbol, account, dateStr)
	snap := &DailySnapshot{}
	var snapshotTime, createdAt time.Time
	var dStr string
	var acct sql.NullFloat64
	var spotQty sql.NullFloat64
	var pnlAsset string
	err := row.Scan(
		&snap.ID, &snap.Exchange, &snap.MarketType, &snap.AccountScope, &snap.Symbol, &snap.Account, &dStr,
		&snap.UnrealizedPnL, &snap.TotalPositionValue, &snap.IntradayMaxDrawdown, &snap.IntradayMaxDrawdownPct,
		&snap.IntradayPeakEquity, &snap.ClosingPrice, &snapshotTime, &createdAt, &acct, &spotQty, &pnlAsset,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查詢 daily_snapshot 失败: %w", err)
	}
	if acct.Valid {
		v := acct.Float64
		snap.AccountEquity = &v
	}
	if spotQty.Valid {
		value := spotQty.Float64
		snap.SpotPositionQty = &value
	}
	if t, e := time.Parse("2006-01-02", dStr); e == nil {
		snap.Date = t
	}
	snap.SnapshotTime = snapshotTime
	snap.UnrealizedPnLAsset = pnlAsset
	snap.CreatedAt = createdAt
	return snap, nil
}

// GetDailySnapshotByMarketType returns a snapshot for one explicit market; unknown maps to legacy blank rows.
func (s *SQLStorage) GetDailySnapshotByMarketType(exchange, marketType, symbol, account string, date time.Time) (*DailySnapshot, error) {
	if marketType == "unknown" {
		marketType = ""
	}
	dateStr := date.Format("2006-01-02")
	legacyScope, hashedLegacyScope := legacyAccountScopes(account)
	row := s.db.QueryRow(`SELECT id, exchange, market_type, account_scope, symbol, account, date, unrealized_pnl, total_position_value,
		intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at, account_equity, spot_position_qty, unrealized_pnl_asset
		FROM daily_snapshots WHERE exchange = ? AND market_type = ? AND (account_scope = '' OR account_scope IN (?, ?)) AND symbol = ? AND account = ? AND date = ?`,
		exchange, marketType, legacyScope, hashedLegacyScope, symbol, account, dateStr)
	snap := &DailySnapshot{}
	var snapshotTime, createdAt time.Time
	var dateValue string
	var acct sql.NullFloat64
	var spotQty sql.NullFloat64
	var pnlAsset string
	err := row.Scan(&snap.ID, &snap.Exchange, &snap.MarketType, &snap.AccountScope, &snap.Symbol, &snap.Account, &dateValue,
		&snap.UnrealizedPnL, &snap.TotalPositionValue, &snap.IntradayMaxDrawdown, &snap.IntradayMaxDrawdownPct,
		&snap.IntradayPeakEquity, &snap.ClosingPrice, &snapshotTime, &createdAt, &acct, &spotQty, &pnlAsset)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("按市場查詢 daily_snapshot 失败: %w", err)
	}
	if acct.Valid {
		value := acct.Float64
		snap.AccountEquity = &value
	}
	if spotQty.Valid {
		value := spotQty.Float64
		snap.SpotPositionQty = &value
	}
	snap.Date, _ = time.Parse("2006-01-02", dateValue)
	snap.SnapshotTime, snap.CreatedAt = snapshotTime, createdAt
	snap.UnrealizedPnLAsset = pnlAsset
	return snap, nil
}

func (s *SQLStorage) GetDailySnapshotByScope(exchange, marketType, symbol, accountScope string, date time.Time) (*DailySnapshot, error) {
	if marketType == "unknown" {
		marketType = ""
	}
	rows, err := s.queryDailySnapshotsByScope(exchange, marketType, symbol, accountScope, date, date)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// QueryHourlyEquityRecords 查詢時間範圍內的小時權益記錄（用於計算日內最大回撤）
func (s *SQLStorage) QueryHourlyEquityRecords(exchange, symbol, account string, startTime, endTime time.Time) ([]*HourlyEquityRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, exchange, market_type, account_scope, symbol, account, timestamp, equity, unrealized_pnl, total_position_value, market_price, spot_position_qty, account_equity, created_at, unrealized_pnl_asset
		FROM hourly_equity_records
		WHERE exchange = ? AND market_type = '' AND account_scope = '' AND symbol = ? AND account = ? AND timestamp >= ? AND timestamp <= ?
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
		var acct sql.NullFloat64
		var spotQty sql.NullFloat64
		var pnlAsset string
		if err := rows.Scan(&r.ID, &r.Exchange, &r.MarketType, &r.AccountScope, &r.Symbol, &r.Account, &ts, &r.Equity, &r.UnrealizedPnL, &r.TotalPositionValue, &r.MarketPrice, &spotQty, &acct, &createdAt, &pnlAsset); err != nil {
			// 權益曲线缺一個點就是圖表失真，不能靜默跳過
			return nil, fmt.Errorf("解析權益記錄失败: %w", err)
		}
		if acct.Valid {
			v := acct.Float64
			r.AccountEquity = &v
		}
		if spotQty.Valid {
			value := spotQty.Float64
			r.SpotPositionQty = &value
		}
		r.Timestamp = ts
		r.UnrealizedPnLAsset = pnlAsset
		r.CreatedAt = createdAt
		out = append(out, r)
	}
	return out, rows.Err()
}

// QueryHourlyEquityRecordsByMarketType limits drawdown aggregation to a single market.
func (s *SQLStorage) QueryHourlyEquityRecordsByMarketType(exchange, marketType, symbol, account string, startTime, endTime time.Time) ([]*HourlyEquityRecord, error) {
	return s.queryHourlyEquityRecords(`SELECT id, exchange, market_type, account_scope, symbol, account, timestamp, equity, unrealized_pnl, total_position_value, market_price, spot_position_qty, account_equity, created_at, unrealized_pnl_asset
		FROM hourly_equity_records WHERE exchange = ? AND market_type = ? AND account_scope = '' AND symbol = ? AND account = ? AND timestamp >= ? AND timestamp <= ? ORDER BY timestamp ASC`, exchange, marketType, symbol, account, startTime, endTime)
}

func (s *SQLStorage) QueryHourlyEquityRecordsByScope(exchange, marketType, symbol, accountScope string, startTime, endTime time.Time) ([]*HourlyEquityRecord, error) {
	return s.queryHourlyEquityRecords(`SELECT id, exchange, market_type, account_scope, symbol, account, timestamp, equity, unrealized_pnl, total_position_value, market_price, spot_position_qty, account_equity, created_at, unrealized_pnl_asset
		FROM hourly_equity_records WHERE exchange = ? AND market_type = ? AND account_scope = ? AND symbol = ? AND timestamp >= ? AND timestamp <= ? ORDER BY timestamp ASC`,
		exchange, marketType, accountScope, symbol, startTime, endTime)
}

func (s *SQLStorage) queryHourlyEquityRecords(query string, args ...interface{}) ([]*HourlyEquityRecord, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查詢 hourly_equity_records 失败: %w", err)
	}
	defer rows.Close()
	var out []*HourlyEquityRecord
	for rows.Next() {
		record := &HourlyEquityRecord{}
		var timestamp, createdAt time.Time
		var accountEquity sql.NullFloat64
		var spotQty sql.NullFloat64
		var pnlAsset string
		if err := rows.Scan(&record.ID, &record.Exchange, &record.MarketType, &record.AccountScope, &record.Symbol, &record.Account, &timestamp,
			&record.Equity, &record.UnrealizedPnL, &record.TotalPositionValue, &record.MarketPrice, &spotQty, &accountEquity, &createdAt, &pnlAsset); err != nil {
			return nil, fmt.Errorf("解析按市場權益記錄失败: %w", err)
		}
		record.Timestamp, record.CreatedAt = timestamp, createdAt
		record.UnrealizedPnLAsset = pnlAsset
		if accountEquity.Valid {
			value := accountEquity.Float64
			record.AccountEquity = &value
		}
		if spotQty.Valid {
			value := spotQty.Float64
			record.SpotPositionQty = &value
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

// SaveAccountEquityRecord stores one account-level sample, independent of symbol runtimes.
func (s *SQLStorage) SaveAccountEquityRecord(record *AccountEquityRecord) error {
	query := `INSERT INTO account_equity_records (exchange, market_type, account_scope, account, timestamp, account_equity, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(exchange, market_type, account_scope, timestamp) DO UPDATE SET account = excluded.account, account_equity = excluded.account_equity`
	if s.dbType == "mysql" {
		query = `INSERT INTO account_equity_records (exchange, market_type, account_scope, account, timestamp, account_equity, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE account = VALUES(account), account_equity = VALUES(account_equity)`
	}
	if _, err := s.db.Exec(query, record.Exchange, record.MarketType, record.AccountScope, record.Account, record.Timestamp, record.AccountEquity, time.Now()); err != nil {
		return fmt.Errorf("保存账户权益记录失败: %w", err)
	}
	return nil
}

// QueryAccountEquityRecords returns account-level equity samples in timestamp order.
func (s *SQLStorage) QueryAccountEquityRecords(exchange, account string, startTime, endTime time.Time) ([]*AccountEquityRecord, error) {
	legacyScope, hashedLegacyScope := legacyAccountScopes(account)
	return s.queryAccountEquityRecords(`SELECT id, exchange, market_type, account_scope, account, timestamp, account_equity, created_at
		FROM account_equity_records WHERE exchange = ? AND account = ? AND (account_scope = '' OR account_scope IN (?, ?)) AND timestamp >= ? AND timestamp <= ? ORDER BY timestamp ASC`,
		exchange, account, legacyScope, hashedLegacyScope, startTime, endTime)
}

func (s *SQLStorage) QueryAccountEquityRecordsByMarketType(exchange, marketType, account string, startTime, endTime time.Time) ([]*AccountEquityRecord, error) {
	if marketType == "unknown" {
		marketType = ""
	}
	legacyScope, hashedLegacyScope := legacyAccountScopes(account)
	return s.queryAccountEquityRecords(`SELECT id, exchange, market_type, account_scope, account, timestamp, account_equity, created_at
		FROM account_equity_records WHERE exchange = ? AND market_type = ? AND account = ? AND (account_scope = '' OR account_scope IN (?, ?)) AND timestamp >= ? AND timestamp <= ? ORDER BY timestamp ASC`,
		exchange, marketType, account, legacyScope, hashedLegacyScope, startTime, endTime)
}

func legacyAccountScopes(account string) (string, string) {
	return "legacy:" + account, fmt.Sprintf("legacy:%x", sha256.Sum256([]byte(account)))
}

func (s *SQLStorage) QueryAccountEquityRecordsByScope(exchange, marketType, accountScope string, startTime, endTime time.Time) ([]*AccountEquityRecord, error) {
	if marketType == "unknown" {
		marketType = ""
	}
	return s.queryAccountEquityRecords(`SELECT id, exchange, market_type, account_scope, account, timestamp, account_equity, created_at
		FROM account_equity_records WHERE exchange = ? AND market_type = ? AND account_scope = ? AND timestamp >= ? AND timestamp <= ? ORDER BY timestamp ASC`,
		exchange, marketType, accountScope, startTime, endTime)
}

func (s *SQLStorage) queryAccountEquityRecords(query string, args ...interface{}) ([]*AccountEquityRecord, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询账户权益记录失败: %w", err)
	}
	defer rows.Close()
	var records []*AccountEquityRecord
	for rows.Next() {
		record := &AccountEquityRecord{}
		if err := rows.Scan(&record.ID, &record.Exchange, &record.MarketType, &record.AccountScope, &record.Account, &record.Timestamp, &record.AccountEquity, &record.CreatedAt); err != nil {
			return nil, fmt.Errorf("解析账户权益记录失败: %w", err)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *SQLStorage) DeleteAccountEquityRecordsBefore(cutoff time.Time) error {
	if _, err := s.db.Exec(`DELETE FROM account_equity_records WHERE timestamp < ?`, cutoff); err != nil {
		return fmt.Errorf("清理过期账户权益记录失败: %w", err)
	}
	return nil
}

// DeleteHourlyEquityRecordsBefore 刪除指定時間之前的小時級數據（用於 90 天清理）
func (s *SQLStorage) DeleteHourlyEquityRecordsBefore(cutoff time.Time) error {
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

// ProtectKlineFile 保護K線文件
func (s *SQLStorage) ProtectKlineFile(filename string) error {
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO protected_kline_files (filename) VALUES (?)`,
		filename)
	if err != nil {
		return fmt.Errorf("保護文件失败: %w", err)
	}
	return nil
}

// UnprotectKlineFile 取消保護K線文件
func (s *SQLStorage) UnprotectKlineFile(filename string) error {
	_, err := s.db.Exec(`DELETE FROM protected_kline_files WHERE filename = ?`, filename)
	if err != nil {
		return fmt.Errorf("取消保護文件失败: %w", err)
	}
	return nil
}

// GetProtectedKlineFiles 獲取所有保護的文件列表
func (s *SQLStorage) GetProtectedKlineFiles() ([]string, error) {
	rows, err := s.db.Query(`SELECT filename FROM protected_kline_files`)
	if err != nil {
		return nil, fmt.Errorf("查詢保護文件列表失败: %w", err)
	}
	defer rows.Close()

	var files []string
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			// 漏掉受保護的檔名會導致該檔案被誤刪
			return nil, fmt.Errorf("解析受保護 K線檔名失败: %w", err)
		}
		files = append(files, filename)
	}
	return files, rows.Err()
}

// IsKlineFileProtected 檢查文件是否被保護
func (s *SQLStorage) IsKlineFileProtected(filename string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM protected_kline_files WHERE filename = ?`, filename).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("查詢文件保護狀態失败: %w", err)
	}
	return count > 0, nil
}
