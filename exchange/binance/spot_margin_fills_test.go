package binance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/adshao/go-binance/v2"
	futuresSDK "github.com/adshao/go-binance/v2/futures"
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
		body := `{"rows":[{"txId":7001,"asset":"BTC","amount":"0.400000000000000001","status":"CONFIRMED","timestamp":1790503200000}],"total":1}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client}, marginClient: NewMarginClient(client)}
	records, total, err := adapter.GetMarginBorrowHistory(context.Background(), "BTC", startTime, endTime, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(records) != 1 || records[0].TransferID != 7001 || records[0].Asset != "BTC" || records[0].Amount != 0.4 ||
		records[0].RawAmount != "0.400000000000000001" || records[0].RawPrincipal != "0.400000000000000001" || records[0].RawInterest != "0" || records[0].Status != "CONFIRMED" {
		t.Fatalf("unexpected margin borrow history mapping: total=%d records=%+v", total, records)
	}
}

func TestSpotMarginBorrowByIDDerivesRawComponentsFromWireAmount(t *testing.T) {
	const transactionID = int64(7004)
	client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint("https://margin.test")
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("type") != "BORROW" || req.URL.Query().Get("txId") != fmt.Sprint(transactionID) {
			return nil, fmt.Errorf("unexpected margin transaction query: %s", req.URL.RawQuery)
		}
		body := `{"rows":[{"txId":7004,"asset":"BTC","amount":"0.125000000000000001","status":"CONFIRMED","timestamp":1790503200123}],"total":1}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client}, marginClient: NewMarginClient(client)}
	record, err := adapter.GetMarginTransactionByID(context.Background(), "BTC", "BORROW", transactionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.RawAmount != "0.125000000000000001" || record.RawPrincipal != "0.125000000000000001" || record.RawInterest != "0" ||
		record.Principal != record.Amount || record.Interest != 0 {
		t.Fatalf("borrow components were not derived from the authoritative wire amount: %+v", record)
	}
}

