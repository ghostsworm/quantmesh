package web

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestStrategyCapitalReleaseHTTPContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, all := range []bool{false, true} {
		for _, scenario := range []string{"legacy", "failure", "partial", "success", "cancelled", "invalid result"} {
			t.Run(scenario+map[bool]string{false: "/single", true: "/all"}[all], func(t *testing.T) {
				previous := strategyProvider
				defer func() { strategyProvider = previous }()
				calls := 0
				amount := 0.0
				var releaseErr error
				if scenario == "partial" || scenario == "success" {
					amount = 200
				}
				if scenario == "failure" || scenario == "partial" {
					releaseErr = errors.New("internal verification detail must not reach HTTP client")
				}
				if scenario == "invalid result" {
					amount = math.NaN()
				}
				allocation := func() map[string]StrategyCapitalInfo { return nil }
				strategyProvider = NewVerifiedStrategyProviderAdapter(allocation,
					func(context.Context, string) (float64, error) { calls++; return amount, releaseErr },
					func(context.Context) (map[string]float64, error) {
						calls++
						return map[string]float64{"dca": amount}, releaseErr
					},
				)
				if scenario == "legacy" {
					strategyProvider = NewStrategyProviderAdapter(allocation,
						func(string) float64 { calls++; return 200 },
						func() map[string]float64 { calls++; return map[string]float64{"dca": 200} },
					)
				}
				router := gin.New()
				path := "/api/strategies/dca/release-capital"
				if all {
					path = "/api/strategies/release-all-capital"
					router.POST(path, releaseAllStrategiesCapital)
				} else {
					router.POST("/api/strategies/:id/release-capital", releaseStrategyCapital)
				}
				request := httptest.NewRequest(http.MethodPost, path, nil)
				if scenario == "cancelled" {
					ctx, cancel := context.WithCancel(request.Context())
					cancel()
					request = request.WithContext(ctx)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				wantStatus := http.StatusConflict
				if scenario == "success" {
					wantStatus = http.StatusOK
				}
				if scenario == "invalid result" {
					wantStatus = http.StatusInternalServerError
				}
				var body map[string]interface{}
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if response.Code != wantStatus || body["success"] != (scenario == "success") || strings.Contains(response.Body.String(), "internal verification detail") {
					t.Fatalf("release result misreported: status=%d body=%s", response.Code, response.Body.String())
				}
				wantCalls := 1
				if scenario == "legacy" || scenario == "cancelled" {
					wantCalls = 0
				}
				if calls != wantCalls {
					t.Fatalf("unexpected release callback count=%d want=%d", calls, wantCalls)
				}
				if scenario == "partial" && (body["partial"] != true || body["total_released"] != float64(200)) {
					t.Fatalf("partial mutation was concealed: %s", response.Body.String())
				}
			})
		}
	}
}
