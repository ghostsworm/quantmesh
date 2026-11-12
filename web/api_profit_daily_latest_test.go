package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"strings"

	"quantmesh/accounting/daily"
	"quantmesh/config"
	"quantmesh/exchange/accounting"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

func TestLatestClosedDailyDateRespectsTheSafetyLag(t *testing.T) {
	location, err := time.LoadLocation(daily.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	justAfter := time.Date(2026, 11, 12, 0, 6, 0, 0, location)
	if got := latestClosedDailyDate(justAfter); got != "2026-11-11" {
		t.Fatalf("closed day = %s, want 2026-11-11", got)
	}
	tooSoon := time.Date(2026, 11, 12, 0, 4, 0, 0, location)
	if got := latestClosedDailyDate(tooSoon); got != "2026-11-10" {
		t.Fatalf("day inside the safety lag = %s, want 2026-11-10", got)
	}
}

func TestDailyLatestReportsTheClosedDayWithoutChangingSummaryFlags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const exchangeName = "binance"
	exchangeConfig := config.ExchangeConfig{APIKey: "daily-latest-test-apikey", SecretKey: "daily-latest-test-secret"}
	scope := config.AccountScopeID(exchangeName, exchangeConfig)
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
	cfg.Storage.Path = t.TempDir() + "/daily-latest.db"
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 16, 4
	cfg.Exchanges = map[string]config.ExchangeConfig{exchangeName: exchangeConfig}

	previousSession := SwapSessionManager(NewEphemeralSessionManager())
	previousConfig := GetConfig()
	t.Cleanup(func() {
		SetStorageServiceProvider(nil)
		if previousSession == nil {
			previousSession = NewEphemeralSessionManager()
		}
		SetSessionManager(previousSession)
		if previousConfig == nil {
			SetFileConfigManager(nil)
			return
		}
		restored := NewFileConfigManager("")
		if err := restored.SetRuntimeConfig(previousConfig); err != nil {
			t.Errorf("restore runtime config: %v", err)
		}
		SetFileConfigManager(restored)
	})
	manager := NewFileConfigManager("")
	if err := manager.SetRuntimeConfig(cfg); err != nil {
		t.Fatalf("install runtime config: %v", err)
	}
	SetFileConfigManager(manager)
	service, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatalf("start storage: %v", err)
	}
	t.Cleanup(service.Stop)
	SetStorageServiceProvider(NewStorageServiceAdapter(service))

	router := gin.New()
	SetupRoutesWithConfig(router, cfg)
	session, err := GetSessionManager().CreateSession("daily-latest", "admin", "192.0.2.45", "test")
	if err != nil {
		t.Fatal(err)
	}
	date := latestClosedDailyDate(time.Now().UTC())
	read := func() map[string]any {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/profit/daily-reconciliation/latest?exchange_id=binance", nil)
		request.AddCookie(&http.Cookie{Name: "session_id", Value: session.SessionID})
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}

	unpublished := read()
	if unpublished["status"] != "unpublished" || unpublished["date"] != date {
		t.Fatalf("missing report = %#v", unpublished)
	}
	if _, present := unpublished["equityReconciliationComplete"]; present {
		t.Fatal("an unpublished day must not carry a verified flag")
	}

	repo := service.GetStorage().(daily.Repository)
	evidence, _, err := daily.BuildFuturesWalletEvidence(daily.FuturesEvidenceImport{
		Query:    daily.Query{Exchange: exchangeName, AccountScope: scope, Date: date},
		Revision: 1,
		Now:      time.Now().UTC(),
		Snapshot: futuresCandidateSnapshot(date),
	})
	if err != nil {
		t.Fatalf("build candidate evidence: %v", err)
	}
	if _, err := repo.ImportDailyEquityEvidence(context.Background(), evidence, 0); err != nil {
		t.Fatalf("import evidence: %v", err)
	}
	loaded, err := repo.LoadDailyEquityInputs(context.Background(), daily.Query{Exchange: exchangeName, AccountScope: scope, Date: date})
	if err != nil {
		t.Fatal(err)
	}
	result, err := daily.Evaluate(loaded, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CommitDailyEquityResult(context.Background(), daily.Query{Exchange: exchangeName, AccountScope: scope, Date: date}, result.InputDigest, result); err != nil {
		t.Fatalf("publish: %v", err)
	}

	published := read()
	if published["status"] != "published" || published["equityReconciliationComplete"] != false {
		t.Fatalf("published day must stay incomplete: %#v", published)
	}
	reasonText, _ := json.Marshal(published["reasons"])
	for _, want := range []string{daily.ReasonFuturesEvidenceCandidate, daily.ReasonWalletEvidenceMissing + ":" + daily.WalletSpot, daily.ReasonWalletEvidenceMissing + ":" + daily.WalletMargin} {
		if !strings.Contains(string(reasonText), want) {
			t.Fatalf("published reasons missing %q: %s", want, reasonText)
		}
	}
}

func futuresCandidateSnapshot(date string) accounting.Snapshot {
	start, end, err := daily.DayBounds(date)
	if err != nil {
		panic(err)
	}
	return accounting.Snapshot{
		Currency: "USDT", ObservedAt: end.Add(time.Hour),
		Wallet: accounting.Wallet{Balance: "10", From: start.Add(-time.Hour), Through: end},
	}
}
