package huobi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type huobiRoundTripFunc func(*http.Request) (*http.Response, error)

func (f huobiRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newMockHuobiClient(handler func(*http.Request) (int, string)) *HuobiClient {
	client := NewHuobiClient("api-key", "secret-key")
	client.baseURL = "https://mock.huobi.local"
	client.httpClient = &http.Client{
		Transport: huobiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			status, body := handler(req)
			return &http.Response{
				StatusCode: status,
				Status:     http.StatusText(status),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(body)),
				Request:    req,
			}, nil
		}),
	}
	return client
}

func TestHuobiClientHTTPMethodsWithMockTransport(t *testing.T) {
	client := newMockHuobiClient(func(req *http.Request) (int, string) {
		switch req.URL.Path {
		case "/linear-swap-api/v1/swap_contract_info":
			requireHuobiSignedQuery(t, req)
			if req.URL.Query().Get("contract_code") != "BTC-USDT" {
				t.Fatalf("contract query = %s", req.URL.RawQuery)
			}
			return http.StatusOK, `{"status":"ok","data":[{"symbol":"BTC","contract_code":"BTC-USDT","price_tick":"0.1","contract_size":0.001}]}`
		case "/linear-swap-api/v1/swap_order":
			requireHuobiSignedQuery(t, req)
			return http.StatusOK, `{"status":"ok","data":{"order_id":11,"client_order_id":"cid-1"}}`
		case "/linear-swap-api/v1/swap_cancel":
			requireHuobiSignedQuery(t, req)
			return http.StatusOK, `{"status":"ok","data":{"successes":"11"}}`
		case "/linear-swap-api/v1/swap_order_info":
			requireHuobiSignedQuery(t, req)
			return http.StatusOK, `{"status":"ok","data":[{"order_id":11,"contract_code":"BTC-USDT","direction":"buy","offset":"open","price":65000}]}`
		case "/linear-swap-api/v1/swap_openorders":
			requireHuobiSignedQuery(t, req)
			return http.StatusOK, `{"status":"ok","data":{"orders":[{"order_id":12,"contract_code":"BTC-USDT","status":3}]}}`
		case "/linear-swap-api/v1/swap_account_info":
			requireHuobiSignedQuery(t, req)
			return http.StatusOK, `{"status":"ok","data":[{"symbol":"BTC","margin_balance":100,"margin_available":80}]}`
		case "/linear-swap-api/v1/swap_position_info":
			requireHuobiSignedQuery(t, req)
			return http.StatusOK, `{"status":"ok","data":[{"symbol":"BTC","contract_code":"BTC-USDT","volume":2,"direction":"buy"}]}`
		case "/linear-swap-api/v1/swap_funding_rate":
			requireHuobiSignedQuery(t, req)
			return http.StatusOK, `{"status":"ok","data":{"symbol":"BTC","contract_code":"BTC-USDT","funding_rate":"0.0001","funding_time":"1"}}`
		case "/linear-swap-ex/market/history/kline":
			if req.URL.Query().Get("period") != "1min" || req.URL.Query().Get("size") != "2" {
				t.Fatalf("kline query = %s", req.URL.RawQuery)
			}
			return http.StatusOK, `{"status":"ok","data":[{"id":1,"open":1,"high":2,"low":0.5,"close":1.5,"amount":10,"vol":20,"count":3}]}`
		case "/linear-swap-ex/market/detail/merged":
			return http.StatusOK, `{"status":"ok","tick":{"close":"65000.5","ts":9}}`
		case "/linear-swap-ex/market/depth":
			return http.StatusOK, `{"status":"ok","tick":{"bids":[[1,2]],"asks":[[3,4]],"ts":10}}`
		case "/v2/account/transfer":
			requireHuobiSignedQuery(t, req)
			if req.URL.Host != spotAPIHost {
				t.Fatalf("spot host = %s", req.URL.Host)
			}
			var body map[string]interface{}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode transfer body: %v", err)
			}
			if body["from"] != "spot" || body["to"] != "linear-swap" || body["margin-account"] != "USDT" {
				t.Fatalf("unexpected transfer body: %#v", body)
			}
			return http.StatusOK, `{"code":200,"success":true,"data":"tx-1"}`
		default:
			return http.StatusNotFound, `missing`
		}
	})

	ctx := context.Background()
	contracts, err := client.GetContractInfo(ctx, "BTC-USDT")
	if err != nil || len(contracts) != 1 || contracts[0].ContractCode != "BTC-USDT" {
		t.Fatalf("GetContractInfo() = %#v, %v", contracts, err)
	}
	placed, err := client.PlaceOrder(ctx, map[string]interface{}{"contract_code": "BTC-USDT"})
	if err != nil || placed.OrderId != 11 {
		t.Fatalf("PlaceOrder() = %#v, %v", placed, err)
	}
	if err := client.CancelOrder(ctx, "BTC-USDT", "11", ""); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	order, err := client.GetOrder(ctx, "BTC-USDT", "11", "")
	if err != nil || order.OrderId != 11 {
		t.Fatalf("GetOrder() = %#v, %v", order, err)
	}
	openOrders, err := client.GetOpenOrders(ctx, "BTC-USDT")
	if err != nil || len(openOrders) != 1 || openOrders[0].OrderId != 12 {
		t.Fatalf("GetOpenOrders() = %#v, %v", openOrders, err)
	}
	accounts, err := client.GetAccountInfo(ctx, "BTC-USDT")
	if err != nil || len(accounts) != 1 || accounts[0].MarginAvailable != 80 {
		t.Fatalf("GetAccountInfo() = %#v, %v", accounts, err)
	}
	positions, err := client.GetPositionInfo(ctx, "BTC-USDT")
	if err != nil || len(positions) != 1 || positions[0].Volume != 2 {
		t.Fatalf("GetPositionInfo() = %#v, %v", positions, err)
	}
	rate, err := client.GetFundingRate(ctx, "BTC-USDT")
	if err != nil || rate.FundingRate != "0.0001" {
		t.Fatalf("GetFundingRate() = %#v, %v", rate, err)
	}
	klines, err := client.GetKlines(ctx, "BTC-USDT", "1min", 2)
	if err != nil || len(klines) != 1 || klines[0].Close != 1.5 {
		t.Fatalf("GetKlines() = %#v, %v", klines, err)
	}
	closePrice, ts, err := client.GetPublicMergedClose(ctx, "BTC-USDT")
	if err != nil || closePrice != 65000.5 || ts != 9 {
		t.Fatalf("GetPublicMergedClose() = %v, %d, %v", closePrice, ts, err)
	}
	bids, asks, depthTS, err := client.GetPublicDepth(ctx, "BTC-USDT")
	if err != nil || depthTS != 10 || len(bids) != 1 || len(asks) != 1 {
		t.Fatalf("GetPublicDepth() bids=%#v asks=%#v ts=%d err=%v", bids, asks, depthTS, err)
	}
	txID, err := client.LinearSwapAccountTransfer(ctx, "USDT", 10, true)
	if err != nil || txID != "tx-1" {
		t.Fatalf("LinearSwapAccountTransfer() = %q, %v", txID, err)
	}
}

