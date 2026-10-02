package phemex

import (
	"context"
	"net/http"
	"testing"
)

func TestGetAccountOpenOrdersEnumeratesEveryPerpetualSymbol(t *testing.T) {
	var queried = make(map[string]bool)
	client, closeServer := newMockPhemexClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/public/products":
			_, _ = w.Write([]byte(`{"code":0,"data":{"perpProductsV2":[{"symbol":"BTCUSD","priceScale":4},{"symbol":"ETHUSD","priceScale":2}]}}`))
		case "/g-orders/activeList":
			if r.Header.Get("x-phemex-access-token") != "api-key" || r.Header.Get("x-phemex-request-expiry") == "" || r.Header.Get("x-phemex-request-signature") == "" {
				t.Error("activeList request is missing authentication headers")
			}
			symbol := r.URL.Query().Get("symbol")
			if symbol == "" {
				t.Error("activeList request omitted required symbol")
			}
			queried[symbol] = true
			switch symbol {
			case "BTCUSD":
				_, _ = w.Write([]byte(`{"code":0,"data":{"rows":[{"orderID":"btc-order","symbol":"BTCUSD","side":"Buy","ordStatus":"Untriggered","orderQty":2,"cumQty":0,"priceEp":650000000}]}}`))
			case "ETHUSD":
				_, _ = w.Write([]byte(`{"code":0,"data":{"rows":[{"orderID":"eth-order","symbol":"ETHUSD","side":"Sell","ordStatus":"PartiallyFilled","orderQty":3,"cumQty":1,"priceEp":250000}]}}`))
			default:
				t.Errorf("unexpected symbol query %q", symbol)
				w.WriteHeader(http.StatusBadRequest)
			}
		default:
			t.Errorf("unexpected endpoint %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer closeServer()

	adapter := &Adapter{client: client, priceScale: 4}
	orders, err := adapter.GetAccountOpenOrders(context.Background())
	if err != nil {
		t.Fatalf("GetAccountOpenOrders(): %v", err)
	}
	if len(orders) != 2 || !queried["BTCUSD"] || !queried["ETHUSD"] {
		t.Fatalf("orders = %#v, queried = %#v", orders, queried)
	}
	if orders[0].Symbol == orders[1].Symbol {
		t.Fatalf("account snapshot did not retain both symbols: %#v", orders)
	}
	for _, order := range orders {
		if order.Status != OrderStatusNew && order.Status != OrderStatusPartiallyFilled {
			t.Errorf("order %s mapped to unexpected open status %q", order.OrderID, order.Status)
		}
	}
}

func TestGetAccountOpenOrdersRejectsIncompleteCatalogAndRows(t *testing.T) {
	tests := []struct {
		name      string
		catalog   string
		orderRows string
		wantErr   bool
	}{
		{name: "missing catalog", catalog: `{"code":0,"data":{}}`, wantErr: true},
		{name: "empty catalog", catalog: `{"code":0,"data":{"perpProductsV2":[]}}`, wantErr: true},
		{name: "missing order rows", catalog: `{"code":0,"data":{"perpProductsV2":[{"symbol":"BTCUSD","priceScale":4}]}}`, wantErr: true},
		{name: "invalid order identity", catalog: `{"code":0,"data":{"perpProductsV2":[{"symbol":"BTCUSD","priceScale":4}]}}`, orderRows: `{"rows":[{"symbol":"BTCUSD","side":"Buy","ordStatus":"New","orderQty":1}]}`, wantErr: true},
		{name: "unknown order status", catalog: `{"code":0,"data":{"perpProductsV2":[{"symbol":"BTCUSD","priceScale":4}]}}`, orderRows: `{"rows":[{"orderID":"x","symbol":"BTCUSD","side":"Buy","ordStatus":"Mystery","orderQty":1}]}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, closeServer := newMockPhemexClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/public/products" {
					_, _ = w.Write([]byte(tt.catalog))
					return
				}
				if tt.orderRows == "" {
					_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
					return
				}
				var body = tt.orderRows
				_, _ = w.Write([]byte(`{"code":0,"data":` + body + `}`))
			})
			defer closeServer()

			if _, err := (&Adapter{client: client}).GetAccountOpenOrders(context.Background()); tt.wantErr && err == nil {
				t.Fatal("GetAccountOpenOrders() accepted incomplete or invalid evidence")
			} else if !tt.wantErr && err != nil {
				t.Fatalf("GetAccountOpenOrders(): %v", err)
			}
		})
	}
}
