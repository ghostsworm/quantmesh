package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// parseTimeField 解析时间字段，支持 Unix 时间戳和字符串格式
func parseTimeField(v interface{}) time.Time {
	if v == nil {
		return time.Time{}
	}
	switch t := v.(type) {
	case int64:
		return time.Unix(t, 0)
	case float64:
		return time.Unix(int64(t), 0)
	case time.Time:
		return t
	case string:
		// 尝试解析常见的时间格式
		formats := []string{
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05Z",
			"2006-01-02T15:04:05-07:00",
			time.RFC3339,
		}
		for _, format := range formats {
			if parsed, err := time.Parse(format, t); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

// KlineFile K线文件元信息
type KlineFile struct {
	ID          int        `json:"id"`
	Filename    string     `json:"filename"`
	Exchange    string     `json:"exchange"`
	Symbol      string     `json:"symbol"`
	Interval    string     `json:"interval"`
	StartTime   time.Time  `json:"start_time"`
	EndTime     *time.Time `json:"end_time,omitempty"` // 采集中时为 nil
	Status      string     `json:"status"`             // collecting | completed | error
	HasDepth    bool       `json:"has_depth"`
	CandleCount int        `json:"candle_count"`
	FileSize    int64      `json:"file_size"` // 文件大小（字节）
	Source      string     `json:"source"`    // collector | backtest_cache | manual
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// klineFilesIntervalCol 列名 interval 在 MySQL 中為保留字，必須用反引號；SQLite 亦接受反引號標識符。
// 始終使用反引號，避免僅依賴 dbType 時漏判導致 Error 1064。
const klineFilesIntervalCol = "`interval`"

func (s *SQLiteStorage) klineFilesSelectColumns() string {
	return fmt.Sprintf("id, filename, exchange, symbol, %s, start_time, end_time, status, has_depth, candle_count, file_size, source, created_at, updated_at", klineFilesIntervalCol)
}

// CreateKlineFile 创建 K 线文件记录
func (s *SQLiteStorage) CreateKlineFile(kf *KlineFile) error {
	query := fmt.Sprintf(`
		INSERT INTO kline_files (filename, exchange, symbol, %s, start_time, end_time, status, has_depth, candle_count, file_size, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, klineFilesIntervalCol)
	var endTime interface{}
	if kf.EndTime != nil {
		endTime = kf.EndTime.Unix()
	}

	hasDepthInt := 0
	if kf.HasDepth {
		hasDepthInt = 1
	}

	result, err := s.db.Exec(query,
		kf.Filename, kf.Exchange, kf.Symbol, kf.Interval,
		kf.StartTime.Unix(), endTime, kf.Status, hasDepthInt,
		kf.CandleCount, kf.FileSize, kf.Source)
	if err != nil {
		return fmt.Errorf("创建K线文件记录失败: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("获取插入ID失败: %w", err)
	}
	kf.ID = int(id)
	kf.CreatedAt = time.Now()
	kf.UpdatedAt = time.Now()

	return nil
}

// GetKlineFile 根据ID获取 K 线文件记录
func (s *SQLiteStorage) GetKlineFile(id int) (*KlineFile, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM kline_files WHERE id = ?
	`, s.klineFilesSelectColumns())

	kf := &KlineFile{}
	var startTime interface{}
	var endTime interface{}
	var createdAt interface{}
	var updatedAt interface{}
	var hasDepthInt int

	err := s.db.QueryRow(query, id).Scan(
		&kf.ID, &kf.Filename, &kf.Exchange, &kf.Symbol, &kf.Interval,
		&startTime, &endTime, &kf.Status, &hasDepthInt, &kf.CandleCount,
		&kf.FileSize, &kf.Source, &createdAt, &updatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("查询K线文件记录失败: %w", err)
	}

	kf.StartTime = parseTimeField(startTime)
	if endTime != nil {
		endTimeVal := parseTimeField(endTime)
		if !endTimeVal.IsZero() {
			kf.EndTime = &endTimeVal
		}
	}
	kf.HasDepth = hasDepthInt == 1
	kf.CreatedAt = parseTimeField(createdAt)
	kf.UpdatedAt = parseTimeField(updatedAt)

	return kf, nil
}

// GetKlineFileByFilename 根据文件名获取 K 线文件记录
func (s *SQLiteStorage) GetKlineFileByFilename(filename string) (*KlineFile, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM kline_files WHERE filename = ?
	`, s.klineFilesSelectColumns())

	kf := &KlineFile{}
	var startTime interface{}
	var endTime interface{}
	var createdAt interface{}
	var updatedAt interface{}
	var hasDepthInt int

	err := s.db.QueryRow(query, filename).Scan(
		&kf.ID, &kf.Filename, &kf.Exchange, &kf.Symbol, &kf.Interval,
		&startTime, &endTime, &kf.Status, &hasDepthInt, &kf.CandleCount,
		&kf.FileSize, &kf.Source, &createdAt, &updatedAt)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("查询K线文件记录失败: %w", err)
	}

	kf.StartTime = parseTimeField(startTime)
	if endTime != nil {
		endTimeVal := parseTimeField(endTime)
		if !endTimeVal.IsZero() {
			kf.EndTime = &endTimeVal
		}
	}
	kf.HasDepth = hasDepthInt == 1
	kf.CreatedAt = parseTimeField(createdAt)
	kf.UpdatedAt = parseTimeField(updatedAt)

	return kf, nil
}

// ListKlineFiles 列出 K 线文件记录
type KlineFileFilter struct {
	Exchange string
	Symbol   string
	Interval string
	Status   string
	Source   string
	Limit    int
	Offset   int
}

func (s *SQLiteStorage) ListKlineFiles(filter *KlineFileFilter) ([]*KlineFile, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM kline_files WHERE 1=1
	`, s.klineFilesSelectColumns())
	args := make([]interface{}, 0)

	if filter != nil {
		if filter.Exchange != "" {
			query += " AND exchange = ?"
			args = append(args, filter.Exchange)
		}
		if filter.Symbol != "" {
			query += " AND symbol = ?"
			args = append(args, filter.Symbol)
		}
		if filter.Interval != "" {
			query += fmt.Sprintf(" AND %s = ?", klineFilesIntervalCol)
			args = append(args, filter.Interval)
		}
		if filter.Status != "" {
			query += " AND status = ?"
			args = append(args, filter.Status)
		}
		if filter.Source != "" {
			query += " AND source = ?"
			args = append(args, filter.Source)
		}
	}

	query += " ORDER BY created_at DESC"

	if filter != nil && filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)

		if filter.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, filter.Offset)
		}
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询K线文件列表失败: %w", err)
	}
	defer rows.Close()

	var files []*KlineFile
	for rows.Next() {
		kf := &KlineFile{}
		var startTime interface{}
		var endTime interface{}
		var createdAt interface{}
		var updatedAt interface{}
		var hasDepthInt int

		err := rows.Scan(
			&kf.ID, &kf.Filename, &kf.Exchange, &kf.Symbol, &kf.Interval,
			&startTime, &endTime, &kf.Status, &hasDepthInt, &kf.CandleCount,
			&kf.FileSize, &kf.Source, &createdAt, &updatedAt)

		if err != nil {
			return nil, fmt.Errorf("扫描K线文件记录失败: %w", err)
		}

		kf.StartTime = parseTimeField(startTime)
		if endTime != nil {
			endTimeVal := parseTimeField(endTime)
			if !endTimeVal.IsZero() {
				kf.EndTime = &endTimeVal
			}
		}
		kf.HasDepth = hasDepthInt == 1
		kf.CreatedAt = parseTimeField(createdAt)
		kf.UpdatedAt = parseTimeField(updatedAt)

		files = append(files, kf)
	}

	return files, rows.Err()
}

