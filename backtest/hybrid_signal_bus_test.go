package backtest

import "testing"

func TestBacktestSignalBusPublishQueryAndClear(t *testing.T) {
	disabled := NewBacktestSignalBus(nil)
	if disabled.IsEnabled() {
		t.Fatal("bus without rules should be disabled")
	}
	disabled.Publish(&BacktestSignal{Type: "trend", Source: "s1", Value: "up"})
	if disabled.GetSignalCount() != 0 {
		t.Fatal("disabled bus should ignore published signals")
	}

	bus := NewBacktestSignalBus([]TaskCollaborationRule{{ID: "r1", Enabled: true}})
	if !bus.IsEnabled() {
		t.Fatal("bus with rules should be enabled")
	}
	bus.Publish(&BacktestSignal{Type: "trend", Source: "s1", Value: "up"})
	bus.Publish(&BacktestSignal{Type: "trend", Source: "s2", Value: "down", Timestamp: 123})

	if bus.GetSignalCount() != 2 {
		t.Fatalf("signal count = %d", bus.GetSignalCount())
	}
	if latest := bus.GetLatest("trend"); latest == nil || latest.Source != "s2" || latest.Timestamp != 123 {
		t.Fatalf("unexpected latest signal: %#v", latest)
	}
	if latest := bus.GetLatestBySource("trend", "s1"); latest == nil || latest.Value != "up" || latest.Timestamp == 0 {
		t.Fatalf("unexpected source signal: %#v", latest)
	}
	if got := bus.GetLatest("missing"); got != nil {
		t.Fatalf("missing signal = %#v", got)
	}

	bus.Clear()
	if bus.GetSignalCount() != 0 {
		t.Fatal("Clear should remove signals")
	}
}

func TestBacktestSignalBusEvaluateRulesAndComparisons(t *testing.T) {
	rules := []TaskCollaborationRule{
		{
			ID: "rule-gt", Name: "greater", Enabled: true, Priority: 3,
			When: TaskSignalCondition{SourceStrategy: "source-a", SignalType: "score", Operator: "gt", Value: 10.0},
			Then: []TaskAction{{TargetStrategy: "target-a", Operation: "pause", Params: map[string]interface{}{"reason": "hot"}}},
		},
		{
			ID: "rule-in", Name: "in-list", Enabled: true, Priority: 2,
			When: TaskSignalCondition{SourceStrategy: "source-b", SignalType: "state", Operator: "in", Value: []string{"risk", "stop"}},
			Then: []TaskAction{{TargetStrategy: "target-b", Operation: "reduce"}},
		},
		{
			ID: "rule-disabled", Name: "disabled", Enabled: false,
			When: TaskSignalCondition{SourceStrategy: "source-a", SignalType: "score", Operator: "gt", Value: 1.0},
			Then: []TaskAction{{TargetStrategy: "target-disabled", Operation: "noop"}},
		},
	}
	bus := NewBacktestSignalBus(rules)
	bus.Publish(&BacktestSignal{Type: "score", Source: "source-a", Value: 12.5})
	bus.Publish(&BacktestSignal{Type: "state", Source: "source-b", Value: "risk"})

	actions := bus.EvaluateRules()
	if len(actions["target-a"]) != 1 || actions["target-a"][0].RuleID != "rule-gt" || actions["target-a"][0].RulePriority != 3 {
		t.Fatalf("target-a actions = %#v", actions["target-a"])
	}
	if len(actions["target-b"]) != 1 || actions["target-b"][0].Operation != "reduce" {
		t.Fatalf("target-b actions = %#v", actions["target-b"])
	}
	if _, ok := actions["target-disabled"]; ok {
		t.Fatalf("disabled rule should not produce actions: %#v", actions)
	}

	if got := NewBacktestSignalBus(nil).EvaluateRules(); got != nil {
		t.Fatalf("disabled bus EvaluateRules = %#v", got)
	}
}

func TestBacktestSignalBusCompareOperators(t *testing.T) {
	bus := NewBacktestSignalBus([]TaskCollaborationRule{{ID: "r", Enabled: true}})
	cases := []struct {
		actual   interface{}
		operator string
		expected interface{}
		want     bool
	}{
		{"up", "==", "up", true},
		{"up", "ne", "down", true},
		{3.0, "gte", 3.0, true},
		{4.0, ">", 3.0, true},
		{2, "lt", 3, true},
		{2, "<=", 2, true},
		{"risk", "in", []interface{}{"risk", "stop"}, true},
		{"risk", "not_in", []string{"ok", "idle"}, true},
		{"risk", "unknown", "risk", false},
		{"bad", "gt", 3.0, false},
	}

	for _, tc := range cases {
		if got := bus.compareValues(tc.actual, tc.operator, tc.expected); got != tc.want {
			t.Fatalf("compareValues(%v %s %v) = %v, want %v", tc.actual, tc.operator, tc.expected, got, tc.want)
		}
	}
}
