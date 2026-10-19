package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/exchange/income"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/storage"
	"quantmesh/strategy"
	"quantmesh/web"

	"github.com/gin-gonic/gin"
)

const (
	fundingIncomeRecoveryProcessDBEnv     = "QUANTMESH_FUNDING_INCOME_RECOVERY_PROCESS_DB"
	fundingIncomeRecoveryProcessResultEnv = "QUANTMESH_FUNDING_INCOME_RECOVERY_PROCESS_RESULT"
)

func TestProductionOrderReconciliationCompositionFailsClosedAcrossRestart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// The production auth middleware persists sessions relative to the current
	// directory. Isolate that database as well as the trading storage database.
	originalWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	temporaryWorkingDir := t.TempDir()
	if err := os.Chdir(temporaryWorkingDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWorkingDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	storagePath := filepath.Join(temporaryWorkingDir, "runtime", "quantmesh.db")
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = storagePath
	cfg.Storage.BufferSize = 16
	cfg.Storage.BatchSize = 4

	var storageService *storage.StorageService
	openStorageService := func() *storage.StorageService {
		t.Helper()
		service, err := storage.NewStorageService(cfg, context.Background())
		if err != nil {
			t.Fatalf("start StorageService and migrate SQLite schema: %v", err)
		}
		return service
	}
	storageService = openStorageService()
	t.Cleanup(func() {
		if storageService != nil {
			storageService.Stop()
		}
	})

	// An empty runtime registry is sufficient to exercise the production
	// composition while ensuring no exchange, account, or order is reachable.
	compose := func(service *storage.StorageService) *gin.Engine {
		t.Helper()
		productionService, err := buildProductionOrderReconciliationService(service.GetStorage(), &BotManager{})
		if err != nil {
			t.Fatalf("build production reconciliation service: %v", err)
		}
		if productionService.Accounting != nil || productionService.Gate != nil {
			t.Fatal("production composition unexpectedly enabled accounting or gate release")
		}
		web.SetOrderReconciliationService(productionService)

		router := gin.New()
		web.SetupRoutesWithConfig(router, cfg)
		return router
	}
	t.Cleanup(func() { web.SetOrderReconciliationService(nil) })
	router := compose(storageService)

	unauthenticated := httptest.NewRecorder()
	router.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/capital/order-reconciliations", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("production route must require authentication: status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}

	adminSession, err := web.GetSessionManager().CreateSession("composition-test-admin", "admin", "127.0.0.1", "composition-test")
	if err != nil {
		t.Fatalf("create isolated admin session: %v", err)
	}
	adminRequest := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.AddCookie(&http.Cookie{Name: "session_id", Value: adminSession.SessionID})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
	recoveryStore, ok := storageService.GetStorage().(storage.FundingRecoveryStore)
	if !ok {
		t.Fatal("production storage does not expose the durable recovery repository")
	}
	recoveryEpisode := accounting.RecoveryEpisode{
		ID: "composition-recovery-episode", Bot: "bot-test", Strategy: "FundingCarry",
		AccountScopes: []string{fmt.Sprintf("%064x", 2)},
		Revision:      1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared,
	}
	generationStore, ok := storageService.GetStorage().(storage.FundingCarryRuntimeGenerationStore)
	if !ok {
		t.Fatal("production storage does not expose owner-fenced recovery generations")
	}
	generation, err := generationStore.ClaimFundingCarryRuntimeGeneration(t.Context(), recoveryEpisode.AccountScopes)
	if err != nil {
		t.Fatalf("claim isolated recovery runtime generation: %v", err)
	}
	recoveryEpisode.OwnerGeneration = generation.ID()
	if _, err := recoveryStore.CreateRecoveryEpisode(t.Context(), generation, recoveryEpisode); err != nil {
		t.Fatalf("persist recovery episode through production storage: %v", err)
	}
	recoveryStatus := adminRequest(http.MethodGet, "/api/capital/funding-recovery/"+recoveryEpisode.ID, "")
	if recoveryStatus.Code != http.StatusOK || !strings.Contains(recoveryStatus.Body.String(), recoveryEpisode.ID) {
		t.Fatalf("read recovery status through production API: status=%d body=%s", recoveryStatus.Code, recoveryStatus.Body.String())
	}

	testOwner := accounting.OrderOwner{Exchange: "fake", Market: "spot", AccountScope: "isolated-test-account", Bot: "bot-test", Symbol: "BTCUSDT", StrategyName: "trend", StrategyType: "trend", ClientOrderID: "client-test-1", VenueOrderID: "42"}
	createRequest := `{"owner":{"exchange":"fake","market":"spot","account_scope":"isolated-test-account","bot":"bot-test","symbol":"BTCUSDT","strategy_name":"trend","strategy_type":"trend","client_order_id":"client-test-1","venue_order_id":"42"},"expected_intent_revision":"intent-rev-1","expected_strategy_revision":"strategy-rev-1","reason":"composition acceptance","evidence_refs":["test-fixture"],"idempotency_key":"composition-test-request-1"}`
	created := adminRequest(http.MethodPost, "/api/capital/order-reconciliations", createRequest)
	if created.Code != http.StatusCreated {
		t.Fatalf("create case through production route: status=%d body=%s", created.Code, created.Body.String())
	}
	var createdEnvelope struct {
		Case struct {
			ID       string                            `json:"case_id"`
			Owner    accounting.OrderOwner             `json:"owner"`
			Revision int64                             `json:"revision"`
			Status   storage.OrderReconciliationStatus `json:"status"`
		} `json:"case"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &createdEnvelope); err != nil {
		t.Fatalf("decode created case: %v", err)
	}
	caseID := createdEnvelope.Case.ID
	if caseID == "" || createdEnvelope.Case.Owner != testOwner || createdEnvelope.Case.Revision != 1 || createdEnvelope.Case.Status != storage.OrderReconciliationPrepared {
		t.Fatalf("unexpected created case: %+v", createdEnvelope.Case)
	}

	// Missing strategy accounting and durable quarantine gate must keep both
	// mutation paths unavailable without changing the case or its audit trail.
	reconcileBody := `{"case_revision":1,"confirm":true}`
	reconciled := adminRequest(http.MethodPost, "/api/capital/order-reconciliations/"+caseID+"/reconcile", reconcileBody)
	if reconciled.Code != http.StatusServiceUnavailable {
		t.Fatalf("reconcile must remain blocked without Accounting: status=%d body=%s", reconciled.Code, reconciled.Body.String())
	}
	releaseBody := `{"case_revision":1,"evidence_hash":"` + strings.Repeat("a", 64) + `","confirm":true}`
	released := adminRequest(http.MethodPost, "/api/capital/order-reconciliations/"+caseID+"/release", releaseBody)
	if released.Code != http.StatusServiceUnavailable {
		t.Fatalf("release must remain blocked without Accounting/Gate: status=%d body=%s", released.Code, released.Body.String())
	}

	storageService.Stop()
	storageService = nil
	storageService = openStorageService()
	router = compose(storageService)
	adminRequestAfterRestart := func(method, path string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(method, path, nil)
		request.AddCookie(&http.Cookie{Name: "session_id", Value: adminSession.SessionID})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
	recoveryStatusAfterRestart := adminRequestAfterRestart(http.MethodGet, "/api/capital/funding-recovery/"+recoveryEpisode.ID)
	if recoveryStatusAfterRestart.Code != http.StatusOK || !strings.Contains(recoveryStatusAfterRestart.Body.String(), recoveryEpisode.ID) {
		t.Fatalf("read persisted recovery status after production restart: status=%d body=%s", recoveryStatusAfterRestart.Code, recoveryStatusAfterRestart.Body.String())
	}

	listResponse := adminRequestAfterRestart(http.MethodGet, "/api/capital/order-reconciliations")
	if listResponse.Code != http.StatusOK {
		t.Fatalf("read cases after StorageService restart: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var listEnvelope struct {
		Cases []struct {
			ID       string                            `json:"case_id"`
			Owner    accounting.OrderOwner             `json:"owner"`
			Revision int64                             `json:"revision"`
			Status   storage.OrderReconciliationStatus `json:"status"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("decode cases after restart: %v", err)
	}
	if len(listEnvelope.Cases) != 1 || listEnvelope.Cases[0].ID != caseID || listEnvelope.Cases[0].Owner != testOwner || listEnvelope.Cases[0].Status != storage.OrderReconciliationPrepared || listEnvelope.Cases[0].Revision != 1 {
		t.Fatalf("case was not durably read back after restart: %+v", listEnvelope.Cases)
	}

	auditResponse := adminRequestAfterRestart(http.MethodGet, "/api/capital/order-reconciliations/"+caseID+"/audit")
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("read audit after StorageService restart: status=%d body=%s", auditResponse.Code, auditResponse.Body.String())
	}
	var auditEnvelope struct {
		CaseID string                             `json:"case_id"`
		Audit  []storage.OrderReconciliationAudit `json:"audit"`
	}
	if err := json.Unmarshal(auditResponse.Body.Bytes(), &auditEnvelope); err != nil {
		t.Fatalf("decode audit after restart: %v", err)
	}
	if auditEnvelope.CaseID != caseID || len(auditEnvelope.Audit) != 1 || auditEnvelope.Audit[0].Action != "case_created" || auditEnvelope.Audit[0].Actor != "composition-test-admin" {
		t.Fatalf("case/audit persistence or fail-closed audit invariant violated: %+v", auditEnvelope)
	}
}

type productionRecoveryLookupVenue struct {
	exchange.IExchange
	queriedSymbol string
	queriedCID    string
	order         *exchange.Order
	fillBatch     *exchange.OrderFillBatch
	fillQueries   int
}

type failOnceFundingCarryRuntimeStateAdapter struct {
	*fundingCarryRuntimeStateAdapter
	failNextCAS         bool
	loseNextCASResponse bool
}

func (a *failOnceFundingCarryRuntimeStateAdapter) CompareAndSwapRuntimeState(ctx context.Context, name string, expectedVersion int, expectedPayload string, nextVersion int, nextPayload string) (bool, error) {
	if a.failNextCAS {
		a.failNextCAS = false
		return false, errors.New("injected runtime projection CAS response loss")
	}
	saved, err := a.fundingCarryRuntimeStateAdapter.CompareAndSwapRuntimeState(ctx, name, expectedVersion, expectedPayload, nextVersion, nextPayload)
	if err == nil && saved && a.loseNextCASResponse {
		a.loseNextCASResponse = false
		return false, errors.New("injected lost response after durable runtime projection CAS")
	}
	return saved, err
}

func (v *productionRecoveryLookupVenue) GetName() string       { return "binance" }
func (v *productionRecoveryLookupVenue) GetMarketType() string { return "futures" }
func (v *productionRecoveryLookupVenue) GetBaseAsset() string  { return "BTC" }
func (v *productionRecoveryLookupVenue) GetQuoteAsset() string { return "USDT" }
func (v *productionRecoveryLookupVenue) GetOrderByClientOrderID(_ context.Context, symbol, cid string) (*exchange.Order, error) {
	v.queriedSymbol, v.queriedCID = symbol, cid
	return v.order, nil
}

func (v *productionRecoveryLookupVenue) GetOrderFillBatch(_ context.Context, symbol string, orderID int64) (*exchange.OrderFillBatch, error) {
	v.fillQueries++
	if v.fillBatch == nil || v.fillBatch.Symbol != symbol || v.fillBatch.OrderID != orderID {
		return nil, fmt.Errorf("unexpected fill batch request symbol=%s order_id=%d", symbol, orderID)
	}
	batch := *v.fillBatch
	batch.Fills = append([]*exchange.OrderFill(nil), v.fillBatch.Fills...)
	return &batch, nil
}

type integrationRecoverySourceWindowVerifier struct{}

type productionFundingIncomeVenue struct {
	*productionRecoveryLookupVenue
	firstWindow       exchange.BinanceFundingIncomeWindow
	continuation      exchange.BinanceFundingIncomeContinuation
	windowCalls       int
	continuationCalls int
}