func TestSpotMarginBorrowHistoryRejectsMissingOrMalformedRawAmount(t *testing.T) {
	for _, rawAmount := range []string{"", "not-a-number"} {
		t.Run(fmt.Sprintf("amount_%q", rawAmount), func(t *testing.T) {
			client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint("https://margin.test")
			client.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body := fmt.Sprintf(`{"rows":[{"txId":7001,"asset":"BTC","amount":%q,"status":"CONFIRMED","timestamp":1790503200000}],"total":1}`, rawAmount)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
			})}
			adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: &BinanceSpotAdapter{client: client}, marginClient: NewMarginClient(client)}
			if records, _, err := adapter.GetMarginBorrowHistory(context.Background(), "BTC", 1790503199000, 1790503200000, 1, 100); err == nil || records != nil {
				t.Fatalf("invalid raw amount accepted: records=%+v err=%v", records, err)
			}
		})
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
		records[0].RawAmount != "0.399" || records[0].RawPrincipal != "0.398" || records[0].RawInterest != "0.001" ||
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
	if record.TransferID != transactionID || record.Asset != "BTC" || record.Amount != 0.4005 || record.Principal != 0.4 || record.Interest != 0.0005 ||
		record.RawAmount != "0.4005" || record.RawPrincipal != "0.4" || record.RawInterest != "0.0005" || record.Status != "CONFIRMED" || record.Timestamp != transactionTime {
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
	if records[0].RawPrincipal != "0.4" || records[0].RawInterest != "0.0001" || records[0].RawRate != "0.00025" {
		t.Fatalf("raw interest source values were not preserved: %+v", records[0])
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
		`{"rows":[{"txId":8001,"interestAccuredTime":1790503199500,"asset":"BTC","principal":"","interest":"0.0001","interestRate":"0.00025","type":"PERIODIC"}],"total":1}`,
		`{"rows":[{"txId":8001,"interestAccuredTime":1790503199500,"asset":"BTC","principal":"0.4","interest":"bad","interestRate":"0.00025","type":"PERIODIC"}],"total":1}`,
		`{"rows":[{"txId":8001,"interestAccuredTime":1790503199500,"asset":"BTC","principal":"0.4","interest":"0.0001","interestRate":"","type":"PERIODIC"}],"total":1}`,
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

func TestBinanceOrderFillsPreserveRawDecimalsAndExplicitZeroCommission(t *testing.T) {
	const (
		symbol      = "BTCUSDT"
		orderID     = int64(71)
		tradeID     = int64(9)
		tradeTime   = int64(1790503200000)
		priceRaw    = "123.4500"
		quantityRaw = "0.006700"
		quoteRaw    = "0.82711500"
		commission  = "0.00000000"
		pnlRaw      = "0.00000000"
	)
	tests := []struct {
		name string
		path string
		new  func(string) func() ([]*OrderFill, error)
		pnl  bool
	}{
		{
			name: "futures",
			path: "/fapi/v1/userTrades",
			new: func(endpoint string) func() ([]*OrderFill, error) {
				client := futuresSDK.NewClient("api-key", "api-secret").SetApiEndpoint(endpoint)
				adapter := &BinanceAdapter{client: client, apiKey: "api-key", secretKey: "api-secret"}
				return func() ([]*OrderFill, error) { return adapter.GetOrderFills(context.Background(), symbol, orderID) }
			},
			pnl: true,
		},
		{
			name: "spot",
			path: "/api/v3/myTrades",
			new: func(endpoint string) func() ([]*OrderFill, error) {
				client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint(endpoint)
				adapter := &BinanceSpotAdapter{client: client, symbol: symbol, apiKey: "api-key", secretKey: "api-secret", baseAsset: "BTC", quoteAsset: "USDT"}
				return func() ([]*OrderFill, error) { return adapter.GetOrderFills(context.Background(), symbol, orderID) }
			},
		},
		{
			name: "cross margin",
			path: "/sapi/v1/margin/myTrades",
			new: func(endpoint string) func() ([]*OrderFill, error) {
				client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint(endpoint)
				spot := &BinanceSpotAdapter{client: client, symbol: symbol, apiKey: "api-key", secretKey: "api-secret", baseAsset: "BTC", quoteAsset: "USDT"}
				adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: spot, marginClient: NewMarginClient(client)}
				return func() ([]*OrderFill, error) { return adapter.GetOrderFills(context.Background(), symbol, orderID) }
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path && !(tt.name == "cross margin" && r.URL.Path == "/sapi/v1/margin/order") {
					t.Errorf("unexpected endpoint %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if tt.name == "cross margin" && r.URL.Path == "/sapi/v1/margin/order" {
					_, _ = fmt.Fprintf(w, `{"symbol":%q,"orderId":%d,"price":%q,"origQty":%q,"executedQty":%q,"cummulativeQuoteQty":%q,"status":"FILLED","time":%d,"updateTime":%d,"side":"BUY"}`,
						symbol, orderID, priceRaw, quantityRaw, quantityRaw, quoteRaw, tradeTime, tradeTime)
					return
				}
				commissionField := fmt.Sprintf(`"commission":%q,"commissionAsset":"USDT",`, commission)
				if tt.name == "futures" {
					_, _ = fmt.Fprintf(w, `[{"id":%d,"symbol":%q,"orderId":%d,"price":%q,"qty":%q,"quoteQty":%q,%s"realizedPnl":%q,"time":%d,"side":"BUY","maker":true,"buyer":true}]`,
						tradeID, symbol, orderID, priceRaw, quantityRaw, quoteRaw, commissionField, pnlRaw, tradeTime)
					return
				}
				_, _ = fmt.Fprintf(w, `[{"id":%d,"symbol":%q,"orderId":%d,"price":%q,"qty":%q,"quoteQty":%q,%s"time":%d,"isBuyer":true,"isMaker":true}]`,
					tradeID, symbol, orderID, priceRaw, quantityRaw, quoteRaw, commissionField, tradeTime)
			}))
			defer server.Close()

			fills, err := tt.new(server.URL)()
			if err != nil {
				t.Fatalf("GetOrderFills: %v", err)
			}
			if len(fills) != 1 {
				t.Fatalf("GetOrderFills returned %d fills, want 1", len(fills))
			}
			fill := fills[0]
			if fill.PriceRaw != priceRaw || fill.QuantityRaw != quantityRaw || fill.QuoteQuantityRaw != quoteRaw || fill.CommissionRaw != commission {
				t.Fatalf("raw decimal evidence changed: price=%q quantity=%q quote=%q commission=%q", fill.PriceRaw, fill.QuantityRaw, fill.QuoteQuantityRaw, fill.CommissionRaw)
			}
			if !fill.CommissionKnown || fill.Commission != 0 || fill.CommissionAsset != "USDT" {
				t.Fatalf("explicit zero commission was not authoritative: known=%v amount=%v asset=%q", fill.CommissionKnown, fill.Commission, fill.CommissionAsset)
			}
			if tt.pnl && (fill.RealizedPnLRaw != pnlRaw || !fill.RealizedPnLKnown || fill.RealizedPnL != 0) {
				t.Fatalf("raw explicit-zero realized PnL not retained: raw=%q known=%v amount=%v", fill.RealizedPnLRaw, fill.RealizedPnLKnown, fill.RealizedPnL)
			}
		})
	}
}

