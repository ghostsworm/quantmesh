package backtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"
)

// GenerateReport 生成 Markdown 回测报告
func GenerateReport(result *BacktestResult) (string, error) {
	// 创建报告目录
	reportDir := filepath.Join("backtest", "reports")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return "", fmt.Errorf("创建报告目录失败: %w", err)
	}

	// 生成报告文件名
	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("%s_%s_%s.md",
		result.Strategy,
		result.Symbol,
		timestamp,
	)
	reportPath := filepath.Join(reportDir, filename)

	// 准备模板数据
	data := prepareReportData(result)

	// 渲染模板
	content, err := renderReportTemplate(data)
	if err != nil {
		return "", fmt.Errorf("渲染报告模板失败: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(reportPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("写入报告文件失败: %w", err)
	}

	return reportPath, nil
}

// ReportData 报告数据
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

	// 收益指标
	TotalReturn      string
	AnnualizedReturn string

	// 风险指标
	MaxDrawdown         string
	MaxDrawdownDuration string
	Volatility          string

	// 风险调整收益
	SharpeRatio  string
	SortinoRatio string
	CalmarRatio  string

	// 交易指标
	TotalTrades          string
	WinRate              string
	ProfitFactor         string
	AvgWin               string
	AvgLoss              string
	LargestWin           string
	LargestLoss          string
	MaxConsecutiveWins   string
	MaxConsecutiveLosses string

	// 交易明细
	TopTrades []TradeRow

	// 风险指标
	VaR95  string
	VaR99  string
	CVaR95 string
	CVaR99 string

	// 结论
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

// prepareReportData 准备报告数据
func prepareReportData(result *BacktestResult) ReportData {
	m := result.Metrics

	// 计算持续时间
	duration := result.EndTime.Sub(result.StartTime)
	durationStr := fmt.Sprintf("%d 天", int(duration.Hours()/24))

	// 准备交易明细（前20笔）
	topTrades := make([]TradeRow, 0)
	count := 0
	for _, trade := range result.Trades {
		if trade.Type == "sell" && count < 20 {
			tradeTime := time.Unix(trade.Timestamp/1000, 0)
			topTrades = append(topTrades, TradeRow{
				Time:     tradeTime.Format("2006-01-02 15:04"),
				Type:     trade.Type,
				Price:    fmt.Sprintf("%.2f", trade.Price),
				Quantity: fmt.Sprintf("%.4f", trade.Quantity),
				PnL:      fmt.Sprintf("%.2f", trade.PnL),
			})
			count++
		}
	}

	// 生成结论
	conclusion := generateConclusion(result)

	return ReportData{
		PluginName:     result.Strategy,
		Symbol:         result.Symbol,
		GeneratedAt:    time.Now().Format("2006-01-02 15:04:05"),
		StartDate:      result.StartTime.Format("2006-01-02"),
		EndDate:        result.EndTime.Format("2006-01-02"),
		Duration:       durationStr,
		InitialCapital: fmt.Sprintf("%.2f", result.InitialCapital),
		FinalCapital:   fmt.Sprintf("%.2f", result.FinalCapital),

		TotalReturn:      fmt.Sprintf("%.2f%%", m.TotalReturn),
		AnnualizedReturn: fmt.Sprintf("%.2f%%", m.AnnualizedReturn),

		MaxDrawdown:         fmt.Sprintf("%.2f%%", m.MaxDrawdown),
		MaxDrawdownDuration: fmt.Sprintf("%d 天", m.MaxDrawdownDuration),
		Volatility:          fmt.Sprintf("%.2f%%", m.Volatility),

		SharpeRatio:  fmt.Sprintf("%.2f", m.SharpeRatio),
		SortinoRatio: fmt.Sprintf("%.2f", m.SortinoRatio),
		CalmarRatio:  fmt.Sprintf("%.2f", m.CalmarRatio),

		TotalTrades:          fmt.Sprintf("%d", m.TotalTrades),
		WinRate:              fmt.Sprintf("%.2f%%", m.WinRate),
		ProfitFactor:         fmt.Sprintf("%.2f", m.ProfitFactor),
		AvgWin:               fmt.Sprintf("%.2f", m.AvgWin),
		AvgLoss:              fmt.Sprintf("%.2f", m.AvgLoss),
		LargestWin:           fmt.Sprintf("%.2f", m.LargestWin),
		LargestLoss:          fmt.Sprintf("%.2f", m.LargestLoss),
		MaxConsecutiveWins:   fmt.Sprintf("%d", m.MaxConsecutiveWins),
		MaxConsecutiveLosses: fmt.Sprintf("%d", m.MaxConsecutiveLosses),

		TopTrades: topTrades,

		VaR95:  fmt.Sprintf("%.2f%%", result.RiskMetrics.VaR95),
		VaR99:  fmt.Sprintf("%.2f%%", result.RiskMetrics.VaR99),
		CVaR95: fmt.Sprintf("%.2f%%", result.RiskMetrics.CVaR95),
		CVaR99: fmt.Sprintf("%.2f%%", result.RiskMetrics.CVaR99),

		Conclusion: conclusion,
	}
}

