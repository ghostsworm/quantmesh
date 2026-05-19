package ai

import (
	"strings"
	"testing"
	"time"

	"quantmesh/polymarket"
)

func TestParsePolymarketJSON(t *testing.T) {
	raw := `{"signal":"bullish","strength":0.7,"confidence":0.8,"reasoning":"test","signals":[{"question":"q","signal":"neutral","probability":0.5,"strength":0.5,"confidence":0.6,"reasoning":"r","relevance":"macro"}]}`
	a, err := parsePolymarketJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if a.Signal != "bullish" || len(a.Signals) != 1 {
		t.Fatalf("%+v", a)
	}
}

func TestParsePolymarketJSON_MarkdownFence(t *testing.T) {
	raw := "Here is JSON:\n```\n{\"signal\":\"neutral\",\"strength\":0.1,\"confidence\":0.2,\"reasoning\":\"x\"}\n```"
	a, err := parsePolymarketJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if a.Signal != "neutral" {
		t.Fatal(a.Signal)
	}
}

func TestNormalizePolymarketAnalysis(t *testing.T) {
	a := &PolymarketSignalAnalysis{
		Signal:     "risk-on",
		Strength:   1.7,
		Confidence: -0.2,
		Signals: []SignalItem{{
			Signal:      "BULLISH",
			Probability: 1.2,
			Strength:    -1,
			Confidence:  0.8,
		}},
	}

	normalizePolymarketAnalysis(a)

	if a.Signal != "neutral" || a.Strength != 1 || a.Confidence != 0 {
		t.Fatalf("unexpected normalized analysis: %+v", a)
	}
	if a.Signals[0].Signal != "bullish" || a.Signals[0].Probability != 1 || a.Signals[0].Strength != 0 {
		t.Fatalf("unexpected normalized item: %+v", a.Signals[0])
	}
}

func TestBuildPolymarketPromptIncludesOutcomePrice(t *testing.T) {
	prompt := buildPolymarketPrompt([]polymarket.MarketInfo{{
		EventTitle:     "Bitcoin price",
		Question:       "BTC above 100k?",
		EndDate:        time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		Outcomes:       []string{"Yes", "No"},
		OutcomePrices:  []float64{0.73, 0.27},
		YesProbability: 0.73,
		Volume:         1000,
		Liquidity:      500,
	}})

	if !strings.Contains(prompt, "Yes概率:0.7300") || !strings.Contains(prompt, "结果:Yes/No") {
		t.Fatalf("prompt missing market probability: %s", prompt)
	}
}
