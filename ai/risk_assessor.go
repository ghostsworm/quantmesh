package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// RiskAssessor AI 风險评估器
// 在策略啟動前進行智能风險评估
type RiskAssessor struct {
	client GeminiClient
}

// NewRiskAssessor 創建风險评估器
func NewRiskAssessor(apiKey string) *RiskAssessor {
	return &RiskAssessor{
		client: NewGeminiClient(apiKey),
	}
}

// RiskAssessmentRequest 风險评估请求
type RiskAssessmentRequest struct {
	// 策略信息
	StrategyType   string                 `json:"strategy_type"`   // dca/martingale/grid/combo
	StrategyName   string                 `json:"strategy_name"`
	StrategyParams map[string]interface{} `json:"strategy_params"`
	
	// 交易配置
	Symbol         string  `json:"symbol"`
	Exchange       string  `json:"exchange"`
	Timeframe      string  `json:"timeframe"`      // 1m/5m/15m/1h/4h/1d
	TotalCapital   float64 `json:"total_capital"`
	Leverage       int     `json:"leverage"`
	
	// 市场信息
	CurrentPrice   float64 `json:"current_price"`
	Volatility24h  float64 `json:"volatility_24h"`  // 24小時波动率
	Volume24h      float64 `json:"volume_24h"`      // 24小時成交量
	
	// 用戶偏好
	RiskTolerance  string  `json:"risk_tolerance"` // conservative/moderate/aggressive
}

// RiskAssessmentResponse 风險评估响应
type RiskAssessmentResponse struct {
	// 總体评分 (0-100)
	OverallScore    int    `json:"overall_score"`
	RiskLevel       string `json:"risk_level"` // low/medium/high/extreme
	
	// 详细评分
	ScoreBreakdown  ScoreBreakdown `json:"score_breakdown"`
	
	// 风險因素
	RiskFactors     []RiskFactor `json:"risk_factors"`
	
	// 优化建议
	Suggestions     []Suggestion `json:"suggestions"`
	
	// 警告信息
	Warnings        []string `json:"warnings"`
	
	// 综合分析
	Summary         string `json:"summary"`
	
	// 是否建议继续
	Recommended     bool   `json:"recommended"`
}

// ScoreBreakdown 评分细分
type ScoreBreakdown struct {
	CapitalManagement  int `json:"capital_management"`  // 资金管理 (0-25)
	RiskControl        int `json:"risk_control"`        // 风險控制 (0-25)
	StrategyFit        int `json:"strategy_fit"`        // 策略适配 (0-25)
	MarketCondition    int `json:"market_condition"`    // 市场条件 (0-25)
}

// RiskFactor 风險因素
type RiskFactor struct {
	Factor      string `json:"factor"`       // 风險因素名称
	Severity    string `json:"severity"`     // low/medium/high/critical
	Description string `json:"description"`  // 描述
	Impact      string `json:"impact"`       // 潜在影响
}

// Suggestion 优化建议
type Suggestion struct {
	Category    string `json:"category"`    // 類别
	Priority    string `json:"priority"`    // high/medium/low
	Title       string `json:"title"`       // 標题
	Description string `json:"description"` // 描述
	Parameter   string `json:"parameter"`   // 相关参數
	CurrentVal  string `json:"current_val"` // 當前值
	SuggestVal  string `json:"suggest_val"` // 建议值
}

// AssessRisk 執行风險评估
func (r *RiskAssessor) AssessRisk(ctx context.Context, req *RiskAssessmentRequest) (*RiskAssessmentResponse, error) {
	prompt := r.buildPrompt(req)

	// 定义 JSON Schema
	schema := r.buildSchema()

	// 使用接口方法生成内容
	aiText, err := r.client.GenerateContent(ctx, prompt, schema)
	if err != nil {
		return nil, err
	}

	var result RiskAssessmentResponse
	if err := json.Unmarshal([]byte(aiText), &result); err != nil {
		return nil, fmt.Errorf("解析 AI 评估結果失败: %w (响应: %s)", err, aiText)
	}

	return &result, nil
}

