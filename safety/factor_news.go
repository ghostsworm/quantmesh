package safety

import (
	"context"
	"quantmesh/config"
	"quantmesh/monitor"
	"sync"
)

// NewsRiskFactor AI 新闻因子
type NewsRiskFactor struct {
	cfg          *config.Config
	newsMonitor  *monitor.NewsMonitor
	symbol       string
	weight       float64
	mu           sync.RWMutex
}

// NewNewsRiskFactor 创建 AI 新闻因子
func NewNewsRiskFactor(cfg *config.Config, symbol string, newsMonitor *monitor.NewsMonitor) *NewsRiskFactor {
	w := 0.30
	if cfg != nil && cfg.CompositeRisk.Factors.News.Weight > 0 {
		w = cfg.CompositeRisk.Factors.News.Weight
	}
	return &NewsRiskFactor{
		cfg:         cfg,
		newsMonitor: newsMonitor,
		symbol:      symbol,
		weight:      w,
	}
}

// SetNewsMonitor 设置新闻监控器（可由 main 在创建 NewsMonitor 后注入）
func (f *NewsRiskFactor) SetNewsMonitor(nm *monitor.NewsMonitor) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.newsMonitor = nm
}

// Name 实现 RiskFactor
func (f *NewsRiskFactor) Name() string { return "news" }

// Weight 实现 RiskFactor
func (f *NewsRiskFactor) Weight() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.weight
}

// Evaluate 实现 RiskFactor
func (f *NewsRiskFactor) Evaluate(ctx context.Context) FactorResult {
	f.mu.RLock()
	nm := f.newsMonitor
	symbol := f.symbol
	f.mu.RUnlock()

	if nm == nil {
		return FactorResult{
			Score:      0,
			Confidence: 0,
			Reason:     "新闻监控未接入",
		}
	}

	var assessment *monitor.NewsRiskAssessment
	if symbol != "" {
		assessment = nm.GetRiskAssessmentBySymbol(symbol)
	} else {
		assessment = nm.GetRiskAssessment()
	}
	if assessment == nil {
		return FactorResult{
			Score:      0,
			Confidence: 0.3,
			Reason:     "暂无新闻评估",
		}
	}

	score := assessment.OverallRiskScore
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	// CrashProbability > 0.5 时额外加分
	if assessment.CrashProbability > 0.5 {
		extra := (assessment.CrashProbability - 0.5) * 40 // 最多 +20
		if score+extra > 100 {
			score = 100
		} else {
			score += extra
		}
	}
	// Recommendation 级别微调
	switch assessment.Recommendation {
	case "stop_trading":
		if score < 80 {
			score = 80
		}
	case "reduce_position":
		if score < 50 {
			score = 50
		}
	case "caution":
		if score < 35 {
			score = 35
		}
	}

	reason := assessment.Recommendation
	if reason == "" {
		reason = "normal"
	}
	return FactorResult{
		Score:      score,
		Confidence: 0.9,
		Reason:     "新闻评估: " + reason,
		Details: map[string]interface{}{
			"overall_risk_score": assessment.OverallRiskScore,
			"crash_probability":  assessment.CrashProbability,
			"recommendation":     assessment.Recommendation,
		},
	}
}
