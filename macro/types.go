package macro

import "time"

// EventCategory 宏观事件分类
type EventCategory string

const (
	CategoryGeopolitics  EventCategory = "geopolitics"  // 地缘政治/战争
	CategoryInterestRate EventCategory = "interest_rate" // 利率决议
	CategoryCurrency     EventCategory = "currency"     // 汇率/货币政策
	CategoryRegulation   EventCategory = "regulation"   // 监管政策
	CategoryRecession    EventCategory = "recession"    // 经济衰退/通胀
	CategoryUnknown      EventCategory = "unknown"     // 未分类（过滤掉）
)

// CryptoImpact 对加密市场的影响方向
type CryptoImpact string

const (
	ImpactBearishShortBullishLong CryptoImpact = "bearish_short_bullish_long" // 短期利空、长期利好
	ImpactRateInverse             CryptoImpact = "rate_inverse"               // 与利率反向
	ImpactUSDInverse              CryptoImpact = "usd_inverse"                // 与美元反向
	ImpactDirect                  CryptoImpact = "direct"                     // 直接相关（利好/利空取决于事件）
	ImpactRiskAsset               CryptoImpact = "risk_asset"                 // 风险资产（衰退利空）
)

// MacroMarket 预测市场中的单个市场（Polymarket Gamma API 格式）
type MacroMarket struct {
	ID           string    `json:"id"`
	Question     string    `json:"question"`
	Description  string    `json:"description"`
	Outcomes     string    `json:"outcomes"`      // JSON 数组字符串，如 "[\"Yes\", \"No\"]"
	OutcomePrices string   `json:"outcomePrices"` // JSON 数组字符串，如 "[\"0.65\", \"0.35\"]"
	Volume       string    `json:"volume"`
	Liquidity    string    `json:"liquidity"`
	VolumeNum    float64   `json:"volumeNum"`
	LiquidityNum float64   `json:"liquidityNum"`
	Volume24hr   float64   `json:"volume24hr"`
	Active       bool      `json:"active"`
	Closed       bool      `json:"closed"`
	EndDate      time.Time `json:"endDate"`
	Category     string    `json:"category"`
}

// GammaEvent Polymarket Gamma API 返回的 event 结构
type GammaEvent struct {
	ID          string        `json:"id"`
	Ticker      string        `json:"ticker"`
	Slug        string        `json:"slug"`
	Title      string        `json:"title"`
	Description string       `json:"description"`
	Category   string        `json:"category"`
	Active     bool          `json:"active"`
	Closed     bool          `json:"closed"`
	Liquidity  float64       `json:"liquidity"`
	Volume     float64       `json:"volume"`
	Volume24hr float64       `json:"volume24hr"`
	Markets    []MacroMarket `json:"markets"`
	StartDate  time.Time     `json:"startDate"`
	EndDate    time.Time     `json:"endDate"`
	CreatedAt  time.Time     `json:"createdAt"`
	UpdatedAt  time.Time     `json:"updatedAt"`
}

// MacroEvent 归一化后的宏观事件（供内部使用和 API 返回）
type MacroEvent struct {
	ID             string        `json:"id"`
	Title          string        `json:"title"`
	Description    string        `json:"description"`
	Category       EventCategory `json:"category"`
	CategoryLabel  string        `json:"category_label"`
	Probability    float64       `json:"probability"`     // YES 概率 0-1
	ProbabilityDelta float64    `json:"probability_delta"` // 与上次相比的变化
	Volume         float64      `json:"volume"`
	Volume24hr     float64      `json:"volume_24hr"`
	Liquidity      float64      `json:"liquidity"`
	SourceURL      string       `json:"source_url"`
	EndDate        time.Time    `json:"end_date"`
	LastUpdated    time.Time    `json:"last_updated"`
	MarketCount    int          `json:"market_count"`
}

// ImpactAssessment 事件对加密市场的影响评估
type ImpactAssessment struct {
	EventID       string        `json:"event_id"`
	EventTitle    string        `json:"event_title"`
	Category      EventCategory `json:"category"`
	Probability   float64       `json:"probability"`
	ProbabilityDelta float64    `json:"probability_delta"`
	RiskScore     float64       `json:"risk_score"`     // 0-100
	ImpactDirection string      `json:"impact_direction"` // bearish/bullish/neutral
	CryptoImpact  CryptoImpact `json:"crypto_impact"`
	Reason        string       `json:"reason"`
	Weight        float64      `json:"weight"`
}

// MacroImpactSummary 宏观影响汇总（供风控因子和 API 使用）
type MacroImpactSummary struct {
	CompositeRiskScore float64            `json:"composite_risk_score"` // 0-100
	EventCount         int                `json:"event_count"`
	HighImpactCount    int                `json:"high_impact_count"`
	Assessments        []ImpactAssessment `json:"assessments"`
	LastFetched        time.Time          `json:"last_fetched"`
}
