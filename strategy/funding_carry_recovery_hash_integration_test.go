package strategy

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/storage"
)

func TestFundingCarryRecoveryProjectsSemanticAndLegacyOutboxesAndRetries(t *testing.T) {
	tests := []struct {
		name       string
		legacyHash bool
	}{
		{name: "semantic hash survives JSON representation normalization"},
		{name: "legacy raw hash survives JSON representation normalization", legacyHash: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFundingCarryRecoveryHashIntegrationFixture(t, test.legacyHash)
			if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err == nil || !strings.Contains(err.Error(), "injected projection mark failure") {
				t.Fatalf("first recovery pass did not stop at injected durable mark failure: %v", err)
			}
			if fixture.runtimeStore.markCalls() != 1 {
				t.Fatalf("first recovery pass mark attempts = %d, want 1", fixture.runtimeStore.markCalls())
			}
			outbox, found, err := fixture.store.GetRecoveryOutbox(fixture.ctx, fixture.episode.ID, fixture.operationID)
			if err != nil || !found || outbox.State != accounting.RecoveryProjectionPending {
				t.Fatalf("failed mark did not retain pending outbox: outbox=%+v found=%t err=%v", outbox, found, err)
			}
			var projected fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(fixture.runtime.payload), &projected); err != nil || len(projected.RecoveryProjectionMarkers) != 1 {
				t.Fatalf("runtime CAS was not persisted before the retry: markers=%+v err=%v", projected.RecoveryProjectionMarkers, err)
			}

			// Retry the real FundingCarry reconciliation entry point. It must read
			// the already-projected runtime marker, complete the durable outbox CAS,
			// and keep the operator hold active.
			err = fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx)
			var held *FundingCarryDurableRecoveryRequiredError
			if !errors.As(err, &held) {
				t.Fatalf("retry should finish projection but retain the human-reconciliation hold; got %v", err)
			}
			if fixture.runtimeStore.markCalls() != 2 {
				t.Fatalf("retry mark attempts = %d, want 2", fixture.runtimeStore.markCalls())
			}
			projectedOutbox, found, err := fixture.store.GetRecoveryOutbox(fixture.ctx, fixture.episode.ID, fixture.operationID)
			if err != nil || !found || projectedOutbox.State != accounting.RecoveryProjected || projectedOutbox.AppliedHash != projectedOutbox.ProjectionHash {
				t.Fatalf("retry did not durably mark the exact projection: outbox=%+v found=%t err=%v", projectedOutbox, found, err)
			}
			receipt, found, err := fixture.store.GetRecoveryReceipt(fixture.ctx, fixture.episode.ID, fixture.operationID)
			if err != nil || !found || receipt.State != accounting.RecoveryProjected || receipt.ProjectionHash != projectedOutbox.ProjectionHash {
				t.Fatalf("retry receipt readback is inconsistent: receipt=%+v found=%t err=%v", receipt, found, err)
			}
			hold, found, err := fixture.store.GetRecoveryHold(fixture.ctx, fixture.episode.ID, fixture.episode.RecoveryGeneration)
			if err != nil || !found || hold.State != "held" {
				t.Fatalf("projection retry changed the human-reconciliation hold: hold=%+v found=%t err=%v", hold, found, err)
			}
			events, err := fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID)
			if err != nil || len(events) != 1 || events[0].ID != "event-recovery-hash-integration" {
				t.Fatalf("projection retry changed durable ledger facts: events=%+v err=%v", events, err)
			}
		})
	}
}

func TestFundingCarryDurableProjectionRejectsReceiptOnlyMismatchBeforeRuntimeCAS(t *testing.T) {
	fixture := newFundingCarryRecoveryHashIntegrationFixture(t, false)
	stateBinding := fixture.strategy.runtimeStateStore.(*fundingCarryRecoveryHashRuntimeStore)
	provider := stateBinding.provider
	underlying := provider.repository
	provider.repository = &tamperedFundingCarryRecoveryReceiptReader{FundingRecoveryStore: underlying,
		episodeID: fixture.episode.ID, operationID: fixture.operationID}
	fixture.runtime.mu.Lock()
	versionBefore, payloadBefore, callsBefore := fixture.runtime.version, fixture.runtime.payload, fixture.runtime.compareCalls
	fixture.runtime.mu.Unlock()
	if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err == nil || !strings.Contains(err.Error(), "receipt/outbox") {
		t.Fatalf("durable projection accepted receipt-only mismatch: %v", err)
	}
	fixture.runtime.mu.Lock()
	if fixture.runtime.version != versionBefore || fixture.runtime.payload != payloadBefore || fixture.runtime.compareCalls != callsBefore {
		fixture.runtime.mu.Unlock()
		t.Fatal("receipt-only mismatch reached or changed runtime CAS")
	}
	fixture.runtime.mu.Unlock()
	outbox, found, err := fixture.store.GetRecoveryOutbox(fixture.ctx, fixture.episode.ID, fixture.operationID)
	if err != nil || !found || outbox.State != accounting.RecoveryProjectionPending {
		t.Fatalf("rejected projection changed durable outbox: outbox=%+v found=%t err=%v", outbox, found, err)
	}
}

func TestFundingCarryDurableProjectionRejectsExpectedRuntimeRevisionMismatchBeforeRuntimeCAS(t *testing.T) {
	fixture := newFundingCarryRecoveryHashIntegrationFixture(t, false)
	stateBinding := fixture.strategy.runtimeStateStore.(*fundingCarryRecoveryHashRuntimeStore)
	provider := stateBinding.provider
	provider.repository = &tamperedFundingCarryRecoveryOutboxReader{FundingRecoveryStore: provider.repository,
		episodeID: fixture.episode.ID, operationID: fixture.operationID, mutation: "expected_runtime_revision"}
	fixture.runtime.mu.Lock()
	versionBefore, payloadBefore, callsBefore := fixture.runtime.version, fixture.runtime.payload, fixture.runtime.compareCalls
	fixture.runtime.mu.Unlock()
	if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err == nil || !strings.Contains(err.Error(), "runtime revision") {
		t.Fatalf("durable projection accepted an outbox runtime revision not bound by its hashed projection: %v", err)
	}
	fixture.runtime.mu.Lock()
	if fixture.runtime.version != versionBefore || fixture.runtime.payload != payloadBefore || fixture.runtime.compareCalls != callsBefore {
		fixture.runtime.mu.Unlock()
		t.Fatal("runtime revision mismatch reached or changed runtime CAS")
	}
	fixture.runtime.mu.Unlock()
}

