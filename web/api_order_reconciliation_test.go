package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/storage"
	"quantmesh/strategy"
)

func TestFundingIncomeReplayUsesItsOutboxCursorSnapshot(t *testing.T) {
	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	window := accounting.RecoverySourceWindow{Source: accounting.RecoverySourceBinanceFuturesIncomeV1, Exchange: "binance", Market: "futures",
		AccountScope: strings.Repeat("a", 64), Symbol: "BTCUSDT", Kind: accounting.RecoveryFundingEvent, Asset: "USDT",
		From: from, Through: from.Add(time.Minute), ExpectedRevision: 4, CompletionMarker: accounting.RecoverySourceWindowCompleteV1,
		EvidenceHash: strings.Repeat("b", 64), InitialFrom: from, HistoricalEvidenceHash: strings.Repeat("c", 64)}
	episode := accounting.RecoveryEpisode{ID: "episode-replay", Revision: 8}
	cursor := accounting.RecoverySourceCursor{RecoverySourceWindow: window, EpisodeID: episode.ID, Revision: 5}
	payload, err := json.Marshal(struct {
		Episode      accounting.RecoveryEpisode       `json:"episode"`
		Events       []accounting.RecoveryEvent       `json:"events"`
		SourceCursor *accounting.RecoverySourceCursor `json:"source_cursor"`
	}{episode, []accounting.RecoveryEvent{}, &cursor})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(payload)
	projectionHash := hex.EncodeToString(digest[:])
	request := strategy.ApplyFundingRecoveryIncomeRequest{EpisodeID: episode.ID, OperationID: "operation-old", ExpectedEpisodeRevision: 7}
	receipt := accounting.RecoveryReceipt{EpisodeID: episode.ID, OperationID: request.OperationID,
		EpisodeRevision: 9, State: accounting.RecoveryProjected, ProjectionHash: projectionHash}
	outbox := accounting.RecoveryOutbox{EpisodeID: episode.ID, OperationID: request.OperationID,
		TargetEpisodeRevision: 8, State: accounting.RecoveryProjected, ProjectionHash: projectionHash, ProjectionPayload: payload}
	result := strategy.ApplyFundingRecoveryIncomeResult{SourceWindow: window, Events: []accounting.RecoveryEvent{}, Replayed: true}
	currentEpisode := accounting.RecoveryEpisode{ID: episode.ID}

	replayed, err := fundingIncomeReplayCursorSnapshot(outbox, receipt, request, result, currentEpisode)
	if err != nil || replayed.Revision != 5 || replayed.EpisodeID != episode.ID {
		t.Fatalf("replay did not return its original durable cursor snapshot: cursor=%+v err=%v", replayed, err)
	}
	request.ExpectedEpisodeRevision++
	if _, err := fundingIncomeReplayCursorSnapshot(outbox, receipt, request, result, currentEpisode); err == nil {
		t.Fatal("replay accepted a request with a different expected episode revision")
	}
	request.ExpectedEpisodeRevision--
	outbox.ProjectionPayload = append(outbox.ProjectionPayload, ' ')
	if _, err := fundingIncomeReplayCursorSnapshot(outbox, receipt, request, result, currentEpisode); err == nil {
		t.Fatal("replay accepted a payload that no longer matches its durable hash")
	}

	tests := []struct {
		name   string
		mutate func(*accounting.RecoveryEpisode, *[]accounting.RecoveryEvent, *accounting.RecoverySourceCursor)
	}{
		{name: "episode revision", mutate: func(episode *accounting.RecoveryEpisode, _ *[]accounting.RecoveryEvent, _ *accounting.RecoverySourceCursor) {
			episode.Revision++
		}},
		{name: "cursor revision", mutate: func(_ *accounting.RecoveryEpisode, _ *[]accounting.RecoveryEvent, cursor *accounting.RecoverySourceCursor) {
			cursor.Revision++
		}},
		{name: "cursor episode", mutate: func(_ *accounting.RecoveryEpisode, _ *[]accounting.RecoveryEvent, cursor *accounting.RecoverySourceCursor) {
			cursor.EpisodeID = "other-episode"
		}},
		{name: "source window", mutate: func(_ *accounting.RecoveryEpisode, _ *[]accounting.RecoveryEvent, cursor *accounting.RecoverySourceCursor) {
			cursor.Through = cursor.Through.Add(time.Minute)
		}},
		{name: "events", mutate: func(_ *accounting.RecoveryEpisode, events *[]accounting.RecoveryEvent, _ *accounting.RecoverySourceCursor) {
			*events = append(*events, accounting.RecoveryEvent{ID: "unexpected"})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changedEpisode := episode
			changedEvents := []accounting.RecoveryEvent{}
			changedCursor := cursor
			test.mutate(&changedEpisode, &changedEvents, &changedCursor)
			changedPayload, err := json.Marshal(struct {
				Episode      accounting.RecoveryEpisode       `json:"episode"`
				Events       []accounting.RecoveryEvent       `json:"events"`
				SourceCursor *accounting.RecoverySourceCursor `json:"source_cursor"`
			}{changedEpisode, changedEvents, &changedCursor})
			if err != nil {
				t.Fatal(err)
			}
			changedDigest := sha256.Sum256(changedPayload)
			changedHash := hex.EncodeToString(changedDigest[:])
			changedOutbox := outbox
			changedOutbox.ProjectionPayload = changedPayload
			changedOutbox.ProjectionHash = changedHash
			changedReceipt := receipt
			changedReceipt.ProjectionHash = changedHash
			if _, err := fundingIncomeReplayCursorSnapshot(changedOutbox, changedReceipt, request, result, currentEpisode); err == nil {
				t.Fatalf("replay accepted a rehashed but inconsistent %s snapshot", test.name)
			}
		})
	}
}

