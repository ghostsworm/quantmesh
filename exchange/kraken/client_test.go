package kraken

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Kraken API secret 在签名時需能 base64 解碼；測試中用固定合法串避免請求階段報錯。
func testKrakenValidSecret() string {
	return base64.StdEncoding.EncodeToString([]byte("test-secret-key-bytes!!"))
}

func TestGetOpenOrdersUsesCompleteFuturesSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/derivatives/api/v3/openorders" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s, want unsigned-query-free account snapshot path", r.Method, r.URL.String())
		}
		if r.Header.Get("APIKey") != "test-key" || r.Header.Get("Nonce") == "" || r.Header.Get("Authent") == "" {
			t.Errorf("missing Kraken authentication headers")
		}
		_, _ = w.Write([]byte(`{"result":"success","openOrders":[{"order_id":"id-1","symbol":"PI_XBTUSD","side":"buy","orderType":"lmt","limitPrice":100,"unfilledSize":3,"receivedTime":"2026-10-02T01:02:03.000Z","status":"untouched","filledSize":2,"reduceOnly":false,"lastUpdateTime":"2026-10-02T01:02:04.000Z"},{"order_id":"id-2","symbol":"PF_ETHUSD","side":"sell","orderType":"stp","limitPrice":0,"unfilledSize":1,"receivedTime":"2026-10-02T01:02:03.000Z","status":"partiallyFilled","filledSize":4,"reduceOnly":true,"lastUpdateTime":"2026-10-02T01:02:04.000Z"}]}`))
	}))
	defer server.Close()
	client := NewKrakenClient("test-key", testKrakenValidSecret())
	client.baseURL = server.URL
	client.httpClient = server.Client()

	orders, err := client.GetOpenOrders(context.Background())
	if err != nil {
		t.Fatalf("GetOpenOrders() error = %v", err)
	}
	if len(orders) != 2 || orders[0].Symbol != "PI_XBTUSD" || orders[0].Quantity != 5 || orders[0].Filled != 2 || orders[1].Quantity != 5 || orders[1].Filled != 4 {
		t.Fatalf("GetOpenOrders() = %#v, want account-wide rows and mapped filled/unfilled sizes", orders)
	}
}

func TestGetOpenOrdersRejectsIncompleteSnapshot(t *testing.T) {
	valid := `{"order_id":"id-1","symbol":"PI_XBTUSD","side":"buy","orderType":"lmt","limitPrice":100,"unfilledSize":3,"receivedTime":"2026-10-02T01:02:03.000Z","status":"untouched","filledSize":0,"reduceOnly":false,"lastUpdateTime":"2026-10-02T01:02:04.000Z"}`
	tests := []struct {
		name string
		body string
	}{
		{name: "missing list", body: `{"result":"success"}`},
		{name: "null list", body: `{"result":"success","openOrders":null}`},
		{name: "missing field", body: `{"result":"success","openOrders":[{"order_id":"id-1"}]}`},
		{name: "duplicate id", body: `{"result":"success","openOrders":[` + valid + `,` + valid + `]}`},
		{name: "unknown side", body: strings.Replace(valid, `"buy"`, `"hold"`, 1)},
		{name: "unknown status", body: strings.Replace(valid, `"untouched"`, `"mystery"`, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tt.body)) }))
			defer server.Close()
			client := NewKrakenClient("test-key", testKrakenValidSecret())
			client.baseURL = server.URL
			client.httpClient = server.Client()
			if _, err := client.GetOpenOrders(context.Background()); err == nil {
				t.Fatal("GetOpenOrders() succeeded on incomplete or untrusted snapshot")
			}
		})
	}
}

func TestKrakenAdapterAccountPreservesAPICurrency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/derivatives/api/v3/accounts" {
			t.Errorf("path = %q, want account endpoint", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"success","accounts":[{"currency":"usd","balanceValue":1500.5,"availableMargin":900,"marginEquity":1200}]}`))
	}))
	defer server.Close()

	client := NewKrakenClient("key", testKrakenValidSecret())
	client.baseURL = server.URL
	client.httpClient = server.Client()
	adapter := &Adapter{client: client}
	account, err := adapter.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if account.BalanceAsset != "USD" || account.TotalBalance != 1500.5 || account.MarginBalance != 1200 {
		t.Fatalf("account = %+v, want API-reported USD currency", account)
	}
}

func TestNewKrakenClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := testKrakenValidSecret()

	client := NewKrakenClient(apiKey, secretKey)
	if client == nil {
		t.Fatal("創建客戶端失败")
	}
	if client.apiKey != apiKey {
		t.Errorf("API Key 設置錯误")
	}
	if client.secretKey != secretKey {
		t.Errorf("Secret Key 設置錯误")
	}
}

func TestSignRequest(t *testing.T) {
	client := NewKrakenClient("test_key", testKrakenValidSecret())

	path := "/0/private/Balance"
	nonce := "1234567890"
	postData := "nonce=1234567890"

	signature := client.signRequest(path, nonce, postData)

	if signature == "" {
		t.Fatal("签名不能為空")
	}

	// 驗证相同输入產生相同签名
	signature2 := client.signRequest(path, nonce, postData)
	if signature != signature2 {
		t.Error("相同输入应該產生相同签名")
	}
}

func TestNewAdapter(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": testKrakenValidSecret(),
		"testnet":    "false",
	}

	adapter, err := NewKrakenAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能為 nil")
	}

	if adapter.GetName() != "Kraken" {
		t.Errorf("交易所名称錯误: 期望 Kraken, 得到 %s", adapter.GetName())
	}
}

func TestAdapterBasicMethods(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": testKrakenValidSecret(),
		"testnet":    "false",
	}

	adapter, err := NewKrakenAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	// 测試基本方法
	if adapter.GetPriceDecimals() <= 0 {
		t.Error("價格精度应該大於 0")
	}

	// 合約/整數張數時數量小數位可為 0
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
