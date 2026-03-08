package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"quantmesh/agent/types"
)

// Tool 工具接口
type Tool interface {
	// 工具元信息
	Name() string
	Description() string
	Category() ToolCategory

	// 参数 Schema
	ParameterSchema() map[string]interface{}

	// 执行工具
	Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error)

	// 风险评估
	AssessRisk(params map[string]interface{}) types.SecurityLevel
}

// ToolCategory 工具类别
type ToolCategory string

const (
	CategoryParameter ToolCategory = "parameter" // 参数管理
	CategoryStrategy  ToolCategory = "strategy"  // 策略操作
	CategoryBacktest  ToolCategory = "backtest"  // 回测执行
	CategoryRisk      ToolCategory = "risk"      // 风险分析
	CategoryMarket    ToolCategory = "market"    // 市场数据
	CategorySystem    ToolCategory = "system"    // 系统操作
)

// ToolRegistry 工具注册表
type ToolRegistry struct {
	tools      map[string]Tool
	permissions map[string]types.SecurityLevel
}

// NewToolRegistry 创建工具注册表
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools:       make(map[string]Tool),
		permissions: make(map[string]types.SecurityLevel),
	}
}

// Register 注册工具
func (tr *ToolRegistry) Register(tool Tool) error {
	name := tool.Name()
	if name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}

	tr.tools[name] = tool
	tr.permissions[name] = tool.AssessRisk(nil)

	return nil
}

// Get 获取工具
func (tr *ToolRegistry) Get(name string) (Tool, bool) {
	tool, ok := tr.tools[name]
	return tool, ok
}

// List 列出所有工具
func (tr *ToolRegistry) List() []Tool {
	tools := make([]Tool, 0, len(tr.tools))
	for _, tool := range tr.tools {
		tools = append(tools, tool)
	}
	return tools
}

// ListByCategory 按类别列出工具
func (tr *ToolRegistry) ListByCategory(category ToolCategory) []Tool {
	tools := make([]Tool, 0)
	for _, tool := range tr.tools {
		if tool.Category() == category {
			tools = append(tools, tool)
		}
	}
	return tools
}

// GetToolDefinitions 获取工具定义（用于 LLM）
func (tr *ToolRegistry) GetToolDefinitions() []types.ToolDefinition {
	definitions := make([]types.ToolDefinition, 0, len(tr.tools))

	for _, tool := range tr.tools {
		def := types.ToolDefinition{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.ParameterSchema(),
		}
		definitions = append(definitions, def)
	}

	return definitions
}

// ExecuteTool 执行工具调用
func (tr *ToolRegistry) ExecuteTool(ctx context.Context, call types.ToolCall) (types.ToolResult, error) {
	tool, ok := tr.Get(call.Name)
	if !ok {
		return types.ToolResult{}, fmt.Errorf("tool not found: %s", call.Name)
	}

	// 风险评估
	risk := tool.AssessRisk(call.Arguments)
	if risk >= types.SecurityLevelHigh {
		// 高风险操作需要确认
		return types.ToolResult{
			CallID: call.ID,
			Result: map[string]interface{}{
				"requires_confirmation": true,
				"risk_level":           risk.String(),
				"risk_color":           risk.Color(),
			},
		}, nil
	}

	// 执行工具
	result, err := tool.Execute(ctx, call.Arguments)
	if err != nil {
		return types.ToolResult{}, err
	}

	result.CallID = call.ID
	return result, nil
}

// BaseTool 基础工具实现
type BaseTool struct {
	name        string
	description string
	category    ToolCategory
	schema      map[string]interface{}
}

func (bt *BaseTool) Name() string        { return bt.name }
func (bt *BaseTool) Description() string { return bt.description }
func (bt *BaseTool) Category() ToolCategory { return bt.category }
func (bt *BaseTool) ParameterSchema() map[string]interface{} { return bt.schema }
func (bt *BaseTool) AssessRisk(_ map[string]interface{}) types.SecurityLevel {
	return types.SecurityLevelLow
}

// ToolExecutor 工具执行器函数类型
type ToolExecutor func(ctx context.Context, params map[string]interface{}) (interface{}, error)

// SimpleTool 简单工具实现
type SimpleTool struct {
	BaseTool
	executor ToolExecutor
	risk     types.SecurityLevel
}

func NewSimpleTool(name, description string, category ToolCategory, schema map[string]interface{}, executor ToolExecutor, risk types.SecurityLevel) *SimpleTool {
	return &SimpleTool{
		BaseTool: BaseTool{
			name:        name,
			description: description,
			category:    category,
			schema:      schema,
		},
		executor: executor,
		risk:     risk,
	}
}

func (st *SimpleTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
	result, err := st.executor(ctx, params)
	if err != nil {
		return types.ToolResult{
			Error: err.Error(),
		}, nil
	}

	return types.ToolResult{
		Result: result,
	}, nil
}

func (st *SimpleTool) AssessRisk(_ map[string]interface{}) types.SecurityLevel {
	return st.risk
}

// CreateParameterSchema 创建参数 Schema
func CreateParameterSchema(params map[string]SchemaProperty) map[string]interface{} {
	properties := make(map[string]interface{})
	required := make([]string, 0)

	for name, prop := range params {
		properties[name] = prop.ToMap()
		if prop.Required {
			required = append(required, name)
		}
	}

	return map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}

// SchemaProperty Schema 属性
type SchemaProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     interface{} `json:"default,omitempty"`
}

func (sp *SchemaProperty) ToMap() map[string]interface{} {
	result := map[string]interface{}{
		"type":        sp.Type,
		"description": sp.Description,
	}

	if len(sp.Enum) > 0 {
		result["enum"] = sp.Enum
	}

	if sp.Default != nil {
		result["default"] = sp.Default
	}

	return result
}

// MustJSONMarshal 辅助函数：序列化为 JSON，失败时 panic
func MustJSONMarshal(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}