func TestFundingCarryCompletedLegacyReplayRequiresManualReconciliation(t *testing.T) {
	fixture := newFundingCarryRecoveryHashIntegrationFixture(t, false)
	if err := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx); err == nil || !strings.Contains(err.Error(), "injected projection mark failure") {
		t.Fatalf("seed response-loss projection: %v", err)
	}
	finishErr := fixture.strategy.reconcileDurableFundingCarryRecovery(fixture.ctx)
	var held *FundingCarryDurableRecoveryRequiredError
	if !errors.As(finishErr, &held) {
		t.Fatalf("finish projection while preserving hold: %v", finishErr)
	}
	outbox, found, err := fixture.store.GetRecoveryOutbox(fixture.ctx, fixture.episode.ID, fixture.operationID)
	if err != nil || !found || outbox.State != accounting.RecoveryProjected {
		t.Fatalf("read completed projection fixture: outbox=%+v found=%t err=%v", outbox, found, err)
	}
	stateBinding := fixture.strategy.runtimeStateStore.(*fundingCarryRecoveryHashRuntimeStore)
	legacyStore := &legacyFundingCarryRecoveryOutboxReader{FundingRecoveryStore: stateBinding.provider.repository,
		episodeID: fixture.episode.ID, operationID: fixture.operationID}
	if err := fixture.strategy.projectFundingRecoveryFillOperation(fixture.ctx,
		fundingCarryRecoveryBinding{store: legacyStore, generation: stateBinding.provider.generation,
			botID: fixture.episode.Bot, scopeKeys: fixture.episode.AccountScopes, stateStore: fixture.runtimeStore},
		fixture.episode.ID, fixture.operationID); err == nil || !strings.Contains(err.Error(), "manual reconciliation") {
		t.Fatalf("completed legacy projection replay bypassed manual reconciliation: %v", err)
	}
}

func TestFundingCarryFillResponseLossRetryRejectsUnhashedProjectionTamper(t *testing.T) {
	fixture := newFundingRecoveryFillFixture(t, nil, true, true, true)
	preview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil {
		t.Fatal("preview fill response-loss fixture:", err)
	}
	request := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "fill-hash-tamper-retry",
		ExpectedEpisodeRevision: preview.EpisodeRevision, ExpectedChildRevision: preview.ChildRevision,
		PreviewSourceRevision: preview.Batch.SourceRevision, Actor: "operator", AuditRef: "incident-fill-hash-tamper", Confirmed: true}
	fixture.runtime.casErr = errors.New("inject response loss after durable fill receipt")
	if result, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request); err == nil || result.Projected || !result.HoldRetained {
		t.Fatalf("initial fill must leave a pending durable response-loss retry: result=%+v err=%v", result, err)
	}
	fixture.runtime.casErr = nil
	assertFundingCarryRetryRejectsTamperedOutbox(t, fixture, request.OperationID, "unknown", "verify FundingCarry fill response-loss outbox", func() error {
		_, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request)
		return err
	})
}

func TestFundingCarryFillCompletedOperationReplayRejectsUnhashedProjectionTamper(t *testing.T) {
	fixture := newFundingRecoveryFillFixture(t, nil, true, true, true)
	preview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil {
		t.Fatal("preview completed fill replay fixture:", err)
	}
	request := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "fill-completed-hash-replay",
		ExpectedEpisodeRevision: preview.EpisodeRevision, ExpectedChildRevision: preview.ChildRevision,
		PreviewSourceRevision: preview.Batch.SourceRevision, Actor: "operator", AuditRef: "incident-fill-completed-hash-replay", Confirmed: true}
	result, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request)
	if err != nil || !result.Projected || result.Replayed || !result.HoldRetained {
		t.Fatalf("first fill request must complete and project successfully: result=%+v err=%v", result, err)
	}
	assertFundingCarryRetryRejectsTamperedOutbox(t, fixture, request.OperationID, "unknown", "verify FundingCarry fill response-loss outbox", func() error {
		_, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request)
		return err
	})
}

