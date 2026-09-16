package binance

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	binancesdk "github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/common"
	"github.com/adshao/go-binance/v2/futures"
)

const (
	guardSymbol          = "DOGEUSDT"
	guardMinNotional     = 5.0
	exchangeInfoEndpoint = "/fapi/v1/exchangeInfo"
	positionModeEndpoint = "/fapi/v1/positionSide/dual"
	futuresOrderEndpoint = "/fapi/v1/order"
	spotOrderEndpoint    = "/api/v3/order"
	marginOrderEndpoint  = "/sapi/v1/margin/order"
	testBrokerPrefix     = "x-zdfVM8vY"
	guardCachedPrice     = 0.1
	guardErrCodeGeneric  = -1000
	guardFloatTolerance  = 1e-9
)

// fakeFuturesServer 模擬下單相關的合約 REST 接口
type fakeFuturesServer struct {
	mu          sync.Mutex
	dualSide    bool
	modeErr     bool
	modeCalls   int
	placed      []map[string]string
	queriedCIDs []string
	orderByCID  map[string]map[string]interface{}
}

func (f *fakeFuturesServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	params := requestParams(r)
	writeJSON := func(status int, body interface{}) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	switch {
	case r.URL.Path == exchangeInfoEndpoint:
		writeJSON(http.StatusOK, map[string]interface{}{
			"symbols": []interface{}{
				map[string]interface{}{
					"symbol": guardSymbol, "pricePrecision": 5, "quantityPrecision": 0,
					"baseAsset": "DOGE", "quoteAsset": "USDT",
					"filters": []interface{}{
						map[string]interface{}{"filterType": "PRICE_FILTER", "tickSize": "0.00001", "minPrice": "0.00001", "maxPrice": "100"},
						map[string]interface{}{"filterType": "LOT_SIZE", "stepSize": "1", "minQty": "1", "maxQty": "1000000"},
						map[string]interface{}{"filterType": "MIN_NOTIONAL", "notional": strconv.FormatFloat(guardMinNotional, 'f', -1, 64)},
					},
				},
				map[string]interface{}{
					"symbol": "BTCUSDT", "pricePrecision": 1, "quantityPrecision": 3,
					"filters": []interface{}{map[string]interface{}{"filterType": "MIN_NOTIONAL", "notional": "100"}},
				},
			},
		})
	case r.URL.Path == positionModeEndpoint:
		f.modeCalls++
		if f.modeErr {
			writeJSON(http.StatusInternalServerError, apiErrBody(guardErrCodeGeneric, "down"))
			return
		}
		writeJSON(http.StatusOK, map[string]interface{}{"dualSidePosition": f.dualSide})
	case r.URL.Path == futuresOrderEndpoint && r.Method == http.MethodPost:
		flat := map[string]string{}
		for k := range params {
			flat[k] = params.Get(k)
		}
		f.placed = append(f.placed, flat)
		writeJSON(http.StatusOK, map[string]interface{}{"orderId": len(f.placed), "status": "NEW", "clientOrderId": params.Get("newClientOrderId")})
	case r.URL.Path == futuresOrderEndpoint && r.Method == http.MethodGet:
		cid := params.Get("origClientOrderId")
		f.queriedCIDs = append(f.queriedCIDs, cid)
		if o, ok := f.orderByCID[cid]; ok {
			writeJSON(http.StatusOK, o)
			return
		}
		writeJSON(http.StatusBadRequest, apiErrBody(binanceErrCodeNoSuchOrder, "Order does not exist."))
	default:
		writeJSON(http.StatusNotFound, apiErrBody(guardErrCodeGeneric, "not found"))
	}
}

func newGuardAdapter(t *testing.T, fake *fakeFuturesServer) *BinanceAdapter {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client := futures.NewClient("test-key", "test-secret")
	client.BaseURL = srv.URL
	a := &BinanceAdapter{
		client:    client,
		symbol:    guardSymbol,
		wsManager: &WebSocketManager{latestPrice: guardCachedPrice},
	}
	if err := a.fetchExchangeInfo(context.Background()); err != nil {
		t.Fatalf("fetchExchangeInfo: %v", err)
	}
	return a
}

