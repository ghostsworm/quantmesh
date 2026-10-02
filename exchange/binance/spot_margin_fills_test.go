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

func TestSpotMarginBorrowHistoryUsesBoundedBorrowQuery(t *testing.T) {
	const startTime = int64(1790503199000)
	const endTime = int64(1790503200000)
	client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint("https://margin.test")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		if req.Method != http.MethodGet || req.URL.Path != "/sapi/v1/margin/borrow-repay" ||
			query.Get("type") != "BORROW" || query.Get("asset") != "BTC" ||
			query.Get("startTime") != fmt.Sprint(startTime) || query.Get("endTime") != fmt.Sprint(endTime) ||
			query.Get("current") != "1" || query.Get("size") != "100" {
			return nil, fmt.Errorf("unexpected bounded margin borrow query: %s %s", req.Method, req.URL.String())
		}
		body := `{"rows":[{"txId":7001,"asset":"BTC","amount":"0.4","status":"CONFIRMED","timestamp":1790503200000}],"total":1}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client}, marginClient: NewMarginClient(client)}
	records, total, err := adapter.GetMarginBorrowHistory(context.Background(), "BTC", startTime, endTime, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(records) != 1 || records[0].TransferID != 7001 || records[0].Asset != "BTC" || records[0].Amount != 0.4 || records[0].Status != "CONFIRMED" {
		t.Fatalf("unexpected margin borrow history mapping: total=%d records=%+v", total, records)
	}
}

func TestSpotMarginRepayHistoryUsesExactAssetAndTimeWindow(t *testing.T) {
	const startTime = int64(1790503199000)
	const endTime = int64(1790503200000)
	client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint("https://margin.test")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		if req.Method != http.MethodGet || req.URL.Path != "/sapi/v1/margin/borrow-repay" ||
			query.Get("type") != "REPAY" || query.Get("asset") != "BTC" ||
			query.Get("startTime") != fmt.Sprint(startTime) || query.Get("endTime") != fmt.Sprint(endTime) ||
			query.Get("current") != "1" || query.Get("size") != "100" {
			return nil, fmt.Errorf("unexpected bounded margin repay query: %s %s", req.Method, req.URL.String())
		}
		body := `{"rows":[{"txId":7003,"asset":"BTC","amount":"0.399","principal":"0.398","interest":"0.001","status":"CONFIRMED","timestamp":1790503199500}],"total":1}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client}, marginClient: NewMarginClient(client)}
	records, total, err := adapter.GetMarginTransactionHistory(context.Background(), "BTC", "REPAY", startTime, endTime, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(records) != 1 || records[0].TransferID != 7003 || records[0].Asset != "BTC" ||
		records[0].Amount != 0.399 || records[0].Principal != 0.398 || records[0].Interest != 0.001 ||
		records[0].Status != "CONFIRMED" || records[0].Timestamp != 1790503199500 {
		t.Fatalf("unexpected margin repay history mapping: total=%d records=%+v", total, records)
	}
}

func TestSpotMarginTransactionByIDUsesExchangeTimestampAndRequiresConfirmedCrossMargin(t *testing.T) {
	const transactionID int64 = 7002
	const transactionTime int64 = 1790503200123
	client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint("https://margin.test")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		if req.Method != http.MethodGet || req.URL.Path != "/sapi/v1/margin/borrow-repay" || query.Get("type") != "REPAY" ||
			query.Get("asset") != "BTC" || query.Get("txId") != fmt.Sprint(transactionID) || query.Get("size") != "1" {
			return nil, fmt.Errorf("unexpected margin tx lookup: %s %s", req.Method, req.URL.String())
		}
		body := `{"rows":[{"txId":7002,"asset":"BTC","amount":"0.4005","principal":"0.4","interest":"0.0005","status":"CONFIRMED","timestamp":1790503200123}],"total":1}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client}, marginClient: NewMarginClient(client)}
	record, err := adapter.GetMarginTransactionByID(context.Background(), "BTC", "REPAY", transactionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.TransferID != transactionID || record.Asset != "BTC" || record.Amount != 0.4005 || record.Principal != 0.4 || record.Interest != 0.0005 || record.Status != "CONFIRMED" || record.Timestamp != transactionTime {
		t.Fatalf("unexpected confirmed margin transaction: %+v", record)
	}
}

func TestSpotMarginInterestHistoryPreservesCrossMarginAccountScope(t *testing.T) {
	const startTime = int64(1790503199000)
	const endTime = int64(1790503200000)
	client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint("https://margin.test")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		if req.Method != http.MethodGet || req.URL.Path != "/sapi/v1/margin/interestHistory" ||
			query.Get("asset") != "" || query.Get("startTime") != fmt.Sprint(startTime) ||
			query.Get("endTime") != fmt.Sprint(endTime) || query.Get("current") != "2" || query.Get("size") != "50" {
			return nil, fmt.Errorf("unexpected bounded margin interest query: %s %s", req.Method, req.URL.String())
		}
		body := `{"rows":[{"txId":8001,"interestAccuredTime":1790503199500,"asset":"BNB","rawAsset":"BTC","principal":"0.4","interest":"0.0001","interestRate":"0.00025","type":"PERIODIC_CONVERTED"}],"total":51}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client}, marginClient: NewMarginClient(client)}
	adapter.feeHistoricalRateFetcher = func(_ context.Context, asset, quote string, tradeTime int64) (float64, error) {
		if asset != "BNB" || quote != "USDT" || tradeTime != 1790503199500 {
			t.Fatalf("unexpected historical interest quote request: %s/%s at %d", asset, quote, tradeTime)
		}
		return 600, nil
	}
	records, total, err := adapter.GetMarginInterestHistory(context.Background(), "", startTime, endTime, 2, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total != 51 || len(records) != 1 || records[0].TransactionID != 8001 || records[0].Asset != "BNB" || records[0].RawAsset != "BTC" ||
		records[0].Principal != 0.4 || records[0].Interest != 0.0001 || records[0].Type != "PERIODIC_CONVERTED" || records[0].IsolatedSymbol != "" {
		t.Fatalf("unexpected cross-margin interest mapping: total=%d records=%+v", total, records)
	}
	if records[0].ValuationStatus != "VALUED" || records[0].ValuationAsset != "USDT" || records[0].ValuationRate != 600 ||
		records[0].ValuationAmount < 0.06-1e-12 || records[0].ValuationAmount > 0.06+1e-12 ||
		records[0].ValuationMinute != 1790503140000 || records[0].ValuationSource != "BINANCE_SPOT_1M_CLOSE" {
		t.Fatalf("unexpected historical interest valuation: %+v", records[0])
	}
}

