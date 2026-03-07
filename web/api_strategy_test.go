package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type mockStrategyRuntimeProvider struct {
	statuses []StrategyRuntimeStatusResponse
	err      error
}

func (m *mockStrategyRuntimeProvider) GetAllStrategyStatus(exchange, symbol string) ([]StrategyRuntimeStatusResponse, error) {
	return m.statuses, m.err
}

func (m *mockStrategyRuntimeProvider) GetAllStrategyStatusAll() ([]SymbolStrategyRuntimeItem, error) {
	return nil, nil
}

func (m *mockStrategyRuntimeProvider) GetStrategyStatus(exchange, symbol, strategyName string) (*StrategyRuntimeStatusResponse, error) {
	return nil, nil
}

func TestGetStrategyRuntimeStatusHandler_NotFoundReturns200WithEmptyStrategies(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originProvider := strategyRuntimeProvider
	defer func() { strategyRuntimeProvider = originProvider }()
	strategyRuntimeProvider = &mockStrategyRuntimeProvider{
		err: errors.New("交易對 binance:BTCUSDT 未找到"),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/strategies/runtime?exchange=binance&symbol=BTCUSDT", nil)

	getStrategyRuntimeStatusHandler(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, `"success":true`) {
		t.Fatalf("expected success true, got body: %s", body)
	}
	if !strings.Contains(body, `"strategies":[]`) {
		t.Fatalf("expected empty strategies, got body: %s", body)
	}
}

func TestGetStrategyRuntimeStatusHandler_OtherErrorsStillReturn500(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originProvider := strategyRuntimeProvider
	defer func() { strategyRuntimeProvider = originProvider }()
	strategyRuntimeProvider = &mockStrategyRuntimeProvider{
		err: errors.New("database timeout"),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/strategies/runtime?exchange=binance&symbol=BTCUSDT", nil)

	getStrategyRuntimeStatusHandler(c)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", w.Code)
	}
}

