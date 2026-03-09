package storage

import "time"

// Order 订單模型
type Order struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          string
	Exchange      string  // 交易所名稱（binance, gate 等）
	Type          string  // 订單類型（LIMIT, MARKET 等）
	Price         float64
	Quantity      float64
	FilledQty     float64  // 已成交數量
	Status        string
	RealizedPnL   *float64 // 交易所計算的已實現盈虧（nil 表示無數據）
	StrategyName  string   // 策略名称（如 "Grid-BTCUSDT-1"）
	StrategyType  string   // 策略類型（如 "grid", "dca", "martingale"）
	OrderSource   string   // 订單來源（"normal"=正常限價委托, "stop_loss"=止損平倉, "liquidation"=強制平倉）
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Position 持倉模型
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

// Trade 交易模型（買賣配對）
type Trade struct {
	BuyOrderID      int64
	SellOrderID     int64
	Exchange        string
	Account         string // 账戶標识（如 API Key 的哈希或前缀）
	Symbol          string
	BuyPrice        float64
	SellPrice       float64
	Quantity        float64
	PnL             float64  // 网格方式盈亏（sell_price - buy_level）* quantity
	ExchangePnL     float64  // 交易所方式盈亏（average cost basis）
	Fee             float64  // 手續費（買+賣合計）
	FeeAsset        string  // 手續費幣種
	BuyPriceDeviation  float64 // 🔥 買入價格偏差（實際買入價 - 委託買入價，USDT）
	SellPriceDeviation float64 // 🔥 賣出價格偏差（實際賣出價 - 委託賣出價，USDT）
	CreatedAt       time.Time
}

// Statistics 统计模型
type Statistics struct {
	Date              time.Time
	TotalTrades       int
	TotalVolume       float64
	TotalPnL          float64 // 淨利潤（毛利 - 手續費）
	GrossPnL          float64 // 毛利（價差盈虧，未扣手續費）
	TotalFee          float64 // 手續費合計
	WinRate           float64
	TotalBuyDeviation float64 // 🔥 買入價格偏差總和（USDT）
	TotalSellDeviation float64 // 🔥 賣出價格偏差總和（USDT）
	CreatedAt         time.Time
}

// TodayStatistics 當日統計模型（用於 Bot 概覽頁）
type TodayStatistics struct {
	TotalTrades      int     // 當日成交筆數
	GridPnL          float64 // 當日網格盈虧（trades 表）
	ExchangePnL      float64 // 當日交易所盈虧（orders 表 realized_pnl）
}

// DailyStatisticsWithTradeCount 每日统计（包含盈利/亏损交易數）
type DailyStatisticsWithTradeCount struct {
	Date           time.Time
	TotalTrades    int
	TotalVolume    float64
	TotalPnL       float64 // 當日淨利潤（毛利 - 手續費）
	GrossPnL       float64 // 當日毛利
	TotalFee       float64 // 當日手續費
	WinRate        float64
	WinningTrades  int
	LosingTrades   int
	VolumeProfit   float64 // 盈利交易量（pnl>0 的交易 quantity 之和）
	VolumeStopLoss float64 // 止損交易量（pnl<=0 的交易 quantity 之和）
	OpenPrice      float64 // 當日开盘價
	ClosePrice     float64 // 當日收盘價
	PriceChange    float64 // 價格變化（收盘價-开盘價）
	PriceChangePct float64 // 價格變化百分比
}

// HourlyEquityRecord 小時級權益記錄（用於計算日內最大回撤）
type HourlyEquityRecord struct {
	ID                 int64
	Exchange           string
	Symbol             string
	Account            string
	Timestamp          time.Time
	Equity             float64 // 權益 = 持倉價值 + 未實現盈虧
	UnrealizedPnL      float64
	TotalPositionValue float64
	CreatedAt          time.Time
}

