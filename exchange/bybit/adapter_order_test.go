package bybit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

var btcLinear = Instrument{
	Symbol: "BTCUSDT", BaseCoin: "BTC", QuoteCoin: "USDT",
	PriceFilter:   PriceFilter{TickSize: "0.1"},
	LotSizeFilter: LotSizeFilter{QtyStep: "0.001", MinOrderQty: "0.001"},
}

func newTestBybitAdapter(t *testing.T, baseURL string) *BybitAdapter {
	t.Helper()
	c := NewBybitClient("k", "s", false)
	c.baseURL = baseURL
	b := &BybitAdapter{client: c, symbol: "BTCUSDT"}
	if err := b.applyInstrument(btcLinear); err != nil {
		t.Fatalf("applyInstrument: %v", err)
	}
	return b
}

type bybitFakeServer struct {
	mu          sync.Mutex
	positionIdx string
	orderBody   map[string]interface{}
	orders      int
}

func (f *bybitFakeServer) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/v5/position/list":
		list := `[{"symbol":"BTCUSDT","size":"0","positionIdx":` + f.positionIdx + `,"tradeMode":0}]`
		if f.positionIdx != "0" {
			list = `[{"symbol":"BTCUSDT","size":"0","positionIdx":1,"tradeMode":0},{"symbol":"BTCUSDT","size":"0","positionIdx":2,"tradeMode":0}]`
		}
		_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"list":`+list+`}}`)
	case "/v5/order/create":
		f.orders++
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &f.orderBody)
		_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"orderId":"42","orderLinkId":"c1"}}`)
	default:
		http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
	}
}

func TestBybitPlaceOrderFloorsQtyAndAlignsPrice(t *testing.T) {
	tests := []struct {
		name      string
		req       OrderRequest
		wantQty   string
		wantPrice interface{}
		wantTIF   interface{}
	}{
		{"買單價格向下", OrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, PostOnly: true, Quantity: 0.0129, Price: 70000.19}, "0.012", "70000.1", "PostOnly"},
		{"賣單價格向上", OrderRequest{Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeLimit, Quantity: 0.003, Price: 70000.11}, "0.003", "70000.2", "GTC"},
		{"市價單不帶價格", OrderRequest{Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeMarket, Quantity: 0.0021, ReduceOnly: true}, "0.002", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &bybitFakeServer{positionIdx: "0"}
			srv := httptest.NewServer(http.HandlerFunc(fake.handler))
			defer srv.Close()
			b := newTestBybitAdapter(t, srv.URL)

			req := tt.req
			if _, err := b.PlaceOrder(context.Background(), &req); err != nil {
				t.Fatalf("PlaceOrder: %v", err)
			}
			if fake.orderBody["qty"] != tt.wantQty {
				t.Fatalf("qty=%v want %v", fake.orderBody["qty"], tt.wantQty)
			}
			if fake.orderBody["price"] != tt.wantPrice {
				t.Fatalf("price=%v want %v", fake.orderBody["price"], tt.wantPrice)
			}
			if fake.orderBody["timeInForce"] != tt.wantTIF {
				t.Fatalf("timeInForce=%v want %v", fake.orderBody["timeInForce"], tt.wantTIF)
			}
		})
	}
}

func TestBybitPlaceOrderRejectsBelowMinQty(t *testing.T) {
	fake := &bybitFakeServer{positionIdx: "0"}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	b := newTestBybitAdapter(t, srv.URL)

	_, err := b.PlaceOrder(context.Background(), &OrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, Quantity: 0.0009, Price: 70000})
	if err == nil || !strings.Contains(err.Error(), "最小下單量") {
		t.Fatalf("期望最小下單量錯誤，得到 %v", err)
	}
	if fake.orders != 0 {
		t.Fatal("不應發出下單請求（舊邏輯會靜默放大數量）")
	}
}

