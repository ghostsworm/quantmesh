package okx

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

var btcSpot = Instrument{InstId: "BTC-USDT", InstType: "SPOT", BaseCcy: "BTC", QuoteCcy: "USDT", TickSz: "0.1", LotSz: "0.00001", MinSz: "0.0001"}

// newTestOKXSpotAdapter 構造不觸網的現貨適配器（REST 指向 httptest）
func newTestOKXSpotAdapter(t *testing.T, baseURL string) *OKXSpotAdapter {
	t.Helper()
	c := NewOKXClient("k", "s", "p", false)
	c.baseURL = baseURL
	a := &OKXSpotAdapter{client: c, symbol: "BTCUSDT", instId: "BTC-USDT"}
	if err := a.applySpotInstrument(btcSpot); err != nil {
		t.Fatalf("applySpotInstrument: %v", err)
	}
	return a
}

type okxSpotFakeServer struct {
	mu        sync.Mutex
	orderBody map[string]interface{}
	orders    int
	fillsURI  string
	fillsJSON string
}

func (f *okxSpotFakeServer) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/v5/trade/order" && r.Method == http.MethodPost:
		f.orders++
		body, _ := io.ReadAll(r.Body)
		f.orderBody = nil
		_ = json.Unmarshal(body, &f.orderBody)
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[{"ordId":"77","clOrdId":"c1","sCode":"0","sMsg":""}]}`)
	case r.URL.Path == "/api/v5/trade/fills":
		f.fillsURI = r.URL.RequestURI()
		if f.fillsJSON != "" {
			_, _ = io.WriteString(w, f.fillsJSON)
			return
		}
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[
			{"instId":"BTC-USDT","ordId":"77","tradeId":"t1","side":"buy","fillSz":"0.01","fillPx":"60000","fee":"-0.00001","feeCcy":"BTC","execType":"M","ts":"1700000000000"},
			{"instId":"BTC-USDT","ordId":"77","tradeId":"t2","side":"sell","fillSz":"0.01","fillPx":"60010","fee":"-0.6001","feeCcy":"USDT","execType":"T","ts":"1700000000001"}]}`)
	default:
		http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
	}
}

func TestOKXSpotApplyInstrument(t *testing.T) {
	a := newTestOKXSpotAdapter(t, "http://unused")
	if a.GetBaseAsset() != "BTC" || a.GetQuoteAsset() != "USDT" || a.GetQuantityDecimals() != 5 || a.GetPriceDecimals() != 1 {
		t.Fatalf("unexpected spec base=%s quote=%s qd=%d pd=%d", a.GetBaseAsset(), a.GetQuoteAsset(), a.GetQuantityDecimals(), a.GetPriceDecimals())
	}
	bad := btcSpot
	bad.LotSz = "0"
	if err := (&OKXSpotAdapter{instId: "BTC-USDT"}).applySpotInstrument(bad); err == nil {
		t.Fatal("lotSz=0 應報錯")
	}
}

func TestOKXSpotOrderFeeAuthorityRequiresPresentParseableField(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
		want bool
	}{
		{name: "explicit zero fee", data: map[string]interface{}{"fillFee": "0", "fillFeeCcy": "USDT"}, want: true},
		{name: "missing fee", data: map[string]interface{}{"fillFeeCcy": "USDT"}},
		{name: "malformed fee", data: map[string]interface{}{"fillFee": "unknown", "fillFeeCcy": "USDT"}},
		{name: "non-finite fee", data: map[string]interface{}{"fillFee": "NaN", "fillFeeCcy": "USDT"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got OrderUpdate
			manager := &WebSocketManager{orderCallback: func(update OrderUpdate) { got = update }}
			data := map[string]interface{}{
				"ordId": "7", "instId": "BTC-USDT", "side": "buy", "ordType": "limit",
				"state": "partially_filled", "sz": "1", "accFillSz": "0.5", "avgPx": "100",
			}
			for key, value := range tt.data {
				data[key] = value
			}
			manager.handleOrderUpdate(map[string]interface{}{"data": []interface{}{data}})
			if got.CommissionKnown != tt.want {
				t.Fatalf("CommissionKnown=%v, want %v", got.CommissionKnown, tt.want)
			}
		})
	}
}

