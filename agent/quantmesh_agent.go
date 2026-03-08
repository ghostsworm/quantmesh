package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"quantmesh/agent/llm"
	"quantmesh/agent/tools"
	"quantmesh/agent/types"
	"quantmesh/logger"
)

// QuantMeshAgent QuantMesh AI Agent
type QuantMeshAgent struct {
	conversation *ConversationManager
	tools        *tools.ToolRegistry
	planner      *TODOPlanner
	llm          types.LLMClient
	subAgents    map[string]types.Agent

	mu           sync.RWMutex
	state        AgentState
	config       AgentConfig
}

// AgentState Agent 状态
type AgentState int

const (
	StateIdle     AgentState = iota
	StateRunning
	StatePaused
	StateError
)

// AgentConfig Agent 配置
type AgentConfig struct {
	LLMProvider    string            `json:"llm_provider"`
	LLMAPIKey      string            `json:"llm_api_key"`
	LLMModel       string            `json:"llm_model"`
	MaxTokens      int               `json:"max_tokens"`
	Temperature    float64           `json:"temperature"`
	SystemPrompt   string            `json:"system_prompt"`
	EnableTools    []string          `json:"enable_tools"`
	Settings       map[string]interface{} `json:"settings"`
}

// NewQuantMeshAgent 创建 Agent
func NewQuantMeshAgent(config AgentConfig) (*QuantMeshAgent, error) {
	// 创建 LLM 客户端
	var llmClient types.LLMClient
	var err error

	switch config.LLMProvider {
	case "claude":
		llmClient = llm.NewClaudeClient(config.LLMAPIKey, config.LLMModel)
	case "openai":
		llmClient, err = llm.NewOpenAIClient(config.LLMAPIKey, config.LLMModel)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.LLMProvider)
	}

	if err != nil {
		return nil, err
	}

	// 创建工具注册表
	toolRegistry := tools.NewToolRegistry()

	// 注册核心工具
	coreTools := []tools.Tool{
		tools.NewGetParametersTool(nil),
		tools.NewSetParameterTool(nil),
		tools.NewValidateParametersTool(nil),
		tools.NewSuggestParametersTool(nil),
	}

	for _, tool := range coreTools {
		if err := toolRegistry.Register(tool); err != nil {
			logger.Warn("Failed to register tool %s: %v", tool.Name(), err)
		}
	}

	// 创建对话管理器
	conversationMgr := NewConversationManager()

	// 创建 TODO 规划器
	planner := NewTODOPlanner()

	agent := &QuantMeshAgent{
		conversation: conversationMgr,
		tools:        toolRegistry,
		planner:      planner,
		llm:          llmClient,
		subAgents:    make(map[string]types.Agent),
		state:        StateIdle,
		config:       config,
	}

	return agent, nil
}

// ProcessMessage 处理用户消息
func (qa *QuantMeshAgent) ProcessMessage(ctx context.Context, msg types.Message) (types.Response, error) {
	qa.mu.Lock()
	qa.state = StateRunning
	qa.mu.Unlock()

	defer func() {
		qa.mu.Lock()
		qa.state = StateIdle
		qa.mu.Unlock()
	}()

	// 添加用户消息到对话历史
	if err := qa.conversation.AddMessage(msg); err != nil {
		return types.Response{}, err
	}

	// 构建生成请求
	llmReq := qa.buildLLMRequest()

	// 主循环：生成响应和执行工具
	var response types.Response
	var iterations int
	const maxIterations = 10

	for iterations < maxIterations {
		iterations++

		// 生成 LLM 响应
		llmResp, err := qa.llm.Generate(ctx, llmReq)
		if err != nil {
			logger.Error("LLM generation failed: %v", err)
			return types.Response{}, err
		}

		// 如果没有工具调用，返回文本响应
		if len(llmResp.ToolCalls) == 0 {
			response.Message = llmResp.Message
			response.NeedsMore = qa.planner.HasPendingTasks()
			response.Suggestions = qa.planner.GetNextSuggestions()
			break
		}

		// 执行工具调用
		response.ToolCalls = llmResp.ToolCalls
		toolResults := make([]types.ToolResult, 0, len(llmResp.ToolCalls))

		for _, call := range llmResp.ToolCalls {
			result, err := qa.tools.ExecuteTool(ctx, call)
			if err != nil {
				logger.Error("Tool execution failed: %v", err)
				result.Error = err.Error()
			}
			toolResults = append(toolResults, result)

			// 记录工具调用事件
			qa.conversation.RecordEvent(types.ConfigEvent{
				Type:   types.EventTypeToolCall,
				Action: types.ConfigAction{
					Type:   "tool_call",
					Target: call.Name,
					Params: call.Arguments,
				},
				Result: types.ConfigResult{
					Success: result.Error == "",
					Data:    map[string]interface{}{"result": result.Result},
					Error:   result.Error,
				},
			})

			// 添加工具结果到 LLM 请求
			llmReq.Messages = append(llmReq.Messages, types.LLMMessage{
				Role:    "tool",
				ToolID:  call.ID,
				Content: fmt.Sprintf("%+v", result),
			})
		}

		// 如果没有更多工具调用，完成循环
		if len(llmResp.ToolCalls) == 0 {
			break
		}

		// 准备下一轮生成
		response.Message = llmResp.Message
	}

	// 添加助手响应到对话历史
	qa.conversation.AddMessage(types.Message{
		Role:    "assistant",
		Content: response.Message,
	})

	return response, nil
}

