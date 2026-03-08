package backtest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"
)

// ReportMeta 報告用元數據（K 線周期、回測參數等，由任務調用方傳入）
type ReportMeta struct {
	Interval string                 // K 線周期，如 1m, 5m, 1h
	Params   map[string]interface{} // 回測參數（含策略參數與風控參數）
}

// GenerateReport 生成 Markdown 回测报告
func GenerateReport(result *BacktestResult) (string, error) {
	// 創建报告目錄
	reportDir := filepath.Join("backtest", "reports")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return "", fmt.Errorf("創建报告目錄失敗: %w", err)
	}

	// 生成报告檔案名
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("%s_%s_%s.md",
		result.Strategy,
		result.Symbol,
		timestamp,
	)
	reportPath := filepath.Join(reportDir, filename)

	// 准备模板數據（無 meta 時不輸出 K 線周期與參數表）
	data := prepareReportData(result, nil)

	// 渲染模板
	content, err := renderReportTemplate(data)
	if err != nil {
		return "", fmt.Errorf("渲染报告模板失敗: %w", err)
	}

	// 寫入檔案
	if err := os.WriteFile(reportPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("寫入报告檔案失敗: %w", err)
	}

	return reportPath, nil
}

// GenerateReportToFile 生成回测报告到指定路径（供任務結果使用）。meta 可為 nil，為 nil 時不輸出 K 線周期與參數表。
func GenerateReportToFile(result *BacktestResult, reportPath string, meta *ReportMeta) error {
	data := prepareReportData(result, meta)
	content, err := renderReportTemplate(data)
	if err != nil {
		return fmt.Errorf("渲染报告模板失敗: %w", err)
	}
	dir := filepath.Dir(reportPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("創建报告目錄失敗: %w", err)
	}
	return os.WriteFile(reportPath, []byte(content), 0644)
}

// ComparisonReportData 对比报告數據
type ComparisonReportData struct {
	ReportData
	// 無風控
	NoRiskTotalReturn  string
	NoRiskMaxDrawdown  string
	NoRiskTotalTrades  string
	NoRiskBuyCount     string
	NoRiskSellCount    string
	NoRiskFinalCapital string
	// 有風控
	WithRiskTotalReturn  string
	WithRiskMaxDrawdown  string
	WithRiskTotalTrades  string
	WithRiskBuyCount     string
	WithRiskSellCount    string
	WithRiskFinalCapital string
	// 差異
	ReturnDiff     string
	DrawdownDiff   string
	TradeCountDiff string
	// 風控介入
	InterventionCount int
	SkippedSignals    int
	// 分類介入記錄：有跳過買入（介入）和無跳過買入（未介入）
	InterventionsSkipped    []RiskInterventionRow // 有跳過買入的介入（前10條）
	InterventionsNotSkipped []RiskInterventionRow // 無跳過買入的介入（前10條）
	TotalSkippedCount       int                   // 有跳過買入的總數
	TotalNotSkippedCount    int                   // 無跳過買入的總數
	RiskAnalysis            string
}

// RiskInterventionRow 風控介入行（用於報告表格）
type RiskInterventionRow struct {
	TimeStr     string
	Reason      string
	RiskType    string
	Duration    string
	SkippedBuys string
}

// 風控介入記錄顯示上限
const maxInterventionDisplay = 10

// GenerateComparisonReportToFile 生成對比報告到指定路徑。meta 可為 nil，為 nil 時不輸出 K 線周期與參數表。
func GenerateComparisonReportToFile(comparison *ComparisonResult, reportPath string, meta *ReportMeta) error {
	data := prepareComparisonReportData(comparison, meta)
	content, err := renderComparisonReportTemplate(data)
	if err != nil {
		return fmt.Errorf("渲染對比報告模板失敗: %w", err)
	}
	dir := filepath.Dir(reportPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("創建报告目錄失敗: %w", err)
	}
	return os.WriteFile(reportPath, []byte(content), 0644)
}

// prepareComparisonReportData 準備對比報告數據
func prepareComparisonReportData(comp *ComparisonResult, meta *ReportMeta) ComparisonReportData {
	noRisk := comp.NoRiskResult
	withRisk := comp.WithRiskResult
	cm := comp.Comparison
	if cm == nil {
		cm = &ComparisonMetrics{}
	}

	// 使用無風控結果作為基礎 ReportData（帶 meta 以輸出 K 線周期與參數）
	base := prepareReportData(noRisk, meta)

	// 風控介入表格：分為有跳過買入（介入）和無跳過買入（未介入）
	var intervSkipped, intervNotSkipped []RiskInterventionRow
	totalSkippedCount, totalNotSkippedCount := 0, 0
	for _, inv := range withRisk.RiskInterventions {
		timeStr := inv.TimeStr
		if timeStr == "" {
			timeStr = formatRiskTimestamp(inv.Timestamp)
		}
		row := RiskInterventionRow{
			TimeStr:     timeStr,
			Reason:      inv.Reason,
			RiskType:    inv.RiskType,
			Duration:    fmt.Sprintf("%d", inv.Duration),
			SkippedBuys: fmt.Sprintf("%d", inv.SkippedBuys),
		}
		if inv.SkippedBuys > 0 {
			totalSkippedCount++
			if len(intervSkipped) < maxInterventionDisplay {
				intervSkipped = append(intervSkipped, row)
			}
		} else {
			totalNotSkippedCount++
			if len(intervNotSkipped) < maxInterventionDisplay {
				intervNotSkipped = append(intervNotSkipped, row)
			}
		}
	}

	// 風控效果分析
	riskAnalysis := generateRiskAnalysis(comp)

	return ComparisonReportData{
		ReportData:              base,
		NoRiskTotalReturn:       fmt.Sprintf("%.4f%%", noRisk.Metrics.TotalReturn),
		NoRiskMaxDrawdown:       fmt.Sprintf("%.4f%%", noRisk.Metrics.MaxDrawdown),
		NoRiskTotalTrades:       fmt.Sprintf("%d", noRisk.Metrics.TotalTrades),
		NoRiskBuyCount:          fmt.Sprintf("%d", noRisk.Metrics.BuyCount),
		NoRiskSellCount:         fmt.Sprintf("%d", noRisk.Metrics.SellCount),
		NoRiskFinalCapital:      fmt.Sprintf("%.4f", noRisk.FinalCapital),
		WithRiskTotalReturn:     fmt.Sprintf("%.4f%%", withRisk.Metrics.TotalReturn),
		WithRiskMaxDrawdown:     fmt.Sprintf("%.4f%%", withRisk.Metrics.MaxDrawdown),
		WithRiskTotalTrades:     fmt.Sprintf("%d", withRisk.Metrics.TotalTrades),
		WithRiskBuyCount:        fmt.Sprintf("%d", withRisk.Metrics.BuyCount),
		WithRiskSellCount:       fmt.Sprintf("%d", withRisk.Metrics.SellCount),
		WithRiskFinalCapital:    fmt.Sprintf("%.4f", withRisk.FinalCapital),
		ReturnDiff:              fmt.Sprintf("%+.4f%%", cm.ReturnDiff),
		DrawdownDiff:            fmt.Sprintf("%+.4f%%", cm.DrawdownDiff),
		TradeCountDiff:          fmt.Sprintf("%+d", cm.TradeCountDiff),
		InterventionCount:       cm.RiskInterventionCount,
		SkippedSignals:          cm.SkippedSignals,
		InterventionsSkipped:    intervSkipped,
		InterventionsNotSkipped: intervNotSkipped,
		TotalSkippedCount:       totalSkippedCount,
		TotalNotSkippedCount:    totalNotSkippedCount,
		RiskAnalysis:            riskAnalysis,
	}
}