// buildPrompt 構建提示词
func (r *RiskAssessor) buildPrompt(req *RiskAssessmentRequest) string {
	toleranceDesc := map[string]string{
		"conservative": "保守型（追求稳定，低风險）",
		"moderate":     "稳健型（平衡风險與收益）",
		"aggressive":   "激進型（追求高收益，可承受高风險）",
	}[req.RiskTolerance]

	strategyParamsJSON, _ := json.MarshalIndent(req.StrategyParams, "", "  ")

	prompt := fmt.Sprintf(`你是一位专业的加密貨幣量化交易风險评估专家。请對以下策略配置進行全面的风險评估。

## 策略信息
- 策略類型: %s
- 策略名称: %s
- 策略参數:
%s

## 交易配置
- 交易對: %s
- 交易所: %s
- 時间周期: %s
- 總资金: %.2f USDT
- 杠杆倍數: %d倍

## 市场信息
- 當前價格: $%.2f
- 24小時波动率: %.2f%%
- 24小時成交量: $%.2f

## 用戶风險偏好
%s

## 评估要求

请從以下四個维度進行评估（每项0-25分，满分100分）：

1. **资金管理 (0-25分)**
   - 單笔订單金額是否合理
   - 最大倉位是否過大
   - 杠杆使用是否安全
   - 资金利用率是否合理

2. **风險控制 (0-25分)**
   - 止损設置是否合理
   - 止盈設置是否合理
   - 最大回撤控制
   - 是否有瀑布式下跌保护

3. **策略适配 (0-25分)**
   - 策略参數是否合理
   - 是否符合用戶风險偏好
   - 策略複杂度與用戶經驗匹配度
   - 参數設置是否有明显錯误

4. **市场条件 (0-25分)**
   - 當前市場波动率评估
   - 流动性评估
   - 時间周期选擇是否合适
   - 交易對风險等级

## 输出要求

请提供：
1. 總体评分 (0-100) 和风險等级 (low/medium/high/extreme)
2. 各维度详细评分
3. 识别的风險因素（每個因素注明严重程度）
4. 具体优化建议（包含當前值和建议值）
5. 重要警告信息
6. 综合分析摘要 (100-200字)
7. 是否建议继续執行此策略

## 评估標准
- 80-100分: 低风險，可以放心使用
- 60-79分: 中等风險，建议优化后使用
- 40-59分: 高风險，强烈建议修改配置
- 0-39分: 极高风險，不建议使用

注意：
- 對於马丁格尔策略，重点关注最大层數和加倉倍數的风險
- 對於DCA策略，关注ATR参數和止盈止损設置
- 對於高杠杆配置，必須给出严重警告
- 如果使用的時间周期太短（如1分钟），需要提醒滑点和手续费风險
`, req.StrategyType, req.StrategyName, strategyParamsJSON,
		req.Symbol, req.Exchange, req.Timeframe,
		req.TotalCapital, req.Leverage,
		req.CurrentPrice, req.Volatility24h, req.Volume24h,
		toleranceDesc)

	return prompt
}

