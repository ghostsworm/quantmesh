package ai

import (
	"strings"
	"testing"
)

func TestRiskAssessorQuickAssessLowRisk(t *testing.T) {
	assessor := &RiskAssessor{}
	req := &RiskAssessmentRequest{
		StrategyType: "dca",
		StrategyName: "稳健 DCA",
		StrategyParams: map[string]interface{}{
			"base_order_amount":  100.0,
			"max_levels":         3.0,
			"stop_loss":          5.0,
			"take_profit":        3.0,
			"cascade_protection": true,
			"trend_filter":       true,
		},
		Symbol:        "BTCUSDT",
		Exchange:      "binance",
		Timeframe:     "1h",
		TotalCapital:  10000,
		Leverage:      2,
		CurrentPrice:  100000,
		Volatility24h: 2,
		Volume24h:     2_000_000,
		RiskTolerance: "moderate",
	}

	resp := assessor.QuickAssess(req)
	if resp.OverallScore != 100 {
		t.Fatalf("OverallScore = %d, want 100", resp.OverallScore)
	}
	if resp.RiskLevel != "low" || !resp.Recommended {
		t.Fatalf("expected low recommended response, got level=%q recommended=%v", resp.RiskLevel, resp.Recommended)
	}
	if len(resp.RiskFactors) != 0 {
		t.Fatalf("expected no risk factors, got %#v", resp.RiskFactors)
	}
	if !strings.Contains(resp.Summary, "建议可以使用") {
		t.Fatalf("unexpected summary: %s", resp.Summary)
	}
}

func TestRiskAssessorQuickAssessExtremeRisk(t *testing.T) {
	assessor := &RiskAssessor{}
	req := &RiskAssessmentRequest{
		StrategyType: "martingale",
		StrategyName: "激进马丁",
		StrategyParams: map[string]interface{}{
			"base_order_amount": 5000.0,
			"max_levels":        12.0,
			"multiplier":        3.0,
			"trend_filter":      false,
		},
		Symbol:        "BTCUSDT",
		Exchange:      "binance",
		Timeframe:     "1m",
		TotalCapital:  10000,
		Leverage:      25,
		CurrentPrice:  100000,
		Volatility24h: 12,
		Volume24h:     500000,
		RiskTolerance: "conservative",
	}

	resp := assessor.QuickAssess(req)
	if resp.OverallScore != 15 {
		t.Fatalf("OverallScore = %d, want 15", resp.OverallScore)
	}
	if resp.RiskLevel != "extreme" || resp.Recommended {
		t.Fatalf("expected extreme not recommended response, got level=%q recommended=%v", resp.RiskLevel, resp.Recommended)
	}
	if len(resp.RiskFactors) != 4 {
		t.Fatalf("expected 4 risk factors, got %#v", resp.RiskFactors)
	}
	if len(resp.Suggestions) != 3 {
		t.Fatalf("expected 3 suggestions, got %#v", resp.Suggestions)
	}
	if len(resp.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %#v", resp.Warnings)
	}
	if !strings.Contains(resp.Summary, "建议先修改配置") {
		t.Fatalf("unexpected summary: %s", resp.Summary)
	}
}

func TestRiskAssessorPromptAndSchema(t *testing.T) {
	assessor := &RiskAssessor{}
	req := &RiskAssessmentRequest{
		StrategyType:   "grid",
		StrategyName:   "网格",
		StrategyParams: map[string]interface{}{"grid_count": 10.0},
		Symbol:         "ETHUSDT",
		Exchange:       "okx",
		Timeframe:      "15m",
		TotalCapital:   5000,
		Leverage:       3,
		CurrentPrice:   4000,
		Volatility24h:  3,
		Volume24h:      3_000_000,
		RiskTolerance:  "aggressive",
	}

	prompt := assessor.buildPrompt(req)
	for _, want := range []string{"网格", "ETHUSDT", "激進型", "grid_count"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}

	schema := assessor.buildSchema()
	if schema["type"] != "object" {
		t.Fatalf("schema type = %#v, want object", schema["type"])
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("schema properties has unexpected type: %#v", schema["properties"])
	}
	if _, ok := props["overall_score"]; !ok {
		t.Fatal("schema should include overall_score")
	}
}

func TestRiskAssessorScoreHelpers(t *testing.T) {
	assessor := &RiskAssessor{}
	req := &RiskAssessmentRequest{
		StrategyType: "grid",
		StrategyParams: map[string]interface{}{
			"stop_loss":    25.0,
			"take_profit":  3.0,
			"trend_filter": true,
		},
		Timeframe:     "4h",
		TotalCapital:  1000,
		Leverage:      8,
		Volatility24h: 0.5,
		Volume24h:     2_000_000,
	}

	if got := assessor.assessCapitalManagement(req); got != 20 {
		t.Fatalf("assessCapitalManagement = %d, want 20", got)
	}
	if got := assessor.assessRiskControl(req); got != 19 {
		t.Fatalf("assessRiskControl = %d, want 19", got)
	}
	if got := assessor.assessStrategyFit(req); got != 25 {
		t.Fatalf("assessStrategyFit = %d, want 25", got)
	}
	if got := assessor.assessMarketCondition(req); got != 22 {
		t.Fatalf("assessMarketCondition = %d, want 22", got)
	}
}

func TestRiskColorAndScoreEmoji(t *testing.T) {
	colorTests := map[string]string{
		"low":     "green",
		"medium":  "yellow",
		"high":    "orange",
		"extreme": "red",
		"unknown": "gray",
	}
	for input, want := range colorTests {
		if got := GetRiskColor(input); got != want {
			t.Fatalf("GetRiskColor(%q) = %q, want %q", input, got, want)
		}
	}

	scoreTests := map[int]string{
		80: "✅",
		60: "⚠️",
		40: "🔶",
		39: "🚨",
	}
	for input, want := range scoreTests {
		if got := GetScoreEmoji(input); got != want {
			t.Fatalf("GetScoreEmoji(%d) = %q, want %q", input, got, want)
		}
	}
}