func requireHuobiSignedQuery(t *testing.T, req *http.Request) {
	t.Helper()
	if req.URL.Query().Get("AccessKeyId") != "api-key" || req.URL.Query().Get("Signature") == "" {
		t.Fatalf("signed query missing: %s", req.URL.RawQuery)
	}
}

func TestHuobiClientErrorAndParserBranches(t *testing.T) {
	t.Run("api and http errors", func(t *testing.T) {
		apiClient := newMockHuobiClient(func(req *http.Request) (int, string) {
			return http.StatusOK, `{"status":"error","err_code":100,"err_msg":"bad"}`
		})
		if _, err := apiClient.GetContractInfo(context.Background(), "BTC-USDT"); err == nil || !strings.Contains(err.Error(), "API 錯误 100") {
			t.Fatalf("expected API error, got %v", err)
		}

		httpClient := newMockHuobiClient(func(req *http.Request) (int, string) {
			return http.StatusBadGateway, `bad gateway`
		})
		if _, err := httpClient.GetContractInfo(context.Background(), "BTC-USDT"); err == nil || !strings.Contains(err.Error(), "HTTP 錯误 502") {
			t.Fatalf("expected HTTP error, got %v", err)
		}
	})

	t.Run("spot response variants", func(t *testing.T) {
		if data, err := parseHuobiSpotResponse([]byte(`{"status":"ok","data":{"x":1}}`)); err != nil || !strings.Contains(string(data), `"x":1`) {
			t.Fatalf("legacy spot parse = %s, %v", data, err)
		}
		if data, err := parseHuobiSpotResponse([]byte(`{"code":"200","success":false,"data":"tx-2"}`)); err != nil || string(data) != `"tx-2"` {
			t.Fatalf("string code spot parse = %s, %v", data, err)
		}
		if data, err := parseHuobiSpotResponse([]byte(`{"code":200,"success":false}`)); err != nil || !strings.Contains(string(data), `"code":200`) {
			t.Fatalf("empty data spot parse = %s, %v", data, err)
		}
		if _, err := parseHuobiSpotResponse([]byte(`{"code":500,"success":false,"message":"bad"}`)); err == nil || !strings.Contains(err.Error(), "code=500") {
			t.Fatalf("expected spot error, got %v", err)
		}
		if _, err := parseHuobiSpotResponse([]byte(`not-json`)); err == nil {
			t.Fatal("expected spot JSON error")
		}
	})

	t.Run("public endpoint errors", func(t *testing.T) {
		badStatus := newMockHuobiClient(func(req *http.Request) (int, string) {
			return http.StatusOK, `{"status":"error","tick":{}}`
		})
		if _, _, err := badStatus.GetPublicMergedClose(context.Background(), "BTC-USDT"); err == nil || !strings.Contains(err.Error(), "merged status") {
			t.Fatalf("expected merged status error, got %v", err)
		}
		if _, _, _, err := badStatus.GetPublicDepth(context.Background(), "BTC-USDT"); err == nil || !strings.Contains(err.Error(), "depth status") {
			t.Fatalf("expected depth status error, got %v", err)
		}

		badClose := newMockHuobiClient(func(req *http.Request) (int, string) {
			return http.StatusOK, `{"status":"ok","tick":{"close":"bad","ts":1}}`
		})
		if _, _, err := badClose.GetPublicMergedClose(context.Background(), "BTC-USDT"); err == nil || !strings.Contains(err.Error(), "解析 close") {
			t.Fatalf("expected close parse error, got %v", err)
		}
	})
}
