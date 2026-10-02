package okx

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

// newTestOKXAdapter 構造不觸網的適配器（REST 指向 httptest）
func newTestOKXAdapter(t *testing.T, baseURL string, inst Instrument) *OKXAdapter {
	t.Helper()
	c := NewOKXClient("k", "s", "p", false)
	c.baseURL = baseURL
	a := &OKXAdapter{client: c, symbol: "ETHUSDT", instId: "ETH-USDT-SWAP"}
	if err := a.applyInstrument(inst); err != nil {
		t.Fatalf("applyInstrument: %v", err)
	}
	return a
}

var ethSwap = Instrument{InstId: "ETH-USDT-SWAP", CtVal: "0.1", CtValCcy: "ETH", SettleCcy: "USDT", TickSz: "0.01", LotSz: "1", MinSz: "1"}

func TestApplyInstrumentCtVal(t *testing.T) {
	tests := []struct {
		name         string
		inst         Instrument
		baseQty      float64
		wantContract float64
		wantErr      bool
		wantDecimals int
	}{
		{"ETH 0.35 → 3 張", ethSwap, 0.35, 3, false, 1},
		{"ETH 0.3 浮點對齊 → 3 張", ethSwap, 0.3, 3, false, 1},
		{"ETH 低於 minSz", ethSwap, 0.05, 0, true, 1},
		{"BTC 小數張", Instrument{InstId: "BTC-USDT-SWAP", CtVal: "0.01", TickSz: "0.1", LotSz: "0.01", MinSz: "0.01"}, 0.0335, 3.35, false, 4},
		{"現貨無 ctVal 按 1", Instrument{InstId: "BTC-USDT", TickSz: "0.1", LotSz: "0.00001", MinSz: "0.00001"}, 0.001234567, 0.00123, false, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestOKXAdapter(t, "http://unused", tt.inst)
			got, err := a.baseToContracts(tt.baseQty)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.wantContract {
				t.Fatalf("contracts=%v want %v", got, tt.wantContract)
			}
			if a.GetQuantityDecimals() != tt.wantDecimals {
				t.Fatalf("quantityDecimals=%d want %d", a.GetQuantityDecimals(), tt.wantDecimals)
			}
		})
	}
}

func TestOKXAccountOpenOrdersPreservesOtherInstrument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("instType") != "SWAP" || r.URL.Query().Has("instId") {
			t.Errorf("unexpected account-wide SWAP query: %s", r.URL.RequestURI())
		}
		_, _ = io.WriteString(w, `{"code":"0","data":[{"ordId":"123","instId":"BTC-USDT-SWAP","side":"buy","ordType":"limit","px":"10","sz":"1","accFillSz":"0","state":"live"}]}`)
	}))
	defer server.Close()
	adapter := newTestOKXAdapter(t, server.URL, ethSwap)
	orders, err := adapter.GetAccountOpenOrders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].Symbol != "BTC-USDT-SWAP" {
		t.Fatalf("account-wide SWAP orders=%+v", orders)
	}
}

func TestApplyInstrumentRejectsInvalidCtVal(t *testing.T) {
	a := &OKXAdapter{instId: "X"}
	bad := ethSwap
	bad.CtVal = "0"
	if err := a.applyInstrument(bad); err == nil {
		t.Fatal("ctVal=0 應報錯")
	}
}

func TestContractsToBase(t *testing.T) {
	a := newTestOKXAdapter(t, "http://unused", Instrument{InstId: "BTC-USDT-SWAP", CtVal: "0.01", TickSz: "0.1", LotSz: "0.01", MinSz: "0.01"})
	if got := a.contractsToBase(3); got != 0.03 {
		t.Fatalf("3 張 = %v, want 0.03", got)
	}
	if got := a.contractsToBase(-12.5); got != -0.125 {
		t.Fatalf("-12.5 張 = %v, want -0.125", got)
	}
}