// ExecuteTool 执行工具调用
func (qa *QuantMeshAgent) ExecuteTool(ctx context.Context, call types.ToolCall) (types.ToolResult, error) {
	return qa.tools.ExecuteTool(ctx, call)
}

// GetState 获取对话状态
func (qa *QuantMeshAgent) GetState() types.ConversationState {
	return qa.conversation.GetState()
}

// SetState 设置对话状态
func (qa *QuantMeshAgent) SetState(state types.ConversationState) error {
	return qa.conversation.SetState(state)
}

// Pause 暂停 Agent
func (qa *QuantMeshAgent) Pause() error {
	qa.mu.Lock()
	defer qa.mu.Unlock()

	if qa.state != StateRunning {
		return fmt.Errorf("agent is not running")
	}

	// 保存当前状态
	if err := qa.conversation.Save(); err != nil {
		return err
	}

	qa.state = StatePaused
	logger.Info("Agent paused")
	return nil
}

// Resume 恢复 Agent
func (qa *QuantMeshAgent) Resume() error {
	qa.mu.Lock()
	defer qa.mu.Unlock()

	if qa.state != StatePaused {
		return fmt.Errorf("agent is not paused")
	}

	// 加载保存的状态
	if err := qa.conversation.Load(); err != nil {
		return err
	}

	qa.state = StateIdle
	logger.Info("Agent resumed")
	return nil
}

// buildLLMRequest 构建 LLM 请求
func (qa *QuantMeshAgent) buildLLMRequest() types.GenerateRequest {
	// 获取对话历史
	messages := qa.conversation.GetMessages()

	// 获取工具定义
	toolDefs := qa.tools.GetToolDefinitions()

	// 构建 TODO 上下文
	todoContext := qa.planner.BuildContext()

	// 构建系统提示
	systemPrompt := qa.buildSystemPrompt(todoContext)

	return types.GenerateRequest{
		Messages:     messages,
		Tools:        toolDefs,
		Temperature:  qa.config.Temperature,
		MaxTokens:    qa.config.MaxTokens,
		SystemPrompt: systemPrompt,
	}
}

// buildSystemPrompt 构建系统提示
func (qa *QuantMeshAgent) buildSystemPrompt(todoContext string) string {
	basePrompt := `你是 QuantMesh 的交易策略配置助手。你的职责是帮助用户通过自然语言对话来配置和管理交易策略。

## 核心原则

1. **安全优先**: 始终评估配置风险，高风险操作需要明确提示并要求确认
2. **清晰沟通**: 用简洁明了的语言解释技术概念
3. **逐步引导**: 对于复杂配置，分解为多个步骤逐步完成
4. **智能建议**: 基于市场数据和用户偏好提供参数建议
5. **验证优先**: 应用配置前务必验证参数有效性

## 工作流程

1. **理解需求**: 分析用户意图，识别策略类型
2. **收集信息**: 获取必要的参数（投资金额、风险偏好等）
3. **提供建议**: 基于市场条件推荐参数
4. **验证配置**: 检查参数组合的有效性
5. **应用配置**: 保存配置并提示后续操作

## 可用工具

`

	// 添加工具列表
	for _, tool := range qa.tools.List() {
		basePrompt += fmt.Sprintf("- %s: %s\n", tool.Name(), tool.Description())
	}

	// 添加 TODO 上下文
	if todoContext != "" {
		basePrompt += fmt.Sprintf("\n## 当前任务\n%s\n", todoContext)
	}

	// 添加用户自定义系统提示
	if qa.config.SystemPrompt != "" {
		basePrompt += "\n" + qa.config.SystemPrompt
	}

	return basePrompt
}

// ConversationManager 对话管理器
type ConversationManager struct {
	mu          sync.RWMutex
	state       types.ConversationState
	eventStore  EventStore
	contextMgr   *ContextManager
}

// NewConversationManager 创建对话管理器
func NewConversationManager() *ConversationManager {
	return &ConversationManager{
		state: types.ConversationState{
			Messages:     make([]types.Message, 0),
			CurrentConfig: make(map[string]interface{}),
			TODOList:     make([]types.Task, 0),
			Context:      make(map[string]interface{}),
			CreatedAt:    time.Now(),
		},
		eventStore: NewMemoryEventStore(),
		contextMgr: NewContextManager(),
	}
}

