package inspector

import (
	"time"

	"quantmesh/monitor"
)

// InspectionSnapshot 智子巡檢採集的完整快照
type InspectionSnapshot struct {
	Timestamp     time.Time
	AccountSummary AccountSummary
	Positions     []PositionInfo
	PnLSummary   PnLSummary
	RiskStatus   RiskStatus
	NewsRisk     map[string]*monitor.NewsRiskAssessment // by asset type
	MarketData   map[string]MarketInfo
	GoldAnalysis *GoldAnalysis
}

// AccountSummary 賬戶與資金彙總
type AccountSummary struct {
	Exchange         string
	Account          string
	TotalBalance     float64
	AvailableBalance float64
	UsedMargin       float64
	UnrealizedPnL    float64
	Currency         string
}

// PositionInfo 單一交易對持倉概覽
type PositionInfo struct {
	Exchange      string
	Symbol        string
	Size          float64
	EntryPrice    float64
	CurrentPrice  float64
	UnrealizedPnL float64
	PositionValue float64
}

// PnLSummary 盈虧統計（今日/本週/本月）
type PnLSummary struct {
	TodayRealized   float64
	WeekRealized    float64
	MonthRealized   float64
	TotalRealized   float64
	UnrealizedPnL   float64
	TodayTrades     int
	WeekTrades      int
	MonthTrades     int
}

// RiskStatus 風控狀態
type RiskStatus struct {
	Triggered       bool
	Reason          string
	TriggeredAt     time.Time
	MonitorSymbols  []RiskCheckSymbol
	HealthyCount    int
	TotalCount      int
}

// RiskCheckSymbol 風控檢查中的幣種狀態
type RiskCheckSymbol struct {
	Symbol         string
	IsHealthy      bool
	VolumeRatio    float64
	Reason         string
}

// MarketInfo 市場數據（當前價、24h 漲跌、資金費率）
type MarketInfo struct {
	Symbol        string
	Exchange      string
	CurrentPrice  float64
	Change24hPct  float64
	FundingRate   float64
	LastUpdated   time.Time
}

// GoldAnalysis 黃金專項分析
type GoldAnalysis struct {
	CurrentPrice       float64
	Change24h          float64
	Change24hPct       float64
	CorrelationWithBTC float64
	SafeHavenIndex     float64 // 0-100
	GoldMarketNews     []monitor.NewsItem
	TechnicalSignals   []string
	AIInsight          string
	LastUpdated        time.Time
}

// InspectionAnalysis AI 分析結果
type InspectionAnalysis struct {
	Summary         string
	KeyFindings     []Finding
	Recommendations []Recommendation
	RiskLevel       string // overall / elevated / critical
	GoldInsights    *GoldInsights
	AttentionCoins   []string
	GeneratedAt     time.Time
}

// Finding 重要發現
type Finding struct {
	Title       string
	Description string
	Priority    int // 1=highest
	Category    string
}

// Recommendation 操作建議
type Recommendation struct {
	Action      string
	Reason      string
	Priority    int
}

// GoldInsights 黃金專項 AI 洞察
type GoldInsights struct {
	Summary        string
	CorrelationNote string
	SafeHavenNote   string
	ActionHint      string
}

// InspectorReport 巡檢報告（定時或事件）
type InspectorReport struct {
	ID          string
	ReportType  string // "scheduled" | "urgent"
	Title       string
	Body        string
	Snapshot    *InspectionSnapshot
	Analysis    *InspectionAnalysis
	EventType   string // for urgent: e.g. risk_triggered, pnl_alert
	EventData   map[string]interface{}
	GeneratedAt time.Time
}

// InspectorEvent 觸發立即通知的事件類型
const (
	EventPnLAlert           = "pnl_alert"
	EventRiskTriggered      = "risk_triggered"
	EventRiskRecovered      = "risk_recovered"
	EventNewsRiskChange     = "news_risk_change"
	EventFundingRateAlert   = "funding_rate_alert"
	EventBalanceChange      = "balance_change"
	EventGoldCorrelationChange = "gold_correlation_change"
)