func TestFundingCarryFillRejectsReceiptOnlyMismatchBeforeRuntimeCAS(t *testing.T) {
	for _, completed := range []bool{false, true} {
		name := "pending"
		if completed {
			name = "completed"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newFundingRecoveryFillFixture(t, nil, true, true, true)
			preview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
			if err != nil {
				t.Fatal("preview receipt mismatch fixture:", err)
			}
			request := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID,
				OperationID: "fill-receipt-only-" + name, ExpectedEpisodeRevision: preview.EpisodeRevision,
				ExpectedChildRevision: preview.ChildRevision, PreviewSourceRevision: preview.Batch.SourceRevision,
				Actor: "operator", AuditRef: "incident-receipt-only-" + name, Confirmed: true}
			if completed {
				result, applyErr := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request)
				if applyErr != nil || !result.Projected {
					t.Fatalf("seed completed projection: result=%+v err=%v", result, applyErr)
				}
			} else {
				fixture.runtime.casErr = errors.New("injected runtime projection response loss")
				if result, applyErr := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request); applyErr == nil || result.Projected {
					t.Fatalf("seed pending projection: result=%+v err=%v", result, applyErr)
				}
				fixture.runtime.casErr = nil
			}
			stateBinding := fixture.strategy.runtimeStateStore.(*durableRecoveryRuntimeBindingFixture)
			provider := stateBinding.provider.(*durableRecoveryProviderFixture)
			underlying := provider.repository
			provider.repository = &tamperedFundingCarryRecoveryReceiptReader{FundingRecoveryStore: underlying,
				episodeID: request.EpisodeID, operationID: request.OperationID}
			before := readFundingCarryRetrySnapshot(t, fixture, request.OperationID)
			fixture.runtime.mu.Lock()
			versionBefore, payloadBefore, callsBefore := fixture.runtime.version, fixture.runtime.payload, fixture.runtime.compareCalls
			fixture.runtime.mu.Unlock()
			if _, retryErr := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request); retryErr == nil ||
				!strings.Contains(retryErr.Error(), "receipt/outbox") {
				t.Fatalf("same-operation retry accepted receipt-only mismatch: %v", retryErr)
			}
			fixture.runtime.mu.Lock()
			if fixture.runtime.version != versionBefore || fixture.runtime.payload != payloadBefore || fixture.runtime.compareCalls != callsBefore {
				fixture.runtime.mu.Unlock()
				t.Fatal("receipt-only mismatch reached or changed runtime CAS")
			}
			fixture.runtime.mu.Unlock()
			after := readFundingCarryRetrySnapshot(t, fixture, request.OperationID)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("receipt-only mismatch changed durable state: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestFundingCarryFeeResponseLossRetryRejectsUnhashedProjectionTamper(t *testing.T) {
	fill := validFundingRecoveryFill("171", "0.1", false)
	fixture := newFundingRecoveryFillFixture(t, []*exchange.OrderFill{fill}, true, false, true)
	fillPreview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil {
		t.Fatal("preview fee fill fixture:", err)
	}
	fillRequest := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "fee-hash-fill-seed",
		ExpectedEpisodeRevision: fillPreview.EpisodeRevision, ExpectedChildRevision: fillPreview.ChildRevision,
		PreviewSourceRevision: fillPreview.Batch.SourceRevision, Actor: "operator", AuditRef: "incident-fee-hash-tamper", Confirmed: true}
	if result, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, fillRequest); err != nil || !result.Projected || !result.HoldRetained {
		t.Fatalf("seed durable unknown-fee fill: result=%+v err=%v", result, err)
	}
	feePreview, err := fixture.strategy.PreviewFundingRecoveryFeeAdjustment(fixture.ctx, fixture.episode.ID, fixture.child.ID, fill.TradeID)
	if err != nil {
		t.Fatal("preview fee adjustment fixture:", err)
	}
	request := ApplyFundingRecoveryFeeAdjustmentRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, TradeID: fill.TradeID,
		OperationID: "fee-hash-tamper-retry", AdjustmentID: "fee-hash-tamper-row", AmountDeltaRaw: "0.0001", Asset: "BNB",
		EvidenceHash: strings.Repeat("a", 64), SourceRevision: feePreview.SourceRevision, ExpectedEpisodeRevision: feePreview.EpisodeRevision,
		ExpectedChildRevision: feePreview.ChildRevision, Actor: "operator", AuditRef: "incident-fee-hash-tamper", Confirmed: true}
	fixture.runtime.casErr = errors.New("inject response loss after durable fee receipt")
	if result, err := fixture.strategy.ApplyFundingRecoveryFeeAdjustment(fixture.ctx, request); err == nil || result.Projected || !result.HoldRetained {
		t.Fatalf("initial fee adjustment must leave a pending durable response-loss retry: result=%+v err=%v", result, err)
	}
	fixture.runtime.casErr = nil
	assertFundingCarryRetryRejectsTamperedOutbox(t, fixture, request.OperationID, "unknown", "verify funding fee response-loss outbox", func() error {
		_, err := fixture.strategy.ApplyFundingRecoveryFeeAdjustment(fixture.ctx, request)
		return err
	})
}

func TestFundingCarryFeeCompletedOperationReplayRejectsUnhashedProjectionTamper(t *testing.T) {
	fill := validFundingRecoveryFill("171", "0.1", false)
	fixture := newFundingRecoveryFillFixture(t, []*exchange.OrderFill{fill}, true, false, true)
	fillPreview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil {
		t.Fatal("preview fee-fill seed fixture:", err)
	}
	fillRequest := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "fee-completed-fill-seed",
		ExpectedEpisodeRevision: fillPreview.EpisodeRevision, ExpectedChildRevision: fillPreview.ChildRevision,
		PreviewSourceRevision: fillPreview.Batch.SourceRevision, Actor: "operator", AuditRef: "incident-fee-completed-replay", Confirmed: true}
	if result, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, fillRequest); err != nil || !result.Projected {
		t.Fatalf("seed complete fill: result=%+v err=%v", result, err)
	}
	feePreview, err := fixture.strategy.PreviewFundingRecoveryFeeAdjustment(fixture.ctx, fixture.episode.ID, fixture.child.ID, fill.TradeID)
	if err != nil {
		t.Fatal("preview completed fee replay fixture:", err)
	}
	request := ApplyFundingRecoveryFeeAdjustmentRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, TradeID: fill.TradeID,
		OperationID: "fee-completed-hash-replay", AdjustmentID: "fee-completed-hash-row", AmountDeltaRaw: "0.0001", Asset: "BNB",
		EvidenceHash: strings.Repeat("a", 64), SourceRevision: feePreview.SourceRevision, ExpectedEpisodeRevision: feePreview.EpisodeRevision,
		ExpectedChildRevision: feePreview.ChildRevision, Actor: "operator", AuditRef: "incident-fee-completed-replay", Confirmed: true}
	result, err := fixture.strategy.ApplyFundingRecoveryFeeAdjustment(fixture.ctx, request)
	if err != nil || !result.Projected || result.Replayed || !result.HoldRetained {
		t.Fatalf("first fee request must complete and project successfully: result=%+v err=%v", result, err)
	}
	assertFundingCarryRetryRejectsTamperedOutbox(t, fixture, request.OperationID, "unknown", "verify funding fee response-loss outbox", func() error {
		_, err := fixture.strategy.ApplyFundingRecoveryFeeAdjustment(fixture.ctx, request)
		return err
	})
}

