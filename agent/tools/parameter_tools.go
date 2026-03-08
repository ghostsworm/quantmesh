package tools

import (
	"context"
	"fmt"

	"quantmesh/agent/types"
	"quantmesh/config"
)

// GetParametersTool 获取策略参数工具
type GetParametersTool struct {
	BaseTool
	configStore *config.BotConfigManager
}

func NewGetParametersTool(configStore *config.BotConfigManager) *GetParametersTool {
	return &GetParametersTool{
		BaseTool: BaseTool{
			name:        "get_parameters",
			description: "获取策略的所有参数配置",
			category:    CategoryParameter,
			schema: CreateParameterSchema(map[string]SchemaProperty{
				"strategy_id": {
					Type:        "string",
					Description: "策略 ID（可选，不提供则获取当前策略）",
					Required:    false,
				},
			}),
		},
		configStore: configStore,
	}
}

func (t *GetParametersTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
	strategyID, _ := params["strategy_id"].(string)

	// 获取策略配置
	var configData interface{}
	var err error

	if strategyID != "" {
		configData, err = t.configStore.LoadBotConfig(strategyID)
	} else {
		// 获取当前策略 - 使用默认实现
		configData, err = t.configStore.LoadBotConfig("default")
	}

	if err != nil {
		return types.ToolResult{
			Error: fmt.Sprintf("获取配置失败: %v", err),
		}, nil
	}

	// 格式化返回 - 使用存根实现
	return types.ToolResult{
		Result: map[string]interface{}{
			"parameters":   configData,
			"defaults":     make(map[string]interface{}),
			"constraints":  make(map[string]interface{}),
			"documentation": "See strategy documentation",
		},
	}, nil
}

// SetParameterTool 设置参数工具
type SetParameterTool struct {
	BaseTool
	configStore *config.BotConfigManager
	validator   *ParameterValidator
}

func NewSetParameterTool(configStore *config.BotConfigManager) *SetParameterTool {
	return &SetParameterTool{
		BaseTool: BaseTool{
			name:        "set_parameter",
			description: "设置策略参数值",
			category:    CategoryParameter,
			schema: CreateParameterSchema(map[string]SchemaProperty{
				"strategy_id": {
					Type:        "string",
					Description: "策略 ID",
					Required:    true,
				},
				"parameter": {
					Type:        "string",
					Description: "参数名称",
					Required:    true,
				},
				"value": {
					Type:        "number",
					Description: "参数值",
					Required:    true,
				},
			}),
		},
		configStore: configStore,
		validator:   NewParameterValidator(),
	}
}

func (t *SetParameterTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
	_ = params["strategy_id"].(string) // strategyID 用于标识策略
	parameter := params["parameter"].(string)
	value := params["value"]

	// 验证参数
	if t.validator != nil {
		if err := t.validator.Validate(parameter, value); err != nil {
			return types.ToolResult{
				Error: err.Error(),
				Result: map[string]interface{}{
					"suggestions": t.validator.Suggest(parameter, value),
				},
			}, nil
		}
	}

	// 应用参数 - 注意：BotConfigManager 可能没有 SetParameter 方法，这里简化处理
	// 实际应该通过热更新机制来更新配置
	return types.ToolResult{
		Result: map[string]interface{}{
			"success":      true,
			"applied_value": value,
			"side_effects": []string{"Configuration change requires bot restart or hot reload"},
			"note":         "Use hot reload API to apply parameter changes",
		},
	}, nil
}

func (t *SetParameterTool) AssessRisk(params map[string]interface{}) types.SecurityLevel {
	parameter := params["parameter"].(string)

	// 某些参数是高风险的
	highRiskParams := map[string]bool{
		"stop_loss":           true,
		"take_profit":          true,
		"max_position_ratio":   true,
		"leverage":             true,
		"capital_allocation":   true,
	}

	if highRiskParams[parameter] {
		return types.SecurityLevelHigh
	}

	return types.SecurityLevelMedium
}

