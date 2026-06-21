package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestIntelligenceToolsExposeRegisteredToolMetadata(t *testing.T) {
	s := NewServer("3.test", nil)
	RegisterIntelligenceTools(s, Providers{Version: "3.test"})
	s.Register(ToolEntry{
		Tool: Tool{
			Name:        "qm_custom_probe",
			Description: "测试工具",
			InputSchema: schemaObject(map[string]any{
				"symbol": schemaString("交易对"),
			}, "symbol"),
		},
		Handler: func(context.Context, json.RawMessage) (any, error) {
			return "ok", nil
		},
	})

	capabilityEntry, ok := findToolEntryForTest(t, s, "qm_capability_map")
	if !ok {
		t.Fatal("qm_capability_map not registered")
	}
	out, err := capabilityEntry.Handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("qm_capability_map failed: %v", err)
	}
	payload, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type %T", out)
	}
	if payload["tool_count"].(int) != 5 {
		t.Fatalf("unexpected tool_count: %#v", payload["tool_count"])
	}

	helpEntry, ok := findToolEntryForTest(t, s, "qm_tool_help")
	if !ok {
		t.Fatal("qm_tool_help not registered")
	}
	out, err = helpEntry.Handler(context.Background(), json.RawMessage(`{"name":"qm_custom_probe"}`))
	if err != nil {
		t.Fatalf("qm_tool_help failed: %v", err)
	}
	help := out.(map[string]any)
	required := help["required"].([]string)
	if len(required) != 1 || required[0] != "symbol" {
		t.Fatalf("unexpected required fields: %#v", required)
	}
}

func TestFindEntitiesRejectsBlankQuery(t *testing.T) {
	s := NewServer("3.test", nil)
	RegisterIntelligenceTools(s, Providers{Version: "3.test"})
	entry, ok := findToolEntryForTest(t, s, "qm_find_entities")
	if !ok {
		t.Fatal("qm_find_entities not registered")
	}

	_, err := entry.Handler(context.Background(), json.RawMessage(`{"query":"   "}`))
	if err == nil || !strings.Contains(err.Error(), "query 不能为空") {
		t.Fatalf("expected blank query error, got %v", err)
	}
}

func TestRegisterAllToolsIncludesIntelligenceTools(t *testing.T) {
	s := NewServer("3.test", nil)
	RegisterAllTools(s, Providers{Version: "3.test"}, false)

	for _, name := range []string{"qm_capability_map", "qm_tool_help", "qm_health_report", "qm_find_entities"} {
		if _, ok := findToolEntryForTest(t, s, name); !ok {
			t.Fatalf("%s not registered by RegisterAllTools", name)
		}
	}
}

func findToolEntryForTest(t *testing.T, s *Server, name string) (ToolEntry, bool) {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.tools[name]
	return entry, ok
}
