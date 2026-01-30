package backtest

import "time"

// BacktestTask 回测任务
type BacktestTask struct {
	ID           string                 `json:"id"`
	Status       string                 `json:"status"` // pending, running, completed, failed
	Strategy     string                 `json:"strategy"`
	Symbol       string                 `json:"symbol"`
	Interval     string                 `json:"interval"`
	StartTime    time.Time              `json:"start_time"`
	EndTime      time.Time              `json:"end_time"`
	Params       map[string]interface{} `json:"params"`
	TotalCapital float64                `json:"total_capital"`
	Progress     int                    `json:"progress"` // 0-100
	CreatedAt    time.Time              `json:"created_at"`
	StartedAt   *time.Time             `json:"started_at,omitempty"`
	CompletedAt *time.Time             `json:"completed_at,omitempty"`
	Error       string                  `json:"error,omitempty"`
	ResultPath  string                  `json:"result_path,omitempty"`
	ReportPath  string                  `json:"report_path,omitempty"`
}

// BacktestTaskResult 回测任务结果（持久化 JSON）
type BacktestTaskResult struct {
	TaskID string         `json:"task_id"`
	Task   *BacktestTask  `json:"task"`
	Result *BacktestResult `json:"result"`
}
