package storage

import (
	"database/sql"
	"encoding/json"
	"time"

	"quantmesh/backtest"
)

// CreateBacktestTask 創建回测任務（僅 SQLiteStorage 實現，Storage 接口不包含此方法，由調用方断言）
func (s *SQLiteStorage) CreateBacktestTask(task *backtest.BacktestTask) error {
	paramsJSON, err := json.Marshal(task.Params)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		INSERT INTO backtest_tasks (id, status, strategy, symbol, interval, start_time, end_time, params, total_capital, progress, created_at, started_at, completed_at, error, result_path, report_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.Status,
		task.Strategy,
		task.Symbol,
		task.Interval,
		task.StartTime.UnixMilli(),
		task.EndTime.UnixMilli(),
		string(paramsJSON),
		task.TotalCapital,
		task.Progress,
		task.CreatedAt.UnixMilli(),
		nilInt64(task.StartedAt),
		nilInt64(task.CompletedAt),
		task.Error,
		task.ResultPath,
		task.ReportPath,
	)
	return err
}

// GetBacktestTask 獲取回测任務
func (s *SQLiteStorage) GetBacktestTask(id string) (*backtest.BacktestTask, error) {
	var (
		startTime, endTime, createdAt int64
		startedAt, completedAt        sql.NullInt64
		paramsJSON                    string
	)
	task := &backtest.BacktestTask{ID: id}
	err := s.db.QueryRow(`
		SELECT status, strategy, symbol, interval, start_time, end_time, params, total_capital, progress, created_at, started_at, completed_at, error, result_path, report_path
		FROM backtest_tasks WHERE id = ?`, id).Scan(
		&task.Status,
		&task.Strategy,
		&task.Symbol,
		&task.Interval,
		&startTime,
		&endTime,
		&paramsJSON,
		&task.TotalCapital,
		&task.Progress,
		&createdAt,
		&startedAt,
		&completedAt,
		&task.Error,
		&task.ResultPath,
		&task.ReportPath,
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
	if paramsJSON != "" {
		_ = json.Unmarshal([]byte(paramsJSON), &task.Params)
	}
	if task.Params == nil {
		task.Params = make(map[string]interface{})
	}
	return task, nil
}

// ListBacktestTasks 列出回测任務（按創建時间倒序）
func (s *SQLiteStorage) ListBacktestTasks(limit, offset int) ([]*backtest.BacktestTask, error) {
	rows, err := s.db.Query(`
		SELECT id, status, strategy, symbol, interval, start_time, end_time, params, total_capital, progress, created_at, started_at, completed_at, error, result_path, report_path
		FROM backtest_tasks ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*backtest.BacktestTask
	for rows.Next() {
		var (
			startTime, endTime, createdAt int64
			startedAt, completedAt        sql.NullInt64
			paramsJSON                    string
		)
		task := &backtest.BacktestTask{}
		if err := rows.Scan(&task.ID, &task.Status, &task.Strategy, &task.Symbol, &task.Interval, &startTime, &endTime, &paramsJSON, &task.TotalCapital, &task.Progress, &createdAt, &startedAt, &completedAt, &task.Error, &task.ResultPath, &task.ReportPath); err != nil {
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
		if paramsJSON != "" {
			_ = json.Unmarshal([]byte(paramsJSON), &task.Params)
		}
		if task.Params == nil {
			task.Params = make(map[string]interface{})
		}
		list = append(list, task)
	}
	return list, rows.Err()
}

// UpdateBacktestTask 更新回测任務（僅更新状態與結果路径等）
func (s *SQLiteStorage) UpdateBacktestTask(task *backtest.BacktestTask) error {
	_, err := s.db.Exec(`
		UPDATE backtest_tasks SET status=?, progress=?, started_at=?, completed_at=?, error=?, result_path=?, report_path=?
		WHERE id=?`,
		task.Status,
		task.Progress,
		nilInt64(task.StartedAt),
		nilInt64(task.CompletedAt),
		task.Error,
		task.ResultPath,
		task.ReportPath,
		task.ID,
	)
	return err
}

// UpdateBacktestTaskParams 僅更新任務状態相关字段（供 task_manager 使用）
func (s *SQLiteStorage) UpdateBacktestTaskStatus(id, status string, progress int, startedAt, completedAt *time.Time, errMsg, resultPath, reportPath string) error {
	_, err := s.db.Exec(`
		UPDATE backtest_tasks SET status=?, progress=?, started_at=?, completed_at=?, error=?, result_path=?, report_path=?
		WHERE id=?`,
		status,
		progress,
		nilInt64(startedAt),
		nilInt64(completedAt),
		errMsg,
		resultPath,
		reportPath,
		id,
	)
	return err
}

// DeleteBacktestTask 刪除回测任務
func (s *SQLiteStorage) DeleteBacktestTask(id string) error {
	_, err := s.db.Exec(`DELETE FROM backtest_tasks WHERE id=?`, id)
	return err
}

func nilInt64(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

// GetBacktestTaskStore 返回自身作為 backtest.TaskStore 實現
func (s *SQLiteStorage) GetBacktestTaskStore() backtest.TaskStore {
	return s
}
