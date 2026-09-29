package inspector

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"quantmesh/monitor"
	"quantmesh/storage"
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
	if !strings.Contains(fallback.Summary, "未核实") {
		t.Fatalf("unverified realized PnL must not be presented as a verified amount: %q", fallback.Summary)
	}
	nilSnap, err := analyzer.Analyze(context.Background(), nil)
	if err != nil || nilSnap.RiskLevel != "overall" {
		t.Fatalf("nil snap analysis=%#v err=%v", nilSnap, err)
	}
	if prompt := analyzer.buildPrompt(snap); !strings.Contains(prompt, "BTCUSDT") || !strings.Contains(prompt, "黃金") || !strings.Contains(prompt, "未核實") {
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

type scopedFakeSnapshotSource struct {
	fakeSnapshotSource
	scope    string
	market   string
	pnlAsset string
}

func TestCollectorDoesNotAggregateEquityAcrossAccountsAndLabelsPositionAsset(t *testing.T) {
	first := scopedFakeSnapshotSource{
		fakeSnapshotSource: fakeSnapshotSource{exchange: "binance", symbol: "BTCUSDT", account: "account-a", pnl: 3},
		scope:              "scope-a", market: "futures", pnlAsset: "USDT",
	}
	second := scopedFakeSnapshotSource{
		fakeSnapshotSource: fakeSnapshotSource{exchange: "okx", symbol: "ETHBTC", account: "account-b", pnl: 0.2},
		scope:              "scope-b", market: "spot", pnlAsset: "BTC",
	}
	providerCalls := 0
	collector := &Collector{
		GetSnapshotSources: func() []SnapshotSource { return []SnapshotSource{first, second} },
		GetAccountSummary: func(context.Context, string, string) (AccountSummary, error) {
			providerCalls++
			return AccountSummary{TotalBalance: 100, Currency: "USDT"}, nil
		},
	}
	snapshot := collector.Collect(context.Background())
	if snapshot.AccountSummary.Currency != "" || snapshot.AccountSummary.TotalBalance != 0 {
		t.Fatalf("mixed-account equity should remain unavailable: %+v", snapshot.AccountSummary)
	}
	if providerCalls != 1 {
		t.Fatalf("expected at most one exact-account summary request, got %d", providerCalls)
	}
	if snapshot.Positions[0].PnLAsset != "USDT" || snapshot.Positions[1].PnLAsset != "BTC" {
		t.Fatalf("position PnL assets not preserved: %+v", snapshot.Positions)
	}
	prompt := (&Analyzer{}).buildPrompt(snapshot)
	if strings.Contains(prompt, "未實現盈虧 3.20") || !strings.Contains(prompt, "0.20 BTC") {
		t.Fatalf("prompt has mixed/incorrect PnL denomination: %q", prompt)
	}
}

func TestBalanceAlertRequiresSameAccountAndCurrency(t *testing.T) {
	thresholds := DefaultEventThresholds()
	monitor := NewEventMonitor(thresholds)
	previous := &InspectionSnapshot{Timestamp: time.Now(), AccountSummary: AccountSummary{
		Exchange: "binance", Account: "main", Currency: "BTC", TotalBalance: 1,
	}}
	monitor.Check(previous)
	changedCurrency := &InspectionSnapshot{Timestamp: time.Now(), AccountSummary: AccountSummary{
		Exchange: "binance", Account: "main", Currency: "USDT", TotalBalance: 1000,
	}}
	if events := monitor.Check(changedCurrency); len(events) != 0 {
		t.Fatalf("currency change must not be reported as balance movement: %+v", events)
	}
	changedBalance := &InspectionSnapshot{Timestamp: time.Now(), AccountSummary: AccountSummary{
		Exchange: "binance", Account: "main", Currency: "USDT", TotalBalance: 2000,
	}}
	monitor.Check(changedBalance)
	confirmedChange := &InspectionSnapshot{Timestamp: time.Now(), AccountSummary: AccountSummary{
		Exchange: "binance", Account: "main", Currency: "USDT", TotalBalance: 3000,
	}}
	events := monitor.Check(confirmedChange)
	if len(events) != 1 || !strings.Contains(events[0].Message, "USDT") {
		t.Fatalf("same-account same-currency alert should show its asset: %+v", events)
	}
}

func (s scopedFakeSnapshotSource) AccountScope() string { return s.scope }
func (s scopedFakeSnapshotSource) MarketType() string   { return s.market }
func (s scopedFakeSnapshotSource) PnLAsset() string     { return s.pnlAsset }

type inspectorPnLStorageFixture struct {
	storage.Storage
	calls int
	err   error
}

func (f *inspectorPnLStorageFixture) GetPnLByAccountScopeAndAsset(exchange, accountScope, asset string, start, end time.Time) ([]*storage.PnLBySymbol, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return []*storage.PnLBySymbol{{Exchange: exchange, PnLAsset: asset, TotalPnL: 2, TotalTrades: 1}}, nil
}

func TestInspectorRealizedPnLRequiresUniformAssetAndExactScopes(t *testing.T) {
	first := scopedFakeSnapshotSource{fakeSnapshotSource: fakeSnapshotSource{exchange: "binance", symbol: "BTCUSDT"}, scope: "scope-a", market: "futures", pnlAsset: "USDT"}
	duplicate := scopedFakeSnapshotSource{fakeSnapshotSource: fakeSnapshotSource{exchange: "BINANCE", symbol: "ETHUSDT"}, scope: "scope-a", market: "futures", pnlAsset: "usdt"}
	store := &inspectorPnLStorageFixture{}
	summary := collectScopedPnLSummary(store, []SnapshotSource{first, duplicate}, time.Now())
	if !summary.Verified || summary.PnLAsset != "USDT" || summary.TodayRealized != 2 || summary.TodayTrades != 1 || store.calls != 4 {
		t.Fatalf("same-scope duplicates should be queried once with verified units: summary=%+v calls=%d", summary, store.calls)
	}

	foreignAsset := scopedFakeSnapshotSource{fakeSnapshotSource: fakeSnapshotSource{exchange: "okx", symbol: "BTCUSD"}, scope: "scope-b", market: "futures", pnlAsset: "USD"}
	store.calls = 0
	summary = collectScopedPnLSummary(store, []SnapshotSource{first, foreignAsset}, time.Now())
	if summary.Verified || summary.PnLAsset != "" || summary.TodayRealized != 0 || store.calls != 0 {
		t.Fatalf("mixed denomination must not produce a scalar PnL total: summary=%+v calls=%d", summary, store.calls)
	}

	store.calls = 0
	summary = collectScopedPnLSummary(store, []SnapshotSource{first, fakeSnapshotSource{exchange: "binance"}}, time.Now())
	if summary.Verified || summary.TodayRealized != 0 || store.calls != 0 {
		t.Fatalf("source without exact scope evidence must not contribute PnL: summary=%+v calls=%d", summary, store.calls)
	}
}

func TestInspectorRealizedPnLQueryFailureDoesNotPublishPartialTotals(t *testing.T) {
	source := scopedFakeSnapshotSource{fakeSnapshotSource: fakeSnapshotSource{exchange: "binance"}, scope: "scope-a", market: "futures", pnlAsset: "USDT"}
	store := &inspectorPnLStorageFixture{err: fmt.Errorf("ledger unavailable")}
	summary := collectScopedPnLSummary(store, []SnapshotSource{source}, time.Now())
	if summary.Verified || summary.TodayRealized != 0 || summary.WeekRealized != 0 || summary.TotalRealized != 0 {
		t.Fatalf("failed exact-scope query must not leak partial values: %+v", summary)
	}
}

func (s fakeSnapshotSource) Exchange() string { return s.exchange }
func (s fakeSnapshotSource) Symbol() string   { return s.symbol }
func (s fakeSnapshotSource) Account() string  { return s.account }
func (s fakeSnapshotSource) CurrentSnapshot() (float64, float64, float64) {
	return s.price, s.pnl, s.value
}
