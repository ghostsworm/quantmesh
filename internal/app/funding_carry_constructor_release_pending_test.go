package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/storage"
	"quantmesh/strategy"
)

func TestFundingCarryFullConstructorRetainsReleasePendingAfterRestart(t *testing.T) {
	ctx := t.Context()
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "test-key"}}}
	cfg.Storage.Enabled, cfg.Storage.Type, cfg.Storage.Path = true, "sqlite", filepath.Join(t.TempDir(), "release-pending.db")
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 1, 1
	cfg.Timing.PriceSendInterval = 100
	cfg.Timing.SkipExchangeFeeOnBotStart = true
	cfg.Strategies.Configs = map[string]config.StrategyConfig{"funding_carry": {Enabled: true, Type: "funding_carry", Weight: 1}}
	original, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(original.Stop)
	store := original.GetStorage().(*storage.SQLStorage)
	ownershipScopes, err := fundingCarryRuntimeOwnershipScopes(cfg, "binance", "BTCUSDT", true)
	if err != nil {
		t.Fatal(err)
	}
	var scopes []string
	var spotScope string
	for _, scope := range ownershipScopes {
		key, err := scope.Key()
		if err != nil {
			t.Fatal(err)
		}
		scopes = append(scopes, key)
		if scope.Market == "spot" {
			spotScope = key
		}
	}
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, scopes)
	if err != nil {
		t.Fatal(err)
	}
	const botID = "constructor-release-pending"
	episode := accounting.RecoveryEpisode{ID: "constructor-release-episode", Bot: botID, Strategy: "funding_carry",
		AccountScopes: generation.ScopeKeys(), OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared}
	if _, err := store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal(err)
	}
	const runtimeSchema = 8
	const runtimePayload = `{"strategy":"funding_carry","futures_exchange":"binance","spot_exchange":"binance","symbol":"BTCUSDT","ownership_ready":true,"recovery_projection_schema":1}`
	if err := store.SetFundingCarryRuntimeState(ctx, generation, &storage.StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: runtimeSchema, Payload: runtimePayload}); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(runtimePayload))
	observedAt := time.Now().UTC()
	feeEvent := accounting.RecoveryEvent{ID: "constructor-release-fee", EpisodeID: episode.ID,
		Source: "venue", Exchange: "binance", Market: "spot", AccountScope: spotScope,
		Kind: accounting.RecoveryFeeEvent, ExternalID: "constructor-commission-1", Asset: "USDT", Amount: "0.001",
		SourceRevision: "commission-row-1", SourceHash: fmt.Sprintf("%064x", 92), ObservedAt: observedAt}
	operation := accounting.RecoveryOperation{EpisodeID: episode.ID, OperationID: "constructor-release-projection", HashVersion: 1,
		ExpectedEpisodeRevision: episode.Revision, ExpectedRuntimeRevision: fmt.Sprintf("%d:%x", runtimeSchema, digest[:]),
		ExpectedEventRevisions: map[string]uint64{}, Events: []accounting.RecoveryEvent{feeEvent},
		Actor: "operator", AuditRef: "constructor-release-incident", EvidenceHash: fmt.Sprintf("%064x", 91), EvidenceReadAt: observedAt}
	if _, replayed, err := store.ApplyRecoveryOperation(ctx, generation, operation); err != nil || replayed {
		t.Fatalf("prepare original projection: replayed=%t err=%v", replayed, err)
	}
	// Use the real managed strategy startup to create the runtime projection and
	// its receipt before committing release preparation under the original owner.
	carry := strategy.NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"},
		&constructorRecoveryVenue{market: "futures"}, &constructorRecoveryVenue{market: "spot"}, nil, nil)
	carry.SetRuntimeStateStore(&fundingCarryRuntimeStateAdapter{
		strategyRuntimeStateAdapter: &strategyRuntimeStateAdapter{storageService: original, botID: botID}, generation: generation,
	})
	gate := &execution.OpeningGate{}
	carry.SetOpeningGate(gate)
	manager := strategy.NewStrategyManager(cfg, 0)
	manager.RegisterStrategy("funding_carry", carry, 1, 0)
	if retained, err := startFundingCarryManagedStrategy(ctx, manager, carry, gate); err != nil || !retained {
		t.Fatalf("complete original projection under its owner: retained=%t err=%v", retained, err)
	}
	episode, found, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found {
		t.Fatalf("read projected episode: found=%t err=%v", found, err)
	}
	hold, found, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found {
		t.Fatalf("read original hold: found=%t err=%v", found, err)
	}
	outbox, found, err := store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !found {
		t.Fatalf("read completed projection: found=%t err=%v", found, err)
	}
	auth := accounting.RecoveryReleaseAuthorization{EpisodeID: episode.ID, OperationID: "constructor-release-prepare",
		Actor: "operator", AuditRef: "constructor-release-incident", EvidenceHash: outbox.ProjectionHash, ExposureClearanceHash: outbox.ProjectionHash,
		ProjectionHash: outbox.ProjectionHash, RuntimeRevision: outbox.ExpectedRuntimeRevision, OwnerGeneration: generation.ID(),
		ExpectedEpisodeRevision: episode.Revision, RecoveryGeneration: episode.RecoveryGeneration, ExpectedHoldRevision: hold.Revision,
		Confirmed: true, ExposureClearanceConfirmed: true}
	pending, err := store.PrepareRecoveryRelease(ctx, generation, auth)
	if err != nil || pending.State != accounting.RecoveryReleasePending {
		t.Fatalf("commit release preparation: state=%s err=%v", pending.State, err)
	}
	receipt, found, err := store.GetRecoveryReceipt(ctx, episode.ID, auth.OperationID)
	if err != nil || !found || receipt.State != accounting.RecoveryReleasePending {
		t.Fatalf("read durable release receipt: found=%t err=%v", found, err)
	}
	snapshot, err := store.GetStrategyRuntimeState(botID, "funding_carry")
	if err != nil || snapshot == nil {
		t.Fatalf("read original runtime projection: %v", err)
	}
	original.Stop()

	restarted, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal("reopen persisted startup state:", err)
	}
	t.Cleanup(restarted.Stop)
	venues := map[string]*constructorRecoveryVenue{}
	deps := fundingCarryStartupDependencies{
		checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
			return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
		},
		newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
			venue := &constructorRecoveryVenue{market: market}
			venues[market] = venue
			return venue, nil
		},
	}
	symbol := config.SymbolConfig{ID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
		Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
	runtime, err := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, symbol, nil, restarted, lock.NewNopLock(), nil, nil, deps)
	if err != nil || runtime == nil {
		t.Fatalf("full constructor lost release_pending reconciliation runtime: %v", err)
	}
	t.Cleanup(func() { runtime.Stop() })
	retained := runtime.StrategyManager.GetStrategy("funding_carry").(*strategy.FundingCarryStrategy)
	if !runtime.fundingCarryReconciliationOnly || retained.IsRunning() || !runtime.OpeningGate.HasBlock(hold.Source) {
		t.Fatal("restarted constructor started trading or lost the exact held gate")
	}
	_, newGeneration, boundBot, boundScopes, err := retained.CurrentFundingCarryRecoveryBinding()
	if err != nil || newGeneration.ID() == generation.ID() || boundBot != botID || !sameFundingRecoveryScopes(boundScopes, scopes) {
		t.Fatalf("fresh runtime binding did not preserve bot/scopes: %v", err)
	}
	assertConstructorRecoveryEvidence(t, restarted, botID, snapshot.Payload)
	for market, venue := range venues {
		if venue.mutations.Load() != 0 || venue.fillQueries.Load() != 0 {
			t.Fatalf("%s replayed financial operations or fill recovery during release_pending startup", market)
		}
	}
	store = restarted.GetStorage().(*storage.SQLStorage)
	afterEpisode, _, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !reflect.DeepEqual(afterEpisode, pending) {
		t.Fatalf("constructor changed episode ownership/release state: %v", err)
	}
	afterHold, _, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !reflect.DeepEqual(afterHold, hold) {
		t.Fatalf("constructor changed held hold: %v", err)
	}
	afterReceipt, _, err := store.GetRecoveryReceipt(ctx, episode.ID, auth.OperationID)
	if err != nil || !reflect.DeepEqual(afterReceipt, receipt) {
		t.Fatalf("constructor changed release receipt: %v", err)
	}
	afterOutbox, _, err := store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !reflect.DeepEqual(afterOutbox, outbox) {
		t.Fatalf("constructor changed completed projection: %v", err)
	}
	// 將真實 constructor 保留的 runtime 接入生產 service，再由管理員顯式續辦。
	runtime.OpeningGate.Block("manual_constructor_hold")
	runtime.OpeningGate.Block(execution.RecoveryEpisodeGateSource("another-episode", 1))
	bots := &BotManager{runtimes: map[string]*BotRuntime{botID: {BotID: botID, Inner: runtime}}}
	service, err := newProductionOrderReconciliationService(store, bots)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.RecoveryReleaseResumePreview(ctx, episode.ID, auth.OperationID)
	if err != nil {
		t.Fatal("preview release resume through retained constructor runtime:", err)
	}
	result, err := service.RecoveryReleaseResumeApply(ctx, accounting.RecoveryReleaseResumeAuthorization{
		EpisodeID: episode.ID, OperationID: "constructor-admin-resume", ReleaseOperationID: auth.OperationID, ReleaseRequestHash: preview.ReleaseReceipt.RequestHash,
		Actor: "resume-admin", AuditRef: "constructor-resume-incident", EvidenceHash: preview.EvidenceHash,
		ExpectedOwnerGeneration: preview.Episode.OwnerGeneration, OwnerGeneration: preview.OwnerGeneration, AccountScopes: boundScopes,
		ExpectedEpisodeRevision: preview.Episode.Revision, ExpectedHoldRevision: preview.Hold.Revision, RecoveryGeneration: preview.Episode.RecoveryGeneration, Confirmed: true,
	})
	if err != nil || !result.Verified || result.ExecutionReplayed || result.Replayed || retained.IsRunning() ||
		runtime.OpeningGate.HasBlock(hold.Source) || !runtime.OpeningGate.HasBlock("manual_constructor_hold") ||
		!runtime.OpeningGate.HasBlock(execution.RecoveryEpisodeGateSource("another-episode", 1)) {
		t.Fatalf("constructor admin resume failed exact stopped-runtime/Gate invariants: result=%+v err=%v", result, err)
	}
	for market, venue := range venues {
		if venue.mutations.Load() != 0 || venue.fillQueries.Load() != 0 {
			t.Fatalf("%s issued financial operations during release resume", market)
		}
	}
}
