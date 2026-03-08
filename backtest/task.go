package backtest

import "time"

type TaskMode string

const (
	TaskModeSingleStrategy TaskMode = "single_strategy"
	TaskModeBotStrategies  TaskMode = "bot_strategies"
	TaskModeHedgeGroup     TaskMode = "hedge_group"
	TaskModeHybridStrategy TaskMode = "hybrid_strategy"
)

type TaskStrategy struct {
	ID     string                 `json:"id,omitempty"`
	Name   string                 `json:"name,omitempty"`
	Type   string                 `json:"type"`
	Weight float64                `json:"weight"`
	Config map[string]interface{} `json:"config,omitempty"`
}

// TaskSubStrategy 子策略配置（用于混合策略回测）
type TaskSubStrategy struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name,omitempty"`
	Type    string                 `json:"type"`
	Role    string                 `json:"role"`     // primary, signal, hybrid, monitor
	Weight  float64                `json:"weight"`
	Enabled bool                   `json:"enabled"`
	Config  map[string]interface{} `json:"config,omitempty"`
}

// TaskCollaborationRule 协作规则（用于混合策略回测）
type TaskCollaborationRule struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Priority    int                    `json:"priority"`
	Enabled     bool                   `json:"enabled"`
	When        TaskSignalCondition    `json:"when"`
	Then        []TaskAction           `json:"then"`
}

// TaskSignalCondition 信号条件
type TaskSignalCondition struct {
	SourceStrategy string                 `json:"source_strategy"`
	SignalType     string                 `json:"signal_type"`
	Operator       string                 `json:"operator"`
	Value          interface{}            `json:"value"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// TaskAction 动作
type TaskAction struct {
	TargetStrategy string                 `json:"target_strategy"`
	Operation      string                 `json:"operation"`
	Condition      string                 `json:"condition,omitempty"`
	Params         map[string]interface{} `json:"params,omitempty"`
}

// TaskHybridStrategyConfig 混合策略配置
type TaskHybridStrategyConfig struct {
	Name                string                  `json:"name"`
	Description         string                  `json:"description,omitempty"`
	SubStrategies       []TaskSubStrategy       `json:"sub_strategies"`
	CollaborationRules  []TaskCollaborationRule `json:"collaboration_rules"`
	GlobalSettings      map[string]interface{}  `json:"global_settings,omitempty"`
}

// BacktestTask 回测任務
type BacktestTask struct {
	ID              string                      `json:"id"`
	Status          string                      `json:"status"` // pending, running, completed, failed
	Mode            TaskMode                    `json:"mode,omitempty"`
	BotID           string                      `json:"bot_id,omitempty"`
	GroupID         string                      `json:"group_id,omitempty"`
	Strategy        string                      `json:"strategy"`
	Strategies      []TaskStrategy              `json:"strategies,omitempty"`
	HybridStrategy  *TaskHybridStrategyConfig   `json:"hybrid_strategy,omitempty"` // 混合策略配置
	Symbol          string                      `json:"symbol"`
	Interval        string                      `json:"interval"`
	StartTime       time.Time                   `json:"start_time"`
	EndTime         time.Time                   `json:"end_time"`
	Params          map[string]interface{}      `json:"params"`
	TotalCapital    float64                     `json:"total_capital"`
	Leverage        float64                     `json:"leverage"`         // 杠杆倍数 (1-20)
	MaxCapitalRatio float64                     `json:"max_capital_ratio"` // 最大资金占用比例 (0.1-1.0, 如 0.2 表示最多用 20%)
	Progress        int                         `json:"progress"` // 0-100
	CreatedAt       time.Time                   `json:"created_at"`
	StartedAt       *time.Time                  `json:"started_at,omitempty"`
	CompletedAt     *time.Time                  `json:"completed_at,omitempty"`
	Error           string                      `json:"error,omitempty"`
	ResultPath      string                      `json:"result_path,omitempty"`
	ReportPath      string                      `json:"report_path,omitempty"`
	// 數據来源扩展字段
	DataSource string `json:"data_source,omitempty"` // "time_range" | "kline_file" | "cache"
	KlineFile  string `json:"kline_file,omitempty"`  // K線檔案名 (如 1m_binance_BTCUSDT_20260102.csv)
	CacheName  string `json:"cache_name,omitempty"`  // 回测缓存名称
}

// BacktestTaskResult 回测任務結果（持久化 JSON）
type BacktestTaskResult struct {
	TaskID      string               `json:"task_id"`
	Task        *BacktestTask        `json:"task"`
	Result      *BacktestResult      `json:"result"` // 单次回测結果（非对比模式）
	MultiResult *MultiStrategyResult `json:"multi_result,omitempty"`
	HedgeResult *HedgePairResult     `json:"hedge_result,omitempty"`
	Comparison  *ComparisonResult    `json:"comparison"` // 对比結果（网格策略带风控对比时使用）
}