func TestFetchExchangeInfoCachesMinNotional(t *testing.T) {
	a := newGuardAdapter(t, &fakeFuturesServer{})
	tests := []struct {
		symbol string
		want   float64
		wantOK bool
	}{
		{guardSymbol, guardMinNotional, true},
		{"BTCUSDT", 100, true},
		{"ETHUSDT", 0, false},
	}
	for _, tt := range tests {
		got, ok := a.minNotionalFor(tt.symbol)
		if ok != tt.wantOK || math.Abs(got-tt.want) > guardFloatTolerance {
			t.Errorf("minNotionalFor(%s) = %v,%v want %v,%v", tt.symbol, got, ok, tt.want, tt.wantOK)
		}
	}
	if a.stepSize != 1 || a.quoteAsset != "USDT" {
		t.Fatalf("precision not parsed: step=%v quote=%s", a.stepSize, a.quoteAsset)
	}
}

func TestPlaceOrderMinNotionalGuard(t *testing.T) {
	tests := []struct {
		name       string
		req        OrderRequest
		wantErr    error
		wantPlaced bool
		wantQty    string
	}{
		{name: "限價單不足下限直接拒絕且不放大數量", req: OrderRequest{Type: OrderTypeLimit, Price: 0.1, Quantity: 40}, wantErr: ErrOrderNotionalTooSmall},
		{name: "限價單達到下限正常提交", req: OrderRequest{Type: OrderTypeLimit, Price: 0.1, Quantity: 50}, wantPlaced: true, wantQty: "50"},
		{name: "reduceOnly 豁免", req: OrderRequest{Type: OrderTypeLimit, Price: 0.1, Quantity: 10, ReduceOnly: true}, wantPlaced: true, wantQty: "10"},
		{name: "市價單按最新價估算不足拒絕（不再 NaN）", req: OrderRequest{Type: OrderTypeMarket, Quantity: 10}, wantErr: ErrOrderNotionalTooSmall},
		{name: "市價單按最新價估算足額提交", req: OrderRequest{Type: OrderTypeMarket, Quantity: 60}, wantPlaced: true, wantQty: "60"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeFuturesServer{}
			a := newGuardAdapter(t, fake)
			req := tt.req
			req.Symbol = guardSymbol
			req.Side = SideBuy
			_, err := a.PlaceOrder(context.Background(), &req)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), "-4164") {
					t.Errorf("錯誤應包含 -4164 以便上層判定不可重試: %v", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got := len(fake.placed) > 0; got != tt.wantPlaced {
				t.Fatalf("placed = %v, want %v", got, tt.wantPlaced)
			}
			if tt.wantPlaced && fake.placed[0]["quantity"] != tt.wantQty {
				t.Fatalf("quantity = %s, want %s", fake.placed[0]["quantity"], tt.wantQty)
			}
		})
	}
}

func TestEstimateFinalOrderAmountDoesNotEnlarge(t *testing.T) {
	a := newGuardAdapter(t, &fakeFuturesServer{})
	if got := a.EstimateFinalOrderAmount(guardSymbol, 0.1, 40, false); math.Abs(got-4) > guardFloatTolerance {
		t.Fatalf("EstimateFinalOrderAmount = %v, want 4（不放大到下限）", got)
	}
}

func TestPositionModeGuard(t *testing.T) {
	tests := []struct {
		name      string
		fake      *fakeFuturesServer
		wantErr   bool
		wantPlace bool
	}{
		{name: "單向模式正常下單", fake: &fakeFuturesServer{}, wantPlace: true},
		{name: "對沖模式拒絕下單", fake: &fakeFuturesServer{dualSide: true}, wantErr: true},
		{name: "查詢失敗放行", fake: &fakeFuturesServer{modeErr: true}, wantPlace: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newGuardAdapter(t, tt.fake)
			req := &OrderRequest{Symbol: guardSymbol, Side: SideBuy, Type: OrderTypeLimit, Price: 0.1, Quantity: 100}
			_, err := a.PlaceOrder(context.Background(), req)
			if tt.wantErr != errors.Is(err, ErrHedgePositionMode) {
				t.Fatalf("err = %v, wantHedgeErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "-4061") {
				t.Errorf("對沖模式錯誤應含 -4061: %v", err)
			}
			if (len(tt.fake.placed) > 0) != tt.wantPlace {
				t.Fatalf("placed=%d wantPlace=%v", len(tt.fake.placed), tt.wantPlace)
			}
			// 第二次下單：單向確認後不再查詢；對沖/失敗在重查間隔內不重複查詢
			_, _ = a.PlaceOrder(context.Background(), &OrderRequest{Symbol: guardSymbol, Side: SideBuy, Type: OrderTypeLimit, Price: 0.1, Quantity: 100})
			if tt.fake.modeCalls != 1 {
				t.Fatalf("持倉模式查詢次數 = %d, want 1", tt.fake.modeCalls)
			}
			if tt.wantErr {
				if err := a.StartOrderStream(context.Background(), func(interface{}) {}); !errors.Is(err, ErrHedgePositionMode) {
					t.Fatalf("StartOrderStream 應拒絕對沖模式, err=%v", err)
				}
			}
		})
	}
}

