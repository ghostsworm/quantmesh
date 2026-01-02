package storage

import "time"

// Order 订单模型
type Order struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          string
	Price         float64
	Quantity      float64
	Status        string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Position 持仓模型
type Position struct {
	SlotPrice    float64
	Symbol       string
	Size         float64
	EntryPrice   float64
	CurrentPrice float64
	PnL          float64
	OpenedAt     time.Time
	ClosedAt     *time.Time
}

// Trade 交易模型（买卖配对）
type Trade struct {
	BuyOrderID  int64
	SellOrderID int64
	Symbol      string
	BuyPrice    float64
	SellPrice   float64
	Quantity    float64
	PnL         float64
	CreatedAt   time.Time
}

// Statistics 统计模型
type Statistics struct {
	Date        time.Time
	TotalTrades int
	TotalVolume float64
	TotalPnL    float64
	WinRate     float64
	CreatedAt   time.Time
}

// DailyStatisticsWithTradeCount 每日统计（包含盈利/亏损交易数）
type DailyStatisticsWithTradeCount struct {
	Date          time.Time
	TotalTrades   int
	TotalVolume   float64
	TotalPnL      float64
	WinRate       float64
	WinningTrades int
	LosingTrades  int
}

// SystemMetrics 系统监控细粒度数据模型
type SystemMetrics struct {
	ID            int64
	Timestamp     time.Time
	CPUPercent    float64
	MemoryMB      float64
	MemoryPercent float64
	ProcessID     int
	CreatedAt     time.Time
}

// DailySystemMetrics 系统监控每日汇总数据模型
type DailySystemMetrics struct {
	ID            int64
	Date          time.Time
	AvgCPUPercent float64
	MaxCPUPercent float64
	MinCPUPercent float64
	AvgMemoryMB   float64
	MaxMemoryMB   float64
	MinMemoryMB   float64
	SampleCount   int
	CreatedAt     time.Time
}

// ReconciliationHistory 对账历史记录
type ReconciliationHistory struct {
	ID               int64
	Symbol           string
	ReconcileTime    time.Time
	LocalPosition    float64
	ExchangePosition float64
	PositionDiff     float64
	ActiveBuyOrders  int
	ActiveSellOrders int
	PendingSellQty   float64
	TotalBuyQty      float64
	TotalSellQty     float64
	EstimatedProfit  float64
	ActualProfit     float64 // 实际盈利（从 trades 表统计）
	CreatedAt        time.Time
}

// PnLSummary 盈亏汇总（按币种对）
type PnLSummary struct {
	Symbol        string
	TotalPnL      float64
	TotalTrades   int
	TotalVolume   float64
	WinRate       float64
	WinningTrades int
	LosingTrades  int
}

// PnLBySymbol 按币种对的盈亏数据
type PnLBySymbol struct {
	Symbol      string
	TotalPnL    float64
	TotalTrades int
	TotalVolume float64
	WinRate     float64
}

// RiskCheckRecord 风控检查记录（单条）
type RiskCheckRecord struct {
	CheckTime      time.Time
	Symbol         string
	IsHealthy      bool
	PriceDeviation float64
	VolumeRatio    float64
	Reason         string
}

// RiskCheckSymbol 风控检查中的币种状态
type RiskCheckSymbol struct {
	Symbol         string
	IsHealthy      bool
	PriceDeviation float64
	VolumeRatio    float64
	Reason         string
}

// RiskCheckHistory 风控检查历史（按时间分组）
type RiskCheckHistory struct {
	CheckTime    time.Time
	Symbols      []*RiskCheckSymbol
	HealthyCount int
	TotalCount   int
}

// FundingRate 资金费率模型
type FundingRate struct {
	ID        int64
	Symbol    string
	Exchange  string
	Rate      float64
	Timestamp time.Time
	CreatedAt time.Time
}

// AIPromptTemplate AI提示词模板模型
type AIPromptTemplate struct {
	ID           int64
	Module       string // 模块名: market_analysis, parameter_optimization, risk_analysis, sentiment_analysis
	Template     string // 提示词模板（支持占位符）
	SystemPrompt string // 系统提示词（可选）
	UpdatedAt    time.Time
}

// BasisData 价差数据
type BasisData struct {
	Symbol       string    `json:"symbol"`        // 交易对
	Exchange     string    `json:"exchange"`      // 交易所
	SpotPrice    float64   `json:"spot_price"`    // 现货价格
	FuturesPrice float64   `json:"futures_price"` // 合约价格
	Basis        float64   `json:"basis"`         // 价差（合约-现货）
	BasisPercent float64   `json:"basis_percent"` // 价差百分比
	FundingRate  float64   `json:"funding_rate"`  // 资金费率
	Timestamp    time.Time `json:"timestamp"`     // 时间戳
}

// BasisStats 价差统计数据
type BasisStats struct {
	Symbol     string  `json:"symbol"`
	Exchange   string  `json:"exchange"`
	AvgBasis   float64 `json:"avg_basis"`   // 平均价差
	MaxBasis   float64 `json:"max_basis"`   // 最大价差
	MinBasis   float64 `json:"min_basis"`   // 最小价差
	StdDev     float64 `json:"std_dev"`     // 标准差
	DataPoints int     `json:"data_points"` // 数据点数量
	Hours      int     `json:"hours"`       // 统计时间范围（小时）
}
