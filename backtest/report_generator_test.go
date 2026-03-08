package backtest

import (
	"math"
	"strings"
	"testing"
	"time"
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

func TestCalculateProfitFactor(t *testing.T) {
	tests := []struct {
		name   string
		trades []Trade
		want   float64
		isInf  bool
	}{
		{
			name:   "no trades at all",
			trades: []Trade{},
			want:   -1,
		},
		{
			name: "only buy trades, no sell",
			trades: []Trade{
				{Type: "buy", PnL: 0},
				{Type: "buy", PnL: 0},
			},
			want: -1,
		},
		{
			name: "all winning sells, no loss",
			trades: []Trade{
				{Type: "sell", PnL: 100},
				{Type: "sell", PnL: 50},
			},
			isInf: true,
		},
		{
			name: "all losing sells, no profit",
			trades: []Trade{
				{Type: "sell", PnL: -100},
				{Type: "sell", PnL: -50},
			},
			want: 0,
		},
		{
			name: "normal profit factor",
			trades: []Trade{
				{Type: "sell", PnL: 200},
				{Type: "sell", PnL: -100},
			},
			want: 2.0,
		},
		{
			name: "profit factor < 1",
			trades: []Trade{
				{Type: "sell", PnL: 50},
				{Type: "sell", PnL: -100},
			},
			want: 0.5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateProfitFactor(tt.trades)
			if tt.isInf {
				if !math.IsInf(got, 1) {
					t.Errorf("calculateProfitFactor() = %v, want +Inf", got)
				}
				return
			}
			if got != tt.want {
				t.Errorf("calculateProfitFactor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatProfitFactor(t *testing.T) {
	tests := []struct {
		name string
		pf   float64
		want string
	}{
		{"not applicable", -1, "N/A（無已平倉交易）"},
		{"all profit", math.Inf(1), "∞（全部盈利）"},
		{"normal", 1.5, "1.5000"},
		{"zero", 0, "0.0000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatProfitFactor(tt.pf)
			if got != tt.want {
				t.Errorf("formatProfitFactor(%v) = %q, want %q", tt.pf, got, tt.want)
			}
		})
	}
}

func TestGenerateConclusionNoCompletedTrades(t *testing.T) {
	now := time.Now()
	result := &BacktestResult{
		Strategy:       "grid",
		Symbol:         "BTCUSDT",
		InitialCapital: 10000,
		FinalCapital:   11700,
		StartTime:      now.Add(-30 * 24 * time.Hour),
		EndTime:        now,
		Metrics: Metrics{
			TotalReturn:  17.0,
			MaxDrawdown:  10.0,
			SharpeRatio:  -1.0,
			WinRate:      0,
			ProfitFactor: -1,
			TotalTrades:  0,
			BuyCount:     16,
			SellCount:    0,
		},
	}
	conclusion := generateConclusion(result)
	if strings.Contains(conclusion, "利润因子 < 1") {
		t.Error("should not report profit factor < 1 when no completed trades")
	}
	if !strings.Contains(conclusion, "利润因子指標不適用") {
		t.Error("should report profit factor N/A for no completed trades")
	}
	if !strings.Contains(conclusion, "胜率指標不適用") {
		t.Error("should report win rate N/A for no completed trades")
	}
}

func TestGenerateConclusionWithCompletedTrades(t *testing.T) {
	now := time.Now()
	result := &BacktestResult{
		Strategy:       "grid",
		Symbol:         "BTCUSDT",
		InitialCapital: 10000,
		FinalCapital:   8000,
		StartTime:      now.Add(-30 * 24 * time.Hour),
		EndTime:        now,
		Metrics: Metrics{
			TotalReturn:  -20.0,
			MaxDrawdown:  25.0,
			SharpeRatio:  -0.5,
			WinRate:      30,
			ProfitFactor: 0.5,
			TotalTrades:  10,
			BuyCount:     10,
			SellCount:    10,
		},
	}
	conclusion := generateConclusion(result)
	if !strings.Contains(conclusion, "利润因子 < 1") {
		t.Error("should report profit factor < 1 when there ARE completed losing trades")
	}
	if strings.Contains(conclusion, "不適用") {
		t.Error("should not report N/A when there are completed trades")
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
