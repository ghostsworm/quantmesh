package bybit

import (
	"testing"
)

func TestNewBybitClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"

	// 测試主网客戶端
	client := NewBybitClient(apiKey, secretKey, false)
	if client == nil {
		t.Fatal("創建主网客戶端失败")
	}
	if client.apiKey != apiKey {
		t.Errorf("API Key 設置錯误")
	}
	if client.secretKey != secretKey {
		t.Errorf("Secret Key 設置錯误")
	}
	if client.baseURL != MainnetRestURL {
		t.Errorf("主网 URL 錯误: 期望 %s, 得到 %s", MainnetRestURL, client.baseURL)
	}

	// 测試測試網客戶端
	testnetClient := NewBybitClient(apiKey, secretKey, true)
	if testnetClient.baseURL != TestnetRestURL {
		t.Errorf("測試網 URL 錯误: 期望 %s, 得到 %s", TestnetRestURL, testnetClient.baseURL)
	}
}

func TestSign(t *testing.T) {
	client := NewBybitClient("test_key", "test_secret", false)

	params := "api_key=test&symbol=BTCUSDT&timestamp=1234567890"
	signature := client.sign(params)

	if signature == "" {
		t.Fatal("签名不能為空")
	}

	// 驗证签名长度（HMAC-SHA256 应該產生 64 字符的十六進制字符串）
	if len(signature) != 64 {
		t.Errorf("签名长度錯误: 期望 64, 得到 %d", len(signature))
	}

	// 驗证相同输入產生相同签名
	signature2 := client.sign(params)
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

	adapter, err := NewBybitAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能為 nil")
	}

	if adapter.GetName() != "Bybit" {
		t.Errorf("交易所名称錯误: 期望 Bybit, 得到 %s", adapter.GetName())
	}
}

func TestAdapterBasicMethods(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewBybitAdapter(config, "BTCUSDT")
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
