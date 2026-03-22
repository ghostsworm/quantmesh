package cryptocom

import (
	"testing"
)

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
