package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/storage"
	"quantmesh/strategy"
)

func TestFundingIncomePendingProjectionRetryRequiresExactReceiptAndOutbox(t *testing.T) {
	request := strategy.ApplyFundingRecoveryIncomeRequest{EpisodeID: "episode", OperationID: "operation", ExpectedEpisodeRevision: 7}
	receipt := accounting.RecoveryReceipt{EpisodeID: request.EpisodeID, OperationID: request.OperationID,
		EpisodeRevision: 8, State: accounting.RecoveryLedgerCommitted, ProjectionHash: strings.Repeat("a", 64)}
	outbox := accounting.RecoveryOutbox{EpisodeID: request.EpisodeID, OperationID: request.OperationID,
		TargetEpisodeRevision: 8, State: accounting.RecoveryProjectionPending, ProjectionHash: receipt.ProjectionHash}
	if !fundingIncomePendingProjectionMatches(receipt, outbox, request) {
		t.Fatal("exact pending projection was not eligible for retry")
	}
	tests := []struct {
		name   string
		mutate func(*accounting.RecoveryReceipt, *accounting.RecoveryOutbox, *strategy.ApplyFundingRecoveryIncomeRequest)
	}{
		{name: "receipt episode", mutate: func(r *accounting.RecoveryReceipt, _ *accounting.RecoveryOutbox, _ *strategy.ApplyFundingRecoveryIncomeRequest) {
			r.EpisodeID = "other"
		}},
		{name: "receipt operation", mutate: func(r *accounting.RecoveryReceipt, _ *accounting.RecoveryOutbox, _ *strategy.ApplyFundingRecoveryIncomeRequest) {
			r.OperationID = "other"
		}},
		{name: "receipt state", mutate: func(r *accounting.RecoveryReceipt, _ *accounting.RecoveryOutbox, _ *strategy.ApplyFundingRecoveryIncomeRequest) {
			r.State = accounting.RecoveryProjected
		}},
		{name: "receipt revision", mutate: func(r *accounting.RecoveryReceipt, _ *accounting.RecoveryOutbox, _ *strategy.ApplyFundingRecoveryIncomeRequest) {
			r.EpisodeRevision++
		}},
		{name: "outbox episode", mutate: func(_ *accounting.RecoveryReceipt, o *accounting.RecoveryOutbox, _ *strategy.ApplyFundingRecoveryIncomeRequest) {
			o.EpisodeID = "other"
		}},
		{name: "outbox operation", mutate: func(_ *accounting.RecoveryReceipt, o *accounting.RecoveryOutbox, _ *strategy.ApplyFundingRecoveryIncomeRequest) {
			o.OperationID = "other"
		}},
		{name: "outbox state", mutate: func(_ *accounting.RecoveryReceipt, o *accounting.RecoveryOutbox, _ *strategy.ApplyFundingRecoveryIncomeRequest) {
			o.State = accounting.RecoveryProjected
		}},
		{name: "revision", mutate: func(_ *accounting.RecoveryReceipt, o *accounting.RecoveryOutbox, _ *strategy.ApplyFundingRecoveryIncomeRequest) {
			o.TargetEpisodeRevision++
		}},
		{name: "projection hash", mutate: func(_ *accounting.RecoveryReceipt, o *accounting.RecoveryOutbox, _ *strategy.ApplyFundingRecoveryIncomeRequest) {
			o.ProjectionHash = strings.Repeat("b", 64)
		}},
		{name: "request revision", mutate: func(_ *accounting.RecoveryReceipt, _ *accounting.RecoveryOutbox, r *strategy.ApplyFundingRecoveryIncomeRequest) {
			r.ExpectedEpisodeRevision++
		}},
		{name: "revision overflow", mutate: func(_ *accounting.RecoveryReceipt, o *accounting.RecoveryOutbox, r *strategy.ApplyFundingRecoveryIncomeRequest) {
			r.ExpectedEpisodeRevision = accounting.MaxDurableRecoveryRevision - 1
			o.TargetEpisodeRevision = r.ExpectedEpisodeRevision + 1
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changedReceipt, changedOutbox, changedRequest := receipt, outbox, request
			test.mutate(&changedReceipt, &changedOutbox, &changedRequest)
			if fundingIncomePendingProjectionMatches(changedReceipt, changedOutbox, changedRequest) {
				t.Fatal("mismatched pending receipt/outbox was admitted by the production resolver")
			}
		})
	}
}

type recoveryBindingTestRuntimeState struct {
	store      storage.FundingRecoveryStore
	generation storage.FundingCarryRuntimeGeneration
	botID      string
	scopes     []string
}

func (r *recoveryBindingTestRuntimeState) LoadRuntimeState(string) (int, string, bool, error) {
	return 0, "", false, nil
}

func (r *recoveryBindingTestRuntimeState) SaveRuntimeState(string, int, string) error { return nil }

