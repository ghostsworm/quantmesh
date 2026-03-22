package xtcom

import (
	"encoding/json"
	"testing"
)

func TestNewXTClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"

	// 测試主网客戶端
	client := NewXTClient(apiKey, secretKey, false)
	if client == nil {
		t.Fatal("創建主网客戶端失败")
	}
	if client.apiKey != apiKey {
		t.Errorf("API Key 設置錯误")
	}
	if client.secretKey != secretKey {
		t.Errorf("Secret Key 設置錯误")
	}
	if client.baseURL != XTMainnetBaseURL {
		t.Errorf("主网 URL 錯误: 期望 %s, 得到 %s", XTMainnetBaseURL, client.baseURL)
	}

	// 测試測試網客戶端
	testnetClient := NewXTClient(apiKey, secretKey, true)
	if testnetClient.baseURL != XTTestnetBaseURL {
		t.Errorf("測試網 URL 錯误: 期望 %s, 得到 %s", XTTestnetBaseURL, testnetClient.baseURL)
	}
}

func TestSignRequest(t *testing.T) {
	client := NewXTClient("test_key", "test_secret", false)

	method := "POST"
	path := "/v4/order"
	timestamp := "1234567890"
	body := `{"symbol":"btc_usdt","side":"BUY"}`

	signature := client.signRequest(method, path, timestamp, body)

	if signature == "" {
		t.Fatal("签名不能為空")
	}

	// 驗证签名长度（HMAC-SHA256 应該產生 64 字符的十六進制字符串）
	if len(signature) != 64 {
		t.Errorf("签名长度錯误: 期望 64, 得到 %d", len(signature))
	}

	// 驗证相同输入產生相同签名
	signature2 := client.signRequest(method, path, timestamp, body)
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

	if adapter.GetName() != "XT.COM" {
		t.Errorf("交易所名称錯误: 期望 XT.COM, 得到 %s", adapter.GetName())
	}
}

func TestConvertInterval(t *testing.T) {
	tests := []struct {
		input    string
		expected XTInterval
	}{
		{"1m", XTInterval1m},
		{"5m", XTInterval5m},
		{"15m", XTInterval15m},
		{"30m", XTInterval30m},
		{"1h", XTInterval1h},
		{"4h", XTInterval4h},
		{"1d", XTInterval1d},
		{"unknown", XTInterval1m}, // 默认值
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

func TestParseSymbolFromAPIResult_WrappedSymbolsArray(t *testing.T) {
	const sample = `{"time":1,"version":"v","symbols":[{"symbol":"btc_usdt","baseCurrency":"btc","quoteCurrency":"usdt","pricePrecision":2,"quantityPrecision":5}]}`
	var result interface{}
	if err := json.Unmarshal([]byte(sample), &result); err != nil {
		t.Fatal(err)
	}
	s, err := parseSymbolFromAPIResult(result)
	if err != nil {
		t.Fatal(err)
	}
	if s.Symbol != "btc_usdt" || s.PricePrecision != 2 || s.QuantityPrecision != 5 {
		t.Fatalf("unexpected: %+v", s)
	}
	if s.BaseCurrency != "btc" || s.QuoteCurrency != "usdt" {
		t.Fatalf("currencies: %+v", s)
	}
}
