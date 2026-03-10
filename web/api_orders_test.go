package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"

	"github.com/gin-gonic/gin"
)

// TestBatchCancelOrders_ExchangeFromConfigWhenGetterReturnsNil 驗證：當 exchangeGetterFunc 返回 nil（無運行中的 bot）時，
// 從 globalConfig 按需創建交易所，不再報「交易所不存在」
func TestBatchCancelOrders_ExchangeFromConfigWhenGetterReturnsNil(t *testing.T) {
	gin.SetMode(gin.TestMode)

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
	// 實際取消可能因訂單不存在/API 等失敗，但至少應通過「交易所存在」檢查
	// 若為 400 且是交易所不存在，上面已排除；若為 500 則是交易所調用失敗，屬預期
}
