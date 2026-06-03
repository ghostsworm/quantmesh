package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"quantmesh/agent/types"
)

func TestToolRegistryRegistersListsAndExecutesTools(t *testing.T) {
	registry := NewToolRegistry()
	tool := NewSimpleTool(
		"ping",
		"returns pong",
		CategorySystem,
		CreateParameterSchema(map[string]SchemaProperty{
			"message": {
				Type:        "string",
				Description: "message to echo",
				Required:    true,
				Default:     "hello",
				Enum:        []string{"hello", "world"},
			},
		}),
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return map[string]interface{}{"echo": params["message"]}, nil
		},
		types.SecurityLevelLow,
	)

	if err := registry.Register(tool); err != nil {
		t.Fatalf("Register returned error: %v", err)
	}
	if got, ok := registry.Get("ping"); !ok || got.Name() != "ping" {
		t.Fatalf("Get returned %#v ok=%v", got, ok)
	}
	if len(registry.List()) != 1 {
		t.Fatalf("List length = %d, want 1", len(registry.List()))
	}
	if len(registry.ListByCategory(CategorySystem)) != 1 {
		t.Fatalf("ListByCategory length = %d, want 1", len(registry.ListByCategory(CategorySystem)))
	}
	if len(registry.GetToolDefinitions()) != 1 {
		t.Fatalf("GetToolDefinitions length = %d, want 1", len(registry.GetToolDefinitions()))
	}

	result, err := registry.ExecuteTool(context.Background(), types.ToolCall{
		ID:        "call-1",
		Name:      "ping",
		Arguments: map[string]interface{}{"message": "hello"},
	})
	if err != nil {
		t.Fatalf("ExecuteTool returned error: %v", err)
	}
	if result.CallID != "call-1" {
		t.Fatalf("CallID = %q, want call-1", result.CallID)
	}
	payload, ok := result.Result.(map[string]interface{})
	if !ok || payload["echo"] != "hello" {
		t.Fatalf("unexpected result payload: %#v", result.Result)
	}
}

func TestToolRegistryRejectsMissingAndHighRiskTools(t *testing.T) {
	registry := NewToolRegistry()
	if err := registry.Register(NewSimpleTool("", "bad", CategorySystem, nil, nil, types.SecurityLevelLow)); err == nil {
		t.Fatal("empty tool name should be rejected")
	}
	if _, err := registry.ExecuteTool(context.Background(), types.ToolCall{Name: "missing"}); err == nil {
		t.Fatal("missing tool should return error")
	}

	highRisk := NewSimpleTool("danger", "danger", CategorySystem, nil, func(context.Context, map[string]interface{}) (interface{}, error) {
		t.Fatal("high-risk tool should not execute without confirmation")
		return nil, nil
	}, types.SecurityLevelHigh)
	if err := registry.Register(highRisk); err != nil {
		t.Fatalf("Register high risk tool: %v", err)
	}
	result, err := registry.ExecuteTool(context.Background(), types.ToolCall{ID: "call-2", Name: "danger"})
	if err != nil {
		t.Fatalf("ExecuteTool high risk returned error: %v", err)
	}
	payload, ok := result.Result.(map[string]interface{})
	if !ok || payload["requires_confirmation"] != true {
		t.Fatalf("expected confirmation payload, got %#v", result.Result)
	}
}

func TestSimpleToolErrorAndJSONHelpers(t *testing.T) {
	tool := NewSimpleTool("err", "err", CategorySystem, nil, func(context.Context, map[string]interface{}) (interface{}, error) {
		return nil, errors.New("boom")
	}, types.SecurityLevelLow)

	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Error != "boom" {
		t.Fatalf("result error = %q, want boom", result.Error)
	}
	if got := MustJSONMarshal(map[string]string{"a": "b"}); got != `{"a":"b"}` {
		t.Fatalf("MustJSONMarshal = %s", got)
	}
}

