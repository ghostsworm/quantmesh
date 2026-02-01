package kraken

import (
	"testing"
)

func TestNewKrakenClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"

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
	client := NewKrakenClient("test_key", "test_secret")

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
		"secret_key": "test_secret_key",
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
		"secret_key": "test_secret_key",
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
