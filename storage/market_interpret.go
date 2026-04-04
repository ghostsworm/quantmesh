package storage

import (
	"database/sql"
	"time"
)

// MarketInterpretRecord 市场 AI 解读任务记录（持久化）
type MarketInterpretRecord struct {
	TaskID    string    `json:"task_id"`
	PageType  string    `json:"page_type"`  // "basis" | "funding"
	Symbol    string    `json:"symbol"`
	Status    string    `json:"status"`     // pending | running | completed | failed
	Progress  int       `json:"progress"`
	Result    string    `json:"result,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SaveMarketInterpretTask 创建或更新市场解读任务记录
func (s *SQLStorage) SaveMarketInterpretTask(r *MarketInterpretRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO market_interpret_tasks (task_id, page_type, symbol, status, progress, result, error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET
			status = excluded.status,
			progress = excluded.progress,
			result = excluded.result,
			error = excluded.error,
			updated_at = excluded.updated_at
	`,
		r.TaskID, r.PageType, r.Symbol, r.Status, r.Progress, r.Result, r.Error,
		r.CreatedAt.UnixMilli(), r.UpdatedAt.UnixMilli(),
	)
	return err
}

// GetMarketInterpretTask 根据 task_id 获取任务
func (s *SQLStorage) GetMarketInterpretTask(taskID string) (*MarketInterpretRecord, error) {
	var createdAt, updatedAt int64
	r := &MarketInterpretRecord{TaskID: taskID}
	err := s.db.QueryRow(`
		SELECT page_type, symbol, status, progress, COALESCE(result,''), COALESCE(error,''), created_at, updated_at
		FROM market_interpret_tasks WHERE task_id = ?
	`, taskID).Scan(&r.PageType, &r.Symbol, &r.Status, &r.Progress, &r.Result, &r.Error, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.CreatedAt = time.UnixMilli(createdAt)
	r.UpdatedAt = time.UnixMilli(updatedAt)
	return r, nil
}

// GetLatestMarketInterpretByPageType 获取指定页面类型下最新一条任务（用于返回页面时恢复显示）
func (s *SQLStorage) GetLatestMarketInterpretByPageType(pageType string) (*MarketInterpretRecord, error) {
	var createdAt, updatedAt int64
	r := &MarketInterpretRecord{}
	err := s.db.QueryRow(`
		SELECT task_id, page_type, symbol, status, progress, COALESCE(result,''), COALESCE(error,''), created_at, updated_at
		FROM market_interpret_tasks WHERE page_type = ?
		ORDER BY created_at DESC LIMIT 1
	`, pageType).Scan(&r.TaskID, &r.PageType, &r.Symbol, &r.Status, &r.Progress, &r.Result, &r.Error, &createdAt, &updatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.CreatedAt = time.UnixMilli(createdAt)
	r.UpdatedAt = time.UnixMilli(updatedAt)
	return r, nil
}

// ListMarketInterpretHistory 按页面类型列出历史解读（倒序）
func (s *SQLStorage) ListMarketInterpretHistory(pageType string, limit int) ([]*MarketInterpretRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(`
		SELECT task_id, page_type, symbol, status, progress, COALESCE(result,''), COALESCE(error,''), created_at, updated_at
		FROM market_interpret_tasks WHERE page_type = ?
		ORDER BY created_at DESC LIMIT ?
	`, pageType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*MarketInterpretRecord
	for rows.Next() {
		var createdAt, updatedAt int64
		r := &MarketInterpretRecord{}
		if err := rows.Scan(&r.TaskID, &r.PageType, &r.Symbol, &r.Status, &r.Progress, &r.Result, &r.Error, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		r.CreatedAt = time.UnixMilli(createdAt)
		r.UpdatedAt = time.UnixMilli(updatedAt)
		list = append(list, r)
	}
	return list, rows.Err()
}
