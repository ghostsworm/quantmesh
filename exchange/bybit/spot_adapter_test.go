package bybit

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var btcSpot = Instrument{
	Symbol: "BTCUSDT", BaseCoin: "BTC", QuoteCoin: "USDT",
	PriceFilter:   PriceFilter{TickSize: "0.01"},
	LotSizeFilter: LotSizeFilter{BasePrecision: "0.000001", QuotePrecision: "0.00000001", MinOrderQty: "0.000048", MinOrderAmt: "5"},
}

func newTestBybitSpotAdapter(t *testing.T, baseURL string) *BybitSpotAdapter {
	t.Helper()
	c := NewBybitClient("k", "s", false)
	c.baseURL = baseURL
	b := &BybitSpotAdapter{client: c, symbol: "BTCUSDT"}
	if err := b.applySpotInstrument(btcSpot); err != nil {
		t.Fatalf("applySpotInstrument: %v", err)
	}
	return b
}

func TestBybitSpotAccountOpenOrdersQueriesAllSymbols(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != "/v5/order/realtime" || query.Get("category") != "spot" || query.Has("symbol") {
			t.Errorf("unexpected account-wide spot order query: %s", r.URL.RequestURI())
		}
		_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"list":[{"orderId":"102","orderLinkId":"external-spot","symbol":"ETHUSDT","side":"Sell","orderType":"Limit","price":"10","qty":"1","cumExecQty":"0","orderStatus":"New"}],"nextPageCursor":""}}`))
	}))
	defer server.Close()
	adapter := newTestBybitSpotAdapter(t, server.URL)
	orders, err := adapter.GetAccountOpenOrders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].Symbol != "ETHUSDT" || orders[0].OrderID != 102 {
		t.Fatalf("account-wide spot orders=%+v", orders)
	}
}

type bybitSpotFakeServer struct {
	mu        sync.Mutex
	orderBody map[string]interface{}
	orders    int
	fillsURI  string
	fillsJSON string
}

func (f *bybitSpotFakeServer) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/v5/order/create":
		f.orders++
		body, _ := io.ReadAll(r.Body)
		f.orderBody = nil
		_ = json.Unmarshal(body, &f.orderBody)
		_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"orderId":"42","orderLinkId":"c1"}}`)
	case "/v5/execution/list":
		f.fillsURI = r.URL.RequestURI()
		if f.fillsJSON != "" {
			_, _ = io.WriteString(w, f.fillsJSON)
			return
		}
		_, _ = io.WriteString(w, `{"retCode":0,"retMsg":"OK","result":{"list":[
			{"orderId":"42","symbol":"BTCUSDT","side":"Buy","execPrice":"60000","execQty":"0.01","execFee":"0.00001","feeCurrency":"BTC","isMaker":true,"execTime":"1700000000000","tradeId":"t1"},
			{"orderId":"42","symbol":"BTCUSDT","side":"Sell","execPrice":"60010","execQty":"0.01","execFee":"0.6001","feeCurrency":"USDT","isMaker":false,"execTime":"1700000000001","tradeId":"t2"}]}}`)
	default:
		http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
	}
}

func TestBybitSpotApplyInstrumentUsesBasePrecision(t *testing.T) {
	b := newTestBybitSpotAdapter(t, "http://unused")
	if b.qtyStep != 0.000001 || b.minOrderAmt != 5 || b.GetQuantityDecimals() != 6 || b.GetPriceDecimals() != 2 {
		t.Fatalf("unexpected spec step=%v minAmt=%v qd=%d pd=%d", b.qtyStep, b.minOrderAmt, b.GetQuantityDecimals(), b.GetPriceDecimals())
	}
	bad := btcSpot
	bad.LotSizeFilter.BasePrecision = ""
	if err := (&BybitSpotAdapter{symbol: "BTCUSDT"}).applySpotInstrument(bad); err == nil {
		t.Fatal("缺少 basePrecision/qtyStep 應報錯")
	}
}