func TestBinanceOrderFillsRejectMissingCommissionField(t *testing.T) {
	const (
		symbol      = "BTCUSDT"
		orderID     = int64(71)
		priceRaw    = "123.4500"
		quantityRaw = "0.006700"
		quoteRaw    = "0.82711500"
		tradeTime   = int64(1790503200000)
	)
	tests := []struct {
		name string
		path string
		new  func(string) func() ([]*OrderFill, error)
	}{
		{
			name: "futures",
			path: "/fapi/v1/userTrades",
			new: func(endpoint string) func() ([]*OrderFill, error) {
				client := futuresSDK.NewClient("api-key", "api-secret").SetApiEndpoint(endpoint)
				adapter := &BinanceAdapter{client: client, apiKey: "api-key", secretKey: "api-secret"}
				return func() ([]*OrderFill, error) { return adapter.GetOrderFills(context.Background(), symbol, orderID) }
			},
		},
		{
			name: "spot",
			path: "/api/v3/myTrades",
			new: func(endpoint string) func() ([]*OrderFill, error) {
				client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint(endpoint)
				adapter := &BinanceSpotAdapter{client: client, symbol: symbol, apiKey: "api-key", secretKey: "api-secret", baseAsset: "BTC", quoteAsset: "USDT"}
				return func() ([]*OrderFill, error) { return adapter.GetOrderFills(context.Background(), symbol, orderID) }
			},
		},
		{
			name: "cross margin",
			path: "/sapi/v1/margin/myTrades",
			new: func(endpoint string) func() ([]*OrderFill, error) {
				client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint(endpoint)
				spot := &BinanceSpotAdapter{client: client, symbol: symbol, apiKey: "api-key", secretKey: "api-secret", baseAsset: "BTC", quoteAsset: "USDT"}
				adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: spot, marginClient: NewMarginClient(client)}
				return func() ([]*OrderFill, error) { return adapter.GetOrderFills(context.Background(), symbol, orderID) }
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.name == "cross margin" && r.URL.Path == "/sapi/v1/margin/order" {
					_, _ = fmt.Fprintf(w, `{"symbol":%q,"orderId":%d,"price":%q,"origQty":%q,"executedQty":%q,"cummulativeQuoteQty":%q,"status":"FILLED","time":%d,"updateTime":%d,"side":"BUY"}`,
						symbol, orderID, priceRaw, quantityRaw, quantityRaw, quoteRaw, tradeTime, tradeTime)
					return
				}
				if r.URL.Path != tt.path {
					t.Errorf("unexpected endpoint %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				if tt.name == "futures" {
					_, _ = fmt.Fprintf(w, `[{"id":9,"symbol":%q,"orderId":%d,"price":%q,"qty":%q,"quoteQty":%q,"commissionAsset":"USDT","realizedPnl":"0.0000","time":%d,"side":"BUY"}]`,
						symbol, orderID, priceRaw, quantityRaw, quoteRaw, tradeTime)
					return
				}
				_, _ = fmt.Fprintf(w, `[{"id":9,"symbol":%q,"orderId":%d,"price":%q,"qty":%q,"quoteQty":%q,"commissionAsset":"USDT","time":%d,"isBuyer":true}]`,
					symbol, orderID, priceRaw, quantityRaw, quoteRaw, tradeTime)
			}))
			defer server.Close()

			if fills, err := tt.new(server.URL)(); err == nil {
				t.Fatalf("missing commission field accepted: fills=%+v", fills)
			}
		})
	}
}

