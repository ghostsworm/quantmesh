package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

func TestManualTransferReceiptRequiresVerifiableID(t *testing.T) {
	transferErr := errors.New("transfer outcome unknown")
	tests := []struct {
		name        string
		transferID  string
		transferErr error
		wantErr     bool
	}{
		{name: "valid receipt", transferID: "transfer-123"},
		{name: "empty receipt", wantErr: true},
		{name: "whitespace receipt", transferID: " \t", wantErr: true},
		{name: "exchange error", transferErr: transferErr, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateManualTransferReceipt(tt.transferID, tt.transferErr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateManualTransferReceipt() error=%v, wantErr=%v", err, tt.wantErr)
			}
			if tt.transferErr != nil && !errors.Is(err, tt.transferErr) {
				t.Fatalf("exchange error identity not preserved: %v", err)
			}
		})
	}
}

func TestProfitReadEndpointsReportUnavailableStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalStorage := storageServiceProvider
	t.Cleanup(func() { SetStorageServiceProvider(originalStorage) })

	providers := []struct {
		name     string
		provider StorageServiceProvider
	}{
		{name: "provider missing"},
		{name: "storage missing", provider: &testStorageProvider{st: nil}},
	}
	endpoints := []struct {
		name    string
		target  string
		params  gin.Params
		handler func(*gin.Context)
	}{
		{name: "summary", target: "/api/profit/summary", handler: getProfitSummaryHandler},
		{name: "funding history", target: "/api/profit/funding", handler: getFundingHistoryHandler},
		{name: "strategy profits", target: "/api/profit/by-strategy", handler: getStrategyProfitsHandler},
		{name: "strategy detail", target: "/api/profit/by-strategy/BTCUSDT", params: gin.Params{{Key: "id", Value: "BTCUSDT"}}, handler: getStrategyProfitDetailHandler},
		{name: "withdraw rules", target: "/api/profit/withdraw-rules", handler: getWithdrawRulesHandler},
		{name: "withdraw history", target: "/api/profit/history", handler: getWithdrawHistoryHandler},
		{name: "profit trend", target: "/api/profit/trend", handler: getProfitTrendHandler},
	}
	for _, endpoint := range endpoints {
		for _, provider := range providers {
			t.Run(endpoint.name+"/"+provider.name, func(t *testing.T) {
				SetStorageServiceProvider(provider.provider)
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodGet, endpoint.target, nil)
				c.Params = endpoint.params
				endpoint.handler(c)
				if w.Code != http.StatusServiceUnavailable {
					t.Fatalf("status=%d, want %d; body=%s", w.Code, http.StatusServiceUnavailable, w.Body.String())
				}
				var response map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response["success"] != false {
					t.Fatalf("unavailable financial data must not report success: %v", response)
				}
			})
		}
	}
}

