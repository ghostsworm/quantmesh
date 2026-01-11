package database

import (
	"context"
	"time"
)

// Database 数据库接口
type Database interface {
	// 交易记录
	SaveTrade(ctx context.Context, trade *Trade) error
	GetTrades(ctx context.Context, filter *TradeFilter) ([]*Trade, error)
	BatchSaveTrades(ctx context.Context, trades []*Trade) error

	// 订单记录
	SaveOrder(ctx context.Context, order *Order) error
	GetOrders(ctx context.Context, filter *OrderFilter) ([]*Order, error)

	// 统计数据
	SaveStatistics(ctx context.Context, stats *Statistics) error
	GetStatistics(ctx context.Context, filter *StatFilter) ([]*Statistics, error)

	// 对账记录
	SaveReconciliation(ctx context.Context, recon *Reconciliation) error
	GetReconciliations(ctx context.Context, filter *ReconciliationFilter) ([]*Reconciliation, error)

	// 风控记录
	SaveRiskCheck(ctx context.Context, check *RiskCheck) error
	GetRiskChecks(ctx context.Context, filter *RiskCheckFilter) ([]*RiskCheck, error)

	// 事件记录
	SaveEvent(ctx context.Context, event *EventRecord) error
	GetEvents(ctx context.Context, filter *EventFilter) ([]*EventRecord, error)
	GetEventByID(ctx context.Context, id int64) (*EventRecord, error)
	GetEventStats(ctx context.Context) (*EventStats, error)
	CleanupOldEvents(ctx context.Context, severity string, keepCount int, keepDays int) error

	// 事务支持
	BeginTx(ctx context.Context) (Tx, error)

	// 健康检查
	Ping(ctx context.Context) error

	// 关闭连接
	Close() error
}

// Tx 事务接口
type Tx interface {
	Commit() error
	Rollback() error
	Database // 继承所有数据库操作
}

// 数据模型

// Trade 交易记录
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

// Order 订单记录
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

// Statistics 统计数据
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

// Reconciliation 对账记录
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

// RiskCheck 风控检查记录
type RiskCheck struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Exchange  string    `gorm:"index:idx_exchange_symbol;size:50" json:"exchange"`
	Symbol    string    `gorm:"index:idx_exchange_symbol;size:50" json:"symbol"`
	IsHealthy bool      `gorm:"index" json:"is_healthy"`
	Reason    string    `gorm:"type:text" json:"reason"`
	Details   string    `gorm:"type:text" json:"details"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// EventRecord 事件记录
type EventRecord struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Type      string    `gorm:"index:idx_type_severity;size:50" json:"type"`      // 事件类型
	Severity  string    `gorm:"index:idx_type_severity;size:20" json:"severity"`  // 严重程度: critical/warning/info
	Source    string    `gorm:"index;size:20" json:"source"`                      // 事件源: exchange/network/system/strategy/risk/api
	Exchange  string    `gorm:"index:idx_exchange_symbol;size:50" json:"exchange"` // 交易所（可选）
	Symbol    string    `gorm:"index:idx_exchange_symbol;size:50" json:"symbol"`   // 交易对（可选）
	Title     string    `gorm:"size:200" json:"title"`                            // 事件标题
	Message   string    `gorm:"type:text" json:"message"`                         // 事件消息
	Details   string    `gorm:"type:text" json:"details"`                         // 详细信息（JSON）
	CreatedAt time.Time `gorm:"index" json:"created_at"`                          // 创建时间
}

// TableName 指定表名为 events（兼容旧数据）
func (EventRecord) TableName() string {
	return "events"
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

// 过滤器

// TradeFilter 交易记录过滤器
type TradeFilter struct {
	Exchange  string
	Symbol    string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// OrderFilter 订单记录过滤器
type OrderFilter struct {
	Exchange string
	Symbol   string
	Status   string
	Limit    int
	Offset   int
}

// StatFilter 统计数据过滤器
type StatFilter struct {
	Exchange  string
	Symbol    string
	StartDate *time.Time
	EndDate   *time.Time
	Limit     int
	Offset    int
}

// ReconciliationFilter 对账记录过滤器
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

// RiskCheckFilter 风控记录过滤器
type RiskCheckFilter struct {
	Exchange  string
	Symbol    string
	IsHealthy *bool
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// EventFilter 事件记录过滤器
type EventFilter struct {
	Type      string     // 事件类型筛选
	Severity  string     // 严重程度筛选
	Source    string     // 事件源筛选
	Exchange  string     // 交易所筛选
	Symbol    string     // 交易对筛选
	StartTime *time.Time // 开始时间
	EndTime   *time.Time // 结束时间
	Limit     int        // 限制数量
	Offset    int        // 偏移量
}
