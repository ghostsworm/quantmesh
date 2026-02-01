package database

import (
	"context"
	"time"
)

// Database 數據库接口
type Database interface {
	// 交易記錄
	SaveTrade(ctx context.Context, trade *Trade) error
	GetTrades(ctx context.Context, filter *TradeFilter) ([]*Trade, error)
	BatchSaveTrades(ctx context.Context, trades []*Trade) error

	// 订單記錄
	SaveOrder(ctx context.Context, order *Order) error
	GetOrders(ctx context.Context, filter *OrderFilter) ([]*Order, error)

	// 统计數據
	SaveStatistics(ctx context.Context, stats *Statistics) error
	GetStatistics(ctx context.Context, filter *StatFilter) ([]*Statistics, error)

	// 對账記錄
	SaveReconciliation(ctx context.Context, recon *Reconciliation) error
	GetReconciliations(ctx context.Context, filter *ReconciliationFilter) ([]*Reconciliation, error)

	// 风控記錄
	SaveRiskCheck(ctx context.Context, check *RiskCheck) error
	GetRiskChecks(ctx context.Context, filter *RiskCheckFilter) ([]*RiskCheck, error)

	// 事件記錄
	SaveEvent(ctx context.Context, event *EventRecord) error
	GetEvents(ctx context.Context, filter *EventFilter) ([]*EventRecord, error)
	GetEventByID(ctx context.Context, id int64) (*EventRecord, error)
	GetEventStats(ctx context.Context) (*EventStats, error)
	CleanupOldEvents(ctx context.Context, severity string, keepCount int, keepDays int) error

	// 异步任務記錄
	SaveAsyncTask(ctx context.Context, task *AsyncTask) error
	UpdateAsyncTask(ctx context.Context, task *AsyncTask) error
	GetAsyncTask(ctx context.Context, id string) (*AsyncTask, error)
	GetPendingAsyncTasks(ctx context.Context, limit int) ([]*AsyncTask, error)
	GetAsyncTasks(ctx context.Context, filter *AsyncTaskFilter) ([]*AsyncTask, error)
	GetAsyncTaskStats(ctx context.Context, startTime, endTime *time.Time) (*AsyncTaskStats, error)
	CleanupExpiredAsyncTasks(ctx context.Context, cutoff time.Time) (int64, error)

	// 倉位计划記錄
	SavePositionPlan(ctx context.Context, plan *PositionPlan) error
	UpdatePositionPlan(ctx context.Context, plan *PositionPlan) error
	GetPositionPlan(ctx context.Context, id int64) (*PositionPlan, error)
	GetPositionPlans(ctx context.Context, filter *PositionPlanFilter) ([]*PositionPlan, error)

	// 事務支援
	BeginTx(ctx context.Context) (Tx, error)

	// 健康检查
	Ping(ctx context.Context) error

	// 关闭连接
	Close() error
}

// Tx 事務接口
type Tx interface {
	Commit() error
	Rollback() error
	Database // 继承所有數據库操作
}

// 數據模型

// Trade 交易記錄
type Trade struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Exchange  string    `gorm:"index:idx_exchange_symbol_time;size:50" json:"exchange"`
	Symbol    string    `gorm:"index:idx_exchange_symbol_time;size:50" json:"symbol"`
	OrderID   int64     `gorm:"index" json:"order_id"`
	Side      string    `gorm:"size:10" json:"side"` // BUY, SELL
	Price     float64   `json:"price"`
	Quantity  float64   `json:"quantity"`
	Amount    float64   `json:"amount"` // price * quantity
	Fee       float64   `json:"fee"`
	FeeCoin   string    `gorm:"size:20" json:"fee_coin"`
	PnL       float64   `json:"pnl"` // 盈亏
	CreatedAt time.Time `gorm:"index:idx_exchange_symbol_time" json:"created_at"`
}

