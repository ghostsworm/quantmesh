package bitget

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"
	passphrase := "test_passphrase"

	// 测试主网客户端
	client := NewClient(apiKey, secretKey, passphrase, false)
	if client == nil {
		t.Fatal("创建客户端失败")
	}
	if client.signer == nil {
		t.Fatal("签名器不能为 nil")
	}
	if client.signer.GetAPIKey() != apiKey {
		t.Errorf("API Key 设置错误")
	}
	if client.baseURL != BitgetBaseURL {
		t.Errorf("主网 URL 设置错误: 期望 %s, 得到 %s", BitgetBaseURL, client.baseURL)
	}

	// 测试测试网客户端
	testnetClient := NewClient(apiKey, secretKey, passphrase, true)
	if testnetClient == nil {
		t.Fatal("创建测试网客户端失败")
	}
	if testnetClient.baseURL != BitgetTestnetBaseURL {
		t.Errorf("测试网 URL 设置错误: 期望 %s, 得到 %s", BitgetTestnetBaseURL, testnetClient.baseURL)
	}
}

func TestNewSigner(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"
	passphrase := "test_passphrase"

	signer := NewSigner(apiKey, secretKey, passphrase)
	if signer == nil {
		t.Fatal("创建签名器失败")
	}
	if signer.apiKey != apiKey {
		t.Errorf("API Key 设置错误")
	}
	if signer.secretKey != secretKey {
		t.Errorf("Secret Key 设置错误")
	}
	if signer.passphrase != passphrase {
		t.Errorf("Passphrase 设置错误")
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
		t.Fatal("签名不能为空")
	}

	// 验证相同输入产生相同签名
	signature2 := signer.Sign(timestamp, method, requestPath, body)
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

	adapter, err := NewBitgetAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("创建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能为 nil")
	}

	if adapter.GetName() != "Bitget" {
		t.Errorf("交易所名称错误: 期望 Bitget, 得到 %s", adapter.GetName())
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
