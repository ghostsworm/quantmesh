package inspector

import (
	"fmt"
	"strings"
	"time"
)

// ReportConfig 報告配置
type ReportConfig struct {
	Name                 string
	Format               string // "markdown"
	IncludeAIInsights     bool
	IncludeTechnicalAnalysis bool
	MaxNewsItems          int
}

// DefaultReportConfig 預設報告配置
func DefaultReportConfig() ReportConfig {
	return ReportConfig{
		Name:                 "智子巡檢",
		Format:               "markdown",
		IncludeAIInsights:    true,
		MaxNewsItems:         5,
	}
}

// ReportGenerator 報告生成器
type ReportGenerator struct {
	Config ReportConfig
}

// GenerateScheduled 生成定時彙總報告（完整狀態）
func (r *ReportGenerator) GenerateScheduled(snap *InspectionSnapshot, analysis *InspectionAnalysis) *InspectorReport {
	body := r.buildScheduledBody(snap, analysis)
	return &InspectorReport{
		ReportType:   "scheduled",
		Title:        r.Config.Name + "報告 | " + time.Now().Format("2006-01-02 15:04"),
		Body:         body,
		Snapshot:     snap,
		Analysis:     analysis,
		GeneratedAt:  time.Now(),
	}
}

// GenerateUrgent 生成緊急事件報告
func (r *ReportGenerator) GenerateUrgent(payload InspectorEventPayload) *InspectorReport {
	body := r.buildUrgentBody(payload)
	return &InspectorReport{
		ReportType:   "urgent",
		Title:        r.Config.Name + " | " + payload.Title,
		Body:         body,
		Snapshot:     payload.Snapshot,
		EventType:    payload.EventType,
		EventData:    payload.Data,
		GeneratedAt:  payload.At,
	}
}

func (r *ReportGenerator) buildScheduledBody(snap *InspectionSnapshot, analysis *InspectionAnalysis) string {
	var b strings.Builder
	t := time.Now()

	b.WriteString(fmt.Sprintf("📊 %s報告 | %s\n\n", r.Config.Name, t.Format("2006-01-02 15:04")))

	// 資金概覽
	b.WriteString("💰 資金概覽\n")
	b.WriteString(fmt.Sprintf("- 總資產: $%.2f %s\n", snap.AccountSummary.TotalBalance, snap.AccountSummary.Currency))
	b.WriteString(fmt.Sprintf("- 未實現盈虧: $%.2f\n", snap.AccountSummary.UnrealizedPnL))
	b.WriteString(fmt.Sprintf("- 今日已實現: $%.2f | 本週: $%.2f | 本月: $%.2f\n\n",
		snap.PnLSummary.TodayRealized, snap.PnLSummary.WeekRealized, snap.PnLSummary.MonthRealized))

	// 持倉狀態
	b.WriteString("📈 持倉狀態\n")
	for _, p := range snap.Positions {
		b.WriteString(fmt.Sprintf("- %s (%s): 現價 $%.2f | 未實現 PnL: $%.2f | 持倉價值: $%.2f\n",
			p.Symbol, p.Exchange, p.CurrentPrice, p.UnrealizedPnL, p.PositionValue))
	}
	b.WriteString("\n")

	// 風險狀態
	b.WriteString("⚠️ 風險狀態: ")
	if snap.RiskStatus.Triggered {
		b.WriteString("已觸發 - " + snap.RiskStatus.Reason + "\n")
	} else {
		b.WriteString("正常\n")
	}

	// 新聞風險
	b.WriteString("📰 新聞風險: ")
	var riskParts []string
	for asset, a := range snap.NewsRisk {
		if a != nil {
			riskParts = append(riskParts, fmt.Sprintf("%s %.0f/100", asset, a.OverallRiskScore))
		}
	}
	if len(riskParts) > 0 {
		b.WriteString(strings.Join(riskParts, " | ") + "\n")
	} else {
		b.WriteString("無數據\n")
	}
	b.WriteString("\n")

	// 黃金專項（若有）
	if snap.GoldAnalysis != nil {
		b.WriteString("🟡 黃金專項\n")
		b.WriteString(fmt.Sprintf("- 現價: $%.2f，24h 漲跌: %.2f%%\n", snap.GoldAnalysis.CurrentPrice, snap.GoldAnalysis.Change24hPct))
		b.WriteString(fmt.Sprintf("- 與 BTC 相關性: %.2f，避險情緒指數: %.0f/100\n\n",
			snap.GoldAnalysis.CorrelationWithBTC, snap.GoldAnalysis.SafeHavenIndex))
	}

	// 需要關注 / AI 建議
	if analysis != nil && r.Config.IncludeAIInsights {
		if len(analysis.AttentionCoins) > 0 {
			b.WriteString("🔔 需要關注: " + strings.Join(analysis.AttentionCoins, ", ") + "\n")
		}
		if analysis.Summary != "" {
			b.WriteString("💡 AI 建議: " + analysis.Summary + "\n")
		}
		if len(analysis.KeyFindings) > 0 {
			b.WriteString("\n重要發現:\n")
			for i, f := range analysis.KeyFindings {
				if i >= 3 {
					break
				}
				b.WriteString(fmt.Sprintf("- %s: %s\n", f.Title, f.Description))
			}
		}
		if len(analysis.Recommendations) > 0 {
			b.WriteString("\n操作建議:\n")
			for i, rec := range analysis.Recommendations {
				if i >= 3 {
					break
				}
				b.WriteString(fmt.Sprintf("- %s（%s）\n", rec.Action, rec.Reason))
			}
		}
		if analysis.GoldInsights != nil && analysis.GoldInsights.ActionHint != "" {
			b.WriteString("\n黃金: " + analysis.GoldInsights.ActionHint + "\n")
		}
	}

	return b.String()
}

func (r *ReportGenerator) buildUrgentBody(payload InspectorEventPayload) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("🚨 %s | %s\n\n", r.Config.Name, payload.Title))
	b.WriteString(payload.Message + "\n\n")
	b.WriteString("時間: " + payload.At.Format("2006-01-02 15:04:05") + "\n")
	if payload.Snapshot != nil {
		b.WriteString(fmt.Sprintf("\n當前總資產: $%.2f，未實現盈虧: $%.2f\n",
			payload.Snapshot.AccountSummary.TotalBalance, payload.Snapshot.AccountSummary.UnrealizedPnL))
	}
	return b.String()
}