// generateConclusion 生成结论
func generateConclusion(result *BacktestResult) string {
	m := result.Metrics
	var conclusions []string

	// 收益评估
	if m.TotalReturn > 50 {
		conclusions = append(conclusions, "✅ 策略表现优秀，总收益率超过 50%")
	} else if m.TotalReturn > 20 {
		conclusions = append(conclusions, "✅ 策略表现良好，总收益率超过 20%")
	} else if m.TotalReturn > 0 {
		conclusions = append(conclusions, "⚠️ 策略盈利，但收益率较低")
	} else {
		conclusions = append(conclusions, "❌ 策略亏损，需要优化参数或更换策略")
	}

	// 风险评估
	if m.MaxDrawdown < 10 {
		conclusions = append(conclusions, "✅ 风险控制良好，最大回撤小于 10%")
	} else if m.MaxDrawdown < 20 {
		conclusions = append(conclusions, "⚠️ 风险适中，最大回撤在 10-20% 之间")
	} else {
		conclusions = append(conclusions, "❌ 风险较高，最大回撤超过 20%")
	}

	// 夏普比率评估
	if m.SharpeRatio > 2 {
		conclusions = append(conclusions, "✅ 风险调整收益优秀，夏普比率 > 2")
	} else if m.SharpeRatio > 1 {
		conclusions = append(conclusions, "✅ 风险调整收益良好，夏普比率 > 1")
	} else if m.SharpeRatio > 0 {
		conclusions = append(conclusions, "⚠️ 风险调整收益一般，夏普比率 < 1")
	} else {
		conclusions = append(conclusions, "❌ 风险调整收益差，夏普比率为负")
	}

	// 胜率评估
	if m.WinRate > 60 {
		conclusions = append(conclusions, "✅ 胜率高，超过 60%")
	} else if m.WinRate > 50 {
		conclusions = append(conclusions, "✅ 胜率良好，超过 50%")
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
		conclusions = append(conclusions, "❌ 利润因子 < 1，平均亏损大于平均盈利")
	}

	return strings.Join(conclusions, "\n\n")
}

// renderReportTemplate 渲染报告模板
func renderReportTemplate(data ReportData) (string, error) {
	tmpl := `# {{.PluginName}} 策略回测报告

生成时间: {{.GeneratedAt}}

## 执行摘要

- **交易对**: {{.Symbol}}
- **回测期间**: {{.StartDate}} 至 {{.EndDate}} ({{.Duration}})
- **初始资金**: ${{.InitialCapital}}
- **最终资金**: ${{.FinalCapital}}
- **总收益率**: {{.TotalReturn}}
- **年化收益率**: {{.AnnualizedReturn}}
- **最大回撤**: {{.MaxDrawdown}}
- **夏普比率**: {{.SharpeRatio}}

## 收益指标

| 指标 | 数值 |
|------|------|
| 总收益率 | {{.TotalReturn}} |
| 年化收益率 | {{.AnnualizedReturn}} |

## 风险指标

| 指标 | 数值 |
|------|------|
| 最大回撤 | {{.MaxDrawdown}} |
| 最大回撤持续时间 | {{.MaxDrawdownDuration}} |
| 波动率（年化） | {{.Volatility}} |

## 风险调整收益

| 指标 | 数值 |
|------|------|
| 夏普比率 | {{.SharpeRatio}} |
| 索提诺比率 | {{.SortinoRatio}} |
| 卡玛比率 | {{.CalmarRatio}} |

## 交易指标

| 指标 | 数值 |
|------|------|
| 总交易次数 | {{.TotalTrades}} |
| 胜率 | {{.WinRate}} |
| 利润因子 | {{.ProfitFactor}} |
| 平均盈利 | ${{.AvgWin}} |
| 平均亏损 | ${{.AvgLoss}} |
| 最大单笔盈利 | ${{.LargestWin}} |
| 最大单笔亏损 | ${{.LargestLoss}} |
| 最大连续盈利 | {{.MaxConsecutiveWins}} 笔 |
| 最大连续亏损 | {{.MaxConsecutiveLosses}} 笔 |

## 交易明细（前20笔）

| 时间 | 类型 | 价格 | 数量 | 盈亏 |
|------|------|------|------|------|
{{range .TopTrades}}| {{.Time}} | {{.Type}} | {{.Price}} | {{.Quantity}} | {{.PnL}} |
{{end}}

## 高级风险指标

| 指标 | 数值 | 说明 |
|------|------|------|
| VaR (95%) | {{.VaR95}} | 95% 置信度下的最大损失 |
| VaR (99%) | {{.VaR99}} | 99% 置信度下的最大损失 |
| CVaR (95%) | {{.CVaR95}} | 超过 VaR 的平均损失 |
| CVaR (99%) | {{.CVaR99}} | 超过 VaR 的平均损失 |

**说明**：
- **VaR (Value at Risk)**: 在给定置信度下，投资组合在未来特定时间内可能遭受的最大损失。
- **CVaR (Conditional Value at Risk)**: 也称为预期损失，是超过 VaR 阈值的平均损失，比 VaR 更能反映极端风险。

## 结论

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

// SaveEquityCurveCSV 保存权益曲线到 CSV
func SaveEquityCurveCSV(result *BacktestResult) (string, error) {
	reportDir := filepath.Join("backtest", "reports")
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return "", fmt.Errorf("创建报告目录失败: %w", err)
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
		return "", fmt.Errorf("创建 CSV 文件失败: %w", err)
	}
	defer file.Close()

	// 写入表头
	file.WriteString("timestamp,equity\n")

	// 写入数据
	for _, point := range result.Equity {
		file.WriteString(fmt.Sprintf("%d,%.2f\n", point.Timestamp, point.Equity))
	}

	return csvPath, nil
}
