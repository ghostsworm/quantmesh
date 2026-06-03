package agent

import (
	"context"
	"strings"
	"testing"

	"quantmesh/agent/tools"
	"quantmesh/agent/types"
)

type fakeLLMClient struct {
	responses []types.GenerateResponse
	requests  []types.GenerateRequest
}

func (f *fakeLLMClient) Generate(ctx context.Context, req types.GenerateRequest) (types.GenerateResponse, error) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return types.GenerateResponse{Message: "done"}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeLLMClient) GenerateStream(ctx context.Context, req types.GenerateRequest) (<-chan types.GenerateChunk, error) {
	ch := make(chan types.GenerateChunk, 1)
	ch <- types.GenerateChunk{Delta: "done", IsComplete: true}
	close(ch)
	return ch, nil
}

func (f *fakeLLMClient) GenerateWithImage(ctx context.Context, text string, images []types.ImageData, req types.GenerateRequest) (types.GenerateResponse, error) {
	return f.Generate(ctx, req)
}

func newTestAgent(llmClient types.LLMClient) *QuantMeshAgent {
	return &QuantMeshAgent{
		conversation: NewConversationManager(),
		tools:        tools.NewToolRegistry(),
		planner:      NewTODOPlanner(),
		llm:          llmClient,
		subAgents:    make(map[string]types.Agent),
		state:        StateIdle,
		config: AgentConfig{
			Temperature:  0.2,
			MaxTokens:    256,
			SystemPrompt: "自定义提示",
		},
	}
}

func TestConversationManagerStateAndMessages(t *testing.T) {
	manager := NewConversationManager()

	if err := manager.AddMessage(types.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}
	if err := manager.AddMessage(types.Message{Role: "assistant", Content: "world"}); err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}

	messages := manager.GetMessages()
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Role != "user" || messages[0].Content != "hello" {
		t.Fatalf("unexpected first message: %#v", messages[0])
	}

	state := manager.GetState()
	if len(state.Messages) != 2 {
		t.Fatalf("state should keep 2 messages, got %d", len(state.Messages))
	}
	if state.Messages[0].ID == "" {
		t.Fatal("expected AddMessage to assign an id")
	}

	nextState := types.ConversationState{SessionID: "session-1"}
	if err := manager.SetState(nextState); err != nil {
		t.Fatalf("SetState failed: %v", err)
	}
	if got := manager.GetState().SessionID; got != "session-1" {
		t.Fatalf("SessionID = %q, want session-1", got)
	}
	if err := manager.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := manager.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
}

func TestQuantMeshAgentBuildRequestAndSystemPrompt(t *testing.T) {
	llmClient := &fakeLLMClient{}
	agent := newTestAgent(llmClient)
	agent.planner.AddTask("检查网格参数", 1)
	if err := agent.conversation.AddMessage(types.Message{Role: "user", Content: "帮我检查"}); err != nil {
		t.Fatalf("AddMessage failed: %v", err)
	}
	if err := agent.tools.Register(tools.NewSimpleTool(
		"inspect_config",
		"检查配置",
		tools.CategorySystem,
		map[string]interface{}{"type": "object"},
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			return "ok", nil
		},
		types.SecurityLevelLow,
	)); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	req := agent.buildLLMRequest()
	if len(req.Messages) != 1 || req.Messages[0].Content != "帮我检查" {
		t.Fatalf("unexpected messages: %#v", req.Messages)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "inspect_config" {
		t.Fatalf("unexpected tools: %#v", req.Tools)
	}
	if req.Temperature != 0.2 || req.MaxTokens != 256 {
		t.Fatalf("unexpected generation settings: %#v", req)
	}
	if !strings.Contains(req.SystemPrompt, "检查网格参数") ||
		!strings.Contains(req.SystemPrompt, "inspect_config") ||
		!strings.Contains(req.SystemPrompt, "自定义提示") {
		t.Fatalf("system prompt missing expected context: %s", req.SystemPrompt)
	}
}

func TestQuantMeshAgentProcessMessageWithoutTools(t *testing.T) {
	llmClient := &fakeLLMClient{responses: []types.GenerateResponse{{Message: "可以调整网格间距"}}}
	agent := newTestAgent(llmClient)
	agent.planner.AddTask("确认风险", 1)

	resp, err := agent.ProcessMessage(context.Background(), types.Message{Role: "user", Content: "怎么调？"})
	if err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}
	if resp.Message != "可以调整网格间距" {
		t.Fatalf("message = %q", resp.Message)
	}
	if !resp.NeedsMore {
		t.Fatal("expected pending planner task to mark NeedsMore")
	}
	if agent.state != StateIdle {
		t.Fatalf("state = %v, want idle", agent.state)
	}
	state := agent.GetState()
	if len(state.Messages) != 2 || state.Messages[1].Role != "assistant" {
		t.Fatalf("conversation messages not recorded: %#v", state.Messages)
	}
	if len(llmClient.requests) != 1 || len(llmClient.requests[0].Messages) != 1 {
		t.Fatalf("unexpected LLM requests: %#v", llmClient.requests)
	}
}

