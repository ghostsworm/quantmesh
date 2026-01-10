package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RiskAssessor AI 风险评估器
// 在策略启动前进行智能风险评估
type RiskAssessor struct {
	client *GeminiClient
}

// NewRiskAssessor 创建风险评估器
func NewRiskAssessor(apiKey string) *RiskAssessor {
	return &RiskAssessor{
		client: NewGeminiClient(apiKey),
	}
}

// RiskAssessmentRequest 风险评估请求
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
	Volatility24h  float64 `json:"volatility_24h"`  // 24小时波动率
	Volume24h      float64 `json:"volume_24h"`      // 24小时成交量
	
	// 用户偏好
	RiskTolerance  string  `json:"risk_tolerance"` // conservative/moderate/aggressive
}

// RiskAssessmentResponse 风险评估响应
type RiskAssessmentResponse struct {
	// 总体评分 (0-100)
	OverallScore    int    `json:"overall_score"`
	RiskLevel       string `json:"risk_level"` // low/medium/high/extreme
	
	// 详细评分
	ScoreBreakdown  ScoreBreakdown `json:"score_breakdown"`
	
	// 风险因素
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
	RiskControl        int `json:"risk_control"`        // 风险控制 (0-25)
	StrategyFit        int `json:"strategy_fit"`        // 策略适配 (0-25)
	MarketCondition    int `json:"market_condition"`    // 市场条件 (0-25)
}

// RiskFactor 风险因素
type RiskFactor struct {
	Factor      string `json:"factor"`       // 风险因素名称
	Severity    string `json:"severity"`     // low/medium/high/critical
	Description string `json:"description"`  // 描述
	Impact      string `json:"impact"`       // 潜在影响
}

// Suggestion 优化建议
type Suggestion struct {
	Category    string `json:"category"`    // 类别
	Priority    string `json:"priority"`    // high/medium/low
	Title       string `json:"title"`       // 标题
	Description string `json:"description"` // 描述
	Parameter   string `json:"parameter"`   // 相关参数
	CurrentVal  string `json:"current_val"` // 当前值
	SuggestVal  string `json:"suggest_val"` // 建议值
}

// AssessRisk 执行风险评估
func (r *RiskAssessor) AssessRisk(ctx context.Context, req *RiskAssessmentRequest) (*RiskAssessmentResponse, error) {
	prompt := r.buildPrompt(req)

	// 定义 JSON Schema
	schema := r.buildSchema()

	geminiReq := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":      0.3, // 降低温度以获得更一致的评估
			"topK":             40,
			"topP":             0.95,
			"responseMimeType": "application/json",
			"responseSchema":   schema,
		},
	}

	jsonData, err := json.Marshal(geminiReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	url := fmt.Sprintf("%s/models/gemini-3-flash-preview:generateContent?key=%s", r.client.baseURL, r.client.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := r.client.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API 返回错误: %d - %s", resp.StatusCode, string(body))
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("AI 未返回有效响应")
	}

	aiText := geminiResp.Candidates[0].Content.Parts[0].Text
	aiText = strings.TrimPrefix(aiText, "```json")
	aiText = strings.TrimPrefix(aiText, "```")
	aiText = strings.TrimSuffix(aiText, "```")
	aiText = strings.TrimSpace(aiText)

	var result RiskAssessmentResponse
	if err := json.Unmarshal([]byte(aiText), &result); err != nil {
		return nil, fmt.Errorf("解析 AI 评估结果失败: %w (响应: %s)", err, aiText)
	}

	return &result, nil
}

