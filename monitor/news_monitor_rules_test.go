package monitor

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"quantmesh/config"
)

func TestNewsMonitorRiskScoringAndClassification(t *testing.T) {
	nm := NewNewsMonitor(&config.Config{}, nil)
	now := time.Now()

	tests := []struct {
		name         string
		item         NewsItem
		wantCategory string
		wantMinScore float64
	}{
		{
			name: "geopolitical reuters source gets weighted",
			item: NewsItem{
				Title:       "伊朗 以色列 冲突 升级",
				Content:     "爆炸 与 制裁 引发市场恐慌",
				Source:      "Reuters",
				PublishedAt: now,
			},
			wantCategory: "地緣政治",
			wantMinScore: 70,
		},
		{
			name: "regulatory news",
			item: NewsItem{
				Title:       "SEC 监管 法案",
				Content:     "交易所 面临 禁令",
				PublishedAt: now,
			},
			wantCategory: "監管政策",
			wantMinScore: 40,
		},
		{
			name: "market abnormal news",
			item: NewsItem{
				Title:       "BTC 暴跌 崩盘",
				Content:     "恐慌 抛售 与 强制平仓",
				PublishedAt: now,
			},
			wantCategory: "市场异常",
			wantMinScore: 60,
		},
		{
			name: "old neutral news decays to low score",
			item: NewsItem{
				Title:       "weekly market recap",
				Content:     "normal trading session",
				Source:      "blog",
				PublishedAt: now.Add(-10 * 24 * time.Hour),
			},
			wantCategory: "其他",
			wantMinScore: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nm.categorizeNews(tt.item); got != tt.wantCategory {
				t.Fatalf("categorizeNews() = %q, want %q", got, tt.wantCategory)
			}
			score := nm.calculateRiskScore(tt.item)
			if score < tt.wantMinScore {
				t.Fatalf("calculateRiskScore() = %.2f, want >= %.2f", score, tt.wantMinScore)
			}
			if score < 0 || score > 100 {
				t.Fatalf("calculateRiskScore() = %.2f, want clamped into [0,100]", score)
			}
		})
	}
}

func TestNewsMonitorImpactProbabilityAndRecommendation(t *testing.T) {
	nm := NewNewsMonitor(&config.Config{}, nil)

	impactCases := []struct {
		score float64
		want  string
	}{
		{80, "critical"},
		{60, "high"},
		{40, "medium"},
		{39.9, "low"},
	}
	for _, tc := range impactCases {
		if got := nm.determineImpactLevel(tc.score); got != tc.want {
			t.Fatalf("determineImpactLevel(%.1f) = %q, want %q", tc.score, got, tc.want)
		}
	}

	probCases := []struct {
		score float64
		count int
		want  float64
	}{
		{50, 2, 0.6},
		{90, 20, 1},
		{-10, 1, 0},
	}
	for _, tc := range probCases {
		if got := nm.calculateCrashProbability(tc.score, tc.count); got != tc.want {
			t.Fatalf("calculateCrashProbability(%.1f, %d) = %.2f, want %.2f", tc.score, tc.count, got, tc.want)
		}
	}

	recommendations := []struct {
		score float64
		prob  float64
		want  string
	}{
		{80, 0.1, "stop_trading"},
		{20, 0.7, "stop_trading"},
		{60, 0.1, "reduce_position"},
		{20, 0.5, "reduce_position"},
		{40, 0.1, "caution"},
		{20, 0.3, "caution"},
		{10, 0.1, "normal"},
	}
	for _, tc := range recommendations {
		if got := nm.generateRecommendation(tc.score, tc.prob); got != tc.want {
			t.Fatalf("generateRecommendation(%.1f, %.1f) = %q, want %q", tc.score, tc.prob, got, tc.want)
		}
	}
}

