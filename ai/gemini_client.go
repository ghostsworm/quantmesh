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

// 全局任务服务，在 main.go 中初始化
var GlobalTaskService *service.TaskService

// GeminiClient Gemini API 客户端接口
type GeminiClient interface {
	GenerateConfig(ctx context.Context, req *GenerateConfigRequest) (*GenerateConfigResponse, error)
	GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}) (string, error)
	GenerateContentWithGoogleSearch(ctx context.Context, prompt string, schema map[string]interface{}) (string, error)
}

// AsyncGeminiClient 异步 Gemini API 客户端
type AsyncGeminiClient struct {
	apiKey string
}

// NewGeminiClient 创建 Gemini 客户端（现在统一使用异步内置方式）
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

// GenerateContent 生成内容（通过内置异步系统）
func (c *AsyncGeminiClient) GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}) (string, error) {
	return c.generateContentInternal(ctx, prompt, schema, false)
}

// GenerateContentWithGoogleSearch 生成内容（启用 Google Search 实时搜索）
func (c *AsyncGeminiClient) GenerateContentWithGoogleSearch(ctx context.Context, prompt string, schema map[string]interface{}) (string, error) {
	return c.generateContentInternal(ctx, prompt, schema, true)
}

func (c *AsyncGeminiClient) generateContentInternal(ctx context.Context, prompt string, schema map[string]interface{}, useGoogleSearch bool) (string, error) {
	if GlobalTaskService == nil {
		return "", fmt.Errorf("AI 任务服务未初始化")
	}

	// 1. 创建异步任务
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

	taskID, err := GlobalTaskService.CreateTask(ctx, "generate_content", requestData, 900, 3)
	if err != nil {
		return "", fmt.Errorf("创建异步任务失败: %w", err)
	}

	logger.Info("🔄 已创建 AI 异步任务: %s，开始轮询结果...", taskID)

	// 2. 轮询任务结果
	maxPolls := 300 // 约 10 分钟
	pollInterval := 2 * time.Second

	for i := 0; i < maxPolls; i++ {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("任务被取消: %w", ctx.Err())
		case <-time.After(pollInterval):
			task, err := GlobalTaskService.GetTask(ctx, taskID)
			if err != nil {
				if i%10 == 0 {
					logger.Warn("⚠️ 轮询任务 %s 失败 (第 %d 次): %v", taskID, i+1, err)
				}
				continue
			}

			if i%10 == 0 {
				logger.Info("🔄 轮询任务 %s 状态: %s (第 %d 次)", taskID, task.Status, i+1)
			}

			if task.Status == "completed" {
				var resultData map[string]interface{}
				if err := json.Unmarshal([]byte(task.Result), &resultData); err != nil {
					return "", fmt.Errorf("解析任务结果失败: %w", err)
				}
				if text, ok := resultData["text"].(string); ok {
					return text, nil
				}
				return "", fmt.Errorf("任务结果中缺少文本内容")
			} else if task.Status == "failed" || task.Status == "timeout" {
				errMsg := "未知错误"
				if task.ErrorMessage != nil {
					errMsg = *task.ErrorMessage
				}
				return "", fmt.Errorf("AI 任务执行失败: %s", errMsg)
			}
		}
	}

	return "", fmt.Errorf("AI 任务处理超时 (TaskID: %s)", taskID)
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
		capitalInfo = "资金配置模式：按币种分配\n各币种资金分配：\n"
		for _, sc := range req.SymbolCapitals {
			capitalInfo += fmt.Sprintf("- %s: %.2f USDT\n", sc.Symbol, sc.Capital)
			totalCapital += sc.Capital
		}
		capitalInfo += fmt.Sprintf("总计资金：%.2f USDT\n", totalCapital)
	} else {
		totalCapital = req.TotalCapital
		capitalInfo = fmt.Sprintf("资金配置模式：总金额分配\n可用资金：%.2f USDT", totalCapital)
	}

	var assetAllocInfo string
	if len(req.SymbolAllocations) > 0 {
		assetAllocInfo = "\n用户预设资产分配比例：\n"
		for symbol, weight := range req.SymbolAllocations {
			assetAllocInfo += fmt.Sprintf("- %s: %.1f%%\n", symbol, weight*100)
		}
	}

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