func TestFundingIncomeReplayAllowsOnlyAuditedTakeoverRevisionAdvance(t *testing.T) {
	outbox := accounting.RecoveryOutbox{TargetEpisodeRevision: 8}
	projectedEpisode := accounting.RecoveryEpisode{ID: "episode", Revision: 8, OwnerGeneration: "old-owner"}
	receipt := accounting.RecoveryReceipt{State: accounting.RecoveryProjected, EpisodeRevision: 10, OwnerGeneration: "old-owner"}
	currentEpisode := accounting.RecoveryEpisode{ID: "episode", OwnerGeneration: "new-owner", OwnerTakeovers: []accounting.RecoveryOwnerTakeoverRecord{{
		OperationID: "takeover", Actor: "admin", AuditRef: "incident", EvidenceHash: strings.Repeat("a", 64),
		PreviousOwnerGeneration: "old-owner", OwnerGeneration: "new-owner", EpisodeRevision: 9,
	}}}
	if !fundingIncomeReplayReceiptRevisionMatches(receipt, outbox, projectedEpisode, currentEpisode) {
		t.Fatal("receipt advanced by the exact audited takeover was rejected")
	}
	for _, test := range []struct {
		name   string
		mutate func(*accounting.RecoveryReceipt, *accounting.RecoveryOutbox, *accounting.RecoveryEpisode)
	}{
		{name: "missing takeover", mutate: func(_ *accounting.RecoveryReceipt, _ *accounting.RecoveryOutbox, episode *accounting.RecoveryEpisode) {
			episode.OwnerTakeovers = nil
		}},
		{name: "wrong takeover revision", mutate: func(_ *accounting.RecoveryReceipt, _ *accounting.RecoveryOutbox, episode *accounting.RecoveryEpisode) {
			episode.OwnerTakeovers[0].EpisodeRevision++
		}},
		{name: "wrong previous owner", mutate: func(_ *accounting.RecoveryReceipt, _ *accounting.RecoveryOutbox, episode *accounting.RecoveryEpisode) {
			episode.OwnerTakeovers[0].PreviousOwnerGeneration = "other-owner"
		}},
		{name: "takeover owner is not the current episode owner", mutate: func(_ *accounting.RecoveryReceipt, _ *accounting.RecoveryOutbox, episode *accounting.RecoveryEpisode) {
			episode.OwnerTakeovers[0].OwnerGeneration = "other-owner"
		}},
		{name: "wrong receipt revision", mutate: func(receipt *accounting.RecoveryReceipt, _ *accounting.RecoveryOutbox, _ *accounting.RecoveryEpisode) {
			receipt.EpisodeRevision++
		}},
		{name: "overflow", mutate: func(_ *accounting.RecoveryReceipt, outbox *accounting.RecoveryOutbox, _ *accounting.RecoveryEpisode) {
			outbox.TargetEpisodeRevision = accounting.MaxDurableRecoveryRevision - 1
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changedReceipt, changedOutbox, changedEpisode := receipt, outbox, currentEpisode
			test.mutate(&changedReceipt, &changedOutbox, &changedEpisode)
			if fundingIncomeReplayReceiptRevisionMatches(changedReceipt, changedOutbox, projectedEpisode, changedEpisode) {
				t.Fatal("unproven receipt revision advance was accepted")
			}
		})
	}
}

type fakeOrderEvidence struct{ evidence VerifiedOrderEvidence }

func (f fakeOrderEvidence) ReadVerifiedOrderEvidence(context.Context, storage.OrderReconciliationCase) (VerifiedOrderEvidence, error) {
	return f.evidence, nil
}

type fakeOrderAccounting struct {
	snapshot          accounting.AccountingSnapshot
	receipt           *accounting.ReconciliationReceipt
	calls             int
	commitThenTimeout bool
	operationIDs      []string
}

func (f *fakeOrderAccounting) ReadOrderAccounting(_ context.Context, owner accounting.OrderOwner) (accounting.AccountingSnapshot, error) {
	return f.snapshot, nil
}
func (f *fakeOrderAccounting) ReconcileOrderAccounting(_ context.Context, r accounting.ReconcileRequest) (accounting.ReconciliationReceipt, error) {
	f.calls++
	f.operationIDs = append(f.operationIDs, r.OperationID)
	if err := r.Validate(); err != nil {
		return accounting.ReconciliationReceipt{}, err
	}
	f.snapshot = accounting.AccountingSnapshot{Owner: r.Owner, Revision: "strategy-rev-2", Cursor: r.Target, Result: accounting.EconomicResult{NetQuantity: "0.1"}, ReadAt: time.Now().UTC()}
	receipt := accounting.ReconciliationReceipt{OperationID: r.OperationID, Owner: r.Owner, ExpectedRevision: r.ExpectedRevision, From: r.From, Target: r.Target, Fills: r.Fills, Fees: r.Fees, Evidence: r.Evidence, Revision: "strategy-rev-2", Result: f.snapshot.Result, CompletedAt: time.Now().UTC()}
	f.receipt = &receipt
	if f.commitThenTimeout {
		f.commitThenTimeout = false
		return accounting.ReconciliationReceipt{}, errFakeTimeout
	}
	return receipt, nil
}
func (f *fakeOrderAccounting) ReadOrderAccountingReceipt(_ context.Context, id string) (accounting.ReconciliationReceipt, bool, error) {
	if f.receipt == nil || f.receipt.OperationID != id {
		return accounting.ReconciliationReceipt{}, false, nil
	}
	return *f.receipt, true, nil
}

type fakeOrderGate struct {
	calls   int
	effects map[string]bool
}

func (f *fakeOrderGate) ReleaseOrderQuarantine(_ context.Context, c storage.OrderReconciliationCase, _ string) error {
	if c.Owner.VenueOrderID == "" {
		return errFakeUnexpectedOwner
	}
	f.calls++
	if f.effects == nil {
		f.effects = make(map[string]bool)
	}
	f.effects[c.ID] = true
	return nil
}

type failReleaseFinalizeRepository struct {
	orderReconciliationRepository
	failOnce bool
}

type failAuditRepository struct{ orderReconciliationRepository }

func (r failAuditRepository) ListOrderReconciliationAudit(context.Context, string) ([]storage.OrderReconciliationAudit, error) {
	return nil, errFakeTimeout
}

