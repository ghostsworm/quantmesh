package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"quantmesh/config"
)

// GeminiClient Gemini API 客户端接口
type GeminiClient interface {
	GenerateConfig(ctx context.Context, req *GenerateConfigRequest) (*GenerateConfigResponse, error)
	GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}) (string, error)
}

// NativeGeminiClient 原生 Gemini API 客户端（直接访问 Google）
type NativeGeminiClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// ProxyGeminiClient 代理 Gemini API 客户端（通过中转服务）
type ProxyGeminiClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewGeminiClient 创建 Gemini 客户端（根据配置选择实现方式）
func NewGeminiClient(apiKey string, accessMode string, proxyBaseURL string, proxyUsername string, proxyPassword string) GeminiClient {
	if accessMode == "proxy" {
		if proxyBaseURL == "" {
			proxyBaseURL = "https://gemini.facev.app"
		}
		return &ProxyGeminiClient{
			apiKey:     apiKey,
			baseURL:    proxyBaseURL,
			httpClient: &http.Client{Timeout: 120 * time.Second},
		}
	}

	// 默认使用原生方式
	return &NativeGeminiClient{
		apiKey:     apiKey,
		baseURL:    "https://generativelanguage.googleapis.com/v1beta",
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// SymbolCapitalConfig 币种资金配置
type SymbolCapitalConfig struct {
	Symbol  string  `json:"symbol"`
	Capital float64 `json:"capital"`
}

// GenerateConfigRequest AI 配置生成请求
type GenerateConfigRequest struct {
	Exchange          string                             `json:"exchange"`
	Symbols           []string                           `json:"symbols"`
	TotalCapital      float64                            `json:"total_capital,omitempty"`      // 总金额模式时使用
	SymbolCapitals    []SymbolCapitalConfig              `json:"symbol_capitals,omitempty"`   // 按币种分配模式时使用
	CapitalMode       string                             `json:"capital_mode"`                 // total 或 per_symbol
	RiskProfile       string                             `json:"risk_profile"`                 // conservative/balanced/aggressive
	CurrentPrices     map[string]float64                 `json:"current_prices"`
	SymbolAllocations map[string]float64                 `json:"symbol_allocations,omitempty"` // 币种比例分配
	StrategySplits    map[string][]config.StrategyInstance `json:"strategy_splits,omitempty"`    // 策略分配
	WithdrawalPolicy  config.WithdrawalPolicy            `json:"withdrawal_policy,omitempty"`  // 提现策略
}

// GenerateConfigResponse AI 配置生成响应
type GenerateConfigResponse struct {
	Explanation   string                   `json:"explanation"`
	GridConfig    []SymbolGridConfig       `json:"grid_config"`
	Allocation    []SymbolAllocationConfig  `json:"allocation"`
	SymbolsConfig []config.SymbolConfig    `json:"symbols_config"` // 包含分级资产配置后的完整币种配置
}

// SymbolGridConfig 币种网格配置
type SymbolGridConfig struct {
	Exchange       string  `json:"exchange"`
	Symbol         string  `json:"symbol"`
	PriceInterval  float64 `json:"price_interval"`
	OrderQuantity  float64 `json:"order_quantity"`
	BuyWindowSize  int     `json:"buy_window_size"`
	SellWindowSize int     `json:"sell_window_size"`
	// 网格风控参数（可选）
	GridRiskControl *GridRiskControlConfig `json:"grid_risk_control,omitempty"`
}

// GridRiskControlConfig 网格风控配置
type GridRiskControlConfig struct {
	Enabled                 bool    `json:"enabled"`
	MaxGridLayers           int     `json:"max_grid_layers"`
	StopLossRatio           float64 `json:"stop_loss_ratio"`
	TakeProfitTriggerRatio  float64 `json:"take_profit_trigger_ratio"`
	TrailingTakeProfitRatio float64 `json:"trailing_take_profit_ratio"`
	TrendFilterEnabled      bool    `json:"trend_filter_enabled"`
}

// SymbolAllocationConfig 币种资金分配配置
type SymbolAllocationConfig struct {
	Exchange      string  `json:"exchange"`
	Symbol        string  `json:"symbol"`
	MaxAmountUSDT float64 `json:"max_amount_usdt"`
	MaxPercentage float64 `json:"max_percentage"`
}

// GenerateConfig 生成配置建议（原生实现）
func (c *NativeGeminiClient) GenerateConfig(ctx context.Context, req *GenerateConfigRequest) (*GenerateConfigResponse, error) {
	prompt := buildPrompt(req)
	schema := buildConfigSchema()

	aiText, err := c.GenerateContent(ctx, prompt, schema)
	if err != nil {
		return nil, err
	}

	var result GenerateConfigResponse
	if err := json.Unmarshal([]byte(aiText), &result); err != nil {
		return nil, fmt.Errorf("解析 AI 配置失败: %w (响应: %s)", err, aiText)
	}

	return &result, nil
}

// GenerateContent 生成内容（原生实现）
func (c *NativeGeminiClient) GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}) (string, error) {
	geminiReq := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]interface{}{
					{"text": prompt},
				},
			},
		},
		"generationConfig": map[string]interface{}{
			"temperature":      0.7,
			"topK":             40,
			"topP":             0.95,
			"responseMimeType": "application/json",
			"responseSchema":   schema,
		},
	}

	jsonData, err := json.Marshal(geminiReq)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	// 使用 gemini-3-flash-preview 模型（最新版本，更快、更便宜）
	url := fmt.Sprintf("%s/models/gemini-3-flash-preview:generateContent?key=%s", c.baseURL, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API 返回错误: %d - %s", resp.StatusCode, string(body))
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
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("AI 未返回有效响应")
	}

	aiText := geminiResp.Candidates[0].Content.Parts[0].Text
	aiText = strings.TrimPrefix(aiText, "```json")
	aiText = strings.TrimPrefix(aiText, "```")
	aiText = strings.TrimSuffix(aiText, "```")
	aiText = strings.TrimSpace(aiText)

	return aiText, nil
}

