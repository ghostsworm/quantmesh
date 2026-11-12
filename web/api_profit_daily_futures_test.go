package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quantmesh/accounting/daily"
	"quantmesh/config"
	"quantmesh/exchange/accounting"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

type futuresEvidenceStub struct {
	snapshot accounting.Snapshot
	err      error
	called   int
}

func (s *futuresEvidenceStub) reset(snapshot accounting.Snapshot, err error) {
	s.snapshot, s.err = snapshot, err
}

func (s *futuresEvidenceStub) ReadAccountEvidence(context.Context, time.Time) (accounting.Snapshot, error) {
	s.called++
	return s.snapshot, s.err
}

func TestImportDailyFuturesEvidenceStaysIncomplete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const exchangeName = "binance"
	exchangeConfig := config.ExchangeConfig{APIKey: "daily-futures-test-apikey", SecretKey: "daily-futures-test-secret"}
	scope := config.AccountScopeID(exchangeName, exchangeConfig)
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
	cfg.Storage.Path = t.TempDir() + "/futures-evidence.db"
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 16, 4
	cfg.Exchanges = map[string]config.ExchangeConfig{exchangeName: exchangeConfig}

	previous := futuresEvidenceReader
	stub := &futuresEvidenceStub{}
	futuresEvidenceReader = func(string) (accounting.Source, bool, error) {
		return stub, exchangeConfig.Testnet, nil
	}
	t.Cleanup(func() { futuresEvidenceReader = previous })

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

	location, err := time.LoadLocation(daily.Timezone)
	if err != nil {
		t.Fatal(err)
	}
	date := time.Now().In(location).AddDate(0, 0, -1).Format("2006-01-02")
	start, end, err := daily.DayBounds(date)
	if err != nil {
		t.Fatal(err)
	}
	stub.snapshot = accounting.Snapshot{
		Currency: "USDT", ObservedAt: time.Now().UTC(),
		Wallet:  accounting.Wallet{Balance: "250.5", From: start.Add(-time.Hour), Through: end},
		Entries: []accounting.Entry{{ID: "FUNDING_FEE:9", Kind: "funding", Currency: "USDT", Amount: "1.25", At: start.Add(time.Hour)}},
	}

	router := gin.New()
	SetupRoutesWithConfig(router, cfg)
	session, err := GetSessionManager().CreateSession("futures-evidence", "admin", "192.0.2.44", "test")
	if err != nil {
		t.Fatal(err)
	}
	target := "/api/profit/daily-reconciliation/futures-evidence?date=" + date + "&exchange_id=binance&account_scope=" + scope
	stub.err = errFuturesEvidenceRead
	rejected := httptest.NewRequest(http.MethodPost, target, nil)
	rejected.AddCookie(&http.Cookie{Name: "session_id", Value: session.SessionID})
	rejectedBack := httptest.NewRecorder()
	router.ServeHTTP(rejectedBack, rejected)
	if rejectedBack.Code != http.StatusBadGateway {
		t.Fatalf("first read failure status=%d body=%s", rejectedBack.Code, rejectedBack.Body.String())
	}
	nothing := httptest.NewRequest(http.MethodGet, "/api/profit/daily-reconciliation?date="+date+"&exchange_id=binance&account_scope="+scope, nil)
	nothing.AddCookie(&http.Cookie{Name: "session_id", Value: session.SessionID})
	nothingBack := httptest.NewRecorder()
	router.ServeHTTP(nothingBack, nothing)
	if nothingBack.Code != http.StatusNotFound {
		t.Fatalf("a rejected read must not publish a report: status=%d body=%s", nothingBack.Code, nothingBack.Body.String())
	}
	stub.err = nil

	request := httptest.NewRequest(http.MethodPost, target, nil)
	request.AddCookie(&http.Cookie{Name: "session_id", Value: session.SessionID})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, want := range []string{daily.ReasonFuturesEvidenceCandidate, daily.ReasonWalletEvidenceMissing + ":" + daily.WalletSpot, daily.ReasonWalletEvidenceMissing + ":" + daily.WalletMargin} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "\"equityReconciliationComplete\":true") || strings.Contains(body, "\"equityReconciliationComplete\": true") {
		t.Fatalf("futures-only import must stay incomplete: %s", body)
	}
	if stub.called != 2 {
		t.Fatalf("evidence reader called %d times before the failed re-read", stub.called)
	}

	read := httptest.NewRequest(http.MethodGet, "/api/profit/daily-reconciliation?date="+date+"&exchange_id=binance&account_scope="+scope, nil)
	read.AddCookie(&http.Cookie{Name: "session_id", Value: session.SessionID})
	readBack := httptest.NewRecorder()
	router.ServeHTTP(readBack, read)
	if readBack.Code != http.StatusOK {
		t.Fatalf("read back status=%d body=%s", readBack.Code, readBack.Body.String())
	}
	if !strings.Contains(readBack.Body.String(), daily.ReasonFuturesEvidenceCandidate) {
		t.Fatalf("published report lost the candidate-sample reason: %s", readBack.Body.String())
	}

	stub.reset(accounting.Snapshot{}, errFuturesEvidenceRead)
	failed := httptest.NewRequest(http.MethodPost, target, nil)
	failed.AddCookie(&http.Cookie{Name: "session_id", Value: session.SessionID})
	failedBack := httptest.NewRecorder()
	router.ServeHTTP(failedBack, failed)
	if failedBack.Code != http.StatusBadGateway {
		t.Fatalf("read failure status=%d body=%s", failedBack.Code, failedBack.Body.String())
	}
	afterFailure := httptest.NewRequest(http.MethodGet, "/api/profit/daily-reconciliation?date="+date+"&exchange_id=binance&account_scope="+scope, nil)
	afterFailure.AddCookie(&http.Cookie{Name: "session_id", Value: session.SessionID})
	afterFailureBack := httptest.NewRecorder()
	router.ServeHTTP(afterFailureBack, afterFailure)
	if afterFailureBack.Code != http.StatusOK || !strings.Contains(afterFailureBack.Body.String(), daily.ReasonFuturesEvidenceCandidate) {
		t.Fatalf("a failed re-read must keep the earlier report: status=%d body=%s", afterFailureBack.Code, afterFailureBack.Body.String())
	}
	if stub.called != 3 {
		t.Fatalf("evidence reader called %d times, want the rejection, the import and the failed re-read", stub.called)
	}
}

var errFuturesEvidenceRead = futuresEvidenceReadError("income interval invalid or outside reliable retention window")

type futuresEvidenceReadError string

func (e futuresEvidenceReadError) Error() string { return string(e) }