// buildSchema 構建 JSON Schema
func (r *RiskAssessor) buildSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"overall_score": map[string]interface{}{
				"type":        "integer",
				"description": "總体评分 (0-100)",
			},
			"risk_level": map[string]interface{}{
				"type":        "string",
				"description": "风險等级",
				"enum":        []string{"low", "medium", "high", "extreme"},
			},
			"score_breakdown": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"capital_management": map[string]interface{}{"type": "integer"},
					"risk_control":       map[string]interface{}{"type": "integer"},
					"strategy_fit":       map[string]interface{}{"type": "integer"},
					"market_condition":   map[string]interface{}{"type": "integer"},
				},
				"required": []string{"capital_management", "risk_control", "strategy_fit", "market_condition"},
			},
			"risk_factors": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"factor":      map[string]interface{}{"type": "string"},
						"severity":    map[string]interface{}{"type": "string", "enum": []string{"low", "medium", "high", "critical"}},
						"description": map[string]interface{}{"type": "string"},
						"impact":      map[string]interface{}{"type": "string"},
					},
					"required": []string{"factor", "severity", "description", "impact"},
				},
			},
			"suggestions": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"category":    map[string]interface{}{"type": "string"},
						"priority":    map[string]interface{}{"type": "string", "enum": []string{"high", "medium", "low"}},
						"title":       map[string]interface{}{"type": "string"},
						"description": map[string]interface{}{"type": "string"},
						"parameter":   map[string]interface{}{"type": "string"},
						"current_val": map[string]interface{}{"type": "string"},
						"suggest_val": map[string]interface{}{"type": "string"},
					},
					"required": []string{"category", "priority", "title", "description"},
				},
			},
			"warnings": map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
			"summary": map[string]interface{}{
				"type":        "string",
				"description": "综合分析摘要 (100-200字)",
			},
			"recommended": map[string]interface{}{
				"type":        "boolean",
				"description": "是否建议继续執行此策略",
			},
		},
		"required": []string{"overall_score", "risk_level", "score_breakdown", "risk_factors", "suggestions", "warnings", "summary", "recommended"},
	}
}

// QuickAssess 快速评估（不調用 AI，使用规则引擎）
func (r *RiskAssessor) QuickAssess(req *RiskAssessmentRequest) *RiskAssessmentResponse {
	response := &RiskAssessmentResponse{
		ScoreBreakdown: ScoreBreakdown{},
		RiskFactors:    make([]RiskFactor, 0),
		Suggestions:    make([]Suggestion, 0),
		Warnings:       make([]string, 0),
	}

	// 资金管理评分
	response.ScoreBreakdown.CapitalManagement = r.assessCapitalManagement(req)
	
	// 风險控制评分
	response.ScoreBreakdown.RiskControl = r.assessRiskControl(req)
	
	// 策略适配评分
	response.ScoreBreakdown.StrategyFit = r.assessStrategyFit(req)
	
	// 市场条件评分
	response.ScoreBreakdown.MarketCondition = r.assessMarketCondition(req)

	// 计算總分
	response.OverallScore = response.ScoreBreakdown.CapitalManagement +
		response.ScoreBreakdown.RiskControl +
		response.ScoreBreakdown.StrategyFit +
		response.ScoreBreakdown.MarketCondition

	// 确定风險等级
	switch {
	case response.OverallScore >= 80:
		response.RiskLevel = "low"
		response.Recommended = true
	case response.OverallScore >= 60:
		response.RiskLevel = "medium"
		response.Recommended = true
	case response.OverallScore >= 40:
		response.RiskLevel = "high"
		response.Recommended = false
	default:
		response.RiskLevel = "extreme"
		response.Recommended = false
	}

	// 添加风險因素和建议
	r.addRiskFactors(req, response)
	r.addSuggestions(req, response)

	// 生成摘要
	response.Summary = r.generateSummary(req, response)

	return response
}

// assessCapitalManagement 评估资金管理
func (r *RiskAssessor) assessCapitalManagement(req *RiskAssessmentRequest) int {
	score := 25

	// 检查杠杆
	if req.Leverage > 20 {
		score -= 15
	} else if req.Leverage > 10 {
		score -= 10
	} else if req.Leverage > 5 {
		score -= 5
	}

	// 检查單笔订單金額占比
	if baseAmount, ok := req.StrategyParams["base_order_amount"].(float64); ok {
		ratio := baseAmount / req.TotalCapital * 100
		if ratio > 20 {
			score -= 10
		} else if ratio > 10 {
			score -= 5
		}
	}

	// 检查最大层數（马丁格尔/DCA）
	if maxLevels, ok := req.StrategyParams["max_levels"].(float64); ok {
		if maxLevels > 10 {
			score -= 8
		} else if maxLevels > 6 {
			score -= 4
		}
	}

	if score < 0 {
		score = 0
	}
	return score
}