func TestOKXSpotPlaceOrderBodyAndRounding(t *testing.T) {
	tests := []struct {
		name    string
		req     OrderRequest
		want    map[string]interface{}
		absent  []string
		wantQty float64
	}{
		{"買單 post only 價格向下、數量向下",
			OrderRequest{Side: SideBuy, Type: OrderTypeLimit, PostOnly: true, Quantity: 0.012349, Price: 60000.19},
			map[string]interface{}{"side": "buy", "ordType": "post_only", "px": "60000.1", "sz": "0.01234", "tdMode": "cash", "instId": "BTC-USDT"},
			[]string{"postOnly", "tgtCcy", "reduceOnly"}, 0.01234},
		{"賣單限價價格向上",
			OrderRequest{Side: SideSell, Type: OrderTypeLimit, Quantity: 0.001, Price: 60000.11},
			map[string]interface{}{"side": "sell", "ordType": "limit", "px": "60000.2", "sz": "0.00100"},
			[]string{"postOnly"}, 0.001},
		{"市價單按基礎幣數量",
			OrderRequest{Side: SideBuy, Type: OrderTypeMarket, Quantity: 0.00051},
			map[string]interface{}{"side": "buy", "ordType": "market", "sz": "0.00051", "tgtCcy": "base_ccy"},
			[]string{"px"}, 0.00051},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &okxSpotFakeServer{}
			srv := httptest.NewServer(http.HandlerFunc(fake.handler))
			defer srv.Close()
			a := newTestOKXSpotAdapter(t, srv.URL)

			req := tt.req
			order, err := a.PlaceOrder(context.Background(), &req)
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
			if order.Quantity != tt.wantQty || order.Symbol != "BTCUSDT" {
				t.Fatalf("返回訂單 qty=%v symbol=%s", order.Quantity, order.Symbol)
			}
		})
	}
}

