package storage

import (
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
		INSERT INTO hourly_equity_records (exchange, symbol, account, timestamp, equity, unrealized_pnl, total_position_value, account_equity, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Exchange, record.Symbol, record.Account, record.Timestamp,
		record.Equity, record.UnrealizedPnL, record.TotalPositionValue, acct, time.Now())
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
	_, err := s.db.Exec(`
		INSERT INTO daily_snapshots (exchange, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at, account_equity)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(exchange, symbol, account, date) DO UPDATE SET
			unrealized_pnl = excluded.unrealized_pnl,
			total_position_value = excluded.total_position_value,
			intraday_max_drawdown = excluded.intraday_max_drawdown,
			intraday_max_drawdown_pct = excluded.intraday_max_drawdown_pct,
			intraday_peak_equity = excluded.intraday_peak_equity,
			closing_price = excluded.closing_price,
			snapshot_time = excluded.snapshot_time,
			account_equity = COALESCE(excluded.account_equity, account_equity)`,
		snapshot.Exchange, snapshot.Symbol, snapshot.Account, snapshot.Date.Format("2006-01-02"),
		snapshot.UnrealizedPnL, snapshot.TotalPositionValue, snapshot.IntradayMaxDrawdown, snapshot.IntradayMaxDrawdownPct,
		snapshot.IntradayPeakEquity, snapshot.ClosingPrice, snapshot.SnapshotTime, time.Now(), acct)
	if err != nil {
		return fmt.Errorf("保存 daily_snapshot 失败: %w", err)
	}
	return nil
}

// QueryDailySnapshots 查詢日期範圍內的每日快照
func (s *SQLStorage) QueryDailySnapshots(exchange, symbol, account string, startDate, endDate time.Time) ([]*DailySnapshot, error) {
	rows, err := s.db.Query(`
		SELECT id, exchange, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at, account_equity
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
		var acct sql.NullFloat64
		if err := rows.Scan(
			&snap.ID, &snap.Exchange, &snap.Symbol, &snap.Account, &dateStr,
			&snap.UnrealizedPnL, &snap.TotalPositionValue, &snap.IntradayMaxDrawdown, &snap.IntradayMaxDrawdownPct,
			&snap.IntradayPeakEquity, &snap.ClosingPrice, &snapshotTime, &createdAt, &acct,
		); err != nil {
			continue
		}
		if acct.Valid {
			v := acct.Float64
			snap.AccountEquity = &v
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
func (s *SQLStorage) GetDailySnapshot(exchange, symbol, account string, date time.Time) (*DailySnapshot, error) {
	dateStr := date.Format("2006-01-02")
	row := s.db.QueryRow(`
		SELECT id, exchange, symbol, account, date, unrealized_pnl, total_position_value, intraday_max_drawdown, intraday_max_drawdown_pct, intraday_peak_equity, closing_price, snapshot_time, created_at, account_equity
		FROM daily_snapshots
		WHERE exchange = ? AND symbol = ? AND account = ? AND date = ?`,
		exchange, symbol, account, dateStr)
	snap := &DailySnapshot{}
	var snapshotTime, createdAt time.Time
	var dStr string
	var acct sql.NullFloat64
	err := row.Scan(
		&snap.ID, &snap.Exchange, &snap.Symbol, &snap.Account, &dStr,
		&snap.UnrealizedPnL, &snap.TotalPositionValue, &snap.IntradayMaxDrawdown, &snap.IntradayMaxDrawdownPct,
		&snap.IntradayPeakEquity, &snap.ClosingPrice, &snapshotTime, &createdAt, &acct,
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
	if t, e := time.Parse("2006-01-02", dStr); e == nil {
		snap.Date = t
	}
	snap.SnapshotTime = snapshotTime
	snap.CreatedAt = createdAt
	return snap, nil
}

// QueryHourlyEquityRecords 查詢時間範圍內的小時權益記錄（用於計算日內最大回撤）
func (s *SQLStorage) QueryHourlyEquityRecords(exchange, symbol, account string, startTime, endTime time.Time) ([]*HourlyEquityRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, exchange, symbol, account, timestamp, equity, unrealized_pnl, total_position_value, account_equity, created_at
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
		var acct sql.NullFloat64
		if err := rows.Scan(&r.ID, &r.Exchange, &r.Symbol, &r.Account, &ts, &r.Equity, &r.UnrealizedPnL, &r.TotalPositionValue, &acct, &createdAt); err != nil {
			continue
		}
		if acct.Valid {
			v := acct.Float64
			r.AccountEquity = &v
		}
		r.Timestamp = ts
		r.CreatedAt = createdAt
		out = append(out, r)
	}
	return out, rows.Err()
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
			continue
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