func TestNewsMonitorAssessRiskAndCachedAssessments(t *testing.T) {
	nm := NewNewsMonitor(&config.Config{}, nil)

	empty := nm.assessRisk()
	if empty.OverallRiskScore != 0 || empty.CrashProbability != 0 || empty.Recommendation != "normal" {
		t.Fatalf("empty assessRisk() = %+v, want normal zero-risk assessment", empty)
	}

	now := time.Now()
	nm.newsCache = []NewsItem{
		{Title: "high", RiskScore: 90, Category: "地緣政治", PublishedAt: now.Add(-time.Hour)},
		{Title: "old high ignored", RiskScore: 100, Category: "交易所安全", PublishedAt: now.Add(-48 * time.Hour)},
		{Title: "medium", RiskScore: 45, Category: "市场异常", PublishedAt: now.Add(-2 * time.Hour)},
	}

	assessment := nm.assessRisk()
	if assessment.OverallRiskScore <= 70 {
		t.Fatalf("assessRisk().OverallRiskScore = %.2f, want high risk", assessment.OverallRiskScore)
	}
	if len(assessment.RecentHighRiskNews) != 1 {
		t.Fatalf("RecentHighRiskNews length = %d, want 1", len(assessment.RecentHighRiskNews))
	}
	nm.lastAssessment = assessment
	if !nm.IsHighRisk() {
		t.Fatalf("IsHighRisk() = false, want true after high-risk assessment")
	}

	fallback := &NewsRiskAssessment{OverallRiskScore: 33, Recommendation: "caution"}
	assetSpecific := &NewsRiskAssessment{AssetType: AssetTypeCommodityGold, OverallRiskScore: 88, Recommendation: "stop_trading"}
	nm.lastAssessment = fallback
	nm.lastAssessmentByAsset[AssetTypeCommodityGold] = assetSpecific

	got := nm.GetRiskAssessmentBySymbol("PAXGUSDT")
	if got.OverallRiskScore != 88 || got.AssetType != AssetTypeCommodityGold {
		t.Fatalf("GetRiskAssessmentBySymbol(PAXGUSDT) = %+v, want gold-specific assessment", got)
	}
	got.OverallRiskScore = 1
	if nm.lastAssessmentByAsset[AssetTypeCommodityGold].OverallRiskScore != 88 {
		t.Fatalf("GetRiskAssessmentBySymbol returned internal pointer instead of copy")
	}

	got = nm.GetRiskAssessmentBySymbol("UNKNOWN")
	if got.OverallRiskScore != 33 {
		t.Fatalf("GetRiskAssessmentBySymbol(UNKNOWN) = %+v, want fallback assessment", got)
	}
}

func TestNewsMonitorSourceAndIntervalHelpers(t *testing.T) {
	cfg := &config.Config{}
	cfg.NewsMonitor.Sources = []string{"rss", "newsapi", "unknown"}
	cfg.NewsMonitor.RSSFeeds = []string{"http://127.0.0.1:1/unreachable"}
	cfg.NewsMonitor.CustomRSSFeeds = []string{}
	cfg.NewsMonitor.Keywords = []string{"bitcoin"}
	nm := NewNewsMonitor(cfg, nil)

	if got := containsAny("the sec issued a 禁令", []string{"sec", "missing"}); !got {
		t.Fatalf("containsAny() = false, want true")
	}
	if got := containsAny("calm market", []string{"sec", "禁令"}); got {
		t.Fatalf("containsAny() = true, want false")
	}

	intervalCases := []struct {
		input string
		want  time.Duration
	}{
		{"", 30 * time.Minute},
		{"5m", 5 * time.Minute},
		{"90m", 90 * time.Minute},
	}
	for _, tc := range intervalCases {
		got, err := parseAnalysisInterval(tc.input)
		if err != nil {
			t.Fatalf("parseAnalysisInterval(%q) unexpected error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("parseAnalysisInterval(%q) = %s, want %s", tc.input, got, tc.want)
		}
	}
	for _, input := range []string{"bad", "30s", "72h"} {
		if _, err := parseAnalysisInterval(input); err == nil {
			t.Fatalf("parseAnalysisInterval(%q) expected error", input)
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<rss><channel><item><title>ignored</title></item></channel></rss>`))
	}))
	defer server.Close()
	items, err := nm.fetchFromRSS(server.URL)
	if err != nil {
		t.Fatalf("fetchFromRSS() unexpected error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("fetchFromRSS() length = %d, want placeholder parser to return 0", len(items))
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer errorServer.Close()
	if _, err := nm.fetchFromRSS(errorServer.URL); err == nil {
		t.Fatalf("fetchFromRSS() expected HTTP error")
	}

	got := nm.fetchNewsFromSources()
	if len(got) != 0 {
		t.Fatalf("fetchNewsFromSources() length = %d, want empty on unavailable/no-key sources", len(got))
	}
}
