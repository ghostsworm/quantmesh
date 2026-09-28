package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

func TestPnLBySymbolRequiresExplicitExchangeAndMarketWhenAmbiguous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "pnl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	now := time.Now().UTC()
	accountID := GetCurrentAccountID()
	if accountID == "" {
		accountID = "default"
	}
	for _, trade := range []storage.Trade{
		{Account: accountID, MarketType: "spot", Symbol: "BTCUSDT", Quantity: 1, PnL: 10, CreatedAt: now},
		{Account: accountID, MarketType: "futures", Symbol: "BTCUSDT", Quantity: 2, PnL: 20, CreatedAt: now},
	} {
		if err := st.SaveTrade(&trade); err != nil {
			t.Fatal(err)
		}
	}
	originalStorage := storageServiceProvider
	SetStorageServiceProvider(&testStorageProvider{st: st})
	t.Cleanup(func() { SetStorageServiceProvider(originalStorage) })

	request := httptest.NewRequest(http.MethodGet, "/api/statistics/pnl/symbol?symbol=BTCUSDT", nil)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	getPnLBySymbol(ctx)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unscoped ambiguous query status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/statistics/pnl/symbol?symbol=BTCUSDT&market_type=spot", nil)
	response = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(response)
	ctx.Request = request
	getPnLBySymbol(ctx)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("market-only ambiguous query status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/statistics/pnl/symbol?symbol=BTCUSDT&exchange=binance&market_type=spot", nil)
	response = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(response)
	ctx.Request = request
	getPnLBySymbol(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("scoped query status=%d body=%s", response.Code, response.Body.String())
	}
	var result PnLSummaryResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Exchange != "binance" || result.MarketType != "spot" || result.TotalPnL != 10 || result.TotalTrades != 1 {
		t.Fatalf("exchange/market-scoped PnL response includes unrelated ledger: %+v", result)
	}
}
