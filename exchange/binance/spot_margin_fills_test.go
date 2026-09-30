package binance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/adshao/go-binance/v2"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSpotMarginOrderClientIDSubmitAndLookupUseMarginAPI(t *testing.T) {
	const clientOrderID = "spotshort-test-cid"
	client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint("https://margin.test")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/sapi/v1/margin/order" {
			return nil, fmt.Errorf("unexpected endpoint %s", req.URL.Path)
		}
		response := `{"symbol":"BTCUSDT","orderId":71,"clientOrderId":"spotshort-test-cid","price":"100","origQty":"0.5","executedQty":"0","cummulativeQuoteQty":"0","status":"NEW","time":1790503200000,"updateTime":1790503200000,"side":"SELL","type":"LIMIT"}`
		if req.Method == http.MethodPost {
			if err := req.ParseForm(); err != nil {
				return nil, fmt.Errorf("parse margin order form: %w", err)
			}
			if req.Form.Get("newClientOrderId") != clientOrderID {
				return nil, fmt.Errorf("margin order did not carry client ID: query=%s body=%s", req.URL.RawQuery, req.Form.Encode())
			}
		} else if req.Method == http.MethodGet {
			if req.URL.Query().Get("origClientOrderId") != clientOrderID || req.URL.Query().Get("symbol") != "BTCUSDT" {
				return nil, fmt.Errorf("margin lookup did not use exact client ID: %s", req.URL.RawQuery)
			}
		} else {
			return nil, fmt.Errorf("unexpected method %s", req.Method)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response)), Request: req}, nil
	})}
	adapter := &BinanceSpotMarginAdapter{
		BinanceSpotAdapter: &BinanceSpotAdapter{client: client, symbol: "BTCUSDT", apiKey: "api-key", secretKey: "api-secret", quantityDecimals: 3, priceDecimals: 2},
		marginClient:       NewMarginClient(client),
	}
	placed, err := adapter.PlaceOrder(context.Background(), &OrderRequest{Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeLimit,
		Quantity: 0.5, Price: 100, ClientOrderID: clientOrderID})
	if err != nil {
		t.Fatal(err)
	}
	if placed.ClientOrderID != clientOrderID {
		t.Fatalf("submit response lost client order ID: %+v", placed)
	}
	found, err := adapter.GetOrderByClientOrderID(context.Background(), "BTCUSDT", clientOrderID)
	if err != nil {
		t.Fatal(err)
	}
	if found == nil || found.OrderID != 71 || found.ClientOrderID != clientOrderID || found.Side != SideSell || found.Quantity != 0.5 {
		t.Fatalf("unexpected reconciled margin order: %+v", found)
	}
}

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
