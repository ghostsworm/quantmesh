package ai

import (
	"context"
	"strings"
	"testing"

	"quantmesh/config"
)

func TestConfigServiceApplySymbolsConfigPreservesAndDefaults(t *testing.T) {
	cfg := &config.Config{}
	cfg.App.CurrentExchange = "binance"
	cfg.Trading.Symbols = []config.SymbolConfig{{
		Exchange: "binance", Symbol: "BTCUSDT",
		MinOrderValue: 11, ReconcileInterval: 22, OrderCleanupThreshold: 33,
		CleanupBatchSize: 44, MarginLockDurationSec: 55, PositionSafetyCheck: 66,
		TotalAllocatedCapital: 100,
	}}

	resp := &GenerateConfigResponse{SymbolsConfig: []config.SymbolConfig{
		{Symbol: "BTCUSDT", TotalAllocatedCapital: 150},
		{Exchange: "okx", Symbol: "ETHUSDT", TotalAllocatedCapital: 250},
	}}
	if err := NewConfigService().ApplyAIConfig(resp, cfg); err != nil {
		t.Fatalf("ApplyAIConfig failed: %v", err)
	}
	if len(cfg.Trading.Symbols) != 2 {
		t.Fatalf("symbol count = %d", len(cfg.Trading.Symbols))
	}
	btc := cfg.Trading.Symbols[0]
	if btc.Exchange != "binance" || btc.MinOrderValue != 11 || btc.PositionSafetyCheck != 66 || btc.TotalAllocatedCapital != 150 {
		t.Fatalf("existing symbol was not preserved/updated correctly: %#v", btc)
	}
	eth := cfg.Trading.Symbols[1]
	if eth.MinOrderValue != 20 || eth.ReconcileInterval != 60 || eth.CleanupBatchSize != 10 || eth.PositionSafetyCheck == 0 {
		t.Fatalf("new symbol defaults missing: %#v", eth)
	}
	if !cfg.PositionAllocation.Enabled || len(cfg.PositionAllocation.Allocations) != 2 {
		t.Fatalf("position allocation not synchronized: %#v", cfg.PositionAllocation)
	}
}

func TestConfigServiceApplyGridConfigAndValidate(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbols = []config.SymbolConfig{{Exchange: "binance", Symbol: "BTCUSDT"}}
	risk := &GridRiskControlConfig{
		Enabled: true, MaxGridLayers: 9, MaxOpenOrdersAtCap: 3,
		StopLossRatio: 0.1, TakeProfitTriggerRatio: 0.2, TrailingTakeProfitRatio: 0.05,
		TrendFilterEnabled: true,
	}
	resp := &GenerateConfigResponse{
		GridConfig: []SymbolGridConfig{
			{Exchange: "binance", Symbol: "BTCUSDT", PriceInterval: 100, OrderQuantity: 10, BuyWindowSize: 2, SellWindowSize: 3, GridRiskControl: risk},
			{Exchange: "okx", Symbol: "ETHUSDT", PriceInterval: 50, OrderQuantity: 5, BuyWindowSize: 4, SellWindowSize: 5, GridRiskControl: risk},
		},
		Allocation: []SymbolAllocationConfig{{Exchange: "okx", Symbol: "ETHUSDT", MaxAmountUSDT: 500}},
	}
	service := NewConfigService()
	if err := service.ApplyAIConfig(resp, cfg); err != nil {
		t.Fatalf("ApplyAIConfig failed: %v", err)
	}
	if len(cfg.Trading.Symbols) != 2 {
		t.Fatalf("symbol count = %d", len(cfg.Trading.Symbols))
	}
	if cfg.Trading.Symbols[0].PriceInterval != 100 || !cfg.Trading.Symbols[0].GridRiskControl.Enabled {
		t.Fatalf("existing grid config not applied: %#v", cfg.Trading.Symbols[0])
	}
	if cfg.Trading.Symbols[1].OrderCleanupThreshold != 80 || cfg.Trading.Symbols[1].CleanupBatchSize != 20 {
		t.Fatalf("new grid defaults missing: %#v", cfg.Trading.Symbols[1])
	}
	if err := service.ValidateAIConfig(resp, 1000); err != nil {
		t.Fatalf("ValidateAIConfig valid response failed: %v", err)
	}

	invalids := []*GenerateConfigResponse{
		{Allocation: []SymbolAllocationConfig{{Symbol: "BTCUSDT", MaxAmountUSDT: 2000}}},
		{GridConfig: []SymbolGridConfig{{Symbol: "BTCUSDT", PriceInterval: 0, OrderQuantity: 1, BuyWindowSize: 1, SellWindowSize: 1}}},
		{GridConfig: []SymbolGridConfig{{Symbol: "BTCUSDT", PriceInterval: 1, OrderQuantity: 0, BuyWindowSize: 1, SellWindowSize: 1}}},
		{GridConfig: []SymbolGridConfig{{Symbol: "BTCUSDT", PriceInterval: 1, OrderQuantity: 1, BuyWindowSize: 0, SellWindowSize: 1}}},
		{GridConfig: []SymbolGridConfig{{Symbol: "BTCUSDT", PriceInterval: 1, OrderQuantity: 1, BuyWindowSize: 1, SellWindowSize: 1, GridRiskControl: &GridRiskControlConfig{Enabled: true, StopLossRatio: 2}}}},
		{GridConfig: []SymbolGridConfig{{Symbol: "BTCUSDT", PriceInterval: 1, OrderQuantity: 1, BuyWindowSize: 1, SellWindowSize: 1, GridRiskControl: &GridRiskControlConfig{Enabled: true, MaxGridLayers: -1}}}},
	}
	for _, invalid := range invalids {
		if err := service.ValidateAIConfig(invalid, 1000); err == nil {
			t.Fatalf("expected validation error for %#v", invalid)
		}
	}
}

