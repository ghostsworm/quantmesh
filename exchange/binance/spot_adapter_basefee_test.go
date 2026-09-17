package binance

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
)

const binanceFeeTestEps = 1e-9

func TestBinanceSpotStreamUpdateFeeConversion(t *testing.T) {
	var calls atomic.Int64
	a := &BinanceSpotAdapter{symbol: "BTCUSDT", baseAsset: "BTC", quoteAsset: "USDT"}
	a.feePriceFetcher = func(ctx context.Context, pair string) (float64, error) {
		calls.Add(1)
		if pair == "BNBUSDT" {
			return 600, nil
		}
		return 0, errors.New("no such pair")
	}
	tests := []struct {
		name        string
		asset       string
		fee         float64
		avgPx       float64
		wantComm    float64
		wantAsset   string
		wantBaseFee float64
	}{
		{name: "基礎幣收費按成交價換算", asset: "BTC", fee: 0.00001, avgPx: 60000, wantComm: 0.6, wantAsset: "USDT", wantBaseFee: 0.00001},
		{name: "基礎幣無成交價降級保留原幣種", asset: "BTC", fee: 0.00001, avgPx: 0, wantComm: 0.00001, wantAsset: "BTC", wantBaseFee: 0.00001},
		{name: "BNB 抵扣按 BNBUSDT 換算", asset: "BNB", fee: 0.001, avgPx: 60000, wantComm: 0.6, wantAsset: "USDT"},
		{name: "計價幣收費原樣", asset: "USDT", fee: 0.5, avgPx: 60000, wantComm: 0.5, wantAsset: "USDT"},
		{name: "未知幣種降級", asset: "XYZ", fee: 2, avgPx: 60000, wantComm: 2, wantAsset: "XYZ"},
		{name: "無手續費", asset: "BTC", fee: 0, avgPx: 60000, wantComm: 0, wantAsset: "BTC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := OrderUpdate{OrderID: 1, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.01,
				AvgPrice: tt.avgPx, Commission: tt.fee, CommissionAsset: tt.asset}
			got := a.toSpotStreamUpdate(up)
			if math.Abs(got.BaseFeeQty-tt.wantBaseFee) > binanceFeeTestEps {
				t.Fatalf("BaseFeeQty=%v want %v", got.BaseFeeQty, tt.wantBaseFee)
			}
			if math.Abs(got.Commission-tt.wantComm) > binanceFeeTestEps || got.CommissionAsset != tt.wantAsset {
				t.Fatalf("Commission=%v %s want %v %s", got.Commission, got.CommissionAsset, tt.wantComm, tt.wantAsset)
			}
			if got.ExecutedQty != 0.01 {
				t.Fatalf("其他字段不應改變: %+v", got)
			}
		})
	}

	// 價格緩存：重複 BNB 成交不再查價；失敗結果同樣緩存
	before := calls.Load()
	a.toSpotStreamUpdate(OrderUpdate{Commission: 0.001, CommissionAsset: "BNB"})
	a.toSpotStreamUpdate(OrderUpdate{Commission: 1, CommissionAsset: "XYZ"})
	if calls.Load() != before {
		t.Fatalf("TTL 內應命中緩存, calls %d -> %d", before, calls.Load())
	}
}

func TestBinanceSpotStreamUpdateUnknownQuote(t *testing.T) {
	a := &BinanceSpotAdapter{symbol: "BTCUSDT", baseAsset: "BTC"}
	got := a.toSpotStreamUpdate(OrderUpdate{Commission: 0.00001, CommissionAsset: "BTC", AvgPrice: 60000})
	if got.Commission != 0.00001 || got.CommissionAsset != "BTC" || got.BaseFeeQty != 0.00001 {
		t.Fatalf("計價幣未知時應降級保留原值: %+v", got)
	}
}
