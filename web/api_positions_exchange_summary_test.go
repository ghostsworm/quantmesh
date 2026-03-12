package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetExchangePositionsSummary_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		exchange string
		symbol   string
		wantCode int
	}{
		{"missing both", "", "", http.StatusBadRequest},
		{"missing exchange", "", "BTCUSDT", http.StatusBadRequest},
		{"missing symbol", "binance", "", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest(http.MethodGet, "/api/positions/exchange-summary", nil)
			q := req.URL.Query()
			if tt.exchange != "" {
				q.Set("exchange", tt.exchange)
			}
			if tt.symbol != "" {
				q.Set("symbol", tt.symbol)
			}
			req.URL.RawQuery = q.Encode()
			c.Request = req

			getExchangePositionsSummary(c)

			if w.Code != tt.wantCode {
				t.Errorf("getExchangePositionsSummary() = %d, want %d", w.Code, tt.wantCode)
			}
		})
	}
}
