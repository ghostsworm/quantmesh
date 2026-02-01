package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// QuerySystemMetrics 查询系统监控细粒度数据
func (s *SQLiteStorage) QuerySystemMetrics(startTime, endTime time.Time) ([]*SystemMetrics, error) {
	rows, err := s.db.Query(`
		SELECT id, timestamp, cpu_percent, memory_mb, memory_percent, process_id, created_at
		FROM system_metrics
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC
	`, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("查询系统监控数据失败: %w", err)
	}
	defer rows.Close()

	var metrics []*SystemMetrics
	for rows.Next() {
		m := &SystemMetrics{}
		var memoryPercent sql.NullFloat64
		err := rows.Scan(
			&m.ID,
			&m.Timestamp,
			&m.CPUPercent,
			&m.MemoryMB,
			&memoryPercent,
			&m.ProcessID,
			&m.CreatedAt,
		)
		if err != nil {
			continue
		}
		if memoryPercent.Valid {
			m.MemoryPercent = memoryPercent.Float64
		}
		metrics = append(metrics, m)
	}

	return metrics, nil
}

// QueryDailySystemMetrics 查询每日汇总数据
func (s *SQLiteStorage) QueryDailySystemMetrics(days int) ([]*DailySystemMetrics, error) {
	startDate := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	startDate = time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, startDate.Location())

	rows, err := s.db.Query(`
		SELECT id, date, avg_cpu_percent, max_cpu_percent, min_cpu_percent,
		       avg_memory_mb, max_memory_mb, min_memory_mb, sample_count, created_at
		FROM daily_system_metrics
		WHERE date >= ?
		ORDER BY date ASC
	`, startDate)
	if err != nil {
		return nil, fmt.Errorf("查询每日汇总数据失败: %w", err)
	}
	defer rows.Close()

	var metrics []*DailySystemMetrics
	for rows.Next() {
		m := &DailySystemMetrics{}
		err := rows.Scan(
			&m.ID,
			&m.Date,
			&m.AvgCPUPercent,
			&m.MaxCPUPercent,
			&m.MinCPUPercent,
			&m.AvgMemoryMB,
			&m.MaxMemoryMB,
			&m.MinMemoryMB,
			&m.SampleCount,
			&m.CreatedAt,
		)
		if err != nil {
			continue
		}
		metrics = append(metrics, m)
	}

	return metrics, nil
}

// GetLatestSystemMetrics 获取最新的系统监控数据
func (s *SQLiteStorage) GetLatestSystemMetrics() (*SystemMetrics, error) {
	row := s.db.QueryRow(`
		SELECT id, timestamp, cpu_percent, memory_mb, memory_percent, process_id, created_at
		FROM system_metrics
		ORDER BY timestamp DESC
		LIMIT 1
	`)

	m := &SystemMetrics{}
	var memoryPercent sql.NullFloat64
	err := row.Scan(
		&m.ID,
		&m.Timestamp,
		&m.CPUPercent,
		&m.MemoryMB,
		&memoryPercent,
		&m.ProcessID,
		&m.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("查询最新监控数据失败: %w", err)
	}

	if memoryPercent.Valid {
		m.MemoryPercent = memoryPercent.Float64
	}

	return m, nil
}

// CleanupSystemMetrics 清理过期的细粒度数据
func (s *SQLiteStorage) CleanupSystemMetrics(beforeTime time.Time) error {
	_, err := s.db.Exec(`
		DELETE FROM system_metrics
		WHERE timestamp < ?
	`, beforeTime)
	return err
}

// CleanupDailySystemMetrics 清理过期的每日汇总数据
func (s *SQLiteStorage) CleanupDailySystemMetrics(beforeDate time.Time) error {
	_, err := s.db.Exec(`
		DELETE FROM daily_system_metrics
		WHERE date < ?
	`, beforeDate)
	return err
}