func (r *recoveryBindingTestRuntimeState) FundingCarryRecoveryBinding() (storage.FundingRecoveryStore, storage.FundingCarryRuntimeGeneration, string, []string, error) {
	return r.store, r.generation, r.botID, append([]string(nil), r.scopes...), nil
}

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
	if service.Repository == nil || service.Evidence == nil || service.RecoveryRuntimeResolver == nil {
		t.Fatalf("durable repository, evidence provider and read-only runtime resolver must be installed: %+v", service)
	}
	if service.Accounting != nil || service.Gate != nil {
		t.Fatalf("unsafe accounting or gate capability was installed: %+v", service)
	}
	evidenceProvider, ok := service.Evidence.(*runtimeTrustedOrderEvidenceProvider)
	if !ok || evidenceProvider.bots != bots {
		t.Fatalf("service must use the supplied BotManager for read-only evidence: %#v", service.Evidence)
	}
	blockedEpisode := accounting.RecoveryEpisode{ID: "episode-no-runtime", Bot: "bot-no-runtime", Strategy: "funding_carry", State: accounting.RecoveryBlocked, RecoveryGeneration: 1}
	if _, err := service.ResolveFundingRecoveryRuntime(ctx, blockedEpisode); err == nil {
		t.Fatal("runtime resolver must fail closed when the bot has no retained reconciliation runtime")
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

func TestProductionRecoveryResolverRequiresRetainedExactOwnerAndHeldEpisode(t *testing.T) {
	ctx := context.Background()
	store := newOrderReconciliationServiceTestStorage(t)
	if err := store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal(err)
	}
	scope := fmt.Sprintf("%064x", 909)
	oldGeneration, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{SchemaVersion: 1, Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Side: "BUY", OrderType: "LIMIT", TimeInForce: "GTC", Price: "100", Quantity: "0.1", ClientOrderID: "resolver-original-cid"}
	intentHash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	action := accounting.RecoveryAction{ID: "resolver-action", Ordinal: 1, Kind: accounting.RecoveryActionOrder, Role: "open_futures", AccountScope: scope, Market: intent.Market, Symbol: intent.Symbol, OwnerGeneration: oldGeneration.ID(), OrderIntentHash: intentHash, Status: "authorized"}
	episode := accounting.RecoveryEpisode{
		ID: "episode-runtime-resolver", Bot: "bot-runtime-resolver", Strategy: "funding_carry", AccountScopes: []string{scope},
		OwnerGeneration: oldGeneration.ID(), Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1, State: accounting.RecoveryPrepared, ActionPlan: []accounting.RecoveryAction{action},
	}
	if _, err := store.CreateRecoveryEpisode(ctx, oldGeneration, episode); err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{ID: "resolver-permit", EpisodeID: episode.ID, Generation: 1, ActionID: action.ID, ActionOrdinal: 1, Kind: accounting.RecoveryActionOrder, AccountScope: scope, OwnerGeneration: oldGeneration.ID(), LeaseID: 1, OrderIntentHash: intentHash, OrderIntent: &intent, ExpectedRevision: 1, State: accounting.PermitAuthorized}
	if _, err := store.CreateRecoveryPermit(ctx, oldGeneration, permit); err != nil {
		t.Fatal(err)
	}
	if _, maySubmit, err := store.ConsumeRecoveryPermit(ctx, oldGeneration, permit); err != nil || !maySubmit {
		t.Fatalf("consume unresolved permit: maySubmit=%t err=%v", maySubmit, err)
	}
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	carry := strategy.NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil, nil)
	carry.SetRuntimeStateStore(&recoveryBindingTestRuntimeState{store: store, generation: generation, botID: episode.Bot, scopes: []string{scope}})
	manager := strategy.NewStrategyManager(&config.Config{}, 0)
	manager.RegisterStrategy("funding_carry", carry, 1, 0)
	bots := &BotManager{runtimes: map[string]*BotRuntime{episode.Bot: {
		BotID: episode.Bot, Inner: &SymbolRuntime{fundingCarryReconciliationOnly: true, StrategyManager: manager},
	}}}
	service, err := newProductionOrderReconciliationService(store, bots)
	if err != nil {
		t.Fatal(err)
	}
	if service.RecoveryAnomalyAdjustmentApply == nil || service.RecoveryFundingIncomeApply == nil {
		t.Fatal("production service did not register funding-history anomaly capture/adjustment handlers")
	}
	if _, err := service.ResolveFundingRecoveryRuntime(ctx, episode); err == nil {
		t.Fatal("found adjudication resolver must reject the old owner before explicit API takeover")
	}
	takenOver, replayed, err := service.RecoveryOwnerTakeover(ctx, episode.ID, "resolver-takeover", "admin", "incident-resolver", episode.Revision, true)
	if err != nil || replayed || takenOver.OwnerGeneration != generation.ID() || takenOver.State != accounting.RecoveryBlocked {
		t.Fatalf("production service takeover failed: episode=%+v replayed=%t err=%v", takenOver, replayed, err)
	}
	if _, err := service.ResolveFundingRecoveryRuntime(ctx, takenOver); err != nil {
		t.Fatalf("exact held runtime should resolve after explicit takeover without another claim: %v", err)
	}
	if _, replayed, err = service.RecoveryOwnerTakeover(ctx, episode.ID, "resolver-takeover", "admin", "incident-resolver", episode.Revision, true); err != nil || !replayed {
		t.Fatalf("same takeover request retry: replayed=%t err=%v", replayed, err)
	}
	wrongScope := takenOver
	wrongScope.AccountScopes = []string{fmt.Sprintf("%064x", 910)}
	if _, err := service.ResolveFundingRecoveryRuntime(ctx, wrongScope); err == nil {
		t.Fatal("runtime resolver accepted different account scope")
	}
	bots.runtimes[episode.Bot].Inner.fundingCarryReconciliationOnly = false
	if _, err := service.ResolveFundingRecoveryRuntime(ctx, takenOver); err == nil {
		t.Fatal("runtime resolver accepted a normal/non-reconciliation runtime")
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
