package okx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
)

func TestGetAllOpenOrdersByInstTypePaginatesAcrossSymbols(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/v5/trade/orders-pending" || r.URL.Query().Get("instType") != "SWAP" || r.URL.Query().Has("instId") {
			t.Errorf("unexpected account-wide OKX request: %s", r.URL.RequestURI())
		}
		if r.URL.Query().Get("limit") != "100" {
			t.Errorf("limit=%q, want 100", r.URL.Query().Get("limit"))
		}
		rows := make([]OKXOrder, 0)
		if r.URL.Query().Get("after") == "" {
			for i := 1; i <= 100; i++ {
				rows = append(rows, OKXOrder{OrdId: strconv.Itoa(i), InstId: "ETH-USDT-SWAP", State: "live"})
			}
		} else if r.URL.Query().Get("after") != "100" {
			t.Errorf("after=%q, want 100", r.URL.Query().Get("after"))
		}
		payload, err := json.Marshal(map[string]interface{}{"code": "0", "data": rows})
		if err != nil {
			t.Errorf("marshal response: %v", err)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	client := NewOKXClient("k", "s", "p", false)
	client.baseURL = server.URL
	orders, err := client.GetAllOpenOrdersByInstType(context.Background(), " swap ")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(orders) != 100 || orders[0].OrdId != "1" || orders[99].OrdId != "100" {
		t.Fatalf("requests=%d orders=%d, want two pages and 100 orders", requests, len(orders))
	}
}

func TestGetAllOpenOrdersByInstTypeRejectsNullData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"0","data":null}`))
	}))
	defer server.Close()
	client := NewOKXClient("k", "s", "p", false)
	client.baseURL = server.URL
	if orders, err := client.GetAllOpenOrdersByInstType(context.Background(), "SPOT"); err == nil || orders != nil {
		t.Fatalf("null data returned orders=%v err=%v, want fail-closed error", orders, err)
	}
}

func TestGetAllOpenOrdersByInstTypeRejectsRepeatedOrderID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rows := make([]OKXOrder, 100)
		for i := range rows {
			rows[i] = OKXOrder{OrdId: strconv.Itoa(i + 1)}
		}
		payload, _ := json.Marshal(map[string]interface{}{"code": "0", "data": rows})
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	client := NewOKXClient("k", "s", "p", false)
	client.baseURL = server.URL
	if orders, err := client.GetAllOpenOrdersByInstType(context.Background(), "SWAP"); err == nil || orders != nil {
		t.Fatalf("repeated order ID returned orders=%v err=%v, want fail-closed error", orders, err)
	}
}

// networkTestsEnv 設為 1 時才運行會訪問真實交易所的測試，保證 go test ./... 默認不觸網
const networkTestsEnv = "QUANTMESH_NETWORK_TESTS"

func requireNetworkTests(t *testing.T) {
	t.Helper()
	if os.Getenv(networkTestsEnv) != "1" {
		t.Skipf("訪問真實 OKX 網絡，設置 %s=1 後運行", networkTestsEnv)
	}
}

// TestOrderBookDataArrayUnmarshal 回歸：request() 對 books 返回的 data 為數組，須解到 []OKXOrderBookResponse，不可再包一層 { "data": ... }。
func TestOrderBookDataArrayUnmarshal(t *testing.T) {
	raw := []byte(`[{"instId":"BTC-USDT-SWAP","bids":[["70000","1","0","1"]],"asks":[["70100","2","0","2"]],"ts":"1234567890123"}]`)
	var rows []OKXOrderBookResponse
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].InstID != "BTC-USDT-SWAP" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestNewOKXClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"
	passphrase := "test_passphrase"

	// 测試主网客戶端
	client := NewOKXClient(apiKey, secretKey, passphrase, false)
	if client == nil {
		t.Fatal("創建主网客戶端失败")
	}
	if client.apiKey != apiKey {
		t.Errorf("API Key 設置錯误")
	}
	if client.secretKey != secretKey {
		t.Errorf("Secret Key 設置錯误")
	}
	if client.passphrase != passphrase {
		t.Errorf("Passphrase 設置錯误")
	}
	if client.baseURL != MainnetRestURL {
		t.Errorf("主网 URL 錯误: 期望 %s, 得到 %s", MainnetRestURL, client.baseURL)
	}

	// 测試測試網客戶端
	testnetClient := NewOKXClient(apiKey, secretKey, passphrase, true)
	if testnetClient.baseURL != TestnetRestURL {
		t.Errorf("測試網 URL 錯误: 期望 %s, 得到 %s", TestnetRestURL, testnetClient.baseURL)
	}
}

func TestSign(t *testing.T) {
	client := NewOKXClient("test_key", "test_secret", "test_pass", false)

	timestamp := "2023-01-01T00:00:00.000Z"
	method := "POST"
	requestPath := "/api/v5/trade/order"
	body := `{"instId":"BTC-USDT-SWAP","side":"buy"}`

	signature := client.sign(timestamp, method, requestPath, body)

	if signature == "" {
		t.Fatal("签名不能為空")
	}

	// 驗证相同输入產生相同签名
	signature2 := client.sign(timestamp, method, requestPath, body)
	if signature != signature2 {
		t.Error("相同输入应該產生相同签名")
	}
}

func TestNewAdapter(t *testing.T) {
	requireNetworkTests(t)
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"passphrase": "test_passphrase",
		"testnet":    "false",
	}

	adapter, err := NewOKXAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能為 nil")
	}

	if adapter.GetName() != "OKX" {
		t.Errorf("交易所名称錯误: 期望 OKX, 得到 %s", adapter.GetName())
	}
}

func TestAdapterBasicMethods(t *testing.T) {
	requireNetworkTests(t)
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"passphrase": "test_passphrase",
		"testnet":    "false",
	}

	adapter, err := NewOKXAdapter(config, "BTCUSDT")
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