func TestGetOrderByClientOrderID(t *testing.T) {
	fake := &fakeFuturesServer{orderByCID: map[string]map[string]interface{}{
		testBrokerPrefix + "cid-1": {"orderId": 77, "clientOrderId": testBrokerPrefix + "cid-1", "symbol": guardSymbol, "status": "FILLED", "executedQty": "50", "origQty": "50", "price": "0.1"},
	}}
	a := newGuardAdapter(t, fake)

	tests := []struct {
		name    string
		cid     string
		wantID  int64
		wantNil bool
	}{
		{name: "原始 ID 自動加前綴", cid: "cid-1", wantID: 77},
		{name: "已帶前綴不重複添加", cid: testBrokerPrefix + "cid-1", wantID: 77},
		{name: "不存在返回 nil,nil", cid: "missing", wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := a.GetOrderByClientOrderID(context.Background(), guardSymbol, tt.cid)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if tt.wantNil {
				if o != nil {
					t.Fatalf("want nil, got %#v", o)
				}
				return
			}
			if o == nil || o.OrderID != tt.wantID || o.Status != OrderStatusFilled || o.ExecutedQty != 50 {
				t.Fatalf("order = %#v", o)
			}
		})
	}
	if _, err := a.GetOrderByClientOrderID(context.Background(), guardSymbol, ""); err == nil {
		t.Fatal("空 clientOrderID 應返回錯誤")
	}
}

func TestIsBinanceAuthError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{&common.APIError{Code: errCodeRejectedMBXKey}, true},
		{&common.APIError{Code: errCodeInvalidSignature}, true},
		{&common.APIError{Code: binanceErrCodeUnknownOrder}, false},
		{errors.New("network"), false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := isBinanceAuthError(tt.err); got != tt.want {
			t.Errorf("isBinanceAuthError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

// fakeSpotCancelServer 模擬現貨 / 槓桿撤單接口
type fakeSpotCancelServer struct {
	mu    sync.Mutex
	fails map[int64]int64 // orderID -> 錯誤碼
	paths []string
}

func (f *fakeSpotCancelServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	params := requestParams(r)
	f.paths = append(f.paths, r.URL.Path)
	orderID, _ := strconv.ParseInt(params.Get("orderId"), 10, 64)
	w.Header().Set("Content-Type", "application/json")
	if code, ok := f.fails[orderID]; ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(apiErrBody(code, "err"))
		return
	}
	if r.URL.Path == marginOrderEndpoint {
		// 槓桿撤單響應的 orderId 為字符串
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"orderId": strconv.FormatInt(orderID, 10), "symbol": "BTCUSDT"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"orderId": orderID, "symbol": "BTCUSDT"})
}

func TestSpotBatchCancelAggregatesErrors(t *testing.T) {
	fake := &fakeSpotCancelServer{fails: map[int64]int64{2: binanceErrCodeUnknownOrder, 3: guardErrCodeGeneric}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	client := binancesdk.NewClient("k", "s")
	client.BaseURL = srv.URL
	spot := &BinanceSpotAdapter{client: client, symbol: "BTCUSDT"}
	margin := &BinanceSpotMarginAdapter{BinanceSpotAdapter: spot}

	tests := []struct {
		name     string
		cancel   func(ctx context.Context, symbol string, ids []int64) error
		wantPath string
	}{
		{name: "spot", cancel: spot.BatchCancelOrders, wantPath: spotOrderEndpoint},
		{name: "margin 走槓桿撤單接口", cancel: margin.BatchCancelOrders, wantPath: marginOrderEndpoint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake.mu.Lock()
			fake.paths = nil
			fake.mu.Unlock()
			err := tt.cancel(context.Background(), "BTCUSDT", []int64{1, 2, 3})
			if err == nil || !strings.Contains(err.Error(), "1 of 3") || !strings.Contains(err.Error(), "order 3") {
				t.Fatalf("應只匯總訂單 3 的失敗, err=%v", err)
			}
			for _, p := range fake.paths {
				if p != tt.wantPath {
					t.Fatalf("撤單路徑 = %s, want %s", p, tt.wantPath)
				}
			}
		})
	}
}