func TestBybitSpotPlaceOrderBodyAndRounding(t *testing.T) {
	tests := []struct {
		name   string
		req    OrderRequest
		want   map[string]interface{}
		absent []string
	}{
		{"買單 PostOnly 價格向下、數量向下",
			OrderRequest{Symbol: "btcusdt", Side: SideBuy, Type: OrderTypeLimit, PostOnly: true, Quantity: 0.0012349, Price: 60000.019},
			map[string]interface{}{"category": "spot", "symbol": "BTCUSDT", "side": "Buy", "orderType": "Limit", "qty": "0.001234", "price": "60000.01", "timeInForce": "PostOnly"},
			[]string{"reduceOnly", "marketUnit"}},
		{"賣單價格向上",
			OrderRequest{Side: SideSell, Type: OrderTypeLimit, Quantity: 0.001, Price: 60000.011},
			map[string]interface{}{"side": "Sell", "price": "60000.02", "timeInForce": "GTC", "qty": "0.001000"},
			[]string{"reduceOnly"}},
		{"市價單按基礎幣",
			OrderRequest{Side: SideBuy, Type: OrderTypeMarket, Quantity: 0.0005, ReduceOnly: true},
			map[string]interface{}{"orderType": "Market", "marketUnit": "baseCoin", "qty": "0.000500"},
			[]string{"price", "timeInForce", "reduceOnly"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &bybitSpotFakeServer{}
			srv := httptest.NewServer(http.HandlerFunc(fake.handler))
			defer srv.Close()
			b := newTestBybitSpotAdapter(t, srv.URL)

			req := tt.req
			order, err := b.PlaceOrder(context.Background(), &req)
			if err != nil {
				t.Fatalf("PlaceOrder: %v", err)
			}
			fake.mu.Lock()
			body := fake.orderBody
			fake.mu.Unlock()
			for k, v := range tt.want {
				if body[k] != v {
					t.Fatalf("%s=%v want %v（body=%v）", k, body[k], v, body)
				}
			}
			for _, k := range tt.absent {
				if _, ok := body[k]; ok {
					t.Fatalf("不應發送 %s（body=%v）", k, body)
				}
			}
			if order.Symbol != "BTCUSDT" {
				t.Fatalf("返回訂單 symbol=%s", order.Symbol)
			}
		})
	}
}