func formatRiskTimestamp(ts int64) string {
	var sec int64
	if ts > 10000000000 {
		sec = ts / 1000
	} else {
		sec = ts
	}
	return time.Unix(sec, 0).Format("2006-01-02 15:04:05")
}

func generateRiskAnalysis(comp *ComparisonResult) string {
	cm := comp.Comparison
	if cm == nil {
		return "風控對比數據不可用。"
	}
	withRisk := comp.WithRiskResult
	noRisk := comp.NoRiskResult

	var lines []string
	if cm.RiskInterventionCount > 0 {
		lines = append(lines, fmt.Sprintf("本次回測風控共介入 **%d** 次，跳過 **%d** 個買入信號。", cm.RiskInterventionCount, cm.SkippedSignals))
	} else {
		lines = append(lines, "本次回測期間風控未觸發，無風控與有風控結果一致。")
	}

	// 收益率對比
	if cm.ReturnDiff > 0 {
		lines = append(lines, fmt.Sprintf("有風控收益率（%.4f%%）較無風控（%.4f%%）高 %.4f 個百分點。", withRisk.Metrics.TotalReturn, noRisk.Metrics.TotalReturn, cm.ReturnDiff))
	} else if cm.ReturnDiff < 0 {
		lines = append(lines, fmt.Sprintf("有風控收益率（%.4f%%）較無風控（%.4f%%）低 %.4f 個百分點，風控在部分時段跳過買入，可能錯過部分反彈。", withRisk.Metrics.TotalReturn, noRisk.Metrics.TotalReturn, -cm.ReturnDiff))
	} else {
		lines = append(lines, "無風控與有風控收益率相同。")
	}

	// 回撤對比
	if cm.DrawdownDiff < 0 {
		lines = append(lines, fmt.Sprintf("有風控最大回撤（%.4f%%）較無風控（%.4f%%）改善 %.4f 個百分點，風控起到保護作用。", withRisk.Metrics.MaxDrawdown, noRisk.Metrics.MaxDrawdown, -cm.DrawdownDiff))
	} else if cm.DrawdownDiff > 0 {
		lines = append(lines, fmt.Sprintf("有風控最大回撤（%.4f%%）較無風控（%.4f%%）增大 %.4f 個百分點。", withRisk.Metrics.MaxDrawdown, noRisk.Metrics.MaxDrawdown, cm.DrawdownDiff))
	}

	lines = append(lines, "建議將回測結果視為策略潛力參考，實盤需關注風控日誌與暫停時段。")
	return strings.Join(lines, "\n\n")
}

