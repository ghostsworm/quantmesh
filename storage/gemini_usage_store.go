package storage

import (
	"database/sql"
	"fmt"
	"time"

	"quantmesh/logger"
)

// migrateGeminiUsageTable 創建 gemini_usage 表（SQLite，由 createTables 調用）
func migrateGeminiUsageTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS gemini_usage (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			called_at TIMESTAMP NOT NULL,
			model TEXT,
			source TEXT,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_gemini_usage_called_at ON gemini_usage(called_at);
	`)
	return err
}

// migrateGeminiUsageTableMySQL 創建 gemini_usage 表（MySQL 路徑不跑 createTables）
func migrateGeminiUsageTableMySQL(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS gemini_usage (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  called_at DATETIME(3) NOT NULL,
  model VARCHAR(512) DEFAULT '',
  source VARCHAR(255) DEFAULT '',
  input_tokens BIGINT NOT NULL DEFAULT 0,
  output_tokens BIGINT NOT NULL DEFAULT 0,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_gemini_usage_called_at (called_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
`)
	if err != nil {
		return err
	}
	logger.Info("✅ MySQL gemini_usage 表已就緒")
	return nil
}

// SaveGeminiUsageRecord 持久化單次 Gemini 調用用量
func (s *SQLStorage) SaveGeminiUsageRecord(rec *GeminiUsageRecord) error {
	if s == nil || rec == nil {
		return nil
	}
	if rec.CalledAt.IsZero() {
		rec.CalledAt = time.Now().UTC()
	}
	now := time.Now().UTC()
	rec.CreatedAt = now

	result, err := s.db.Exec(`
		INSERT INTO gemini_usage (called_at, model, source, input_tokens, output_tokens, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, rec.CalledAt, rec.Model, rec.Source, rec.InputTokens, rec.OutputTokens, rec.DurationMs, rec.CreatedAt)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err == nil {
		rec.ID = id
	}
	return nil
}

func buildGeminiUsageWhere(startTime, endTime *time.Time) (where string, args []interface{}) {
	args = []interface{}{}
	if startTime != nil && endTime != nil {
		return "called_at >= ? AND called_at <= ?", []interface{}{*startTime, *endTime}
	}
	if startTime != nil {
		return "called_at >= ?", []interface{}{*startTime}
	}
	if endTime != nil {
		return "called_at <= ?", []interface{}{*endTime}
	}
	return "1=1", args
}

// QueryGeminiUsageRecords 分頁查詢（按 called_at 降序）
func (s *SQLStorage) QueryGeminiUsageRecords(startTime, endTime *time.Time, limit, offset int) ([]*GeminiUsageRecord, int64, error) {
	if s == nil {
		return nil, 0, fmt.Errorf("storage nil")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	where, wargs := buildGeminiUsageWhere(startTime, endTime)

	countSQL := "SELECT COUNT(*) FROM gemini_usage WHERE " + where
	var total int64
	if err := s.db.QueryRow(countSQL, wargs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args := append(append([]interface{}{}, wargs...), limit, offset)
	q := `
		SELECT id, called_at, model, source, input_tokens, output_tokens, duration_ms, created_at
		FROM gemini_usage
		WHERE ` + where + `
		ORDER BY called_at DESC
		LIMIT ? OFFSET ?`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []*GeminiUsageRecord
	for rows.Next() {
		var r GeminiUsageRecord
		var model, source sql.NullString
		if err := rows.Scan(&r.ID, &r.CalledAt, &model, &source, &r.InputTokens, &r.OutputTokens, &r.DurationMs, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		if model.Valid {
			r.Model = model.String
		}
		if source.Valid {
			r.Source = source.String
		}
		list = append(list, &r)
	}
	return list, total, rows.Err()
}

// AggregateGeminiUsageTotals 在時間範圍內聚合次數與 token
func (s *SQLStorage) AggregateGeminiUsageTotals(startTime, endTime *time.Time) (callCount int, inTok, outTok int64, err error) {
	if s == nil {
		return 0, 0, 0, fmt.Errorf("storage nil")
	}
	where, wargs := buildGeminiUsageWhere(startTime, endTime)
	sqlStr := `SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0) FROM gemini_usage WHERE ` + where
	var cnt int64
	var sumIn, sumOut sql.NullInt64
	if err := s.db.QueryRow(sqlStr, wargs...).Scan(&cnt, &sumIn, &sumOut); err != nil {
		return 0, 0, 0, err
	}
	inTok = sumIn.Int64
	outTok = sumOut.Int64
	return int(cnt), inTok, outTok, nil
}