// UpdateKlineFile 更新 K 线文件记录
func (s *SQLiteStorage) UpdateKlineFile(kf *KlineFile) error {
	query := fmt.Sprintf(`
		UPDATE kline_files 
		SET exchange=?, symbol=?, %s=?, start_time=?, end_time=?, status=?, has_depth=?, candle_count=?, file_size=?, source=?, updated_at=?
		WHERE id=?
	`, klineFilesIntervalCol)
	var endTime interface{}
	if kf.EndTime != nil {
		endTime = kf.EndTime.Unix()
	}

	hasDepthInt := 0
	if kf.HasDepth {
		hasDepthInt = 1
	}

	kf.UpdatedAt = time.Now()

	_, err := s.db.Exec(query,
		kf.Exchange, kf.Symbol, kf.Interval,
		kf.StartTime.Unix(), endTime, kf.Status, hasDepthInt,
		kf.CandleCount, kf.FileSize, kf.Source, kf.UpdatedAt.Unix(), kf.ID)

	if err != nil {
		return fmt.Errorf("更新K线文件记录失败: %w", err)
	}

	return nil
}

// UpdateKlineFileStatus 更新 K 线文件状态
func (s *SQLiteStorage) UpdateKlineFileStatus(filename string, status string, endTime *time.Time, candleCount int, fileSize int64) error {
	query := `
		UPDATE kline_files 
		SET status=?, end_time=?, candle_count=?, file_size=?, updated_at=?
		WHERE filename=?
	`
	var endTimeUnix interface{}
	if endTime != nil {
		endTimeUnix = endTime.Unix()
	}

	_, err := s.db.Exec(query, status, endTimeUnix, candleCount, fileSize, time.Now().Unix(), filename)
	if err != nil {
		return fmt.Errorf("更新K线文件状态失败: %w", err)
	}

	return nil
}

