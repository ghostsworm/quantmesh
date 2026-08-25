package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

func setupSetupAPITestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/setup/status", getSetupStatusHandler)
	r.POST("/api/setup/init", initSetupHandler)
	r.POST("/api/setup/exchange-symbols", getExchangeSymbolsHandler)
	return r
}

func installSetupAPITestGlobals(t *testing.T, cfg *config.Config) *FileConfigManager {
	t.Helper()
	restoreStorage := setupTestPrimaryAppConfigStorage(t)
	t.Cleanup(restoreStorage)

	origFCM := fileConfigManager
	origHotReloader := configHotReloader
	origPasswordManager := globalPasswordManager
	t.Cleanup(func() {
		SetFileConfigManager(origFCM)
		SetConfigHotReloader(origHotReloader)
		globalPasswordManager = origPasswordManager
	})

	fcm := NewFileConfigManager("")
	if cfg != nil {
		fcm.SetRuntimeConfig(cfg)
	}
	SetFileConfigManager(fcm)
	SetConfigHotReloader(nil)
	globalPasswordManager = nil
	return fcm
}

func performSetupAPIRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestGetSetupStatusHandlerReportsMissingAndCompleteConfig(t *testing.T) {
	router := setupSetupAPITestRouter()

	origFCM := fileConfigManager
	SetFileConfigManager(nil)
	t.Cleanup(func() { SetFileConfigManager(origFCM) })

	w := performSetupAPIRequest(router, http.MethodGet, "/api/setup/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("missing manager status = %d", w.Code)
	}
	var missing SetupStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &missing); err != nil {
		t.Fatalf("decode missing status: %v", err)
	}
	if !missing.NeedsSetup {
		t.Fatalf("missing manager should require setup")
	}

	cfg := config.CreateMinimalConfig()
	cfg.App.CurrentExchange = "binance"
	cfg.Exchanges["binance"] = config.ExchangeConfig{
		APIKey:    "api-key",
		SecretKey: "secret-key",
		FeeRate:   0.0002,
	}
	cfg.Trading.Symbols = []config.SymbolConfig{{
		Exchange:       "binance",
		Symbol:         "BTCUSDT",
		PriceInterval:  100,
		OrderQuantity:  0.01,
		BuyWindowSize:  2,
		SellWindowSize: 2,
	}}
	fcm := NewFileConfigManager("")
	fcm.SetRuntimeConfig(cfg)
	SetFileConfigManager(fcm)

	w = performSetupAPIRequest(router, http.MethodGet, "/api/setup/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("complete config status = %d", w.Code)
	}
	var complete SetupStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &complete); err != nil {
		t.Fatalf("decode complete status: %v", err)
	}
	if complete.NeedsSetup {
		t.Fatalf("complete config should not require setup")
	}
	if len(complete.Symbols) != 1 || complete.Symbols[0].Symbol != "BTCUSDT" {
		t.Fatalf("unexpected symbols: %+v", complete.Symbols)
	}
}