func (v *productionFundingIncomeVenue) GetFundingIncomeWindow(_ context.Context, symbol string, from time.Time, _ time.Duration) (exchange.BinanceFundingIncomeWindow, error) {
	v.windowCalls++
	if v.firstWindow.Symbol != symbol || !v.firstWindow.From.Equal(from) {
		return exchange.BinanceFundingIncomeWindow{}, errors.New("unexpected funding-income initial window request")
	}
	return v.firstWindow, nil
}

func (v *productionFundingIncomeVenue) GetFundingIncomeWindowRange(context.Context, string, time.Time, time.Time) (exchange.BinanceFundingIncomeWindow, error) {
	return exchange.BinanceFundingIncomeWindow{}, errors.New("unexpected direct range scan")
}

func (v *productionFundingIncomeVenue) GetFundingIncomeContinuation(_ context.Context, symbol string, initialFrom, previousThrough time.Time, _ time.Duration) (exchange.BinanceFundingIncomeContinuation, error) {
	v.continuationCalls++
	if v.continuation.Window.Symbol != symbol || !v.continuation.InitialFrom.Equal(initialFrom) || !v.continuation.Window.From.Equal(previousThrough) {
		return exchange.BinanceFundingIncomeContinuation{}, errors.New("unexpected funding-income continuation request")
	}
	return v.continuation, nil
}