func TestBybitSpotPlaceOrderRejects(t *testing.T) {
	fake := &bybitSpotFakeServer{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	b := newTestBybitSpotAdapter(t, srv.URL)
	ctx := context.Background()

	if _, err := b.PlaceOrder(ctx, &OrderRequest{Side: SideBuy, Type: OrderTypeLimit, Quantity: 0.0000479, Price: 1000000}); err == nil || !strings.Contains(err.Error(), "最小下單量") {
		t.Fatalf("期望最小下單量錯誤，得到 %v", err)
	}
	if _, err := b.PlaceOrder(ctx, &OrderRequest{Side: SideBuy, Type: OrderTypeLimit, Quantity: 0.00005, Price: 60000}); err == nil || !strings.Contains(err.Error(), "最小下單金額") {
		t.Fatalf("期望最小下單金額錯誤（0.00005×60000=3 < 5），得到 %v", err)
	}
	if _, err := b.PlaceOrder(ctx, &OrderRequest{Side: Side("BUY"), Type: OrderTypeLimit, Quantity: 1, Price: 1}); err == nil {
		t.Fatal("未映射的 BUY 應被拒絕")
	}
	if _, err := b.PlaceOrder(ctx, &OrderRequest{Side: SideBuy, Type: OrderTypeMarket, PostOnly: true, Quantity: 1}); err == nil {
		t.Fatal("市價單 PostOnly 應被拒絕")
	}
	if fake.orders != 0 {
		t.Fatalf("被拒絕的請求不應發出，實際發出 %d 筆", fake.orders)
	}
}

func TestBybitSpotNormalizeOrderUpdate(t *testing.T) {
	b := newTestBybitSpotAdapter(t, "http://unused")
	got, err := b.normalizeOrderUpdate(OrderUpdate{OrderID: 1, Symbol: "BTCUSDT", Side: SideSell, Type: OrderTypeLimit, Status: OrderStatusPartiallyFilledCanceled})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got.Symbol != "BTCUSDT" || got.Side != "SELL" || got.Type != "LIMIT" || got.Status != "CANCELED" || got.CommissionAsset != "USDT" {
		t.Fatalf("unexpected %+v", got)
	}
	if _, err := b.normalizeOrderUpdate(OrderUpdate{Symbol: "ETHUSDT", Side: SideBuy, Type: OrderTypeLimit, Status: OrderStatusNew}); err == nil {
		t.Fatal("非本交易對推送應報錯")
	}
	if _, err := b.normalizeOrderUpdate(OrderUpdate{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, Status: "Weird"}); err == nil {
		t.Fatal("未知狀態應報錯")
	}
}

func TestBybitSpotGetOrderFillsConvertsFee(t *testing.T) {
	fake := &bybitSpotFakeServer{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	b := newTestBybitSpotAdapter(t, srv.URL)

	fills, err := b.GetOrderFills(context.Background(), "BTCUSDT", 42)
	if err != nil {
		t.Fatalf("GetOrderFills: %v", err)
	}
	if !strings.Contains(fake.fillsURI, "category=spot") || !strings.Contains(fake.fillsURI, "orderId=42") {
		t.Fatalf("請求參數錯誤: %s", fake.fillsURI)
	}
	if len(fills) != 2 {
		t.Fatalf("fills=%d", len(fills))
	}
	if math.Abs(fills[0].Commission-0.6) > 1e-9 || fills[0].CommissionAsset != "USDT" || !fills[0].IsMaker {
		t.Fatalf("基礎幣手續費應換算為 USDT: %+v", fills[0])
	}
	if math.Abs(fills[0].BaseFeeQty-0.00001) > 1e-12 {
		t.Fatalf("基礎幣手續費數量應保留在 BaseFeeQty: %+v", fills[0])
	}
	if math.Abs(fills[1].Commission-0.6001) > 1e-9 || fills[1].CommissionAsset != "USDT" || fills[1].BaseFeeQty != 0 {
		t.Fatalf("fill1=%+v", fills[1])
	}
}

func TestBybitSpotExecutionRejectsUnverifiedFeeFields(t *testing.T) {
	for _, tc := range []struct {
		name      string
		feeFields string
	}{
		{name: "missing fee currency", feeFields: `"execFee":"0.01"`},
		{name: "malformed fee amount", feeFields: `"execFee":"invalid","feeCurrency":"USDT"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &bybitSpotFakeServer{fillsJSON: `{"retCode":0,"retMsg":"OK","result":{"list":[{"orderId":"42","symbol":"BTCUSDT","side":"Buy","execPrice":"60000","execQty":"0.01",` + tc.feeFields + `,"execTime":"1700000000000","tradeId":"bad-fee"}]}}`}
			srv := httptest.NewServer(http.HandlerFunc(fake.handler))
			defer srv.Close()
			b := newTestBybitSpotAdapter(t, srv.URL)
			if _, _, err := b.GetOrderHistoryPage(context.Background(), "BTCUSDT", 1, 1700000000001, "", 10); err == nil {
				t.Fatal("history page must reject incomplete fee evidence")
			}
			if _, err := b.GetOrderFills(context.Background(), "BTCUSDT", 42); err == nil {
				t.Fatal("order fill query must reject incomplete fee evidence")
			}
		})
	}
}

// TestBybitSpotOrderStreamFiltersCategoryAndReconnects 現貨適配器複用私有 WS：
// 認證確認後才訂閱、斷線重連；只轉發 category=spot 的推送並歸一化。
func TestBybitSpotOrderStreamFiltersCategoryAndReconnects(t *testing.T) {
	for _, tc := range []struct {
		category string
		wantPush bool
	}{{bybitCategorySpot, true}, {bybitCategoryLinear, false}} {
		t.Run(tc.category, func(t *testing.T) {
			fake := &fakeBybitPrivateServer{category: tc.category}
			srv := httptest.NewServer(http.HandlerFunc(fake.handle))
			defer srv.Close()

			b := newTestBybitSpotAdapter(t, "http://unused")
			b.wsManager = NewWebSocketManager("k", "s", false)
			b.wsManager.privateURL = "ws" + strings.TrimPrefix(srv.URL, "http")
			b.wsManager.reconnectInitialBackoff = 10 * time.Millisecond
			b.wsManager.reconnectMaxBackoff = 20 * time.Millisecond

			updates := make(chan StreamOrderUpdate, 4)
			if err := b.StartOrderStream(context.Background(), func(u interface{}) {
				if su, ok := u.(StreamOrderUpdate); ok {
					updates <- su
				}
			}); err != nil {
				t.Fatalf("StartOrderStream: %v", err)
			}
			defer func() { _ = b.StopOrderStream() }()

			if !tc.wantPush {
				deadline := time.After(testWaitTimeout)
				for fake.conns.Load() < 2 {
					select {
					case u := <-updates:
						t.Fatalf("linear 推送應被現貨適配器過濾: %+v", u)
					case <-deadline:
						t.Fatalf("未發生重連，連接數 %d", fake.conns.Load())
					case <-time.After(10 * time.Millisecond):
					}
				}
				select {
				case u := <-updates:
					t.Fatalf("linear 推送應被現貨適配器過濾: %+v", u)
				case <-time.After(100 * time.Millisecond):
				}
				return
			}

			seen := map[int64]bool{}
			deadline := time.After(testWaitTimeout)
			for len(seen) < 2 {
				select {
				case u := <-updates:
					if u.Symbol != "BTCUSDT" || u.Side != "BUY" || u.Type != "LIMIT" || u.Status != "FILLED" {
						t.Fatalf("推送未正確歸一化: %+v", u)
					}
					seen[u.OrderID] = true
				case <-deadline:
					t.Fatalf("重連後未收到推送，已收到 %v，連接數 %d", seen, fake.conns.Load())
				}
			}
			if fake.earlySubscribe.Load() {
				t.Fatal("在認證確認之前就發送了訂閱")
			}
		})
	}
}
