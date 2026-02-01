package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"quantmesh/ai"
	"quantmesh/config"
	"quantmesh/logger"
)

// GeminiNewsAnalyzer Gemini 新聞分析器：整合历史新闻 + 實時搜索，输出價格概率預测
type GeminiNewsAnalyzer struct {
	cfg           *config.Config
	newsCollector *NewsCollector
	analyzing     atomic.Bool
	lastResult    *NewsRiskAssessment
	lastAnalysis  time.Time
}

// NewGeminiNewsAnalyzer 創建 Gemini 新聞分析器
func NewGeminiNewsAnalyzer(cfg *config.Config, newsCollector *NewsCollector) *GeminiNewsAnalyzer {
	return &GeminiNewsAnalyzer{
		cfg:           cfg,
		newsCollector: newsCollector,
	}
}

// AssetType 资產類型常量
const (
	AssetTypeCryptoBTC   = "crypto_btc"
	AssetTypeCommodityGold = "commodity_gold"
)

// Analyze 執行新聞分析（預設 crypto_btc）
func (g *GeminiNewsAnalyzer) Analyze(ctx context.Context, symbol string, currentPrice float64) (*NewsRiskAssessment, error) {
	return g.AnalyzeAsset(ctx, AssetTypeCryptoBTC, symbol, currentPrice, "")
}

// AnalyzeWithFocus 執行新聞分析，可指定焦点事件（預設 crypto_btc）
func (g *GeminiNewsAnalyzer) AnalyzeWithFocus(ctx context.Context, symbol string, currentPrice float64, focusEvent string) (*NewsRiskAssessment, error) {
	return g.AnalyzeAsset(ctx, AssetTypeCryptoBTC, symbol, currentPrice, focusEvent)
}

// AnalyzeAsset 按资產類型執行新聞分析
func (g *GeminiNewsAnalyzer) AnalyzeAsset(ctx context.Context, assetType, symbol string, currentPrice float64, focusEvent string) (*NewsRiskAssessment, error) {
	if !g.analyzing.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("已有分析任務在執行中")
	}
	defer g.analyzing.Store(false)

	apiKey := g.cfg.AI.GeminiAPIKey
	if apiKey == "" {
		apiKey = g.cfg.AI.APIKey
	}
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API Key 未配置")
	}

	if !g.cfg.NewsMonitor.UseGeminiSearch {
		// 降级：使用规则引擎（由 NewsMonitor 处理）
		return nil, fmt.Errorf("Gemini 搜索未啟用，请使用 NewsMonitor 的规则引擎")
	}

	prompt := g.buildPrompt(assetType, symbol, currentPrice, focusEvent)
	schema := g.buildOutputSchema()

	client := ai.NewGeminiClient(apiKey)
	aiText, err := client.GenerateContentWithGoogleSearch(ctx, prompt, schema)
	if err != nil {
		logger.Warn("📰 Gemini 新聞分析失败: %v", err)
		return nil, err
	}

	assessment, err := g.parseResponse(aiText, assetType, symbol, currentPrice)
	if err != nil {
		logger.Warn("📰 解析 Gemini 响应失败: %v (原始: %s)", err, truncate(aiText, 200))
		return nil, err
	}

	g.lastResult = assessment
	g.lastAnalysis = time.Now()
	logger.Info("📰 Gemini 新聞分析完成: 建议=%s, 2h跌5%%概率=%.0f%%",
		assessment.Recommendation, g.getProb2hDown5(assessment)*100)
	return assessment, nil
}

// IsAnalyzing 是否正在分析
func (g *GeminiNewsAnalyzer) IsAnalyzing() bool {
	return g.analyzing.Load()
}

// GetLastResult 獲取最近一次分析結果
func (g *GeminiNewsAnalyzer) GetLastResult() *NewsRiskAssessment {
	if g.lastResult == nil {
		return nil
	}
	// 返回副本
	result := *g.lastResult
	return &result
}

// GetLastAnalysisTime 獲取最近一次分析時间
func (g *GeminiNewsAnalyzer) GetLastAnalysisTime() time.Time {
	return g.lastAnalysis
}

func (g *GeminiNewsAnalyzer) getProb2hDown5(a *NewsRiskAssessment) float64 {
	for _, pred := range a.PricePredictions {
		if pred.Timeframe != "2h" {
			continue
		}
		for _, s := range pred.Scenarios {
			if s.Direction == "down" && s.ChangePercent <= -5 {
				return s.Probability
			}
		}
	}
	return 0
}

