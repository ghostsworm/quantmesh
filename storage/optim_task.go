package storage

import (
	"database/sql"
	"time"

	"quantmesh/backtest"
)

func nilInt64Opt(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

// CreateOptimTask 创建参数优化任务
func (s *SQLStorage) CreateOptimTask(task *backtest.OptimTask) error {
	searchSpaceJSON, err := task.SearchSpaceToJSON()
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO optim_tasks (id, status, strategy, symbol, `+s.mysqlQuoteIdent("interval")+`, start_time, end_time, total_capital, search_space, progress, total_combos, completed_combos, created_at, started_at, completed_at, result_path, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.Status,
		task.Strategy,
		task.Symbol,
		task.Interval,
		task.StartTime.UnixMilli(),
		task.EndTime.UnixMilli(),
		task.TotalCapital,
		searchSpaceJSON,
		task.Progress,
		task.TotalCombos,
		task.CompletedCombos,
		task.CreatedAt.UnixMilli(),
		nilInt64Opt(task.StartedAt),
		nilInt64Opt(task.CompletedAt),
		task.ResultPath,
		task.Error,
	)
	return err
}

// GetOptimTask 获取参数优化任务
func (s *SQLStorage) GetOptimTask(id string) (*backtest.OptimTask, error) {
	var (
		startTime, endTime, createdAt int64
		startedAt, completedAt        sql.NullInt64
		searchSpaceJSON               string
	)
	task := &backtest.OptimTask{ID: id}
	err := s.db.QueryRow(`
		SELECT status, strategy, symbol, `+s.mysqlQuoteIdent("interval")+`, start_time, end_time, total_capital, search_space, progress, total_combos, completed_combos, created_at, started_at, completed_at, result_path, error
		FROM optim_tasks WHERE id = ?`, id).Scan(
		&task.Status,
		&task.Strategy,
		&task.Symbol,
		&task.Interval,
		&startTime,
		&endTime,
		&task.TotalCapital,
		&searchSpaceJSON,
		&task.Progress,
		&task.TotalCombos,
		&task.CompletedCombos,
		&createdAt,
		&startedAt,
		&completedAt,
		&task.ResultPath,
		&task.Error,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	task.StartTime = time.UnixMilli(startTime)
	task.EndTime = time.UnixMilli(endTime)
	task.CreatedAt = time.UnixMilli(createdAt)
	if startedAt.Valid {
		t := time.UnixMilli(startedAt.Int64)
		task.StartedAt = &t
	}
	if completedAt.Valid {
		t := time.UnixMilli(completedAt.Int64)
		task.CompletedAt = &t
	}
	if searchSpaceJSON != "" {
		_ = task.SearchSpaceFromJSON(searchSpaceJSON)
	}
	return task, nil
}

// ListOptimTasks 列出参数优化任务
func (s *SQLStorage) ListOptimTasks(limit, offset int) ([]*backtest.OptimTask, error) {
	rows, err := s.db.Query(`
		SELECT id, status, strategy, symbol, `+s.mysqlQuoteIdent("interval")+`, start_time, end_time, total_capital, search_space, progress, total_combos, completed_combos, created_at, started_at, completed_at, result_path, error
		FROM optim_tasks ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*backtest.OptimTask
	for rows.Next() {
		var (
			startTime, endTime, createdAt int64
			startedAt, completedAt        sql.NullInt64
			searchSpaceJSON               string
		)
		task := &backtest.OptimTask{}
		if err := rows.Scan(&task.ID, &task.Status, &task.Strategy, &task.Symbol, &task.Interval, &startTime, &endTime, &task.TotalCapital, &searchSpaceJSON, &task.Progress, &task.TotalCombos, &task.CompletedCombos, &createdAt, &startedAt, &completedAt, &task.ResultPath, &task.Error); err != nil {
			return nil, err
		}
		task.StartTime = time.UnixMilli(startTime)
		task.EndTime = time.UnixMilli(endTime)
		task.CreatedAt = time.UnixMilli(createdAt)
		if startedAt.Valid {
			t := time.UnixMilli(startedAt.Int64)
			task.StartedAt = &t
		}
		if completedAt.Valid {
			t := time.UnixMilli(completedAt.Int64)
			task.CompletedAt = &t
		}
		if searchSpaceJSON != "" {
			_ = task.SearchSpaceFromJSON(searchSpaceJSON)
		}
		list = append(list, task)
	}
	return list, rows.Err()
}

// UpdateOptimTaskProgress 更新任务进度
func (s *SQLStorage) UpdateOptimTaskProgress(id string, completed int, progress int) error {
	_, err := s.db.Exec(`
		UPDATE optim_tasks SET completed_combos=?, progress=? WHERE id=?`,
		completed, progress, id,
	)
	return err
}

// UpdateOptimTaskStatus 更新任务状态（startedAt 用于 running 时，completedAt 用于 completed/failed 时）
func (s *SQLStorage) UpdateOptimTaskStatus(id, status string, startedAt, completedAt *time.Time, errMsg, resultPath string) error {
	_, err := s.db.Exec(`
		UPDATE optim_tasks SET status=?, started_at=COALESCE(?, started_at), completed_at=COALESCE(?, completed_at), error=?, result_path=? WHERE id=?`,
		status,
		nilInt64Opt(startedAt),
		nilInt64Opt(completedAt),
		errMsg,
		resultPath,
		id,
	)
	return err
}

// DeleteOptimTask 删除参数优化任务
func (s *SQLStorage) DeleteOptimTask(id string) error {
	_, err := s.db.Exec(`DELETE FROM optim_tasks WHERE id=?`, id)
	return err
}

// GetOptimTaskStore 返回自身作为 backtest.OptimTaskStore 实现
func (s *SQLStorage) GetOptimTaskStore() backtest.OptimTaskStore {
	return s
}
