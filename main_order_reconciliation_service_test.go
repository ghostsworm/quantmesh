package main

import (
	"context"
	"path/filepath"
	"testing"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/storage"
)

func TestNewProductionOrderReconciliationServiceRejectsMissingDependencies(t *testing.T) {
	bots := &BotManager{}

	if service, err := newProductionOrderReconciliationService(nil, bots); err == nil || service != nil {
		t.Fatalf("nil storage must fail without a partial service: service=%+v err=%v", service, err)
	}

	store := newOrderReconciliationServiceTestStorage(t)
	if service, err := newProductionOrderReconciliationService(store, nil); err == nil || service != nil {
		t.Fatalf("nil BotManager must fail without a partial service: service=%+v err=%v", service, err)
	}
}

func TestBuildProductionOrderReconciliationServiceRejectsUnsupportedStorage(t *testing.T) {
	if service, err := buildProductionOrderReconciliationService(nil, &BotManager{}); err == nil || service != nil {
		t.Fatalf("missing initialized SQL storage must fail closed: service=%+v err=%v", service, err)
	}
}

func TestOrderReconciliationSQLiteMigrationFailureIsReturned(t *testing.T) {
	store := newOrderReconciliationServiceTestStorage(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := store.MigrateOrderReconciliations(context.Background()); err == nil {
		t.Fatal("migration against a closed isolated database must fail")
	}
}

func TestNewProductionOrderReconciliationServiceUsesInitializedSchemaAndPersistsCaseAudit(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "production-composition.db")
	storageService := newOrderReconciliationServiceTestService(t, dbPath)
	bots := &BotManager{}
	service, err := buildProductionOrderReconciliationService(storageService.GetStorage(), bots)
	if err != nil {
		t.Fatalf("first service initialization failed: %v", err)
	}
	if service.Repository == nil || service.Evidence == nil {
		t.Fatalf("durable repository and evidence provider must be installed: %+v", service)
	}
	if service.Accounting != nil || service.Gate != nil {
		t.Fatalf("unsafe accounting or gate capability was installed: %+v", service)
	}
	evidenceProvider, ok := service.Evidence.(*runtimeTrustedOrderEvidenceProvider)
	if !ok || evidenceProvider.bots != bots {
		t.Fatalf("service must use the supplied BotManager for read-only evidence: %#v", service.Evidence)
	}

	input := storage.NewOrderReconciliationCase{
		ID:                       "case-production-composition",
		IdempotencyKey:           "request-production-composition",
		Owner:                    storageTestReconciliationOwner(),
		ExpectedIntentRevision:   "intent-rev-1",
		ExpectedStrategyRevision: "strategy-rev-1",
		Reason:                   "verify production composition persistence",
		Actor:                    "test-admin",
		EvidenceRefs:             []string{"incident-test"},
	}
	created, replayed, err := service.Repository.CreateOrderReconciliationCase(ctx, input)
	if err != nil || replayed {
		t.Fatalf("create case through composed repository failed: replayed=%t err=%v", replayed, err)
	}
	audit, err := service.Repository.ListOrderReconciliationAudit(ctx, created.ID)
	if err != nil || len(audit) != 1 || audit[0].CaseID != created.ID || audit[0].Action != "case_created" {
		t.Fatalf("audit readback through production composition failed: audit=%+v err=%v", audit, err)
	}
	storageService.Stop()

	// A new StorageService runs the real startup migrations against the same
	// isolated file before production composition is rebuilt.
	restartedStorage := newOrderReconciliationServiceTestService(t, dbPath)
	restarted, err := buildProductionOrderReconciliationService(restartedStorage.GetStorage(), bots)
	if err != nil {
		t.Fatalf("service reinitialization after StorageService restart failed: %v", err)
	}
	if restarted.Accounting != nil || restarted.Gate != nil {
		t.Fatalf("reinitialization installed unsafe capabilities: %+v", restarted)
	}
	readback, found, err := restarted.Repository.GetOrderReconciliation(ctx, created.ID)
	if err != nil || !found || readback.ID != created.ID || readback.Owner != input.Owner {
		t.Fatalf("case readback through initial composition failed: case=%+v found=%t err=%v", readback, found, err)
	}
	audit, err = restarted.Repository.ListOrderReconciliationAudit(ctx, created.ID)
	if err != nil || len(audit) != 1 || audit[0].CaseID != created.ID || audit[0].Action != "case_created" {
		t.Fatalf("audit readback through initial composition failed: audit=%+v err=%v", audit, err)
	}
}

func newOrderReconciliationServiceTestStorage(t *testing.T) *storage.SQLStorage {
	t.Helper()
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "order-reconciliation.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated SQLite storage: %v", err)
		}
	})
	return store
}

func newOrderReconciliationServiceTestService(t *testing.T, dbPath string) *storage.StorageService {
	t.Helper()
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = dbPath
	cfg.Storage.BufferSize = 16
	cfg.Storage.BatchSize = 4
	service, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("initialize isolated production StorageService: %v", err)
	}
	t.Cleanup(service.Stop)
	return service
}

func storageTestReconciliationOwner() accounting.OrderOwner {
	return accounting.OrderOwner{
		Exchange: "fake", Market: "spot", AccountScope: "test-account", Bot: "test-bot",
		Symbol: "BTCUSDT", StrategyName: "test-strategy", StrategyType: "grid",
		ClientOrderID: "test-client-order", VenueOrderID: "42",
	}
}