// GenerateConfig 生成配置建议（代理实现）
func (c *ProxyGeminiClient) GenerateConfig(ctx context.Context, req *GenerateConfigRequest) (*GenerateConfigResponse, error) {
	prompt := buildPrompt(req)
	schema := buildConfigSchema()

	aiText, err := c.GenerateContent(ctx, prompt, schema)
	if err != nil {
		return nil, err
	}

	var result GenerateConfigResponse
	if err := json.Unmarshal([]byte(aiText), &result); err != nil {
		return nil, fmt.Errorf("解析 AI 配置失败: %w (响应: %s)", err, aiText)
	}

	return &result, nil
}

// GenerateContent 生成内容（代理实现）
func (c *ProxyGeminiClient) GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}) (string, error) {
	// 将 schema 转换为 JSON 字符串
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("序列化 schema 失败: %w", err)
	}

	// 构建请求体
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加 prompt
	writer.WriteField("system_instruction", prompt)
	writer.WriteField("prompt", prompt)
	writer.WriteField("model", "gemini-2.5-flash")
	writer.WriteField("gemini_api_key", c.apiKey)
	writer.WriteField("json_schema", string(schemaJSON))
	
	// 开启异步模式
	writer.WriteField("async_mode", "1")
	writer.WriteField("timeout_seconds", "900")
	writer.WriteField("max_retries", "3")

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("关闭 multipart writer 失败: %w", err)
	}

	// 1. 发送异步任务请求
	url := fmt.Sprintf("%s/api/analyze-image", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, body)
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取完整响应体
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("API 返回错误: %d - %s", resp.StatusCode, string(respBody))
	}

	// 检查响应是否为 HTML（错误页面）
	respStr := strings.TrimSpace(string(respBody))
	if strings.HasPrefix(respStr, "<") || strings.HasPrefix(respStr, "<!") {
		truncated := respStr
		if len(truncated) > 200 {
			truncated = truncated[:200]
		}
		return "", fmt.Errorf("代理服务返回了 HTML 错误页面，请检查代理地址和认证信息: %s", truncated)
	}

	var proxyResp struct {
		TaskID  string `json:"task_id"`
		Status  string `json:"status"`
		Error   string `json:"error"`
		Message string `json:"message"`
		// 同步模式可能直接返回结果
		Text   string                 `json:"text"`
		Result map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &proxyResp); err != nil {
		truncated := respStr
		if len(truncated) > 500 {
			truncated = truncated[:500]
		}
		return "", fmt.Errorf("解析响应失败: %w (响应: %s)", err, truncated)
	}

	// 检查是否有错误
	if proxyResp.Error != "" {
		return "", fmt.Errorf("代理服务错误: %s", proxyResp.Error)
	}

	// 同步模式：直接返回结果
	if proxyResp.Text != "" {
		resultText := proxyResp.Text
		resultText = strings.TrimPrefix(resultText, "```json")
		resultText = strings.TrimPrefix(resultText, "```")
		resultText = strings.TrimSuffix(resultText, "```")
		return strings.TrimSpace(resultText), nil
	}

	// 异步模式：需要轮询
	if proxyResp.TaskID == "" {
		truncated := respStr
		if len(truncated) > 500 {
			truncated = truncated[:500]
		}
		return "", fmt.Errorf("未获取到任务 ID，响应: %s", truncated)
	}

	// 2. 轮询任务结果
	taskID := proxyResp.TaskID
	maxPolls := 60 // 最多轮询 60 次 (约 2 分钟)
	pollInterval := 2 * time.Second

	for i := 0; i < maxPolls; i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(pollInterval):
			statusURL := fmt.Sprintf("%s/api/task/%s", c.baseURL, taskID)
			statusReq, _ := http.NewRequestWithContext(ctx, "GET", statusURL, nil)

			statusResp, err := c.httpClient.Do(statusReq)
			if err != nil {
				continue // 网络错误重试
			}
			defer statusResp.Body.Close()

			var statusData map[string]interface{}
			if err := json.NewDecoder(statusResp.Body).Decode(&statusData); err != nil {
				continue
			}

			status, _ := statusData["status"].(string)
			if status == "completed" || status == "success" {
				// 提取结果
				var resultText string
				if data, ok := statusData["data"].(map[string]interface{}); ok {
					if text, ok := data["text"].(string); ok {
						resultText = text
					} else if result, ok := data["result"].(string); ok {
						resultText = result
					}
				} else if text, ok := statusData["text"].(string); ok {
					resultText = text
				} else if result, ok := statusData["result"].(string); ok {
					resultText = result
				}

				if resultText != "" {
					resultText = strings.TrimPrefix(resultText, "```json")
					resultText = strings.TrimPrefix(resultText, "```")
					resultText = strings.TrimSuffix(resultText, "```")
					return strings.TrimSpace(resultText), nil
				}
			} else if status == "failed" || status == "error" {
				return "", fmt.Errorf("任务执行失败: %v", statusData["error"])
			}
			// 其他状态继续轮询
		}
	}

	return "", fmt.Errorf("任务处理超时 (TaskID: %s)", taskID)
}

