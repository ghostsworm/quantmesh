package binance

import (
	"testing"
)

func TestNewAdapter(t *testing.T) {
	config := map[string]string{
		"api_key":    "test_api_key",
		"secret_key": "test_secret_key",
		"testnet":    "false",
	}

	adapter, err := NewBinanceAdapter(config, "BTCUSDT")
	if err != nil {
		t.Fatalf("创建适配器失败: %v", err)
	}

	if adapter == nil {
		t.Fatal("适配器不能为 nil")
	}

	if adapter.GetName() != "Binance" {
		t.Errorf("交易所名称错误: 期望 Binance, 得到 %s", adapter.GetName())
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

func TestSideConstants(t *testing.T) {
	if SideBuy != "BUY" {
		t.Errorf("SideBuy 常量错误: 期望 BUY, 得到 %s", SideBuy)
	}
	if SideSell != "SELL" {
		t.Errorf("SideSell 常量错误: 期望 SELL, 得到 %s", SideSell)
	}
}

func TestOrderTypeConstants(t *testing.T) {
	if OrderTypeLimit != "LIMIT" {
		t.Errorf("OrderTypeLimit 常量错误: 期望 LIMIT, 得到 %s", OrderTypeLimit)
	}
	if OrderTypeMarket != "MARKET" {
		t.Errorf("OrderTypeMarket 常量错误: 期望 MARKET, 得到 %s", OrderTypeMarket)
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
			t.Errorf("订单状态常量不能为空")
		}
	}
}

