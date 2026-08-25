package backtest

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
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

func TestComputeEndPosition(t *testing.T) {
	tests := []struct {
		name      string
		trades    []Trade
		endPrice  float64
		direction string
		wantQty   float64
		wantValue float64
	}{
		{"LONG no trades", nil, 70000, "LONG", 0, 0},
		{"LONG buy only", []Trade{{Type: "buy", Quantity: 0.1}}, 70000, "LONG", 0.1, 7000},
		{"LONG buy sell closed", []Trade{{Type: "buy", Quantity: 0.1}, {Type: "sell", Quantity: 0.1}}, 70000, "LONG", 0, 0},
		{"SHORT sell only (欠)", []Trade{{Type: "sell", Quantity: 0.1}}, 70000, "SHORT", -0.1, -7000},
		{"SHORT sell buy closed", []Trade{{Type: "sell", Quantity: 0.1}, {Type: "buy", Quantity: 0.1}}, 70000, "SHORT", 0, 0},
		{"SHORT partial close", []Trade{{Type: "sell", Quantity: 0.2}, {Type: "buy", Quantity: 0.1}}, 70000, "SHORT", -0.1, -7000},
		{"LONG negative qty zeroed", []Trade{{Type: "sell", Quantity: 0.1}}, 70000, "LONG", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotQty, gotValue := computeEndPosition(tt.trades, tt.endPrice, tt.direction)
			if gotQty != tt.wantQty || gotValue != tt.wantValue {
				t.Errorf("computeEndPosition() = (%.4f, %.4f), want (%.4f, %.4f)", gotQty, gotValue, tt.wantQty, tt.wantValue)
			}
		})
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

func TestCalcOpenCloseDiff(t *testing.T) {
	if got := calcOpenCloseDiff(978, 984, "SHORT"); got != 6 {
		t.Fatalf("SHORT open-close diff = %d, want 6", got)
	}
	if got := calcOpenCloseDiff(984, 978, "LONG"); got != 6 {
		t.Fatalf("LONG open-close diff = %d, want 6", got)
	}
}

func TestBuildTradeCountExplain(t *testing.T) {
	t.Run("count mismatch but flat quantity", func(t *testing.T) {
		got := buildTradeCountExplain("SHORT", 6, 0, 2, 0)
		if !strings.Contains(got, "持倉為 0") {
			t.Fatalf("expected flat-quantity explanation, got: %s", got)
		}
	})

	t.Run("non-flat end quantity", func(t *testing.T) {
		got := buildTradeCountExplain("SHORT", 6, -0.1, 0, 0)
		if !strings.Contains(got, "未平空頭倉位") {
			t.Fatalf("expected short liability explanation, got: %s", got)
		}
	})

	t.Run("fully matched", func(t *testing.T) {
		got := buildTradeCountExplain("LONG", 0, 0, 0, 0)
		if !strings.Contains(got, "一致") {
			t.Fatalf("expected consistent explanation, got: %s", got)
		}
	})
}

func TestFormatParamKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"grid_spacing", "網格間距"},
		{"grid_count", "格子數"},
		{"order_quantity", "單筆訂單大小（USDT）"},
		{"fee_rate", "手續費率"},
		{"risk_volume_multiplier", "風控-成交量倍數"},
		{"risk_average_window", "風控-均線窗口"},
		{"unknown_key", "unknown_key"},
	}
	for _, tt := range tests {
		got := formatParamKey(tt.key)
		if got != tt.want {
			t.Errorf("formatParamKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestMultiStrategyReportEndSettlement(t *testing.T) {
	now := time.Now()
	// 估值收官
	resultNoLiq := &MultiStrategyResult{
		Symbol:         "BTCUSDT",
		StartTime:      now.Add(-7 * 24 * time.Hour),
		EndTime:        now,
		InitialCapital: 10000,
		FinalEquity:    10500,
		EndSettlement:  EndSettlementDetail{Liquidated: false},
		RiskMetrics:    &MultiStrategyRiskMetrics{},
	}
	content, err := renderMultiStrategyReportTemplate(buildMultiStrategyReportData(resultNoLiq))
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if !strings.Contains(content, "估值收官") {
		preview := content
		if len(preview) > 500 {
			preview = preview[:500]
		}
		t.Errorf("expected 估值收官 when not liquidated, got: %s", preview)
	}
	if strings.Contains(content, "強平收官") {
		t.Error("should not show 強平收官 when not liquidated")
	}

	// 強平收官
	resultLiq := &MultiStrategyResult{
		Symbol:         "BTCUSDT",
		StartTime:      now.Add(-7 * 24 * time.Hour),
		EndTime:        now,
		InitialCapital: 10000,
		FinalEquity:    2000,
		EndSettlement: EndSettlementDetail{
			Liquidated:       true,
			LiquidationPrice: 65000,
			LiquidationQty:   0.05,
			LiquidationAmt:   3250,
		},
		RiskMetrics: &MultiStrategyRiskMetrics{},
	}
	content2, err := renderMultiStrategyReportTemplate(buildMultiStrategyReportData(resultLiq))
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if !strings.Contains(content2, "強平收官") {
		preview := content2
		if len(preview) > 500 {
			preview = preview[:500]
		}
		t.Errorf("expected 強平收官 when liquidated, got: %s", preview)
	}
	if !strings.Contains(content2, "65000.00") || !strings.Contains(content2, "0.050000") || !strings.Contains(content2, "3250.0000") {
		preview := content2
		if len(preview) > 800 {
			preview = preview[:800]
		}
		t.Errorf("expected liquidation price/qty/amt in report, got: %s", preview)
	}
}

func buildMultiStrategyReportData(result *MultiStrategyResult) MultiStrategyReportData {
	endLiqPrice, endLiqQty, endLiqAmt := "", "", ""
	if result.EndSettlement.Liquidated {
		endLiqPrice = fmt.Sprintf("%.2f", result.EndSettlement.LiquidationPrice)
		endLiqQty = fmt.Sprintf("%.6f", result.EndSettlement.LiquidationQty)
		endLiqAmt = fmt.Sprintf("%.4f USDT", result.EndSettlement.LiquidationAmt)
	}
	return MultiStrategyReportData{
		GeneratedAt:         time.Now().Format("2006-01-02 15:04:05"),
		Symbol:              result.Symbol,
		StartDate:           result.StartTime.Format("2006-01-02 15:04:05"),
		EndDate:             result.EndTime.Format("2006-01-02 15:04:05"),
		Duration:            "7 天",
		InitialCapital:      result.InitialCapital,
		FinalEquity:         result.FinalEquity,
		TotalReturnPct:      5,
		MaxDrawdownPct:      2,
		SharpeRatio:         1,
		WinRate:             60,
		TotalTrades:         10,
		TotalFees:           1,
		TotalFunding:        0,
		EndLiquidated:       result.EndSettlement.Liquidated,
		EndLiquidationPrice: endLiqPrice,
		EndLiquidationQty:   endLiqQty,
		EndLiquidationAmt:   endLiqAmt,
		ParamsTable:         nil,
	}
}

func TestReportParamsTableWithLabels(t *testing.T) {
	now := time.Now()
	result := &BacktestResult{
		Strategy:       "grid",
		Symbol:         "BTCUSDT",
		InitialCapital: 10000,
		FinalCapital:   11000,
		StartTime:      now.Add(-7 * 24 * time.Hour),
		EndTime:        now,
		Metrics:        Metrics{TotalReturn: 10, MaxDrawdown: 2, SharpeRatio: 1.5, TotalTrades: 20},
	}
	meta := &ReportMeta{
		Interval: "1m",
		Params: map[string]interface{}{
			"grid_spacing":           170,
			"grid_count":             60,
			"order_quantity":         350,
			"fee_rate":               0.0004,
			"direction":              "SHORT",
			"risk_volume_multiplier": 3.0,
			"risk_average_window":    20,
			"total_capital":          10000,
		},
	}
	data := prepareReportData(result, meta)
	if len(data.ParamsTable) == 0 {
		t.Fatal("ParamsTable should not be empty when meta has params")
	}
	keySet := make(map[string]bool)
	for _, row := range data.ParamsTable {
		keySet[row.Key] = true
	}
	wantLabels := []string{"網格間距", "格子數", "單筆訂單大小（USDT）", "手續費率", "風控-成交量倍數", "風控-均線窗口"}
	for _, label := range wantLabels {
		if !keySet[label] {
			t.Errorf("ParamsTable should contain label %q", label)
		}
	}
	// fee_rate 應顯示為百分比
	for _, row := range data.ParamsTable {
		if row.Key == "手續費率" && !strings.Contains(row.Value, "%") {
			t.Errorf("fee_rate value should show as percentage, got %q", row.Value)
		}
	}
}

func TestReportHelperFormattingBranches(t *testing.T) {
	if got := getDirectionFromMeta(&ReportMeta{Params: map[string]interface{}{"direction": float64(0)}}); got != "LONG" {
		t.Fatalf("float direction 0 = %q, want LONG", got)
	}
	if got := getDirectionFromMeta(&ReportMeta{Params: map[string]interface{}{"direction": float64(1)}}); got != "SHORT" {
		t.Fatalf("float direction 1 = %q, want SHORT", got)
	}
	if got := getDirectionFromMeta(&ReportMeta{Params: map[string]interface{}{"direction": float64(2)}}); got != "BOTH" {
		t.Fatalf("float direction 2 = %q, want BOTH", got)
	}
	if got := formatGridDirectionLabel(&ReportMeta{Params: map[string]interface{}{"direction": float64(2)}}); got != "雙向網格" {
		t.Fatalf("float direction label = %q, want 雙向網格", got)
	}

	if got := formatComparisonEndPositionQty(-0.25, "SHORT", "ETH"); got != "欠 0.250000 ETH" {
		t.Fatalf("short end qty = %q", got)
	}
	if got := formatComparisonEndPositionValue(-500, "SHORT"); got != "倉位負債 500.0000 USDT" {
		t.Fatalf("short end value = %q", got)
	}
	if got := baseAssetFromSymbol("ethusdc"); got != "ETH" {
		t.Fatalf("base asset = %q, want ETH", got)
	}
	if got := baseAssetFromSymbol("CUSTOMPAIR"); got != "CUSTOMPAIR" {
		t.Fatalf("custom base asset = %q", got)
	}
	if got := formatStrategyConfig(map[string]interface{}{"z": true, "a": 2}); got != "a=2, z=true" {
		t.Fatalf("strategy config = %q", got)
	}
}

func TestGenerateReportToFileWithPriceCurveAndShortLeverage(t *testing.T) {
	now := time.Unix(1780000000, 0)
	result := &BacktestResult{
		Strategy:       "grid",
		Symbol:         "ETHUSDT",
		InitialCapital: 10000,
		FinalCapital:   10800,
		StartTime:      now.Add(-48 * time.Hour),
		EndTime:        now,
		Trades: []Trade{
			{Timestamp: now.Add(-40 * time.Hour).UnixMilli(), Type: "sell", Price: 3200, Quantity: 2},
			{Timestamp: now.Add(-24 * time.Hour).UnixMilli(), Type: "buy", Price: 3000, Quantity: 1, PnL: 200},
		},
		Metrics: Metrics{
			TotalReturn:      8,
			AnnualizedReturn: 20,
			MaxDrawdown:      5,
			SharpeRatio:      1.6,
			TotalTrades:      1,
			BuyCount:         1,
			SellCount:        1,
			ProfitFactor:     math.Inf(1),
		},
		RiskMetrics: RiskMetrics{VaR95: 1.2, VaR99: 2.3, CVaR95: 3.4, CVaR99: 4.5},
		PriceCurve: &PriceCurveSummary{
			StartPrice:            3100,
			EndPrice:              2900,
			Top3Valleys:           []float64{2800, 2850},
			Top3Peaks:             []float64{3300, 3250},
			MaxConsecutiveDecline: 300,
			MaxConsecutiveRise:    180,
		},
	}
	meta := &ReportMeta{
		Interval: "5m",
		Params: map[string]interface{}{
			"direction": "SHORT",
			"leverage":  float64(5),
			"fee_rate":  float64(0.0005),
		},
	}

	path := filepath.Join(t.TempDir(), "reports", "short.md")
	if err := GenerateReportToFile(result, path, meta); err != nil {
		t.Fatalf("GenerateReportToFile() error = %v", err)
	}
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	content := string(contentBytes)
	for _, want := range []string{
		"K 線周期 | 5m",
		"欠 1.000000 ETH / 5x = 0.200000 ETH",
		"倉位負債 2900.0000 USDT / 5x = 580.0000 USDT",
		"∞（全部盈利）",
		"策略 8.0000%",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("report should contain %q, got preview: %.500s", want, content)
		}
	}
}

func TestGenerateComparisonReportToFileAndAnalysisBranches(t *testing.T) {
	now := time.Unix(1780000000, 0)
	noRisk := &BacktestResult{
		Strategy:       "grid",
		Symbol:         "BTCUSDT",
		InitialCapital: 10000,
		FinalCapital:   11200,
		StartTime:      now.Add(-72 * time.Hour),
		EndTime:        now,
		Trades: []Trade{
			{Timestamp: now.Add(-60 * time.Hour).UnixMilli(), Type: "buy", Price: 60000, Quantity: 0.1},
			{Timestamp: now.Add(-36 * time.Hour).UnixMilli(), Type: "sell", Price: 62000, Quantity: 0.1, PnL: 200},
		},
		Metrics:    Metrics{TotalReturn: 12, MaxDrawdown: 8, TotalTrades: 2, BuyCount: 1, SellCount: 1, SharpeRatio: 1.2, ProfitFactor: 2},
		PriceCurve: &PriceCurveSummary{StartPrice: 60000, EndPrice: 61000},
	}
	withRisk := &BacktestResult{
		Strategy:       "grid",
		Symbol:         "BTCUSDT",
		InitialCapital: 10000,
		FinalCapital:   11600,
		StartTime:      noRisk.StartTime,
		EndTime:        noRisk.EndTime,
		Trades: []Trade{
			{Timestamp: now.Add(-58 * time.Hour).UnixMilli(), Type: "buy", Price: 59500, Quantity: 0.1},
			{Timestamp: now.Add(-30 * time.Hour).UnixMilli(), Type: "sell", Price: 62500, Quantity: 0.1, PnL: 300},
		},
		Metrics: Metrics{TotalReturn: 16, MaxDrawdown: 5, TotalTrades: 2, BuyCount: 1, SellCount: 1, SharpeRatio: 1.4, ProfitFactor: 3},
		RiskInterventions: []RiskIntervention{
			{Timestamp: now.UnixMilli(), Reason: "volume spike", RiskType: "volume", Duration: 3, SkippedBuys: 2},
			{TimeStr: "custom-time", Reason: "depth thin", RiskType: "depth", Duration: 1, SkippedBuys: 0},
		},
		PriceCurve: &PriceCurveSummary{StartPrice: 60000, EndPrice: 61000},
	}
	comparison := &ComparisonResult{
		NoRiskResult:   noRisk,
		WithRiskResult: withRisk,
		Comparison: &ComparisonMetrics{
			ReturnDiff:            4,
			DrawdownDiff:          -3,
			TradeCountDiff:        0,
			RiskInterventionCount: 2,
			SkippedSignals:        2,
		},
	}
	path := filepath.Join(t.TempDir(), "comparison", "risk.md")
	if err := GenerateComparisonReportToFile(comparison, path, &ReportMeta{Interval: "1m", Params: map[string]interface{}{"direction": "LONG"}}); err != nil {
		t.Fatalf("GenerateComparisonReportToFile() error = %v", err)
	}
	contentBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read comparison report: %v", err)
	}
	content := string(contentBytes)
	for _, want := range []string{
		"無風控 vs 有風控對比",
		"本次回測風控共介入 **2** 次",
		"volume spike",
		"custom-time",
		"有風控收益率（16.0000%）較無風控（12.0000%）高 4.0000 個百分點",
		"改善 3.0000 個百分點",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("comparison report should contain %q, got preview: %.800s", want, content)
		}
	}
}

func TestGenerateRiskAnalysisFallbacks(t *testing.T) {
	comp := &ComparisonResult{
		NoRiskResult:   &BacktestResult{Metrics: Metrics{TotalReturn: 5, MaxDrawdown: 4}},
		WithRiskResult: &BacktestResult{Metrics: Metrics{TotalReturn: 5, MaxDrawdown: 6}},
	}
	if got := generateRiskAnalysis(comp); got != "風控對比數據不可用。" {
		t.Fatalf("nil comparison analysis = %q", got)
	}

	comp.Comparison = &ComparisonMetrics{ReturnDiff: 0, DrawdownDiff: 2}
	got := generateRiskAnalysis(comp)
	if !strings.Contains(got, "風控未觸發") || !strings.Contains(got, "收益率相同") || !strings.Contains(got, "增大 2.0000 個百分點") {
		t.Fatalf("unexpected neutral analysis: %s", got)
	}
}
