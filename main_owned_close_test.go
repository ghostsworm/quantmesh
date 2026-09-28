package main

import (
	"context"
	"errors"
	"path/filepath"
	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
	"testing"
)

func TestRuntimeRetainsManagedCloseRecordsAndStopsItsWorker(t *testing.T) {
	v := &shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	rt := newShutdownRuntime(v, "a", "account")
	br := &BotRuntime{BotID: "a", Config: config.BotConfig{ID: "a", Symbol: "BTCUSDT"}, Inner: rt}
	m, shutdown, err := br.ownedCloseManager(t.Context())
	if err != nil || shutdown {
		t.Fatal(err)
	}
	defer m.Stop()
	other, _, err := br.ownedCloseManager(t.Context())
	if err != nil || other != m {
		t.Fatal("request created disconnected manager")
	}
	r, err := m.ClosePositions(t.Context(), "SELL", 0.5, config.ClosePositionConfig{Method: "limit", PriceOffset: 10})
	if err != nil {
		t.Fatal(err)
	}
	if records := br.GetCloseRecords(); len(records) != 1 || records[0].RecordID != r.RecordID || records[0].ClientOrderID == "" {
		t.Fatalf("record not queryable: %+v", records)
	}
	sealRuntimeShutdown(rt)
	if err := prepareRuntimeShutdown(t.Context(), rt, true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.ClosePositions(t.Context(), "SELL", 0.5, config.ClosePositionConfig{Method: "market"}); err == nil {
		t.Fatal("stopped worker admitted close")
	}
	if _, err := rt.ExchangeExecutor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 0.5, ReduceOnly: true, ClientOrderID: "late"}); !errors.Is(err, order.ErrRuntimeStopping) {
		t.Fatalf("ordinary late close escaped: %v", err)
	}
	if records := br.GetCloseRecords(); records[0].Status != position.CloseStatusUnknown && records[0].Status != position.CloseStatusCanceled {
		t.Fatalf("lost unresolved close: %+v", records[0])
	}
}

func TestShutdownCloseUsesSeparateScopedManager(t *testing.T) {
	v := &shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	rt := newShutdownRuntime(v, "a", "account")
	br := &BotRuntime{BotID: "a", Inner: rt}
	normal, _, err := br.ownedCloseManager(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer normal.Stop()
	sealRuntimeShutdown(rt)
	ctx, err := rt.ExchangeExecutor.ShutdownCloseContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	exit, shutdown, err := br.ownedCloseManager(ctx)
	if err != nil || !shutdown || exit == normal {
		t.Fatalf("shutdown not isolated: %v", err)
	}
	defer exit.Stop()
}

func TestManagedCloseRuntimeUsesPersistentOwnerJournal(t *testing.T) {
	v := &shutdownBarrierVenue{fakeCloseExchange: newFakeCloseExchange(100, 0)}
	rt := newShutdownRuntime(v, "a", "account")
	scope := execution.IntentScope{Account: "account", Exchange: "fake", Market: "futures", Symbol: "BTCUSDT", Bot: "a"}
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "close.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.MigrateExecutionIntents(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := configureRuntimeIntentJournal(t.Context(), rt.ExchangeExecutor, rt.SuperPositionManager.OpeningGate(), v, store, scope); err != nil {
		t.Fatal(err)
	}
	br := &BotRuntime{BotID: "a", Inner: rt}
	m, _, err := br.ownedCloseManager(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop()
	r, err := m.ClosePositions(t.Context(), "SELL", 0.5, config.ClosePositionConfig{Method: "limit", PriceOffset: 10})
	if err != nil {
		t.Fatal(err)
	}
	key, _ := scope.Key()
	rows, err := store.LoadExecutionIntents(t.Context(), key, 0, 10)
	if err != nil || len(rows) != 1 || rows[0].ClientOrderID != r.ClientOrderID || rows[0].Revision < 3 {
		t.Fatalf("managed close bypassed journal: %v %+v", err, rows)
	}
}
