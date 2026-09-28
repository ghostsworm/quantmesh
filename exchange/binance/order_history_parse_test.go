package binance

import (
	"testing"

	"github.com/adshao/go-binance/v2/futures"
)

func TestParseFuturesUserTradeRejectsUnparseableOrInvalidEconomics(t *testing.T) {
	base := &futures.AccountTrade{ID: 9, OrderID: 90, Symbol: "BTCUSDT", Side: futures.SideTypeBuy, Price: "100", Quantity: "0.5",
		QuoteQuantity: "50", Commission: "0.01", CommissionAsset: "USDT", RealizedPnl: "0", Time: 1_790_000_000_000}
	t.Run("valid execution", func(t *testing.T) {
		got, err := parseFuturesUserTrade(base, "BTCUSDT")
		if err != nil || got.ID != 9 || got.Quantity != 0.5 || got.Commission != 0.01 {
			t.Fatalf("valid execution parse = %+v, %v", got, err)
		}
	})
	for name, mutate := range map[string]func(*futures.AccountTrade){
		"invalid commission": func(row *futures.AccountTrade) { row.Commission = "not-a-number" },
		"missing fee asset":  func(row *futures.AccountTrade) { row.CommissionAsset = "" },
		"nonfinite price":    func(row *futures.AccountTrade) { row.Price = "NaN" },
		"unknown side":       func(row *futures.AccountTrade) { row.Side = "UNKNOWN" },
		"wrong symbol":       func(row *futures.AccountTrade) { row.Symbol = "ETHUSDT" },
		"missing time":       func(row *futures.AccountTrade) { row.Time = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			row := *base
			mutate(&row)
			if _, err := parseFuturesUserTrade(&row, "BTCUSDT"); err == nil {
				t.Fatal("invalid exchange history row was accepted")
			}
		})
	}
}
