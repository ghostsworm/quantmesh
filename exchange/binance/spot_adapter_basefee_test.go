package binance

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
)

func TestHistoricalAssetUSDTQuoteUsesExactDirectOrInverseMinute(t *testing.T) {
	const tradeTime int64 = 1_790_503_199_500
	const quoteMinute int64 = tradeTime / spotFeeMinuteMillis * spotFeeMinuteMillis
	for _, test := range []struct {
		name       string
		inverse    bool
		missing    bool
		wantSymbol string
		wantRate   float64
	}{
		{name: "direct", wantSymbol: "BNBUSDT", wantRate: 600},
		{name: "inverse", inverse: true, wantSymbol: "USDTBNB", wantRate: 600},
		{name: "missing direct and inverse markets", missing: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var requested []string
			client := binancesdk.NewClient("", "").SetApiEndpoint("https://spot.test")
			client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/api/v3/klines" || req.URL.Query().Get("interval") != "1m" ||
					req.URL.Query().Get("startTime") != fmt.Sprint(quoteMinute) || req.URL.Query().Get("endTime") != fmt.Sprint(quoteMinute+spotFeeMinuteMillis-1) {
					return nil, fmt.Errorf("unexpected minute kline request: %s", req.URL.String())
				}
				symbol := req.URL.Query().Get("symbol")
				requested = append(requested, symbol)
				if test.missing || test.inverse && symbol == "BNBUSDT" {
					return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":-1121,"msg":"Invalid symbol."}`)), Request: req}, nil
				}
				closePrice := "600"
				if test.inverse {
					closePrice = "0.0016666666666667"
				}
				body := fmt.Sprintf("[[%d,\"1\",\"1\",\"1\",\"%s\",\"1\",%d,\"1\",1,\"1\",\"1\",\"0\"]]", quoteMinute, closePrice, quoteMinute+spotFeeMinuteMillis-1)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			adapter := &BinanceSpotAdapter{client: client}
			got, err := adapter.historicalAssetQuoteRate(context.Background(), "bnb", "usdt", tradeTime)
			if test.missing {
				if err == nil || got != 0 || len(requested) != 2 {
					t.Fatalf("missing minute should remain unvalued: rate=%v err=%v requests=%v", got, err, requested)
				}
				return
			}
			if err != nil || math.Abs(got-test.wantRate) > 1e-9 || len(requested) != 1+boolInt(test.inverse) ||
				requested[len(requested)-1] != test.wantSymbol {
				t.Fatalf("historical rate=%v err=%v requests=%v, want %.9f via %s", got, err, requested, test.wantRate, test.wantSymbol)
			}
		})
	}
	if rate, err := (&BinanceSpotAdapter{}).historicalAssetQuoteRate(context.Background(), "usdt", "USDT", tradeTime); err != nil || rate != 1 {
		t.Fatalf("USDT must use identity rate: rate=%v err=%v", rate, err)
	}
}

func TestHistoricalAssetQuoteRatesDeduplicatesMinutes(t *testing.T) {
	const first int64 = 1_790_503_199_500
	var requests []int64
	adapter := &BinanceSpotAdapter{}
	adapter.feeHistoricalRateFetcher = func(_ context.Context, asset, quote string, tradeTime int64) (float64, error) {
		if asset != "BNB" || quote != "USDT" {
			t.Fatalf("unexpected batched quote pair %s/%s", asset, quote)
		}
		requests = append(requests, tradeTime)
		return 600, nil
	}
	rates, err := adapter.historicalAssetQuoteRates(context.Background(), "bnb", "usdt", []int64{
		first, first - 1_000, first + 500,
	})
	if err != nil {
		t.Fatal(err)
	}
	minute1 := first / spotFeeMinuteMillis * spotFeeMinuteMillis
	minute2 := minute1 + spotFeeMinuteMillis
	if len(requests) != 2 || requests[0] != first || requests[1] != first+500 || rates[minute1] != 600 || rates[minute2] != 600 {
		t.Fatalf("same-minute requests were not deduplicated: requests=%v rates=%v", requests, rates)
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

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

func TestSpotFeeCoverageRequiresLatestFillToMatchCumulativeDelta(t *testing.T) {
	w := NewSpotUserDataWebSocketManager(nil, false)
	tests := []struct {
		name         string
		cumulative   float64
		latest       float64
		wantCoverage bool
	}{
		{name: "first observed fill", cumulative: 0.25, latest: 0.25, wantCoverage: true},
		{name: "missed fill before reconnect", cumulative: 0.75, latest: 0.25},
		{name: "next contiguous fill", cumulative: 0.9, latest: 0.15, wantCoverage: true},
		{name: "duplicate delivery", cumulative: 0.9, latest: 0.15},
		{name: "stale cumulative event", cumulative: 0.8, latest: 0.1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := w.spotFeeCoversFill("BTCUSDT", 42, tt.cumulative, tt.latest); got != tt.wantCoverage {
				t.Fatalf("spotFeeCoversFill() = %v, want %v", got, tt.wantCoverage)
			}
		})
	}
	w.clearSpotOrderFill("BTCUSDT", 42)
	if !w.spotFeeCoversFill("BTCUSDT", 42, 0.1, 0.1) {
		t.Fatal("terminal-order cleanup should allow a fresh order cursor")
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
