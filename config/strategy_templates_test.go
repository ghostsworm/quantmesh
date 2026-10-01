package config

import (
	"strings"
	"testing"
)

func TestComboGridTrendTemplateDoesNotAdvertiseUnconfiguredProtection(t *testing.T) {
	manager := &StrategyTemplateManager{templates: make(map[string]*StrategyTemplate)}
	manager.initBuiltinTemplates()
	template, ok := manager.GetTemplate("combo_grid_trend_eth")
	if !ok {
		t.Fatal("combo_grid_trend_eth template is missing")
	}
	if strings.Contains(template.Name, "止损") || strings.Contains(template.Name, "止盈") {
		t.Fatalf("template name advertises protection it does not configure: %q", template.Name)
	}
	if !strings.Contains(template.Description, "不启用止盈或止损") {
		t.Fatalf("template does not explicitly state that protection is disabled: %q", template.Description)
	}
	if len(template.Params) != 0 {
		t.Fatalf("template exposes parameters that are not routed to the strategies: %+v", template.Params)
	}
	strategies, ok := template.Config["strategies"].([]map[string]interface{})
	if !ok || len(strategies) != 2 {
		t.Fatalf("template strategies = %#v, want grid + trend_following", template.Config["strategies"])
	}
	if strategies[0]["type"] != "grid" || strategies[0]["weight"] != 0.8 || strategies[1]["type"] != "trend_following" || strategies[1]["weight"] != 0.2 {
		t.Fatalf("template composition changed unexpectedly: %#v", strategies)
	}
}

func TestBuiltinTemplatesUseSupportedRuntimeStrategyTypes(t *testing.T) {
	manager := &StrategyTemplateManager{templates: make(map[string]*StrategyTemplate)}
	manager.initBuiltinTemplates()
	for _, template := range manager.ListTemplates() {
		if template.StrategyType == "combo" || template.StrategyType == "grid+trend" {
			continue
		}
		if key := apiStrategyTypeToRuntimeKey(template.StrategyType); key == "" {
			t.Errorf("template %q selects unsupported strategy type %q", template.ID, template.StrategyType)
		}
	}
}

func TestHighRiskTemplatesMatchImplementedStrategies(t *testing.T) {
	manager := &StrategyTemplateManager{templates: make(map[string]*StrategyTemplate)}
	manager.initBuiltinTemplates()

	martingale, ok := manager.GetTemplate("martingale_grid_btc")
	if !ok || martingale.StrategyType != "martingale" {
		t.Fatalf("martingale template type = %+v, want supported martingale strategy", martingale)
	}
	for _, key := range []string{"initial_amount", "multiplier", "max_levels", "price_step"} {
		if _, ok := martingale.Params[key]; !ok {
			t.Errorf("martingale template missing runtime parameter %q", key)
		}
	}
	if _, exists := martingale.Params["base_amount"]; exists {
		t.Error("martingale template still exposes unconsumed base_amount parameter")
	}

	momentum, ok := manager.GetTemplate("momentum_grid_eth")
	if !ok || momentum.StrategyType != "momentum" {
		t.Fatalf("momentum template type = %+v, want supported momentum strategy", momentum)
	}
	for _, key := range []string{"rsi_period", "overbought", "oversold", "order_amount"} {
		if _, ok := momentum.Params[key]; !ok {
			t.Errorf("momentum template missing runtime parameter %q", key)
		}
	}
}