func TestRiskAssessorQuickAssessPromptSchemaAndHelpers(t *testing.T) {
	assessor := &RiskAssessor{}
	highRiskReq := &RiskAssessmentRequest{
		StrategyType: "martingale", StrategyName: "hot", Symbol: "BTCUSDT", Exchange: "binance",
		Timeframe: "1m", TotalCapital: 1000, Leverage: 25, CurrentPrice: 60000,
		Volatility24h: 12, Volume24h: 500000, RiskTolerance: "conservative",
		StrategyParams: map[string]interface{}{
			"base_order_amount": 300.0,
			"max_levels":        12.0,
			"multiplier":        3.0,
		},
	}
	high := assessor.QuickAssess(highRiskReq)
	if high.RiskLevel != "extreme" || high.Recommended || len(high.RiskFactors) < 3 || len(high.Suggestions) < 3 {
		t.Fatalf("unexpected high risk assessment: %#v", high)
	}
	if !strings.Contains(high.Summary, "martingale") || !strings.Contains(high.Warnings[0], "高杠杆") {
		t.Fatalf("unexpected summary/warnings: %#v", high)
	}

	lowRiskReq := &RiskAssessmentRequest{
		StrategyType: "grid", StrategyName: "steady", Symbol: "ETHUSDT", Exchange: "okx",
		Timeframe: "1h", TotalCapital: 10000, Leverage: 2, CurrentPrice: 3000,
		Volatility24h: 3, Volume24h: 20000000, RiskTolerance: "moderate",
		StrategyParams: map[string]interface{}{
			"base_order_amount":  500.0,
			"max_levels":         4.0,
			"stop_loss":          8.0,
			"take_profit":        3.0,
			"cascade_protection": true,
			"trend_filter":       true,
		},
	}
	low := assessor.QuickAssess(lowRiskReq)
	if low.RiskLevel != "low" || !low.Recommended || low.OverallScore < 80 {
		t.Fatalf("unexpected low risk assessment: %#v", low)
	}

	prompt := assessor.buildPrompt(lowRiskReq)
	if !strings.Contains(prompt, "ETHUSDT") || !strings.Contains(prompt, "稳健型") || !strings.Contains(prompt, "base_order_amount") {
		t.Fatalf("prompt missing expected content: %s", prompt)
	}
	schema := assessor.buildSchema()
	if schema["type"] != "object" || len(schema["required"].([]string)) == 0 {
		t.Fatalf("unexpected schema: %#v", schema)
	}
	if GetRiskColor("low") != "green" || GetRiskColor("unknown") != "gray" {
		t.Fatal("risk color mapping mismatch")
	}
	if GetScoreEmoji(90) == GetScoreEmoji(10) {
		t.Fatal("score emoji should vary by score")
	}
}

func TestRiskAssessorAssessRiskParsesClientResponse(t *testing.T) {
	client := &fakeRiskClient{response: `{
		"overall_score": 75,
		"risk_level": "medium",
		"score_breakdown": {"capital_management":20,"risk_control":18,"strategy_fit":19,"market_condition":18},
		"risk_factors": [{"factor":"leverage","severity":"medium","description":"x","impact":"y"}],
		"suggestions": [{"category":"risk","priority":"medium","title":"lower","description":"desc"}],
		"warnings": ["check"],
		"summary": "ok",
		"recommended": true
	}`}
	assessor := &RiskAssessor{client: client}
	resp, err := assessor.AssessRisk(context.Background(), &RiskAssessmentRequest{StrategyParams: map[string]interface{}{}})
	if err != nil {
		t.Fatalf("AssessRisk failed: %v", err)
	}
	if resp.OverallScore != 75 || !resp.Recommended || client.prompt == "" || client.schema["type"] != "object" {
		t.Fatalf("unexpected parsed response or captured request: %#v prompt=%q", resp, client.prompt)
	}

	client.response = "{"
	if _, err := assessor.AssessRisk(context.Background(), &RiskAssessmentRequest{StrategyParams: map[string]interface{}{}}); err == nil {
		t.Fatal("expected invalid json error")
	}
}

type fakeRiskClient struct {
	response string
	prompt   string
	schema   map[string]interface{}
}

func (f *fakeRiskClient) GenerateConfig(ctx context.Context, req *GenerateConfigRequest) (*GenerateConfigResponse, error) {
	return &GenerateConfigResponse{Explanation: "ok"}, nil
}

func (f *fakeRiskClient) GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}) (string, error) {
	f.prompt = prompt
	f.schema = schema
	return f.response, nil
}

func (f *fakeRiskClient) GenerateContentWithGoogleSearch(ctx context.Context, prompt string, schema map[string]interface{}) (string, error) {
	return f.GenerateContent(ctx, prompt, schema)
}
