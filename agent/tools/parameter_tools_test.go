package tools

import (
	"context"
	"strings"
	"testing"
)

func TestSetParameterToolRejectsInvalidRequiredParams(t *testing.T) {
	tool := NewSetParameterTool(nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"strategy_id": "s1",
		"value":       1.0,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Error == "" || !strings.Contains(result.Error, "parameter") {
		t.Fatalf("expected parameter validation error, got %#v", result)
	}
}

func TestValidateParametersToolRejectsNonObjectParameters(t *testing.T) {
	tool := NewValidateParametersTool(nil)

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"parameters": "not-an-object",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Error == "" || !strings.Contains(result.Error, "parameters") {
		t.Fatalf("expected parameters validation error, got %#v", result)
	}
}

func TestSuggestParametersToolDefaultsOptimizerDependencies(t *testing.T) {
	tool := NewSuggestParametersTool(&ParameterOptimizer{})

	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"strategy_type": "grid",
		"symbol":        "BTCUSDT",
		"capital":       1000.0,
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Error != "" {
		t.Fatalf("unexpected tool error: %s", result.Error)
	}

	payload, ok := result.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %#v", result.Result)
	}
	suggestions, ok := payload["suggestions"].([]ParameterSuggestion)
	if !ok || len(suggestions) == 0 {
		t.Fatalf("expected parameter suggestions, got %#v", payload["suggestions"])
	}
}
