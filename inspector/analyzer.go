package inspector

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"quantmesh/logger"
)

// GeminiContentGenerator 用於調用 Gemini 生成結構化內容（由 main 注入 ai.GeminiClient）
type GeminiContentGenerator interface {
	GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}) (string, error)
}

// Analyzer 智子巡檢 AI 分析引擎
type Analyzer struct {
	Client GeminiContentGenerator
}

// Analyze 根據快照生成 AI 分析
func (a *Analyzer) Analyze(ctx context.Context, snap *InspectionSnapshot) (*InspectionAnalysis, error) {
	if a.Client == nil {
		return a.fallbackAnalysis(snap), nil
	}
	if snap == nil {
		return &InspectionAnalysis{RiskLevel: "overall", GeneratedAt: time.Now()}, nil
	}

	prompt := a.buildPrompt(snap)
	schema := a.buildSchema()
	text, err := a.Client.GenerateContent(ctx, prompt, schema)
	if err != nil {
		logger.Warn("智子巡檢 AI 分析失敗: %v", err)
		return a.fallbackAnalysis(snap), nil
	}

	out, err := a.parseResponse(text)
	if err != nil {
		logger.Warn("智子巡檢 解析 AI 響應失敗: %v (原始: %s)", err, truncateStr(text, 300))
		return a.fallbackAnalysis(snap), nil
	}
	out.GeneratedAt = time.Now()
	return out, nil
}

func (a *Analyzer) fallbackAnalysis(snap *InspectionSnapshot) *InspectionAnalysis {
	out := &InspectionAnalysis{
		Summary:     "未啟用 AI 分析或分析失敗，請查看下方數據概覽。",
		RiskLevel:   "overall",
		GeneratedAt: time.Now(),
	}
	if snap != nil {
		if snap.RiskStatus.Triggered {
			out.RiskLevel = "elevated"
			out.KeyFindings = append(out.KeyFindings, Finding{
				Title:       "風控已觸發",
				Description: snap.RiskStatus.Reason,
				Priority:    1,
				Category:    "risk",
			})
		}
		var totalUnrealized float64
		for _, p := range snap.Positions {
			totalUnrealized += p.UnrealizedPnL
		}
		out.Summary = fmt.Sprintf("持倉 %d 個交易對，未實現盈虧 %.2f USDT，今日已實現 %.2f USDT。",
			len(snap.Positions), totalUnrealized, snap.PnLSummary.TodayRealized)
	}
	return out
}

