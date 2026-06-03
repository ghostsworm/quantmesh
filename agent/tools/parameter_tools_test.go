package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"quantmesh/agent/types"
)

func TestSetParameterToolValidationAndRisk(t *testing.T) {
	min := 1.0
	max := 10.0
	tool := NewSetParameterTool(nil)
	tool.validator.AddRule("grid_count", ValidationRule{Type: "range", Min: &min, Max: &max})

	if res, _ := tool.Execute(context.Background(), map[string]interface{}{}); !strings.Contains(res.Error, "strategy_id") {
		t.Fatalf("expected missing strategy_id error, got %q", res.Error)
	}
	if res, _ := tool.Execute(context.Background(), map[string]interface{}{"strategy_id": "s1"}); !strings.Contains(res.Error, "parameter") {
		t.Fatalf("expected missing parameter error, got %q", res.Error)
	}
	if res, _ := tool.Execute(context.Background(), map[string]interface{}{"strategy_id": "s1", "parameter": "grid_count"}); !strings.Contains(res.Error, "value") {
		t.Fatalf("expected missing value error, got %q", res.Error)
	}
	if res, _ := tool.Execute(context.Background(), map[string]interface{}{"strategy_id": "s1", "parameter": "grid_count", "value": 20.0}); res.Error == "" {
		t.Fatal("expected range validation error")
	}

	res, err := tool.Execute(context.Background(), map[string]interface{}{"strategy_id": "s1", "parameter": "grid_count", "value": 5.0})
	if err != nil || res.Error != "" {
		t.Fatalf("Execute failed: %v, %q", err, res.Error)
	}
	payload := res.Result.(map[string]interface{})
	if payload["success"] != true || payload["applied_value"] != 5.0 {
		t.Fatalf("unexpected payload: %#v", payload)
	}

	if risk := tool.AssessRisk(map[string]interface{}{"parameter": "leverage"}); risk != types.SecurityLevelHigh {
		t.Fatalf("high risk parameter = %s", risk.String())
	}
	if risk := tool.AssessRisk(map[string]interface{}{"parameter": "grid_count"}); risk != types.SecurityLevelMedium {
		t.Fatalf("normal parameter = %s", risk.String())
	}
	if risk := tool.AssessRisk(map[string]interface{}{}); risk != types.SecurityLevelMedium {
		t.Fatalf("missing parameter risk = %s", risk.String())
	}
}

func TestValidateParametersToolCoversValidatorRules(t *testing.T) {
	min := 0.0
	max := 1.0
	validator := NewParameterValidator()
	validator.AddRule("ratio", ValidationRule{Type: "range", Min: &min, Max: &max})
	validator.AddRule("mode", ValidationRule{Type: "enum", Enum: []interface{}{"grid", "dca"}})
	validator.AddRule("custom", ValidationRule{Type: "custom", Validator: func(value interface{}) error {
		if value != "ok" {
			return errors.New("custom invalid")
		}
		return nil
	}})
	tool := NewValidateParametersTool(validator)

	if res, _ := tool.Execute(context.Background(), map[string]interface{}{"parameters": "bad"}); !strings.Contains(res.Error, "对象") {
		t.Fatalf("expected object error, got %q", res.Error)
	}

	res, err := tool.Execute(context.Background(), map[string]interface{}{"parameters": map[string]interface{}{
		"ratio":  2.0,
		"mode":   "unknown",
		"custom": "bad",
	}})
	if err != nil || res.Error != "" {
		t.Fatalf("Execute failed: %v, %q", err, res.Error)
	}
	payload := res.Result.(map[string]interface{})
	if payload["valid"] != false {
		t.Fatalf("expected invalid payload: %#v", payload)
	}
	if len(payload["errors"].([]string)) != 3 {
		t.Fatalf("expected three validation errors: %#v", payload)
	}

	res, _ = tool.Execute(context.Background(), map[string]interface{}{"parameters": map[string]interface{}{
		"ratio":  0.5,
		"mode":   "grid",
		"custom": "ok",
	}})
	if res.Result.(map[string]interface{})["valid"] != true {
		t.Fatalf("expected valid payload: %#v", res.Result)
	}
}

func TestSuggestParametersToolAndOptimizer(t *testing.T) {
	tool := NewSuggestParametersTool(nil)

	if res, _ := tool.Execute(context.Background(), map[string]interface{}{}); !strings.Contains(res.Error, "strategy_type") {
		t.Fatalf("expected strategy_type error, got %q", res.Error)
	}
	if res, _ := tool.Execute(context.Background(), map[string]interface{}{"strategy_type": "grid"}); !strings.Contains(res.Error, "symbol") {
		t.Fatalf("expected symbol error, got %q", res.Error)
	}

	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"strategy_type": "grid",
		"symbol":        "BTCUSDT",
		"capital":       10000.0,
		"risk_profile":  "moderate",
	})
	if err != nil || res.Error != "" {
		t.Fatalf("Execute failed: %v, %q", err, res.Error)
	}
	payload := res.Result.(map[string]interface{})
	suggestions := payload["suggestions"].([]ParameterSuggestion)
	if len(suggestions) != 2 {
		t.Fatalf("suggestions = %#v", suggestions)
	}
	if suggestions[0].Parameter != "price_interval" || suggestions[1].Parameter != "grid_count" {
		t.Fatalf("unexpected suggestions: %#v", suggestions)
	}
	if !strings.Contains(payload["reasoning"].(string), "市场历史数据") {
		t.Fatalf("unexpected reasoning: %#v", payload["reasoning"])
	}

	dca := (&ParameterOptimizer{}).Optimize("dca", "BTCUSDT", 1000, "conservative")
	if len(dca) != 0 {
		t.Fatalf("DCA optimizer should currently return empty suggestions: %#v", dca)
	}
}

func TestParameterValidatorDirectHelpers(t *testing.T) {
	validator := NewParameterValidator()
	if err := validator.Validate("unknown", 1); err != nil {
		t.Fatalf("unknown parameter should be accepted: %v", err)
	}

	min := 1.0
	validator.AddRule("size", ValidationRule{Type: "range", Min: &min})
	if err := validator.Validate("size", "bad"); err == nil {
		t.Fatal("expected numeric type error")
	}
	suggestions := validator.Suggest("size", 0)
	if len(suggestions) == 0 || !strings.Contains(suggestions[0], "size") {
		t.Fatalf("unexpected suggestions: %#v", suggestions)
	}

	if _, err := requiredStringParam(map[string]interface{}{"name": "   "}, "name"); err == nil {
		t.Fatal("expected blank string error")
	}
	if got, err := requiredStringParam(map[string]interface{}{"name": "bot"}, "name"); err != nil || got != "bot" {
		t.Fatalf("requiredStringParam() = %q, %v", got, err)
	}
}