func TestBuildOrderFillBatchRequiresExactIdentityAndDecimalCompleteness(t *testing.T) {
	baseOrder := &Order{OrderID: 71, ClientOrderID: "cid-71", Symbol: "BTCUSDT", Side: SideBuy,
		Status: OrderStatusFilled, UpdateTime: 1790503200000, ExecutedQtyRaw: "0.030000000000000003"}
	baseFill := &OrderFill{OrderID: 71, TradeID: "101", Symbol: "BTCUSDT", Side: SideBuy,
		PriceRaw: "100.00000000000000001", QuantityRaw: "0.01", QuoteQuantityRaw: "1.0000000000000000001",
		CommissionRaw: "0.00000000", CommissionKnown: true, CommissionAsset: "BNB"}
	tests := []struct {
		name             string
		order            func() *Order
		fill             func() []*OrderFill
		historyComplete  bool
		wantComplete     bool
		wantFeesComplete bool
		wantErr          bool
	}{
		{name: "exact raw precision complete", order: func() *Order { return baseOrder }, fill: func() []*OrderFill {
			return []*OrderFill{baseFill, {OrderID: 71, TradeID: "102", Symbol: "BTCUSDT", Side: SideBuy,
				PriceRaw: "100", QuantityRaw: "0.020000000000000003", QuoteQuantityRaw: "2",
				CommissionRaw: "0", CommissionKnown: true, CommissionAsset: "USDT"}}
		}, historyComplete: true, wantComplete: true, wantFeesComplete: true},
		{name: "unknown commission keeps batch complete but fees incomplete", order: func() *Order { return baseOrder }, fill: func() []*OrderFill {
			f := *baseFill
			f.CommissionRaw, f.CommissionKnown, f.CommissionAsset = "", false, ""
			return []*OrderFill{&f, {OrderID: 71, TradeID: "102", Symbol: "BTCUSDT", Side: SideBuy,
				PriceRaw: "100", QuantityRaw: "0.020000000000000003", QuoteQuantityRaw: "2",
				CommissionRaw: "0", CommissionKnown: true, CommissionAsset: "USDT"}}
		}, historyComplete: true, wantComplete: true, wantFeesComplete: false},
		{name: "sum mismatch remains incomplete", order: func() *Order { o := *baseOrder; o.ExecutedQtyRaw = "0.03"; return &o }, fill: func() []*OrderFill {
			return []*OrderFill{baseFill}
		}, historyComplete: true, wantFeesComplete: true},
		{name: "non-terminal remains incomplete", order: func() *Order { o := *baseOrder; o.Status = OrderStatusPartiallyFilled; return &o }, fill: func() []*OrderFill {
			return []*OrderFill{baseFill, {OrderID: 71, TradeID: "102", Symbol: "BTCUSDT", Side: SideBuy,
				PriceRaw: "100", QuantityRaw: "0.020000000000000003", QuoteQuantityRaw: "2",
				CommissionRaw: "0", CommissionKnown: true, CommissionAsset: "USDT"}}
		}, historyComplete: true, wantFeesComplete: true},
		{name: "duplicate trade IDs rejected", order: func() *Order { return baseOrder }, fill: func() []*OrderFill { return []*OrderFill{baseFill, baseFill} }, wantErr: true},
		{name: "wrong order ID rejected", order: func() *Order { return baseOrder }, fill: func() []*OrderFill { f := *baseFill; f.OrderID++; return []*OrderFill{&f} }, wantErr: true},
		{name: "wrong symbol rejected", order: func() *Order { return baseOrder }, fill: func() []*OrderFill { f := *baseFill; f.Symbol = "ETHUSDT"; return []*OrderFill{&f} }, wantErr: true},
		{name: "wrong side rejected", order: func() *Order { return baseOrder }, fill: func() []*OrderFill { f := *baseFill; f.Side = SideSell; return []*OrderFill{&f} }, wantErr: true},
		{name: "unverifiable raw precision rejected", order: func() *Order { o := *baseOrder; o.ExecutedQtyRaw = "3e-2"; return &o }, fill: func() []*OrderFill { return []*OrderFill{baseFill} }, wantErr: true},
		{name: "missing quote raw is not synthesized", order: func() *Order { return baseOrder }, fill: func() []*OrderFill {
			f := *baseFill
			f.QuoteQuantityRaw = ""
			return []*OrderFill{&f, {OrderID: 71, TradeID: "102", Symbol: "BTCUSDT", Side: SideBuy,
				PriceRaw: "100", QuantityRaw: "0.020000000000000003", QuoteQuantityRaw: "2",
				CommissionRaw: "0", CommissionKnown: true, CommissionAsset: "USDT"}}
		}, historyComplete: true, wantComplete: true, wantFeesComplete: true},
		{name: "missing history proof is incomplete", order: func() *Order { return baseOrder }, fill: func() []*OrderFill {
			return []*OrderFill{baseFill, {OrderID: 71, TradeID: "102", Symbol: "BTCUSDT", Side: SideBuy,
				PriceRaw: "100", QuantityRaw: "0.020000000000000003", QuoteQuantityRaw: "2",
				CommissionRaw: "0", CommissionKnown: true, CommissionAsset: "USDT"}}
		}, wantFeesComplete: true},
		{name: "zero-execution empty terminal snapshot is complete", order: func() *Order { o := *baseOrder; o.ExecutedQtyRaw = "0.000"; return &o },
			fill: func() []*OrderFill { return nil }, historyComplete: true, wantComplete: true, wantFeesComplete: true},
		{name: "positive execution without fills is incomplete", order: func() *Order { o := *baseOrder; o.ExecutedQtyRaw = "0.01"; return &o },
			fill: func() []*OrderFill { return nil }, historyComplete: true, wantFeesComplete: true},
		{name: "missing update time rejected", order: func() *Order { o := *baseOrder; o.UpdateTime = 0; return &o },
			fill: func() []*OrderFill { return []*OrderFill{baseFill} }, historyComplete: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batch, err := buildOrderFillBatch(tt.order(), tt.fill(), tt.historyComplete)
			if (err != nil) != tt.wantErr {
				t.Fatalf("buildOrderFillBatch() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if batch.Complete != tt.wantComplete {
				t.Fatalf("Complete = %t, want %t", batch.Complete, tt.wantComplete)
			}
			if batch.FeesComplete != tt.wantFeesComplete {
				t.Fatalf("FeesComplete = %t, want %t", batch.FeesComplete, tt.wantFeesComplete)
			}
			if batch.SourceRevision == "" {
				t.Fatal("SourceRevision is empty")
			}
		})
	}
}