func (g *GeminiNewsAnalyzer) buildPrompt(assetType, symbol string, currentPrice float64, focusEvent string) string {
	recentNews := ""
	if g.newsCollector != nil {
		recentNews = g.newsCollector.GetNewsSummaryText(assetType)
	}
	if recentNews == "" {
		recentNews = "（暂無最近2小時内的新闻）"
	}

	timeframes := g.cfg.NewsMonitor.PredictionTimeframes
	if len(timeframes) == 0 {
		timeframes = []string{"2h", "4h", "6h", "12h", "24h"}
	}
	tfList := strings.Join(timeframes, "、")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("當前市場信息：\n- 交易對: %s\n- 當前價格: $%.2f\n\n", symbol, currentPrice))

	if assetType == AssetTypeCommodityGold {
		sb.WriteString(`你是一位专业的黃金市场分析师。请基於實時搜索的最新新闻和历史新闻，分析黃金價格走势。

`)
		sb.WriteString(`请重点关注：
1. 美聯儲货币政策和利率預期
2. 美元指數（DXY）走势
3. 通脹數據（CPI、PCE）
4. 地緣政治事件（戰爭、制裁、避險情绪）
5. 央行黃金儲备变化
6. 市場波動率和避險需求

`)
	} else {
		sb.WriteString(`你是一位专业的加密貨幣市场分析师。请基於實時搜索的最新新闻和历史新闻，分析比特币價格走势。

请主动搜索：地緣政治、監管政策、總體經濟、交易所安全事件、市场异常等。

`)
	}

	if focusEvent != "" {
		sb.WriteString(fmt.Sprintf(`## 焦点事件
%s
请特别关注並搜索此事件的最新進展、影响範圍、市场反应。

`, focusEvent))
	}

	sb.WriteString(`## 最近2小時内的历史新闻（按時间顺序）
`)
	sb.WriteString(recentNews)
	sb.WriteString(`

## 请使用 Google 搜索獲取最新新闻
注意：你需要主动使用 Google Search 搜索最新新闻，不要只依赖上述历史新闻。

## 判断规则
- 地緣政治重大事件 → 高概率下跌
- 監管/政策负面消息 → 中高概率下跌
- 總體經濟负面消息 → 中概率下跌
- 多個负面因素叠加 → 概率叠加

## 输出要求
请输出以下時间窗口的價格变动概率預测：` + tfList + `

每個時间窗口需包含：下跌5%%、下跌10%%、持平等场景及其概率。

同時提供：當前價位分析、未来可能的目標價位区间、主要风險因素、建议操作（normal/caution/reduce_position/stop_trading）、分析摘要。

请严格按照 JSON Schema 格式输出。`)
	return sb.String()
}

func (g *GeminiNewsAnalyzer) buildOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"current_price_analysis": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"current_price":      map[string]interface{}{"type": "number"},
					"price_trend":        map[string]interface{}{"type": "string"},
					"support_level":      map[string]interface{}{"type": "number"},
					"resistance_level":   map[string]interface{}{"type": "number"},
					"change_24h_percent": map[string]interface{}{"type": "number"},
				},
			},
			"price_predictions": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"timeframe": map[string]interface{}{"type": "string"},
						"scenarios": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"direction":      map[string]interface{}{"type": "string"},
									"change_percent": map[string]interface{}{"type": "number"},
									"probability":    map[string]interface{}{"type": "number"},
								},
							},
						},
						"target_price_range": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"min": map[string]interface{}{"type": "number"},
								"max": map[string]interface{}{"type": "number"},
							},
						},
					},
				},
			},
			"risk_factors": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"factor":   map[string]interface{}{"type": "string"},
						"severity": map[string]interface{}{"type": "string"},
						"impact":   map[string]interface{}{"type": "string"},
					},
				},
			},
			"recommendation":    map[string]interface{}{"type": "string"},
			"analysis_summary":  map[string]interface{}{"type": "string"},
		},
		"required": []interface{}{"current_price_analysis", "price_predictions", "risk_factors", "recommendation"},
	}
}

