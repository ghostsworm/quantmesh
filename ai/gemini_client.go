package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GeminiClient Gemini API 客户端
type GeminiClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewGeminiClient 创建 Gemini 客户端
func NewGeminiClient(apiKey string) *GeminiClient {
	return &GeminiClient{
		apiKey:  apiKey,
		baseURL: "https://generativelanguage.googleapis.com/v1beta",
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // 增加超时时间
		},
	}
}

// GenerateConfigRequest AI 配置生成请求
type GenerateConfigRequest struct {
	Exchange      string             `json:"exchange"`
	Symbols       []string           `json:"symbols"`
	TotalCapital  float64            `json:"total_capital"`
	RiskProfile   string             `json:"risk_profile"` // conservative/balanced/aggressive
	CurrentPrices map[string]float64 `json:"current_prices"`
}

// GenerateConfigResponse AI 配置生成响应
type GenerateConfigResponse struct {
	Explanation string                  `json:"explanation"`
	GridConfig  []SymbolGridConfig     `json:"grid_config"`
	Allocation  []SymbolAllocationConfig `json:"allocation"`
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

// GenerateConfig 生成配置建议
func (c *GeminiClient) GenerateConfig(ctx context.Context, req *GenerateConfigRequest) (*GenerateConfigResponse, error) {
	prompt := c.buildPrompt(req)

	// 定义 JSON Schema 确保输出格式正确
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"explanation": map[string]interface{}{
				"type":        "string",
				"description": "配置方案的详细解释，包括设计思路和风险提示",
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
		"required": []string{"explanation", "grid_config", "allocation"},
	}

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
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 使用 gemini-3-flash-preview 模型（最新版本，更快、更便宜）
	url := fmt.Sprintf("%s/models/gemini-3-flash-preview:generateContent?key=%s", c.baseURL, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
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

	// 解析 AI 返回的 JSON（使用 JSON Schema 后格式更可靠）
	aiText := geminiResp.Candidates[0].Content.Parts[0].Text

	// 清理可能的 markdown 代码块标记（虽然使用了 JSON Schema，但以防万一）
	aiText = strings.TrimPrefix(aiText, "```json")
	aiText = strings.TrimPrefix(aiText, "```")
	aiText = strings.TrimSuffix(aiText, "```")
	aiText = strings.TrimSpace(aiText)

	var result GenerateConfigResponse
	if err := json.Unmarshal([]byte(aiText), &result); err != nil {
		return nil, fmt.Errorf("解析 AI 配置失败: %w (响应: %s)", err, aiText)
	}

	return &result, nil
}

// buildPrompt 构建提示词
func (c *GeminiClient) buildPrompt(req *GenerateConfigRequest) string {
	riskDesc := map[string]string{
		"conservative": "保守型（低风险，稳健收益）",
		"balanced":     "平衡型（中等风险，适中收益）",
		"aggressive":   "激进型（高风险，追求高收益）",
	}[req.RiskProfile]

	prompt := fmt.Sprintf(`你是一个加密货币网格交易专家。请根据以下信息，为用户设计一套网格交易和资金分配方案：

交易所：%s
交易币种：%v
可用资金：%.2f USDT
风险偏好：%s

当前价格信息：
`, req.Exchange, req.Symbols, req.TotalCapital, riskDesc)

	for symbol, price := range req.CurrentPrices {
		prompt += fmt.Sprintf("- %s: $%.2f\n", symbol, price)
	}

	prompt += `
请提供：
1. 每个币种的网格参数（价格间隔、买卖窗口大小、每单金额）
2. 每个币种的资金分配限额（USDT金额和百分比）
3. 网格风控参数（用于防止死扛和过度亏损）：
   - max_grid_layers: 最大持仓层数（建议：保守型 10-15，平衡型 15-20，激进型 20-30）
   - stop_loss_ratio: 硬止损比例（建议：保守型 0.05-0.08，平衡型 0.08-0.12，激进型 0.12-0.15）
   - take_profit_trigger_ratio: 盈利触发回撤监控的阈值（建议：0.05-0.10，即盈利5-10%%后开始监控回撤）
   - trailing_take_profit_ratio: 盈利回撤止盈比例（建议：0.02-0.05，即从最高点回撤2-5%%时止盈）
   - trend_filter_enabled: 是否启用趋势过滤（建议：保守型和平衡型启用，激进型可选）
4. 配置方案的解释和风险提示

要求：
- 价格间隔应根据币种价格和波动性合理设置
- 资金分配要均衡，避免单一币种占用过多资金
- 考虑用户的风险偏好调整参数
- 确保所有币种的资金总和不超过可用资金的90%%（留10%%作为缓冲）
- 风控参数应根据风险偏好设置：保守型更严格，激进型更宽松

请返回 JSON 格式的配置方案，包含：
- explanation: 配置方案的详细解释（200-500字）
- grid_config: 数组，每个元素包含 exchange, symbol, price_interval, order_quantity, buy_window_size, sell_window_size, grid_risk_control
- allocation: 数组，每个元素包含 exchange, symbol, max_amount_usdt, max_percentage

注意事项：
- 价格间隔建议：BTC 约为价格的 0.1-0.3%%，ETH 约为 0.2-0.5%%，其他币种根据波动性调整
- 窗口大小：保守型 15-25，平衡型 25-35，激进型 35-50
- 每单金额：总资金除以预期最大持仓数（通常是买单窗口大小的 1.5-2 倍）
- 风控参数说明：
  * max_grid_layers: 限制最大买入层数，防止在单边下跌时无限加仓
  * stop_loss_ratio: 当浮亏达到此比例时强制平仓，避免死扛
  * take_profit_trigger_ratio + trailing_take_profit_ratio: 盈利回撤止盈机制，锁定已有收益
  * trend_filter_enabled: 在下跌趋势中暂停买入，从源头减少死扛风险`

	return prompt
}