func (a *Analyzer) buildPrompt(snap *InspectionSnapshot) string {
	var b strings.Builder
	b.WriteString("你是一個量化交易系統的智能巡檢助手（智子巡檢）。請根據以下當前狀態數據，給出一份簡潔的巡檢分析報告。\n\n")

	b.WriteString("## 當前狀態\n")
	b.WriteString(fmt.Sprintf("- 時間: %s\n", snap.Timestamp.Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("- 總權益/餘額: %.2f USDT，未實現盈虧: %.2f USDT\n",
		snap.AccountSummary.TotalBalance, snap.AccountSummary.UnrealizedPnL))
	b.WriteString(fmt.Sprintf("- 今日已實現盈虧: %.2f USDT，本週: %.2f，本月: %.2f\n",
		snap.PnLSummary.TodayRealized, snap.PnLSummary.WeekRealized, snap.PnLSummary.MonthRealized))
	b.WriteString(fmt.Sprintf("- 風控狀態: %s\n", formatRiskTriggered(snap.RiskStatus.Triggered, snap.RiskStatus.Reason)))

	b.WriteString("\n## 持倉概覽\n")
	for _, p := range snap.Positions {
		b.WriteString(fmt.Sprintf("- %s (%s): 當前價 %.2f，未實現盈虧 %.2f USDT，持倉價值 %.2f\n",
			p.Symbol, p.Exchange, p.CurrentPrice, p.UnrealizedPnL, p.PositionValue))
	}

	b.WriteString("\n## 新聞風險\n")
	for asset, r := range snap.NewsRisk {
		if r != nil {
			b.WriteString(fmt.Sprintf("- %s: 綜合風險評分 %.0f/100，建議: %s\n", asset, r.OverallRiskScore, r.Recommendation))
		}
	}

	if snap.GoldAnalysis != nil {
		b.WriteString("\n## 黃金專項\n")
		b.WriteString(fmt.Sprintf("- 當前價: %.2f，24h 漲跌: %.2f%%\n", snap.GoldAnalysis.CurrentPrice, snap.GoldAnalysis.Change24hPct))
		b.WriteString(fmt.Sprintf("- 與 BTC 相關性: %.2f，避險情緒指數: %.0f/100\n",
			snap.GoldAnalysis.CorrelationWithBTC, snap.GoldAnalysis.SafeHavenIndex))
	}

	b.WriteString("\n請以 JSON 格式回覆，包含: summary（一句話總結）, key_findings（重要發現陣列，每項含 title, description, priority, category）, recommendations（操作建議陣列，每項含 action, reason, priority）, risk_level（overall/elevated/critical）, gold_insights（可選，含 summary, correlation_note, safe_haven_note, action_hint）, attention_coins（需要關注的幣種字串陣列）。")
	return b.String()
}

func formatRiskTriggered(triggered bool, msg string) string {
	if triggered {
		return "已觸發 - " + msg
	}
	return "正常"
}

func (a *Analyzer) buildSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"summary":         map[string]interface{}{"type": "string"},
			"key_findings":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"title": map[string]interface{}{"type": "string"}, "description": map[string]interface{}{"type": "string"}, "priority": map[string]interface{}{"type": "integer"}, "category": map[string]interface{}{"type": "string"}}}},
			"recommendations": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"action": map[string]interface{}{"type": "string"}, "reason": map[string]interface{}{"type": "string"}, "priority": map[string]interface{}{"type": "integer"}}}},
			"risk_level":      map[string]interface{}{"type": "string"},
			"gold_insights":   map[string]interface{}{"type": "object", "properties": map[string]interface{}{"summary": map[string]interface{}{"type": "string"}, "correlation_note": map[string]interface{}{"type": "string"}, "safe_haven_note": map[string]interface{}{"type": "string"}, "action_hint": map[string]interface{}{"type": "string"}}},
			"attention_coins": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
		"required": []string{"summary", "risk_level"},
	}
}

func (a *Analyzer) parseResponse(text string) (*InspectionAnalysis, error) {
	text = extractJSON(text)
	var raw struct {
		Summary         string   `json:"summary"`
		KeyFindings     []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    int    `json:"priority"`
			Category    string `json:"category"`
		} `json:"key_findings"`
		Recommendations []struct {
			Action   string `json:"action"`
			Reason   string `json:"reason"`
			Priority int    `json:"priority"`
		} `json:"recommendations"`
		RiskLevel     string `json:"risk_level"`
		GoldInsights  *struct {
			Summary         string `json:"summary"`
			CorrelationNote  string `json:"correlation_note"`
			SafeHavenNote    string `json:"safe_haven_note"`
			ActionHint      string `json:"action_hint"`
		} `json:"gold_insights"`
		AttentionCoins []string `json:"attention_coins"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, err
	}
	out := &InspectionAnalysis{
		Summary:        raw.Summary,
		RiskLevel:      raw.RiskLevel,
		AttentionCoins: raw.AttentionCoins,
	}
	for _, f := range raw.KeyFindings {
		out.KeyFindings = append(out.KeyFindings, Finding{
			Title:       f.Title,
			Description: f.Description,
			Priority:    f.Priority,
			Category:    f.Category,
		})
	}
	for _, r := range raw.Recommendations {
		out.Recommendations = append(out.Recommendations, Recommendation{
			Action:   r.Action,
			Reason:   r.Reason,
			Priority: r.Priority,
		})
	}
	if raw.GoldInsights != nil {
		out.GoldInsights = &GoldInsights{
			Summary:        raw.GoldInsights.Summary,
			CorrelationNote: raw.GoldInsights.CorrelationNote,
			SafeHavenNote:   raw.GoldInsights.SafeHavenNote,
			ActionHint:     raw.GoldInsights.ActionHint,
		}
	}
	return out, nil
}

func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "```json"); idx >= 0 {
		s = s[idx+7:]
	} else if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