func (g *GeminiNewsAnalyzer) parseResponse(aiText, assetType, symbol string, currentPrice float64) (*NewsRiskAssessment, error) {
	aiText = strings.TrimSpace(aiText)
	aiText = strings.TrimPrefix(aiText, "```json")
	aiText = strings.TrimPrefix(aiText, "```")
	aiText = strings.TrimSuffix(aiText, "```")
	aiText = strings.TrimSpace(aiText)

	var raw struct {
		CurrentPriceAnalysis *struct {
			CurrentPrice     float64 `json:"current_price"`
			PriceTrend       string  `json:"price_trend"`
			SupportLevel     float64 `json:"support_level"`
			ResistanceLevel  float64 `json:"resistance_level"`
			Change24hPercent float64 `json:"change_24h_percent"`
		} `json:"current_price_analysis"`
		PricePredictions []struct {
			Timeframe        string `json:"timeframe"`
			Scenarios        []struct {
				Direction     string  `json:"direction"`
				ChangePercent float64 `json:"change_percent"`
				Probability   float64 `json:"probability"`
			} `json:"scenarios"`
			TargetPriceRange struct {
				Min float64 `json:"min"`
				Max float64 `json:"max"`
			} `json:"target_price_range"`
		} `json:"price_predictions"`
		RiskFactors []struct {
			Factor   string `json:"factor"`
			Severity string `json:"severity"`
			Impact   string `json:"impact"`
		} `json:"risk_factors"`
		Recommendation   string `json:"recommendation"`
		AnalysisSummary  string `json:"analysis_summary"`
	}

	if err := json.Unmarshal([]byte(aiText), &raw); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}

	assessment := &NewsRiskAssessment{
		AssetType:       assetType,
		AnalysisID:      fmt.Sprintf("nm-%d", time.Now().UnixMilli()),
		LastUpdated:     time.Now(),
		Recommendation:  normalizeRecommendation(raw.Recommendation),
		AnalysisSummary: raw.AnalysisSummary,
	}

	if raw.CurrentPriceAnalysis != nil {
		assessment.CurrentPriceAnalysis = CurrentPriceAnalysis{
			CurrentPrice:     raw.CurrentPriceAnalysis.CurrentPrice,
			PriceTrend:       raw.CurrentPriceAnalysis.PriceTrend,
			SupportLevel:     raw.CurrentPriceAnalysis.SupportLevel,
			ResistanceLevel:  raw.CurrentPriceAnalysis.ResistanceLevel,
			Change24hPercent: raw.CurrentPriceAnalysis.Change24hPercent,
		}
		if assessment.CurrentPriceAnalysis.CurrentPrice == 0 {
			assessment.CurrentPriceAnalysis.CurrentPrice = currentPrice
		}
	} else {
		assessment.CurrentPriceAnalysis = CurrentPriceAnalysis{CurrentPrice: currentPrice}
	}

	for _, p := range raw.PricePredictions {
		pred := PricePrediction{
			Timeframe:        p.Timeframe,
			TargetPriceRange: PriceRange{Min: p.TargetPriceRange.Min, Max: p.TargetPriceRange.Max},
		}
		for _, s := range p.Scenarios {
			pred.Scenarios = append(pred.Scenarios, PriceScenario{
				Direction:     s.Direction,
				ChangePercent: s.ChangePercent,
				Probability:   s.Probability,
			})
		}
		assessment.PricePredictions = append(assessment.PricePredictions, pred)
	}

	for _, r := range raw.RiskFactors {
		assessment.RiskFactorDetails = append(assessment.RiskFactorDetails, RiskFactor{
			Factor:   r.Factor,
			Severity: r.Severity,
			Impact:   r.Impact,
		})
		assessment.RiskFactors = append(assessment.RiskFactors, r.Factor)
	}

	// 计算大跌概率（取 2h 内跌 5% 的概率，若無则取最高下跌概率）
	assessment.CrashProbability = g.inferCrashProbability(assessment)
	assessment.OverallRiskScore = assessment.CrashProbability * 100
	if assessment.OverallRiskScore > 100 {
		assessment.OverallRiskScore = 100
	}

	return assessment, nil
}

func normalizeRecommendation(r string) string {
	r = strings.ToLower(strings.TrimSpace(r))
	switch {
	case strings.Contains(r, "stop") || r == "stop_trading":
		return "stop_trading"
	case strings.Contains(r, "reduce") || r == "reduce_position":
		return "reduce_position"
	case strings.Contains(r, "caution"):
		return "caution"
	default:
		return "normal"
	}
}

func (g *GeminiNewsAnalyzer) inferCrashProbability(a *NewsRiskAssessment) float64 {
	maxProb := 0.0
	for _, pred := range a.PricePredictions {
		for _, s := range pred.Scenarios {
			if s.Direction == "down" && s.ChangePercent < 0 && s.Probability > maxProb {
				maxProb = s.Probability
			}
		}
	}
	return maxProb
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
