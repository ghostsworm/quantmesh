package kucoin

import (
	"testing"
)

func TestNewKuCoinClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"
	passphrase := "test_passphrase"

	client := NewKuCoinClient(apiKey, secretKey, passphrase)
	if client == nil {
		t.Fatal("創建客戶端失败")
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
}

func TestSignRequest(t *testing.T) {
	client := NewKuCoinClient("test_key", "test_secret", "test_pass")

	timestamp := "1234567890"
	method := "POST"
	path := "/api/v1/orders"
	body := `{"clientOid":"test","side":"buy","symbol":"BTC-USDT"}`

	signature, _ := client.signRequest(timestamp, method, path, body)

	if signature == "" {
		t.Fatal("签名不能為空")
	}

	// 驗证相同输入產生相同签名
	signature2, _ := client.signRequest(timestamp, method, path, body)
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

	adapter, err := NewKuCoinAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能為 nil")
	}

	if adapter.GetName() != "KuCoin" {
		t.Errorf("交易所名称錯误: 期望 KuCoin, 得到 %s", adapter.GetName())
	}
}

func TestAdapterBasicMethods(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"passphrase": "test_passphrase",
		"testnet":    "false",
	}

	adapter, err := NewKuCoinAdapter(config, "BTCUSDT")
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