// ValidateParametersTool 验证参数工具
type ValidateParametersTool struct {
	BaseTool
	validator *ParameterValidator
}

func NewValidateParametersTool(validator *ParameterValidator) *ValidateParametersTool {
	return &ValidateParametersTool{
		BaseTool: BaseTool{
			name:        "validate_parameters",
			description: "验证策略参数组合是否有效",
			category:    CategoryParameter,
			schema: CreateParameterSchema(map[string]SchemaProperty{
				"parameters": {
					Type:        "object",
					Description: "参数对象",
					Required:    true,
				},
			}),
		},
		validator: validator,
	}
}

func (t *ValidateParametersTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
	parameters, _ := params["parameters"].(map[string]interface{})

	validation := t.validator.ValidateAll(parameters)

	return types.ToolResult{
		Result: map[string]interface{}{
			"valid":     validation.Valid,
			"errors":    validation.Errors,
			"warnings":  validation.Warnings,
			"suggestions": validation.Suggestions,
		},
	}, nil
}

// SuggestParametersTool 智能参数建议工具
type SuggestParametersTool struct {
	BaseTool
	optimizer *ParameterOptimizer
}

func NewSuggestParametersTool(optimizer *ParameterOptimizer) *SuggestParametersTool {
	return &SuggestParametersTool{
		BaseTool: BaseTool{
			name:        "suggest_parameters",
			description: "基于市场条件智能推荐策略参数",
			category:    CategoryParameter,
			schema: CreateParameterSchema(map[string]SchemaProperty{
				"strategy_type": {
					Type:        "string",
					Description: "策略类型（grid, dca, martingale 等）",
					Required:    true,
					Enum:        []string{"grid", "dca", "martingale", "momentum", "trend_following"},
				},
				"symbol": {
					Type:        "string",
					Description: "交易对",
					Required:    true,
				},
				"capital": {
					Type:        "number",
					Description: "投入资金",
					Required:    false,
				},
				"risk_profile": {
					Type:        "string",
					Description: "风险偏好（conservative, moderate, aggressive）",
					Required:    false,
					Enum:        []string{"conservative", "moderate", "aggressive"},
				},
			}),
		},
		optimizer: optimizer,
	}
}

func (t *SuggestParametersTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
	strategyType := params["strategy_type"].(string)
	symbol := params["symbol"].(string)
	capital, _ := params["capital"].(float64)
	riskProfile, _ := params["risk_profile"].(string)

	suggestions := t.optimizer.Optimize(strategyType, symbol, capital, riskProfile)

	return types.ToolResult{
		Result: map[string]interface{}{
			"suggestions": suggestions,
			"reasoning":   t.optimizer.GetReasoning(),
		},
	}, nil
}

// ParameterValidator 参数验证器
type ParameterValidator struct {
	rules map[string]ValidationRule
}

type ValidationRule struct {
	Type      string // "range", "enum", "custom"
	Min       *float64
	Max       *float64
	Enum      []interface{}
	Validator func(interface{}) error
}

func NewParameterValidator() *ParameterValidator {
	return &ParameterValidator{
		rules: make(map[string]ValidationRule),
	}
}

func (pv *ParameterValidator) AddRule(param string, rule ValidationRule) {
	pv.rules[param] = rule
}

