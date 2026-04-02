package storage

import (
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
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

// TestQueryOrdersWithFilterExchange 驗證按 exchange 篩選時，會包含 exchange 匹配的訂單及 exchange 為空的歷史訂單（向後兼容）
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

	// 按 exchange=binance 篩選，應返回 exchange=binance 的訂單 + exchange 為空的歷史訂單（向後兼容）
	orders, err := st.QueryOrdersWithFilter(10, 0, "FILLED", "binance", "PAXGUSDT", nil, nil)
	if err != nil {
		t.Fatalf("查詢訂單失败: %v", err)
	}
	if len(orders) != 2 {
		t.Errorf("按 exchange=binance 篩選應返回 2 筆（含歷史空 exchange）：得到 %d 筆，order_ids=%v", len(orders), func() []int64 {
			ids := make([]int64, len(orders))
			for i, o := range orders {
				ids[i] = o.OrderID
			}
			return ids
		}())
	}
	// 應包含 order 111 (exchange=binance) 和 order 222 (exchange 為空)
	has111, has222 := false, false
	for _, o := range orders {
		if o.OrderID == 111 {
			has111 = true
		}
		if o.OrderID == 222 {
			has222 = true
		}
	}
	if !has111 || !has222 {
		t.Errorf("應同時包含 order 111 和 222，得到 order_ids=%v", func() []int64 {
			ids := make([]int64, len(orders))
			for i, o := range orders {
				ids[i] = o.OrderID
			}
			return ids
		}())
	}
}

