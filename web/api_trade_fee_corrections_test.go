package web

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/storage"
)

func TestGetPendingTradeFeeCorrectionsIsScopedAndReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := storage.NewSQLStorage(t.TempDir() + "/fee-corrections.db")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()

	for _, correction := range []*storage.TradeFeeCorrection{
		{CorrectionID: "scoped-correction", BotID: "bot-a", Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", AccountScope: "secret-scope-digest", Account: "private-account", OrderID: 42, Leg: "open", Side: "BUY", Fee: 0.1, FeeAsset: "USDT", ExecutedQty: 0.01, Reason: "trade rows unavailable", CreatedAt: time.Now().UTC()},
		{CorrectionID: "other-bot-correction", BotID: "bot-b", Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", AccountScope: "secret-scope-digest", OrderID: 43, Leg: "close", Side: "SELL", Fee: 0.2, FeeAsset: "USDT", ExecutedQty: 0.02, Reason: "other owner", CreatedAt: time.Now().UTC()},
	} {
		if err := store.SaveTradeFeeCorrection(correction); err != nil {
			t.Fatalf("save correction: %v", err)
		}
	}

	previousStatus, previousStorageProvider := currentStatus, storageServiceProvider
	currentStatus = &SystemStatus{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", AccountScope: "secret-scope-digest"}
	storageServiceProvider = &testStorageProvider{st: store}
	t.Cleanup(func() {
		currentStatus, storageServiceProvider = previousStatus, previousStorageProvider
	})

	request := httptest.NewRequest(http.MethodGet, "/api/profit/fee-corrections?exchange=binance&symbol=BTCUSDT&market_type=futures&bot_id=bot-a", nil)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	getPendingTradeFeeCorrectionsHandler(context)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, "scoped-correction") || strings.Contains(body, "other-bot-correction") ||
		strings.Contains(body, "secret-scope-digest") || strings.Contains(body, "private-account") ||
		!strings.Contains(body, `"resolution_supported":true`) || !strings.Contains(body, `"basic_apply_eligibility":true`) {
		t.Fatalf("response is not scoped/read-only or leaked owner details: %s", body)
	}
}

func TestGetPendingTradeFeeCorrectionsRejectsRuntimeScopeMismatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousStatus := currentStatus
	currentStatus = &SystemStatus{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", AccountScope: "scope"}
	t.Cleanup(func() { currentStatus = previousStatus })

	request := httptest.NewRequest(http.MethodGet, "/api/profit/fee-corrections?exchange=binance&symbol=BTCUSDT&market_type=futures&bot_id=bot-a", nil)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	getPendingTradeFeeCorrectionsHandler(context)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReconcileTradeFeeCorrectionRequiresExplicitConfirmationAndRestartsGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := storage.NewSQLStorage(t.TempDir() + "/fee-correction-apply.db")
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	defer store.Close()
	correction := &storage.TradeFeeCorrection{
		CorrectionID: "apply-correction", BotID: "bot-a", Exchange: "binance", MarketType: "futures",
		Symbol: "BTCUSDT", AccountScope: "scope-a", Account: "account-a", OrderID: 42,
		Leg: "open", Side: "BUY", Fee: 0.1, FeeAsset: "USDT", ExecutedQty: 0.5,
		Reason: "late quote fee", CreatedAt: time.Now().UTC(),
	}
	if err := store.SaveTradeFeeCorrection(correction); err != nil {
		t.Fatalf("save correction: %v", err)
	}
	if err := store.SaveTrade(&storage.Trade{BuyOrderID: 42, SellOrderID: 43, BotID: "bot-a", Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", AccountScope: "scope-a", Account: "account-a", Symbol: "BTCUSDT", Quantity: 0.5, FeeAsset: "USDT", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("save paired trade: %v", err)
	}
	previousStatus, previousStorageProvider := currentStatus, storageServiceProvider
	currentStatus = &SystemStatus{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", AccountScope: "scope-a"}
	storageServiceProvider = &testStorageProvider{st: store}
	t.Cleanup(func() { currentStatus, storageServiceProvider = previousStatus, previousStorageProvider })

	request := httptest.NewRequest(http.MethodPost, "/api/profit/fee-corrections/apply-correction/reconcile", strings.NewReader(`{"bot_id":"bot-a","exchange":"binance","market_type":"futures","symbol":"BTCUSDT","evidence":"exchange fill ledger ref 42","confirm_apply":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Params = gin.Params{{Key: "id", Value: "apply-correction"}}
	reconcileTradeFeeCorrectionHandler(context)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"restart_required":true`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	count, err := store.CountPendingTradeFeeCorrections("binance", "futures", "BTCUSDT", "scope-a", "bot-a")
	if err != nil || count != 0 {
		t.Fatalf("pending correction count=%d err=%v", count, err)
	}
	trades, err := store.QueryTrades(time.Time{}, time.Now().Add(24*time.Hour), 10, 0)
	if err != nil || len(trades) != 1 || math.Abs(trades[0].Fee-0.1) > 1e-10 {
		t.Fatalf("applied paired trades=%+v err=%v, want fee 0.1", trades, err)
	}
}

func TestReconcileTradeFeeCorrectionRejectsUnconfirmedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/api/profit/fee-corrections/correction/reconcile", strings.NewReader(`{"bot_id":"bot-a","exchange":"binance","market_type":"futures","symbol":"BTCUSDT","evidence":"exchange fill ledger ref 42","confirm_apply":false}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = request
	context.Params = gin.Params{{Key: "id", Value: "correction"}}
	reconcileTradeFeeCorrectionHandler(context)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want confirmation rejection", response.Code, response.Body.String())
	}
}
