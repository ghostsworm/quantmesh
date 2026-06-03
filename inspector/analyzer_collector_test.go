package inspector

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"quantmesh/monitor"
)

func TestAnalyzerFallbackParseAndCollector(t *testing.T) {
	snap := &InspectionSnapshot{
		Timestamp:      time.Now(),
		AccountSummary: AccountSummary{TotalBalance: 1000, UnrealizedPnL: -20},
		Positions: []PositionInfo{
			{Exchange: "binance", Symbol: "BTCUSDT", CurrentPrice: 60000, UnrealizedPnL: -12, PositionValue: 500},
			{Exchange: "okx", Symbol: "ETHUSDT", CurrentPrice: 3000, UnrealizedPnL: 5, PositionValue: 200},
		},
		PnLSummary: PnLSummary{TodayRealized: 7},
		RiskStatus: RiskStatus{Triggered: true, Reason: "volume spike"},
		NewsRisk: map[string]*monitor.NewsRiskAssessment{
			"crypto_btc": {OverallRiskScore: 80, Recommendation: "reduce_position"},
		},
		GoldAnalysis: &GoldAnalysis{CurrentPrice: 2300, Change24hPct: 1.2, CorrelationWithBTC: -0.3, SafeHavenIndex: 75},
	}
	analyzer := &Analyzer{}
	fallback, err := analyzer.Analyze(context.Background(), snap)
	if err != nil || fallback.RiskLevel != "elevated" || len(fallback.KeyFindings) == 0 {
		t.Fatalf("fallback=%#v err=%v", fallback, err)
	}
	nilSnap, err := analyzer.Analyze(context.Background(), nil)
	if err != nil || nilSnap.RiskLevel != "overall" {
		t.Fatalf("nil snap analysis=%#v err=%v", nilSnap, err)
	}
	if prompt := analyzer.buildPrompt(snap); !strings.Contains(prompt, "BTCUSDT") || !strings.Contains(prompt, "黃金") {
		t.Fatalf("prompt=%q", prompt)
	}
	if formatRiskTriggered(true, "x") != "已觸發 - x" || formatRiskTriggered(false, "") != "正常" {
		t.Fatalf("risk formatting mismatch")
	}
	if schema := analyzer.buildSchema(); schema["type"] != "object" {
		t.Fatalf("schema=%#v", schema)
	}
	parsed, err := analyzer.parseResponse("```json\n" + `{"summary":"ok","risk_level":"critical","key_findings":[{"title":"T","description":"D","priority":1,"category":"risk"}],"recommendations":[{"action":"A","reason":"R","priority":2}],"gold_insights":{"summary":"G","correlation_note":"C","safe_haven_note":"S","action_hint":"H"},"attention_coins":["BTC"]}` + "\n```")
	if err != nil || parsed.RiskLevel != "critical" || len(parsed.KeyFindings) != 1 || parsed.GoldInsights == nil || parsed.AttentionCoins[0] != "BTC" {
		t.Fatalf("parsed=%#v err=%v", parsed, err)
	}
	if _, err := analyzer.parseResponse("not json"); err == nil {
		t.Fatalf("bad json should fail")
	}
	if extractJSON("```{\"summary\":\"x\"}```") != `{"summary":"x"}` {
		t.Fatalf("extract json mismatch")
	}
	if truncateStr("abcdef", 3) != "abc..." || truncateStr("ab", 3) != "ab" {
		t.Fatalf("truncate mismatch")
	}

	ai := &Analyzer{Client: fakeInspectionGenerator{text: `{"summary":"ai","risk_level":"overall"}`}}
	analysis, err := ai.Analyze(context.Background(), snap)
	if err != nil || analysis.Summary != "ai" {
		t.Fatalf("ai analysis=%#v err=%v", analysis, err)
	}
	ai.Client = fakeInspectionGenerator{err: fmt.Errorf("boom")}
	if analysis, err := ai.Analyze(context.Background(), snap); err != nil || analysis.Summary == "ai" {
		t.Fatalf("ai fallback=%#v err=%v", analysis, err)
	}

	source := fakeSnapshotSource{exchange: "binance", symbol: "BTCUSDT", account: "main", price: 61000, pnl: 12, value: 600}
	collector := &Collector{
		GetSnapshotSources: func() []SnapshotSource { return []SnapshotSource{source} },
		GetPrice:           func(symbol string) float64 { return 62000 },
		GetAccountSummary: func(context.Context, string, string) (AccountSummary, error) {
			return AccountSummary{Exchange: "binance", Account: "main", TotalBalance: 1234, Currency: "USDT"}, nil
		},
		IsRiskTriggered: func() (bool, string) { return true, "risk" },
		GetNewsRisk: func(symbol string) *monitor.NewsRiskAssessment {
			if symbol == "BTCUSDT" {
				return &monitor.NewsRiskAssessment{OverallRiskScore: 55}
			}
			return nil
		},
		GetGoldAnalysis: func() *GoldAnalysis { return &GoldAnalysis{CurrentPrice: 2300} },
	}
	collected := collector.Collect(nil)
	if len(collected.Positions) != 1 || collected.MarketData["BTCUSDT"].CurrentPrice != 62000 ||
		!collected.RiskStatus.Triggered || collected.NewsRisk["crypto_btc"] == nil || collected.GoldAnalysis == nil {
		t.Fatalf("collected=%#v", collected)
	}
	empty := (&Collector{GetSnapshotSources: func() []SnapshotSource { return nil }}).Collect(context.Background())
	if len(empty.Positions) != 0 || empty.NewsRisk == nil || empty.MarketData == nil {
		t.Fatalf("empty snapshot=%#v", empty)
	}
}

type fakeInspectionGenerator struct {
	text string
	err  error
}

func (g fakeInspectionGenerator) GenerateContent(context.Context, string, map[string]interface{}) (string, error) {
	return g.text, g.err
}

type fakeSnapshotSource struct {
	exchange string
	symbol   string
	account  string
	price    float64
	pnl      float64
	value    float64
}

func (s fakeSnapshotSource) Exchange() string { return s.exchange }
func (s fakeSnapshotSource) Symbol() string   { return s.symbol }
func (s fakeSnapshotSource) Account() string  { return s.account }
func (s fakeSnapshotSource) CurrentSnapshot() (float64, float64, float64) {
	return s.price, s.pnl, s.value
}
