package exchange

import (
	"testing"

	"quantmesh/exchange/bitget"
	"quantmesh/exchange/gate"
	"quantmesh/exchange/whitebit"
)

func TestConvertGateSpotTradeRejectsUnverifiedFields(t *testing.T) {
	valid := gate.GateSpotTrade{ID: "trade-1", OrderID: "17", CurrencyPair: "BTC_USDT", Side: "buy", Amount: "0.2", Price: "100", Fee: "0", FeeCurrency: "USDT", CreateTime: "1700000000"}
	tests := []struct {
		name string
		edit func(*gate.GateSpotTrade)
	}{
		{name: "unknown side", edit: func(row *gate.GateSpotTrade) { row.Side = "unknown" }},
		{name: "malformed fee", edit: func(row *gate.GateSpotTrade) { row.Fee = "bad" }},
		{name: "missing fee currency", edit: func(row *gate.GateSpotTrade) { row.FeeCurrency = "" }},
		{name: "wrong order", edit: func(row *gate.GateSpotTrade) { row.OrderID = "18" }},
		{name: "invalid timestamp", edit: func(row *gate.GateSpotTrade) { row.CreateTime = "bad" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := valid
			test.edit(&row)
			if _, err := convertGateSpotTrade(row, "BTCUSDT", 17); err == nil {
				t.Fatal("expected invalid Gate spot trade to be rejected")
			}
		})
	}
	if fill, err := convertGateSpotTrade(valid, "BTCUSDT", 17); err != nil || fill.Side != SideBuy {
		t.Fatalf("valid trade conversion = (%+v, %v), want buy fill", fill, err)
	}
}

func TestConvertWhiteBITDealRejectsUnverifiedFields(t *testing.T) {
	valid := whitebit.OrderDealRecord{ID: 1, DealOrderID: 17, Time: 1700000000, Fee: "0", Price: "100", Amount: "0.2", Role: 1, Deal: "buy", FeeAsset: "USDT"}
	tests := []struct {
		name string
		edit func(*whitebit.OrderDealRecord)
	}{
		{name: "unknown side", edit: func(row *whitebit.OrderDealRecord) { row.Deal = "unknown" }},
		{name: "malformed fee", edit: func(row *whitebit.OrderDealRecord) { row.Fee = "bad" }},
		{name: "missing fee currency", edit: func(row *whitebit.OrderDealRecord) { row.FeeAsset = "" }},
		{name: "wrong order", edit: func(row *whitebit.OrderDealRecord) { row.DealOrderID = 18 }},
		{name: "invalid timestamp", edit: func(row *whitebit.OrderDealRecord) { row.Time = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := valid
			test.edit(&row)
			if _, err := convertWhiteBITDeal(row, "BTC_USDT", 17); err == nil {
				t.Fatal("expected invalid WhiteBIT deal to be rejected")
			}
		})
	}
	if fill, err := convertWhiteBITDeal(valid, "BTC_USDT", 17); err != nil || fill.Side != SideBuy {
		t.Fatalf("valid deal conversion = (%+v, %v), want buy fill", fill, err)
	}
}

func TestConvertBitgetSpotFillRejectsUnverifiedFields(t *testing.T) {
	valid := bitget.BitgetSpotFill{
		OrderId: "17", TradeId: "trade-1", Symbol: "BTCUSDT", Side: "buy", PriceAvg: "100", Size: "0.2", CTime: "1700000000000",
		FeeDetail: []struct {
			Fee     string `json:"fee"`
			FeeCoin string `json:"feeCoin"`
		}{{Fee: "0", FeeCoin: "USDT"}},
	}
	tests := []struct {
		name string
		edit func(*bitget.BitgetSpotFill)
	}{
		{name: "unknown side", edit: func(row *bitget.BitgetSpotFill) { row.Side = "unknown" }},
		{name: "malformed fee", edit: func(row *bitget.BitgetSpotFill) { row.FeeDetail[0].Fee = "bad" }},
		{name: "missing fee coin", edit: func(row *bitget.BitgetSpotFill) { row.FeeDetail[0].FeeCoin = "" }},
		{name: "wrong order", edit: func(row *bitget.BitgetSpotFill) { row.OrderId = "18" }},
		{name: "wrong symbol", edit: func(row *bitget.BitgetSpotFill) { row.Symbol = "ETHUSDT" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := valid
			row.FeeDetail = append([]struct {
				Fee     string `json:"fee"`
				FeeCoin string `json:"feeCoin"`
			}(nil), valid.FeeDetail...)
			test.edit(&row)
			if _, err := convertBitgetSpotFill(row, "BTCUSDT", 17); err == nil {
				t.Fatal("expected invalid Bitget spot fill to be rejected")
			}
		})
	}
	if fill, err := convertBitgetSpotFill(valid, "BTCUSDT", 17); err != nil || fill.Side != SideBuy {
		t.Fatalf("valid fill conversion = (%+v, %v), want buy fill", fill, err)
	}
}
