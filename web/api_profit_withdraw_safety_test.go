package web

import (
	"bytes"
	"context"
	"encoding/json"
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

type ambiguousWithdrawExchange struct{ exchange.IExchange }

func (ambiguousWithdrawExchange) InternalTransfer(context.Context, string, string, string, float64) (string, error) {
	return "", context.DeadlineExceeded
}

func TestManualWithdrawAmbiguousTransferRemainsPending(t *testing.T) {
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
	SetExchangeGetter(func(string) exchange.IExchange { return ambiguousWithdrawExchange{} })
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

	request := httptest.NewRequest(http.MethodPost, "/api/profit/withdraw", bytes.NewBufferString(`{"exchangeId":"binance","amount":25,"currency":"USDT"}`))
	request.Header.Set("Content-Type", "application/json")
	expectedAccount := GetCurrentAccountID()
	if expectedAccount == "" {
		expectedAccount = "default"
	}
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	withdrawProfitHandler(ctx)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Success bool           `json:"success"`
		Record  WithdrawRecord `json:"record"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Success || payload.Record.Status != "pending" || payload.Record.FailedReason == "" {
		t.Fatalf("ambiguous transfer must be returned as pending reconciliation: %+v", payload)
	}
	records, err := st.GetWithdrawRecords(expectedAccount, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != "pending" || records[0].FailedReason == "" {
		t.Fatalf("persisted record=%+v", records)
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
		WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
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