func TestAlignPriceByDirection(t *testing.T) {
	a := newTestOKXAdapter(t, "http://unused", Instrument{InstId: "BTC-USDT-SWAP", CtVal: "0.01", TickSz: "0.1", LotSz: "0.01", MinSz: "0.01"})
	tests := []struct {
		side  Side
		price float64
		want  float64
	}{
		{SideBuy, 100.07, 100.0},
		{SideSell, 100.07, 100.1},
		{SideBuy, 8.7, 8.7},
		{SideSell, 100.1, 100.1},
	}
	for _, tt := range tests {
		got, err := a.alignPrice(tt.price, tt.side, 0)
		if err != nil || got != tt.want {
			t.Fatalf("alignPrice(%v,%s)=%v,%v want %v", tt.price, tt.side, got, err, tt.want)
		}
	}
	if _, err := a.alignPrice(100, Side("BUY"), 0); err == nil {
		t.Fatal("非原生方向應報錯")
	}
}

type okxFakeServer struct {
	mu        sync.Mutex
	posMode   string
	orderBody map[string]interface{}
	orders    int
	fillsPath string
}

func (f *okxFakeServer) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/api/v5/account/config":
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[{"posMode":"`+f.posMode+`"}]}`)
	case r.URL.Path == "/api/v5/trade/order" && r.Method == http.MethodPost:
		f.orders++
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &f.orderBody)
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[{"ordId":"123","clOrdId":"c1","sCode":"0","sMsg":""}]}`)
	case r.URL.Path == "/api/v5/trade/fills":
		f.fillsPath = r.URL.RequestURI()
		_, _ = io.WriteString(w, `{"code":"0","msg":"","data":[
			{"instId":"ETH-USDT-SWAP","ordId":"123","tradeId":"t1","side":"buy","fillSz":"2","fillPx":"2000.5","fee":"-0.04","feeCcy":"USDT","fillPnl":"0","execType":"T","ts":"1700000000000"},
			{"instId":"ETH-USDT-SWAP","ordId":"123","tradeId":"t2","side":"buy","fillSz":"1","fillPx":"2000.4","fee":"0.01","feeCcy":"USDT","fillPnl":"1.25","execType":"M","ts":"1700000000001"}]}`)
	default:
		http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
	}
}

func TestPlaceOrderConvertsToContractsAndRounds(t *testing.T) {
	fake := &okxFakeServer{posMode: "net_mode"}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	a := newTestOKXAdapter(t, srv.URL, ethSwap)

	order, err := a.PlaceOrder(context.Background(), &OrderRequest{
		Symbol: "ETHUSDT", Side: SideSell, Type: OrderTypeLimit, PostOnly: true,
		Quantity: 0.39, Price: 2000.051, PriceDecimals: 2,
	})
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	fake.mu.Lock()
	body := fake.orderBody
	fake.mu.Unlock()
	if body["sz"] != "3" {
		t.Fatalf("sz=%v want 3（0.39 ETH / ctVal 0.1 向下取整）", body["sz"])
	}
	if body["px"] != "2000.06" {
		t.Fatalf("px=%v want 2000.06（賣單向上取整）", body["px"])
	}
	if body["ordType"] != "post_only" || body["side"] != "sell" {
		t.Fatalf("ordType/side=%v/%v", body["ordType"], body["side"])
	}
	if _, ok := body["postOnly"]; ok {
		t.Fatal("OKX 無 postOnly 參數，不應發送")
	}
	if order.Quantity != 0.3 || order.Price != 2000.06 {
		t.Fatalf("返回訂單應為基礎幣數量 0.3 / 對齊價 2000.06，得到 %v / %v", order.Quantity, order.Price)
	}
}

func TestPlaceOrderRejectsBelowMinSz(t *testing.T) {
	fake := &okxFakeServer{posMode: "net_mode"}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	a := newTestOKXAdapter(t, srv.URL, ethSwap)

	_, err := a.PlaceOrder(context.Background(), &OrderRequest{Side: SideBuy, Type: OrderTypeLimit, Quantity: 0.09, Price: 2000})
	if err == nil || !strings.Contains(err.Error(), "最小下單量") {
		t.Fatalf("期望最小下單量錯誤，得到 %v", err)
	}
	if fake.orders != 0 {
		t.Fatal("不應發出下單請求")
	}
}

