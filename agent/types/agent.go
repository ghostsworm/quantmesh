package types

import (
	"context"
	"time"
)

// Agent 核心接口
type Agent interface {
	// 处理用户消息
	ProcessMessage(ctx context.Context, msg Message) (Response, error)

	// 执行工具调用
	ExecuteTool(ctx context.Context, call ToolCall) (ToolResult, error)

	// 管理对话状态
	GetState() ConversationState
	SetState(state ConversationState) error

	// 暂停/恢复
	Pause() error
	Resume() error
}

// Message 对话消息
type Message struct {
	ID        string                 `json:"id"`
	Role      string                 `json:"role"` // system, user, assistant, tool
	Content   string                 `json:"content"`
	Timestamp time.Time              `json:"timestamp"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Response Agent 响应
type Response struct {
	Message    string         `json:"message"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	NeedsMore  bool           `json:"needs_more"` // 是否需要更多输入
	Suggestions []string       `json:"suggestions,omitempty"` // 建议的下一步操作
	Images     []ImageData     `json:"images,omitempty"` // AI 生成的图片
	Files      []GeneratedFile `json:"files,omitempty"`  // AI 生成的文件
}

// ToolCall 工具调用
type ToolCall struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolResult 工具执行结果
type ToolResult struct {
	CallID string      `json:"call_id"`
	Result interface{} `json:"result"`
	Error  string      `json:"error,omitempty"`
}

// ConversationState 对话状态
type ConversationState struct {
	SessionID    string                 `json:"session_id"`
	Messages     []Message              `json:"messages"`
	CurrentConfig map[string]interface{} `json:"current_config"`
	TODOList     []Task                 `json:"todo_list"`
	Context      map[string]interface{} `json:"context"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// Task 任务
type Task struct {
	ID          string       `json:"id"`
	Content     string       `json:"content"`
	Status      TaskStatus   `json:"status"`
	Priority    int          `json:"priority"`
	Dependencies []string    `json:"dependencies,omitempty"`
	Context     TaskContext  `json:"context"`
}

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusSkipped    TaskStatus = "skipped"
)

// TaskContext 任务上下文
type TaskContext struct {
	StrategyID string                 `json:"strategy_id,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// SecurityLevel 安全等级
type SecurityLevel int

const (
	SecurityLevelNone     SecurityLevel = 0
	SecurityLevelLow      SecurityLevel = 1
	SecurityLevelMedium   SecurityLevel = 2
	SecurityLevelHigh     SecurityLevel = 3
	SecurityLevelCritical SecurityLevel = 4
)

// String 返回安全等级的字符串表示
func (s SecurityLevel) String() string {
	switch s {
	case SecurityLevelNone:
		return "none"
	case SecurityLevelLow:
		return "low"
	case SecurityLevelMedium:
		return "medium"
	case SecurityLevelHigh:
		return "high"
	case SecurityLevelCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Color 返回安全等级对应的颜色（用于 UI）
func (s SecurityLevel) Color() string {
	switch s {
	case SecurityLevelNone, SecurityLevelLow:
		return "green"
	case SecurityLevelMedium:
		return "yellow"
	case SecurityLevelHigh:
		return "orange"
	case SecurityLevelCritical:
		return "red"
	default:
		return "gray"
	}
}

// ConfigEvent 配置事件
type ConfigEvent struct {
	ID        string                 `json:"id"`
	Timestamp time.Time              `json:"timestamp"`
	Type      EventType              `json:"type"`
	Action    ConfigAction           `json:"action"`
	Result    ConfigResult           `json:"result"`
	Risk      SecurityLevel          `json:"risk"`
	Metadata  map[string]interface{} `json:"metadata"`
}

// EventType 事件类型
type EventType string

const (
	EventTypeUserMessage      EventType = "user_message"
	EventTypeAgentResponse    EventType = "agent_response"
	EventTypeToolCall         EventType = "tool_call"
	EventTypeParameterChange  EventType = "parameter_change"
	EventTypeValidationError  EventType = "validation_error"
	EventTypeRiskWarning      EventType = "risk_warning"
	EventTypeConfigApplied    EventType = "config_applied"
)

// ConfigAction 配置动作
type ConfigAction struct {
	Type     string                 `json:"type"` // set_parameter, create_strategy, etc.
	Target   string                 `json:"target"`
	Params   map[string]interface{} `json:"params"`
}

// ConfigResult 配置结果
type ConfigResult struct {
	Success   bool                   `json:"success"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Warnings  []string               `json:"warnings,omitempty"`
}

// LLMClient LLM 客户端接口
type LLMClient interface {
	// Generate 生成响应（支持 Tool Call）
	Generate(ctx context.Context, req GenerateRequest) (GenerateResponse, error)

	// GenerateStream 流式生成
	GenerateStream(ctx context.Context, req GenerateRequest) (<-chan GenerateChunk, error)

	// GenerateWithImage 生成响应（支持图片输入）
	GenerateWithImage(ctx context.Context, text string, images []ImageData, req GenerateRequest) (GenerateResponse, error)
}

// ImageData 图片数据（用于多模态）
type ImageData struct {
	MimeType string `json:"mime_type"` // image/png, image/jpeg, image/gif, image/webp
	Data     string `json:"data"`      // base64 encoded
}

// GenerateRequest LLM 生成请求
type GenerateRequest struct {
	Messages     []LLMMessage      `json:"messages"`
	Tools        []ToolDefinition  `json:"tools,omitempty"`
	Temperature  float64           `json:"temperature,omitempty"`
	MaxTokens    int               `json:"max_tokens,omitempty"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Images       []ImageData       `json:"images,omitempty"` // 多模态图片数据
}

// LLMMessage LLM 消息
type LLMMessage struct {
	Role      string         `json:"role"` // system, user, assistant, tool
	Content   string         `json:"content"`
	ToolCalls []ToolCall     `json:"tool_calls,omitempty"`
	ToolID    string         `json:"tool_id,omitempty"`
}

// ToolDefinition 工具定义（用于 LLM）
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// GenerateResponse LLM 生成响应
type GenerateResponse struct {
	Message      string        `json:"message"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	FinishReason string        `json:"finish_reason"`
	Usage        TokenUsage    `json:"usage"`
	Images       []ImageData    `json:"images,omitempty"` // AI 生成的图片
	Files        []GeneratedFile `json:"files,omitempty"` // AI 生成的文件
}

// GeneratedFile AI 生成的文件
type GeneratedFile struct {
	Type       string `json:"type"`       // "image", "video", "chart" 等
	URL        string `json:"url"`        // 文件访问 URL
	Path       string `json:"path"`       // 本地文件路径
	Filename   string `json:"filename"`   // 文件名
	Size       int64  `json:"size"`       // 文件大小
	MimeType   string `json:"mime_type"`  // MIME 类型
}

// GenerateChunk 流式生成块
type GenerateChunk struct {
	Delta      string    `json:"delta"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	IsComplete bool      `json:"is_complete"`
}

// TokenUsage Token 使用统计
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