func (r *failReleaseFinalizeRepository) TransitionOrderReconciliation(ctx context.Context, id string, rev int64, from, to storage.OrderReconciliationStatus, actor, action, outcome string, detail []byte, receipt *storage.OrderReconciliationReceipt, hash string) (storage.OrderReconciliationCase, error) {
	if r.failOnce && from == storage.OrderReconciliationReleasePending && to == storage.OrderReconciliationReleased {
		r.failOnce = false
		return storage.OrderReconciliationCase{}, errFakeTimeout
	}
	return r.orderReconciliationRepository.TransitionOrderReconciliation(ctx, id, rev, from, to, actor, action, outcome, detail, receipt, hash)
}

var errFakeUnexpectedOwner = &fakeError{"unexpected owner"}
var errFakeTimeout = &fakeError{"ambiguous strategy response"}

type fakeError struct{ s string }

func (e *fakeError) Error() string { return e.s }

func TestOrderReconciliationAPIUsesTrustedAdapterAndSeparateReleaseConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	repo, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.MigrateOrderReconciliations(ctx); err != nil {
		t.Fatal(err)
	}
	owner := accounting.OrderOwner{Exchange: "fake", Market: "spot", AccountScope: "account-1", Bot: "bot-1", Symbol: "BTCUSDT", StrategyName: "trend", StrategyType: "trend", ClientOrderID: "cid-1", VenueOrderID: "42"}
	verified := VerifiedOrderEvidence{Owner: owner, IntentRevision: "intent-rev-1", OwnerMatched: true, VenueTerminal: true, FillsComplete: true, FeesComplete: true, Target: accounting.Cursor{Sequence: 1, TradeID: "trade-1"}, Fills: []accounting.VerifiedFill{{TradeID: "trade-1", CursorSequence: 1, Side: "BUY", PositionSide: "LONG", OrderRole: "entry", Price: "10", Quantity: "0.1", CommissionAmount: "0", CommissionAsset: "USDT", TradeTime: time.Now().UTC()}}, ExpectedResult: accounting.EconomicResult{NetQuantity: "0.1"}, Summary: accounting.EvidenceSummary{Source: "fake-exchange-adapter", Summary: "complete fake evidence", VerifiedBy: "admin", VerifiedAt: time.Now().UTC()}}
	if err := (accounting.ReconcileRequest{OperationID: "case", Owner: owner, ExpectedRevision: "strategy-rev-1", From: accounting.Cursor{}, Target: verified.Target, Fills: verified.Fills, Evidence: verified.Summary}).Validate(); err != nil {
		t.Fatalf("test evidence invalid: %v", err)
	}
	accountant := &fakeOrderAccounting{snapshot: accounting.AccountingSnapshot{Owner: owner, Revision: "strategy-rev-1", Cursor: accounting.Cursor{}, Result: accounting.EconomicResult{NetQuantity: "0"}, ReadAt: time.Now().UTC()}, commitThenTimeout: true}
	gate := &fakeOrderGate{}
	SetOrderReconciliationService(&OrderReconciliationService{Repository: repo, Evidence: fakeOrderEvidence{verified}, Accounting: accountant, Gate: gate})
	defer SetOrderReconciliationService(nil)
	router := gin.New()
	router.Use(testReconciliationAuth())
	router.GET("/cases", listOrderReconciliationsHandler)
	router.GET("/cases/:case_id/audit", listOrderReconciliationAuditHandler)
	router.POST("/cases", createOrderReconciliationHandler)
	router.POST("/cases/:case_id/reconcile", reconcileOrderReconciliationHandler)
	router.POST("/cases/:case_id/release", releaseOrderReconciliationHandler)
	request := `{"owner":{"exchange":"fake","market":"spot","account_scope":"account-1","bot":"bot-1","symbol":"BTCUSDT","strategy_name":"trend","strategy_type":"trend","client_order_id":"cid-1","venue_order_id":"42"},"expected_intent_revision":"intent-rev-1","expected_strategy_revision":"strategy-rev-1","reason":"manual review","evidence_refs":["incident-1"],"idempotency_key":"request-1"}`
	created := performReconciliationRequest(router, http.MethodPost, "/cases", request, false)
	if created.Code != http.StatusForbidden {
		t.Fatalf("local-dev write should be forbidden: %d", created.Code)
	}
	created = performReconciliationRequest(router, http.MethodPost, "/cases", request, true)
	if created.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", created.Code, created.Body.String())
	}
	var createResult struct {
		Case storage.OrderReconciliationCase `json:"case"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createResult); err != nil {
		t.Fatal(err)
	}
	var createEnvelope map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &createEnvelope); err != nil {
		t.Fatal(err)
	}
	createdCase := createEnvelope["case"].(map[string]any)
	if _, ok := createdCase["strategy_cursor"]; !ok {
		t.Fatal("case DTO omitted strategy_cursor")
	}
	if _, ok := createdCase["validation"]; !ok {
		t.Fatal("case DTO omitted validation")
	}
	auditResponse := performReconciliationRequest(router, http.MethodGet, "/cases/"+createdCase["case_id"].(string)+"/audit", "", true)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("admin audit read failed: %d %s", auditResponse.Code, auditResponse.Body.String())
	}
	var auditEnvelope struct {
		CaseID string                             `json:"case_id"`
		Audit  []storage.OrderReconciliationAudit `json:"audit"`
	}
	if err := json.Unmarshal(auditResponse.Body.Bytes(), &auditEnvelope); err != nil {
		t.Fatal(err)
	}
	if auditEnvelope.CaseID != createdCase["case_id"].(string) || len(auditEnvelope.Audit) != 1 || auditEnvelope.Audit[0].Action != "case_created" {
		t.Fatalf("audit readback mismatch: %+v", auditEnvelope)
	}
	if forbiddenAudit := performReconciliationRequest(router, http.MethodGet, "/cases/"+auditEnvelope.CaseID+"/audit", "", false); forbiddenAudit.Code != http.StatusForbidden {
		t.Fatalf("non-admin audit read should be forbidden: %d", forbiddenAudit.Code)
	}
	if missingAudit := performReconciliationRequest(router, http.MethodGet, "/cases/missing/audit", "", true); missingAudit.Code != http.StatusNotFound {
		t.Fatalf("missing case audit should return 404: %d", missingAudit.Code)
	}
	SetOrderReconciliationService(&OrderReconciliationService{Repository: failAuditRepository{orderReconciliationRepository: repo}})
	if failedAudit := performReconciliationRequest(router, http.MethodGet, "/cases/"+auditEnvelope.CaseID+"/audit", "", true); failedAudit.Code != http.StatusServiceUnavailable {
		t.Fatalf("audit storage failure should return 503: %d", failedAudit.Code)
	}
	SetOrderReconciliationService(&OrderReconciliationService{Repository: repo, Evidence: fakeOrderEvidence{verified}, Accounting: accountant, Gate: gate})
	malicious := strings.TrimSuffix(request, "}") + `,"fills":[{"trade_id":"forged"}]}`
	if response := performReconciliationRequest(router, http.MethodPost, "/cases", malicious, true); response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("client fill input must be rejected: %d", response.Code)
	}
	reconcilePath := "/cases/" + createResult.Case.ID + "/reconcile"
	reconciled := performReconciliationRequest(router, http.MethodPost, reconcilePath, `{"case_revision":1,"confirm":true}`, true)
	if reconciled.Code != http.StatusServiceUnavailable {
		t.Fatalf("ambiguous strategy result must remain blocked: %d %s", reconciled.Code, reconciled.Body.String())
	}
	pending, found, err := repo.GetOrderReconciliation(ctx, createResult.Case.ID)
	if err != nil || !found || pending.Status != storage.OrderReconciliationRunning || pending.OperationRequest == nil {
		t.Fatalf("durable operation missing after response loss: %+v err=%v", pending, err)
	}
	reconciled = performReconciliationRequest(router, http.MethodPost, reconcilePath, `{"case_revision":2,"confirm":true}`, true)
	if reconciled.Code != http.StatusOK {
		t.Fatalf("same-operation recovery failed: %d %s", reconciled.Code, reconciled.Body.String())
	}
	if accountant.calls != 1 || len(accountant.operationIDs) != 1 || accountant.operationIDs[0] != createResult.Case.ID {
		t.Fatalf("retry created/replayed a different operation: calls=%d IDs=%v", accountant.calls, accountant.operationIDs)
	}
	var reconcileResult struct {
		Case storage.OrderReconciliationCase `json:"case"`
	}
	if err := json.Unmarshal(reconciled.Body.Bytes(), &reconcileResult); err != nil {
		t.Fatal(err)
	}
	if reconcileResult.Case.Status != storage.OrderReconciliationReady || reconcileResult.Case.Receipt == nil {
		t.Fatalf("durable reconcile readback missing: %+v", reconcileResult.Case)
	}
	badRelease := performReconciliationRequest(router, http.MethodPost, "/cases/"+createResult.Case.ID+"/release", `{"case_revision":4,"evidence_hash":"0000000000000000000000000000000000000000000000000000000000000000","confirm":true}`, true)
	if badRelease.Code != http.StatusConflict || gate.calls != 0 {
		t.Fatalf("mismatched evidence released quarantine: code=%d calls=%d", badRelease.Code, gate.calls)
	}
	goodBody, _ := json.Marshal(releaseOrderReconciliationRequest{CaseRevision: reconcileResult.Case.Revision, EvidenceHash: reconcileResult.Case.ReleaseEvidenceHash, Confirm: true})
	lateFeeEvidence := verified
	lateFeeEvidence.Fees = []accounting.VerifiedFee{{FeeID: "late-fee", TradeID: "trade-1", Amount: "0.001", Asset: "USDT", ChargedAt: time.Now().UTC()}}
	SetOrderReconciliationService(&OrderReconciliationService{Repository: repo, Evidence: fakeOrderEvidence{lateFeeEvidence}, Accounting: accountant, Gate: gate})
	changedEvidence := performReconciliationRequest(router, http.MethodPost, "/cases/"+createResult.Case.ID+"/release", string(goodBody), true)
	if changedEvidence.Code != http.StatusConflict || gate.calls != 0 {
		t.Fatalf("late fee evidence change was not fenced: code=%d calls=%d", changedEvidence.Code, gate.calls)
	}
	accountant.snapshot.Revision = "strategy-rev-late-change"
	SetOrderReconciliationService(&OrderReconciliationService{Repository: repo, Evidence: fakeOrderEvidence{verified}, Accounting: accountant, Gate: gate})
	changedCursor := performReconciliationRequest(router, http.MethodPost, "/cases/"+createResult.Case.ID+"/release", string(goodBody), true)
	if changedCursor.Code != http.StatusConflict || gate.calls != 0 {
		t.Fatalf("strategy revision TOCTOU was not fenced: code=%d calls=%d", changedCursor.Code, gate.calls)
	}
	accountant.snapshot.Revision = "strategy-rev-2"
	SetOrderReconciliationService(&OrderReconciliationService{Repository: &failReleaseFinalizeRepository{orderReconciliationRepository: repo, failOnce: true}, Evidence: fakeOrderEvidence{verified}, Accounting: accountant, Gate: gate})
	released := performReconciliationRequest(router, http.MethodPost, "/cases/"+createResult.Case.ID+"/release", string(goodBody), true)
	if released.Code != http.StatusServiceUnavailable {
		t.Fatalf("simulated final audit loss should remain pending: code=%d body=%s", released.Code, released.Body.String())
	}
	pendingRelease, found, err := repo.GetOrderReconciliation(ctx, createResult.Case.ID)
	if err != nil || !found || pendingRelease.Status != storage.OrderReconciliationReleasePending {
		t.Fatalf("release pending status not durable: %+v err=%v", pendingRelease, err)
	}
	retryReleaseBody, _ := json.Marshal(releaseOrderReconciliationRequest{CaseRevision: pendingRelease.Revision, EvidenceHash: pendingRelease.ReleaseEvidenceHash, Confirm: true})
	released = performReconciliationRequest(router, http.MethodPost, "/cases/"+createResult.Case.ID+"/release", string(retryReleaseBody), true)
	if released.Code != http.StatusOK || gate.calls != 2 || !gate.effects[createResult.Case.ID] {
		t.Fatalf("explicit release failed: code=%d calls=%d body=%s", released.Code, gate.calls, released.Body.String())
	}
	if strings.Contains(released.Body.String(), "commission_amount") || strings.Contains(released.Body.String(), "operation_request") {
		t.Fatal("API leaked durable operation fills/fees")
	}
}

func TestOrderReconciliationAPIUnavailableAdaptersRemainFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	SetOrderReconciliationService(&OrderReconciliationService{})
	defer SetOrderReconciliationService(nil)
	router := gin.New()
	router.Use(testReconciliationAuth())
	router.POST("/cases/:case_id/reconcile", reconcileOrderReconciliationHandler)
	router.POST("/cases/:case_id/release", releaseOrderReconciliationHandler)
	if got := performReconciliationRequest(router, http.MethodPost, "/cases/missing/reconcile", `{"case_revision":1,"confirm":true}`, true).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("missing adapters should return 503, got %d", got)
	}
	if got := performReconciliationRequest(router, http.MethodPost, "/cases/missing/release", `{"case_revision":1,"evidence_hash":"0123456789012345678901234567890123456789012345678901234567890123","confirm":true}`, true).Code; got != http.StatusServiceUnavailable {
		t.Fatalf("missing gate bridge should return 503, got %d", got)
	}
}

func TestOrderReconciliationAPIFailsClosedForPersistedCaseWithoutProductionAdapters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	repo, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "production-composition.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.MigrateOrderReconciliations(ctx); err != nil {
		t.Fatal(err)
	}
	owner := accounting.OrderOwner{Exchange: "fake", Market: "spot", AccountScope: "production-composition-test", Bot: "bot-1", Symbol: "BTCUSDT", StrategyName: "grid", StrategyType: "grid", ClientOrderID: "cid-1", VenueOrderID: "42"}
	created, replayed, err := repo.CreateOrderReconciliationCase(ctx, storage.NewOrderReconciliationCase{
		ID: "production-composition-case", IdempotencyKey: "production-composition-key", Owner: owner,
		ExpectedIntentRevision: "intent-r1", ExpectedStrategyRevision: "strategy-r1",
		Reason: "fail-closed production composition test", Actor: "admin", EvidenceRefs: []string{"test-evidence"},
	})
	if err != nil || replayed {
		t.Fatalf("create durable test case: replayed=%t err=%v", replayed, err)
	}
	SetOrderReconciliationService(&OrderReconciliationService{
		Repository: repo,
		Evidence:   fakeOrderEvidence{},
		// Production currently has no audited per-strategy accountant or gate.
		Accounting: nil,
		Gate:       nil,
	})
	defer SetOrderReconciliationService(nil)
	router := gin.New()
	router.Use(testReconciliationAuth())
	router.GET("/cases", listOrderReconciliationsHandler)
	router.GET("/cases/:case_id/audit", listOrderReconciliationAuditHandler)
	router.POST("/cases/:case_id/reconcile", reconcileOrderReconciliationHandler)
	router.POST("/cases/:case_id/release", releaseOrderReconciliationHandler)

	if response := performReconciliationRequest(router, http.MethodGet, "/cases", "", true); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), created.ID) {
		t.Fatalf("admin must read persisted cases: status=%d body=%s", response.Code, response.Body.String())
	}
	if response := performReconciliationRequest(router, http.MethodGet, "/cases/"+created.ID+"/audit", "", true); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "case_created") {
		t.Fatalf("admin must read persisted audit history: status=%d body=%s", response.Code, response.Body.String())
	}
	reconcile := performReconciliationRequest(router, http.MethodPost, "/cases/"+created.ID+"/reconcile", `{"case_revision":1,"confirm":true}`, true)
	if reconcile.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing strategy accounting must reject reconcile: status=%d body=%s", reconcile.Code, reconcile.Body.String())
	}
	release := performReconciliationRequest(router, http.MethodPost, "/cases/"+created.ID+"/release", `{"case_revision":1,"evidence_hash":"0123456789012345678901234567890123456789012345678901234567890123","confirm":true}`, true)
	if release.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing durable gate bridge must reject release: status=%d body=%s", release.Code, release.Body.String())
	}
	stored, found, err := repo.GetOrderReconciliation(ctx, created.ID)
	if err != nil || !found || stored.Status != storage.OrderReconciliationPrepared || stored.Revision != created.Revision {
		t.Fatalf("blocked operations must not alter the durable case: case=%+v found=%t err=%v", stored, found, err)
	}
	audit, err := repo.ListOrderReconciliationAudit(ctx, created.ID)
	if err != nil || len(audit) != 1 || audit[0].Action != "case_created" {
		t.Fatalf("blocked operations must not append reconcile/release audit: audit=%+v err=%v", audit, err)
	}
}

func TestVerifiedOrderEvidenceRequiresEveryIndependentProof(t *testing.T) {
	owner := accounting.OrderOwner{Exchange: "fake", Market: "spot", AccountScope: "acct", Bot: "bot", Symbol: "BTCUSDT", StrategyName: "trend", StrategyType: "trend", ClientOrderID: "cid", VenueOrderID: "1"}
	item := storage.OrderReconciliationCase{ID: "case", Owner: owner, ExpectedIntentRevision: "intent-r1", ExpectedStrategyRevision: "strategy-r1"}
	base := VerifiedOrderEvidence{Owner: owner, IntentRevision: "intent-r1", OwnerMatched: true, VenueTerminal: true, FillsComplete: true, FeesComplete: true, Target: accounting.Cursor{Sequence: 1, TradeID: "trade-1"}, Fills: []accounting.VerifiedFill{{TradeID: "trade-1", CursorSequence: 1, Side: "BUY", PositionSide: "LONG", OrderRole: "entry", Price: "1", Quantity: "1", CommissionAmount: "0", CommissionAsset: "USDT", TradeTime: time.Now().UTC()}}, ExpectedResult: accounting.EconomicResult{NetQuantity: "1"}, Summary: accounting.EvidenceSummary{Source: "fake", Summary: "verified", VerifiedBy: "admin", VerifiedAt: time.Now().UTC()}}
	cases := []struct {
		name   string
		mutate func(*VerifiedOrderEvidence)
	}{
		{"owner mismatch", func(e *VerifiedOrderEvidence) { e.Owner.Bot = "other" }},
		{"intent mismatch", func(e *VerifiedOrderEvidence) { e.IntentRevision = "intent-r2" }},
		{"owner unresolved", func(e *VerifiedOrderEvidence) { e.OwnerMatched = false }},
		{"venue nonterminal", func(e *VerifiedOrderEvidence) { e.VenueTerminal = false }},
		{"fills incomplete", func(e *VerifiedOrderEvidence) { e.FillsComplete = false }},
		{"fees incomplete", func(e *VerifiedOrderEvidence) { e.FeesComplete = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := base
			tc.mutate(&e)
			if verifiedOrderEvidenceComplete(e, item) {
				t.Fatal("incomplete evidence accepted")
			}
		})
	}
}

func TestVerifiedFullHistoryCursorPrefixSelectsOnlyMissingSuffix(t *testing.T) {
	owner := accounting.OrderOwner{Exchange: "fake", Market: "spot", AccountScope: "acct", Bot: "bot", Symbol: "BTCUSDT", StrategyName: "trend", StrategyType: "trend", ClientOrderID: "cid", VenueOrderID: "1"}
	verifiedAt := time.Now().UTC()
	fills := []accounting.VerifiedFill{
		{TradeID: "trade-1", CursorSequence: 1, Side: "BUY", PositionSide: "LONG", OrderRole: "entry", Price: "10", Quantity: "0.1", CommissionAmount: "0", CommissionAsset: "USDT", TradeTime: verifiedAt},
		{TradeID: "trade-2", CursorSequence: 2, Side: "BUY", PositionSide: "LONG", OrderRole: "entry", Price: "11", Quantity: "0.1", CommissionAmount: "0", CommissionAsset: "USDT", TradeTime: verifiedAt},
		{TradeID: "trade-3", CursorSequence: 3, Side: "SELL", PositionSide: "LONG", OrderRole: "exit", Price: "12", Quantity: "0.1", CommissionAmount: "0", CommissionAsset: "USDT", TradeTime: verifiedAt},
	}
	item := storage.OrderReconciliationCase{ID: "case", Owner: owner, ExpectedIntentRevision: "intent-r1", ExpectedStrategyRevision: "strategy-r7"}
	evidence := VerifiedOrderEvidence{Owner: owner, IntentRevision: item.ExpectedIntentRevision, OwnerMatched: true, VenueTerminal: true, FillsComplete: true, FeesComplete: true, Target: accounting.Cursor{Sequence: 3, TradeID: "trade-3"}, Fills: fills, Fees: []accounting.VerifiedFee{{FeeID: "fee-prefix", TradeID: "trade-1", Amount: "0.01", Asset: "USDT", ChargedAt: verifiedAt}, {FeeID: "fee-suffix", TradeID: "trade-3", Amount: "0.02", Asset: "USDT", ChargedAt: verifiedAt}}, ExpectedResult: accounting.EconomicResult{NetQuantity: "0.1"}, Summary: accounting.EvidenceSummary{Source: "fake", Summary: "complete", VerifiedBy: "admin", VerifiedAt: verifiedAt}}
	suffix, fees, ok := verifiedHistorySuffix(evidence, item, accounting.Cursor{Sequence: 2, TradeID: "trade-2"})
	if !ok || len(suffix) != 1 || suffix[0].TradeID != "trade-3" || len(fees) != 1 || fees[0].FeeID != "fee-suffix" {
		t.Fatalf("suffix selection mismatch: fills=%+v fees=%+v ok=%t", suffix, fees, ok)
	}
	if _, _, ok := verifiedHistorySuffix(evidence, item, accounting.Cursor{Sequence: 2, TradeID: "wrong-trade"}); ok {
		t.Fatal("cursor with a different prefix trade ID was accepted")
	}
	missing := evidence
	missing.Fills = append([]accounting.VerifiedFill(nil), fills[1:]...)
	if _, _, ok := verifiedHistorySuffix(missing, item, accounting.Cursor{Sequence: 2, TradeID: "trade-2"}); ok {
		t.Fatal("suffix-only provider evidence was accepted as full history")
	}
}

func TestAlreadyAccountedReadbackOnlyRequiresExactEconomicResult(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		name := "matching_result"
		if mismatch {
			name = "mismatching_result"
		}
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			ctx := context.Background()
			repo, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "readback.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			if err := repo.MigrateOrderReconciliations(ctx); err != nil {
				t.Fatal(err)
			}
			owner := accounting.OrderOwner{Exchange: "fake", Market: "spot", AccountScope: "acct", Bot: "bot", Symbol: "BTCUSDT", StrategyName: "trend", StrategyType: "trend", ClientOrderID: "cid", VenueOrderID: "1"}
			now := time.Now().UTC()
			evidence := VerifiedOrderEvidence{Owner: owner, IntentRevision: "intent-r1", OwnerMatched: true, VenueTerminal: true, FillsComplete: true, FeesComplete: true, Target: accounting.Cursor{Sequence: 1, TradeID: "trade-1"}, Fills: []accounting.VerifiedFill{{TradeID: "trade-1", CursorSequence: 1, Side: "BUY", PositionSide: "LONG", OrderRole: "entry", Price: "10", Quantity: "0.1", CommissionAmount: "0", CommissionAsset: "USDT", TradeTime: now}}, ExpectedResult: accounting.EconomicResult{NetQuantity: "0.1"}, Summary: accounting.EvidenceSummary{Source: "fake", Summary: "complete", VerifiedBy: "admin", VerifiedAt: now}}
			created, _, err := repo.CreateOrderReconciliationCase(ctx, storage.NewOrderReconciliationCase{ID: "case-readback", IdempotencyKey: "key-readback", Owner: owner, ExpectedIntentRevision: "intent-r1", ExpectedStrategyRevision: "strategy-r1", Reason: "uncertain prior write", Actor: "admin", EvidenceRefs: []string{"incident"}})
			if err != nil {
				t.Fatal(err)
			}
			result := evidence.ExpectedResult
			if mismatch {
				result = accounting.EconomicResult{NetQuantity: "0.2"}
			}
			accountant := &fakeOrderAccounting{snapshot: accounting.AccountingSnapshot{Owner: owner, Revision: "strategy-r1", Cursor: evidence.Target, Result: result, ReadAt: now}}
			SetOrderReconciliationService(&OrderReconciliationService{Repository: repo, Evidence: fakeOrderEvidence{evidence}, Accounting: accountant})
			defer SetOrderReconciliationService(nil)
			router := gin.New()
			router.Use(testReconciliationAuth())
			router.POST("/cases/:case_id/reconcile", reconcileOrderReconciliationHandler)
			response := performReconciliationRequest(router, http.MethodPost, "/cases/"+created.ID+"/reconcile", `{"case_revision":1,"confirm":true}`, true)
			stored, found, err := repo.GetOrderReconciliation(ctx, created.ID)
			if err != nil || !found {
				t.Fatalf("case readback failed: found=%t err=%v", found, err)
			}
			if mismatch {
				if response.Code != http.StatusConflict || stored.Status != storage.OrderReconciliationPrepared || accountant.calls != 0 {
					t.Fatalf("mismatched readback should stay blocked: status=%d case=%s calls=%d", response.Code, stored.Status, accountant.calls)
				}
				return
			}
			if response.Code != http.StatusOK || stored.Status != storage.OrderReconciliationReady || stored.Receipt == nil || stored.Receipt.Outcome != "already_accounted" || accountant.calls != 0 {
				t.Fatalf("matching durable result should be readback-only: status=%d case=%+v calls=%d body=%s", response.Code, stored, accountant.calls, response.Body.String())
			}
		})
	}
}

func performReconciliationRequest(router http.Handler, method, path, body string, admin bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if !admin {
		req.Header.Set("X-Test-LocalDev", "true")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func testReconciliationAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetHeader("X-Test-LocalDev") == "true" {
			c.Set("local_dev_mode", true)
		}
		c.Set("session", &Session{Username: "admin", Role: "admin"})
		c.Next()
	}
}

func TestFundingRecoveryAdjudicationRequiresAdminAndRejectsClientVenueFacts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer gin.SetMode(gin.DebugMode)
	calls := 0
	var received accounting.RecoverySubmissionAdjudication
	SetOrderReconciliationService(&OrderReconciliationService{
		Recovery: &storage.SQLStorage{},
		RecoverySubmissionAdjudicator: func(_ context.Context, episodeID string, adjudication accounting.RecoverySubmissionAdjudication) (accounting.RecoverySubmissionAttempt, bool, error) {
			calls++
			received = adjudication
			if episodeID != "episode-1" {
				t.Fatalf("episode path = %q", episodeID)
			}
			return accounting.RecoverySubmissionAttempt{State: accounting.RecoverySubmissionFound, VenueOrderID: "venue-verified"}, false, nil
		},
	})
	defer SetOrderReconciliationService(nil)
	router := gin.New()
	router.Use(testReconciliationAuth())
	router.POST("/funding-recovery/:episode_id/submission-attempts/adjudicate-found", adjudicateFundingRecoverySubmissionHandler)

	bad := performReconciliationRequest(router, http.MethodPost, "/funding-recovery/episode-1/submission-attempts/adjudicate-found", `{"operation_id":"op-1","permit_id":"permit-1","expected_attempt_revision":1,"expected_episode_revision":2,"audit_ref":"incident-1","confirmed":true,"venue_order_id":"attacker-chosen"}`, true)
	if bad.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("client-supplied venue fact response=%d calls=%d body=%s", bad.Code, calls, bad.Body.String())
	}
	denied := performReconciliationRequest(router, http.MethodPost, "/funding-recovery/episode-1/submission-attempts/adjudicate-found", `{"operation_id":"op-1","permit_id":"permit-1","expected_attempt_revision":1,"expected_episode_revision":2,"audit_ref":"incident-1","confirmed":true}`, false)
	if denied.Code != http.StatusForbidden || calls != 0 {
		t.Fatalf("non-admin response=%d calls=%d body=%s", denied.Code, calls, denied.Body.String())
	}
	good := performReconciliationRequest(router, http.MethodPost, "/funding-recovery/episode-1/submission-attempts/adjudicate-found", `{"operation_id":"op-1","permit_id":"permit-1","expected_attempt_revision":1,"expected_episode_revision":2,"audit_ref":"incident-1","confirmed":true}`, true)
	if good.Code != http.StatusOK || calls != 1 || received.Actor != "admin" || received.VenueOrderID != "" || received.EvidenceHash != "" || received.Decision != accounting.RecoverySubmissionFound {
		t.Fatalf("admin adjudication response=%d calls=%d request=%+v body=%s", good.Code, calls, received, good.Body.String())
	}
}

func TestFundingRecoveryTakeoverUsesSessionActorAndServerOwnedGeneration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer gin.SetMode(gin.DebugMode)
	calls := 0
	var actor, auditRef, operationID string
	var expectedRevision uint64
	SetOrderReconciliationService(&OrderReconciliationService{
		Recovery: &storage.SQLStorage{},
		RecoveryOwnerTakeover: func(_ context.Context, episodeID, opID, who, ref string, revision uint64, confirmed bool) (accounting.RecoveryEpisode, bool, error) {
			calls++
			if episodeID != "episode-takeover" || !confirmed {
				t.Fatalf("takeover input: episode=%q confirmed=%t", episodeID, confirmed)
			}
			actor, auditRef, operationID, expectedRevision = who, ref, opID, revision
			return accounting.RecoveryEpisode{ID: episodeID, State: accounting.RecoveryBlocked, OwnerGeneration: "server-current-generation"}, false, nil
		},
	})
	defer SetOrderReconciliationService(nil)
	router := gin.New()
	router.Use(testReconciliationAuth())
	router.POST("/funding-recovery/:episode_id/takeover", takeOverFundingRecoveryEpisodeHandler)

	forged := performReconciliationRequest(router, http.MethodPost, "/funding-recovery/episode-takeover/takeover", `{"operation_id":"take-1","expected_episode_revision":1,"audit_ref":"incident","confirmed":true,"expected_owner_generation":"attacker"}`, true)
	if forged.Code != http.StatusBadRequest || calls != 0 {
		t.Fatalf("client generation was not rejected: status=%d calls=%d body=%s", forged.Code, calls, forged.Body.String())
	}
	denied := performReconciliationRequest(router, http.MethodPost, "/funding-recovery/episode-takeover/takeover", `{"operation_id":"take-1","expected_episode_revision":1,"audit_ref":"incident","confirmed":true}`, false)
	if denied.Code != http.StatusForbidden || calls != 0 {
		t.Fatalf("non-admin takeover was not rejected: status=%d calls=%d", denied.Code, calls)
	}
	response := performReconciliationRequest(router, http.MethodPost, "/funding-recovery/episode-takeover/takeover", `{"operation_id":"take-1","expected_episode_revision":1,"audit_ref":"incident","confirmed":true}`, true)
	if response.Code != http.StatusOK || calls != 1 || actor != "admin" || auditRef != "incident" || operationID != "take-1" || expectedRevision != 1 {
		t.Fatalf("admin takeover input: status=%d calls=%d actor=%q audit=%q operation=%q revision=%d body=%s", response.Code, calls, actor, auditRef, operationID, expectedRevision, response.Body.String())
	}
}

type installedRecoveryAuthSettingsProvider struct{ localDevSettingsProvider }

func (installedRecoveryAuthSettingsProvider) GetSystemSettingBool(ctx context.Context, key string, defaultValue bool) (bool, error) {
	if key == "local_dev_mode" {
		return false, nil
	}
	return defaultValue, nil
}

func TestProductionFundingRecoveryRouteUsesSessionAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	defer gin.SetMode(gin.DebugMode)
	originalStorage := storageProvider
	originalService := currentOrderReconciliationService()
	originalSessionManager := SwapSessionManager(NewEphemeralSessionManager())
	t.Cleanup(func() {
		SetStorageProvider(originalStorage)
		SetOrderReconciliationService(originalService)
		if originalSessionManager == nil {
			originalSessionManager = NewEphemeralSessionManager()
		}
		SetSessionManager(originalSessionManager)
	})
	SetStorageProvider(installedRecoveryAuthSettingsProvider{})

	sessionManager := GetSessionManager()
	adminSession, err := sessionManager.CreateSession("auditor", "admin", "192.0.2.10", "recovery-test")
	if err != nil {
		t.Fatal(err)
	}
	nonAdminSession, err := sessionManager.CreateSession("operator", "user", "192.0.2.10", "recovery-test")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	takeoverCalls := 0
	var takeoverActor string
	SetOrderReconciliationService(&OrderReconciliationService{
		Recovery: &storage.SQLStorage{},
		RecoverySubmissionAdjudicator: func(_ context.Context, episodeID string, adjudication accounting.RecoverySubmissionAdjudication) (accounting.RecoverySubmissionAttempt, bool, error) {
			calls++
			if episodeID != "episode-production-route" || adjudication.Actor != "auditor" {
				t.Fatalf("production route callback got episode=%q actor=%q", episodeID, adjudication.Actor)
			}
			return accounting.RecoverySubmissionAttempt{State: accounting.RecoverySubmissionFound, VenueOrderID: "venue-verified"}, false, nil
		},
		RecoveryOwnerTakeover: func(_ context.Context, episodeID, operationID, actor, auditRef string, revision uint64, confirmed bool) (accounting.RecoveryEpisode, bool, error) {
			takeoverCalls++
			takeoverActor = actor
			if episodeID != "episode-production-route" || operationID != "op-takeover" || auditRef != "incident-takeover" || revision != 3 || !confirmed {
				t.Fatalf("production takeover route callback input: episode=%q operation=%q audit=%q revision=%d confirmed=%t", episodeID, operationID, auditRef, revision, confirmed)
			}
			return accounting.RecoveryEpisode{ID: episodeID, State: accounting.RecoveryBlocked, OwnerGeneration: "server-owned"}, false, nil
		},
	})

	router := gin.New()
	router.Use(gin.Recovery())
	cfg := &config.Config{}
	cfg.Web.SharedDir = t.TempDir()
	SetupRoutesWithConfig(router, cfg)
	path := "/api/capital/funding-recovery/episode-production-route/submission-attempts/adjudicate-found"
	body := `{"operation_id":"op-production","permit_id":"permit-1","expected_attempt_revision":1,"expected_episode_revision":2,"audit_ref":"incident-production","confirmed":true}`

	for _, tc := range []struct {
		name        string
		session     *Session
		wantStatus  int
		wantCallCnt int
	}{
		{name: "unauthenticated", wantStatus: http.StatusUnauthorized},
		{name: "non-admin session", session: nonAdminSession, wantStatus: http.StatusForbidden},
		{name: "admin session", session: adminSession, wantStatus: http.StatusOK, wantCallCnt: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.RemoteAddr = "192.0.2.10:1234" // never take the local-dev bypass branch.
			if tc.session != nil {
				req.AddCookie(&http.Cookie{Name: "session_id", Value: tc.session.SessionID})
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.wantStatus || calls != tc.wantCallCnt {
				t.Fatalf("status=%d calls=%d want status=%d calls=%d body=%s", response.Code, calls, tc.wantStatus, tc.wantCallCnt, response.Body.String())
			}
		})
	}

	takeoverPath := "/api/capital/funding-recovery/episode-production-route/takeover"
	takeoverBody := `{"operation_id":"op-takeover","expected_episode_revision":3,"audit_ref":"incident-takeover","confirmed":true}`
	for _, tc := range []struct {
		name       string
		session    *Session
		wantStatus int
		wantCalls  int
	}{
		{name: "unauthenticated takeover", wantStatus: http.StatusUnauthorized},
		{name: "non-admin takeover session", session: nonAdminSession, wantStatus: http.StatusForbidden},
		{name: "admin takeover session", session: adminSession, wantStatus: http.StatusOK, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, takeoverPath, strings.NewReader(takeoverBody))
			req.RemoteAddr = "192.0.2.10:1234"
			if tc.session != nil {
				req.AddCookie(&http.Cookie{Name: "session_id", Value: tc.session.SessionID})
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.wantStatus || takeoverCalls != tc.wantCalls || (tc.wantCalls == 1 && takeoverActor != "auditor") {
				t.Fatalf("takeover status=%d calls=%d actor=%q want status=%d calls=%d body=%s", response.Code, takeoverCalls, takeoverActor, tc.wantStatus, tc.wantCalls, response.Body.String())
			}
		})
	}
}
