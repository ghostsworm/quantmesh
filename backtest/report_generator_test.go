package backtest

import "testing"

func TestFormatGridDirectionLabel(t *testing.T) {
	tests := []struct {
		name     string
		meta     *ReportMeta
		expected string
	}{
		{"nil meta", nil, "開多"},
		{"nil params", &ReportMeta{Params: nil}, "開多"},
		{"empty params", &ReportMeta{Params: map[string]interface{}{}}, "開多"},
		{"LONG", &ReportMeta{Params: map[string]interface{}{"direction": "LONG"}}, "開多"},
		{"long lowercase", &ReportMeta{Params: map[string]interface{}{"direction": "long"}}, "開多"},
		{"SHORT", &ReportMeta{Params: map[string]interface{}{"direction": "SHORT"}}, "開空"},
		{"short lowercase", &ReportMeta{Params: map[string]interface{}{"direction": "short"}}, "開空"},
		{"BOTH", &ReportMeta{Params: map[string]interface{}{"direction": "BOTH"}}, "雙向網格"},
		{"both lowercase", &ReportMeta{Params: map[string]interface{}{"direction": "both"}}, "雙向網格"},
		{"unknown falls through", &ReportMeta{Params: map[string]interface{}{"direction": "CUSTOM"}}, "CUSTOM"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatGridDirectionLabel(tt.meta)
			if got != tt.expected {
				t.Errorf("formatGridDirectionLabel() = %q, want %q", got, tt.expected)
			}
		})
	}
}