// renderComparisonReportTemplate 渲染對比報告模板
func renderComparisonReportTemplate(data ComparisonReportData) (string, error) {
	tmpl := `# {{.PluginName}} 策略回测报告（無風控 vs 有風控對比）

生成時间: {{.GeneratedAt}}

## 執行摘要

- **交易對**: {{.Symbol}}
- **回测期间**: {{.StartDate}} 至 {{.EndDate}} ({{.Duration}})
- **初始資金**: ${{.InitialCapital}}

{{if .Interval}}
## 回测配置

| 項目 | 值 |
|------|------|
| K 線周期 | {{.Interval}} |
| 回測時間 | {{.StartDate}} 至 {{.EndDate}} |
{{range .ParamsTable}}| {{.Key}} | {{.Value}} |
{{end}}
{{end}}

## 風控對比

| 指標 | 無風控 | 有風控 | 差異 |
|------|--------|--------|------|
| 總收益率 | {{.NoRiskTotalReturn}} | {{.WithRiskTotalReturn}} | {{.ReturnDiff}} |
| 最大回撤 | {{.NoRiskMaxDrawdown}} | {{.WithRiskMaxDrawdown}} | {{.DrawdownDiff}} |
| 總交易次數 | {{.NoRiskTotalTrades}} | {{.WithRiskTotalTrades}} | {{.TradeCountDiff}} |
| 買入次數 | {{.NoRiskBuyCount}} | {{.WithRiskBuyCount}} | - |
| 賣出次數 | {{.NoRiskSellCount}} | {{.WithRiskSellCount}} | - |
| 最終資金 | ${{.NoRiskFinalCapital}} | ${{.WithRiskFinalCapital}} | - |

## 風控介入記錄

共觸發 **{{.InterventionCount}}** 次，跳過 **{{.SkippedSignals}}** 個買入信號。

### 有跳過買入的介入（共 {{.TotalSkippedCount}} 次，顯示前 10 條）

{{if .InterventionsSkipped}}
| 時間 | 原因 | 類型 | 持續K線數 | 跳過買入數 |
|------|------|------|----------|-----------|
{{range .InterventionsSkipped}}| {{.TimeStr}} | {{.Reason}} | {{.RiskType}} | {{.Duration}} | {{.SkippedBuys}} |
{{end}}
{{else}}
本次回測期間無跳過買入的風控介入。
{{end}}

### 無跳過買入的介入（共 {{.TotalNotSkippedCount}} 次，顯示前 10 條）

{{if .InterventionsNotSkipped}}
| 時間 | 原因 | 類型 | 持續K線數 | 跳過買入數 |
|------|------|------|----------|-----------|
{{range .InterventionsNotSkipped}}| {{.TimeStr}} | {{.Reason}} | {{.RiskType}} | {{.Duration}} | {{.SkippedBuys}} |
{{end}}
{{else}}
本次回測期間無未跳過買入的風控觸發。
{{end}}

## 風控效果分析

{{.RiskAnalysis}}

## 無風控詳細指標

| 指標 | 數值 |
|------|------|
| 總收益率 | {{.NoRiskTotalReturn}} |
| 最大回撤 | {{.NoRiskMaxDrawdown}} |
| 夏普比率 | {{.SharpeRatio}} |
| 交易次數 | {{.NoRiskTotalTrades}} |
| 買入/賣出 | {{.NoRiskBuyCount}} / {{.NoRiskSellCount}} |

## 有風控詳細指標

| 指標 | 數值 |
|------|------|
| 總收益率 | {{.WithRiskTotalReturn}} |
| 最大回撤 | {{.WithRiskMaxDrawdown}} |
| 夏普比率 | {{.SharpeRatio}} |
| 交易次數 | {{.WithRiskTotalTrades}} |
| 買入/賣出 | {{.WithRiskBuyCount}} / {{.WithRiskSellCount}} |

## 價格曲線概況

{{if .HasPriceCurve}}
| 項目 | 數值 |
|------|------|
| 開始價 | {{.PriceCurveStartPrice}} |
| 結束價 | {{.PriceCurveEndPrice}} |
| 始末涨跌幅 | {{.PriceCurveStartEndReturn}} |

**策略 vs 持有**：{{.PriceCurveStrategyVsHold}}
{{else}}
（無價格曲線數據）
{{end}}

## 成對交易（無風控，前10笔）

{{if .TopPairedTrades}}
| 買入時間 | 買入價 | 賣出時間 | 賣出價 | 數量 | 盈虧 |
|----------|--------|----------|--------|------|------|
{{range .TopPairedTrades}}| {{.BuyTime}} | {{.BuyPrice}} | {{.SellTime}} | {{.SellPrice}} | {{.Quantity}} | {{.PnL}} |
{{end}}
{{end}}

## 結論

{{.Conclusion}}

---

*本报告由 QuantMesh 回测系统自动生成（無風控 vs 有風控對比）*
`

	t, err := template.New("comparison_report").Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// ReportParamRow 報告參數行（用於參數表）
type ReportParamRow struct {
	Key   string
	Value string
}

// ReportData 报告數據
type ReportData struct {
	// 基本信息
	PluginName     string
	Symbol         string
	GeneratedAt    string
	StartDate      string
	EndDate        string
	Duration       string
	InitialCapital string
	FinalCapital   string

	// 數據與參數（由 ReportMeta 填充，meta 為 nil 時為空）
	Interval    string           // K 線周期，如 1m, 5m, 1h
	ParamsTable []ReportParamRow // 回測參數表（含策略與風控參數）

	// 收益指標
	TotalReturn      string
	AnnualizedReturn string

	// 风險指標
	MaxDrawdown         string
	MaxDrawdownDuration string
	Volatility          string

	// 风險調整收益
	SharpeRatio  string
	SortinoRatio string
	CalmarRatio  string

	// 交易指標
	TotalTrades          string
	BuyCount             string
	SellCount            string
	WinRate              string
	ExchangeWinRate      string // 交易所风格勝率（基于平均持仓成本）
	ProfitFactor         string
	AvgWin               string
	AvgLoss              string
	LargestWin           string
	LargestLoss          string
	MaxConsecutiveWins   string
	MaxConsecutiveLosses string
	MaxPosition          string // 最大持倉（基幣數量，如 0.1234 BTC）
	EndPositionQty       string // 期末持倉（基幣數量）
	EndPositionValue     string // 期末持倉市值（USDT）
	EndCashUSDT          string // 期末持有 USDT（現金）
	TotalSlippageLoss    string // 🔥 累计價格偏差（slippage）損失（USDT）

	// 交易明细
	TopTrades         []TradeRow  // 前20筆原始成交
	TopPairedTrades   []PairedRow // 成對交易前10
	TopUnpairedTrades []TradeRow  // 未成對交易前10

	// 风險指標
	VaR95  string
	VaR99  string
	CVaR95 string
	CVaR99 string

	// 價格曲線概況（拐点、起止价、最大连续涨跌价差）
	HasPriceCurve            bool
	PriceCurveStartPrice     string
	PriceCurveEndPrice       string
	PriceCurveStartEndReturn string   // 始末涨跌幅（买入持有收益率 %）
	PriceCurveStrategyVsHold string   // 策略收益 vs 持有收益 对比说明
	PriceCurveValleys        []string // 最重要的 3 个谷底价（由低到高）
	PriceCurvePeaks          []string // 最重要的 3 个峰值价（由高到低）
	PriceCurveMaxDecline     string   // 最大连续下跌价差
	PriceCurveMaxRise        string   // 最大连续上涨价差

	// 結論
	Conclusion string
}

// TradeRow 交易行
type TradeRow struct {
	Time     string
	Type     string
	Price    string
	Quantity string
	PnL      string
}

// PairedRow 成對交易行（買入+賣出一對）
type PairedRow struct {
	BuyTime   string
	BuyPrice  string
	SellTime  string
	SellPrice string
	Quantity  string
	PnL       string
}

// extractPairedAndUnpairedTrades 從交易序列提取成對與未成對交易（FIFO 匹配）
// 返回：成對列表（前 maxN 個）、未成對列表（前 maxN 個）
func extractPairedAndUnpairedTrades(trades []Trade, maxN int) ([]PairedRow, []TradeRow) {
	var buyQueue []Trade
	var paired []PairedRow
	var unpaired []TradeRow

	for _, t := range trades {
		if t.Type == "buy" {
			buyQueue = append(buyQueue, t)
		} else if t.Type == "sell" {
			if len(buyQueue) > 0 {
				buy := buyQueue[0]
				buyQueue = buyQueue[1:]
				buyTime := time.Unix(buy.Timestamp/1000, 0)
				sellTime := time.Unix(t.Timestamp/1000, 0)
				if len(paired) < maxN {
					paired = append(paired, PairedRow{
						BuyTime:   buyTime.Format("2006-01-02 15:04"),
						BuyPrice:  fmt.Sprintf("%.2f", buy.Price),
						SellTime:  sellTime.Format("2006-01-02 15:04"),
						SellPrice: fmt.Sprintf("%.2f", t.Price),
						Quantity:  fmt.Sprintf("%.4f", t.Quantity),
						PnL:       fmt.Sprintf("%.2f", t.PnL),
					})
				}
			} else {
				// 無對應買入的賣出（如做空場景）
				if len(unpaired) < maxN {
					sellTime := time.Unix(t.Timestamp/1000, 0)
					unpaired = append(unpaired, TradeRow{
						Time:     sellTime.Format("2006-01-02 15:04"),
						Type:     "sell",
						Price:    fmt.Sprintf("%.2f", t.Price),
						Quantity: fmt.Sprintf("%.4f", t.Quantity),
						PnL:      fmt.Sprintf("%.2f", t.PnL),
					})
				}
			}
		}
	}
	// 剩餘未匹配的買入
	for _, buy := range buyQueue {
		if len(unpaired) >= maxN {
			break
		}
		buyTime := time.Unix(buy.Timestamp/1000, 0)
		unpaired = append(unpaired, TradeRow{
			Time:     buyTime.Format("2006-01-02 15:04"),
			Type:     "buy",
			Price:    fmt.Sprintf("%.2f", buy.Price),
			Quantity: fmt.Sprintf("%.4f", buy.Quantity),
			PnL:      "-",
		})
	}
	return paired, unpaired
}

// computeEndPosition 從交易記錄計算期末持倉數量與市值
func computeEndPosition(trades []Trade, endPrice float64) (qty float64, value float64) {
	for _, t := range trades {
		if t.Type == "buy" {
			qty += t.Quantity
		} else if t.Type == "sell" {
			qty -= t.Quantity
		}
	}
	if qty < 0 {
		qty = 0
	}
	value = qty * endPrice
	return qty, value
}

// baseAssetFromSymbol 從交易對推導基幣名稱，如 BTCUSDT -> BTC
func baseAssetFromSymbol(symbol string) string {
	s := strings.ToUpper(symbol)
	for _, suffix := range []string{"USDT", "BUSD", "USDC", "DAI", "U"} {
		if strings.HasSuffix(s, suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return symbol
}

// formatParamValue 格式化參數值為字符串（用於報告參數表）
func formatParamValue(v interface{}) string {
	switch val := v.(type) {
	case float64:
		// 根據數值大小決定格式
		if val == float64(int(val)) {
			return fmt.Sprintf("%.0f", val)
		}
		return fmt.Sprintf("%g", val)
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// prepareReportData 准备报告數據。meta 可為 nil，為 nil 時 Interval 與 ParamsTable 為空。
func prepareReportData(result *BacktestResult, meta *ReportMeta) ReportData {
	m := result.Metrics

	// 計算持续時间
	duration := result.EndTime.Sub(result.StartTime)
	durationStr := fmt.Sprintf("%d 天", int(duration.Hours()/24))

	// K 線周期與回測參數表（由 meta 填充）
	var interval string
	var paramsTable []ReportParamRow
	if meta != nil {
		interval = meta.Interval
		if len(meta.Params) > 0 {
			keys := make([]string, 0, len(meta.Params))
			for k := range meta.Params {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				v := meta.Params[k]
				paramsTable = append(paramsTable, ReportParamRow{Key: k, Value: formatParamValue(v)})
			}
		}
	}

	// 准备交易明细（前20笔）：包含买/卖所有成交，避免仅統計 sell 时策略多为买入导致明细为空
	topTrades := make([]TradeRow, 0)
	for i, trade := range result.Trades {
		if i >= 20 {
			break
		}
		tradeTime := time.Unix(trade.Timestamp/1000, 0)
		topTrades = append(topTrades, TradeRow{
			Time:     tradeTime.Format("2006-01-02 15:04"),
			Type:     trade.Type,
			Price:    fmt.Sprintf("%.2f", trade.Price),
			Quantity: fmt.Sprintf("%.4f", trade.Quantity),
			PnL:      fmt.Sprintf("%.2f", trade.PnL),
		})
	}

	// 成對交易（前10）、未成對交易（前10）
	topPaired, topUnpaired := extractPairedAndUnpairedTrades(result.Trades, 10)

	// 價格曲線概況（若有）
	hasPriceCurve := result.PriceCurve != nil
	priceStart, priceEnd := "", ""
	priceStartEndReturn := ""
	priceStrategyVsHold := ""
	var priceValleys, pricePeaks []string
	priceMaxDecline, priceMaxRise := "", ""
	if hasPriceCurve {
		pc := result.PriceCurve
		priceStart = fmt.Sprintf("%.2f", pc.StartPrice)
		priceEnd = fmt.Sprintf("%.2f", pc.EndPrice)
		if pc.StartPrice > 0 {
			startEndPct := (pc.EndPrice - pc.StartPrice) / pc.StartPrice * 100
			priceStartEndReturn = fmt.Sprintf("%.4f%%", startEndPct)
			// 策略總收益率 vs 持有收益率，差距（百分點）
			diff := m.TotalReturn - startEndPct
			if diff >= 0 {
				priceStrategyVsHold = fmt.Sprintf("策略 %s，持有 %s，策略跑贏持有 %.4f 個百分點。", fmt.Sprintf("%.4f%%", m.TotalReturn), priceStartEndReturn, diff)
			} else {
				priceStrategyVsHold = fmt.Sprintf("策略 %s，持有 %s，持有跑贏策略 %.4f 個百分點。", fmt.Sprintf("%.4f%%", m.TotalReturn), priceStartEndReturn, -diff)
			}
		}
		for _, v := range pc.Top3Valleys {
			priceValleys = append(priceValleys, fmt.Sprintf("%.2f", v))
		}
		for _, p := range pc.Top3Peaks {
			pricePeaks = append(pricePeaks, fmt.Sprintf("%.2f", p))
		}
		priceMaxDecline = fmt.Sprintf("%.2f", pc.MaxConsecutiveDecline)
		priceMaxRise = fmt.Sprintf("%.2f", pc.MaxConsecutiveRise)
	}

	// 期末持倉：從交易記錄推算，使用結束價計市值
	endPrice := 0.0
	if hasPriceCurve {
		endPrice = result.PriceCurve.EndPrice
	}
	endPosQty, endPosValue := computeEndPosition(result.Trades, endPrice)
	endCashUSDT := result.FinalCapital - endPosValue // 最終權益 - 持倉市值 = 現金
	if endCashUSDT < 0 {
		endCashUSDT = 0
	}
	base := baseAssetFromSymbol(result.Symbol)

	// 生成結論
	conclusion := generateConclusion(result)

	return ReportData{
		PluginName:     result.Strategy,
		Symbol:         result.Symbol,
		GeneratedAt:    time.Now().Format("2006-01-02 15:04:05"),
		StartDate:      result.StartTime.Format("2006-01-02"),
		EndDate:        result.EndTime.Format("2006-01-02"),
		Duration:       durationStr,
		InitialCapital: fmt.Sprintf("%.4f", result.InitialCapital),
		FinalCapital:   fmt.Sprintf("%.4f", result.FinalCapital),
		Interval:       interval,
		ParamsTable:    paramsTable,

		TotalReturn:      fmt.Sprintf("%.4f%%", m.TotalReturn),
		AnnualizedReturn: fmt.Sprintf("%.4f%%", m.AnnualizedReturn),

		MaxDrawdown:         fmt.Sprintf("%.4f%%", m.MaxDrawdown),
		MaxDrawdownDuration: fmt.Sprintf("%d 天", m.MaxDrawdownDuration),
		Volatility:          fmt.Sprintf("%.4f%%", m.Volatility),

		SharpeRatio:  fmt.Sprintf("%.4f", m.SharpeRatio),
		SortinoRatio: fmt.Sprintf("%.4f", m.SortinoRatio),
		CalmarRatio:  fmt.Sprintf("%.4f", m.CalmarRatio),

		TotalTrades:          fmt.Sprintf("%d", m.TotalTrades),
		BuyCount:             fmt.Sprintf("%d", m.BuyCount),
		SellCount:            fmt.Sprintf("%d", m.SellCount),
		WinRate:              fmt.Sprintf("%.4f%%", m.WinRate),
		ExchangeWinRate:      fmt.Sprintf("%.4f%%", m.ExchangeStyle.ExchangeWinRate),
		ProfitFactor:         fmt.Sprintf("%.4f", m.ProfitFactor),
		AvgWin:               fmt.Sprintf("%.4f", m.AvgWin),
		AvgLoss:              fmt.Sprintf("%.4f", m.AvgLoss),
		LargestWin:           fmt.Sprintf("%.4f", m.LargestWin),
		LargestLoss:          fmt.Sprintf("%.4f", m.LargestLoss),
		MaxConsecutiveWins:   fmt.Sprintf("%d", m.MaxConsecutiveWins),
		MaxConsecutiveLosses: fmt.Sprintf("%d", m.MaxConsecutiveLosses),
		MaxPosition:          fmt.Sprintf("%.6f %s", m.MaxPosition, base),
		EndPositionQty:       fmt.Sprintf("%.6f %s", endPosQty, base),
		EndPositionValue:     fmt.Sprintf("%.4f USDT", endPosValue),
		EndCashUSDT:          fmt.Sprintf("%.4f USDT", endCashUSDT),
		TotalSlippageLoss:    fmt.Sprintf("%.4f USDT", m.TotalSlippageLoss), // 🔥 價格偏差損失

		TopTrades:         topTrades,
		TopPairedTrades:   topPaired,
		TopUnpairedTrades: topUnpaired,

		VaR95:  fmt.Sprintf("%.4f%%", result.RiskMetrics.VaR95),
		VaR99:  fmt.Sprintf("%.4f%%", result.RiskMetrics.VaR99),
		CVaR95: fmt.Sprintf("%.4f%%", result.RiskMetrics.CVaR95),
		CVaR99: fmt.Sprintf("%.4f%%", result.RiskMetrics.CVaR99),

		HasPriceCurve:            hasPriceCurve,
		PriceCurveStartPrice:     priceStart,
		PriceCurveEndPrice:       priceEnd,
		PriceCurveStartEndReturn: priceStartEndReturn,
		PriceCurveStrategyVsHold: priceStrategyVsHold,
		PriceCurveValleys:        priceValleys,
		PriceCurvePeaks:          pricePeaks,
		PriceCurveMaxDecline:     priceMaxDecline,
		PriceCurveMaxRise:        priceMaxRise,

		Conclusion: conclusion,
	}
}

// generateConclusion 生成結論
func generateConclusion(result *BacktestResult) string {
	m := result.Metrics
	var conclusions []string

	// 收益评估
	if m.TotalReturn > 50 {
		conclusions = append(conclusions, "✅ 策略表現优秀，總收益率超過 50%")
	} else if m.TotalReturn > 20 {
		conclusions = append(conclusions, "✅ 策略表現良好，總收益率超過 20%")
	} else if m.TotalReturn > 0 {
		conclusions = append(conclusions, "⚠️ 策略盈利，但收益率较低")
	} else {
		conclusions = append(conclusions, "❌ 策略亏损，需要优化参數或更换策略")
	}

	// 风險评估
	if m.MaxDrawdown < 10 {
		conclusions = append(conclusions, "✅ 风險控制良好，最大回撤小於 10%")
	} else if m.MaxDrawdown < 20 {
		conclusions = append(conclusions, "⚠️ 风險适中，最大回撤在 10-20% 之间")
	} else {
		conclusions = append(conclusions, "❌ 风險较高，最大回撤超過 20%")
	}

	// 夏普比率评估
	if m.SharpeRatio > 2 {
		conclusions = append(conclusions, "✅ 风險調整收益优秀，夏普比率 > 2")
	} else if m.SharpeRatio > 1 {
		conclusions = append(conclusions, "✅ 风險調整收益良好，夏普比率 > 1")
	} else if m.SharpeRatio > 0 {
		conclusions = append(conclusions, "⚠️ 风險調整收益一般，夏普比率 < 1")
	} else {
		conclusions = append(conclusions, "❌ 风險調整收益差，夏普比率為负")
	}

	// 胜率评估
	if m.WinRate > 60 {
		conclusions = append(conclusions, "✅ 胜率高，超過 60%")
	} else if m.WinRate > 50 {
		conclusions = append(conclusions, "✅ 胜率良好，超過 50%")
	} else {
		conclusions = append(conclusions, "⚠️ 胜率较低，需要优化策略")
	}

	// 利润因子评估
	if m.ProfitFactor > 2 {
		conclusions = append(conclusions, "✅ 利润因子优秀，盈利能力强")
	} else if m.ProfitFactor > 1.5 {
		conclusions = append(conclusions, "✅ 利润因子良好")
	} else if m.ProfitFactor > 1 {
		conclusions = append(conclusions, "⚠️ 利润因子一般")
	} else {
		conclusions = append(conclusions, "❌ 利润因子 < 1，平均亏损大於平均盈利")
	}

	return strings.Join(conclusions, "\n\n")
}

// renderReportTemplate 渲染报告模板
func renderReportTemplate(data ReportData) (string, error) {
	tmpl := `# {{.PluginName}} 策略回测报告

生成時间: {{.GeneratedAt}}

## 執行摘要

- **交易對**: {{.Symbol}}
- **回测期间**: {{.StartDate}} 至 {{.EndDate}} ({{.Duration}})
- **初始資金**: ${{.InitialCapital}}
- **最终資金**: ${{.FinalCapital}}
- **總收益率**: {{.TotalReturn}}
- **年化收益率**: {{.AnnualizedReturn}}
- **最大回撤**: {{.MaxDrawdown}}
- **夏普比率**: {{.SharpeRatio}}

{{if .Interval}}
## 回测配置

| 項目 | 值 |
|------|------|
| K 線周期 | {{.Interval}} |
| 回測時間 | {{.StartDate}} 至 {{.EndDate}} |
{{range .ParamsTable}}| {{.Key}} | {{.Value}} |
{{end}}
{{end}}

{{if .HasPriceCurve}}
## 價格曲線概況

基於回测區間 K 線收盤價的拐點與漲跌統計，用於描述價格走勢：

| 項目 | 數值 |
|------|------|
| 開始價 | {{.PriceCurveStartPrice}} |
| 結束價 | {{.PriceCurveEndPrice}} |
| 始末涨跌幅（持有收益） | {{.PriceCurveStartEndReturn}} |
| 最大連續下跌价差 | {{.PriceCurveMaxDecline}} |
| 最大連續上漲价差 | {{.PriceCurveMaxRise}} |

**策略 vs 持有**：{{.PriceCurveStrategyVsHold}}

**期間最重要的 3 個谷底價（由低到高）**：{{range .PriceCurveValleys}} {{.}}{{end}}

**期間最重要的 3 個峰值價（由高到低）**：{{range .PriceCurvePeaks}} {{.}}{{end}}

{{end}}
## 收益指標

| 指標 | 數值 |
|------|------|
| 總收益率 | {{.TotalReturn}} |
| 年化收益率 | {{.AnnualizedReturn}} |

## 风險指標

| 指標 | 數值 |
|------|------|
| 最大回撤 | {{.MaxDrawdown}} |
| 最大回撤持续時间 | {{.MaxDrawdownDuration}} |
| 波动率（年化） | {{.Volatility}} |

## 风險調整收益

| 指標 | 數值 |
|------|------|
| 夏普比率 | {{.SharpeRatio}} |
| 索提诺比率 | {{.SortinoRatio}} |
| 卡玛比率 | {{.CalmarRatio}} |

## 交易指標

| 指標 | 數值 |
|------|------|
| 總交易次數（成對） | {{.TotalTrades}} |
| 買入次數 | {{.BuyCount}} |
| 賣出次數 | {{.SellCount}} |
| 胜率（网格） | {{.WinRate}} |
| 胜率（交易所） | {{.ExchangeWinRate}} |
| 利润因子 | {{.ProfitFactor}} |
| 平均盈利 | ${{.AvgWin}} |
| 平均亏损 | ${{.AvgLoss}} |
| 最大單笔盈利 | ${{.LargestWin}} |
| 最大單笔亏损 | ${{.LargestLoss}} |
| 最大连续盈利 | {{.MaxConsecutiveWins}} 笔 |
| 最大连续亏损 | {{.MaxConsecutiveLosses}} 笔 |
| 最大持倉（基幣） | {{.MaxPosition}} |
| 期末持倉（基幣） | {{.EndPositionQty}} |
| 期末持倉市值 | {{.EndPositionValue}} |
| 期末持有 USDT | {{.EndCashUSDT}} |
| 🔥 價格偏差（slippage）累計損失 | {{.TotalSlippageLoss}} |

## 交易明细（前20笔）

| 時间 | 類型 | 價格 | 數量 | 盈亏 |
|------|------|------|------|------|
{{range .TopTrades}}| {{.Time}} | {{.Type}} | {{.Price}} | {{.Quantity}} | {{.PnL}} |
{{end}}

{{if .TopPairedTrades}}
## 成對交易（前10笔）

| 買入時間 | 買入價 | 賣出時間 | 賣出價 | 數量 | 盈虧 |
|----------|--------|----------|--------|------|------|
{{range .TopPairedTrades}}| {{.BuyTime}} | {{.BuyPrice}} | {{.SellTime}} | {{.SellPrice}} | {{.Quantity}} | {{.PnL}} |
{{end}}
{{end}}

{{if .TopUnpairedTrades}}
## 未成對交易（前10笔）

| 時间 | 類型 | 價格 | 數量 | 盈亏 |
|------|------|------|------|------|
{{range .TopUnpairedTrades}}| {{.Time}} | {{.Type}} | {{.Price}} | {{.Quantity}} | {{.PnL}} |
{{end}}
{{end}}

## 高级风險指標

| 指標 | 數值 | 說明 |
|------|------|------|
| VaR (95%) | {{.VaR95}} | 95% 置信度下的最大損失 |
| VaR (99%) | {{.VaR99}} | 99% 置信度下的最大損失 |
| CVaR (95%) | {{.CVaR95}} | 超過 VaR 的平均損失 |
| CVaR (99%) | {{.CVaR99}} | 超過 VaR 的平均損失 |

**說明**：
- **VaR (Value at Risk)**: 在给定置信度下，投资组合在未来特定時间内可能遭受的最大損失。
- **CVaR (Conditional Value at Risk)**: 也称為預期損失，是超過 VaR 阈值的平均損失，比 VaR 更能反映极端风險。

## 风控說明

⚠️ **本回测未接入风控**：上述結果為「理想情景」，實盤 QuantMesh 有多套风控會干預交易。

### 實盤风控可如何介入

| 风控組件 | 配置區塊 | 介入方式 |
|----------|----------|----------|
| **RiskMonitor** | risk_control | K 線成交量異常（如暴量）時，暫停新單直至市場恢復 |
| **帳戶安全** | 啟動時 | 餘額不足、槓桿超限時拒絕啟動 |
| **深度監控** | risk_control.depth_monitor | 訂單簿深度過薄時觸發风控，暫停交易 |
| **訂單清理** | trading.order_cleanup_* | 未成交訂單過多時撤銷部分訂單 |
| **對帳** | trading.reconcile_interval | 週期性同步本地與交易所，修正槽位狀態 |

### 影響說明

回测假設每根 K 線均可執行策略信號；實盤若遇 RiskMonitor 或深度監控觸發，對應時段將不下新單，實際成交筆數可能少於回测。建議將回测結果視為策略潛力參考，實盤需關注风控日誌與暫停時段。

## 結論

{{.Conclusion}}

---

*本报告由 QuantMesh 回测系统自动生成*
`

	t, err := template.New("report").Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// SaveEquityCurveCSV 保存權益曲線到 CSV
func SaveEquityCurveCSV(result *BacktestResult) (string, error) {
	reportDir := filepath.Join("backtest", "reports")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return "", fmt.Errorf("創建报告目錄失敗: %w", err)
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("%s_%s_%s_equity.csv",
		result.Strategy,
		result.Symbol,
		timestamp,
	)
	csvPath := filepath.Join(reportDir, filename)

	file, err := os.Create(csvPath)
	if err != nil {
		return "", fmt.Errorf("創建 CSV 檔案失敗: %w", err)
	}
	defer file.Close()

	// 寫入表头
	file.WriteString("timestamp,equity\n")

	// 寫入數據
	for _, point := range result.Equity {
		file.WriteString(fmt.Sprintf("%d,%.2f\n", point.Timestamp, point.Equity))
	}

	return csvPath, nil
}

// ========== 多策略回測報告 ==========

// MultiStrategyReportData 多策略報告數據
type MultiStrategyReportData struct {
	GeneratedAt    string                      `json:"generated_at"`
	Symbol         string                      `json:"symbol"`
	Interval       string                      `json:"interval"`
	StartDate      string                      `json:"start_date"`
	EndDate        string                      `json:"end_date"`
	Duration       string                      `json:"duration"`
	InitialCapital float64                     `json:"initial_capital"`
	FinalEquity    float64                     `json:"final_equity"`
	TotalReturnPct float64                     `json:"total_return_pct"`
	TotalTrades    int                         `json:"total_trades"`
	TotalFees      float64                     `json:"total_fees"`
	TotalFunding   float64                     `json:"total_funding"`
	MaxDrawdownPct float64                     `json:"max_drawdown_pct"`
	SharpeRatio    float64                     `json:"sharpe_ratio"`
	WinRate        float64                     `json:"win_rate"`
	Strategies     []MultiStrategyReportItem   `json:"strategies"`
	ParamsTable    []ReportParamRow            `json:"params_table"`
}

// MultiStrategyReportItem 多策略報告中的單個策略項
type MultiStrategyReportItem struct {
	Name           string  `json:"name"`
	Weight         float64 `json:"weight"`
	TotalTrades    int     `json:"total_trades"`
	RealizedPnl    float64 `json:"realized_pnl"`
	WinRate        float64 `json:"win_rate"`
	MaxDrawdown    float64 `json:"max_drawdown"`
}

// GenerateMultiStrategyReportToFile 生成多策略報告到指定路徑
func GenerateMultiStrategyReportToFile(result *MultiStrategyResult, reportPath string, task *BacktestTask) error {
	// 計算持續時間
	duration := result.EndTime.Sub(result.StartTime)
	durationStr := fmt.Sprintf("%d 天", int(duration.Hours()/24))

	// 構建參數表
	var paramsTable []ReportParamRow
	if task != nil && len(task.Strategies) > 0 {
		for _, s := range task.Strategies {
			paramsTable = append(paramsTable, ReportParamRow{
				Key:   fmt.Sprintf("策略: %s (權重: %.0f%%)", s.Type, s.Weight),
				Value: fmt.Sprintf("配置: %+v", s.Config),
			})
		}
	}

	data := MultiStrategyReportData{
		GeneratedAt:    time.Now().Format("2006-01-02 15:04:05"),
		Symbol:         result.Symbol,
		Interval:       "", // 將在 task_manager 中填充
		StartDate:      result.StartTime.Format("2006-01-02 15:04:05"),
		EndDate:        result.EndTime.Format("2006-01-02 15:04:05"),
		Duration:       durationStr,
		InitialCapital: result.InitialCapital,
		FinalEquity:    result.FinalEquity,
		TotalReturnPct: result.TotalReturnPct,
		TotalTrades:    result.TotalTrades,
		TotalFees:      result.TotalFees,
		TotalFunding:   result.TotalFunding,
		MaxDrawdownPct: result.RiskMetrics.MaxDrawdownPct,
		SharpeRatio:    result.RiskMetrics.SharpeRatio,
		WinRate:        result.RiskMetrics.WinRate,
		ParamsTable:    paramsTable,
	}

	// 構建各策略統計
	if result.StatsByStrategy != nil {
		for name, stats := range result.StatsByStrategy {
			// 從 task.Strategies 中找到對應的權重
			var weight float64
			for _, s := range task.Strategies {
				if s.Type == stats.Type {
					weight = s.Weight
					break
				}
			}
			data.Strategies = append(data.Strategies, MultiStrategyReportItem{
				Name:        name,
				Weight:      weight,
				TotalTrades: stats.TotalTrades,
				RealizedPnl: stats.RealizedPnL,
				WinRate:     stats.WinRate,
				MaxDrawdown: stats.MaxDrawdown,
			})
		}
		// 按權重排序
		sort.Slice(data.Strategies, func(i, j int) bool {
			return data.Strategies[i].Weight > data.Strategies[j].Weight
		})
	}

	content, err := renderMultiStrategyReportTemplate(data)
	if err != nil {
		return fmt.Errorf("渲染多策略報告模板失敗: %w", err)
	}
	dir := filepath.Dir(reportPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("創建报告目錄失敗: %w", err)
	}
	return os.WriteFile(reportPath, []byte(content), 0644)
}

// renderMultiStrategyReportTemplate 渲染多策略報告模板
func renderMultiStrategyReportTemplate(data MultiStrategyReportData) (string, error) {
	tmpl := `# 多策略組合回測報告

生成時间: {{.GeneratedAt}}

## 執行摘要

- **交易對**: {{.Symbol}}
- **回测期间**: {{.StartDate}} 至 {{.EndDate}} ({{.Duration}})
- **初始資金**: ${{.InitialCapital}}
- **最終權益**: ${{.FinalEquity}}
- **總收益率**: {{.TotalReturnPct}}%
- **最大回撤**: {{.MaxDrawdownPct}}%
- **夏普比率**: {{.SharpeRatio}}
- **勝率**: {{.WinRate}}%

## 交易統計

- **總交易次數**: {{.TotalTrades}}
- **總手續費**: ${{.TotalFees}}
- **總資金費**: ${{.TotalFunding}}

{{if .ParamsTable}}
## 策略配置

{{range .ParamsTable}}
- **{{.Key}}**: {{.Value}}
{{end}}
{{end}}

{{if .Strategies}}
## 各策略表現

| 策略 | 權重 | 交易次數 | 已實現盈虧 | 勝率 | 最大回撤 |
|------|------|----------|------------|------|----------|
{{range .Strategies}}
| {{.Name}} | {{.Weight}}% | {{.TotalTrades}} | ${{.RealizedPnl}} | {{.WinRate}}% | {{.MaxDrawdown}}% |
{{end}}
{{end}}

---

*本報告由 QuantMesh 回測系統自動生成*
`

	t, err := template.New("multi_strategy_report").Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// ========== 對沖回測報告 ==========

// HedgeReportData 對沖報告數據
type HedgeReportData struct {
	GeneratedAt     string  `json:"generated_at"`
	StartTime       string  `json:"start_time"`
	EndTime         string  `json:"end_time"`
	InitialCapital  float64 `json:"initial_capital"`
	FinalEquity     float64 `json:"final_equity"`
	TotalReturnPct  float64 `json:"total_return_pct"`
	MaxDrawdownPct  float64 `json:"max_drawdown_pct"`
	RebalanceCount  int     `json:"rebalance_count"`
	AlignedPoints   int     `json:"aligned_points"`
	LongSymbol      string  `json:"long_symbol"`
	ShortSymbol     string  `json:"short_symbol"`
	ParamsTable     []ReportParamRow `json:"params_table"`
}

// GenerateHedgeReportToFile 生成對沖報告到指定路徑
func GenerateHedgeReportToFile(result *HedgePairResult, reportPath string, task *BacktestTask) error {
	// 構建參數表
	var paramsTable []ReportParamRow
	if task != nil {
		hedgeRatio := getFloat(task.Params, "hedge_ratio", 1.0)
		rebalanceThreshold := getFloat(task.Params, "rebalance_threshold", 0.15)
		rebalanceInterval := getInt(task.Params, "rebalance_interval", 24)
		paramsTable = []ReportParamRow{
			{Key: "對沖比例", Value: fmt.Sprintf("%.2f", hedgeRatio)},
			{Key: "再平衡閾值", Value: fmt.Sprintf("%.2f%%", rebalanceThreshold*100)},
			{Key: "再平衡間隔", Value: fmt.Sprintf("%d 小時", rebalanceInterval)},
		}
	}

	data := HedgeReportData{
		GeneratedAt:     time.Now().Format("2006-01-02 15:04:05"),
		StartTime:       result.StartTime.Format("2006-01-02 15:04:05"),
		EndTime:         result.EndTime.Format("2006-01-02 15:04:05"),
		InitialCapital:  result.InitialCapital,
		FinalEquity:     result.FinalEquity,
		TotalReturnPct:  result.TotalReturnPct,
		MaxDrawdownPct:  result.MaxDrawdownPct,
		RebalanceCount:  result.RebalanceCount,
		AlignedPoints:   result.AlignedPoints,
		LongSymbol:      result.LongSymbol,
		ShortSymbol:     result.ShortSymbol,
		ParamsTable:     paramsTable,
	}

	content, err := renderHedgeReportTemplate(data)
	if err != nil {
		return fmt.Errorf("渲染對沖報告模板失敗: %w", err)
	}
	dir := filepath.Dir(reportPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("創建报告目錄失敗: %w", err)
	}
	return os.WriteFile(reportPath, []byte(content), 0644)
}

// renderHedgeReportTemplate 渲染對沖報告模板
func renderHedgeReportTemplate(data HedgeReportData) (string, error) {
	tmpl := `# 對沖組合回測報告

生成時间: {{.GeneratedAt}}

## 執行摘要

- **回測期間**: {{.StartTime}} 至 {{.EndTime}}
- **初始資金**: ${{.InitialCapital}}
- **最終權益**: ${{.FinalEquity}}
- **總收益率**: {{.TotalReturnPct}}%
- **最大回撤**: {{.MaxDrawdownPct}}%

## 對沖配置

- **多腿交易對**: {{.LongSymbol}}
- **空腿交易對**: {{.ShortSymbol}}
- **K線對齊點數**: {{.AlignedPoints}}

## 再平衡統計

- **再平衡次數**: {{.RebalanceCount}}

{{if .ParamsTable}}
## 對沖參數

| 參數 | 值 |
|------|-----|
{{range .ParamsTable}}
| {{.Key}} | {{.Value}} |
{{end}}
{{end}}

---

*本報告由 QuantMesh 回測系統自動生成*
`

	t, err := template.New("hedge_report").Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

