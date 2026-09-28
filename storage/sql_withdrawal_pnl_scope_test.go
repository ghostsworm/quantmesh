package storage

import (
	"math"
	"testing"
	"time"
)

func TestGetRealizedPnLForWithdrawalIsolatesFundingAccountMarketAndSymbol(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdrawal-pnl.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC()
	trades := []Trade{
		{Exchange: "binance", MarketType: "futures", Account: "same-prefix", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 100, Fee: 2, CreatedAt: now},
		{Exchange: "binance", MarketType: "futures", Account: "same-prefix", AccountScope: "scope-a", Symbol: "ETHUSDT", PnL: 50, CreatedAt: now},
		{Exchange: "bybit", MarketType: "futures", Account: "same-prefix", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 900, CreatedAt: now},
		{Exchange: "binance", MarketType: "spot", Account: "same-prefix", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 800, CreatedAt: now},
		{Exchange: "binance", MarketType: "futures", Account: "same-prefix", AccountScope: "scope-b", Symbol: "BTCUSDT", PnL: 700, CreatedAt: now},
		{Exchange: "binance", MarketType: "futures", Account: "", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 600, CreatedAt: now},
		{Exchange: "binance", MarketType: "", Account: "same-prefix", AccountScope: "scope-a", Symbol: "BTCUSDT", PnL: 500, CreatedAt: now},
	}
	for i := range trades {
		if err := st.SaveTrade(&trades[i]); err != nil {
			t.Fatalf("save trade %d: %v", i, err)
		}
	}

	tests := []struct {
		name   string
		symbol string
		want   float64
	}{
		{name: "one symbol only", symbol: "BTCUSDT", want: 698},
		{name: "exchange futures account aggregate", want: 748},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := st.GetRealizedPnLForWithdrawal("binance", tt.symbol, "scope-a", now.Add(-time.Minute), now.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("PnL=%v, want %v", got, tt.want)
			}
		})
	}
	if _, err := st.GetRealizedPnLForWithdrawal("", "", "scope-a", now, now); err == nil {
		t.Fatal("missing exchange identity must fail closed")
	}
	if _, err := st.GetRealizedPnLForWithdrawal("binance", "", "", now, now); err == nil {
		t.Fatal("missing account scope must fail closed")
	}
}
