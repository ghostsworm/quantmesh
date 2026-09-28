package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

type manualCloseHTTPProvider struct {
	SymbolManagerProvider
	result *ClosePositionsResponse
	err    error
}

func (p manualCloseHTTPProvider) ClosePositions(string, string) (*ClosePositionsResponse, error) {
	return p.result, p.err
}

func TestManualCloseHTTPDoesNotHideVerificationFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := symbolManagerProvider
	t.Cleanup(func() { symbolManagerProvider = original })
	for _, tc := range []struct {
		name   string
		result *ClosePositionsResponse
		err    error
		code   int
	}{
		{"unknown", nil, errors.New("close remains unverified"), http.StatusInternalServerError},
		{"partial evidence with error", &ClosePositionsResponse{SuccessCount: 1}, errors.New("final query unavailable"), http.StatusInternalServerError},
		{"confirmed", &ClosePositionsResponse{SuccessCount: 1}, nil, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			symbolManagerProvider = manualCloseHTTPProvider{result: tc.result, err: tc.err}
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/positions/close-all?exchange=binance&symbol=BTCUSDT", nil)
			closeAllPositions(c)
			if w.Code != tc.code {
				t.Fatalf("HTTP=%d body=%s", w.Code, w.Body.String())
			}
			var body map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.err != nil {
				if _, ok := body["success_count"]; ok || body["error"] == nil {
					t.Fatalf("failure carries success: %v", body)
				}
			} else if body["success_count"] != float64(1) {
				t.Fatalf("lost confirmed count: %v", body)
			}
		})
	}
}
