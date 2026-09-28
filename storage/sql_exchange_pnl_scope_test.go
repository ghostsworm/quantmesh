package storage

import (
	"testing"
	"time"
)

func TestGetExchangePnLByAccountScopeIsolatesCredentials(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/exchange-pnl.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	for _, order := range []*Order{
		{OrderID: 1001, AccountScope: "scope-a", Exchange: "binance", Status: "FILLED", RealizedPnL: floatPointer(12.5), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{OrderID: 1002, AccountScope: "scope-b", Exchange: "binance", Status: "FILLED", RealizedPnL: floatPointer(900), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{OrderID: 1003, AccountScope: "scope-a", Exchange: "bybit", Status: "FILLED", RealizedPnL: floatPointer(400), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		{OrderID: 1004, AccountScope: "", Exchange: "binance", Status: "FILLED", RealizedPnL: floatPointer(700), CreatedAt: time.Now(), UpdatedAt: time.Now()},
	} {
		if err := st.SaveOrder(order); err != nil {
			t.Fatalf("SaveOrder(%d): %v", order.OrderID, err)
		}
	}

	got, err := st.GetExchangePnLByAccountScope("binance", "scope-a")
	if err != nil {
		t.Fatal(err)
	}
	if got != 12.5 {
		t.Fatalf("scoped exchange PnL=%v, want 12.5", got)
	}
	if _, err := st.GetExchangePnLByAccountScope("", "scope-a"); err == nil {
		t.Fatal("expected incomplete scope to be rejected")
	}
}

func floatPointer(value float64) *float64 { return &value }