// DailySnapshot 每日收盤快照（未實現盈虧、日內最大回撤）
type DailySnapshot struct {
	ID                     int64
	Exchange               string
	Symbol                 string
	Account                string
	Date                   time.Time
	UnrealizedPnL          float64 // 收盤時的未實現盈虧
	TotalPositionValue     float64
	IntradayMaxDrawdown    float64 // 日內最大回撤金額
	IntradayMaxDrawdownPct float64 // 日內最大回撤百分比
	IntradayPeakEquity     float64
	ClosingPrice           float64
	SnapshotTime           time.Time
	CreatedAt              time.Time
}

// SystemMetrics 系统監控细粒度數據模型
type SystemMetrics struct {
	ID            int64
	Timestamp     time.Time
	CPUPercent    float64
	MemoryMB      float64
	MemoryPercent float64
	ProcessID     int
	CreatedAt     time.Time
}

// DailySystemMetrics 系统監控每日彙總數據模型
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

// ReconciliationHistory 對账历史記錄
type ReconciliationHistory struct {
	ID               int64
	Exchange         string
	Symbol           string
	Account          string // 账戶標识
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
	ActualProfit     float64 // 實際盈利（從 trades 表统计）
	CreatedAt        time.Time
}

// PnLSummary 盈亏彙總（按币种對）
type PnLSummary struct {
	Symbol           string
	TotalPnL         float64  // 网格方式盈亏
	ExchangePnL      float64  // 交易所方式盈亏
	TotalTrades      int
	TotalVolume      float64
	WinRate          float64  // 网格方式胜率
	ExchangeWinRate  float64  // 交易所方式胜率
	WinningTrades    int
	LosingTrades     int
}

// PnLBySymbol 按币种對的盈亏數據
type PnLBySymbol struct {
	Exchange          string
	Symbol            string
	TotalPnL          float64  // 网格方式盈亏
	ExchangePnL       float64  // 交易所方式盈亏
	TotalTrades       int
	TotalVolume       float64
	WinRate           float64  // 网格方式胜率
	ExchangeWinRate   float64  // 交易所方式胜率
}

// RiskCheckRecord 风控检查記錄（單条）
type RiskCheckRecord struct {
	CheckTime      time.Time
	Symbol         string
	IsHealthy      bool
	PriceDeviation float64
	VolumeRatio    float64
	Reason         string
}

// RiskCheckSymbol 风控检查中的币种状態
type RiskCheckSymbol struct {
	Symbol         string
	IsHealthy      bool
	PriceDeviation float64
	VolumeRatio    float64
	Reason         string
}

// RiskCheckHistory 风控检查历史（按時间分组）
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

// FundingPayment 资金費用記錄（實際支付/收取的資金費）
type FundingPayment struct {
	ID            int64
	Exchange      string
	Symbol        string
	Account       string
	IncomeType    string  // FUNDING_FEE 等
	Income        float64 // 正=收入，負=支出
	Asset         string
	Info          string
	TransactionID int64
	TradeTime     time.Time
	CreatedAt     time.Time
}

// AIPromptTemplate AI提示词模板模型
type AIPromptTemplate struct {
	ID           int64
	Module       string // 模塊名: market_analysis, parameter_optimization, risk_analysis, sentiment_analysis
	Template     string // 提示词模板（支援占位符）
	SystemPrompt string // 系统提示词（可選）
	UpdatedAt    time.Time
}

// BasisData 價差數據
type BasisData struct {
	Symbol       string    `json:"symbol"`        // 交易對
	Exchange     string    `json:"exchange"`      // 交易所
	SpotPrice    float64   `json:"spot_price"`    // 現貨價格
	FuturesPrice float64   `json:"futures_price"` // 合約價格
	Basis        float64   `json:"basis"`         // 價差（合約-現貨）
	BasisPercent float64   `json:"basis_percent"` // 價差百分比
	FundingRate  float64   `json:"funding_rate"`  // 资金费率
	Timestamp    time.Time `json:"timestamp"`     // 時间戳
}

