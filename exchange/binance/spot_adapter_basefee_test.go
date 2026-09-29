package binance

import (
	"context"
	"errors"
	"math"
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
)

func TestSummarizeSpotQuoteBalanceNeverAddsDifferentAssets(t *testing.T) {
	balances := []binancesdk.Balance{
		{Asset: "BTC", Free: "2", Locked: "1"},
		{Asset: "USDT", Free: "10", Locked: "3"},
		{Asset: "USDC", Free: "99", Locked: "4"},
	}
	total, available, err := summarizeSpotQuoteBalance(balances, "USDT")
	if err != nil {
		t.Fatalf("summarize quote balance: %v", err)
	}
	if total != 13 || available != 10 {
		t.Fatalf("got total=%v available=%v, want quote-only total=13 available=10", total, available)
	}
	if total, available, err := summarizeSpotQuoteBalance(balances, "EUR"); err != nil || total != 0 || available != 0 {
		t.Fatalf("missing quote asset should be zero: total=%v available=%v err=%v", total, available, err)
	}
	if _, _, err := summarizeSpotQuoteBalance([]binancesdk.Balance{{Asset: "USDT", Free: "NaN", Locked: "0"}}, "USDT"); err == nil {
		t.Fatal("invalid quote balance must fail")
	}
}

func TestCumulativeAveragePrice(t *testing.T) {
	tests := []struct {
		name            string
		cumulativeQuote float64
		executedQty     float64
		want            float64
	}{
		{name: "partial fills use cumulative quote", cumulativeQuote: 250, executedQty: 2, want: 125},
		{name: "no execution has no average", cumulativeQuote: 0, executedQty: 0, want: 0},
		{name: "missing cumulative quote is not replaced by order price", cumulativeQuote: 0, executedQty: 2, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cumulativeAveragePrice(tt.cumulativeQuote, tt.executedQty); got != tt.want {
				t.Fatalf("cumulativeAveragePrice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSpotCommissionAuthority(t *testing.T) {
	tests := []struct {
		name, raw, asset string
		want             float64
		known            bool
	}{
		{name: "explicit zero", raw: "0", known: true},
		{name: "valid fee and asset", raw: "0.001", asset: "BTC", want: 0.001, known: true},
		{name: "missing fee", asset: "USDT"},
		{name: "positive fee without asset", raw: "0.001", want: 0.001},
		{name: "malformed fee", raw: "unknown", asset: "USDT"},
		{name: "non-finite fee", raw: "NaN", asset: "USDT", want: math.NaN()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, known := parseSpotCommission(tt.raw, tt.asset)
			if known != tt.known || tt.known && got != tt.want || !tt.known && !math.IsNaN(tt.want) && got != tt.want {
				t.Fatalf("parseSpotCommission()=(%v,%v), want (%v,%v)", got, known, tt.want, tt.known)
			}
		})
	}
}

const binanceFeeTestEps = 1e-9

func TestBinanceSpotStreamUpdateFeeConversion(t *testing.T) {
	a := &BinanceSpotAdapter{symbol: "BTCUSDT", baseAsset: "BTC", quoteAsset: "USDT"}
	var calls int
	a.feeHistoricalRateFetcher = func(ctx context.Context, asset, quote string, tradeTime int64) (float64, error) {
		calls++
		if asset == "BNB" && quote == "USDT" && tradeTime == 1_790_000_000_123 {
			return 600, nil
		}
		return 0, errors.New("no candle covering trade minute")
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
		{name: "BNB 抵扣按成交分鐘歷史收盤價換算", asset: "BNB", fee: 0.001, avgPx: 60000, wantComm: 0.6, wantAsset: "USDT"},
		{name: "計價幣收費原樣", asset: "USDT", fee: 0.5, avgPx: 60000, wantComm: 0.5, wantAsset: "USDT"},
		{name: "未知幣種降級", asset: "XYZ", fee: 2, avgPx: 60000, wantComm: 2, wantAsset: "XYZ"},
		{name: "無手續費", asset: "BTC", fee: 0, avgPx: 60000, wantComm: 0, wantAsset: "BTC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := OrderUpdate{OrderID: 1, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.01, UpdateTime: 1_790_000_000_123,
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

	if calls != 2 { // 只有 BNB/XYZ 兩筆第三資產費用會查歷史價格
		t.Fatalf("historical fee lookup count = %d, want 2", calls)
	}
}

func TestBinanceSpotStreamUpdateUnknownQuote(t *testing.T) {
	a := &BinanceSpotAdapter{symbol: "BTCUSDT", baseAsset: "BTC"}
	got := a.toSpotStreamUpdate(OrderUpdate{Commission: 0.00001, CommissionAsset: "BTC", AvgPrice: 60000})
	if got.Commission != 0.00001 || got.CommissionAsset != "BTC" || got.BaseFeeQty != 0.00001 {
		t.Fatalf("計價幣未知時應降級保留原值: %+v", got)
	}
}