func TestSpotMarginInterestHistoryRejectsRowsOutsideQueryScope(t *testing.T) {
	for _, body := range []string{
		`{"rows":[{"txId":8001,"interestAccuredTime":1790503200001,"asset":"BTC","principal":"0.4","interest":"0.0001","interestRate":"0.00025","type":"PERIODIC"}],"total":1}`,
		`{"rows":[{"txId":8001,"interestAccuredTime":1790503199500,"asset":"ETH","principal":"0.4","interest":"0.0001","interestRate":"0.00025","type":"PERIODIC"}],"total":1}`,
		`{"rows":[{"txId":8001,"interestAccuredTime":1790503199500,"asset":"BNB","principal":"0.4","interest":"0.0001","interestRate":"0.00025","type":"PERIODIC_CONVERTED"}],"total":1}`,
	} {
		client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint("https://margin.test")
		client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		})}
		adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client}, marginClient: NewMarginClient(client)}
		if _, _, err := adapter.GetMarginInterestHistory(context.Background(), "BTC", 1790503199000, 1790503200000, 1, 50); err == nil {
			t.Fatalf("accepted interest row outside requested scope: %s", body)
		}
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

func TestSpotMarginOrderFillsRejectMissingExecutionEvidence(t *testing.T) {
	createdAt := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	tradeCases := []struct {
		name string
		row  string
	}{
		{name: "missing fee asset", row: `{"id":9,"symbol":"BTCUSDT","orderId":71,"price":"100","qty":"0.5","quoteQty":"50","commission":"0","commissionAsset":"","time":1790503200000,"isBuyer":true}`},
		{name: "wrong order", row: `{"id":9,"symbol":"BTCUSDT","orderId":72,"price":"100","qty":"0.5","quoteQty":"50","commission":"0","commissionAsset":"USDT","time":1790503200000,"isBuyer":true}`},
		{name: "invalid timestamp", row: `{"id":9,"symbol":"BTCUSDT","orderId":71,"price":"100","qty":"0.5","quoteQty":"50","commission":"0","commissionAsset":"USDT","time":0,"isBuyer":true}`},
	}
	for _, test := range tradeCases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/sapi/v1/margin/order":
					fmt.Fprintf(w, `{"symbol":"BTCUSDT","orderId":71,"price":"100","origQty":"1","executedQty":"0.5","cummulativeQuoteQty":"50","status":"PARTIALLY_FILLED","time":%d,"updateTime":%d,"side":"BUY"}`,
						createdAt.UnixMilli(), createdAt.Add(time.Minute).UnixMilli())
				case "/sapi/v1/margin/myTrades":
					fmt.Fprintf(w, `[%s]`, test.row)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint(server.URL)
			spot := &BinanceSpotAdapter{client: client, symbol: "BTCUSDT", baseAsset: "BTC", apiKey: "api-key", secretKey: "api-secret"}
			adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: spot, marginClient: NewMarginClient(client)}
			if _, err := adapter.GetOrderFills(context.Background(), "BTCUSDT", 71); err == nil {
				t.Fatal("expected incomplete margin execution evidence to be rejected")
			}
		})
	}
}
