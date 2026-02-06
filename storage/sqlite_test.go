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
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer storage.Close()

	// 1. 测試保存和查詢訂單
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
		t.Errorf("保存订單失败: %v", err)
	}

	orders, err := storage.QueryOrdersWithTimeRange(10, 0, "FILLED", nil, nil)
	if err != nil {
		t.Errorf("查詢訂單失败: %v", err)
	}
	if len(orders) != 1 || orders[0].OrderID != order.OrderID {
		t.Errorf("查詢訂單結果不正确: 期望 123456789, 得到 %v", orders)
	}

	// 2. 测試资金费率保存逻辑（变动存儲）
	timestamp := time.Now()
	if err := storage.SaveFundingRate("BTCUSDT", "binance", 0.0001, timestamp); err != nil {
		t.Errorf("第一次保存资金费率失败: %v", err)
	}

	// 再次保存相同的费率，不应該新增記錄
	if err := storage.SaveFundingRate("BTCUSDT", "binance", 0.0001, timestamp.Add(time.Hour)); err != nil {
		t.Errorf("第二次保存相同资金费率失败: %v", err)
	}

	history, err := storage.GetFundingRateHistory("BTCUSDT", "binance", 10)
	if err != nil {
		t.Errorf("獲取资金费率历史失败: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("相同费率不应重複存儲，當前記錄數: %d", len(history))
	}

	// 保存不同的费率，应該新增
	if err := storage.SaveFundingRate("BTCUSDT", "binance", 0.0002, timestamp.Add(2*time.Hour)); err != nil {
		t.Errorf("保存不同资金费率失败: %v", err)
	}
	history, _ = storage.GetFundingRateHistory("BTCUSDT", "binance", 10)
	if len(history) != 2 {
		t.Errorf("不同费率应新增記錄，當前記錄數: %d", len(history))
	}

	// 3. 测試统计數據查詢
	trade := &Trade{
		BuyOrderID:  1,
		SellOrderID: 2,
		Symbol:      "BTCUSDT",
		Account:     "test_account",
		BuyPrice:    50000.0,
		SellPrice:   51000.0,
		Quantity:    0.1,
		PnL:         100.0,
		CreatedAt:   time.Now(),
	}
	storage.SaveTrade(trade)

	summary, err := storage.GetPnLBySymbol("BTCUSDT", "test_account", time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Errorf("獲取盈亏彙總失败: %v", err)
	}
	if summary.TotalPnL != 100.0 {
		t.Errorf("盈亏彙總计算錯误: 期望 100.0, 得到 %.2f", summary.TotalPnL)
	}

	// 测試不同账戶隔离
	summaryOther, _ := storage.GetPnLBySymbol("BTCUSDT", "other_account", time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour))
	if summaryOther.TotalPnL != 0 {
		t.Errorf("账戶隔离失败: 期望 0, 得到 %.2f", summaryOther.TotalPnL)
	}
}
