package backtest

import (
	"testing"
)

func TestGetGridStrategyDefinition_HasProfitSpread(t *testing.T) {
	def := GetGridStrategyDefinition()
	if def.StrategyType != "grid" {
		t.Errorf("expected strategy type 'grid', got '%s'", def.StrategyType)
	}

	found := false
	for _, p := range def.Params {
		if p.Name == "profit_spread" {
			found = true
			if p.Type != "number" {
				t.Errorf("profit_spread type should be 'number', got '%s'", p.Type)
			}
			if p.Required {
				t.Error("profit_spread should not be required")
			}
			break
		}
	}
	if !found {
		t.Error("profit_spread param not found in grid strategy definition")
	}
}

func TestGetGridStrategyDefinition_HasGridSpacingBeforeProfitSpread(t *testing.T) {
	def := GetGridStrategyDefinition()
	gridSpacingIdx := -1
	profitSpreadIdx := -1
	for i, p := range def.Params {
		if p.Name == "grid_spacing" {
			gridSpacingIdx = i
		}
		if p.Name == "profit_spread" {
			profitSpreadIdx = i
		}
	}
	if gridSpacingIdx < 0 {
		t.Fatal("grid_spacing not found")
	}
	if profitSpreadIdx < 0 {
		t.Fatal("profit_spread not found")
	}
	if gridSpacingIdx >= profitSpreadIdx {
		t.Errorf("grid_spacing (idx=%d) should come before profit_spread (idx=%d)", gridSpacingIdx, profitSpreadIdx)
	}
}

func TestGetGridStrategyDefinition_HasAllRequiredParams(t *testing.T) {
	def := GetGridStrategyDefinition()
	requiredParams := []string{"grid_spacing", "profit_spread", "grid_count", "order_quantity", "total_capital"}
	for _, name := range requiredParams {
		found := false
		for _, p := range def.Params {
			if p.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("param '%s' not found in grid strategy definition", name)
		}
	}
}

func TestGetTrendFollowingStrategyDefinition(t *testing.T) {
	def := GetTrendFollowingStrategyDefinition()
	if def.StrategyType != "trend_following" {
		t.Errorf("expected strategy type 'trend_following', got '%s'", def.StrategyType)
	}
	requiredParams := []string{"fast_period", "slow_period", "total_capital"}
	for _, name := range requiredParams {
		found := false
		for _, p := range def.Params {
			if p.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("param '%s' not found in trend_following strategy definition", name)
		}
	}
}

func TestGetAllStrategyDefinitions_ContainsAllTypes(t *testing.T) {
	defs := GetAllStrategyDefinitions()
	expectedTypes := []string{"grid", "momentum", "mean_reversion", "trend_following", "dca", "martingale", "combo"}
	typeMap := make(map[string]bool)
	for _, d := range defs {
		typeMap[d.StrategyType] = true
	}
	for _, expected := range expectedTypes {
		if !typeMap[expected] {
			t.Errorf("strategy type '%s' not found in GetAllStrategyDefinitions", expected)
		}
	}
}