// assessRiskControl 评估风險控制
func (r *RiskAssessor) assessRiskControl(req *RiskAssessmentRequest) int {
	score := 25

	// 检查止损
	hasStopLoss := false
	if stopLoss, ok := req.StrategyParams["stop_loss"].(float64); ok && stopLoss > 0 {
		hasStopLoss = true
		if stopLoss > 20 {
			score -= 8 // 止损太宽
		}
	}
	if !hasStopLoss {
		score -= 15 // 没有止损
	}

	// 检查止盈
	hasTakeProfit := false
	if tp, ok := req.StrategyParams["take_profit"].(float64); ok && tp > 0 {
		hasTakeProfit = true
	}
	if !hasTakeProfit {
		score -= 5
	}

	// 检查瀑布保护
	if cascadeProtection, ok := req.StrategyParams["cascade_protection"].(bool); ok && cascadeProtection {
		score += 3
	}

	// 检查趨勢過濾
	if trendFilter, ok := req.StrategyParams["trend_filter"].(bool); ok && trendFilter {
		score += 2
	}

	if score < 0 {
		score = 0
	}
	if score > 25 {
		score = 25
	}
	return score
}

// assessStrategyFit 评估策略适配
func (r *RiskAssessor) assessStrategyFit(req *RiskAssessmentRequest) int {
	score := 25

	// 根據策略類型和风險偏好评估
	switch req.StrategyType {
	case "martingale":
		if req.RiskTolerance == "conservative" {
			score -= 10 // 保守型不适合马丁
		}
		// 检查倍數
		if multiplier, ok := req.StrategyParams["multiplier"].(float64); ok {
			if multiplier > 2.5 {
				score -= 8
			} else if multiplier > 2.0 {
				score -= 4
			}
		}
	case "dca":
		// DCA 相對安全
		score += 5
	case "grid":
		// 网格适合震荡市
		score += 3
	}

	// 時间周期评估
	switch req.Timeframe {
	case "1m":
		score -= 10 // 1分钟周期风險高
	case "5m":
		score -= 5
	case "15m", "1h":
		// 合理的時间周期
	case "4h", "1d":
		score += 2
	}

	if score < 0 {
		score = 0
	}
	if score > 25 {
		score = 25
	}
	return score
}

// assessMarketCondition 评估市场条件
func (r *RiskAssessor) assessMarketCondition(req *RiskAssessmentRequest) int {
	score := 25

	// 波动率评估
	if req.Volatility24h > 10 {
		score -= 10 // 极高波动
	} else if req.Volatility24h > 5 {
		score -= 5
	} else if req.Volatility24h < 1 {
		score -= 3 // 波动太低可能不适合网格
	}

	// 成交量评估
	if req.Volume24h < 1000000 {
		score -= 5 // 流动性不足
	}

	if score < 0 {
		score = 0
	}
	return score
}

// addRiskFactors 添加风險因素
func (r *RiskAssessor) addRiskFactors(req *RiskAssessmentRequest, resp *RiskAssessmentResponse) {
	// 高杠杆风險
	if req.Leverage > 10 {
		resp.RiskFactors = append(resp.RiskFactors, RiskFactor{
			Factor:      "高杠杆",
			Severity:    "high",
			Description: fmt.Sprintf("使用了 %d 倍杠杆", req.Leverage),
			Impact:      "可能導致快速爆倉",
		})
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("⚠️ 高杠杆警告：%d倍杠杆风險极高，建议降低至5倍以下", req.Leverage))
	}

	// 马丁策略风險
	if req.StrategyType == "martingale" {
		if multiplier, ok := req.StrategyParams["multiplier"].(float64); ok && multiplier > 2.0 {
			resp.RiskFactors = append(resp.RiskFactors, RiskFactor{
				Factor:      "马丁倍數過高",
				Severity:    "high",
				Description: fmt.Sprintf("加倉倍數為 %.1f", multiplier),
				Impact:      "后期倉位可能失控",
			})
		}
	}

	// 高波动风險
	if req.Volatility24h > 10 {
		resp.RiskFactors = append(resp.RiskFactors, RiskFactor{
			Factor:      "市场高波动",
			Severity:    "medium",
			Description: fmt.Sprintf("24小時波动率达 %.2f%%", req.Volatility24h),
			Impact:      "可能触发多次加倉或止损",
		})
	}

	// 無止损风險
	if stopLoss, ok := req.StrategyParams["stop_loss"].(float64); !ok || stopLoss <= 0 {
		resp.RiskFactors = append(resp.RiskFactors, RiskFactor{
			Factor:      "無止损設置",
			Severity:    "critical",
			Description: "未設置止损保护",
			Impact:      "可能導致無限亏损",
		})
		resp.Warnings = append(resp.Warnings, "🚨 严重警告：未設置止损，极端行情下可能導致巨額亏损！")
	}
}