// TestSaveOrderUpsert 驗證 order_placed 等事件的 SaveOrder 使用 ON CONFLICT 正確 upsert
// 修復: ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint
func TestSaveOrderUpsert(t *testing.T) {
	dbPath := "./test_order_upsert.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	now := time.Now()
	// 模擬 order_placed: 首次插入
	o1 := &Order{
		OrderID:       999888,
		ClientOrderID: "oid_999",
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		Exchange:      "binance",
		Price:         50000,
		Quantity:      0.01,
		FilledQty:     0,
		Status:        "NEW",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := st.SaveOrder(o1); err != nil {
		t.Fatalf("保存訂單失败: %v", err)
	}

	// 模擬 order_filled: 相同 order_id 更新，應 upsert 而非報錯
	o2 := &Order{
		OrderID:       999888,
		ClientOrderID: "oid_999",
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		Exchange:      "binance",
		Price:         50000,
		Quantity:      0.01,
		FilledQty:     0.01,
		Status:        "FILLED",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := st.SaveOrder(o2); err != nil {
		t.Fatalf("upsert 訂單失败 (ON CONFLICT): %v", err)
	}

	orders, err := st.QueryOrdersWithTimeRange(10, 0, "FILLED", nil, nil)
	if err != nil {
		t.Fatalf("查詢訂單失败: %v", err)
	}
	if len(orders) != 1 || orders[0].Status != "FILLED" || orders[0].FilledQty != 0.01 {
		t.Errorf("upsert 後應僅 1 筆且 status=FILLED, filled_qty=0.01，得到 %d 筆 status=%s filled_qty=%f",
			len(orders), orders[0].Status, orders[0].FilledQty)
	}
}

// TestSaveOrderWithPartialIndexMigration 驗證從 partial 索引遷移到完整索引後 SaveOrder 正常
func TestSaveOrderWithPartialIndexMigration(t *testing.T) {
	dbPath := "./test_partial_migrate.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	// 手動創建帶 partial 索引的 orders 表（模擬舊版遷移產物）
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("打開 DB 失败: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id BIGINT,
			client_order_id TEXT, symbol TEXT, side TEXT, exchange TEXT, type TEXT,
			price DECIMAL(20,8), quantity DECIMAL(20,8), filled_qty DECIMAL(20,8),
			status TEXT, realized_pnl DECIMAL(20,8), strategy_name TEXT, strategy_type TEXT,
			order_source TEXT, created_at TIMESTAMP, updated_at TIMESTAMP
		);
		CREATE UNIQUE INDEX idx_orders_order_id ON orders(order_id) WHERE order_id IS NOT NULL AND order_id != 0
	`)
	db.Close()
	if err != nil {
		t.Fatalf("創建 partial 索引表失败: %v", err)
	}

	// NewSQLiteStorage 會執行遷移：刪除 partial 索引並創建完整索引
	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	// 應能正常 SaveOrder（修復前會報 ON CONFLICT clause does not match）
	o := &Order{
		OrderID: 777666, Symbol: "ETHUSDT", Side: "BUY", Exchange: "binance",
		Price: 3000, Quantity: 0.1, Status: "NEW", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := st.SaveOrder(o); err != nil {
		t.Fatalf("遷移後 SaveOrder 失败: %v", err)
	}
}

// TestSaveOrderWithNonUniqueIndexMigration 驗證從非唯一索引遷移到唯一索引後 SaveOrder 正常
// 模擬 createTables 曾創建的 CREATE INDEX idx_orders_order_id（非 UNIQUE）導致 ON CONFLICT 報錯的場景
func TestSaveOrderWithNonUniqueIndexMigration(t *testing.T) {
	dbPath := "./test_nonunique_migrate.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("打開 DB 失败: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			order_id BIGINT,
			client_order_id TEXT, symbol TEXT, side TEXT, exchange TEXT, type TEXT,
			price DECIMAL(20,8), quantity DECIMAL(20,8), filled_qty DECIMAL(20,8),
			status TEXT, realized_pnl DECIMAL(20,8), strategy_name TEXT, strategy_type TEXT,
			order_source TEXT, created_at TIMESTAMP, updated_at TIMESTAMP
		);
		CREATE INDEX idx_orders_order_id ON orders(order_id)
	`)
	db.Close()
	if err != nil {
		t.Fatalf("創建非唯一索引表失败: %v", err)
	}

	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	o := &Order{
		OrderID: 888999, Symbol: "BTCUSDT", Side: "SELL", Exchange: "binance",
		Price: 50000, Quantity: 0.01, Status: "FILLED", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := st.SaveOrder(o); err != nil {
		t.Fatalf("遷移後 SaveOrder 失败（非唯一索引應被替換為唯一索引）: %v", err)
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

// TestPostgresUnsupported 驗證 PostgreSQL 暫不支持時返回明確錯誤
func TestPostgresUnsupported(t *testing.T) {
	_, err := NewStorage("postgres", "host=localhost dbname=test")
	if err == nil {
		t.Fatal("期望 postgres 返回錯誤，得到 nil")
	}
	if !strings.Contains(err.Error(), "PostgreSQL 暂不支持") {
		t.Errorf("期望錯誤包含「PostgreSQL 暂不支持」，得到: %v", err)
	}
}

func TestQueryRiskCheckHistoryByBotID(t *testing.T) {
	dbPath := "./test_risk_check_bot_filter.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC()
	if err := st.SaveRiskCheck(&RiskCheckRecord{
		CheckTime:      now,
		BotID:          "binance:BTCUSDT:futures",
		Exchange:       "binance",
		MarketType:     "futures",
		Symbol:         "BTCUSDT",
		IsHealthy:      false,
		PriceDeviation: -7.2,
		VolumeRatio:    3.1,
		Reason:         "panic",
	}); err != nil {
		t.Fatalf("保存风控记录 A 失败: %v", err)
	}
	if err := st.SaveRiskCheck(&RiskCheckRecord{
		CheckTime:      now.Add(1 * time.Minute),
		BotID:          "binance:ETHUSDT:futures",
		Exchange:       "binance",
		MarketType:     "futures",
		Symbol:         "ETHUSDT",
		IsHealthy:      true,
		PriceDeviation: 0.2,
		VolumeRatio:    1.1,
		Reason:         "ok",
	}); err != nil {
		t.Fatalf("保存风控记录 B 失败: %v", err)
	}

	histories, err := st.QueryRiskCheckHistory(now.Add(-1*time.Hour), now.Add(1*time.Hour), 200, "binance:BTCUSDT:futures")
	if err != nil {
		t.Fatalf("按 bot_id 查询风控历史失败: %v", err)
	}
	if len(histories) == 0 {
		t.Fatalf("期望有风控历史数据")
	}

	foundBTC := false
	foundETH := false
	for _, h := range histories {
		for _, s := range h.Symbols {
			if s.Symbol == "BTCUSDT" {
				foundBTC = true
			}
			if s.Symbol == "ETHUSDT" {
				foundETH = true
			}
		}
	}
	if !foundBTC {
		t.Fatalf("按 bot_id 过滤后应包含 BTCUSDT")
	}
	if foundETH {
		t.Fatalf("按 bot_id 过滤后不应包含 ETHUSDT")
	}
}

// TestSaveOrderKeepIsolationByExchangeAndBotID 驗證相同 order_id 在不同交易所/Bot 不會互相覆蓋
func TestSaveOrderKeepIsolationByExchangeAndBotID(t *testing.T) {
	dbPath := "./test_order_isolation.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	now := time.Now()
	orderA := &Order{
		OrderID:       12345,
		BotID:         "binance:BTCUSDT:futures",
		ClientOrderID: "oid-a",
		Exchange:      "binance",
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		Price:         60000,
		Quantity:      0.01,
		Status:        "NEW",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	orderB := &Order{
		OrderID:       12345,
		BotID:         "gate:BTCUSDT:futures",
		ClientOrderID: "oid-b",
		Exchange:      "gate",
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		Price:         59999,
		Quantity:      0.02,
		Status:        "NEW",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := st.SaveOrder(orderA); err != nil {
		t.Fatalf("保存訂單 A 失败: %v", err)
	}
	if err := st.SaveOrder(orderB); err != nil {
		t.Fatalf("保存訂單 B 失败: %v", err)
	}

	orders, err := st.QueryOrders(10, 0, "NEW")
	if err != nil {
		t.Fatalf("查詢訂單失败: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("期望保留兩筆同 order_id 不同交易所訂單，實際 %d", len(orders))
	}

	foundBotA, foundBotB := false, false
	for _, o := range orders {
		if o.Exchange == "binance" && o.BotID == "binance:BTCUSDT:futures" {
			foundBotA = true
		}
		if o.Exchange == "gate" && o.BotID == "gate:BTCUSDT:futures" {
			foundBotB = true
		}
	}
	if !foundBotA || !foundBotB {
		t.Fatalf("期望查詢結果保留兩個 bot_id，foundBotA=%v foundBotB=%v", foundBotA, foundBotB)
	}
}

// TestSaveOrderKeepIsolationByExchangeAccountSymbol 驗證相同 exchange+order_id 在不同 account/symbol 下可共存
func TestSaveOrderKeepIsolationByExchangeAccountSymbol(t *testing.T) {
	dbPath := "./test_order_exchange_account_symbol.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	now := time.Now()
	baseOrder := &Order{
		OrderID:       999001,
		BotID:         "binance:BTCUSDT:futures",
		Account:       "acc-A",
		ClientOrderID: "oid-a1",
		Exchange:      "binance",
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		Price:         50000,
		Quantity:      0.01,
		Status:        "NEW",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	sameIDDiffAccount := &Order{
		OrderID:       999001,
		BotID:         "binance:BTCUSDT:futures",
		Account:       "acc-B",
		ClientOrderID: "oid-b1",
		Exchange:      "binance",
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		Price:         50001,
		Quantity:      0.02,
		Status:        "NEW",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	sameIDDiffSymbol := &Order{
		OrderID:       999001,
		BotID:         "binance:ETHUSDT:futures",
		Account:       "acc-A",
		ClientOrderID: "oid-a2",
		Exchange:      "binance",
		Symbol:        "ETHUSDT",
		Side:          "BUY",
		Price:         3000,
		Quantity:      0.5,
		Status:        "NEW",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := st.SaveOrder(baseOrder); err != nil {
		t.Fatalf("保存 baseOrder 失败: %v", err)
	}
	if err := st.SaveOrder(sameIDDiffAccount); err != nil {
		t.Fatalf("保存 sameIDDiffAccount 失败: %v", err)
	}
	if err := st.SaveOrder(sameIDDiffSymbol); err != nil {
		t.Fatalf("保存 sameIDDiffSymbol 失败: %v", err)
	}

	orders, err := st.QueryOrders(20, 0, "NEW")
	if err != nil {
		t.Fatalf("查詢訂單失败: %v", err)
	}
	if len(orders) != 3 {
		t.Fatalf("期望 3 筆不同行鍵訂單，實際 %d", len(orders))
	}

	// 同一複合鍵再次寫入應 upsert，不應新增行
	upsertSameKey := &Order{
		OrderID:       999001,
		BotID:         "binance:BTCUSDT:futures",
		Account:       "acc-A",
		ClientOrderID: "oid-a1-updated",
		Exchange:      "binance",
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		Price:         50002,
		Quantity:      0.03,
		Status:        "FILLED",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := st.SaveOrder(upsertSameKey); err != nil {
		t.Fatalf("upsert same key 失败: %v", err)
	}

	allOrders, err := st.QueryOrdersWithTimeRange(50, 0, "", nil, nil)
	if err != nil {
		t.Fatalf("查詢所有訂單失败: %v", err)
	}
	if len(allOrders) != 3 {
		t.Fatalf("同鍵 upsert 後行數應保持 3，實際 %d", len(allOrders))
	}
}

func TestFixSessionStateCRUD(t *testing.T) {
	dbPath := "./test_fix_session_state.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC()
	state := &FixSessionState{
		SessionID:       "FIX.4.4:SELLER->BUYER",
		BotID:           "binance:BTCUSDT:futures",
		Role:            "acceptor",
		BeginString:     "FIX.4.4",
		SenderCompID:    "SELLER",
		TargetCompID:    "BUYER",
		NextSenderSeq:   12,
		NextTargetSeq:   34,
		IsLoggedOn:      true,
		LastLogonAt:     &now,
		LastHeartbeatAt: &now,
		UpdatedAt:       now,
	}
	if err := st.UpsertFixSessionState(state); err != nil {
		t.Fatalf("保存 FIX 会话状态失败: %v", err)
	}

	got, err := st.GetFixSessionState(state.SessionID)
	if err != nil {
		t.Fatalf("查询 FIX 会话状态失败: %v", err)
	}
	if got == nil || got.NextSenderSeq != 12 || got.NextTargetSeq != 34 || !got.IsLoggedOn || got.BotID != state.BotID {
		t.Fatalf("FIX 会话状态读取异常: %+v", got)
	}

	// 再次 upsert 验证更新
	state.NextSenderSeq = 13
	state.NextTargetSeq = 35
	state.IsLoggedOn = false
	state.LastHeartbeatAt = nil
	state.UpdatedAt = now.Add(time.Minute)
	if err := st.UpsertFixSessionState(state); err != nil {
		t.Fatalf("更新 FIX 会话状态失败: %v", err)
	}

	got, err = st.GetFixSessionState(state.SessionID)
	if err != nil {
		t.Fatalf("查询 FIX 会话状态失败: %v", err)
	}
	if got.NextSenderSeq != 13 || got.NextTargetSeq != 35 || got.IsLoggedOn {
		t.Fatalf("FIX 会话状态更新异常: %+v", got)
	}

	list, err := st.ListFixSessionStates(10, 0)
	if err != nil {
		t.Fatalf("列出 FIX 会话状态失败: %v", err)
	}
	if len(list) != 1 || list[0].SessionID != state.SessionID {
		t.Fatalf("FIX 会话列表异常: %+v", list)
	}
}

func TestFixOrderLinkCRUD(t *testing.T) {
	dbPath := "./test_fix_order_link.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC()
	link := &FixOrderLink{
		SessionID:       "FIX.4.4:SELLER->BUYER",
		ClOrdID:         "A-1001",
		BotID:           "binance:BTCUSDT:futures",
		Exchange:        "binance",
		Symbol:          "BTCUSDT",
		Side:            "BUY",
		InternalOrderID: 987654321,
		LastExecID:      "exec-1",
		OrdStatus:       "NEW",
		CumQty:          0,
		LeavesQty:       1,
		AvgPx:           0,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := st.UpsertFixOrderLink(link); err != nil {
		t.Fatalf("保存 FIX 订单映射失败: %v", err)
	}

	byClOrdID, err := st.GetFixOrderLinkByClOrdID(link.SessionID, link.ClOrdID)
	if err != nil {
		t.Fatalf("按 cl_ord_id 查询失败: %v", err)
	}
	if byClOrdID == nil || byClOrdID.InternalOrderID != link.InternalOrderID {
		t.Fatalf("按 cl_ord_id 查询结果异常: %+v", byClOrdID)
	}

	// 模拟成交推进
	link.LastExecID = "exec-2"
	link.OrdStatus = "PARTIALLY_FILLED"
	link.CumQty = 0.4
	link.LeavesQty = 0.6
	link.AvgPx = 60500
	link.UpdatedAt = now.Add(time.Minute)
	if err := st.UpsertFixOrderLink(link); err != nil {
		t.Fatalf("更新 FIX 订单映射失败: %v", err)
	}

	byInternal, err := st.GetFixOrderLinkByInternalOrderID(link.SessionID, link.InternalOrderID)
	if err != nil {
		t.Fatalf("按 internal_order_id 查询失败: %v", err)
	}
	if byInternal == nil || byInternal.LastExecID != "exec-2" || byInternal.OrdStatus != "PARTIALLY_FILLED" {
		t.Fatalf("按 internal_order_id 查询结果异常: %+v", byInternal)
	}

	list, err := st.ListFixOrderLinks(link.SessionID, "PARTIALLY_FILLED", 10, 0)
	if err != nil {
		t.Fatalf("按状态列出 FIX 订单映射失败: %v", err)
	}
	if len(list) != 1 || list[0].ClOrdID != link.ClOrdID {
		t.Fatalf("FIX 订单映射列表异常: %+v", list)
	}
}

func TestSQLiteStorage_mysqlQuoteIdent(t *testing.T) {
	t.Parallel()
	mysqlSt := &SQLiteStorage{dbType: "mysql"}
	if got := mysqlSt.mysqlQuoteIdent("interval"); got != "`interval`" {
		t.Fatalf("mysql interval: want `interval`, got %q", got)
	}
	if got := mysqlSt.mysqlQuoteIdent("key"); got != "`key`" {
		t.Fatalf("mysql key: want `key`, got %q", got)
	}
	sqliteSt := &SQLiteStorage{dbType: "sqlite"}
	if got := sqliteSt.mysqlQuoteIdent("interval"); got != "interval" {
		t.Fatalf("sqlite: want interval, got %q", got)
	}
}
