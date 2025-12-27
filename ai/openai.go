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

// OpenAIService OpenAI AI服务实现
type OpenAIService struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// OpenAIRequest OpenAI API请求结构
type OpenAIRequest struct {
	Model    string    `json:"model"`
	Messages []OpenAIMessage `json:"messages"`
	Temperature float64 `json:"temperature,omitempty"`
	MaxTokens int      `json:"max_tokens,omitempty"`
}

// OpenAIMessage 消息
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OpenAIResponse OpenAI API响应
type OpenAIResponse struct {
	Choices []OpenAIChoice `json:"choices"`
	Error   *OpenAIError   `json:"error,omitempty"`
}

// OpenAIChoice 选择
type OpenAIChoice struct {
	Message OpenAIMessage `json:"message"`
}

// OpenAIError OpenAI错误
type OpenAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// NewOpenAIService 创建OpenAI服务
func NewOpenAIService(apiKey string, baseURL string) (*OpenAIService, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("OpenAI API Key不能为空")
	}

	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	return &OpenAIService{
		apiKey:  apiKey,
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// callAPI 调用OpenAI API
func (os *OpenAIService) callAPI(ctx context.Context, messages []OpenAIMessage) (string, error) {
	url := fmt.Sprintf("%s/chat/completions", os.baseURL)

	reqBody := OpenAIRequest{
		Model:       "gpt-4o-mini", // 使用更经济的模型
		Messages:    messages,
		Temperature: 0.7,
		MaxTokens:   2000,
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
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.apiKey))

	resp, err := os.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var openAIErr OpenAIError
		if err := json.Unmarshal(body, &openAIErr); err == nil {
			return "", fmt.Errorf("OpenAI API错误: %s", openAIErr.Message)
		}
		return "", fmt.Errorf("HTTP错误: %d, 响应: %s", resp.StatusCode, string(body))
	}

	var openAIResp OpenAIResponse
	if err := json.Unmarshal(body, &openAIResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if openAIResp.Error != nil {
		return "", fmt.Errorf("OpenAI API错误: %s", openAIResp.Error.Message)
	}

	if len(openAIResp.Choices) == 0 {
		return "", fmt.Errorf("OpenAI API返回空响应")
	}

	return openAIResp.Choices[0].Message.Content, nil
}

// AnalyzeMarket 分析市场
func (os *OpenAIService) AnalyzeMarket(ctx context.Context, req *MarketAnalysisRequest) (*MarketAnalysisResponse, error) {
	prompt := os.buildMarketAnalysisPrompt(req)
	messages := []OpenAIMessage{
		{Role: "system", Content: "你是一个专业的加密货币市场分析师。"},
		{Role: "user", Content: prompt},
	}
	
	response, err := os.callAPI(ctx, messages)
	if err != nil {
		return nil, err
	}

	return os.parseMarketAnalysisResponse(response)
}

// OptimizeParameters 优化参数
func (os *OpenAIService) OptimizeParameters(ctx context.Context, req *ParameterOptimizationRequest) (*ParameterOptimizationResponse, error) {
	prompt := os.buildParameterOptimizationPrompt(req)
	messages := []OpenAIMessage{
		{Role: "system", Content: "你是一个专业的量化交易参数优化专家。"},
		{Role: "user", Content: prompt},
	}
	
	response, err := os.callAPI(ctx, messages)
	if err != nil {
		return nil, err
	}

	return os.parseParameterOptimizationResponse(response)
}

// AnalyzeRisk 分析风险
func (os *OpenAIService) AnalyzeRisk(ctx context.Context, req *RiskAnalysisRequest) (*RiskAnalysisResponse, error) {
	prompt := os.buildRiskAnalysisPrompt(req)
	messages := []OpenAIMessage{
		{Role: "system", Content: "你是一个专业的风险管理专家。"},
		{Role: "user", Content: prompt},
	}
	
	response, err := os.callAPI(ctx, messages)
	if err != nil {
		return nil, err
	}

	return os.parseRiskAnalysisResponse(response)
}

// AnalyzeSentiment 分析情绪
func (os *OpenAIService) AnalyzeSentiment(ctx context.Context, req *SentimentAnalysisRequest) (*SentimentAnalysisResponse, error) {
	prompt := os.buildSentimentAnalysisPrompt(req)
	messages := []OpenAIMessage{
		{Role: "system", Content: "你是一个专业的市场情绪分析师。"},
		{Role: "user", Content: prompt},
	}
	
	response, err := os.callAPI(ctx, messages)
	if err != nil {
		return nil, err
	}

	return os.parseSentimentAnalysisResponse(response)
}

