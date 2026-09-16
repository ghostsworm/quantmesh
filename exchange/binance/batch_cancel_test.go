package binance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/adshao/go-binance/v2/common"
	"github.com/adshao/go-binance/v2/futures"
)

const (
	testCancelSymbol     = "BTCUSDT"
	batchCancelEndpoint  = "/fapi/v1/batchOrders"
	singleCancelEndpoint = "/fapi/v1/order"
	testErrCodeGeneric   = -1000
)

type cancelResult struct {
	status int
	body   interface{}
}

// fakeCancelServer 模擬 Binance 撤單接口
type fakeCancelServer struct {
	mu          sync.Mutex
	batchStatus int
	batchBody   interface{}
	single      map[int64]cancelResult
	singleCalls []int64
}

func requestParams(r *http.Request) url.Values {
	params := r.URL.Query()
	body, _ := io.ReadAll(r.Body)
	if bodyParams, err := url.ParseQuery(string(body)); err == nil {
		for k, v := range bodyParams {
			params[k] = v
		}
	}
	return params
}

func (f *fakeCancelServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	params := requestParams(r)

	writeJSON := func(status int, body interface{}) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}

	switch r.URL.Path {
	case batchCancelEndpoint:
		writeJSON(f.batchStatus, f.batchBody)
	case singleCancelEndpoint:
		orderID, _ := strconv.ParseInt(params.Get("orderId"), 10, 64)
		f.singleCalls = append(f.singleCalls, orderID)
		res, ok := f.single[orderID]
		if !ok {
			res = cancelResult{status: http.StatusOK, body: map[string]interface{}{"orderId": orderID}}
		}
		writeJSON(res.status, res.body)
	default:
		writeJSON(http.StatusNotFound, map[string]interface{}{"code": testErrCodeGeneric, "msg": "not found"})
	}
}

func newTestCancelAdapter(t *testing.T, fake *fakeCancelServer) *BinanceAdapter {
	t.Helper()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	client := futures.NewClient("test-key", "test-secret")
	client.BaseURL = srv.URL
	return &BinanceAdapter{client: client}
}

func apiErrBody(code int64, msg string) map[string]interface{} {
	return map[string]interface{}{"code": code, "msg": msg}
}

func TestBatchCancelOrders(t *testing.T) {
	tests := []struct {
		name            string
		fake            *fakeCancelServer
		orderIDs        []int64
		wantErr         bool
		wantErrContains []string
		wantNotContains []string
		wantSingleCalls []int64
	}{
		{
			name: "全部成功",
			fake: &fakeCancelServer{
				batchStatus: http.StatusOK,
				batchBody:   []map[string]interface{}{{"orderId": 1}, {"orderId": 2}},
			},
			orderIDs: []int64{1, 2},
		},
		{
			name: "部分條目失敗：-2011 視為成功，其他錯誤返回",
			fake: &fakeCancelServer{
				batchStatus: http.StatusOK,
				batchBody: []interface{}{
					map[string]interface{}{"orderId": 1},
					apiErrBody(binanceErrCodeUnknownOrder, "Unknown order sent."),
					apiErrBody(testErrCodeGeneric, "boom"),
				},
				single: map[int64]cancelResult{
					2: {status: http.StatusBadRequest, body: apiErrBody(binanceErrCodeUnknownOrder, "Unknown order sent.")},
					3: {status: http.StatusBadRequest, body: apiErrBody(testErrCodeGeneric, "boom")},
				},
			},
			orderIDs:        []int64{1, 2, 3},
			wantErr:         true,
			wantErrContains: []string{"cancel order 3", "1 of 3"},
			wantNotContains: []string{"cancel order 2"},
			wantSingleCalls: []int64{2, 3},
		},
		{
			name: "批量請求失敗後逐個撤單全部不存在",
			fake: &fakeCancelServer{
				batchStatus: http.StatusBadRequest,
				batchBody:   apiErrBody(testErrCodeGeneric, "batch down"),
				single: map[int64]cancelResult{
					1: {status: http.StatusBadRequest, body: apiErrBody(binanceErrCodeUnknownOrder, "Unknown order sent.")},
					2: {status: http.StatusBadRequest, body: apiErrBody(binanceErrCodeUnknownOrder, "Unknown order sent.")},
				},
			},
			orderIDs:        []int64{1, 2},
			wantSingleCalls: []int64{1, 2},
		},
		{
			name: "單個訂單失敗返回錯誤",
			fake: &fakeCancelServer{
				single: map[int64]cancelResult{
					9: {status: http.StatusBadRequest, body: apiErrBody(testErrCodeGeneric, "boom")},
				},
			},
			orderIDs:        []int64{9},
			wantErr:         true,
			wantErrContains: []string{"cancel order 9"},
			wantSingleCalls: []int64{9},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := newTestCancelAdapter(t, tt.fake)
			err := adapter.BatchCancelOrders(context.Background(), testCancelSymbol, tt.orderIDs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("BatchCancelOrders err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				for _, s := range tt.wantErrContains {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("錯誤 %q 應包含 %q", err, s)
					}
				}
				for _, s := range tt.wantNotContains {
					if strings.Contains(err.Error(), s) {
						t.Errorf("錯誤 %q 不應包含 %q", err, s)
					}
				}
				var apiErr *common.APIError
				if !errors.As(err, &apiErr) {
					t.Errorf("錯誤應可通過 errors.As 取得 *common.APIError: %v", err)
				}
			}
			if len(tt.wantSingleCalls) > 0 {
				got := tt.fake.singleCalls
				if len(got) != len(tt.wantSingleCalls) {
					t.Fatalf("逐個撤單調用 = %v, want %v", got, tt.wantSingleCalls)
				}
				for i := range got {
					if got[i] != tt.wantSingleCalls[i] {
						t.Fatalf("逐個撤單調用 = %v, want %v", got, tt.wantSingleCalls)
					}
				}
			}
		})
	}
}

func TestIsBinanceUnknownOrderError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "APIError -2011", err: &common.APIError{Code: binanceErrCodeUnknownOrder, Message: "Unknown order sent."}, want: true},
		{name: "包裝後的 APIError -2011", err: errors.Join(errors.New("ctx"), &common.APIError{Code: binanceErrCodeUnknownOrder}), want: true},
		{name: "其他 APIError", err: &common.APIError{Code: testErrCodeGeneric, Message: "boom"}, want: false},
		{name: "普通錯誤", err: errors.New("network down"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isBinanceUnknownOrderError(tt.err); got != tt.want {
				t.Fatalf("isBinanceUnknownOrderError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
