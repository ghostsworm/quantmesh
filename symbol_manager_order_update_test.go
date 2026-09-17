package main

import (
	"testing"

	"quantmesh/exchange/binance"
	"quantmesh/exchange/okx"
)

// TestToPositionOrderUpdateBaseFeeQty 現貨適配器以嵌入結構附加 BaseFeeQty，反射映射需讀到提升字段
func TestToPositionOrderUpdateBaseFeeQty(t *testing.T) {
	okxUp := okx.SpotStreamOrderUpdate{
		StreamOrderUpdate: okx.StreamOrderUpdate{OrderID: 7, ClientOrderID: "c7", Status: "FILLED", ExecutedQty: 0.01, Commission: 0.6, CommissionAsset: "USDT"},
		BaseFeeQty:        0.00001,
	}
	binUp := binance.SpotStreamOrderUpdate{
		OrderUpdate: binance.OrderUpdate{OrderID: 8, ClientOrderID: "c8", Status: "PARTIALLY_FILLED", ExecutedQty: 0.02, Commission: 0.00002, CommissionAsset: "BTC"},
		BaseFeeQty:  0.00002,
	}
	tests := []struct {
		name     string
		in       interface{}
		wantID   int64
		wantExec float64
		wantComm float64
		wantBase float64
	}{
		{name: "okx spot", in: okxUp, wantID: 7, wantExec: 0.01, wantComm: 0.6, wantBase: 0.00001},
		{name: "binance spot", in: binUp, wantID: 8, wantExec: 0.02, wantComm: 0.00002, wantBase: 0.00002},
		{name: "無 BaseFeeQty 字段", in: okxUp.StreamOrderUpdate, wantID: 7, wantExec: 0.01, wantComm: 0.6, wantBase: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toPositionOrderUpdate(tt.in)
			if got == nil {
				t.Fatal("nil update")
			}
			if got.OrderID != tt.wantID || got.ExecutedQty != tt.wantExec || got.Commission != tt.wantComm || got.BaseFeeQty != tt.wantBase {
				t.Fatalf("got %+v", *got)
			}
		})
	}
}
