package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"quantmesh/ai/service"
	"quantmesh/config"
	"quantmesh/logger"
)

// 全局任務服務，在 main.go 中初始化
var GlobalTaskService *service.TaskService

// GeminiClient Gemini API 客戶端接口
type GeminiClient interface {
	GenerateConfig(ctx context.Context, req *GenerateConfigRequest) (*GenerateConfigResponse, error)
	GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}) (string, error)
	GenerateContentWithGoogleSearch(ctx context.Context, prompt string, schema map[string]interface{}) (string, error)
}

// AsyncGeminiClient 异步 Gemini API 客戶端
type AsyncGeminiClient struct {
	apiKey string
}

// NewGeminiClient 創建 Gemini 客戶端（現在统一使用异步内置方式）
func NewGeminiClient(apiKey string) GeminiClient {
	return &AsyncGeminiClient{
		apiKey: apiKey,
	}
}

// GenerateConfig 生成配置建议
func (c *AsyncGeminiClient) GenerateConfig(ctx context.Context, req *GenerateConfigRequest) (*GenerateConfigResponse, error) {
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

// GenerateContent 生成内容（通過内置异步系统）
func (c *AsyncGeminiClient) GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}) (string, error) {
	return c.generateContentInternal(ctx, prompt, schema, false)
}

// GenerateContentWithGoogleSearch 生成内容（啟用 Google Search 實時搜索）
func (c *AsyncGeminiClient) GenerateContentWithGoogleSearch(ctx context.Context, prompt string, schema map[string]interface{}) (string, error) {
	return c.generateContentInternal(ctx, prompt, schema, true)
}

func (c *AsyncGeminiClient) generateContentInternal(ctx context.Context, prompt string, schema map[string]interface{}, useGoogleSearch bool) (string, error) {
	if GlobalTaskService == nil {
		return "", fmt.Errorf("AI 任務服務未初始化")
	}

	// 1. 創建异步任務
	requestData := map[string]interface{}{
		"prompt":             prompt,
		"system_instruction": prompt,
		"gemini_api_key":     c.apiKey,
		"json_schema":        schema,
		"model":              "gemini-3-flash-preview",
	}
	if useGoogleSearch {
		requestData["use_google_search"] = true
	}

	// 任務超時 10 分鐘；超時運行中的任務會由 processor 定期標記為 timeout
	taskID, err := GlobalTaskService.CreateTask(ctx, "generate_content", requestData, 600, 3)
	if err != nil {
		return "", fmt.Errorf("創建异步任務失败: %w", err)
	}

	logger.Info("🔄 已創建 AI 异步任務: %s，开始輪詢結果...", taskID)

	// 2. 輪詢任務結果
	maxPolls := 600 // 约 20 分钟（與任務超時 10 分鐘對齊後仍有餘量）
	pollInterval := 2 * time.Second

	for i := 0; i < maxPolls; i++ {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("任務被取消: %w", ctx.Err())
		case <-time.After(pollInterval):
			task, err := GlobalTaskService.GetTask(ctx, taskID)
			if err != nil {
				if i%10 == 0 {
					logger.Warn("⚠️ 輪詢任務 %s 失败 (第 %d 次): %v", taskID, i+1, err)
				}
				continue
			}

			if i%10 == 0 {
				logger.Info("🔄 輪詢任務 %s 状態: %s (第 %d 次)", taskID, task.Status, i+1)
			}

			if task.Status == "completed" {
				var resultData map[string]interface{}
				if err := json.Unmarshal([]byte(task.Result), &resultData); err != nil {
					return "", fmt.Errorf("解析任務結果失败: %w", err)
				}
				if text, ok := resultData["text"].(string); ok {
					return text, nil
				}
				return "", fmt.Errorf("任務結果中缺少文本内容")
			} else if task.Status == "failed" || task.Status == "timeout" {
				errMsg := "未知錯误"
				if task.ErrorMessage != nil {
					errMsg = *task.ErrorMessage
				}
				return "", fmt.Errorf("AI 任務執行失败: %s", errMsg)
			}
		}
	}

	return "", fmt.Errorf("AI 任務处理超時 (TaskID: %s)", taskID)
}

// SymbolCapitalConfig 币种资金配置
type SymbolCapitalConfig struct {
	Symbol  string  `json:"symbol"`
	Capital float64 `json:"capital"`
}

// GenerateConfigRequest AI 配置生成请求
type GenerateConfigRequest struct {
	Exchange          string                               `json:"exchange"`
	Symbols           []string                             `json:"symbols"`
	TotalCapital      float64                              `json:"total_capital,omitempty"`   // 總金額模式時使用
	SymbolCapitals    []SymbolCapitalConfig                `json:"symbol_capitals,omitempty"` // 按币种分配模式時使用
	CapitalMode       string                               `json:"capital_mode"`              // total 或 per_symbol
	RiskProfile       string                               `json:"risk_profile"`              // conservative/balanced/aggressive
	CurrentPrices     map[string]float64                   `json:"current_prices"`
	SymbolAllocations map[string]float64                   `json:"symbol_allocations,omitempty"` // 币种比例分配
	StrategySplits    map[string][]config.StrategyInstance `json:"strategy_splits,omitempty"`    // 策略分配
	WithdrawalPolicy  config.WithdrawalPolicy              `json:"withdrawal_policy,omitempty"`  // 提現策略
}

