package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"

	"github.com/gin-gonic/gin"
)

type cancelFixtureTransport func(*http.Request) (*http.Response, error)

func (f cancelFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestBatchCancelOrders_ExchangeFromConfigWhenGetterReturnsNil 驗證：當 exchangeGetterFunc 返回 nil（無運行中的 bot）時，
// 從 globalConfig 按需創建交易所，不再報「交易所不存在」
func TestBatchCancelOrders_ExchangeFromConfigWhenGetterReturnsNil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Exercise the real config-created SDK without any external request, even
	// during server-time/metadata initialization. Unknown endpoints fail closed.
	previousTransport := http.DefaultTransport
	cancelRequests := 0
	http.DefaultTransport = cancelFixtureTransport(func(req *http.Request) (*http.Response, error) {
		var body string
		switch req.URL.Path {
		case "/fapi/v1/time":
			body = fmt.Sprintf(`{"serverTime":%d}`, time.Now().UnixMilli())
		case "/fapi/v1/exchangeInfo":
			body = `{"symbols":[{"symbol":"ETHUSDT","baseAsset":"ETH","quoteAsset":"USDT","pricePrecision":2,"quantityPrecision":3,"filters":[]}]}`
		case "/fapi/v1/order":
			params := req.URL.Query()
			if req.Body != nil {
				data, err := io.ReadAll(req.Body)
				if err != nil {
					return nil, fmt.Errorf("fixture cannot read request body")
				}
				bodyParams, err := url.ParseQuery(string(data))
				if err != nil {
					return nil, fmt.Errorf("fixture cannot parse request body")
				}
				for key, values := range bodyParams {
					params[key] = values
				}
			}
			if req.Method != http.MethodDelete || params.Get("symbol") != "ETHUSDT" || params.Get("orderId") != "12345" {
				return nil, fmt.Errorf("unexpected fixture cancellation identity")
			}
			cancelRequests++
			body = `{"symbol":"ETHUSDT","orderId":12345,"status":"CANCELED"}`
		default:
			return nil, fmt.Errorf("fixture rejects unknown endpoint")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	// 保存並恢復原始狀態
	origGetter := exchangeGetterFunc
	origConfig := globalConfig
	defer func() {
		exchangeGetterFunc = origGetter
		globalConfig = origConfig
	}()

	// 設置 getter 返回 nil（模擬無運行中的 binance bot）
	exchangeGetterFunc = func(_ string) exchange.IExchange {
		return nil
	}

	// 設置 minimal binance 配置（用於按需創建交易所）
	globalConfig = &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {
				APIKey:    "test-key",
				SecretKey: "test-secret",
				Testnet:   true,
			},
		},
	}

	body, _ := json.Marshal(map[string]interface{}{
		"order_ids":   []int64{12345},
		"exchange":    "binance",
		"symbol":      "ETHUSDT",
		"market_type": "futures",
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/orders/cancel", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	batchCancelOrders(c)

	// 關鍵斷言：不應返回「交易所不存在」（修復前會返回 400 + 該錯誤）
	bodyStr := w.Body.String()
	if strings.Contains(bodyStr, `"message":"交易所不存在: binance"`) {
		t.Fatalf("不應返回「交易所不存在」，當 globalConfig 有 binance 配置時應從配置創建交易所。body: %s", bodyStr)
	}
	if w.Code != http.StatusOK || cancelRequests != 1 {
		t.Fatalf("config-created exchange did not execute fixture cancellation: status=%d requests=%d", w.Code, cancelRequests)
	}
}
