package macro

import (
	"strings"

	"quantmesh/config"
)

// EventImpactClassifier 基于关键词将事件分类并映射到加密市场影响
type EventImpactClassifier struct {
	cfg *config.Config
	// category -> keywords (lowercase)
	keywords map[EventCategory][]string
	// category -> crypto impact
	impact map[EventCategory]CryptoImpact
	// category -> risk weight (0-1)
	weights map[EventCategory]float64
}

// NewEventImpactClassifier 创建分类器
func NewEventImpactClassifier(cfg *config.Config) *EventImpactClassifier {
	c := &EventImpactClassifier{
		cfg:      cfg,
		keywords: make(map[EventCategory][]string),
		impact:   make(map[EventCategory]CryptoImpact),
		weights:  make(map[EventCategory]float64),
	}
	c.initDefaults()
	if cfg != nil && cfg.MacroEvent.Enabled {
		c.loadFromConfig()
	}
	return c
}

func (c *EventImpactClassifier) initDefaults() {
	for cat, kw := range defaultCategoryKeywords {
		c.keywords[cat] = kw
	}
	for cat, imp := range defaultCategoryImpact {
		c.impact[cat] = imp
	}
	for cat, w := range defaultCategoryWeights {
		c.weights[cat] = w
	}
}

var defaultCategoryKeywords = map[EventCategory][]string{
	CategoryGeopolitics:   {"war", "conflict", "invasion", "military", "sanctions", "nuclear", "attack", "strike", "troops"},
	CategoryInterestRate:  {"fed", "interest rate", "rate cut", "rate hike", "ecb", "monetary policy", "fomc", "central bank"},
	CategoryCurrency:      {"dollar", "usd", "eur", "cny", "exchange rate", "devaluation", "forex", "currency"},
	CategoryRegulation:    {"crypto ban", "sec", "regulation", "cbdc", "stablecoin", "bitcoin etf", "crypto regulation"},
	CategoryRecession:     {"recession", "gdp", "unemployment", "inflation", "cpi", "economic crisis"},
}

var defaultCategoryImpact = map[EventCategory]CryptoImpact{
	CategoryGeopolitics:   ImpactBearishShortBullishLong,
	CategoryInterestRate:  ImpactRateInverse,
	CategoryCurrency:      ImpactUSDInverse,
	CategoryRegulation:    ImpactDirect,
	CategoryRecession:     ImpactRiskAsset,
}

var defaultCategoryWeights = map[EventCategory]float64{
	CategoryGeopolitics:   0.9,
	CategoryInterestRate:  0.8,
	CategoryCurrency:      0.7,
	CategoryRegulation:    0.85,
	CategoryRecession:     0.6,
}

var categoryLabels = map[EventCategory]string{
	CategoryGeopolitics:   "地缘政治",
	CategoryInterestRate:  "利率决议",
	CategoryCurrency:      "汇率/货币",
	CategoryRegulation:    "监管政策",
	CategoryRecession:    "经济衰退/通胀",
}

func (c *EventImpactClassifier) loadFromConfig() {
	for cat, kw := range c.cfg.MacroEvent.Categories {
		ec := EventCategory(cat)
		if len(kw.Keywords) > 0 {
			c.keywords[ec] = kw.Keywords
		}
		if kw.CryptoImpact != "" {
			c.impact[ec] = CryptoImpact(kw.CryptoImpact)
		}
		if kw.RiskWeight > 0 {
			c.weights[ec] = kw.RiskWeight
		}
	}
}

// Classify 返回 (category, label)，若未匹配则返回 (CategoryUnknown, "")
func (c *EventImpactClassifier) Classify(text string) (EventCategory, string) {
	lower := strings.ToLower(text)
	for cat, kws := range c.keywords {
		if cat == CategoryUnknown {
			continue
		}
		for _, kw := range kws {
			if strings.Contains(lower, strings.ToLower(kw)) {
				return cat, categoryLabels[cat]
			}
		}
	}
	return CategoryUnknown, ""
}

// GetImpact 获取事件类别对加密市场的影响类型
func (c *EventImpactClassifier) GetImpact(cat EventCategory) CryptoImpact {
	if imp, ok := c.impact[cat]; ok {
		return imp
	}
	return ImpactDirect
}

// GetWeight 获取事件类别的风险权重
func (c *EventImpactClassifier) GetWeight(cat EventCategory) float64 {
	if w, ok := c.weights[cat]; ok {
		return w
	}
	return 0.5
}

// Assess 评估单个事件对加密市场的风险分数
func (c *EventImpactClassifier) Assess(e MacroEvent) ImpactAssessment {
	w := c.GetWeight(e.Category)
	imp := c.GetImpact(e.Category)
	riskScore := 0.0
	direction := "neutral"
	reason := ""

	// 概率越高，风险越可能兑现
	probFactor := e.Probability
	if probFactor > 0.5 {
		probFactor = 0.5 + (e.Probability-0.5)*2 // 0.5-1 映射到 0.5-1.5
	}
	riskScore = probFactor * 100 * w

	// 概率变化大时增加风险分数
	if e.ProbabilityDelta > 0.1 {
		riskScore += 15
		reason = "概率显著上升"
	} else if e.ProbabilityDelta < -0.1 {
		riskScore += 5
		reason = "概率显著下降"
	}

	if riskScore > 100 {
		riskScore = 100
	}

	switch imp {
	case ImpactBearishShortBullishLong:
		direction = "bearish_short"
		if reason == "" {
			reason = "地缘风险短期利空"
		}
	case ImpactRateInverse:
		// 加息概率高 -> 利空
		if e.Probability > 0.6 {
			direction = "bearish"
			reason = "加息预期"
		} else if e.Probability < 0.4 {
			direction = "bullish"
			reason = "降息预期"
		}
	case ImpactUSDInverse:
		direction = "usd_inverse"
		if reason == "" {
			reason = "汇率波动影响"
		}
	case ImpactDirect:
		if e.Probability > 0.6 {
			direction = "bearish"
		} else if e.Probability < 0.4 {
			direction = "bullish"
		}
	case ImpactRiskAsset:
		direction = "bearish"
		if reason == "" {
			reason = "衰退预期利空风险资产"
		}
	}

	return ImpactAssessment{
		EventID:          e.ID,
		EventTitle:       e.Title,
		Category:         e.Category,
		Probability:      e.Probability,
		ProbabilityDelta: e.ProbabilityDelta,
		RiskScore:        riskScore,
		ImpactDirection:  direction,
		CryptoImpact:     imp,
		Reason:           reason,
		Weight:           w,
	}
}