func TestPlaceOrderRefusesLongShortMode(t *testing.T) {
	fake := &okxFakeServer{posMode: "long_short_mode"}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	a := newTestOKXAdapter(t, srv.URL, ethSwap)

	_, err := a.PlaceOrder(context.Background(), &OrderRequest{Side: SideBuy, Type: OrderTypeLimit, Quantity: 1, Price: 2000})
	if err == nil || !strings.Contains(err.Error(), "雙向持倉") {
		t.Fatalf("期望雙向持倉錯誤，得到 %v", err)
	}
	if fake.orders != 0 {
		t.Fatal("雙向持倉模式下不應發出下單請求")
	}
}

func TestGetOrderFillsParsesFeeAndContracts(t *testing.T) {
	fake := &okxFakeServer{posMode: "net_mode"}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	defer srv.Close()
	a := newTestOKXAdapter(t, srv.URL, ethSwap)

	fills, err := a.GetOrderFills(context.Background(), "ETHUSDT", 123)
	if err != nil {
		t.Fatalf("GetOrderFills: %v", err)
	}
	if !strings.Contains(fake.fillsPath, "instId=ETH-USDT-SWAP") || !strings.Contains(fake.fillsPath, "ordId=123") {
		t.Fatalf("請求參數錯誤: %s", fake.fillsPath)
	}
	if len(fills) != 2 {
		t.Fatalf("fills=%d", len(fills))
	}
	if fills[0].Quantity != 0.2 || fills[0].Commission != 0.04 || fills[0].IsMaker || fills[0].Symbol != "ETHUSDT" {
		t.Fatalf("fill0=%+v", fills[0])
	}
	if fills[1].Commission != -0.01 || !fills[1].IsMaker {
		t.Fatalf("返佣應為負手續費: %+v", fills[1])
	}
	if !fills[0].RealizedPnLKnown || fills[0].RealizedPnL != 0 || fills[0].RealizedPnLAsset != "USDT" ||
		!fills[1].RealizedPnLKnown || fills[1].RealizedPnL != 1.25 || fills[1].RealizedPnLAsset != "USDT" {
		t.Fatalf("逐筆已實現盈虧映射錯誤: %+v %+v", fills[0], fills[1])
	}
}

func TestNormalizeOrderUpdate(t *testing.T) {
	a := newTestOKXAdapter(t, "http://unused", ethSwap)
	u := OrderUpdate{OrderID: 1, Symbol: "ETH-USDT-SWAP", Side: SideBuy, Type: OrderTypePostOnly, Status: OrderStatusFilled, Quantity: 3, ExecutedQty: 3}
	got, err := a.normalizeOrderUpdate(u)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got.Symbol != "ETHUSDT" || got.Side != "BUY" || got.Type != "LIMIT" || got.Status != "FILLED" || got.ExecutedQty != 0.3 {
		t.Fatalf("unexpected %+v", got)
	}

	u.Status = OrderStatus("weird")
	if _, err := a.normalizeOrderUpdate(u); err == nil {
		t.Fatal("未知狀態應報錯")
	}
	u.Status = OrderStatusFilled
	u.Symbol = "BTC-USDT-SWAP"
	if _, err := a.normalizeOrderUpdate(u); err == nil {
		t.Fatal("非本合約推送應報錯")
	}
}

func TestHandleOrderUpdateParsesFillFee(t *testing.T) {
	w := NewWebSocketManager("k", "s", "p", false)
	var got []OrderUpdate
	w.orderCallback = func(u OrderUpdate) { got = append(got, u) }
	msg := []byte(`{"arg":{"channel":"orders","instType":"SWAP","instId":"ETH-USDT-SWAP"},"data":[
		{"instId":"ETH-USDT-SWAP","ordId":"9","side":"sell","ordType":"limit","state":"partially_filled","sz":"3","accFillSz":"1","fillFee":"-0.012","fillFeeCcy":"USDT"},
		{"instId":"ETH-USDT-SWAP","ordId":"10","side":"buy","ordType":"post_only","state":"filled","sz":"1","accFillSz":"1","fillFee":"0.002","fillFeeCcy":"USDT"}]}`)
	w.handleMessage(msg)
	if len(got) != 2 {
		t.Fatalf("updates=%d", len(got))
	}
	if got[0].Commission != 0.012 || got[0].Side != SideSell || got[0].Symbol != "ETH-USDT-SWAP" {
		t.Fatalf("update0=%+v", got[0])
	}
	if got[1].Commission != -0.002 {
		t.Fatalf("maker 返佣應為負手續費: %+v", got[1])
	}
}
