package web

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

func TestValidatePnLDiagnosisTradeRequiresScopedFiniteSameAssetLedger(t *testing.T) {
	valid := &storage.Trade{
		Exchange: "Binance", AccountScope: "scope-a", MarketType: "FUTURES", PnLAsset: "usdt",
		FeeAsset: "USDT", Symbol: "BTCUSDT", PnL: 5, Fee: 1, Quantity: 2,
	}
	if got, err := validatePnLDiagnosisTrade(valid, "binance", "scope-a", "futures", "USDT"); err != nil || got != 4 {
		t.Fatalf("valid net PnL=%v err=%v, want 4", got, err)
	}
	tests := []struct {
		name   string
		mutate func(*storage.Trade)
	}{
		{name: "missing row", mutate: nil},
		{name: "wrong scope", mutate: func(trade *storage.Trade) { trade.AccountScope = "scope-b" }},
		{name: "wrong exchange", mutate: func(trade *storage.Trade) { trade.Exchange = "okx" }},
		{name: "non-finite pnl", mutate: func(trade *storage.Trade) { trade.PnL = math.NaN() }},
		{name: "fee asset mismatch", mutate: func(trade *storage.Trade) { trade.FeeAsset = "BNB" }},
		{name: "negative quantity", mutate: func(trade *storage.Trade) { trade.Quantity = -1 }},
		{name: "net pnl overflow", mutate: func(trade *storage.Trade) { trade.PnL = math.MaxFloat64; trade.Fee = -math.MaxFloat64 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var trade *storage.Trade
			if tt.mutate != nil {
				copy := *valid
				tt.mutate(&copy)
				trade = &copy
			}
			if _, err := validatePnLDiagnosisTrade(trade, "binance", "scope-a", "futures", "USDT"); err == nil {
				t.Fatal("invalid diagnostic trade was accepted")
			}
		})
	}
}

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
	const apiKey = "pnl-scope-test-key"
	const secretKey = "pnl-scope-test-secret"
	fcm := NewFileConfigManager("")
	cfg := config.CreateMinimalConfig()
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: apiKey, SecretKey: secretKey}}
	cfg.App.CurrentExchange = "binance"
	if err := fcm.SetRuntimeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	originalConfigManager := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(originalConfigManager) })
	accountScope := accountScopeForExchange("binance")
	for _, trade := range []storage.Trade{
		{Account: accountID, AccountScope: accountScope, Exchange: "binance", MarketType: "spot", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 1, PnL: 10, CreatedAt: now},
		{Account: accountID, AccountScope: accountScope, Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 2, PnL: 20, CreatedAt: now},
		{Account: accountID, AccountScope: accountScope, Exchange: "binance", MarketType: "futures", PnLAsset: "USDC", FeeAsset: "USDC", Symbol: "BTCUSDC", Quantity: 2, PnL: 200, CreatedAt: now},
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
	if result.Exchange != "binance" || result.MarketType != "spot" || result.PnLAsset != "USDT" || result.TotalPnL != 10 || result.TotalTrades != 1 {
		t.Fatalf("exchange/market-scoped PnL response includes unrelated ledger: %+v", result)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/statistics/pnl/symbol?symbol=BTCUSDC&exchange=binance&market_type=futures&pnl_asset=USDC", nil)
	response = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(response)
	ctx.Request = request
	getPnLBySymbol(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("explicit asset-scoped query status=%d body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.PnLAsset != "USDC" || result.TotalPnL != 200 {
		t.Fatalf("explicit asset query mixed or lost ledgers: %+v", result)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/statistics/pnl/diagnosis?symbol=BTCUSDT&exchange=binance&market_type=futures&pnl_asset=USDT", nil)
	response = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(response)
	ctx.Request = request
	getExchangePnLDiagnosis(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("scoped diagnosis status=%d body=%s", response.Code, response.Body.String())
	}
	var diagnosis map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &diagnosis); err != nil {
		t.Fatal(err)
	}
	comparison, ok := diagnosis["pnl_comparison"].(map[string]any)
	if !ok || comparison["exchange_pnl"] != nil || comparison["discrepancy"] != nil || comparison["grid_pnl"] != float64(20) {
		t.Fatalf("diagnosis exposed unverified exchange PnL or wrong scoped grid result: %+v", diagnosis)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/statistics/anomalous-trades?symbol=BTCUSDT&exchange=binance&market_type=futures&pnl_asset=USDT", nil)
	response = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(response)
	ctx.Request = request
	getAnomalousTrades(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("scoped anomalous-trades query status=%d body=%s", response.Code, response.Body.String())
	}
}
