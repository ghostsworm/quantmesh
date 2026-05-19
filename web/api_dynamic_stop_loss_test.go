package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/risk"
)

func TestAdjustStopLossRejectsInvalidOrUnimplementedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orig := globalDynamicStopLossManager
	SetDynamicStopLossManager(risk.NewDynamicStopLossManager(&config.DynamicStopLossConfig{}, nil, nil))
	t.Cleanup(func() { SetDynamicStopLossManager(orig) })

	cases := []struct {
		name string
		body map[string]interface{}
		want int
	}{
		{
			name: "missing_bot_id",
			body: map[string]interface{}{"new_stop_loss": 0.1},
			want: http.StatusBadRequest,
		},
		{
			name: "negative_stop_loss",
			body: map[string]interface{}{"bot_id": "bot-1", "new_stop_loss": -0.1},
			want: http.StatusBadRequest,
		},
		{
			name: "over_100_percent_stop_loss",
			body: map[string]interface{}{"bot_id": "bot-1", "new_stop_loss": 1.2},
			want: http.StatusBadRequest,
		},
		{
			name: "valid_but_not_implemented",
			body: map[string]interface{}{"bot_id": "bot-1", "new_stop_loss": 0.1, "reason": "manual"},
			want: http.StatusNotImplemented,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.body)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/dynamic-stop-loss/adjust", bytes.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")

			adjustStopLoss(c)
			if w.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, w.Code, w.Body.String())
			}
		})
	}
}