// AddMessage 添加消息
func (cm *ConversationManager) AddMessage(msg types.Message) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	msg.ID = generateID()
	msg.Timestamp = time.Now()
	cm.state.Messages = append(cm.state.Messages, msg)
	cm.state.UpdatedAt = time.Now()

	return nil
}

// GetMessages 获取消息
func (cm *ConversationManager) GetMessages() []types.LLMMessage {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	messages := make([]types.LLMMessage, 0, len(cm.state.Messages))

	for _, msg := range cm.state.Messages {
		messages = append(messages, types.LLMMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return messages
}

// RecordEvent 记录事件
func (cm *ConversationManager) RecordEvent(event types.ConfigEvent) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	event.ID = generateID()
	event.Timestamp = time.Now()
	cm.eventStore.Save(event)
}

// GetState 获取状态
func (cm *ConversationManager) GetState() types.ConversationState {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.state
}

// SetState 设置状态
func (cm *ConversationManager) SetState(state types.ConversationState) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.state = state
	return nil
}

// Save 保存对话
func (cm *ConversationManager) Save() error {
	// 实现保存逻辑
	return nil
}

// Load 加载对话
func (cm *ConversationManager) Load() error {
	// 实现加载逻辑
	return nil
}

// TODOPlanner TODO 规划器
type TODOPlanner struct {
	mu     sync.Mutex
	items  []types.Task
	current int
}

// NewTODOPlanner 创建 TODO 规划器
func NewTODOPlanner() *TODOPlanner {
	return &TODOPlanner{
		items: make([]types.Task, 0),
	}
}

// AddTask 添加任务
func (tp *TODOPlanner) AddTask(content string, priority int) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	task := types.Task{
		ID:       generateID(),
		Content:  content,
		Status:   types.TaskStatusPending,
		Priority: priority,
	}

	tp.items = append(tp.items, task)
}

// UpdateTask 更新任务状态
func (tp *TODOPlanner) UpdateTask(id string, status types.TaskStatus) {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	for i, task := range tp.items {
		if task.ID == id {
			tp.items[i].Status = status
			break
		}
	}
}

// HasPendingTasks 是否有待处理任务
func (tp *TODOPlanner) HasPendingTasks() bool {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	for _, task := range tp.items {
		if task.Status == types.TaskStatusPending || task.Status == types.TaskStatusInProgress {
			return true
		}
	}
	return false
}

// GetNextSuggestions 获取下一步建议
func (tp *TODOPlanner) GetNextSuggestions() []string {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	suggestions := make([]string, 0)

	for _, task := range tp.items {
		if task.Status == types.TaskStatusPending {
			suggestions = append(suggestions, fmt.Sprintf("继续配置: %s", task.Content))
		}
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions, "应用配置", "查看配置详情", "运行回测")
	}

	return suggestions
}

// BuildContext 构建 TODO 上下文
func (tp *TODOPlanner) BuildContext() string {
	tp.mu.Lock()
	defer tp.mu.Unlock()

	if len(tp.items) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString("当前任务列表:\n")

	for i, task := range tp.items {
		status := "待处理"
		switch task.Status {
		case types.TaskStatusInProgress:
			status = "进行中"
		case types.TaskStatusCompleted:
			status = "已完成"
		case types.TaskStatusFailed:
			status = "失败"
		}

		builder.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, status, task.Content))
	}

	return builder.String()
}

// ContextManager 上下文管理器
type ContextManager struct {
	maxTokens int
}

// NewContextManager 创建上下文管理器
func NewContextManager() *ContextManager {
	return &ContextManager{
		maxTokens: 100000, // 默认 100K tokens
	}
}

// EventStore 事件存储接口
type EventStore interface {
	Save(event types.ConfigEvent) error
	Get(sessionID string) ([]types.ConfigEvent, error)
}

// MemoryEventStore 内存事件存储
type MemoryEventStore struct {
	mu     sync.RWMutex
	events []types.ConfigEvent
}

// NewMemoryEventStore 创建内存事件存储
func NewMemoryEventStore() *MemoryEventStore {
	return &MemoryEventStore{
		events: make([]types.ConfigEvent, 0),
	}
}

// Save 保存事件
func (s *MemoryEventStore) Save(event types.ConfigEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

// Get 获取事件
func (s *MemoryEventStore) Get(sessionID string) ([]types.ConfigEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filtered := make([]types.ConfigEvent, 0)
	for _, event := range s.events {
		if strings.Contains(event.ID, sessionID) {
			filtered = append(filtered, event)
		}
	}

	return filtered, nil
}

// generateID 生成唯一 ID
func generateID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), randomString(8))
}

// randomString 生成随机字符串
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
}
