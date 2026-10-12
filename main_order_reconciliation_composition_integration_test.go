package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/storage"
	"quantmesh/web"

	"github.com/gin-gonic/gin"
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
