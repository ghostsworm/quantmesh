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