func TestFundingCarryAnomalyResponseLossRetryRejectsUnhashedProjectionTamper(t *testing.T) {
	fixture := newFundingRecoveryFillFixture(t, nil, true, true, true)
	if err := fixture.store.SetRecoverySourceWindowVerifier(fundingCarryHashTestSourceWindowVerifier{}); err != nil {
		t.Fatal("install isolated source-window verifier:", err)
	}
	store, generation, _, scopes, err := fixture.strategy.CurrentFundingCarryRecoveryBinding()
	if err != nil || len(scopes) != 1 {
		t.Fatalf("read exact recovery binding: scopes=%v err=%v", scopes, err)
	}
	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)
	identity := accounting.RecoverySourceIdentity{Source: accounting.RecoverySourceBinanceFuturesIncomeV1, Exchange: "binance", Market: "futures",
		AccountScope: scopes[0], Symbol: "BTCUSDT", Kind: accounting.RecoveryFundingEvent, Asset: "USDT"}
	window := accounting.RecoverySourceWindow{Source: identity.Source, Exchange: identity.Exchange, Market: identity.Market,
		AccountScope: identity.AccountScope, Symbol: identity.Symbol, Kind: identity.Kind, Asset: identity.Asset,
		From: from, Through: through, Rows: 1, TotalRows: 1, CompletionMarker: accounting.RecoverySourceWindowCompleteV1,
		CollectorEvidenceHash: strings.Repeat("c", 64), InitialFrom: from, HistoricalEvidenceHash: strings.Repeat("d", 64),
		EvidenceHash: strings.Repeat("0", 64)}
	original := accounting.RecoveryEvent{ID: "hash-anomaly-original", Source: identity.Source, Exchange: identity.Exchange, Market: identity.Market,
		AccountScope: identity.AccountScope, Symbol: identity.Symbol, Kind: identity.Kind, ExternalID: "9001", EpisodeID: fixture.episode.ID,
		Asset: identity.Asset, Amount: "-1.00", SourceRevision: "income-source-1", SourceHash: strings.Repeat("e", 64), ObservedAt: from.Add(time.Minute)}
	window.EvidenceHash, err = accounting.RecoverySourceWindowEvidenceHash(window, []accounting.RecoveryEvent{original})
	if err != nil {
		t.Fatal("hash exact anomaly source-window fixture:", err)
	}
	fixture.runtime.mu.Lock()
	runtimeVersion, runtimePayload := fixture.runtime.version, fixture.runtime.payload
	fixture.runtime.mu.Unlock()
	seed := accounting.RecoveryOperation{EpisodeID: fixture.episode.ID, OperationID: "hash-anomaly-cursor-seed", HashVersion: 1,
		ExpectedEpisodeRevision: fixture.episode.Revision, ExpectedRuntimeRevision: fundingCarryRuntimeRevision(runtimeVersion, runtimePayload),
		ExpectedChildRevisions: map[string]uint64{}, ExpectedEventRevisions: map[string]uint64{}, Events: []accounting.RecoveryEvent{original},
		SourceWindow: &window, Actor: "operator", AuditRef: "incident-anomaly-seed", EvidenceHash: strings.Repeat("f", 64), EvidenceReadAt: through}
	if _, replayed, err := store.ApplyRecoveryOperation(fixture.ctx, generation, seed); err != nil || replayed {
		t.Fatalf("seed exact durable funding cursor: replayed=%t err=%v", replayed, err)
	}
	binding, err := resolveFundingCarryRecoveryBindingFromStrategy(fixture.strategy)
	if err != nil {
		t.Fatal("resolve exact strategy recovery binding:", err)
	}
	if err := fixture.strategy.projectFundingRecoveryFillOperation(fixture.ctx, binding, fixture.episode.ID, seed.OperationID); err != nil {
		t.Fatalf("project seed cursor through FundingCarry production path: %v", err)
	}
	currentEpisode, found, err := store.GetRecoveryEpisode(fixture.ctx, fixture.episode.ID)
	if err != nil || !found {
		t.Fatalf("read episode before anomaly capture: found=%t err=%v", found, err)
	}
	observed := original
	observed.ID, observed.Amount, observed.SourceRevision, observed.SourceHash = "hash-anomaly-observed", "-1.25", "income-source-late", strings.Repeat("1", 64)
	observed.ObservedAt = from.Add(2 * time.Minute)
	delta, err := accounting.RecoveryFundingAmountDelta(&original, &observed)
	if err != nil {
		t.Fatal("calculate audited source anomaly delta:", err)
	}
	anomaly := accounting.RecoverySourceAnomaly{EpisodeID: currentEpisode.ID, RecoveryGeneration: currentEpisode.RecoveryGeneration,
		EpisodeRevision: currentEpisode.Revision, Identity: identity, CursorRevision: 1, InitialFrom: from, Through: through,
		ExpectedHistoryHash: window.HistoricalEvidenceHash, ObservedHistoryHash: strings.Repeat("2", 64),
		Facts: []accounting.RecoverySourceAnomalyFact{{Kind: accounting.RecoverySourceAnomalyChanged, ExternalID: original.ExternalID,
			OriginalEvent: &original, ObservedEvent: &observed, DeltaAmount: delta}},
		Revision: 1, State: accounting.RecoverySourceAnomalyOpen, DetectedActor: "auditor", DetectedAuditRef: "incident-anomaly-detected",
		DetectedAt: through.Add(time.Minute)}
	anomaly.ID, err = anomaly.Fingerprint()
	if err != nil {
		t.Fatal("fingerprint source anomaly:", err)
	}
	if _, replayed, err := store.RecordRecoverySourceAnomaly(fixture.ctx, generation, anomaly); err != nil || replayed {
		t.Fatalf("record real durable source anomaly: replayed=%t err=%v", replayed, err)
	}
	request := ApplyFundingRecoveryAnomalyAdjustmentRequest{EpisodeID: currentEpisode.ID, AnomalyID: anomaly.ID,
		OperationID: "hash-anomaly-adjustment-retry", ExpectedEpisodeRevision: currentEpisode.Revision, ExpectedAnomalyRevision: 1,
		EvidenceHash: strings.Repeat("3", 64), Actor: "operator", AuditRef: "incident-anomaly-adjustment", Confirmed: true}
	fixture.runtime.casErr = errors.New("inject response loss after durable anomaly receipt")
	if result, err := fixture.strategy.ApplyFundingRecoveryAnomalyAdjustment(fixture.ctx, request); err == nil || result.Projected || !result.HoldRetained {
		t.Fatalf("initial anomaly adjustment must leave a pending response-loss retry: result=%+v err=%v", result, err)
	}
	fixture.runtime.casErr = nil
	assertFundingCarryRetryRejectsTamperedOutbox(t, fixture, request.OperationID, "unknown", "verify funding anomaly response-loss outbox", func() error {
		_, err := fixture.strategy.ApplyFundingRecoveryAnomalyAdjustment(fixture.ctx, request)
		return err
	})
}

