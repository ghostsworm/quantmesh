package bitget

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func accountPositionClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient("fixture-key", "fixture-secret", "fixture-pass", false)
	client.baseURL = server.URL
	client.httpClient = server.Client()
	return client
}

func TestGetAccountFuturesPositionsRequiresExplicitAllProductSnapshots(t *testing.T) {
	products := make(map[string]int)
	client := accountPositionClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/mix/position/all-position" || r.URL.Query().Get("symbol") != "" {
			t.Errorf("unexpected account position request: %s", r.URL.String())
		}
		product := r.URL.Query().Get("productType")
		products[product]++
		_, _ = fmt.Fprint(w, `{"code":"00000","data":[]}`)
	})

	positions, err := client.GetAccountFuturesPositions(context.Background())
	if err != nil || positions == nil || len(positions) != 0 {
		t.Fatalf("explicit flat snapshots must return a non-nil empty result: positions=%+v err=%v", positions, err)
	}
	for _, product := range accountFuturesProductTypes {
		if products[product] != 1 {
			t.Errorf("product %s queried %d times, want once", product, products[product])
		}
	}
}

func TestReadOnlyAccountEvidenceAdapterExposesAccountWidePositions(t *testing.T) {
	client := accountPositionClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"code":"00000","data":[]}`)
	})
	adapter, err := NewBitgetAccountEvidenceAdapter("fixture-key", "fixture-secret", "fixture-pass", false)
	if err != nil {
		t.Fatal(err)
	}
	adapter.client = client
	var reader AccountFuturesPositionReader = adapter
	positions, err := reader.GetAccountFuturesPositions(context.Background())
	if err != nil || positions == nil || len(positions) != 0 {
		t.Fatalf("read-only adapter position evidence=%+v err=%v", positions, err)
	}
}

func TestReadAccountFuturesFlatnessEvidenceCombinesCompletePositionsAndOrders(t *testing.T) {
	for _, tt := range []struct {
		name      string
		position  bool
		openOrder bool
		wantFlat  bool
	}{
		{name: "flat", wantFlat: true},
		{name: "tiny position", position: true},
		{name: "open order", openOrder: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := accountPositionClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/mix/position/all-position":
					if tt.position && r.URL.Query().Get("productType") == "USDC-FUTURES" {
						_, _ = fmt.Fprint(w, `{"code":"00000","data":[{"symbol":"BTCUSDC","marginCoin":"USDC","holdSide":"long","total":"0.000000000000000001"}]}`)
					} else {
						_, _ = fmt.Fprint(w, `{"code":"00000","data":[]}`)
					}
				case "/api/v2/mix/order/orders-pending":
					if tt.openOrder && r.URL.Query().Get("productType") == "USDT-FUTURES" && r.URL.Query().Get("status") == "live" {
						_, _ = fmt.Fprint(w, `{"code":"00000","data":{"entrustedList":[{"orderId":"123","symbol":"BTCUSDT","size":"1","baseVolume":"0","price":"50000","priceAvg":"0","side":"buy","status":"live","uTime":"1760000000000"}],"endId":""}}`)
					} else {
						_, _ = fmt.Fprint(w, `{"code":"00000","data":{"entrustedList":[],"endId":""}}`)
					}
				default:
					t.Errorf("unexpected request path %q", r.URL.Path)
					http.NotFound(w, r)
				}
			})
			adapter, err := NewBitgetAccountEvidenceAdapter("fixture-key", "fixture-secret", "fixture-pass", false)
			if err != nil {
				t.Fatal(err)
			}
			adapter.client = client
			evidence, err := adapter.ReadAccountFuturesFlatnessEvidence(context.Background())
			if err != nil {
				t.Fatalf("ReadAccountFuturesFlatnessEvidence: %v", err)
			}
			flat, err := evidence.IsFlat()
			if err != nil || flat != tt.wantFlat {
				t.Fatalf("flat=%v want=%v evidence=%+v err=%v", flat, tt.wantFlat, evidence, err)
			}
			if !evidence.Complete || evidence.ObservedAt.IsZero() {
				t.Fatalf("successful evidence missing completion metadata: %+v", evidence)
			}
		})
	}
}

func TestReadAccountFuturesFlatnessEvidenceRejectsIncompleteOpenOrders(t *testing.T) {
	client := accountPositionClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v2/mix/position/all-position" {
			_, _ = fmt.Fprint(w, `{"code":"00000","data":[]}`)
			return
		}
		if r.URL.Query().Get("productType") == "USDC-FUTURES" && r.URL.Query().Get("status") == "live" {
			_, _ = fmt.Fprint(w, `{"code":"00000","data":{"entrustedList":null,"endId":""}}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"code":"00000","data":{"entrustedList":[],"endId":""}}`)
	})
	adapter, err := NewBitgetAccountEvidenceAdapter("fixture-key", "fixture-secret", "fixture-pass", false)
	if err != nil {
		t.Fatal(err)
	}
	adapter.client = client
	evidence, err := adapter.ReadAccountFuturesFlatnessEvidence(context.Background())
	if err == nil || evidence.Complete {
		t.Fatalf("incomplete open-order evidence accepted as complete: evidence=%+v err=%v", evidence, err)
	}
}

func TestGetAccountFuturesPositionsPreservesTinyNonzeroQuantity(t *testing.T) {
	client := accountPositionClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("productType") == "USDT-FUTURES" {
			_, _ = fmt.Fprint(w, `{"code":"00000","data":[{"symbol":"BTCUSDT","marginCoin":"USDT","holdSide":"short","total":"0.000000000000000001"}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"code":"00000","data":[]}`)
	})

	positions, err := client.GetAccountFuturesPositions(context.Background())
	if err != nil || len(positions) != 1 || positions[0].Total != "0.000000000000000001" {
		t.Fatalf("tiny position was not preserved exactly: positions=%+v err=%v", positions, err)
	}
	open, err := positions[0].HasOpenQuantity()
	if err != nil || !open {
		t.Fatalf("tiny nonzero position reported flat: open=%v err=%v", open, err)
	}
}

func TestGetAccountFuturesPositionsRejectsMissingNullMalformedAndFailedProducts(t *testing.T) {
	tests := []struct {
		name string
		usdt string
		usdc string
		coin string
	}{
		{name: "missing data", usdt: `{"code":"00000"}`},
		{name: "null data", usdt: `{"code":"00000","data":null}`},
		{name: "malformed row", usdt: `{"code":"00000","data":[{"symbol":"BTCUSDT","marginCoin":"USDT","holdSide":"long","total":"NaN"}]}`},
		{name: "second product failure", usdt: `{"code":"00000","data":[]}`, usdc: `{"code":"40001","msg":"unavailable"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := accountPositionClient(t, func(w http.ResponseWriter, r *http.Request) {
				body := map[string]string{"USDT-FUTURES": tt.usdt, "USDC-FUTURES": tt.usdc, "COIN-FUTURES": tt.coin}[r.URL.Query().Get("productType")]
				if body == "" {
					body = `{"code":"00000","data":[]}`
				}
				_, _ = fmt.Fprint(w, body)
			})
			positions, err := client.GetAccountFuturesPositions(context.Background())
			if err == nil || positions != nil {
				t.Fatalf("incomplete account snapshot accepted: positions=%+v err=%v", positions, err)
			}
		})
	}
}

func TestAccountFuturesPositionHasOpenQuantityUsesExactDecimal(t *testing.T) {
	for _, tt := range []struct {
		quantity string
		wantOpen bool
	}{{"0", false}, {"0.000000000000000001", true}, {"invalid", false}, {"-1", false}} {
		open, err := (AccountFuturesPosition{Total: tt.quantity}).HasOpenQuantity()
		if err != nil {
			if tt.quantity == "invalid" || tt.quantity == "-1" {
				continue
			}
			t.Fatalf("quantity %q: %v", tt.quantity, err)
		}
		if open != tt.wantOpen {
			t.Errorf("quantity %q open=%v want=%v", tt.quantity, open, tt.wantOpen)
		}
	}
}
