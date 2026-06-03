package agent

import (
	"strings"
	"testing"

	"quantmesh/agent/types"
)

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