func TestParameterValidatorRulesAndTools(t *testing.T) {
	validator := NewParameterValidator()
	min := 1.0
	max := 3.0
	validator.AddRule("size", ValidationRule{Type: "range", Min: &min, Max: &max})
	validator.AddRule("mode", ValidationRule{Type: "enum", Enum: []interface{}{"grid", "dca"}})
	validator.AddRule("custom", ValidationRule{Type: "custom", Validator: func(value interface{}) error {
		if value != "ok" {
			return errors.New("not ok")
		}
		return nil
	}})

	for _, tc := range []struct {
		name  string
		param string
		value interface{}
	}{
		{"not number", "size", "bad"},
		{"below min", "size", 0.5},
		{"above max", "size", 4.0},
		{"bad enum", "mode", "other"},
		{"bad custom", "custom", "bad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validator.Validate(tc.param, tc.value); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if err := validator.Validate("size", 2.0); err != nil {
		t.Fatalf("valid range returned error: %v", err)
	}
	if err := validator.Validate("unknown", 99); err != nil {
		t.Fatalf("unknown param returned error: %v", err)
	}

	all := validator.ValidateAll(map[string]interface{}{"size": 5.0, "mode": "grid"})
	if all.Valid || len(all.Errors) != 1 {
		t.Fatalf("ValidateAll = %#v, want one error", all)
	}

	validateTool := NewValidateParametersTool(validator)
	result, err := validateTool.Execute(context.Background(), map[string]interface{}{
		"parameters": map[string]interface{}{"size": 2.0},
	})
	if err != nil {
		t.Fatalf("ValidateParametersTool returned error: %v", err)
	}
	payload, ok := result.Result.(map[string]interface{})
	if !ok || payload["valid"] != true {
		t.Fatalf("unexpected validation payload: %#v", result.Result)
	}

	setTool := NewSetParameterTool(nil)
	setTool.validator = validator
	invalid, err := setTool.Execute(context.Background(), map[string]interface{}{
		"strategy_id": "s1",
		"parameter":   "size",
		"value":       4.0,
	})
	if err != nil {
		t.Fatalf("SetParameterTool invalid returned error: %v", err)
	}
	if invalid.Error == "" || !strings.Contains(invalid.Error, "不能大于") {
		t.Fatalf("unexpected invalid set result: %#v", invalid)
	}
	valid, err := setTool.Execute(context.Background(), map[string]interface{}{
		"strategy_id": "s1",
		"parameter":   "size",
		"value":       2.0,
	})
	if err != nil {
		t.Fatalf("SetParameterTool valid returned error: %v", err)
	}
	if valid.Error != "" {
		t.Fatalf("unexpected valid set error: %s", valid.Error)
	}
}

func TestVolatilityConfigToolBuildsStatusAndSuggestions(t *testing.T) {
	tool := NewVolatilityConfigTool(nil)
	schema := tool.ParameterSchema()
	if schema["type"] != "object" {
		t.Fatalf("schema type = %#v, want object", schema["type"])
	}
	if tool.AssessRisk(nil) != types.SecurityLevelMedium {
		t.Fatal("volatility config should be medium risk")
	}

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"symbol":             "BTCUSDT",
		"enable_detection":   true,
		"use_preset":         false,
		"pause_on_high":      true,
		"pause_on_extreme":   true,
		"pause_on_downtrend": true,
		"custom_thresholds":  map[string]interface{}{"high": 5.0},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	payload, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map payload, got %#v", result.Result)
	}
	if payload["symbol"] != "BTCUSDT" || payload["custom_thresholds"] == nil {
		t.Fatalf("unexpected volatility payload: %#v", payload)
	}
	if len(payload["suggestions"].([]string)) == 0 {
		t.Fatal("expected configuration suggestions")
	}

	status := tool.GetVolatilityStatus("ETHUSDT")
	if status["symbol"] != "ETHUSDT" || status["thresholds"] == nil {
		t.Fatalf("unexpected volatility status: %#v", status)
	}
	if len(tool.ListAvailablePresets()) == 0 {
		t.Fatal("expected available presets")
	}
}
