package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/storage"
)

func TestStrategyRuntimeStateAdapterContextAndOwnerIsolation(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "state-context.db")
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 1, 1
	ss, err := storage.NewStorageService(cfg, t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer ss.GetStorage().Close()
	a := &strategyRuntimeStateAdapter{storageService: ss, botID: "bot-a"}
	if err := a.SaveRuntimeState("spot_short", 6, `{}`); err != nil {
		t.Fatal(err)
	}
	version, payload, found, err := a.LoadRuntimeStateContext(t.Context(), "spot_short")
	if err != nil || !found || version != 6 || payload != `{}` {
		t.Fatalf("normal context read failed: version=%d found=%v err=%v", version, found, err)
	}
	other := &strategyRuntimeStateAdapter{storageService: ss, botID: "bot-b"}
	if _, _, found, err := other.LoadRuntimeStateContext(t.Context(), "spot_short"); err != nil || found {
		t.Fatalf("owner isolation failed: found=%v err=%v", found, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, found, err := a.LoadRuntimeStateContext(ctx, "spot_short"); found || !errors.Is(err, context.Canceled) {
		t.Fatalf("adapter ignored cancelled proof: found=%v err=%v", found, err)
	}
	if _, _, _, err := a.LoadRuntimeStateContext(nil, "spot_short"); err == nil {
		t.Fatal("nil context accepted")
	}
	var missing *strategyRuntimeStateAdapter
	if _, _, _, err := missing.LoadRuntimeStateContext(t.Context(), "spot_short"); err == nil {
		t.Fatal("missing storage accepted")
	}
}

func TestGenericStrategyRuntimeStateAdapterFencesStaleOwnerWrites(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "state-owner-generation.db")
	service, err := storage.NewStorageService(cfg, t.Context())
	if err != nil {
		t.Fatal("open owner-generation storage:", err)
	}
	t.Cleanup(func() { _ = service.GetStorage().Close() })
	scope := execution.IntentScope{Account: "account-scope", Exchange: "binance", Market: "spot", Symbol: "BTCUSDT", Bot: runtimeOwnershipScopeOwner}
	oldOwner, err := newOwnerFencedStrategyRuntimeStateAdapter(t.Context(), service, "bot-a", scope)
	if err != nil {
		t.Fatal("claim initial state owner:", err)
	}
	if err := oldOwner.SaveRuntimeState("spot_long", 2, `{"owner":"old"}`); err != nil {
		t.Fatal("initial durable state save:", err)
	}
	currentOwner, err := newOwnerFencedStrategyRuntimeStateAdapter(t.Context(), service, "bot-a", scope)
	if err != nil {
		t.Fatal("claim replacement state owner:", err)
	}
	if err := oldOwner.SaveRuntimeState("spot_long", 2, `{"owner":"stale"}`); !errors.Is(err, storage.ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("stale owner save error = %v, want generation-lost", err)
	}
	if saved, err := oldOwner.CompareAndSwapRuntimeState(t.Context(), "spot_long", 2, `{"owner":"old"}`, 2, `{"owner":"stale-cas"}`); saved || !errors.Is(err, storage.ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("stale owner CAS = saved %v, err %v", saved, err)
	}
	if err := currentOwner.SaveRuntimeState("spot_long", 2, `{"owner":"current"}`); err != nil {
		t.Fatal("current owner save:", err)
	}
	state, err := service.GetStorage().(storage.StrategyRuntimeStateStore).GetStrategyRuntimeState("bot-a", "spot_long")
	if err != nil || state == nil || state.Payload != `{"owner":"current"}` {
		t.Fatalf("canonical runtime state = %+v, err = %v", state, err)
	}
}