// buildPrompt 构建提示词
func (r *RiskAssessor) buildPrompt(req *RiskAssessmentRequest) string {
	toleranceDesc := map[string]string{
		"conservative": "保守型（追求稳定，低风险）",
		"moderate":     "稳健型（平衡风险与收益）",
		"aggressive":   "激进型（追求高收益，可承受高风险）",
	}[req.RiskTolerance]

	strategyParamsJSON, _ := json.MarshalIndent(req.StrategyParams, "", "  ")

	prompt := fmt.Sprintf(`你是一位专业的加密货币量化交易风险评估专家。请对以下策略配置进行全面的风险评估。

## 策略信息
- 策略类型: %s
- 策略名称: %s
- 策略参数:
%s

## 交易配置
- 交易对: %s
- 交易所: %s
- 时间周期: %s
- 总资金: %.2f USDT
- 杠杆倍数: %d倍

## 市场信息
- 当前价格: $%.2f
- 24小时波动率: %.2f%%
- 24小时成交量: $%.2f

## 用户风险偏好
%s

## 评估要求

请从以下四个维度进行评估（每项0-25分，满分100分）：

1. **资金管理 (0-25分)**
   - 单笔订单金额是否合理
   - 最大仓位是否过大
   - 杠杆使用是否安全
   - 资金利用率是否合理

2. **风险控制 (0-25分)**
   - 止损设置是否合理
   - 止盈设置是否合理
   - 最大回撤控制
   - 是否有瀑布式下跌保护

3. **策略适配 (0-25分)**
   - 策略参数是否合理
   - 是否符合用户风险偏好
   - 策略复杂度与用户经验匹配度
   - 参数设置是否有明显错误

4. **市场条件 (0-25分)**
   - 当前市场波动率评估
   - 流动性评估
   - 时间周期选择是否合适
   - 交易对风险等级

## 输出要求

请提供：
1. 总体评分 (0-100) 和风险等级 (low/medium/high/extreme)
2. 各维度详细评分
3. 识别的风险因素（每个因素注明严重程度）
4. 具体优化建议（包含当前值和建议值）
5. 重要警告信息
6. 综合分析摘要 (100-200字)
7. 是否建议继续执行此策略

## 评估标准
- 80-100分: 低风险，可以放心使用
- 60-79分: 中等风险，建议优化后使用
- 40-59分: 高风险，强烈建议修改配置
- 0-39分: 极高风险，不建议使用

注意：
- 对于马丁格尔策略，重点关注最大层数和加仓倍数的风险
- 对于DCA策略，关注ATR参数和止盈止损设置
- 对于高杠杆配置，必须给出严重警告
- 如果使用的时间周期太短（如1分钟），需要提醒滑点和手续费风险
`, req.StrategyType, req.StrategyName, strategyParamsJSON,
		req.Symbol, req.Exchange, req.Timeframe,
		req.TotalCapital, req.Leverage,
		req.CurrentPrice, req.Volatility24h, req.Volume24h,
		toleranceDesc)

	return prompt
}

// buildSchema 构建 JSON Schema
func (r *RiskAssessor) buildSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"overall_score": map[string]interface{}{
				"type":        "integer",
				"description": "总体评分 (0-100)",
			},
			"risk_level": map[string]interface{}{
				"type":        "string",
				"description": "风险等级",
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
				"description": "是否建议继续执行此策略",
			},
		},
		"required": []string{"overall_score", "risk_level", "score_breakdown", "risk_factors", "suggestions", "warnings", "summary", "recommended"},
	}
}

