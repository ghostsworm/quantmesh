package storage

import (
	"path/filepath"
	"testing"
	"time"

	"quantmesh/database"
)

func countOrdersRows(t *testing.T, st *SQLStorage) int {
	t.Helper()
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM orders`).Scan(&n); err != nil {
		t.Fatalf("統計 orders 行數失败: %v", err)
	}
	return n
}

func saveSameOrderTwice(t *testing.T, st *SQLStorage) {
	t.Helper()
	now := time.Now()
	first := &Order{
		OrderID: 424242, BotID: "bot-1", Symbol: "BTCUSDT", Side: "SELL", Exchange: "binance",
		Price: 60000, Quantity: 0.01, Status: "NEW", CreatedAt: now, UpdatedAt: now,
	}
	if err := st.SaveOrder(first); err != nil {
		t.Fatalf("第一次 SaveOrder 失败: %v", err)
	}
	second := &Order{
		OrderID: 424242, BotID: "bot-1", Symbol: "BTCUSDT", Side: "SELL", Exchange: "binance",
		Price: 60000, Quantity: 0.01, FilledQty: 0.01, Status: "CANCELED", CreatedAt: now, UpdatedAt: now.Add(time.Second),
	}
	if err := st.SaveOrder(second); err != nil {
		t.Fatalf("第二次 SaveOrder（upsert）失败: %v", err)
	}
	if n := countOrdersRows(t, st); n != 1 {
		t.Fatalf("upsert 後應僅 1 行，得到 %d", n)
	}
	var status string
	if err := st.db.QueryRow(`SELECT status FROM orders WHERE order_id = ?`, 424242).Scan(&status); err != nil {
		t.Fatalf("查詢訂單狀態失败: %v", err)
	}
	if status != "CANCELED" {
		t.Fatalf("upsert 應更新 status=CANCELED，得到 %s", status)
	}
}

// TestSaveOrderFreshDatabaseUpsert 全新 SQLite：createTables 應直接建好與 ON CONFLICT 一致的複合唯一索引。
func TestSaveOrderFreshDatabaseUpsert(t *testing.T) {
	st, err := NewSQLStorage(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()

	ok, err := ordersCompositeUniqueIndexMatches(st.db)
	if err != nil || !ok {
		t.Fatalf("全新庫應已具備複合唯一索引: ok=%v err=%v", ok, err)
	}
	saveSameOrderTwice(t, st)
}

// TestCreateTablesCreatesOrdersCompositeIndex createTables 單獨執行（不跑遷移）也應建出複合唯一索引。
func TestCreateTablesCreatesOrdersCompositeIndex(t *testing.T) {
	st, err := NewSQLStorage(filepath.Join(t.TempDir(), "ct.db"))
	if err != nil {
		t.Fatalf("創建存儲失败: %v", err)
	}
	defer st.Close()
	if _, err := st.db.Exec(`DROP INDEX ` + ordersCompositeUniqueIndexName); err != nil {
		t.Fatalf("刪除索引失败: %v", err)
	}
	if err := createTables(st.db); err != nil {
		t.Fatalf("createTables 失败: %v", err)
	}
	ok, err := ordersCompositeUniqueIndexMatches(st.db)
	if err != nil || !ok {
		t.Fatalf("createTables 後應具備複合唯一索引: ok=%v err=%v", ok, err)
	}
}

// TestSaveOrderAfterGORMAutoMigrateSameFile 復現測試網首啟問題：database 包 GORM AutoMigrate
// 與 storage 共用同一 SQLite 文件時會重建 orders 表並丟失複合唯一索引。
// EnsureOrdersSchema 與 SaveOrder 自愈都應讓首次寫入成功。
func TestSaveOrderAfterGORMAutoMigrateSameFile(t *testing.T) {
	for _, tc := range []struct {
		name          string
		explicitCheck bool
	}{
		{name: "explicit_repair", explicitCheck: true},
		{name: "self_heal_on_save", explicitCheck: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "shared.db")
			st, err := NewSQLStorage(path)
			if err != nil {
				t.Fatalf("創建存儲失败: %v", err)
			}
			defer st.Close()

			gdb, err := database.NewDatabase(&database.Config{Type: "sqlite", DSN: path})
			if err != nil {
				t.Fatalf("初始化 GORM 數據庫失败: %v", err)
			}
			defer gdb.Close()

			if tc.explicitCheck {
				if err := st.EnsureOrdersSchema(); err != nil {
					t.Fatalf("EnsureOrdersSchema 失败: %v", err)
				}
				ok, err := ordersCompositeUniqueIndexMatches(st.db)
				if err != nil || !ok {
					t.Fatalf("修復後應具備複合唯一索引: ok=%v err=%v", ok, err)
				}
			}
			saveSameOrderTwice(t, st)
		})
	}
}
