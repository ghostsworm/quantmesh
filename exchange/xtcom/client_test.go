package xtcom

import (
	"testing"
)

func TestNewXTClient(t *testing.T) {
	apiKey := "test_api_key"
	secretKey := "test_secret_key"

	// 测试主网客户端
	client := NewXTClient(apiKey, secretKey, false)
	if client == nil {
		t.Fatal("创建主网客户端失败")
	}
	if client.apiKey != apiKey {
		t.Errorf("API Key 设置错误")
	}
	if client.secretKey != secretKey {
		t.Errorf("Secret Key 设置错误")
	}
	if client.baseURL != XTMainnetBaseURL {
		t.Errorf("主网 URL 错误: 期望 %s, 得到 %s", XTMainnetBaseURL, client.baseURL)
	}

	// 测试测试网客户端
	testnetClient := NewXTClient(apiKey, secretKey, true)
	if testnetClient.baseURL != XTTestnetBaseURL {
		t.Errorf("测试网 URL 错误: 期望 %s, 得到 %s", XTTestnetBaseURL, testnetClient.baseURL)
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
		t.Fatal("签名不能为空")
	}

	// 验证签名长度（HMAC-SHA256 应该产生 64 字符的十六进制字符串）
	if len(signature) != 64 {
		t.Errorf("签名长度错误: 期望 64, 得到 %d", len(signature))
	}

	// 验证相同输入产生相同签名
	signature2 := client.signRequest(method, path, timestamp, body)
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

	if adapter.GetName() != "XT.COM" {
		t.Errorf("交易所名称错误: 期望 XT.COM, 得到 %s", adapter.GetName())
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
			t.Errorf("转换 %s: 期望 %s, 得到 %s", tt.input, tt.expected, result)
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
