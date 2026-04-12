package okx

import (
	"encoding/json"
	"testing"
)

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