func TestBinanceBatchSourceRevisionBindsOrderAndRawFillSnapshot(t *testing.T) {
	order := &Order{OrderID: 71, ClientOrderID: "cid-71", Symbol: "BTCUSDT", Side: SideBuy,
		Status: OrderStatusFilled, UpdateTime: 1790503200000, ExecutedQtyRaw: "0.01"}
	fill := &OrderFill{OrderID: 71, TradeID: "1", Symbol: "BTCUSDT", Side: SideBuy,
		PriceRaw: "10.00", QuantityRaw: "0.01", QuoteQuantityRaw: "0.1",
		CommissionRaw: "0", CommissionKnown: true, CommissionAsset: "USDT"}
	base, err := buildOrderFillBatch(order, []*OrderFill{fill}, true)
	if err != nil {
		t.Fatalf("build base batch: %v", err)
	}
	orderChanged := *order
	orderChanged.UpdateTime++
	changedOrderBatch, err := buildOrderFillBatch(&orderChanged, []*OrderFill{fill}, true)
	if err != nil {
		t.Fatalf("build changed order batch: %v", err)
	}
	fillChanged := *fill
	fillChanged.PriceRaw = "10.000"
	changedFillBatch, err := buildOrderFillBatch(order, []*OrderFill{&fillChanged}, true)
	if err != nil {
		t.Fatalf("build changed fill batch: %v", err)
	}
	if base.SourceRevision == changedOrderBatch.SourceRevision || base.SourceRevision == changedFillBatch.SourceRevision {
		t.Fatalf("source revision did not bind order updateTime/raw fills: base=%s changedOrder=%s changedFill=%s",
			base.SourceRevision, changedOrderBatch.SourceRevision, changedFillBatch.SourceRevision)
	}
}

