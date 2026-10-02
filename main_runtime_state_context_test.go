package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"quantmesh/config"
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
