package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"quantmesh/ai"
	"quantmesh/config"
	"quantmesh/logger"
)

// GeminiNewsAnalyzer Gemini 新聞分析器：整合历史新闻 + 實時搜索，输出價格概率預测
// 现在支持多种AI Provider（Gemini、OpenAI、Claude、Poe）
type GeminiNewsAnalyzer struct {
	cfg           *config.Config
	newsCollector *NewsCollector
	aiClient      ai.AIClient
	analyzing     atomic.Bool
	lastResult    *NewsRiskAssessment
	lastAnalysis  time.Time
}

// NewGeminiNewsAnalyzer 創建 Gemini 新聞分析器
func NewGeminiNewsAnalyzer(cfg *config.Config, newsCollector *NewsCollector) *GeminiNewsAnalyzer {
	analyzer := &GeminiNewsAnalyzer{
		cfg:           cfg,
		newsCollector: newsCollector,
	}

	// 初始化AI客户端
	provider := cfg.NewsMonitor.AIProvider.Provider
	if provider == "" {
		provider = "gemini" // 默认使用Gemini
	}

	apiKey := cfg.NewsMonitor.AIProvider.APIKey
	if apiKey == "" {
		// 兼容旧配置：从全局AI配置读取
		if provider == "gemini" {
			apiKey = cfg.AI.GeminiAPIKey
			if apiKey == "" {
				apiKey = cfg.AI.APIKey
			}
		} else {
			apiKey = cfg.AI.APIKey
		}
	}

	if apiKey != "" {
		aiClient, err := ai.NewAIClient(
			provider,
			cfg.NewsMonitor.AIProvider.Model,
			apiKey,
			cfg.NewsMonitor.AIProvider.BaseURL,
		)
		if err != nil {
			logger.Warn("⚠️ 初始化AI客户端失败: %v，将使用规则引擎", err)
		} else {
			analyzer.aiClient = aiClient
		}
	}

	return analyzer
}

