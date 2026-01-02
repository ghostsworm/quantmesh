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
		t.Fatal("创建客户端失败")
	}
	if client.apiKey != apiKey {
		t.Errorf("API Key 设置错误")
	}
	if client.secretKey != secretKey {
		t.Errorf("Secret Key 设置错误")
	}
	if client.passphrase != passphrase {
		t.Errorf("Passphrase 设置错误")
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
		t.Fatal("签名不能为空")
	}

	// 验证相同输入产生相同签名
	signature2, _ := client.signRequest(timestamp, method, path, body)
	if signature != signature2 {
		t.Error("相同输入应该产生相同签名")
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
		t.Fatalf("创建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能为 nil")
	}

	if adapter.GetName() != "KuCoin" {
		t.Errorf("交易所名称错误: 期望 KuCoin, 得到 %s", adapter.GetName())
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
		t.Fatalf("创建适配器失败: %v", err)
	}

	// 测试基本方法
	if adapter.GetPriceDecimals() <= 0 {
		t.Error("价格精度应该大于 0")
	}

	if adapter.GetQuantityDecimals() <= 0 {
		t.Error("数量精度应该大于 0")
	}

	if adapter.GetBaseAsset() == "" {
		t.Error("基础资产不能为空")
	}

	if adapter.GetQuoteAsset() == "" {
		t.Error("报价资产不能为空")
	}
}