func TestInitSetupHandlerValidationAndSuccess(t *testing.T) {
	router := setupSetupAPITestRouter()
	fcm := installSetupAPITestGlobals(t, config.CreateMinimalConfig())

	tests := []struct {
		name string
		body string
	}{
		{
			name: "invalid_json",
			body: "{",
		},
		{
			name: "missing_symbols",
			body: `{"exchange":"binance","api_key":"k","secret_key":"s","price_interval":100,"order_quantity":0.01,"buy_window_size":2}`,
		},
		{
			name: "invalid_numeric_fields",
			body: `{"exchange":"binance","api_key":"k","secret_key":"s","symbols":["BTCUSDT"],"price_interval":0,"order_quantity":0.01,"buy_window_size":2}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := performSetupAPIRequest(router, http.MethodPost, "/api/setup/init", tt.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			var resp SetupInitResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.Success {
				t.Fatalf("invalid request should not succeed")
			}
		})
	}

	body := `{"exchange":"binance","api_key":"k","secret_key":"s","passphrase":"p","symbols":["BTCUSDT","ETHUSDT"],"price_interval":100,"profit_spread":125,"order_quantity":0.01,"buy_window_size":2,"testnet":true}`
	w := performSetupAPIRequest(router, http.MethodPost, "/api/setup/init", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp SetupInitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode success response: %v", err)
	}
	if !resp.Success || !resp.RequiresRestart {
		t.Fatalf("unexpected success response: %+v", resp)
	}

	cfg, err := fcm.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.App.CurrentExchange != "binance" {
		t.Fatalf("current exchange = %q", cfg.App.CurrentExchange)
	}
	exCfg := cfg.Exchanges["binance"]
	if exCfg.APIKey != "k" || exCfg.SecretKey != "s" || exCfg.Passphrase != "p" || !exCfg.Testnet {
		t.Fatalf("unexpected exchange config: %+v", exCfg)
	}
	if exCfg.FeeRate != 0.0002 {
		t.Fatalf("default fee rate = %v", exCfg.FeeRate)
	}
	if cfg.Trading.Symbol != "BTCUSDT" || len(cfg.Trading.Symbols) != 2 {
		t.Fatalf("unexpected trading symbols: symbol=%q symbols=%+v", cfg.Trading.Symbol, cfg.Trading.Symbols)
	}
	for _, symbol := range cfg.Trading.Symbols {
		if symbol.SellWindowSize != 2 || symbol.MinOrderValue != 20 {
			t.Fatalf("unexpected symbol defaults: %+v", symbol)
		}
	}
}

func TestInitSetupHandlerPreservesOtherExchangeSymbolsAndHandlesNilManager(t *testing.T) {
	router := setupSetupAPITestRouter()
	cfg := config.CreateMinimalConfig()
	cfg.Exchanges["okx"] = config.ExchangeConfig{
		APIKey:    "okx-key",
		SecretKey: "okx-secret",
		FeeRate:   0.0002,
	}
	cfg.Trading.Symbols = []config.SymbolConfig{
		{Exchange: "okx", Symbol: "SOLUSDT", PriceInterval: 10, OrderQuantity: 1, BuyWindowSize: 1, SellWindowSize: 1},
		{Exchange: "binance", Symbol: "OLDUSDT", PriceInterval: 10, OrderQuantity: 1, BuyWindowSize: 1, SellWindowSize: 1},
	}
	fcm := installSetupAPITestGlobals(t, cfg)

	body := `{"exchange":"binance","api_key":"k","secret_key":"s","symbol":"BTCUSDT","price_interval":50,"order_quantity":0.02,"min_order_value":35,"buy_window_size":3,"sell_window_size":4,"fee_rate":0.0004}`
	w := performSetupAPIRequest(router, http.MethodPost, "/api/setup/init", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	updated, err := fcm.GetConfig()
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	gotSymbols := make([]string, 0, len(updated.Trading.Symbols))
	for _, symbol := range updated.Trading.Symbols {
		gotSymbols = append(gotSymbols, symbol.Exchange+":"+symbol.Symbol)
	}
	joined := strings.Join(gotSymbols, ",")
	if joined != "okx:SOLUSDT,binance:BTCUSDT" {
		t.Fatalf("unexpected symbol replacement order: %s", joined)
	}
	if updated.Exchanges["binance"].FeeRate != 0.0004 {
		t.Fatalf("custom fee rate was not preserved")
	}

	SetFileConfigManager(nil)
	w = performSetupAPIRequest(router, http.MethodPost, "/api/setup/init", body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil manager status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestGetExchangeSymbolsHandlerValidationBranches(t *testing.T) {
	router := setupSetupAPITestRouter()

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantText   string
	}{
		{
			name:       "invalid_json",
			body:       "{",
			wantStatus: http.StatusBadRequest,
			wantText:   "请求参數錯误",
		},
		{
			name:       "unsupported_exchange",
			body:       `{"exchange":"kraken","api_key":"k","secret_key":"s"}`,
			wantStatus: http.StatusBadRequest,
			wantText:   "暫不支援",
		},
		{
			name:       "huobi_spot_unsupported",
			body:       `{"exchange":"huobi","market_type":"spot","api_key":"k","secret_key":"s"}`,
			wantStatus: http.StatusBadRequest,
			wantText:   "Huobi 現貨",
		},
		{
			name:       "kucoin_spot_unsupported",
			body:       `{"exchange":"kucoin","market_type":"spot","api_key":"k","secret_key":"s"}`,
			wantStatus: http.StatusBadRequest,
			wantText:   "KuCoin 現貨",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := performSetupAPIRequest(router, http.MethodPost, "/api/setup/exchange-symbols", tt.body)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tt.wantText) {
				t.Fatalf("body %q does not contain %q", w.Body.String(), tt.wantText)
			}
			var resp ExchangeSymbolsResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.Success || len(resp.Symbols) != 0 {
				t.Fatalf("unexpected response: %+v", resp)
			}
		})
	}
}