func TestWithdrawProfitLedgerSeparatesCompletedAndReservedAmounts(t *testing.T) {
	ledger, err := aggregateWithdrawProfitLedger([]*storage.ProfitWithdrawRecord{
		{ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 100, Currency: "USDT", Status: "completed"},
		{ExchangeID: "BINANCE", StrategyID: "btcusdt", Amount: 30, Currency: "USDT", Status: "pending"},
		{ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 20, Currency: "USDT", Status: "processing"},
		{ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 40, Currency: "USDT", Status: "future_state"},
		{ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 15, Currency: "USDT", Status: "failed"},
		{ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 5, Currency: "USDT", Status: "cancelled"},
		{ExchangeID: "gate", StrategyID: "BTCUSDT", Amount: 50, Currency: "USDT", Status: "completed"},
		{ExchangeID: "binance", StrategyID: "ETHUSDT", Amount: 25, Currency: "USDT", Status: "completed"},
		{ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 7, Currency: "BTC", Status: "completed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	stream := ledger.amountsFor("Binance", "BTCUSDT")
	if stream.withdrawn != 107 || stream.reserved != 90 {
		t.Fatalf("unexpected BTC withdrawal totals: %+v", stream)
	}
	allExchanges := ledger.amountsFor("", "BTCUSDT")
	if allExchanges.withdrawn != 157 || allExchanges.reserved != 90 {
		t.Fatalf("unexpected all-exchange totals: %+v", allExchanges)
	}
	usdtAmounts := ledger.amountsForAsset("", "BTCUSDT", "USDT")
	if usdtAmounts.withdrawn != 150 || usdtAmounts.reserved != 90 {
		t.Fatalf("non-USDT withdrawal leaked into USDT totals: %+v", usdtAmounts)
	}
	verified := verifiedWithdrawProfit{
		newWithdrawProfitStream("binance", "BTCUSDT"): 60,
		newWithdrawProfitStream("gate", "BTCUSDT"):    50,
		newWithdrawProfitStream("binance", "ETHUSDT"): 25,
	}
	if got := sumVerifiedWithdrawProfit(verified, "Binance", "BTCUSDT"); got != 60 {
		t.Fatalf("verified Binance BTC profit=%v, want 60", got)
	}
	if got := sumVerifiedWithdrawProfit(verified, "", "BTCUSDT"); got != 110 {
		t.Fatalf("verified all-exchange BTC profit=%v, want 110", got)
	}
	if got := sumVerifiedWithdrawProfit(verifiedWithdrawProfit{newWithdrawProfitStream("binance", "BTCUSDT"): math.Inf(1)}, "", ""); got != 0 {
		t.Fatalf("non-finite verified amount was returned: %v", got)
	}
}

func TestWithdrawProfitLedgerRejectsUnverifiableAmountsAndRecords(t *testing.T) {
	for _, records := range [][]*storage.ProfitWithdrawRecord{
		{nil},
		{{ID: "invalid-amount", Amount: -1, Status: "pending"}},
		{{ID: "nan-amount", Amount: math.NaN(), Status: "completed"}},
		{{ID: "missing-currency", Amount: 1, Status: "completed"}},
		{
			{ID: "large-amount-a", Amount: math.MaxFloat64, Currency: "USDT", Status: "completed"},
			{ID: "large-amount-b", Amount: math.MaxFloat64, Currency: "USDT", Status: "completed"},
		},
	} {
		if _, err := aggregateWithdrawProfitLedger(records); err == nil {
			t.Fatalf("ledger accepted unverifiable records: %+v", records)
		}
	}
}

func TestWithdrawDetailReadsScopedLedgerAndCancelFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := storage.NewSQLStorage(t.TempDir() + "/withdraw-detail.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	originalStorage := storageServiceProvider
	SetStorageServiceProvider(&testStorageProvider{st: st})
	t.Cleanup(func() { SetStorageServiceProvider(originalStorage) })

	accountID := GetCurrentAccountID()
	if accountID == "" {
		accountID = "default"
	}
	createdAt := time.Date(2026, 9, 28, 10, 30, 0, 0, time.UTC)
	if err := st.SaveWithdrawRecord(&storage.ProfitWithdrawRecord{
		ID: "pending-real-record", AccountID: accountID, ExchangeID: "binance", StrategyID: "BTCUSDT",
		Amount: 12.5, Fee: 0.1, NetAmount: 12.4, Currency: "USDT", Type: "manual", Status: "pending",
		Destination: "account", TransferID: "", CreatedAt: createdAt, FailedReason: "transfer outcome unknown",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveWithdrawRecord(&storage.ProfitWithdrawRecord{
		ID: "foreign-record", AccountID: "another-account", ExchangeID: "binance", StrategyID: "ETHUSDT",
		Amount: 5000, Currency: "USDT", Type: "manual", Status: "completed", CreatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/profit/withdraw/pending-real-record", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "pending-real-record"}}
	getWithdrawDetailHandler(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Record WithdrawRecord `json:"record"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Record.ID != "pending-real-record" || payload.Record.Amount != 12.5 || payload.Record.Status != "pending" || payload.Record.FailedReason != "transfer outcome unknown" {
		t.Fatalf("detail did not reflect persisted record: %+v", payload.Record)
	}

	response = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/profit/withdraw/foreign-record", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "foreign-record"}}
	getWithdrawDetailHandler(ctx)
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign detail status=%d body=%s, want not found", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/profit/withdraw/pending-real-record/cancel", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "pending-real-record"}}
	cancelWithdrawHandler(ctx)
	if response.Code != http.StatusConflict {
		t.Fatalf("cancel status=%d body=%s, want conflict", response.Code, response.Body.String())
	}
	record, err := st.GetWithdrawRecord(accountID, "pending-real-record")
	if err != nil || record.Status != "pending" {
		t.Fatalf("cancel changed ambiguous transfer state: record=%+v err=%v", record, err)
	}
}

type ambiguousWithdrawExchange struct {
	exchange.IExchange
	transferCalled *bool
}

func (e ambiguousWithdrawExchange) InternalTransfer(context.Context, string, string, string, float64) (string, error) {
	*e.transferCalled = true
	return "", context.DeadlineExceeded
}

func TestManualWithdrawRejectsUncoveredProfitBeforeTransfer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dbPath := t.TempDir() + "/withdraw.db"
	defer os.Remove(dbPath)
	st, err := storage.NewSQLStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	originalStorage := storageServiceProvider
	originalGetter := exchangeGetterFunc
	originalConfigManager := fileConfigManager
	SetStorageServiceProvider(&testStorageProvider{st: st})
	transferCalled := false
	SetExchangeGetter(func(string) exchange.IExchange { return ambiguousWithdrawExchange{transferCalled: &transferCalled} })
	fcm := NewFileConfigManager("")
	testCfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "withdraw-test-key", SecretKey: "withdraw-test-secret"}}}
	testCfg.App.CurrentExchange = "binance"
	fcm.SetRuntimeConfig(testCfg)
	SetFileConfigManager(fcm)
	t.Cleanup(func() {
		SetStorageServiceProvider(originalStorage)
		SetExchangeGetter(originalGetter)
		SetFileConfigManager(originalConfigManager)
	})

	request := httptest.NewRequest(http.MethodPost, "/api/profit/withdraw", bytes.NewBufferString(`{"exchangeId":"binance","strategyId":"BTCUSDT","amount":25,"currency":"USDT","destination":"account"}`))
	request.Header.Set("Content-Type", "application/json")
	expectedAccount := GetCurrentAccountID()
	if expectedAccount == "" {
		expectedAccount = "default"
	}
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	withdrawProfitHandler(ctx)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if transferCalled {
		t.Fatal("transfer was called despite missing realized-profit coverage")
	}
	records, err := st.GetWithdrawRecords(expectedAccount, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("unverified request created a withdrawal reservation: %+v", records)
	}
}

func TestManualWithdrawWindowUsesVerifiedFillAndFundingCoverage(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/manual-window.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC().Add(-time.Minute)
	const scope = "scope-manual"
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", scope, start, end); err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceOrderFillCoverage("binance", "futures", "BTCUSDT", scope, start, end); err != nil {
		t.Fatal(err)
	}
	pnl := 100.0
	fill := storage.OrderFill{Exchange: "binance", MarketType: "futures", AccountScope: scope, Symbol: "BTCUSDT", TradeID: "manual-profit-fill",
		OrderID: 77, Side: "SELL", Price: 100, Quantity: 1, Commission: 2, CommissionAsset: "USDT", RealizedPnL: &pnl, RealizedPnLAsset: "USDT", TradeTime: end.Add(-time.Minute)}
	if err := st.SaveOrderFill(&fill); err != nil {
		t.Fatal(err)
	}
	windowStart, windowEnd, checkpoint, verified, err := manualWithdrawWindow(st, "acct", scope, "binance", "BTCUSDT", 50, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint != "" || !windowStart.Equal(start) || !windowEnd.Equal(end) || verified != 98 {
		t.Fatalf("manual window=(%v,%v] checkpoint=%q verified=%v", windowStart, windowEnd, checkpoint, verified)
	}
	if _, _, _, _, err := manualWithdrawWindow(st, "acct", scope, "binance", "BTCUSDT", 99, time.Now().UTC()); err == nil {
		t.Fatal("manual amount above verified realized net profit must be rejected")
	}
}

func TestVerifiedWithdrawProfitUsesFillCoverageAndBlocksUnresolvedTransfers(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/verified-available.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	originalConfigManager := fileConfigManager
	fcm := NewFileConfigManager("")
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "verified-available-api-key", SecretKey: "test-secret"}}}
	cfg.App.CurrentExchange = "binance"
	fcm.SetRuntimeConfig(cfg)
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(originalConfigManager) })

	accountID := GetCurrentAccountID()
	scope := accountScopeForExchange("binance")
	now := time.Now().UTC()
	coveredFrom, coveredThrough := now.Add(-10*time.Minute), now.Add(time.Minute)
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", scope, coveredFrom, coveredThrough); err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceOrderFillCoverage("binance", "futures", "BTCUSDT", scope, coveredFrom, coveredThrough); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveTrade(&storage.Trade{Account: accountID, AccountScope: scope, Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", Symbol: "BTCUSDT",
		Quantity: 1, PnL: 100, Fee: 2, FeeAsset: "USDT", CreatedAt: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	realized := 100.0
	if err := st.SaveOrderFill(&storage.OrderFill{Exchange: "binance", Account: accountID, AccountScope: scope, MarketType: "futures", Symbol: "BTCUSDT",
		TradeID: "verified-available-fill", OrderID: 9, Side: "SELL", Price: 100, Quantity: 1, Commission: 2, CommissionAsset: "USDT",
		RealizedPnL: &realized, RealizedPnLAsset: "USDT", TradeTime: now.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}

	available, err := readVerifiedWithdrawProfit(st, accountID, "binance", now)
	if err != nil {
		t.Fatal(err)
	}
	if got := available[newWithdrawProfitStream("binance", "BTCUSDT")]; got != 98 {
		t.Fatalf("verified available profit=%v, want 98 net USDT", got)
	}

	if err := st.SaveWithdrawRecord(&storage.ProfitWithdrawRecord{ID: "unresolved-transfer", AccountID: accountID, AccountScope: scope,
		ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 20, Currency: "USDT", Type: "manual", Status: "pending", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	available, err = readVerifiedWithdrawProfit(st, accountID, "binance", now)
	if err != nil {
		t.Fatal(err)
	}
	if got := sumVerifiedWithdrawProfit(available, "binance", "BTCUSDT"); got != 0 {
		t.Fatalf("unresolved transfer left profit withdrawable: %v", got)
	}
}

func TestManualWithdrawWindowRejectsLegacyWithdrawalWithoutCompletionTime(t *testing.T) {
	st, err := storage.NewSQLStorage(t.TempDir() + "/legacy-manual-window.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	start := time.Now().UTC().Add(-time.Hour)
	end := time.Now().UTC().Add(-time.Minute)
	const scope = "scope-legacy-manual"
	if err := st.MarkFundingIncomeCoverage("binance", "BTCUSDT", "futures", scope, start, end); err != nil {
		t.Fatal(err)
	}
	if err := st.AdvanceOrderFillCoverage("binance", "futures", "BTCUSDT", scope, start, end); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveWithdrawRecord(&storage.ProfitWithdrawRecord{
		ID: "legacy-eth-withdrawal", AccountID: "acct", ExchangeID: "binance", StrategyID: "ETHUSDT",
		Amount: 10, Currency: "USDT", Type: "manual", Status: "completed", CreatedAt: start.Add(5 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := manualWithdrawWindow(st, "acct", scope, "binance", "BTCUSDT", 1, time.Now().UTC()); err == nil {
		t.Fatal("legacy completed withdrawal without a persisted completion time must block manual transfer")
	}
}

func TestReconcileWithdrawRecordRequiresScopedLedgerEvidenceAndReleasesClaim(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := storage.NewSQLStorage(t.TempDir() + "/reconcile-withdraw.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	originalStorage := storageServiceProvider
	originalConfigManager := fileConfigManager
	SetStorageServiceProvider(&testStorageProvider{st: st})
	fcm := NewFileConfigManager("")
	testCfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "reconcile-test-key", SecretKey: "reconcile-test-secret"}}}
	testCfg.App.CurrentExchange = "binance"
	fcm.SetRuntimeConfig(testCfg)
	SetFileConfigManager(fcm)
	t.Cleanup(func() {
		SetStorageServiceProvider(originalStorage)
		SetFileConfigManager(originalConfigManager)
	})

	accountID := GetCurrentAccountID()
	scope := accountScopeForExchange("binance")
	if err := st.UpsertProfitWithdrawRule(accountID, &storage.ProfitWithdrawRule{
		ID: "rule-reconcile", ExchangeID: "binance", AccountScope: scope,
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
	}); err != nil {
		t.Fatal(err)
	}
	if won, err := st.ClaimProfitWithdrawRule("rule-reconcile", "claim-reconcile"); err != nil || !won {
		t.Fatalf("claim=(%v, %v)", won, err)
	}
	if err := st.SaveWithdrawRecord(&storage.ProfitWithdrawRecord{
		ID: "record-reconcile", RuleID: "rule-reconcile", AccountID: accountID,
		AccountScope: scope, ClaimID: "claim-reconcile", ExchangeID: "binance",
		Amount: 25, NetAmount: 25, Currency: "USDT", Type: "auto", Status: "pending",
		Destination: "account", CreatedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	body := `{"outcome":"completed","reference":"exchange-ledger-transfer-42","evidence":"Checked exact account, asset, amount and transfer timestamp against the exchange ledger." ,"confirmed":true}`
	request := httptest.NewRequest(http.MethodPost, "/api/profit/withdraw/record-reconcile/reconcile", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "record-reconcile"}}
	reconcileWithdrawRecordHandler(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	record, err := st.GetWithdrawRecord(accountID, "record-reconcile")
	if err != nil || record.Status != "completed" || record.TransferID != "exchange-ledger-transfer-42" {
		t.Fatalf("record after reconciliation=%+v err=%v", record, err)
	}
	if won, err := st.ClaimProfitWithdrawRule("rule-reconcile", "claim-retry"); err != nil || !won {
		t.Fatalf("reconciled claim could not be reacquired: (%v, %v)", won, err)
	}
}

func TestUpsertWithdrawRuleRejectsRatioAboveProfitShare(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := storage.NewSQLStorage(t.TempDir() + "/rules.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	originalStorage := storageServiceProvider
	SetStorageServiceProvider(&testStorageProvider{st: st})
	t.Cleanup(func() { SetStorageServiceProvider(originalStorage) })

	request := httptest.NewRequest(http.MethodPost, "/api/profit/withdraw/rules", bytes.NewBufferString(`{"exchangeId":"binance","isEnabled":true,"triggerAmount":10,"withdrawRatio":1.5,"frequency":"immediate","destination":"account"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	upsertWithdrawRuleHandler(ctx)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	accountID := GetCurrentAccountID()
	rules, err := st.ListProfitWithdrawRules(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("invalid rule was persisted: %+v", rules)
	}
}

func TestUpsertWithdrawRuleRejectsOverlappingAccountingStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := storage.NewSQLStorage(t.TempDir() + "/duplicate-rules.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	originalStorage := storageServiceProvider
	originalConfigManager := fileConfigManager
	SetStorageServiceProvider(&testStorageProvider{st: st})
	fcm := NewFileConfigManager("")
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "duplicate-rule-test-key", SecretKey: "duplicate-rule-test-secret"}}}
	fcm.SetRuntimeConfig(cfg)
	SetFileConfigManager(fcm)
	t.Cleanup(func() {
		SetStorageServiceProvider(originalStorage)
		SetFileConfigManager(originalConfigManager)
	})
	for _, id := range []string{"rule-one", "rule-two"} {
		body, err := json.Marshal(map[string]interface{}{"id": id, "exchangeId": "binance", "strategyId": "BTCUSDT",
			"enabled": true, "triggerAmount": 10, "withdrawRatio": 0.5, "frequency": "immediate", "destination": "account"})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/profit/withdraw/rules", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(response)
		ctx.Request = request
		upsertWithdrawRuleHandler(ctx)
		want := http.StatusOK
		if id == "rule-two" {
			want = http.StatusBadRequest
		}
		if response.Code != want {
			t.Fatalf("id=%s status=%d want=%d body=%s", id, response.Code, want, response.Body.String())
		}
	}
	accountID := GetCurrentAccountID()
	rules, err := st.ListProfitWithdrawRules(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != "rule-one" {
		t.Fatalf("duplicate API write changed stored rules: %+v", rules)
	}
}