func TestDCATemplatesMatchEnhancedRuntimeInsteadOfScheduledBuying(t *testing.T) {
	manager := &StrategyTemplateManager{templates: make(map[string]*StrategyTemplate)}
	manager.initBuiltinTemplates()

	for _, id := range []string{"dca_regular_btc", "dca_regular", "combo_grid_dca_btc"} {
		template, ok := manager.GetTemplate(id)
		if !ok {
			t.Fatalf("template %q is missing", id)
		}
		if !strings.Contains(template.Description, "不是") {
			t.Errorf("template %q must clarify it is not scheduled buying: %q", id, template.Description)
		}
		if _, exists := template.Params["dca_interval"]; exists {
			t.Errorf("template %q exposes unsupported dca_interval", id)
		}
	}

	for _, id := range []string{"dca_regular_btc", "dca_regular"} {
		regular, _ := manager.GetTemplate(id)
		if regular.StrategyType != "dca_enhanced" {
			t.Errorf("DCA template %q type = %q, want dca_enhanced", id, regular.StrategyType)
		}
		if regular.RiskLevel != "medium" {
			t.Errorf("DCA template %q risk = %q, want medium", id, regular.RiskLevel)
		}
		for _, key := range []string{"base_order_amount", "safety_order_amount", "max_safety_orders"} {
			if _, ok := regular.Params[key]; !ok {
				t.Errorf("DCA template %q missing runtime parameter %q", id, key)
			}
		}
	}
	combo, _ := manager.GetTemplate("combo_grid_dca_btc")
	strategies, ok := combo.Config["strategies"].([]map[string]interface{})
	if !ok || len(strategies) != 2 || strategies[1]["type"] != "dca_enhanced" {
		t.Errorf("combo DCA strategy = %#v, want dca_enhanced", combo.Config["strategies"])
	}
}

func TestLegacyMomentumAndMartingaleTemplatesExposeRuntimeParameters(t *testing.T) {
	manager := &StrategyTemplateManager{templates: make(map[string]*StrategyTemplate)}
	manager.initBuiltinTemplates()

	momentum, ok := manager.GetTemplate("momentum")
	if !ok || momentum.StrategyType != "momentum" {
		t.Fatalf("momentum template = %+v, want supported momentum strategy", momentum)
	}
	for _, key := range []string{"rsi_period", "overbought", "oversold", "order_amount"} {
		if _, ok := momentum.Params[key]; !ok {
			t.Errorf("momentum template missing runtime parameter %q", key)
		}
	}
	if _, exists := momentum.Params["momentum_period"]; exists {
		t.Error("momentum template still exposes unconsumed momentum_period")
	}

	martingale, ok := manager.GetTemplate("martingale")
	if !ok || martingale.StrategyType != "martingale" {
		t.Fatalf("martingale template = %+v, want supported martingale strategy", martingale)
	}
	for _, key := range []string{"initial_amount", "multiplier", "max_levels", "price_step"} {
		if _, ok := martingale.Params[key]; !ok {
			t.Errorf("martingale template missing runtime parameter %q", key)
		}
	}
	if _, exists := martingale.Params["base_amount"]; exists {
		t.Error("martingale template still exposes unconsumed base_amount")
	}
}

func TestTrendAndMeanReversionTemplatesUseConsumedRuntimeParameters(t *testing.T) {
	manager := &StrategyTemplateManager{templates: make(map[string]*StrategyTemplate)}
	manager.initBuiltinTemplates()

	for _, test := range []struct {
		id    string
		want  []string
		stale []string
	}{
		{id: "trend_following", want: []string{"short_period", "long_period", "order_amount"}, stale: []string{"trend_period", "trend_threshold"}},
		{id: "mean_reversion", want: []string{"period", "std_multiplier", "reversion_threshold", "order_amount"}, stale: []string{"mean_period", "std_dev_threshold"}},
	} {
		template, ok := manager.GetTemplate(test.id)
		if !ok {
			t.Fatalf("template %q is missing", test.id)
		}
		for _, key := range test.want {
			if _, ok := template.Params[key]; !ok {
				t.Errorf("template %q missing runtime parameter %q", test.id, key)
			}
		}
		for _, key := range test.stale {
			if _, exists := template.Params[key]; exists {
				t.Errorf("template %q exposes unconsumed parameter %q", test.id, key)
			}
		}
	}
}