func TestFundingCarryAnomalyCompletedOperationReplayRejectsUnhashedProjectionTamper(t *testing.T) {
	fixture := newFundingRecoveryFillFixture(t, nil, true, true, true)
	if err := fixture.store.SetRecoverySourceWindowVerifier(fundingCarryHashTestSourceWindowVerifier{}); err != nil {
		t.Fatal("install isolated source-window verifier:", err)
	}
	store, generation, _, scopes, err := fixture.strategy.CurrentFundingCarryRecoveryBinding()
	if err != nil || len(scopes) != 1 {
		t.Fatalf("read exact recovery binding: scopes=%v err=%v", scopes, err)
	}
	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)
	identity := accounting.RecoverySourceIdentity{Source: accounting.RecoverySourceBinanceFuturesIncomeV1, Exchange: "binance", Market: "futures",
		AccountScope: scopes[0], Symbol: "BTCUSDT", Kind: accounting.RecoveryFundingEvent, Asset: "USDT"}
	window := accounting.RecoverySourceWindow{Source: identity.Source, Exchange: identity.Exchange, Market: identity.Market,
		AccountScope: identity.AccountScope, Symbol: identity.Symbol, Kind: identity.Kind, Asset: identity.Asset,
		From: from, Through: through, Rows: 1, TotalRows: 1, CompletionMarker: accounting.RecoverySourceWindowCompleteV1,
		CollectorEvidenceHash: strings.Repeat("c", 64), InitialFrom: from, HistoricalEvidenceHash: strings.Repeat("d", 64), EvidenceHash: strings.Repeat("0", 64)}
	original := accounting.RecoveryEvent{ID: "hash-anomaly-completed-original", Source: identity.Source, Exchange: identity.Exchange, Market: identity.Market,
		AccountScope: identity.AccountScope, Symbol: identity.Symbol, Kind: identity.Kind, ExternalID: "9002", EpisodeID: fixture.episode.ID,
		Asset: identity.Asset, Amount: "-1.00", SourceRevision: "income-source-1", SourceHash: strings.Repeat("e", 64), ObservedAt: from.Add(time.Minute)}
	window.EvidenceHash, err = accounting.RecoverySourceWindowEvidenceHash(window, []accounting.RecoveryEvent{original})
	if err != nil {
		t.Fatal("hash exact anomaly source-window fixture:", err)
	}
	fixture.runtime.mu.Lock()
	runtimeVersion, runtimePayload := fixture.runtime.version, fixture.runtime.payload
	fixture.runtime.mu.Unlock()
	seed := accounting.RecoveryOperation{EpisodeID: fixture.episode.ID, OperationID: "hash-anomaly-completed-cursor-seed", HashVersion: 1,
		ExpectedEpisodeRevision: fixture.episode.Revision, ExpectedRuntimeRevision: fundingCarryRuntimeRevision(runtimeVersion, runtimePayload),
		ExpectedChildRevisions: map[string]uint64{}, ExpectedEventRevisions: map[string]uint64{}, Events: []accounting.RecoveryEvent{original},
		SourceWindow: &window, Actor: "operator", AuditRef: "incident-anomaly-completed-seed", EvidenceHash: strings.Repeat("f", 64), EvidenceReadAt: through}
	if _, replayed, err := store.ApplyRecoveryOperation(fixture.ctx, generation, seed); err != nil || replayed {
		t.Fatalf("seed exact durable funding cursor: replayed=%t err=%v", replayed, err)
	}
	binding, err := resolveFundingCarryRecoveryBindingFromStrategy(fixture.strategy)
	if err != nil {
		t.Fatal("resolve exact strategy recovery binding:", err)
	}
	if err := fixture.strategy.projectFundingRecoveryFillOperation(fixture.ctx, binding, fixture.episode.ID, seed.OperationID); err != nil {
		t.Fatalf("project seed cursor through FundingCarry production path: %v", err)
	}
	currentEpisode, found, err := store.GetRecoveryEpisode(fixture.ctx, fixture.episode.ID)
	if err != nil || !found {
		t.Fatalf("read episode before anomaly capture: found=%t err=%v", found, err)
	}
	observed := original
	observed.ID, observed.Amount, observed.SourceRevision, observed.SourceHash = "hash-anomaly-completed-observed", "-1.25", "income-source-late", strings.Repeat("1", 64)
	observed.ObservedAt = from.Add(2 * time.Minute)
	delta, err := accounting.RecoveryFundingAmountDelta(&original, &observed)
	if err != nil {
		t.Fatal("calculate audited source anomaly delta:", err)
	}
	anomaly := accounting.RecoverySourceAnomaly{EpisodeID: currentEpisode.ID, RecoveryGeneration: currentEpisode.RecoveryGeneration,
		EpisodeRevision: currentEpisode.Revision, Identity: identity, CursorRevision: 1, InitialFrom: from, Through: through,
		ExpectedHistoryHash: window.HistoricalEvidenceHash, ObservedHistoryHash: strings.Repeat("2", 64),
		Facts: []accounting.RecoverySourceAnomalyFact{{Kind: accounting.RecoverySourceAnomalyChanged, ExternalID: original.ExternalID,
			OriginalEvent: &original, ObservedEvent: &observed, DeltaAmount: delta}},
		Revision: 1, State: accounting.RecoverySourceAnomalyOpen, DetectedActor: "auditor", DetectedAuditRef: "incident-anomaly-completed-detected",
		DetectedAt: through.Add(time.Minute)}
	anomaly.ID, err = anomaly.Fingerprint()
	if err != nil {
		t.Fatal("fingerprint source anomaly:", err)
	}
	if _, replayed, err := store.RecordRecoverySourceAnomaly(fixture.ctx, generation, anomaly); err != nil || replayed {
		t.Fatalf("record real durable source anomaly: replayed=%t err=%v", replayed, err)
	}
	request := ApplyFundingRecoveryAnomalyAdjustmentRequest{EpisodeID: currentEpisode.ID, AnomalyID: anomaly.ID,
		OperationID: "hash-anomaly-completed-adjustment", ExpectedEpisodeRevision: currentEpisode.Revision, ExpectedAnomalyRevision: 1,
		EvidenceHash: strings.Repeat("3", 64), Actor: "operator", AuditRef: "incident-anomaly-completed-adjustment", Confirmed: true}
	result, err := fixture.strategy.ApplyFundingRecoveryAnomalyAdjustment(fixture.ctx, request)
	if err != nil || !result.Projected || result.Replayed || !result.HoldRetained {
		t.Fatalf("first anomaly request must complete and project successfully: result=%+v err=%v", result, err)
	}
	assertFundingCarryRetryRejectsTamperedOutbox(t, fixture, request.OperationID, "unknown", "verify funding anomaly response-loss outbox", func() error {
		_, err := fixture.strategy.ApplyFundingRecoveryAnomalyAdjustment(fixture.ctx, request)
		return err
	})
}

