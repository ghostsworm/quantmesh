package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStrategyRuntimeStateRoundTripAndIsolation(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "runtime-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	state := &StrategyRuntimeState{
		BotID: "bot-a", StrategyName: "dca", SchemaVersion: 1,
		Payload:   `{"layers":[{"quantity":0.25,"opening_fee":0.02}]}`,
		UpdatedAt: time.Date(2026, 9, 27, 1, 2, 3, 0, time.UTC),
	}
	if err := store.SetStrategyRuntimeState(state); err != nil {
		t.Fatal(err)
	}
	state.Payload = `{"layers":[{"quantity":0.5,"opening_fee":0.04}]}`
	if err := store.SetStrategyRuntimeState(state); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetStrategyRuntimeState("bot-a", "dca")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.SchemaVersion != 1 || loaded.Payload != state.Payload {
		t.Fatalf("loaded state mismatch: %+v", loaded)
	}
	other, err := store.GetStrategyRuntimeState("bot-b", "dca")
	if err != nil {
		t.Fatal(err)
	}
	if other != nil {
		t.Fatalf("state leaked across bot identities: %+v", other)
	}
}

func TestStrategyRuntimeStateRejectsIncompleteIdentity(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "runtime-state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetStrategyRuntimeState(&StrategyRuntimeState{StrategyName: "dca", SchemaVersion: 1, Payload: `{}`}); err == nil {
		t.Fatal("expected incomplete bot identity to be rejected")
	}
}

func TestListStrategyRuntimeStatesReturnsEveryBotInStableOrder(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "runtime-state-list.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, botID := range []string{"bot-z", "bot-a"} {
		if err := store.SetStrategyRuntimeState(&StrategyRuntimeState{
			BotID: botID, StrategyName: "funding_carry", SchemaVersion: 1, Payload: `{"strategy":"funding_carry"}`,
		}); err != nil {
			t.Fatal("save runtime state:", err)
		}
	}
	if err := store.SetStrategyRuntimeState(&StrategyRuntimeState{
		BotID: "bot-other", StrategyName: "spot_short", SchemaVersion: 1, Payload: `{}`,
	}); err != nil {
		t.Fatal("save unrelated runtime state:", err)
	}
	states, err := store.ListStrategyRuntimeStates("funding_carry")
	if err != nil {
		t.Fatal("list runtime states:", err)
	}
	if len(states) != 2 || states[0].BotID != "bot-a" || states[1].BotID != "bot-z" {
		t.Fatalf("unexpected funding_carry states: %+v", states)
	}
	if _, err := store.ListStrategyRuntimeStates("  "); err == nil {
		t.Fatal("empty strategy name should be rejected")
	}
}
