package backtest

import (
	"testing"
)

func TestTradeTypeLabel(t *testing.T) {
	tests := []struct {
		tradeType string
		dir       string
		want      string
	}{
		{"buy", "LONG", "開倉"},
		{"sell", "LONG", "平倉"},
		{"buy", "SHORT", "平倉"},
		{"sell", "SHORT", "開倉"},
		{"buy", "BOTH", "buy"},
		{"sell", "BOTH", "sell"},
		{"buy", "", "buy"},
		{"sell", "", "sell"},
	}
	for _, tt := range tests {
		got := tradeTypeLabel(tt.tradeType, tt.dir)
		if got != tt.want {
			t.Errorf("tradeTypeLabel(%q, %q) = %q, want %q", tt.tradeType, tt.dir, got, tt.want)
		}
	}
}

func TestExtractPairedAndUnpairedTradesShort(t *testing.T) {
	// 做空：先賣後買，sell 開倉 buy 平倉
	trades := []Trade{
		{Timestamp: 1000, Type: "sell", Price: 100, Quantity: 1, PnL: 0},
		{Timestamp: 2000, Type: "buy", Price: 95, Quantity: 1, PnL: 5},
	}
	paired, unpaired := extractPairedAndUnpairedTrades(trades, 10, "SHORT")
	if len(paired) != 1 {
		t.Fatalf("len(paired)=%d, want 1", len(paired))
	}
	// SHORT: Open=賣出, Close=買入
	if paired[0].OpenPrice != "100.00" || paired[0].ClosePrice != "95.00" {
		t.Errorf("OpenPrice=%s ClosePrice=%s, want Open=100 Close=95", paired[0].OpenPrice, paired[0].ClosePrice)
	}
	if len(unpaired) != 0 {
		t.Errorf("len(unpaired)=%d, want 0", len(unpaired))
	}
}

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