func assertFundingCarryRetryRejectsTamperedOutbox(t *testing.T, fixture fundingRecoveryFillFixture, operationID, mutation, errorText string, retry func() error) {
	t.Helper()
	stateBinding, ok := fixture.strategy.runtimeStateStore.(*durableRecoveryRuntimeBindingFixture)
	if !ok {
		t.Fatalf("unexpected production runtime binding fixture type %T", fixture.strategy.runtimeStateStore)
	}
	provider, ok := stateBinding.provider.(*durableRecoveryProviderFixture)
	if !ok {
		t.Fatalf("unexpected production recovery provider fixture type %T", stateBinding.provider)
	}
	underlyingRepository := provider.repository
	assertTamperRejectedWithoutWrites := func(stage, tamper string) {
		t.Helper()
		provider.repository = &tamperedFundingCarryRecoveryOutboxReader{FundingRecoveryStore: underlyingRepository,
			episodeID: fixture.episode.ID, operationID: operationID, mutation: tamper}
		before := readFundingCarryRetrySnapshot(t, fixture, operationID)
		if before.hold.State != "held" {
			t.Fatalf("%s retry fixture did not retain its durable hold", stage)
		}
		fixture.runtime.mu.Lock()
		beforeRuntimeVersion, beforeRuntimePayload, beforeCASCalls := fixture.runtime.version, fixture.runtime.payload, fixture.runtime.compareCalls
		fixture.runtime.mu.Unlock()
		if err := retry(); err == nil || !strings.Contains(err.Error(), errorText) {
			t.Fatalf("%s response-loss retry did not reject tampered outbox before replay: %v", stage, err)
		}
		// The fake changes only the readback payload; the underlying SQL rows remain
		// intact so this asserts a failed retry causes no durable writes.
		fixture.runtime.mu.Lock()
		if fixture.runtime.version != beforeRuntimeVersion || fixture.runtime.payload != beforeRuntimePayload || fixture.runtime.compareCalls != beforeCASCalls {
			fixture.runtime.mu.Unlock()
			t.Fatalf("%s retry reached runtime projection before verifying the payload hash: version %d->%d CAS calls %d->%d",
				stage, beforeRuntimeVersion, fixture.runtime.version, beforeCASCalls, fixture.runtime.compareCalls)
		}
		fixture.runtime.mu.Unlock()
		after := readFundingCarryRetrySnapshot(t, fixture, operationID)
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("%s tamper rejection changed durable retry state: before=%+v after=%+v", stage, before, after)
		}
	}

	// Exercise both states through the same public retry entry point: first a
	// pending projection (the original mark-failure case), then a completed
	// projection whose subsequent same-operation request takes the replay branch.
	assertTamperRejectedWithoutWrites("pending", mutation)
	provider.repository = underlyingRepository
	if err := retry(); err != nil {
		t.Fatalf("complete pending projection through the same operation retry: %v", err)
	}
	outbox, found, err := fixture.store.GetRecoveryOutbox(fixture.ctx, fixture.episode.ID, operationID)
	if err != nil || !found || outbox.State != accounting.RecoveryProjected {
		t.Fatalf("same-operation retry did not reach already-projected state: outbox=%+v found=%t err=%v", outbox, found, err)
	}
	assertTamperRejectedWithoutWrites("already-projected", "source_cursor")
}

type fundingCarryRetrySnapshot struct {
	episode     accounting.RecoveryEpisode
	receipt     accounting.RecoveryReceipt
	outbox      accounting.RecoveryOutbox
	events      []accounting.RecoveryEvent
	cursor      accounting.RecoverySourceCursor
	cursorFound bool
	anomalies   []accounting.RecoverySourceAnomaly
	hold        accounting.RecoveryHold
}

func readFundingCarryRetrySnapshot(t *testing.T, fixture fundingRecoveryFillFixture, operationID string) fundingCarryRetrySnapshot {
	t.Helper()
	var snapshot fundingCarryRetrySnapshot
	var found bool
	var err error
	if snapshot.episode, found, err = fixture.store.GetRecoveryEpisode(fixture.ctx, fixture.episode.ID); err != nil || !found {
		t.Fatalf("read retry episode: found=%t err=%v", found, err)
	}
	if snapshot.receipt, found, err = fixture.store.GetRecoveryReceipt(fixture.ctx, fixture.episode.ID, operationID); err != nil || !found {
		t.Fatalf("read retry receipt: found=%t err=%v", found, err)
	}
	if snapshot.outbox, found, err = fixture.store.GetRecoveryOutbox(fixture.ctx, fixture.episode.ID, operationID); err != nil || !found {
		t.Fatalf("read retry outbox: found=%t err=%v", found, err)
	}
	if snapshot.events, err = fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID); err != nil {
		t.Fatal("read retry ledger:", err)
	}
	identity := accounting.RecoverySourceIdentity{Source: accounting.RecoverySourceBinanceFuturesIncomeV1, Exchange: "binance", Market: "futures",
		AccountScope: fixture.child.AccountScope, Symbol: fixture.child.Symbol, Kind: accounting.RecoveryFundingEvent, Asset: "USDT"}
	if snapshot.cursor, snapshot.cursorFound, err = fixture.store.GetRecoverySourceCursor(fixture.ctx, identity); err != nil {
		t.Fatal("read retry source cursor:", err)
	}
	if snapshot.anomalies, err = fixture.store.ListRecoverySourceAnomalies(fixture.ctx, fixture.episode.ID); err != nil {
		t.Fatal("read retry source anomalies:", err)
	}
	if snapshot.hold, found, err = fixture.store.GetRecoveryHold(fixture.ctx, fixture.episode.ID, fixture.episode.RecoveryGeneration); err != nil || !found {
		t.Fatalf("read retry hold: found=%t err=%v", found, err)
	}
	return snapshot
}

type fundingCarryHashTestSourceWindowVerifier struct{}

func (fundingCarryHashTestSourceWindowVerifier) VerifyRecoverySourceWindow(_ context.Context, window accounting.RecoverySourceWindow, events []accounting.RecoveryEvent) error {
	digest, err := accounting.RecoverySourceWindowEvidenceHash(window, events)
	if err != nil {
		return err
	}
	if digest != window.EvidenceHash {
		return errors.New("test source-window evidence hash mismatch")
	}
	return nil
}

type tamperedFundingCarryRecoveryOutboxReader struct {
	storage.FundingRecoveryStore
	episodeID, operationID string
	mutation               string
}

type tamperedFundingCarryRecoveryReceiptReader struct {
	storage.FundingRecoveryStore
	episodeID, operationID string
}

type legacyFundingCarryRecoveryOutboxReader struct {
	storage.FundingRecoveryStore
	episodeID, operationID string
}

