package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sql-driver/mysql"
	"quantmesh/accounting/daily"
	"quantmesh/config"
	"quantmesh/storage"
	"quantmesh/web"
)

// dailyCompositionClock freezes one closed natural day relative to the real
// clock so the 90-day interest history and five-minute safety lag stay valid
// whenever the acceptance suite runs.
type dailyCompositionClock struct {
	date             string
	start            time.Time
	end              time.Time
	now              time.Time
	fillAt           time.Time
	fundingAt        time.Time
	interestAt       time.Time
	coverageFrom     time.Time
	coverageThrough  time.Time
	coverageObserved time.Time
}

func newDailyCompositionClock(t *testing.T) dailyCompositionClock {
	t.Helper()
	location, err := time.LoadLocation(daily.Timezone)
	if err != nil {
		t.Fatalf("load reconciliation timezone: %v", err)
	}
	date := time.Now().In(location).AddDate(0, 0, -1).Format("2006-01-02")
	start, end, err := daily.DayBounds(date)
	if err != nil {
		t.Fatalf("day bounds for %s: %v", date, err)
	}
	now := time.Now().UTC()
	if !now.After(end.Add(daily.SafetyLag)) {
		t.Fatalf("closed day %s is inside the safety lag", date)
	}
	return dailyCompositionClock{
		date:             date,
		start:            start,
		end:              end,
		now:              now,
		fillAt:           start.Add(8 * time.Hour),
		fundingAt:        start.Add(9 * time.Hour),
		interestAt:       start.Add(10 * time.Hour),
		coverageFrom:     start.Add(-time.Hour),
		coverageThrough:  end,
		coverageObserved: now,
	}
}

func dailyCompositionQuery(exchange, scope string, clock dailyCompositionClock) daily.Query {
	return daily.Query{Exchange: exchange, AccountScope: scope, Date: clock.date}
}

