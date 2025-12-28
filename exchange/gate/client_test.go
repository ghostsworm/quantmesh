package gate

import (
	"testing"
)

func TestNewClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"

	client := NewClient(apiKey, secretKey)
	if client == nil {
		t.Fatal("创建客户端失败")
	}
	if client.signer == nil {
		t.Fatal("签名器不能为 nil")
	}
	if client.signer.GetAPIKey() != apiKey {
		t.Errorf("API Key 设置错误")
	}
}

func TestNewSigner(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"

	signer := NewSigner(apiKey, secretKey)
	if signer == nil {
		t.Fatal("创建签名器失败")
	}
	if signer.apiKey != apiKey {
		t.Errorf("API Key 设置错误")
	}
	if signer.secretKey != secretKey {
		t.Errorf("Secret Key 设置错误")
	}
}

func TestSignREST(t *testing.T) {
	signer := NewSigner("test_key", "test_secret")
	
	method := "POST"
	urlPath := "/api/v4/futures/usdt/orders"
	queryString := "settle=usdt"
	body := `{"contract":"BTC_USDT","size":1,"price":"50000"}`
	timestamp := int64(1234567890)
	
	signature := signer.SignREST(method, urlPath, queryString, body, timestamp)
	
	if signature == "" {
		t.Fatal("签名不能为空")
	}
	
	// 验证相同输入产生相同签名
	signature2 := signer.SignREST(method, urlPath, queryString, body, timestamp)
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

	adapter, err := NewGateAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("创建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能为 nil")
	}

	if adapter.GetName() != "Gate.io" {
		t.Errorf("交易所名称错误: 期望 Gate.io, 得到 %s", adapter.GetName())
	}
}

func TestAdapterBasicMethods(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewGateAdapter(config, "BTCUSDT")
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