func TestBinanceOrderFillBatchIsCompleteOnlyFromTerminalExactSnapshots(t *testing.T) {
	const (
		symbol    = "BTCUSDT"
		orderID   = int64(71)
		rawQty    = "0.006700000000000001"
		tradeTime = int64(1790503200000)
	)
	tests := []struct {
		name      string
		orderPath string
		fillPath  string
		new       func(string, *http.Client) func() (*OrderFillBatch, error)
	}{
		{
			name: "futures", orderPath: "/fapi/v1/order", fillPath: "/fapi/v1/userTrades",
			new: func(endpoint string, httpClient *http.Client) func() (*OrderFillBatch, error) {
				client := futuresSDK.NewClient("api-key", "api-secret").SetApiEndpoint(endpoint)
				client.HTTPClient = httpClient
				adapter := &BinanceAdapter{client: client, apiKey: "api-key", secretKey: "api-secret"}
				return func() (*OrderFillBatch, error) {
					return adapter.GetOrderFillBatch(context.Background(), symbol, orderID)
				}
			},
		},
		{
			name: "spot", orderPath: "/api/v3/order", fillPath: "/api/v3/myTrades",
			new: func(endpoint string, httpClient *http.Client) func() (*OrderFillBatch, error) {
				client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint(endpoint)
				client.HTTPClient = httpClient
				adapter := &BinanceSpotAdapter{client: client, symbol: symbol, apiKey: "api-key", secretKey: "api-secret", baseAsset: "BTC", quoteAsset: "USDT"}
				return func() (*OrderFillBatch, error) {
					return adapter.GetOrderFillBatch(context.Background(), symbol, orderID)
				}
			},
		},
		{
			name: "cross margin", orderPath: "/sapi/v1/margin/order", fillPath: "/sapi/v1/margin/myTrades",
			new: func(endpoint string, httpClient *http.Client) func() (*OrderFillBatch, error) {
				client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint(endpoint)
				client.HTTPClient = httpClient
				spot := &BinanceSpotAdapter{client: client, symbol: symbol, apiKey: "api-key", secretKey: "api-secret", baseAsset: "BTC", quoteAsset: "USDT"}
				adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: spot, marginClient: NewMarginClient(client)}
				return func() (*OrderFillBatch, error) {
					return adapter.GetOrderFillBatch(context.Background(), symbol, orderID)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := ""
				if r.URL.Path == tt.orderPath {
					if tt.name == "futures" {
						body = fmt.Sprintf(`{"symbol":%q,"orderId":%d,"clientOrderId":"cid-71","price":"100","origQty":"1","executedQty":%q,"cumQuote":"0.67","status":"FILLED","side":"BUY","type":"LIMIT","updateTime":%d}`, symbol, orderID, rawQty, tradeTime)
					} else {
						body = fmt.Sprintf(`{"symbol":%q,"orderId":%d,"clientOrderId":"cid-71","price":"100","origQty":"1","executedQty":%q,"cummulativeQuoteQty":"0.67","status":"FILLED","side":"BUY","type":"LIMIT","time":%d,"updateTime":%d}`, symbol, orderID, rawQty, tradeTime, tradeTime)
					}
				} else if r.URL.Path == tt.fillPath {
					if tt.name == "futures" {
						body = fmt.Sprintf(`[{"id":9,"symbol":%q,"orderId":%d,"price":"100.00000000000000001","qty":%q,"quoteQty":"0.670000000000000001","commission":"0","commissionAsset":"USDT","realizedPnl":"0","time":%d,"side":"BUY","maker":false}]`, symbol, orderID, rawQty, tradeTime)
					} else {
						trade := fmt.Sprintf(`{"id":9,"symbol":%q,"orderId":%d,"price":"100.00000000000000001","qty":%q,"quoteQty":"0.670000000000000001","commission":"0","commissionAsset":"USDT","time":%d,"isBuyer":true,"isMaker":false}`, symbol, orderID, rawQty, tradeTime)
						body = fmt.Sprintf("[%s]", trade)
					}
				} else {
					return nil, fmt.Errorf("unexpected endpoint %s", r.URL.Path)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}

			batch, err := tt.new("https://mock.binance", httpClient)()
			if err != nil {
				t.Fatalf("GetOrderFillBatch: %v", err)
			}
			if !batch.Complete || batch.OrderID != orderID || batch.Symbol != symbol || batch.Side != SideBuy ||
				batch.ClientOrderID != "cid-71" || batch.OrderUpdateTime != tradeTime || !batch.FillHistoryComplete ||
				batch.TerminalStatus != OrderStatusFilled || batch.ExecutedQuantityRaw != rawQty || batch.SourceRevision == "" || !batch.FeesComplete {
				t.Fatalf("batch is not authoritative and complete: %+v", batch)
			}
			if len(batch.Fills) != 1 || batch.Fills[0].QuantityRaw != rawQty || batch.Fills[0].PriceRaw != "100.00000000000000001" ||
				batch.Fills[0].CommissionRaw != "0" || !batch.Fills[0].CommissionKnown || batch.Fills[0].CommissionAsset != "USDT" {
				t.Fatalf("raw fill evidence was not preserved: %+v", batch.Fills)
			}
		})
	}
}