func (s *legacyFundingCarryRecoveryOutboxReader) GetRecoveryOutbox(ctx context.Context, episodeID, operationID string) (accounting.RecoveryOutbox, bool, error) {
	outbox, found, err := s.FundingRecoveryStore.GetRecoveryOutbox(ctx, episodeID, operationID)
	if err != nil || !found || episodeID != s.episodeID || operationID != s.operationID {
		return outbox, found, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(outbox.ProjectionPayload, &fields); err != nil {
		return accounting.RecoveryOutbox{}, false, err
	}
	delete(fields, "expected_runtime_revision")
	outbox.ProjectionPayload, err = json.Marshal(fields)
	if err != nil {
		return accounting.RecoveryOutbox{}, false, err
	}
	digest := sha256.Sum256(outbox.ProjectionPayload)
	outbox.ProjectionHash = hex.EncodeToString(digest[:])
	outbox.AppliedHash = outbox.ProjectionHash
	return outbox, true, nil
}

func (s *legacyFundingCarryRecoveryOutboxReader) GetRecoveryReceipt(ctx context.Context, episodeID, operationID string) (accounting.RecoveryReceipt, bool, error) {
	receipt, found, err := s.FundingRecoveryStore.GetRecoveryReceipt(ctx, episodeID, operationID)
	if err != nil || !found || episodeID != s.episodeID || operationID != s.operationID {
		return receipt, found, err
	}
	outbox, outboxFound, outboxErr := s.GetRecoveryOutbox(ctx, episodeID, operationID)
	if outboxErr != nil || !outboxFound {
		return accounting.RecoveryReceipt{}, false, errors.Join(outboxErr, errors.New("legacy outbox not found"))
	}
	receipt.ProjectionHash = outbox.ProjectionHash
	return receipt, true, nil
}

func (s *tamperedFundingCarryRecoveryReceiptReader) GetRecoveryReceipt(ctx context.Context, episodeID, operationID string) (accounting.RecoveryReceipt, bool, error) {
	receipt, found, err := s.FundingRecoveryStore.GetRecoveryReceipt(ctx, episodeID, operationID)
	if err == nil && found && episodeID == s.episodeID && operationID == s.operationID {
		receipt.ProjectionHash = strings.Repeat("f", 64)
	}
	return receipt, found, err
}

func (s *tamperedFundingCarryRecoveryOutboxReader) GetRecoveryOutbox(ctx context.Context, episodeID, operationID string) (accounting.RecoveryOutbox, bool, error) {
	outbox, found, err := s.FundingRecoveryStore.GetRecoveryOutbox(ctx, episodeID, operationID)
	if err != nil || !found || episodeID != s.episodeID || operationID != s.operationID {
		return outbox, found, err
	}
	if s.mutation == "expected_runtime_revision" {
		outbox.ExpectedRuntimeRevision += ":tampered"
		return outbox, true, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(outbox.ProjectionPayload, &fields); err != nil {
		return accounting.RecoveryOutbox{}, false, err
	}
	switch s.mutation {
	case "unknown":
		fields["reviewer_unhashed_semantic"] = json.RawMessage(`{"tampered":true}`)
	case "source_cursor":
		fields["source_cursor"] = json.RawMessage(`{"evidence_hash":"tampered"}`)
	default:
		return accounting.RecoveryOutbox{}, false, fmt.Errorf("unsupported test tamper %q", s.mutation)
	}
	outbox.ProjectionPayload, err = json.Marshal(fields)
	return outbox, true, err
}

func (s *tamperedFundingCarryRecoveryOutboxReader) ListPendingRecoveryOutbox(ctx context.Context) ([]accounting.RecoveryOutbox, error) {
	outboxes, err := s.FundingRecoveryStore.ListPendingRecoveryOutbox(ctx)
	if err != nil {
		return nil, err
	}
	for index := range outboxes {
		if outboxes[index].EpisodeID == s.episodeID && outboxes[index].OperationID == s.operationID && s.mutation == "expected_runtime_revision" {
			outboxes[index].ExpectedRuntimeRevision += ":tampered"
		}
	}
	return outboxes, nil
}

type fundingCarryRecoveryHashIntegrationFixture struct {
	ctx          context.Context
	store        *storage.SQLStorage
	strategy     *FundingCarryStrategy
	runtime      *durableRecoveryRuntimeFixture
	runtimeStore *fundingCarryRecoveryHashRuntimeStore
	episode      accounting.RecoveryEpisode
	operationID  string
}

type fundingCarryRecoveryHashStore struct {
	storage.FundingRecoveryStore
	mu        sync.Mutex
	markCalls int
	failFirst bool
}

func (s *fundingCarryRecoveryHashStore) MarkRecoveryProjected(ctx context.Context, generation storage.FundingCarryRuntimeGeneration, outbox accounting.RecoveryOutbox) error {
	s.mu.Lock()
	s.markCalls++
	call := s.markCalls
	failFirst := s.failFirst && call == 1
	s.mu.Unlock()
	if failFirst {
		return errors.New("injected projection mark failure")
	}
	return s.FundingRecoveryStore.MarkRecoveryProjected(ctx, generation, outbox)
}

func (s *fundingCarryRecoveryHashStore) markCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.markCalls
}

type fundingCarryRecoveryHashRuntimeStore struct {
	*durableRecoveryRuntimeFixture
	provider *durableRecoveryProviderFixture
}

func (s *fundingCarryRecoveryHashRuntimeStore) FundingCarryRecoveryBinding() (storage.FundingRecoveryStore, storage.FundingCarryRuntimeGeneration, string, []string, error) {
	return s.provider.FundingCarryRecoveryBinding()
}

func (s *fundingCarryRecoveryHashRuntimeStore) markCalls() int {
	return s.provider.repository.(*fundingCarryRecoveryHashStore).markCallCount()
}

func newFundingCarryRecoveryHashIntegrationFixture(t *testing.T, legacyHash bool) fundingCarryRecoveryHashIntegrationFixture {
	t.Helper()
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "funding-carry-recovery-hash.db")
	store, err := storage.NewSQLStorage(databasePath)
	if err != nil {
		t.Fatal("open isolated recovery storage:", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated recovery storage: %v", err)
		}
	})
	if err := store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal("migrate isolated FundingCarry recovery storage:", err)
	}
	scope := fmt.Sprintf("%064x", 918)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal("claim FundingCarry runtime generation:", err)
	}
	botID := "bot-recovery-hash-integration"
	episode := accounting.RecoveryEpisode{
		ID: "episode-recovery-hash-integration", Bot: botID, Strategy: "funding_carry", AccountScopes: []string{scope},
		OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared,
	}
	if _, err := store.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal("create held FundingCarry recovery episode:", err)
	}
	baseState := fundingCarryRuntimeState{
		Strategy: "funding_carry", FuturesExchange: "binance", SpotExchange: "binance", Symbol: "BTCUSDT",
		OwnershipReady: true, RecoveryProjectionSchema: fundingCarryRecoveryProjectionSchemaVersion,
	}
	basePayload, err := json.Marshal(baseState)
	if err != nil {
		t.Fatal("encode base FundingCarry runtime snapshot:", err)
	}
	const runtimeVersion = fundingCarryRuntimeStateVersion
	runtime := &durableRecoveryRuntimeFixture{version: runtimeVersion, payload: string(basePayload), found: true}
	operationID := "operation-recovery-hash-integration"
	operation := accounting.RecoveryOperation{
		EpisodeID: episode.ID, OperationID: operationID, HashVersion: 1,
		ExpectedEpisodeRevision: episode.Revision,
		ExpectedRuntimeRevision: fundingCarryRuntimeRevision(runtimeVersion, string(basePayload)),
		ExpectedChildRevisions:  map[string]uint64{}, ExpectedEventRevisions: map[string]uint64{},
		Events: []accounting.RecoveryEvent{{
			ID: "event-recovery-hash-integration", Source: "venue", Exchange: "binance", Market: "spot", AccountScope: scope,
			Kind: accounting.RecoveryFeeEvent, ExternalID: "commission-recovery-hash-integration", EpisodeID: episode.ID,
			Asset: "USDT", Amount: "0.001", SourceRevision: "commission-row-1", SourceHash: strings.Repeat("a", 64),
			ObservedAt: time.Date(2026, 10, 13, 9, 30, 0, 0, time.UTC),
		}},
		Actor: "test-operator", AuditRef: "rc1187-recovery-hash-test", EvidenceHash: strings.Repeat("b", 64), EvidenceReadAt: time.Date(2026, 10, 13, 9, 31, 0, 0, time.UTC),
	}
	if _, replayed, err := store.ApplyRecoveryOperation(ctx, generation, operation); err != nil || replayed {
		t.Fatalf("create durable pending projection using production storage: replayed=%t err=%v", replayed, err)
	}
	outbox, found, err := store.GetRecoveryOutbox(ctx, episode.ID, operationID)
	if err != nil || !found || outbox.State != accounting.RecoveryProjectionPending {
		t.Fatalf("read production-created pending projection: outbox=%+v found=%t err=%v", outbox, found, err)
	}
	if legacyHash {
		legacyDigest := sha256.Sum256(outbox.ProjectionPayload)
		outbox.ProjectionHash = hex.EncodeToString(legacyDigest[:])
	} else if err := storage.VerifyFundingRecoveryProjectionHash(outbox.ProjectionPayload, outbox.ProjectionHash); err != nil {
		t.Fatalf("storage did not create a semantic projection hash: %v", err)
	}
	normalizedPayload := reorderFundingCarryProjectionJSON(t, outbox.ProjectionPayload)
	mutationDB, err := sql.Open("sqlite3", databasePath)
	if err != nil {
		t.Fatal("open test-only SQLite evidence mutation handle:", err)
	}
	defer func() {
		if err := mutationDB.Close(); err != nil {
			t.Errorf("close test-only SQLite evidence mutation handle: %v", err)
		}
	}()
	if err := replaceFundingCarryRecoveryOutboxPayload(ctx, mutationDB, outbox, normalizedPayload); err != nil {
		t.Fatalf("persist normalized projection fixture: %v", err)
	}
	if legacyHash {
		if err := replaceFundingCarryRecoveryReceiptHash(ctx, mutationDB, episode.ID, operationID, outbox.ProjectionHash); err != nil {
			t.Fatalf("persist historical raw-hash receipt: %v", err)
		}
	}

	repository := &fundingCarryRecoveryHashStore{FundingRecoveryStore: store, failFirst: true}
	provider := &durableRecoveryProviderFixture{repository: repository, generation: generation, bot: botID, scopes: []string{scope}}
	runtimeStore := &fundingCarryRecoveryHashRuntimeStore{durableRecoveryRuntimeFixture: runtime, provider: provider}
	futures := &mockFCExchange{name: "binance", marketType: "futures"}
	spot := &mockFCExchange{name: "binance", marketType: "spot"}
	carry := NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
	carry.SetRuntimeStateStore(runtimeStore)
	return fundingCarryRecoveryHashIntegrationFixture{
		ctx: ctx, store: store, strategy: carry, runtime: runtime, runtimeStore: runtimeStore, episode: episode, operationID: operationID,
	}
}

