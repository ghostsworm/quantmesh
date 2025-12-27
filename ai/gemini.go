package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"quantmesh/logger"
)

// GeminiService Gemini AI服务实现
type GeminiService struct {
	apiKey        string
	baseURL       string
	client        *http.Client
	promptManager *PromptManager
}

// GeminiRequest Gemini API请求结构
type GeminiRequest struct {
	Contents []GeminiContent `json:"contents"`
}

// GeminiContent 内容
type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
}

// GeminiPart 部分
type GeminiPart struct {
	Text string `json:"text"`
}

// GeminiResponse Gemini API响应
type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
	Error       *GeminiError     `json:"error,omitempty"`
}

// GeminiCandidate 候选响应
type GeminiCandidate struct {
	Content GeminiContent `json:"content"`
}

// GeminiError Gemini错误
type GeminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewGeminiService 创建Gemini服务
func NewGeminiService(apiKey string, baseURL string, promptManager *PromptManager) (*GeminiService, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Gemini API Key不能为空")
	}

	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com/v1beta"
	}

	return &GeminiService{
		apiKey:        apiKey,
		baseURL:       baseURL,
		promptManager: promptManager,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// callAPI 调用Gemini API
func (gs *GeminiService) callAPI(ctx context.Context, prompt string) (string, error) {
	url := fmt.Sprintf("%s/models/gemini-pro:generateContent?key=%s", gs.baseURL, gs.apiKey)

	reqBody := GeminiRequest{
		Contents: []GeminiContent{
			{
				Parts: []GeminiPart{
					{Text: prompt},
				},
			},
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := gs.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var geminiErr GeminiError
		if err := json.Unmarshal(body, &geminiErr); err == nil {
			return "", fmt.Errorf("Gemini API错误: %s", geminiErr.Message)
		}
		return "", fmt.Errorf("HTTP错误: %d, 响应: %s", resp.StatusCode, string(body))
	}

	var geminiResp GeminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if geminiResp.Error != nil {
		return "", fmt.Errorf("Gemini API错误: %s", geminiResp.Error.Message)
	}

	if len(geminiResp.Candidates) == 0 {
		return "", fmt.Errorf("Gemini API返回空响应")
	}

	if len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini API返回空内容")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// AnalyzeMarket 分析市场
func (gs *GeminiService) AnalyzeMarket(ctx context.Context, req *MarketAnalysisRequest) (*MarketAnalysisResponse, error) {
	prompt := gs.buildMarketAnalysisPrompt(req)
	
	response, err := gs.callAPI(ctx, prompt)
	if err != nil {
		return nil, err
	}

	return gs.parseMarketAnalysisResponse(response)
}

// OptimizeParameters 优化参数
func (gs *GeminiService) OptimizeParameters(ctx context.Context, req *ParameterOptimizationRequest) (*ParameterOptimizationResponse, error) {
	prompt := gs.buildParameterOptimizationPrompt(req)
	
	response, err := gs.callAPI(ctx, prompt)
	if err != nil {
		return nil, err
	}

	return gs.parseParameterOptimizationResponse(response)
}

// AnalyzeRisk 分析风险
func (gs *GeminiService) AnalyzeRisk(ctx context.Context, req *RiskAnalysisRequest) (*RiskAnalysisResponse, error) {
	prompt := gs.buildRiskAnalysisPrompt(req)
	
	response, err := gs.callAPI(ctx, prompt)
	if err != nil {
		return nil, err
	}

	return gs.parseRiskAnalysisResponse(response)
}

// AnalyzeSentiment 分析情绪
func (gs *GeminiService) AnalyzeSentiment(ctx context.Context, req *SentimentAnalysisRequest) (*SentimentAnalysisResponse, error) {
	prompt := gs.buildSentimentAnalysisPrompt(req)
	
	response, err := gs.callAPI(ctx, prompt)
	if err != nil {
		return nil, err
	}

	return gs.parseSentimentAnalysisResponse(response)
}

// GenerateStrategy 生成策略
func (gs *GeminiService) GenerateStrategy(ctx context.Context, description string, constraints map[string]interface{}) (string, error) {
	prompt := fmt.Sprintf(`你是一个专业的量化交易策略开发专家。请根据以下描述和约束条件，生成一个Go语言实现的交易策略。

描述: %s

约束条件: %v

请返回完整的Go代码，包括策略结构体、初始化方法、价格变化处理方法和订单更新处理方法。`, description, constraints)

	return gs.callAPI(ctx, prompt)
}

// Close 关闭服务
func (gs *GeminiService) Close() error {
	return nil
}

// buildMarketAnalysisPrompt 构建市场分析Prompt
func (gs *GeminiService) buildMarketAnalysisPrompt(req *MarketAnalysisRequest) string {
	var template, systemPrompt string
	var err error
	
	if gs.promptManager != nil {
		template, systemPrompt, err = gs.promptManager.GetPrompt("market_analysis")
		if err != nil {
			logger.Warn("⚠️ 获取提示词模板失败，使用默认模板: %v", err)
		}
	}
	
	// 如果模板为空，使用默认模板
	if template == "" {
		template = `你是一个专业的加密货币市场分析师。请分析以下市场数据并给出专业的市场分析。

交易对: %s
当前价格: %.2f
成交量: %.2f

请分析市场趋势（up/down/side）、给出价格预测、交易信号（buy/sell/hold）以及分析理由。

请以JSON格式返回，格式如下：
{
  "trend": "up|down|side",
  "confidence": 0.0-1.0,
  "signal": "buy|sell|hold",
  "reasoning": "分析理由"
}`
	}
	
	_ = systemPrompt // 暂时未使用系统提示词
	
	return fmt.Sprintf(template, req.Symbol, req.CurrentPrice, req.Volume)
}

// parseMarketAnalysisResponse 解析市场分析响应
func (gs *GeminiService) parseMarketAnalysisResponse(response string) (*MarketAnalysisResponse, error) {
	// 尝试提取JSON
	jsonStr := extractJSON(response)
	
	var result MarketAnalysisResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// 如果解析失败，尝试从文本中提取信息
		logger.Warn("⚠️ 无法解析AI响应为JSON，使用默认值: %v", err)
		result = MarketAnalysisResponse{
			Trend:      "side",
			Confidence: 0.5,
			Signal:     "hold",
			Reasoning:  response,
		}
	}

	return &result, nil
}

// buildParameterOptimizationPrompt 构建参数优化Prompt
func (gs *GeminiService) buildParameterOptimizationPrompt(req *ParameterOptimizationRequest) string {
	var template, systemPrompt string
	var err error
	
	if gs.promptManager != nil {
		template, systemPrompt, err = gs.promptManager.GetPrompt("parameter_optimization")
		if err != nil {
			logger.Warn("⚠️ 获取提示词模板失败，使用默认模板: %v", err)
		}
	}
	
	// 如果模板为空，使用默认模板
	if template == "" {
		template = `你是一个专业的量化交易参数优化专家。请根据以下数据优化交易参数。

当前参数:
- 价格间隔: %.2f
- 买单窗口: %d
- 卖单窗口: %d
- 订单金额: %.2f

历史表现:
- 总交易数: %d
- 胜率: %.2f%%
- 总盈亏: %.2f
- 最大回撤: %.2f%%

请推荐优化的参数值，并说明预期改进和理由。

请以JSON格式返回，格式如下：
{
  "recommended_params": {
    "price_interval": 数值,
    "buy_window_size": 数值,
    "sell_window_size": 数值,
    "order_quantity": 数值
  },
  "expected_improvement": 预期改进百分比,
  "confidence": 0.0-1.0,
  "reasoning": "优化理由"
}`
	}
	
	_ = systemPrompt // 暂时未使用系统提示词
	
	return fmt.Sprintf(template, req.CurrentParams.PriceInterval, req.CurrentParams.BuyWindowSize, 
		req.CurrentParams.SellWindowSize, req.CurrentParams.OrderQuantity,
		req.Performance.TotalTrades, req.Performance.WinRate*100,
		req.Performance.TotalPnL, req.Performance.MaxDrawdown*100)
}

// parseParameterOptimizationResponse 解析参数优化响应
func (gs *GeminiService) parseParameterOptimizationResponse(response string) (*ParameterOptimizationResponse, error) {
	jsonStr := extractJSON(response)
	
	var result ParameterOptimizationResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		logger.Warn("⚠️ 无法解析AI响应为JSON: %v", err)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &result, nil
}

// buildRiskAnalysisPrompt 构建风险分析Prompt
func (gs *GeminiService) buildRiskAnalysisPrompt(req *RiskAnalysisRequest) string {
	var template, systemPrompt string
	var err error
	
	if gs.promptManager != nil {
		template, systemPrompt, err = gs.promptManager.GetPrompt("risk_analysis")
		if err != nil {
			logger.Warn("⚠️ 获取提示词模板失败，使用默认模板: %v", err)
		}
	}
	
	// 如果模板为空，使用默认模板
	if template == "" {
		template = `你是一个专业的风险管理专家。请分析以下交易风险。

交易对: %s
当前价格: %.2f
账户余额: %.2f
已用保证金: %.2f
市场波动率: %.2f%%
持仓数量: %d
未完成订单: %d

请评估风险等级（low/medium/high/critical）、风险评分（0-1）、警告和建议。

请以JSON格式返回，格式如下：
{
  "risk_score": 0.0-1.0,
  "risk_level": "low|medium|high|critical",
  "warnings": ["警告1", "警告2"],
  "recommendations": ["建议1", "建议2"],
  "reasoning": "分析理由"
}`
	}
	
	_ = systemPrompt // 暂时未使用系统提示词
	
	return fmt.Sprintf(template, req.Symbol, req.CurrentPrice, req.AccountBalance, req.UsedMargin,
		req.MarketVolatility*100, len(req.Positions), req.OpenOrders)
}

// parseRiskAnalysisResponse 解析风险分析响应
func (gs *GeminiService) parseRiskAnalysisResponse(response string) (*RiskAnalysisResponse, error) {
	jsonStr := extractJSON(response)
	
	var result RiskAnalysisResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		logger.Warn("⚠️ 无法解析AI响应为JSON: %v", err)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &result, nil
}

// buildSentimentAnalysisPrompt 构建情绪分析Prompt
func (gs *GeminiService) buildSentimentAnalysisPrompt(req *SentimentAnalysisRequest) string {
	var template, systemPrompt string
	var err error
	
	if gs.promptManager != nil {
		template, systemPrompt, err = gs.promptManager.GetPrompt("sentiment_analysis")
		if err != nil {
			logger.Warn("⚠️ 获取提示词模板失败，使用默认模板: %v", err)
		}
	}
	
	// 如果模板为空，使用默认模板
	if template == "" {
		template = `你是一个专业的市场情绪分析师。请分析以下市场情绪数据。

交易对: %s

新闻摘要:
%s

%s%s

请分析市场情绪（-1到1，-1极度悲观，1极度乐观）、趋势（bullish/bearish/neutral）、关键因素和理由。

请以JSON格式返回，格式如下：
{
  "sentiment_score": -1.0到1.0,
  "trend": "bullish|bearish|neutral",
  "key_factors": ["因素1", "因素2"],
  "news_summary": "新闻摘要",
  "reasoning": "分析理由"
}`
	}
	
	_ = systemPrompt // 暂时未使用系统提示词
	
	// 构建数据部分
	newsSummary := ""
	for i, news := range req.NewsItems {
		if i >= 5 { // 只取前5条
			break
		}
		newsSummary += fmt.Sprintf("- %s: %s\n", news.Title, news.Description)
	}

	fearGreedInfo := ""
	if req.FearGreedIndex != nil {
		fearGreedInfo = fmt.Sprintf("恐慌贪婪指数: %d (%s)", req.FearGreedIndex.Value, req.FearGreedIndex.Classification)
	}

	redditSummary := ""
	if len(req.RedditPosts) > 0 {
		redditSummary = "\nReddit热门帖子:\n"
		for i, post := range req.RedditPosts {
			if i >= 5 { // 只取前5条
				break
			}
			redditSummary += fmt.Sprintf("- [r/%s] %s (分数: %d, 点赞率: %.1f%%)\n", 
				post.Subreddit, post.Title, post.Score, post.UpvoteRatio*100)
		}
	}

	return fmt.Sprintf(template, req.Symbol, newsSummary, fearGreedInfo, redditSummary)
}

// parseSentimentAnalysisResponse 解析情绪分析响应
func (gs *GeminiService) parseSentimentAnalysisResponse(response string) (*SentimentAnalysisResponse, error) {
	jsonStr := extractJSON(response)
	
	var result SentimentAnalysisResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		logger.Warn("⚠️ 无法解析AI响应为JSON: %v", err)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &result, nil
}

// extractJSON 从文本中提取JSON
func extractJSON(text string) string {
	// 查找第一个 { 和最后一个 }
	start := -1
	end := -1
	
	for i, r := range text {
		if r == '{' && start == -1 {
			start = i
		}
		if r == '}' {
			end = i
		}
	}
	
	if start != -1 && end != -1 && end > start {
		return text[start : end+1]
	}
	
	// 如果没有找到JSON，返回原始文本
	return text
}