func TestOKXSpotPlaceOrderRejects(t *testing.T) {
	fake := &okxSpotFakeServer{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	a := newTestOKXSpotAdapter(t, srv.URL)

	_, err := a.PlaceOrder(context.Background(), &OrderRequest{Side: SideBuy, Type: OrderTypeLimit, Quantity: 0.000099, Price: 60000})
	if err == nil || !strings.Contains(err.Error(), "最小下單量") {
		t.Fatalf("期望最小下單量錯誤，得到 %v", err)
	}
	if _, err := a.PlaceOrder(context.Background(), &OrderRequest{Side: Side("BUY"), Type: OrderTypeLimit, Quantity: 1, Price: 1}); err == nil {
		t.Fatal("未映射的 BUY 應被拒絕")
	}
	if _, err := a.PlaceOrder(context.Background(), &OrderRequest{Side: SideBuy, Type: OrderTypeMarket, PostOnly: true, Quantity: 1}); err == nil {
		t.Fatal("市價單 post only 應被拒絕")
	}
	if _, err := a.PlaceOrder(context.Background(), &OrderRequest{Side: SideBuy, Type: OrderType("LIMIT"), Quantity: 1, Price: 1}); err == nil {
		t.Fatal("非原生類型應被拒絕")
	}
	if fake.orders != 0 {
		t.Fatalf("被拒絕的請求不應發出，實際發出 %d 筆", fake.orders)
	}
}

func TestOKXSpotNormalizeSpotStreamUpdateBaseFeeQty(t *testing.T) {
	a := newTestOKXSpotAdapter(t, "http://unused")
	u := OrderUpdate{OrderID: 1, Symbol: "BTC-USDT", Side: SideBuy, Type: OrderTypeLimit, Status: OrderStatusPartiallyFilled,
		ExecutedQty: 0.01, Commission: 0.00001, CommissionAsset: "BTC", FillPrice: 60000}
	got, err := a.normalizeSpotStreamUpdate(u)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got.BaseFeeQty != 0.00001 || math.Abs(got.Commission-0.6) > 1e-9 || got.CommissionAsset != "USDT" {
		t.Fatalf("基礎幣手續費應同時給出 BaseFeeQty 與計價幣 Commission: %+v", got)
	}
	u.CommissionAsset, u.Commission = "USDT", 0.5
	if got, _ := a.normalizeSpotStreamUpdate(u); got.BaseFeeQty != 0 {
		t.Fatalf("計價幣手續費 BaseFeeQty 應為 0: %+v", got)
	}
	u.CommissionAsset, u.Commission = "BTC", -0.00001 // 返佣
	if got, _ := a.normalizeSpotStreamUpdate(u); got.BaseFeeQty != 0 {
		t.Fatalf("返佣 BaseFeeQty 應為 0: %+v", got)
	}
}

func TestOKXSpotNormalizeOrderUpdate(t *testing.T) {
	a := newTestOKXSpotAdapter(t, "http://unused")
	u := OrderUpdate{OrderID: 1, Symbol: "BTC-USDT", Side: SideBuy, Type: OrderTypePostOnly, Status: OrderStatusPartiallyFilled,
		ExecutedQty: 0.01, Commission: 0.00001, CommissionAsset: "BTC", FillPrice: 60000}
	got, err := a.normalizeOrderUpdate(u)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got.Symbol != "BTCUSDT" || got.Side != "BUY" || got.Type != "LIMIT" || got.Status != "PARTIALLY_FILLED" {
		t.Fatalf("unexpected %+v", got)
	}
	if math.Abs(got.Commission-0.6) > 1e-9 || got.CommissionAsset != "USDT" {
		t.Fatalf("基礎幣手續費應按成交價換算為 USDT: %v %s", got.Commission, got.CommissionAsset)
	}

	u.CommissionAsset, u.Commission = "USDT", 0.5
	if got, _ := a.normalizeOrderUpdate(u); got.Commission != 0.5 || got.CommissionAsset != "USDT" {
		t.Fatalf("計價幣手續費不應換算: %+v", got)
	}
	u.CommissionAsset = "OKB"
	if got, _ := a.normalizeOrderUpdate(u); got.Commission != 0.5 || got.CommissionAsset != "OKB" {
		t.Fatalf("其他幣種應保留原幣種: %+v", got)
	}

	u.Status = OrderStatus("weird")
	if _, err := a.normalizeOrderUpdate(u); err == nil {
		t.Fatal("未知狀態應報錯")
	}
	u.Status = OrderStatusFilled
	u.Symbol = "ETH-USDT"
	if _, err := a.normalizeOrderUpdate(u); err == nil {
		t.Fatal("非本交易對推送應報錯")
	}
}

func TestOKXSpotGetOrderFillsConvertsFee(t *testing.T) {
	fake := &okxSpotFakeServer{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	a := newTestOKXSpotAdapter(t, srv.URL)

	fills, err := a.GetOrderFills(context.Background(), "BTCUSDT", 77)
	if err != nil {
		t.Fatalf("GetOrderFills: %v", err)
	}
	if !strings.Contains(fake.fillsURI, "instType=SPOT") || !strings.Contains(fake.fillsURI, "instId=BTC-USDT") || !strings.Contains(fake.fillsURI, "ordId=77") {
		t.Fatalf("請求參數錯誤: %s", fake.fillsURI)
	}
	if len(fills) != 2 {
		t.Fatalf("fills=%d", len(fills))
	}
	if math.Abs(fills[0].Commission-0.6) > 1e-9 || fills[0].CommissionAsset != "USDT" || !fills[0].IsMaker || fills[0].Symbol != "BTCUSDT" {
		t.Fatalf("fill0=%+v", fills[0])
	}
	if math.Abs(fills[0].BaseFeeQty-0.00001) > 1e-12 {
		t.Fatalf("基礎幣手續費數量應保留在 BaseFeeQty: %+v", fills[0])
	}
	if math.Abs(fills[1].Commission-0.6001) > 1e-9 || fills[1].CommissionAsset != "USDT" || fills[1].Side != SideSell || fills[1].BaseFeeQty != 0 {
		t.Fatalf("fill1=%+v", fills[1])
	}
}

func TestOKXSpotExecutionRejectsUnverifiedFeeFields(t *testing.T) {
	for _, tc := range []struct {
		name      string
		feeFields string
	}{
		{name: "missing fee currency", feeFields: `"fee":"-0.01"`},
		{name: "malformed fee amount", feeFields: `"fee":"invalid","feeCcy":"USDT"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &okxSpotFakeServer{fillsJSON: `{"code":"0","msg":"","data":[{"instId":"BTC-USDT","ordId":"77","tradeId":"bad-fee","side":"buy","fillSz":"0.01","fillPx":"60000",` + tc.feeFields + `,"execType":"M","ts":"1700000000000"}]}`}
			srv := httptest.NewServer(http.HandlerFunc(fake.handler))
			defer srv.Close()
			a := newTestOKXSpotAdapter(t, srv.URL)
			if _, err := a.GetOrderFills(context.Background(), "BTCUSDT", 77); err == nil {
				t.Fatal("spot order fills must reject incomplete fee evidence")
			}
		})
	}
}

func TestOKXSpotHandleOrderUpdateParsesFillPx(t *testing.T) {
	w := NewWebSocketManager("k", "s", "p", false)
	var got []OrderUpdate
	w.orderCallback = func(u OrderUpdate) { got = append(got, u) }
	w.handleMessage([]byte(`{"arg":{"channel":"orders","instType":"SPOT","instId":"BTC-USDT"},"data":[
		{"instId":"BTC-USDT","ordId":"5","side":"buy","ordType":"limit","state":"filled","sz":"0.01","accFillSz":"0.01","fillPx":"60000","fillFee":"-0.00001","fillFeeCcy":"BTC"}]}`))
	if len(got) != 1 || got[0].FillPrice != 60000 || got[0].CommissionAsset != "BTC" || got[0].Commission != 0.00001 {
		t.Fatalf("unexpected %+v", got)
	}
}

func TestOKXSpotOrderWSSubscribesSpotAndReconnects(t *testing.T) {
	fake := &fakeOKXPrivateServer{t: t, instId: "BTC-USDT"}
	srv, url := newFakeOKXWS(t, fake)
	defer srv.Close()

	m := NewSpotOrderWebSocketManager("k", "s", "p", false, "BTC-USDT")
	m.ws.privateURL = url
	m.ws.reconnectInitialBackoff = 10 * time.Millisecond
	m.ws.reconnectMaxBackoff = 20 * time.Millisecond

	a := newTestOKXSpotAdapter(t, "http://unused")
	updates := make(chan StreamOrderUpdate, 4)
	err := m.Start(context.Background(), func(u OrderUpdate) {
		n, err := a.normalizeOrderUpdate(u)
		if err != nil {
			t.Errorf("normalize: %v", err)
			return
		}
		updates <- n
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop()

	seen := map[int64]StreamOrderUpdate{}
	deadline := time.After(testWaitTimeout)
	for len(seen) < 2 {
		select {
		case u := <-updates:
			seen[u.OrderID] = u
		case <-deadline:
			t.Fatalf("重連後未收到推送，已收到 %v，連接數 %d", seen, fake.conns.Load())
		}
	}
	if fake.earlySubscribe.Load() {
		t.Fatal("在登錄確認之前就發送了訂閱")
	}
	if got, _ := fake.subscribedInstType.Load().(string); got != okxInstTypeSpot {
		t.Fatalf("訂閱 instType=%q want SPOT", got)
	}
	for _, u := range seen {
		if u.Symbol != "BTCUSDT" || u.Status != "FILLED" || u.Side != "BUY" || math.Abs(u.Commission-0.1) > 1e-9 || u.CommissionAsset != "USDT" {
			t.Fatalf("推送未正確歸一化: %+v", u)
		}
	}
}

func TestSymbolToSpotInstId(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"BTCUSDT", "BTC-USDT"},
		{"ETHUSDT", "ETH-USDT"},
		{"  BTCUSDT  ", "BTC-USDT"},
	}
	for _, c := range cases {
		if got := symbolToSpotInstId(c.in); got != c.want {
			t.Errorf("symbolToSpotInstId(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