func TestProductionFundingIncomeRouteProjectsAndReplaysOriginalCursorAfterAdvance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousSessionManager := web.SwapSessionManager(web.NewEphemeralSessionManager())
	t.Cleanup(func() {
		web.SetOrderReconciliationService(nil)
		if previousSessionManager == nil {
			previousSessionManager = web.NewEphemeralSessionManager()
		}
		web.SetSessionManager(previousSessionManager)
		gin.SetMode(gin.DebugMode)
	})
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "runtime", "funding-income-production.db")
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 16, 4
	cfg.Web.SharedDir = t.TempDir()
	storageService, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal("start production SQLite recovery storage:", err)
	}
	t.Cleanup(func() { storageService.Stop() })
	store := storageService.GetStorage().(*storage.SQLStorage)
	ctx := context.Background()
	const botID = "funding-income-production-bot"
	scope := fmt.Sprintf("%064x", 9179)
	oldGeneration, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal("claim original owner:", err)
	}
	origin := accounting.RecoveryOriginIntent{Bot: botID, Strategy: "funding_carry", Exchange: "binance", Market: "futures", AccountScope: scope,
		OwnerScopes: []string{scope}, Symbol: "BTCUSDT", ClientOrderID: "income-origin", Side: "BUY", IntentRevision: 1, IntentHash: strings.Repeat("a", 64)}
	episode := accounting.RecoveryEpisode{ID: "funding-income-production-episode", Bot: botID, Strategy: "funding_carry",
		AccountScopes: []string{scope}, OwnerGeneration: oldGeneration.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryBlocked}
	if _, _, err := store.CreateRecoveryEpisodeForIntent(ctx, oldGeneration, origin, episode); err != nil {
		t.Fatal("create exact held source episode:", err)
	}
	newGeneration, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal("claim retained runtime generation:", err)
	}
	const runtimeSchema = 8
	runtimePayload := `{"strategy":"funding_carry","futures_exchange":"binance","spot_exchange":"binance","symbol":"BTCUSDT","ownership_ready":true,"recovery_projection_schema":1}`
	if err := store.SetFundingCarryRuntimeState(ctx, newGeneration, &storage.StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: runtimeSchema, Payload: runtimePayload}); err != nil {
		t.Fatal("seed current generation runtime:", err)
	}
	takenOver, replayed, err := store.TakeOverRecoveryEpisode(ctx, newGeneration, accounting.RecoveryOwnerTakeoverAuthorization{
		EpisodeID: episode.ID, OperationID: "income-runtime-takeover", Actor: "admin", AuditRef: "income-production-incident",
		ExpectedOwnerGeneration: oldGeneration.ID(), ExpectedEpisodeRevision: 1, Confirmed: true})
	if err != nil || replayed || !takenOver.HasCurrentOwnerTakeover() {
		t.Fatalf("take over held episode: episode=%+v replayed=%t err=%v", takenOver, replayed, err)
	}

	from := time.Now().UTC().Add(-20 * time.Minute).Truncate(time.Millisecond)
	firstThrough := from.Add(time.Minute)
	secondThrough := firstThrough.Add(time.Minute)
	thirdThrough := secondThrough.Add(time.Minute)
	readAt := time.Now().UTC()
	firstRow := &income.Income{Symbol: "BTCUSDT", IncomeType: "FUNDING_FEE", Income: 0.01, RawIncome: "0.0100", Asset: "USDT", Info: "funding-1", TransactionID: 1, TradeTime: from.Add(10 * time.Second)}
	secondRow := &income.Income{Symbol: "BTCUSDT", IncomeType: "FUNDING_FEE", Income: -0.02, RawIncome: "-0.0200", Asset: "USDT", Info: "funding-2", TransactionID: 2, TradeTime: firstThrough.Add(10 * time.Second)}
	thirdRow := &income.Income{Symbol: "BTCUSDT", IncomeType: "FUNDING_FEE", Income: 0.03, RawIncome: "0.0300", Asset: "USDT", Info: "funding-3", TransactionID: 3, TradeTime: secondThrough.Add(10 * time.Second)}
	firstDigest, secondDigest := strings.Repeat("1", 64), strings.Repeat("2", 64)
	venue := &productionFundingIncomeVenue{productionRecoveryLookupVenue: &productionRecoveryLookupVenue{}}
	venue.firstWindow = exchange.BinanceFundingIncomeWindow{Symbol: "BTCUSDT", From: from, Through: firstThrough, ReadAt: readAt, Rows: []*income.Income{firstRow},
		TotalRows: 1, CompletionMarker: "complete_stable_double_scan_v1", EvidenceHash: firstDigest}
	venue.continuation = exchange.BinanceFundingIncomeContinuation{InitialFrom: from, HistoricalRows: []*income.Income{firstRow}, PriorEvidenceHash: firstDigest,
		CumulativeEvidenceHash: secondDigest, Window: exchange.BinanceFundingIncomeWindow{Symbol: "BTCUSDT", From: firstThrough, Through: secondThrough,
			ReadAt: readAt, Rows: []*income.Income{secondRow}, TotalRows: 1, CompletionMarker: "complete_stable_double_scan_v1", EvidenceHash: secondDigest}}
	gate := &execution.OpeningGate{}
	gate.Block(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration))
	carry := strategy.NewFundingCarryStrategy("funding_carry", cfg, config.SymbolConfig{Symbol: "BTCUSDT"}, venue, &productionRecoveryLookupVenue{}, nil, nil)
	carry.SetOpeningGate(gate)
	runtimeAdapter := &failOnceFundingCarryRuntimeStateAdapter{fundingCarryRuntimeStateAdapter: &fundingCarryRuntimeStateAdapter{
		strategyRuntimeStateAdapter: &strategyRuntimeStateAdapter{storageService: storageService, botID: botID},
		generation:                  newGeneration, fundingIncomeScope: scope}, failNextCAS: true}
	carry.SetRuntimeStateStore(runtimeAdapter)
	strategyManager := strategy.NewStrategyManager(cfg, 0)
	strategyManager.RegisterStrategy("funding_carry", carry, 1, 0)
	bots := &BotManager{runtimes: map[string]*BotRuntime{botID: {BotID: botID, Inner: &SymbolRuntime{fundingCarryReconciliationOnly: true, StrategyManager: strategyManager}}}}
	productionService, err := buildProductionOrderReconciliationService(store, bots)
	if err != nil {
		t.Fatal("build production recovery composition:", err)
	}
	if productionService.Accounting != nil || productionService.Gate != nil {
		t.Fatal("funding-income reconciliation must not enable automatic accounting or unlock")
	}
	web.SetOrderReconciliationService(productionService)
	router := gin.New()
	web.SetupRoutesWithConfig(router, cfg)
	adminSession, err := web.GetSessionManager().CreateSession("income-admin", "admin", "192.0.2.79", "funding-income-production")
	if err != nil {
		t.Fatal("create isolated admin session:", err)
	}
	postIncome := func(operationID string, expectedRevision uint64, windowFrom time.Time) *httptest.ResponseRecorder {
		t.Helper()
		body, marshalErr := json.Marshal(struct {
			OperationID             string    `json:"operation_id"`
			ExpectedEpisodeRevision uint64    `json:"expected_episode_revision"`
			From                    time.Time `json:"from"`
			AuditRef                string    `json:"audit_ref"`
			Confirmed               bool      `json:"confirmed"`
		}{operationID, expectedRevision, windowFrom, "income-production-audit", true})
		if marshalErr != nil {
			t.Fatal("encode admin request:", marshalErr)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/capital/funding-recovery/"+episode.ID+"/funding-income", strings.NewReader(string(body)))
		request.AddCookie(&http.Cookie{Name: "session_id", Value: adminSession.SessionID})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	firstResponse := postIncome("income-operation-a", takenOver.Revision, from)
	if firstResponse.Code != http.StatusConflict {
		t.Fatalf("injected first funding-income projection failure did not fail closed: status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	pendingReceipt, pendingReceiptFound, err := store.GetRecoveryReceipt(ctx, episode.ID, "income-operation-a")
	pendingOutbox, pendingOutboxFound, outboxErr := store.GetRecoveryOutbox(ctx, episode.ID, "income-operation-a")
	if err != nil || outboxErr != nil || !pendingReceiptFound || !pendingOutboxFound || pendingReceipt.State != accounting.RecoveryLedgerCommitted ||
		pendingOutbox.State != accounting.RecoveryProjectionPending || pendingReceipt.EpisodeRevision != pendingOutbox.TargetEpisodeRevision {
		t.Fatalf("failed projection did not leave an exact durable pending operation: receipt=%+v found=%t outbox=%+v found=%t errors=%v/%v",
			pendingReceipt, pendingReceiptFound, pendingOutbox, pendingOutboxFound, err, outboxErr)
	}
	retryPendingResponse := postIncome("income-operation-a", takenOver.Revision, from)
	if retryPendingResponse.Code != http.StatusOK {
		t.Fatalf("production retry of exact pending projection failed: status=%d body=%s", retryPendingResponse.Code, retryPendingResponse.Body.String())
	}
	var firstEnvelope struct {
		Readback struct {
			Episode accounting.RecoveryEpisode `json:"episode"`
		} `json:"readback"`
	}
	if err := json.Unmarshal(retryPendingResponse.Body.Bytes(), &firstEnvelope); err != nil || firstEnvelope.Readback.Episode.Revision <= takenOver.Revision {
		t.Fatalf("decode retried recovery projection revision: response=%s err=%v", retryPendingResponse.Body.String(), err)
	}
	if venue.windowCalls != 1 || venue.continuationCalls != 0 {
		t.Fatalf("pending projection retry queried funding source again: first=%d continuation=%d", venue.windowCalls, venue.continuationCalls)
	}
	secondResponse := postIncome("income-operation-b", firstEnvelope.Readback.Episode.Revision, firstThrough)
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("cursor-advancing production funding-income request: status=%d body=%s", secondResponse.Code, secondResponse.Body.String())
	}
	currentCursor, found, err := store.GetRecoverySourceCursor(ctx, accounting.RecoverySourceIdentity{Source: accounting.RecoverySourceBinanceFuturesIncomeV1,
		Exchange: "binance", Market: "futures", AccountScope: scope, Symbol: "BTCUSDT", Kind: accounting.RecoveryFundingEvent, Asset: "USDT"})
	if err != nil || !found || currentCursor.Revision != 2 || !currentCursor.Through.Equal(secondThrough) {
		t.Fatalf("second request did not advance durable cursor: cursor=%+v found=%t err=%v", currentCursor, found, err)
	}
	retryResponse := postIncome("income-operation-a", takenOver.Revision, from)
	if retryResponse.Code != http.StatusOK {
		t.Fatalf("retry old production operation after cursor advance: status=%d body=%s", retryResponse.Code, retryResponse.Body.String())
	}
	var retryEnvelope struct {
		Readback struct {
			Result struct {
				Replayed bool `json:"replayed"`
			} `json:"result"`
			Cursor   accounting.RecoverySourceCursor `json:"cursor"`
			Verified bool                            `json:"verified"`
		} `json:"readback"`
	}
	if err := json.Unmarshal(retryResponse.Body.Bytes(), &retryEnvelope); err != nil || !retryEnvelope.Readback.Verified || !retryEnvelope.Readback.Result.Replayed ||
		retryEnvelope.Readback.Cursor.Revision != 1 || !retryEnvelope.Readback.Cursor.Through.Equal(firstThrough) {
		t.Fatalf("old operation did not return its own durable cursor snapshot: readback=%+v err=%v", retryEnvelope.Readback, err)
	}
	finalCursor, found, err := store.GetRecoverySourceCursor(ctx, currentCursor.Identity())
	if err != nil || !found || finalCursor.Revision != 2 || !finalCursor.Through.Equal(secondThrough) {
		t.Fatalf("old retry changed current cursor: cursor=%+v found=%t err=%v", finalCursor, found, err)
	}
	if venue.windowCalls != 1 || venue.continuationCalls != 1 {
		t.Fatalf("idempotent retry queried source again: first=%d continuation=%d", venue.windowCalls, venue.continuationCalls)
	}
	hold, held, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" || !gate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		t.Fatalf("funding-income production route released its safety hold: hold=%+v held=%t err=%v", hold, held, err)
	}

	// A complete rescan that changes an already accepted Binance row must first
	// persist the anomaly without moving the cursor. The admin route then appends
	// the exact source delta; only a second matching full-history scan may move it.
	firstRowChanged := *firstRow
	firstRowChanged.RawIncome, firstRowChanged.Income = "0.0150", 0.015
	firstRow = &firstRowChanged
	anomalyPriorDigest, postAdjustmentDigest := strings.Repeat("4", 64), strings.Repeat("5", 64)
	venue.continuation = exchange.BinanceFundingIncomeContinuation{InitialFrom: from, HistoricalRows: []*income.Income{firstRow, secondRow},
		PriorEvidenceHash: anomalyPriorDigest, CumulativeEvidenceHash: postAdjustmentDigest, Window: exchange.BinanceFundingIncomeWindow{Symbol: "BTCUSDT",
			From: secondThrough, Through: thirdThrough, ReadAt: readAt, Rows: []*income.Income{thirdRow}, TotalRows: 1,
			CompletionMarker: "complete_stable_double_scan_v1", EvidenceHash: strings.Repeat("3", 64)}}
	currentEpisode, found, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found {
		t.Fatal("read episode before anomaly detection:", err)
	}
	anomalyDetected := postIncome("income-anomaly-detect", currentEpisode.Revision, secondThrough)
	if anomalyDetected.Code != http.StatusConflict {
		t.Fatalf("changed full-history row should be captured and held: status=%d body=%s", anomalyDetected.Code, anomalyDetected.Body.String())
	}
	anomalies, err := store.ListRecoverySourceAnomalies(ctx, episode.ID)
	if err != nil || len(anomalies) != 1 || anomalies[0].State != accounting.RecoverySourceAnomalyOpen || anomalies[0].Facts[0].DeltaAmount != "0.005" {
		t.Fatalf("exact changed-row anomaly was not durably captured: anomalies=%+v err=%v", anomalies, err)
	}
	unchangedCursor, found, err := store.GetRecoverySourceCursor(ctx, currentCursor.Identity())
	if err != nil || !found || unchangedCursor.Revision != 2 || !unchangedCursor.Through.Equal(secondThrough) {
		t.Fatalf("anomaly capture advanced the source cursor: cursor=%+v found=%t err=%v", unchangedCursor, found, err)
	}
	postAdjustmentResponse := httptest.NewRecorder()
	adjustmentBody, err := json.Marshal(map[string]any{"operation_id": "income-anomaly-adjustment", "expected_episode_revision": currentEpisode.Revision,
		"expected_anomaly_revision": anomalies[0].Revision, "evidence_hash": strings.Repeat("f", 64), "audit_ref": "income-anomaly-ticket", "confirmed": true})
	if err != nil {
		t.Fatal("encode source anomaly adjustment:", err)
	}
	adjustmentPath := "/api/capital/funding-recovery/" + episode.ID + "/source-anomalies/" + anomalies[0].ID + "/adjust"
	postAdjustment := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, adjustmentPath, strings.NewReader(string(adjustmentBody)))
		request.AddCookie(&http.Cookie{Name: "session_id", Value: adminSession.SessionID})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	runtimeAdapter.failNextCAS = true
	postAdjustmentResponse = postAdjustment()
	if postAdjustmentResponse.Code != http.StatusConflict {
		t.Fatalf("injected adjustment projection failure did not remain fail-closed: status=%d body=%s", postAdjustmentResponse.Code, postAdjustmentResponse.Body.String())
	}
	pendingAdjustmentReceipt, receiptFound, receiptErr := store.GetRecoveryReceipt(ctx, episode.ID, "income-anomaly-adjustment")
	pendingAdjustmentOutbox, outboxFound, outboxErr := store.GetRecoveryOutbox(ctx, episode.ID, "income-anomaly-adjustment")
	if receiptErr != nil || outboxErr != nil || !receiptFound || !outboxFound || pendingAdjustmentReceipt.State != accounting.RecoveryLedgerCommitted ||
		pendingAdjustmentOutbox.State != accounting.RecoveryProjectionPending {
		t.Fatalf("failed anomaly projection did not leave exact durable pending operation: receipt=%+v outbox=%+v errors=%v/%v",
			pendingAdjustmentReceipt, pendingAdjustmentOutbox, receiptErr, outboxErr)
	}
	postAdjustmentResponse = postAdjustment()
	if postAdjustmentResponse.Code != http.StatusOK {
		t.Fatalf("same-operation retry did not project pending anomaly adjustment: status=%d body=%s", postAdjustmentResponse.Code, postAdjustmentResponse.Body.String())
	}
	adjustedAnomaly, found, err := store.GetRecoverySourceAnomaly(ctx, episode.ID, anomalies[0].ID)
	if err != nil || !found || adjustedAnomaly.State != accounting.RecoverySourceAnomalyAdjustmentApplied {
		t.Fatalf("source anomaly adjustment was not durably projected: anomaly=%+v found=%t err=%v", adjustedAnomaly, found, err)
	}
	ledgerAfterAdjustment, err := store.ListRecoveryEvents(ctx, episode.ID)
	if err != nil {
		t.Fatal("read append-only funding ledger:", err)
	}
	originalPreserved, exactAdjustment := false, false
	adjustmentCount := 0
	for _, event := range ledgerAfterAdjustment {
		if event.Source == accounting.RecoverySourceBinanceFuturesIncomeV1 && event.ExternalID == "1" && event.Amount == "0.0100" {
			originalPreserved = true
		}
		if event.Source == accounting.RecoveryManualFundingAdjustmentSource && event.RelatedExternalID == "1" {
			adjustmentCount++
			exactAdjustment = event.Amount == "0.005" && event.FundingAdjustment != nil && event.FundingAdjustment.OriginalAmountRaw == "0.0100" && event.FundingAdjustment.ObservedAmountRaw == "0.0150"
		}
	}
	if !originalPreserved || !exactAdjustment || adjustmentCount != 1 {
		t.Fatalf("original source fact was overwritten or exact adjustment replay duplicated/missing: %+v", ledgerAfterAdjustment)
	}

	currentEpisode, found, err = store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found {
		t.Fatal("read episode before post-adjustment source verification:", err)
	}
	verifiedRescan := postIncome("income-anomaly-verified-rescan", currentEpisode.Revision, secondThrough)
	if verifiedRescan.Code != http.StatusOK {
		t.Fatalf("complete rescan after explicit adjustment failed: status=%d body=%s", verifiedRescan.Code, verifiedRescan.Body.String())
	}
	verifiedAnomaly, found, err := store.GetRecoverySourceAnomaly(ctx, episode.ID, anomalies[0].ID)
	verifiedCursor, cursorFound, cursorErr := store.GetRecoverySourceCursor(ctx, currentCursor.Identity())
	if err != nil || !found || verifiedAnomaly.State != accounting.RecoverySourceAnomalyVerified || cursorErr != nil || !cursorFound ||
		verifiedCursor.Revision != 3 || !verifiedCursor.Through.Equal(thirdThrough) {
		t.Fatalf("rescan did not atomically verify anomaly and advance cursor: anomaly=%+v cursor=%+v found=%t/%t errors=%v/%v",
			verifiedAnomaly, verifiedCursor, found, cursorFound, err, cursorErr)
	}

	// Simulate the crash window where runtime CAS committed but its response was
	// lost before the durable outbox could be marked projected.
	fourthThrough := thirdThrough.Add(time.Minute)
	fourthDigest := strings.Repeat("6", 64)
	fourthRow := &income.Income{Symbol: "BTCUSDT", IncomeType: "FUNDING_FEE", Income: -0.04, RawIncome: "-0.0400", Asset: "USDT", Info: "funding-4", TransactionID: 4, TradeTime: thirdThrough.Add(10 * time.Second)}
	venue.continuation = exchange.BinanceFundingIncomeContinuation{InitialFrom: from, HistoricalRows: []*income.Income{firstRow, secondRow, thirdRow},
		PriorEvidenceHash: postAdjustmentDigest, CumulativeEvidenceHash: fourthDigest, Window: exchange.BinanceFundingIncomeWindow{Symbol: "BTCUSDT",
			From: thirdThrough, Through: fourthThrough, ReadAt: readAt, Rows: []*income.Income{fourthRow}, TotalRows: 1,
			CompletionMarker: "complete_stable_double_scan_v1", EvidenceHash: fourthDigest}}
	currentEpisode, found, err = store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || currentEpisode.State != accounting.RecoveryProjected {
		t.Fatalf("read current episode before response-loss injection: episode=%+v found=%t err=%v", currentEpisode, found, err)
	}
	runtimeAdapter.loseNextCASResponse = true
	responseLost := postIncome("income-operation-c", currentEpisode.Revision, thirdThrough)
	if responseLost.Code != http.StatusConflict {
		t.Fatalf("runtime CAS response-loss did not remain fail-closed: status=%d body=%s", responseLost.Code, responseLost.Body.String())
	}
	responseLostReceipt, receiptFound, err := store.GetRecoveryReceipt(ctx, episode.ID, "income-operation-c")
	responseLostOutbox, outboxFound, outboxErr := store.GetRecoveryOutbox(ctx, episode.ID, "income-operation-c")
	if err != nil || outboxErr != nil || !receiptFound || !outboxFound || responseLostReceipt.State != accounting.RecoveryLedgerCommitted ||
		responseLostOutbox.State != accounting.RecoveryProjectionPending {
		t.Fatalf("CAS response-loss did not leave the expected durable pending state: receipt=%+v outbox=%+v errors=%v/%v",
			responseLostReceipt, responseLostOutbox, err, outboxErr)
	}
	pendingEpisode, found, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || pendingEpisode.State != accounting.RecoveryLedgerCommitted || pendingEpisode.Revision != responseLostOutbox.TargetEpisodeRevision {
		t.Fatalf("runtime CAS response-loss changed durable episode phase: episode=%+v found=%t err=%v", pendingEpisode, found, err)
	}
	runtimeState, err := store.GetStrategyRuntimeStateContext(ctx, botID, "funding_carry")
	if err != nil || runtimeState == nil || !fundingCarryProjectionMarkerReadbackMatches(runtimeState.Payload, episode.ID, "income-operation-c", responseLostOutbox) {
		t.Fatalf("runtime CAS did not durably persist projection marker before response loss: state=%+v err=%v", runtimeState, err)
	}

	// Restart a real test-binary subprocess against the durable SQLite file. The
	// child rebuilds the production route, claims a new generation, explicitly
	// takes over, and replays the original operation.
	storageService.Stop()
	resultPath := filepath.Join(t.TempDir(), "restart-child-result")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("locate test executable for restart helper:", err)
	}
	processCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	child := exec.CommandContext(processCtx, executable, "-test.run=^TestFundingIncomeRecoveryAfterSQLiteRestartProcessHelper$", "-test.count=1")
	child.Env = append(os.Environ(), fundingIncomeRecoveryProcessDBEnv+"="+cfg.Storage.Path,
		fundingIncomeRecoveryProcessResultEnv+"="+resultPath)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("post-restart production recovery subprocess failed: %v; output=%s", err, output)
	}
	marker, err := os.ReadFile(resultPath)
	if err != nil || string(marker) != "ORIGINAL_OPERATION_REPLAYED" {
		t.Fatalf("restart process did not confirm original-operation recovery: marker=%q err=%v", marker, err)
	}

	storageService, err = storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal("reopen production SQLite recovery storage after child restart:", err)
	}
	t.Cleanup(func() { storageService.Stop() })
	store = storageService.GetStorage().(*storage.SQLStorage)
	currentEpisode, found, err = store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || currentEpisode.State != accounting.RecoveryProjected || !currentEpisode.HasCurrentOwnerTakeover() {
		t.Fatalf("post-restart recovery did not persist projected episode and audited owner: episode=%+v found=%t err=%v", currentEpisode, found, err)
	}
	finalReceipt, receiptFound, err := store.GetRecoveryReceipt(ctx, episode.ID, "income-operation-c")
	finalOutbox, outboxFound, outboxErr := store.GetRecoveryOutbox(ctx, episode.ID, "income-operation-c")
	if err != nil || outboxErr != nil || !receiptFound || !outboxFound || finalReceipt.State != accounting.RecoveryProjected || finalOutbox.State != accounting.RecoveryProjected {
		t.Fatalf("restart recovery did not persist original receipt/outbox projection: receipt=%+v outbox=%+v found=%t/%t errors=%v/%v",
			finalReceipt, finalOutbox, receiptFound, outboxFound, err, outboxErr)
	}
	finalHold, holdFound, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !holdFound || finalHold.State != "held" {
		t.Fatalf("restart recovery unexpectedly released durable hold: hold=%+v found=%t err=%v", finalHold, holdFound, err)
	}
	finalCursor, found, err = store.GetRecoverySourceCursor(ctx, currentCursor.Identity())
	if err != nil || !found || finalCursor.Revision != 4 || !finalCursor.Through.Equal(fourthThrough) || venue.continuationCalls != 4 {
		t.Fatalf("post-restart retry changed current source state or requeried parent venue: cursor=%+v found=%t calls=%d err=%v", finalCursor, found, venue.continuationCalls, err)
	}
}

