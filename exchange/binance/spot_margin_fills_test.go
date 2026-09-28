package binance

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/adshao/go-binance/v2"
)

func TestSpotMarginOrderFillsUseMarginLedgerAndPreserveBaseFee(t *testing.T) {
	createdAt := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sapi/v1/margin/order":
			if r.URL.Query().Get("orderId") != "71" || r.URL.Query().Get("symbol") != "BTCUSDT" {
				t.Errorf("unexpected margin order query: %s", r.URL.RawQuery)
			}
			fmt.Fprintf(w, `{"symbol":"BTCUSDT","orderId":71,"price":"100","origQty":"1","executedQty":"0.5","cummulativeQuoteQty":"50","status":"PARTIALLY_FILLED","time":%d,"updateTime":%d,"side":"BUY"}`,
				createdAt.UnixMilli(), createdAt.Add(time.Minute).UnixMilli())
		case "/sapi/v1/margin/myTrades":
			if r.URL.Query().Get("isIsolated") != "false" && r.URL.Query().Get("isIsolated") != "" {
				t.Errorf("cross-margin query should not set isolated=true: %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("startTime") == "" {
				t.Error("fill lookup must be bounded from order creation time")
			}
			fmt.Fprint(w, `[{"id":9,"symbol":"BTCUSDT","orderId":71,"price":"100","qty":"0.5","quoteQty":"50","commission":"0.001","commissionAsset":"BTC","time":1790503200000,"isBuyer":true,"isMaker":true}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint(server.URL)
	spot := &BinanceSpotAdapter{client: client, symbol: "BTCUSDT", baseAsset: "BTC", apiKey: "api-key", secretKey: "api-secret"}
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: spot, marginClient: NewMarginClient(client)}

	fills, err := adapter.GetOrderFills(context.Background(), "BTCUSDT", 71)
	if err != nil {
		t.Fatal(err)
	}
	if len(fills) != 1 {
		t.Fatalf("fills=%+v, want one margin trade", fills)
	}
	fill := fills[0]
	if fill.TradeID != "9" || fill.OrderID != 71 || fill.Side != SideBuy || fill.Quantity != 0.5 || fill.BaseFeeQty != 0.001 || fill.CommissionAsset != "BTC" {
		t.Fatalf("margin fill mapping lost order/fee evidence: %+v", fill)
	}
}