func TestBybitPlaceOrderRefusesHedgeMode(t *testing.T) {
	fake := &bybitFakeServer{positionIdx: "1"}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	b := newTestBybitAdapter(t, srv.URL)

	_, err := b.PlaceOrder(context.Background(), &OrderRequest{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, Quantity: 0.01, Price: 70000})
	if err == nil || !strings.Contains(err.Error(), "雙向持倉") {
		t.Fatalf("期望雙向持倉錯誤，得到 %v", err)
	}
	if fake.orders != 0 {
		t.Fatal("雙向持倉模式下不應發出下單請求")
	}
}

func TestBybitPlaceOrderRejectsNonNativeSide(t *testing.T) {
	b := newTestBybitAdapter(t, "http://unused")
	if _, err := b.PlaceOrder(context.Background(), &OrderRequest{Side: Side("BUY"), Type: OrderTypeLimit, Quantity: 1, Price: 1}); err == nil {
		t.Fatal("未映射的 BUY 應被拒絕")
	}
}

func TestBybitMapping(t *testing.T) {
	if s, err := ToNativeSide("SELL"); err != nil || s != SideSell {
		t.Fatalf("ToNativeSide(SELL)=%v,%v", s, err)
	}
	if typ, err := ToNativeOrderType("MARKET"); err != nil || typ != OrderTypeMarket {
		t.Fatalf("ToNativeOrderType(MARKET)=%v,%v", typ, err)
	}
	if _, err := ToNativeOrderType("STOP"); err == nil {
		t.Fatal("未知類型應報錯")
	}
	if ToNativeTimeInForce(false, "GTX") != TimeInForcePO || ToNativeTimeInForce(false, "GTC") != TimeInForceGTC {
		t.Fatal("TimeInForce 映射錯誤")
	}
	statuses := map[OrderStatus]string{
		"New": "NEW", "Untriggered": "NEW", "PartiallyFilled": "PARTIALLY_FILLED", "Filled": "FILLED",
		"Cancelled": "CANCELED", "PartiallyFilledCanceled": "CANCELED", "Deactivated": "CANCELED", "Rejected": "REJECTED",
	}
	for native, want := range statuses {
		if got, err := ToInternalStatus(native); err != nil || got != want {
			t.Fatalf("ToInternalStatus(%s)=%v,%v want %s", native, got, err, want)
		}
	}
	if _, err := ToInternalStatus("FILLED"); err == nil {
		t.Fatal("非原生狀態不應透傳")
	}
	if _, err := ToInternalSide("buy"); err == nil {
		t.Fatal("非原生方向不應透傳")
	}
}

func TestBybitNormalizeAndCategoryFilter(t *testing.T) {
	w := NewWebSocketManager("k", "s", false)
	w.SetOrderCategory(bybitCategoryLinear)
	var got []StreamOrderUpdate
	w.orderCallback = func(u OrderUpdate) {
		n, err := normalizeOrderUpdate(u)
		if err != nil {
			t.Errorf("normalize: %v", err)
			return
		}
		got = append(got, n)
	}
	msg := []byte(`{"topic":"order","data":[
		{"category":"spot","symbol":"BTCUSDT","orderId":"1","side":"Buy","orderType":"Limit","orderStatus":"Filled"},
		{"category":"linear","symbol":"BTCUSDT","orderId":"2","side":"Sell","orderType":"Market","orderStatus":"PartiallyFilled","cumExecQty":"0.001"}]}`)
	w.handleMessage(msg)
	if len(got) != 1 {
		t.Fatalf("現貨推送應被過濾，得到 %d 條", len(got))
	}
	if got[0].OrderID != 2 || got[0].Side != "SELL" || got[0].Type != "MARKET" || got[0].Status != "PARTIALLY_FILLED" {
		t.Fatalf("unexpected %+v", got[0])
	}
	if _, err := normalizeOrderUpdate(OrderUpdate{Side: SideBuy, Type: OrderTypeLimit, Status: "Weird"}); err == nil {
		t.Fatal("未知狀態應報錯")
	}
}
