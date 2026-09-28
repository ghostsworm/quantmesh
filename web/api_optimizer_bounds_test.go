package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/backtest/optimizer"
)

func TestOptimizerAPIRejectsOversizedSearchBeforeStartingTask(t *testing.T) {
	req := OptimizerRunRequest{
		Symbol: "BTCUSDT", Exchange: "binance", Interval: "1h",
		StartTime: time.Unix(1000, 0), EndTime: time.Unix(2000, 0), InitialCapital: 1000,
		SearchSpace: optimizer.OptimSearchSpace{
			PriceLowRange:  optimizer.Range{Min: 1, Max: 1e9, Step: 0.01},
			PriceHighRange: optimizer.Range{Min: 1e9, Max: 2e9, Step: 1},
			GridCountRange: optimizer.IntRange{Min: 1, Max: 2, Step: 1},
			OrderQtyRange:  optimizer.Range{Min: 1, Max: 2, Step: 1},
		}, Config: optimizer.DefaultOptimConfig(),
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/optimizer/run", postOptimizerRun)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/optimizer/run", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "resource limit") {
		t.Fatalf("expected early rejection, got %d %s", recorder.Code, recorder.Body.String())
	}
}