// buildPrompt 构建提示词
func buildPrompt(req *GenerateConfigRequest) string {
	riskDesc := map[string]string{
		"conservative": "保守型（低风险，稳健收益）",
		"balanced":     "平衡型（中等风险，适中收益）",
		"aggressive":   "激进型（高风险，追求高收益）",
	}[req.RiskProfile]

	var capitalInfo string
	var totalCapital float64

	if req.CapitalMode == "per_symbol" && len(req.SymbolCapitals) > 0 {
		// 按币种分配模式
		capitalInfo = "资金配置模式：按币种分配\n各币种资金分配：\n"
		for _, sc := range req.SymbolCapitals {
			capitalInfo += fmt.Sprintf("- %s: %.2f USDT\n", sc.Symbol, sc.Capital)
			totalCapital += sc.Capital
		}
		capitalInfo += fmt.Sprintf("总计资金：%.2f USDT\n", totalCapital)
	} else {
		// 总金额模式
		totalCapital = req.TotalCapital
		capitalInfo = fmt.Sprintf("资金配置模式：总金额分配\n可用资金：%.2f USDT", totalCapital)
	}

	// 添加资产优先分配信息
	var assetAllocInfo string
	if len(req.SymbolAllocations) > 0 {
		assetAllocInfo = "\n用户预设资产分配比例：\n"
		for symbol, weight := range req.SymbolAllocations {
			assetAllocInfo += fmt.Sprintf("- %s: %.1f%%\n", symbol, weight*100)
		}
	}

	// 添加策略组合信息
	var strategySplitInfo string
	if len(req.StrategySplits) > 0 {
		strategySplitInfo = "\n用户预设策略组合：\n"
		for symbol, strategies := range req.StrategySplits {
			strategySplitInfo += fmt.Sprintf("- %s: ", symbol)
			for i, s := range strategies {
				if i > 0 {
					strategySplitInfo += " + "
				}
				strategySplitInfo += fmt.Sprintf("%s(%.0f%%)", s.Type, s.Weight*100)
			}
			strategySplitInfo += "\n"
		}
	}

	prompt := fmt.Sprintf(`你是一个加密货币交易专家，擅长多策略资产配置。请根据以下信息，为用户设计一套分级的量化交易配置方案：

交易所：%s
交易币种：%v
%s
%s
%s
风险偏好：%s

当前价格信息：
`, req.Exchange, req.Symbols, capitalInfo, assetAllocInfo, strategySplitInfo, riskDesc)

	for symbol, price := range req.CurrentPrices {
		prompt += fmt.Sprintf("- %s: $%.2f\n", symbol, price)
	}

	prompt += `
请提供一个详细的配置方案，要求：
1. **资产分配层**：为每个币种设定 symbol_config，包括其分配的总资金 (total_allocated_capital)。
2. **策略组合层**：为每个币种配置 strategies 列表。如果用户已提供策略权重，请在此基础上优化参数。
3. **参数细节层**：
   - 对于网格策略 (grid)，请提供价格间隔 (price_interval)、买卖窗口大小、每单金额等。
   - 考虑波动率设置合理的网格风控。
4. **提现策略层**：根据用户提供的提现策略设置 (withdrawal_policy)，确认其合理性并集成到配置中。

请返回 JSON 格式的配置方案，必须符合以下结构：
{
  "explanation": "配置思路和风险提示...",
  "symbols_config": [
    {
      "symbol": "BTCUSDT",
      "total_allocated_capital": 5000,
      "withdrawal_policy": {"enabled": true, "threshold": 0.1},
      "strategies": [
        {"type": "grid", "weight": 0.7, "config": {"price_interval": 0.5, "order_quantity": 20, ...}},
        {"type": "dca", "weight": 0.3, "config": {...}}
      ],
      "price_interval": 0.5,
      "order_quantity": 20,
      "buy_window_size": 20,
      "sell_window_size": 20,
      "grid_risk_control": {...}
    }
  ]
}

要求：
- 解释应详细说明为什么这样分配资金和设置参数。
- 所有币种分配的总资金之和不能超过可用资金的 95%。
- 网格参数应根据风险偏好和当前币价计算默认值。
`

	return prompt
}

