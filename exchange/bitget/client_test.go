package bitget

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"
	passphrase := "test_passphrase"

	// 测試主网客戶端
	client := NewClient(apiKey, secretKey, passphrase, false)
	if client == nil {
		t.Fatal("創建客戶端失败")
	}
	if client.signer == nil {
		t.Fatal("签名器不能為 nil")
	}
	if client.signer.GetAPIKey() != apiKey {
		t.Errorf("API Key 設置錯误")
	}
	if client.baseURL != BitgetBaseURL {
		t.Errorf("主网 URL 設置錯误: 期望 %s, 得到 %s", BitgetBaseURL, client.baseURL)
	}

	// 测試測試網客戶端
	testnetClient := NewClient(apiKey, secretKey, passphrase, true)
	if testnetClient == nil {
		t.Fatal("創建測試網客戶端失败")
	}
	if testnetClient.baseURL != BitgetTestnetBaseURL {
		t.Errorf("測試網 URL 設置錯误: 期望 %s, 得到 %s", BitgetTestnetBaseURL, testnetClient.baseURL)
	}
}

func TestNewSigner(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"
	passphrase := "test_passphrase"

	signer := NewSigner(apiKey, secretKey, passphrase)
	if signer == nil {
		t.Fatal("創建签名器失败")
	}
	if signer.apiKey != apiKey {
		t.Errorf("API Key 設置錯误")
	}
	if signer.secretKey != secretKey {
		t.Errorf("Secret Key 設置錯误")
	}
	if signer.passphrase != passphrase {
		t.Errorf("Passphrase 設置錯误")
	}
}

func TestSign(t *testing.T) {
	signer := NewSigner("test_key", "test_secret", "test_pass")

	timestamp := "1234567890"
	method := "POST"
	requestPath := "/api/mix/v1/order/placeOrder"
	body := `{"symbol":"BTCUSDT","side":"buy"}`

	signature := signer.Sign(timestamp, method, requestPath, body)

	if signature == "" {
		t.Fatal("签名不能為空")
	}

	// 驗证相同输入產生相同签名
	signature2 := signer.Sign(timestamp, method, requestPath, body)
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

	adapter, err := NewBitgetAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能為 nil")
	}

	if adapter.GetName() != "Bitget" {
		t.Errorf("交易所名称錯误: 期望 Bitget, 得到 %s", adapter.GetName())
	}
}

func TestAdapterBasicMethods(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"passphrase": "test_passphrase",
		"testnet":    "false",
	}

	adapter, err := NewBitgetAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	if adapter.GetPriceDecimals() <= 0 {
		t.Error("價格精度应該大於 0")
	}

	if adapter.GetQuantityDecimals() <= 0 {
		t.Error("數量精度应該大於 0")
	}

	if adapter.GetBaseAsset() == "" {
		t.Error("基础资產不能為空")
	}

	if adapter.GetQuoteAsset() == "" {
		t.Error("报價资產不能為空")
	}
}
