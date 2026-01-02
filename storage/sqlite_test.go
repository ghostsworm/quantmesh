package storage

import (
	"os"
	"testing"
	"time"
)

func TestSQLiteStorage(t *testing.T) {
	dbPath := "./test_quantmesh.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer storage.Close()

	// 1. 测试保存和查询订单
	order := &Order{
		OrderID:       123456789,
		ClientOrderID: "test_oid_1",
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		Price:         50000.0,
		Quantity:      0.1,
		Status:        "FILLED",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := storage.SaveOrder(order); err != nil {
		t.Errorf("保存订单失败: %v", err)
	}

	orders, err := storage.QueryOrders(10, 0, "FILLED")
	if err != nil {
		t.Errorf("查询订单失败: %v", err)
	}
	if len(orders) != 1 || orders[0].OrderID != order.OrderID {
		t.Errorf("查询订单结果不正确: 期望 123456789, 得到 %v", orders)
	}

	// 2. 测试资金费率保存逻辑（变动存储）
	timestamp := time.Now()
	if err := storage.SaveFundingRate("BTCUSDT", "binance", 0.0001, timestamp); err != nil {
		t.Errorf("第一次保存资金费率失败: %v", err)
	}

	// 再次保存相同的费率，不应该新增记录
	if err := storage.SaveFundingRate("BTCUSDT", "binance", 0.0001, timestamp.Add(time.Hour)); err != nil {
		t.Errorf("第二次保存相同资金费率失败: %v", err)
	}

	history, err := storage.GetFundingRateHistory("BTCUSDT", "binance", 10)
	if err != nil {
		t.Errorf("获取资金费率历史失败: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("相同费率不应重复存储，当前记录数: %d", len(history))
	}

	// 保存不同的费率，应该新增
	if err := storage.SaveFundingRate("BTCUSDT", "binance", 0.0002, timestamp.Add(2*time.Hour)); err != nil {
		t.Errorf("保存不同资金费率失败: %v", err)
	}
	history, _ = storage.GetFundingRateHistory("BTCUSDT", "binance", 10)
	if len(history) != 2 {
		t.Errorf("不同费率应新增记录，当前记录数: %d", len(history))
	}

	// 3. 测试统计数据查询
	trade := &Trade{
		BuyOrderID:  1,
		SellOrderID: 2,
		Symbol:      "BTCUSDT",
		BuyPrice:    50000.0,
		SellPrice:   51000.0,
		Quantity:    0.1,
		PnL:         100.0,
		CreatedAt:   time.Now(),
	}
	storage.SaveTrade(trade)

	summary, err := storage.GetPnLBySymbol("BTCUSDT", time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Errorf("获取盈亏汇总失败: %v", err)
	}
	if summary.TotalPnL != 100.0 {
		t.Errorf("盈亏汇总计算错误: 期望 100.0, 得到 %.2f", summary.TotalPnL)
	}
}
