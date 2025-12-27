package ai

import "time"

// AIServiceType AI服务类型
type AIServiceType string

const (
	AIServiceGemini AIServiceType = "gemini"
	AIServiceOpenAI AIServiceType = "openai"
)

// DecisionMode 决策模式
type DecisionMode string

const (
	DecisionModeAdvisor DecisionMode = "advisor" // 建议模式
	DecisionModeExecutor DecisionMode = "executor" // 执行模式
	DecisionModeHybrid  DecisionMode = "hybrid"   // 混合模式
)

// MarketAnalysisRequest 市场分析请求
type MarketAnalysisRequest struct {
	Symbol        string
	CurrentPrice  float64
	Klines        []KlineData
	Positions     []PositionData
	Volume        float64
	PriceHistory  []float64
	Timestamp     time.Time
}

// KlineData K线数据
type KlineData struct {
	OpenTime  time.Time
	CloseTime time.Time
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
}

// PositionData 持仓数据
type PositionData struct {
	Symbol     string
	Size       float64
	EntryPrice float64
	PnL        float64
}

// MarketAnalysisResponse 市场分析响应
type MarketAnalysisResponse struct {
	Trend        string  // up, down, side
	Confidence   float64 // 0-1
	PricePrediction *PricePrediction
	Signal       string  // buy, sell, hold
	Reasoning    string
}

// PricePrediction 价格预测
type PricePrediction struct {
	ShortTerm float64 // 短期预测价格
	LongTerm  float64 // 长期预测价格
	Timeframe string  // 预测时间范围
}

// ParameterOptimizationRequest 参数优化请求
type ParameterOptimizationRequest struct {
	Symbol           string
	CurrentParams   TradingParams
	HistoricalTrades []TradeData
	Performance     PerformanceData
	MarketCondition string
}

// TradingParams 交易参数
type TradingParams struct {
	PriceInterval float64
	BuyWindowSize int
	SellWindowSize int
	OrderQuantity float64
}

// TradeData 交易数据
type TradeData struct {
	BuyPrice  float64
	SellPrice float64
	Quantity  float64
	PnL       float64
	Timestamp time.Time
}

// PerformanceData 性能数据
type PerformanceData struct {
	TotalTrades   int
	WinRate      float64
	TotalPnL     float64
	TotalVolume  float64
	MaxDrawdown  float64
	SharpeRatio  float64
}

// ParameterOptimizationResponse 参数优化响应
type ParameterOptimizationResponse struct {
	RecommendedParams TradingParams
	ExpectedImprovement float64 // 预期改进百分比
	Confidence        float64
	Reasoning         string
}

// RiskAnalysisRequest 风险分析请求
type RiskAnalysisRequest struct {
	Symbol           string
	CurrentPrice     float64
	Positions         []PositionData
	OpenOrders       int
	AccountBalance   float64
	UsedMargin       float64
	MarketVolatility float64
	Klines           []KlineData
}

// RiskAnalysisResponse 风险分析响应
type RiskAnalysisResponse struct {
	RiskScore      float64 // 0-1，越高越危险
	RiskLevel      string  // low, medium, high, critical
	Warnings       []string
	Recommendations []string
	Reasoning      string
}

// SentimentAnalysisRequest 情绪分析请求
type SentimentAnalysisRequest struct {
	Symbol      string
	NewsItems   []NewsItem
	FearGreedIndex *FearGreedIndex
	RedditPosts []RedditPost
	Timestamp   time.Time
}

// NewsItem 新闻项
type NewsItem struct {
	Title       string
	Description string
	URL         string
	PublishedAt time.Time
	Source      string
}

// FearGreedIndex 恐慌贪婪指数
type FearGreedIndex struct {
	Value      int       // 0-100
	Classification string // Extreme Fear, Fear, Neutral, Greed, Extreme Greed
	Timestamp  time.Time
}

// RedditPost Reddit帖子
type RedditPost struct {
	Title       string
	Content     string
	URL         string
	Subreddit   string
	Score       int
	UpvoteRatio float64
	CreatedAt   time.Time
	Author      string
}

// SentimentAnalysisResponse 情绪分析响应
type SentimentAnalysisResponse struct {
	SentimentScore float64 // -1到1，-1极度悲观，1极度乐观
	Trend          string   // bullish, bearish, neutral
	KeyFactors     []string
	NewsSummary    string
	Reasoning      string
}

// TradingDecision 交易决策
type TradingDecision struct {
	Action      string  // buy, sell, hold, adjust
	Confidence  float64 // 0-1
	Reasoning   string
	Parameters  *TradingParams // 如果需要调整参数
	RiskLevel   string
	RequiresConfirmation bool
}

// PolymarketMarket 预测市场
type PolymarketMarket struct {
	ID          string    // 市场ID
	Question    string    // 问题描述
	Description string    // 详细描述
	EndDate     time.Time // 结束时间
	Outcomes    []string  // 可能的结果
	Volume      float64   // 交易量
	Liquidity   float64   // 流动性
}

// PolymarketMarketData 市场数据
type PolymarketMarketData struct {
	MarketID      string
	YesPrice      float64 // YES 价格（0-1，表示概率）
	NoPrice       float64 // NO 价格（0-1）
	Volume24h     float64 // 24小时交易量
	BestBid       float64 // 最佳买价
	BestAsk       float64 // 最佳卖价
	LastPrice     float64 // 最新成交价
	Timestamp     time.Time
}

// PolymarketSignal 预测市场信号
type PolymarketSignal struct {
	MarketID      string
	Question      string
	Probability   float64 // 预测概率（0-1）
	Signal        string  // buy, sell, hold
	Strength      float64 // 信号强度（0-1）
	Confidence    float64 // 置信度（0-1）
	Reasoning     string  // 推理过程
	Relevance     string  // 与加密货币的相关性（high, medium, low）
}

// PolymarketSignalAnalysisRequest 预测市场信号分析请求
type PolymarketSignalAnalysisRequest struct {
	Symbol      string
	Markets     []*PolymarketMarket
	MarketData  []*PolymarketMarketData
	Timestamp   time.Time
}

// PolymarketSignalAnalysisResponse 预测市场信号分析响应
type PolymarketSignalAnalysisResponse struct {
	Signals     []*PolymarketSignal
	AggregateSignal string  // buy, sell, hold
	AggregateStrength float64 // 0-1
	AggregateConfidence float64 // 0-1
	Reasoning   string
	Timestamp   time.Time
}

// AIError AI错误
type AIError struct {
	Code    string
	Message string
	Retryable bool
}

func (e *AIError) Error() string {
	return e.Message
}