// Order 订單記錄
type Order struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Exchange      string    `gorm:"index:idx_exchange_symbol;size:50" json:"exchange"`
	Symbol        string    `gorm:"index:idx_exchange_symbol;size:50" json:"symbol"`
	OrderID       int64     `gorm:"uniqueIndex" json:"order_id"`
	ClientOrderID string    `gorm:"index;size:100" json:"client_order_id"`
	Side          string    `gorm:"size:10" json:"side"` // BUY, SELL
	Type          string    `gorm:"size:20" json:"type"` // LIMIT, MARKET
	Price         float64   `json:"price"`
	Quantity      float64   `json:"quantity"`
	FilledQty     float64   `json:"filled_qty"`
	Status        string    `gorm:"index;size:20" json:"status"` // NEW, FILLED, CANCELED
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Statistics 统计數據
type Statistics struct {
	ID         int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Exchange   string    `gorm:"index:idx_exchange_symbol_date;size:50" json:"exchange"`
	Symbol     string    `gorm:"index:idx_exchange_symbol_date;size:50" json:"symbol"`
	Date       time.Time `gorm:"index:idx_exchange_symbol_date" json:"date"`
	TotalPnL   float64   `json:"total_pnl"`
	WinRate    float64   `json:"win_rate"`
	Volume     float64   `json:"volume"`
	TradeCount int       `json:"trade_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// Reconciliation 對账記錄
type Reconciliation struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Exchange    string     `gorm:"index:idx_exchange_symbol;size:50" json:"exchange"`
	Symbol      string     `gorm:"index:idx_exchange_symbol;size:50" json:"symbol"`
	Type        string     `gorm:"size:50" json:"type"` // order_diff, position_diff
	LocalValue  string     `gorm:"type:text" json:"local_value"`
	RemoteValue string     `gorm:"type:text" json:"remote_value"`
	Diff        string     `gorm:"type:text" json:"diff"`
	Resolved    bool       `gorm:"index" json:"resolved"`
	ResolvedAt  *time.Time `json:"resolved_at"`
	CreatedAt   time.Time  `gorm:"index" json:"created_at"`
}

// RiskCheck 风控检查記錄
type RiskCheck struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Exchange  string    `gorm:"index:idx_exchange_symbol;size:50" json:"exchange"`
	Symbol    string    `gorm:"index:idx_exchange_symbol;size:50" json:"symbol"`
	IsHealthy bool      `gorm:"index" json:"is_healthy"`
	Reason    string    `gorm:"type:text" json:"reason"`
	Details   string    `gorm:"type:text" json:"details"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// EventRecord 事件記錄
type EventRecord struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Type      string    `gorm:"index:idx_type_severity;size:50" json:"type"`      // 事件類型
	Severity  string    `gorm:"index:idx_type_severity;size:20" json:"severity"`  // 严重程度: critical/warning/info
	Source    string    `gorm:"index;size:20" json:"source"`                      // 事件源: exchange/network/system/strategy/risk/api
	Exchange  string    `gorm:"index:idx_exchange_symbol;size:50" json:"exchange"` // 交易所（可選）
	Symbol    string    `gorm:"index:idx_exchange_symbol;size:50" json:"symbol"`   // 交易對（可選）
	Title     string    `gorm:"size:200" json:"title"`                            // 事件標题
	Message   string    `gorm:"type:text" json:"message"`                         // 事件消息
	Details   string    `gorm:"type:text" json:"details"`                         // 详细信息（JSON）
	CreatedAt time.Time `gorm:"index" json:"created_at"`                          // 創建時间
}

// TableName 指定表名為 events（兼容舊數據）
func (EventRecord) TableName() string {
	return "events"
}

// AsyncTask 异步 AI 任務記錄
type AsyncTask struct {
	ID               string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	TaskType         string     `gorm:"type:varchar(50);not null" json:"task_type"`
	Status           string     `gorm:"type:varchar(20);not null;default:'pending'" json:"status"`
	RequestData      string     `gorm:"type:text;not null" json:"request_data"` // 使用 string 存儲 JSON 以提高兼容性
	Result           string     `gorm:"type:text" json:"result"`
	ErrorMessage     *string    `gorm:"type:text" json:"error_message"`
	Model            *string    `gorm:"type:varchar(50)" json:"model"`
	AIInput          *string    `gorm:"type:text" json:"ai_input"`
	AIOutput         *string    `gorm:"type:text" json:"ai_output"`
	InputTokens      int64      `gorm:"type:bigint;default:0" json:"input_tokens"`
	OutputTokens     int64      `gorm:"type:bigint;default:0" json:"output_tokens"`
	ProcessingTimeMs int64      `gorm:"type:bigint;default:0" json:"processing_time_ms"`
	UsedAPIKey       *string    `gorm:"type:varchar(100)" json:"used_api_key"`
	RetryCount       int        `gorm:"type:int;default:0" json:"retry_count"`
	MaxRetries       int        `gorm:"type:int;default:15" json:"max_retries"`
	TimeoutSeconds   int        `gorm:"type:int;default:300" json:"timeout_seconds"` // 5 分鐘，超時由 processor 定期標記
	CreatedAt        time.Time  `gorm:"type:timestamp;default:CURRENT_TIMESTAMP" json:"created_at"`
	StartedAt        *time.Time `gorm:"type:datetime" json:"started_at"`
	CompletedAt      *time.Time `gorm:"type:datetime" json:"completed_at"`
	ExpiresAt        *time.Time `gorm:"type:datetime" json:"expires_at"`
}