func dailyCompositionFundingIdentity(exchange, scope, account, market, incomeType, symbol, asset string, transactionID int64) string {
	identity := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(exchange)), strings.TrimSpace(scope), strings.TrimSpace(account),
		strings.ToLower(strings.TrimSpace(market)), strings.ToUpper(strings.TrimSpace(incomeType)),
		strings.ToUpper(strings.TrimSpace(symbol)), strings.ToUpper(strings.TrimSpace(asset)), fmt.Sprint(transactionID),
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func dailyCompositionInterestIdentity(exchange, scope, asset string, transactionID int64) string {
	identity := strings.Join([]string{
		strings.ToLower(strings.TrimSpace(exchange)), strings.TrimSpace(scope),
		strings.ToUpper(strings.TrimSpace(asset)), fmt.Sprint(transactionID),
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(sum[:])
}

func dailyCompositionBalance(wallet, quantity, interest string, at time.Time) daily.Balance {
	return daily.Balance{Wallet: wallet, Asset: daily.Asset, Quantity: quantity, Principal: "0", Interest: interest, Price: "1", PriceAt: at}
}

func dailyCompositionSnapshot(sourceID string, at, observed time.Time, revision int64, balances []daily.Balance, positions []daily.Position) daily.Snapshot {
	return daily.Snapshot{
		SourceID:      sourceID,
		At:            at,
		ObservedAt:    observed,
		ScopeRevision: revision,
		Complete:      true,
		Balances:      balances,
		Positions:     positions,
	}
}

func dailyCompositionCoverage(clock dailyCompositionClock, source, wallet, symbol string) daily.Coverage {
	digest := sha256.Sum256([]byte(strings.Join([]string{"composition-coverage", source, wallet, symbol}, "\x00")))
	return daily.Coverage{
		Source:        source,
		Wallet:        wallet,
		Symbol:        symbol,
		From:          clock.coverageFrom,
		Through:       clock.coverageThrough,
		ObservedAt:    clock.coverageObserved,
		Complete:      true,
		Pages:         1,
		ExpectedCount: 0,
		ActualCount:   0,
		Digest:        hex.EncodeToString(digest[:]),
	}
}

// dailyCompositionFixture mirrors the golden day exercised in
// accounting/daily: +200 deposit, -50 withdrawal, -2 other fee, futures +30 PnL
// and -2 fee, +3 funding, and 1 USDT margin interest. Opening equity 1000,
// closing 1178, net external capital 150, explained profit 28, residual zero.
func dailyCompositionFixture(t *testing.T, exchange, scope string, clock dailyCompositionClock) daily.Evidence {
	t.Helper()
	const revision int64 = 7
	fundingLedgerID := "funding/" + dailyCompositionFundingIdentity(exchange, scope, "acct", "futures", "FUNDING_FEE", "BTCUSDT", daily.Asset, 111)
	interestLedgerID := "interest/" + dailyCompositionInterestIdentity(exchange, scope, daily.Asset, 222)
	return daily.Evidence{
		Scope: daily.Scope{
			Exchange:         exchange,
			AccountScope:     scope,
			Revision:         revision,
			Wallets:          []string{daily.WalletSpot, daily.WalletMargin, daily.WalletFutures},
			HistoryStart:     clock.start.AddDate(0, 0, -30),
			EffectiveFrom:    clock.start.AddDate(0, 0, -30),
			EffectiveThrough: clock.end.AddDate(0, 0, 30),
		},
		Date:     clock.date,
		Timezone: daily.Timezone,
		Method:   daily.MethodVersion,
		Opening: dailyCompositionSnapshot("composition-opening-snapshot", clock.start, clock.start.Add(10*time.Minute), revision,
			[]daily.Balance{
				dailyCompositionBalance(daily.WalletSpot, "500", "0", clock.start),
				dailyCompositionBalance(daily.WalletMargin, "400", "0", clock.start),
				dailyCompositionBalance(daily.WalletFutures, "100", "0", clock.start),
			}, nil),
		Closing: dailyCompositionSnapshot("composition-closing-snapshot", clock.end, clock.end.Add(10*time.Minute), revision,
			[]daily.Balance{
				dailyCompositionBalance(daily.WalletSpot, "648", "0", clock.end),
				dailyCompositionBalance(daily.WalletMargin, "400", "1", clock.end),
				dailyCompositionBalance(daily.WalletFutures, "131", "0", clock.end),
			}, []daily.Position{{
				Symbol: "BTCUSDT", Quantity: "0.01", EntryPrice: "1000", MarkPrice: "1000",
				Unrealized: "0", PriceSource: "composition-fixture", PriceAt: clock.end,
			}}),
		Events: []daily.Event{
			{Source: daily.SourceCashflow, SourceID: "composition-deposit-1", Wallet: daily.WalletSpot, Asset: daily.Asset, At: clock.start.Add(2 * time.Hour), Kind: daily.Deposit, Amount: "200"},
			{Source: daily.SourceCashflow, SourceID: "composition-withdrawal-1", Wallet: daily.WalletSpot, Asset: daily.Asset, At: clock.start.Add(3 * time.Hour), Kind: daily.Withdrawal, Amount: "-50"},
			{Source: daily.SourceCashflow, SourceID: "composition-other-fee-1", Wallet: daily.WalletSpot, Asset: daily.Asset, At: clock.start.Add(4 * time.Hour), Kind: daily.OtherFee, Amount: "-2"},
			{Source: daily.SourceExecution, SourceID: "composition-trade-1-pnl", Wallet: daily.WalletFutures, Asset: daily.Asset, At: clock.fillAt, Kind: daily.RealizedPnL, Amount: "30", LedgerRef: "fill/futures/BTCUSDT/trade-1/pnl"},
			{Source: daily.SourceExecution, SourceID: "composition-trade-1-fee", Wallet: daily.WalletFutures, Asset: daily.Asset, At: clock.fillAt, Kind: daily.TradingFee, Amount: "-2", LedgerRef: "fill/futures/BTCUSDT/trade-1/fee"},
			{Source: daily.SourceFunding, SourceID: "composition-funding-1", Wallet: daily.WalletFutures, Asset: daily.Asset, At: clock.fundingAt, Kind: daily.Funding, Amount: "3", LedgerRef: fundingLedgerID},
			{Source: daily.SourceInterest, SourceID: "composition-interest-1", Wallet: daily.WalletMargin, Asset: daily.Asset, At: clock.interestAt, Kind: daily.InterestAccrual, Amount: "0", InterestDelta: "1", LedgerRef: interestLedgerID},
		},
		Coverage: []daily.Coverage{
			dailyCompositionCoverage(clock, daily.SourceCashflow, daily.WalletSpot, ""),
			dailyCompositionCoverage(clock, daily.SourceCashflow, daily.WalletMargin, ""),
			dailyCompositionCoverage(clock, daily.SourceCashflow, daily.WalletFutures, ""),
			dailyCompositionCoverage(clock, daily.SourcePositions, daily.WalletFutures, ""),
			dailyCompositionCoverage(clock, daily.SourceExecution, daily.WalletSpot, ""),
			dailyCompositionCoverage(clock, daily.SourceExecution, daily.WalletMargin, ""),
			dailyCompositionCoverage(clock, daily.SourceExecution, daily.WalletFutures, ""),
			dailyCompositionCoverage(clock, daily.SourceFunding, daily.WalletFutures, ""),
		},
		FuturesSymbols: []string{"BTCUSDT"},
	}
}

func seedDailyCompositionLedger(t *testing.T, st *storage.SQLStorage, exchange, scope string, clock dailyCompositionClock) {
	t.Helper()
	realized := 30.0
	if err := st.SaveOrderFill(&storage.OrderFill{
		Exchange: exchange, MarketType: "futures", AccountScope: scope, Account: "acct", Symbol: "BTCUSDT",
		TradeID: "trade-1", OrderID: 1, Side: "BUY", Price: 1000, Quantity: 0.01, QuoteQuantity: 10,
		Commission: 2, CommissionAsset: daily.Asset, RealizedPnL: &realized, RealizedPnLAsset: daily.Asset,
		TradeTime: clock.fillAt,
	}); err != nil {
		t.Fatalf("save composition order fill: %v", err)
	}
	if err := st.SaveFundingPayment(&storage.FundingPayment{
		Exchange: exchange, Symbol: "BTCUSDT", Account: "acct", MarketType: "futures", AccountScope: scope,
		IncomeType: "FUNDING_FEE", Income: 3, Asset: daily.Asset, TransactionID: 111, TradeTime: clock.fundingAt,
	}); err != nil {
		t.Fatalf("save composition funding payment: %v", err)
	}
	if err := st.SaveMarginInterestPayment(&storage.MarginInterestPayment{
		Exchange: exchange, Account: "acct", AccountScope: scope, Asset: daily.Asset, RawAsset: daily.Asset,
		Principal: 100, Interest: 1, Rate: 0.01, InterestType: "BORROW", TransactionID: 222, AccruedAt: clock.interestAt,
		ValuationAsset: daily.Asset, ValuationRate: 1, ValuationAmount: 1, ValuationStatus: "VALUED",
		ValuationMinute: 0, ValuationSource: "composition-fixture",
	}); err != nil {
		t.Fatalf("save composition margin interest payment: %v", err)
	}
	if err := st.AdvanceOrderFillCoverage(exchange, "futures", "BTCUSDT", scope, clock.coverageFrom, clock.coverageThrough); err != nil {
		t.Fatalf("advance composition execution coverage: %v", err)
	}
	if err := st.MarkFundingIncomeCoverage(exchange, "BTCUSDT", "futures", scope, clock.coverageFrom, clock.coverageThrough); err != nil {
		t.Fatalf("mark composition funding coverage: %v", err)
	}
	if err := st.MarkMarginInterestCoverage(exchange, scope, "*", clock.coverageFrom, clock.coverageThrough); err != nil {
		t.Fatalf("mark composition interest coverage: %v", err)
	}
}

type dailyCompositionEnvelope struct {
	Success             bool         `json:"success"`
	DailyReconciliation daily.Result `json:"dailyReconciliation"`
}

func requestDailyComposition(t *testing.T, router *gin.Engine, sessionID, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func decodeDailyComposition(t *testing.T, recorder *httptest.ResponseRecorder) dailyCompositionEnvelope {
	t.Helper()
	var envelope dailyCompositionEnvelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode daily reconciliation response: %v body=%s", err, recorder.Body.String())
	}
	return envelope
}

func requireDailyCompositionComplete(t *testing.T, envelope dailyCompositionEnvelope) daily.Result {
	t.Helper()
	result := envelope.DailyReconciliation
	if !envelope.Success || !result.EquityReconciliationComplete || len(result.Reasons) != 0 {
		t.Fatalf("daily reconciliation is not complete: success=%t result=%+v", envelope.Success, result)
	}
	want := daily.Amounts{
		OpeningEquity:      "1000",
		ClosingEquity:      "1178",
		NetExternalCapital: "150",
		TradingGross:       "30",
		TradingFees:        "2",
		TradingNet:         "28",
		FundingNet:         "3",
		InterestCost:       "1",
		UnrealizedChange:   "0",
		ValuationChange:    "0",
		OtherCosts:         "2",
		EquityProfit:       "28",
		ExplainedProfit:    "28",
		Residual:           "0",
	}
	if result.Amounts != want {
		t.Fatalf("daily amounts mismatch:\n got %+v\nwant %+v", result.Amounts, want)
	}
	if result.Timezone != daily.Timezone || result.Asset != daily.Asset || result.Method != daily.MethodVersion {
		t.Fatalf("daily fixed contract fields missing: %+v", result)
	}
	return result
}

func runDailyEquityProductionComposition(t *testing.T, cfg *config.Config, openStorage func() *storage.StorageService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	previousSessionManager := web.SwapSessionManager(web.NewEphemeralSessionManager())
	previousConfig := web.GetConfig()
	t.Cleanup(func() {
		web.SetStorageServiceProvider(nil)
		if previousSessionManager == nil {
			previousSessionManager = web.NewEphemeralSessionManager()
		}
		web.SetSessionManager(previousSessionManager)
		if previousConfig == nil {
			web.SetFileConfigManager(nil)
		} else {
			restored := web.NewFileConfigManager("")
			if err := restored.SetRuntimeConfig(previousConfig); err != nil {
				t.Errorf("restore previous runtime config: %v", err)
			}
			web.SetFileConfigManager(restored)
		}
		gin.SetMode(gin.DebugMode)
	})

	// The production session middleware persists sessions relative to the
	// process working directory; isolate it beside the temporary database.
	originalWorkingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("read working directory: %v", err)
	}
	temporaryWorkingDir := t.TempDir()
	if err := os.Chdir(temporaryWorkingDir); err != nil {
		t.Fatalf("isolate working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWorkingDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	const exchange = "binance"
	exchangeConfig := config.ExchangeConfig{APIKey: "composition-daily-equity-fixture-"}
	scope := config.AccountScopeID(exchange, exchangeConfig)
	cfg.Exchanges = map[string]config.ExchangeConfig{exchange: exchangeConfig}
	cfg.Web.SharedDir = t.TempDir()
	fcm := web.NewFileConfigManager("")
	if err := fcm.SetRuntimeConfig(cfg); err != nil {
		t.Fatalf("install isolated runtime config: %v", err)
	}
	web.SetFileConfigManager(fcm)

	clock := newDailyCompositionClock(t)
	query := dailyCompositionQuery(exchange, scope, clock)
	storageService := openStorage()
	t.Cleanup(func() {
		if storageService != nil {
			storageService.Stop()
		}
	})
	web.SetStorageServiceProvider(web.NewStorageServiceAdapter(storageService))
	composeRouter := func() *gin.Engine {
		router := gin.New()
		web.SetupRoutesWithConfig(router, cfg)
		return router
	}
	router := composeRouter()
	adminSession, err := web.GetSessionManager().CreateSession("daily-composition-admin", "admin", "192.0.2.201", "daily-composition")
	if err != nil {
		t.Fatalf("create isolated admin session: %v", err)
	}
	target := func(date, candidateScope string) string {
		return fmt.Sprintf("/api/profit/daily-reconciliation?date=%s&exchange_id=%s&account_scope=%s", date, exchange, candidateScope)
	}

	if response := requestDailyComposition(t, router, adminSession.SessionID, target(clock.date, strings.Repeat("0", 64))); response.Code != http.StatusForbidden {
		t.Fatalf("unconfigured account scope must be forbidden: status=%d body=%s", response.Code, response.Body.String())
	}
	if response := requestDailyComposition(t, router, adminSession.SessionID, target(clock.date, scope)); response.Code != http.StatusNotFound {
		t.Fatalf("missing report must be 404 rather than a verified zero: status=%d body=%s", response.Code, response.Body.String())
	}

	store, ok := storageService.GetStorage().(*storage.SQLStorage)
	if !ok {
		t.Fatalf("production storage is %T, want *storage.SQLStorage", storageService.GetStorage())
	}
	seedDailyCompositionLedger(t, store, exchange, scope, clock)
	evidence := dailyCompositionFixture(t, exchange, scope, clock)
	repository := storageService.GetStorage().(daily.Repository)
	revision, err := repository.ImportDailyEquityEvidence(context.Background(), evidence, 0)
	if err != nil || revision != 1 {
		t.Fatalf("import composition evidence: revision=%d err=%v", revision, err)
	}
	// Coverage receipts are written with the storage's own clock after the
	// fixture clock was captured, so reconcile at a strictly later instant.
	published, err := daily.NewService(repository).Reconcile(context.Background(), query, time.Now().UTC())
	if err != nil {
		t.Fatalf("reconcile composition day through production service: %v", err)
	}
	if !published.EquityReconciliationComplete {
		t.Fatalf("production service did not verify the closed day: %+v", published)
	}

	response := requestDailyComposition(t, router, adminSession.SessionID, target(clock.date, scope))
	if response.Code != http.StatusOK {
		t.Fatalf("read published report through production route: status=%d body=%s", response.Code, response.Body.String())
	}
	result := requireDailyCompositionComplete(t, decodeDailyComposition(t, response))
	if result.Query != query || result.Revision != 7 || result.EvidenceRevision != 1 {
		t.Fatalf("published report identity mismatch: %+v", result)
	}

	// Restarting the real storage service must preserve the immutable report
	// and let the production route serve it without recomputation.
	storageService.Stop()
	storageService = openStorage()
	web.SetStorageServiceProvider(web.NewStorageServiceAdapter(storageService))
	router = composeRouter()
	afterRestart := requestDailyComposition(t, router, adminSession.SessionID, target(clock.date, scope))
	if afterRestart.Code != http.StatusOK {
		t.Fatalf("read persisted report after production restart: status=%d body=%s", afterRestart.Code, afterRestart.Body.String())
	}
	persisted := requireDailyCompositionComplete(t, decodeDailyComposition(t, afterRestart))
	if persisted.Revision != result.Revision || persisted.InputDigest != result.InputDigest {
		t.Fatalf("restart changed the published report: before=%+v after=%+v", result, persisted)
	}

	// A mutable ledger fact arriving after publication invalidates the old
	// verified flag on read while preserving the historical report revision.
	store, ok = storageService.GetStorage().(*storage.SQLStorage)
	if !ok {
		t.Fatalf("restarted production storage is %T, want *storage.SQLStorage", storageService.GetStorage())
	}
	latePnL := 1.0
	if err := store.SaveOrderFill(&storage.OrderFill{
		Exchange: exchange, MarketType: "futures", AccountScope: scope, Account: "acct", Symbol: "BTCUSDT",
		TradeID: "trade-2", OrderID: 2, Side: "SELL", Price: 1000, Quantity: 0.01, QuoteQuantity: 10,
		Commission: 0.5, CommissionAsset: daily.Asset, RealizedPnL: &latePnL, RealizedPnLAsset: daily.Asset,
		TradeTime: clock.start.Add(12 * time.Hour),
	}); err != nil {
		t.Fatalf("append late mutable ledger fact: %v", err)
	}
	stale := requestDailyComposition(t, router, adminSession.SessionID, target(clock.date, scope))
	if stale.Code != http.StatusOK {
		t.Fatalf("read stale report through production route: status=%d body=%s", stale.Code, stale.Body.String())
	}
	staleEnvelope := decodeDailyComposition(t, stale)
	if staleEnvelope.DailyReconciliation.EquityReconciliationComplete || staleEnvelope.DailyReconciliation.Revision != result.Revision {
		t.Fatalf("changed source facts must invalidate completion without rewriting history: %+v", staleEnvelope.DailyReconciliation)
	}
	foundStaleReason := false
	for _, reason := range staleEnvelope.DailyReconciliation.Reasons {
		if reason == "source_changed" {
			foundStaleReason = true
			break
		}
	}
	if !foundStaleReason {
		t.Fatalf("stale report missing source_changed reason: %v", staleEnvelope.DailyReconciliation.Reasons)
	}
}

func TestSQLiteDailyEquityProductionComposition(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "runtime", "daily-equity-composition.db")
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 16, 4
	open := func() *storage.StorageService {
		t.Helper()
		service, err := storage.NewStorageService(cfg, context.Background())
		if err != nil {
			t.Fatalf("start SQLite storage service and migrate daily equity schema: %v", err)
		}
		return service
	}
	runDailyEquityProductionComposition(t, cfg, open)
}

func TestMySQLDailyEquityProductionComposition(t *testing.T) {
	base := constructorMySQLConfig(t)
	admin, err := sql.Open("mysql", base.FormatDSN())
	if err != nil {
		t.Fatalf("open disposable MySQL admin connection: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Close(); err != nil {
			t.Errorf("close disposable MySQL admin connection: %v", err)
		}
	})
	schema := fmt.Sprintf("quantmesh_daily_%d", time.Now().UnixNano())
	createCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(createCtx, "CREATE DATABASE `"+schema+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("create isolated daily equity schema: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if _, err := admin.ExecContext(dropCtx, "DROP DATABASE `"+schema+"`"); err != nil {
			t.Errorf("remove isolated daily equity schema: %v", err)
		}
	})
	scoped, err := mysql.ParseDSN(base.FormatDSN())
	if err != nil {
		t.Fatalf("parse disposable MySQL DSN: %v", err)
	}
	scoped.DBName = schema
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type, cfg.Storage.Path = true, "mysql", scoped.FormatDSN()
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 16, 4
	open := func() *storage.StorageService {
		t.Helper()
		service, err := storage.NewStorageService(cfg, context.Background())
		if err != nil {
			t.Fatalf("start MySQL storage service and migrate daily equity schema: %v", err)
		}
		return service
	}
	runDailyEquityProductionComposition(t, cfg, open)
}
