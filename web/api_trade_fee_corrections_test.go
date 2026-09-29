package web

import (
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
		{CorrectionID: "scoped-correction", BotID: "bot-a", Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", AccountScope: "secret-scope-digest", Account: "private-account", OrderID: 42, Leg: "open", Side: "BUY", Fee: 0.1, FeeAsset: "USDT", Reason: "trade rows unavailable", CreatedAt: time.Now().UTC()},
		{CorrectionID: "other-bot-correction", BotID: "bot-b", Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", AccountScope: "secret-scope-digest", OrderID: 43, Leg: "close", Side: "SELL", Fee: 0.2, FeeAsset: "USDT", Reason: "other owner", CreatedAt: time.Now().UTC()},
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
		!strings.Contains(body, `"resolution_supported":false`) {
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