// GenerateConfigResponse AI 配置生成响应
type GenerateConfigResponse struct {
	Explanation   string                   `json:"explanation"`
	GridConfig    []SymbolGridConfig       `json:"grid_config"`
	Allocation    []SymbolAllocationConfig `json:"allocation"`
	SymbolsConfig []config.SymbolConfig    `json:"symbols_config"` // 包含分级资產配置后的完整币种配置
}

// SymbolGridConfig 币种网格配置
type SymbolGridConfig struct {
	Exchange       string  `json:"exchange"`
	Symbol         string  `json:"symbol"`
	PriceInterval  float64 `json:"price_interval"`
	OrderQuantity  float64 `json:"order_quantity"`
	BuyWindowSize  int     `json:"buy_window_size"`
	SellWindowSize int     `json:"sell_window_size"`
	// 网格风控参數（可選）
	GridRiskControl *GridRiskControlConfig `json:"grid_risk_control,omitempty"`
}

// GridRiskControlConfig 网格風控配置
type GridRiskControlConfig struct {
	Enabled                 bool    `json:"enabled"`
	MaxGridLayers           int     `json:"max_grid_layers"`
	MaxOpenOrdersAtCap      int     `json:"max_open_orders_at_cap"`      // 達到最大持倉預警時最多允許的開倉單數；0=僅不新開倉不撤單
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

// buildPrompt 構建提示词
func buildPrompt(req *GenerateConfigRequest) string {
	riskDesc := map[string]string{
		"conservative": "保守型（低风險，稳健收益）",
		"balanced":     "平衡型（中等风險，适中收益）",
		"aggressive":   "激進型（高风險，追求高收益）",
	}[req.RiskProfile]

	var capitalInfo string
	var totalCapital float64

	if req.CapitalMode == "per_symbol" && len(req.SymbolCapitals) > 0 {
		capitalInfo = "资金配置模式：按币种分配\n各币种资金分配：\n"
		for _, sc := range req.SymbolCapitals {
			capitalInfo += fmt.Sprintf("- %s: %.2f USDT\n", sc.Symbol, sc.Capital)
			totalCapital += sc.Capital
		}
		capitalInfo += fmt.Sprintf("總计资金：%.2f USDT\n", totalCapital)
	} else {
		totalCapital = req.TotalCapital
		capitalInfo = fmt.Sprintf("资金配置模式：總金額分配\n可用资金：%.2f USDT", totalCapital)
	}

	var assetAllocInfo string
	if len(req.SymbolAllocations) > 0 {
		assetAllocInfo = "\n用戶預設资產分配比例：\n"
		for symbol, weight := range req.SymbolAllocations {
			assetAllocInfo += fmt.Sprintf("- %s: %.1f%%\n", symbol, weight*100)
		}
	}

	var strategySplitInfo string
	if len(req.StrategySplits) > 0 {
		strategySplitInfo = "\n用戶預設策略组合：\n"
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

	prompt := fmt.Sprintf(`你是一個加密貨幣交易专家，擅长多策略资產配置。请根據以下信息，為用戶設计一套分级的量化交易配置方案：

交易所：%s
交易币种：%v
%s
%s
%s
风險偏好：%s

當前價格信息：
`, req.Exchange, req.Symbols, capitalInfo, assetAllocInfo, strategySplitInfo, riskDesc)

	for symbol, price := range req.CurrentPrices {
		prompt += fmt.Sprintf("- %s: $%.2f\n", symbol, price)
	}

	prompt += `
请提供一個详细的配置方案，要求：
1. **资產分配层**：為每個币种設定 symbol_config，包括其分配的總资金 (total_allocated_capital)。
2. **策略组合层**：為每個币种配置 strategies 列表。如果用戶已提供策略权重，请在此基础上优化参數。
3. **参數细节层**：
   - 對於網格策略 (grid)，请提供價格間隔 (price_interval)、買賣窗口大小、每單金額等。
   - 考虑波动率設置合理的网格风控。
4. **提現策略层**：根據用戶提供的提現策略設置 (withdrawal_policy)，确认其合理性並集成到配置中。

请回傳 JSON 格式的配置方案，必須符合以下結構：
{
  "explanation": "配置思路和风險提示...",
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
- 解释应详细說明為什麼这样分配资金和設置参數。
- 所有币种分配的總资金之和不能超過可用资金的 95%。
- 网格参數应根據风險偏好和當前币價计算默认值。
`
	return prompt
}

// buildConfigSchema 構建配置生成的 JSON Schema
func buildConfigSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"explanation": map[string]interface{}{
				"type":        "string",
				"description": "配置方案的详细解释，包括設计思路和风險提示",
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
		},
		"required": []string{"explanation", "symbols_config"},
	}
}
