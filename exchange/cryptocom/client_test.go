package cryptocom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateOrderAndGetPositionsUseReduceOnlyAndSignedPositionAPI(t *testing.T) {
	var sawReduceOnly bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch request.Method {
		case "private/create-order":
			instructions, ok := request.Params["exec_inst"].([]interface{})
			if !ok || len(instructions) != 1 || instructions[0] != "REDUCE_ONLY" {
				t.Errorf("exec_inst = %#v, want [REDUCE_ONLY]", request.Params["exec_inst"])
			}
			sawReduceOnly = true
			_, _ = w.Write([]byte(`{"id":"123","code":"0","result":{"order_id":42,"client_oid":"close"}}`))
		case "private/get-positions":
			if request.Params["instrument_name"] != "BTCUSD-PERP" {
				t.Errorf("instrument_name = %#v", request.Params["instrument_name"])
			}
			_, _ = w.Write([]byte(`{"id":"124","code":"0","result":{"data":[{"instrument_name":"BTCUSD-PERP","quantity":"-0.25","cost":"12000","open_position_pnl":"-12.5"},{"instrument_name":"BTCUSD-PERP","quantity":"0","cost":"0","open_position_pnl":"0"}]}}`))
		default:
			t.Errorf("unexpected method %q", request.Method)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := NewCryptoComClient("key", "secret", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	if _, err := client.CreateOrder(context.Background(), &OrderRequest{
		InstrumentName: "BTCUSD-PERP", Side: "SELL", Type: "LIMIT", Quantity: 0.25, Price: 60000,
		ClientOID: "close", ReduceOnly: true,
	}); err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if !sawReduceOnly {
		t.Fatal("reduce-only instruction was not sent")
	}
	positions, err := client.GetPositions(context.Background(), "BTCUSD-PERP")
	if err != nil {
		t.Fatalf("GetPositions: %v", err)
	}
	if len(positions) != 2 || positions[0].Quantity != -0.25 || positions[0].OpenPositionPNL != -12.5 {
		t.Fatalf("positions = %#v", positions)
	}
}

func TestGetAccountSummaryReturnsCurrencyAndRejectsEmptyAccounts(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantErr  bool
	}{
		{name: "currency evidence", response: `{"id":"1","code":"0","result":{"accounts":[{"currency":"btc","balance":"2.5","available":"2"}]}}`},
		{name: "empty account list", response: `{"id":"1","code":"0","result":{"accounts":[]}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewCryptoComClient("key", "secret", false)
			client.baseURL = server.URL
			client.httpClient = server.Client()
			adapter := &Adapter{client: client}
			account, err := adapter.GetAccount(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected empty account list to fail")
				}
				return
			}
			if err != nil {
				t.Fatalf("GetAccountSummary: %v", err)
			}
			if account.BalanceAsset != "BTC" {
				t.Fatalf("BalanceAsset = %q, want BTC", account.BalanceAsset)
			}
		})
	}
}

func TestAccountOpenOrdersReadsAllInstrumentsAndValidatesSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		response string
		wantErr  bool
		wantLen  int
	}{
		{
			name:     "all instruments including pending order",
			response: `{"id":"1","code":"0","result":{"order_list":[{"order_id":42,"instrument_name":"BTCUSD-PERP","side":"BUY","status":"ACTIVE","quantity":"2","cumulative_quantity":"0"},{"order_id":43,"instrument_name":"ETHUSD-PERP","side":"SELL","status":"PENDING","quantity":"1","cumulative_quantity":"0.25"}]}}`,
			wantLen:  2,
		},
		{name: "empty complete snapshot", response: `{"id":"1","code":"0","result":{"order_list":[]}}`},
		{name: "missing order list", response: `{"id":"1","code":"0","result":{}}`, wantErr: true},
		{name: "null order list", response: `{"id":"1","code":"0","result":{"order_list":null}}`, wantErr: true},
		{name: "unknown status", response: `{"id":"1","code":"0","result":{"order_list":[{"order_id":42,"instrument_name":"BTCUSD-PERP","side":"BUY","status":"MYSTERY","quantity":"1","cumulative_quantity":"0"}]}}`, wantErr: true},
		{name: "duplicate order ID", response: `{"id":"1","code":"0","result":{"order_list":[{"order_id":42,"instrument_name":"BTCUSD-PERP","side":"BUY","status":"ACTIVE","quantity":"1","cumulative_quantity":"0"},{"order_id":42,"instrument_name":"ETHUSD-PERP","side":"SELL","status":"ACTIVE","quantity":"1","cumulative_quantity":"0"}]}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					Method string                 `json:"method"`
					Params map[string]interface{} `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				if request.Method != "private/get-open-orders" {
					t.Errorf("method = %q", request.Method)
				}
				if _, filtered := request.Params["instrument_name"]; filtered {
					t.Errorf("account snapshot was instrument-filtered: %#v", request.Params)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewCryptoComClient("key", "secret", false)
			client.baseURL = server.URL
			client.httpClient = server.Client()
			orders, err := (&Adapter{client: client}).GetAccountOpenOrders(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("GetAccountOpenOrders() accepted invalid snapshot")
				}
				return
			}
			if err != nil {
				t.Fatalf("GetAccountOpenOrders(): %v", err)
			}
			if len(orders) != tt.wantLen {
				t.Fatalf("len(orders) = %d, want %d", len(orders), tt.wantLen)
			}
			if tt.wantLen == 2 && (orders[0].InstrumentName != "BTCUSD-PERP" || orders[1].InstrumentName != "ETHUSD-PERP" || orders[1].ExecutedQty != 0.25) {
				t.Fatalf("orders = %#v", orders)
			}
		})
	}
}

func TestNewCryptoComClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"

	// 测試主网客戶端
	client := NewCryptoComClient(apiKey, secretKey, false)
	if client == nil {
		t.Fatal("創建主网客戶端失败")
	}
	if client.apiKey != apiKey {
		t.Errorf("API Key 設置錯误")
	}
	if client.secretKey != secretKey {
		t.Errorf("Secret Key 設置錯误")
	}
	if client.baseURL != CryptoComMainnetBaseURL {
		t.Errorf("主网 URL 錯误: 期望 %s, 得到 %s", CryptoComMainnetBaseURL, client.baseURL)
	}

	// 测試測試網客戶端
	testnetClient := NewCryptoComClient(apiKey, secretKey, true)
	if testnetClient.baseURL != CryptoComTestnetBaseURL {
		t.Errorf("測試網 URL 錯误: 期望 %s, 得到 %s", CryptoComTestnetBaseURL, testnetClient.baseURL)
	}
}

func TestSignRequest(t *testing.T) {
	client := NewCryptoComClient("test_key", "test_secret", false)

	method := "private/create-order"
	params := map[string]interface{}{
		"instrument_name": "BTC_USDT",
		"side":            "BUY",
		"type":            "LIMIT",
		"quantity":        "0.001",
		"price":           "50000",
	}
	nonce := int64(1234567890)

	signature := client.signRequest(method, params, nonce)

	if signature == "" {
		t.Fatal("签名不能為空")
	}

	// 驗证签名长度（HMAC-SHA256 应該產生 64 字符的十六進制字符串）
	if len(signature) != 64 {
		t.Errorf("签名长度錯误: 期望 64, 得到 %d", len(signature))
	}

	// 驗证相同输入產生相同签名
	signature2 := client.signRequest(method, params, nonce)
	if signature != signature2 {
		t.Error("相同输入应該產生相同签名")
	}
}

func TestNewAdapter(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能為 nil")
	}

	if adapter.GetName() != "Crypto.com" {
		t.Errorf("交易所名称錯误: 期望 Crypto.com, 得到 %s", adapter.GetName())
	}
}

func TestConvertInterval(t *testing.T) {
	tests := []struct {
		input    string
		expected CryptoComTimeframe
	}{
		{"1m", CryptoComTimeframe1m},
		{"5m", CryptoComTimeframe5m},
		{"15m", CryptoComTimeframe15m},
		{"30m", CryptoComTimeframe30m},
		{"1h", CryptoComTimeframe1h},
		{"4h", CryptoComTimeframe4h},
		{"1d", CryptoComTimeframe1D},
		{"unknown", CryptoComTimeframe1m}, // 默认值
	}

	for _, tt := range tests {
		result := ConvertInterval(tt.input)
		if result != tt.expected {
			t.Errorf("轉换 %s: 期望 %s, 得到 %s", tt.input, tt.expected, result)
		}
	}
}

func TestAdapterBasicMethods(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	// 测試基本方法
	if adapter.GetPriceDecimals() <= 0 {
		t.Error("價格精度应該大於 0")
	}

	if adapter.GetQuantityDecimals() < 0 {
		t.Error("數量精度不應為負")
	}

	if adapter.GetBaseAsset() == "" {
		t.Error("基础资產不能為空")
	}

	if adapter.GetQuoteAsset() == "" {
		t.Error("报價资產不能為空")
	}
}