// GenerateStrategy 生成策略
func (os *OpenAIService) GenerateStrategy(ctx context.Context, description string, constraints map[string]interface{}) (string, error) {
	prompt := fmt.Sprintf(`你是一个专业的量化交易策略开发专家。请根据以下描述和约束条件，生成一个Go语言实现的交易策略。

描述: %s

约束条件: %v

请返回完整的Go代码，包括策略结构体、初始化方法、价格变化处理方法和订单更新处理方法。`, description, constraints)

	messages := []OpenAIMessage{
		{Role: "system", Content: "你是一个专业的量化交易策略开发专家。"},
		{Role: "user", Content: prompt},
	}

	return os.callAPI(ctx, messages)
}

// Close 关闭服务
func (os *OpenAIService) Close() error {
	return nil
}

// buildMarketAnalysisPrompt 构建市场分析Prompt（与Gemini相同）
func (os *OpenAIService) buildMarketAnalysisPrompt(req *MarketAnalysisRequest) string {
	return fmt.Sprintf(`请分析以下市场数据并给出专业的市场分析。

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
}`, req.Symbol, req.CurrentPrice, req.Volume)
}

// parseMarketAnalysisResponse 解析市场分析响应（与Gemini相同）
func (os *OpenAIService) parseMarketAnalysisResponse(response string) (*MarketAnalysisResponse, error) {
	jsonStr := extractJSON(response)
	
	var result MarketAnalysisResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
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

// buildParameterOptimizationPrompt 构建参数优化Prompt（与Gemini相同）
func (os *OpenAIService) buildParameterOptimizationPrompt(req *ParameterOptimizationRequest) string {
	return fmt.Sprintf(`请根据以下数据优化交易参数。

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
}`, req.CurrentParams.PriceInterval, req.CurrentParams.BuyWindowSize, 
		req.CurrentParams.SellWindowSize, req.CurrentParams.OrderQuantity,
		req.Performance.TotalTrades, req.Performance.WinRate*100,
		req.Performance.TotalPnL, req.Performance.MaxDrawdown*100)
}

// parseParameterOptimizationResponse 解析参数优化响应（与Gemini相同）
func (os *OpenAIService) parseParameterOptimizationResponse(response string) (*ParameterOptimizationResponse, error) {
	jsonStr := extractJSON(response)
	
	var result ParameterOptimizationResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		logger.Warn("⚠️ 无法解析AI响应为JSON: %v", err)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &result, nil
}

// buildRiskAnalysisPrompt 构建风险分析Prompt（与Gemini相同）
func (os *OpenAIService) buildRiskAnalysisPrompt(req *RiskAnalysisRequest) string {
	return fmt.Sprintf(`请分析以下交易风险。

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
}`, req.Symbol, req.CurrentPrice, req.AccountBalance, req.UsedMargin,
		req.MarketVolatility*100, len(req.Positions), req.OpenOrders)
}

// parseRiskAnalysisResponse 解析风险分析响应（与Gemini相同）
func (os *OpenAIService) parseRiskAnalysisResponse(response string) (*RiskAnalysisResponse, error) {
	jsonStr := extractJSON(response)
	
	var result RiskAnalysisResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		logger.Warn("⚠️ 无法解析AI响应为JSON: %v", err)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &result, nil
}

// buildSentimentAnalysisPrompt 构建情绪分析Prompt（与Gemini相同）
func (os *OpenAIService) buildSentimentAnalysisPrompt(req *SentimentAnalysisRequest) string {
	newsSummary := ""
	for i, news := range req.NewsItems {
		if i >= 5 {
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

	return fmt.Sprintf(`请分析以下市场情绪数据。

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
}`, req.Symbol, newsSummary, fearGreedInfo, redditSummary)
}

// parseSentimentAnalysisResponse 解析情绪分析响应（与Gemini相同）
func (os *OpenAIService) parseSentimentAnalysisResponse(response string) (*SentimentAnalysisResponse, error) {
	jsonStr := extractJSON(response)
	
	var result SentimentAnalysisResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		logger.Warn("⚠️ 无法解析AI响应为JSON: %v", err)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &result, nil
}