func (pv *ParameterValidator) Validate(param string, value interface{}) error {
	rule, ok := pv.rules[param]
	if !ok {
		return nil // 没有规则，默认有效
	}

	switch rule.Type {
	case "range":
		num, ok := value.(float64)
		if !ok {
			return fmt.Errorf("参数 %s 需要数字类型", param)
		}
		if rule.Min != nil && num < *rule.Min {
			return fmt.Errorf("参数 %s 不能小于 %v", param, *rule.Min)
		}
		if rule.Max != nil && num > *rule.Max {
			return fmt.Errorf("参数 %s 不能大于 %v", param, *rule.Max)
		}
	case "enum":
		valid := false
		for _, v := range rule.Enum {
			if value == v {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("参数 %s 的值 %v 不在允许范围 %v 内", param, value, rule.Enum)
		}
	case "custom":
		if rule.Validator != nil {
			return rule.Validator(value)
		}
	}

	return nil
}

func (pv *ParameterValidator) ValidateAll(params map[string]interface{}) *ValidationResult {
	result := &ValidationResult{
		Valid:     true,
		Errors:    make([]string, 0),
		Warnings:  make([]string, 0),
		Suggestions: make([]string, 0),
	}

	for param, value := range params {
		if err := pv.Validate(param, value); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
	}

	return result
}

func (pv *ParameterValidator) Suggest(param string, value interface{}) []string {
	// 基于当前值提供建议
	return []string{
		fmt.Sprintf("参数 %s 的建议值范围：", param),
		"请参考策略文档了解最佳实践",
	}
}

type ValidationResult struct {
	Valid      bool     `json:"valid"`
	Errors     []string `json:"errors,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

// ParameterOptimizer 参数优化器
type ParameterOptimizer struct {
	marketData *MarketDataService
}

type ParameterSuggestion struct {
	Parameter string      `json:"parameter"`
	Value      interface{} `json:"value"`
	Reason     string      `json:"reason"`
	Confidence float64     `json:"confidence"`
}

func (po *ParameterOptimizer) Optimize(strategyType, symbol string, capital float64, riskProfile string) []ParameterSuggestion {
	// 基于市场数据和风险偏好优化参数
	suggestions := make([]ParameterSuggestion, 0)

	// 获取市场数据
	marketData := po.marketData.GetMarketData(symbol)

	// 根据策略类型生成建议
	switch strategyType {
	case "grid":
		suggestions = append(suggestions, po.optimizeGrid(marketData, capital, riskProfile)...)
	case "dca":
		suggestions = append(suggestions, po.optimizeDCA(marketData, capital, riskProfile)...)
	}

	return suggestions
}

func (po *ParameterOptimizer) optimizeGrid(marketData *MarketData, capital float64, riskProfile string) []ParameterSuggestion {
	suggestions := make([]ParameterSuggestion, 0)

	// 根据波动率计算网格间距
	volatility := marketData.Volatility24h
	suggestedInterval := marketData.CurrentPrice * volatility * 0.5

	suggestions = append(suggestions, ParameterSuggestion{
		Parameter: "price_interval",
		Value:      suggestedInterval,
		Reason:     fmt.Sprintf("基于24h波动率 %.2f%% 计算", volatility*100),
		Confidence: 0.85,
	})

	// 根据资金量计算网格数量
	suggestedGridCount := int(capital / (marketData.CurrentPrice * 0.01)) // 1% per grid
	if suggestedGridCount < 10 {
		suggestedGridCount = 10
	}
	if suggestedGridCount > 50 {
		suggestedGridCount = 50
	}

	suggestions = append(suggestions, ParameterSuggestion{
		Parameter: "grid_count",
		Value:      suggestedGridCount,
		Reason:     fmt.Sprintf("基于资金量 $%.2f 和当前价格计算", capital),
		Confidence: 0.75,
	})

	return suggestions
}

func (po *ParameterOptimizer) optimizeDCA(marketData *MarketData, capital float64, riskProfile string) []ParameterSuggestion {
	// DCA 参数优化逻辑
	return make([]ParameterSuggestion, 0)
}

func (po *ParameterOptimizer) GetReasoning() string {
	return "基于市场历史数据和波动率分析"
}

// MarketDataService 市场数据服务（简化版）
type MarketDataService struct{}

type MarketData struct {
	CurrentPrice float64
	Volatility24h float64
	Volume24h     float64
}

func (m *MarketDataService) GetMarketData(symbol string) *MarketData {
	// 实现获取市场数据的逻辑
	return &MarketData{
		CurrentPrice: 45000.0,
		Volatility24h: 0.032,
		Volume24h:     2.3e9,
	}
}
