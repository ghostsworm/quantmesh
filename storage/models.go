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
	ID                int64
	Symbol            string
	ReconcileTime     time.Time
	LocalPosition     float64
	ExchangePosition  float64
	PositionDiff      float64
	ActiveBuyOrders   int
	ActiveSellOrders  int
	PendingSellQty    float64
	TotalBuyQty       float64
	TotalSellQty      float64
	EstimatedProfit   float64
	CreatedAt         time.Time
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
	Symbol        string
	TotalPnL      float64
	TotalTrades   int
	TotalVolume   float64
	WinRate       float64
}