// AssetType 资產類型常量
const (
	AssetTypeCryptoBTC        = "crypto_btc"
	AssetTypeCryptoETH        = "crypto_eth"
	AssetTypeCryptoSOL        = "crypto_sol"
	AssetTypeCryptoDOGE       = "crypto_doge"
	AssetTypeCommodityGold    = "commodity_gold"
	AssetTypeCommoditySilver  = "commodity_silver"
	AssetTypeStockUS          = "stock_us"
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

	if g.aiClient == nil {
		return nil, fmt.Errorf("AI 客户端未初始化，请检查配置")
	}

	// 检查是否启用AI分析（兼容旧配置）
	if !g.cfg.NewsMonitor.UseGeminiSearch {
		// 如果UseGeminiSearch为false，但配置了AI Provider，仍然可以使用
		// 否则降级到规则引擎
		if g.cfg.NewsMonitor.AIProvider.Provider == "" && g.cfg.NewsMonitor.AIProvider.APIKey == "" {
			return nil, fmt.Errorf("AI 搜索未啟用，请使用 NewsMonitor 的规则引擎")
		}
	}

	prompt := g.buildPrompt(assetType, symbol, currentPrice, focusEvent)
	schema := g.buildOutputSchema()

	// 判断是否使用Google Search（仅Gemini原生支持）
	useGoogleSearch := false
	if g.cfg.NewsMonitor.AIProvider.Provider == "gemini" || g.cfg.NewsMonitor.AIProvider.Provider == "" {
		useGoogleSearch = g.cfg.NewsMonitor.UseGeminiSearch
	}

	aiText, err := g.aiClient.GenerateContent(ctx, prompt, schema, useGoogleSearch)
	if err != nil {
		logger.Warn("📰 AI 新聞分析失败: %v", err)
		return nil, err
	}

	assessment, err := g.parseResponse(aiText, assetType, symbol, currentPrice)
	if err != nil {
		logger.Warn("📰 解析 AI 响应失败: %v (原始: %s)", err, truncate(aiText, 200))
		return nil, err
	}

	g.lastResult = assessment
	g.lastAnalysis = time.Now()
	provider := g.cfg.NewsMonitor.AIProvider.Provider
	if provider == "" {
		provider = "gemini"
	}
	logger.Info("📰 %s 新聞分析完成: 建议=%s, 2h跌5%%概率=%.0f%%",
		provider, assessment.Recommendation, g.getProb2hDown5(assessment)*100)
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

	switch assetType {
	case AssetTypeCommodityGold:
		sb.WriteString(`你是一位专业的黃金市场分析师。请基於實時搜索的最新新闻和历史新闻，分析國際金價（PAXG/USDT）走势。

`)
		sb.WriteString(`请重点关注：
1. 美聯儲货币政策和利率預期
2. 美元指數（DXY）走势
3. 通脹數據（CPI、PCE）
4. 地緣政治事件（戰爭、制裁、避險情绪）
5. 央行黃金儲备变化
6. 市場波動率和避險需求

`)
	case AssetTypeCommoditySilver:
		sb.WriteString(`你是一位专业的白銀市场分析师。请基於實時搜索的最新新闻和历史新闻，分析白銀價格走势。

`)
		sb.WriteString(`请重点关注：
1. 美聯儲货币政策和利率預期
2. 美元指數（DXY）走势
3. 通脹數據（CPI、PCE）
4. 工業需求（太陽能、電子產品）
5. 黃金白銀比價關係
6. 地緣政治和避險需求

`)
	case AssetTypeStockUS:
		sb.WriteString(`你是一位专业的美股市场分析师。请基於實時搜索的最新新闻和历史新闻，分析美股（S&P 500）走势。

`)
		sb.WriteString(`请重点关注：
1. 美聯儲货币政策和利率決議
2. 經濟數據（GDP、失業率、CPI、PCE）
3. 企業財報季和盈利預期
4. 地緣政治風險
5. 科技股表現（FAANG、AI相關）
6. 市場情緒和資金流向

`)
	case AssetTypeCryptoETH:
		sb.WriteString(`你是一位专业的以太坊市场分析师。请基於實時搜索的最新新闻和历史新闻，分析以太坊（ETH）價格走势。

`)
		sb.WriteString(`请重点关注：
1. 以太坊 ETF 進展和批准情況
2. 以太坊升級和技術發展
3. DeFi 生態系統發展
4. Layer 2 解決方案採用
5. Gas 費用和網絡擁堵
6. 監管政策和機構採用

`)
	case AssetTypeCryptoSOL:
		sb.WriteString(`你是一位专业的 Solana 市场分析师。请基於實時搜索的最新新闻和历史新闻，分析 Solana（SOL）價格走势。

`)
		sb.WriteString(`请重点关注：
1. Solana ETF 進展和批准情況
2. Solana 網絡穩定性和停機事件
3. Solana 生態系統發展（DeFi、NFT）
4. 機構採用和合作夥伴關係
5. 技術升級和性能改進
6. 市場競爭和替代方案

`)
	case AssetTypeCryptoDOGE:
		sb.WriteString(`你是一位专业的 Dogecoin 市场分析师。请基於實時搜索的最新新闻和历史新闻，分析 Dogecoin（DOGE）價格走势。

`)
		sb.WriteString(`请重点关注：
1. Elon Musk 和特斯拉相關動態
2. 模因幣市場整體趨勢
3. DOGE 採用和支付場景
4. 社區活動和社交媒體熱度
5. 市場情緒和投機需求
6. 監管環境變化

`)
	default:
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

**重要格式说明**：
- timeframe 字段必须是简单的時间标识，如 "2h"、"4h"、"6h"、"12h"、"24h"
- 每個時间窗口的 scenarios 数组必须包含多個场景對象
- 每個场景對象必须包含：direction（"down"/"up"/"neutral"）、change_percent（數字，如 -5、-10、0）、probability（0-1 之间的小數）
- target_price_range 必须包含 min 和 max 两個數字

**正确的 price_predictions 示例**：
` + "```json" + `
{
  "price_predictions": [
    {
      "timeframe": "2h",
      "scenarios": [
        {"direction": "down", "change_percent": -5, "probability": 0.15},
        {"direction": "down", "change_percent": -10, "probability": 0.05},
        {"direction": "neutral", "change_percent": 0, "probability": 0.60},
        {"direction": "up", "change_percent": 5, "probability": 0.20}
      ],
      "target_price_range": {"min": 95000, "max": 98000}
    },
    {
      "timeframe": "4h",
      "scenarios": [
        {"direction": "down", "change_percent": -5, "probability": 0.20},
        {"direction": "neutral", "change_percent": 0, "probability": 0.50},
        {"direction": "up", "change_percent": 5, "probability": 0.30}
      ],
      "target_price_range": {"min": 94000, "max": 99000}
    }
  ]
}
` + "```" + `

同時提供：當前價位分析（current_price_analysis）、主要风險因素（risk_factors 数组）、建议操作 recommendation（必须是 normal/caution/reduce_position/stop_trading 之一）、分析摘要（analysis_summary）。

## 非常重要：JSON 输出格式要求

你必须严格按照以下完整 JSON 结构输出，不要添加任何额外的文字说明：

` + "```json" + `
{
  "current_price_analysis": {
    "current_price": 96500.00,
    "price_trend": "down",
    "support_level": 95000,
    "resistance_level": 98000,
    "change_24h_percent": -2.5
  },
  "price_predictions": [
    {
      "timeframe": "2h",
      "scenarios": [
        {"direction": "down", "change_percent": -5, "probability": 0.15},
        {"direction": "down", "change_percent": -10, "probability": 0.05},
        {"direction": "neutral", "change_percent": 0, "probability": 0.60},
        {"direction": "up", "change_percent": 5, "probability": 0.20}
      ],
      "target_price_range": {"min": 95000, "max": 98000}
    }
  ],
  "risk_factors": [
    {"factor": "美聯儲鷹派政策", "severity": "high", "impact": "可能導致風險資產拋售"},
    {"factor": "地緣政治緊張", "severity": "medium", "impact": "避險情緒上升"}
  ],
  "recommendation": "caution",
  "analysis_summary": "當前市場受到宏觀經濟不確定性影響..."
}
` + "```" + `

**禁止**：
- 不要在 timeframe 中写入除 "2h"、"4h"、"6h"、"12h"、"24h" 之外的内容
- 不要将多个字段合并成一个字符串
- 不要在 JSON 外添加任何解释文字
- 确保 probability 是 0-1 之间的小数（如 0.15），不是百分比（如 15）

只输出上述格式的纯 JSON，不要有其他内容。`)
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
					"price_trend":        map[string]interface{}{"type": "string", "enum": []interface{}{"up", "down", "neutral"}},
					"support_level":      map[string]interface{}{"type": "number"},
					"resistance_level":   map[string]interface{}{"type": "number"},
					"change_24h_percent": map[string]interface{}{"type": "number"},
				},
				"required": []interface{}{"current_price", "price_trend", "support_level", "resistance_level"},
			},
			"price_predictions": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"timeframe": map[string]interface{}{
							"type":        "string",
							"description": "時间窗口标识，必须是简单格式如 2h、4h、6h、12h、24h",
							"pattern":     "^\\d+h$",
						},
						"scenarios": map[string]interface{}{
							"type":        "array",
							"description": "不同價格变动场景及其概率",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"direction": map[string]interface{}{
										"type":        "string",
										"enum":        []interface{}{"up", "down", "neutral"},
										"description": "價格变动方向",
									},
									"change_percent": map[string]interface{}{
										"type":        "number",
										"description": "變化百分比，下跌用负數如 -5、-10，上涨用正數如 5、10，持平为 0",
									},
									"probability": map[string]interface{}{
										"type":        "number",
										"minimum":     0,
										"maximum":     1,
										"description": "发生概率，0-1 之间的小數，如 0.15 表示 15%%",
									},
								},
								"required": []interface{}{"direction", "change_percent", "probability"},
							},
						},
						"target_price_range": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"min": map[string]interface{}{"type": "number", "description": "預测價格区间下限"},
								"max": map[string]interface{}{"type": "number", "description": "預测價格区间上限"},
							},
							"required": []interface{}{"min", "max"},
						},
					},
					"required": []interface{}{"timeframe", "scenarios", "target_price_range"},
				},
			},
			"risk_factors": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"factor":   map[string]interface{}{"type": "string", "description": "风險因素名稱"},
						"severity": map[string]interface{}{"type": "string", "enum": []interface{}{"low", "medium", "high", "critical"}},
						"impact":   map[string]interface{}{"type": "string", "description": "對價格的潛在影响"},
					},
					"required": []interface{}{"factor", "severity"},
				},
			},
			"recommendation": map[string]interface{}{
				"type":        "string",
				"enum":        []interface{}{"normal", "caution", "reduce_position", "stop_trading"},
				"description": "操作建议",
			},
			"analysis_summary": map[string]interface{}{
				"type":        "string",
				"description": "分析摘要，简要说明当前市场状况和主要影响因素",
			},
		},
		"required": []interface{}{"current_price_analysis", "price_predictions", "risk_factors", "recommendation", "analysis_summary"},
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
			Timeframe string `json:"timeframe"`
			Scenarios []struct {
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
		Recommendation  string `json:"recommendation"`
		AnalysisSummary string `json:"analysis_summary"`
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
		// 规范化 timeframe：提取純時间标识（如 "2h"）
		timeframe := normalizeTimeframe(p.Timeframe)
		if timeframe == "" {
			logger.Warn("📰 跳過無效的 timeframe: %s", p.Timeframe)
			continue
		}

		pred := PricePrediction{
			Timeframe:        timeframe,
			TargetPriceRange: PriceRange{Min: p.TargetPriceRange.Min, Max: p.TargetPriceRange.Max},
		}
		for _, s := range p.Scenarios {
			// 规范化 probability：确保在 0-1 之间
			prob := s.Probability
			if prob > 1 {
				prob = prob / 100.0 // 如果是百分比格式（如 15），轉換為小數（0.15）
			}
			if prob < 0 {
				prob = 0
			}
			if prob > 1 {
				prob = 1
			}

			// 规范化 direction
			direction := strings.ToLower(strings.TrimSpace(s.Direction))
			if direction != "up" && direction != "down" && direction != "neutral" {
				if s.ChangePercent < 0 {
					direction = "down"
				} else if s.ChangePercent > 0 {
					direction = "up"
				} else {
					direction = "neutral"
				}
			}

			pred.Scenarios = append(pred.Scenarios, PriceScenario{
				Direction:     direction,
				ChangePercent: s.ChangePercent,
				Probability:   prob,
			})
		}

		// 只添加有效的预测（至少有一个场景）
		if len(pred.Scenarios) > 0 {
			assessment.PricePredictions = append(assessment.PricePredictions, pred)
		}
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

// normalizeTimeframe 规范化時间窗口标识，提取純格式如 "2h"
func normalizeTimeframe(tf string) string {
	tf = strings.TrimSpace(tf)
	// 已经是正确格式
	if matched, _ := regexp.MatchString(`^\d+h$`, tf); matched {
		return tf
	}

	// 尝试提取 "2h"、"4h" 等模式
	re := regexp.MustCompile(`(\d+)\s*h`)
	matches := re.FindStringSubmatch(strings.ToLower(tf))
	if len(matches) >= 2 {
		return matches[1] + "h"
	}

	// 尝试提取小時數
	re = regexp.MustCompile(`(\d+)\s*(小時|hours?|hour)`)
	matches = re.FindStringSubmatch(strings.ToLower(tf))
	if len(matches) >= 2 {
		return matches[1] + "h"
	}

	return ""
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