func (AsyncTask) TableName() string {
	return "async_tasks"
}

// PositionPlan 倉位目標计划
type PositionPlan struct {
	ID               int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Exchange         string     `gorm:"index:idx_plan_exchange_symbol;size:50" json:"exchange"`
	Symbol           string     `gorm:"index:idx_plan_exchange_symbol;size:50" json:"symbol"`
	StrategyID       string     `gorm:"size:100" json:"strategy_id"` // 可選，空表示所有策略
	TargetAmountUSDT float64    `gorm:"type:decimal(20,8)" json:"target_amount_usdt"`
	Direction        string     `gorm:"size:20" json:"direction"` // reduce / increase
	Status           string     `gorm:"index;size:20" json:"status"` // pending / in_progress / completed / cancelled
	InitialAmount    float64    `gorm:"type:decimal(20,8)" json:"initial_amount"`
	CurrentAmount    float64    `gorm:"type:decimal(20,8)" json:"current_amount"`
	NotifyOnComplete bool       `json:"notify_on_complete"`
	AutoAdjustLimit  bool       `json:"auto_adjust_limit"`
	OriginalLimit    float64    `gorm:"type:decimal(20,8)" json:"original_limit"`
	CreatedAt        time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

func (PositionPlan) TableName() string {
	return "position_plans"
}

// EventStats 事件统计
type EventStats struct {
	TotalCount       int            `json:"total_count"`
	CriticalCount    int            `json:"critical_count"`
	WarningCount     int            `json:"warning_count"`
	InfoCount        int            `json:"info_count"`
	CountByType      map[string]int `json:"count_by_type"`
	CountBySource    map[string]int `json:"count_by_source"`
	Last24HoursCount int            `json:"last_24_hours_count"`
}

// 過滤器

// TradeFilter 交易記錄過滤器
type TradeFilter struct {
	Exchange  string
	Symbol    string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// OrderFilter 订單記錄過滤器
type OrderFilter struct {
	Exchange string
	Symbol   string
	Status   string
	Limit    int
	Offset   int
}

// StatFilter 统计數據過滤器
type StatFilter struct {
	Exchange  string
	Symbol    string
	StartDate *time.Time
	EndDate   *time.Time
	Limit     int
	Offset    int
}

// ReconciliationFilter 對账記錄過滤器
type ReconciliationFilter struct {
	Exchange  string
	Symbol    string
	Type      string
	Resolved  *bool
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// RiskCheckFilter 风控記錄過滤器
type RiskCheckFilter struct {
	Exchange  string
	Symbol    string
	IsHealthy *bool
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// EventFilter 事件記錄過滤器
type EventFilter struct {
	Type      string     // 事件類型筛选
	Severity  string     // 严重程度筛选
	Source    string     // 事件源筛选
	Exchange  string     // 交易所筛选
	Symbol    string     // 交易對筛选
	StartTime *time.Time // 开始時间
	EndTime   *time.Time // 結束時间
	Limit     int        // 限制數量
	Offset    int        // 偏移量
}

// AsyncTaskFilter 异步任務過滤器
type AsyncTaskFilter struct {
	Status    string     // 任務状態：pending, running, completed, failed, timeout
	TaskType  string     // 任務類型：generate_content, generate_config 等
	StartTime *time.Time // 开始時间
	EndTime   *time.Time // 結束時间
	Limit     int        // 限制數量
	Offset    int        // 偏移量
}

// PositionPlanFilter 倉位计划過滤器
type PositionPlanFilter struct {
	Exchange string // 交易所
	Symbol   string // 交易對
	Status   string // pending / in_progress / completed / cancelled
	Limit    int
	Offset   int
}

// AsyncTaskStats 异步任務统计
type AsyncTaskStats struct {
	TotalTasks        int              `json:"total_tasks"`
	TotalInputTokens  int64            `json:"total_input_tokens"`
	TotalOutputTokens int64            `json:"total_output_tokens"`
	TotalTokens       int64            `json:"total_tokens"`
	TodayInputTokens  int64            `json:"today_input_tokens"`
	TodayOutputTokens int64            `json:"today_output_tokens"`
	TodayTokens       int64            `json:"today_tokens"`
	DailyStats        []DailyTokenStat `json:"daily_stats"` // 每天的使用量
}

// DailyTokenStat 每日Token统计
type DailyTokenStat struct {
	Date        time.Time `json:"date"`
	InputTokens int64     `json:"input_tokens"`
	OutputTokens int64    `json:"output_tokens"`
	TotalTokens int64     `json:"total_tokens"`
	TaskCount   int       `json:"task_count"`
}