// QuickAssess 快速评估（不调用 AI，使用规则引擎）
func (r *RiskAssessor) QuickAssess(req *RiskAssessmentRequest) *RiskAssessmentResponse {
	response := &RiskAssessmentResponse{
		ScoreBreakdown: ScoreBreakdown{},
		RiskFactors:    make([]RiskFactor, 0),
		Suggestions:    make([]Suggestion, 0),
		Warnings:       make([]string, 0),
	}

	// 资金管理评分
	response.ScoreBreakdown.CapitalManagement = r.assessCapitalManagement(req)
	
	// 风险控制评分
	response.ScoreBreakdown.RiskControl = r.assessRiskControl(req)
	
	// 策略适配评分
	response.ScoreBreakdown.StrategyFit = r.assessStrategyFit(req)
	
	// 市场条件评分
	response.ScoreBreakdown.MarketCondition = r.assessMarketCondition(req)

	// 计算总分
	response.OverallScore = response.ScoreBreakdown.CapitalManagement +
		response.ScoreBreakdown.RiskControl +
		response.ScoreBreakdown.StrategyFit +
		response.ScoreBreakdown.MarketCondition

	// 确定风险等级
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

	// 添加风险因素和建议
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

	// 检查单笔订单金额占比
	if baseAmount, ok := req.StrategyParams["base_order_amount"].(float64); ok {
		ratio := baseAmount / req.TotalCapital * 100
		if ratio > 20 {
			score -= 10
		} else if ratio > 10 {
			score -= 5
		}
	}

	// 检查最大层数（马丁格尔/DCA）
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

// assessRiskControl 评估风险控制
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

	// 检查趋势过滤
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

	// 根据策略类型和风险偏好评估
	switch req.StrategyType {
	case "martingale":
		if req.RiskTolerance == "conservative" {
			score -= 10 // 保守型不适合马丁
		}
		// 检查倍数
		if multiplier, ok := req.StrategyParams["multiplier"].(float64); ok {
			if multiplier > 2.5 {
				score -= 8
			} else if multiplier > 2.0 {
				score -= 4
			}
		}
	case "dca":
		// DCA 相对安全
		score += 5
	case "grid":
		// 网格适合震荡市
		score += 3
	}

	// 时间周期评估
	switch req.Timeframe {
	case "1m":
		score -= 10 // 1分钟周期风险高
	case "5m":
		score -= 5
	case "15m", "1h":
		// 合理的时间周期
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

// addRiskFactors 添加风险因素
func (r *RiskAssessor) addRiskFactors(req *RiskAssessmentRequest, resp *RiskAssessmentResponse) {
	// 高杠杆风险
	if req.Leverage > 10 {
		resp.RiskFactors = append(resp.RiskFactors, RiskFactor{
			Factor:      "高杠杆",
			Severity:    "high",
			Description: fmt.Sprintf("使用了 %d 倍杠杆", req.Leverage),
			Impact:      "可能导致快速爆仓",
		})
		resp.Warnings = append(resp.Warnings, fmt.Sprintf("⚠️ 高杠杆警告：%d倍杠杆风险极高，建议降低至5倍以下", req.Leverage))
	}

	// 马丁策略风险
	if req.StrategyType == "martingale" {
		if multiplier, ok := req.StrategyParams["multiplier"].(float64); ok && multiplier > 2.0 {
			resp.RiskFactors = append(resp.RiskFactors, RiskFactor{
				Factor:      "马丁倍数过高",
				Severity:    "high",
				Description: fmt.Sprintf("加仓倍数为 %.1f", multiplier),
				Impact:      "后期仓位可能失控",
			})
		}
	}

	// 高波动风险
	if req.Volatility24h > 10 {
		resp.RiskFactors = append(resp.RiskFactors, RiskFactor{
			Factor:      "市场高波动",
			Severity:    "medium",
			Description: fmt.Sprintf("24小时波动率达 %.2f%%", req.Volatility24h),
			Impact:      "可能触发多次加仓或止损",
		})
	}

	// 无止损风险
	if stopLoss, ok := req.StrategyParams["stop_loss"].(float64); !ok || stopLoss <= 0 {
		resp.RiskFactors = append(resp.RiskFactors, RiskFactor{
			Factor:      "无止损设置",
			Severity:    "critical",
			Description: "未设置止损保护",
			Impact:      "可能导致无限亏损",
		})
		resp.Warnings = append(resp.Warnings, "🚨 严重警告：未设置止损，极端行情下可能导致巨额亏损！")
	}
}

// addSuggestions 添加优化建议
func (r *RiskAssessor) addSuggestions(req *RiskAssessmentRequest, resp *RiskAssessmentResponse) {
	// 杠杆建议
	if req.Leverage > 5 {
		resp.Suggestions = append(resp.Suggestions, Suggestion{
			Category:    "风险控制",
			Priority:    "high",
			Title:       "降低杠杆倍数",
			Description: "高杠杆会放大风险，建议使用较低杠杆",
			Parameter:   "leverage",
			CurrentVal:  fmt.Sprintf("%d倍", req.Leverage),
			SuggestVal:  "3-5倍",
		})
	}

	// 止损建议
	if stopLoss, ok := req.StrategyParams["stop_loss"].(float64); !ok || stopLoss <= 0 {
		resp.Suggestions = append(resp.Suggestions, Suggestion{
			Category:    "风险控制",
			Priority:    "high",
			Title:       "添加止损设置",
			Description: "建议设置合理的止损比例以限制最大亏损",
			Parameter:   "stop_loss",
			CurrentVal:  "未设置",
			SuggestVal:  "5-15%",
		})
	}

	// 趋势过滤建议
	if trendFilter, ok := req.StrategyParams["trend_filter"].(bool); !ok || !trendFilter {
		resp.Suggestions = append(resp.Suggestions, Suggestion{
			Category:    "策略优化",
			Priority:    "medium",
			Title:       "启用趋势过滤",
			Description: "在下跌趋势中暂停买入，减少死扛风险",
			Parameter:   "trend_filter",
			CurrentVal:  "禁用",
			SuggestVal:  "启用",
		})
	}
}

// generateSummary 生成摘要
func (r *RiskAssessor) generateSummary(req *RiskAssessmentRequest, resp *RiskAssessmentResponse) string {
	var summary strings.Builder

	summary.WriteString(fmt.Sprintf("策略类型: %s, 评分: %d/100 (%s风险)。",
		req.StrategyType, resp.OverallScore, resp.RiskLevel))

	if len(resp.RiskFactors) > 0 {
		summary.WriteString(fmt.Sprintf(" 发现 %d 个风险因素。", len(resp.RiskFactors)))
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

// GetRiskColor 获取风险等级对应的颜色
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

// GetScoreEmoji 获取评分对应的表情
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