func TestFundingIncomeRecoveryAfterSQLiteRestartProcessHelper(t *testing.T) {
	dbPath := os.Getenv(fundingIncomeRecoveryProcessDBEnv)
	resultPath := os.Getenv(fundingIncomeRecoveryProcessResultEnv)
	if dbPath == "" || resultPath == "" {
		t.Skip("invoked only by the FundingIncome subprocess restart parent test")
	}
	gin.SetMode(gin.TestMode)
	previousSessionManager := web.SwapSessionManager(web.NewEphemeralSessionManager())
	t.Cleanup(func() {
		web.SetOrderReconciliationService(nil)
		if previousSessionManager == nil {
			previousSessionManager = web.NewEphemeralSessionManager()
		}
		web.SetSessionManager(previousSessionManager)
		gin.SetMode(gin.DebugMode)
	})
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type, cfg.Storage.Path = true, "sqlite", dbPath
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 16, 4
	cfg.Web.SharedDir = t.TempDir()
	cfg.Strategies.Enabled = true
	cfg.Strategies.Configs = map[string]config.StrategyConfig{"funding_carry": {Enabled: true, Type: "funding_carry", Weight: 1}}
	storageService, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal("start child SQLite recovery storage:", err)
	}
	defer storageService.Stop()
	store := storageService.GetStorage().(*storage.SQLStorage)
	const botID = "funding-income-production-bot"
	const episodeID = "funding-income-production-episode"
	scope := fmt.Sprintf("%064x", 9179)
	episode, found, err := store.GetRecoveryEpisode(t.Context(), episodeID)
	if err != nil || !found || episode.State != accounting.RecoveryLedgerCommitted {
		t.Fatalf("child did not read durable pending episode: episode=%+v found=%t err=%v", episode, found, err)
	}
	generation, err := store.ClaimFundingCarryRuntimeGeneration(t.Context(), []string{scope})
	if err != nil {
		t.Fatal("claim child recovery runtime generation:", err)
	}
	gate := &execution.OpeningGate{}
	venue := &productionFundingIncomeVenue{productionRecoveryLookupVenue: &productionRecoveryLookupVenue{}}
	carry := strategy.NewFundingCarryStrategy("funding_carry", cfg, config.SymbolConfig{Symbol: "BTCUSDT"}, venue, &productionRecoveryLookupVenue{}, nil, nil)
	carry.SetOpeningGate(gate)
	carry.SetRuntimeStateStore(&fundingCarryRuntimeStateAdapter{strategyRuntimeStateAdapter: &strategyRuntimeStateAdapter{storageService: storageService, botID: botID},
		generation: generation, fundingIncomeScope: scope})
	strategyManager := strategy.NewStrategyManager(cfg, 0)
	strategyManager.RegisterStrategy("funding_carry", carry, 1, 0)
	reconciliationOnly, err := startFundingCarryManagedStrategy(t.Context(), strategyManager, carry, gate)
	if err != nil || !reconciliationOnly || !gate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		t.Fatalf("production startup did not restore the exact durable hold Gate before admin routes: reconciliation_only=%t blocked=%t err=%v",
			reconciliationOnly, gate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)), err)
	}
	bots := &BotManager{runtimes: map[string]*BotRuntime{botID: {BotID: botID, Inner: &SymbolRuntime{fundingCarryReconciliationOnly: true, StrategyManager: strategyManager}}}}
	service, err := buildProductionOrderReconciliationService(store, bots)
	if err != nil {
		t.Fatal("build child production recovery composition:", err)
	}
	web.SetOrderReconciliationService(service)
	router := gin.New()
	web.SetupRoutesWithConfig(router, cfg)
	session, err := web.GetSessionManager().CreateSession("income-admin", "admin", "192.0.2.80", "funding-income-restart")
	if err != nil {
		t.Fatal("create child admin session:", err)
	}
	requestWithSession := func(sessionID, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		return requestWithSession(session.SessionID, method, path, body)
	}
	takeoverBody, err := json.Marshal(map[string]any{"operation_id": "income-restart-takeover",
		"expected_episode_revision": episode.Revision, "audit_ref": "income-restart-incident", "confirmed": true})
	if err != nil {
		t.Fatal("encode child takeover request:", err)
	}
	if response := request(http.MethodPost, "/api/capital/funding-recovery/"+episode.ID+"/takeover", string(takeoverBody)); response.Code != http.StatusOK {
		t.Fatalf("child explicit takeover failed: status=%d body=%s", response.Code, response.Body.String())
	}
	cursor, found, err := store.GetRecoverySourceCursor(t.Context(), accounting.RecoverySourceIdentity{Source: accounting.RecoverySourceBinanceFuturesIncomeV1,
		Exchange: "binance", Market: "futures", AccountScope: scope, Symbol: "BTCUSDT", Kind: accounting.RecoveryFundingEvent, Asset: "USDT"})
	if err != nil || !found || cursor.Revision != 4 {
		t.Fatalf("child did not read durable source cursor before replay: cursor=%+v found=%t err=%v", cursor, found, err)
	}
	incomeRetry := strategy.ApplyFundingRecoveryIncomeRequest{EpisodeID: episode.ID, OperationID: "income-operation-c",
		ExpectedEpisodeRevision: finalFundingIncomeRequestRevision(t, store, episode.ID), From: cursor.From.UTC(), Actor: "income-admin",
		AuditRef: "income-production-audit", Confirmed: true}
	incomeBody, err := json.Marshal(map[string]any{"operation_id": incomeRetry.OperationID, "expected_episode_revision": incomeRetry.ExpectedEpisodeRevision,
		"from": incomeRetry.From, "audit_ref": incomeRetry.AuditRef, "confirmed": incomeRetry.Confirmed})
	if err != nil {
		t.Fatal("encode child original-operation retry:", err)
	}
	const retryPath = "/api/capital/funding-recovery/" + episodeID + "/funding-income"
	wrongActor, err := web.GetSessionManager().CreateSession("different-admin", "admin", "192.0.2.81", "funding-income-wrong-actor")
	if err != nil {
		t.Fatal("create different admin session:", err)
	}
	if response := requestWithSession(wrongActor.SessionID, http.MethodPost, retryPath, string(incomeBody)); response.Code != http.StatusConflict {
		t.Fatalf("original operation accepted a different actor after restart: status=%d body=%s", response.Code, response.Body.String())
	}
	var wrongAuditBody map[string]any
	if err := json.Unmarshal(incomeBody, &wrongAuditBody); err != nil {
		t.Fatal("decode original retry request:", err)
	}
	wrongAuditBody["audit_ref"] = "different-audit-ref"
	wrongAuditPayload, err := json.Marshal(wrongAuditBody)
	if err != nil {
		t.Fatal("encode wrong audit reference request:", err)
	}
	if response := request(http.MethodPost, retryPath, string(wrongAuditPayload)); response.Code != http.StatusConflict {
		t.Fatalf("original operation accepted a different audit ref after restart: status=%d body=%s", response.Code, response.Body.String())
	}
	response := request(http.MethodPost, retryPath, string(incomeBody))
	if response.Code != http.StatusOK {
		_, directErr := carry.ApplyFundingRecoveryIncome(t.Context(), incomeRetry)
		t.Fatalf("child production route failed to replay original operation: status=%d body=%s direct_error=%v", response.Code, response.Body.String(), directErr)
	}
	var replay struct {
		Readback struct {
			Result struct {
				Replayed bool `json:"replayed"`
			} `json:"result"`
			Cursor   accounting.RecoverySourceCursor `json:"cursor"`
			Verified bool                            `json:"verified"`
		} `json:"readback"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &replay); err != nil || !replay.Readback.Verified || !replay.Readback.Result.Replayed ||
		replay.Readback.Cursor.Revision != cursor.Revision || !replay.Readback.Cursor.Through.Equal(cursor.Through) {
		t.Fatalf("child replay did not return original durable cursor snapshot: readback=%+v err=%v", replay.Readback, err)
	}
	if venue.windowCalls != 0 || venue.continuationCalls != 0 {
		t.Fatalf("child original-operation replay re-queried venue: initial=%d continuation=%d", venue.windowCalls, venue.continuationCalls)
	}
	if !gate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		t.Fatal("child original-operation replay unexpectedly cleared episode Gate")
	}
	hold, held, err := store.GetRecoveryHold(t.Context(), episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" {
		t.Fatalf("child original-operation replay changed durable recovery hold: hold=%+v held=%t err=%v", hold, held, err)
	}
	if err := os.WriteFile(resultPath, []byte("ORIGINAL_OPERATION_REPLAYED"), 0600); err != nil {
		t.Fatal("write child restart result marker:", err)
	}
}

func finalFundingIncomeRequestRevision(t *testing.T, store storage.FundingRecoveryStore, episodeID string) uint64 {
	t.Helper()
	outboxes, err := store.ListRecoveryOutboxes(t.Context(), episodeID)
	if err != nil {
		t.Fatal("list durable funding-income operations:", err)
	}
	for _, outbox := range outboxes {
		if outbox.OperationID == "income-operation-c" {
			return outbox.TargetEpisodeRevision - 1
		}
	}
	t.Fatal("original income operation outbox not found")
	return 0
}

func (integrationRecoverySourceWindowVerifier) VerifyRecoverySourceWindow(_ context.Context, window accounting.RecoverySourceWindow, events []accounting.RecoveryEvent) error {
	if window.CompletionMarker != accounting.RecoverySourceWindowCompleteV1 || window.Rows != uint64(len(events)) || window.TotalRows != uint64(len(events)) {
		return errors.New("test source window is not complete")
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	if window.EvidenceHash != fmt.Sprintf("%x", digest[:]) {
		return errors.New("test source evidence hash mismatch")
	}
	return nil
}

func TestProductionTakeoverProjectsOnlyExistingHashedOutbox(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalSessionManager := web.SwapSessionManager(web.NewEphemeralSessionManager())
	t.Cleanup(func() {
		web.SetOrderReconciliationService(nil)
		if originalSessionManager == nil {
			originalSessionManager = web.NewEphemeralSessionManager()
		}
		web.SetSessionManager(originalSessionManager)
		gin.SetMode(gin.DebugMode)
	})
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "runtime", "takeover-outbox.db")
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 16, 4
	cfg.Web.SharedDir = t.TempDir()
	storageService, err := storage.NewStorageService(cfg, t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storageService.Stop)
	store, ok := storageService.GetStorage().(*storage.SQLStorage)
	if !ok {
		t.Fatal("production fixture did not provide SQLStorage")
	}
	ctx := t.Context()
	scope := fmt.Sprintf("%064x", 812)
	oldOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	episode := accounting.RecoveryEpisode{ID: "takeover-outbox-episode", Bot: "takeover-outbox-bot", Strategy: "funding_carry", AccountScopes: []string{scope}, OwnerGeneration: oldOwner.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared}
	if _, err = store.CreateRecoveryEpisode(ctx, oldOwner, episode); err != nil {
		t.Fatal(err)
	}
	runtimePayload := `{"strategy":"funding_carry","futures_exchange":"binance","spot_exchange":"binance","symbol":"BTCUSDT","ownership_ready":true,"recovery_projection_schema":1}`
	const runtimeSchema = 8
	runtimeDigest := sha256.Sum256([]byte(runtimePayload))
	observedAt := time.Now().UTC().Truncate(time.Millisecond)
	event := accounting.RecoveryEvent{ID: "existing-borrow-event", Source: accounting.RecoverySourceBinanceMarginDebtV1, Exchange: "binance", Market: "spot_margin", AccountScope: scope, Kind: accounting.RecoveryBorrowEvent, ExternalID: "borrow-812", EpisodeID: episode.ID, Asset: "BTC", Amount: "0.1", SourceRevision: "r1", SourceHash: fmt.Sprintf("%064x", 813), ObservedAt: observedAt,
		DebtEvidence: &accounting.RecoveryDebtEvidence{PrincipalRaw: "0.1", InterestRaw: "0"}}
	windowFrom, windowThrough := observedAt.Add(-time.Minute), observedAt.Add(time.Minute)
	window := &accounting.RecoverySourceWindow{Source: event.Source, Exchange: event.Exchange, Market: event.Market, AccountScope: event.AccountScope,
		Kind: event.Kind, Asset: event.Asset, From: windowFrom, Through: windowThrough, Rows: 1, TotalRows: 1,
		CompletionMarker: accounting.RecoverySourceWindowCompleteV1, EvidenceHash: strings.Repeat("0", 64)}
	windowDigest, err := accounting.RecoverySourceWindowEvidenceHash(*window, []accounting.RecoveryEvent{event})
	if err != nil {
		t.Fatal("compute trusted complete test source digest:", err)
	}
	window.EvidenceHash = windowDigest
	operation := accounting.RecoveryOperation{EpisodeID: episode.ID, OperationID: "existing-safe-projection", HashVersion: 1, ExpectedEpisodeRevision: 1, ExpectedRuntimeRevision: fmt.Sprintf("%d:%x", runtimeSchema, runtimeDigest[:]), ExpectedEventRevisions: map[string]uint64{}, Events: []accounting.RecoveryEvent{event}, SourceWindow: window, Actor: "admin-before-restart", AuditRef: "prior-incident", EvidenceHash: fmt.Sprintf("%064x", 814), EvidenceReadAt: observedAt}
	if _, replayed, err := store.ApplyRecoveryOperation(ctx, oldOwner, operation); err != nil || replayed {
		t.Fatalf("create pre-existing hash-validated outbox: replayed=%t err=%v", replayed, err)
	}
	newOwner, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetFundingCarryRuntimeState(ctx, newOwner, &storage.StrategyRuntimeState{BotID: episode.Bot, StrategyName: "funding_carry", SchemaVersion: runtimeSchema, Payload: runtimePayload}); err != nil {
		t.Fatal(err)
	}
	futures := &productionRecoveryLookupVenue{}
	spot := &productionRecoveryLookupVenue{}
	carry := strategy.NewFundingCarryStrategy("funding_carry", &config.Config{}, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
	carry.SetRuntimeStateStore(&fundingCarryRuntimeStateAdapter{strategyRuntimeStateAdapter: &strategyRuntimeStateAdapter{storageService: storageService, botID: episode.Bot}, generation: newOwner})
	strategyManager := strategy.NewStrategyManager(cfg, 0)
	strategyManager.RegisterStrategy("funding_carry", carry, 1, 0)
	bots := &BotManager{runtimes: map[string]*BotRuntime{episode.Bot: {BotID: episode.Bot, Inner: &SymbolRuntime{fundingCarryReconciliationOnly: true, StrategyManager: strategyManager}}}}
	service, err := buildProductionOrderReconciliationService(store, bots)
	if err != nil {
		t.Fatal(err)
	}
	if service.Accounting != nil || service.Gate != nil {
		t.Fatal("outbox projection must not wire accounting action or Gate release")
	}
	web.SetOrderReconciliationService(service)
	router := gin.New()
	router.Use(gin.Recovery())
	web.SetupRoutesWithConfig(router, cfg)
	adminSession, err := web.GetSessionManager().CreateSession("outbox-admin", "admin", "192.0.2.48", "takeover-outbox")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/capital/funding-recovery/"+episode.ID+"/takeover", strings.NewReader(`{"operation_id":"explicit-outbox-takeover","expected_episode_revision":2,"audit_ref":"takeover-outbox-incident","confirmed":true}`))
	request.AddCookie(&http.Cookie{Name: "session_id", Value: adminSession.SessionID})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("takeover+existing projection route: status=%d body=%s", response.Code, response.Body.String())
	}
	retryRequest := httptest.NewRequest(http.MethodPost, "/api/capital/funding-recovery/"+episode.ID+"/takeover", strings.NewReader(`{"operation_id":"explicit-outbox-takeover","expected_episode_revision":2,"audit_ref":"takeover-outbox-incident","confirmed":true}`))
	retryRequest.AddCookie(&http.Cookie{Name: "session_id", Value: adminSession.SessionID})
	retryResponse := httptest.NewRecorder()
	router.ServeHTTP(retryResponse, retryRequest)
	if retryResponse.Code != http.StatusOK {
		t.Fatalf("idempotent takeover/projection response-loss retry: status=%d body=%s", retryResponse.Code, retryResponse.Body.String())
	}
	projectedEpisode, found, err := store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || projectedEpisode.State != accounting.RecoveryProjected || projectedEpisode.OwnerGeneration != newOwner.ID() || !projectedEpisode.HasCurrentOwnerTakeover() {
		t.Fatalf("projected episode lost explicit takeover: episode=%+v found=%t err=%v", projectedEpisode, found, err)
	}
	outbox, found, err := store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || outbox.State != accounting.RecoveryProjected || outbox.AppliedHash != outbox.ProjectionHash {
		t.Fatalf("existing outbox was not durably marked/read back: outbox=%+v found=%t err=%v", outbox, found, err)
	}
	projectedRuntime, err := store.GetStrategyRuntimeState(episode.Bot, "funding_carry")
	if err != nil || projectedRuntime == nil || !strings.Contains(projectedRuntime.Payload, operation.OperationID) || !strings.Contains(projectedRuntime.Payload, "existing-borrow-event") {
		t.Fatalf("runtime projection was not durably read back: state=%+v err=%v", projectedRuntime, err)
	}
	hold, held, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" {
		t.Fatalf("takeover projection released original hold: hold=%+v held=%t err=%v", hold, held, err)
	}
}

func TestProductionFundingRecoveryRouteTakeoverAndFoundUsesRetainedRuntime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalSessionManager := web.SwapSessionManager(web.NewEphemeralSessionManager())
	t.Cleanup(func() {
		web.SetOrderReconciliationService(nil)
		if originalSessionManager == nil {
			originalSessionManager = web.NewEphemeralSessionManager()
		}
		web.SetSessionManager(originalSessionManager)
		gin.SetMode(gin.DebugMode)
	})

	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "runtime", "recovery-e2e.db")
	cfg.Storage.BufferSize = 16
	cfg.Storage.BatchSize = 4
	cfg.Web.SharedDir = t.TempDir()
	storageService, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("start isolated production SQLite storage: %v", err)
	}
	t.Cleanup(storageService.Stop)
	recoveryStore, ok := storageService.GetStorage().(storage.FundingRecoveryStore)
	if !ok {
		t.Fatal("production SQLite storage lacks FundingRecoveryStore")
	}
	generationStore, ok := storageService.GetStorage().(storage.FundingCarryRuntimeGenerationStore)
	if !ok {
		t.Fatal("production SQLite storage lacks runtime-generation storage")
	}

	ctx := context.Background()
	scope := fmt.Sprintf("%064x", 123)
	oldGeneration, err := generationStore.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{
		SchemaVersion: 1, Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Side: "BUY",
		OrderType: "LIMIT", TimeInForce: "GTC", Price: "100", Quantity: "0.5", ClientOrderID: "production-recovery-cid",
	}
	intentHash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	action := accounting.RecoveryAction{
		ID: "production-recovery-action", Ordinal: 1, Kind: accounting.RecoveryActionOrder, Role: "open_futures",
		AccountScope: scope, Market: intent.Market, Symbol: intent.Symbol, OwnerGeneration: oldGeneration.ID(), OrderIntentHash: intentHash, Status: "authorized",
	}
	episode := accounting.RecoveryEpisode{
		ID: "production-recovery-episode", Bot: "production-recovery-bot", Strategy: "funding_carry", AccountScopes: []string{scope},
		OwnerGeneration: oldGeneration.ID(), Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1,
		State: accounting.RecoveryPrepared, ActionPlan: []accounting.RecoveryAction{action},
	}
	if _, err := recoveryStore.CreateRecoveryEpisode(ctx, oldGeneration, episode); err != nil {
		t.Fatalf("create episode and held protection: %v", err)
	}
	permit := accounting.OneShotPermit{
		ID: "production-recovery-permit", EpisodeID: episode.ID, Generation: 1, ActionID: action.ID, ActionOrdinal: 1,
		Kind: accounting.RecoveryActionOrder, AccountScope: scope, OwnerGeneration: oldGeneration.ID(), LeaseID: 1,
		OrderIntentHash: intentHash, OrderIntent: &intent, ExpectedRevision: 1, State: accounting.PermitAuthorized,
	}
	if _, err := recoveryStore.CreateRecoveryPermit(ctx, oldGeneration, permit); err != nil {
		t.Fatalf("create one-shot permit: %v", err)
	}
	if _, maySubmit, err := recoveryStore.ConsumeRecoveryPermit(ctx, oldGeneration, permit); err != nil || !maySubmit {
		t.Fatalf("consume original one-shot permit: maySubmit=%t err=%v", maySubmit, err)
	}
	newGeneration, err := generationStore.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}

	venue := &productionRecoveryLookupVenue{order: &exchange.Order{
		OrderID: 9081, ClientOrderID: intent.ClientOrderID, Symbol: intent.Symbol, Side: exchange.SideBuy,
		Type: exchange.OrderTypeLimit, Price: 100, Quantity: 0.5, Status: exchange.OrderStatusPartiallyFilled,
	}}
	carry := strategy.NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: intent.Symbol}, venue, &productionRecoveryLookupVenue{}, nil, nil)
	openingGate := &execution.OpeningGate{}
	openingGate.Block(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration))
	carry.SetOpeningGate(openingGate)
	runtimeAdapter := &failOnceFundingCarryRuntimeStateAdapter{fundingCarryRuntimeStateAdapter: &fundingCarryRuntimeStateAdapter{
		strategyRuntimeStateAdapter: &strategyRuntimeStateAdapter{storageService: storageService, botID: episode.Bot},
		generation:                  newGeneration,
	}}
	const runtimeSchema = 8
	runtimePayload := `{"strategy":"funding_carry","futures_exchange":"binance","spot_exchange":"binance","symbol":"BTCUSDT","ownership_ready":true,"recovery_projection_schema":1}`
	if err := runtimeAdapter.SaveRuntimeState("funding_carry", runtimeSchema, runtimePayload); err != nil {
		t.Fatalf("seed retained FundingCarry runtime snapshot: %v", err)
	}
	carry.SetRuntimeStateStore(runtimeAdapter)
	strategyManager := strategy.NewStrategyManager(cfg, 0)
	strategyManager.RegisterStrategy("funding_carry", carry, 1, 0)
	bots := &BotManager{runtimes: map[string]*BotRuntime{episode.Bot: {
		BotID: episode.Bot,
		Inner: &SymbolRuntime{fundingCarryReconciliationOnly: true, StrategyManager: strategyManager},
	}}}
	productionService, err := buildProductionOrderReconciliationService(storageService.GetStorage(), bots)
	if err != nil {
		t.Fatalf("build production recovery service: %v", err)
	}
	if productionService.Accounting != nil || productionService.Gate != nil {
		t.Fatal("found evidence must not enable accounting projection or Gate release")
	}
	web.SetOrderReconciliationService(productionService)
	router := gin.New()
	router.Use(gin.Recovery())
	web.SetupRoutesWithConfig(router, cfg)
	adminSession, err := web.GetSessionManager().CreateSession("recovery-admin", "admin", "192.0.2.30", "recovery-e2e")
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}
	adminPost := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.RemoteAddr = "192.0.2.30:1234"
		request.AddCookie(&http.Cookie{Name: "session_id", Value: adminSession.SessionID})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	adminGet := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.RemoteAddr = "192.0.2.30:1234"
		request.AddCookie(&http.Cookie{Name: "session_id", Value: adminSession.SessionID})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	takeoverResponse := adminPost("/api/capital/funding-recovery/"+episode.ID+"/takeover", `{"operation_id":"e2e-takeover","expected_episode_revision":1,"audit_ref":"incident-e2e","confirmed":true}`)
	if takeoverResponse.Code != http.StatusOK {
		t.Fatalf("production takeover route failed: status=%d body=%s", takeoverResponse.Code, takeoverResponse.Body.String())
	}
	takenOver, found, err := recoveryStore.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || takenOver.OwnerGeneration != newGeneration.ID() {
		t.Fatalf("takeover route did not persist current generation: episode=%+v found=%t err=%v", takenOver, found, err)
	}
	adjudicationBody := `{"operation_id":"e2e-found","permit_id":"production-recovery-permit","expected_attempt_revision":1,"expected_episode_revision":2,"audit_ref":"incident-e2e","confirmed":true}`
	adjudicationResponse := adminPost("/api/capital/funding-recovery/"+episode.ID+"/submission-attempts/adjudicate-found", adjudicationBody)
	if adjudicationResponse.Code != http.StatusOK {
		t.Fatalf("production found-adjudication route failed: status=%d body=%s", adjudicationResponse.Code, adjudicationResponse.Body.String())
	}
	if venue.queriedSymbol != intent.Symbol || venue.queriedCID != intent.ClientOrderID {
		t.Fatalf("production strategy did not query the durable original CID: symbol=%q cid=%q", venue.queriedSymbol, venue.queriedCID)
	}
	attempt, found, err := recoveryStore.GetRecoverySubmissionAttempt(ctx, episode.ID, permit.ID)
	if err != nil || !found || attempt.State != accounting.RecoverySubmissionFound || attempt.VenueOrderID != "9081" || attempt.Revision != 2 {
		t.Fatalf("found route did not durably CAS the matched original attempt: attempt=%+v found=%t err=%v", attempt, found, err)
	}
	children, err := recoveryStore.ListRecoveryChildren(ctx, episode.ID)
	if err != nil || len(children) != 1 || children[0].State != accounting.ChildBound || children[0].ClientOrderID != intent.ClientOrderID || children[0].VenueOrderID != "9081" {
		t.Fatalf("found route did not bind the original provisional child in the same durable operation: children=%+v err=%v", children, err)
	}
	hold, held, err := recoveryStore.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" {
		t.Fatalf("found route released or lost the active hold: hold=%+v held=%t err=%v", hold, held, err)
	}
	children, err = recoveryStore.ListRecoveryChildren(ctx, episode.ID)
	if err != nil || len(children) != 1 {
		t.Fatalf("read bound child for fill reconciliation: children=%+v err=%v", children, err)
	}
	child := children[0]
	firstFill := &exchange.OrderFill{OrderID: 9081, TradeID: "90811001", Symbol: intent.Symbol, Side: exchange.SideBuy,
		Price: 100, PriceRaw: "100", Quantity: .1, QuantityRaw: "0.1", QuoteQuantityRaw: "10", CommissionKnown: false,
		TradeTime: time.Now().UnixMilli()}
	secondFill := &exchange.OrderFill{OrderID: 9081, TradeID: "90811002", Symbol: intent.Symbol, Side: exchange.SideBuy,
		Price: 100, PriceRaw: "100", Quantity: .15, QuantityRaw: "0.15", QuoteQuantityRaw: "15", Commission: .0015,
		CommissionRaw: "0.0015", CommissionKnown: true, CommissionAsset: "USDT", TradeTime: time.Now().Add(time.Millisecond).UnixMilli()}
	venue.fillBatch = &exchange.OrderFillBatch{
		OrderID: 9081, ClientOrderID: intent.ClientOrderID, Symbol: intent.Symbol, Side: exchange.SideBuy,
		TerminalStatus: exchange.OrderStatusCanceled, OrderUpdateTime: time.Now().Add(time.Millisecond).UnixMilli(), ExecutedQuantityRaw: "0.25",
		SourceRevision: "fixture-source-revision-1", FillHistoryComplete: true, Complete: true, FeesComplete: false,
		Fills: []*exchange.OrderFill{firstFill, secondFill},
	}
	fillPath := "/api/capital/funding-recovery/" + episode.ID + "/children/" + child.ID + "/fills/"
	if _, previewErr := productionService.PreviewFundingRecoveryFill(ctx, episode.ID, child.ID); previewErr != nil {
		t.Fatalf("production service fill preview failed before HTTP: %v", previewErr)
	}
	previewResponse := adminGet(fillPath + "preview")
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("production read-only fill preview failed: status=%d body=%s", previewResponse.Code, previewResponse.Body.String())
	}
	var previewEnvelope struct {
		Preview  strategy.FundingRecoveryFillPreview `json:"preview"`
		ReadOnly bool                                `json:"read_only"`
	}
	if err := json.Unmarshal(previewResponse.Body.Bytes(), &previewEnvelope); err != nil {
		t.Fatalf("decode fill preview: %v", err)
	}
	if !previewEnvelope.ReadOnly || previewEnvelope.Preview.EpisodeRevision == 0 || previewEnvelope.Preview.ChildRevision != child.Revision ||
		previewEnvelope.Preview.Batch.SourceRevision != venue.fillBatch.SourceRevision || len(previewEnvelope.Preview.NewTradeIDs) != 2 {
		t.Fatalf("preview omitted authoritative source/CAS facts: %+v", previewEnvelope)
	}
	eventsBefore, err := recoveryStore.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(eventsBefore) != 0 {
		t.Fatalf("preview performed accounting writes: events=%+v err=%v", eventsBefore, err)
	}
	invalidClientFacts := fmt.Sprintf(`{"operation_id":"e2e-fill-apply","expected_episode_revision":%d,"expected_child_revision":%d,"source_revision":%q,"audit_ref":"incident-e2e","confirmed":true,"fills":[{"trade_id":"forged"}]}`,
		previewEnvelope.Preview.EpisodeRevision, previewEnvelope.Preview.ChildRevision, previewEnvelope.Preview.Batch.SourceRevision)
	invalidApplyResponse := adminPost(fillPath+"apply", invalidClientFacts)
	if invalidApplyResponse.Code != http.StatusBadRequest {
		t.Fatalf("fill apply accepted client-supplied venue facts: status=%d body=%s", invalidApplyResponse.Code, invalidApplyResponse.Body.String())
	}
	validApply := fmt.Sprintf(`{"operation_id":"e2e-fill-apply","expected_episode_revision":%d,"expected_child_revision":%d,"source_revision":%q,"audit_ref":"incident-e2e","confirmed":true}`,
		previewEnvelope.Preview.EpisodeRevision, previewEnvelope.Preview.ChildRevision, previewEnvelope.Preview.Batch.SourceRevision)
	runtimeAdapter.failNextCAS = true
	responseLossResponse := adminPost(fillPath+"apply", validApply)
	if responseLossResponse.Code != http.StatusConflict {
		t.Fatalf("injected runtime projection loss should leave a retryable conflict: status=%d body=%s", responseLossResponse.Code, responseLossResponse.Body.String())
	}
	pendingOutbox, found, err := recoveryStore.GetRecoveryOutbox(ctx, episode.ID, "e2e-fill-apply")
	if err != nil || !found || pendingOutbox.State != accounting.RecoveryProjectionPending {
		t.Fatalf("response loss should preserve the exact pending outbox: outbox=%+v found=%v err=%v", pendingOutbox, found, err)
	}
	applyResponse := adminPost(fillPath+"apply", validApply)
	if applyResponse.Code != http.StatusOK {
		t.Fatalf("production fill apply failed: status=%d body=%s", applyResponse.Code, applyResponse.Body.String())
	}
	var applyEnvelope struct {
		Readback struct {
			Result   strategy.ApplyFundingRecoveryFillsResult `json:"result"`
			Receipt  accounting.RecoveryReceipt               `json:"receipt"`
			Outbox   accounting.RecoveryOutbox                `json:"outbox"`
			Episode  accounting.RecoveryEpisode               `json:"episode"`
			Hold     accounting.RecoveryHold                  `json:"hold"`
			Verified bool                                     `json:"verified"`
		} `json:"readback"`
	}
	if err := json.Unmarshal(applyResponse.Body.Bytes(), &applyEnvelope); err != nil {
		t.Fatalf("decode fill apply receipt: %v", err)
	}
	if !applyEnvelope.Readback.Verified || !applyEnvelope.Readback.Result.Projected || !applyEnvelope.Readback.Result.HoldRetained ||
		applyEnvelope.Readback.Receipt.OperationID != "e2e-fill-apply" || applyEnvelope.Readback.Outbox.State != accounting.RecoveryProjected ||
		applyEnvelope.Readback.Hold.State != "held" || applyEnvelope.Readback.Episode.State != accounting.RecoveryProjected ||
		!openingGate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		t.Fatalf("production apply readback or held invariant failed: %+v", applyEnvelope.Readback)
	}
	persistedEvents, err := recoveryStore.ListRecoveryEvents(ctx, episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	fillEvents := make(map[string]accounting.RecoveryEvent)
	completionMarkerFound := false
	for _, event := range persistedEvents {
		if event.Kind == accounting.RecoveryFillEvent {
			fillEvents[event.ExternalID] = event
		}
		if event.Kind == accounting.RecoveryFillBatchCompleteEvent && event.CompletenessMarker == string(accounting.RecoveryFillBatchCompleteEvent) {
			completionMarkerFound = true
		}
	}
	if len(persistedEvents) != 3 || !completionMarkerFound || fillEvents["90811001"].FillEvidence == nil ||
		fillEvents["90811001"].FillEvidence.QuantityRaw != "0.1" || fillEvents["90811001"].CursorSequence != 1 ||
		fillEvents["90811002"].FillEvidence == nil || fillEvents["90811002"].FillEvidence.QuantityRaw != "0.15" || fillEvents["90811002"].CursorSequence != 2 {
		t.Fatalf("expected two server-derived sequential cursor fill events, got %+v", persistedEvents)
	}
	duplicateResponse := adminPost(fillPath+"apply", validApply)
	if duplicateResponse.Code != http.StatusOK {
		t.Fatalf("idempotent fill apply retry failed: status=%d body=%s", duplicateResponse.Code, duplicateResponse.Body.String())
	}
	var duplicateEnvelope struct {
		Readback struct {
			Result  strategy.ApplyFundingRecoveryFillsResult `json:"result"`
			Receipt accounting.RecoveryReceipt               `json:"receipt"`
			Hold    accounting.RecoveryHold                  `json:"hold"`
		} `json:"readback"`
	}
	if err := json.Unmarshal(duplicateResponse.Body.Bytes(), &duplicateEnvelope); err != nil || !duplicateEnvelope.Readback.Result.Replayed || duplicateEnvelope.Readback.Receipt.OperationID != "e2e-fill-apply" || duplicateEnvelope.Readback.Hold.State != "held" {
		t.Fatalf("duplicate retry did not return original receipt while retaining hold: body=%s err=%v", duplicateResponse.Body.String(), err)
	}
	persistedEvents, err = recoveryStore.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(persistedEvents) != 3 {
		t.Fatalf("duplicate retry created duplicate accounting events: events=%+v err=%v", persistedEvents, err)
	}

	feePath := fillPath + "90811001/fee-adjustments/"
	feePreviewResponse := adminGet(feePath + "preview")
	if feePreviewResponse.Code != http.StatusOK {
		t.Fatalf("production manual fee preview failed: status=%d body=%s", feePreviewResponse.Code, feePreviewResponse.Body.String())
	}
	var feePreviewEnvelope struct {
		Preview strategy.FundingRecoveryFeeAdjustmentPreview `json:"preview"`
	}
	if err := json.Unmarshal(feePreviewResponse.Body.Bytes(), &feePreviewEnvelope); err != nil {
		t.Fatalf("decode fee preview: %v", err)
	}
	feePreview := feePreviewEnvelope.Preview
	if feePreview.FillEvent.FillEvidence == nil || feePreview.FillEvent.FillEvidence.CommissionKnown || feePreview.SourceRevision != venue.fillBatch.SourceRevision || !feePreview.HoldRetained {
		t.Fatalf("fee preview omitted exact unknown-fee fill/source facts: %+v", feePreview)
	}
	feeApplyBody := fmt.Sprintf(`{"operation_id":"e2e-fee-adjustment","adjustment_id":"statement-line-e2e","amount_delta_raw":"0.00042","asset":"bnb","evidence_hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_revision":%q,"expected_episode_revision":%d,"expected_child_revision":%d,"audit_ref":"statement-2026-10-13","confirmed":true}`,
		feePreview.SourceRevision, feePreview.EpisodeRevision, feePreview.ChildRevision)
	clientActorBody := strings.TrimSuffix(feeApplyBody, "}") + `,"actor":"forged-admin"}`
	clientActorResponse := adminPost(feePath+"apply", clientActorBody)
	if clientActorResponse.Code != http.StatusBadRequest {
		t.Fatalf("fee apply accepted client-supplied actor: status=%d body=%s", clientActorResponse.Code, clientActorResponse.Body.String())
	}
	runtimeAdapter.failNextCAS = true
	feeResponseLoss := adminPost(feePath+"apply", feeApplyBody)
	if feeResponseLoss.Code != http.StatusConflict {
		t.Fatalf("injected fee runtime projection loss should remain retryable: status=%d body=%s", feeResponseLoss.Code, feeResponseLoss.Body.String())
	}
	pendingFeeOutbox, found, err := recoveryStore.GetRecoveryOutbox(ctx, episode.ID, "e2e-fee-adjustment")
	if err != nil || !found || pendingFeeOutbox.State != accounting.RecoveryProjectionPending {
		t.Fatalf("fee response loss should preserve exact pending outbox: outbox=%+v found=%v err=%v", pendingFeeOutbox, found, err)
	}
	feeApplyResponse := adminPost(feePath+"apply", feeApplyBody)
	if feeApplyResponse.Code != http.StatusOK {
		t.Fatalf("production manual fee apply retry failed: status=%d body=%s", feeApplyResponse.Code, feeApplyResponse.Body.String())
	}
	var feeApplyEnvelope struct {
		Readback web.FundingRecoveryFeeAdjustmentReadback `json:"readback"`
	}
	if err := json.Unmarshal(feeApplyResponse.Body.Bytes(), &feeApplyEnvelope); err != nil {
		t.Fatalf("decode fee adjustment readback: %v", err)
	}
	if !feeApplyEnvelope.Readback.Verified || !feeApplyEnvelope.Readback.Result.Replayed ||
		feeApplyEnvelope.Readback.Receipt.Actor != "recovery-admin" || feeApplyEnvelope.Readback.Outbox.State != accounting.RecoveryProjected ||
		feeApplyEnvelope.Readback.FeeEvent.RelatedExternalID != "90811001" || feeApplyEnvelope.Readback.FeeEvent.Amount != "0.00042" ||
		feeApplyEnvelope.Readback.FeeEvent.Asset != "BNB" || feeApplyEnvelope.Readback.FeeEvent.SourceHash != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
		feeApplyEnvelope.Readback.Hold.State != "held" || !openingGate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		t.Fatalf("manual fee readback lost actor/evidence/hold invariants: %+v", feeApplyEnvelope.Readback)
	}
	duplicateFeeResponse := adminPost(feePath+"apply", feeApplyBody)
	if duplicateFeeResponse.Code != http.StatusOK {
		t.Fatalf("duplicate fee operation retry failed: status=%d body=%s", duplicateFeeResponse.Code, duplicateFeeResponse.Body.String())
	}
	persistedEvents, err = recoveryStore.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(persistedEvents) != 4 {
		t.Fatalf("manual fee adjustment or duplicate retry produced unexpected event count: events=%+v err=%v", persistedEvents, err)
	}

	currentEpisode, found, err := recoveryStore.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found {
		t.Fatalf("read current episode before opening balance: found=%t err=%v", found, err)
	}
	openingSnapshot := accounting.RecoveryOpeningBalanceSnapshot{Exchange: "binance", Market: "futures", AccountScope: scope,
		SnapshotAt: time.Now().UTC(), Assets: []accounting.RecoveryOpeningBalanceEntry{{Asset: "BTC", AmountRaw: "0.250000000000000001"}, {Asset: "USDT", AmountRaw: "100.00"}}}
	openingBody, err := json.Marshal(struct {
		OperationID             string                                    `json:"operation_id"`
		ExpectedEpisodeRevision uint64                                    `json:"expected_episode_revision"`
		AuditRef                string                                    `json:"audit_ref"`
		Snapshot                accounting.RecoveryOpeningBalanceSnapshot `json:"snapshot"`
		Confirmed               bool                                      `json:"confirmed"`
	}{OperationID: "e2e-opening-balance", ExpectedEpisodeRevision: currentEpisode.Revision,
		AuditRef: "account-statement-2026-10-13", Snapshot: openingSnapshot, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	openingPath := "/api/capital/funding-recovery/" + episode.ID + "/opening-balances"
	forgedActorBody := strings.TrimSuffix(string(openingBody), "}") + `,"actor":"forged-admin"}`
	if response := adminPost(openingPath, forgedActorBody); response.Code != http.StatusBadRequest {
		t.Fatalf("opening balance accepted client-supplied actor: status=%d body=%s", response.Code, response.Body.String())
	}
	runtimeAdapter.failNextCAS = true
	openingResponseLoss := adminPost(openingPath, string(openingBody))
	if openingResponseLoss.Code != http.StatusConflict {
		t.Fatalf("injected opening balance projection loss should remain retryable: status=%d body=%s", openingResponseLoss.Code, openingResponseLoss.Body.String())
	}
	pendingOpeningOutbox, found, err := recoveryStore.GetRecoveryOutbox(ctx, episode.ID, "e2e-opening-balance")
	if err != nil || !found || pendingOpeningOutbox.State != accounting.RecoveryProjectionPending {
		t.Fatalf("opening balance response loss did not preserve pending outbox: outbox=%+v found=%t err=%v", pendingOpeningOutbox, found, err)
	}
	openingApplyResponse := adminPost(openingPath, string(openingBody))
	if openingApplyResponse.Code != http.StatusOK {
		t.Fatalf("production opening balance apply retry failed: status=%d body=%s", openingApplyResponse.Code, openingApplyResponse.Body.String())
	}
	var openingEnvelope struct {
		Readback web.FundingRecoveryOpeningBalanceReadback `json:"readback"`
	}
	if err := json.Unmarshal(openingApplyResponse.Body.Bytes(), &openingEnvelope); err != nil {
		t.Fatalf("decode opening balance readback: %v", err)
	}
	openingReadback := openingEnvelope.Readback
	if !openingReadback.DurableReadbackVerified || !openingReadback.OperatorAttestedOnly || openingReadback.AuthoritativeSourceVerified ||
		!openingReadback.Result.Projected || !openingReadback.Result.Replayed || openingReadback.Receipt.Actor != "recovery-admin" ||
		openingReadback.Receipt.AuditRef != "account-statement-2026-10-13" || openingReadback.Outbox.State != accounting.RecoveryProjected ||
		openingReadback.Hold.State != "held" || !openingGate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		t.Fatalf("opening balance readback overstated evidence or changed hold: %+v", openingReadback)
	}
	persistedEvents, err = recoveryStore.ListRecoveryEvents(ctx, episode.ID)
	openingCount := 0
	for _, event := range persistedEvents {
		if event.Kind == accounting.RecoveryOpeningBalanceEvent {
			openingCount++
			if event.SourceHash != openingReadback.Receipt.EvidenceHash || event.OpeningBalance == nil || event.OpeningBalance.Actor != "recovery-admin" {
				t.Fatalf("opening balance fact lost canonical snapshot/actor evidence: %+v", event)
			}
		}
	}
	if err != nil || len(persistedEvents) != 6 || openingCount != 2 {
		t.Fatalf("opening balance retry duplicated or lost durable facts: events=%+v openingCount=%d err=%v", persistedEvents, openingCount, err)
	}

	openingGate.Block("manual_operator_hold")
	releasePreviewResponse := adminGet("/api/capital/funding-recovery/" + episode.ID + "/release/preview")
	if releasePreviewResponse.Code != http.StatusOK {
		t.Fatalf("production release preview failed: status=%d body=%s", releasePreviewResponse.Code, releasePreviewResponse.Body.String())
	}
	var releasePreviewEnvelope struct {
		Preview web.FundingRecoveryReleasePreview `json:"preview"`
	}
	if err := json.Unmarshal(releasePreviewResponse.Body.Bytes(), &releasePreviewEnvelope); err != nil {
		t.Fatalf("decode release preview: %v", err)
	}
	releasePreview := releasePreviewEnvelope.Preview
	if !releasePreview.Ready || !releasePreview.OperatorClearanceRequired || releasePreview.EvidenceHash == "" || len(releasePreview.OtherGateSources) != 1 || releasePreview.OtherGateSources[0] != "manual_operator_hold" {
		t.Fatalf("release preview omitted evidence or unrelated Gate source: %+v", releasePreview)
	}
	releasePath := "/api/capital/funding-recovery/" + episode.ID + "/release"
	releaseBody, err := json.Marshal(struct {
		OperationID                string `json:"operation_id"`
		AuditRef                   string `json:"audit_ref"`
		EvidenceHash               string `json:"evidence_hash"`
		ExposureClearanceHash      string `json:"exposure_clearance_hash"`
		ProjectionHash             string `json:"projection_hash"`
		RuntimeRevision            string `json:"runtime_revision"`
		OwnerGeneration            string `json:"owner_generation"`
		ExpectedEpisodeRevision    uint64 `json:"expected_episode_revision"`
		RecoveryGeneration         uint64 `json:"recovery_generation"`
		ExpectedHoldRevision       uint64 `json:"expected_hold_revision"`
		Confirmed                  bool   `json:"confirmed"`
		ExposureClearanceConfirmed bool   `json:"exposure_clearance_confirmed"`
	}{OperationID: "e2e-manual-release", AuditRef: "incident-e2e-release", EvidenceHash: releasePreview.EvidenceHash,
		ExposureClearanceHash: strings.Repeat("b", 64), ExposureClearanceConfirmed: true,
		ProjectionHash: releasePreview.ProjectionHash, RuntimeRevision: releasePreview.RuntimeRevision, OwnerGeneration: releasePreview.OwnerGeneration,
		ExpectedEpisodeRevision: releasePreview.ExpectedEpisodeRevision, RecoveryGeneration: releasePreview.RecoveryGeneration,
		ExpectedHoldRevision: releasePreview.ExpectedHoldRevision, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	wrongEvidence := strings.Replace(string(releaseBody), releasePreview.EvidenceHash, strings.Repeat("0", 64), 1)
	if response := adminPost(releasePath, wrongEvidence); response.Code != http.StatusConflict {
		t.Fatalf("release accepted a stale/forged evidence digest: status=%d body=%s", response.Code, response.Body.String())
	}
	missingClearance := strings.Replace(string(releaseBody), `"exposure_clearance_confirmed":true`, `"exposure_clearance_confirmed":false`, 1)
	if response := adminPost(releasePath, missingClearance); response.Code != http.StatusConflict {
		t.Fatalf("release accepted without explicit position/liability clearance: status=%d body=%s", response.Code, response.Body.String())
	}
	releaseResponse := adminPost(releasePath, string(releaseBody))
	if releaseResponse.Code != http.StatusOK {
		t.Fatalf("production manual release failed: status=%d body=%s", releaseResponse.Code, releaseResponse.Body.String())
	}
	var releaseEnvelope struct {
		Readback web.FundingRecoveryReleaseReadback `json:"readback"`
	}
	if err := json.Unmarshal(releaseResponse.Body.Bytes(), &releaseEnvelope); err != nil {
		t.Fatalf("decode release readback: %v", err)
	}
	if !releaseEnvelope.Readback.Verified || releaseEnvelope.Readback.ExecutionReplayed || releaseEnvelope.Readback.Episode.State != accounting.RecoveryReleased ||
		releaseEnvelope.Readback.Hold.State != "released" || containsFundingRecoveryGateSource(releaseEnvelope.Readback.GateSources, releasePreview.RecoveryGateSource) ||
		releaseEnvelope.Readback.Receipt.ExposureClearanceHash != strings.Repeat("b", 64) ||
		!openingGate.HasBlock("manual_operator_hold") || openingGate.HasBlock(releasePreview.RecoveryGateSource) {
		t.Fatalf("manual release readback cleared an unrelated Gate or failed to retain the operator hold: %+v sources=%v", releaseEnvelope.Readback, openingGate.Sources())
	}
	duplicateReleaseResponse := adminPost(releasePath, string(releaseBody))
	if duplicateReleaseResponse.Code != http.StatusOK {
		t.Fatalf("idempotent release retry failed: status=%d body=%s", duplicateReleaseResponse.Code, duplicateReleaseResponse.Body.String())
	}
	if !openingGate.HasBlock("manual_operator_hold") {
		t.Fatal("idempotent release retry removed the unrelated manual Gate source")
	}
}

type retainedStartupRecoveryVenue struct {
	*constructorRecoveryVenue
	queriedSymbol string
	queriedCID    string
}

func (v *retainedStartupRecoveryVenue) GetOrderByClientOrderID(_ context.Context, symbol, clientID string) (*exchange.Order, error) {
	v.queriedSymbol, v.queriedCID = symbol, clientID
	return &exchange.Order{OrderID: 9091, ClientOrderID: clientID, Symbol: symbol, Side: exchange.SideBuy,
		Type: exchange.OrderTypeLimit, Price: 100, Quantity: 0.25, Status: exchange.OrderStatusPartiallyFilled}, nil
}

// This goes through SymbolManager.StartBot and the real FundingCarry runtime
// constructor, while all exchange calls terminate in an isolated fixture.
func TestProductionStartupRetainsDurableRecoveryRuntimeThroughBotManagerHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalSessionManager := web.SwapSessionManager(web.NewEphemeralSessionManager())
	t.Cleanup(func() {
		web.SetOrderReconciliationService(nil)
		if originalSessionManager == nil {
			originalSessionManager = web.NewEphemeralSessionManager()
		}
		web.SetSessionManager(originalSessionManager)
		gin.SetMode(gin.DebugMode)
	})

	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "isolated-startup-fixture"}}}
	cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "runtime", "startup-recovery.db")
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 16, 4
	cfg.Timing.SkipExchangeFeeOnBotStart = true
	cfg.Timing.PriceSendInterval = 100
	storageService, err := storage.NewStorageService(cfg, t.Context())
	if err != nil {
		t.Fatalf("start isolated SQLite storage: %v", err)
	}
	t.Cleanup(storageService.Stop)
	recoveryStore := storageService.GetStorage().(storage.FundingRecoveryStore)
	generationStore := storageService.GetStorage().(storage.FundingCarryRuntimeGenerationStore)

	const botID = "production-startup-recovery-bot"
	scopes, err := fundingCarryRuntimeOwnershipScopes(cfg, "binance", "BTCUSDT", true)
	if err != nil {
		t.Fatal(err)
	}
	scopeKeys := make([]string, 0, len(scopes))
	var orderScope string
	for _, scope := range scopes {
		key, keyErr := scope.Key()
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		scopeKeys = append(scopeKeys, key)
		if scope.Market == "futures" {
			orderScope = key
		}
	}
	oldGeneration, err := generationStore.ClaimFundingCarryRuntimeGeneration(t.Context(), scopeKeys)
	if err != nil {
		t.Fatal(err)
	}
	intent := accounting.RecoveryOrderIntent{SchemaVersion: 1, Exchange: "binance", Market: "futures", Symbol: "BTCUSDT", Side: "BUY",
		OrderType: "LIMIT", TimeInForce: "GTC", Price: "100", Quantity: "0.25", ClientOrderID: "startup-recovery-original-cid"}
	intentHash, err := intent.CanonicalHash()
	if err != nil {
		t.Fatal(err)
	}
	action := accounting.RecoveryAction{ID: "startup-recovery-action", Ordinal: 1, Kind: accounting.RecoveryActionOrder,
		Role: "open_futures", AccountScope: orderScope, Market: "futures", Symbol: intent.Symbol,
		OwnerGeneration: oldGeneration.ID(), OrderIntentHash: intentHash, Status: "authorized"}
	episode := accounting.RecoveryEpisode{ID: "startup-recovery-episode", Bot: botID, Strategy: "funding_carry", AccountScopes: scopeKeys,
		OwnerGeneration: oldGeneration.ID(), Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1,
		State: accounting.RecoveryPrepared, ActionPlan: []accounting.RecoveryAction{action}}
	if _, err := recoveryStore.CreateRecoveryEpisode(t.Context(), oldGeneration, episode); err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{ID: "startup-recovery-permit", EpisodeID: episode.ID, Generation: 1,
		ActionID: action.ID, ActionOrdinal: 1, Kind: accounting.RecoveryActionOrder, AccountScope: orderScope,
		OwnerGeneration: oldGeneration.ID(), LeaseID: 1, OrderIntentHash: intentHash, OrderIntent: &intent,
		ExpectedRevision: 1, State: accounting.PermitAuthorized}
	if _, err := recoveryStore.CreateRecoveryPermit(t.Context(), oldGeneration, permit); err != nil {
		t.Fatal(err)
	}
	if _, maySubmit, err := recoveryStore.ConsumeRecoveryPermit(t.Context(), oldGeneration, permit); err != nil || !maySubmit {
		t.Fatalf("consume isolated one-shot permit: maySubmit=%t err=%v", maySubmit, err)
	}

	venues := make(map[string]*retainedStartupRecoveryVenue)
	deps := fundingCarryStartupDependencies{
		checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
			return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
		},
		newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
			venue := &retainedStartupRecoveryVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}
			venues[market] = venue
			return venue, nil
		},
	}
	eventBus := event.NewEventBus(16)
	symbols := NewSymbolManager(cfg, eventBus, storageService, lock.NewNopLock(), "")
	symbols.botManager.runtimeStarter = func(ctx context.Context, base *config.Config, symbol config.SymbolConfig, bus *event.EventBus,
		service *storage.StorageService, distributed lock.DistributedLock, onStop func(string), pauses []storage.OpeningPauseHolder) (*SymbolRuntime, error) {
		return startFundingCarrySymbolRuntimeWithDependencies(ctx, base, symbol, bus, service, distributed, onStop, pauses, deps)
	}
	startCtx, cancelStart := context.WithCancel(t.Context())
	botRuntime, err := symbols.StartBot(startCtx, config.BotConfig{ID: botID, Exchange: "binance", Symbol: "BTCUSDT",
		MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
		Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}})
	if err != nil || botRuntime == nil {
		cancelStart()
		t.Fatalf("production SymbolManager.StartBot: runtime=%v err=%v", botRuntime, err)
	}
	t.Cleanup(func() {
		cancelStart()
		if botRuntime.Inner != nil && botRuntime.Inner.Stop != nil {
			botRuntime.Inner.Stop()
		}
	})
	registered, found := symbols.GetBotManager().Get(botID)
	if !found || registered != botRuntime || !registered.Inner.fundingCarryReconciliationOnly {
		t.Fatal("production start path did not register its retained reconciliation-only Bot runtime")
	}
	carry, ok := registered.Inner.StrategyManager.GetStrategy("funding_carry").(*strategy.FundingCarryStrategy)
	if !ok || carry == nil || carry.IsRunning() || registered.Inner.OpeningGate == nil || !registered.Inner.OpeningGate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		t.Fatal("durable recovery startup enabled trading or failed to retain the exact durable Gate hold")
	}
	_, newGeneration, boundBot, boundScopes, err := carry.CurrentFundingCarryRecoveryBinding()
	if err != nil || boundBot != botID || newGeneration.ID() == oldGeneration.ID() || !sameFundingRecoveryScopes(boundScopes, scopeKeys) {
		t.Fatalf("retained production strategy has wrong owner binding: bot=%q generation=%q err=%v", boundBot, newGeneration.ID(), err)
	}

	productionService, err := buildProductionOrderReconciliationService(storageService.GetStorage(), symbols.GetBotManager())
	if err != nil {
		t.Fatal(err)
	}
	if productionService.Accounting != nil || productionService.Gate != nil {
		t.Fatal("production startup composition enabled accounting or Gate release")
	}
	web.SetOrderReconciliationService(productionService)
	router := gin.New()
	router.Use(gin.Recovery())
	web.SetupRoutesWithConfig(router, cfg)
	adminSession, err := web.GetSessionManager().CreateSession("startup-recovery-admin", "admin", "192.0.2.31", "startup-recovery")
	if err != nil {
		t.Fatal(err)
	}
	adminPost := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.AddCookie(&http.Cookie{Name: "session_id", Value: adminSession.SessionID})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	takeover := adminPost("/api/capital/funding-recovery/"+episode.ID+"/takeover", `{"operation_id":"startup-takeover","expected_episode_revision":1,"audit_ref":"isolated-test","confirmed":true}`)
	if takeover.Code != http.StatusOK {
		t.Fatalf("takeover on actual started runtime: status=%d body=%s", takeover.Code, takeover.Body.String())
	}
	foundResponse := adminPost("/api/capital/funding-recovery/"+episode.ID+"/submission-attempts/adjudicate-found",
		`{"operation_id":"startup-found","permit_id":"startup-recovery-permit","expected_attempt_revision":1,"expected_episode_revision":2,"audit_ref":"isolated-test","confirmed":true}`)
	if foundResponse.Code != http.StatusOK {
		t.Fatalf("found adjudication on actual started runtime: status=%d body=%s", foundResponse.Code, foundResponse.Body.String())
	}
	if venues["futures"].queriedSymbol != intent.Symbol || venues["futures"].queriedCID != intent.ClientOrderID {
		t.Fatal("HTTP adjudicator did not query the original venue order identity")
	}
	for market, venue := range venues {
		if venue.mutations.Load() != 0 {
			t.Fatalf("startup recovery called a mutating %s venue RPC", market)
		}
	}
	hold, held, err := recoveryStore.GetRecoveryHold(t.Context(), episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" || !registered.Inner.OpeningGate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		t.Fatalf("HTTP adjudication released or removed the durable hold: hold=%+v held=%t err=%v", hold, held, err)
	}
}