func reorderFundingCarryProjectionJSON(t *testing.T, payload []byte) []byte {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatalf("decode projection for serialization normalization: %v", err)
	}
	ordered, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("encode reordered projection: %v", err)
	}
	return append(append([]byte(" \n\t"), ordered...), []byte("\n ")...)
}

func replaceFundingCarryRecoveryOutboxPayload(ctx context.Context, db *sql.DB, outbox accounting.RecoveryOutbox, payload []byte) error {
	var envelope map[string]json.RawMessage
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT payload_json FROM funding_recovery_outbox WHERE episode_id=? AND operation_id=?`, outbox.EpisodeID, outbox.OperationID).Scan(&raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	envelope["projection_payload"] = append(json.RawMessage(nil), payload...)
	envelope["projection_hash"], _ = json.Marshal(outbox.ProjectionHash)
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET projection_hash=?,payload_json=? WHERE episode_id=? AND operation_id=?`, outbox.ProjectionHash, encoded, outbox.EpisodeID, outbox.OperationID)
	return err
}

func replaceFundingCarryRecoveryReceiptHash(ctx context.Context, db *sql.DB, episodeID, operationID, projectionHash string) error {
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT receipt_json FROM funding_recovery_operations WHERE episode_id=? AND operation_id=?`, episodeID, operationID).Scan(&raw); err != nil {
		return err
	}
	var receipt accounting.RecoveryReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		return err
	}
	receipt.ProjectionHash = projectionHash
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE funding_recovery_operations SET receipt_json=? WHERE episode_id=? AND operation_id=?`, encoded, episodeID, operationID)
	return err
}