// DeleteKlineFile 删除 K 线文件记录
func (s *SQLiteStorage) DeleteKlineFile(id int) error {
	query := `DELETE FROM kline_files WHERE id = ?`

	_, err := s.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("删除K线文件记录失败: %w", err)
	}

	return nil
}

// DeleteKlineFileByFilename 根据文件名删除 K 线文件记录
func (s *SQLiteStorage) DeleteKlineFileByFilename(filename string) error {
	query := `DELETE FROM kline_files WHERE filename = ?`

	_, err := s.db.Exec(query, filename)
	if err != nil {
		return fmt.Errorf("删除K线文件记录失败: %w", err)
	}

	return nil
}

// GetCompletedKlineFiles 获取已完成的 K 线文件列表（可用于回测）
func (s *SQLiteStorage) GetCompletedKlineFiles(exchange, symbol, interval string) ([]*KlineFile, error) {
	filter := &KlineFileFilter{
		Exchange: exchange,
		Symbol:   symbol,
		Interval: interval,
		Status:   "completed",
	}
	return s.ListKlineFiles(filter)
}

// GetKlineFilesInTimeRange 获取时间范围内的 K 线文件列表
func (s *SQLiteStorage) GetKlineFilesInTimeRange(exchange, symbol, interval string, startTimeParam, endTimeParam time.Time) ([]*KlineFile, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM kline_files 
		WHERE exchange=? AND symbol=? AND %s=? AND status='completed'
		  AND start_time <= ? AND (end_time IS NULL OR end_time >= ?)
		ORDER BY start_time ASC
	`, s.klineFilesSelectColumns(), klineFilesIntervalCol)

	rows, err := s.db.Query(query, exchange, symbol, interval, endTimeParam.Unix(), startTimeParam.Unix())
	if err != nil {
		return nil, fmt.Errorf("查询时间范围内K线文件失败: %w", err)
	}
	defer rows.Close()

	var files []*KlineFile
	for rows.Next() {
		kf := &KlineFile{}
		var startTime interface{}
		var endTime interface{}
		var createdAt interface{}
		var updatedAt interface{}
		var hasDepthInt int

		err := rows.Scan(
			&kf.ID, &kf.Filename, &kf.Exchange, &kf.Symbol, &kf.Interval,
			&startTime, &endTime, &kf.Status, &hasDepthInt, &kf.CandleCount,
			&kf.FileSize, &kf.Source, &createdAt, &updatedAt)

		if err != nil {
			return nil, fmt.Errorf("扫描K线文件记录失败: %w", err)
		}

		kf.StartTime = parseTimeField(startTime)
		if endTime != nil {
			endTimeVal := parseTimeField(endTime)
			if !endTimeVal.IsZero() {
				kf.EndTime = &endTimeVal
			}
		}
		kf.HasDepth = hasDepthInt == 1
		kf.CreatedAt = parseTimeField(createdAt)
		kf.UpdatedAt = parseTimeField(updatedAt)

		files = append(files, kf)
	}

	return files, rows.Err()
}