// BasisStats 價差统计數據
type BasisStats struct {
	Symbol     string  `json:"symbol"`
	Exchange   string  `json:"exchange"`
	AvgBasis   float64 `json:"avg_basis"`   // 平均價差
	MaxBasis   float64 `json:"max_basis"`   // 最大價差
	MinBasis   float64 `json:"min_basis"`   // 最小價差
	StdDev     float64 `json:"std_dev"`     // 標准差
	DataPoints int     `json:"data_points"` // 數據点數量
	Hours      int     `json:"hours"`       // 统计時间範圍（小時）
}

// ProfitWithdrawRule 自动提取规则（盈利管理）
type ProfitWithdrawRule struct {
	ID                string
	AccountID         string
	ExchangeID        string
	StrategyID        string
	Enabled           bool
	TriggerAmount     float64
	WithdrawRatio     float64
	Frequency         string
	Destination       string
	WalletAddress     string
	MinWithdrawAmount float64
	MaxWithdrawAmount *float64
	LastTriggeredAt   *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// PriceHistory 價格历史記錄（用於預测驗证）
type PriceHistory struct {
	ID         int64
	AssetType  string
	Symbol     string
	Price      float64
	Source     string // exchange, gemini, external
	RecordedAt time.Time
	CreatedAt  time.Time
}

// PredictionVerification 預测驗证記錄（方向是否正确）
type PredictionVerification struct {
	ID                   int64
	AnalysisID           int64
	AssetType            string
	Symbol               string
	PredictionTime       time.Time
	Timeframe            string
	PredictedDirection   string // up, down, stable
	PredictedChangePct   float64
	PredictedProbability float64
	ActualPriceAtPred    float64
	ActualPriceAtVerify  float64
	ActualDirection      string
	ActualChangePct      float64
	IsCorrect            bool
	VerifiedAt           time.Time
	Status               string // pending, verified, expired
}

// NewsAnalysisHistory 新聞分析历史記錄
type NewsAnalysisHistory struct {
	ID                int64
	AnalysisTime      time.Time
	Symbol            string
	CurrentPrice      float64
	Assessment        string // JSON 序列化的 NewsRiskAssessment
	RecentNewsSummary string // 最近2小時新闻摘要
	GeminiPrompt      string // 发送给 Gemini 的提示词（可選，調試用）
	GeminiResponse    string // Gemini 原始响应（可選，調試用）
	CreatedAt         time.Time
}

// InspectionReport 智子巡檢報告歷史記錄
type InspectionReport struct {
	ID            int64
	ReportType    string // scheduled, urgent
	Title         string
	Body          string
	SnapshotJSON  string // JSON 序列化的快照（可選）
	AnalysisJSON  string // JSON 序列化的 AI 分析（可選）
	EventType     string
	EventDataJSON string // JSON（可選）
	GeneratedAt   time.Time
	CreatedAt     time.Time
}

// ProfitWithdrawRecord 盈利提取記錄
type ProfitWithdrawRecord struct {
	ID           string
	RuleID       string
	AccountID    string
	ExchangeID   string
	StrategyID   string
	Amount       float64
	Fee          float64
	NetAmount    float64
	Currency     string
	Type         string // auto, manual
	Status       string // pending, processing, completed, failed
	Destination  string // account, wallet
	TransferID   string
	CreatedAt    time.Time
	CompletedAt  *time.Time
	FailedReason string
	Note         string
}

// BotState Bot 啟停狀態（數據庫存儲，優先級高於配置文件）
type BotState struct {
	BotID     string    // Bot ID (格式: exchange:symbol:market_type)
	Enabled   bool      // 是否啟用
	UpdatedAt time.Time // 最後更新時間
	UpdatedBy string    // 更新來源: web_ui, api, system
	Reason    string    // 停用原因（可選）
}