func TestBinanceFuturesAndSpotFullPerOrderPageFailsClosed(t *testing.T) {
	const symbol = "BTCUSDT"
	tests := []struct {
		name string
		path string
		new  func(*http.Client) func() error
	}{
		{
			name: "futures", path: "/fapi/v1/userTrades",
			new: func(httpClient *http.Client) func() error {
				client := futuresSDK.NewClient("api-key", "api-secret").SetApiEndpoint("https://mock.binance")
				client.HTTPClient = httpClient
				adapter := &BinanceAdapter{client: client, apiKey: "api-key", secretKey: "api-secret"}
				return func() error { _, err := adapter.GetOrderFillBatch(context.Background(), symbol, 71); return err }
			},
		},
		{
			name: "spot", path: "/api/v3/myTrades",
			new: func(httpClient *http.Client) func() error {
				client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint("https://mock.binance")
				client.HTTPClient = httpClient
				adapter := &BinanceSpotAdapter{client: client, symbol: symbol, apiKey: "api-key", secretKey: "api-secret"}
				return func() error { _, err := adapter.GetOrderFillBatch(context.Background(), symbol, 71); return err }
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload strings.Builder
			payload.WriteByte('[')
			for index := 0; index < 1000; index++ {
				if index > 0 {
					payload.WriteByte(',')
				}
				_, _ = fmt.Fprintf(&payload, `{"id":%d,"symbol":%q,"orderId":71,"price":"1","qty":"1","quoteQty":"1","commission":"0","commissionAsset":"USDT","realizedPnl":"0","time":1,"side":"BUY","isBuyer":true}`, index+1, symbol)
			}
			payload.WriteByte(']')
			httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path != tt.path {
					return nil, fmt.Errorf("unexpected endpoint %s", request.URL.Path)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload.String())), Request: request}, nil
			})}
			if err := tt.new(httpClient)(); err == nil || !strings.Contains(err.Error(), "completeness is unknown") {
				t.Fatalf("full 1000-row page error = %v, want fail-closed truncation error", err)
			}
		})
	}
}

