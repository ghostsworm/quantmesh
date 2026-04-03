package binance

import (
	"testing"
)

func TestNormalizeBinanceSymbolTypo(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"BTCUSTD", "BTCUSDT"},
		{"btcustd", "BTCUSDT"},
		{"ETHUSTD", "ETHUSDT"},
		{"BTCUSDT", "BTCUSDT"},
		{"  BTCUSDT  ", "BTCUSDT"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeBinanceSymbolTypo(tt.in)
		if got != tt.want {
			t.Errorf("normalizeBinanceSymbolTypo(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNewAdapter(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewBinanceAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("創建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能為 nil")
	}

	if adapter.GetName() != "Binance" {
		t.Errorf("交易所名称錯误: 期望 Binance, 得到 %s", adapter.GetName())
	}
}

func TestAdapterBasicMethods(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewBinanceAdapter(config, "BTCUSDT")
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

func TestSideConstants(t *testing.T) {
	if SideBuy != "BUY" {
		t.Errorf("SideBuy 常量錯误: 期望 BUY, 得到 %s", SideBuy)
	}
	if SideSell != "SELL" {
		t.Errorf("SideSell 常量錯误: 期望 SELL, 得到 %s", SideSell)
	}
}

func TestOrderTypeConstants(t *testing.T) {
	if OrderTypeLimit != "LIMIT" {
		t.Errorf("OrderTypeLimit 常量錯误: 期望 LIMIT, 得到 %s", OrderTypeLimit)
	}
	if OrderTypeMarket != "MARKET" {
		t.Errorf("OrderTypeMarket 常量錯误: 期望 MARKET, 得到 %s", OrderTypeMarket)
	}
}

func TestOrderStatusConstants(t *testing.T) {
	statuses := []OrderStatus{
		OrderStatusNew,
		OrderStatusPartiallyFilled,
		OrderStatusFilled,
		OrderStatusCanceled,
		OrderStatusRejected,
		OrderStatusExpired,
	}

	for _, status := range statuses {
		if status == "" {
			t.Errorf("订單状態常量不能為空")
		}
	}
}
