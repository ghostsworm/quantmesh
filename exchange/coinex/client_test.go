package coinex

import (
	"testing"
)

func TestNewCoinExClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"

	client := NewCoinExClient(apiKey, secretKey, false)
	if client == nil {
		t.Fatal("创建客户端失败")
	}
	if client.apiKey != apiKey {
		t.Errorf("API Key 设置错误")
	}
	if client.secretKey != secretKey {
		t.Errorf("Secret Key 设置错误")
	}
}

func TestSignRequest(t *testing.T) {
	client := NewCoinExClient("test_key", "test_secret", false)

	params := map[string]string{
		"access_id": "test_key",
		"market":    "BTCUSDT",
		"tonce":     "1234567890",
	}

	signature := client.signRequest(params)

	if signature == "" {
		t.Fatal("签名不能为空")
	}

	// 验证相同输入产生相同签名
	signature2 := client.signRequest(params)
	if signature != signature2 {
		t.Error("相同输入应该产生相同签名")
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
		t.Fatalf("创建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能为 nil")
	}

	if adapter.GetName() != "CoinEx" {
		t.Errorf("交易所名称错误: 期望 CoinEx, 得到 %s", adapter.GetName())
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
		t.Fatalf("创建适配器失败: %v", err)
	}

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