// buildConfigSchema 构建配置生成的 JSON Schema
func buildConfigSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"explanation": map[string]interface{}{
				"type":        "string",
				"description": "配置方案的详细解释，包括设计思路和风险提示",
			},
			"symbols_config": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"symbol": map[string]interface{}{
							"type": "string",
						},
						"total_allocated_capital": map[string]interface{}{
							"type": "number",
						},
						"price_interval": map[string]interface{}{
							"type": "number",
						},
						"order_quantity": map[string]interface{}{
							"type": "number",
						},
						"buy_window_size": map[string]interface{}{
							"type": "integer",
						},
						"sell_window_size": map[string]interface{}{
							"type": "integer",
						},
						"withdrawal_policy": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"enabled": map[string]interface{}{
									"type": "boolean",
								},
								"threshold": map[string]interface{}{
									"type": "number",
								},
							},
						},
						"strategies": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"type": map[string]interface{}{
										"type": "string",
									},
									"weight": map[string]interface{}{
										"type": "number",
									},
									"config": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											// 网格策略常见字段
											"grid_count": map[string]interface{}{
												"type": "number",
											},
											"upper_price": map[string]interface{}{
												"type": "number",
											},
											"lower_price": map[string]interface{}{
												"type": "number",
											},
											"total_amount": map[string]interface{}{
												"type": "number",
											},
											// DCA 策略常见字段
											"interval": map[string]interface{}{
												"type": "string",
											},
											"amount": map[string]interface{}{
												"type": "number",
											},
											"base_order_amount": map[string]interface{}{
												"type": "number",
											},
											"safety_order_amount": map[string]interface{}{
												"type": "number",
											},
											"max_safety_orders": map[string]interface{}{
												"type": "number",
											},
											"atr_period": map[string]interface{}{
												"type": "number",
											},
											"atr_multiplier": map[string]interface{}{
												"type": "number",
											},
											"total_take_profit": map[string]interface{}{
												"type": "number",
											},
											"stop_loss": map[string]interface{}{
												"type": "number",
											},
											// 其他可能的动态字段（使用通用类型）
											"parameters": map[string]interface{}{
												"type": "object",
												"properties": map[string]interface{}{
													"value": map[string]interface{}{
														"type": "number",
													},
												},
											},
										},
									},
								},
							},
						},
						"grid_risk_control": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"enabled": map[string]interface{}{
									"type": "boolean",
								},
								"max_grid_layers": map[string]interface{}{
									"type": "integer",
								},
								"stop_loss_ratio": map[string]interface{}{
									"type": "number",
								},
								"take_profit_trigger_ratio": map[string]interface{}{
									"type": "number",
								},
								"trailing_take_profit_ratio": map[string]interface{}{
									"type": "number",
								},
								"trend_filter_enabled": map[string]interface{}{
									"type": "boolean",
								},
							},
						},
					},
					"required": []string{"symbol", "total_allocated_capital", "price_interval", "order_quantity", "buy_window_size", "sell_window_size"},
				},
			},
			"grid_config": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"exchange": map[string]interface{}{
							"type": "string",
						},
						"symbol": map[string]interface{}{
							"type": "string",
						},
						"price_interval": map[string]interface{}{
							"type": "number",
						},
						"order_quantity": map[string]interface{}{
							"type": "number",
						},
						"buy_window_size": map[string]interface{}{
							"type": "integer",
						},
						"sell_window_size": map[string]interface{}{
							"type": "integer",
						},
						"grid_risk_control": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"enabled": map[string]interface{}{
									"type": "boolean",
								},
								"max_grid_layers": map[string]interface{}{
									"type": "integer",
								},
								"stop_loss_ratio": map[string]interface{}{
									"type": "number",
								},
								"take_profit_trigger_ratio": map[string]interface{}{
									"type": "number",
								},
								"trailing_take_profit_ratio": map[string]interface{}{
									"type": "number",
								},
								"trend_filter_enabled": map[string]interface{}{
									"type": "boolean",
								},
							},
						},
					},
					"required": []string{"exchange", "symbol", "price_interval", "order_quantity", "buy_window_size", "sell_window_size"},
				},
			},
			"allocation": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"exchange": map[string]interface{}{
							"type": "string",
						},
						"symbol": map[string]interface{}{
							"type": "string",
						},
						"max_amount_usdt": map[string]interface{}{
							"type": "number",
						},
						"max_percentage": map[string]interface{}{
							"type": "number",
						},
					},
					"required": []string{"exchange", "symbol", "max_amount_usdt", "max_percentage"},
				},
			},
		},
		"required": []string{"explanation", "symbols_config"},
	}
}