func TestBinanceMarginFullMaxPagesFailsClosed(t *testing.T) {
	const symbol = "BTCUSDT"
	client := sdk.NewClient("api-key", "api-secret").SetApiEndpoint("https://mock.binance")
	var tradePages int
	client.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := ""
		switch request.URL.Path {
		case "/sapi/v1/margin/order":
			body = `{"symbol":"BTCUSDT","orderId":71,"price":"1","origQty":"1","executedQty":"0","cummulativeQuoteQty":"0","status":"FILLED","side":"BUY","time":1790503200000,"updateTime":1790503200000}`
		case "/sapi/v1/margin/myTrades":
			tradePages++
			firstID := int64(1)
			if fromID := request.URL.Query().Get("fromId"); fromID != "" {
				parsed, err := strconv.ParseInt(fromID, 10, 64)
				if err != nil {
					return nil, err
				}
				firstID = parsed
			}
			var payload strings.Builder
			payload.WriteByte('[')
			for index := int64(0); index < 1000; index++ {
				if index > 0 {
					payload.WriteByte(',')
				}
				_, _ = fmt.Fprintf(&payload, `{"id":%d,"symbol":"BTCUSDT","orderId":99,"price":"1","qty":"1","quoteQty":"1","commission":"0","commissionAsset":"USDT","time":1790503200000,"isBuyer":true,"isMaker":false}`, firstID+index)
			}
			payload.WriteByte(']')
			body = payload.String()
		default:
			return nil, fmt.Errorf("unexpected endpoint %s", request.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})}
	spot := &BinanceSpotAdapter{client: client, symbol: symbol, apiKey: "api-key", secretKey: "api-secret"}
	adapter := &BinanceSpotMarginAdapter{BinanceSpotAdapter: spot, marginClient: NewMarginClient(client)}
	_, err := adapter.GetOrderFillBatch(context.Background(), symbol, 71)
	if err == nil || !strings.Contains(err.Error(), "exceeded 100 pages") {
		t.Fatalf("GetOrderFillBatch error = %v, want max-pages fail-closed error", err)
	}
	if tradePages != 100 {
		t.Fatalf("margin trade pages = %d, want 100", tradePages)
	}
}

func TestBinanceBatchKeepsUnknownFeeFillAndDistinguishesZeroFill(t *testing.T) {
	tests := []struct {
		name                string
		executedRaw         string
		trades              string
		wantComplete        bool
		wantFillCount       int
		wantFeesComplete    bool
		wantRawQuoteMissing bool
	}{
		{name: "unknown fee retains confirmed fill", executedRaw: "0.010000000000000001",
			trades:       `[{"id":1,"symbol":"BTCUSDT","orderId":71,"price":"10.0000000000000001","qty":"0.010000000000000001","side":"BUY"}]`,
			wantComplete: true, wantFillCount: 1, wantRawQuoteMissing: true},
		{name: "empty zero execution terminal snapshot", executedRaw: "0.000",
			trades: `[]`, wantComplete: true, wantFeesComplete: true},
		{name: "positive execution without fills", executedRaw: "0.01",
			trades: `[]`, wantComplete: false, wantFeesComplete: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := futuresSDK.NewClient("api-key", "api-secret").SetApiEndpoint("https://mock.binance")
			client.HTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := tt.trades
				if request.URL.Path == "/fapi/v1/order" {
					body = fmt.Sprintf(`{"symbol":"BTCUSDT","orderId":71,"clientOrderId":"cid-71","price":"10","origQty":"1","executedQty":%q,"cumQuote":"0","status":"FILLED","side":"BUY","type":"LIMIT","updateTime":1790503200000}`, tt.executedRaw)
				} else if request.URL.Path != "/fapi/v1/userTrades" {
					return nil, fmt.Errorf("unexpected endpoint %s", request.URL.Path)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})}
			adapter := &BinanceAdapter{client: client, apiKey: "api-key", secretKey: "api-secret"}
			batch, err := adapter.GetOrderFillBatch(context.Background(), "BTCUSDT", 71)
			if err != nil {
				t.Fatalf("GetOrderFillBatch: %v", err)
			}
			if batch.Complete != tt.wantComplete || batch.FeesComplete != tt.wantFeesComplete || len(batch.Fills) != tt.wantFillCount {
				t.Fatalf("batch completeness/fills = complete:%t fees:%t count:%d; want %t/%t/%d", batch.Complete,
					batch.FeesComplete, len(batch.Fills), tt.wantComplete, tt.wantFeesComplete, tt.wantFillCount)
			}
			if tt.wantRawQuoteMissing && (batch.Fills[0].QuoteQuantityRaw != "" || batch.Fills[0].CommissionKnown || batch.Fills[0].CommissionAsset != "") {
				t.Fatalf("unknown raw evidence was fabricated: %+v", batch.Fills[0])
			}
		})
	}
}
