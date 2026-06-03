package macro

import (
	"testing"

	"quantmesh/config"
)

func TestEventImpactClassifierDefaults(t *testing.T) {
	classifier := NewEventImpactClassifier(nil)

	category, label := classifier.Classify("Fed rate hike odds rise before FOMC")
	if category != CategoryInterestRate {
		t.Fatalf("category = %q, want %q", category, CategoryInterestRate)
	}
	if label != "利率决议" {
		t.Fatalf("label = %q, want 利率决议", label)
	}
	if got := classifier.GetImpact(category); got != ImpactRateInverse {
		t.Fatalf("impact = %q, want %q", got, ImpactRateInverse)
	}
	if got := classifier.GetWeight(category); got != 0.8 {
		t.Fatalf("weight = %v, want 0.8", got)
	}

	unknown, unknownLabel := classifier.Classify("local exchange maintenance window")
	if unknown != CategoryUnknown || unknownLabel != "" {
		t.Fatalf("unknown classification = %q/%q", unknown, unknownLabel)
	}
	if got := classifier.GetImpact(CategoryUnknown); got != ImpactDirect {
		t.Fatalf("unknown impact = %q, want direct", got)
	}
	if got := classifier.GetWeight(CategoryUnknown); got != 0.5 {
		t.Fatalf("unknown weight = %v, want 0.5", got)
	}
}

func TestEventImpactClassifierConfigOverrides(t *testing.T) {
	cfg := &config.Config{}
	cfg.MacroEvent.Enabled = true
	cfg.MacroEvent.Categories = map[string]struct {
		Keywords     []string `yaml:"keywords"`
		CryptoImpact string   `yaml:"crypto_impact"`
		RiskWeight   float64  `yaml:"risk_weight"`
	}{
		string(CategoryRegulation): {
			Keywords:     []string{"MiCA", "stablecoin bill"},
			CryptoImpact: string(ImpactRiskAsset),
			RiskWeight:   0.42,
		},
	}

	classifier := NewEventImpactClassifier(cfg)
	category, _ := classifier.Classify("EU MiCA enforcement update")
	if category != CategoryRegulation {
		t.Fatalf("category = %q, want regulation", category)
	}
	if got := classifier.GetImpact(category); got != ImpactRiskAsset {
		t.Fatalf("impact = %q, want risk_asset", got)
	}
	if got := classifier.GetWeight(category); got != 0.42 {
		t.Fatalf("weight = %v, want 0.42", got)
	}
}

func TestAssessMapsProbabilityAndImpactDirection(t *testing.T) {
	classifier := NewEventImpactClassifier(nil)

	assessment := classifier.Assess(MacroEvent{
		ID:               "evt-rate",
		Title:            "Rate decision",
		Category:         CategoryInterestRate,
		Probability:      0.7,
		ProbabilityDelta: 0.2,
	})
	if !floatAlmostEqual(assessment.RiskScore, 87) {
		t.Fatalf("risk score = %v, want 87", assessment.RiskScore)
	}
	if assessment.ImpactDirection != "bearish" || assessment.Reason != "加息预期" {
		t.Fatalf("unexpected assessment direction/reason: %#v", assessment)
	}

	highRisk := classifier.Assess(MacroEvent{
		ID:               "evt-war",
		Title:            "Conflict risk",
		Category:         CategoryGeopolitics,
		Probability:      0.95,
		ProbabilityDelta: 0.5,
	})
	if highRisk.RiskScore != 100 {
		t.Fatalf("capped risk score = %v, want 100", highRisk.RiskScore)
	}
	if highRisk.ImpactDirection != "bearish_short" {
		t.Fatalf("impact direction = %q, want bearish_short", highRisk.ImpactDirection)
	}

	bullish := classifier.Assess(MacroEvent{
		ID:          "evt-cut",
		Title:       "Rate cut",
		Category:    CategoryInterestRate,
		Probability: 0.3,
	})
	if bullish.ImpactDirection != "bullish" || bullish.Reason != "降息预期" {
		t.Fatalf("unexpected bullish assessment: %#v", bullish)
	}
}

func floatAlmostEqual(a, b float64) bool {
	const epsilon = 1e-9
	if a > b {
		return a-b < epsilon
	}
	return b-a < epsilon
}
