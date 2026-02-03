package backtest

import "time"

// BacktestTask 回测任務
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
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
	Error        string                 `json:"error,omitempty"`
	ResultPath   string                 `json:"result_path,omitempty"`
	ReportPath   string                 `json:"report_path,omitempty"`
	// 数据来源扩展字段
	DataSource   string                 `json:"data_source,omitempty"` // "time_range" | "kline_file" | "cache"
	KlineFile    string                 `json:"kline_file,omitempty"`  // K线文件名 (如 1m_binance_BTCUSDT_20260102.csv)
	CacheName    string                 `json:"cache_name,omitempty"`  // 回测缓存名称
}

// BacktestTaskResult 回测任務結果（持久化 JSON）
type BacktestTaskResult struct {
	TaskID     string            `json:"task_id"`
	Task       *BacktestTask     `json:"task"`
	Result     *BacktestResult   `json:"result"`     // 单次回测结果（非对比模式）
	Comparison *ComparisonResult `json:"comparison"` // 对比结果（网格策略带风控对比时使用）
}
