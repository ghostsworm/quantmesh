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

// TestQueryOrdersWithFilterExchange 驗證按 exchange 篩選時，exchange 為空的訂單會被排除
func TestQueryOrdersWithFilterExchange(t *testing.T) {
	dbPath := "./test_quantmesh_exchange_filter.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	now := time.Now()
	// 有 exchange 的訂單
	orderWithEx := &Order{
		OrderID:       111,
		ClientOrderID: "oid_111",
		Symbol:        "PAXGUSDT",
		Side:          "SELL",
		Exchange:      "binance",
		Price:         5100,
		Quantity:      0.01,
		Status:        "FILLED",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	// exchange 為空的訂單（歷史遺留）
	orderEmptyEx := &Order{
		OrderID:       222,
		ClientOrderID: "oid_222",
		Symbol:        "PAXGUSDT",
		Side:          "SELL",
		Exchange:      "",
		Price:         5099,
		Quantity:      0.01,
		Status:        "FILLED",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := st.SaveOrder(orderWithEx); err != nil {
		t.Fatalf("保存訂單失败: %v", err)
	}
	if err := st.SaveOrder(orderEmptyEx); err != nil {
		t.Fatalf("保存訂單失败: %v", err)
	}

	// 先驗證兩筆訂單都能查到（不按 exchange 篩選）
	allOrders, _ := st.QueryOrdersWithFilter(10, 0, "FILLED", "", "PAXGUSDT", nil, nil)
	if len(allOrders) != 2 {
		t.Fatalf("預期 2 筆訂單，得到 %d 筆", len(allOrders))
	}

	// 按 exchange=binance 篩選，應只返回有 exchange 的訂單（exchange 為空的會被排除）
	// 不傳時間範圍，專注驗證 exchange 篩選邏輯
	orders, err := st.QueryOrdersWithFilter(10, 0, "FILLED", "binance", "PAXGUSDT", nil, nil)
	if err != nil {
		t.Fatalf("查詢訂單失败: %v", err)
	}
	if len(orders) != 1 || orders[0].OrderID != 111 {
		t.Errorf("按 exchange=binance 篩選應只返回 1 筆：得到 %d 筆，order_ids=%v", len(orders), func() []int64 {
			ids := make([]int64, len(orders))
			for i, o := range orders {
				ids[i] = o.OrderID
			}
			return ids
		}())
	}
	if len(orders) > 0 && orders[0].Exchange != "binance" {
		t.Errorf("返回訂單的 exchange 應為 binance，得到 %q", orders[0].Exchange)
	}
}

// TestGetReconciliationCount 驗證對账歷史記錄數統計（與對账頁面卡片顯示一致）
func TestGetReconciliationCount(t *testing.T) {
	dbPath := "./test_recon_count.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	now := time.Now()
	// 插入 3 條對账記錄
	for i := 0; i < 3; i++ {
		h := &ReconciliationHistory{
			Exchange:         "binance",
			Symbol:           "BTCUSDT",
			Account:          "acc1",
			ReconcileTime:    now.Add(time.Duration(i) * time.Minute),
			LocalPosition:    0.001 * float64(i+1),
			ExchangePosition: 0.001 * float64(i+1),
			PositionDiff:     0,
			ActiveBuyOrders:  0,
			ActiveSellOrders: 0,
			PendingSellQty:   0,
			TotalBuyQty:      1,
			TotalSellQty:     0.5,
			EstimatedProfit:  10,
			ActualProfit:     8,
			CreatedAt:        now,
		}
		if err := st.SaveReconciliationHistory(h); err != nil {
			t.Fatalf("保存對账歷史失败: %v", err)
		}
	}

	// 按 exchange+symbol+account 查詢應返回 3
	cnt, err := st.GetReconciliationCount("binance", "BTCUSDT", "acc1")
	if err != nil {
		t.Fatalf("GetReconciliationCount 失败: %v", err)
	}
	if cnt != 3 {
		t.Errorf("期望 3 條記錄，得到 %d", cnt)
	}

	// 不同 account 應返回 0
	cntOther, _ := st.GetReconciliationCount("binance", "BTCUSDT", "other_acc")
	if cntOther != 0 {
		t.Errorf("不同 account 應為 0，得到 %d", cntOther)
	}

	// 空 account 時兼容舊數據（匹配 account 為空或 NULL 的記錄）
	hNoAccount := &ReconciliationHistory{
		Exchange: "binance", Symbol: "ETHUSDT", Account: "",
		ReconcileTime: now, LocalPosition: 0, ExchangePosition: 0, PositionDiff: 0,
		ActiveBuyOrders: 0, ActiveSellOrders: 0, PendingSellQty: 0,
		TotalBuyQty: 0, TotalSellQty: 0, EstimatedProfit: 0, ActualProfit: 0, CreatedAt: now,
	}
	if err := st.SaveReconciliationHistory(hNoAccount); err != nil {
		t.Fatalf("保存對账歷史(無account)失败: %v", err)
	}
	cntEmpty, _ := st.GetReconciliationCount("binance", "ETHUSDT", "acc1")
	if cntEmpty != 1 {
		t.Errorf("空 account 記錄應被 acc1 查詢到(兼容)，期望 1，得到 %d", cntEmpty)
	}
}
