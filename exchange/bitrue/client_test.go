package bitrue

import (
	"testing"
)

func TestNewBitrueClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"

	// 测试主网客户端
	client := NewBitrueClient(apiKey, secretKey, false)
	if client == nil {
		t.Fatal("创建主网客户端失败")
	}
	if client.apiKey != apiKey {
		t.Errorf("API Key 设置错误: 期望 %s, 得到 %s", apiKey, client.apiKey)
	}
	if client.secretKey != secretKey {
		t.Errorf("Secret Key 设置错误")
	}
	if client.baseURL != BitrueMainnetBaseURL {
		t.Errorf("主网 URL 错误: 期望 %s, 得到 %s", BitrueMainnetBaseURL, client.baseURL)
	}

	// 测试测试网客户端
	testnetClient := NewBitrueClient(apiKey, secretKey, true)
	if testnetClient.baseURL != BitrueTestnetBaseURL {
		t.Errorf("测试网 URL 错误: 期望 %s, 得到 %s", BitrueTestnetBaseURL, testnetClient.baseURL)
	}
}

func TestSignRequest(t *testing.T) {
	client := NewBitrueClient("test_key", "test_secret", false)
	
	queryString := "symbol=BTCUSDT&timestamp=1234567890"
	signature := client.signRequest(queryString)
	
	if signature == "" {
		t.Fatal("签名不能为空")
	}
	
	// 验证签名长度（HMAC-SHA256 应该产生 64 字符的十六进制字符串）
	if len(signature) != 64 {
		t.Errorf("签名长度错误: 期望 64, 得到 %d", len(signature))
	}
	
	// 验证相同输入产生相同签名
	signature2 := client.signRequest(queryString)
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

	if adapter.symbol != "BTCUSDT" {
		t.Errorf("交易对设置错误: 期望 BTCUSDT, 得到 %s", adapter.symbol)
	}

	if adapter.GetName() != "Bitrue" {
		t.Errorf("交易所名称错误: 期望 Bitrue, 得到 %s", adapter.GetName())
	}
}

func TestConvertInterval(t *testing.T) {
	tests := []struct {
		input    string
		expected BitrueInterval
	}{
		{"1m", BitrueInterval1m},
		{"5m", BitrueInterval5m},
		{"15m", BitrueInterval15m},
		{"30m", BitrueInterval30m},
		{"1h", BitrueInterval1h},
		{"4h", BitrueInterval4h},
		{"1d", BitrueInterval1d},
		{"unknown", BitrueInterval1m}, // 默认值
	}

	for _, tt := range tests {
		result := ConvertInterval(tt.input)
		if result != tt.expected {
			t.Errorf("转换 %s: 期望 %s, 得到 %s", tt.input, tt.expected, result)
		}
	}
}

func TestAdapterGetPriceDecimals(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("创建适配器失败: %v", err)
	}

	decimals := adapter.GetPriceDecimals()
	if decimals <= 0 {
		t.Errorf("价格精度应该大于 0, 得到 %d", decimals)
	}
}

func TestAdapterGetQuantityDecimals(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("创建适配器失败: %v", err)
	}

	decimals := adapter.GetQuantityDecimals()
	if decimals <= 0 {
		t.Errorf("数量精度应该大于 0, 得到 %d", decimals)
	}
}