// addSuggestions 添加优化建议
func (r *RiskAssessor) addSuggestions(req *RiskAssessmentRequest, resp *RiskAssessmentResponse) {
	// 杠杆建议
	if req.Leverage > 5 {
		resp.Suggestions = append(resp.Suggestions, Suggestion{
			Category:    "风險控制",
			Priority:    "high",
			Title:       "降低杠杆倍數",
			Description: "高杠杆會放大风險，建议使用较低杠杆",
			Parameter:   "leverage",
			CurrentVal:  fmt.Sprintf("%d倍", req.Leverage),
			SuggestVal:  "3-5倍",
		})
	}

	// 止损建议
	if stopLoss, ok := req.StrategyParams["stop_loss"].(float64); !ok || stopLoss <= 0 {
		resp.Suggestions = append(resp.Suggestions, Suggestion{
			Category:    "风險控制",
			Priority:    "high",
			Title:       "添加止损設置",
			Description: "建议設置合理的止损比例以限制最大亏损",
			Parameter:   "stop_loss",
			CurrentVal:  "未設置",
			SuggestVal:  "5-15%",
		})
	}

	// 趨勢過濾建议
	if trendFilter, ok := req.StrategyParams["trend_filter"].(bool); !ok || !trendFilter {
		resp.Suggestions = append(resp.Suggestions, Suggestion{
			Category:    "策略优化",
			Priority:    "medium",
			Title:       "啟用趨勢過濾",
			Description: "在下跌趋势中暂停買入，减少死扛风險",
			Parameter:   "trend_filter",
			CurrentVal:  "禁用",
			SuggestVal:  "啟用",
		})
	}
}

// generateSummary 生成摘要
func (r *RiskAssessor) generateSummary(req *RiskAssessmentRequest, resp *RiskAssessmentResponse) string {
	var summary strings.Builder

	summary.WriteString(fmt.Sprintf("策略類型: %s, 评分: %d/100 (%s风險)。",
		req.StrategyType, resp.OverallScore, resp.RiskLevel))

	if len(resp.RiskFactors) > 0 {
		summary.WriteString(fmt.Sprintf(" 发現 %d 個风險因素。", len(resp.RiskFactors)))
	}

	if resp.Recommended {
		summary.WriteString(" 建议可以使用此策略配置。")
	} else {
		summary.WriteString(" 建议先修改配置后再使用。")
	}

	if len(resp.Suggestions) > 0 {
		highPriority := 0
		for _, s := range resp.Suggestions {
			if s.Priority == "high" {
				highPriority++
			}
		}
		if highPriority > 0 {
			summary.WriteString(fmt.Sprintf(" 有 %d 条高优先级优化建议。", highPriority))
		}
	}

	return summary.String()
}

// GetRiskColor 獲取风險等级對应的颜色
func GetRiskColor(riskLevel string) string {
	switch riskLevel {
	case "low":
		return "green"
	case "medium":
		return "yellow"
	case "high":
		return "orange"
	case "extreme":
		return "red"
	default:
		return "gray"
	}
}

// GetScoreEmoji 獲取评分對应的表情
func GetScoreEmoji(score int) string {
	switch {
	case score >= 80:
		return "✅"
	case score >= 60:
		return "⚠️"
	case score >= 40:
		return "🔶"
	default:
		return "🚨"
	}
}