func TestQuantMeshAgentProcessMessageExecutesToolCalls(t *testing.T) {
	llmClient := &fakeLLMClient{responses: []types.GenerateResponse{
		{
			Message: "我先看一下配置",
			ToolCalls: []types.ToolCall{{
				ID:        "call-1",
				Name:      "inspect_config",
				Arguments: map[string]interface{}{"symbol": "BTCUSDT"},
			}},
		},
		{Message: "配置正常"},
	}}
	agent := newTestAgent(llmClient)
	if err := agent.tools.Register(tools.NewSimpleTool(
		"inspect_config",
		"检查配置",
		tools.CategorySystem,
		map[string]interface{}{"type": "object"},
		func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
			if params["symbol"] != "BTCUSDT" {
				t.Fatalf("unexpected params: %#v", params)
			}
			return map[string]interface{}{"ok": true}, nil
		},
		types.SecurityLevelLow,
	)); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	resp, err := agent.ProcessMessage(context.Background(), types.Message{Role: "user", Content: "检查配置"})
	if err != nil {
		t.Fatalf("ProcessMessage failed: %v", err)
	}
	if resp.Message != "配置正常" {
		t.Fatalf("message = %q", resp.Message)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "inspect_config" {
		t.Fatalf("tool calls = %#v", resp.ToolCalls)
	}
	if len(llmClient.requests) != 2 {
		t.Fatalf("expected two LLM requests, got %d", len(llmClient.requests))
	}
	if got := llmClient.requests[1].Messages[len(llmClient.requests[1].Messages)-1]; got.Role != "tool" || got.ToolID != "call-1" {
		t.Fatalf("expected tool result message in second request, got %#v", got)
	}
}

func TestQuantMeshAgentStateHelpersAndPauseResume(t *testing.T) {
	agent := newTestAgent(&fakeLLMClient{})

	if agent.GetLLMClient() == nil {
		t.Fatal("expected LLM client")
	}
	if err := agent.Pause(); err == nil {
		t.Fatal("expected pause to require running state")
	}
	agent.state = StateRunning
	if err := agent.Pause(); err != nil {
		t.Fatalf("Pause failed: %v", err)
	}
	if agent.state != StatePaused {
		t.Fatalf("state = %v, want paused", agent.state)
	}
	if err := agent.Resume(); err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	if agent.state != StateIdle {
		t.Fatalf("state = %v, want idle", agent.state)
	}
	if err := agent.Resume(); err == nil {
		t.Fatal("expected resume to require paused state")
	}

	nextState := types.ConversationState{SessionID: "agent-session"}
	if err := agent.SetState(nextState); err != nil {
		t.Fatalf("SetState failed: %v", err)
	}
	if got := agent.GetState().SessionID; got != "agent-session" {
		t.Fatalf("SessionID = %q", got)
	}
}

func TestTODOPlannerLifecycle(t *testing.T) {
	planner := NewTODOPlanner()

	if planner.HasPendingTasks() {
		t.Fatal("new planner should not have pending tasks")
	}
	if context := planner.BuildContext(); context != "" {
		t.Fatalf("empty context = %q, want empty", context)
	}

	planner.AddTask("配置策略", 2)
	planner.AddTask("设置风控", 1)

	if !planner.HasPendingTasks() {
		t.Fatal("expected pending tasks after AddTask")
	}
	if suggestions := planner.GetNextSuggestions(); len(suggestions) != 2 || !strings.Contains(suggestions[0], "配置策略") {
		t.Fatalf("unexpected suggestions: %#v", suggestions)
	}

	firstID := planner.items[0].ID
	planner.UpdateTask(firstID, types.TaskStatusCompleted)
	planner.UpdateTask("missing", types.TaskStatusFailed)

	context := planner.BuildContext()
	if !strings.Contains(context, "[已完成] 配置策略") || !strings.Contains(context, "[待处理] 设置风控") {
		t.Fatalf("unexpected context: %s", context)
	}

	planner.UpdateTask(planner.items[1].ID, types.TaskStatusCompleted)
	if planner.HasPendingTasks() {
		t.Fatal("completed tasks should not be pending")
	}
	if suggestions := planner.GetNextSuggestions(); len(suggestions) != 3 || suggestions[0] != "应用配置" {
		t.Fatalf("unexpected fallback suggestions: %#v", suggestions)
	}
}

func TestMemoryEventStoreFiltersBySessionID(t *testing.T) {
	store := NewMemoryEventStore()
	if err := store.Save(types.ConfigEvent{ID: "session-a-1"}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if err := store.Save(types.ConfigEvent{ID: "session-b-1"}); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	events, err := store.Get("session-a")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(events) != 1 || events[0].ID != "session-a-1" {
		t.Fatalf("unexpected filtered events: %#v", events)
	}
}

func TestGenerateIDAndRandomString(t *testing.T) {
	id := generateID()
	if !strings.Contains(id, "-") {
		t.Fatalf("expected generated id to contain separator, got %q", id)
	}
	if got := randomString(12); len(got) != 12 {
		t.Fatalf("randomString length = %d, want 12", len(got))
	}
}
